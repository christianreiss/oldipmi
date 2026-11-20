// Package vmedia implements the AMI MegaRAC virtual-media (IUSB) data plane:
// it presents a local disk image to the host as a USB CD/DVD (or floppy/HD) by
// answering the BMC's SCSI sector reads over the proprietary IUSB protocol.
//
// The BMC uses a client-streams-sectors model: it exposes a USB mass-storage
// device to the host and, on demand, pulls the sectors the host reads from us
// over a dedicated TLS socket (CD 5120 / FD 5122 / HD 5123). This is the opposite
// of the device-local-image model every open IP-KVM stack uses, which is why the
// data plane is necessarily ours.
//
// The wire protocol was reverse-engineered from the decompiled JViewer
// `com.ami.iusb.*` sources (the SCSI *emulation* there is native, but the
// transport/handshake/framing are Java and reproduced here).
package vmedia

import "oldipmi/internal/ami/iusb"

// Device types (IUSBHeader.deviceType). Only CD-ROM is confirmed from the RE;
// floppy/HD codes still need verification against a capture.
const (
	DeviceCDROM = 5
)

// Default per-device TLS ports on the BMC.
const (
	PortCD = 5120
	PortFD = 5122
	PortHD = 5123
)

// Opcodes carried in the SCSI payload at offset opcodeOffset. The 0xF* values
// are AMI's redirection-control opcodes; 0x1B is the standard SCSI START STOP
// UNIT (used by the host to eject).
const (
	OpAuth          = 0xF2 // client→server: session-token authentication
	OpRedirectAck   = 0xF1 // server→client: redirection acknowledgement
	OpKillRedir     = 0xF6 // server→client: terminate this redirection
	OpStartStopUnit = 0x1B // SCSI START STOP UNIT (eject when CDB[4] low nibble == 2)
)

// Offsets inside the IUSB payload (after the 32-byte header). The SCSI CDB
// begins at opcodeOffset, so opcode == CDB[0] and ejectByteOffset == CDB[4].
const (
	envOffDataLen     = 0  // payload[0:4]  = transfer/data length, u32 LE (request: bytes wanted)
	envOffRespLen     = 25 // payload[25:] = response data length the BMC forwards to the host (the field that actually counts)
	opcodeOffset      = 9  // payload[9]  = SCSI CDB byte 0 (opcode)
	ejectByteOffset   = 13 // payload[13] = SCSI CDB byte 4 (START STOP UNIT loej bits)
	connStatusOffset  = 30 // payload[30] = connectionStatus in an ACK
	authTokenOffset   = 31 // payload[31..] = web session token in an auth packet
	authPayloadLen    = 128
	ssiAuthPayloadLen = 240
)

// Connection-status values reported in an ACK's connStatusOffset byte.
const (
	connOK        = 1 // redirection accepted
	connErrInUse5 = 5
	connErrInUse8 = 8
)

// Packet is a decoded IUSB packet: a header plus its payload (the SCSI block).
type Packet struct {
	Header  iusb.Header
	Payload []byte
}

// Opcode returns the SCSI/redirection opcode (payload[opcodeOffset]), or 0 if the
// payload is too short to carry one.
func (p *Packet) Opcode() int {
	if len(p.Payload) <= opcodeOffset {
		return 0
	}
	return int(p.Payload[opcodeOffset])
}

// IsEject reports whether this is a START STOP UNIT eject request (opcode 0x1B,
// CDB[4] low bits == 2), matching JViewer's detection.
func (p *Packet) IsEject() bool {
	return p.Opcode() == OpStartStopUnit &&
		len(p.Payload) > ejectByteOffset && p.Payload[ejectByteOffset]&0x03 == 2
}

// IsKill reports whether the BMC asked to terminate the redirection.
func (p *Packet) IsKill() bool { return p.Opcode() == OpKillRedir }
