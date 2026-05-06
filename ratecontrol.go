package govpx

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
	CQLevel      int

	UndershootPct int
	OvershootPct  int

	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int

	DropFrameAllowed bool

	MaxIntraBitratePct int
	GFCBRBoostPct      int
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
	cqLevel          int
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

	maxIntraBitratePct int
	gfCBRBoostPct      int
}

const (
	keyFrameTargetBoost             = 4
	defaultRateControlUndershootPct = 50
	defaultRateControlOvershootPct  = 100
	defaultCQLevel                  = 10
	libvpxBPerMBNormBits            = 9
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
	rc.cqLevel = normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer)
	rc.undershootPct = normalizeRateControlPct(cfg.UndershootPct, defaultRateControlUndershootPct)
	rc.overshootPct = normalizeRateControlPct(cfg.OvershootPct, defaultRateControlOvershootPct)
	rc.bufferSizeMs = cfg.BufferSizeMs
	rc.bufferInitialSizeMs = cfg.BufferInitialSizeMs
	rc.bufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	rc.dropFrameAllowed = cfg.DropFrameAllowed
	rc.maxIntraBitratePct = cfg.MaxIntraBitratePct
	rc.gfCBRBoostPct = cfg.GFCBRBoostPct
	if err := rc.setBitrateKbps(cfg.TargetBitrateKbps, timing); err != nil {
		return err
	}
	if rc.mode == RateControlCQ {
		rc.currentQuantizer = rc.cqLevel
		rc.lastQuantizer = rc.cqLevel
	}
	return nil
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
	rc.beginFrameWithTarget(keyFrame, rc.bitsPerFrame)
}

func (rc *rateControlState) beginFrameWithTarget(keyFrame bool, baseTargetBits int) {
	targetBits := baseTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	baseFrameTargetBits := targetBits
	if keyFrame {
		if targetBits > maxInt()/keyFrameTargetBoost {
			targetBits = maxInt()
		} else {
			targetBits *= keyFrameTargetBoost
		}
		if rc.maxIntraBitratePct > 0 {
			maxIntraBits := percentOf(baseFrameTargetBits, rc.maxIntraBitratePct)
			if maxIntraBits <= 0 {
				maxIntraBits = 1
			}
			if targetBits > maxIntraBits {
				targetBits = maxIntraBits
			}
		}
	}
	if rc.mode == RateControlCBR && rc.rollingTargetBits > 0 {
		targetBits = rc.bufferAdjustedFrameTargetBits(targetBits)
		rc.adjustQuantizerForBuffer()
	} else if rc.mode == RateControlCQ {
		if rc.currentQuantizer < rc.cqLevel {
			rc.currentQuantizer = rc.cqLevel
		}
	}
	rc.frameTargetBits = targetBits
	rc.clampQuantizer()
}

func (rc *rateControlState) selectQuantizerForFrame(keyFrame bool, macroblocks int) {
	if rc.mode != RateControlCBR || macroblocks <= 0 {
		return
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	rc.currentQuantizer = libvpxRegulatedQuantizer(keyFrame, targetBits, macroblocks, rc.minQuantizer, rc.maxQuantizer, 1.0)
	rc.clampQuantizer()
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
	if rc.mode == RateControlCQ {
		rc.adjustCQQuantizer(actualBits, targetBits)
	} else {
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
	if targetBits < rc.bitsPerFrame {
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

func (rc *rateControlState) frameSizeFeedbackQuantizer(sizeBytes int) int {
	q := rc.currentQuantizer
	if rc.mode == RateControlCQ {
		return rc.cqFrameSizeFeedbackQuantizer(sizeBytes)
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return q
	}
	actualBits := encodedSizeBits(sizeBytes)
	projectedBuffer := saturatingSub(saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame), actualBits)
	if rc.maximumBufferBits > 0 && projectedBuffer > rc.maximumBufferBits {
		projectedBuffer = rc.maximumBufferBits
	}
	if projectedBuffer < 0 {
		projectedBuffer = 0
	}
	lowBuffer := rc.bufferOptimalBits > 0 && projectedBuffer < rc.bufferOptimalBits/2
	highBuffer := rc.bufferOptimalBits > 0 && projectedBuffer > rc.bufferOptimalBits
	overshootLimit := rc.overshootLimitBits(targetBits)
	undershootLimit := rc.undershootLimitBits(targetBits)
	switch {
	case actualBits > overshootLimit || lowBuffer:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) || lowBuffer {
			step = 2
		}
		q += step
	case actualBits < undershootLimit && highBuffer:
		q--
	}
	return rc.clampedQuantizerValue(q)
}

func (rc *rateControlState) cqFrameSizeFeedbackQuantizer(sizeBytes int) int {
	q := rc.currentQuantizer
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return rc.clampedCQQuantizerValue(q)
	}
	actualBits := encodedSizeBits(sizeBytes)
	projectedBuffer := saturatingSub(saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame), actualBits)
	if rc.maximumBufferBits > 0 && projectedBuffer > rc.maximumBufferBits {
		projectedBuffer = rc.maximumBufferBits
	}
	if projectedBuffer < 0 {
		projectedBuffer = 0
	}
	lowBuffer := rc.bufferOptimalBits > 0 && projectedBuffer < rc.bufferOptimalBits/2
	highBuffer := rc.bufferOptimalBits > 0 && projectedBuffer > rc.bufferOptimalBits
	overshootLimit := rc.overshootLimitBits(targetBits)
	undershootLimit := rc.undershootLimitBits(targetBits)
	switch {
	case actualBits > overshootLimit || lowBuffer:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) || lowBuffer {
			step = 2
		}
		q += step
	case actualBits < undershootLimit && highBuffer:
		q--
	}
	return rc.clampedCQQuantizerValue(q)
}

func (rc *rateControlState) clampedQuantizerValue(q int) int {
	if q < rc.minQuantizer {
		return rc.minQuantizer
	}
	if q > rc.maxQuantizer {
		return rc.maxQuantizer
	}
	return q
}

func (rc *rateControlState) clampedCQQuantizerValue(q int) int {
	if q < rc.cqLevel {
		return rc.cqLevel
	}
	return rc.clampedQuantizerValue(q)
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

func (rc *rateControlState) adjustCQQuantizer(actualBits int, targetBits int) {
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
	rc.currentQuantizer = rc.clampedCQQuantizerValue(rc.currentQuantizer)
}

func (rc *rateControlState) bufferAdjustedFrameTargetBits(targetBits int) int {
	if targetBits <= 0 || rc.bufferOptimalBits <= 0 {
		return targetBits
	}
	lowWater := rc.bufferOptimalBits / 2
	highWater := saturatingAdd(rc.bufferOptimalBits, rc.bufferOptimalBits/2)
	switch {
	case rc.bufferLevelBits <= lowWater:
		adjusted := targetBits / 2
		if adjusted <= 0 {
			return 1
		}
		return adjusted
	case rc.bufferLevelBits > highWater:
		return saturatingAdd(targetBits, targetBits/2)
	default:
		return targetBits
	}
}

func (rc *rateControlState) adjustQuantizerForBuffer() {
	if rc.bufferOptimalBits <= 0 {
		return
	}
	lowWater := rc.bufferOptimalBits / 2
	highWater := saturatingAdd(rc.bufferOptimalBits, rc.bufferOptimalBits/2)
	switch {
	case rc.bufferLevelBits <= lowWater:
		rc.currentQuantizer += 2
	case rc.bufferLevelBits > highWater:
		rc.currentQuantizer -= 2
	case rc.bufferLevelBits > rc.bufferOptimalBits:
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
	if cfg.CQLevel < 0 || cfg.CQLevel > maxQuantizer {
		return ErrInvalidQuantizer
	}
	cqLevel := normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer)
	if cfg.Mode == RateControlCQ && (cqLevel < cfg.MinQuantizer || cqLevel > cfg.MaxQuantizer) {
		return ErrInvalidQuantizer
	}
	if cfg.UndershootPct < 0 || cfg.OvershootPct < 0 {
		return ErrInvalidConfig
	}
	if cfg.BufferSizeMs <= 0 || cfg.BufferInitialSizeMs < 0 || cfg.BufferOptimalSizeMs < 0 {
		return ErrInvalidConfig
	}
	if cfg.MaxIntraBitratePct < 0 {
		return ErrInvalidConfig
	}
	if cfg.GFCBRBoostPct < 0 {
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
		CQLevel:             opts.CQLevel,
		UndershootPct:       undershoot,
		OvershootPct:        overshoot,
		BufferSizeMs:        bufferSize,
		BufferInitialSizeMs: bufferInitial,
		BufferOptimalSizeMs: bufferOptimal,
		DropFrameAllowed:    opts.DropFrameAllowed,
		MaxIntraBitratePct:  opts.MaxIntraBitratePct,
		GFCBRBoostPct:       opts.GFCBRBoostPct,
	}
}

func boostedFrameTargetBits(baseTargetBits int, boostPct int) int {
	if baseTargetBits <= 0 || boostPct <= 0 {
		return baseTargetBits
	}
	if boostPct > (maxInt()/baseTargetBits)-100 {
		return maxInt()
	}
	return baseTargetBits * (100 + boostPct) / 100
}

func normalizedCQLevel(level int, minQuantizer int) int {
	if level == 0 && minQuantizer > 0 {
		return defaultCQLevel
	}
	return level
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

// libvpxBitsPerMB ports vp8/encoder/ratectrl.c vp8_bits_per_mb. Values are
// bits per macroblock multiplied by 1<<libvpxBPerMBNormBits.
var libvpxBitsPerMB = [2][128]int{
	{
		1125000, 900000, 750000, 642857, 562500, 500000, 450000, 450000,
		409090, 375000, 346153, 321428, 300000, 281250, 264705, 264705,
		250000, 236842, 225000, 225000, 214285, 214285, 204545, 204545,
		195652, 195652, 187500, 180000, 180000, 173076, 166666, 160714,
		155172, 150000, 145161, 140625, 136363, 132352, 128571, 125000,
		121621, 121621, 118421, 115384, 112500, 109756, 107142, 104651,
		102272, 100000, 97826, 97826, 95744, 93750, 91836, 90000,
		88235, 86538, 84905, 83333, 81818, 80357, 78947, 77586,
		76271, 75000, 73770, 72580, 71428, 70312, 69230, 68181,
		67164, 66176, 65217, 64285, 63380, 62500, 61643, 60810,
		60000, 59210, 59210, 58441, 57692, 56962, 56250, 55555,
		54878, 54216, 53571, 52941, 52325, 51724, 51136, 50561,
		49450, 48387, 47368, 46875, 45918, 45000, 44554, 44117,
		43269, 42452, 41666, 40909, 40178, 39473, 38793, 38135,
		36885, 36290, 35714, 35156, 34615, 34090, 33582, 33088,
		32608, 32142, 31468, 31034, 30405, 29801, 29220, 28662,
	},
	{
		712500, 570000, 475000, 407142, 356250, 316666, 285000, 259090,
		237500, 219230, 203571, 190000, 178125, 167647, 158333, 150000,
		142500, 135714, 129545, 123913, 118750, 114000, 109615, 105555,
		101785, 98275, 95000, 91935, 89062, 86363, 83823, 81428,
		79166, 77027, 75000, 73076, 71250, 69512, 67857, 66279,
		64772, 63333, 61956, 60638, 59375, 58163, 57000, 55882,
		54807, 53773, 52777, 51818, 50892, 50000, 49137, 47500,
		45967, 44531, 43181, 41911, 40714, 39583, 38513, 37500,
		36538, 35625, 34756, 33928, 33139, 32386, 31666, 30978,
		30319, 29687, 29081, 28500, 27941, 27403, 26886, 26388,
		25909, 25446, 25000, 24568, 23949, 23360, 22800, 22265,
		21755, 21268, 20802, 20357, 19930, 19520, 19127, 18750,
		18387, 18037, 17701, 17378, 17065, 16764, 16473, 16101,
		15745, 15405, 15079, 14766, 14467, 14179, 13902, 13636,
		13380, 13133, 12895, 12666, 12445, 12179, 11924, 11632,
		11445, 11220, 11003, 10795, 10594, 10401, 10215, 10035,
	},
}

func libvpxRegulatedQuantizer(keyFrame bool, targetBitsPerFrame int, macroblocks int, minQ int, maxQ int, correctionFactor float64) int {
	if macroblocks <= 0 || targetBitsPerFrame <= 0 {
		return clampQuantizerValue(minQ, minQ, maxQ)
	}
	if correctionFactor <= 0 {
		correctionFactor = 1.0
	}
	targetBitsPerMB := 0
	if targetBitsPerFrame > maxInt()>>libvpxBPerMBNormBits {
		temp := targetBitsPerFrame / macroblocks
		if temp > maxInt()>>libvpxBPerMBNormBits {
			targetBitsPerMB = maxInt()
		} else {
			targetBitsPerMB = temp << libvpxBPerMBNormBits
		}
	} else {
		targetBitsPerMB = (targetBitsPerFrame << libvpxBPerMBNormBits) / macroblocks
	}
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	q := maxQ
	lastError := maxInt()
	for i := minQ; i <= maxQ && i < len(libvpxBitsPerMB[frameType]); i++ {
		bitsAtQ := int(0.5 + correctionFactor*float64(libvpxBitsPerMB[frameType][i]))
		if bitsAtQ <= targetBitsPerMB {
			if targetBitsPerMB-bitsAtQ <= lastError {
				q = i
			} else {
				q = i - 1
			}
			break
		}
		lastError = bitsAtQ - targetBitsPerMB
	}
	return clampQuantizerValue(q, minQ, maxQ)
}

func clampQuantizerValue(q int, minQ int, maxQ int) int {
	if q < minQ {
		return minQ
	}
	if q > maxQ {
		return maxQ
	}
	return q
}
