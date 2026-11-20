package ami

import (
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/video/raster"
)

// Legacy MegaRAC VSC frames: 9-byte header, followed by rectangular RGB555
// updates. LRLE control words are consumed from the end of each rectangle.
// Independently implemented from wire-format inspection of Lab-AMI's viewer;
// no vendor binary or decompiled source is distributed.
type legacyDecoder struct{ frame protocol.Frame }

func (d *legacyDecoder) Decode(b []byte) (protocol.Frame, error) {
	fail := func() (protocol.Frame, error) {
		return protocol.Frame{}, fmt.Errorf("malformed legacy AMI video frame")
	}
	if len(b) < 9 {
		return fail()
	}
	le := binary.LittleEndian
	w, h := int(le.Uint16(b[4:])), int(le.Uint16(b[6:]))
	if !raster.ValidSize(w, h) {
		return fail()
	}
	n := int(le.Uint32(b))
	if n > len(b)-9 {
		return protocol.Frame{}, fmt.Errorf("legacy frame length %d, declared %d, %dx%d", len(b), n, w, h)
	}
	if d.frame.Width != w || d.frame.Height != h {
		d.frame = protocol.Frame{Width: w, Height: h, Pixels: make([]byte, w*h*4)}
	}
	if b[8] != 8 {
		return protocol.Frame{}, fmt.Errorf("unsupported legacy AMI color mode %d; expected RGB555", b[8])
	}
	b = b[9 : 9+n]
	for len(b) > 0 {
		if len(b) < 16 {
			return fail()
		}
		x, y, rw, rh := int(le.Uint16(b)), int(le.Uint16(b[2:])), int(le.Uint16(b[4:])), int(le.Uint16(b[6:]))
		mode, size := int(le.Uint32(b[8:])), int(le.Uint32(b[12:]))
		if rw < 1 || rh < 1 || x+rw > w || y+rh > h || size > len(b)-16 {
			return protocol.Frame{}, fmt.Errorf("legacy rect %d,%d %dx%d mode %d size %d available %d", x, y, rw, rh, mode, size, len(b))
		}
		p := b[16 : 16+size]
		b = b[16+size:]
		pixels := make([]uint16, rw*rh)
		if mode == 0 {
			if len(p) != len(pixels)*2 {
				return fail()
			}
			for i := range pixels {
				pixels[i] = le.Uint16(p[i*2:])
			}
		}
		if mode == 2 || mode == 3 {
			step := 2
			if mode == 3 {
				step = 4
			}
			if len(p)%step != 0 {
				return fail()
			}
			out := len(pixels)
			word := func() (uint32, bool) {
				if len(p) < step {
					return 0, false
				}
				pos := len(p) - step
				var v uint32
				if step == 2 {
					v = uint32(le.Uint16(p[pos:]))
				} else {
					v = le.Uint32(p[pos:])
				}
				p = p[:pos]
				return v, true
			}
			for len(p) > 0 {
				ctl, ok := word()
				if !ok {
					return fail()
				}
				flag := uint32(1) << uint(step*8-1)
				count := int(ctl & (flag - 1))
				units := step / 2
				if count < 1 || count > out/units {
					return fail()
				}
				var v uint32
				for k := 0; k < count; k++ {
					if k == 0 || ctl&flag == 0 {
						v, ok = word()
						if !ok {
							return fail()
						}
					}
					for j := units - 1; j >= 0; j-- {
						out--
						pixels[out] = uint16(v >> uint(j*16))
					}
				}
			}
			if out != 0 {
				return fail()
			}
		} else if mode != 0 {
			return protocol.Frame{}, fmt.Errorf("unsupported legacy AMI compression %d", mode)
		}
		for yy := 0; yy < rh; yy++ {
			for xx := 0; xx < rw; xx++ {
				v := pixels[yy*rw+xx]
				o := ((y+yy)*w + x + xx) * 4
				d.frame.Pixels[o] = byte(int((v>>10)&31) * 255 / 31)
				d.frame.Pixels[o+1] = byte(int((v>>5)&31) * 255 / 31)
				d.frame.Pixels[o+2] = byte(int(v&31) * 255 / 31)
				d.frame.Pixels[o+3] = 255
			}
		}
	}
	return d.frame, nil
}
