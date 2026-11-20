package ami

import (
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"os"
	"testing"
)

// Golden frames: what a session shows for a run of video packets, compared
// with the pictures in testdata/frames (UPDATE_GOLDEN=1 rewrites them, see
// internal/testkit). They pin the packet assembly and the decoders that Phase 3
// moves: AST2100 over IVTP (the streams in codec/testdata/streams, with pass-2
// blocks and RC4 that carry state across frames) and the legacy VSC rectangles
// (RGB555 raw, RLE and LRLE), which update the picture in place.

// fragments sends a frame as the BMC does: in video packets that each start
// with their index, the last one with bit 15 set.
func fragments(e end, typ uint16, frame []byte, size int) error {
	for i := 0; len(frame) > 0; i++ {
		n := min(size, len(frame))
		seq := uint16(i)
		if n == len(frame) {
			seq |= 0x8000
		}
		if err := e.send(typ, 0, append(binary.LittleEndian.AppendUint16(nil, seq), frame[:n]...)); err != nil {
			return err
		}
		frame = frame[n:]
	}
	return nil
}

func TestIVTPGoldenFrames(t *testing.T) {
	var frames [][]byte
	for i := 1; ; i++ {
		b, err := os.ReadFile(fmt.Sprintf("codec/testdata/streams/ivtp-%d.bin", i))
		if err != nil {
			break
		}
		frames = append(frames, b)
	}
	if len(frames) != 5 {
		t.Fatalf("%d streams in codec/testdata/streams, want 5", len(frames))
	}
	cert := selfSigned(t)
	ln, port := listen(t, &cert)
	done := accept(ln, false, func(e end) error { return e.send(23, 0, nil) }, func(e end, typ uint16) error {
		switch typ {
		case 18:
			return e.send(19, 0, []byte{1})
		case 6: // the session has started: the picture follows
			for _, f := range frames {
				if err := fragments(e, 25, f, 100); err != nil {
					return err
				}
			}
		}
		return nil
	})
	c := client(protocol.Profile{Host: "127.0.0.1", Username: "admin", LegacyTLS: true},
		protocol.Launch{Family: "ami", Token: "ivtp-token", Port: port, Secure: true})
	defer c.Close()
	var stages []string
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	got := testkit.Collect(t, len(frames), c.Run)
	c.Close()
	if s := <-done; s.err != nil {
		t.Fatal(s.err)
	}
	testkit.Frames(t, "ivtp", got)
}

func rgb555(r, g, b int) uint16 { return uint16(r<<10 | g<<5 | b) }

type vscRect struct {
	x, y, w, h, mode int
	data             []byte
}

// vscFrame is a legacy VSC video frame: the length of its rectangles, the
// screen size and the colour mode (8 is RGB555), then each rectangle with its
// position, size, compression and length.
func vscFrame(w, h int, rects ...vscRect) []byte {
	le := binary.LittleEndian
	var body []byte
	for _, r := range rects {
		hdr := make([]byte, 16)
		le.PutUint16(hdr, uint16(r.x))
		le.PutUint16(hdr[2:], uint16(r.y))
		le.PutUint16(hdr[4:], uint16(r.w))
		le.PutUint16(hdr[6:], uint16(r.h))
		le.PutUint32(hdr[8:], uint32(r.mode))
		le.PutUint32(hdr[12:], uint32(len(r.data)))
		body = append(append(body, hdr...), r.data...)
	}
	b := make([]byte, 9, 9+len(body))
	le.PutUint32(b, uint32(len(body)))
	le.PutUint16(b[4:], uint16(w))
	le.PutUint16(b[6:], uint16(h))
	b[8] = 8
	return append(b, body...)
}

func raw555(pixels []uint16) []byte {
	var b []byte
	for _, v := range pixels {
		b = binary.LittleEndian.AppendUint16(b, v)
	}
	return b
}

// vscRLE encodes pixels for the legacy modes 2 (16-bit words, one pixel each)
// and 3 (32-bit words, two pixels each). The decoder reads words from the end
// backwards, filling the pixels from the last one: a control word with its top
// bit set repeats the word after it, else that many words follow.
func vscRLE(pixels []uint16, mode int) []byte {
	units, step := 1, 2
	if mode == 3 {
		units, step = 2, 4
	}
	var vals []uint32 // in the order the decoder takes them
	for out := len(pixels); out > 0; out -= units {
		v := uint32(pixels[out-1])
		if units == 2 {
			v = v<<16 | uint32(pixels[out-2])
		}
		vals = append(vals, v)
	}
	flag := uint32(1) << (step*8 - 1)
	var words []uint32
	for i := 0; i < len(vals); {
		run := 1
		for i+run < len(vals) && vals[i+run] == vals[i] {
			run++
		}
		if run > 1 {
			words = append(words, flag|uint32(run), vals[i])
			i += run
			continue
		}
		lit := 1
		for i+lit < len(vals) && !(i+lit+1 < len(vals) && vals[i+lit] == vals[i+lit+1]) {
			lit++
		}
		words = append(append(words, uint32(lit)), vals[i:i+lit]...)
		i += lit
	}
	var b []byte
	for k := len(words) - 1; k >= 0; k-- {
		if step == 2 {
			b = binary.LittleEndian.AppendUint16(b, uint16(words[k]))
		} else {
			b = binary.LittleEndian.AppendUint32(b, words[k])
		}
	}
	return b
}

func TestVSCGoldenFrames(t *testing.T) {
	const w, h = 32, 16
	var full []uint16
	for y := range h {
		for x := range w {
			full = append(full, rgb555(x, y*2, 31-x))
		}
	}
	// 8x4: a run, a row of alternating colours, then one colour for two rows.
	var runs []uint16
	for i := range 32 {
		switch {
		case i < 8:
			runs = append(runs, rgb555(31, 0, 0))
		case i < 16 && i%2 == 0:
			runs = append(runs, rgb555(0, 0, 31))
		case i < 16:
			runs = append(runs, rgb555(31, 31, 31))
		default:
			runs = append(runs, rgb555(0, 31, 0))
		}
	}
	// 12x6 for the 32-bit words: pairs that repeat, then a gradient.
	var pairs []uint16
	for i := range 72 {
		if i < 36 {
			pairs = append(pairs, []uint16{rgb555(31, 16, 0), rgb555(0, 16, 31)}[i%2])
		} else {
			pairs = append(pairs, rgb555(i-36, 31-(i-36)/2, 8))
		}
	}
	frames := [][]byte{
		vscFrame(w, h, vscRect{0, 0, w, h, 0, raw555(full)}),
		vscFrame(w, h, // updates in place, the rest stays
			vscRect{4, 2, 8, 4, 2, vscRLE(runs, 2)},
			vscRect{16, 8, 12, 6, 3, vscRLE(pairs, 3)},
			vscRect{30, 14, 2, 2, 0, raw555([]uint16{0, rgb555(31, 31, 31), rgb555(31, 31, 31), 0})}),
	}
	cert := selfSigned(t)
	video, videoPort := listen(t, nil)
	input, inputPort := listen(t, &cert)
	prelogin := func(e end) error { return e.send(14, 0, nil) }
	videoDone := accept(video, true, prelogin, func(e end, typ uint16) error {
		if typ != 7 { // the last packet of the setup
			return nil
		}
		for _, f := range frames {
			if err := fragments(e, 3, f, 300); err != nil {
				return err
			}
		}
		return nil
	})
	inputDone := accept(input, true, prelogin, nil)
	c := client(protocol.Profile{Host: "127.0.0.1", Username: "admin", LegacyTLS: true, AllowPlainConsole: true},
		protocol.Launch{Family: "ami", Legacy: true, Token: "vsc-token", Cookie: "cookie-1", Port: videoPort, HIDPort: inputPort})
	defer c.Close()
	var stages []string
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	got := testkit.Collect(t, len(frames), c.Run)
	c.Close()
	if v, in := <-videoDone, <-inputDone; v.err != nil || in.err != nil {
		t.Fatal(v.err, in.err)
	}
	testkit.Frames(t, "vsc", got)
}
