// APCP/AVPT protocol and codec adapted from beefkvm (MIT); native transport.
package avocent

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"oldipmi/internal/avocent/apcp"
	"oldipmi/internal/avocent/video"
	"oldipmi/internal/protocol"
	"sync"
	"time"
)

type Client struct {
	p              protocol.Profile
	password       string
	control, video *protocol.Wire
	mu             sync.Mutex
	pressed        map[uint32]bool
	once           sync.Once
}

func Connect(ctx context.Context, p protocol.Profile, password string, jnlp []byte) (c *Client, err error) {
	c = &Client{p: p, password: password, pressed: map[uint32]bool{}}
	defer func() {
		if err != nil {
			c.Close()
		}
	}()
	if len(jnlp) != 0 {
		return c, fmt.Errorf("Avocent currently requires account login rather than JNLP import")
	}
	port := 5900
	if p.Family == "cisco" {
		port = 2068
	}
	open := func(kind apcp.SessionType, skip bool) (*apcp.Session, error) {
		var stop func() bool
		s := &apcp.Session{Type: kind, SkipLogin: skip, Username: p.Username, Password: password}
		s.Dial = func() (net.Conn, error) {
			raw, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(p.Host, fmt.Sprint(port)))
			if e == nil {
				raw.SetDeadline(time.Now().Add(15 * time.Second))
				stop = context.AfterFunc(ctx, func() { raw.Close() })
			}
			return raw, e
		}
		s.UpgradeTLS = func(raw net.Conn) (net.Conn, error) {
			cfg := protocol.TLSConfig(p)
			t := tls.Client(raw, cfg)
			return t, t.HandshakeContext(ctx)
		}

		defer func() {
			if stop != nil {
				stop()
			}
		}()
		if e := s.Open(); e != nil {
			return nil, e
		}
		if !s.Setup.SupportsSSL() {
			s.Conn.Close()
			return nil, fmt.Errorf("Avocent console requires TLS")
		}
		if !skip && s.KVMLoginResponse.Status != 0 {
			s.Conn.Close()
			return nil, fmt.Errorf("Avocent login: %s", apcp.LoginStatusName(s.KVMLoginResponse.Status))
		}
		s.Conn.SetDeadline(time.Time{})
		return s, nil
	}
	s, e := open(apcp.SessionTypeKVM, false)
	if e != nil {
		return c, e
	}
	c.control = &protocol.Wire{Conn: s.Conn}
	c.video = c.control
	if s.KVMLoginResponse.NeedsCompanion {
		sub, e := open(apcp.SessionTypeKVMVMSubchannel, true)
		if e != nil {
			return c, e
		}
		c.video = &protocol.Wire{Conn: sub.Conn}
		if e = c.video.WritePacket(apcp.EncodeVideoSyncDC(s.KVMSessionID, s.KVMLoginResponse.SessionID)); e != nil {
			return c, e
		}
	}
	for _, f := range []apcp.AVPTFrame{{ID: 782, Payload: []byte{1, 1, 0, 0, 0, 0, 0, 0}}, {ID: 770, Payload: []byte{4, 0, 3, 0, 0, 0, 0, 0}}, {ID: 772, Payload: make([]byte, 8)}} {
		if e = c.control.WritePacket(f.Encode()); e != nil {
			return c, e
		}
	}
	return c, nil
}
func (c *Client) Run(ctx context.Context, emit func(protocol.Frame)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { c.control.Close(); c.video.Close() })
	defer stop()
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if c.control.WritePacket(apcp.KVMHeartbeat()) != nil {
					cancel()
					return
				}
				if c.video != c.control && c.video.WritePacket(apcp.KVMHeartbeat()) != nil {
					cancel()
					return
				}
			}
		}
	}()
	if c.video != c.control {
		go func() {
			for {
				if _, e := apcp.ReadAVPTFrame(c.control); e != nil {
					cancel()
					return
				}
			}
		}()
	}
	fb := video.NewFramebuffer(1024, 768)
	rgb := video.NewDecoder(video.Depth15)
	gray := video.NewDecoder(video.Depth7Gray)
	pal := video.NewDecoder(video.Depth7Palette)

	var agg []byte
	var ack byte
	for {
		f, e := apcp.ReadAVPTFrame(c.video)
		if e != nil {
			return e
		}

		ack++
		if e = c.video.WritePacket((apcp.AVPTFrame{ID: 0, Payload: []byte{ack, 0, 0, 0, 0, 0, 0, 0}}).Encode()); e != nil {
			return e
		}
		if f.ID == 134 || f.ID == 34310 {
			return fmt.Errorf("unsupported Avocent packet-134 codec")
		} else if f.ID == 129 || f.ID == 130 || f.ID == 131 || f.ID == 34305 || f.ID == 34306 || f.ID == 34307 {
			if len(f.Payload) < 12 {
				return fmt.Errorf("short Avocent video packet")
			}
			height := int(binary.BigEndian.Uint16(f.Payload[4:]))
			width := int(binary.BigEndian.Uint16(f.Payload[6:]))
			if width < 1 || height < 1 || width > 4096 || height > 2160 {
				return fmt.Errorf("invalid Avocent framebuffer dimensions")
			}
			fb.Resize(width, height)
			if f.Payload[8]&1 != 0 {
				fb.Cur = 0
				agg = nil
			}

			d := rgb
			if f.ID == 130 || f.ID == 34306 {
				d = pal
			}
			if f.ID == 131 || f.ID == 34307 {
				d = gray
			}
			if len(agg)+len(f.Payload)-12 > protocol.MaxPayload {
				return fmt.Errorf("oversized Avocent frame")
			}
			agg = append(agg, f.Payload[12:]...)
			if f.Payload[8]&2 == 0 {
				continue
			}
			if _, e = d.Decode(fb, agg); e != nil {
				return e
			}
		} else if f.ID == 138 || f.ID == 34314 {
			return fmt.Errorf("Avocent VGA palette updates are unsupported")
		}
		if fb.Dirty {
			pix := make([]byte, len(fb.Pixels)*4)
			for i, v := range fb.Pixels {
				pix[i*4] = byte(v >> 16)
				pix[i*4+1] = byte(v >> 8)
				pix[i*4+2] = byte(v)
				pix[i*4+3] = 255
			}
			emit(protocol.Frame{Width: fb.W, Height: fb.H, Pixels: pix})
			fb.Dirty = false
		}
	}
}
func (c *Client) key(u uint32, down bool) error {
	b := make([]byte, 8)
	if !down {
		b[1] = 1
	}
	binary.BigEndian.PutUint16(b[2:], uint16(u))
	return c.control.WritePacket((apcp.AVPTFrame{ID: 512, Payload: b}).Encode())
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
	return c.key(i.Usage, i.Down)
}
func (c *Client) Pointer(i protocol.Input) error {
	b := make([]byte, 8)
	b[1] = i.Buttons & 7
	binary.BigEndian.PutUint16(b[2:], uint16(max(0, min(65535, i.X))))
	binary.BigEndian.PutUint16(b[4:], uint16(max(0, min(65535, i.Y))))
	binary.BigEndian.PutUint16(b[6:], uint16(int16(max(-1, min(1, i.Wheel)))))
	return c.control.WritePacket((apcp.AVPTFrame{ID: 513, Payload: b}).Encode())
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
	return c.Pointer(protocol.Input{})
}
func (c *Client) Mount(context.Context, string) error {
	return fmt.Errorf("Avocent virtual media is not implemented yet")
}
func (c *Client) Eject() error  { return nil }
func (c *Client) Mounted() bool { return false }
func (c *Client) Power(ctx context.Context, action string) error {
	return protocol.Power(ctx, c.p, c.password, action)
}
func (c *Client) Close() error {
	c.once.Do(func() {
		if c.control != nil {
			c.control.Close()
		}
		if c.video != nil {
			c.video.Close()
		}
		c.password = ""
	})
	return nil
}
