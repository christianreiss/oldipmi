package raster

// RGB555 splits a 15-bit pixel, red in bits 10-14, into 8-bit channels by
// shifting each to the top of its byte: 31 becomes 248.
func RGB555(v uint16) (r, g, b byte) {
	return byte(v>>10&31) << 3, byte(v>>5&31) << 3, byte(v&31) << 3
}

// RGB565 splits a 16-bit pixel, red in bits 11-15 and six bits of green, the
// same way: 31 becomes 248 and 63 becomes 252.
func RGB565(v uint16) (r, g, b byte) {
	return byte(v>>11&31) << 3, byte(v>>5&63) << 2, byte(v&31) << 3
}

// RGB555Scaled splits a 15-bit pixel into 8-bit channels scaled to the full
// range: 31 becomes 255.
func RGB555Scaled(v uint16) (r, g, b byte) {
	return byte(int(v>>10&31) * 255 / 31), byte(int(v>>5&31) * 255 / 31), byte(int(v&31) * 255 / 31)
}
