package aten

import (
	"encoding/binary"
	"testing"

	"oldipmi/internal/video/ast"
)

// oldAST2100Header is the 86-byte AMI video header that decodeAST2100 built
// for a rectangle before ast.Params existed, kept to pin html5Params to it.
func oldAST2100Header(width, height int, selector byte, mode, size int) []byte {
	hdr := make([]byte, 86)
	le := binary.LittleEndian
	le.PutUint16(hdr[4:], uint16(width))
	le.PutUint16(hdr[6:], uint16(height))
	le.PutUint16(hdr[13:], uint16(width))
	le.PutUint16(hdr[15:], uint16(height))
	hdr[43] = 16
	hdr[44] = selector
	hdr[48] = 16
	if mode == 422 {
		hdr[55] = 1
	}
	le.PutUint32(hdr[69:], uint32(size))
	return hdr
}

func TestHTML5ParamsAreTheOldHeader(t *testing.T) {
	for selector := 0; selector < 256; selector++ {
		for _, mode := range []int{422, 444} {
			for _, s := range [][3]int{{1024, 768, 8}, {1, 1, 4}, {1920, 1200, 1 << 20}, {65535, 65535, 12}, {0, 0, 0}, {640, 480, 1<<31 - 4}} {
				want, _, err := ast.ParseHeader(oldAST2100Header(s[0], s[1], byte(selector), mode, s[2]))
				if err != nil {
					t.Fatal(err)
				}
				if got := html5Params(s[0], s[1], byte(selector), mode, s[2]); got != want {
					t.Fatalf("selector %d, mode %d, %v: %+v, the old header gives %+v", selector, mode, s, got, want)
				}
			}
		}
	}
}
