package apcp

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// TestEncodeSessionRequest exercises the 53-byte SessionRequest against the
// exact byte layout produced by the Java client
// (com.avocent.vm.VirtualMedia.sendSessionRequest).
func TestEncodeSessionRequest(t *testing.T) {
	var nonce [32]byte
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	req := SessionRequest{
		SessionType: 2,
		Capability:  5,
		Nonce:       nonce,
		NonceLen:    32,
	}
	got := EncodeSessionRequest(req)
	if len(got) != SessionRequestLen {
		t.Fatalf("len = %d, want %d", len(got), SessionRequestLen)
	}
	if string(got[0:4]) != "APCP" {
		t.Fatalf("magic = %q", got[0:4])
	}
	if binary.BigEndian.Uint32(got[4:8]) != SessionRequestLen {
		t.Errorf("frame length field = %d", binary.BigEndian.Uint32(got[4:8]))
	}
	if binary.BigEndian.Uint16(got[8:10]) != ProtoVersion {
		t.Errorf("version = 0x%04x", binary.BigEndian.Uint16(got[8:10]))
	}
	if got[12] != 2 {
		t.Errorf("session type = %d", got[12])
	}
	if binary.BigEndian.Uint32(got[16:20]) != 5 {
		t.Errorf("capability = %d", binary.BigEndian.Uint32(got[16:20]))
	}
	if got[20] != 32 {
		t.Errorf("nonce length byte = %d", got[20])
	}
	if !bytes.Equal(got[21:53], nonce[:]) {
		t.Errorf("nonce mismatch: %s", hex.EncodeToString(got[21:53]))
	}
}

func TestDecodeSessionSetup(t *testing.T) {
	// Build a synthetic BMC response matching the Java parser
	// (VirtualMedia.receiveSessionSetup).
	buf := make([]byte, SessionRequestLen)
	copy(buf[0:4], "APCP")
	binary.BigEndian.PutUint32(buf[4:8], SessionRequestLen)
	binary.BigEndian.PutUint16(buf[8:10], SessionSetupStatusOK)
	buf[12] = 2                               // version major
	buf[13] = 3                               // version minor
	binary.BigEndian.PutUint32(buf[14:18], 4) // caps: SSL only
	binary.BigEndian.PutUint16(buf[18:20], 2068)
	buf[20] = 32
	for i := 0; i < 32; i++ {
		buf[21+i] = byte(0x80 + i)
	}
	s, err := DecodeSessionSetup(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !s.SupportsSSL() {
		t.Errorf("SupportsSSL = false, want true")
	}
	if s.VersionMajor != 2 || s.VersionMinor != 3 {
		t.Errorf("version = %d.%d", s.VersionMajor, s.VersionMinor)
	}
	if s.TCPPort != 2068 {
		t.Errorf("tcp port = %d", s.TCPPort)
	}
	if s.NonceLen != 32 {
		t.Errorf("nonce len = %d", s.NonceLen)
	}
	if s.Nonce[0] != 0x80 || s.Nonce[31] != 0x9f {
		t.Errorf("nonce bounds: first=%x last=%x", s.Nonce[0], s.Nonce[31])
	}
}
