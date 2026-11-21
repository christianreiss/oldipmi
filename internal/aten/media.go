// ATEN USB bulk-only transport framing adapted from ATENtion (MIT).
package aten

import (
	"context"
	"encoding/binary"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/scsi"
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
}

// newMedia opens the ISO and connects the media channel; ctx bounds the
// connection only, the session lasts until close.
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
	return &mediaSession{wire: w, file: f, blocks: uint32(st.Size() / 2048)}, nil
}
func (s *mediaSession) close() { s.once.Do(func() { s.wire.Close() }) }
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
		if alloc > scsi.MaxTransfer {
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

// drive is what the virtual CD-ROM tells the host in its INQUIRY data.
var drive = scsi.Identity{Format: 0x21, Vendor: "OldIPMI ", Product: "Virtual CD-ROM  ", Revision: "0001"}

// scsi answers one command of the host with its data and status. A failed
// command keeps its sense data for the next REQUEST SENSE.
func (s *mediaSession) scsi(c []byte, alloc int) ([]byte, byte) {
	fail := func(key, asc byte) ([]byte, byte) { s.sense = [3]byte{key, asc, 0}; return nil, 1 }
	if len(c) < 16 {
		return fail(scsi.KeyIllegalRequest, scsi.ASCInvalidField)
	}
	switch c[0] {
	case scsi.TestUnitReady, scsi.StartStopUnit, scsi.PreventAllow:
		return nil, 0
	case scsi.RequestSense:
		b := scsi.Sense(s.sense[0], s.sense[1], s.sense[2])
		s.sense = [3]byte{}
		return b[:min(len(b), int(c[4]))], 0
	case scsi.Inquiry:
		b := drive.Inquiry()
		return b[:min(len(b), int(c[4]))], 0
	case scsi.ModeSense6:
		return scsi.ModeHeader6(), 0
	case scsi.ModeSense10:
		return scsi.ModeHeader10(), 0
	case scsi.ReadCapacity10:
		return scsi.Capacity(s.blocks - 1), 0
	case scsi.Read10, scsi.Read12:
		lba, n := scsi.Transfer(c)
		if uint64(lba)+uint64(n) > uint64(s.blocks) {
			return fail(scsi.KeyIllegalRequest, scsi.ASCLBAOutOfRange)
		}
		b := make([]byte, min(uint64(n)*scsi.BlockSize, uint64(alloc)))
		if _, e := s.file.ReadAt(b, int64(lba)*scsi.BlockSize); e != nil {
			return fail(scsi.KeyMediumError, scsi.ASCUnrecoveredRead)
		}
		return b, 0
	case scsi.ReadTOC:
		b := scsi.TOC(s.blocks, c[1]&2 != 0)
		return b[:min(len(b), int(binary.BigEndian.Uint16(c[7:])))], 0
	default:
		return fail(scsi.KeyIllegalRequest, scsi.ASCInvalidOpcode)
	}
}
