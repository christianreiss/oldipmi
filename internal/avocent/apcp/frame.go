// Package apcp encodes and decodes the Avocent KVM protocol of BMCs with
// Avocent firmware, such as Dell iDRAC (PowerEdge R420) and Cisco IMC.
//
// Two framing layers matter:
//
//   - APCP: the session handshake before TLS (SessionRequest, SessionSetup).
//   - AVPT ("BEEF" magic): the KVM channel after TLS, login included.
//
// The virtual-media channel (AVMP) is not implemented. This package is pure
// encoding and state; network I/O lives in the caller.
package apcp

import (
	"encoding/binary"
	"fmt"
)

// MagicAPCP starts both handshake frames.
var MagicAPCP = [4]byte{'A', 'P', 'C', 'P'}

const (
	SessionRequestLen = 53

	// SessionSetupStatusOK is the fixed status field in a valid SessionSetup
	// response. Java source encodes it as -32512 via readShort; the wire form
	// is 0x8100.
	SessionSetupStatusOK uint16 = 0x8100

	// ProtoVersion is the protocol version the client sends in its
	// SessionRequest. The two bytes represent major/minor (1.0).
	ProtoVersion uint16 = 0x0100
)

// SessionRequest fields, built with EncodeSessionRequest.
type SessionRequest struct {
	SessionType byte     // Java calls it "byte a"; VM sends 2, KVM sends 2 too.
	Field2      byte     // reserved-ish flags
	Field3      byte     // reserved-ish flags
	Capability  uint32   // VM: 5. Requests the caps we can accept.
	Nonce       [32]byte // client random; up to 32 bytes are copied in.
	NonceLen    byte     // length actually populated in Nonce
}

// EncodeSessionRequest builds the 53-byte APCP SessionRequest frame.
func EncodeSessionRequest(r SessionRequest) []byte {
	buf := make([]byte, SessionRequestLen)
	copy(buf[0:4], MagicAPCP[:])
	binary.BigEndian.PutUint32(buf[4:8], SessionRequestLen)
	binary.BigEndian.PutUint16(buf[8:10], ProtoVersion)
	// bytes 10..11 zero
	buf[12] = r.SessionType
	buf[13] = r.Field2
	buf[14] = r.Field3
	// buf[15] zero
	binary.BigEndian.PutUint32(buf[16:20], r.Capability)
	buf[20] = r.NonceLen
	copy(buf[21:53], r.Nonce[:])
	return buf
}

// SessionSetup fields returned by the BMC after SessionRequest.
type SessionSetup struct {
	Status       uint16
	VersionMajor byte
	VersionMinor byte
	Capabilities uint32
	TCPPort      uint16
	NonceLen     byte
	Nonce        [32]byte
}

// SupportsSSL reports whether the SSL bit is set in the returned capabilities.
// From VirtualMedia.java switch (getConnectionCapabilities()) case 4.
func (s SessionSetup) SupportsSSL() bool { return s.Capabilities&0x04 != 0 }

// DecodeSessionSetup parses the APCP SessionSetup frame. It reads exactly
// SessionRequestLen bytes (the response is the same 53-byte layout as the
// request, just with different semantics for the trailing fields).
func DecodeSessionSetup(buf []byte) (SessionSetup, error) {
	if len(buf) < SessionRequestLen {
		return SessionSetup{}, fmt.Errorf("apcp: session setup too short: %d", len(buf))
	}
	if [4]byte{buf[0], buf[1], buf[2], buf[3]} != MagicAPCP {
		return SessionSetup{}, fmt.Errorf("apcp: bad magic %x", buf[0:4])
	}
	status := binary.BigEndian.Uint16(buf[8:10])
	if status != SessionSetupStatusOK {
		return SessionSetup{}, fmt.Errorf("apcp: session setup status 0x%04x, want 0x%04x", status, SessionSetupStatusOK)
	}
	s := SessionSetup{
		Status:       status,
		VersionMajor: buf[12],
		VersionMinor: buf[13],
		Capabilities: binary.BigEndian.Uint32(buf[14:18]),
		TCPPort:      binary.BigEndian.Uint16(buf[18:20]),
		NonceLen:     buf[20],
	}
	copy(s.Nonce[:], buf[21:53])
	return s, nil
}

// LoginStatusCode names the status byte of a KVM login response
// (KVMLoginResponse.Status). From com.avocent.vm.AVMPStatus.
var LoginStatusCode = map[byte]string{
	0:   "SUCCEEDED",
	1:   "INVALID_USER_NAME",
	2:   "INVALID_PASSWORD",
	3:   "CHANNEL_ACCESS_DENIED",
	4:   "CHANNEL_IN_USE",
	5:   "CHANNEL_NOT_FOUND",
	6:   "SERVER_NOT_AVAILABLE_CAN_PREEMPT",
	8:   "ALL_CHANNELS_IN_USE",
	11:  "CHANNEL_IN_USE_BY_LOCAL_USER_CAN_PREEMPT",
	22:  "NETWORK_AUTH_SERVER_ERROR",
	23:  "INVALID_EXPIRED_CERT_ERROR",
	52:  "PREEMPT_REJECTED",
	255: "GENERIC_FAILED", // Java uses -1
}

// LoginStatusName returns a human-readable string for the login status byte.
func LoginStatusName(b byte) string {
	if name, ok := LoginStatusCode[b]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", b)
}
