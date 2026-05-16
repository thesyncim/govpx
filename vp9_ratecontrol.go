package govpx

type vp9RateControlState struct {
	enabled bool
	mode    RateControlMode

	targetBitrateKbps   int
	targetBandwidthBits int
	bitsPerFrame        int
	frameTargetBits     int
	minFrameBandwidth   int
	maxFrameBandwidth   int
	frameRateNum        int
	frameRateDen        int

	bufferSizeMs        int
	bufferInitialSizeMs int
	bufferOptimalSizeMs int
	bufferSizeBits      int
	bufferInitialBits   int
	bufferOptimalBits   int
	bufferLevelBits     int
	codedWidth          uint16
	codedHeight         uint16

	dropFrameAllowed    bool
	dropFramesWaterMark uint8
	decimationFactor    uint8
	decimationCount     uint8

	minBitrateKbps     int
	maxBitrateKbps     int
	undershootPct      uint8
	overshootPct       uint8
	maxIntraBitratePct int
	gfCBRBoostPct      int

	bestQuality  uint8
	worstQuality uint8
	cqLevel      uint8

	avgFrameQIndexKey   uint8
	avgFrameQIndexInter uint8
	lastQKey            uint8
	lastQInter          uint8
	lastBoostedQIndex   uint8

	totalActualBits int64
	totalTargetBits int64

	rateCorrectionFactors [vp9RateFactorLevels]float64
	dampedAdjustment      [vp9RateFactorLevels]bool
	q1Frame               uint8
	q2Frame               uint8
	rc1Frame              int8
	rc2Frame              int8

	framesSinceKey uint16
	framesTillGF   uint8

	afRatioOnePassVBR   uint8
	baselineGFInterval  uint8
	facActiveWorstInter uint16
	facActiveWorstGF    uint16
}

type vp9DropReason uint8

const (
	vp9DropNone vp9DropReason = iota
	vp9DropNegativeBuffer
	vp9DropWatermarkDecimation
)

func validateVP9RateControlOptions(opts VP9EncoderOptions) error {
	if opts.BufferSizeMs < 0 || opts.BufferInitialSizeMs < 0 ||
		opts.BufferOptimalSizeMs < 0 || opts.DropFrameWaterMark < 0 {
		return ErrInvalidConfig
	}
	if !opts.RateControlModeSet {
		if opts.BufferSizeMs != 0 || opts.BufferInitialSizeMs != 0 ||
			opts.BufferOptimalSizeMs != 0 || opts.DropFrameAllowed ||
			opts.DropFrameWaterMark != 0 {
			return ErrInvalidConfig
		}
		return nil
	}
	if !validRateControlMode(opts.RateControlMode) {
		return ErrInvalidConfig
	}
	if opts.TargetBitrateKbps <= 0 {
		return ErrInvalidBitrate
	}
	if opts.RateControlMode != RateControlCBR &&
		(opts.DropFrameAllowed || opts.DropFrameWaterMark != 0) {
		return ErrInvalidConfig
	}
	if err := validateVP9RateControlBounds(opts.MinBitrateKbps, opts.MaxBitrateKbps,
		opts.TargetBitrateKbps, opts.UndershootPct, opts.OvershootPct,
		opts.MaxIntraBitratePct, opts.GFCBRBoostPct); err != nil {
		return err
	}
	if opts.RateControlMode != RateControlCBR && opts.GFCBRBoostPct != 0 {
		return ErrInvalidConfig
	}
	return nil
}

func validateVP9RateControlConfig(cfg RateControlConfig) error {
	if !validRateControlMode(cfg.Mode) {
		return ErrInvalidConfig
	}
	if cfg.TargetBitrateKbps <= 0 {
		return ErrInvalidBitrate
	}
	if err := validateVP9RateControlBounds(cfg.MinBitrateKbps, cfg.MaxBitrateKbps,
		cfg.TargetBitrateKbps, cfg.UndershootPct, cfg.OvershootPct,
		cfg.MaxIntraBitratePct, cfg.GFCBRBoostPct); err != nil {
		return err
	}
	if cfg.BufferSizeMs < 0 || cfg.BufferInitialSizeMs < 0 ||
		cfg.BufferOptimalSizeMs < 0 || cfg.DropFrameWaterMark < 0 {
		return ErrInvalidConfig
	}
	if cfg.Mode != RateControlCBR &&
		(cfg.DropFrameAllowed || cfg.DropFrameWaterMark != 0) {
		return ErrInvalidConfig
	}
	if cfg.Mode != RateControlCBR && cfg.GFCBRBoostPct != 0 {
		return ErrInvalidConfig
	}
	return validateVP9PublicQuantizerOptions(VP9EncoderOptions{
		MinQuantizer: cfg.MinQuantizer,
		MaxQuantizer: cfg.MaxQuantizer,
		CQLevel:      cfg.CQLevel,
	})
}

// validateVP9RateControlBounds enforces the libvpx VP9 rate-control bound
// invariants shared by VP9EncoderOptions and the runtime RateControlConfig.
func validateVP9RateControlBounds(minKbps, maxKbps, targetKbps, undershootPct,
	overshootPct, maxIntraPct, gfBoostPct int) error {
	if minKbps < 0 || maxKbps < 0 {
		return ErrInvalidBitrate
	}
	if minKbps > 0 && maxKbps > 0 && minKbps > maxKbps {
		return ErrInvalidBitrate
	}
	if targetKbps > 0 && targetKbps < minKbps {
		return ErrInvalidBitrate
	}
	if maxKbps > 0 && targetKbps > maxKbps {
		return ErrInvalidBitrate
	}
	if undershootPct < 0 || undershootPct > maxRateControlUndershootPct ||
		overshootPct < 0 || overshootPct > maxRateControlOvershootPct {
		return ErrInvalidConfig
	}
	if maxIntraPct < 0 || gfBoostPct < 0 {
		return ErrInvalidConfig
	}
	return nil
}

func vp9RateControlOptionsFromConfig(opts VP9EncoderOptions, cfg RateControlConfig) (VP9EncoderOptions, error) {
	if err := validateVP9RateControlConfig(cfg); err != nil {
		return VP9EncoderOptions{}, err
	}
	opts.RateControlModeSet = true
	opts.RateControlMode = cfg.Mode
	opts.TargetBitrateKbps = cfg.TargetBitrateKbps
	opts.MinBitrateKbps = cfg.MinBitrateKbps
	opts.MaxBitrateKbps = cfg.MaxBitrateKbps
	opts.UndershootPct = cfg.UndershootPct
	opts.OvershootPct = cfg.OvershootPct
	opts.MaxIntraBitratePct = cfg.MaxIntraBitratePct
	opts.GFCBRBoostPct = cfg.GFCBRBoostPct
	opts.MinQuantizer = cfg.MinQuantizer
	opts.MaxQuantizer = cfg.MaxQuantizer
	opts.CQLevel = cfg.CQLevel
	opts.BufferSizeMs = cfg.BufferSizeMs
	opts.BufferInitialSizeMs = cfg.BufferInitialSizeMs
	opts.BufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	opts.DropFrameAllowed = cfg.DropFrameAllowed
	opts.DropFrameWaterMark = cfg.DropFrameWaterMark
	if err := validateVP9EncoderOptions(opts); err != nil {
		return VP9EncoderOptions{}, err
	}
	return opts, nil
}

func (rc *vp9RateControlState) applyOptions(opts VP9EncoderOptions, timing timingState) error {
	*rc = vp9RateControlState{}
	if !opts.RateControlModeSet {
		return nil
	}
	if !validRateControlMode(opts.RateControlMode) {
		return ErrInvalidConfig
	}
	bufferSize := opts.BufferSizeMs
	if bufferSize == 0 {
		bufferSize = libvpxDefaultBufferSizeMs
	}
	bufferInitial := opts.BufferInitialSizeMs
	if bufferInitial == 0 {
		bufferInitial = libvpxDefaultBufferInitialMs
	}
	bufferOptimal := opts.BufferOptimalSizeMs
	if bufferOptimal == 0 {
		bufferOptimal = libvpxDefaultBufferOptimalMs
	}
	if bufferSize <= 0 || bufferInitial < 0 || bufferOptimal < 0 {
		return ErrInvalidConfig
	}
	waterMark := opts.DropFrameWaterMark
	if opts.DropFrameAllowed && waterMark <= 0 {
		waterMark = defaultDropFramesWaterMark
	}
	if waterMark > 100 {
		waterMark = 100
	}

	rc.enabled = true
	rc.mode = opts.RateControlMode
	rc.bufferSizeMs = bufferSize
	rc.bufferInitialSizeMs = bufferInitial
	rc.bufferOptimalSizeMs = bufferOptimal
	if opts.RateControlMode == RateControlCBR {
		rc.dropFrameAllowed = opts.DropFrameAllowed
		rc.dropFramesWaterMark = uint8(waterMark)
	}
	rc.applyBitrateBoundsFromOptions(opts)
	rc.setFrameSize(opts.Width, opts.Height)
	rc.initQuantizerStateFromOptions(opts)
	if err := rc.setBitrateKbps(opts.TargetBitrateKbps, timing); err != nil {
		return err
	}
	rc.initOnePassVBRState(timing)
	rc.bufferLevelBits = rc.bufferInitialBits
	return nil
}

// applyBitrateBoundsFromOptions stores the libvpx VP9 rate-control bound
// settings into rc. Zero undershoot/overshoot select libvpx's VP9 default of
// 100, matching vpxenc's behavior. GFCBRBoostPct is honored only in CBR mode
// to match libvpx's VP9E_SET_GF_CBR_BOOST_PCT scope.
func (rc *vp9RateControlState) applyBitrateBoundsFromOptions(opts VP9EncoderOptions) {
	rc.minBitrateKbps = opts.MinBitrateKbps
	rc.maxBitrateKbps = opts.MaxBitrateKbps
	rc.undershootPct = uint8(normalizeRateControlPct(opts.UndershootPct, defaultRateControlUndershootPct))
	rc.overshootPct = uint8(normalizeRateControlPct(opts.OvershootPct, defaultRateControlOvershootPct))
	rc.maxIntraBitratePct = opts.MaxIntraBitratePct
	rc.gfCBRBoostPct = 0
	if rc.mode == RateControlCBR {
		rc.gfCBRBoostPct = opts.GFCBRBoostPct
	}
}

func (rc *vp9RateControlState) applyRuntimeConfig(opts VP9EncoderOptions, timing timingState) error {
	if !rc.enabled {
		return rc.applyOptions(opts, timing)
	}
	if !opts.RateControlModeSet || !validRateControlMode(opts.RateControlMode) {
		return ErrInvalidConfig
	}
	bufferSize := opts.BufferSizeMs
	if bufferSize == 0 {
		bufferSize = libvpxDefaultBufferSizeMs
	}
	bufferInitial := opts.BufferInitialSizeMs
	if bufferInitial == 0 {
		bufferInitial = libvpxDefaultBufferInitialMs
	}
	bufferOptimal := opts.BufferOptimalSizeMs
	if bufferOptimal == 0 {
		bufferOptimal = libvpxDefaultBufferOptimalMs
	}
	if bufferSize <= 0 || bufferInitial < 0 || bufferOptimal < 0 {
		return ErrInvalidConfig
	}
	waterMark := opts.DropFrameWaterMark
	if opts.DropFrameAllowed && waterMark <= 0 {
		waterMark = defaultDropFramesWaterMark
	}
	if waterMark > 100 {
		waterMark = 100
	}

	prev := *rc
	rc.mode = opts.RateControlMode
	rc.bufferSizeMs = bufferSize
	rc.bufferInitialSizeMs = bufferInitial
	rc.bufferOptimalSizeMs = bufferOptimal
	rc.dropFrameAllowed = false
	rc.dropFramesWaterMark = 0
	if opts.RateControlMode == RateControlCBR {
		rc.dropFrameAllowed = opts.DropFrameAllowed
		rc.dropFramesWaterMark = uint8(waterMark)
	}
	rc.applyBitrateBoundsFromOptions(opts)
	rc.setFrameSize(opts.Width, opts.Height)
	rc.setQuantizerBoundsFromOptions(opts)
	if err := rc.setBitrateKbps(opts.TargetBitrateKbps, timing); err != nil {
		*rc = prev
		return err
	}
	rc.setRuntimeOnePassVBRGoldenCadence(prev)
	return nil
}

func (rc *vp9RateControlState) setBitrateKbps(kbps int, timing timingState) error {
	if !rc.enabled {
		return nil
	}
	if kbps <= 0 {
		return ErrInvalidBitrate
	}
	kbps = rc.clampBitrateKbps(kbps)
	targetBits, ok := checkedMul(kbps, 1000)
	if !ok {
		return ErrInvalidBitrate
	}
	bitsPerFrame := computeVP9BitsPerFrame(targetBits, timing)
	if bitsPerFrame <= 0 {
		return ErrInvalidBitrate
	}
	bufferSizeBits, ok := checkedMul(kbps, rc.bufferSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	bufferInitialBits, ok := checkedMul(kbps, rc.bufferInitialSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	bufferOptimalBits, ok := checkedMul(kbps, rc.bufferOptimalSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.targetBitrateKbps = kbps
	rc.targetBandwidthBits = targetBits
	rc.bitsPerFrame = bitsPerFrame
	rc.frameTargetBits = bitsPerFrame
	rc.frameRateNum = timing.timebaseDen
	rc.frameRateDen = timing.timebaseNum * timing.frameDuration
	rc.bufferSizeBits = bufferSizeBits
	rc.bufferInitialBits = bufferInitialBits
	rc.bufferOptimalBits = bufferOptimalBits
	rc.updateFrameBandwidthBounds()
	rc.initOnePassVBRState(timing)
	rc.clampBuffer()
	return nil
}

func computeVP9BitsPerFrame(targetBandwidthBits int, timing timingState) int {
	if targetBandwidthBits <= 0 || timing.timebaseNum <= 0 ||
		timing.timebaseDen <= 0 || timing.frameDuration <= 0 {
		return 0
	}
	num := int64(targetBandwidthBits) * int64(timing.timebaseNum) *
		int64(timing.frameDuration)
	den := int64(timing.timebaseDen)
	if den <= 0 {
		return 0
	}
	v := num / den
	if v > int64(maxInt()) {
		return 0
	}
	return int(v)
}

func (rc *vp9RateControlState) setBufferModel(sizeMs, initialMs, optimalMs int) error {
	if !rc.enabled {
		return nil
	}
	if sizeMs <= 0 || initialMs < 0 || optimalMs < 0 {
		return ErrInvalidConfig
	}
	sizeBits, ok := checkedMul(rc.targetBitrateKbps, sizeMs)
	if !ok {
		return ErrInvalidConfig
	}
	initialBits, ok := checkedMul(rc.targetBitrateKbps, initialMs)
	if !ok {
		return ErrInvalidConfig
	}
	optimalBits, ok := checkedMul(rc.targetBitrateKbps, optimalMs)
	if !ok {
		return ErrInvalidConfig
	}
	rc.bufferSizeMs = sizeMs
	rc.bufferInitialSizeMs = initialMs
	rc.bufferOptimalSizeMs = optimalMs
	rc.bufferSizeBits = sizeBits
	rc.bufferInitialBits = initialBits
	rc.bufferOptimalBits = optimalBits
	rc.clampBuffer()
	return nil
}

func (rc *vp9RateControlState) initQuantizerStateFromOptions(opts VP9EncoderOptions) {
	rc.setQuantizerBoundsFromOptions(opts)
	if rc.mode == RateControlCBR {
		rc.avgFrameQIndexKey = rc.worstQuality
		rc.avgFrameQIndexInter = rc.worstQuality
	} else {
		mid := uint8((int(rc.bestQuality) + int(rc.worstQuality)) >> 1)
		rc.avgFrameQIndexKey = mid
		rc.avgFrameQIndexInter = mid
	}
	rc.lastQKey = rc.bestQuality
	rc.lastQInter = rc.worstQuality
	rc.lastBoostedQIndex = rc.worstQuality
	rc.afRatioOnePassVBR = vp9DefaultAFRatioOnePassVBR
	rc.facActiveWorstInter = vp9DefaultActiveWorstInterPct
	rc.facActiveWorstGF = vp9DefaultActiveWorstGFPct
	for i := range rc.rateCorrectionFactors {
		rc.rateCorrectionFactors[i] = 1
	}
	rc.q1Frame = 0
	rc.q2Frame = 0
	rc.rc1Frame = 0
	rc.rc2Frame = 0
	rc.framesSinceKey = 8
}

func (rc *vp9RateControlState) setQuantizerBoundsFromOptions(opts VP9EncoderOptions) {
	minQ, maxQ, cqLevel := vp9NormalizedPublicQuantizers(opts)
	best := vp9PublicQuantizerToQIndex(minQ)
	worst := vp9PublicQuantizerToQIndex(maxQ)
	if best > worst {
		best = worst
	}
	rc.bestQuality = uint8(best)
	rc.worstQuality = uint8(worst)
	rc.cqLevel = uint8(vp9PublicQuantizerToQIndex(cqLevel))
	rc.clampQuantizerHistory()
}

func (rc *vp9RateControlState) clampQuantizerHistory() {
	best := rc.bestQuality
	worst := rc.worstQuality
	rc.avgFrameQIndexKey = clampUint8(rc.avgFrameQIndexKey, best, worst)
	rc.avgFrameQIndexInter = clampUint8(rc.avgFrameQIndexInter, best, worst)
	rc.lastQKey = clampUint8(rc.lastQKey, best, worst)
	rc.lastQInter = clampUint8(rc.lastQInter, best, worst)
	rc.lastBoostedQIndex = clampUint8(rc.lastBoostedQIndex, best, worst)
	rc.cqLevel = clampUint8(rc.cqLevel, best, worst)
	rc.q1Frame = clampUint8(rc.q1Frame, best, worst)
	rc.q2Frame = clampUint8(rc.q2Frame, best, worst)
}

func (rc *vp9RateControlState) initOnePassVBRState(timing timingState) {
	if !rc.enabled {
		return
	}
	rc.afRatioOnePassVBR = vp9DefaultAFRatioOnePassVBR
	if rc.mode == RateControlQ {
		rc.baselineGFInterval = vp9FixedGFInterval
		return
	}
	minInterval := vp9DefaultMinGFInterval(timing)
	maxInterval := vp9DefaultMaxGFInterval(timing, minInterval)
	rc.baselineGFInterval = uint8((minInterval + maxInterval) >> 1)
}

func (rc *vp9RateControlState) beginFrame(isKey bool, frameIndex int) {
	rc.beginFrameWithRefresh(isKey, frameIndex, 0)
}

// beginFrameWithRefresh is the beginFrame variant that lets CBR mode apply
// the libvpx VP9 GF CBR boost on golden-frame refreshes. Non-CBR modes route
// through setOnePassVBRFrameTarget which already understands refresh flags.
func (rc *vp9RateControlState) beginFrameWithRefresh(isKey bool, frameIndex int, refreshFlags uint8) {
	if !rc.enabled {
		return
	}
	if isKey {
		rc.frameTargetBits = rc.keyFrameTargetBits(frameIndex)
		return
	}
	if rc.mode == RateControlCBR && vp9BoostedInterRefresh(refreshFlags) {
		rc.frameTargetBits = rc.boostedInterFrameTargetBits()
		return
	}
	rc.frameTargetBits = rc.interFrameTargetBits()
}

func (rc *vp9RateControlState) shouldDropInterFrame() bool {
	_, drop := rc.testDropInterFrame()
	return drop
}

func (rc *vp9RateControlState) preEncodeFrame(showFrame bool) {
	if !rc.enabled || !showFrame {
		return
	}
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
}

// testDropInterFrame mirrors libvpx v1.16.0 vp9_test_drop for the non-SVC CBR
// path. It mutates decimation state exactly like the source branch it models.
func (rc *vp9RateControlState) testDropInterFrame() (vp9DropReason, bool) {
	if !rc.enabled || rc.mode != RateControlCBR || !rc.dropFrameAllowed ||
		rc.dropFramesWaterMark == 0 {
		return vp9DropNone, false
	}
	if rc.bufferLevelBits < 0 {
		return vp9DropNegativeBuffer, true
	}
	dropMark := int(rc.dropFramesWaterMark) * rc.bufferOptimalBits / 100
	if rc.bufferLevelBits > dropMark && rc.decimationFactor > 0 {
		rc.decimationFactor--
	} else if rc.bufferLevelBits <= dropMark && rc.decimationFactor == 0 {
		rc.decimationFactor = 1
	}
	if rc.decimationFactor > 0 {
		if rc.decimationCount > 0 {
			rc.decimationCount--
			return vp9DropWatermarkDecimation, true
		}
		rc.decimationCount = rc.decimationFactor
		return vp9DropNone, false
	}
	rc.decimationCount = 0
	return vp9DropNone, false
}

func (rc *vp9RateControlState) postDropFrame() {
	if !rc.enabled {
		return
	}
	rc.rc2Frame = 0
	rc.rc1Frame = 0
	rc.lastQInter = rc.q1Frame
	rc.incrementFramesSinceKey()
}

func vp9DropReasonString(reason vp9DropReason) string {
	switch reason {
	case vp9DropNegativeBuffer:
		return "negative_buffer"
	case vp9DropWatermarkDecimation:
		return "watermark_decimation"
	default:
		return ""
	}
}

func (rc *vp9RateControlState) postEncodeFrame(sizeBytes int, showFrame bool, qindex int, intraOnly bool, refreshFlags uint8, macroblocks int) {
	if !rc.enabled {
		return
	}
	encodedBits := encodedSizeBits(sizeBytes)
	rc.updateRateCorrectionFactor(encodedBits, qindex, intraOnly, refreshFlags, macroblocks)
	rc.updateQHistory(qindex, intraOnly, refreshFlags, showFrame)
	rc.postOnePassVBRRefresh(refreshFlags)
	rc.totalActualBits += int64(encodedBits)
	if showFrame {
		rc.totalTargetBits += int64(rc.bitsPerFrame)
	}
	rc.bufferLevelBits = vp9PostEncodeBufferLevel(rc.bufferLevelBits,
		rc.bufferSizeBits, encodedBits)
}

func (rc *vp9RateControlState) setFrameSize(width int, height int) {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	if width > int(^uint16(0)) {
		width = int(^uint16(0))
	}
	if height > int(^uint16(0)) {
		height = int(^uint16(0))
	}
	rc.codedWidth = uint16(width)
	rc.codedHeight = uint16(height)
	rc.updateFrameBandwidthBounds()
}

func (rc *vp9RateControlState) updateFrameBandwidthBounds() {
	if !rc.enabled {
		return
	}
	rc.minFrameBandwidth = vp9FrameOverhead
	if rc.bitsPerFrame > 0 && rc.bitsPerFrame>>5 > rc.minFrameBandwidth {
		rc.minFrameBandwidth = rc.bitsPerFrame >> 5
	}
	miRows := (int(rc.codedHeight) + 7) >> 3
	miCols := (int(rc.codedWidth) + 7) >> 3
	mbs := vp9MacroblockCount(miRows, miCols)
	maxByMB := 0
	if mbs > 0 {
		if mbs > maxInt()/vp9MaxMBRateBits {
			maxByMB = maxInt()
		} else {
			maxByMB = mbs * vp9MaxMBRateBits
		}
	}
	maxBits := max(maxByMB, vp9MaxRate1080PBits)
	if rc.bitsPerFrame > 0 {
		vbrMax := percentOf(rc.bitsPerFrame, vp9DefaultVBRMaxSectionPct)
		if vbrMax > maxBits {
			maxBits = vbrMax
		}
	}
	rc.maxFrameBandwidth = maxBits
}

func (rc *vp9RateControlState) setFrameDropAllowed(enabled bool, waterMark int) {
	if !rc.enabled {
		return
	}
	rc.dropFrameAllowed = enabled
	if !enabled {
		rc.dropFramesWaterMark = 0
		rc.decimationFactor = 0
		rc.decimationCount = 0
		return
	}
	if waterMark <= 0 {
		waterMark = int(rc.dropFramesWaterMark)
	}
	if waterMark <= 0 {
		waterMark = defaultDropFramesWaterMark
	}
	if waterMark > 100 {
		waterMark = 100
	}
	rc.dropFramesWaterMark = uint8(waterMark)
}

func (rc *vp9RateControlState) clampBuffer() {
	if !rc.enabled {
		return
	}
	if rc.bufferLevelBits > rc.bufferSizeBits {
		rc.bufferLevelBits = rc.bufferSizeBits
	}
}

func vp9PostEncodeBufferLevel(level, maxBits, encodedBits int) int {
	next := int64(level)
	next -= int64(encodedBits)
	if next > int64(maxBits) {
		return maxBits
	}
	if next < int64(-maxInt()) {
		return -maxInt()
	}
	return int(next)
}

func clampUint8(v, lo, hi uint8) uint8 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
