package iusb

import (
	"encoding/binary"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	in := Header{
		Major:          Major,
		Minor:          Minor,
		DataPacketLen:  0x12345678,
		DeviceType:     5, // a CD-ROM
		Protocol:       1,
		Direction:      128,
		Instance:       3,
		SequenceNumber: 0xCAFEBABE,
	}
	buf := make([]byte, HeaderLen)
	in.Marshal(buf)

	got, err := ParseHeader(buf)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if got != in {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

// The checksum byte must make the unsigned sum of all 32 header bytes ≡ 0 (mod
// 256), which is exactly what the BMC validates.
func TestHeaderChecksumSumsToZero(t *testing.T) {
	h := Header{Major: 1, DataPacketLen: 128, DeviceType: 5, Protocol: 1, Direction: 128, Instance: 7}
	buf := make([]byte, HeaderLen)
	h.Marshal(buf)

	var sum byte
	for _, b := range buf {
		sum += b
	}
	if sum != 0 {
		t.Errorf("header byte sum = %d, want 0", sum)
	}
	if buf[10] != HeaderLen {
		t.Errorf("packetHeaderLen = %d, want %d", buf[10], HeaderLen)
	}
	if string(buf[0:8]) != signature {
		t.Errorf("signature = %q, want %q", buf[0:8], signature)
	}
}

func TestParseHeaderRejectsBadSignature(t *testing.T) {
	buf := make([]byte, HeaderLen)
	copy(buf, "NOTIUSB!")
	if _, err := ParseHeader(buf); err == nil {
		t.Fatal("expected error for bad signature, got nil")
	}
	if _, err := ParseHeader(buf[:10]); err == nil {
		t.Fatal("expected error for short header, got nil")
	}
}

// Guard against accidental endianness drift in the framing-length field, which
// the receive loop relies on to know how many payload bytes follow the header.
func TestDataPacketLenIsLittleEndian(t *testing.T) {
	h := Header{Major: 1, DataPacketLen: 0x00020000} // 128 KiB max transfer
	buf := make([]byte, HeaderLen)
	h.Marshal(buf)
	if got := binary.LittleEndian.Uint32(buf[12:16]); got != 0x00020000 {
		t.Errorf("dataPacketLen LE = 0x%08X, want 0x00020000", got)
	}
}
