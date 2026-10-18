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
