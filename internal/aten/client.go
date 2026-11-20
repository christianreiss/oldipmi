// ATEN wire framing adapted from ATENtion (MIT); no vendor runtime.
package aten

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"oldipmi/internal/hid"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"oldipmi/internal/video/ast"
	"oldipmi/internal/video/raster"
	"sync"
	"time"
)

type Client struct {
	p           protocol.Profile
	web         *web
	launch      Launch
	wire        *transport.Wire
	mu          sync.Mutex
	pressed     hid.Pressed
	controlling bool
	dec         decoder
	media       *mediaSession
	once        sync.Once
	ast         *ast.Decoder // HTML5 mode video decoder
	reqW, reqH  int          // HTML5 mode update-request size
	lastPix     []byte       // last emitted HTML5 frame, to drop unchanged ones
	mouseLen    int          // mouse-status (0x37) body length, 0 until known
}

func Connect(ctx context.Context, p protocol.Profile, password string) (c *Client, err error) {
	c = &Client{p: p, controlling: true}
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
	err = c.attach(ctx)
	return c, err
}

// attach dials the console described by c.launch and runs the handshake.
func (c *Client) attach(ctx context.Context) (err error) {
	p := c.p
	info := c.launch.ConsoleInfo(p)
	protocol.Report(ctx, protocol.StageConnecting, &info)
	if c.launch.HTML5 {
		c.wire, err = dialHTML5(ctx, p, c.launch)
	} else {
		c.wire, err = transport.Dial(ctx, p.Policy(), c.launch.Port, c.launch.Secure)
	}
	if err == nil {
		err = c.negotiate(ctx)
	}
	if err != nil && c.launch.Redfish && ctx.Err() == nil {
		// Verified on two X12/X13-generation BMCs; other firmware may differ.
		err = fmt.Errorf("%w (this console was found through the Supermicro Redfish iKVM resource, as on X12/X13 boards; if it keeps failing, use the BMC web interface's Remote Console and report the BMC model and firmware version)", err)
	}
	return err
}

// negotiate performs the RFB-style handshake on the dialed console connection.
func (c *Client) negotiate(ctx context.Context) (err error) {
	stop := context.AfterFunc(ctx, func() { c.wire.Close() })
	defer stop()
	c.wire.SetDeadline(time.Now().Add(15 * time.Second))
	banner, err := protocol.Read(c.wire, 12)
	if err != nil {
		return err
	}
	if string(banner[:4]) != "RFB " {
		return fmt.Errorf("unrecognized ATEN banner")
	}
	if err = c.wire.WritePacket(banner); err != nil {
		return err
	}
	count, err := protocol.Read(c.wire, 1)
	if err != nil {
		return err
	}
	if count[0] == 0 {
		return fmt.Errorf("ATEN refused session")
	}
	offered, err := protocol.Read(c.wire, int(count[0]))
	if err != nil {
		return err
	}
	found := false
	for _, t := range offered {
		if t == 16 {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("unsupported ATEN security negotiation")
	}
	if err = c.wire.WritePacket([]byte{16}); err != nil {
		return err
	}
	if _, err = protocol.Read(c.wire, 24); err != nil {
		return err
	}
	if len(c.launch.Token) > 24 || len(c.launch.PasswordToken) > 24 {
		return fmt.Errorf("ATEN token exceeds credential field")
	}
	if err = c.wire.WritePacket(c.tokenAuth()); err != nil {
		return err
	}
	result, err := protocol.Read(c.wire, 4)
	if err != nil {
		return err
	}
	if binary.BigEndian.Uint32(result) != 0 {
		return fmt.Errorf("the BMC rejected the console session token (it expired or was already used); reconnect to obtain a fresh one")
	}
	shared := byte(0)
	if c.launch.HTML5 {
		shared = 1
	}
	if err = c.wire.WritePacket([]byte{shared}); err != nil {
		return err
	}
	init, err := protocol.Read(c.wire, 24)
	if err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(init[20:])
	if n > 65536 {
		return fmt.Errorf("invalid ATEN desktop name length")
	}
	if _, err = protocol.Read(c.wire, int(n)+12); err != nil {
		return err
	}
	c.wire.SetDeadline(time.Time{})
	if c.launch.HTML5 {
		c.reqW, c.reqH = int(binary.BigEndian.Uint16(init)), int(binary.BigEndian.Uint16(init[2:]))
		return c.updateRequest(false)
	}
	c.dec.Resize(1024, 768)
	err = c.syncPointer()
	if err == nil {
		err = c.updateRequest(false)
	}
	return err
}

// updateRequest asks for the next framebuffer update of the whole screen, an
// incremental one for what changed only: the HTML5 console for the size of its
// last frame (at first the size the server announced), the Java one for the
// size of the decoder's frame.
func (c *Client) updateRequest(incremental bool) error {
	w, h := c.dec.frame.Width, c.dec.frame.Height
	if c.launch.HTML5 {
		w, h = c.reqW, c.reqH
	}
	p := make([]byte, 10)
	p[0] = 3
	if incremental {
		p[1] = 1
	}
	binary.BigEndian.PutUint16(p[6:], uint16(w))
	binary.BigEndian.PutUint16(p[8:], uint16(h))
	return c.wire.WritePacket(p)
}

// syncPointer sends the mouse sync message (0x37) of the web UI.
func (c *Client) syncPointer() error {
	return c.wire.WritePacket([]byte{0x37, 7, 0x07, 0x80})
}
func (c *Client) Run(ctx context.Context, frame func(protocol.Frame)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !c.launch.HTML5 { // the HTML5 console sends no keepalive
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
	}

	stop := context.AfterFunc(ctx, func() { c.wire.Close() })
	defer stop()
	rd := &lookahead{r: c.wire}
	for {
		t, e := protocol.Read(rd, 1)
		if e != nil {
			return e
		}
		switch t[0] {
		case 0:
			b, e := protocol.Read(rd, 3)
			if e != nil {
				return e
			}
			n := int(binary.BigEndian.Uint16(b[1:]))
			if n > 4096 {
				return fmt.Errorf("too many ATEN rectangles")
			}
			changed := false
			for i := 0; i < n; i++ {
				h, e := protocol.Read(rd, 20)
				if e != nil {
					return e
				}
				size := binary.BigEndian.Uint32(h[16:])
				if size > protocol.MaxPayload {
					return fmt.Errorf("ATEN frame too large")
				}
				b, e := protocol.Read(rd, int(size))
				if e != nil {
					return e
				}
				if len(b) == 0 {
					continue
				}
				x, y, w, hgt := int(binary.BigEndian.Uint16(h)), int(binary.BigEndian.Uint16(h[2:])), int(binary.BigEndian.Uint16(h[4:])), int(binary.BigEndian.Uint16(h[6:]))
				if c.launch.HTML5 {
					if binary.BigEndian.Uint32(h[8:]) != encAST2100 {
						return fmt.Errorf("unsupported HTML5 video encoding %d", binary.BigEndian.Uint32(h[8:]))
					}
					f, e := c.decodeAST2100(w, hgt, b)
					if e != nil {
						return e
					}
					c.reqW, c.reqH = f.Width, f.Height
					if bytes.Equal(f.Pixels, c.lastPix) {
						// The BMC answers every request, even without changes; pace it.
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(40 * time.Millisecond):
						}
						continue
					}
					if c.lastPix == nil { // like the web UI: sync the pointer once the size is known
						if e = c.syncPointer(); e != nil {
							return e
						}
					}
					c.lastPix = f.Pixels
					frame(f)
					continue
				}
				if x == 0 && y == 0 && raster.ValidSize(w, hgt) {
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
			if e = c.updateRequest(true); e != nil {
				return e
			}
		case 4:
			b, e := protocol.Read(rd, 20)
			if e != nil {
				return e
			}
			w, h := binary.BigEndian.Uint32(b[8:]), binary.BigEndian.Uint32(b[12:])
			if w > 256 || h > 256 {
				return fmt.Errorf("oversized cursor")
			}
			if binary.BigEndian.Uint32(b[16:]) == 1 {
				if _, e = protocol.Read(rd, 4+int(w*h*2)); e != nil {
					return e
				}
			}
		case 0x37:
			n, e := c.mouseStatusLen(rd)
			if e != nil {
				return e
			}
			if _, e = protocol.Read(rd, n); e != nil {
				return e
			}
		case 0x16, 0x35, 0x3c:
			if _, e = protocol.Read(rd, bodySizes[t[0]]); e != nil {
				return e
			}
		case 0x39:
			b, e := protocol.Read(rd, 264)
			if e != nil {
				return e
			}
			c.mu.Lock()
			c.controlling = !(binary.BigEndian.Uint32(b) == 1 && binary.BigEndian.Uint32(b[4:]) == 4)
			c.mu.Unlock()
		default:
			if c.skipUnknown() {
				continue
			}
			return fmt.Errorf("unsupported ATEN message 0x%02x", t[0])
		}
	}
}

// bodySizes are the fixed body lengths of the messages 0x16, 0x35 and 0x3c,
// which this client reads past.
var bodySizes = map[byte]int{0x16: 1, 0x35: 5, 0x3c: 8}

// lookahead returns peeked bytes before reading on.
type lookahead struct {
	r   io.Reader
	buf []byte
}

func (l *lookahead) Read(p []byte) (int, error) {
	if len(l.buf) > 0 {
		n := copy(p, l.buf)
		l.buf = l.buf[n:]
		return n, nil
	}
	return l.r.Read(p)
}

// mouseStatusLen returns the body length of the mouse-status message (0x37).
// Most firmware sends crypto, mode and status (3 bytes, as the noVNC clients
// read it); old ATEN firmware (seen live: firmware 2.50, iKVM 1.69.13) sends
// only 2, with the same banner, security type and server init. The bytes after
// the first 0x37 decide once per session: after a 2-byte body comes a message
// type, normally a framebuffer update with rectangles; after a 3-byte body
// comes a status byte (0 or 1) and then a message whose next bytes read as
// "zero rectangles". HTML5 consoles always use 3.
func (c *Client) mouseStatusLen(rd *lookahead) (int, error) {
	if c.launch.HTML5 {
		return 3, nil
	}
	if c.mouseLen == 0 {
		b, e := protocol.Read(rd, 6)
		if e != nil {
			return 0, e
		}
		rd.buf = append(b, rd.buf...)
		c.mouseLen = 3
		switch b[2] {
		case 0:
			if binary.BigEndian.Uint16(b[4:]) > 0 {
				c.mouseLen = 2
			}
		case 4, 0x16, 0x35, 0x37, 0x39, 0x3c:
			c.mouseLen = 2
		}
	}
	return c.mouseLen, nil
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
	if !hid.ValidKey(i.Usage) {
		return hid.ErrInvalidUsage
	}
	c.pressed.Set(i.Usage, i.Down)
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
	for _, u := range c.pressed.Keys() {
		if e := c.key(u, false); e != nil {
			return e
		}
	}
	c.pressed.Clear()
	return c.pointer(protocol.Input{})
}

// powerCodes are what the console protocol sends for each power action.
var powerCodes = map[protocol.PowerAction]byte{protocol.PowerOff: 0, protocol.PowerOn: 1, protocol.PowerReset: 2, protocol.PowerSoft: 3}

func (c *Client) Power(ctx context.Context, action protocol.PowerAction) error {
	v, ok := powerCodes[action]
	if !ok {
		return protocol.ErrUnknownPowerAction
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
	if c.launch.HTML5 {
		return fmt.Errorf("virtual media is not available in the ATEN HTML5 console mode")
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
