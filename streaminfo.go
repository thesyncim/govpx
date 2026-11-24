package govpx

import (
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
	info, err := vp8dec.PeekStreamInfo(packet)
	return streamInfoFromInternal(info), err
}

func peekVP8FrameHeader(packet []byte) (vp8dec.FrameHeader, StreamInfo, error) {
	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		return vp8dec.FrameHeader{}, StreamInfo{}, ErrInvalidData
	}
	return header, streamInfoFromInternal(vp8dec.StreamInfoFromFrameHeader(header)), nil
}

func streamInfoFromInternal(info vp8dec.StreamInfo) StreamInfo {
	return StreamInfo{
		Width:              info.Width,
		Height:             info.Height,
		Profile:            info.Profile,
		KeyFrame:           info.KeyFrame,
		ShowFrame:          info.ShowFrame,
		FirstPartitionSize: info.FirstPartitionSize,
	}
}

// VP9StreamInfo describes parser-visible VP9 uncompressed-header metadata
// that can be read without reconstructing the frame. It is returned by
// [PeekVP9StreamInfo] for RTP/WebRTC routing and packet diagnostics.
type VP9StreamInfo struct {
	// Width and Height are the coded dimensions when carried explicitly in the
	// packet. Inter frames may inherit dimensions from a reference; in that case
	// FrameSizeFromReference is true and Width / Height are zero.
	Width  int
	Height int
	// Profile is the VP9 bitstream profile. govpx supports profile 0 only.
	Profile int

	KeyFrame          bool
	ShowFrame         bool
	ShowExistingFrame bool
	ExistingFrameSlot uint8
	IntraOnly         bool
	ErrorResilient    bool

	RefreshFrameFlags  uint8
	Quantizer          int
	FirstPartitionSize int

	FrameSizeFromReference bool
	FrameSizeReference     int

	// TileInfoAvailable reports whether TileLog2Cols/TileLog2Rows were present
	// in the packet and could be parsed without reference-frame context. Inter
	// frames whose size is inherited from a reference do not expose tile layout
	// through this stateless peek API.
	TileInfoAvailable bool
	TileLog2Cols      int
	TileLog2Rows      int

	Superframe       bool
	SuperframeFrames int
}

// PeekVP9StreamInfo parses VP9 uncompressed-header metadata without decoding
// the frame. For superframes it reports the first contained frame and sets the
// Superframe fields. Returns [ErrInvalidVP9Data] for malformed packets and
// [ErrVP9NotImplemented] for non-profile0 packets.
func PeekVP9StreamInfo(packet []byte) (VP9StreamInfo, error) {
	info, err := vp9dec.PeekStreamInfo(packet)
	return vp9StreamInfoFromInternal(info), err
}

func vp9StreamInfoFromInternal(info vp9dec.StreamInfo) VP9StreamInfo {
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
