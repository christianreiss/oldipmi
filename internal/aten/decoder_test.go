package aten

import (
	"encoding/binary"
	"testing"
)

func TestHermonKeyframeAndTile(t *testing.T) {
	d := decoder{}
	if e := d.Resize(16, 16); e != nil {
		t.Fatal(e)
	}
	p := make([]byte, 522)
	p[0] = 1
	for i := 10; i < len(p); i += 2 {
		p[i+1] = 0x7c
	}
	if e := d.Decode(p); e != nil {
		t.Fatal(e)
	}
	if d.frame.Pixels[0] != 255 || d.frame.Pixels[3] != 255 {
		t.Fatal("wrong color")
	}
	p = make([]byte, 528)
	binary.BigEndian.PutUint32(p[2:], 1)
	for i := 16; i < len(p); i += 2 {
		p[i] = 31
	}
	if e := d.Decode(p); e != nil {
		t.Fatal(e)
	}
	if d.frame.Pixels[0] != 0 || d.frame.Pixels[2] != 255 {
		t.Fatal("tile did not replace keyframe")
	}
}
func TestRLEBounds(t *testing.T) {
	for _, b := range [][]byte{{3, 0, 0, 0, 0x55, 9, 8}, {2, 0, 0, 0, 0x55, 9}, {5, 0, 0, 0, 1}} {
		if _, e := unrle(b, 4); e == nil {
			t.Fatal("accepted malformed RLE")
		}
	}
	b, e := unrle([]byte{3, 0, 0, 0, 0x55, 3, 7}, 4)
	if e != nil || len(b) != 4 || b[3] != 7 {
		t.Fatal(b, e)
	}
}
func TestPalette4(t *testing.T) {
	d := decoder{}
	d.Resize(4, 1)
	p := make([]byte, 1036)
	p[0] = 1
	p[1] = 5
	p[2] = 2
	binary.BigEndian.PutUint32(p[6:], uint32(len(p)))
	p[12] = 255
	p[1034] = 0
	p[1035] = 0
	if e := d.Decode(p); e != nil {
		t.Fatal(e)
	}
	if d.frame.Pixels[0] != 255 {
		t.Fatal("bad palette")
	}
}
func FuzzDecode(f *testing.F) {
	f.Add([]byte{0})
	f.Add(make([]byte, 10))
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > 1<<20 {
			return
		}
		d := decoder{}
		d.Resize(16, 16)
		d.Decode(p)
	})
}
