package decoder

import (
	vp9bits "github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

// Stream-info peeking follows the uncompressed-header order in libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c without reconstructing tile data.

// StreamInfo describes parser-visible VP9 uncompressed-header metadata that
// can be read without reconstructing the frame.
type StreamInfo struct {
	// Width and Height are the coded dimensions when carried explicitly in the
	// packet. Inter frames may inherit dimensions from a reference; in that
	// case FrameSizeFromReference is true and Width / Height are zero.
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

	TileInfoAvailable bool
	TileLog2Cols      int
	TileLog2Rows      int

	Superframe       bool
	SuperframeFrames int
}

// PeekStreamInfo parses VP9 uncompressed-header metadata without decoding the
// frame. For superframes it reports the first contained frame and sets the
// Superframe fields.
func PeekStreamInfo(packet []byte) (StreamInfo, error) {
	sf, err := vp9bits.ParseSuperframe(packet)
	if err != nil {
		return StreamInfo{}, err
	}
	frame := packet
	info := StreamInfo{SuperframeFrames: 1}
	if sf.Count != 0 {
		frame = sf.Frames[0]
		info.Superframe = true
		info.SuperframeFrames = sf.Count
	}
	if err := peekFrameStreamInfo(frame, &info); err != nil {
		return StreamInfo{}, err
	}
	return info, nil
}

func peekFrameStreamInfo(packet []byte, info *StreamInfo) error {
	if len(packet) == 0 {
		return vpxerrors.ErrInvalidVP9Data
	}
	var r BitReader
	r.Init(packet)
	if err := ReadFrameMarker(&r); err != nil {
		return vpxerrors.ErrInvalidVP9Data
	}
	profile := ReadProfile(&r)
	if profile >= common.MaxProfiles {
		return vpxerrors.ErrInvalidVP9Data
	}
	if profile != common.Profile0 {
		return vpxerrors.ErrVP9NotImplemented
	}
	info.Profile = int(profile)

	info.ShowExistingFrame = r.ReadBit() != 0
	if info.ShowExistingFrame {
		info.ExistingFrameSlot = uint8(r.ReadLiteral(3))
		info.ShowFrame = true
		return peekCheck(&r)
	}

	frameType := common.FrameType(r.ReadBit())
	info.KeyFrame = frameType == common.KeyFrame
	info.ShowFrame = r.ReadBit() != 0
	info.ErrorResilient = r.ReadBit() != 0

	if info.KeyFrame {
		if !ReadSyncCode(&r) {
			return vpxerrors.ErrInvalidVP9Data
		}
		if _, err := ReadBitdepthColorspaceSampling(&r, profile); err != nil {
			return vpxerrors.ErrInvalidVP9Data
		}
		width, height := ReadFrameSize(&r)
		info.Width, info.Height = int(width), int(height)
		_ = ReadRenderSize(&r, width, height)
		info.RefreshFrameFlags = 0xff
		return finishStreamInfoPeek(&r, info)
	}

	if !info.ShowFrame {
		info.IntraOnly = r.ReadBit() != 0
	}
	if !info.ErrorResilient {
		_ = r.ReadLiteral(2) // reset_frame_context
	}
	if info.IntraOnly {
		if !ReadSyncCode(&r) {
			return vpxerrors.ErrInvalidVP9Data
		}
		info.RefreshFrameFlags = uint8(r.ReadLiteral(common.RefFrames))
		width, height := ReadFrameSize(&r)
		info.Width, info.Height = int(width), int(height)
		_ = ReadRenderSize(&r, width, height)
		return finishStreamInfoPeek(&r, info)
	}

	info.RefreshFrameFlags = uint8(r.ReadLiteral(common.RefFrames))
	_ = ReadInterRefBlock(&r)
	for i := range 3 {
		if r.ReadBit() != 0 {
			info.FrameSizeFromReference = true
			info.FrameSizeReference = i
			_ = ReadRenderSize(&r, 0, 0)
			_ = r.ReadBit() // allow_high_precision_mv
			_ = ReadInterpFilter(&r)
			return finishStreamInfoPeek(&r, info)
		}
	}
	width, height := ReadFrameSize(&r)
	info.Width, info.Height = int(width), int(height)
	_ = ReadRenderSize(&r, width, height)
	_ = r.ReadBit() // allow_high_precision_mv
	_ = ReadInterpFilter(&r)
	return finishStreamInfoPeek(&r, info)
}

func finishStreamInfoPeek(r *BitReader, info *StreamInfo) error {
	if !info.ErrorResilient {
		_ = r.ReadBit() // refresh_frame_context
		_ = r.ReadBit() // frame_parallel_decoding
	}
	_ = r.ReadLiteral(common.FrameContextsLog2)

	var lf LoopfilterParams
	ReadLoopfilter(r, &lf)
	var q QuantizationParams
	ReadQuantization(r, &q)
	info.Quantizer = int(q.BaseQindex)
	var seg SegmentationParams
	ReadSegmentation(r, &seg)

	if info.Width == 0 {
		return peekCheck(r)
	}
	miCols := (info.Width + common.MiSize - 1) >> common.MiSizeLog2
	var tile TileInfo
	if err := ReadTileInfo(r, miCols, &tile); err != nil {
		return vpxerrors.ErrInvalidVP9Data
	}
	info.TileInfoAvailable = true
	info.TileLog2Cols = tile.Log2TileCols
	info.TileLog2Rows = tile.Log2TileRows
	info.FirstPartitionSize = int(r.ReadLiteral(16))
	return peekCheck(r)
}

func peekCheck(r *BitReader) error {
	if r.HasError() {
		return vpxerrors.ErrInvalidVP9Data
	}
	return nil
}
