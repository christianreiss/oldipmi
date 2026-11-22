package raster

// RGB555 splits a 15-bit pixel, red in bits 10-14, into 8-bit channels over
// the full range, each the value nearest to its share of 255: 31 becomes 255.
func RGB555(v uint16) (r, g, b byte) {
	return scale5(v >> 10), scale5(v >> 5), scale5(v)
}

// RGB565 splits a 16-bit pixel, red in bits 11-15 and six bits of green, the
// same way: 31 and 63 become 255.
func RGB565(v uint16) (r, g, b byte) {
	return scale5(v >> 11), scale6(v >> 5), scale5(v)
}

// scale5 and scale6 scale the low five or six bits of v to 0-255, rounded to
// the nearest value: there is never a tie, as 31 and 63 are odd.
func scale5(v uint16) byte { return byte((int(v&31)*255 + 15) / 31) }
func scale6(v uint16) byte { return byte((int(v&63)*255 + 31) / 63) }

// RGB555Scaled splits a 15-bit pixel into 8-bit channels as v*255/31 rounded
// down, the legacy AMI decoder's formula (ami/legacy.go), which its golden
// frames pin: 31 becomes 255, and 3 becomes 24 where RGB555 gives 25.
func RGB555Scaled(v uint16) (r, g, b byte) {
	return byte(int(v>>10&31) * 255 / 31), byte(int(v>>5&31) * 255 / 31), byte(int(v&31) * 255 / 31)
}
