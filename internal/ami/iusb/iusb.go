// Package iusb frames the IUSB packets of AMI consoles, which carry the HID
// input of both consoles and the virtual media: a 32-byte header with a
// checksum, then the payload. The format comes from the decompiled JViewer
// com.ami.iusb.* sources (see package vmedia).
package iusb

import (
	"encoding/binary"
	"fmt"
)

// HeaderLen is the fixed size of an IUSB packet header.
const HeaderLen = 32

// signature is the 8-byte magic at the start of every IUSB header ("IUSB" + 4 spaces).
const signature = "IUSB    "

// The protocol version this client sends (Header.Major and Header.Minor).
const (
	Major = 1
	Minor = 0
)

// Header is a decoded IUSB packet header. Multi-byte fields are little-endian on
// the wire; DataPacketLen is the framing length (payload bytes following the
// header).
type Header struct {
	Major, Minor    uint8
	DataPacketLen   uint32
	ServerCaps      uint8
	DeviceType      uint8
	Protocol        uint8
	Direction       uint8
	DeviceNumber    uint8
	InterfaceNumber uint8
	ClientData      uint8
	Instance        uint8
	SequenceNumber  uint32
}

// Marshal writes the 32-byte header into dst (which must be >= HeaderLen) and
// sets the checksum byte so the receiver's sum over the 32 header bytes is zero.
// JViewer computes the checksum over the header only (the payload is appended
// after), so we do the same — the BMC never checksums the payload.
func (h *Header) Marshal(dst []byte) {
	b := dst[:HeaderLen]
	for i := range b {
		b[i] = 0
	}
	copy(b[0:8], signature)
	b[8] = h.Major
	b[9] = h.Minor
	b[10] = HeaderLen
	// b[11] checksum filled in below.
	binary.LittleEndian.PutUint32(b[12:16], h.DataPacketLen)
	b[16] = h.ServerCaps
	b[17] = h.DeviceType
	b[18] = h.Protocol
	b[19] = h.Direction
	b[20] = h.DeviceNumber
	b[21] = h.InterfaceNumber
	b[22] = h.ClientData
	b[23] = h.Instance
	binary.LittleEndian.PutUint32(b[24:28], h.SequenceNumber)
	// reserved[28:32] left zero.

	var sum byte
	for _, x := range b {
		sum += x
	}
	b[11] = -sum // (-Σ) & 0xFF; byte arithmetic wraps mod 256
}

// ParseHeader decodes a 32-byte IUSB header. It validates the signature but not
// the checksum (the BMC's own auth packet proves the checksum need not cover the
// payload, and we have no reason to reject otherwise-valid frames).
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderLen {
		return Header{}, fmt.Errorf("iusb: short header: %d bytes", len(b))
	}
	if string(b[0:8]) != signature {
		return Header{}, fmt.Errorf("iusb: bad signature %q", b[0:8])
	}
	return Header{
		Major:           b[8],
		Minor:           b[9],
		DataPacketLen:   binary.LittleEndian.Uint32(b[12:16]),
		ServerCaps:      b[16],
		DeviceType:      b[17],
		Protocol:        b[18],
		Direction:       b[19],
		DeviceNumber:    b[20],
		InterfaceNumber: b[21],
		ClientData:      b[22],
		Instance:        b[23],
		SequenceNumber:  binary.LittleEndian.Uint32(b[24:28]),
	}, nil
}
