package libgopx

type RateControlMode int

const (
	RateControlVBR RateControlMode = iota
	RateControlCBR
	RateControlCQ
)

type RateControlConfig struct {
	Mode RateControlMode

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
}

type RealtimeTarget struct {
	BitrateKbps int
	FPS         int

	Width  int
	Height int

	MinQuantizer int
	MaxQuantizer int

	AllowFrameDrop bool
}

type timingState struct {
	timebaseNum   int
	timebaseDen   int
	frameDuration int
}

type rateControlState struct {
	mode RateControlMode

	targetBitrateKbps   int
	targetBandwidthBits int
	minBitrateKbps      int
	maxBitrateKbps      int

	minQuantizer     int
	maxQuantizer     int
	currentQuantizer int
	lastQuantizer    int

	undershootPct int
	overshootPct  int

	bufferSizeMs        int
	bufferInitialSizeMs int
	bufferOptimalSizeMs int

	bufferSizeBits    int
	bufferInitialBits int
	bufferOptimalBits int
	bufferLevelBits   int
	maximumBufferBits int

	bitsPerFrame    int
	frameTargetBits int

	dropFrameAllowed  bool
	frameDropPressure int

	framesSinceKeyframe int
	rollingActualBits   int
	rollingTargetBits   int
}

const (
	keyFrameTargetBoost             = 4
	defaultRateControlUndershootPct = 50
	defaultRateControlOvershootPct  = 100
)

func (rc *rateControlState) applyConfig(cfg RateControlConfig, timing timingState) error {
	if err := validateRateControlConfig(cfg); err != nil {
		return err
	}
	rc.mode = cfg.Mode
	rc.minBitrateKbps = cfg.MinBitrateKbps
	rc.maxBitrateKbps = cfg.MaxBitrateKbps
	rc.minQuantizer = cfg.MinQuantizer
	rc.maxQuantizer = cfg.MaxQuantizer
	rc.undershootPct = normalizeRateControlPct(cfg.UndershootPct, defaultRateControlUndershootPct)
	rc.overshootPct = normalizeRateControlPct(cfg.OvershootPct, defaultRateControlOvershootPct)
	rc.bufferSizeMs = cfg.BufferSizeMs
	rc.bufferInitialSizeMs = cfg.BufferInitialSizeMs
	rc.bufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	rc.dropFrameAllowed = cfg.DropFrameAllowed
	return rc.setBitrateKbps(cfg.TargetBitrateKbps, timing)
}

func (rc *rateControlState) setBitrateKbps(kbps int, timing timingState) error {
	if kbps <= 0 {
		return ErrInvalidBitrate
	}
	if rc.minBitrateKbps > 0 && kbps < rc.minBitrateKbps {
		return ErrInvalidBitrate
	}
	if rc.maxBitrateKbps > 0 && kbps > rc.maxBitrateKbps {
		return ErrInvalidBitrate
	}
	targetBits := kbps * 1000
	if targetBits/1000 != kbps {
		return ErrInvalidBitrate
	}

	rc.targetBitrateKbps = kbps
	rc.targetBandwidthBits = targetBits
	rc.bitsPerFrame = computeBitsPerFrame(targetBits, timing)
	if rc.bitsPerFrame <= 0 {
		return ErrInvalidBitrate
	}

	var ok bool
	rc.bufferSizeBits, ok = checkedMul(kbps, rc.bufferSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.bufferInitialBits, ok = checkedMul(kbps, rc.bufferInitialSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.bufferOptimalBits, ok = checkedMul(kbps, rc.bufferOptimalSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.maximumBufferBits = rc.bufferSizeBits
	if rc.bufferLevelBits == 0 {
		rc.bufferLevelBits = rc.bufferInitialBits
	}
	rc.frameTargetBits = rc.bitsPerFrame
	rc.clampBuffer()
	rc.clampQuantizer()
	return nil
}

func (rc *rateControlState) beginFrame(keyFrame bool) {
	targetBits := rc.bitsPerFrame
	if keyFrame {
		if targetBits > maxInt()/keyFrameTargetBoost {
			targetBits = maxInt()
		} else {
			targetBits *= keyFrameTargetBoost
		}
	}
	rc.frameTargetBits = targetBits
}

func (rc *rateControlState) clampBuffer() {
	if rc.bufferLevelBits > rc.maximumBufferBits {
		rc.bufferLevelBits = rc.maximumBufferBits
	}
	if rc.bufferLevelBits < 0 {
		rc.bufferLevelBits = 0
	}
}

func (rc *rateControlState) clampQuantizer() {
	if rc.currentQuantizer < rc.minQuantizer {
		rc.currentQuantizer = rc.minQuantizer
	}
	if rc.currentQuantizer > rc.maxQuantizer {
		rc.currentQuantizer = rc.maxQuantizer
	}
	if rc.lastQuantizer < rc.minQuantizer {
		rc.lastQuantizer = rc.minQuantizer
	}
	if rc.lastQuantizer > rc.maxQuantizer {
		rc.lastQuantizer = rc.maxQuantizer
	}
}

func (rc *rateControlState) postEncodeFrame(sizeBytes int, keyFrame bool) {
	actualBits := encodedSizeBits(sizeBytes)
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	rc.rollingActualBits = saturatingAdd(rc.rollingActualBits, actualBits)
	rc.rollingTargetBits = saturatingAdd(rc.rollingTargetBits, targetBits)
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.bufferLevelBits = saturatingSub(rc.bufferLevelBits, actualBits)
	rc.clampBuffer()

	rc.lastQuantizer = rc.currentQuantizer
	if rc.mode != RateControlCQ {
		rc.adjustQuantizer(actualBits, targetBits)
	}
	rc.clampQuantizer()

	if keyFrame {
		rc.framesSinceKeyframe = 0
		return
	}
	rc.framesSinceKeyframe++
}

func (rc *rateControlState) shouldDropInterFrame() bool {
	if !rc.dropFrameAllowed || rc.mode != RateControlCBR {
		return false
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	return targetBits > 0 && rc.bufferLevelBits <= targetBits
}

func (rc *rateControlState) postDropFrame() {
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	rc.rollingTargetBits = saturatingAdd(rc.rollingTargetBits, targetBits)
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
	if rc.frameDropPressure > 0 {
		rc.frameDropPressure--
	}
	rc.framesSinceKeyframe++
}

func (rc *rateControlState) adjustQuantizer(actualBits int, targetBits int) {
	if targetBits <= 0 {
		return
	}
	lowBuffer := rc.bufferOptimalBits > 0 && rc.bufferLevelBits < rc.bufferOptimalBits/2
	highBuffer := rc.bufferOptimalBits > 0 && rc.bufferLevelBits > rc.bufferOptimalBits
	overshootLimit := rc.overshootLimitBits(targetBits)
	undershootLimit := rc.undershootLimitBits(targetBits)
	switch {
	case actualBits > overshootLimit || lowBuffer:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) || lowBuffer {
			step = 2
		}
		rc.currentQuantizer += step
	case actualBits < undershootLimit && highBuffer:
		rc.currentQuantizer--
	}
}

func (rc *rateControlState) overshootLimitBits(targetBits int) int {
	return saturatingAdd(targetBits, percentOf(targetBits, rc.overshootPct))
}

func (rc *rateControlState) undershootLimitBits(targetBits int) int {
	allowed := percentOf(targetBits, rc.undershootPct)
	if allowed >= targetBits {
		return 0
	}
	return targetBits - allowed
}

func encodedSizeBits(sizeBytes int) int {
	if sizeBytes <= 0 {
		return 0
	}
	if sizeBytes > maxInt()/8 {
		return maxInt()
	}
	return sizeBytes * 8
}

func saturatingAdd(a int, b int) int {
	if b > 0 && a > maxInt()-b {
		return maxInt()
	}
	if b < 0 && a < -maxInt()-b {
		return -maxInt()
	}
	return a + b
}

func saturatingSub(a int, b int) int {
	if b == -maxInt() {
		return saturatingAdd(a, maxInt())
	}
	return saturatingAdd(a, -b)
}

func computeBitsPerFrame(targetBandwidthBits int, timing timingState) int {
	if targetBandwidthBits <= 0 || timing.timebaseNum <= 0 || timing.timebaseDen <= 0 || timing.frameDuration <= 0 {
		return 0
	}
	num := int64(targetBandwidthBits) * int64(timing.timebaseNum) * int64(timing.frameDuration)
	den := int64(timing.timebaseDen)
	if den <= 0 {
		return 0
	}
	v := (num + den/2) / den
	if v > int64(maxInt()) {
		return 0
	}
	return int(v)
}

func validateRateControlConfig(cfg RateControlConfig) error {
	if cfg.Mode < RateControlVBR || cfg.Mode > RateControlCQ {
		return ErrInvalidConfig
	}
	if cfg.TargetBitrateKbps <= 0 {
		return ErrInvalidBitrate
	}
	if cfg.MinBitrateKbps < 0 || cfg.MaxBitrateKbps < 0 {
		return ErrInvalidBitrate
	}
	if cfg.MinBitrateKbps > 0 && cfg.MaxBitrateKbps > 0 && cfg.MinBitrateKbps > cfg.MaxBitrateKbps {
		return ErrInvalidBitrate
	}
	if cfg.TargetBitrateKbps < cfg.MinBitrateKbps {
		return ErrInvalidBitrate
	}
	if cfg.MaxBitrateKbps > 0 && cfg.TargetBitrateKbps > cfg.MaxBitrateKbps {
		return ErrInvalidBitrate
	}
	if cfg.MinQuantizer < 0 || cfg.MaxQuantizer < 0 || cfg.MinQuantizer > maxQuantizer || cfg.MaxQuantizer > maxQuantizer {
		return ErrInvalidQuantizer
	}
	if cfg.MinQuantizer > cfg.MaxQuantizer {
		return ErrInvalidQuantizer
	}
	if cfg.UndershootPct < 0 || cfg.OvershootPct < 0 {
		return ErrInvalidConfig
	}
	if cfg.BufferSizeMs <= 0 || cfg.BufferInitialSizeMs < 0 || cfg.BufferOptimalSizeMs < 0 {
		return ErrInvalidConfig
	}
	return nil
}

func defaultRateControlConfig(opts EncoderOptions) RateControlConfig {
	minQ := opts.MinQuantizer
	maxQ := opts.MaxQuantizer
	if minQ == 0 && maxQ == 0 {
		minQ = 4
		maxQ = 56
	}

	undershoot := opts.UndershootPct
	if undershoot == 0 {
		undershoot = defaultRateControlUndershootPct
	}
	overshoot := opts.OvershootPct
	if overshoot == 0 {
		overshoot = defaultRateControlOvershootPct
	}

	bufferSize := opts.BufferSizeMs
	if bufferSize == 0 {
		bufferSize = 600
	}
	bufferInitial := opts.BufferInitialSizeMs
	if bufferInitial == 0 {
		bufferInitial = 400
	}
	bufferOptimal := opts.BufferOptimalSizeMs
	if bufferOptimal == 0 {
		bufferOptimal = 500
	}

	return RateControlConfig{
		Mode:                opts.RateControlMode,
		TargetBitrateKbps:   opts.TargetBitrateKbps,
		MinBitrateKbps:      opts.MinBitrateKbps,
		MaxBitrateKbps:      opts.MaxBitrateKbps,
		MinQuantizer:        minQ,
		MaxQuantizer:        maxQ,
		UndershootPct:       undershoot,
		OvershootPct:        overshoot,
		BufferSizeMs:        bufferSize,
		BufferInitialSizeMs: bufferInitial,
		BufferOptimalSizeMs: bufferOptimal,
		DropFrameAllowed:    opts.DropFrameAllowed,
	}
}

func normalizeRateControlPct(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func percentOf(value int, pct int) int {
	if value <= 0 || pct <= 0 {
		return 0
	}
	if value > maxInt()/pct {
		return maxInt()
	}
	return (value * pct) / 100
}

func checkedMul(a int, b int) (int, bool) {
	if a < 0 || b < 0 {
		return 0, false
	}
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > maxInt()/b {
		return 0, false
	}
	return a * b, true
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
