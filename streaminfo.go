package govpx

import (
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp9/common"
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

	Superframe       bool
	SuperframeFrames int
}

// PeekVP9StreamInfo parses VP9 uncompressed-header metadata without decoding
// the frame. For superframes it reports the first contained frame and sets the
// Superframe fields. Returns [ErrInvalidVP9Data] for malformed packets and
// [ErrVP9NotImplemented] for non-profile0 packets.
func PeekVP9StreamInfo(packet []byte) (VP9StreamInfo, error) {
	sf, err := vp9ParseSuperframe(packet)
	if err != nil {
		return VP9StreamInfo{}, err
	}
	frame := packet
	info := VP9StreamInfo{SuperframeFrames: 1}
	if sf.count != 0 {
		frame = sf.frames[0]
		info.Superframe = true
		info.SuperframeFrames = sf.count
	}
	if err := peekVP9FrameStreamInfo(frame, &info); err != nil {
		return VP9StreamInfo{}, err
	}
	return info, nil
}

func peekVP9FrameStreamInfo(packet []byte, info *VP9StreamInfo) error {
	if len(packet) == 0 {
		return ErrInvalidVP9Data
	}
	var r vp9dec.BitReader
	r.Init(packet)
	if err := vp9dec.ReadFrameMarker(&r); err != nil {
		return ErrInvalidVP9Data
	}
	profile := vp9dec.ReadProfile(&r)
	if profile >= common.MaxProfiles {
		return ErrInvalidVP9Data
	}
	if profile != common.Profile0 {
		return ErrVP9NotImplemented
	}
	info.Profile = int(profile)

	info.ShowExistingFrame = r.ReadBit() != 0
	if info.ShowExistingFrame {
		info.ExistingFrameSlot = uint8(r.ReadLiteral(3))
		info.ShowFrame = true
		return vp9PeekCheck(&r)
	}

	frameType := common.FrameType(r.ReadBit())
	info.KeyFrame = frameType == common.KeyFrame
	info.ShowFrame = r.ReadBit() != 0
	info.ErrorResilient = r.ReadBit() != 0

	if info.KeyFrame {
		if !vp9dec.ReadSyncCode(&r) {
			return ErrInvalidVP9Data
		}
		if _, err := vp9dec.ReadBitdepthColorspaceSampling(&r, profile); err != nil {
			return ErrInvalidVP9Data
		}
		width, height := vp9dec.ReadFrameSize(&r)
		info.Width, info.Height = int(width), int(height)
		_ = vp9dec.ReadRenderSize(&r, width, height)
		info.RefreshFrameFlags = 0xff
		return finishVP9StreamInfoPeek(&r, info)
	}

	if !info.ShowFrame {
		info.IntraOnly = r.ReadBit() != 0
	}
	if !info.ErrorResilient {
		_ = r.ReadLiteral(2) // reset_frame_context
	}
	if info.IntraOnly {
		if !vp9dec.ReadSyncCode(&r) {
			return ErrInvalidVP9Data
		}
		info.RefreshFrameFlags = uint8(r.ReadLiteral(common.RefFrames))
		width, height := vp9dec.ReadFrameSize(&r)
		info.Width, info.Height = int(width), int(height)
		_ = vp9dec.ReadRenderSize(&r, width, height)
		return finishVP9StreamInfoPeek(&r, info)
	}

	info.RefreshFrameFlags = uint8(r.ReadLiteral(common.RefFrames))
	_ = vp9dec.ReadInterRefBlock(&r)
	for i := range 3 {
		if r.ReadBit() != 0 {
			info.FrameSizeFromReference = true
			info.FrameSizeReference = i
			_ = vp9dec.ReadRenderSize(&r, 0, 0)
			_ = r.ReadBit() // allow_high_precision_mv
			_ = vp9dec.ReadInterpFilter(&r)
			return finishVP9StreamInfoPeek(&r, info)
		}
	}
	width, height := vp9dec.ReadFrameSize(&r)
	info.Width, info.Height = int(width), int(height)
	_ = vp9dec.ReadRenderSize(&r, width, height)
	_ = r.ReadBit() // allow_high_precision_mv
	_ = vp9dec.ReadInterpFilter(&r)
	return finishVP9StreamInfoPeek(&r, info)
}

func finishVP9StreamInfoPeek(r *vp9dec.BitReader, info *VP9StreamInfo) error {
	if !info.ErrorResilient {
		_ = r.ReadBit() // refresh_frame_context
		_ = r.ReadBit() // frame_parallel_decoding
	}
	_ = r.ReadLiteral(common.FrameContextsLog2)

	var lf vp9dec.LoopfilterParams
	vp9dec.ReadLoopfilter(r, &lf)
	var q vp9dec.QuantizationParams
	vp9dec.ReadQuantization(r, &q)
	info.Quantizer = int(q.BaseQindex)
	var seg vp9dec.SegmentationParams
	vp9dec.ReadSegmentation(r, &seg)

	if info.Width == 0 {
		return vp9PeekCheck(r)
	}
	miCols := (info.Width + common.MiSize - 1) >> common.MiSizeLog2
	var tile vp9dec.TileInfo
	if err := vp9dec.ReadTileInfo(r, miCols, &tile); err != nil {
		return ErrInvalidVP9Data
	}
	info.FirstPartitionSize = int(r.ReadLiteral(16))
	return vp9PeekCheck(r)
}

func vp9PeekCheck(r *vp9dec.BitReader) error {
	if r.HasError() {
		return ErrInvalidVP9Data
	}
	return nil
}
