package ami

import (
	"context"
	"fmt"
	"oldipmi/internal/transport"
	"time"
)

// Legacy MegaRAC VSC packet types this client sends or expects.
const (
	vscVideo    = 3  // a fragment of a video frame
	vscHID      = 4  // a HID report, on the input connection
	vscStart    = 7  // the last packet of the handshake, empty
	vscEnd      = 9  // the console ends the session
	vscOptions  = 11 // sent with the byte 8 before vscStart; what it sets is not known
	vscToken    = 12 // the session token from the launch file
	vscPrelogin = 14 // the console's first packet on each connection; status 0 lets the client on
	vscCookie   = 32 // the web session's cookie
	vscPing     = 33 // the console expects an empty vscPong in reply
	vscPong     = 34
)

// attachVSC opens the legacy MegaRAC VSC console on c.wire. Video and input
// (c.hid, always TLS) each start with a prelogin answer; then the client
// sends its token, the web session's cookie, vscOptions and vscStart on the
// video connection.
func (c *Client) attachVSC(ctx context.Context) (err error) {
	t, st, _, e := c.read(c.wire)
	if e != nil {
		return e
	}
	if t != vscPrelogin || st != 0 {
		return fmt.Errorf("AMI video prelogin refused (%d/%d)", t, st)
	}
	c.hid, err = transport.Dial(ctx, c.p.Policy(), c.launch.HIDPort, true)
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
	if t != vscPrelogin || st != 0 {
		return fmt.Errorf("AMI HID prelogin refused (%d/%d)", t, st)
	}
	c.hid.SetDeadline(time.Time{})
	auth := make([]byte, 99)
	copy(auth[1:], c.launch.Token)
	if err = c.send(c.wire, vscToken, auth); err != nil {
		return err
	}
	cookie := c.launch.Cookie
	if len(c.launch.Pos) > 10 {
		cookie = c.launch.Pos[10]
	}
	if cookie != "" {
		if err = c.send(c.wire, vscCookie, []byte(cookie)); err != nil {
			return err
		}
	}
	if err = c.send(c.wire, vscOptions, []byte{8}); err != nil {
		return err
	}
	return c.send(c.wire, vscStart, nil)
}
