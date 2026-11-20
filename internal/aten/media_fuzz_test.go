package aten

import (
	"bytes"
	"encoding/binary"
	"net"
	"oldipmi/internal/protocol"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// replay is the BMC's end of a media connection: it plays back what the BMC
// sends and keeps the answers.
type replay struct {
	in  *bytes.Reader
	out bytes.Buffer
}

func (r *replay) Read(p []byte) (int, error)       { return r.in.Read(p) }
func (r *replay) Write(p []byte) (int, error)      { return r.out.Write(p) }
func (r *replay) Close() error                     { return nil }
func (r *replay) LocalAddr() net.Addr              { return nil }
func (r *replay) RemoteAddr() net.Addr             { return nil }
func (r *replay) SetDeadline(time.Time) error      { return nil }
func (r *replay) SetReadDeadline(time.Time) error  { return nil }
func (r *replay) SetWriteDeadline(time.Time) error { return nil }

// The ISO channel fed arbitrary BMC packets: the session ends with an error or
// an eject, never a panic, and every answer is a whole packet.
func FuzzMediaSession(f *testing.F) {
	iso := filepath.Join(f.TempDir(), "boot.iso")
	if err := os.WriteFile(iso, make([]byte, 8*2048), 0o600); err != nil {
		f.Fatal(err)
	}
	// command wraps a SCSI command (its first byte op) for the data-in direction.
	command := func(op byte, alloc uint32, cdb ...byte) []byte {
		p := make([]byte, 31)
		copy(p, "USBC")
		binary.LittleEndian.PutUint32(p[4:], 7)
		binary.LittleEndian.PutUint32(p[8:], alloc)
		p[12], p[14], p[15] = 0x80, 16, op
		copy(p[16:], cdb)
		return append(mediaHeader(0, 0, 31), p...)
	}
	f.Add(command(0x12, 36, 0, 0, 0, 36))                                                      // INQUIRY
	f.Add(command(0x25, 8))                                                                    // READ CAPACITY
	f.Add(append(command(0x28, 2048, 0, 0, 0, 0, 0, 0, 0, 1), command(3, 18, 0, 0, 0, 18)...)) // READ(10), REQUEST SENSE
	f.Add(append(command(0xa8, 4096, 0, 0, 0, 0, 7, 0, 0, 0, 2), mediaHeader(0, 4, 0)...))     // READ(12) past the end, eject
	f.Fuzz(func(t *testing.T, in []byte) {
		file, err := os.Open(iso)
		if err != nil {
			t.Fatal(err)
		}
		conn := &replay{in: bytes.NewReader(in)}
		s := &mediaSession{wire: &protocol.Wire{Conn: conn}, file: file, blocks: 8, stop: func() bool { return true }}
		s.serve()
		for out := conn.out.Bytes(); len(out) > 0; {
			if len(out) < 8 || out[0] != 0x22 {
				t.Fatalf("answer % x", out)
			}
			n := int(binary.LittleEndian.Uint32(out[4:]))
			if n > len(out)-8 {
				t.Fatalf("an answer claims %d bytes, %d follow", n, len(out)-8)
			}
			out = out[8+n:]
		}
	})
}
