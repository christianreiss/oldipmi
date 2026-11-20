package vmedia

import "testing"

// FuzzDeviceHandle feeds IUSB request payloads, as a BMC sends them, to the
// CD-ROM and disk emulation: no input may crash them, and no reply may carry
// more than maxRead of data. testdata/fuzz holds the inputs that crashed them
// (A2-01): a READ(12) cut short and one for 2^32-1 blocks, 8 TiB.
func FuzzDeviceHandle(f *testing.F) {
	envelope := func(cdb ...byte) []byte {
		p := make([]byte, 29)
		copy(p[opcodeOffset:], cdb)
		return p
	}
	f.Add(envelope(scsiRead10, 0, 0, 0, 0, 14, 0, 0, 2, 0))       // READ(10), LBA 14, 2 blocks
	f.Add(envelope(scsiRead12, 0, 0, 0, 0, 14, 0, 0, 0, 2, 0, 0)) // READ(12)
	f.Add(envelope(scsiTestUnitReady))
	f.Add(envelope(scsiInquiry, 0, 0, 0, 36, 0))
	f.Add(envelope(scsiReadTOC, 0, 0, 0, 0, 0, 0, 0, 12, 0))
	f.Fuzz(func(t *testing.T, payload []byte) {
		for _, d := range []*Device{NewCDROM(patReader{size: 1 << 20}), NewDisk(patReader{size: 1 << 20})} {
			out, err := d.Handle(&Packet{Payload: payload})
			if err == nil && len(out) > len(payload)+maxRead {
				t.Fatalf("a %d-byte reply to a %d-byte request", len(out), len(payload))
			}
		}
	})
}
