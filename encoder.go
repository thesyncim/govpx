package libgopx

import (
	"errors"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
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

	UndershootPct int
	OvershootPct  int

	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int

	DropFrameAllowed bool

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

	PTS      uint64
	Duration uint64

	Quantizer int

	SizeBytes int

	TargetBitrateKbps int
	FrameTargetBits   int
	BufferLevelBits   int

	PSNRHint float64
}

type VP8Encoder struct {
	opts EncoderOptions

	timing timingState
	rc     rateControlState

	closed        bool
	forceKeyFrame bool
	frameCount    uint64

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
}

const encoderQuantizerFeedbackMaxAttempts = 2

type interFrameEncodeAttempt struct {
	Config        vp8enc.InterFrameStateConfig
	RefFrame      vp8common.MVReferenceFrame
	Ref           *vp8common.Image
	Size          int
	ZeroReference bool
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
	}
	if err := e.initReferenceFrames(normalized.Width, normalized.Height); err != nil {
		return nil, err
	}
	if err := e.rc.applyConfig(cfg, timing); err != nil {
		return nil, err
	}
	e.rc.currentQuantizer = e.rc.minQuantizer
	e.rc.lastQuantizer = e.rc.currentQuantizer
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

	keyFrame := e.shouldEncodeKeyFrame(src, flags)
	e.rc.beginFrame(keyFrame)

	result := EncodeResult{
		KeyFrame:          keyFrame,
		PTS:               pts,
		Duration:          duration,
		Quantizer:         e.rc.currentQuantizer,
		TargetBitrateKbps: e.rc.targetBitrateKbps,
		FrameTargetBits:   e.rc.frameTargetBits,
		BufferLevelBits:   e.rc.bufferLevelBits,
	}
	invisible := flags&EncodeInvisibleFrame != 0
	if !keyFrame && !invisible && e.rc.shouldDropInterFrame() {
		e.rc.postDropFrame()
		result.Dropped = true
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
		e.frameCount++
		return result, nil
	}

	rows := encoderMacroblockRows(e.opts.Height)
	cols := encoderMacroblockCols(e.opts.Width)
	required := rows * cols
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
		attempt, err := e.encodeInterFrameWithQuantizerFeedback(dst, source, rows, cols, required, flags)
		if err != nil {
			return EncodeResult{}, err
		}
		finalQuantizer := e.rc.currentQuantizer
		e.commitInterFrameAttempt(attempt)
		result.Data = dst[:attempt.Size]
		result.SizeBytes = attempt.Size
		result.Quantizer = finalQuantizer
		e.rc.postEncodeFrame(attempt.Size, false)
		result.BufferLevelBits = e.rc.bufferLevelBits
		e.forceKeyFrame = false
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
	e.rc.postEncodeFrame(n, true)
	result.BufferLevelBits = e.rc.bufferLevelBits
	e.forceKeyFrame = false
	e.frameCount++
	return result, nil
}

func (e *VP8Encoder) encodeInterFrame(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags) (int, error) {
	attempt, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags)
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
		if attempt+1 >= encoderQuantizerFeedbackMaxAttempts || !e.updateQuantizerForEncodedFrameSize(n) {
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
		assignKeyFrameStaticSegments(source, rows, cols, e.opts.StaticThreshold, e.keyFrameModes[:required])
		err = e.buildReconstructingKeyFrameCoefficientsWithSegmentation(source, e.rc.currentQuantizer, segmentation, true, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	} else {
		err = e.buildReconstructingKeyFrameCoefficients(source, e.rc.currentQuantizer, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], rows, cols)
	}
	if err != nil {
		return 0, translateEncoderError(err)
	}
	lfLevel, lfSharpness := e.encoderLoopFilter()
	if err := e.applyReconstructionLoopFilter(vp8common.KeyFrame, lfLevel, lfSharpness, rows, cols, required); err != nil {
		return 0, err
	}

	n, err := vp8enc.WriteCoefficientKeyFrame(dst, e.opts.Width, e.opts.Height, vp8enc.KeyFrameStateConfig{InvisibleFrame: invisible, TokenPartition: vp8common.TokenPartition(e.opts.TokenPartitions), BaseQIndex: uint8(e.rc.currentQuantizer), LoopFilterLevel: lfLevel, SharpnessLevel: lfSharpness, Segmentation: segmentation}, e.keyFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols])
	if err != nil {
		return 0, translateEncoderError(err)
	}
	return n, nil
}

func (e *VP8Encoder) encodeInterFrameWithQuantizerFeedback(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags) (interFrameEncodeAttempt, error) {
	for attempt := 0; ; attempt++ {
		result, err := e.encodeInterFrameAttempt(dst, source, rows, cols, required, flags)
		if err != nil {
			return interFrameEncodeAttempt{}, err
		}
		if attempt+1 >= encoderQuantizerFeedbackMaxAttempts || !e.updateQuantizerForEncodedFrameSize(result.Size) {
			return result, nil
		}
	}
}

func (e *VP8Encoder) encodeInterFrameAttempt(dst []byte, source vp8enc.SourceImage, rows int, cols int, required int, flags EncodeFlags) (interFrameEncodeAttempt, error) {
	cfg := vp8enc.DefaultInterFrameStateConfig(uint8(e.rc.currentQuantizer))
	cfg.InvisibleFrame = flags&EncodeInvisibleFrame != 0
	cfg.TokenPartition = vp8common.TokenPartition(e.opts.TokenPartitions)
	cfg.LoopFilterLevel, cfg.SharpnessLevel = e.encoderLoopFilter()
	cfg.RefreshLast = flags&EncodeNoUpdateLast == 0
	cfg.RefreshGolden = flags&EncodeNoUpdateGolden == 0
	cfg.RefreshAltRef = flags&EncodeNoUpdateAltRef == 0
	if cfg.LoopFilterLevel == 0 {
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
			return interFrameEncodeAttempt{Config: cfg, RefFrame: refFrame, Ref: ref, Size: n, ZeroReference: true}, nil
		}
	}
	if len(e.interFrameModes) < required || len(e.keyFrameCoeffs) < required || len(e.tokenAbove) < cols {
		return interFrameEncodeAttempt{}, ErrInvalidConfig
	}
	segmentation := e.staticSegmentationConfig()
	var err error
	if segmentation.Enabled {
		assignInterFrameStaticSegments(source, rows, cols, e.opts.StaticThreshold, e.interFrameModes[:required])
		cfg.Segmentation = segmentation
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
	n, err := vp8enc.WriteCoefficientInterFrame(dst, e.opts.Width, e.opts.Height, cfg, e.interFrameModes[:required], e.keyFrameCoeffs[:required], e.tokenAbove[:cols])
	if err != nil {
		return interFrameEncodeAttempt{}, translateEncoderError(err)
	}
	return interFrameEncodeAttempt{Config: cfg, Size: n}, nil
}

func (e *VP8Encoder) updateQuantizerForEncodedFrameSize(sizeBytes int) bool {
	next := e.rc.frameSizeFeedbackQuantizer(sizeBytes)
	if next == e.rc.currentQuantizer {
		return false
	}
	e.rc.currentQuantizer = next
	return true
}

func (e *VP8Encoder) commitInterFrameAttempt(attempt interFrameEncodeAttempt) {
	if attempt.ZeroReference {
		e.refreshZeroInterFrameReferences(attempt.Config, attempt.Ref, attempt.RefFrame)
		return
	}
	e.refreshInterFrameReferencesFromAnalysis(attempt.Config)
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

func (e *VP8Encoder) SetBitrateKbps(kbps int) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.rc.setBitrateKbps(kbps, e.timing)
}

func (e *VP8Encoder) SetRateControl(cfg RateControlConfig) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	return e.rc.applyConfig(cfg, e.timing)
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
	if target.MinQuantizer != 0 {
		e.rc.minQuantizer = target.MinQuantizer
	}
	if target.MaxQuantizer != 0 {
		e.rc.maxQuantizer = target.MaxQuantizer
	}
	if e.rc.minQuantizer > e.rc.maxQuantizer {
		return ErrInvalidQuantizer
	}
	e.rc.clampQuantizer()
	e.rc.dropFrameAllowed = target.AllowFrameDrop
	if target.BitrateKbps > 0 {
		return e.rc.setBitrateKbps(target.BitrateKbps, e.timing)
	}
	return e.rc.setBitrateKbps(e.rc.targetBitrateKbps, e.timing)
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
	e.rc.framesSinceKeyframe = 0
	e.rc.rollingActualBits = 0
	e.rc.rollingTargetBits = 0
	e.rc.bufferLevelBits = e.rc.bufferInitialBits
	e.rc.frameDropPressure = 0
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
	if opts.MinQuantizer < 0 || opts.MaxQuantizer < 0 || opts.MinQuantizer > maxQuantizer || opts.MaxQuantizer > maxQuantizer {
		return EncoderOptions{}, timingState{}, ErrInvalidQuantizer
	}
	if opts.MinQuantizer > opts.MaxQuantizer && opts.MaxQuantizer != 0 {
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

func (e *VP8Encoder) encoderLoopFilter() (uint8, uint8) {
	if e.opts.Sharpness == 0 {
		return 0, 0
	}
	level := e.rc.currentQuantizer / 4
	if level < 1 {
		level = 1
	}
	if level > 63 {
		level = 63
	}
	return uint8(level), uint8(e.opts.Sharpness)
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
		Is4x4:       src.Mode == vp8common.BPred,
		BModes:      src.BModes,
		MV:          vp8dec.MotionVector{Row: src.MV.Row, Col: src.MV.Col},
		MBSkipCoeff: src.MBSkipCoeff,
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
