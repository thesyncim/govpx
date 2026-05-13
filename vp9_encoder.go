package govpx

import (
	"errors"
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	vp9EncoderTxCoeffSlots    = 256
	vp9EncoderBlockCoeffSlots = 256 * vp9EncoderTxCoeffSlots
)

// VP9EncoderOptions configures a VP9 profile 0 encoder.
type VP9EncoderOptions struct {
	// Width and Height are the fixed visible dimensions accepted by
	// EncodeInto. Must both be positive.
	Width  int
	Height int

	// FPS sets a 1/FPS timebase when TimebaseNum and TimebaseDen are
	// both zero. Defaults to 30 if all three are unset.
	FPS int

	// TimebaseNum is the numerator of the caller timebase.
	TimebaseNum int
	// TimebaseDen is the denominator of the caller timebase.
	TimebaseDen int

	// Threads is reserved for VP9 encode paths that can split work by
	// tile. Zero or 1 use the serial path. Negative values return
	// ErrInvalidConfig.
	Threads int

	// TargetBitrateKbps is a non-negative bitrate hint for profile 0 encode
	// configuration. The current packet path does not run rate control.
	TargetBitrateKbps int

	// Quantizer selects a fixed VP9 base qindex in [0, 255]. Zero uses the
	// packet path default.
	Quantizer int

	// MaxKeyframeInterval bounds the gap between key frames. Zero
	// uses libvpx's default (kf_max_dist=128).
	MaxKeyframeInterval int

	// ErrorResilient enables the libvpx error-resilient bit on every
	// frame header.
	ErrorResilient bool
}

// ErrVP9EncoderNotImplemented is retained for callers that already branch on
// this sentinel.
//
// Deprecated: Encode and EncodeInto no longer return this error.
var ErrVP9EncoderNotImplemented = errors.New("govpx: VP9 encoder path unavailable")

// VP9Encoder is the public entry point for VP9 profile 0 stream encoding.
type VP9Encoder struct {
	opts   VP9EncoderOptions
	closed bool

	// frameIndex tracks the frame number for the key-frame cadence
	// gate. Mirrors libvpx's cpi->common.current_video_frame.
	frameIndex int

	// fc carries the per-frame entropy context across frames.
	// Reset on every keyframe via ResetFrameContext.
	fc vp9dec.FrameContext

	// scratch is the reusable compressed-header staging buffer that
	// PackBitstream consults. Sized to 64KB so libvpx's
	// first_partition_size 16-bit cap can never overflow.
	scratch [65536]byte

	// aboveSegCtx / leftSegCtx are the partition-history arrays the
	// per-SB walker stamps. Sized to the frame's mi_cols at first
	// EncodeInto.
	aboveSegCtx []int8
	leftSegCtx  []int8

	// miGrid mirrors the decoder-visible MODE_INFO grid at 8x8 granularity so
	// subsequent block mode-context probabilities see the same above/left
	// state that libvpx's decoder sees.
	miGrid []vp9dec.NeighborMi

	// refWidth / refHeight mirror the encoder-side VP9 reference map so
	// inter headers can emit write_frame_size_with_refs without allocating.
	refWidth  [common.RefFrames]uint32
	refHeight [common.RefFrames]uint32
	refValid  [common.RefFrames]bool

	// planes carries coefficient entropy contexts for source-backed frames.
	planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane

	intraScratch vp9dec.IntraPredictorScratch

	reconFrame Image
	reconYFull []byte
	reconUFull []byte
	reconVFull []byte
	reconY     []byte
	reconU     []byte
	reconV     []byte

	refFrames [common.RefFrames]vp9ReferenceFrame

	blockCoeffs    [vp9dec.MaxMbPlane][vp9EncoderBlockCoeffSlots]int16
	coefScratch    [1024]int16
	residueScratch [256]int16
	txCoeffScratch [256]int16
	dqCoeffScratch [256]int16
	frameCounts    encoder.FrameCounts
}

// NewVP9Encoder creates a VP9 encoder with validated options.
// Width and Height must be positive; Threads / Quantizer /
// TargetBitrateKbps / MaxKeyframeInterval must be non-negative.
func NewVP9Encoder(opts VP9EncoderOptions) (*VP9Encoder, error) {
	if err := validateVP9EncoderOptions(opts); err != nil {
		return nil, err
	}
	return &VP9Encoder{opts: opts}, nil
}

func validateVP9EncoderOptions(opts VP9EncoderOptions) error {
	if opts.Width <= 0 || opts.Height <= 0 {
		return ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return ErrInvalidConfig
	}
	if opts.TargetBitrateKbps < 0 || opts.Quantizer < 0 || opts.MaxKeyframeInterval < 0 {
		return ErrInvalidConfig
	}
	if opts.Quantizer > 255 {
		return ErrInvalidQuantizer
	}
	if opts.FPS < 0 {
		return ErrInvalidConfig
	}
	if (opts.TimebaseNum < 0) || (opts.TimebaseDen < 0) {
		return ErrInvalidConfig
	}
	// Either FPS xor both timebase components must be set, or all
	// three may be zero (defaults to 30 fps in libvpx).
	if (opts.TimebaseNum != 0) != (opts.TimebaseDen != 0) {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP9Encoder) validateVP9EncoderSource(img *image.YCbCr) error {
	if img == nil {
		return ErrInvalidConfig
	}
	if img.Rect.Dx() != e.opts.Width || img.Rect.Dy() != e.opts.Height {
		return ErrInvalidConfig
	}
	if img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return ErrInvalidConfig
	}
	if img.YStride < e.opts.Width || img.CStride < (e.opts.Width+1)/2 {
		return ErrInvalidConfig
	}
	if len(img.Y) < ycbcrPlaneLen(img.YStride, e.opts.Width, e.opts.Height) {
		return ErrInvalidConfig
	}
	uvWidth := (e.opts.Width + 1) / 2
	uvHeight := (e.opts.Height + 1) / 2
	if len(img.Cb) < ycbcrPlaneLen(img.CStride, uvWidth, uvHeight) ||
		len(img.Cr) < ycbcrPlaneLen(img.CStride, uvWidth, uvHeight) {
		return ErrInvalidConfig
	}
	return nil
}

func ycbcrPlaneLen(stride, width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	return (height-1)*stride + width
}

// IsKeyFrameNext reports whether the next call to EncodeInto would
// emit a key frame. The first frame is always a key; subsequent
// frames key on MaxKeyframeInterval boundaries.
func (e *VP9Encoder) IsKeyFrameNext() bool {
	if e == nil || e.closed {
		return false
	}
	if e.frameIndex == 0 {
		return true
	}
	cadence := e.opts.MaxKeyframeInterval
	if cadence <= 0 {
		cadence = 128 // libvpx default kf_max_dist
	}
	return e.frameIndex%cadence == 0
}

// EncodeInto packs the next profile 0 frame into dst. The current packet path
// emits source-backed keyframes with 4x4 residuals and visible LAST/ZeroMv
// inter frames with optional 4x4 residuals. A deterministic prepass walks the
// same mode tree to collect frame counts before the compressed header, so the
// real tile is encoded with same-frame counts-driven probability updates.
//
// Returns the number of bytes written into dst. Caller sizes dst; leave room
// for up to 64 KiB to match libvpx's first-partition header bound.
func (e *VP9Encoder) EncodeInto(img *image.YCbCr, dst []byte) (int, error) {
	if e == nil || e.closed {
		return 0, ErrClosed
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return 0, err
	}
	if len(dst) == 0 {
		return 0, ErrBufferTooSmall
	}

	width := uint32(e.opts.Width)
	height := uint32(e.opts.Height)
	miCols := int((width + 7) >> 3)
	miRows := int((height + 7) >> 3)
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(miRows, miCols)

	isKey := e.IsKeyFrameNext()
	if !isKey && !e.refFrames[0].valid {
		isKey = true
	}
	if isKey {
		vp9dec.ResetFrameContext(&e.fc)
	}
	e.prepareVP9EncoderOutputFrame(int(width), int(height))

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		ShowFrame:             true,
		ErrorResilientMode:    e.opts.ErrorResilient,
		Width:                 width,
		Height:                height,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	// BaseQindex=1 dodges the lossless inference libvpx makes when
	// base_qindex + every delta_q are all zero. Lossless mode forces
	// tx_mode=ONLY_4X4 on the decoder side and skips the tx_mode
	// literal in the compressed header; staying out of lossless keeps
	// the wire layout consistent with the rest of the zero-residue path.
	qindex := e.opts.Quantizer
	if qindex == 0 {
		qindex = 1
	}
	header.Quant.BaseQindex = int16(qindex)
	if isKey {
		header.FrameType = common.KeyFrame
		header.RefreshFrameFlags = 0xff
	} else {
		header.FrameType = common.InterFrame
		header.RefreshFrameFlags = 1
		header.InterRef.RefIndex = [3]uint8{0, 0, 0}
		header.InterRef.SignBias = [3]uint8{0, 0, 0}
	}

	txMode := common.Allow16x16
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx16x16,
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	if !isKey {
		baseMi.Mode = common.ZeroMv
		baseMi.InterpFilter = uint8(vp9dec.InterpEighttap)
		baseMi.RefFrame = [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}
	}
	var seg vp9dec.SegmentationParams // disabled — no map / no data update
	var dq vp9dec.DequantTables
	var keyState *vp9KeyframeEncodeState
	var interState *vp9InterEncodeState
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(header.Quant.BaseQindex),
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	if isKey {
		keyState = &vp9KeyframeEncodeState{
			img: img,
			hdr: &header,
			dq:  &dq,
		}
	} else {
		interState = &vp9InterEncodeState{
			img: img,
			dq:  &dq,
			ref: &e.refFrames[0],
		}
	}
	e.resetVP9EncoderAboveEntropyContexts()

	// libvpx swaps in vp9_kf_partition_probs (not fc->partition_prob)
	// for keyframe / intra-only frames — see set_partition_probs in
	// vp9/common/vp9_onyxc_int.h. The two tables have the same shape
	// but different probabilities, so the bool stream desyncs if the
	// encoder uses the wrong one.
	partitionProbs := tables.KfPartitionProbs
	if !isKey {
		partitionProbs = e.fc.PartitionProb
	}

	counts := e.collectVP9EncodeFrameCounts(int(width), int(height), miRows, miCols,
		&partitionProbs, &seg, baseMi, isKey, header.IntraOnly, keyState, interState)

	compSize, err := encoder.WriteCompressedHeaderFromCounts(e.scratch[:], encoder.WriteCompressedHeaderFromCountsArgs{
		Lossless:           false,
		TxMode:             txMode,
		IntraOnly:          isKey || header.IntraOnly,
		InterpFilter:       vp9dec.InterpEighttap,
		ReferenceMode:      vp9dec.SingleReference,
		CompoundRefAllowed: false,
		CoefStepsize:       4,
		Probs:              &e.fc,
		Counts:             counts,
	})
	if err != nil {
		return 0, err
	}
	if compSize > 0xffff {
		return 0, encoder.ErrCompressedHeaderTooLarge
	}
	header.FirstPartitionSize = uint16(compSize)
	if !isKey {
		partitionProbs = e.fc.PartitionProb
	}

	var headerBW encoder.BitWriter
	headerBW.Init(dst)
	var uncSize int
	if header.FrameType == common.KeyFrame {
		uncSize = encoder.WriteKeyframeUncompressedHeader(&headerBW, &header)
	} else if header.IntraOnly {
		uncSize = encoder.WriteIntraOnlyUncompressedHeader(&headerBW, &header)
	} else {
		uncSize = encoder.WriteInterUncompressedHeader(&headerBW, &header, e.vp9RefDims)
	}
	if uncSize+compSize >= len(dst) {
		return uncSize, encoder.ErrPackBufferFull
	}
	copy(dst[uncSize:uncSize+compSize], e.scratch[:compSize])

	var tileBW bitstream.Writer
	tileStart := uncSize + compSize
	tileBW.Start(dst[tileStart:])
	if isKey {
		e.writeVP9KeyframeSourceModesTile(&tileBW, miRows, miCols,
			&partitionProbs, &seg, baseMi, keyState)
	} else if header.IntraOnly {
		e.writeVP9StubModesTile(&tileBW, miRows, miCols, &partitionProbs, &seg, baseMi)
	} else {
		e.writeVP9InterSourceModesTile(&tileBW, miRows, miCols,
			&partitionProbs, &seg, baseMi, interState)
	}
	tileSize, err := tileBW.Stop()
	if err != nil {
		return tileStart, err
	}
	n := tileStart + tileSize
	e.refreshVP9EncoderRefs(&header)
	e.frameIndex++
	return n, nil
}

func (e *VP9Encoder) vp9RefDims(slot uint8) (uint32, uint32) {
	idx := int(slot)
	if idx < len(e.refValid) && e.refValid[idx] {
		return e.refWidth[idx], e.refHeight[idx]
	}
	return uint32(e.opts.Width), uint32(e.opts.Height)
}

func (e *VP9Encoder) refreshVP9EncoderRefs(header *vp9dec.UncompressedHeader) {
	flags := header.RefreshFrameFlags
	for slot := range e.refValid {
		if flags&(1<<uint(slot)) == 0 {
			continue
		}
		e.refWidth[slot] = header.Width
		e.refHeight[slot] = header.Height
		e.refValid[slot] = true
		if e.reconFrame.Width != 0 && e.reconFrame.Height != 0 {
			e.refFrames[slot].store(e.reconFrame)
		}
	}
}

func (e *VP9Encoder) ensureVP9EncoderModeBuffers(miRows, miCols int) {
	miColsAligned := alignToSb(miCols)
	if cap(e.aboveSegCtx) < miColsAligned {
		e.aboveSegCtx = make([]int8, miColsAligned)
	} else {
		e.aboveSegCtx = e.aboveSegCtx[:miColsAligned]
		for i := range e.aboveSegCtx {
			e.aboveSegCtx[i] = 0
		}
	}
	if cap(e.leftSegCtx) < common.MiBlockSize {
		e.leftSegCtx = make([]int8, common.MiBlockSize)
	} else {
		e.leftSegCtx = e.leftSegCtx[:common.MiBlockSize]
	}
	miGridLen := miRows * miCols
	if cap(e.miGrid) < miGridLen {
		e.miGrid = make([]vp9dec.NeighborMi, miGridLen)
	} else {
		e.miGrid = e.miGrid[:miGridLen]
		for i := range e.miGrid {
			e.miGrid[i] = vp9dec.NeighborMi{}
		}
	}
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		aboveLen := vp9PlaneEntropyLen(miColsAligned, pd.SubsamplingX)
		leftLen := vp9PlaneEntropyLen(common.MiBlockSize, pd.SubsamplingY)
		if cap(pd.AboveContext) < aboveLen {
			pd.AboveContext = make([]uint8, aboveLen)
		} else {
			pd.AboveContext = pd.AboveContext[:aboveLen]
		}
		if cap(pd.LeftContext) < leftLen {
			pd.LeftContext = make([]uint8, leftLen)
		} else {
			pd.LeftContext = pd.LeftContext[:leftLen]
		}
	}
}

func (e *VP9Encoder) resetVP9EncoderAboveEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := e.planes[plane].AboveContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (e *VP9Encoder) resetVP9EncoderLeftEntropyContexts() {
	for plane := range vp9dec.MaxMbPlane {
		ctx := e.planes[plane].LeftContext
		for i := range ctx {
			ctx[i] = 0
		}
	}
}

func (e *VP9Encoder) vp9EncoderPlaneContextOffsets(miRow, miCol int) (
	above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int,
) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
}

func (e *VP9Encoder) prepareVP9EncoderOutputFrame(width, height int) {
	layout := vp9FrameBufferLayout(width, height)
	e.reconYFull = ensureVP9AlignedPlaneCapacity(e.reconYFull, layout.yFullLen)
	e.reconUFull = ensureVP9AlignedPlaneCapacity(e.reconUFull, layout.uvFullLen)
	e.reconVFull = ensureVP9AlignedPlaneCapacity(e.reconVFull, layout.uvFullLen)
	fillVP9Plane(e.reconYFull, 128)
	fillVP9Plane(e.reconUFull, 128)
	fillVP9Plane(e.reconVFull, 128)
	e.reconY = e.reconYFull[layout.yOrigin:]
	e.reconU = e.reconUFull[layout.uvOrigin:]
	e.reconV = e.reconVFull[layout.uvOrigin:]
	e.reconFrame = Image{
		Width:   width,
		Height:  height,
		Y:       e.reconY,
		U:       e.reconU,
		V:       e.reconV,
		YStride: layout.yStride,
		UStride: layout.uvStride,
		VStride: layout.uvStride,
	}
}

func (e *VP9Encoder) resetVP9EncoderCodingState(width, height int) {
	e.prepareVP9EncoderOutputFrame(width, height)
	for i := range e.aboveSegCtx {
		e.aboveSegCtx[i] = 0
	}
	for i := range e.leftSegCtx {
		e.leftSegCtx[i] = 0
	}
	for i := range e.miGrid {
		e.miGrid[i] = vp9dec.NeighborMi{}
	}
	e.resetVP9EncoderAboveEntropyContexts()
	e.resetVP9EncoderLeftEntropyContexts()
}

func (e *VP9Encoder) collectVP9EncodeFrameCounts(width, height, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	isKey, intraOnly bool, key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) *encoder.FrameCounts {
	counts := &e.frameCounts
	*counts = encoder.FrameCounts{}

	var countKey *vp9KeyframeEncodeState
	if key != nil {
		tmp := *key
		tmp.counts = counts
		countKey = &tmp
	}
	var countInter *vp9InterEncodeState
	if inter != nil {
		tmp := *inter
		tmp.counts = counts
		countInter = &tmp
	}

	var bw bitstream.Writer
	bw.Start(e.scratch[:])
	switch {
	case isKey:
		e.writeVP9KeyframeSourceModesTile(&bw, miRows, miCols,
			partitionProbs, seg, baseMi, countKey)
	case intraOnly:
		e.writeVP9StubModesTile(&bw, miRows, miCols, partitionProbs, seg, baseMi)
	default:
		e.writeVP9InterSourceModesTile(&bw, miRows, miCols,
			partitionProbs, seg, baseMi, countInter)
	}

	e.resetVP9EncoderCodingState(width, height)
	return counts
}

func vp9EncodeCountsForState(key *vp9KeyframeEncodeState,
	inter *vp9InterEncodeState,
) *encoder.FrameCounts {
	if key != nil && key.counts != nil {
		return key.counts
	}
	if inter != nil {
		return inter.counts
	}
	return nil
}

func txModeForMi(mi vp9dec.NeighborMi) common.TxMode {
	if mi.TxSize >= common.Tx16x16 {
		return common.Allow16x16
	}
	if mi.TxSize >= common.Tx8x8 {
		return common.Allow8x8
	}
	return common.Only4x4
}

func clampVP9TxSizeForBlock(tx common.TxSize, bsize common.BlockSize) common.TxSize {
	maxTx := common.MaxTxsizeLookup[bsize]
	if tx > maxTx {
		return maxTx
	}
	return tx
}

func countVP9Skip(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, above, left *vp9dec.NeighborMi, skip uint8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	ctx := vp9dec.GetSkipContext(above, left)
	counts.Skip[ctx][skip]++
}

func countVP9IntraInter(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, isInter int,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	counts.IntraInter[ctx][isInter]++
}

func countVP9SingleRef(counts *encoder.FrameCounts,
	seg *vp9dec.SegmentationParams, segID int,
	above, left *vp9dec.NeighborMi, refFrame int8,
) {
	if counts == nil || vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return
	}
	ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
	bit0 := 0
	if refFrame != vp9dec.LastFrame {
		bit0 = 1
	}
	counts.ReferenceMode.SingleRef[ctx0][0][bit0]++
	if bit0 == 0 {
		return
	}
	ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
	bit1 := 0
	if refFrame != vp9dec.GoldenFrame {
		bit1 = 1
	}
	counts.ReferenceMode.SingleRef[ctx1][1][bit1]++
}

func countVP9InterMode(counts *encoder.FrameCounts, seg *vp9dec.SegmentationParams,
	segID int, bsize common.BlockSize, ctx int, mode common.PredictionMode,
) {
	if counts == nil || bsize < common.Block8x8 ||
		vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlSkip) {
		return
	}
	sub := int(mode) - int(common.NearestMv)
	if sub >= 0 && sub < common.InterModes {
		counts.InterMode[ctx][sub]++
	}
}

func vp9CoefBranchStats(counts *encoder.FrameCounts) *encoder.FrameCoefBranchStats {
	if counts == nil {
		return nil
	}
	return &counts.CoefBranchStats
}

func (e *VP9Encoder) writeVP9StubModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi, vp9ModeTreeKeyframe, nil, nil)
}

func (e *VP9Encoder) writeVP9KeyframeSourceModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	key *vp9KeyframeEncodeState,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi, vp9ModeTreeKeyframeSource, key, nil)
}

func (e *VP9Encoder) writeVP9InterSkipModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi, vp9ModeTreeInterSkip, nil, nil)
}

func (e *VP9Encoder) writeVP9InterSourceModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
	inter *vp9InterEncodeState,
) {
	e.writeVP9ModesTile(bw, miRows, miCols, partitionProbs, seg, baseMi, vp9ModeTreeInterSource, nil, inter)
}

func (e *VP9Encoder) writeVP9StubModesTileBounds(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi,
) {
	e.writeVP9ModesTileBounds(bw, miRows, miCols, tile, partitionProbs, seg, baseMi, vp9ModeTreeKeyframe, nil, nil)
}

type vp9ModeTreeKind uint8

const (
	vp9ModeTreeKeyframe vp9ModeTreeKind = iota
	vp9ModeTreeKeyframeSource
	vp9ModeTreeInterSkip
	vp9ModeTreeInterSource
)

type vp9KeyframeEncodeState struct {
	img    *image.YCbCr
	hdr    *vp9dec.UncompressedHeader
	dq     *vp9dec.DequantTables
	counts *encoder.FrameCounts
}

type vp9InterEncodeState struct {
	img    *image.YCbCr
	dq     *vp9dec.DequantTables
	ref    *vp9ReferenceFrame
	counts *encoder.FrameCounts
}

func (e *VP9Encoder) writeVP9ModesTile(bw *bitstream.Writer, miRows, miCols int,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, kind vp9ModeTreeKind,
	key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	tile := vp9dec.TileBounds{
		MiRowStart: 0,
		MiRowEnd:   miRows,
		MiColStart: 0,
		MiColEnd:   miCols,
	}
	e.writeVP9ModesTileBounds(bw, miRows, miCols, tile, partitionProbs, seg, baseMi, kind, key, inter)
}

func (e *VP9Encoder) writeVP9ModesTileBounds(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, kind vp9ModeTreeKind,
	key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range e.leftSegCtx {
			e.leftSegCtx[i] = 0
		}
		if kind == vp9ModeTreeKeyframeSource || kind == vp9ModeTreeInterSource {
			e.resetVP9EncoderLeftEntropyContexts()
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, partitionProbs, seg, baseMi, kind, key, inter)
		}
	}
}

func (e *VP9Encoder) writeVP9ModesSb(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, kind vp9ModeTreeKind,
	key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	if miRow >= miRows || miCol >= miCols {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, bsize)
	partition := common.PartitionLookup[bsl][target]
	if counts := vp9EncodeCountsForState(key, inter); counts != nil {
		ctx := vp9dec.PartitionPlaneContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, bsize)
		counts.Partition[ctx][partition]++
	}
	encoder.WritePartitionForBlock(bw, encoder.WriteModesSbArgs{
		AboveSegCtx:    e.aboveSegCtx,
		LeftSegCtx:     e.leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile, seg, baseMi, kind, key, inter)
	} else {
		switch partition {
		case common.PartitionNone:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile, seg, baseMi, kind, key, inter)
		case common.PartitionHorz:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile, seg, baseMi, kind, key, inter)
			if miRow+bs < miRows {
				e.writeVP9ModeBlock(bw, miRows, miCols, miRow+bs, miCol, subsize, tile, seg, baseMi, kind, key, inter)
			}
		case common.PartitionVert:
			e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol, subsize, tile, seg, baseMi, kind, key, inter)
			if miCol+bs < miCols {
				e.writeVP9ModeBlock(bw, miRows, miCols, miRow, miCol+bs, subsize, tile, seg, baseMi, kind, key, inter)
			}
		default:
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol,
				subsize, tile, partitionProbs, seg, baseMi, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, partitionProbs, seg, baseMi, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, partitionProbs, seg, baseMi, kind, key, inter)
			e.writeVP9ModesSb(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, partitionProbs, seg, baseMi, kind, key, inter)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(e.aboveSegCtx, e.leftSegCtx,
			miRow, miCol, subsize, 2*bs)
	}
}

var vp9StubBlockSizeOrder = [...]common.BlockSize{
	common.Block64x64,
	common.Block64x32,
	common.Block32x64,
	common.Block32x32,
	common.Block32x16,
	common.Block16x32,
	common.Block16x16,
	common.Block16x8,
	common.Block8x16,
	common.Block8x8,
	common.Block8x4,
	common.Block4x8,
	common.Block4x4,
}

func vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol int, root common.BlockSize) common.BlockSize {
	maxW := int(common.Num8x8BlocksWideLookup[root])
	maxH := int(common.Num8x8BlocksHighLookup[root])
	availW := min(miCols-miCol, maxW)
	availH := min(miRows-miRow, maxH)
	for _, bsize := range vp9StubBlockSizeOrder {
		if int(common.Num8x8BlocksWideLookup[bsize]) <= availW &&
			int(common.Num8x8BlocksHighLookup[bsize]) <= availH {
			return bsize
		}
	}
	return common.Block4x4
}

func (e *VP9Encoder) writeVP9ModeBlock(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	seg *vp9dec.SegmentationParams, baseMi vp9dec.NeighborMi, kind vp9ModeTreeKind,
	key *vp9KeyframeEncodeState, inter *vp9InterEncodeState,
) {
	cur := baseMi
	cur.SbType = bsize
	cur.TxSize = clampVP9TxSizeForBlock(cur.TxSize, bsize)
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	counts := vp9EncodeCountsForState(key, inter)
	if kind == vp9ModeTreeInterSkip || kind == vp9ModeTreeInterSource {
		reconBsize := vp9ModeInfoDecodeBSize(bsize)
		hasResidue := false
		if kind == vp9ModeTreeInterSource && inter != nil {
			hasResidue = e.prepareVP9InterBlockResidue(inter, miRows, miCols,
				miRow, miCol, reconBsize, &cur)
			if hasResidue {
				cur.Skip = 0
			}
		}
		segID := int(cur.SegIDPredicted)
		interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols,
			tile, miRows, miRow, miCol, bsize)
		countVP9Skip(counts, seg, segID, above, left, cur.Skip)
		countVP9IntraInter(counts, seg, segID, above, left, 1)
		countVP9SingleRef(counts, seg, segID, above, left, cur.RefFrame[0])
		countVP9InterMode(counts, seg, segID, bsize, interModeCtx, cur.Mode)
		encoder.WriteInterBlock(bw, encoder.WriteInterBlockArgs{
			Seg:          seg,
			Mi:           &cur,
			AboveMi:      above,
			LeftMi:       left,
			Fc:           &e.fc,
			TxMode:       txModeForMi(cur),
			FrameRefMode: vp9dec.SingleReference,
			InterpFilter: vp9dec.InterpEighttap,
			InterModeCtx: interModeCtx,
		})
		if kind == vp9ModeTreeInterSource && inter != nil {
			aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
			if !hasResidue {
				vp9dec.ResetSkipContext(e.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
				e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
				return
			}
			_ = encoder.WriteCoefSb(bw, encoder.WriteCoefSbArgs{
				BSize:        reconBsize,
				MiTxSize:     cur.TxSize,
				IsInter:      1,
				Lossless:     false,
				Mi:           &cur,
				Planes:       &e.planes,
				AboveOffsets: aboveOffsets,
				LeftOffsets:  leftOffsets,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					inter.dq.Y[0],
					inter.dq.Uv[0],
					inter.dq.Uv[0],
				},
				Fc:              &e.fc.CoefProbs,
				CoefBranchStats: vp9CoefBranchStats(counts),
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					return e.vp9BlockCoeffs(plane, reconBsize, r, c, tx)
				},
			})
		}
		e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
		return
	}
	if kind == vp9ModeTreeKeyframeSource && key != nil {
		reconBsize := vp9ModeInfoDecodeBSize(bsize)
		hasResidue := e.prepareVP9KeyframeBlockResidue(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur, common.DcPred)
		if hasResidue {
			cur.Skip = 0
		}
		countVP9Skip(counts, seg, int(cur.SegIDPredicted), above, left, cur.Skip)
		encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
			Seg:       seg,
			Mi:        &cur,
			AboveMi:   above,
			LeftMi:    left,
			TxMode:    txModeForMi(cur),
			SkipProbs: e.fc.SkipProbs,
		})
		encoder.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
		aboveOffsets, leftOffsets := e.vp9EncoderPlaneContextOffsets(miRow, miCol)
		if !hasResidue {
			vp9dec.ResetSkipContext(e.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
			e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
			return
		}
		_ = encoder.WriteCoefSb(bw, encoder.WriteCoefSbArgs{
			BSize:        reconBsize,
			MiTxSize:     cur.TxSize,
			IsInter:      0,
			Lossless:     false,
			Mi:           &cur,
			Planes:       &e.planes,
			AboveOffsets: aboveOffsets,
			LeftOffsets:  leftOffsets,
			PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
				key.dq.Y[0],
				key.dq.Uv[0],
				key.dq.Uv[0],
			},
			Fc:              &e.fc.CoefProbs,
			CoefBranchStats: vp9CoefBranchStats(counts),
			GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
				return e.vp9BlockCoeffs(plane, reconBsize, r, c, tx)
			},
		})
		e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
		return
	}
	encoder.WriteKeyframeBlock(bw, encoder.WriteKeyframeBlockArgs{
		Seg:       seg,
		Mi:        &cur,
		AboveMi:   above,
		LeftMi:    left,
		TxMode:    txModeForMi(cur),
		SkipProbs: e.fc.SkipProbs,
	})
	encoder.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
	e.fillVP9MiGrid(miRows, miCols, miRow, miCol, bsize, cur)
}

func (e *VP9Encoder) prepareVP9KeyframeBlockResidue(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, uvMode common.PredictionMode,
) bool {
	hasResidue := false
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		e.clearVP9PlaneBlockCoeffs(plane, planeBsize)
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - max4x4W) >> txSize) * blockStep
		blockIdx := 0
		dequant := key.dq.Y[0]
		if plane > 0 {
			dequant = key.dq.Uv[0]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				coeffBase := (rr*full4x4W + cc) * vp9EncoderTxCoeffSlots
				coeffs := e.blockCoeffs[plane][coeffBase : coeffBase+vp9EncoderTxCoeffSlots]
				if e.prepareVP9KeyframeTxResidue(key, pd, plane, mode,
					txSize, tile, miRows, miCols, miRow, miCol, bsize, rr, cc, dequant, coeffs) {
					hasResidue = true
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	return hasResidue
}

func (e *VP9Encoder) prepareVP9InterBlockResidue(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) bool {
	if !e.copyVP9InterPredictionBlock(inter, miRow, miCol, bsize) {
		return false
	}
	hasResidue := false
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		e.clearVP9PlaneBlockCoeffs(plane, planeBsize)
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		max4x4W, max4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols,
			miRow, miCol, bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		dequant := inter.dq.Y[0]
		if plane > 0 {
			dequant = inter.dq.Uv[0]
		}
		for rr := 0; rr < max4x4H; rr += step {
			for cc := 0; cc < max4x4W; cc += step {
				coeffBase := (rr*full4x4W + cc) * vp9EncoderTxCoeffSlots
				coeffs := e.blockCoeffs[plane][coeffBase : coeffBase+vp9EncoderTxCoeffSlots]
				if e.prepareVP9InterTxResidue(inter, pd, plane, txSize,
					miRow, miCol, rr, cc, dequant, coeffs) {
					hasResidue = true
				}
			}
		}
	}
	return hasResidue
}

func (e *VP9Encoder) copyVP9InterPredictionBlock(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize,
) bool {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return false
	}
	for plane := range vp9dec.MaxMbPlane {
		pd := &e.planes[plane]
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			return false
		}
		dst, dstStride := e.vp9EncoderReconPlane(plane)
		src, srcStride := vp9ReferencePlane(inter.ref, plane)
		if dstStride <= 0 || srcStride <= 0 || len(dst) == 0 || len(src) == 0 {
			return false
		}
		dstRows := len(dst) / dstStride
		srcRows := len(src) / srcStride
		x0 := (miCol * common.MiSize) >> pd.SubsamplingX
		y0 := (miRow * common.MiSize) >> pd.SubsamplingY
		if x0 >= dstStride || y0 >= dstRows || x0 >= srcStride || y0 >= srcRows {
			continue
		}
		bw := int(common.Num4x4BlocksWideLookup[planeBsize]) * 4
		bh := int(common.Num4x4BlocksHighLookup[planeBsize]) * 4
		w := min(bw, min(dstStride-x0, srcStride-x0))
		h := min(bh, min(dstRows-y0, srcRows-y0))
		if w <= 0 || h <= 0 {
			continue
		}
		copyPlane(dst[y0*dstStride+x0:], dstStride,
			src[y0*srcStride+x0:], srcStride, w, h)
	}
	return true
}

func (e *VP9Encoder) clearVP9PlaneBlockCoeffs(plane int, bsize common.BlockSize) {
	if plane < 0 || plane >= vp9dec.MaxMbPlane || bsize >= common.BlockSizes {
		return
	}
	n := int(common.Num4x4BlocksWideLookup[bsize]) *
		int(common.Num4x4BlocksHighLookup[bsize]) * vp9EncoderTxCoeffSlots
	if n > len(e.blockCoeffs[plane]) {
		n = len(e.blockCoeffs[plane])
	}
	for i := range e.blockCoeffs[plane][:n] {
		e.blockCoeffs[plane][i] = 0
	}
}

func (e *VP9Encoder) prepareVP9KeyframeTxResidue(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int, dequant [2]int16, out []int16,
) bool {
	dst, stride, x0, y0, ok := e.predictVP9KeyframeTx(key.hdr, pd, plane, mode,
		txSize, tile, miRows, miCols, miRow, miCol, bsize, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	txType := common.DctDct
	if plane == 0 {
		txType = common.IntraModeToTxType[mode]
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	return e.quantizeVP9TxResidual(dst, stride, txSize, txType, dequant, out)
}

func (e *VP9Encoder) prepareVP9InterTxResidue(inter *vp9InterEncodeState,
	pd *vp9dec.MacroblockdPlane, plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int, dequant [2]int16, out []int16,
) bool {
	dst, stride, x0, y0, ok := e.vp9EncoderTxDst(pd, plane, txSize,
		miRow, miCol, blockRow4x4, blockCol4x4)
	if !ok {
		return false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, plane)
	if !e.gatherVP9TxResidual(src, srcStride, srcW, srcH, dst, stride, x0, y0, txSize) {
		return false
	}
	return e.quantizeVP9TxResidual(dst, stride, txSize, common.DctDct, dequant, out)
}

func (e *VP9Encoder) gatherVP9TxResidual(src []byte, srcStride, srcW, srcH int,
	dst []byte, dstStride, x0, y0 int, txSize common.TxSize,
) bool {
	bs := 4 << uint(txSize)
	if bs*bs > len(e.residueScratch) {
		return false
	}
	for i := range e.residueScratch[:bs*bs] {
		e.residueScratch[i] = 0
	}
	hasDiff := false
	for y := 0; y < bs && y0+y < srcH; y++ {
		srcRow := src[(y0+y)*srcStride:]
		dstRow := dst[y*dstStride:]
		for x := 0; x < bs && x0+x < srcW; x++ {
			diff := int(srcRow[x0+x]) - int(dstRow[x])
			e.residueScratch[y*bs+x] = int16(diff)
			if diff != 0 {
				hasDiff = true
			}
		}
	}
	return hasDiff
}

func (e *VP9Encoder) quantizeVP9TxResidual(dst []byte, stride int,
	txSize common.TxSize, txType common.TxType, dequant [2]int16, out []int16,
) bool {
	maxEob := vp9dec.MaxEobForTxSize(txSize)
	if txType != common.DctDct || maxEob > vp9EncoderTxCoeffSlots ||
		dequant[0] == 0 || dequant[1] == 0 || len(out) < maxEob {
		return false
	}
	for i := range e.txCoeffScratch[:maxEob] {
		e.txCoeffScratch[i] = 0
		e.dqCoeffScratch[i] = 0
	}
	switch txSize {
	case common.Tx4x4:
		encoder.ForwardDCT4x4Into(e.residueScratch[:], 4, e.txCoeffScratch[:maxEob])
	case common.Tx8x8:
		encoder.ForwardDCT8x8Into(e.residueScratch[:], 8, e.txCoeffScratch[:maxEob])
	case common.Tx16x16:
		encoder.ForwardDCT16x16Into(e.residueScratch[:], 16, e.txCoeffScratch[:maxEob])
	default:
		return false
	}
	eob := encoder.QuantizeFP(e.txCoeffScratch[:maxEob], dequant,
		common.DefaultScanOrders[txSize].Scan, e.dqCoeffScratch[:maxEob])
	if eob == 0 {
		return false
	}
	copy(out[:maxEob], e.dqCoeffScratch[:maxEob])
	vp9dec.InverseTransformBlock(out[:maxEob],
		dst, stride, txSize, txType, eob, false)
	return true
}

func (e *VP9Encoder) predictVP9KeyframeTx(hdr *vp9dec.UncompressedHeader,
	pd *vp9dec.MacroblockdPlane, plane int, mode common.PredictionMode,
	txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return nil, 0, 0, 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return nil, 0, 0, 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := vp9AlignTo(int(hdr.Width), 8)
	alignedHeight := vp9AlignTo(int(hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, 0, 0, false
	}

	bounds := vp9BlockBoundsEdges(miRows, miCols, miRow, miCol, bsize)
	leftAvailable := blockCol4x4 != 0 || miCol > tile.MiColStart
	left := e.intraScratch.Left[:bs]
	if leftAvailable {
		for i := range bs {
			sy := y0 + i
			if bounds.MbToBottomEdge < 0 && sy >= planeHeight {
				sy = planeHeight - 1
			}
			left[i] = planeData[sy*stride+x0-1]
		}
	}

	edges := vp9dec.IntraEdgeRefs{
		AboveLeft: 127,
		Left:      left,
	}
	upAvailable := blockRow4x4 != 0 || miRow > 0
	if upAvailable {
		edges.Above = planeData[(y0-1)*stride+x0:]
		if leftAvailable {
			edges.AboveLeft = planeData[(y0-1)*stride+x0-1]
		}
	}
	planeBlock4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	txw := 1 << uint(txSize)
	rightAvailable := blockCol4x4+txw < planeBlock4x4W
	dst = planeData[y0*stride+x0:]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            dst,
		DstStride:      stride,
		Mode:           mode,
		TxSize:         txSize,
		Edges:          edges,
		UpAvailable:    upAvailable,
		LeftAvailable:  leftAvailable,
		RightAvailable: rightAvailable,
		FrameWidth:     planeWidth,
		FrameHeight:    planeHeight,
		X0:             x0,
		Y0:             y0,
		MbToRightEdge:  bounds.MbToRightEdge,
		MbToBottomEdge: bounds.MbToBottomEdge,
	}, &e.intraScratch)
	return dst, stride, x0, y0, true
}

func (e *VP9Encoder) vp9EncoderTxDst(pd *vp9dec.MacroblockdPlane,
	plane int, txSize common.TxSize,
	miRow, miCol int, blockRow4x4, blockCol4x4 int,
) (dst []byte, stride, x0, y0 int, ok bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 {
		return nil, 0, 0, 0, false
	}
	rows := len(planeData) / stride
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 = baseX + blockCol4x4*4
	y0 = baseY + blockRow4x4*4
	bs := 4 << uint(txSize)
	if x0+bs > stride || y0+bs > rows {
		return nil, 0, 0, 0, false
	}
	return planeData[y0*stride+x0:], stride, x0, y0, true
}

func (e *VP9Encoder) vp9BlockCoeffs(plane int,
	bsize common.BlockSize, r, c int, tx common.TxSize,
) []int16 {
	coeffs := e.coefScratch[:vp9dec.MaxEobForTxSize(tx)]
	for i := range coeffs {
		coeffs[i] = 0
	}
	if plane < 0 || plane >= vp9dec.MaxMbPlane {
		return coeffs
	}
	pd := &e.planes[plane]
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return coeffs
	}
	full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
	coeffBase := (r*full4x4W + c) * vp9EncoderTxCoeffSlots
	maxEob := vp9dec.MaxEobForTxSize(tx)
	if maxEob <= vp9EncoderTxCoeffSlots && coeffBase >= 0 &&
		coeffBase+maxEob <= len(e.blockCoeffs[plane]) {
		copy(coeffs, e.blockCoeffs[plane][coeffBase:coeffBase+maxEob])
	}
	return coeffs
}

func (e *VP9Encoder) vp9EncoderReconPlane(plane int) ([]byte, int) {
	switch plane {
	case 0:
		return e.reconY, e.reconFrame.YStride
	case 1:
		return e.reconU, e.reconFrame.UStride
	case 2:
		return e.reconV, e.reconFrame.VStride
	default:
		return nil, 0
	}
}

func vp9EncoderSourcePlane(img *image.YCbCr, plane int) (
	pixels []byte, stride, width, height int,
) {
	if img == nil {
		return nil, 0, 0, 0
	}
	switch plane {
	case 0:
		return img.Y, img.YStride, img.Rect.Dx(), img.Rect.Dy()
	case 1:
		return img.Cb, img.CStride, (img.Rect.Dx() + 1) >> 1, (img.Rect.Dy() + 1) >> 1
	case 2:
		return img.Cr, img.CStride, (img.Rect.Dx() + 1) >> 1, (img.Rect.Dy() + 1) >> 1
	default:
		return nil, 0, 0, 0
	}
}

func (e *VP9Encoder) vp9MiAt(miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &e.miGrid[r*miCols+c]
}

func (e *VP9Encoder) fillVP9MiGrid(miRows, miCols, r, c int, bsize common.BlockSize, mi vp9dec.NeighborMi) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := e.miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// Encode is the alloc-returning wrapper around EncodeInto. Sizes dst at 64 KB
// upfront so EncodeInto can never overflow the compressed-header staging
// buffer.
func (e *VP9Encoder) Encode(img *image.YCbCr) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(img, dst)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, dst[:n])
	return out, nil
}

func alignToSb(miCols int) int {
	const mask = common.MiBlockSize - 1
	return (miCols + mask) &^ mask
}

// Close releases internal state and marks the encoder as no longer
// usable. Subsequent Encode / EncodeInto calls return [ErrClosed].
func (e *VP9Encoder) Close() error {
	if e == nil {
		return ErrClosed
	}
	e.closed = true
	return nil
}

// Codec reports the codec this encoder targets.
func (e *VP9Encoder) Codec() Codec { return CodecVP9 }
