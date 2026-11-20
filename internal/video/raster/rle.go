package raster

import "errors"

// The errors of ExpandRLE.
var (
	ErrRLECodes    = errors.New("repeat and triplet codes are equal")
	ErrRLETriplet  = errors.New("truncated triplet")
	ErrRLERun      = errors.New("truncated run")
	ErrRLEOverflow = errors.New("expanded data exceeds the frame")
)

// ExpandRLE appends the run-length coded src to dst. A triplet code byte is
// followed by a value written three times; a repeat code byte by n and a
// value written n+1 times, except that n of 0 or 1 stands for a literal
// repeat or triplet code byte (two bytes in all). dst grows to limit bytes
// at most. A code byte that ends src is an error, unless lenientTail is set:
// then it stands for itself.
func ExpandRLE(dst, src []byte, repeat, triple byte, limit int, lenientTail bool) ([]byte, error) {
	if repeat == triple {
		return nil, ErrRLECodes
	}
	emit := func(v byte, n int) error {
		if n > limit-len(dst) {
			return ErrRLEOverflow
		}
		for range n {
			dst = append(dst, v)
		}
		return nil
	}
	for i := 0; i < len(src); {
		var err error
		switch b := src[i]; {
		case (b == triple || b == repeat) && i+1 == len(src) && lenientTail:
			err, i = emit(b, 1), i+1
		case b == triple:
			if i+1 >= len(src) {
				return nil, ErrRLETriplet
			}
			err, i = emit(src[i+1], 3), i+2
		case b == repeat:
			if i+1 >= len(src) {
				return nil, ErrRLERun
			}
			switch n := int(src[i+1]) + 1; n {
			case 1:
				err, i = emit(repeat, 1), i+2
			case 2:
				err, i = emit(triple, 1), i+2
			default:
				if i+2 >= len(src) {
					return nil, ErrRLERun
				}
				err, i = emit(src[i+2], n), i+3
			}
		default:
			err, i = emit(b, 1), i+1
		}
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}
