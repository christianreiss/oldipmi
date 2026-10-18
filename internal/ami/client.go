package ami

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"oldipmi/internal/ami/codec"
	"oldipmi/internal/ami/vmedia"
	"oldipmi/internal/protocol"
	"sync"
	"time"
)

type Client struct {
	p             protocol.Profile
	launch        protocol.Launch
	web           *protocol.Web
	wire, hid     *protocol.Wire
	mu            sync.Mutex
	pressed       map[uint32]bool
	width, height int
	seq           uint32
	media         *vmedia.Session
	mediaCancel   context.CancelFunc
	once          sync.Once
	password      string
}

func Connect(ctx context.Context, p protocol.Profile, password string, jnlp []byte) (c *Client, err error) {
	c = &Client{p: p, pressed: map[uint32]bool{}, width: 1024, height: 768, password: password}
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
	if c.launch.Family != "ami" {
		return c, fmt.Errorf("this JNLP requires the ATEN adapter")
	}
	if c.launch.Args["singleportenabled"] == "1" {
		return c, fmt.Errorf("AMI single-port tunneling is not supported by this firmware adapter")
	}
	c.wire, err = protocol.Dial(ctx, p, c.launch.Port, c.launch.Secure)
	if err != nil {
		return c, err
	}
	stop := context.AfterFunc(ctx, func() { c.wire.Close() })
	defer stop()
	c.wire.SetDeadline(time.Now().Add(15 * time.Second))
	if c.launch.Legacy {
		t, st, _, e := c.read(c.wire)
		if e != nil {
			return c, e
		}
		if t != 14 || st != 0 {
			return c, fmt.Errorf("AMI video prelogin refused (%d/%d)", t, st)
		}
		c.hid, err = protocol.Dial(ctx, p, c.launch.HIDPort, true)
		if err != nil {
			return c, err
		}
		c.hid.SetDeadline(time.Now().Add(15 * time.Second))
		t, st, _, err = c.read(c.hid)
		if err != nil {
			return c, err
		}
		if t != 14 || st != 0 {
			return c, fmt.Errorf("AMI HID prelogin refused (%d/%d)", t, st)
		}
		c.hid.SetDeadline(time.Time{})
		auth := make([]byte, 99)
		copy(auth[1:], c.launch.Token)
		if err = c.send(c.wire, 12, auth); err != nil {
			return c, err
		}
		cookie := c.launch.Cookie
		if len(c.launch.Pos) > 10 {
			cookie = c.launch.Pos[10]
		}
		if cookie != "" {
			if err = c.send(c.wire, 32, []byte(cookie)); err != nil {
				return c, err
			}
		}
		if err = c.send(c.wire, 11, []byte{8}); err != nil {
			return c, err
		}
		err = c.send(c.wire, 7, nil)
	} else {
		for i := 0; i < 20; i++ {
			t, _, b, e := c.read(c.wire)
			if e != nil {
				return c, e
			}
			switch t {
			case 23:
				if err = c.send(c.wire, 58, nil); err != nil {
					return c, err
				}
				b := make([]byte, 373)
				copy(b[1:130], c.launch.Token)
				host, _, _ := net.SplitHostPort(c.wire.LocalAddr().String())
				copy(b[130:195], host)
				copy(b[195:324], p.Username)
				if err = c.send(c.wire, 18, b); err != nil {
					return c, err
				}
			case 19:
				if len(b) == 0 || b[0] != 1 {
					return c, fmt.Errorf("AMI rejected native session")
				}
				err = c.send(c.wire, 6, nil)
				i = 20
			case 8:
				return c, fmt.Errorf("AMI stopped native session")
			}
			if i == 19 {
				return c, fmt.Errorf("AMI authentication response missing")
			}
		}
	}
	c.wire.SetDeadline(time.Time{})
	return c, err
}
func (c *Client) read(w *protocol.Wire) (uint16, uint16, []byte, error) {
	n := 8
	if c.launch.Legacy {
		n = 7
	}
	b, e := protocol.Read(w, n)
	if e != nil {
		return 0, 0, nil, e
	}
	off := 2
	t := binary.LittleEndian.Uint16(b)
	if n == 7 {
		off = 1
		t = uint16(b[0])
	}
	size := binary.LittleEndian.Uint32(b[off:])
	st := binary.LittleEndian.Uint16(b[off+4:])
	if size > protocol.MaxPayload {
		return t, st, nil, fmt.Errorf("AMI packet too large")
	}
	b, e = protocol.Read(w, int(size))
	return t, st, b, e
}
func (c *Client) send(w *protocol.Wire, t uint16, b []byte) error {
	n, off := 8, 2
	if c.launch.Legacy {
		n, off = 7, 1
	}
	p := make([]byte, n+len(b))
	if n == 7 {
		p[0] = byte(t)
	} else {
		binary.LittleEndian.PutUint16(p, t)
	}
	binary.LittleEndian.PutUint32(p[off:], uint32(len(b)))
	copy(p[n:], b)
	return w.WritePacket(p)
}
func (c *Client) Run(ctx context.Context, onFrame func(protocol.Frame)) error {
	stop := context.AfterFunc(ctx, func() {
		c.wire.Close()
		if c.hid != nil {
			c.hid.Close()
		}
	})
	defer stop()
	runctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.hid != nil {
		go func() {
			for {
				_, _, _, e := c.read(c.hid)
				if e != nil {
					return
				}
			}
		}()
	}
	if !c.launch.Legacy {
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-runctx.Done():
					return
				case <-t.C:
					c.send(c.wire, 57, nil)
				}
			}
		}()
	}
	var fragments []byte
	next := 0
	modern := codec.New()
	legacy := legacyDecoder{}
	for {
		t, st, b, e := c.read(c.wire)
		if e != nil {
			return e
		}
		video := uint16(25)
		if c.launch.Legacy {
			video = 3
			if t == 33 {
				if e = c.send(c.wire, 34, nil); e != nil {
					return e
				}
			}
			if t == 9 {
				return fmt.Errorf("BMC ended session (%d)", st)
			}
		} else if t == 8 {
			return fmt.Errorf("BMC ended session (%d)", st)
		}
		if t != video {
			continue
		}
		if len(b) < 2 {
			return fmt.Errorf("short AMI fragment")
		}
		seq := int(binary.LittleEndian.Uint16(b))
		idx := seq & 0x7fff
		if idx == 0 {
			fragments = nil
			next = 0
		}
		if idx != next {
			return fmt.Errorf("AMI video fragment sequence mismatch")
		}
		next++
		if len(fragments)+len(b)-2 > protocol.MaxPayload {
			return fmt.Errorf("AMI frame exceeds limit")
		}
		fragments = append(fragments, b[2:]...)
		if seq&0x8000 != 0 {
			var f protocol.Frame
			if c.launch.Legacy {
				f, e = legacy.Decode(fragments)
			} else {
				var cf *codec.Frame
				cf, e = modern.Decode(fragments)
				if e == nil {
					f = protocol.Frame{Width: cf.W, Height: cf.H, Pixels: cf.Pix}
					for i := 3; i < len(f.Pixels); i += 4 {
						f.Pixels[i] = 255
					}
				}
			}
			if e != nil {
				return e
			}
			c.mu.Lock()
			c.width, c.height = f.Width, f.Height
			c.mu.Unlock()
			onFrame(f)
			fragments = nil
		}
	}
}
func (c *Client) hidPacket(device, proto, iface byte, report []byte) error {
	p := make([]byte, 33+len(report))
	copy(p, []byte("IUSB    "))
	p[8] = 1
	p[10] = 32
	binary.LittleEndian.PutUint32(p[12:], uint32(len(report)+1))
	p[17] = device
	p[18] = proto
	p[19] = 128
	p[20] = 2
	p[21] = iface
	c.seq++
	binary.LittleEndian.PutUint32(p[24:], c.seq)
	p[32] = byte(len(report))
	copy(p[33:], report)
	var sum byte
	for _, v := range p[:32] {
		sum += v
	}
	p[11] = -sum
	w, t := c.wire, uint16(1)
	if c.hid != nil {
		w, t = c.hid, 4
	}
	return c.send(w, t, p)
}
func (c *Client) Key(i protocol.Input) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i.Usage < 4 || i.Usage > 231 {
		return fmt.Errorf("invalid HID usage")
	}
	if i.Down {
		c.pressed[i.Usage] = true
	} else {
		delete(c.pressed, i.Usage)
	}
	return c.keyboard()
}
func (c *Client) keyboard() error {
	r := make([]byte, 8)
	n := 2
	for u := uint32(4); u <= 231; u++ {
		if !c.pressed[u] {
			continue
		}
		if u >= 224 {
			r[0] |= 1 << uint(u-224)
		} else if n < 8 {
			r[n] = byte(u)
			n++
		}
	}
	return c.hidPacket(48, 16, 0, r)
}
func (c *Client) Pointer(i protocol.Input) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := make([]byte, 6)
	r[0] = i.Buttons & 7
	binary.LittleEndian.PutUint16(r[1:], uint16(max(0, min(i.X, c.width-1))*32767/max(1, c.width-1)))
	binary.LittleEndian.PutUint16(r[3:], uint16(max(0, min(i.Y, c.height-1))*32767/max(1, c.height-1)))
	r[5] = byte(max(-1, min(1, i.Wheel)))
	return c.hidPacket(49, 32, 1, r)
}
func (c *Client) Release() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.pressed)
	e := c.keyboard()
	if e != nil {
		return e
	}
	return c.hidPacket(49, 32, 1, make([]byte, 6))
}
func (c *Client) Close() error {
	c.once.Do(func() {
		c.Eject()
		if c.wire != nil {
			c.wire.Close()
		}
		if c.hid != nil {
			c.hid.Close()
		}
		if c.web != nil {
			c.web.Close()
		}
		c.password = ""
	})
	return nil
}
func (c *Client) Power(ctx context.Context, action string) error {
	return protocol.Power(ctx, c.p, c.password, action)
}
func (c *Client) Mount(ctx context.Context, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.media != nil {
		return fmt.Errorf("eject the mounted ISO first")
	}
	file, e := vmedia.OpenFile(path)
	if e != nil {
		return e
	}
	if file.Size() < 2048 || file.Size()%2048 != 0 {
		file.Close()
		return fmt.Errorf("ISO size must be a positive multiple of 2048 bytes")
	}
	wire, e := protocol.Dial(ctx, c.p, c.launch.CDPort, c.launch.MediaSecure)
	if e != nil {
		file.Close()
		return e
	}
	s, e := vmedia.ConnectWire(ctx, wire, c.launch.Token)
	if e != nil {
		wire.Close()
		file.Close()
		return e
	}
	mctx, cancel := context.WithCancel(ctx)
	c.media = s
	c.mediaCancel = cancel
	go func() {
		defer file.Close()
		defer s.Close()
		defer cancel()
		s.Serve(mctx, vmedia.NewCDROM(file))
		c.mu.Lock()
		if c.media == s {
			c.media = nil
			c.mediaCancel = nil
		}
		c.mu.Unlock()
	}()
	return nil
}
func (c *Client) Eject() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mediaCancel != nil {
		c.mediaCancel()
	}
	if c.media != nil {
		c.media.Close()
	}
	c.media = nil
	c.mediaCancel = nil
	return nil
}

func (c *Client) Mounted() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.media != nil }
