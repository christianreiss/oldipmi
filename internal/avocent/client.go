// APCP/AVPT protocol and codec adapted from beefkvm (MIT); native transport.
package avocent

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"oldipmi/internal/avocent/apcp"
	"oldipmi/internal/avocent/video"
	"oldipmi/internal/hid"
	"oldipmi/internal/power"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"os"
	"sync"
	"time"
)

type Client struct {
	// IPMI is the chassis power control (Power) with the account of the
	// login; Close forgets its password.
	power.IPMI
	p              protocol.Profile
	control, video *transport.Wire
	mu             sync.Mutex
	pressed        hid.Pressed
	once           sync.Once
}

func Connect(ctx context.Context, p protocol.Profile, password string) (c *Client, err error) {
	c = &Client{IPMI: power.NewIPMI(p, password), p: p}
	defer func() {
		if err != nil {
			c.Close()
		}
	}()
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
		s.UpgradeTLS = func(raw net.Conn) (net.Conn, error) { return transport.UpgradeTLS(ctx, raw, p.Policy()) }

		defer func() {
			if stop != nil {
				stop()
			}
		}()
		if e := s.Open(); e != nil {
			return nil, e
		}
		if !skip && s.KVMLoginResponse.Status != 0 {
			s.Conn.Close()
			err := fmt.Errorf("Avocent login: %s", apcp.LoginStatusName(s.KVMLoginResponse.Status))
			// INVALID_USER_NAME and INVALID_PASSWORD. An account that may not use
			// the console (CHANNEL_ACCESS_DENIED) needs no other password.
			if st := s.KVMLoginResponse.Status; st == 1 || st == 2 {
				err = protocol.Mark(protocol.CodeAuth, err)
			}
			return nil, err
		}
		s.Conn.SetDeadline(time.Time{})
		return s, nil
	}
	protocol.Report(ctx, protocol.StageLoggingIn, nil)
	s, e := open(apcp.SessionTypeKVM, false)
	if e != nil {
		return c, e
	}
	protocol.Report(ctx, protocol.StageDiscovering, nil)
	info := protocol.ConsoleInfo{Protocol: "Avocent APCP", LegacyTLS: p.LegacyTLS,
		ProtocolVersion: fmt.Sprintf("%d.%d", s.Setup.VersionMajor, s.Setup.VersionMinor),
		Companion:       s.KVMLoginResponse.NeedsCompanion,
		Endpoints:       []protocol.Endpoint{protocol.ConsoleEndpoint("Console", p.Host, port, true)},
		Capabilities:    []string{"Video", "Keyboard", "Pointer"}}
	protocol.Report(ctx, protocol.StageConnecting, &info)
	c.control = &transport.Wire{Conn: s.Conn}
	c.video = c.control
	if s.KVMLoginResponse.NeedsCompanion {
		sub, e := open(apcp.SessionTypeKVMVMSubchannel, true)
		if e != nil {
			return c, e
		}
		c.video = &transport.Wire{Conn: sub.Conn}
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

// OLDIPMI_TRACE_AVPT=1 logs AVPT packet types and the first 12 payload bytes
// (the header) to stderr for diagnosis; never screen content.
var trace = os.Getenv("OLDIPMI_TRACE_AVPT") == "1"
var dumped int

func head(b []byte) []byte {
	if len(b) > 12 {
		return b[:12]
	}
	return b
}

func (c *Client) Run(ctx context.Context, emit func(protocol.Frame)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { c.control.Close(); c.video.Close() })
	defer stop()
	go transport.Keepalive(ctx, 5*time.Second, func() error {
		if e := c.control.WritePacket(apcp.KVMHeartbeat()); e != nil || c.video == c.control {
			return e
		}
		return c.video.WritePacket(apcp.KVMHeartbeat())
	}, func(error) { cancel() })
	if c.video != c.control {
		go func() {
			for {
				f, e := apcp.ReadAVPTFrame(c.control)
				if e != nil {
					if trace {
						fmt.Fprintf(os.Stderr, "avpt control closed: %v\n", e)
					}
					cancel()
					return
				}
				if trace {
					fmt.Fprintf(os.Stderr, "avpt control id=%d len=%d head=% x\n", f.ID, len(f.Payload), head(f.Payload))
				}
			}
		}()
	}
	fb := video.NewFramebuffer(1024, 768)
	stream := video.NewStream(protocol.MaxPayload)
	var rle video.RVAS

	var ack byte
	for {
		f, e := apcp.ReadAVPTFrame(c.video)
		if e != nil {
			return e
		}
		if trace {
			fmt.Fprintf(os.Stderr, "%s avpt video id=%d len=%d head=% x\n", time.Now().Format("15:04:05.000"), f.ID, len(f.Payload), head(f.Payload))
			// Developer-only: payloads contain screen content; mode 0600, never by default.
			if dir := os.Getenv("OLDIPMI_TRACE_AVPT_DUMP"); dir != "" {
				dumped++
				if fh, e := os.OpenFile(fmt.Sprintf("%s/%04d-%d.bin", dir, dumped, f.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); e == nil {
					fh.Write(f.Payload)
					fh.Close()
				}
			}
		}

		ack++
		if e = c.video.WritePacket((apcp.AVPTFrame{ID: apcp.IDVideoAck, Payload: []byte{ack, 0, 0, 0, 0, 0, 0, 0}}).Encode()); e != nil {
			return e
		}
		switch id := apcp.VideoID(f.ID); id {
		case apcp.IDUnsupportedCodec:
			return fmt.Errorf("unsupported Avocent packet-134 codec")
		case apcp.IDRLEVideo:
			// Cisco IMC (seen on APCP 2.34): the RLE / bit-slice format, not
			// the 129-131 opcode stream.
			skipped := rle.Skipped
			if _, e = rle.Feed(fb, f.Payload); e != nil {
				return e
			}
			if trace && rle.Skipped != skipped {
				fmt.Fprintf(os.Stderr, "avpt rle: skipped packet, waiting for a reference frame\n")
			}
		case apcp.IDRLEPalette:
			if e = rle.SetPalette(f.Payload); e != nil {
				return e
			}
		case apcp.IDRLEFont:
			// Font table for RLE text mode; text mode itself reports an error.
		case apcp.IDVideo15, apcp.IDVideoPalette, apcp.IDVideoGray:
			done, e := stream.Feed(fb, videoDepths[id], f.Payload)
			if e != nil {
				return e
			}
			if !done {
				continue
			}
		case apcp.IDVGAPalette:
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

// videoDepths are the colour depths of the video packets 129-131.
var videoDepths = map[uint16]video.Depth{apcp.IDVideo15: video.Depth15, apcp.IDVideoPalette: video.Depth7Palette, apcp.IDVideoGray: video.Depth7Gray}

func (c *Client) key(u uint32, down bool) error {
	b := make([]byte, 8)
	if !down {
		b[1] = 1
	}
	binary.BigEndian.PutUint16(b[2:], uint16(u))
	return c.control.WritePacket((apcp.AVPTFrame{ID: apcp.IDKey, Payload: b}).Encode())
}
func (c *Client) Key(i protocol.Input) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !hid.ValidKey(i.Usage) {
		return hid.ErrInvalidUsage
	}
	c.pressed.Set(i.Usage, i.Down)
	return c.key(i.Usage, i.Down)
}
func (c *Client) Pointer(i protocol.Input) error {
	b := make([]byte, 8)
	b[1] = i.Buttons & 7
	binary.BigEndian.PutUint16(b[2:], uint16(max(0, min(65535, i.X))))
	binary.BigEndian.PutUint16(b[4:], uint16(max(0, min(65535, i.Y))))
	binary.BigEndian.PutUint16(b[6:], uint16(int16(max(-1, min(1, i.Wheel)))))
	return c.control.WritePacket((apcp.AVPTFrame{ID: apcp.IDPointer, Payload: b}).Encode())
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
	return c.Pointer(protocol.Input{})
}
func (c *Client) Close() error {
	c.once.Do(func() {
		if c.control != nil {
			c.control.Close()
		}
		if c.video != nil {
			c.video.Close()
		}
		c.IPMI.Close()
	})
	return nil
}
