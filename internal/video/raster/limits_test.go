package raster

import "testing"

func TestValidSize(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want bool
	}{
		{1, 1, true}, {4096, 2160, true}, {640, 480, true},
		{0, 480, false}, {640, 0, false}, {-1, 480, false},
		{4097, 2160, false}, {4096, 2161, false}, {2160, 4096, false},
	} {
		if got := ValidSize(c.w, c.h); got != c.want {
			t.Errorf("ValidSize(%d, %d) = %t", c.w, c.h, got)
		}
	}
}
