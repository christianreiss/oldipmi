package video

import (
	"encoding/binary"
	"slices"
	"testing"
)

// part is a packet of the opcode stream: the frame's height and width, flags
// (1 starts a frame, 2 ends it) and opcodes.
func part(w, h int, flags byte, code ...byte) []byte {
	p := make([]byte, 12, 12+len(code))
	binary.BigEndian.PutUint16(p[4:], uint16(h))
	binary.BigEndian.PutUint16(p[6:], uint16(w))
	p[8] = flags
	return append(p, code...)
}

// A frame whose opcodes span two packets, split inside an opcode, comes out as
// the same opcodes decoded at once; only the last packet completes it.
func TestStreamGathersTheFrameParts(t *testing.T) {
	code := []byte{0xFF, 0x00, 0x83, 0xE0, 0x80, 0x1F, 0xFF, 0xFF, 0x20 | 4} // four colours, then the last one 4 more times
	want := NewFramebuffer(8, 1)
	if _, err := NewDecoder(Depth15).Decode(want, code); err != nil {
		t.Fatal(err)
	}
	s, fb := NewStream(1<<20), NewFramebuffer(1024, 768)
	if done, err := s.Feed(fb, Depth15, part(8, 1, 1, code[:3]...)); done || err != nil {
		t.Fatalf("first part: done %v, %v", done, err)
	}
	if done, err := s.Feed(fb, Depth15, part(8, 1, 2, code[3:]...)); !done || err != nil {
		t.Fatalf("last part: done %v, %v", done, err)
	}
	if fb.W != 8 || fb.H != 1 || !slices.Equal(fb.Pixels, want.Pixels) {
		t.Fatalf("frame %dx%d %x, want 8x1 %x", fb.W, fb.H, fb.Pixels, want.Pixels)
	}
}

// The parts of one frame may carry the limit's bytes together, not one more;
// a new frame counts from zero.
func TestStreamLimitsAFrame(t *testing.T) {
	s, fb := NewStream(4), NewFramebuffer(8, 1)
	if _, err := s.Feed(fb, Depth15, part(8, 1, 1, 0xFF, 0x00, 0x83, 0xE0)); err != nil {
		t.Fatalf("four bytes: %v", err)
	}
	if _, err := s.Feed(fb, Depth15, part(8, 1, 0, 0x80)); err == nil || err.Error() != "oversized Avocent frame" {
		t.Fatalf("a fifth byte: %v", err)
	}
	if _, err := s.Feed(fb, Depth15, part(8, 1, 1, 0xFF, 0x00, 0x83, 0xE0)); err != nil {
		t.Fatalf("a new frame: %v", err)
	}
}

func TestStreamRejectsBadParts(t *testing.T) {
	for _, c := range []struct {
		name string
		part []byte
		want string
	}{
		{"short", part(8, 1, 3)[:11], "short Avocent video packet"},
		{"no width", part(0, 1, 3), "invalid Avocent framebuffer dimensions"},
		{"too wide", part(4097, 1, 3), "invalid Avocent framebuffer dimensions"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewStream(1<<20).Feed(NewFramebuffer(8, 1), Depth15, c.part); err == nil || err.Error() != c.want {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
}
