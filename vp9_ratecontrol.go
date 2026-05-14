package govpx

type vp9RateControlState struct {
	enabled bool
	mode    RateControlMode

	targetBitrateKbps   int
	targetBandwidthBits int
	bitsPerFrame        int
	frameTargetBits     int
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

	bestQuality  uint8
	worstQuality uint8

	avgFrameQIndexKey   uint8
	avgFrameQIndexInter uint8
	lastQKey            uint8
	lastQInter          uint8
	lastBoostedQIndex   uint8

	rateCorrectionFactors [vp9RateFactorLevels]float64
	dampedAdjustment      [vp9RateFactorLevels]bool
	q1Frame               uint8
	q2Frame               uint8
	rc1Frame              int8
	rc2Frame              int8

	framesSinceKey uint16
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
	if opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	if opts.TargetBitrateKbps <= 0 {
		return ErrInvalidBitrate
	}
	return nil
}

func (rc *vp9RateControlState) applyOptions(opts VP9EncoderOptions, timing timingState) error {
	*rc = vp9RateControlState{}
	if !opts.RateControlModeSet {
		return nil
	}
	if opts.RateControlMode != RateControlCBR {
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
	rc.dropFrameAllowed = opts.DropFrameAllowed
	rc.dropFramesWaterMark = uint8(waterMark)
	rc.setFrameSize(opts.Width, opts.Height)
	rc.initQuantizerStateFromOptions(opts)
	if err := rc.setBitrateKbps(opts.TargetBitrateKbps, timing); err != nil {
		return err
	}
	rc.bufferLevelBits = rc.bufferInitialBits
	return nil
}

func (rc *vp9RateControlState) setBitrateKbps(kbps int, timing timingState) error {
	if !rc.enabled {
		return nil
	}
	if kbps <= 0 {
		return ErrInvalidBitrate
	}
	targetBits, ok := checkedMul(kbps, 1000)
	if !ok {
		return ErrInvalidBitrate
	}
	bitsPerFrame := computeBitsPerFrame(targetBits, timing)
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
	rc.clampBuffer()
	return nil
}

func (rc *vp9RateControlState) initQuantizerStateFromOptions(opts VP9EncoderOptions) {
	rc.setQuantizerBoundsFromOptions(opts)
	rc.avgFrameQIndexKey = rc.worstQuality
	rc.avgFrameQIndexInter = rc.worstQuality
	rc.lastQKey = rc.bestQuality
	rc.lastQInter = rc.worstQuality
	rc.lastBoostedQIndex = rc.worstQuality
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
	minQ, maxQ, _ := vp9NormalizedPublicQuantizers(opts)
	best := vp9PublicQuantizerToQIndex(minQ)
	worst := vp9PublicQuantizerToQIndex(maxQ)
	if best > worst {
		best = worst
	}
	rc.bestQuality = uint8(best)
	rc.worstQuality = uint8(worst)
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
	rc.q1Frame = clampUint8(rc.q1Frame, best, worst)
	rc.q2Frame = clampUint8(rc.q2Frame, best, worst)
}

func (rc *vp9RateControlState) beginFrame(isKey bool, frameIndex int) {
	if !rc.enabled {
		return
	}
	if isKey {
		rc.frameTargetBits = rc.keyFrameTargetBits(frameIndex)
		return
	}
	rc.frameTargetBits = rc.interFrameTargetBits()
}

func (rc *vp9RateControlState) shouldDropInterFrame() bool {
	_, drop := rc.testDropInterFrame()
	return drop
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
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
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
	rc.bufferLevelBits = vp9PostEncodeBufferLevel(
		rc.bufferLevelBits, rc.bitsPerFrame, rc.bufferSizeBits,
		encodedBits, showFrame)
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

func vp9PostEncodeBufferLevel(level, bitsPerFrame, maxBits, encodedBits int, showFrame bool) int {
	next := int64(level)
	if showFrame {
		next += int64(bitsPerFrame)
	}
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
