package aten

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestISOReadsAndRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.iso")
	original := bytes.Repeat([]byte{0x5a}, 4096)
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	file, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	s := mediaSession{file: file, blocks: 2}
	c := make([]byte, 16)
	c[0] = 0x28
	binary.BigEndian.PutUint32(c[2:], 1)
	binary.BigEndian.PutUint16(c[7:], 1)
	b, status := s.scsi(c, 2048)
	if status != 0 || !bytes.Equal(b, original[2048:]) {
		t.Fatal("ISO read did not match backing file")
	}
	binary.BigEndian.PutUint32(c[2:], 2)
	if _, status = s.scsi(c, 2048); status == 0 {
		t.Fatal("accepted read beyond medium")
	}
	c[0] = 0x2a
	if _, status = s.scsi(c, 2048); status == 0 {
		t.Fatal("accepted ISO write")
	}
	after, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(after, original) {
		t.Fatal("ISO backing changed")
	}
}

// TestDriveAnswers pins what the virtual CD-ROM answers besides image data, in
// order: the sense of a failed command waits for REQUEST SENSE, which clears it.
func TestDriveAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.iso")
	if e := os.WriteFile(path, make([]byte, 25*2048), 0600); e != nil {
		t.Fatal(e)
	}
	file, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	s := mediaSession{file: file, blocks: 25}
	sense := func(key, asc byte) []byte {
		b := make([]byte, 18)
		b[0], b[2], b[7], b[12] = 0x70, key, 10, asc
		return b
	}
	inquiry := append([]byte{5, 0x80, 0, 0x21, 31, 0, 0, 0}, "OldIPMI Virtual CD-ROM  0001"...)
	for _, c := range []struct {
		name   string
		cdb    []byte // padded to 16 bytes
		alloc  int
		want   []byte
		status byte
	}{
		{"TEST UNIT READY", []byte{0x00}, 0, nil, 0},
		{"INQUIRY", []byte{0x12, 0, 0, 0, 36}, 36, inquiry, 0},
		{"INQUIRY cut to its allocation", []byte{0x12, 0, 0, 0, 8}, 8, inquiry[:8], 0},
		{"MODE SENSE(6): write-protected", []byte{0x1a, 0, 0x3f, 0, 4}, 4, []byte{3, 0, 0x80, 0}, 0},
		{"MODE SENSE(10): write-protected", []byte{0x5a, 0, 0x3f, 0, 0, 0, 0, 0, 8}, 8, []byte{0, 6, 0, 0x80, 0, 0, 0, 0}, 0},
		{"READ CAPACITY: last block 24", []byte{0x25}, 8, []byte{0, 0, 0, 24, 0, 0, 8, 0}, 0},
		{"READ TOC in LBA", []byte{0x43, 0, 0, 0, 0, 0, 0, 0, 20}, 20,
			[]byte{0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 0, 0, 0, 0x14, 0xaa, 0, 0, 0, 0, 25}, 0},
		{"READ TOC in MSF", []byte{0x43, 2, 0, 0, 0, 0, 0, 0, 20}, 20,
			[]byte{0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 2, 0, 0, 0x14, 0xaa, 0, 0, 0, 2, 25}, 0},
		{"READ TOC cut to its allocation", []byte{0x43, 0, 0, 0, 0, 0, 0, 0, 4}, 4, []byte{0, 18, 1, 1}, 0},
		{"REQUEST SENSE: no sense", []byte{0x03, 0, 0, 0, 18}, 18, sense(0, 0), 0},
		{"READ(10) past the end", []byte{0x28, 0, 0, 0, 0, 25, 0, 0, 1}, 2048, nil, 1},
		{"REQUEST SENSE: LBA out of range", []byte{0x03, 0, 0, 0, 18}, 18, sense(5, 0x21), 0},
		{"REQUEST SENSE: cleared", []byte{0x03, 0, 0, 0, 18}, 18, sense(0, 0), 0},
		{"GET CONFIGURATION: not offered", []byte{0x46, 0, 0, 0, 0, 0, 0, 0, 12}, 12, nil, 1},
		{"REQUEST SENSE: invalid command", []byte{0x03, 0, 0, 0, 18}, 18, sense(5, 0x20), 0},
	} {
		cdb := make([]byte, 16)
		copy(cdb, c.cdb)
		got, status := s.scsi(cdb, c.alloc)
		if status != c.status || !bytes.Equal(got, c.want) {
			t.Errorf("%s: % x status %d, want % x status %d", c.name, got, status, c.want, c.status)
		}
	}
	// A command shorter than the wrapper's 16 bytes is refused.
	if _, status := s.scsi(make([]byte, 10), 18); status != 1 || s.sense != [3]byte{5, 0x24, 0} {
		t.Errorf("short command: status %d, sense % x", status, s.sense)
	}
}
