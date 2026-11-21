package vmedia

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// patReader returns deterministic bytes (byte i == i%251) so reads are verifiable
// without a real image.
type patReader struct{ size int64 }

func (p patReader) Size() int64 { return p.size }
func (p patReader) ReadAt(b []byte, off int64) (int, error) {
	for i := range b {
		b[i] = byte((off + int64(i)) % 251)
	}
	return len(b), nil
}

// TestRead10Golden replays the exact READ(10) request captured from the live BMC
// and asserts the response the host accepted: the command envelope echoed, the
// SCSI data appended, and BOTH length fields (envOffDataLen and the decisive
// envOffRespLen) set to the byte count. Guards the wire format against drift —
// this is precisely the contract that, when broken, gave the host DID_ERROR.
func TestRead10Golden(t *testing.T) {
	// Real capture: READ(10), LBA 14, 2 blocks (offset 28672, 4096 bytes).
	req := &Packet{Payload: []byte{
		0x00, 0x10, 0x00, 0x00, 0x61, 0x00, 0x00, 0x00,
		0x01, 0x28, 0x00, 0x00, 0x00, 0x00, 0x0e, 0x00,
		0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00,
	}}
	if req.Opcode() != scsiRead10 {
		t.Fatalf("opcode = 0x%02X, want READ(10) 0x28", req.Opcode())
	}

	cd := NewCDROM(patReader{size: 1 << 20})
	resp, err := cd.Handle(req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	const wantData = 2 * cdBlockSize // 4096
	if len(resp) != len(req.Payload)+wantData {
		t.Fatalf("resp len = %d, want %d", len(resp), len(req.Payload)+wantData)
	}
	// Envelope echoed verbatim except the length fields we stamp.
	if binary.LittleEndian.Uint32(resp[envOffDataLen:envOffDataLen+4]) != wantData {
		t.Errorf("envOffDataLen = %d, want %d", binary.LittleEndian.Uint32(resp[0:4]), wantData)
	}
	if got := binary.LittleEndian.Uint32(resp[envOffRespLen : envOffRespLen+4]); got != wantData {
		t.Errorf("envOffRespLen = %d, want %d (the field the BMC actually forwards)", got, wantData)
	}
	// Appended data must be the image bytes at LBA 14 (offset 28672).
	data := resp[len(req.Payload):]
	for i := 0; i < 8; i++ {
		want := byte((28672 + i) % 251)
		if data[i] != want {
			t.Fatalf("data[%d] = %d, want %d (wrong read offset)", i, data[i], want)
		}
	}
}

// TestTestUnitReadyEcho verifies the media-present poll: a status-only reply that
// echoes the request envelope unchanged (no data, zero length).
func TestTestUnitReadyEcho(t *testing.T) {
	req := &Packet{Payload: []byte{
		0x00, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00,
	}}
	cd := NewCDROM(patReader{size: 1 << 20})
	resp, err := cd.Handle(req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(resp) != len(req.Payload) {
		t.Fatalf("TUR resp len = %d, want %d (echo, no data)", len(resp), len(req.Payload))
	}
}

// memRW is an in-memory ReadWriter backing for the write test.
type memRW struct{ b []byte }

func (m *memRW) Size() int64                              { return int64(len(m.b)) }
func (m *memRW) ReadAt(p []byte, off int64) (int, error)  { return copy(p, m.b[off:]), nil }
func (m *memRW) WriteAt(p []byte, off int64) (int, error) { return copy(m.b[off:], p), nil }

// TestWrite10 verifies the WRITE(10) path: the host's data rides at the tail of
// the request payload (after the command envelope) and is written at lba*512.
func TestWrite10(t *testing.T) {
	m := &memRW{b: make([]byte, 64*diskBlockSize)}
	d := NewDiskRW(m)

	// A writable device must NOT report write-protect, or the host mounts RO.
	if d.modeSense6()[2]&0x80 != 0 {
		t.Error("writable device reports write-protect")
	}

	// WRITE(10): LBA 5, 1 block. CDB at payload[9]: opcode 0x2A, LBA (BE) at
	// [11:15] = 00 00 00 05, block count (BE) at [16:18] = 00 01.
	env := make([]byte, 29)
	env[opcodeOffset] = scsiWrite10
	env[14] = 0x05 // LBA low byte
	env[17] = 0x01 // 1 block
	data := make([]byte, diskBlockSize)
	for i := range data {
		data[i] = byte(i*7 + 3)
	}
	req := &Packet{Payload: append(append([]byte{}, env...), data...)}

	if _, err := d.Handle(req); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := m.b[5*diskBlockSize : 6*diskBlockSize]
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("written byte %d = %d, want %d (wrong offset or tail extraction)", i, got[i], data[i])
		}
	}
}

// A read-only disk must report write-protect and ignore WRITE.
func TestReadOnlyDiskWriteProtect(t *testing.T) {
	d := NewDisk(patReader{size: 64 * diskBlockSize})
	if d.modeSense6()[2]&0x80 == 0 {
		t.Error("read-only disk should report write-protect")
	}
	env := make([]byte, 29)
	env[opcodeOffset] = scsiWrite10
	env[17] = 0x01
	req := &Packet{Payload: append(make([]byte, 0, 29+diskBlockSize), append(env, make([]byte, diskBlockSize)...)...)}
	if _, err := d.Handle(req); err != nil { // must not panic / error (w is nil → ignored)
		t.Fatalf("Handle on read-only device: %v", err)
	}
}

// TestDriveAnswers pins the data the CD-ROM sends for each command it knows,
// as the host gets it after the echoed envelope, for a disc of 25 blocks.
func TestDriveAnswers(t *testing.T) {
	cd := NewCDROM(patReader{size: 25 * cdBlockSize})
	noSense := make([]byte, 18)
	noSense[0], noSense[7] = 0x70, 10
	discInfo := make([]byte, 34)
	copy(discInfo, []byte{0, 32, 0x0e, 1, 1, 1, 1})
	for _, c := range []struct {
		name string
		cdb  []byte
		want []byte
	}{
		{"TEST UNIT READY", []byte{scsiTestUnitReady, 0, 0, 0, 0, 0}, nil},
		{"INQUIRY", []byte{scsiInquiry, 0, 0, 0, 36, 0},
			append([]byte{5, 0x80, 5, 2, 31, 0, 0, 0}, "RD450X  Virtual CD-ROM  1.0 "...)},
		{"REQUEST SENSE: no sense", []byte{scsiRequestSense, 0, 0, 0, 18, 0}, noSense},
		{"MODE SENSE(6): write-protected", []byte{scsiModeSense6, 0, 0x3f, 0, 4, 0}, []byte{3, 0, 0x80, 0}},
		{"MODE SENSE(10): write-protected", []byte{scsiModeSense10, 0, 0x3f, 0, 0, 0, 0, 0, 8, 0},
			[]byte{0, 6, 0, 0x80, 0, 0, 0, 0}},
		{"READ CAPACITY: last block 24", []byte{scsiReadCapacity10, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			[]byte{0, 0, 0, 24, 0, 0, 8, 0}},
		{"READ TOC: track 1, lead-out at 25", []byte{scsiReadTOC, 0, 0, 0, 0, 0, 0, 0, 20, 0},
			[]byte{0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 0, 0, 0, 0x14, 0xaa, 0, 0, 0, 0, 25}},
		{"GET CONFIGURATION: CD-ROM", []byte{scsiGetConfig, 0, 0, 0, 0, 0, 0, 0, 12, 0},
			[]byte{0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0}},
		{"GET EVENT STATUS: no change", []byte{scsiGetEventStatus, 1, 0, 0, 0x10, 0, 0, 0, 8, 0}, []byte{0, 2, 0, 0x10}},
		{"READ DISC INFORMATION: finalized", []byte{scsiReadDiscInfo, 0, 0, 0, 0, 0, 0, 0, 34, 0}, discInfo},
		{"WRITE(10): ignored", []byte{scsiWrite10, 0, 0, 0, 0, 5, 0, 0, 1, 0}, nil},
		{"AMI control 0xF3: acknowledged", []byte{0xf3}, nil},
		{"unknown command: nothing", []byte{0x35, 0, 0, 0, 0, 0, 0, 0, 0, 0}, nil},
	} {
		p := make([]byte, 29)
		copy(p[opcodeOffset:], c.cdb)
		resp, err := cd.Handle(&Packet{Payload: p})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := resp[len(p):]; !bytes.Equal(got, c.want) {
			t.Errorf("%s: % x, want % x", c.name, got, c.want)
		}
	}
}

// NewDisk (floppy/HD/USB) must use 512-byte Direct-Access blocks: READ CAPACITY
// reports 512 and a READ(10) addresses 512-byte LBAs (so LBA 3 == byte 1536).
func TestDiskProfile512(t *testing.T) {
	d := NewDisk(patReader{size: 2880 * diskBlockSize}) // 1.44 MB floppy
	cap := d.readCapacity()
	if got := binary.BigEndian.Uint32(cap[4:8]); got != diskBlockSize {
		t.Errorf("disk block size = %d, want %d", got, diskBlockSize)
	}
	if got := binary.BigEndian.Uint32(cap[0:4]); got != 2879 {
		t.Errorf("last LBA = %d, want 2879", got)
	}

	// READ(10): LBA 3, 1 block → offset 1536, 512 bytes. CDB at payload[9]:
	// opcode 0x28, LBA (BE) at [11:15] = 00 00 00 03, block count (BE) at [16:18] = 00 01.
	req := &Packet{Payload: []byte{
		0x00, 0x02, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x28, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00,
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00,
	}}
	resp, err := d.Handle(req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	data := resp[len(req.Payload):]
	if len(data) != diskBlockSize {
		t.Fatalf("read len = %d, want %d", len(data), diskBlockSize)
	}
	if want := byte((3*diskBlockSize + 0) % 251); data[0] != want {
		t.Errorf("data[0] = %d, want %d (wrong 512-byte LBA scaling)", data[0], want)
	}
}

// Requests a BMC cannot mean are errors, which end the media session instead
// of the core (A2-01).
func TestMalformedRequestsAreErrors(t *testing.T) {
	cd := NewCDROM(patReader{size: 1 << 20})
	for name, cdb := range map[string][]byte{
		"READ(10) cut short":  {scsiRead10, 0, 0, 0, 0, 14},
		"READ(12) cut short":  {scsiRead12, 0},
		"WRITE(12) cut short": {scsiWrite12, 0, 0},
		"READ(12) of 8 TiB":   {scsiRead12, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0, 0},
		"READ(10) of 128 MiB": {scsiRead10, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0},
	} {
		p := append(make([]byte, opcodeOffset), cdb...)
		if out, err := cd.Handle(&Packet{Payload: p}); err == nil {
			t.Errorf("%s: answered with %d bytes", name, len(out))
		}
	}
	// The largest READ there is: 512 blocks of 2048 bytes.
	p := append(make([]byte, opcodeOffset), scsiRead10, 0, 0, 0, 0, 0, 0, 0x02, 0x00, 0)
	if out, err := cd.Handle(&Packet{Payload: p}); err != nil || len(out) != len(p)+maxRead {
		t.Errorf("READ(10) of 1 MiB: %d bytes, %v", len(out), err)
	}
}
