// Package scsi holds what the virtual CD-ROM drives of the console families
// share of the SCSI and MMC command sets: the command and sense codes, the
// fields of the transfer commands and the layout of each answer. How a drive
// answers stays with its family, because the transports differ: whether it
// reports a status and keeps sense data, checks a read against the end of the
// disc, or cuts an answer to the host's allocation length.
package scsi

import "encoding/binary"

// BlockSize is a CD-ROM's logical block: the host addresses the disc in blocks
// of this many bytes.
const BlockSize = 2048

// MaxTransfer is the most data one command may move; the hosts ask for far
// less.
const MaxTransfer = 1 << 20

// Command codes, the first byte of a command descriptor block (CDB).
const (
	TestUnitReady    = 0x00
	RequestSense     = 0x03
	Inquiry          = 0x12
	ModeSelect6      = 0x15
	ModeSense6       = 0x1A
	StartStopUnit    = 0x1B
	PreventAllow     = 0x1E
	ReadCapacity10   = 0x25
	Read10           = 0x28
	Write10          = 0x2A
	ReadTOC          = 0x43
	GetConfiguration = 0x46
	GetEventStatus   = 0x4A
	ReadDiscInfo     = 0x51
	ModeSense10      = 0x5A
	Read12           = 0xA8
	Write12          = 0xAA
)

// Sense keys and additional sense codes of the errors the drives report.
const (
	KeyMediumError    = 0x03
	KeyIllegalRequest = 0x05

	ASCUnrecoveredRead = 0x11 // unrecovered read error
	ASCInvalidOpcode   = 0x20 // invalid command operation code
	ASCLBAOutOfRange   = 0x21 // logical block address out of range
	ASCInvalidField    = 0x24 // invalid field in CDB
)

// Transfer returns the first block and the number of blocks of a READ or a
// WRITE, (10) or (12). The caller checks that cdb is as long as its command.
func Transfer(cdb []byte) (lba, blocks uint32) {
	lba = binary.BigEndian.Uint32(cdb[2:6])
	if cdb[0] == Read12 || cdb[0] == Write12 {
		return lba, binary.BigEndian.Uint32(cdb[6:10])
	}
	return lba, uint32(binary.BigEndian.Uint16(cdb[7:9]))
}

// Sense is fixed-format sense data (18 bytes) of a current error.
func Sense(key, asc, ascq byte) []byte {
	b := make([]byte, 18)
	b[0] = 0x70 // current error, fixed format
	b[2] = key & 0x0F
	b[7] = 10 // additional sense length
	b[12] = asc
	b[13] = ascq
	return b
}

// Identity is what a drive tells the host about itself in its INQUIRY data.
type Identity struct {
	Version  byte   // the SPC version it claims, 0 for none
	Format   byte   // response data format, with its flags
	Vendor   string // 8 characters
	Product  string // 16 characters
	Revision string // 4 characters
}

// Inquiry is the standard INQUIRY data (36 bytes) of a removable CD-ROM drive.
func (id Identity) Inquiry() []byte {
	b := make([]byte, 36)
	b[0] = 0x05 // peripheral device type: CD/DVD
	b[1] = 0x80 // removable
	b[2] = id.Version
	b[3] = id.Format
	b[4] = 31 // additional length (36-5)
	copy(b[8:16], id.Vendor)
	copy(b[16:32], id.Product)
	copy(b[32:36], id.Revision)
	return b
}

// Capacity is the READ CAPACITY (10) data: the address of the last block and
// the block size.
func Capacity(lastLBA uint32) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], lastLBA)
	binary.BigEndian.PutUint32(b[4:8], BlockSize)
	return b
}

// ModeHeader6 is the MODE SENSE (6) answer of a write-protected disc: the mode
// parameter header alone (mode data length, medium type, device-specific
// parameters with the write-protect bit, block descriptor length).
func ModeHeader6() []byte { return []byte{3, 0, 0x80, 0} }

// ModeHeader10 is the same answer to MODE SENSE (10), in its 8-byte header.
func ModeHeader10() []byte { return []byte{0, 6, 0, 0x80, 0, 0, 0, 0} }

// TOC is the READ TOC answer (format 0) for a disc with one data track of
// blocks blocks: the header, track 1 at block 0 and the lead-out after the
// last block. With msf the addresses are minute, second and frame instead of
// block numbers.
func TOC(blocks uint32, msf bool) []byte {
	b := make([]byte, 20)
	b[1] = 18         // data length after this field
	b[2], b[3] = 1, 1 // first and last track
	for i, t := range [2]struct {
		track byte
		lba   uint32
	}{{1, 0}, {0xAA, blocks}} {
		d := b[4+8*i : 12+8*i]
		d[1] = 0x14 // ADR 1, data track
		d[2] = t.track
		if msf {
			f := t.lba + 150 // block 0 is at 00:02:00
			d[5], d[6], d[7] = byte(f/4500), byte(f/75%60), byte(f%75)
		} else {
			binary.BigEndian.PutUint32(d[4:8], t.lba)
		}
	}
	return b
}

// Configuration is a GET CONFIGURATION answer: a feature header that names
// CD-ROM as the current profile, followed by four zero bytes.
func Configuration() []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)-4)) // data length
	binary.BigEndian.PutUint16(b[6:8], 0x0008)           // current profile: CD-ROM
	return b
}

// NoEvent is a GET EVENT STATUS NOTIFICATION answer that reports no event; it
// names the event classes the host asked for (cdb[4]) as supported.
func NoEvent(cdb []byte) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:2], 2)
	if len(cdb) > 4 {
		b[3] = cdb[4]
	}
	return b
}

// DiscInfo is a READ DISC INFORMATION answer for a finalized disc of one
// session with one track.
func DiscInfo() []byte {
	b := make([]byte, 34)
	binary.BigEndian.PutUint16(b[0:2], uint16(len(b)-2))
	b[2] = 0x0E // disc status: finalized, last session complete
	b[3] = 1    // first track
	b[4] = 1    // sessions (LSB)
	b[5] = 1    // first track in last session
	b[6] = 1    // last track in last session
	return b
}
