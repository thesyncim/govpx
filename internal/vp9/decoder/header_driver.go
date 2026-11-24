package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

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
