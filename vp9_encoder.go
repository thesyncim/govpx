package govpx

import (
	"errors"
	"image"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9dsp "github.com/thesyncim/govpx/internal/vp9/dsp"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	vp9EncoderTxCoeffSlots    = 1024
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
	// forceKeyFrame is a sticky one-shot request consumed by the next
	// successfully committed frame.
	forceKeyFrame bool

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
	modeScratch  [1024]byte
	// interPredictScratch is passed through the decoder-shared inter
	// predictor so odd luma MVs can use the same chroma/subpel extension
	// path as the real decoder without per-block allocations after warmup.
	interPredictScratch []byte

	reconFrame Image
	reconYFull []byte
	reconUFull []byte
	reconVFull []byte
	reconY     []byte
	reconU     []byte
	reconV     []byte

	refFrames [common.RefFrames]vp9ReferenceFrame

	prevFrameMvs      []vp9MvRef
	prevFrameMvRows   int
	prevFrameMvCols   int
	prevFrameMvsValid bool

	blockCoeffs    [vp9dec.MaxMbPlane][vp9EncoderBlockCoeffSlots]int16
	coefScratch    [1024]int16
	residueScratch [1024]int16
	txCoeffScratch [1024]int16
	dqCoeffScratch [1024]int16
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
	if e.frameIndex == 0 || e.forceKeyFrame {
		return true
	}
	cadence := e.opts.MaxKeyframeInterval
	if cadence <= 0 {
		cadence = 128 // libvpx default kf_max_dist
	}
	return e.frameIndex%cadence == 0
}

// ForceKeyFrame requests that the next successfully committed VP9 packet be
// a key frame. Calls on a nil or closed encoder are no-ops.
func (e *VP9Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

// EncodeInto packs the next profile 0 frame into dst. It is equivalent to
// EncodeIntoWithFlags with no flags.
//
// Returns the number of bytes written into dst. Caller sizes dst; leave room
// for up to 64 KiB to match libvpx's first-partition header bound.
func (e *VP9Encoder) EncodeInto(img *image.YCbCr, dst []byte) (int, error) {
	return e.EncodeIntoWithFlags(img, dst, 0)
}

// EncodeIntoWithFlags packs the next profile 0 frame into dst while applying
// the VP9-compatible subset of EncodeFlags: EncodeForceKeyFrame,
// EncodeNoReference{Last,Golden,AltRef}, EncodeNoUpdate{Last,Golden,AltRef},
// and EncodeNoUpdateEntropy. Invisible frames and forced GOLDEN / ALTREF
// refreshes are not implemented by the current profile 0 packet path.
//
// The current packet path emits source-backed keyframes and visible LAST inter
// frames with fixed-size DCT_DCT residual transforms up to Tx32x32, including
// bounded rate-aware LAST-frame motion search with quarter-pel refinement. A
// deterministic prepass walks the same mode tree to collect frame counts before
// the compressed header, so the real tile is encoded with same-frame
// counts-driven probability updates.
func (e *VP9Encoder) EncodeIntoWithFlags(img *image.YCbCr, dst []byte, flags EncodeFlags) (int, error) {
	if e == nil || e.closed {
		return 0, ErrClosed
	}
	if err := validateVP9EncodeFlags(flags); err != nil {
		return 0, err
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

	isKey := e.vp9ShouldEncodeKeyFrame(flags)
	if !isKey && !e.refFrames[0].valid {
		isKey = true
	}
	if isKey && flags&vp9NoUpdateRefFlags != 0 {
		return 0, ErrInvalidConfig
	}
	if isKey {
		vp9dec.ResetFrameContext(&e.fc)
	} else if e.opts.ErrorResilient {
		vp9dec.ResetFrameContext(&e.fc)
	}
	frameContextSeed := e.fc
	restoreFrameContext := e.opts.ErrorResilient || flags&EncodeNoUpdateEntropy != 0
	defer func() {
		if restoreFrameContext {
			e.fc = frameContextSeed
		}
	}()
	e.prepareVP9EncoderOutputFrame(int(width), int(height))

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		ShowFrame:             true,
		ErrorResilientMode:    e.opts.ErrorResilient,
		Width:                 width,
		Height:                height,
		RefreshFrameContext:   flags&EncodeNoUpdateEntropy == 0,
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
		if flags&EncodeNoUpdateLast != 0 {
			header.RefreshFrameFlags = 0
		}
		header.InterRef.RefIndex = [3]uint8{0, 0, 0}
		header.InterRef.SignBias = [3]uint8{0, 0, 0}
	}

	txMode := common.Allow32x32
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx32x32,
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
			img:      img,
			dq:       &dq,
			ref:      &e.refFrames[0],
			selectFc: e.fc,
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
	e.refreshVP9EncoderMvRefs(isKey, miRows, miCols)
	e.refreshVP9EncoderRefs(&header)
	e.frameIndex++
	if isKey {
		e.forceKeyFrame = false
	}
	return n, nil
}

const vp9NoUpdateRefFlags = EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef

func validateVP9EncodeFlags(flags EncodeFlags) error {
	if err := validateEncodeFlags(flags); err != nil {
		return err
	}
	const unsupported = EncodeInvisibleFrame | EncodeForceGoldenFrame | EncodeForceAltRefFrame
	if flags&unsupported != 0 {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP9Encoder) vp9ShouldEncodeKeyFrame(flags EncodeFlags) bool {
	if e == nil || e.closed {
		return false
	}
	if flags&EncodeForceKeyFrame != 0 || flags&EncodeNoReferenceLast != 0 {
		return true
	}
	return e.IsKeyFrameNext()
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

func (e *VP9Encoder) refreshVP9EncoderMvRefs(isKey bool, miRows, miCols int) {
	if isKey {
		e.prevFrameMvsValid = false
		e.prevFrameMvRows = 0
		e.prevFrameMvCols = 0
		return
	}
	need := miRows * miCols
	if cap(e.prevFrameMvs) < need {
		e.prevFrameMvs = make([]vp9MvRef, need)
	} else {
		e.prevFrameMvs = e.prevFrameMvs[:need]
	}
	for i := 0; i < need; i++ {
		mi := e.miGrid[i]
		e.prevFrameMvs[i] = vp9MvRef{RefFrame: mi.RefFrame, Mv: mi.Mv}
	}
	e.prevFrameMvRows = miRows
	e.prevFrameMvCols = miCols
	e.prevFrameMvsValid = true
}

func (e *VP9Encoder) useVP9EncoderPrevFrameMvs(miRows, miCols int) bool {
	return e.prevFrameMvsValid &&
		!e.opts.ErrorResilient &&
		e.prevFrameMvRows == miRows &&
		e.prevFrameMvCols == miCols &&
		len(e.prevFrameMvs) >= miRows*miCols
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
	if mi.TxSize >= common.Tx32x32 {
		return common.Allow32x32
	}
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

func countVP9NewMv(counts *encoder.FrameCounts, mv, refMv vp9dec.MV) {
	if counts == nil {
		return
	}
	diff := vp9dec.MV{
		Row: mv.Row - refMv.Row,
		Col: mv.Col - refMv.Col,
	}
	vp9IncEncoderMv(diff, &counts.Mv)
}

func vp9IncEncoderMv(mv vp9dec.MV, counts *encoder.NmvContextCounts) {
	joint := vp9GetMvJoint(mv)
	counts.Joints[joint]++
	if joint == tables.MvJointHzVnz || joint == tables.MvJointHnzVnz {
		vp9IncEncoderMvComponent(mv.Row, &counts.Comps[0])
	}
	if joint == tables.MvJointHnzVz || joint == tables.MvJointHnzVnz {
		vp9IncEncoderMvComponent(mv.Col, &counts.Comps[1])
	}
}

func vp9IncEncoderMvComponent(v int16, counts *encoder.NmvComponentCounts) {
	sign := 0
	zv := int(v)
	if zv < 0 {
		sign = 1
		zv = -zv
	}
	counts.Sign[sign]++
	z := zv - 1
	cls, offset := vp9GetMvClass(z)
	counts.Classes[cls]++
	d := offset >> 3
	f := (offset >> 1) & 3
	hp := offset & 1
	if cls == tables.MvClass0 {
		counts.Class0[d]++
		counts.Class0Fp[d][f]++
		counts.Class0Hp[hp]++
		return
	}
	nBits := cls + vp9dec.Class0Bits - 1
	for i := 0; i < nBits; i++ {
		counts.Bits[i][(d>>i)&1]++
	}
	counts.Fp[f]++
	counts.Hp[hp]++
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
	img      *image.YCbCr
	dq       *vp9dec.DequantTables
	ref      *vp9ReferenceFrame
	selectFc vp9dec.FrameContext
	counts   *encoder.FrameCounts
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
				miRow, miCol, reconBsize, tile, &cur)
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
		bestRefMv := e.vp9EncoderBestInterRefMvs(tile, miRows, miCols,
			miRow, miCol, bsize, &cur)
		if cur.Mode == common.NewMv {
			countVP9NewMv(counts, cur.Mv[0], bestRefMv[0])
		}
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
			Mv:           cur.Mv,
			BestRefMv:    bestRefMv,
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
		cur.Mode = e.pickVP9KeyframeMode(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur)
		uvMode := e.pickVP9KeyframeUvMode(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur)
		hasResidue := e.prepareVP9KeyframeBlockResidue(key, tile, miRows, miCols,
			miRow, miCol, reconBsize, &cur, uvMode)
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
		encoder.WriteKeyframeUvMode(bw, uvMode, cur.Mode)
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

func (e *VP9Encoder) pickVP9KeyframeMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) common.PredictionMode {
	// VP9 Tx32 is DCT-only, so all intra predictors can reuse the
	// current transform path. Smaller tx sizes need forward hybrid
	// ADST kernels before non-DC mode picking can safely expand there.
	if key == nil || mi == nil || mi.TxSize != common.Tx32x32 {
		return common.DcPred
	}
	bestMode := common.DcPred
	bestScore, ok := e.scoreVP9KeyframeTxPrediction(key, &e.planes[0], bestMode,
		0, mi.TxSize, tile, miRows, miCols, miRow, miCol, bsize, 0, 0)
	if !ok {
		return bestMode
	}
	for mode := common.DcPred + 1; mode <= common.TmPred; mode++ {
		score, ok := e.scoreVP9KeyframeTxPrediction(key, &e.planes[0], mode,
			0, mi.TxSize, tile, miRows, miCols, miRow, miCol, bsize, 0, 0)
		if ok && score < bestScore {
			bestScore = score
			bestMode = mode
		}
	}
	return bestMode
}

func (e *VP9Encoder) pickVP9KeyframeUvMode(key *vp9KeyframeEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi,
) common.PredictionMode {
	if key == nil || mi == nil {
		return common.DcPred
	}
	uvProbs := tables.KfUvModeProb[mi.Mode]
	var uvModeCosts [common.IntraModes]int
	encoder.VP9CostTokens(uvModeCosts[:], uvProbs[:], common.IntraModeTree[:])
	qindex := e.vp9EncoderModeDecisionQIndex()

	bestMode := common.DcPred
	bestScore, ok := e.scoreVP9KeyframeUvPrediction(key, bestMode,
		uvModeCosts[bestMode], qindex, tile, miRows, miCols, miRow, miCol,
		bsize, mi)
	if !ok {
		return bestMode
	}
	for mode := common.DcPred + 1; mode <= common.TmPred; mode++ {
		score, ok := e.scoreVP9KeyframeUvPrediction(key, mode,
			uvModeCosts[mode], qindex, tile, miRows, miCols, miRow, miCol,
			bsize, mi)
		if ok && score < bestScore {
			bestScore = score
			bestMode = mode
		}
	}
	return bestMode
}

func (e *VP9Encoder) scoreVP9KeyframeUvPrediction(key *vp9KeyframeEncodeState,
	mode common.PredictionMode, rate, qindex int, tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) (uint64, bool) {
	var distortion uint64
	for plane := 1; plane < vp9dec.MaxMbPlane; plane++ {
		pd := &e.planes[plane]
		txSize := vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		score, ok := e.scoreVP9KeyframeTxPrediction(key, pd, mode, plane,
			txSize, tile, miRows, miCols, miRow, miCol, bsize, 0, 0)
		if !ok {
			return 0, false
		}
		distortion += score
	}
	return vp9ModeDecisionScore(distortion, rate, qindex), true
}

func (e *VP9Encoder) scoreVP9KeyframeTxPrediction(key *vp9KeyframeEncodeState,
	pd *vp9dec.MacroblockdPlane, mode common.PredictionMode,
	plane int, txSize common.TxSize, tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, blockRow4x4, blockCol4x4 int,
) (uint64, bool) {
	planeData, stride := e.vp9EncoderReconPlane(plane)
	if stride <= 0 || len(planeData) == 0 || int(mode) >= common.IntraModes {
		return 0, false
	}
	planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
	if planeBsize >= common.BlockSizes {
		return 0, false
	}
	rows := len(planeData) / stride
	alignedWidth := vp9AlignTo(int(key.hdr.Width), 8)
	alignedHeight := vp9AlignTo(int(key.hdr.Height), 8)
	planeWidth := alignedWidth >> pd.SubsamplingX
	planeHeight := alignedHeight >> pd.SubsamplingY
	baseX := (miCol * common.MiSize) >> pd.SubsamplingX
	baseY := (miRow * common.MiSize) >> pd.SubsamplingY
	x0 := baseX + blockCol4x4*4
	y0 := baseY + blockRow4x4*4

	bs := 4 << uint(txSize)
	if bs*bs > len(e.modeScratch) || x0+bs > stride || y0+bs > rows {
		return 0, false
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

	pred := e.modeScratch[:bs*bs]
	vp9dec.BuildIntraPredictorsWithScratch(vp9dec.BuildIntraPredictorsArgs{
		Dst:            pred,
		DstStride:      bs,
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

	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(key.img, plane)
	if len(src) == 0 || srcStride <= 0 {
		return 0, false
	}
	var score uint64
	for y := 0; y < bs && y0+y < srcH; y++ {
		srcRow := src[(y0+y)*srcStride:]
		predRow := pred[y*bs:]
		for x := 0; x < bs && x0+x < srcW; x++ {
			diff := int(srcRow[x0+x]) - int(predRow[x])
			score += uint64(diff * diff)
		}
	}
	return score, true
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
	bsize common.BlockSize, tile vp9dec.TileBounds, mi *vp9dec.NeighborMi,
) bool {
	if !e.prepareVP9InterPredictionBlock(inter, miRows, miCols, miRow, miCol,
		bsize, tile, mi) {
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

func (e *VP9Encoder) prepareVP9InterPredictionBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds, mi *vp9dec.NeighborMi,
) bool {
	if mi == nil {
		return false
	}
	mi.Mode = common.ZeroMv
	mi.Mv = [2]vp9dec.MV{}
	if decision, ok := e.pickVP9InterMode(inter, tile, miRows, miCols,
		miRow, miCol, bsize, mi.RefFrame[0]); ok {
		mi.Mode = decision.mode
		mi.Mv[0] = decision.mv
	}
	return e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, mi)
}

type vp9InterModeDecision struct {
	mode  common.PredictionMode
	mv    vp9dec.MV
	rate  int
	sad   uint64
	score uint64
}

func (e *VP9Encoder) pickVP9InterMode(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, refFrame int8,
) (vp9InterModeDecision, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid ||
		refFrame <= vp9dec.IntraFrame {
		return vp9InterModeDecision{}, false
	}
	if bsize < common.Block16x16 {
		return vp9InterModeDecision{}, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(inter.ref, 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return vp9InterModeDecision{}, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > refW || y0+blockH > refH {
		return vp9InterModeDecision{}, false
	}

	interModeCtx := vp9dec.InterModeContext(e.miGrid, miCols, tile,
		miRows, miRow, miCol, bsize)
	qindex := e.vp9EncoderModeDecisionQIndex()
	bestSet := false
	var best vp9InterModeDecision
	consider := func(mode common.PredictionMode, mv, refMv vp9dec.MV, sad uint64) {
		rate := vp9InterModeRateCost(&inter.selectFc, interModeCtx, mode, mv, refMv)
		cand := vp9InterModeDecision{
			mode:  mode,
			mv:    mv,
			rate:  rate,
			sad:   sad,
			score: vp9InterModeScore(sad, rate, qindex),
		}
		if !bestSet || cand.score < best.score ||
			(cand.score == best.score && cand.rate < best.rate) {
			best = cand
			bestSet = true
		}
	}

	zeroSad := vp9BlockSAD(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH, ^uint64(0))
	consider(common.ZeroMv, vp9dec.MV{}, vp9dec.MV{}, zeroSad)

	for _, mode := range [...]common.PredictionMode{common.NearestMv, common.NearMv} {
		mv, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, mode, refFrame)
		if !ok {
			continue
		}
		sad, ok := e.vp9InterPredictionSAD(inter, miRows, miCols,
			miRow, miCol, bsize, mode, mv, ^uint64(0))
		if ok {
			consider(mode, mv, mv, sad)
		}
	}

	if mv, sad, ok := e.pickVP9InterMv(inter, miRows, miCols,
		miRow, miCol, bsize); ok {
		refMv, _ := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
			miRow, miCol, bsize, common.NewMv, refFrame)
		consider(common.NewMv, mv, refMv, sad)
	}
	if !bestSet || best.mode == common.ZeroMv {
		return vp9InterModeDecision{}, false
	}
	return best, true
}

func (e *VP9Encoder) pickVP9InterMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) (vp9dec.MV, uint64, bool) {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return vp9dec.MV{}, 0, false
	}
	if bsize < common.Block16x16 {
		return vp9dec.MV{}, 0, false
	}
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	ref, refStride, refW, refH := vp9ReferenceVisiblePlane(inter.ref, 0)
	if len(src) == 0 || len(ref) == 0 || srcStride <= 0 || refStride <= 0 {
		return vp9dec.MV{}, 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > refW || y0+blockH > refH {
		return vp9dec.MV{}, 0, false
	}

	bestScore := vp9BlockSAD(src, srcStride, ref, refStride,
		x0, y0, x0, y0, blockW, blockH, ^uint64(0))
	bestDx, bestDy := 0, 0
	eval := func(dx, dy int) bool {
		if dx == bestDx && dy == bestDy {
			return false
		}
		refX := x0 + dx
		refY := y0 + dy
		if refX < 0 || refY < 0 || refX+blockW > refW || refY+blockH > refH {
			return false
		}
		score := vp9BlockSAD(src, srcStride, ref, refStride,
			x0, y0, refX, refY, blockW, blockH, bestScore)
		if score < bestScore {
			bestScore = score
			bestDx = dx
			bestDy = dy
			return true
		}
		return false
	}

	const (
		searchRadius = 16
		coarseStep   = 8
		minStep      = 1
	)
	for dy := -searchRadius; dy <= searchRadius; dy += coarseStep {
		for dx := -searchRadius; dx <= searchRadius; dx += coarseStep {
			eval(dx, dy)
		}
	}
	for step := coarseStep >> 1; step >= minStep; step >>= 1 {
		improved := true
		for improved {
			improved = false
			centerDx, centerDy := bestDx, bestDy
			for dy := centerDy - step; dy <= centerDy+step; dy += step {
				for dx := centerDx - step; dx <= centerDx+step; dx += step {
					if dx < -searchRadius || dx > searchRadius ||
						dy < -searchRadius || dy > searchRadius {
						continue
					}
					if eval(dx, dy) {
						improved = true
					}
				}
			}
		}
	}
	mv := vp9dec.MV{Row: int16(bestDy * 8), Col: int16(bestDx * 8)}
	vp9ClampMvRef(&mv, miRows, miCols, miRow, miCol, bsize)
	vp9dec.LowerMvPrecision(&mv, false)
	mv, bestScore = e.refineVP9InterSubpelMv(inter, miRows, miCols,
		miRow, miCol, bsize, mv, bestScore)
	if mv == (vp9dec.MV{}) {
		return vp9dec.MV{}, bestScore, false
	}
	return mv, bestScore, true
}

func (e *VP9Encoder) refineVP9InterSubpelMv(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	best vp9dec.MV, bestScore uint64,
) (vp9dec.MV, uint64) {
	for step := int16(4); step >= 2; step >>= 1 {
		improved := true
		for improved {
			improved = false
			center := best
			for row := center.Row - step; row <= center.Row+step; row += step {
				for col := center.Col - step; col <= center.Col+step; col += step {
					cand := vp9dec.MV{Row: row, Col: col}
					vp9ClampMvRef(&cand, miRows, miCols, miRow, miCol, bsize)
					vp9dec.LowerMvPrecision(&cand, false)
					if cand == best {
						continue
					}
					score, ok := e.vp9InterPredictionSAD(inter, miRows, miCols,
						miRow, miCol, bsize, common.NewMv, cand, bestScore)
					if !ok || score >= bestScore {
						continue
					}
					best = cand
					bestScore = score
					improved = true
				}
			}
		}
	}
	return best, bestScore
}

func (e *VP9Encoder) vp9InterPredictionSAD(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, mv vp9dec.MV, limit uint64,
) (uint64, bool) {
	src, srcStride, srcW, srcH := vp9EncoderSourcePlane(inter.img, 0)
	dst, dstStride := e.vp9EncoderReconPlane(0)
	if len(src) == 0 || len(dst) == 0 || srcStride <= 0 || dstStride <= 0 {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	dstRows := len(dst) / dstStride
	if x0+blockW > srcW || y0+blockH > srcH ||
		x0+blockW > dstStride || y0+blockH > dstRows {
		return 0, false
	}
	mi := vp9dec.NeighborMi{
		SbType: bsize,
		Mode:   mode,
		RefFrame: [2]int8{
			vp9dec.LastFrame,
			vp9dec.NoRefFrame,
		},
		Mv: [2]vp9dec.MV{mv},
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi) {
		return 0, false
	}
	return vp9BlockSAD(src, srcStride, dst, dstStride,
		x0, y0, x0, y0, blockW, blockH, limit), true
}

func (e *VP9Encoder) vp9EncoderModeDecisionQIndex() int {
	qindex := e.opts.Quantizer
	if qindex == 0 {
		qindex = 1
	}
	return qindex
}

func vp9InterModeScore(sad uint64, rate, qindex int) uint64 {
	return vp9ModeDecisionScore(sad, rate, qindex)
}

func vp9ModeDecisionScore(distortion uint64, rate, qindex int) uint64 {
	if rate < 0 {
		rate = 0
	}
	lambda := 1
	if qindex > 0 {
		lambda += qindex / 32
	}
	return (distortion << encoder.VP9ProbCostShift) + uint64(rate*lambda)
}

func vp9InterModeRateCost(fc *vp9dec.FrameContext, ctx int,
	mode common.PredictionMode, mv, refMv vp9dec.MV,
) int {
	if fc == nil || ctx < 0 || ctx >= len(fc.InterModeProbs) {
		return 0
	}
	probs := fc.InterModeProbs[ctx]
	cost := 0
	switch mode {
	case common.ZeroMv:
		cost = encoder.VP9CostBit(probs[0], 0)
	case common.NearestMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 0)
	case common.NearMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1) +
			encoder.VP9CostBit(probs[2], 0)
	case common.NewMv:
		cost = encoder.VP9CostBit(probs[0], 1) +
			encoder.VP9CostBit(probs[1], 1) +
			encoder.VP9CostBit(probs[2], 1) +
			encoder.MvCost(mv, refMv, &fc.Nmvc, false)
	default:
		return 0
	}
	return cost
}

func vp9BlockSAD(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int, limit uint64,
) uint64 {
	if limit == ^uint64(0) {
		if sad, ok := vp9BlockSADNoLimit(src, srcStride, ref, refStride,
			srcX, srcY, refX, refY, w, h); ok {
			return uint64(sad)
		}
	}
	var sad uint64
	for y := range h {
		srcRow := src[(srcY+y)*srcStride+srcX:]
		refRow := ref[(refY+y)*refStride+refX:]
		for x := range w {
			diff := int(srcRow[x]) - int(refRow[x])
			if diff < 0 {
				diff = -diff
			}
			sad += uint64(diff)
		}
		if sad >= limit {
			return sad
		}
	}
	return sad
}

func vp9BlockSADNoLimit(src []byte, srcStride int, ref []byte, refStride int,
	srcX, srcY, refX, refY, w, h int,
) (uint32, bool) {
	srcOff := srcY*srcStride + srcX
	refOff := refY*refStride + refX
	switch {
	case w == 64 && h == 64:
		return vp9dsp.VpxSad64x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 64 && h == 32:
		return vp9dsp.VpxSad64x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 64:
		return vp9dsp.VpxSad32x64(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 32:
		return vp9dsp.VpxSad32x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 32 && h == 16:
		return vp9dsp.VpxSad32x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 32:
		return vp9dsp.VpxSad16x32(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 16:
		return vp9dsp.VpxSad16x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 16 && h == 8:
		return vp9dsp.VpxSad16x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 16:
		return vp9dsp.VpxSad8x16(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 8:
		return vp9dsp.VpxSad8x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 8 && h == 4:
		return vp9dsp.VpxSad8x4(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 8:
		return vp9dsp.VpxSad4x8(src, srcOff, srcStride, ref, refOff, refStride), true
	case w == 4 && h == 4:
		return vp9dsp.VpxSad4x4(src, srcOff, srcStride, ref, refOff, refStride), true
	default:
		return 0, false
	}
}

func (e *VP9Encoder) vp9EncoderBestInterRefMvs(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) [2]vp9dec.MV {
	var best [2]vp9dec.MV
	if mi == nil || mi.Mode == common.ZeroMv || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return best
	}
	if cand, ok := e.vp9EncoderInterModeCandidateMv(tile, miRows, miCols,
		miRow, miCol, bsize, mi.Mode, mi.RefFrame[0]); ok {
		best[0] = cand
	}
	return best
}

func (e *VP9Encoder) vp9EncoderInterModeCandidateMv(tile vp9dec.TileBounds,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mode common.PredictionMode, refFrame int8,
) (vp9dec.MV, bool) {
	if mode == common.ZeroMv || refFrame <= vp9dec.IntraFrame {
		return vp9dec.MV{}, false
	}
	refFinder := VP9Decoder{
		miGrid:          e.miGrid,
		usePrevFrameMvs: e.useVP9EncoderPrevFrameMvs(miRows, miCols),
		prevFrameMvs:    e.prevFrameMvs,
		prevFrameMvRows: e.prevFrameMvRows,
		prevFrameMvCols: e.prevFrameMvCols,
	}
	signBias := [vp9dec.MaxRefFrames]uint8{}
	refList, refCount := refFinder.vp9FindInterMvRefs(tile, miRows, miCols,
		miRow, miCol, bsize, mode, refFrame, signBias)
	if mode == common.NearMv {
		if refCount <= 1 {
			return vp9dec.MV{}, false
		}
	} else if refCount == 0 {
		return vp9dec.MV{}, false
	}
	mv := vp9InterModeMvCandidate(refList, refCount, mode)
	vp9dec.LowerMvPrecision(&mv, false)
	return mv, true
}

func (e *VP9Encoder) predictVP9InterBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	mi *vp9dec.NeighborMi,
) bool {
	if inter == nil || inter.ref == nil || !inter.ref.valid {
		return false
	}
	if mi == nil || mi.RefFrame[0] <= vp9dec.IntraFrame {
		return false
	}
	var refs [common.RefFrames]vp9ReferenceFrame
	refs[0] = *inter.ref
	predictor := VP9Decoder{
		planes:              e.planes,
		frameY:              e.reconY,
		frameU:              e.reconU,
		frameV:              e.reconV,
		lastFrame:           e.reconFrame,
		interPredictScratch: e.interPredictScratch,
		refFrames:           refs,
	}
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(e.opts.Width),
		Height: uint32(e.opts.Height),
		InterRef: vp9dec.InterRefBlock{
			RefIndex: [3]uint8{0, 0, 0},
		},
		InterpFilter: vp9dec.InterpEighttap,
	}
	ok := predictor.reconstructVP9InterPredictBlock(&hdr, mi, miRow, miCol, bsize)
	e.interPredictScratch = predictor.interPredictScratch
	return ok && !predictor.unsupportedReconstruct
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
	if plane == 0 && txSize != common.Tx32x32 {
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
	case common.Tx32x32:
		encoder.ForwardDCT32x32Into(e.residueScratch[:], 32, e.txCoeffScratch[:maxEob])
	default:
		return false
	}
	scan := common.DefaultScanOrders[txSize].Scan
	eob := 0
	if txSize == common.Tx32x32 {
		eob = encoder.QuantizeFP32x32(e.txCoeffScratch[:maxEob], dequant,
			scan, e.dqCoeffScratch[:maxEob])
	} else {
		eob = encoder.QuantizeFP(e.txCoeffScratch[:maxEob], dequant,
			scan, e.dqCoeffScratch[:maxEob])
	}
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

func vp9ReferenceVisiblePlane(ref *vp9ReferenceFrame, plane int) (
	pixels []byte, stride, width, height int,
) {
	if ref == nil || !ref.valid {
		return nil, 0, 0, 0
	}
	pixels, stride = vp9ReferencePlane(ref, plane)
	switch plane {
	case 0:
		return pixels, stride, ref.img.Width, ref.img.Height
	case 1, 2:
		return pixels, stride, (ref.img.Width + 1) >> 1, (ref.img.Height + 1) >> 1
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
	return e.EncodeWithFlags(img, 0)
}

// EncodeWithFlags is the alloc-returning wrapper around EncodeIntoWithFlags.
func (e *VP9Encoder) EncodeWithFlags(img *image.YCbCr, flags EncodeFlags) ([]byte, error) {
	if e == nil || e.closed {
		return nil, ErrClosed
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeIntoWithFlags(img, dst, flags)
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
