// ATEN wire framing adapted from ATENtion (MIT); no vendor runtime.
package aten

import (
	"context"
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"sync"
	"time"
)

type Client struct {
	p           protocol.Profile
	web         *protocol.Web
	launch      protocol.Launch
	wire        *protocol.Wire
	mu          sync.Mutex
	pressed     map[uint32]bool
	controlling bool
	dec         decoder
	media       *mediaSession
	once        sync.Once
}

func Connect(ctx context.Context, p protocol.Profile, password string, jnlp []byte) (c *Client, err error) {
	c = &Client{p: p, pressed: map[uint32]bool{}, controlling: true}
	defer func() {
		if err != nil {
			c.Close()
		}
	}()
	if len(jnlp) > 0 {
		c.launch, err = protocol.ParseJNLP(jnlp)
	} else {
		c.web, err = protocol.NewWeb(p)
		if err == nil {
			c.launch, err = c.web.Login(ctx, password)
		}
	}
	if err != nil {
		return c, err
	}
	if c.launch.Family != "aten" {
		return c, fmt.Errorf("this JNLP requires the AMI adapter")
	}
	c.wire, err = protocol.Dial(ctx, p, c.launch.Port, c.launch.Secure)
	if err != nil {
		return c, err
	}
	stop := context.AfterFunc(ctx, func() { c.wire.Close() })
	defer stop()
	c.wire.SetDeadline(time.Now().Add(15 * time.Second))
	banner, err := protocol.Read(c.wire, 12)
	if err != nil {
		return c, err
	}
	if string(banner[:4]) != "RFB " {
		return c, fmt.Errorf("unrecognized ATEN banner")
	}
	if err = c.wire.WritePacket(banner); err != nil {
		return c, err
	}
	count, err := protocol.Read(c.wire, 1)
	if err != nil {
		return c, err
	}
	if count[0] == 0 {
		return c, fmt.Errorf("ATEN refused session")
	}
	offered, err := protocol.Read(c.wire, int(count[0]))
	if err != nil {
		return c, err
	}
	found := false
	for _, t := range offered {
		if t == 16 {
			found = true
		}
	}
	if !found {
		return c, fmt.Errorf("unsupported ATEN security negotiation")
	}
	if err = c.wire.WritePacket([]byte{16}); err != nil {
		return c, err
	}
	if _, err = protocol.Read(c.wire, 24); err != nil {
		return c, err
	}
	auth := make([]byte, 48)
	if len(c.launch.Token) > 24 {
		return c, fmt.Errorf("ATEN token exceeds credential field")
	}
	copy(auth[:24], c.launch.Token)
	copy(auth[24:], c.launch.Token)
	if err = c.wire.WritePacket(auth); err != nil {
		return c, err
	}
	result, err := protocol.Read(c.wire, 4)
	if err != nil {
		return c, err
	}
	if binary.BigEndian.Uint32(result) != 0 {
		return c, fmt.Errorf("ATEN token rejected")
	}
	if err = c.wire.WritePacket([]byte{0}); err != nil {
		return c, err
	}
	init, err := protocol.Read(c.wire, 24)
	if err != nil {
		return c, err
	}
	n := binary.BigEndian.Uint32(init[20:])
	if n > 65536 {
		return c, fmt.Errorf("invalid ATEN desktop name length")
	}
	if _, err = protocol.Read(c.wire, int(n)+12); err != nil {
		return c, err
	}
	c.dec.Resize(1024, 768)
	c.wire.SetDeadline(time.Time{})
	err = c.wire.WritePacket([]byte{0x37, 7, 0x07, 0x80})
	if err == nil {
		err = c.request(false)
	}
	return c, err
}
func (c *Client) request(incremental bool) error {
	p := make([]byte, 10)
	p[0] = 3
	if incremental {
		p[1] = 1
	}
	binary.BigEndian.PutUint16(p[6:], uint16(c.dec.frame.Width))
	binary.BigEndian.PutUint16(p[8:], uint16(c.dec.frame.Height))
	return c.wire.WritePacket(p)
}
func (c *Client) Run(ctx context.Context, frame func(protocol.Frame)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if c.wire.WritePacket([]byte{0x15, 0, 0, 0, 1, 0, 0, 0, 0}) != nil {
					c.wire.Close()
					return
				}
			}
		}
	}()

	stop := context.AfterFunc(ctx, func() { c.wire.Close() })
	defer stop()
	for {
		t, e := protocol.Read(c.wire, 1)
		if e != nil {
			return e
		}
		switch t[0] {
		case 0:
			b, e := protocol.Read(c.wire, 3)
			if e != nil {
				return e
			}
			n := int(binary.BigEndian.Uint16(b[1:]))
			if n > 4096 {
				return fmt.Errorf("too many ATEN rectangles")
			}
			changed := false
			for i := 0; i < n; i++ {
				h, e := protocol.Read(c.wire, 20)
				if e != nil {
					return e
				}
				size := binary.BigEndian.Uint32(h[16:])
				if size > protocol.MaxPayload {
					return fmt.Errorf("ATEN frame too large")
				}
				b, e := protocol.Read(c.wire, int(size))
				if e != nil {
					return e
				}
				if len(b) == 0 {
					continue
				}
				x, y, w, hgt := int(binary.BigEndian.Uint16(h)), int(binary.BigEndian.Uint16(h[2:])), int(binary.BigEndian.Uint16(h[4:])), int(binary.BigEndian.Uint16(h[6:]))
				if x == 0 && y == 0 && w > 0 && hgt > 0 && w <= 4096 && hgt <= 2160 {
					if e = c.dec.Resize(w, hgt); e != nil {
						return e
					}
				}
				if e = c.dec.Decode(b); e != nil {
					return e
				}
				changed = true
			}
			if changed {
				frame(c.dec.frame)
			}
			if e = c.request(true); e != nil {
				return e
			}
		case 4:
			b, e := protocol.Read(c.wire, 20)
			if e != nil {
				return e
			}
			w, h := binary.BigEndian.Uint32(b[8:]), binary.BigEndian.Uint32(b[12:])
			if w > 256 || h > 256 {
				return fmt.Errorf("oversized cursor")
			}
			if binary.BigEndian.Uint32(b[16:]) == 1 {
				if _, e = protocol.Read(c.wire, 4+int(w*h*2)); e != nil {
					return e
				}
			}
		case 0x16, 0x35, 0x37, 0x3c:
			size := map[byte]int{0x16: 1, 0x35: 5, 0x37: 3, 0x3c: 8}[t[0]]
			if _, e = protocol.Read(c.wire, size); e != nil {
				return e
			}
		case 0x39:
			b, e := protocol.Read(c.wire, 264)
			if e != nil {
				return e
			}
			c.mu.Lock()
			c.controlling = !(binary.BigEndian.Uint32(b) == 1 && binary.BigEndian.Uint32(b[4:]) == 4)
			c.mu.Unlock()
		default:
			return fmt.Errorf("unsupported ATEN message 0x%02x", t[0])
		}
	}
}
func (c *Client) key(usage uint32, down bool) error {
	b := make([]byte, 18)
	b[0] = 4
	if down {
		b[2] = 1
	}
	binary.BigEndian.PutUint32(b[5:], usage)
	return c.wire.WritePacket(b)
}
func (c *Client) Key(i protocol.Input) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.controlling {
		return fmt.Errorf("BMC granted view-only access")
	}
	if i.Usage < 4 || i.Usage > 231 {
		return fmt.Errorf("invalid HID usage")
	}
	if i.Down {
		c.pressed[i.Usage] = true
	} else {
		delete(c.pressed, i.Usage)
	}
	return c.key(i.Usage, i.Down)
}
func (c *Client) Pointer(i protocol.Input) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.controlling {
		return fmt.Errorf("BMC granted view-only access")
	}
	return c.pointer(i)
}
func (c *Client) pointer(i protocol.Input) error {
	b := make([]byte, 18)
	b[0] = 5
	b[2] = (i.Buttons & 1) | ((i.Buttons & 2) << 1) | ((i.Buttons & 4) >> 1)
	if i.Wheel > 0 {
		b[2] |= 8
	} else if i.Wheel < 0 {
		b[2] |= 16
	}
	binary.BigEndian.PutUint16(b[3:], uint16(max(0, min(65535, i.X))))
	binary.BigEndian.PutUint16(b[5:], uint16(max(0, min(65535, i.Y))))
	return c.wire.WritePacket(b)
}
func (c *Client) Release() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for u := range c.pressed {
		if e := c.key(u, false); e != nil {
			return e
		}
	}
	clear(c.pressed)
	return c.pointer(protocol.Input{})
}
func (c *Client) Power(ctx context.Context, action string) error {
	codes := map[string]byte{"off": 0, "on": 1, "reset": 2, "soft": 3}
	v, ok := codes[action]
	if !ok {
		return fmt.Errorf("unknown power action")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.controlling {
		return fmt.Errorf("BMC granted view-only access")
	}
	return c.wire.WritePacket([]byte{0x1a, v})
}
func (c *Client) Close() error {
	c.once.Do(func() {
		c.Eject()
		if c.wire != nil {
			c.wire.Close()
		}
		if c.web != nil {
			c.web.Close()
		}
	})
	return nil
}
func (c *Client) Mount(ctx context.Context, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.media != nil {
		return fmt.Errorf("eject the mounted ISO first")
	}
	s, e := newMedia(ctx, c.p, c.launch.CDPort, path)
	if e != nil {
		return e
	}
	c.media = s
	go func() {
		s.serve()
		s.close()
		c.mu.Lock()
		if c.media == s {
			c.media = nil
		}
		c.mu.Unlock()
	}()
	return nil
}
func (c *Client) Eject() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.media != nil {
		c.media.close()
		c.media = nil
	}
	return nil
}

func (c *Client) Mounted() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.media != nil }
