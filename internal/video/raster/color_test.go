package raster

import "testing"

func TestColours(t *testing.T) {
	for _, c := range []struct {
		name string
		f    func(uint16) (byte, byte, byte)
		v    uint16
		want [3]byte
	}{
		{"RGB555", RGB555, 0x7fff, [3]byte{248, 248, 248}},
		{"RGB555", RGB555, 0x7c00, [3]byte{248, 0, 0}},
		{"RGB555", RGB555, 0x03e0, [3]byte{0, 248, 0}},
		{"RGB555", RGB555, 0x001f, [3]byte{0, 0, 248}},
		{"RGB555", RGB555, 0x8421, [3]byte{8, 8, 8}}, // the top bit is no colour
		{"RGB565", RGB565, 0xffff, [3]byte{248, 252, 248}},
		{"RGB565", RGB565, 0xf800, [3]byte{248, 0, 0}},
		{"RGB565", RGB565, 0x07e0, [3]byte{0, 252, 0}},
		{"RGB565", RGB565, 0x0020, [3]byte{0, 4, 0}},
		{"RGB555Scaled", RGB555Scaled, 0x7fff, [3]byte{255, 255, 255}},
		{"RGB555Scaled", RGB555Scaled, 0x4210, [3]byte{131, 131, 131}}, // 16*255/31 rounds down
		{"RGB555Scaled", RGB555Scaled, 0x8421, [3]byte{8, 8, 8}},
	} {
		if r, g, b := c.f(c.v); [3]byte{r, g, b} != c.want {
			t.Errorf("%s(%#04x) = %d, %d, %d, want %v", c.name, c.v, r, g, b, c.want)
		}
	}
}
