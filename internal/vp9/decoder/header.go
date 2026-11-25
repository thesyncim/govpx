package decoder

import (
	"errors"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// Header parsing helpers for the VP9 uncompressed frame header.
// Ported from libvpx v1.16.0 vp9/decoder/vp9_decodeframe.c —
// specifically vp9_read_profile, vp9_read_sync_code, vp9_read_frame_size,
// and read_bitdepth_colorspace_sampling.

// ErrInvalidHeader is returned when the uncompressed header fails any
// of the validation steps libvpx surfaces as VPX_CODEC_UNSUP_BITSTREAM:
// bad frame marker, bad sync code, illegal subsampling combination, or
// a reserved bit set to 1.
var ErrInvalidHeader = errors.New("govpx: invalid VP9 uncompressed header")

// ReadProfile reads the 2- or 3-bit profile field. Profiles 0..2 use
// 2 bits; profile 3 uses 3 (the third bit is read only when the first
// two say "3"). Mirrors vp9_read_profile.
func ReadProfile(r *BitReader) common.BitstreamProfile {
	p := r.ReadBit()
	p |= r.ReadBit() << 1
	if p > 2 {
		p += r.ReadBit()
	}
	return common.BitstreamProfile(p)
}

// ReadSyncCode consumes the three VP9 sync code bytes and returns true
// iff they match. Mirrors vp9_read_sync_code — callers convert a false
// return into a VPX_CODEC_UNSUP_BITSTREAM error.
func ReadSyncCode(r *BitReader) bool {
	return r.ReadLiteral(8) == common.VP9SyncCode0 &&
		r.ReadLiteral(8) == common.VP9SyncCode1 &&
		r.ReadLiteral(8) == common.VP9SyncCode2
}

// ReadFrameSize reads the 32 bits encoding (width-1, height-1) and
// returns them as 1-based dimensions. Mirrors vp9_read_frame_size.
func ReadFrameSize(r *BitReader) (width, height uint32) {
	width = r.ReadLiteral(16) + 1
	height = r.ReadLiteral(16) + 1
	return
}

// BitDepth values returned by ReadBitdepthColorspaceSampling. The
// non-highbitdepth build is always 8.
const (
	Bits8  = 8
	Bits10 = 10
	Bits12 = 12
)

// BitdepthColorspaceSampling carries the values parsed by
// read_bitdepth_colorspace_sampling: bit depth, color space, color
// range, and (subsampling_x, subsampling_y).
type BitdepthColorspaceSampling struct {
	BitDepth     uint8
	ColorSpace   common.ColorSpace
	ColorRange   common.ColorRange
	SubsamplingX uint8
	SubsamplingY uint8
}

// ReadBitdepthColorspaceSampling parses the block after the sync code
// in a key frame (and in an intra-only frame on profile > 0). Mirrors
// read_bitdepth_colorspace_sampling.
func ReadBitdepthColorspaceSampling(r *BitReader, profile common.BitstreamProfile) (BitdepthColorspaceSampling, error) {
	var out BitdepthColorspaceSampling
	if profile >= common.Profile2 {
		if r.ReadBit() == 1 {
			out.BitDepth = Bits12
		} else {
			out.BitDepth = Bits10
		}
	} else {
		out.BitDepth = Bits8
	}
	out.ColorSpace = common.ColorSpace(r.ReadLiteral(3))
	if out.ColorSpace != common.CSSRGB {
		out.ColorRange = common.ColorRange(r.ReadBit())
		if profile == common.Profile1 || profile == common.Profile3 {
			out.SubsamplingX = uint8(r.ReadBit())
			out.SubsamplingY = uint8(r.ReadBit())
			if out.SubsamplingX == 1 && out.SubsamplingY == 1 {
				return out, ErrInvalidHeader
			}
			if r.ReadBit() != 0 {
				return out, ErrInvalidHeader
			}
		} else {
			out.SubsamplingX = 1
			out.SubsamplingY = 1
		}
	} else {
		out.ColorRange = common.CRFullRange
		if profile == common.Profile1 || profile == common.Profile3 {
			// 4:4:4 chroma is the only legal sampling for sRGB + profile
			// 1/3.
			out.SubsamplingX = 0
			out.SubsamplingY = 0
			if r.ReadBit() != 0 {
				return out, ErrInvalidHeader
			}
		} else {
			// 4:4:4 not allowed in profile 0 or 2.
			return out, ErrInvalidHeader
		}
	}
	return out, nil
}

// ReadFrameMarker validates the 2-bit VP9 frame marker. Returns nil if
// the bits are 0b10; otherwise ErrInvalidHeader. This is the very
// first call against a fresh BitReader on a new frame.
func ReadFrameMarker(r *BitReader) error {
	if r.ReadLiteral(2) != common.VP9FrameMarker {
		return ErrInvalidHeader
	}
	return nil
}

// UncompressedHeader carries the full state the VP9 uncompressed
// header emits. Mirrors the parser-visible subset of VP9_COMMON that
// read_uncompressed_header in libvpx v1.16.0 vp9/decoder/
// vp9_decodeframe.c populates — no buffer pool, no ref-counted frame
// management, just the wire-format fields needed for byte-parity
// validation and to drive the compressed-header parser.
type UncompressedHeader struct {
	Profile               common.BitstreamProfile
	ShowExistingFrame     bool
	ExistingFrameSlot     uint8 // valid when ShowExistingFrame
	FrameType             common.FrameType
	ShowFrame             bool
	ErrorResilientMode    bool
	IntraOnly             bool
	ResetFrameContext     uint8 // 0..3
	RefreshFrameFlags     uint8 // 8 bits, one per ring slot
	BitDepthColor         BitdepthColorspaceSampling
	Width                 uint32
	Height                uint32
	Render                RenderSize
	InterRef              InterRefBlock
	AllowHighPrecisionMv  bool
	InterpFilter          InterpFilter
	RefreshFrameContext   bool
	FrameParallelDecoding bool
	FrameContextIdx       uint8

	Loopfilter LoopfilterParams
	Quant      QuantizationParams
	Seg        SegmentationParams
	Tile       TileInfo

	// FirstPartitionSize is the size in bytes of the compressed header
	// that follows the uncompressed header. Read as a trailing 16-bit
	// literal.
	FirstPartitionSize uint16
}

// ReadUncompressedHeader drives the VP9 uncompressed header parse end
// to end. Caller passes a fresh BitReader sitting at the start of the
// frame, the previous frame's parser state (used as the seed for
// fields the wire format preserves when their update bit is 0, unless
// the new frame triggers vp9_setup_past_independence), and a refDims function
// returning (width, height) of a ring-slot reference frame; refDims
// is called for the three inter refs after the ref-index block is
// read.
//
// Returns ErrInvalidHeader on the malformed-frame branches libvpx
// surfaces as VPX_CODEC_UNSUP_BITSTREAM. Buffer allocation,
// ref-counted frame management, and the compressed-header parse all
// live above this layer.
func ReadUncompressedHeader(r *BitReader, prev *UncompressedHeader,
	refDims func(slot uint8) (uint32, uint32),
) (UncompressedHeader, error) {
	var h UncompressedHeader
	if prev != nil {
		// Carry parser-preserved state forward.
		h.BitDepthColor = prev.BitDepthColor
		h.Loopfilter = prev.Loopfilter
		h.Seg = prev.Seg
		// Tile info and frame size are also re-read every frame, but
		// the wire format never preserves them — they always emit.
	}

	if err := ReadFrameMarker(r); err != nil {
		return h, err
	}

	h.Profile = ReadProfile(r)
	if h.Profile >= common.MaxProfiles {
		return h, ErrInvalidHeader
	}

	h.ShowExistingFrame = r.ReadBit() != 0
	if h.ShowExistingFrame {
		h.ExistingFrameSlot = uint8(r.ReadLiteral(3))
		return h, nil
	}

	h.FrameType = common.FrameType(r.ReadBit())
	h.ShowFrame = r.ReadBit() != 0
	h.ErrorResilientMode = r.ReadBit() != 0

	switch {
	case h.FrameType == common.KeyFrame:
		if !ReadSyncCode(r) {
			return h, ErrInvalidHeader
		}
		bd, err := ReadBitdepthColorspaceSampling(r, h.Profile)
		if err != nil {
			return h, err
		}
		h.BitDepthColor = bd
		h.RefreshFrameFlags = 0xff
		h.Width, h.Height = ReadFrameSize(r)
		h.Render = ReadRenderSize(r, h.Width, h.Height)

	default:
		if h.ShowFrame {
			h.IntraOnly = false
		} else {
			h.IntraOnly = r.ReadBit() != 0
		}
		if !h.ErrorResilientMode {
			h.ResetFrameContext = uint8(r.ReadLiteral(2))
		}
		if h.IntraOnly {
			if !ReadSyncCode(r) {
				return h, ErrInvalidHeader
			}
			if h.Profile > common.Profile0 {
				bd, err := ReadBitdepthColorspaceSampling(r, h.Profile)
				if err != nil {
					return h, err
				}
				h.BitDepthColor = bd
			} else {
				h.BitDepthColor = BitdepthColorspaceSampling{
					BitDepth:     Bits8,
					ColorSpace:   common.CSBT601,
					ColorRange:   common.CRStudioRange,
					SubsamplingX: 1,
					SubsamplingY: 1,
				}
			}
			h.RefreshFrameFlags = uint8(r.ReadLiteral(common.RefFrames))
			h.Width, h.Height = ReadFrameSize(r)
			h.Render = ReadRenderSize(r, h.Width, h.Height)
		} else {
			h.RefreshFrameFlags = uint8(r.ReadLiteral(common.RefFrames))
			h.InterRef = ReadInterRefBlock(r)
			var refWidths, refHeights [3]uint32
			if refDims != nil {
				for i, slot := range h.InterRef.RefIndex {
					refWidths[i], refHeights[i] = refDims(slot)
				}
			}
			fs := ReadFrameSizeWithRefs(r, refWidths, refHeights)
			h.Width, h.Height = fs.Width, fs.Height
			h.Render = fs.Render
			h.AllowHighPrecisionMv = r.ReadBit() != 0
			h.InterpFilter = ReadInterpFilter(r)
		}
	}

	if !h.ErrorResilientMode {
		h.RefreshFrameContext = r.ReadBit() != 0
		h.FrameParallelDecoding = r.ReadBit() != 0
	} else {
		h.RefreshFrameContext = false
		h.FrameParallelDecoding = true
	}
	h.FrameContextIdx = uint8(r.ReadLiteral(common.FrameContextsLog2))

	if prev == nil || h.FrameType == common.KeyFrame || h.IntraOnly ||
		h.ErrorResilientMode {
		SetupPastIndependence(&h)
	}

	ReadLoopfilter(r, &h.Loopfilter)
	ReadQuantization(r, &h.Quant)
	ReadSegmentation(r, &h.Seg)

	miCols := alignPowerOfTwo(int(h.Width), common.MiSizeLog2) >> common.MiSizeLog2
	if err := ReadTileInfo(r, miCols, &h.Tile); err != nil {
		return h, err
	}

	h.FirstPartitionSize = uint16(r.ReadLiteral(16))
	return h, nil
}

// SetupPastIndependence mirrors the header-state portion of libvpx
// vp9_setup_past_independence. Reset-style frames must not inherit
// loop-filter deltas or segment features from earlier frames; the following
// loopfilter / segmentation parser then applies the current header's update
// bits on top of these defaults.
func SetupPastIndependence(h *UncompressedHeader) {
	if h == nil {
		return
	}
	ResetSegmentationFeatures(&h.Seg)
	ResetLoopfilterDeltas(&h.Loopfilter)
}

// HeaderRenderSize returns the render dimensions signalled by the
// uncompressed header, falling back to coded dimensions when no render size was
// present.
func HeaderRenderSize(hdr *UncompressedHeader) (int, int) {
	if hdr == nil {
		return 0, 0
	}
	if hdr.Render.Width > 0 && hdr.Render.Height > 0 {
		return int(hdr.Render.Width), int(hdr.Render.Height)
	}
	return int(hdr.Width), int(hdr.Height)
}

// SupportedOutputFormat reports whether hdr describes an output format that
// the public decoder can currently publish (Profile 0 8-bit 4:2:0,
// Profile 1 8-bit 4:2:2 / 4:4:0 / 4:4:4 / RGB, Profile 2 10/12-bit 4:2:0,
// and Profile 3 10/12-bit 4:2:2 / 4:4:0 / 4:4:4 / RGB).
func SupportedOutputFormat(hdr *UncompressedHeader) bool {
	switch hdr.Profile {
	case common.Profile0:
		return hdr.BitDepthColor.BitDepth == Bits8 &&
			hdr.BitDepthColor.SubsamplingX == 1 && hdr.BitDepthColor.SubsamplingY == 1
	case common.Profile1:
		return hdr.BitDepthColor.BitDepth == Bits8 &&
			hdr.BitDepthColor.SubsamplingX <= 1 && hdr.BitDepthColor.SubsamplingY <= 1 &&
			!(hdr.BitDepthColor.SubsamplingX == 1 && hdr.BitDepthColor.SubsamplingY == 1)
	case common.Profile2:
		return (hdr.BitDepthColor.BitDepth == Bits10 || hdr.BitDepthColor.BitDepth == Bits12) &&
			hdr.BitDepthColor.SubsamplingX == 1 && hdr.BitDepthColor.SubsamplingY == 1
	case common.Profile3:
		return (hdr.BitDepthColor.BitDepth == Bits10 || hdr.BitDepthColor.BitDepth == Bits12) &&
			hdr.BitDepthColor.SubsamplingX <= 1 && hdr.BitDepthColor.SubsamplingY <= 1 &&
			!(hdr.BitDepthColor.SubsamplingX == 1 && hdr.BitDepthColor.SubsamplingY == 1)
	default:
		return false
	}
}

// FrameRefSignBias converts the uncompressed-header reference sign-bias fields
// into the full VP9 reference-frame bias table.
func FrameRefSignBias(hdr *UncompressedHeader) [MaxRefFrames]uint8 {
	var signBias [MaxRefFrames]uint8
	for i := range common.RefsPerFrame {
		signBias[LastFrame+i] = hdr.InterRef.SignBias[i]
	}
	return signBias
}

// CompoundReferenceAllowedForHeader reports whether compound prediction is
// allowed for this uncompressed header.
func CompoundReferenceAllowedForHeader(hdr *UncompressedHeader) bool {
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		return false
	}
	return CompoundReferenceAllowed(FrameRefSignBias(hdr))
}

// HeaderResetsPastIndependence reports whether this header resets state that
// depends on prior frames.
func HeaderResetsPastIndependence(hdr *UncompressedHeader) bool {
	return hdr != nil && (hdr.FrameType == common.KeyFrame ||
		hdr.IntraOnly || hdr.ErrorResilientMode)
}

// PartitionContextUpdateWidth returns the context update width used by libvpx
// for a partition whose half-size is measured in 8x8 mode-info units.
func PartitionContextUpdateWidth(halfBlock8x8 int) int {
	width := 2 * halfBlock8x8
	if width == 0 {
		return 1
	}
	return width
}

// PlaneEntropyLen returns the entropy-context length for miCount mode-info
// units at the plane subsampling factor.
func PlaneEntropyLen(miCount int, subsampling uint8) int {
	return (miCount * 2) >> subsampling
}

// TileOffset returns the tile boundary offset in mode-info units.
func TileOffset(idx, mis, log2 int) int {
	sbCols := common.AlignToSB(mis) >> common.MiBlockSizeLog2
	offset := ((idx * sbCols) >> uint(log2)) << common.MiBlockSizeLog2
	return min(offset, mis)
}

// Loopfilter / quantization / tile-info parsers from the VP9
// uncompressed header. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c — setup_loopfilter, setup_quantization,
// setup_tile_info.

// MaxRefLfDeltas / MaxModeLfDeltas mirror libvpx's MAX_REF_LF_DELTAS
// and MAX_MODE_LF_DELTAS — four ref deltas and two mode deltas per
// frame.
const (
	MaxRefLfDeltas  = 4
	MaxModeLfDeltas = 2
)

// LoopfilterParams holds the loop filter knobs the uncompressed header
// emits. Mirrors libvpx's struct loopfilter (just the parser-visible
// fields).
type LoopfilterParams struct {
	FilterLevel         uint8
	SharpnessLevel      uint8
	ModeRefDeltaEnabled bool
	ModeRefDeltaUpdate  bool
	RefDeltas           [MaxRefLfDeltas]int8
	ModeDeltas          [MaxModeLfDeltas]int8
}

// ResetLoopfilterDeltas mirrors libvpx's set_default_lf_deltas. It is part of
// vp9_setup_past_independence: reset-style frames start from these deltas, and
// setup_loopfilter only overwrites them when mode_ref_delta_update is set.
func ResetLoopfilterDeltas(lf *LoopfilterParams) {
	if lf == nil {
		return
	}
	lf.ModeRefDeltaEnabled = true
	lf.ModeRefDeltaUpdate = true
	lf.RefDeltas = [MaxRefLfDeltas]int8{1, 0, -1, -1}
	lf.ModeDeltas = [MaxModeLfDeltas]int8{0, 0}
}

// ReadLoopfilter parses the 6-bit filter level + 3-bit sharpness +
// optional mode/ref delta block. Mirrors setup_loopfilter exactly:
// when the delta-update bit is 0 the existing deltas are preserved, so
// the caller passes the previous frame's state in `lf` and this
// function only overwrites entries with their per-slot enable bit set.
func ReadLoopfilter(r *BitReader, lf *LoopfilterParams) {
	lf.FilterLevel = uint8(r.ReadLiteral(6))
	lf.SharpnessLevel = uint8(r.ReadLiteral(3))
	lf.ModeRefDeltaUpdate = false
	lf.ModeRefDeltaEnabled = r.ReadBit() != 0
	if !lf.ModeRefDeltaEnabled {
		return
	}
	lf.ModeRefDeltaUpdate = r.ReadBit() != 0
	if !lf.ModeRefDeltaUpdate {
		return
	}
	for i := range MaxRefLfDeltas {
		if r.ReadBit() != 0 {
			lf.RefDeltas[i] = int8(r.ReadSignedLiteral(6))
		}
	}
	for i := range MaxModeLfDeltas {
		if r.ReadBit() != 0 {
			lf.ModeDeltas[i] = int8(r.ReadSignedLiteral(6))
		}
	}
}

// QindexBits is the wire-format width of the base qindex.
const QindexBits = 8

// QuantizationParams holds the frame-level quant knobs.
type QuantizationParams struct {
	BaseQindex int16
	YDcDeltaQ  int8
	UvDcDeltaQ int8
	UvAcDeltaQ int8
	Lossless   bool
}

func readDeltaQ(r *BitReader) int8 {
	if r.ReadBit() != 0 {
		return int8(r.ReadSignedLiteral(4))
	}
	return 0
}

// ReadQuantization mirrors setup_quantization — reads the 8-bit
// base_qindex plus the three optional 4-bit signed deltas and derives
// the lossless flag.
func ReadQuantization(r *BitReader, q *QuantizationParams) {
	q.BaseQindex = int16(r.ReadLiteral(QindexBits))
	q.YDcDeltaQ = readDeltaQ(r)
	q.UvDcDeltaQ = readDeltaQ(r)
	q.UvAcDeltaQ = readDeltaQ(r)
	q.Lossless = q.BaseQindex == 0 && q.YDcDeltaQ == 0 && q.UvDcDeltaQ == 0 && q.UvAcDeltaQ == 0
}

// TileInfo holds the tile-grid sizing derived from the frame's mi
// dimensions and the trailing tile_cols / tile_rows bits.
type TileInfo struct {
	Log2TileCols int
	Log2TileRows int
}

// ReadTileInfo mirrors setup_tile_info. It needs the frame's mi_cols
// to pick the valid (min, max) tile column range. Returns ErrInvalid
// Header if tile_cols exceeds 6 (the VP9 spec limit).
func ReadTileInfo(r *BitReader, miCols int, tile *TileInfo) error {
	minLog2, maxLog2 := TileNBits(miCols)

	tile.Log2TileCols = minLog2
	for n := maxLog2 - minLog2; n > 0; n-- {
		if r.ReadBit() == 0 {
			break
		}
		tile.Log2TileCols++
	}
	if tile.Log2TileCols > 6 {
		return ErrInvalidHeader
	}

	tile.Log2TileRows = int(r.ReadBit())
	if tile.Log2TileRows != 0 {
		tile.Log2TileRows += int(r.ReadBit())
	}
	return nil
}

// TileNBits mirrors vp9_get_tile_n_bits. Returns (min, max) for the
// log2_tile_cols range based on the frame's mode-info column count.
func TileNBits(miCols int) (minLog2, maxLog2 int) {
	const (
		miBlockSizeLog2 = common.MiBlockSizeLog2
		minTileWidthB64 = 4
		maxTileWidthB64 = 64
	)
	sb64Cols := alignPowerOfTwo(miCols, miBlockSizeLog2) >> miBlockSizeLog2

	for (maxTileWidthB64 << minLog2) < sb64Cols {
		minLog2++
	}
	maxLog2 = 1
	for (sb64Cols >> maxLog2) >= minTileWidthB64 {
		maxLog2++
	}
	maxLog2--
	return
}

func alignPowerOfTwo(value, n int) int {
	return (value + (1 << uint(n)) - 1) &^ ((1 << uint(n)) - 1)
}

// VP9 segmentation parser. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c:setup_segmentation.

// Segmentation constants from vp9/common/vp9_seg_common.h.
const (
	MaxSegments     = 8
	SegTreeProbs    = MaxSegments - 1
	PredictionProbs = 3
	SegLvlMax       = 4
	MaxProb         = 255
)

// SegLvl* identify each per-segment feature slot. Matches
// SEG_LVL_FEATURES in libvpx.
const (
	SegLvlAltQ     = 0
	SegLvlAltLf    = 1
	SegLvlRefFrame = 2
	SegLvlSkip     = 3
)

// Maximum data magnitude per feature. Mirrors seg_feature_data_max in
// vp9/common/vp9_seg_common.c. MAXQ = 255, MAX_LOOP_FILTER = 63.
var segFeatureDataMax = [SegLvlMax]int{255, 63, 3, 0}

// Whether the per-segment data is signed. Mirrors
// seg_feature_data_signed in libvpx.
var segFeatureDataSigned = [SegLvlMax]bool{true, true, false, false}

// SegmentationParams holds the parsed segmentation state.
type SegmentationParams struct {
	Enabled        bool
	UpdateMap      bool
	UpdateData     bool
	AbsDelta       bool
	TemporalUpdate bool

	TreeProbs   [SegTreeProbs]uint8
	PredProbs   [PredictionProbs]uint8
	FeatureMask [MaxSegments]uint32
	FeatureData [MaxSegments][SegLvlMax]int16
}

// ResetSegmentationFeatures mirrors the segmentation part of libvpx's
// vp9_setup_past_independence. It clears every feature and restores delta-data
// semantics, while leaving the next header free to set Enabled/UpdateMap/Data.
func ResetSegmentationFeatures(seg *SegmentationParams) {
	if seg == nil {
		return
	}
	*seg = SegmentationParams{}
}

// ReadSegmentation mirrors setup_segmentation. Caller passes an
// existing SegmentationParams; the parser preserves previous-frame
// state when the corresponding update flag is 0. The non-update path
// resets UpdateMap / UpdateData to false.
func ReadSegmentation(r *BitReader, seg *SegmentationParams) {
	seg.UpdateMap = false
	seg.UpdateData = false

	seg.Enabled = r.ReadBit() != 0
	if !seg.Enabled {
		return
	}

	// Map update.
	seg.UpdateMap = r.ReadBit() != 0
	if seg.UpdateMap {
		for i := range SegTreeProbs {
			if r.ReadBit() != 0 {
				seg.TreeProbs[i] = uint8(r.ReadLiteral(8))
			} else {
				seg.TreeProbs[i] = MaxProb
			}
		}
		seg.TemporalUpdate = r.ReadBit() != 0
		if seg.TemporalUpdate {
			for i := range PredictionProbs {
				if r.ReadBit() != 0 {
					seg.PredProbs[i] = uint8(r.ReadLiteral(8))
				} else {
					seg.PredProbs[i] = MaxProb
				}
			}
		} else {
			for i := range PredictionProbs {
				seg.PredProbs[i] = MaxProb
			}
		}
	}

	// Data update.
	seg.UpdateData = r.ReadBit() != 0
	if !seg.UpdateData {
		return
	}
	seg.AbsDelta = r.ReadBit() != 0

	// Reset feature mask + data — mirrors vp9_clearall_segfeatures.
	for i := range seg.FeatureMask {
		seg.FeatureMask[i] = 0
	}
	for i := range seg.FeatureData {
		for j := range seg.FeatureData[i] {
			seg.FeatureData[i][j] = 0
		}
	}

	for i := range MaxSegments {
		for j := range SegLvlMax {
			var data int
			if r.ReadBit() != 0 {
				seg.FeatureMask[i] |= 1 << uint(j)
				data = decodeUnsignedMax(r, segFeatureDataMax[j])
				if segFeatureDataSigned[j] {
					if r.ReadBit() != 0 {
						data = -data
					}
				}
			}
			seg.FeatureData[i][j] = int16(data)
		}
	}
}

// decodeUnsignedMax mirrors the static helper in libvpx — read
// get_unsigned_bits(max) bits, then saturate at max.
func decodeUnsignedMax(r *BitReader, max int) int {
	data := int(r.ReadLiteral(getUnsignedBits(max)))
	if data > max {
		return max
	}
	return data
}

// getUnsignedBits returns the number of bits needed to encode any
// value in [0, n]. Mirrors get_unsigned_bits in vp9_common.h.
func getUnsignedBits(n int) int {
	if n <= 0 {
		return 0
	}
	bits := 0
	for v := uint(n); v > 0; v >>= 1 {
		bits++
	}
	return bits
}

// VP9 frame-size + interpolation-filter parsing helpers used by the
// uncompressed header. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c — read_interp_filter, setup_render_size,
// the frame-size-with-refs reference picker, and the inter-frame ref
// frame index block.

// InterpFilter mirrors the INTERP_FILTER enum in vp9/common/vp9_filter.h.
type InterpFilter uint8

const (
	InterpEighttap       InterpFilter = 0
	InterpEighttapSmooth InterpFilter = 1
	InterpEighttapSharp  InterpFilter = 2
	InterpBilinear       InterpFilter = 3
	InterpSwitchable     InterpFilter = 4
)

// literalToFilter maps the 2-bit literal in read_interp_filter to the
// matching INTERP_FILTER value. Order is fixed by the wire format.
var literalToFilter = [4]InterpFilter{
	InterpEighttapSmooth, InterpEighttap, InterpEighttapSharp, InterpBilinear,
}

// ReadInterpFilter mirrors read_interp_filter. A leading 1-bit selects
// SWITCHABLE (per-block); otherwise a 2-bit literal selects one of the
// four banks.
func ReadInterpFilter(r *BitReader) InterpFilter {
	if r.ReadBit() != 0 {
		return InterpSwitchable
	}
	return literalToFilter[r.ReadLiteral(2)]
}

// RenderSize holds the optional display dimensions a key/intra-only
// frame can carry after the coded dimensions.
type RenderSize struct {
	Width, Height uint32
}

// ReadRenderSize mirrors setup_render_size. If the leading bit is 0
// the render dimensions equal the coded (width, height); otherwise a
// fresh 32-bit (w-1, h-1) field follows.
func ReadRenderSize(r *BitReader, codedWidth, codedHeight uint32) RenderSize {
	out := RenderSize{Width: codedWidth, Height: codedHeight}
	if r.ReadBit() != 0 {
		out.Width, out.Height = ReadFrameSize(r)
	}
	return out
}

// FrameSizeWithRefs carries the state setup_frame_size_with_refs
// emits — either an inherited size from one of the three reference
// frames (Found = true with refer index FromRef) or a freshly read
// (Width, Height) pair (Found = false).
type FrameSizeWithRefs struct {
	Found   bool
	FromRef int
	Width   uint32
	Height  uint32
	Render  RenderSize
}

// ReadFrameSizeWithRefs mirrors the parser shape of
// setup_frame_size_with_refs without doing any buffer allocation. For
// each of the three reference slots a 1-bit flag says "inherit this
// ref's size"; the first set bit wins. If none is set, an explicit
// 32-bit (w-1, h-1) field is read. Then the render size follows.
//
// refWidths / refHeights carry the dimensions of the three references
// in the order LAST / GOLDEN / ALTREF. Callers wire this from the
// frame buffer pool.
func ReadFrameSizeWithRefs(r *BitReader, refWidths, refHeights [3]uint32) FrameSizeWithRefs {
	var out FrameSizeWithRefs
	// libvpx breaks out of the per-ref loop on the first set "found"
	// bit; the encoder mirrors this by emitting only one bit when a
	// match is found. Walking all three slots would consume bits the
	// encoder never wrote, mis-aligning every downstream field.
	for i := range 3 {
		if r.ReadBit() != 0 {
			out.Found = true
			out.FromRef = i
			out.Width = refWidths[i]
			out.Height = refHeights[i]
			break
		}
	}
	if !out.Found {
		out.Width, out.Height = ReadFrameSize(r)
	}
	out.Render = ReadRenderSize(r, out.Width, out.Height)
	return out
}

// InterRefBlock carries the per-reference-frame state read for an
// inter frame: each of REFS_PER_FRAME slots picks a ring slot via 3
// bits and adds a sign-bias bit.
type InterRefBlock struct {
	RefIndex [3]uint8 // 0..7 index into the 8-slot ring
	SignBias [3]uint8 // 0 or 1
}

// ReadInterRefBlock mirrors the per-reference fragment of
// read_uncompressed_header's non-key, non-intra-only path: for each of
// 3 inter refs, read REF_FRAMES_LOG2 (= 3) bits for the ring slot and
// one bit for the sign bias.
func ReadInterRefBlock(r *BitReader) InterRefBlock {
	var out InterRefBlock
	for i := range 3 {
		out.RefIndex[i] = uint8(r.ReadLiteral(3))
		out.SignBias[i] = uint8(r.ReadBit())
	}
	return out
}
