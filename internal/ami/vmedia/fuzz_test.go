package vmedia

import (
	"testing"

	"oldipmi/internal/scsi"
)

// FuzzDeviceHandle feeds IUSB request payloads, as a BMC sends them, to the
// CD-ROM emulation: no input may crash it, and no reply may carry more than
// scsi.MaxTransfer of data. testdata/fuzz holds the inputs that crashed it (A2-01): a
// READ(12) cut short and one for 2^32-1 blocks, 8 TiB.
func FuzzDeviceHandle(f *testing.F) {
	envelope := func(cdb ...byte) []byte {
		p := make([]byte, 29)
		copy(p[opcodeOffset:], cdb)
		return p
	}
	f.Add(envelope(scsi.Read10, 0, 0, 0, 0, 14, 0, 0, 2, 0))       // READ(10), LBA 14, 2 blocks
	f.Add(envelope(scsi.Read12, 0, 0, 0, 0, 14, 0, 0, 0, 2, 0, 0)) // READ(12)
	f.Add(envelope(scsi.TestUnitReady))
	f.Add(envelope(scsi.Inquiry, 0, 0, 0, 36, 0))
	f.Add(envelope(scsi.ReadTOC, 0, 0, 0, 0, 0, 0, 0, 12, 0))
	f.Fuzz(func(t *testing.T, payload []byte) {
		out, err := NewCDROM(patReader{size: 1 << 20}).Handle(&Packet{Payload: payload})
		if err == nil && len(out) > len(payload)+scsi.MaxTransfer {
			t.Fatalf("a %d-byte reply to a %d-byte request", len(out), len(payload))
		}
	})
}
