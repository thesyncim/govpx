package govpx

import vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"

// StreamInfo describes the VP8 frame-header fields that can be read
// without fully decoding a packet. It is returned by [PeekVP8StreamInfo]
// and is suitable for routing decisions before a decoder is constructed.
type StreamInfo struct {
	// Width and Height are the visible coded dimensions carried by key frames.
	// Inter frames reuse the current stream dimensions.
	Width  int
	Height int
	// Profile is the VP8 version_number field from the frame tag. The
	// VP8 spec defines values 0..3; govpx accepts 4..7 for compatibility
	// with libvpx but treats them as version 0.
	Profile int

	// KeyFrame reports whether the packet is a VP8 key frame.
	KeyFrame bool
	// ShowFrame reports whether decoding the packet produces visible output.
	ShowFrame bool

	// FirstPartitionSize is the VP8 first-partition byte count from the frame
	// tag.
	FirstPartitionSize int
}

// ReferenceFlags is a bit mask of VP8 reference buffers. It is used on
// [FrameInfo.RefUpdates] and [FrameInfo.RefUsed] to report which buffers a
// decoded frame refreshed or read.
type ReferenceFlags uint8

const (
	// ReferenceFlagLast identifies the LAST reference buffer.
	ReferenceFlagLast ReferenceFlags = 1 << iota
	// ReferenceFlagGolden identifies the GOLDEN reference buffer.
	ReferenceFlagGolden
	// ReferenceFlagAltRef identifies the alternate-reference buffer.
	ReferenceFlagAltRef
)

// ReferenceFrame selects one VP8 reference buffer for the set/copy
// controls on [VP8Encoder] and [VP8Decoder]. Unlike [ReferenceFlags], it
// is not a bit mask: it must be exactly one of [ReferenceLast],
// [ReferenceGolden], or [ReferenceAltRef].
type ReferenceFrame ReferenceFlags

const (
	// ReferenceLast selects the LAST reference buffer.
	ReferenceLast ReferenceFrame = ReferenceFrame(ReferenceFlagLast)
	// ReferenceGolden selects the GOLDEN reference buffer.
	ReferenceGolden ReferenceFrame = ReferenceFrame(ReferenceFlagGolden)
	// ReferenceAltRef selects the alternate-reference buffer.
	ReferenceAltRef ReferenceFrame = ReferenceFrame(ReferenceFlagAltRef)
)

// FrameInfo describes the most recently decoded VP8 frame. It is
// returned by [VP8Decoder.LastFrameInfo] and the DecodeInto family.
type FrameInfo struct {
	// Width and Height are the visible output dimensions.
	Width  int
	Height int

	// KeyFrame reports whether the frame is a key frame.
	KeyFrame bool
	// ShowFrame reports whether the packet produced visible output.
	ShowFrame bool
	// Corrupted reports decoder corruption or concealment state.
	Corrupted bool

	// Quantizer is the public 0..63 quantizer mapped from
	// InternalQuantizer.
	Quantizer int
	// InternalQuantizer is the raw VP8 base qindex (libvpx's
	// VPXD_GET_LAST_QUANTIZER).
	InternalQuantizer int
	// RefUpdates reports reference buffers refreshed by the frame.
	RefUpdates ReferenceFlags
	// RefUsed reports reference buffers used by inter prediction in the frame.
	RefUsed ReferenceFlags

	// PTS is the caller-provided presentation timestamp.
	PTS uint64
}

// PeekVP8StreamInfo parses VP8 frame-header metadata without decoding the
// frame. It allocates nothing and is safe to call on every received
// packet. Returns [ErrInvalidData] when the frame tag is malformed.
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
