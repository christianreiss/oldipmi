// ATEN Hermon/bit-plane decoder, adapted from ATENtion (MIT).
// See licenses/ATENtion-MIT.txt and THIRD_PARTY.md.
package aten

import (
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/video/raster"
)

type decoder struct {
	frame   protocol.Frame
	palette [1024]byte
}

func (d *decoder) Resize(w, h int) error {
	if !raster.ValidSize(w, h) {
		return fmt.Errorf("invalid ATEN resolution")
	}
	if d.frame.Width != w || d.frame.Height != h {
		d.frame = protocol.Frame{Width: w, Height: h, Pixels: make([]byte, w*h*4)}
	}
	return nil
}
func rgb555(p []byte, plane bool) []byte {
	out := make([]byte, len(p)*2)
	for i := 0; i+1 < len(p); i += 2 {
		o := i * 2
		v := binary.LittleEndian.Uint16(p[i:])
		out[o] = byte((v>>10)&31) << 3
		out[o+1] = byte((v>>5)&31) << 3
		out[o+2] = byte(v&31) << 3
		out[o+3] = 255
		if plane {
			out[o] = p[i+1] & 0xf8
			out[o+1] = (p[i] >> 3 & 0x1c) | (p[i+1] << 5)
			out[o+2] = p[i] << 3
		}
	}
	return out
}
func (d *decoder) blit(p []byte, x, y, w, h int) error {
	if x < 0 || y < 0 || w < 1 || h < 1 || len(p) != w*h*4 {
		return fmt.Errorf("bad ATEN tile")
	}
	for row := 0; row < h && y+row < d.frame.Height; row++ {
		n := min(w, d.frame.Width-x)
		if n <= 0 {
			continue
		}
		o := ((y+row)*d.frame.Width + x) * 4
		copy(d.frame.Pixels[o:o+n*4], p[row*w*4:row*w*4+n*4])
	}
	return nil
}
func (d *decoder) Decode(p []byte) error {
	if len(p) < 10 {
		return fmt.Errorf("short ATEN frame")
	}
	mode, enc, sub := p[0], p[1], p[2]
	off := 10
	if enc == 0 {
		if mode == 1 {
			n := d.frame.Width * d.frame.Height * 2
			if len(p)-10 != n {
				return fmt.Errorf("short ATEN keyframe")
			}
			return d.blit(rgb555(p[10:], false), 0, 0, d.frame.Width, d.frame.Height)
		}
		count := int(binary.BigEndian.Uint32(p[2:]))
		if count > (len(p)-10)/518 {
			return fmt.Errorf("truncated ATEN tile list")
		}
		for i := 0; i < count; i++ {
			y, x := int(p[off+4])*16, int(p[off+5])*16
			off += 6
			if e := d.blit(rgb555(p[off:off+512], false), x, y, 16, 16); e != nil {
				return e
			}
			off += 512
		}
		return nil
	}
	length := int(binary.BigEndian.Uint32(p[6:]))
	if length > len(p) || length < 10 {
		return fmt.Errorf("invalid ATEN frame length")
	}
	p = p[:length]
	if enc == 1 && sub == 0 || enc == 5 {
		if len(p) < 1034 {
			return fmt.Errorf("short ATEN palette")
		}
		copy(d.palette[:], p[10:1034])
		off = 1034
	}
	payload := p[off:]
	pixels := d.frame.Width * d.frame.Height
	if mode == 1 && (sub == 2 || enc == 1 && sub == 0) {
		n := pixels
		if sub == 2 {
			n = (pixels + 1) / 2
		}
		if len(payload) < n {
			return fmt.Errorf("short palette frame")
		}
		out := make([]byte, pixels*4)
		for i := 0; i < pixels; i++ {
			idx := 0
			if sub == 2 {
				idx = int(payload[i/2]>>uint((i%2)*4)) & 15
			} else {
				idx = int(payload[i])
			}
			copy(out[i*4:], []byte{d.palette[idx*4+2], d.palette[idx*4+1], d.palette[idx*4], 255})
		}
		return d.blit(out, 0, 0, d.frame.Width, d.frame.Height)
	}
	slots := 16
	if enc == 4 {
		slots = 32
	}
	if enc != 2 && enc != 4 {
		return fmt.Errorf("unsupported ATEN encoding %d/%d", enc, sub)
	}
	if mode == 1 {
		planes, e := unrle(payload, slots*((pixels+7)/8))
		if e != nil {
			return e
		}
		return d.blit(unplane(planes, pixels, slots), 0, 0, d.frame.Width, d.frame.Height)
	}
	if len(payload) < 4 {
		return fmt.Errorf("short bit-plane tile list")
	}
	count := int(binary.LittleEndian.Uint16(payload))
	end := 4 + count*4
	if end > len(payload) {
		return fmt.Errorf("short tile table")
	}
	total := 0
	for i := 0; i < count; i++ {
		a := 2 + i*4
		total += slots * ((int(payload[a+2])*int(payload[a+3]) + 7) / 8)
		if total > protocol.MaxPayload {
			return fmt.Errorf("tile plane limit exceeded")
		}
	}
	planes, e := unrle(payload[end:], total)
	if e != nil {
		return e
	}
	pos := 0
	for i := 0; i < count; i++ {
		a := 2 + i*4
		y, x, w, h := int(payload[a]), int(payload[a+1]), int(payload[a+2]), int(payload[a+3])
		n := slots * ((w*h + 7) / 8)
		if w*h > 0 {
			if e = d.blit(unplane(planes[pos:pos+n], w*h, slots), x, y, w, h); e != nil {
				return e
			}
		}
		pos += n
	}
	return nil
}
func unrle(p []byte, n int) ([]byte, error) {
	out := make([]byte, 0, n)
	fail := func() ([]byte, error) { return nil, fmt.Errorf("malformed ATEN RLE") }
	for off := 0; off < len(p); {
		off = (off + 3) &^ 3
		if off == len(p) {
			break
		}
		if off+4 > len(p) {
			return fail()
		}
		size := int(binary.LittleEndian.Uint32(p[off:]))
		off += 4
		if size > len(p)-off {
			return fail()
		}
		end := off + size
		for off < end {
			v := p[off]
			off++
			count := 1
			if off < end && v == 0x55 {
				q := p[off]
				off++
				if q == 0 {
					v = 0x55
				} else if q == 1 {
					v = 0xaa
				} else {
					if off >= end {
						return fail()
					}
					v = p[off]
					off++
					count = int(q) + 1
				}
			} else if off < end && v == 0xaa {
				v = p[off]
				off++
				count = 3
			}
			if count > n-len(out) {
				return fail()
			}
			for k := 0; k < count; k++ {
				out = append(out, v)
			}
		}
	}
	if len(out) != n {
		return fail()
	}
	return out, nil
}
func unplane(p []byte, n, slots int) []byte {
	stride := (n + 7) / 8
	channel := func(base, i int) byte {
		var v byte
		for k := 0; k < 8; k++ {
			v |= ((p[(base+7-k)*stride+i/8] >> uint(i%8)) & 1) << uint(k)
		}
		return v
	}
	if slots == 16 {
		b := make([]byte, n*2)
		for i := 0; i < n; i++ {
			b[i*2] = channel(8, i)
			b[i*2+1] = channel(0, i)
		}
		return rgb555(b, true)
	}
	b := make([]byte, n*4)
	for i := 0; i < n; i++ {
		b[i*4] = channel(24, i)
		b[i*4+1] = channel(0, i)
		b[i*4+2] = channel(8, i)
		b[i*4+3] = 255
	}
	return b
}
