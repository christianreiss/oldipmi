package video

import (
	"bytes"
	"encoding/binary"
	"oldipmi/internal/testkit"
	"testing"
)

func TestExpandRLEEscapes(t *testing.T) {
	const rep, tri = 0xaa, 0x55
	in := []byte{
		1, 2, // literals
		tri, 7, // 7 ×3
		rep, 4, 9, // 9 ×5
		rep, 0, // literal repeat code
		rep, 1, // literal triplet code
		3,
	}
	want := []byte{1, 2, 7, 7, 7, 9, 9, 9, 9, 9, rep, tri, 3}
	got, err := expandRLE(in, rep, tri, 100)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("ExpandRLE = % x, %v; want % x", got, err, want)
	}
	if raw, _ := expandRLE(in, 0, 0, 100); !bytes.Equal(raw, in) {
		t.Fatal("codes 0/0 must pass data through unchanged")
	}
	for _, bad := range [][]byte{{tri}, {rep}, {rep, 5}} {
		if _, err := expandRLE(bad, rep, tri, 100); err == nil {
			t.Errorf("truncated escape % x accepted", bad)
		}
	}
	if _, err := expandRLE([]byte{rep, 200, 1}, rep, tri, 100); err == nil {
		t.Error("run beyond the limit accepted")
	}
	if _, err := expandRLE(in, 3, 3, 100); err == nil {
		t.Error("equal repeat and triplet codes accepted")
	}
}

// bitSlice encodes ARGB pixels the way the BMC does: plane k carries bit
// 7-k/3 of channel R, G, B (k%3), pixel i in bit i%8 of byte i/8.
func encodeBitSlice(px []uint32, bpp int) []byte {
	stride := len(px) / 8
	out := make([]byte, stride*bpp)
	for k := range bpp {
		m := planeMask(bpp, k)
		for i, c := range px {
			if c&m != 0 {
				out[k*stride+i/8] |= 1 << (i % 8)
			}
		}
	}
	return out
}

func rvasPacket(sub, flags byte, w, h, bpp int, rect []int, data []byte) []byte {
	p := make([]byte, 12)
	p[0], p[1], p[2], p[3] = sub, flags, 0xaa, 0x55
	binary.BigEndian.PutUint16(p[4:], uint16(h))
	binary.BigEndian.PutUint16(p[6:], uint16(w))
	p[8] = byte(bpp)
	if rect != nil {
		p[9] = byte(len(rect) / 4)
		for _, v := range rect {
			p = binary.BigEndian.AppendUint16(p, uint16(v))
		}
	}
	return append(p, data...)
}

func TestRVASBitSlice15AcrossPackets(t *testing.T) {
	// A 16x1 tile at (8,1) of a 32x4 screen: red, green, blue, white, then grey.
	px := make([]uint32, 16)
	for i := range px {
		px[i] = 0xFF808080
	}
	px[0], px[1], px[2], px[3] = 0xFFF80000, 0xFF00F800, 0xFF0000F8, 0xFFF8F8F8
	enc := encodeBitSlice(px, 15)
	// RLE-code it: every 0xaa / 0x55 byte must be escaped.
	var rle []byte
	for _, b := range enc {
		switch b {
		case 0xaa:
			rle = append(rle, 0xaa, 0)
		case 0x55:
			rle = append(rle, 0xaa, 1)
		default:
			rle = append(rle, b)
		}
	}
	rect := []int{1, 8, 1, 23} // firstY, firstX, lastY, lastX
	half := len(rle) / 2
	fb := NewFramebuffer(1, 1)
	var r RVAS

	// Not a reference frame yet: skipped, nothing shown.
	if done, err := r.Feed(fb, rvasPacket(rvasBitSlice, rvasSOF|rvasEOF, 32, 4, 15, rect, rle)); done || err != nil || r.Skipped != 1 {
		t.Fatalf("non-reference start: done=%v err=%v skipped=%d", done, err, r.Skipped)
	}
	if done, err := r.Feed(fb, rvasPacket(rvasBitSlice, rvasSOF|rvasRef, 32, 4, 15, rect, rle[:half])); done || err != nil {
		t.Fatalf("first chunk: done=%v err=%v", done, err)
	}
	if fb.W != 32 || fb.H != 4 || fb.Dirty {
		t.Fatalf("after SOF: %dx%d dirty=%v", fb.W, fb.H, fb.Dirty)
	}
	done, err := r.Feed(fb, rvasPacket(rvasBitSlice, rvasEOF, 32, 4, 15, nil, rle[half:]))
	if !done || err != nil || !fb.Dirty {
		t.Fatalf("last chunk: done=%v err=%v dirty=%v", done, err, fb.Dirty)
	}
	for i, want := range px {
		if got := fb.Pixels[1*32+8+i]; got != want {
			t.Errorf("pixel %d = %08x, want %08x", i, got, want)
		}
	}
	if fb.Pixels[0] != 0xFF000000 || fb.Pixels[1*32+7] != 0xFF000000 {
		t.Error("pixels outside the tile changed")
	}
	// A keep-alive "no change" packet keeps the picture.
	if done, err := r.Feed(fb, []byte{rvasNoChange, 15, 0, 4, 0, 32}); done || err != nil || fb.Pixels[1*32+8] != px[0] {
		t.Fatalf("no-change packet: done=%v err=%v", done, err)
	}
}

func TestRVASRawAndBytePlane(t *testing.T) {
	var r RVAS
	fb := NewFramebuffer(1, 1)
	rect := []int{0, 0, 0, 1}
	// RAW 24 bpp is B, G, R.
	raw := []byte{0x01, 0x02, 0x03, 0x10, 0x20, 0x30}
	p := rvasPacket(rvasRaw, rvasSOF|rvasEOF|rvasRef, 2, 1, 24, rect, raw)
	p[2], p[3] = 0, 0 // uncompressed
	if done, err := r.Feed(fb, p); !done || err != nil {
		t.Fatalf("raw: done=%v err=%v", done, err)
	}
	if fb.Pixels[0] != 0xFF030201 || fb.Pixels[1] != 0xFF302010 {
		t.Fatalf("raw pixels %08x %08x", fb.Pixels[0], fb.Pixels[1])
	}
	// BYTEPLANE 16 bpp RGB555 (alpha flag): high bytes, then low bytes.
	p = rvasPacket(rvasBytePlane, rvasSOF|rvasEOF|rvasRef|rvasAlpha, 2, 1, 16, rect, []byte{0x7c, 0x03, 0x00, 0xe0})
	p[2], p[3] = 0, 0
	if done, err := r.Feed(fb, p); !done || err != nil {
		t.Fatalf("byteplane: done=%v err=%v", done, err)
	}
	if fb.Pixels[0] != 0xFFF80000 || fb.Pixels[1] != 0xFF00F800 {
		t.Fatalf("byteplane pixels %08x %08x", fb.Pixels[0], fb.Pixels[1])
	}
	// RAW 8 bpp needs the palette.
	p = rvasPacket(rvasRaw, rvasSOF|rvasEOF|rvasRef, 2, 1, 8, rect, []byte{1, 0})
	p[2], p[3] = 0, 0
	if _, err := r.Feed(fb, p); err == nil {
		t.Fatal("paletted frame without palette accepted")
	}
	if err := r.SetPalette([]byte{0, 2, 8, 0, 1, 2, 3, 4, 5, 6}); err != nil {
		t.Fatal(err)
	}
	if done, err := r.Feed(fb, p); !done || err != nil || fb.Pixels[0] != 0xFF040506 || fb.Pixels[1] != 0xFF010203 {
		t.Fatalf("paletted: done=%v err=%v %08x %08x", done, err, fb.Pixels[0], fb.Pixels[1])
	}
}

func TestRVASRejectsBadInput(t *testing.T) {
	var r RVAS
	fb := NewFramebuffer(1, 1)
	cases := map[string][]byte{
		"tile outside screen": rvasPacket(rvasBitSlice, rvasSOF|rvasRef|rvasEOF, 8, 1, 15, []int{0, 0, 0, 8}, make([]byte, 15)),
		"short data":          rvasPacket(rvasBitSlice, rvasSOF|rvasRef|rvasEOF, 8, 1, 15, []int{0, 0, 0, 7}, make([]byte, 3)),
		"huge screen":         rvasPacket(rvasBitSlice, rvasSOF|rvasRef, 9000, 1, 15, nil, nil),
		"text mode":           {rvasASCII, 0, 0, 0, 1, 144, 2, 208, 25, 80, 0, 0},
	}
	for name, p := range cases {
		_, err := r.Feed(fb, p)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		if name == "text mode" {
			testkit.ErrorRow(t, "avocent: text mode", err)
		}
	}
}

// A frame during which the screen changes size is dropped: its tiles were
// placed on the old screen and drawing them overran the new one (A2-02). The
// next frame is drawn.
func TestRVASSizeChangeInsideFrame(t *testing.T) {
	data := encodeBitSlice(make([]uint32, 16), 15)
	for name, change := range map[string]func(*RVAS, *Framebuffer){
		"no-change packet": func(r *RVAS, fb *Framebuffer) { r.Feed(fb, []byte{rvasNoChange, 0, 0, 1, 0, 32}) },
		"another codec":    func(_ *RVAS, fb *Framebuffer) { fb.Resize(32, 1) },
	} {
		var r RVAS
		fb := NewFramebuffer(1024, 768)
		r.Feed(fb, rvasPacket(rvasBitSlice, rvasSOF|rvasRef, 32, 4, 15, []int{1, 8, 1, 23}, data[:15]))
		change(&r, fb)
		if done, err := r.Feed(fb, rvasPacket(rvasBitSlice, rvasEOF, 32, 4, 15, nil, data[15:])); done || err != nil || r.Skipped != 1 {
			t.Errorf("%s: done=%v err=%v skipped=%d", name, done, err, r.Skipped)
		}
		if done, err := r.Feed(fb, rvasPacket(rvasBitSlice, rvasSOF|rvasEOF, 32, 1, 15, []int{0, 8, 0, 23}, data)); !done || err != nil {
			t.Errorf("%s: next frame: done=%v err=%v", name, done, err)
		}
	}
}
