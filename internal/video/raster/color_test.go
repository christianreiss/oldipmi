package raster

import (
	"math"
	"testing"
)

func TestColours(t *testing.T) {
	for _, c := range []struct {
		name string
		f    func(uint16) (byte, byte, byte)
		v    uint16
		want [3]byte
	}{
		{"RGB555", RGB555, 0x7fff, [3]byte{255, 255, 255}},
		{"RGB555", RGB555, 0x7c00, [3]byte{255, 0, 0}},
		{"RGB555", RGB555, 0x03e0, [3]byte{0, 255, 0}},
		{"RGB555", RGB555, 0x001f, [3]byte{0, 0, 255}},
		{"RGB555", RGB555, 0x8421, [3]byte{8, 8, 8}},    // the top bit is no colour
		{"RGB555", RGB555, 0x0c63, [3]byte{25, 25, 25}}, // 3*255/31 is 24.7
		{"RGB565", RGB565, 0xffff, [3]byte{255, 255, 255}},
		{"RGB565", RGB565, 0xf800, [3]byte{255, 0, 0}},
		{"RGB565", RGB565, 0x07e0, [3]byte{0, 255, 0}},
		{"RGB565", RGB565, 0x0020, [3]byte{0, 4, 0}},
		{"RGB555Scaled", RGB555Scaled, 0x7fff, [3]byte{255, 255, 255}},
		{"RGB555Scaled", RGB555Scaled, 0x4210, [3]byte{131, 131, 131}}, // 16*255/31 rounds down
		{"RGB555Scaled", RGB555Scaled, 0x8421, [3]byte{8, 8, 8}},
		{"RGB555Scaled", RGB555Scaled, 0x0c63, [3]byte{24, 24, 24}},
	} {
		if r, g, b := c.f(c.v); [3]byte{r, g, b} != c.want {
			t.Errorf("%s(%#04x) = %d, %d, %d, want %v", c.name, c.v, r, g, b, c.want)
		}
	}
}

// RGB555 and RGB565 give each channel the 8-bit value nearest to its share of
// 255, every value of five or six bits (A2-08: shifting alone made 31 248).
func TestFullRange(t *testing.T) {
	near := func(name string, got byte, v, max int) {
		if math.Abs(float64(got)-float64(v*255)/float64(max)) > 0.5 {
			t.Errorf("%s: %d of %d became %d", name, v, max, got)
		}
	}
	for v := range 64 {
		_, g, _ := RGB565(uint16(v) << 5)
		near("RGB565 green", g, v, 63)
		if v < 32 {
			r, _, _ := RGB555(uint16(v) << 10)
			_, g, b := RGB555(uint16(v)<<5 | uint16(v))
			near("RGB555 red", r, v, 31)
			near("RGB555 green", g, v, 31)
			near("RGB555 blue", b, v, 31)
			r, _, b = RGB565(uint16(v)<<11 | uint16(v))
			near("RGB565 red", r, v, 31)
			near("RGB565 blue", b, v, 31)
		}
	}
}
