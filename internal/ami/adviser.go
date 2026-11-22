package ami

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/video/ast"
)

// The adviser console of older MegaRAC firmware (ASUS ASMB4, for one) frames
// its packets like the legacy VSC, in 7 bytes (type, size, status), but
// numbers them differently and carries AST2100 video. It is plain TCP on one
// port for video and input.
//
// The protocol was worked out, for interoperability, by decompiling the
// vendor's Java viewer (JViewer.jar from an ASMB4 BMC, firmware 2.15) and
// reading what it sends and expects; the handshake and the video were then
// checked against a live ASMB4. This code is written anew from that
// description: no vendor code is copied, and no vendor binary or decompiled
// source is distributed.
//
// The client speaks first: its session token and the additional session
// argument, both from the JNLP. The console answers with advValidateResp and then streams
// video unasked. Its numbers overlap the VSC's with other meanings (33 is the
// VSC's ping, 34 its pong), so the two never share a code path beyond the
// framing.
const (
	advVideo         = 5  // a fragment of a video frame
	advHID           = 6  // a HID report (IUSB), client to console
	advStop          = 25 // either side ends the session
	advEncryption    = 29 // another session turned keyboard/mouse encryption on
	advEncryptionNow = 30 // keyboard/mouse encryption is on from the start
	advBandwidthResp = 32 // bandwidth probe answer: status blocks of size bytes
	advMouseMode     = 33 // the console's mouse mode, one byte
	advToken         = 34 // the session token, zero-padded to advTokenSize
	advValidateResp  = 35 // byte 0: 0 rejects the session
	advMaxSessions   = 49 // no free console session
	advSessionArg    = 52 // additional JNLP session argument
)

// advTokenSize is the session token's field in advToken; the JNLP's token
// must fit it.
const advTokenSize = 16

// Mouse modes the console announces with advMouseMode.
const (
	mouseUnknown  byte = 0
	mouseRelative byte = 1
	mouseAbsolute byte = 2
)

// advVideoHeaderSize is the console's own header before each frame's ASPEED
// video header: 19 reserved bytes, the signature "ASP-2000", the header's
// length, the frame's, a command, a status and two reserved bytes.
const advVideoHeaderSize = 39

var errAdvEncryption = fmt.Errorf("this BMC encrypts keyboard and mouse input, which OldIPMI does not support")
var errAdvMaxSessions = fmt.Errorf("BMC has no free console session; close other viewers and try again")

// attachAdviser sends the session tokens and waits for the console to accept
// them. The mouse mode may come first.
func (c *Client) attachAdviser() error {
	tok := make([]byte, advTokenSize)
	copy(tok, c.launch.Token)
	if err := c.send(c.wire, advToken, tok); err != nil {
		return err
	}
	if err := c.send(c.wire, advSessionArg, []byte(c.launch.AdviserSessionArg)); err != nil {
		return err
	}
	for range 20 {
		t, _, b, err := c.read(c.wire)
		if err != nil {
			return err
		}
		switch t {
		case advValidateResp:
			if len(b) == 0 || b[0] == 0 {
				return fmt.Errorf("AMI rejected the console session token")
			}
			c.mu.Lock()
			c.validated = true
			c.mu.Unlock()
			return nil
		case advMaxSessions:
			return errAdvMaxSessions
		case advStop:
			return fmt.Errorf("AMI stopped native session")
		case advMouseMode:
			c.setMouseMode(b)
		}
	}
	return fmt.Errorf("AMI authentication response missing")
}

func (c *Client) setMouseMode(b []byte) {
	if len(b) > 0 && (b[0] == mouseRelative || b[0] == mouseAbsolute) {
		c.mu.Lock()
		c.mouseMode = b[0]
		c.mu.Unlock()
	}
}

// runAdviser reads the console until it ends: video, the mouse mode, and
// what ends the session. The rest it skips.
func (c *Client) runAdviser(onFrame func(protocol.Frame)) error {
	var frame assembler
	d := ast.New()
	for {
		t, st, b, err := c.read(c.wire)
		if err != nil {
			return err
		}
		switch t {
		case advStop:
			return fmt.Errorf("BMC ended session (%d)", st)
		case advMaxSessions:
			return errAdvMaxSessions
		case advMouseMode:
			c.setMouseMode(b)
		case advVideo:
			whole, done, err := frame.add(b)
			if err != nil {
				return err
			}
			if !done {
				continue
			}
			f, err := decodeAdviser(d, whole)
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.width, c.height = f.Width, f.Height
			c.mu.Unlock()
			onFrame(f)
		}
	}
}

// decodeAdviser decodes a frame behind the console's video header.
func decodeAdviser(d *ast.Decoder, frame []byte) (protocol.Frame, error) {
	if len(frame) < advVideoHeaderSize || !bytes.Equal(frame[19:27], []byte("ASP-2000")) {
		return protocol.Frame{}, fmt.Errorf("AMI adviser frame without its ASP-2000 header")
	}
	return decodeModern(d, frame[advVideoHeaderSize:])
}

// adviserPointer sends the pointer in the console's mouse mode: absolute
// positions scaled to 0..32767, or relative steps of at most 127 from where
// the client last put it. Before the console names its mode the pointer
// sends nothing, as the vendor's viewer does. c.mu is held.
func (c *Client) adviserPointer(i protocol.Input) error {
	buttons := i.Buttons & 7
	switch c.mouseMode {
	case mouseAbsolute:
		c.lastX, c.lastY = max(0, min(i.X, c.width-1)), max(0, min(i.Y, c.height-1))
		return c.absolutePointer(buttons)
	case mouseRelative:
		x, y := max(0, min(i.X, c.width-1)), max(0, min(i.Y, c.height-1))
		dx, dy := x-c.lastX, y-c.lastY
		for first := true; first || dx != 0 || dy != 0; first = false {
			sx, sy := max(-127, min(127, dx)), max(-127, min(127, dy))
			if err := c.hidPacket(49, 32, 1, []byte{buttons, byte(int8(sx)), byte(int8(sy))}); err != nil {
				return err
			}
			dx, dy = dx-sx, dy-sy
			c.lastX, c.lastY = c.lastX+sx, c.lastY+sy
		}
	}
	return nil
}

// absolutePointer sends the buttons at the pointer's last position. c.mu is
// held.
func (c *Client) absolutePointer(buttons byte) error {
	r := make([]byte, 5)
	r[0] = buttons
	binary.LittleEndian.PutUint16(r[1:], uint16(min(c.lastX, c.width-1)*32767/max(1, c.width-1)))
	binary.LittleEndian.PutUint16(r[3:], uint16(min(c.lastY, c.height-1)*32767/max(1, c.height-1)))
	return c.hidPacket(49, 32, 1, r)
}

// adviserRelease lets go of the pointer's buttons in the console's mouse
// mode, where the pointer is. c.mu is held.
func (c *Client) adviserRelease() error {
	switch c.mouseMode {
	case mouseAbsolute:
		return c.absolutePointer(0)
	case mouseRelative:
		return c.hidPacket(49, 32, 1, make([]byte, 3))
	}
	return nil
}
