package ast

import (
	"encoding/binary"
	"fmt"
)

// header.go — port of soc/video/VideoHeader.java (the 86-byte frame header)
// and the subset of soc/SOCFrameHdr.java + VideoEngineInfo that the decoder
// reads. An AMI frame is [86-byte VideoHeader][compressed tile data]; the
// ATEN HTML5 console sends the same tile data behind a header of its own.

// videoHeaderSize is VideoHeader.VIDEO_HDR_SIZE.
const videoHeaderSize = 86

// Params describe a frame's tile data: the fields of VideoEngineInfo's
// FRAME_HEADER and DestinationModeInfo that the decoder reads.
type Params struct {
	Width, Height int // DestinationModeInfo.X/.Y: the picture size in pixels

	TableSelector        int  // JPEGTableSelector (selector), below len(tblY)
	AdvanceTableSelector int  // AdvanceTableSelector (advance_selector), below len(tblY)
	YUVTableMapping      int  // JPEGYUVTableMapping (Mapping): 0 or 1
	Mode420              int  // Mode420: 1 for YUV 4:2:0, 0 for 4:4:4
	RC4                  bool // RC4Enable is 1: the tile data is RC4-encrypted

	// CompressSize is CompressData.CompressSize: the bytes of tile data at
	// the start of the stream; the rest is ignored.
	CompressSize int
}

// ParseHeader ports VideoHeader.set() (little-endian field reads) followed by
// SOCFrameHdr.getFrameVariables(). It returns the Params of an AMI frame and
// the tile data after its 86-byte header.
func ParseHeader(frame []byte) (Params, []byte, error) {
	if len(frame) < videoHeaderSize {
		return Params{}, nil, fmt.Errorf("kvm/codec: frame too short: %d < %d", len(frame), videoHeaderSize)
	}
	le := binary.LittleEndian
	b := frame

	// VideoHeader.set field order (all little-endian):
	//  0  iEngVersion          short
	//  2  wHeaderLen           short
	//  4  SourceMode_X         short
	//  6  SourceMode_Y         short
	//  8  SourceMode_ColorDepth   short
	// 10  SourceMode_RefreshRate  short
	// 12  SourceMode_ModeIndex    byte
	// 13  DestinationMode_X    short
	// 15  DestinationMode_Y    short
	// 17  DestinationMode_ColorDepth short
	// 19  DestinationMode_RefreshRate short
	// 21  DestinationMode_ModeIndex byte
	// 22  FrameHdr_StartCode   int
	// 26  FrameHdr_FrameNumber int
	// 30  FrameHdr_HSize       short
	// 32  FrameHdr_VSize       short
	// 34  FrameHdr_Reserved[0] int
	// 38  FrameHdr_Reserved[1] int
	// 42  FrameHdr_CompressionMode  byte
	// 43  FrameHdr_JPEGScaleFactor  byte
	// 44  FrameHdr_JPEGTableSelector byte
	// 45  FrameHdr_JPEGYUVTableMapping byte
	// 46  FrameHdr_SharpModeSelection  byte
	// 47  FrameHdr_AdvanceTableSelector byte
	// 48  FrameHdr_AdvanceScaleFactor  byte
	// 49  FrameHdr_NumberOfMB  int
	// 53  FrameHdr_RC4Enable   byte
	// 54  FrameHdr_RC4Reset    byte
	// 55  Mode420              byte
	// 56  InfData_DownScalingMethod byte
	// 57  InfData_DifferentialSetting byte
	// 58  InfData_AnalogDifferentialThreshold short
	// 60  InfData_DigitalDifferentialThreshold short
	// 62  InfData_ExternalSignalEnable byte
	// 63  InfData_AutoMode     byte
	// 64  InfData_VQMode       byte
	// 65  CompressData_SourceFrameSize int
	// 69  CompressData_CompressSize    int
	// 73  CompressData_HDebug  int
	// 77  CompressData_VDebug  int
	// 81  InputSignal          byte
	// 82  Cursor_XPos          short
	// 84  Cursor_YPos          short
	p := Params{
		Width:                int(le.Uint16(b[13:])),
		Height:               int(le.Uint16(b[15:])),
		TableSelector:        int(b[44]),
		YUVTableMapping:      int(b[45]),
		AdvanceTableSelector: int(b[47]),
		RC4:                  b[53] == 1,
		Mode420:              int(b[55]),
		CompressSize:         int(int32(le.Uint32(b[69:]))),
	}
	return p, frame[videoHeaderSize:], nil
}
