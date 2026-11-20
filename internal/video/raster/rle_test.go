package raster

import (
	"bytes"
	"errors"
	"testing"
)

func TestExpandRLE(t *testing.T) {
	const R, T = 0x55, 0xaa // the codes ATEN uses
	for _, c := range []struct {
		dst, src []byte
		limit    int
		lenient  bool
		want     []byte
		err      error
	}{
		{nil, []byte{1, 2, 3}, 9, false, []byte{1, 2, 3}, nil},
		{nil, []byte{T, 7}, 9, false, []byte{7, 7, 7}, nil},
		{nil, []byte{R, 0, R, 1}, 9, false, []byte{R, T}, nil}, // literal codes
		{nil, []byte{R, 4, 9}, 9, false, []byte{9, 9, 9, 9, 9}, nil},
		{[]byte{1, 2}, []byte{3}, 9, false, []byte{1, 2, 3}, nil}, // appends
		{nil, []byte{1, T}, 9, false, nil, ErrRLETriplet},
		{nil, []byte{1, R}, 9, false, nil, ErrRLERun},
		{nil, []byte{R, 5}, 9, false, nil, ErrRLERun},
		{nil, []byte{1, T}, 9, true, []byte{1, T}, nil}, // a code that ends src stands for itself
		{nil, []byte{1, R}, 9, true, []byte{1, R}, nil},
		{nil, []byte{R, 5}, 9, true, nil, ErrRLERun}, // a run without its value is short either way
		{nil, []byte{R, 9, 1}, 10, false, bytes.Repeat([]byte{1}, 10), nil},
		{nil, []byte{R, 9, 1}, 9, false, nil, ErrRLEOverflow},
		{[]byte{0}, []byte{T, 1}, 3, false, nil, ErrRLEOverflow}, // the limit counts dst
	} {
		got, err := ExpandRLE(c.dst, c.src, R, T, c.limit, c.lenient)
		if !errors.Is(err, c.err) || !bytes.Equal(got, c.want) {
			t.Errorf("% x (limit %d, lenient %t) = % x, %v; want % x, %v", c.src, c.limit, c.lenient, got, err, c.want, c.err)
		}
	}
	if _, err := ExpandRLE(nil, []byte{1}, 3, 3, 9, false); err != ErrRLECodes {
		t.Errorf("equal codes: %v", err)
	}
}
