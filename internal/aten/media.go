// ATEN USB bulk-only transport framing adapted from ATENtion (MIT).
package aten

import (
	"context"
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"os"
	"sync"
)

type mediaSession struct {
	wire   *transport.Wire
	file   *os.File
	blocks uint32
	sense  [3]byte
	once   sync.Once
	stop   func() bool
}

func newMedia(ctx context.Context, p protocol.Profile, port int, path string) (s *mediaSession, err error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() < 2048 || st.Size()%2048 != 0 || st.Size()/2048 > 0xffffffff {
		f.Close()
		return nil, fmt.Errorf("invalid ISO image")
	}
	w, e := transport.Dial(ctx, p.Policy(), port, false)
	if e != nil {
		f.Close()
		return nil, e
	}
	s = &mediaSession{wire: w, file: f, blocks: uint32(st.Size() / 2048)}
	s.stop = context.AfterFunc(ctx, func() { w.Close() })
	return s, nil
}
func (s *mediaSession) close() { s.once.Do(func() { s.stop(); s.wire.Close() }) }
func mediaHeader(lun, flag byte, n int) []byte {
	b := []byte{0x22, 0, lun, flag, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[4:], uint32(n))
	return b
}
func (s *mediaSession) serve() error {
	defer s.file.Close()
	for {
		h, e := protocol.Read(s.wire, 8)
		if e != nil {
			return e
		}
		n := binary.LittleEndian.Uint32(h[4:])
		if n > 1<<20 {
			return fmt.Errorf("ATEN media packet too large")
		}
		p, e := protocol.Read(s.wire, int(n))
		if e != nil {
			return e
		}
		if h[3] == 4 {
			return nil
		}
		if n != 31 {
			continue
		}
		if string(p[:4]) != "USBC" || p[14] < 1 || p[14] > 16 {
			return fmt.Errorf("invalid USB command wrapper")
		}
		alloc := binary.LittleEndian.Uint32(p[8:])
		if alloc > 1<<20 {
			return fmt.Errorf("ISO transfer exceeds 1 MiB")
		}
		// Data-out commands are rejected; drain their bounded payload before status.
		if p[12]&0x80 == 0 && alloc != 0 {
			return fmt.Errorf("writable virtual media is unsupported")
		}
		data, status := s.scsi(p[15:], int(alloc))
		if len(data) > int(alloc) {
			data = data[:alloc]
		}
		if p[12]&0x80 != 0 && len(data) > 0 {
			if e = s.wire.WritePacket(append(mediaHeader(h[2], 0, len(data)), data...)); e != nil {
				return e
			}
		}
		csw := make([]byte, 13)
		copy(csw, "USBS")
		copy(csw[4:8], p[4:8])
		binary.LittleEndian.PutUint32(csw[8:], alloc-uint32(len(data)))
		csw[12] = status
		if e = s.wire.WritePacket(append(mediaHeader(h[2], 255, 13), csw...)); e != nil {
			return e
		}
	}
}
func (s *mediaSession) scsi(c []byte, alloc int) ([]byte, byte) {
	fail := func(key, asc byte) ([]byte, byte) { s.sense = [3]byte{key, asc, 0}; return nil, 1 }
	if len(c) < 16 {
		return fail(5, 0x24)
	}
	switch c[0] {
	case 0, 0x1b, 0x1e:
		return nil, 0
	case 3:
		b := make([]byte, 18)
		b[0] = 0x70
		b[2] = s.sense[0]
		b[7] = 10
		b[12] = s.sense[1]
		b[13] = s.sense[2]
		s.sense = [3]byte{}
		return b[:min(len(b), int(c[4]))], 0
	case 0x12:
		b := make([]byte, 36)
		copy(b, []byte{5, 128, 0, 0x21, 31})
		copy(b[8:], "OldIPMI Virtual CD-ROM  0001")
		return b[:min(36, int(c[4]))], 0
	case 0x1a:
		return []byte{3, 0, 128, 0}, 0
	case 0x5a:
		return []byte{0, 6, 0, 128, 0, 0, 0, 0}, 0
	case 0x25:
		b := make([]byte, 8)
		binary.BigEndian.PutUint32(b, s.blocks-1)
		binary.BigEndian.PutUint32(b[4:], 2048)
		return b, 0
	case 0x28, 0xa8:
		lba := uint64(binary.BigEndian.Uint32(c[2:]))
		n := uint64(binary.BigEndian.Uint16(c[7:]))
		if c[0] == 0xa8 {
			n = uint64(binary.BigEndian.Uint32(c[6:]))
		}
		if lba+n > uint64(s.blocks) {
			return fail(5, 0x21)
		}
		size := min(n*2048, uint64(alloc))
		b := make([]byte, size)
		if _, e := s.file.ReadAt(b, int64(lba*2048)); e != nil {
			return fail(3, 0x11)
		}
		return b, 0
	case 0x43:
		b := make([]byte, 20)
		b[1] = 18
		b[2] = 1
		b[3] = 1
		b[5] = 0x14
		b[6] = 1
		b[13] = 0x14
		b[14] = 0xaa
		for _, v := range []struct {
			o int
			l uint32
		}{{8, 0}, {16, s.blocks}} {
			if c[1]&2 == 0 {
				binary.BigEndian.PutUint32(b[v.o:], v.l)
			} else {
				f := v.l + 150
				b[v.o+1] = byte(f / 4500)
				b[v.o+2] = byte(f / 75 % 60)
				b[v.o+3] = byte(f % 75)
			}
		}
		return b[:min(len(b), int(binary.BigEndian.Uint16(c[7:])))], 0
	default:
		return fail(5, 0x20)
	}
}
