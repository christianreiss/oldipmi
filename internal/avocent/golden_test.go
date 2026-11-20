package avocent

import (
	"encoding/binary"
	"oldipmi/internal/avocent/apcp"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"testing"
)

// Golden frames: what a session shows for a run of video packets, compared with
// the pictures in testdata/frames (UPDATE_GOLDEN=1 rewrites them, see
// internal/testkit). Avocent sends an opcode stream (packets 129 to 131:
// RGB555, a 7-bit palette and 7-bit grey), Cisco IMC its RLE format (packet
// 139). A frame may span packets, and both draw into one framebuffer that keeps
// what a frame leaves out.

func avpt(id uint16, payload []byte) []byte { return apcp.AVPTFrame{ID: id, Payload: payload}.Encode() }

// opcodes is a packet of the opcode stream: height, width, flags (1 starts a
// frame, 2 ends it) and the opcodes.
func opcodes(id uint16, w, h int, flags byte, code []byte) []byte {
	p := make([]byte, 12, 12+len(code))
	binary.BigEndian.PutUint16(p[4:], uint16(h))
	binary.BigEndian.PutUint16(p[6:], uint16(w))
	p[8] = flags
	return avpt(id, append(p, code...))
}

// colour is the RGB555 colour opcode.
func colour(r, g, b int) []byte {
	v := r<<10 | g<<5 | b
	return []byte{0x80 | byte(v>>8), byte(v)}
}

// session runs a console that sends video, until it has shown frames frames.
func session(t *testing.T, family string, port int, video [][]byte, frames int) []protocol.Frame {
	t.Helper()
	ln, host := listenFixed(t, port)
	defer ln.Close()
	f := &fakeBMC{ssl: true, major: 1, cert: selfSigned(t), video: video}
	done := f.serve(ln)
	var g progress
	s, err := Connect(g.ctx(t), protocol.Profile{Host: host, Family: family, Username: "root", LegacyTLS: true}, "calvin")
	if err != nil {
		t.Fatal(err)
	}
	got := testkit.Collect(t, frames, s.Run)
	s.Close()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	return got
}

func TestAvocentGoldenFrames(t *testing.T) {
	const w, h = 32, 8
	var first []byte
	for _, c := range [][3]int{{31, 0, 0}, {0, 31, 0}, {0, 0, 31}, {31, 31, 0}, {0, 31, 31}, {31, 0, 31}, {16, 16, 16}, {8, 24, 4}} {
		first = append(first, colour(c[0], c[1], c[2])...)
	}
	first = append(first, 0x20|24)    // repeat the last colour: row 0 is complete
	first = append(first, 0x40, 0x42) // copy from the row above, 0 | 2<<5 = 64 pixels: rows 1 and 2
	// A pattern of the last two colours, 4 pixels and 7 for each further byte: row 3.
	first = append(first, 0x60|0x10|0x0a, 0x80|0x66, 0x80|0x19, 0x80|0x55, 0x2a)
	first = append(first, 0x00, 0x01) // skip 0 | 1<<5 = 32 pixels: row 4 keeps the old picture
	first = append(first, colour(20, 10, 5)...)
	first = append(first, 0x20|31, 0x22) // and 31 | 2<<5 = 95 more: rows 5 to 7
	var grey, palette []byte
	for i := range 32 {
		grey = append(grey, 0x80|byte(i*4))
		palette = append(palette, 0x80|byte(i*4))
	}
	video := [][]byte{
		// One frame in two packets, split inside a colour opcode.
		opcodes(129, w, h, 1, first[:5]),
		opcodes(129, w, h, 2, first[5:]),
		// Skip 8 | 1<<5 = 40 pixels, 8 blue ones, skip 16 | 6<<5 = 208: the rest stays.
		opcodes(129, w, h, 3, append(append([]byte{0x08, 0x01}, colour(0, 0, 31)...), 0x20|7, 0x10, 0x06)),
		opcodes(131, w, h, 3, append(grey, 0x40, 0x47)),    // a grey ramp, copied down 0 | 7<<5 = 224 pixels
		opcodes(130, w, h, 3, append(palette, 0x40, 0x47)), // the same through the palette
	}
	testkit.Frames(t, "avocent", session(t, "avocent", 5900, video, 4))
}

// rle is a Cisco IMC packet with 24-bit pixels in blue, green, red: subtype,
// flags (rvasSOF 1, rvasEOF 2, rvasRef 4), the escape bytes (none if zero), the
// size, the bits per pixel and, on a frame's first packet, its block
// (top, left, bottom, right).
func rle(sub, flags byte, escapes bool, w, h int, block []int, data []byte) []byte {
	p := make([]byte, 12)
	p[0], p[1] = sub, flags
	if escapes {
		p[2], p[3] = 0xaa, 0x55
	}
	binary.BigEndian.PutUint16(p[4:], uint16(h))
	binary.BigEndian.PutUint16(p[6:], uint16(w))
	p[8] = 24
	if block != nil {
		p[9] = 1
		for _, v := range block {
			p = binary.BigEndian.AppendUint16(p, uint16(v))
		}
	}
	return avpt(139, append(p, data...))
}

func TestCiscoGoldenFrames(t *testing.T) {
	const w, h = 16, 8
	const raw = 4 // rvasRaw
	var screen, part, escaped []byte
	for y := range h {
		for x := range w {
			screen = append(screen, byte(x*16), byte(y*32), byte(255-x*16))
			// Bytes equal to the escape bytes are sent escaped: 0xaa as 0xaa 0, 0x55 as 0xaa 1.
			if (x+y)%2 == 0 {
				escaped = append(escaped, 0xaa, 0)
			} else {
				escaped = append(escaped, 0xaa, 1)
			}
			escaped = append(escaped, byte(x*16), 0xaa, 1)
		}
	}
	for i := range 32 {
		part = append(part, 0, 200, byte(i*8))
	}
	video := [][]byte{
		rle(raw, 1|2|4, false, w, h, []int{0, 0, h - 1, w - 1}, screen),
		// An 8x4 block in two packets: the rest of the screen stays.
		rle(raw, 1|4, false, w, h, []int{2, 4, 5, 11}, part[:50]),
		rle(raw, 2, false, w, h, nil, part[50:]),
		rle(raw, 1|2, true, w, h, []int{0, 0, h - 1, w - 1}, escaped),
	}
	testkit.Frames(t, "cisco", session(t, "cisco", 2068, video, 3))
}
