package libgopx

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
	ErrorResilient  bool
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
}

func NewVP8Encoder(opts EncoderOptions) (*VP8Encoder, error) {
	normalized, timing, err := normalizeEncoderOptions(opts)
	if err != nil {
		return nil, err
	}

	cfg := defaultRateControlConfig(normalized)
	e := &VP8Encoder{
		opts:   normalized,
		timing: timing,
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

	keyFrame := e.forceKeyFrame || flags&EncodeForceKeyFrame != 0
	if e.frameCount == 0 || e.opts.KeyFrameInterval > 0 && int(e.frameCount)%e.opts.KeyFrameInterval == 0 {
		keyFrame = true
	}

	result := EncodeResult{
		KeyFrame:          keyFrame,
		PTS:               pts,
		Duration:          duration,
		Quantizer:         e.rc.currentQuantizer,
		TargetBitrateKbps: e.rc.targetBitrateKbps,
		FrameTargetBits:   e.rc.frameTargetBits,
		BufferLevelBits:   e.rc.bufferLevelBits,
	}

	e.forceKeyFrame = false
	return result, ErrUnsupportedFeature
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
	if opts.KeyFrameInterval < 0 || opts.TokenPartitions < 0 || opts.TokenPartitions > 3 {
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
