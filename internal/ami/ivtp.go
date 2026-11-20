package ami

// IVTP is the native console protocol. Each packet starts with an 8-byte
// little-endian header:
//
//	type   uint16   (offset 0)
//	size   uint32   (offset 2) — payload bytes following the header
//	status uint16   (offset 6)
//
// The legacy MegaRAC VSC frames the same fields in 7 bytes, with a one-byte
// type, and numbers its packets differently.
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
