package backend

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestIPCVersionAndBounds(t *testing.T) {
	for _, s := range []string{`{"version":2}`, `{broken`} {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(len(s)))
		if _, e := ReadCommand(bytes.NewReader(append(b, []byte(s)...))); e == nil {
			t.Fatal("accepted invalid command")
		}
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, MaxControl+1)
	if _, e := ReadCommand(bytes.NewReader(b)); e == nil {
		t.Fatal("accepted oversized command")
	}
}
func FuzzIPC(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) { ReadCommand(bytes.NewReader(b)) })
}
