package vmedia

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"

	"oldipmi/internal/scsi"
)

// amiControlOpcodeMin is the low end of AMI's redirection-control opcode range
// (0xF0–0xFF). These ride in the SCSI opcode slot (payload[9]) but are NOT SCSI
// commands — e.g. 0xF1 ack, 0xF2 auth, 0xF3 (a periodic poll the BMC emits),
// 0xF4 SET_HARDDISK_TYPE, 0xF6 kill. JViewer routes every opcode through its native
// SCSI handler, which simply acks these; we match that with an empty echo-envelope.
const amiControlOpcodeMin = 0xF0

// drive is what the CD-ROM tells the host in its INQUIRY data (SPC-3, response
// data format 2); the BMC firmware mostly answers INQUIRY itself.
var drive = scsi.Identity{Version: 0x05, Format: 0x02, Vendor: "RD450X  ", Product: "Virtual CD-ROM  ", Revision: "1.0 "}

// Device emulates a read-only USB CD-ROM backed by a Reader, answering the BMC's
// IUSB-wrapped SCSI commands: 2048-byte blocks and the MMC probes (READ TOC /
// GET CONFIGURATION / READ DISC INFORMATION).
//
// The BMC firmware answers INQUIRY itself and forwards TEST UNIT READY (a
// media-present poll) plus the data commands; the SCSI emulation here is standard
// and spec-correct, while the IUSB response framing is in buildResponse.
type Device struct {
	r       Reader
	lastLBA uint32 // highest addressable block
}

// NewCDROM builds a read-only CD-ROM emulator over r.
func NewCDROM(r Reader) *Device {
	last := uint32(0)
	if n := r.Size() / scsi.BlockSize; n > 0 {
		last = uint32(n - 1)
	}
	return &Device{r: r, lastLBA: last}
}

// cdbLen is the length of the commands whose fields Handle reads at fixed
// offsets; a shorter one from the BMC ends the session.
var cdbLen = map[byte]int{scsi.Read10: 10, scsi.Write10: 10, scsi.Read12: 12, scsi.Write12: 12}

// Handle dispatches one IUSB SCSI request, returning the response payload. An
// error means the BMC sent a request that cannot be answered.
func (c *Device) Handle(req *Packet) ([]byte, error) {
	cdb := req.cdb()
	if len(cdb) == 0 {
		return nil, nil
	}
	if n := cdbLen[cdb[0]]; len(cdb) < n {
		return nil, fmt.Errorf("SCSI command 0x%02X is %d bytes long, not %d", cdb[0], len(cdb), n)
	}
	switch cdb[0] {
	case scsi.TestUnitReady, scsi.PreventAllow, scsi.StartStopUnit, scsi.ModeSelect6:
		// Status-only commands: acknowledge with no data. An eject (START STOP
		// UNIT with LoEj, Packet.IsEject) too; Serve then ends the redirection.
		return c.buildResponse(req, nil), nil

	case scsi.Inquiry:
		return c.buildResponse(req, clip(drive.Inquiry(), allocLen(cdb, 3, 4))), nil

	case scsi.RequestSense:
		return c.buildResponse(req, scsi.Sense(0, 0, 0)), nil // no sense

	case scsi.ReadCapacity10:
		return c.buildResponse(req, scsi.Capacity(c.lastLBA)), nil

	case scsi.Read10, scsi.Read12:
		data, err := c.read(scsi.Transfer(cdb))
		if err != nil {
			return nil, err
		}
		return c.buildResponse(req, data), nil

	case scsi.Write10, scsi.Write12:
		// A CD-ROM is read-only: the write is ignored.
		if lba, blocks := scsi.Transfer(cdb); blocks != 0 {
			log.Printf("vmedia: WRITE lba=%d blocks=%d on a read-only device — ignored", lba, blocks)
		}
		return c.buildResponse(req, nil), nil

	case scsi.ModeSense6:
		return c.buildResponse(req, scsi.ModeHeader6()), nil

	case scsi.ModeSense10:
		return c.buildResponse(req, scsi.ModeHeader10()), nil

	// The MMC (CD-ROM) probes.
	case scsi.ReadTOC:
		return c.buildResponse(req, scsi.TOC(c.lastLBA+1, false)), nil
	case scsi.GetConfiguration:
		return c.buildResponse(req, scsi.Configuration()), nil
	case scsi.GetEventStatus:
		return c.buildResponse(req, scsi.NoEvent(cdb)), nil
	case scsi.ReadDiscInfo:
		return c.buildResponse(req, scsi.DiscInfo()), nil

	default:
		// AMI redirection-control opcodes (0xF0–0xFF) are not SCSI; they only need an
		// ack (empty echo-envelope), exactly as JViewer's native handler gives them —
		// no warning. (0xF1 ack/0xF2 auth are consumed in the handshake; 0xF6 kill is
		// caught by the session loop before Handle.) Below 0xF0 is a genuinely
		// unimplemented SCSI command, which is worth logging.
		if cdb[0] >= amiControlOpcodeMin {
			return c.buildResponse(req, nil), nil
		}
		log.Printf("vmedia: unhandled SCSI opcode 0x%02X (replying empty)", cdb[0])
		return c.buildResponse(req, nil), nil
	}
}

// buildResponse wraps SCSI response data in the IUSB response payload.
//
// Confirmed against the live BMC + host (vmedia is plaintext, so verifiable):
// the response payload echoes the request's command envelope, then appends the
// SCSI data-in bytes. The length the BMC forwards to the host is taken from the
// envelope field at envOffRespLen — setting only envOffDataLen yields a 0-length
// read (DID_ERROR on the host). The BMC firmware answers enumeration (INQUIRY)
// itself and forwards TEST UNIT READY (media-present poll) plus the data commands
// (READ(10), …). A bare reply leaves TUR pending and /dev/sr0 never appears; the
// echo-envelope reply with the response length set makes the disc appear and read
// correctly end-to-end.
func (c *Device) buildResponse(req *Packet, data []byte) []byte {
	env := req.Payload
	out := make([]byte, len(env)+len(data))
	copy(out, env)
	copy(out[len(env):], data)
	n := uint32(len(data))
	if len(out) >= envOffDataLen+4 {
		binary.LittleEndian.PutUint32(out[envOffDataLen:envOffDataLen+4], n)
	}
	if len(out) >= envOffRespLen+4 {
		binary.LittleEndian.PutUint32(out[envOffRespLen:envOffRespLen+4], n)
	}
	return out
}

// read returns blocks*scsi.BlockSize bytes starting at lba, zero-filling any
// short read past end-of-medium. More than scsi.MaxTransfer is an error.
func (c *Device) read(lba, blocks uint32) ([]byte, error) {
	n := int64(blocks) * scsi.BlockSize
	if n > scsi.MaxTransfer {
		return nil, fmt.Errorf("READ of %d blocks exceeds %d KiB", blocks, scsi.MaxTransfer>>10)
	}
	buf := make([]byte, n)
	off := int64(lba) * scsi.BlockSize
	if _, err := c.r.ReadAt(buf, off); err != nil && err != io.EOF {
		log.Printf("vmedia: read lba=%d blocks=%d off=%d: %v", lba, blocks, off, err)
	}
	return buf, nil
}

// --- small SCSI helpers ------------------------------------------------------

// cdb returns the SCSI CDB embedded in the IUSB payload (starting at opcodeOffset).
func (p *Packet) cdb() []byte {
	if len(p.Payload) <= opcodeOffset {
		return nil
	}
	return p.Payload[opcodeOffset:]
}

// allocLen reads a big-endian allocation length of 1 or 2 bytes from the CDB.
func allocLen(cdb []byte, off, size int) int {
	if size == 2 {
		if len(cdb) >= off+2 {
			return int(binary.BigEndian.Uint16(cdb[off : off+2]))
		}
		return 0
	}
	if len(cdb) > off {
		return int(cdb[off])
	}
	return 0
}

// clip trims b to at most n bytes (n<=0 means no allocation limit was given).
func clip(b []byte, n int) []byte {
	if n > 0 && n < len(b) {
		return b[:n]
	}
	return b
}
