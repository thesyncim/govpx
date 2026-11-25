package codecapi

import (
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

// StreamInfo describes the VP8 frame-header fields that can be read
// without fully decoding a packet.
type StreamInfo struct {
	Width              int
	Height             int
	Profile            int
	KeyFrame           bool
	ShowFrame          bool
	FirstPartitionSize int
}

// ReferenceFlags is a bit mask of VP8 reference buffers.
type ReferenceFlags uint8

const (
	ReferenceFlagLast ReferenceFlags = 1 << iota
	ReferenceFlagGolden
	ReferenceFlagAltRef
)

// ReferenceFrame selects one VP8 reference buffer.
type ReferenceFrame ReferenceFlags

const (
	ReferenceLast   ReferenceFrame = ReferenceFrame(ReferenceFlagLast)
	ReferenceGolden ReferenceFrame = ReferenceFrame(ReferenceFlagGolden)
	ReferenceAltRef ReferenceFrame = ReferenceFrame(ReferenceFlagAltRef)
)

// FrameInfo describes the most recently decoded VP8 frame.
type FrameInfo struct {
	Width             int
	Height            int
	KeyFrame          bool
	ShowFrame         bool
	Corrupted         bool
	Quantizer         int
	InternalQuantizer int
	RefUpdates        ReferenceFlags
	RefUsed           ReferenceFlags
	PTS               uint64
}

func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	info, err := vp8dec.PeekStreamInfo(packet)
	return StreamInfoFromInternal(info), err
}

func PeekVP8FrameHeader(packet []byte) (vp8dec.FrameHeader, StreamInfo, error) {
	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		return vp8dec.FrameHeader{}, StreamInfo{}, vpxerrors.ErrInvalidData
	}
	return header, StreamInfoFromInternal(vp8dec.StreamInfoFromFrameHeader(header)), nil
}

func StreamInfoFromInternal(info vp8dec.StreamInfo) StreamInfo {
	return StreamInfo{
		Width:              info.Width,
		Height:             info.Height,
		Profile:            info.Profile,
		KeyFrame:           info.KeyFrame,
		ShowFrame:          info.ShowFrame,
		FirstPartitionSize: info.FirstPartitionSize,
	}
}

// VP9StreamInfo describes parser-visible VP9 uncompressed-header metadata.
type VP9StreamInfo struct {
	Width                  int
	Height                 int
	Profile                int
	KeyFrame               bool
	ShowFrame              bool
	ShowExistingFrame      bool
	ExistingFrameSlot      uint8
	IntraOnly              bool
	ErrorResilient         bool
	RefreshFrameFlags      uint8
	Quantizer              int
	FirstPartitionSize     int
	FrameSizeFromReference bool
	FrameSizeReference     int
	TileInfoAvailable      bool
	TileLog2Cols           int
	TileLog2Rows           int
	Superframe             bool
	SuperframeFrames       int
}

func PeekVP9StreamInfo(packet []byte) (VP9StreamInfo, error) {
	info, err := vp9dec.PeekStreamInfo(packet)
	return VP9StreamInfoFromInternal(info), err
}

func VP9StreamInfoFromInternal(info vp9dec.StreamInfo) VP9StreamInfo {
	return VP9StreamInfo{
		Width:                  info.Width,
		Height:                 info.Height,
		Profile:                info.Profile,
		KeyFrame:               info.KeyFrame,
		ShowFrame:              info.ShowFrame,
		ShowExistingFrame:      info.ShowExistingFrame,
		ExistingFrameSlot:      info.ExistingFrameSlot,
		IntraOnly:              info.IntraOnly,
		ErrorResilient:         info.ErrorResilient,
		RefreshFrameFlags:      info.RefreshFrameFlags,
		Quantizer:              info.Quantizer,
		FirstPartitionSize:     info.FirstPartitionSize,
		FrameSizeFromReference: info.FrameSizeFromReference,
		FrameSizeReference:     info.FrameSizeReference,
		TileInfoAvailable:      info.TileInfoAvailable,
		TileLog2Cols:           info.TileLog2Cols,
		TileLog2Rows:           info.TileLog2Rows,
		Superframe:             info.Superframe,
		SuperframeFrames:       info.SuperframeFrames,
	}
}
