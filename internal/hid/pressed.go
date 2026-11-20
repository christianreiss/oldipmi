package hid

import "errors"

// ValidKey reports whether usage is a key a console can send: from A (4)
// through the right Super key (231). Usages are as protocol.Input carries them.
func ValidKey(usage uint32) bool { return usage >= 4 && usage <= RightSuper }

// ErrInvalidUsage refuses a key event whose usage is not ValidKey.
var ErrInvalidUsage = errors.New("invalid HID usage")

// Pressed is the set of keys a console holds down, so that it can let go of
// each again and build its keyboard report. The zero value holds none.
type Pressed struct{ bits [4]uint64 }

// Set records usage going down or up; it ignores a usage that is not ValidKey.
func (p *Pressed) Set(usage uint32, down bool) {
	if !ValidKey(usage) {
		return
	}
	if down {
		p.bits[usage/64] |= 1 << (usage % 64)
	} else {
		p.bits[usage/64] &^= 1 << (usage % 64)
	}
}

// Has reports whether usage is held down.
func (p *Pressed) Has(usage uint32) bool {
	return ValidKey(usage) && p.bits[usage/64]&(1<<(usage%64)) != 0
}

// Keys returns the held keys, lowest usage first.
func (p *Pressed) Keys() []uint32 {
	var keys []uint32
	for u := uint32(4); u <= RightSuper; u++ {
		if p.Has(u) {
			keys = append(keys, u)
		}
	}
	return keys
}

// Clear lets go of every key.
func (p *Pressed) Clear() { *p = Pressed{} }

// BootReport is the held keys as a USB HID boot keyboard report: the modifier
// bits (Left Ctrl is bit 0, Right Super bit 7), a reserved zero byte, then up
// to six other keys, lowest usage first. Further keys are left out.
func (p *Pressed) BootReport() [8]byte {
	var r [8]byte
	n := 2
	for _, u := range p.Keys() {
		if u >= LeftCtrl {
			r[0] |= 1 << (u - LeftCtrl)
		} else if n < len(r) {
			r[n] = byte(u)
			n++
		}
	}
	return r
}
