package video

import (
	"encoding/binary"
	"slices"
	"testing"
)

// FuzzRVAS feeds a stream of Cisco IMC video messages to one decoder and
// framebuffer, as the Avocent client does: no input may crash it, and the
// framebuffer always holds W×H pixels. Each message is a 2-byte big-endian
// header and its bytes. The header's top two bits say what the message is: 0
// a video packet, 1 a palette, 2 a packet of the client's other codecs, which
// resizes the framebuffer they share to the width and height in its first 4
// bytes. The other 14 bits are its length.
func FuzzRVAS(f *testing.F) {
	msg := func(kind int, p []byte) []byte {
		return append(binary.BigEndian.AppendUint16(nil, uint16(kind<<14|len(p))), p...)
	}
	// A 16x1 bit-slice tile at (8,1) of a 32x4 screen, in two packets.
	data := encodeBitSlice(make([]uint32, 16), 15)
	rect := []int{1, 8, 1, 23}
	sof := msg(0, rvasPacket(rvasBitSlice, rvasSOF|rvasRef, 32, 4, 15, rect, data[:15]))
	eof := msg(0, rvasPacket(rvasBitSlice, rvasEOF, 32, 4, 15, nil, data[15:]))
	f.Add(slices.Concat(sof, eof))
	f.Add(slices.Concat(sof, msg(0, []byte{rvasNoChange, 0, 0, 4, 0, 32}), eof))
	f.Add(slices.Concat(sof, msg(2, []byte{0, 32, 0, 4}), eof))
	f.Add(slices.Concat(
		msg(1, []byte{0, 2, 8, 0, 1, 2, 3, 4, 5, 6}),
		msg(0, rvasPacket(rvasRaw, rvasSOF|rvasRef|rvasEOF|rvasPaletted, 2, 1, 8, []int{0, 0, 0, 1}, []byte{1, 0}))))
	f.Fuzz(func(t *testing.T, in []byte) {
		var r RVAS
		fb := NewFramebuffer(1, 1) // small, so that inputs run fast
		for len(in) >= 2 {
			head := binary.BigEndian.Uint16(in)
			n := min(int(head&0x3fff), len(in)-2)
			p := in[2 : 2+n]
			in = in[2+n:]
			switch head >> 14 {
			case 1:
				r.SetPalette(p)
			case 2:
				if len(p) >= 4 {
					w, h := int(binary.BigEndian.Uint16(p)), int(binary.BigEndian.Uint16(p[2:]))
					if w >= 1 && h >= 1 && w <= 4096 && h <= 2160 {
						fb.Resize(w, h)
					}
				}
			default:
				r.Feed(fb, p)
			}
			if len(fb.Pixels) != fb.W*fb.H {
				t.Fatalf("a %dx%d framebuffer holds %d pixels", fb.W, fb.H, len(fb.Pixels))
			}
		}
	})
}
