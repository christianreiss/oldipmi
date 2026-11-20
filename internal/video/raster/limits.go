// Package raster is what the console decoders share about pixels: the largest
// frame a console may send, 16-bit colours and byte run-length coding.
package raster

// The largest frame a console may send, in pixels: as wide as DCI 4K and as
// high as UHD. The desktop refuses larger frames too (testdata/ipc/names.json,
// maxFrame).
const (
	MaxWidth  = 4096
	MaxHeight = 2160
)

// ValidSize reports whether a width × height frame is within the limits.
func ValidSize(width, height int) bool {
	return width >= 1 && height >= 1 && width <= MaxWidth && height <= MaxHeight
}
