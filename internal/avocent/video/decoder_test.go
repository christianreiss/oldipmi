package video

import (
	"bytes"
	"slices"
	"testing"
)

// synthesise a tile that: writes 4 solid pixels then repeats them across the row.
func TestDecoderRGB555Basic(t *testing.T) {
	dec := NewDecoder(Depth15)
	fb := NewFramebuffer(8, 1)

	// two COLOR ops (each = 2 bytes: op | tail):
	// pure red   0xFF0000 → RGB555 0x7C00 → op=0xFF (0x80|0x7F) tail=0x00
	// pure green 0x00FF00 → RGB555 0x03E0 → op=0x83 tail=0xE0
	// pure blue  0x0000FF → RGB555 0x001F → op=0x80 tail=0x1F
	// white     0xFFFFFF → RGB555 0x7FFF → op=0xFF tail=0xFF
	// then REPEAT 4 to fill the rest
	stream := []byte{
		0xFF, 0x00,
		0x83, 0xE0,
		0x80, 0x1F,
		0xFF, 0xFF,
		0x20 | 4, // REPEAT 4
	}
	if _, err := dec.Decode(fb, stream); err != nil {
		t.Fatal(err)
	}
	// first pixel should be red
	if r := (fb.Pixels[0] >> 16) & 0xFF; r < 0xF8 {
		t.Errorf("pixel 0 R = 0x%02x, want ≥0xF8", r)
	}
	// pixel 3 should be white
	if fb.Pixels[3]&0xFFFFFF < 0xF8F8F8 {
		t.Errorf("pixel 3 = 0x%08x, want ~white", fb.Pixels[3])
	}
	// pixels 4..7 should all equal pixel 3 (REPEAT)
	for i := 4; i < 8; i++ {
		if fb.Pixels[i] != fb.Pixels[3] {
			t.Errorf("pixel %d = 0x%08x, expected repeat of pixel 3 0x%08x", i, fb.Pixels[i], fb.Pixels[3])
		}
	}
}

func TestDecoderRGB555LUT(t *testing.T) {
	dec := NewDecoder(Depth15)
	// index 0 → black, index 0x7FFF → white
	if dec.lut[0] != 0xFF000000 {
		t.Errorf("lut[0] = 0x%08x, want black", dec.lut[0])
	}
	if got := dec.lut[0x7FFF]; got&0xFFFFFF < 0xF8F8F8 {
		t.Errorf("lut[0x7FFF] = 0x%08x, want ~white", got)
	}
}

func TestDecoderGray7(t *testing.T) {
	dec := NewDecoder(Depth7Gray)
	fb := NewFramebuffer(2, 1)
	// COLOR 0x80 → gray = 0 (black), 0xFF → gray = 254 (near white)
	stream := []byte{0x80, 0xFF}
	dec.Decode(fb, stream)
	if fb.Pixels[0] != 0xFF000000 {
		t.Errorf("gray 0 → 0x%08x", fb.Pixels[0])
	}
	got := fb.Pixels[1] & 0xFFFFFF
	// r=g=b=0xFE
	if got != 0xFEFEFE {
		t.Errorf("gray 0x7F<<1 → 0x%08x, want 0xFEFEFE", got)
	}
}

func TestDecoderPalette7(t *testing.T) {
	dec := NewDecoder(Depth7Palette)
	fb := NewFramebuffer(2, 1)
	// index 0 (r=g=b=0) then index 124 (r=g=b=255)
	stream := []byte{0x80, 0x80 | 124}
	dec.Decode(fb, stream)
	if fb.Pixels[0] != 0xFF000000 {
		t.Errorf("palette[0] = 0x%08x, want black", fb.Pixels[0])
	}
	if fb.Pixels[1] != 0xFFFFFFFF {
		t.Errorf("palette[124] = 0x%08x, want white", fb.Pixels[1])
	}
}

func TestFramebufferRepeat(t *testing.T) {
	fb := NewFramebuffer(4, 1)
	fb.Pixels[0] = 0xFF112233
	fb.Cur = 1
	fb.repeatColor(0xFFAABBCC, 3)
	if !bytes.Equal(
		asBytes(fb.Pixels[1:]),
		asBytes([]uint32{0xFFAABBCC, 0xFFAABBCC, 0xFFAABBCC}),
	) {
		t.Errorf("repeat failed: %v", fb.Pixels)
	}
}

func asBytes(u []uint32) []byte {
	b := make([]byte, len(u)*4)
	for i, v := range u {
		b[i*4] = byte(v)
		b[i*4+1] = byte(v >> 8)
		b[i*4+2] = byte(v >> 16)
		b[i*4+3] = byte(v >> 24)
	}
	return b
}

// Continuation bytes use bit 7; bit 4 is a palette bit within the seven pixels.
func TestPatternContinuationBit(t *testing.T) {
	fb := NewFramebuffer(8, 4)
	d := NewDecoder(Depth15)
	_, err := d.Decode(fb, []byte{0xfc, 0x00, 0x80, 0x1f, 0x70, 0x80, 0x10, 0x83, 0xe0})
	if err != nil {
		t.Fatal(err)
	}
	if fb.Cur != 21 || fb.Pixels[20] != 0xff00f800 {
		t.Fatalf("cursor=%d final=%08x", fb.Cur, fb.Pixels[20])
	}
}
func FuzzRGBDecoder(f *testing.F) {
	f.Add([]byte{0xfc, 0, 0x70, 0x80, 0})
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > 65536 {
			return
		}
		NewDecoder(Depth15).Decode(NewFramebuffer(16, 16), p)
	})
}

// before finds the colour before the run of last, at most one row back, and
// looks at no more pixels than it has left.
func TestBefore(t *testing.T) {
	red, black := uint32(0xFFF80000), uint32(0xFF000000)
	fb := NewFramebuffer(4, 2)
	for i := range fb.Pixels {
		fb.Pixels[i] = black
	}
	fb.Pixels[1] = red
	for _, c := range []struct {
		i, looks  int
		want      uint32
		looksLeft int
	}{
		{5, 100, red, 96},   // red 4 back
		{6, 100, black, 96}, // red 5 back: more than a row
		{5, 3, black, 0},    // red after the last look
	} {
		looks := c.looks
		if got := fb.before(c.i, black, &looks); got != c.want || looks != c.looksLeft {
			t.Errorf("before(%d) with %d looks: %08x, %d left; want %08x, %d left", c.i, c.looks, got, looks, c.want, c.looksLeft)
		}
	}
}

// The patterns of one frame look at no more than patternLooks pixels per
// pixel of the screen; then a pattern finds no colour before its run. Before,
// a frame of patterns over a plain area looked a row back for every byte, and
// one picture could take a minute (A2-14). The next frame looks again.
func TestPatternLooksPerFrame(t *testing.T) {
	const w = 256
	red, black := []byte{0xfc, 0x00}, []byte{0x80, 0x00}
	fb := NewFramebuffer(w, 2)
	d := NewDecoder(Depth15)
	// Row 0: red, then black to its end (repeat 30 | 7<<5 = 254).
	frame := slices.Concat(red, black, []byte{0x20 | 30, 0x20 | 7})
	// Patterns of black: the first finds red 255 back, each further one looks
	// a whole row back, and 33 use up the 16 looks per pixel of 512 pixels.
	frame = append(frame, bytes.Repeat([]byte{0x60}, 33)...)
	// Red, black, then a pattern of four pixels of the colour before black.
	frame = slices.Concat(frame, red, black, []byte{0x6f})
	if _, err := d.Decode(fb, frame); err != nil {
		t.Fatal(err)
	}
	at := w + 33*4 + 2
	if got := fb.Pixels[at]; got != 0xFF000000 {
		t.Errorf("pattern after the last look: %08x, want black", got)
	}
	fb.Cur = at + 4
	if _, err := d.Decode(fb, slices.Concat(red, black, []byte{0x6f})); err != nil {
		t.Fatal(err)
	}
	if got := fb.Pixels[at+6]; got != 0xFFF80000 {
		t.Errorf("pattern in the next frame: %08x, want red", got)
	}
}
