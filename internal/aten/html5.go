package aten

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"oldipmi/internal/video/ast"

	"golang.org/x/net/websocket"
)

// ATEN firmware without Java iKVM serves a noVNC console: the same RFB-like
// stream as the Java client, carried in WebSocket messages and authenticated by
// the session token from the HTML5 console page. Video uses the ASPEED AST2100
// encoding (87), which internal/video/ast decodes.

const encAST2100 = 87

// wsConn presents WebSocket messages as a byte stream, remembering message
// boundaries so an unknown server message can be skipped safely.
type wsConn struct {
	*websocket.Conn
	buf []byte
}

func (c *wsConn) Read(p []byte) (int, error) {
	for len(c.buf) == 0 {
		var m []byte
		if err := websocket.Message.Receive(c.Conn, &m); err != nil {
			return 0, err
		}
		c.buf = m
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}
func (c *wsConn) Write(p []byte) (int, error) {
	if err := websocket.Message.Send(c.Conn, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// discard drops the unread rest of the current WebSocket message.
func (c *wsConn) discard() { c.buf = nil }

func dialHTML5(ctx context.Context, p protocol.Profile, l Launch) (*transport.Wire, error) {
	ws, err := transport.DialWS(ctx, p.Policy(), l.Port, l.Secure, l.Cookie)
	var dial *websocket.DialError
	if errors.As(err, &dial) {
		return nil, fmt.Errorf("the HTML5 console WebSocket on port %d failed: %w; check that the BMC web port is reachable and no other viewer holds the console, then retry", l.Port, err)
	}
	if err != nil {
		return nil, err
	}
	return &transport.Wire{Conn: &wsConn{Conn: ws}}, nil
}

// skipUnknown discards the rest of an unrecognised WebSocket message.
func (c *Client) skipUnknown() bool {
	if w, ok := c.wire.Conn.(*wsConn); ok && c.launch.HTML5 {
		w.discard()
		return true
	}
	return false
}

// decodeAST2100 decodes one HTML5 video rectangle. The payload starts with
// Y selector, UV selector and a 16-bit YUV mode (422/444), then the tile stream.
func (c *Client) decodeAST2100(width, height int, p []byte) (protocol.Frame, error) {
	if len(p) < 12 {
		return protocol.Frame{}, fmt.Errorf("short AST2100 frame")
	}
	mode := int(binary.BigEndian.Uint16(p[2:]))
	if mode != 422 && mode != 444 {
		return protocol.Frame{}, fmt.Errorf("unsupported AST2100 YUV mode %d", mode)
	}
	stream := p[4:]
	if pad := (4 - len(stream)%4) % 4; pad > 0 {
		stream = append(append([]byte(nil), stream...), make([]byte, pad)...)
	}
	hdr := make([]byte, 86)
	le := binary.LittleEndian
	le.PutUint16(hdr[4:], uint16(width))
	le.PutUint16(hdr[6:], uint16(height))
	le.PutUint16(hdr[13:], uint16(width))
	le.PutUint16(hdr[15:], uint16(height))
	hdr[43] = 16
	hdr[44] = p[0]
	hdr[48] = 16
	if mode == 422 {
		hdr[55] = 1
	}
	le.PutUint32(hdr[69:], uint32(len(stream)))
	if c.ast == nil {
		c.ast = ast.New()
	}
	cf, err := c.ast.Decode(append(hdr, stream...))
	if err != nil {
		return protocol.Frame{}, err
	}
	for i := 3; i < len(cf.Pix); i += 4 {
		cf.Pix[i] = 255
	}
	return protocol.Frame{Width: cf.W, Height: cf.H, Pixels: cf.Pix}, nil
}

func (c *Client) tokenAuth() []byte {
	auth := make([]byte, 48)
	if c.launch.HTML5 {
		copy(auth, c.launch.Token) // token, then 24 zero bytes
		if c.launch.ClientType {
			auth = append(auth, 0) // client type 0, as the X12/X13 noVNC client sends
		}
		return auth
	}
	copy(auth[:24], c.launch.Token)
	if c.launch.PasswordToken != "" {
		copy(auth[24:], c.launch.PasswordToken) // older JNLPs carry distinct user/password fields
	} else {
		copy(auth[24:], c.launch.Token)
	}
	return auth
}

func (c *Client) htmlRequest(incremental bool) error {
	w, h := c.reqW, c.reqH
	p := make([]byte, 10)
	p[0] = 3
	if incremental {
		p[1] = 1
	}
	binary.BigEndian.PutUint16(p[6:], uint16(w))
	binary.BigEndian.PutUint16(p[8:], uint16(h))
	return c.wire.WritePacket(p)
}
