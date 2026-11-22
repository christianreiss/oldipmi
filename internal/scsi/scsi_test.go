package scsi

import (
	"bytes"
	"testing"
)

// The answers byte for byte, as the AMI and ATEN drives sent them before they
// shared this package; their own tests pin what each drive sends.
func TestAnswers(t *testing.T) {
	discInfo := make([]byte, 34)
	copy(discInfo, []byte{0, 32, 0x0e, 1, 1, 1, 1})
	for _, c := range []struct {
		name      string
		got, want []byte
	}{
		{"sense", Sense(KeyIllegalRequest, ASCLBAOutOfRange, 1),
			[]byte{0x70, 0, 5, 0, 0, 0, 0, 10, 0, 0, 0, 0, 0x21, 1, 0, 0, 0, 0}},
		{"INQUIRY", Identity{Version: 5, Format: 2, Vendor: "Vendor  ", Product: "Product         ", Revision: "0.1 "}.Inquiry(),
			append([]byte{5, 0x80, 5, 2, 31, 0, 0, 0}, "Vendor  Product         0.1 "...)},
		{"READ CAPACITY", Capacity(0x01020304), []byte{1, 2, 3, 4, 0, 0, 8, 0}},
		{"MODE SENSE (6)", ModeHeader6(), []byte{3, 0, 0x80, 0}},
		{"MODE SENSE (10)", ModeHeader10(), []byte{0, 6, 0, 0x80, 0, 0, 0, 0}},
		{"READ TOC in LBA", TOC(0x01020304, false),
			[]byte{0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 0, 0, 0, 0x14, 0xaa, 0, 1, 2, 3, 4}},
		// 333037 blocks and the 150 before block 0 are 74 minutes, 2 seconds, 37 frames.
		{"READ TOC in MSF", TOC(333037, true),
			[]byte{0, 18, 1, 1, 0, 0x14, 1, 0, 0, 0, 2, 0, 0, 0x14, 0xaa, 0, 0, 74, 2, 37}},
		{"GET CONFIGURATION", Configuration(), []byte{0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0}},
		{"GET EVENT STATUS", NoEvent([]byte{GetEventStatus, 1, 0, 0, 0x10}), []byte{0, 2, 0, 0x10}},
		{"GET EVENT STATUS cut short", NoEvent([]byte{GetEventStatus, 1}), []byte{0, 2, 0, 0}},
		{"READ DISC INFORMATION", DiscInfo(), discInfo},
	} {
		if !bytes.Equal(c.got, c.want) {
			t.Errorf("%s: % x, want % x", c.name, c.got, c.want)
		}
	}
}

// Only a START STOP UNIT with LoEj set, Start clear and no power condition
// ejects the medium.
func TestEjects(t *testing.T) {
	for _, c := range []struct {
		cdb  []byte
		want bool
	}{
		{[]byte{StartStopUnit, 0, 0, 0, 2, 0}, true},
		{[]byte{StartStopUnit, 1, 0, 0, 2, 0}, true},     // immediate
		{[]byte{StartStopUnit, 0, 0, 0, 0x32, 0}, false}, // a power condition: LoEj is ignored
		{[]byte{StartStopUnit, 0, 0, 0, 3, 0}, false},    // loads the medium
		{[]byte{StartStopUnit, 0, 0, 0, 1, 0}, false},    // starts the drive
		{[]byte{StartStopUnit, 0, 0, 0, 0, 0}, false},    // stops it
		{[]byte{TestUnitReady, 0, 0, 0, 2, 0}, false},
		{[]byte{StartStopUnit, 0, 0, 0}, false},
		{nil, false},
	} {
		if got := Ejects(c.cdb); got != c.want {
			t.Errorf("% x: %t, want %t", c.cdb, got, c.want)
		}
	}
}

func TestTransfer(t *testing.T) {
	for _, c := range []struct {
		cdb         []byte
		lba, blocks uint32
	}{
		{[]byte{Read10, 0, 1, 2, 3, 4, 0, 5, 6, 0}, 0x01020304, 0x0506},
		{[]byte{Write10, 0, 1, 2, 3, 4, 9, 5, 6, 0}, 0x01020304, 0x0506}, // byte 6 is the group
		{[]byte{Read12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0}, 0x01020304, 0x05060708},
		{[]byte{Write12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0}, 0x01020304, 0x05060708},
	} {
		if lba, blocks := Transfer(c.cdb); lba != c.lba || blocks != c.blocks {
			t.Errorf("% x: block %#x, %#x blocks; want %#x, %#x", c.cdb, lba, blocks, c.lba, c.blocks)
		}
	}
}
