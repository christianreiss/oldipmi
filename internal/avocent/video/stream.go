package video

import (
	"encoding/binary"
	"fmt"
	"oldipmi/internal/video/raster"
)

// Stream gathers the parts of a frame that an Avocent console sends in its
// video packets 129-131 and decodes the frame into a Framebuffer once its
// last part is in.
type Stream struct {
	limit          int // the most bytes the parts of one frame may carry together
	rgb, pal, gray *Decoder
	agg            []byte
}

// NewStream returns a Stream whose frames may carry limit bytes.
func NewStream(limit int) *Stream {
	return &Stream{limit: limit, rgb: NewDecoder(Depth15), pal: NewDecoder(Depth7Palette), gray: NewDecoder(Depth7Gray)}
}

// Feed takes one part, in the colour depth of its packet. The part's 12-byte
// header holds the frame's height and width at offsets 4 and 6 and flags at
// 8: 1 starts a frame, 2 ends it. Feed reports whether the part ended a frame;
// the frame is then decoded into fb.
func (s *Stream) Feed(fb *Framebuffer, depth Depth, part []byte) (bool, error) {
	if len(part) < 12 {
		return false, fmt.Errorf("short Avocent video packet")
	}
	height := int(binary.BigEndian.Uint16(part[4:]))
	width := int(binary.BigEndian.Uint16(part[6:]))
	if !raster.ValidSize(width, height) {
		return false, fmt.Errorf("invalid Avocent framebuffer dimensions")
	}
	fb.Resize(width, height)
	if part[8]&1 != 0 {
		fb.Cur = 0
		s.agg = nil
	}
	d := s.rgb
	switch depth {
	case Depth7Palette:
		d = s.pal
	case Depth7Gray:
		d = s.gray
	}
	if len(s.agg)+len(part)-12 > s.limit {
		return false, fmt.Errorf("oversized Avocent frame")
	}
	s.agg = append(s.agg, part[12:]...)
	if part[8]&2 == 0 {
		return false, nil
	}
	if _, err := d.Decode(fb, s.agg); err != nil {
		return false, err
	}
	return true, nil
}
