package video

import (
	"encoding/binary"
	"fmt"
	"oldipmi/internal/video/raster"
)

// RVAS decodes the Avocent "RLE video" stream (AVSP packet 0x8b / 0x860b)
// that Cisco IMC (Pilot-class BMCs) sends instead of the 129-131 opcode
// stream. Reconstructed from the format the BMC's HTML5 viewer accepts.
//
// Each packet starts with a subtype byte. Graphics subtypes (RAW, BYTEPLANE,
// BITSLICE) share a 12-byte header:
//
//	[0] subtype  [1] flags  [2] repeat code  [3] triplet code
//	[4:6] height [6:8] width (big-endian)  [8] bits per pixel  [9] blocks
//
// A frame spans packets from SOF to EOF. The SOF packet's data begins with
// blocks*8 bytes of tile rectangles (firstY, firstX, lastY, lastX), the rest
// of the data of all packets is one byte-level RLE stream expanded at EOF.
type RVAS struct {
	rle            []byte
	blocks         []rvasBlock
	bpp, flags     int
	repeat, triple byte
	palette        []uint32
	reference      bool // a reference (full) frame has been seen
	inFrame        bool
	w, h           int // the screen size the frame in progress was placed on
	subtype        byte
	// Skipped counts packets dropped: those before the first reference frame,
	// and the rest of a frame during which the screen changed size.
	Skipped int
}

type rvasBlock struct{ x, y, w, h int }

const (
	rvasNoChange   = 0
	rvasCursorPos  = 1
	rvasAttribute  = 2
	rvasASCII      = 3
	rvasRaw        = 4
	rvasBytePlane  = 5
	rvasBitSlice   = 6
	rvasCursorBMP  = 7
	rvasCursorMove = 8

	rvasSOF      = 0x01
	rvasEOF      = 0x02
	rvasRef      = 0x04
	rvasAlpha    = 0x08 // 16 bpp is RGB555 rather than RGB565
	rvasPaletted = 0x40
	rvasPacked   = 0x80
)

// SetPalette applies a colour palette message (AVSP 0x88 / 0x8608):
// [0:2] number of colours, [2] bits per channel, entries of R,G,B from [4].
func (r *RVAS) SetPalette(msg []byte) error {
	if len(msg) < 4 {
		return fmt.Errorf("short RLE palette message")
	}
	n := int(binary.BigEndian.Uint16(msg))
	if n > 256 || len(msg) < 4+3*n {
		return fmt.Errorf("invalid RLE palette message")
	}
	r.palette = make([]uint32, n)
	for i := range r.palette {
		p := msg[4+3*i:]
		r.palette[i] = 0xFF000000 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
	}
	return nil
}

// Feed consumes one packet. It returns true when a frame was completed and
// fb holds new pixels.
func (r *RVAS) Feed(fb *Framebuffer, p []byte) (bool, error) {
	if len(p) < 1 {
		return false, fmt.Errorf("empty RLE video packet")
	}
	switch p[0] {
	case rvasNoChange:
		if len(p) < 6 {
			return false, fmt.Errorf("short RLE video packet")
		}
		h, w := int(binary.BigEndian.Uint16(p[2:])), int(binary.BigEndian.Uint16(p[4:]))
		if r.reference {
			if e := resize(fb, w, h); e != nil {
				return false, e
			}
		}
		return false, nil
	case rvasRaw, rvasBytePlane, rvasBitSlice:
		return r.graphics(fb, p)
	case rvasCursorBMP, rvasCursorMove:
		return false, nil // the host draws its own cursor into the picture
	case rvasCursorPos, rvasAttribute, rvasASCII:
		return false, fmt.Errorf("unsupported video format: this BMC is in text mode (Cisco IMC RLE text), which OldIPMI cannot draw yet")
	}
	return false, fmt.Errorf("unsupported Cisco IMC RLE video subtype %d", p[0])
}

func resize(fb *Framebuffer, w, h int) error {
	if !raster.ValidSize(w, h) {
		return fmt.Errorf("invalid Avocent framebuffer dimensions")
	}
	if w != fb.W || h != fb.H {
		fb.Resize(w, h)
		fb.Dirty = false // nothing decoded yet; do not show a black frame
	}
	return nil
}

func (r *RVAS) graphics(fb *Framebuffer, p []byte) (bool, error) {
	if len(p) < 12 {
		return false, fmt.Errorf("short RLE video packet")
	}
	flags := int(p[1])
	data := p[12:]
	if flags&rvasSOF != 0 {
		if !r.reference && flags&rvasRef == 0 {
			r.Skipped++
			return false, nil
		}
		h, w := int(binary.BigEndian.Uint16(p[4:])), int(binary.BigEndian.Uint16(p[6:]))
		if e := resize(fb, w, h); e != nil {
			return false, e
		}
		n := int(p[9])
		if len(data) < n*8 {
			return false, fmt.Errorf("RLE video: block list exceeds packet")
		}
		r.blocks = r.blocks[:0]
		for i := range n {
			b := data[i*8:]
			y0, x0 := int(binary.BigEndian.Uint16(b)), int(binary.BigEndian.Uint16(b[2:]))
			y1, x1 := int(binary.BigEndian.Uint16(b[4:])), int(binary.BigEndian.Uint16(b[6:]))
			if x1 < x0 || y1 < y0 || x1 >= fb.W || y1 >= fb.H {
				return false, fmt.Errorf("RLE video: tile outside the screen")
			}
			r.blocks = append(r.blocks, rvasBlock{x0, y0, x1 - x0 + 1, y1 - y0 + 1})
		}
		data = data[n*8:]
		r.rle = r.rle[:0]
		r.subtype, r.flags, r.bpp = p[0], flags, int(p[8])
		r.w, r.h = fb.W, fb.H
		r.reference, r.inFrame = true, true
	} else if !r.inFrame {
		r.Skipped++
		return false, nil
	} else if fb.W != r.w || fb.H != r.h {
		// A no-change packet or another codec resized the screen: the tiles
		// of this frame were placed on the old one, so it is dropped.
		r.inFrame = false
		r.Skipped++
		return false, nil
	} else if p[0] != r.subtype {
		r.inFrame = false
		return false, fmt.Errorf("RLE video: subtype changed inside a frame")
	}
	r.repeat, r.triple = p[2], p[3]
	if len(r.rle)+len(data) > fb.W*fb.H*4+1024 {
		r.inFrame = false
		return false, fmt.Errorf("RLE video: oversized frame")
	}
	r.rle = append(r.rle, data...)
	if flags&rvasEOF == 0 {
		return false, nil
	}
	r.inFrame = false
	raw, e := ExpandRLE(r.rle, r.repeat, r.triple, fb.W*fb.H*4)
	if e != nil {
		return false, e
	}
	switch r.subtype {
	case rvasBitSlice:
		e = r.bitSlice(fb, raw)
	case rvasBytePlane:
		e = r.bytePlane(fb, raw)
	default:
		e = r.raw(fb, raw)
	}
	if e != nil {
		return false, e
	}
	fb.Dirty = true
	return true, nil
}

// ExpandRLE undoes the byte-level run-length coding. A triplet code byte is
// followed by a value repeated three times; a repeat code byte by n and a
// value repeated n+1 times, where n+1 of 1 or 2 stands for a literal repeat
// or triplet code byte (consuming two bytes). Both codes zero means raw.
func ExpandRLE(in []byte, repeat, triple byte, limit int) ([]byte, error) {
	if repeat == 0 && triple == 0 {
		return in, nil
	}
	if repeat == triple {
		return nil, fmt.Errorf("RLE video: repeat and triplet codes are equal")
	}
	out := make([]byte, 0, min(limit, 4*len(in)))
	emit := func(v byte, n int) error {
		if len(out)+n > limit {
			return fmt.Errorf("RLE video: expanded data exceeds the frame")
		}
		for range n {
			out = append(out, v)
		}
		return nil
	}
	for i := 0; i < len(in); {
		var e error
		switch b := in[i]; {
		case b == triple:
			if i+1 >= len(in) {
				return nil, fmt.Errorf("RLE video: truncated triplet")
			}
			e, i = emit(in[i+1], 3), i+2
		case b == repeat:
			if i+1 >= len(in) {
				return nil, fmt.Errorf("RLE video: truncated run")
			}
			switch n := int(in[i+1]) + 1; n {
			case 1:
				e, i = emit(repeat, 1), i+2
			case 2:
				e, i = emit(triple, 1), i+2
			default:
				if i+2 >= len(in) {
					return nil, fmt.Errorf("RLE video: truncated run")
				}
				e, i = emit(in[i+2], n), i+3
			}
		default:
			e, i = emit(b, 1), i+1
		}
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}

// planeMask maps bit plane k of a bpp-deep bit-sliced pixel to its ARGB bit:
// planes interleave R, G, B from the most significant bit down.
func planeMask(bpp, k int) uint32 {
	if bpp == 16 && k == 15 {
		return 0x400 // the sixth green bit of RGB565
	}
	return 1 << (uint(2-k%3)*8 + uint(7-k/3))
}

func (r *RVAS) bitSlice(fb *Framebuffer, d []byte) error {
	if r.bpp < 9 || r.bpp > 24 || (r.bpp%3 != 0 && r.bpp != 16) {
		return fmt.Errorf("RLE video: unsupported bit-slice depth %d", r.bpp)
	}
	base := 0
	for _, b := range r.blocks {
		px := b.w * b.h
		if px%8 != 0 {
			return fmt.Errorf("RLE video: bit-slice tile is not a multiple of 8 pixels")
		}
		stride := px / 8
		if base+stride*r.bpp > len(d) {
			return fmt.Errorf("RLE video: bit-slice data too short")
		}
		tile := make([]uint32, px)
		for i := range tile {
			tile[i] = 0xFF000000
		}
		for k := range r.bpp {
			plane, m := d[base+k*stride:base+(k+1)*stride], planeMask(r.bpp, k)
			for i := range tile {
				if plane[i>>3]&(1<<(i&7)) != 0 {
					tile[i] |= m
				}
			}
		}
		base += stride * r.bpp
		fb.setTile(b, tile)
	}
	return nil
}

func (r *RVAS) bytePlane(fb *Framebuffer, d []byte) error {
	total := 0
	for _, b := range r.blocks {
		total += b.w * b.h
	}
	planes := map[int]int{16: 2, 24: 3}[r.bpp]
	if planes == 0 {
		return fmt.Errorf("RLE video: unsupported byte-plane depth %d", r.bpp)
	}
	if len(d) < total*planes {
		return fmt.Errorf("RLE video: byte-plane data too short")
	}
	off := 0
	for _, b := range r.blocks {
		tile := make([]uint32, b.w*b.h)
		for i := range tile {
			j := off + i
			if planes == 3 {
				tile[i] = 0xFF000000 | uint32(d[j])<<16 | uint32(d[total+j])<<8 | uint32(d[2*total+j])
			} else {
				tile[i] = r.rgb16(uint16(d[j])<<8 | uint16(d[total+j]))
			}
		}
		off += len(tile)
		fb.setTile(b, tile)
	}
	return nil
}

func (r *RVAS) rgb16(v uint16) uint32 {
	var R, G, B uint32
	if r.flags&rvasAlpha != 0 {
		R, G, B = uint32(v>>10&0x1f)<<3, uint32(v>>5&0x1f)<<3, uint32(v&0x1f)<<3
	} else {
		R, G, B = uint32(v>>11&0x1f)<<3, uint32(v>>5&0x3f)<<2, uint32(v&0x1f)<<3
	}
	return 0xFF000000 | R<<16 | G<<8 | B
}

func (r *RVAS) raw(fb *Framebuffer, d []byte) error {
	o := 0
	need := func(n int) error {
		if o+n > len(d) {
			return fmt.Errorf("RLE video: raw data too short")
		}
		return nil
	}
	pal := func(i byte) (uint32, error) {
		if int(i) >= len(r.palette) {
			return 0, fmt.Errorf("RLE video: paletted frame before the palette")
		}
		return r.palette[i], nil
	}
	for _, b := range r.blocks {
		tile := make([]uint32, b.w*b.h)
		switch r.bpp {
		case 24, 32:
			step := r.bpp / 8
			if e := need(len(tile) * step); e != nil {
				return e
			}
			for i := range tile {
				tile[i] = 0xFF000000 | uint32(d[o+2])<<16 | uint32(d[o+1])<<8 | uint32(d[o])
				o += step
			}
		case 16:
			if e := need(len(tile) * 2); e != nil {
				return e
			}
			for i := range tile {
				tile[i] = r.rgb16(uint16(d[o+1])<<8 | uint16(d[o]))
				o += 2
			}
		case 8:
			if e := need(len(tile)); e != nil {
				return e
			}
			for i := range tile {
				c, e := pal(d[o])
				if e != nil {
					return e
				}
				tile[i], o = c, o+1
			}
		case 4:
			i := 0
			for range b.h {
				for x := range b.w {
					if e := need(1); e != nil {
						return e
					}
					v := d[o]
					if r.flags&rvasPacked == 0 {
						o++
					} else if x&1 != 0 {
						v >>= 4
						o++
					} else {
						v &= 0xf
					}
					c, e := pal(v)
					if e != nil {
						return e
					}
					tile[i], i = c, i+1
				}
			}
		default:
			return fmt.Errorf("RLE video: unsupported raw depth %d", r.bpp)
		}
		fb.setTile(b, tile)
	}
	return nil
}

func (f *Framebuffer) setTile(b rvasBlock, tile []uint32) {
	for y := range b.h {
		copy(f.Pixels[(b.y+y)*f.W+b.x:][:b.w], tile[y*b.w:][:b.w])
	}
}
