package govpx

import (
	"errors"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

type Deadline int

const (
	DeadlineBestQuality Deadline = iota
	DeadlineGoodQuality
	DeadlineRealtime
)

type EncodeFlags uint32

const (
	EncodeForceKeyFrame EncodeFlags = 1 << iota

	EncodeInvisibleFrame

	EncodeNoReferenceLast
	EncodeNoReferenceGolden
	EncodeNoReferenceAltRef

	EncodeNoUpdateLast
	EncodeNoUpdateGolden
	EncodeNoUpdateAltRef

	EncodeNoUpdateEntropy
)

type EncoderOptions struct {
	Width  int
	Height int

	// Convenience framerate model.
	// If FPS is set, TimebaseNum/TimebaseDen may be derived.
	FPS int

	// Explicit timing model.
	TimebaseNum int
	TimebaseDen int

	Threads int

	// Rate control.
	RateControlMode   RateControlMode
	TargetBitrateKbps int
	MinBitrateKbps    int
	MaxBitrateKbps    int

	MinQuantizer int
	MaxQuantizer int
	// CQLevel mirrors libvpx's VP8E_SET_CQ_LEVEL. In RateControlCQ mode,
	// zero uses libvpx's default level unless MinQuantizer is also zero.
	CQLevel int

	UndershootPct int
	OvershootPct  int

	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int
	MaxIntraBitratePct  int
	GFCBRBoostPct       int

	DropFrameAllowed bool

	TemporalScalability TemporalScalabilityConfig

	// Realtime/performance behavior.
	Deadline Deadline
	CpuUsed  int

	// GOP/keyframe behavior.
	KeyFrameInterval int

	// VP8 behavior.
	ErrorResilient bool
	// TokenPartitions is VP8's token partition selector: 0=one, 1=two, 2=four, 3=eight.
	TokenPartitions int

	// Quality knobs.
	Sharpness       int
	StaticThreshold int
}

type EncodeResult struct {
	Data []byte

	KeyFrame bool
	Dropped  bool
	// Droppable reports libvpx's encoded-frame discardability signal: true
	// when the frame updates no reference, entropy, or segmentation state.
	Droppable bool

	PTS      uint64
	Duration uint64

	Quantizer int

	SizeBytes int

	TargetBitrateKbps int
	FrameTargetBits   int
	BufferLevelBits   int

	TemporalLayerID                int
	TemporalLayerCount             int
	TemporalLayerSync              bool
	TL0PICIDX                      uint8
	TemporalLayerTargetBitrateKbps int

	PSNRHint float64
}

type VP8Encoder struct {
	opts EncoderOptions

	timing   timingState
	rc       rateControlState
	temporal temporalState

	closed        bool
	forceKeyFrame bool
	frameCount    uint64

	cyclicRefreshIndex   int
	lastInterZeroMVCount int

	keyFrameModes   []vp8enc.KeyFrameMacroblockMode
	interFrameModes []vp8enc.InterFrameMacroblockMode
	keyFrameCoeffs  []vp8enc.MacroblockCoefficients
	tokenAbove      []vp8enc.TokenContextPlanes

	current   vp8common.FrameBuffer
	analysis  vp8common.FrameBuffer
	lastRef   vp8common.FrameBuffer
	goldenRef vp8common.FrameBuffer
	altRef    vp8common.FrameBuffer

	reconstructModes   []vp8dec.MacroblockMode
	reconstructTokens  []vp8dec.MacroblockTokens
	dequantTables      vp8common.FrameDequantTables
	dequants           [vp8common.MaxMBSegments]vp8common.MacroblockDequant
	reconstructScratch vp8dec.IntraReconstructionScratch
	loopInfo           vp8common.LoopFilterInfo
	coefProbs          vp8tables.CoefficientProbs
	modeProbs          vp8dec.ModeProbs
}

const encoderQuantizerFeedbackMaxAttempts = 2

type interFrameEncodeAttempt struct {
	Config         vp8enc.InterFrameStateConfig
	FrameCoefProbs vp8tables.CoefficientProbs
	FrameMVProbs   [2][vp8tables.MVPCount]uint8
	RefFrame       vp8common.MVReferenceFrame
	Ref            *vp8common.Image
	Size           int
	ZeroReference  bool
}

func NewVP8Encoder(opts EncoderOptions) (*VP8Encoder, error) {
	normalized, timing, err := normalizeEncoderOptions(opts)
	if err != nil {
		return nil, err
	}

	cfg := defaultRateControlConfig(normalized)
	e := &VP8Encoder{
		opts:            normalized,
		timing:          timing,
		keyFrameModes:   make([]vp8enc.KeyFrameMacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		interFrameModes: make([]vp8enc.InterFrameMacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		keyFrameCoeffs:  make([]vp8enc.MacroblockCoefficients, encoderMacroblockCount(normalized.Width, normalized.Height)),
		tokenAbove:      make([]vp8enc.TokenContextPlanes, encoderMacroblockCols(normalized.Width)),

		reconstructModes:  make([]vp8dec.MacroblockMode, encoderMacroblockCount(normalized.Width, normalized.Height)),
		reconstructTokens: make([]vp8dec.MacroblockTokens, encoderMacroblockCount(normalized.Width, normalized.Height)),
		coefProbs:         vp8tables.DefaultCoefProbs,
	}
	vp8dec.ResetModeProbs(&e.modeProbs)
	if err := e.initReferenceFrames(normalized.Width, normalized.Height); err != nil {
		return nil, err
	}
	if err := e.rc.applyConfig(cfg, timing); err != nil {
		return nil, err
	}
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
	} else {
		e.rc.currentQuantizer = e.rc.minQuantizer
	}
	e.rc.lastQuantizer = e.rc.currentQuantizer
	e.opts.CQLevel = e.rc.cqLevel
	if err := e.temporal.configure(normalized.TemporalScalability, e.rc.targetBitrateKbps); err != nil {
		return nil, err
	}
	e.opts.TemporalScalability = e.temporal.config
	return e, nil
}

func (e *VP8Encoder) EncodeInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error) {
	if e == nil || e.closed {
		return EncodeResult{}, ErrClosed
	}
	if !src.validForEncode(e.opts.Width, e.opts.Height) {
		return EncodeResult{}, ErrInvalidConfig
	}
	if len(dst) == 0 {
		return EncodeResult{}, ErrBufferTooSmall
	}

	temporalFrame := e.temporal.nextFrame(e.timing)
	flags |= temporalFrame.Flags
	forcedKeyFrame := e.forceKeyFrameRequested(flags)
	keyFrame := e.shouldEncodeKeyFrame(src, flags)
	temporalReferenceControl := temporalFrame.Enabled && temporalFrame.LayerCount > 1
	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	required := rows * cols
	goldenCBRRefresh := e.shouldRefreshGoldenFrameCBR(keyFrame, temporalReferenceControl, flags, rows, cols)
	if temporalFrame.Enabled && !keyFrame {
		e.rc.beginFrameWithTarget(false, temporalFrame.LayerFrameTargetBits)
	} else {
		e.rc.beginFrameWithTargetAndContext(keyFrame, e.rc.bitsPerFrame, rateControlFrameContext{
			firstFrame:         e.frameCount == 0,
			forcedKeyFrame:     forcedKeyFrame,
			temporalLayerCount: temporalFrame.LayerCount,
			timing:             e.timing,
		})
	}
	if goldenCBRRefresh {
		e.rc.frameTargetBits = boostedFrameTargetBits(e.rc.frameTargetBits, e.rc.gfCBRBoostPct)
	}
	e.rc.selectQuantizerForFrameKind(keyFrame, goldenCBRRefresh, required)

	result := EncodeResult{
		KeyFrame:                       keyFrame,
		PTS:                            pts,
		Duration:                       duration,
		Quantizer:                      e.rc.currentQuantizer,
		TargetBitrateKbps:              e.rc.targetBitrateKbps,
		FrameTargetBits:                e.rc.frameTargetBits,
		BufferLevelBits:                e.rc.bufferLevelBits,
		TemporalLayerID:                temporalFrame.LayerID,
		TemporalLayerCount:             temporalFrame.LayerCount,
		TemporalLayerSync:              temporalFrame.LayerSync,
		TL0PICIDX:                      temporalFrame.TL0PICIDX,
		TemporalLayerTargetBitrateKbps: temporalFrame.LayerTargetBitrateKbps,
	}
	invisible := flags&EncodeInvisibleFrame != 0
	if !keyFrame && !invisible && e.rc.shouldDropInterFrame() {
		e.rc.postDropFrame()
		result.Dropped = true
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
		e.temporal.finishDroppedFrame()
		e.frameCount++
		return result, nil
	}

	source := vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
	if !keyFrame {
		attempt, err := e.encodeInterFrameWithQuantizerFeedback(dst, source, rows, cols, required, flags, temporalReferenceControl, goldenCBRRefresh)
		if err != nil {
			return EncodeResult{}, err
		}
		finalQuantizer := e.rc.currentQuantizer
		e.commitInterFrameAttempt(attempt)
		result.Data = dst[:attempt.Size]
		result.SizeBytes = attempt.Size
		result.Quantizer = finalQuantizer
		result.Droppable = interFrameDroppable(attempt.Config)
		e.rc.postEncodeFrameWithContext(attempt.Size, false, goldenCBRRefresh, required)
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
		if attempt.Config.Segmentation.Enabled {
			e.advanceCyclicRefresh(rows, cols)
		}
		e.lastInterZeroMVCount = countLastZeroMVInterFrameModes(e.interFrameModes[:required])
		e.temporal.finishFrame(temporalFrame, false, temporalReferenceRefresh{
			Last:   attempt.Config.RefreshLast,
			Golden: attempt.Config.RefreshGolden,
			AltRef: attempt.Config.RefreshAltRef,
		})
		e.frameCount++
		return result, nil
	}

	n, err := e.encodeKeyFrameWithQuantizerFeedback(dst, source, rows, cols, required, invisible)
	if err != nil {
		return EncodeResult{}, err
	}
	finalQuantizer := e.rc.currentQuantizer
	e.refreshKeyFrameReferencesFromAnalysis()
	result.Data = dst[:n]
	result.SizeBytes = n
	result.Quantizer = finalQuantizer
	e.rc.postEncodeFrameWithContext(n, true, false, required)
	result.BufferLevelBits = e.rc.bufferLevelBits
	e.forceKeyFrame = false
	e.cyclicRefreshIndex = 0
	e.lastInterZeroMVCount = 0
	e.temporal.finishFrame(temporalFrame, true, temporalReferenceRefresh{Last: true, Golden: true, AltRef: true})
	e.frameCount++
	return result, nil
}

func (e *VP8Encoder) encodeInterFrame(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags) (int, error) {
	attempt, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags, false, false)
	if err != nil {
		return 0, err
	}
	e.commitInterFrameAttempt(attempt)
	return attempt.Size, nil
}

func (e *VP8Encoder) encodeKeyFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, invisible bool) (int, error) {
	for attempt := 0; ; attempt++ {
		n, err := e.encodeKeyFrameAttempt(dst, source, rows, cols, required, invisible)
		if err != nil {
			return 0, err
		}
		if attempt+1 >= encoderQuantizerFeedbackMaxAttempts || !e.updateQuantizerForEncodedFrameSize(n, true, false) {
			return n, nil
		}
	}
}

func (e *VP8Encoder) encodeKeyFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, invisible bool) (int, error) {
	if len(e.keyFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return 0, ErrInvalidConfig
	}
	segmentation := e.staticSegmentationConfig()
	var err error
	if segmentation.Enabled {
		assignKeyFrameStaticSegments(rows, cols, e.keyFrameModes[:required])
		err = e.buildReconstructingKeyFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	} else {
		err = e.buildReconstructingKeyFrameCoefficients(source, e.rc.currentQuantizer, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	}
	if err != nil {
		return 0, translateEncoderError(err)
	}
	lfLevel, lfSharpness := e.encoderLoopFilter(vp8common.KeyFrame)
	if err := e.applyReconstructionLoopFilter(vp8common.KeyFrame, lfLevel, lfSharpness, rows, cols, required); err != nil {
		return 0, err
	}

	cfg := vp8enc.KeyFrameStateConfig{
		InvisibleFrame:      invisible,
		TokenPartition:      vp8common.TokenPartition(e.opts.TokenPartitions),
		BaseQIndex:          uint8(e.rc.currentQuantizer),
		LoopFilterLevel:     lfLevel,
		SharpnessLevel:      lfSharpness,
		Segmentation:        segmentation,
		RefreshEntropyProbs: !e.opts.ErrorResilient,
	}
	n, frameCoefProbs, err := vp8enc.WriteCoefficientKeyFrameWithProbabilityBase(dst, e.opts.Width, e.opts.Height, cfg, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &vp8tables.DefaultCoefProbs)
	if err != nil {
		return 0, translateEncoderError(err)
	}
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	if cfg.RefreshEntropyProbs {
		e.coefProbs = frameCoefProbs
	}
	return n, nil
}

func (e *VP8Encoder) encodeInterFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool) (interFrameEncodeAttempt, error) {
	for attempt := 0; ; attempt++ {
		result, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags, temporalActive, goldenCBRRefresh)
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
		if attempt+1 >= encoderQuantizerFeedbackMaxAttempts || !e.updateQuantizerForEncodedFrameSize(result.Size, false, goldenCBRRefresh) {
			return result, nil
		}
	}
}

func (e *VP8Encoder) encodeInterFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags, temporalActive bool, goldenCBRRefresh bool) (interFrameEncodeAttempt, error) {
	cfg := vp8enc.DefaultInterFrameStateConfig(uint8(e.rc.currentQuantizer))
	cfg.InvisibleFrame = flags&EncodeInvisibleFrame != 0
	cfg.TokenPartition = vp8common.TokenPartition(e.opts.TokenPartitions)
	cfg.LoopFilterLevel, cfg.SharpnessLevel = e.encoderLoopFilter(vp8common.InterFrame)
	cfg.RefreshEntropyProbs = flags&EncodeNoUpdateEntropy == 0 && !e.opts.ErrorResilient
	cfg.RefreshLast = flags&EncodeNoUpdateLast == 0
	// Match libvpx's normal interframe shape: LAST advances by default while
	// golden/altref remain long-lived references unless a future policy updates them.
	cfg.RefreshGolden = false
	cfg.RefreshAltRef = false
	if temporalActive {
		cfg.RefreshGolden = flags&EncodeNoUpdateGolden == 0
		cfg.RefreshAltRef = flags&EncodeNoUpdateAltRef == 0
	} else if goldenCBRRefresh {
		cfg.RefreshGolden = true
	}
	segmentation := e.staticSegmentationConfig()
	if segmentation.Enabled {
		cfg.Segmentation = segmentation
	}
	if cfg.LoopFilterLevel == 0 && !segmentation.Enabled {
		refFrame, ref, ok := e.matchingZeroInterFrameReference(source, flags)
		if ok {
			if len(e.interFrameModes) < required {
				return interFrameEncodeAttempt{}, ErrInvalidConfig
			}
			fillZeroInterFrameModes(e.interFrameModes[:required], refFrame)
			n, err := vp8enc.WriteZeroReferenceInterFrame(dst, e.opts.Width, e.opts.Height, cfg, refFrame)
			if err != nil {
				return interFrameEncodeAttempt{}, translateEncoderError(err)
			}
			return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: e.coefProbs, FrameMVProbs: e.modeProbs.MV, RefFrame: refFrame, Ref: ref, Size: n, ZeroReference: true}, nil
		}
	}
	if len(e.interFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return interFrameEncodeAttempt{}, ErrInvalidConfig
	}
	var err error
	if segmentation.Enabled {
		assignInterFrameStaticSegments(rows, cols, e.cyclicRefreshIndex, e.interFrameModes[:required])
		err = e.buildReconstructingInterFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
	} else {
		err = e.buildReconstructingInterFrameCoefficients(source, e.rc.currentQuantizer, e.interFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols, flags)
	}
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	if err := e.applyReconstructionLoopFilter(vp8common.InterFrame, cfg.LoopFilterLevel, cfg.SharpnessLevel, rows, cols, required); err != nil {
		return interFrameEncodeAttempt{}, err
	}
	n, frameCoefProbs, frameMVProbs, err := vp8enc.WriteCoefficientInterFrameWithProbabilityBase(dst, e.opts.Width, e.opts.Height, cfg, e.interFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols], &e.coefProbs, e.modeProbs.MV)
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	return interFrameEncodeAttempt{Config: cfg, FrameCoefProbs: frameCoefProbs, FrameMVProbs: frameMVProbs, Size: n}, nil
}

func (e *VP8Encoder) updateQuantizerForEncodedFrameSize(sizeBytes int, keyFrame bool, goldenFrame bool) bool {
	next := e.rc.frameSizeFeedbackQuantizerWithContext(sizeBytes, keyFrame, goldenFrame)
	if next == e.rc.currentQuantizer {
		return false
	}
	e.rc.currentQuantizer = next
	return true
}

func (e *VP8Encoder) commitInterFrameAttempt(attempt interFrameEncodeAttempt) {
	e.commitInterFrameEntropy(attempt)
	if attempt.ZeroReference {
		e.refreshZeroInterFrameReferences(attempt.Config, attempt.Ref, attempt.RefFrame)
		return
	}
	e.refreshInterFrameReferencesFromAnalysis(attempt.Config)
}

func (e *VP8Encoder) commitInterFrameEntropy(attempt interFrameEncodeAttempt) {
	if !attempt.Config.RefreshEntropyProbs {
		return
	}
	e.coefProbs = attempt.FrameCoefProbs
	e.modeProbs.MV = attempt.FrameMVProbs
}

func interFrameDroppable(cfg vp8enc.InterFrameStateConfig) bool {
	if cfg.RefreshLast || cfg.RefreshGolden || cfg.RefreshAltRef ||
		cfg.CopyBufferToGolden != 0 || cfg.CopyBufferToAltRef != 0 ||
		cfg.RefreshEntropyProbs {
		return false
	}
	if cfg.Segmentation.Enabled && (cfg.Segmentation.UpdateMap || cfg.Segmentation.UpdateData) {
		return false
	}
	return true
}

func (e *VP8Encoder) matchingZeroInterFrameReference(source vp8enc.SourceImage, flags EncodeFlags) (vp8common.MVReferenceFrame, *vp8common.Image, bool) {
	if flags&EncodeNoReferenceLast == 0 && sourceImageMatchesReference(source, &e.lastRef.Img) {
		return vp8common.LastFrame, &e.lastRef.Img, true
	}
	if flags&EncodeNoReferenceGolden == 0 && sourceImageMatchesReference(source, &e.goldenRef.Img) {
		return vp8common.GoldenFrame, &e.goldenRef.Img, true
	}
	if flags&EncodeNoReferenceAltRef == 0 && sourceImageMatchesReference(source, &e.altRef.Img) {
		return vp8common.AltRefFrame, &e.altRef.Img, true
	}
	return vp8common.IntraFrame, nil, false
}

func fillZeroInterFrameModes(modes []vp8enc.InterFrameMacroblockMode, refFrame vp8common.MVReferenceFrame) {
	for i := range modes {
		modes[i] = vp8enc.InterFrameMacroblockMode{
			MBSkipCoeff: true,
			RefFrame:    refFrame,
			Mode:        vp8common.ZeroMV,
		}
	}
}

func countLastZeroMVInterFrameModes(modes []vp8enc.InterFrameMacroblockMode) int {
	count := 0
	for _, mode := range modes {
		if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
			count++
		}
	}
	return count
}

func (e *VP8Encoder) shouldEncodeKeyFrame(src Image, flags EncodeFlags) bool {
	if e.frameCount == 0 || e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	if flags&(EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef) == EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef {
		return true
	}
	if e.opts.KeyFrameInterval > 0 && e.frameCount%uint64(e.opts.KeyFrameInterval) == 0 {
		return true
	}
	return false
}

func (e *VP8Encoder) forceKeyFrameRequested(flags EncodeFlags) bool {
	if e.forceKeyFrame || flags&EncodeForceKeyFrame != 0 {
		return true
	}
	return flags&(EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef) == EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef
}

func (e *VP8Encoder) shouldRefreshGoldenFrameCBR(keyFrame bool, temporalActive bool, flags EncodeFlags, rows int, cols int) bool {
	if keyFrame ||
		temporalActive ||
		e.opts.ErrorResilient ||
		e.rc.mode != RateControlCBR ||
		e.rc.gfCBRBoostPct <= 0 ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateGolden) != 0 {
		return false
	}
	if required := rows * cols; required <= 0 || e.lastInterZeroMVCount <= required/2 {
		return false
	}
	interval := e.goldenFrameCBRInterval(rows, cols)
	return interval > 0 && e.rc.framesSinceKeyframe > 0 && e.rc.framesSinceKeyframe%interval == 0
}

func (e *VP8Encoder) goldenFrameCBRInterval(rows int, cols int) int {
	interval := 10
	refreshCount := cyclicRefreshMaxMBsPerFrame(rows, cols)
	if refreshCount > 0 {
		interval = (2 * rows * cols) / refreshCount
	}
	if interval < 6 {
		return 6
	}
	if interval > 40 {
		return 40
	}
	return interval
}

func (e *VP8Encoder) SetBitrateKbps(kbps int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(kbps, e.timing); err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetRateControl(cfg RateControlConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextRC := e.rc
	if err := nextRC.applyConfig(cfg, e.timing); err != nil {
		return err
	}
	nextTemporal := e.temporal
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.RateControlMode = cfg.Mode
	e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
	e.opts.MinBitrateKbps = cfg.MinBitrateKbps
	e.opts.MaxBitrateKbps = cfg.MaxBitrateKbps
	e.opts.MinQuantizer = cfg.MinQuantizer
	e.opts.MaxQuantizer = cfg.MaxQuantizer
	e.opts.CQLevel = nextRC.cqLevel
	e.opts.UndershootPct = cfg.UndershootPct
	e.opts.OvershootPct = cfg.OvershootPct
	e.opts.BufferSizeMs = cfg.BufferSizeMs
	e.opts.BufferInitialSizeMs = cfg.BufferInitialSizeMs
	e.opts.BufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	e.opts.DropFrameAllowed = cfg.DropFrameAllowed
	e.opts.MaxIntraBitratePct = cfg.MaxIntraBitratePct
	e.opts.GFCBRBoostPct = cfg.GFCBRBoostPct
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetCQLevel(level int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if level < 0 || level > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if e.rc.mode == RateControlCQ && (level < e.rc.minQuantizer || level > e.rc.maxQuantizer) {
		return ErrInvalidQuantizer
	}
	e.rc.cqLevel = level
	e.opts.CQLevel = level
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = level
		e.rc.lastQuantizer = level
	}
	return nil
}

func (e *VP8Encoder) SetMaxIntraBitratePct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.maxIntraBitratePct = pct
	e.opts.MaxIntraBitratePct = pct
	return nil
}

func (e *VP8Encoder) SetGFCBRBoostPct(pct int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if pct < 0 {
		return ErrInvalidConfig
	}
	e.rc.gfCBRBoostPct = pct
	e.opts.GFCBRBoostPct = pct
	return nil
}

func (e *VP8Encoder) SetTokenPartitions(partitions int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if partitions < int(vp8common.OnePartition) || partitions > int(vp8common.EightPartition) {
		return ErrInvalidConfig
	}
	e.opts.TokenPartitions = partitions
	return nil
}

func (e *VP8Encoder) SetSharpness(sharpness int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if sharpness < 0 || sharpness > 7 {
		return ErrInvalidConfig
	}
	e.opts.Sharpness = sharpness
	return nil
}

func (e *VP8Encoder) SetStaticThreshold(threshold int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if threshold < 0 {
		return ErrInvalidConfig
	}
	e.opts.StaticThreshold = threshold
	return nil
}

func (e *VP8Encoder) SetRealtimeTarget(target RealtimeTarget) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if target.BitrateKbps < 0 || target.FPS < 0 || target.Width < 0 || target.Height < 0 {
		return ErrInvalidConfig
	}
	if target.MinQuantizer < 0 || target.MaxQuantizer < 0 || target.MinQuantizer > maxQuantizer || target.MaxQuantizer > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if target.MinQuantizer > 0 && target.MaxQuantizer > 0 && target.MinQuantizer > target.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if target.Width > 0 || target.Height > 0 {
		if target.Width <= 0 || target.Height <= 0 || !validDimension(target.Width) || !validDimension(target.Height) {
			return ErrInvalidConfig
		}
		if target.Width != e.opts.Width || target.Height != e.opts.Height {
			return ErrInvalidConfig
		}
		e.opts.Width = target.Width
		e.opts.Height = target.Height
	}
	if target.FPS > 0 {
		e.opts.FPS = target.FPS
		e.opts.TimebaseNum = 1
		e.opts.TimebaseDen = target.FPS
		e.timing = timingState{timebaseNum: 1, timebaseDen: target.FPS, frameDuration: 1}
	}
	nextMinQuantizer := e.rc.minQuantizer
	nextMaxQuantizer := e.rc.maxQuantizer
	if target.MinQuantizer != 0 {
		nextMinQuantizer = target.MinQuantizer
	}
	if target.MaxQuantizer != 0 {
		nextMaxQuantizer = target.MaxQuantizer
	}
	if nextMinQuantizer > nextMaxQuantizer {
		return ErrInvalidQuantizer
	}
	if e.rc.mode == RateControlCQ && (e.rc.cqLevel < nextMinQuantizer || e.rc.cqLevel > nextMaxQuantizer) {
		return ErrInvalidQuantizer
	}
	e.rc.minQuantizer = nextMinQuantizer
	e.rc.maxQuantizer = nextMaxQuantizer
	e.rc.clampQuantizer()
	if e.rc.mode == RateControlCQ {
		e.rc.currentQuantizer = e.rc.cqLevel
		e.rc.lastQuantizer = e.rc.cqLevel
	}
	e.rc.dropFrameAllowed = target.AllowFrameDrop
	nextTemporal := e.temporal
	if target.BitrateKbps > 0 {
		nextRC := e.rc
		if err := nextRC.setBitrateKbps(target.BitrateKbps, e.timing); err != nil {
			return err
		}
		if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
			return err
		}
		e.rc = nextRC
		e.temporal = nextTemporal
		e.opts.TargetBitrateKbps = nextRC.targetBitrateKbps
		e.opts.TemporalScalability = nextTemporal.config
		return nil
	}
	nextRC := e.rc
	if err := nextRC.setBitrateKbps(e.rc.targetBitrateKbps, e.timing); err != nil {
		return err
	}
	if err := nextTemporal.refreshBitrate(nextRC.targetBitrateKbps); err != nil {
		return err
	}
	e.rc = nextRC
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetTemporalScalability(cfg TemporalScalabilityConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	nextTemporal := temporalState{}
	if err := nextTemporal.configure(cfg, e.rc.targetBitrateKbps); err != nil {
		return err
	}
	e.temporal = nextTemporal
	e.opts.TemporalScalability = nextTemporal.config
	return nil
}

func (e *VP8Encoder) SetDeadline(deadline Deadline) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if deadline < DeadlineBestQuality || deadline > DeadlineRealtime {
		return ErrInvalidConfig
	}
	e.opts.Deadline = deadline
	return nil
}

func (e *VP8Encoder) SetCPUUsed(cpuUsed int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if cpuUsed < -16 || cpuUsed > 16 {
		return ErrInvalidConfig
	}
	e.opts.CpuUsed = cpuUsed
	return nil
}

func (e *VP8Encoder) SetKeyFrameInterval(frames int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if frames < 0 {
		return ErrInvalidConfig
	}
	e.opts.KeyFrameInterval = frames
	return nil
}

func (e *VP8Encoder) ForceKeyFrame() {
	if e == nil || e.closed {
		return
	}
	e.forceKeyFrame = true
}

func (e *VP8Encoder) Reset() {
	if e == nil {
		return
	}
	e.forceKeyFrame = false
	e.frameCount = 0
	e.cyclicRefreshIndex = 0
	e.lastInterZeroMVCount = 0
	e.rc.framesSinceKeyframe = 0
	e.rc.rollingActualBits = 0
	e.rc.rollingTargetBits = 0
	e.rc.bufferLevelBits = e.rc.bufferInitialBits
	e.rc.frameDropPressure = 0
	e.rc.avgFrameQuantizer = e.rc.maxQuantizer
	e.rc.normalInterQuantizerTotal = 0
	e.rc.normalInterFrames = 0
	e.rc.normalInterAvgQuantizer = e.rc.maxQuantizer
	e.temporal.frameIndex = 0
	e.temporal.tl0PicIdx = 0
	e.temporal.tl0Valid = false
	e.temporal.refLayer = [temporalReferenceCount]int{}
	e.coefProbs = vp8tables.DefaultCoefProbs
	vp8dec.ResetModeProbs(&e.modeProbs)
	e.current.Reset()
	e.analysis.Reset()
	e.lastRef.Reset()
	e.goldenRef.Reset()
	e.altRef.Reset()
}

func (e *VP8Encoder) Close() error {
	if e == nil || e.closed {
		return ErrClosed
	}
	e.Reset()
	e.closed = true
	return nil
}

func normalizeEncoderOptions(opts EncoderOptions) (EncoderOptions, timingState, error) {
	if !validDimension(opts.Width) || !validDimension(opts.Height) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.Threads < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.FPS < 0 || opts.TimebaseNum < 0 || opts.TimebaseDen < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.TimebaseNum == 0 && opts.TimebaseDen != 0 || opts.TimebaseNum != 0 && opts.TimebaseDen == 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.FPS == 0 && opts.TimebaseNum == 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.RateControlMode < RateControlVBR || opts.RateControlMode > RateControlCQ {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.TargetBitrateKbps <= 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidBitrate
	}
	if opts.MaxIntraBitratePct < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.GFCBRBoostPct < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.MinQuantizer > maxQuantizer || opts.MaxQuantizer > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.MinQuantizer > opts.MaxQuantizer && opts.MaxQuantizer != 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.CQLevel < 0 || opts.CQLevel > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.Deadline < DeadlineBestQuality || opts.Deadline > DeadlineRealtime {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.CpuUsed < -16 || opts.CpuUsed > 16 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.KeyFrameInterval < 0 || opts.TokenPartitions < int(vp8common.OnePartition) || opts.TokenPartitions > int(vp8common.EightPartition) {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}
	if opts.Sharpness < 0 || opts.Sharpness > 7 || opts.StaticThreshold < 0 {
		return EncoderOptions{}, timingState{}, ErrInvalidConfig
	}

	timing := timingState{frameDuration: 1}
	if opts.TimebaseNum > 0 {
		timing.timebaseNum = opts.TimebaseNum
		timing.timebaseDen = opts.TimebaseDen
	} else {
		timing.timebaseNum = 1
		timing.timebaseDen = opts.FPS
		opts.TimebaseNum = 1
		opts.TimebaseDen = opts.FPS
	}
	if opts.FPS == 0 && timing.timebaseNum == 1 {
		opts.FPS = timing.timebaseDen
	}
	if opts.KeyFrameInterval == 0 {
		opts.KeyFrameInterval = 120
	}
	return opts, timing, nil
}

func validDimension(v int) bool {
	return v > 0 && v <= maxVP8Dimension
}

func translateEncoderError(err error) error {
	switch {
	case errors.Is(err, vp8enc.ErrBufferTooSmall):
		return ErrBufferTooSmall
	case errors.Is(err, vp8enc.ErrInvalidPacketConfig), errors.Is(err, vp8enc.ErrModeBufferTooSmall):
		return ErrInvalidConfig
	default:
		return err
	}
}

func (e *VP8Encoder) initReferenceFrames(width int, height int) error {
	if err := e.current.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.analysis.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.lastRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.goldenRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.altRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP8Encoder) encoderLoopFilter(frameType vp8common.FrameType) (uint8, uint8) {
	level := libvpxInitialLoopFilterLevel(e.rc.currentQuantizer)
	if level > 63 {
		level = 63
	}
	sharpness := e.opts.Sharpness
	if frameType == vp8common.KeyFrame {
		sharpness = 0
	}
	return uint8(level), uint8(sharpness)
}

func libvpxInitialLoopFilterLevel(qIndex int) int {
	if qIndex <= 0 {
		return 0
	}
	level := qIndex * 3 / 8
	if level > 63 {
		return 63
	}
	return level
}

func (e *VP8Encoder) applyReconstructionLoopFilter(frameType vp8common.FrameType, level uint8, sharpness uint8, rows int, cols int, required int) error {
	if level == 0 {
		return nil
	}
	if len(e.reconstructModes) < required {
		return ErrInvalidConfig
	}
	header := vp8dec.LoopFilterHeader{Level: level, SharpnessLevel: sharpness}
	if err := vp8dec.ApplyLoopFilter(&e.analysis.Img, rows, cols, e.reconstructModes[:required], frameType, header, vp8dec.SegmentationHeader{}, &e.loopInfo); err != nil {
		return ErrInvalidConfig
	}
	e.analysis.ExtendBorders()
	return nil
}

func (e *VP8Encoder) refreshKeyFrameReferencesFromAnalysis() {
	copyFrameImage(&e.current.Img, &e.analysis.Img)
	e.current.ExtendBorders()
	copyFrameImage(&e.lastRef.Img, &e.current.Img)
	e.lastRef.ExtendBorders()
	copyFrameImage(&e.goldenRef.Img, &e.current.Img)
	e.goldenRef.ExtendBorders()
	copyFrameImage(&e.altRef.Img, &e.current.Img)
	e.altRef.ExtendBorders()
}

func (e *VP8Encoder) refreshZeroInterFrameReferences(cfg vp8enc.InterFrameStateConfig, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame) {
	copyFrameImage(&e.current.Img, ref)
	e.current.ExtendBorders()
	if cfg.RefreshLast && refFrame != vp8common.LastFrame {
		copyFrameImage(&e.lastRef.Img, &e.current.Img)
		e.lastRef.ExtendBorders()
	}
	if cfg.RefreshGolden && refFrame != vp8common.GoldenFrame {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
	}
	if cfg.RefreshAltRef && refFrame != vp8common.AltRefFrame {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
	}
}

func (e *VP8Encoder) refreshInterFrameReferencesFromAnalysis(cfg vp8enc.InterFrameStateConfig) {
	copyFrameImage(&e.current.Img, &e.analysis.Img)
	e.current.ExtendBorders()
	if cfg.RefreshLast {
		copyFrameImage(&e.lastRef.Img, &e.current.Img)
		e.lastRef.ExtendBorders()
	}
	if cfg.RefreshGolden {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
	}
	if cfg.RefreshAltRef {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
	}
}

func convertKeyFrameMode(src *vp8enc.KeyFrameMacroblockMode, dst *vp8dec.MacroblockMode) {
	*dst = vp8dec.MacroblockMode{
		SegmentID: src.SegmentID,
		RefFrame:  vp8common.IntraFrame,
		Mode:      src.YMode,
		UVMode:    src.UVMode,
		Is4x4:     src.YMode == vp8common.BPred,
		BModes:    src.BModes,
	}
}

func convertInterFrameMode(src *vp8enc.InterFrameMacroblockMode, dst *vp8dec.MacroblockMode) {
	*dst = vp8dec.MacroblockMode{
		SegmentID:   src.SegmentID,
		RefFrame:    convertInterFrameReference(src),
		Mode:        src.Mode,
		UVMode:      src.UVMode,
		Is4x4:       interFrameModeUses4x4Tokens(src.Mode),
		BModes:      src.BModes,
		MV:          vp8dec.MotionVector{Row: src.MV.Row, Col: src.MV.Col},
		MBSkipCoeff: src.MBSkipCoeff,
		Partition:   src.Partition,
	}
	for i := range src.BlockMV {
		dst.BlockMV[i] = vp8dec.MotionVector{Row: src.BlockMV[i].Row, Col: src.BlockMV[i].Col}
	}
}

func convertInterFrameReference(mode *vp8enc.InterFrameMacroblockMode) vp8common.MVReferenceFrame {
	if mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return vp8common.IntraFrame
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return vp8common.LastFrame
	}
	return mode.RefFrame
}

func convertMacroblockCoefficients(src *vp8enc.MacroblockCoefficients, is4x4 bool, dst *vp8dec.MacroblockTokens) {
	dst.EOB = [25]uint8{}
	if !is4x4 {
		eob := src.EOB[24]
		dst.EOB[24] = eob
		copyQCoeffForEOB(&src.QCoeff[24], eob, &dst.QCoeff[24])
		for i := 0; i < 16; i++ {
			eob := src.EOB[i]
			if eob < 1 {
				eob = 1
			}
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	} else {
		for i := 0; i < 16; i++ {
			eob := src.EOB[i]
			dst.EOB[i] = eob
			copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
		}
	}
	for i := 16; i < 24; i++ {
		eob := src.EOB[i]
		dst.EOB[i] = eob
		copyQCoeffForEOB(&src.QCoeff[i], eob, &dst.QCoeff[i])
	}
}

func interFrameModeUses4x4Tokens(mode vp8common.MBPredictionMode) bool {
	return mode == vp8common.BPred || mode == vp8common.SplitMV
}

func copyQCoeffForEOB(src *[16]int16, eob uint8, dst *[16]int16) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dst[0] = src[0]
		return
	}
	*dst = *src
}

func encoderMacroblockCount(width int, height int) int {
	return encoderMacroblockRows(height) * encoderMacroblockCols(width)
}

func encoderMacroblockRows(height int) int {
	return (height + 15) >> 4
}

func encoderMacroblockCols(width int) int {
	return (width + 15) >> 4
}
