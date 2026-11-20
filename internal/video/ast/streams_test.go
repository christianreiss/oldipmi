package ast

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// The AST2100 streams in testdata/streams feed the golden-frame tests of AMI
// (IVTP, internal/ami) and ATEN (HTML5, internal/aten), which compare what
// their sessions show with golden pictures. Each stream is random data that
// decodes cleanly after the frames before it: it reaches the frame-end code
// well before its data ends, so nothing depends on reading past the end.
// GENERATE_STREAMS=1 writes them anew; then rewrite the pictures in those
// packages as well (UPDATE_GOLDEN=1, see internal/testkit).

type streamSpec struct {
	w, h, selector int
	mode420, rc4   bool
	first          int // the block code the stream starts with; -1 for any (RC4 hides it)
}

var goldenStreams = map[string][]streamSpec{
	"ivtp": {
		{64, 48, 4, true, false, 0},  // JPEG blocks
		{64, 48, 4, true, false, 2},  // starting with a pass-2 block on the frame before
		{64, 48, 4, true, true, -1},  // RC4: the key stream runs on across frames
		{64, 48, 4, true, true, -1},  //
		{40, 24, 7, false, false, 5}, // 4:4:4 at another size, starting with a VQ block
	},
	"html5": {
		{64, 48, 4, true, false, 0},
		{64, 48, 4, true, false, 10}, // a pass-2 block at a position
		{64, 48, 7, false, false, 7}, // 4:4:4, a 4-colour VQ block
	},
}

// astFrame puts an AST2100 header before a stream, with the fields the ATEN
// HTML5 client fills in (decodeAST2100 in internal/aten).
func astFrame(s streamSpec, stream []byte) []byte {
	h := make([]byte, videoHeaderSize)
	le := binary.LittleEndian
	le.PutUint16(h[4:], uint16(s.w))
	le.PutUint16(h[6:], uint16(s.h))
	le.PutUint16(h[13:], uint16(s.w))
	le.PutUint16(h[15:], uint16(s.h))
	h[43], h[44], h[48] = 16, byte(s.selector), 16
	if s.rc4 {
		h[53] = 1
	}
	if s.mode420 {
		h[55] = 1
	}
	le.PutUint32(h[69:], uint32(len(stream)))
	return append(h, stream...)
}

// decodeAll decodes frames in order with one decoder and returns the last
// picture; clean reports whether every frame stopped at its frame-end code at
// least two words before its data ends.
func decodeAll(frames [][]byte) (last *Frame, words int, clean bool) {
	d := New()
	for _, b := range frames {
		f, err := d.Decode(b)
		if err != nil || d.index+2 >= (len(b)-videoHeaderSize)/4 {
			return nil, 0, false
		}
		last, words = f, d.index
	}
	return last, words, true
}

func TestGenerateGoldenStreams(t *testing.T) {
	if os.Getenv("GENERATE_STREAMS") != "1" {
		t.Skip("GENERATE_STREAMS=1 writes testdata/streams")
	}
	for name, specs := range goldenStreams {
		var chosen [][]byte
		h := fnv.New64a()
		h.Write([]byte(name))
		for i, s := range specs {
			frame := searchStream(t, chosen, s, h.Sum64()+uint64(i))
			chosen = append(chosen, frame)
			path := filepath.Join("testdata", "streams", fmt.Sprintf("%s-%d.bin", name, i+1))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, frame, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// searchStream finds a random stream that decodes cleanly after the frames
// before it, runs through several blocks (48 words at least) into a picture
// with some variety that differs from the last one, and cuts it two words
// after its frame-end code.
func searchStream(t *testing.T, before [][]byte, s streamSpec, salt uint64) []byte {
	var prev *Frame
	if len(before) > 0 {
		prev, _, _ = decodeAll(before)
	}
	r := rand.New(rand.NewPCG(uint64(s.w)<<16|uint64(s.h), salt))
	try := func(stream []byte) (*Frame, int, bool) {
		return decodeAll(append(append([][]byte(nil), before...), astFrame(s, stream)))
	}
	for range 200000 {
		stream := make([]byte, 1024)
		for i := range stream {
			stream[i] = byte(r.Uint32())
		}
		if s.first >= 0 {
			stream[3] = stream[3]&0x0f | byte(s.first)<<4 // the top bits of the first word
		}
		f, words, clean := try(stream)
		if !clean || words < 48 || colours(f) < 32 || prev != nil && f.W == prev.W && f.H == prev.H && bytes.Equal(f.Pix, prev.Pix) {
			continue
		}
		if cut := stream[:(words+3)*4]; len(cut) < len(stream) {
			if g, _, ok := try(cut); ok && bytes.Equal(g.Pix, f.Pix) {
				stream = cut
			}
		}
		return astFrame(s, stream)
	}
	t.Fatalf("no clean stream for %+v", s)
	return nil
}

func colours(f *Frame) int {
	seen := map[[3]byte]bool{}
	for i := 0; i+3 < len(f.Pix); i += 4 {
		seen[[3]byte(f.Pix[i:i+3])] = true
	}
	return len(seen)
}
