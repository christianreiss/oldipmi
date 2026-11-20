package hid

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"
)

func TestValidKey(t *testing.T) {
	for u, want := range map[uint32]bool{0: false, 3: false, 4: true, 40: true, 223: true, 224: true, 231: true, 232: false, 255: false, 256: false, 1 << 31: false} {
		if ValidKey(u) != want {
			t.Errorf("ValidKey(%d) = %t", u, !want)
		}
	}
}

func TestPressed(t *testing.T) {
	var p Pressed
	if p.Keys() != nil || p.Has(4) {
		t.Fatal("the zero value holds keys")
	}
	for _, u := range []uint32{231, 40, 4, 224, 40, 63, 64, 127, 128, 191, 192} {
		p.Set(u, true)
	}
	for _, u := range []uint32{0, 3, 232, 255, 256, 1 << 31} { // not keys: ignored
		p.Set(u, true)
		if p.Has(u) {
			t.Errorf("holds the usage %d", u)
		}
	}
	if want := []uint32{4, 40, 63, 64, 127, 128, 191, 192, 224, 231}; !reflect.DeepEqual(p.Keys(), want) {
		t.Fatalf("keys %v, want %v", p.Keys(), want)
	}
	p.Set(64, false)
	p.Set(65, false) // not held
	if want := []uint32{4, 40, 63, 127, 128, 191, 192, 224, 231}; !reflect.DeepEqual(p.Keys(), want) {
		t.Fatalf("keys %v after letting go of 64, want %v", p.Keys(), want)
	}
	p.Clear()
	if p.Keys() != nil {
		t.Fatal("Clear kept", p.Keys())
	}
}

// oldBootReport is the AMI client's keyboard report before Pressed existed
// (internal/ami/client.go keyboard()), kept to pin BootReport to it.
func oldBootReport(pressed map[uint32]bool) []byte {
	r := make([]byte, 8)
	n := 2
	for u := uint32(4); u <= 231; u++ {
		if !pressed[u] {
			continue
		}
		if u >= 224 {
			r[0] |= 1 << uint(u-224)
		} else if n < 8 {
			r[n] = byte(u)
			n++
		}
	}
	return r
}

func TestBootReport(t *testing.T) {
	for _, c := range []struct {
		keys []uint32
		want [8]byte
	}{
		{nil, [8]byte{}},
		{[]uint32{225, 4}, [8]byte{0x02, 0, 4}},                                               // Shift+A
		{[]uint32{231, 224, 226, 76}, [8]byte{0x85, 0, 76}},                                   // Ctrl+Alt+Del, Right Super
		{[]uint32{9, 8, 7, 6, 5, 4}, [8]byte{0, 0, 4, 5, 6, 7, 8, 9}},                         // six keys, sorted
		{[]uint32{30, 31, 32, 33, 34, 35, 36, 228}, [8]byte{0x10, 0, 30, 31, 32, 33, 34, 35}}, // the 7th is left out
	} {
		var p Pressed
		for _, u := range c.keys {
			p.Set(u, true)
		}
		if got := p.BootReport(); got != c.want {
			t.Errorf("%v: % x, want % x", c.keys, got, c.want)
		}
	}
	// Random sets, as the old report saw them.
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		var p Pressed
		old := map[uint32]bool{}
		for n := rng.Intn(14); n > 0; n-- {
			u := uint32(4 + rng.Intn(228))
			if rng.Intn(3) == 0 {
				u = uint32(224 + rng.Intn(8))
			}
			down := rng.Intn(5) != 0
			p.Set(u, down)
			if down {
				old[u] = true
			} else {
				delete(old, u)
			}
		}
		if got, want := p.BootReport(), oldBootReport(old); !bytes.Equal(got[:], want) {
			t.Fatalf("keys %v: % x, the old report % x", p.Keys(), got, want)
		}
	}
}
