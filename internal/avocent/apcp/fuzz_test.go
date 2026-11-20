package apcp

import (
	"bytes"
	"testing"
)

// Everything apcp reads from a BMC, fed arbitrary bytes: errors, never panics,
// and an AVPT frame that ReadAVPTFrame accepts survives Encode unchanged.
func FuzzBMCInput(f *testing.F) {
	f.Add(AVPTFrame{ID: 33541, Payload: []byte{0, 0, 0, 0, 0, 0, 0, 7, 0, 1, 4, 'r', 'o', 'o', 't'}}.Encode())
	f.Add(append(AVPTFrame{ID: 33541, Payload: []byte{2}}.Encode(), AVPTFrame{ID: 129, Payload: make([]byte, 12)}.Encode()...))
	f.Add(append([]byte("APCP\x00\x00\x00\x35\x81\x00\x00\x00\x01\x03\x00\x00\x00\x04\x17\x0c\x20"), make([]byte, 32)...))
	f.Fuzz(func(t *testing.T, b []byte) {
		DecodeSessionSetup(b)
		for r := bytes.NewReader(b); ; {
			fr, err := ReadAVPTFrame(r)
			if err != nil {
				break
			}
			DecodeKVMLoginResponse(fr)
			again, err := ReadAVPTFrame(bytes.NewReader(fr.Encode()))
			if err != nil || again.ID != fr.ID || !bytes.Equal(again.Payload, fr.Payload) {
				t.Fatalf("AVPT frame %d with %d bytes does not survive Encode: %v", fr.ID, len(fr.Payload), err)
			}
		}
	})
}
