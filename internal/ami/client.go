package ami

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"oldipmi/internal/ami/codec"
	"oldipmi/internal/ami/vmedia"
	"oldipmi/internal/hid"
	"oldipmi/internal/power"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"sync"
	"time"
)

type Client struct {
	// IPMI is the chassis power control (Power) with the account of the
	// login; Close forgets its password.
	power.IPMI
	p             protocol.Profile
	launch        Launch
	web           *web
	wire, hid     *transport.Wire
	mu            sync.Mutex
	pressed       hid.Pressed
	width, height int
	seq           uint32
	media         *vmedia.Session
	mediaCancel   context.CancelFunc
	once          sync.Once
}

func Connect(ctx context.Context, p protocol.Profile, password string) (c *Client, err error) {
	c = &Client{IPMI: power.NewIPMI(p, password), p: p, width: 1024, height: 768}
	defer func() {
		if err != nil {
			c.Close()
		}
	}()
	c.web, err = newWeb(p)
	if err == nil {
		c.launch, err = c.web.login(ctx, password)
	}
	if err != nil {
		return c, err
	}
	return c, c.attach(ctx)
}

// attach opens the console that the BMC's launch file describes.
func (c *Client) attach(ctx context.Context) (err error) {
	p := c.p
	if c.launch.Args["singleportenabled"] == "1" {
		return fmt.Errorf("AMI single-port tunneling is not supported by this firmware adapter")
	}
	info := c.launch.ConsoleInfo(p)
	protocol.Report(ctx, protocol.StageConnecting, &info)
	c.wire, err = transport.Dial(ctx, p.Policy(), c.launch.Port, c.launch.Secure)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.wire.Close() })
	defer stop()
	c.wire.SetDeadline(time.Now().Add(15 * time.Second))
	if c.launch.Legacy {
		t, st, _, e := c.read(c.wire)
		if e != nil {
			return e
		}
		if t != 14 || st != 0 {
			return fmt.Errorf("AMI video prelogin refused (%d/%d)", t, st)
		}
		c.hid, err = transport.Dial(ctx, p.Policy(), c.launch.HIDPort, true)
		if err != nil {
			return err
		}
		stopHID := context.AfterFunc(ctx, func() { c.hid.Close() })
		defer stopHID()
		c.hid.SetDeadline(time.Now().Add(15 * time.Second))
		t, st, _, err = c.read(c.hid)
		if err != nil {
			return err
		}
		if t != 14 || st != 0 {
			return fmt.Errorf("AMI HID prelogin refused (%d/%d)", t, st)
		}
		c.hid.SetDeadline(time.Time{})
		auth := make([]byte, 99)
		copy(auth[1:], c.launch.Token)
		if err = c.send(c.wire, 12, auth); err != nil {
			return err
		}
		cookie := c.launch.Cookie
		if len(c.launch.Pos) > 10 {
			cookie = c.launch.Pos[10]
		}
		if cookie != "" {
			if err = c.send(c.wire, 32, []byte(cookie)); err != nil {
				return err
			}
		}
		if err = c.send(c.wire, 11, []byte{8}); err != nil {
			return err
		}
		err = c.send(c.wire, 7, nil)
	} else {
		for i := 0; i < 20; i++ {
			t, _, b, e := c.read(c.wire)
			if e != nil {
				return e
			}
			switch t {
			case opSessionAccepted:
				if err = c.send(c.wire, opConnectionComplete, nil); err != nil {
					return err
				}
				b := make([]byte, validateVideoSize)
				copy(b[1:130], c.launch.Token)
				host, _, _ := net.SplitHostPort(c.wire.LocalAddr().String())
				copy(b[130:195], host)
				copy(b[195:324], p.Username)
				if err = c.send(c.wire, opValidateVideo, b); err != nil {
					return err
				}
			case opValidateVideoResp:
				if len(b) == 0 || b[0] != 1 {
					return fmt.Errorf("AMI rejected native session")
				}
				err = c.send(c.wire, opResumeRedirection, nil)
				i = 20
			case opStopSession:
				return fmt.Errorf("AMI stopped native session")
			}
			if i == 19 {
				return fmt.Errorf("AMI authentication response missing")
			}
		}
	}
	c.wire.SetDeadline(time.Time{})
	return err
}
func (c *Client) read(w *transport.Wire) (uint16, uint16, []byte, error) {
	n := ivtpHeaderSize
	if c.launch.Legacy {
		n = vscHeaderSize
	}
	b, e := protocol.Read(w, n)
	if e != nil {
		return 0, 0, nil, e
	}
	off := 2
	t := binary.LittleEndian.Uint16(b)
	if n == vscHeaderSize {
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
func (c *Client) send(w *transport.Wire, t uint16, b []byte) error {
	n, off := ivtpHeaderSize, 2
	if c.launch.Legacy {
		n, off = vscHeaderSize, 1
	}
	p := make([]byte, n+len(b))
	if n == vscHeaderSize {
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
					c.send(c.wire, opKeepAlive, nil)
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
		video := opVideoFragment
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
		} else if t == opStopSession {
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
	w, t := c.wire, opHIDPkt
	if c.hid != nil {
		w, t = c.hid, 4
	}
	return c.send(w, t, p)
}
func (c *Client) Key(i protocol.Input) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !hid.ValidKey(i.Usage) {
		return hid.ErrInvalidUsage
	}
	c.pressed.Set(i.Usage, i.Down)
	return c.keyboard()
}
func (c *Client) keyboard() error {
	r := c.pressed.BootReport()
	return c.hidPacket(48, 16, 0, r[:])
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
	c.pressed.Clear()
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
		c.IPMI.Close()
	})
	return nil
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
	wire, e := transport.Dial(ctx, c.p.Policy(), c.launch.CDPort, c.launch.MediaSecure)
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
		if err := s.Serve(mctx, vmedia.NewCDROM(file)); err != nil && mctx.Err() == nil {
			log.Printf("virtual media ended: %v", err)
		}
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
