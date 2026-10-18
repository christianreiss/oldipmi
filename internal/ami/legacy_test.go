package ami

import (
	"encoding/binary"
	"testing"
)

func legacyFrame(mode uint32, data []byte) []byte {
	b := make([]byte, 25+len(data))
	le := binary.LittleEndian
	le.PutUint32(b, uint32(16+len(data)))
	le.PutUint16(b[4:], 2)
	le.PutUint16(b[6:], 1)
	b[8] = 8
	le.PutUint16(b[13:], 2)
	le.PutUint16(b[15:], 1)
	le.PutUint32(b[17:], mode)
	le.PutUint32(b[21:], uint32(len(data)))
	copy(b[25:], data)
	return b
}
func TestLegacyPixelsAndLRLE(t *testing.T) {
	for _, tc := range []struct {
		mode uint32
		data []byte
	}{{0, []byte{0, 0x7c, 0, 0x7c}}, {2, []byte{0, 0x7c, 2, 0x80}}, {3, []byte{0, 0x7c, 0, 0x7c, 1, 0, 0, 0x80}}} {
		d := legacyDecoder{}
		f, e := d.Decode(legacyFrame(tc.mode, tc.data))
		if e != nil {
			t.Fatal(e)
		}
		if len(f.Pixels) != 8 || f.Pixels[0] != 255 || f.Pixels[1] != 0 || f.Pixels[4] != 255 {
			t.Fatalf("bad RGB555 conversion: %v", f.Pixels)
		}
	}
}
func TestLegacyRejectsShortRun(t *testing.T) {
	d := legacyDecoder{}
	if _, e := d.Decode(legacyFrame(2, []byte{0, 0x7c, 3, 0x80})); e == nil {
		t.Fatal("accepted RLE overflow")
	}
}
func FuzzLegacyDecode(f *testing.F) {
	f.Add(legacyFrame(0, []byte{0, 0x7c, 31, 0}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		d := legacyDecoder{}
		d.Decode(b)
	})
}
