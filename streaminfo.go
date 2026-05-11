package govpx

import vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"

type StreamInfo struct {
	Width   int
	Height  int
	Profile int

	KeyFrame  bool
	ShowFrame bool

	FirstPartitionSize int
}

type ReferenceFlags uint8

const (
	ReferenceFlagLast ReferenceFlags = 1 << iota
	ReferenceFlagGolden
	ReferenceFlagAltRef
)

type FrameInfo struct {
	Width  int
	Height int

	KeyFrame  bool
	ShowFrame bool
	Corrupted bool

	// Quantizer is the public 0..63 quantizer corresponding to
	// InternalQuantizer. InternalQuantizer is the VP8 base qindex reported by
	// libvpx's VPXD_GET_LAST_QUANTIZER control.
	Quantizer         int
	InternalQuantizer int
	RefUpdates        ReferenceFlags
	RefUsed           ReferenceFlags

	PTS uint64
}

func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		return StreamInfo{}, ErrInvalidData
	}
	return streamInfoFromFrameHeader(header), nil
}

func peekVP8FrameHeader(packet []byte) (vp8dec.FrameHeader, StreamInfo, error) {
	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		return vp8dec.FrameHeader{}, StreamInfo{}, ErrInvalidData
	}
	return header, streamInfoFromFrameHeader(header), nil
}

func streamInfoFromFrameHeader(header vp8dec.FrameHeader) StreamInfo {
	return StreamInfo{
		Width:              header.Width,
		Height:             header.Height,
		Profile:            header.Profile,
		KeyFrame:           header.KeyFrame(),
		ShowFrame:          header.ShowFrame,
		FirstPartitionSize: header.FirstPartitionSize,
	}
}
