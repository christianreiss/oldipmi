package ami

import (
	"fmt"
	"net"
)

// IVTP is the native console protocol. Each packet starts with an 8-byte
// little-endian header:
//
//	type   uint16   (offset 0)
//	size   uint32   (offset 2) — payload bytes following the header
//	status uint16   (offset 6)
//
// The legacy MegaRAC VSC frames the same fields in 7 bytes, with a one-byte
// type, and numbers its packets differently (vsc.go).
const (
	ivtpHeaderSize = 8
	vscHeaderSize  = 7
)

// IVTP packet types this client sends or expects.
const (
	opHIDPkt             uint16 = 1
	opResumeRedirection  uint16 = 6
	opStopSession        uint16 = 8
	opValidateVideo      uint16 = 18
	opValidateVideoResp  uint16 = 19
	opSessionAccepted    uint16 = 23
	opVideoFragment      uint16 = 25
	opKeepAlive          uint16 = 57
	opConnectionComplete uint16 = 58
)

// validateVideoSize is the fixed body length of a VALIDATE_VIDEO_SESSION packet.
const validateVideoSize = 373

// attachIVTP opens the native IVTP console on c.wire: it answers the session
// acceptance with the token, the client's address and the user, and resumes
// the redirection once the console validates them.
func (c *Client) attachIVTP() (err error) {
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
			copy(b[195:324], c.p.Username)
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
	return err
}
