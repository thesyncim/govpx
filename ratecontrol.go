package govpx

import "math"

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

	minQuantizer       int
	maxQuantizer       int
	cqLevel            int
	currentQuantizer   int
	lastQuantizer      int
	lastInterQuantizer int

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

	framesSinceKeyframe   int
	currentTemporalLayers int
	rollingActualBits     int
	rollingTargetBits     int
	longRollingActualBits int
	longRollingTargetBits int

	maxIntraBitratePct int
	gfCBRBoostPct      int

	avgFrameQuantizer         int
	normalInterQuantizerTotal int
	normalInterFrames         int
	normalInterAvgQuantizer   int

	rateCorrectionFactor     float64
	keyFrameCorrectionFactor float64
	goldenCorrectionFactor   float64
	currentZbinOverQuant     int
	activeWorstQChanged      bool

	// libvpx vp8/encoder/ratectrl.c one-pass GF/KF overspend bookkeeping.
	kfOverspendBits        int
	gfOverspendBits        int
	kfBitrateAdjustment    int
	nonGFBitrateAdjustment int
	interFrameTarget       int
	minFrameBandwidth      int
	lastBoost              int
	currentGFInterval      int
	framesTillGFUpdateDue  int
	framesSinceGolden      int
	keyFrameCount          int
	keyFrameFrequency      int
	priorKeyFrameDistance  [keyFrameContextSize]int
}

const (
	defaultRateControlUndershootPct = 50
	defaultRateControlOvershootPct  = 100
	defaultCQLevel                  = 10
	libvpxBPerMBNormBits            = 9
	libvpxIntMax                    = 1<<31 - 1
	libvpxMinBPBFactor              = 0.01
	libvpxMaxBPBFactor              = 50.0
	libvpxZbinOverQuantMax          = 192
	vp8MaxQIndex                    = 127

	// libvpx vp8/encoder/onyx_int.h GF interval defaults.
	// libvpxMinGFInterval is declared in encoder_firstpass.go.
	libvpxDefaultGFInterval = 7
	keyFrameContextSize     = 5
)

// libvpxPriorKeyFrameWeight ports prior_key_frame_weight from
// vp8/encoder/ratectrl.c (used by estimate_keyframe_frequency).
var libvpxPriorKeyFrameWeight = [keyFrameContextSize]int{1, 2, 3, 4, 5}

var libvpxQuantizerTranslation = [maxQuantizer + 1]int{
	0, 1, 2, 3, 4, 5, 7, 8, 9, 10, 12, 13, 15, 17, 18, 19,
	20, 21, 23, 24, 25, 26, 27, 28, 29, 30, 31, 33, 35, 37, 39, 41,
	43, 45, 47, 49, 51, 53, 55, 57, 59, 61, 64, 67, 70, 73, 76, 79,
	82, 85, 88, 91, 94, 97, 100, 103, 106, 109, 112, 115, 118, 121, 124, 127,
}

func (rc *rateControlState) applyConfig(cfg RateControlConfig, timing timingState) error {
	if err := validateRateControlConfig(cfg); err != nil {
		return err
	}
	rc.mode = cfg.Mode
	rc.minBitrateKbps = cfg.MinBitrateKbps
	rc.maxBitrateKbps = cfg.MaxBitrateKbps
	rc.minQuantizer = libvpxPublicQuantizerToQIndex(cfg.MinQuantizer)
	rc.maxQuantizer = libvpxPublicQuantizerToQIndex(cfg.MaxQuantizer)
	rc.cqLevel = libvpxPublicQuantizerToQIndex(normalizedCQLevel(cfg.CQLevel, cfg.MinQuantizer))
	rc.undershootPct = normalizeRateControlPct(cfg.UndershootPct, defaultRateControlUndershootPct)
	rc.overshootPct = normalizeRateControlPct(cfg.OvershootPct, defaultRateControlOvershootPct)
	rc.bufferSizeMs = cfg.BufferSizeMs
	rc.bufferInitialSizeMs = cfg.BufferInitialSizeMs
	rc.bufferOptimalSizeMs = cfg.BufferOptimalSizeMs
	rc.dropFrameAllowed = cfg.DropFrameAllowed
	rc.maxIntraBitratePct = cfg.MaxIntraBitratePct
	rc.gfCBRBoostPct = cfg.GFCBRBoostPct
	rc.avgFrameQuantizer = rc.maxQuantizer
	rc.normalInterQuantizerTotal = 0
	rc.normalInterFrames = 0
	rc.normalInterAvgQuantizer = rc.maxQuantizer
	rc.rateCorrectionFactor = 1.0
	rc.keyFrameCorrectionFactor = 1.0
	rc.goldenCorrectionFactor = 1.0
	if err := rc.setBitrateKbps(cfg.TargetBitrateKbps, timing); err != nil {
		return err
	}
	rc.resetRollingBitAverages()
	if rc.mode == RateControlCQ {
		rc.currentQuantizer = rc.cqLevel
		rc.lastQuantizer = rc.cqLevel
		rc.lastInterQuantizer = rc.cqLevel
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

	initializing := rc.targetBitrateKbps == 0
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
	if initializing && rc.bufferLevelBits == 0 {
		rc.bufferLevelBits = rc.bufferInitialBits
	}
	rc.frameTargetBits = rc.bitsPerFrame
	rc.clampBuffer()
	rc.clampQuantizer()
	return nil
}

func (rc *rateControlState) beginFrame(keyFrame bool) {
	rc.beginFrameWithTargetAndContext(keyFrame, rc.bitsPerFrame, rateControlFrameContext{})
}

func (rc *rateControlState) beginFrameWithTarget(keyFrame bool, baseTargetBits int) {
	rc.beginFrameWithTargetAndContext(keyFrame, baseTargetBits, rateControlFrameContext{})
}

type rateControlFrameContext struct {
	firstFrame         bool
	forcedKeyFrame     bool
	temporalLayerCount int
	timing             timingState
}

func (rc *rateControlState) beginFrameWithTargetAndContext(keyFrame bool, baseTargetBits int, ctx rateControlFrameContext) {
	rc.currentTemporalLayers = ctx.temporalLayerCount
	targetBits := baseTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	baseFrameTargetBits := targetBits
	if keyFrame {
		if ctx.firstFrame && rc.bufferInitialBits > 0 {
			targetBits = rc.initialKeyFrameTargetBits()
		} else {
			targetBits = rc.laterKeyFrameTargetBits(targetBits, ctx)
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
	if !keyFrame && rc.mode == RateControlCBR && rc.rollingTargetBits > 0 && ctx.temporalLayerCount <= 1 {
		targetBits = rc.applyOnePassPFrameOverspendRecovery(targetBits)
		targetBits = rc.bufferAdjustedFrameTargetBits(targetBits)
	} else if rc.mode == RateControlCQ {
		if rc.currentQuantizer < rc.cqLevel {
			rc.currentQuantizer = rc.cqLevel
		}
	}
	rc.frameTargetBits = targetBits
	rc.clampQuantizer()
}

// applyOnePassPFrameOverspendRecovery mirrors the one-pass non-ARF p-frame
// branch of libvpx's calc_pframe_target_size (vp8/encoder/ratectrl.c). It
// drains accumulated kf_overspend_bits / gf_overspend_bits into the
// per-frame target via kf_bitrate_adjustment / non_gf_bitrate_adjustment,
// clamping to min_frame_target = max(min_frame_bandwidth, per_frame_bandwidth/4).
// inter_frame_target is captured after recovery (libvpx records it on every
// non-altref normal frame).
func (rc *rateControlState) applyOnePassPFrameOverspendRecovery(targetBits int) int {
	if targetBits <= 0 {
		return targetBits
	}
	perFrameBandwidth := rc.bitsPerFrame
	if perFrameBandwidth <= 0 {
		return targetBits
	}
	minFrameTarget := rc.minFrameBandwidth
	quarter := perFrameBandwidth / 4
	if minFrameTarget < quarter {
		minFrameTarget = quarter
	}
	if minFrameTarget < 0 {
		minFrameTarget = 0
	}
	thisFrameTarget := targetBits
	if rc.kfOverspendBits > 0 {
		adjustment := rc.kfBitrateAdjustment
		if adjustment > rc.kfOverspendBits {
			adjustment = rc.kfOverspendBits
		}
		if adjustment > perFrameBandwidth-minFrameTarget {
			adjustment = perFrameBandwidth - minFrameTarget
		}
		if adjustment < 0 {
			adjustment = 0
		}
		rc.kfOverspendBits -= adjustment
		thisFrameTarget = targetBits - adjustment
		if thisFrameTarget < minFrameTarget {
			thisFrameTarget = minFrameTarget
		}
	}
	if rc.gfOverspendBits > 0 && thisFrameTarget > minFrameTarget {
		adjustment := rc.nonGFBitrateAdjustment
		if adjustment > rc.gfOverspendBits {
			adjustment = rc.gfOverspendBits
		}
		if adjustment > thisFrameTarget-minFrameTarget {
			adjustment = thisFrameTarget - minFrameTarget
		}
		if adjustment < 0 {
			adjustment = 0
		}
		rc.gfOverspendBits -= adjustment
		thisFrameTarget -= adjustment
	}
	// libvpx also applies a small +/- last_boost adjustment for non-gf
	// frames inside long GF intervals.
	if rc.lastBoost > 150 && rc.framesTillGFUpdateDue > 0 &&
		rc.currentGFInterval >= (libvpxMinGFInterval<<1) {
		adjustment := (rc.lastBoost - 100) >> 5
		if adjustment > 10 {
			adjustment = 10
		}
		if adjustment < 1 {
			adjustment = 1
		}
		adjustment = (thisFrameTarget * adjustment) / 100
		if adjustment > thisFrameTarget-minFrameTarget {
			adjustment = thisFrameTarget - minFrameTarget
		}
		if adjustment < 0 {
			adjustment = 0
		}
		if rc.framesSinceGolden == rc.currentGFInterval>>1 {
			adjustment = (rc.currentGFInterval - 1) * adjustment
			cap10 := (10 * thisFrameTarget) / 100
			if adjustment > cap10 {
				adjustment = cap10
			}
			thisFrameTarget += adjustment
		} else {
			thisFrameTarget -= adjustment
		}
	}
	if thisFrameTarget < minFrameTarget {
		thisFrameTarget = minFrameTarget
	}
	// libvpx records inter_frame_target on every non-altref normal frame.
	rc.interFrameTarget = thisFrameTarget
	return thisFrameTarget
}

func (rc *rateControlState) initialKeyFrameTargetBits() int {
	target := int64(rc.bufferInitialBits) / 2
	maxTarget := int64(maxInt())
	if rc.targetBandwidthBits <= maxInt()/3 {
		maxTarget = int64(rc.targetBandwidthBits) * 3 / 2
	}
	if target > maxTarget {
		target = maxTarget
	}
	if target > int64(maxInt()) {
		return maxInt()
	}
	if target < 1 {
		return 1
	}
	return int(target)
}

func (rc *rateControlState) laterKeyFrameTargetBits(baseTargetBits int, ctx rateControlFrameContext) int {
	if baseTargetBits <= 0 {
		return 0
	}
	q := rc.normalInterAvgQuantizer
	if ctx.forcedKeyFrame {
		q = rc.avgFrameQuantizer
	}
	if q < 0 || q >= len(libvpxKeyFrameBoostQAdjustment) {
		q = rc.clampedQuantizerValue(rc.maxQuantizer)
	}

	const (
		initialBoost = 32
		maxKeyBoost  = 2000
		minKeyBoost  = 16
		targetScale  = 16
	)
	boost := initialBoost
	if ctx.temporalLayerCount <= 1 {
		boost = max(initialBoost, libvpxKeyFrameBoostForFrameRate(ctx.timing))
		if boost > maxKeyBoost {
			boost = maxKeyBoost
		}
	}
	boost = boost * libvpxKeyFrameBoostQAdjustment[q] / 100
	if halfFrameRate := libvpxHalfFrameRate(ctx.timing); halfFrameRate > 0 && float64(rc.framesSinceKeyframe) < halfFrameRate {
		boost = int(float64(boost) * float64(rc.framesSinceKeyframe) / halfFrameRate)
	}
	if boost < minKeyBoost {
		boost = minKeyBoost
	}

	target := int64(16+boost) * int64(baseTargetBits) / targetScale
	if target > int64(maxInt()) {
		return maxInt()
	}
	if target < 1 {
		return 1
	}
	return int(target)
}

func libvpxKeyFrameBoostForFrameRate(timing timingState) int {
	fps := outputFrameRate(timing)
	if fps <= 0 {
		return 0
	}
	return int(math.Round(2*fps - 16))
}

func libvpxHalfFrameRate(timing timingState) float64 {
	fps := outputFrameRate(timing)
	if fps <= 0 {
		return 0
	}
	return fps / 2
}

func outputFrameRate(timing timingState) float64 {
	if timing.timebaseNum <= 0 || timing.timebaseDen <= 0 || timing.frameDuration <= 0 {
		return 0
	}
	return float64(timing.timebaseDen) / (float64(timing.timebaseNum) * float64(timing.frameDuration))
}

func (rc *rateControlState) selectQuantizerForFrame(keyFrame bool, macroblocks int) {
	rc.selectQuantizerForFrameKind(keyFrame, false, macroblocks)
}

func (rc *rateControlState) selectQuantizerForFrameKind(keyFrame bool, goldenFrame bool, macroblocks int) {
	rc.selectQuantizerForFrameKindWithScreenContent(keyFrame, goldenFrame, macroblocks, 0)
}

func (rc *rateControlState) selectQuantizerForFrameKindWithScreenContent(keyFrame bool, goldenFrame bool, macroblocks int, screenContentMode int) {
	if macroblocks <= 0 {
		return
	}
	if rc.mode != RateControlCBR && rc.mode != RateControlVBR && rc.mode != RateControlCQ {
		return
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return
	}
	rc.activeWorstQChanged = false
	correctionFactor := rc.rateCorrectionFactorForFrame(keyFrame, goldenFrame)
	activeBest, activeWorst := rc.libvpxActiveQuantizerBounds(keyFrame, goldenFrame)
	rc.currentQuantizer, rc.currentZbinOverQuant = libvpxRegulatedQuantizerWithZbin(keyFrame, goldenFrame, targetBits, macroblocks, activeBest, activeWorst, correctionFactor)
	if rc.mode == RateControlCQ {
		rc.currentQuantizer = rc.clampedCQQuantizerValue(rc.currentQuantizer)
		if rc.currentQuantizer < vp8MaxQIndex {
			rc.currentZbinOverQuant = 0
		}
	}
	if rc.mode == RateControlCBR && screenContentMode > 0 && !keyFrame {
		rc.currentQuantizer = libvpxLimitCBRInterQuantizerDrop(rc.lastInterQuantizer, rc.currentQuantizer)
		if rc.currentQuantizer < vp8MaxQIndex {
			rc.currentZbinOverQuant = 0
		}
	}
	rc.clampQuantizer()
	if rc.currentQuantizer < vp8MaxQIndex {
		rc.currentZbinOverQuant = 0
	}
}

func (rc *rateControlState) libvpxActiveQuantizerBounds(keyFrame bool, goldenFrame bool) (int, int) {
	activeWorst := rc.libvpxActiveWorstQuantizer()
	if rc.mode == RateControlCBR && rc.bufferOptimalBits > 0 && rc.bufferLevelBits >= rc.bufferOptimalBits {
		activeWorst = rc.libvpxCBRFullBufferActiveWorst(activeWorst)
	}
	activeWorst = rc.clampedQuantizerValue(activeWorst)

	activeBest := rc.minQuantizer
	if rc.normalInterFrames > 150 {
		q := clampQuantizerValue(activeWorst, 0, vp8MaxQIndex)
		switch {
		case keyFrame:
			activeBest = libvpxKeyFrameHighMotionMinQ[q]
		case goldenFrame && rc.currentTemporalLayers <= 1:
			if rc.framesSinceKeyframe > 1 && rc.avgFrameQuantizer < q {
				q = rc.avgFrameQuantizer
			}
			if rc.mode == RateControlCQ && q < rc.cqLevel {
				q = rc.cqLevel
			}
			q = clampQuantizerValue(q, 0, vp8MaxQIndex)
			activeBest = libvpxGoldenFrameHighMotionMinQ[q]
		default:
			activeBest = libvpxInterMinQ[q]
			if rc.mode == RateControlCQ && activeBest < rc.cqLevel {
				activeBest = rc.cqLevel
			}
		}
		if rc.mode == RateControlCBR {
			activeBest = rc.libvpxCBRFullBufferActiveBest(activeBest)
		}
	} else if rc.mode == RateControlCQ {
		if !keyFrame && !goldenFrame && activeBest < rc.cqLevel {
			activeBest = rc.cqLevel
		}
	}

	activeBest = rc.clampedQuantizerValue(activeBest)
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	return activeBest, activeWorst
}

func (rc *rateControlState) libvpxActiveWorstQuantizer() int {
	activeWorst := rc.maxQuantizer
	if rc.mode != RateControlCBR || rc.normalInterFrames <= 150 || rc.bufferOptimalBits <= 0 {
		if rc.mode == RateControlCQ && activeWorst < rc.cqLevel {
			activeWorst = rc.cqLevel
		}
		return activeWorst
	}
	if rc.bufferLevelBits >= rc.bufferOptimalBits {
		activeWorst = rc.normalInterAvgQuantizer
	} else if rc.bufferLevelBits > rc.bufferOptimalBits>>2 {
		denom := (rc.bufferOptimalBits * 3) >> 2
		if denom > 0 {
			qadjustmentRange := rc.maxQuantizer - rc.normalInterAvgQuantizer
			aboveBase := rc.bufferLevelBits - (rc.bufferOptimalBits >> 2)
			activeWorst = rc.maxQuantizer - int((int64(qadjustmentRange)*int64(aboveBase))/int64(denom))
		}
	}
	return activeWorst
}

func (rc *rateControlState) libvpxCBRFullBufferActiveWorst(activeWorst int) int {
	if rc.maximumBufferBits <= rc.bufferOptimalBits {
		return activeWorst
	}
	adjustment := activeWorst / 4
	if adjustment <= 0 {
		return activeWorst
	}
	if rc.bufferLevelBits < rc.maximumBufferBits {
		bufferLevelStep := (rc.maximumBufferBits - rc.bufferOptimalBits) / adjustment
		if bufferLevelStep > 0 {
			adjustment = (rc.bufferLevelBits - rc.bufferOptimalBits) / bufferLevelStep
		} else {
			adjustment = 0
		}
	}
	return activeWorst - adjustment
}

func (rc *rateControlState) libvpxCBRFullBufferActiveBest(activeBest int) int {
	if rc.bufferOptimalBits <= 0 || rc.maximumBufferBits <= rc.bufferOptimalBits {
		return activeBest
	}
	switch {
	case rc.bufferLevelBits >= rc.maximumBufferBits:
		return rc.minQuantizer
	case rc.bufferLevelBits > rc.bufferOptimalBits:
		fraction := int((int64(rc.bufferLevelBits-rc.bufferOptimalBits) * 128) / int64(rc.maximumBufferBits-rc.bufferOptimalBits))
		minQAdjustment := ((activeBest - rc.minQuantizer) * fraction) / 128
		return activeBest - minQAdjustment
	default:
		return activeBest
	}
}

func (rc *rateControlState) clampBuffer() {
	if rc.bufferLevelBits > rc.maximumBufferBits {
		rc.bufferLevelBits = rc.maximumBufferBits
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
	if rc.lastInterQuantizer < rc.minQuantizer {
		rc.lastInterQuantizer = rc.minQuantizer
	}
	if rc.lastInterQuantizer > rc.maxQuantizer {
		rc.lastInterQuantizer = rc.maxQuantizer
	}
}

func (rc *rateControlState) postEncodeFrame(sizeBytes int, keyFrame bool) {
	rc.postEncodeFrameWithContext(sizeBytes, keyFrame, false, 0)
}

func (rc *rateControlState) postEncodeFrameWithContext(sizeBytes int, keyFrame bool, goldenFrame bool, macroblocks int) {
	rc.postEncodeFrameWithPacketContext(sizeBytes, keyFrame, goldenFrame, macroblocks, true)
}

func (rc *rateControlState) postEncodeFrameWithPacketContext(sizeBytes int, keyFrame bool, goldenFrame bool, macroblocks int, showFrame bool) {
	actualBits := encodedSizeBits(sizeBytes)
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if !rc.activeWorstQChanged {
		rc.updateRateCorrectionFactor(actualBits, keyFrame, goldenFrame, macroblocks)
	}
	rc.activeWorstQChanged = false
	rc.updateRollingBitAverages(actualBits, targetBits)
	if showFrame {
		rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	}
	rc.bufferLevelBits = saturatingSub(rc.bufferLevelBits, actualBits)
	rc.clampBuffer()

	// libvpx vp8/encoder/ratectrl.c vp8_adjust_key_frame_context and
	// onyx_if.c update_golden_frame_stats accumulate post-pack overspend
	// before the next frame's calc_pframe_target_size runs.
	rc.accumulatePostPackOverspend(actualBits, keyFrame, goldenFrame)

	encodedQuantizer := rc.currentQuantizer
	rc.lastQuantizer = encodedQuantizer
	if !keyFrame {
		rc.lastInterQuantizer = encodedQuantizer
	}
	if rc.mode == RateControlCQ {
		rc.adjustCQQuantizerWithContext(actualBits, targetBits, keyFrame, goldenFrame)
	} else {
		rc.adjustQuantizerWithContext(actualBits, targetBits, keyFrame, goldenFrame)
	}
	rc.clampQuantizer()

	rc.updateQuantizerAverages(encodedQuantizer, keyFrame, goldenFrame)
	if keyFrame {
		rc.framesSinceKeyframe = 0
		rc.framesSinceGolden = 0
		return
	}
	rc.framesSinceKeyframe++
	if goldenFrame {
		rc.framesSinceGolden = 0
	} else {
		rc.framesSinceGolden++
		if rc.framesTillGFUpdateDue > 0 {
			rc.framesTillGFUpdateDue--
		}
	}
}

// accumulatePostPackOverspend ports libvpx's post-pack overspend
// bookkeeping. For key frames it mirrors vp8_adjust_key_frame_context: when
// the projected (encoded) size exceeds per_frame_bandwidth, 7/8 of the
// overspend is accumulated into kf_overspend_bits and 1/8 into
// gf_overspend_bits (single-layer); kf_bitrate_adjustment is the per-frame
// drain rate computed from estimate_keyframe_frequency. For golden refreshes
// it mirrors update_golden_frame_stats: overspend relative to
// inter_frame_target accumulates into gf_overspend_bits and
// non_gf_bitrate_adjustment is the per-frame drain rate over the next GF
// interval.
func (rc *rateControlState) accumulatePostPackOverspend(actualBits int, keyFrame bool, goldenFrame bool) {
	perFrameBandwidth := rc.bitsPerFrame
	if perFrameBandwidth <= 0 {
		return
	}
	if keyFrame {
		rc.keyFrameCount++
		if actualBits > perFrameBandwidth {
			overspend := actualBits - perFrameBandwidth
			if rc.currentTemporalLayers > 1 {
				rc.kfOverspendBits = saturatingAdd(rc.kfOverspendBits, overspend)
			} else {
				rc.kfOverspendBits = saturatingAdd(rc.kfOverspendBits, overspend*7/8)
				rc.gfOverspendBits = saturatingAdd(rc.gfOverspendBits, overspend/8)
			}
			kfFreq := rc.estimateKeyFrameFrequency()
			if kfFreq <= 0 {
				kfFreq = 1
			}
			rc.kfBitrateAdjustment = rc.kfOverspendBits / kfFreq
		}
		return
	}
	if !goldenFrame {
		return
	}
	// libvpx onyx_if.c update_golden_frame_stats: only accumulate gf
	// overspend on non-key non-altref-active golden refreshes. govpx's
	// CBR oracle does not currently model an active alt-ref, so treat
	// every golden refresh as the non-altref case (matches libvpx
	// behaviour when source_alt_ref_active is 0).
	interTarget := rc.interFrameTarget
	if interTarget <= 0 {
		interTarget = perFrameBandwidth
	}
	if actualBits > interTarget {
		rc.gfOverspendBits = saturatingAdd(rc.gfOverspendBits, actualBits-interTarget)
	}
	if rc.framesTillGFUpdateDue > 0 {
		rc.nonGFBitrateAdjustment = rc.gfOverspendBits / rc.framesTillGFUpdateDue
	}
}

// estimateKeyFrameFrequency ports vp8/encoder/ratectrl.c
// estimate_keyframe_frequency: a weighted average of the last
// KEY_FRAME_CONTEXT key-frame distances (weights 1..5), with the
// key_frame_count == 1 bootstrap returning 1 + 2*output_framerate (or
// key_frame_frequency for auto-key).
func (rc *rateControlState) estimateKeyFrameFrequency() int {
	if rc.keyFrameCount == 1 {
		var avg int
		if rc.keyFrameFrequency > 0 {
			avg = rc.keyFrameFrequency
		} else {
			avg = 1
		}
		// libvpx's first-keyframe bootstrap uses
		// 1 + (int)output_framerate * 2; without timing data fall back
		// to the configured key-frame frequency.
		if rc.keyFrameFrequency > 0 {
			rc.priorKeyFrameDistance[keyFrameContextSize-1] = avg
		}
		if avg < 1 {
			avg = 1
		}
		return avg
	}
	last := rc.framesSinceKeyframe
	if last <= 0 {
		last = 1
	}
	totalWeight := 0
	avg := 0
	for i := 0; i < keyFrameContextSize; i++ {
		if i < keyFrameContextSize-1 {
			rc.priorKeyFrameDistance[i] = rc.priorKeyFrameDistance[i+1]
		} else {
			rc.priorKeyFrameDistance[i] = last
		}
		avg += libvpxPriorKeyFrameWeight[i] * rc.priorKeyFrameDistance[i]
		totalWeight += libvpxPriorKeyFrameWeight[i]
	}
	if totalWeight > 0 {
		avg /= totalWeight
	}
	if avg < 1 {
		avg = 1
	}
	return avg
}

func libvpxLimitCBRInterQuantizerDrop(lastInterQuantizer int, currentQuantizer int) int {
	const limitDown = 12
	if lastInterQuantizer-currentQuantizer > limitDown {
		return lastInterQuantizer - limitDown
	}
	return currentQuantizer
}

func (rc *rateControlState) clampScreenContentBufferDebt(screenContentMode int) {
	if screenContentMode <= 0 || rc.maximumBufferBits <= 0 {
		return
	}
	minimumBuffer := -rc.maximumBufferBits
	if rc.bufferLevelBits < minimumBuffer {
		rc.bufferLevelBits = minimumBuffer
	}
}

func (rc *rateControlState) updateQuantizerAverages(q int, keyFrame bool, goldenFrame bool) {
	if q < 0 {
		return
	}
	if !keyFrame {
		if rc.avgFrameQuantizer <= 0 {
			rc.avgFrameQuantizer = rc.maxQuantizer
		}
		rc.avgFrameQuantizer = (2 + 3*rc.avgFrameQuantizer + q) >> 2
	}
	if keyFrame || goldenFrame {
		return
	}
	rc.normalInterFrames++
	if rc.normalInterFrames <= 0 {
		rc.normalInterFrames = maxInt()
	}
	rc.normalInterQuantizerTotal = saturatingAdd(rc.normalInterQuantizerTotal, q)
	if rc.normalInterFrames > 150 {
		rc.normalInterAvgQuantizer = rc.normalInterQuantizerTotal / rc.normalInterFrames
	} else {
		rc.normalInterAvgQuantizer = ((rc.normalInterQuantizerTotal / rc.normalInterFrames) + rc.maxQuantizer + 1) / 2
	}
	if q > rc.normalInterAvgQuantizer {
		rc.normalInterAvgQuantizer = q - 1
	}
}

func (rc *rateControlState) updateRateCorrectionFactor(actualBits int, keyFrame bool, goldenFrame bool, macroblocks int) {
	if actualBits <= 0 || macroblocks <= 0 {
		return
	}
	if rc.mode != RateControlCBR && rc.mode != RateControlVBR && rc.mode != RateControlCQ {
		return
	}
	q := rc.currentQuantizer
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	if q < 0 || q >= len(libvpxBitsPerMB[frameType]) {
		return
	}
	rateCorrectionFactor := rc.rateCorrectionFactorForFrame(keyFrame, goldenFrame)
	if rateCorrectionFactor <= 0 {
		rateCorrectionFactor = 1.0
	}
	projectedBits := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, rateCorrectionFactor, rc.currentZbinOverQuant)
	if projectedBits <= 0 {
		return
	}
	correctionFactor := int((100 * int64(actualBits)) / int64(projectedBits))
	const finalPackAdjustmentLimit = 0.25
	switch {
	case correctionFactor > 102:
		correctionFactor = int(100.5 + float64(correctionFactor-100)*finalPackAdjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor > libvpxMaxBPBFactor {
			rateCorrectionFactor = libvpxMaxBPBFactor
		}
	case correctionFactor < 99:
		correctionFactor = int(100.5 - float64(100-correctionFactor)*finalPackAdjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor < libvpxMinBPBFactor {
			rateCorrectionFactor = libvpxMinBPBFactor
		}
	}
	rc.setRateCorrectionFactorForFrame(keyFrame, goldenFrame, rateCorrectionFactor)
}

func (rc *rateControlState) rateCorrectionFactorForFrame(keyFrame bool, goldenFrame bool) float64 {
	if keyFrame {
		return normalizedRateCorrectionFactor(rc.keyFrameCorrectionFactor)
	}
	if rc.usesGoldenFrameCorrectionFactor(goldenFrame) {
		return normalizedRateCorrectionFactor(rc.goldenCorrectionFactor)
	}
	return normalizedRateCorrectionFactor(rc.rateCorrectionFactor)
}

func normalizedRateCorrectionFactor(factor float64) float64 {
	if factor <= 0 {
		return 1.0
	}
	return factor
}

func (rc *rateControlState) setRateCorrectionFactorForFrame(keyFrame bool, goldenFrame bool, factor float64) {
	if keyFrame {
		rc.keyFrameCorrectionFactor = factor
		return
	}
	if rc.usesGoldenFrameCorrectionFactor(goldenFrame) {
		rc.goldenCorrectionFactor = factor
		return
	}
	rc.rateCorrectionFactor = factor
}

func (rc *rateControlState) usesGoldenFrameCorrectionFactor(goldenFrame bool) bool {
	if !goldenFrame {
		return false
	}
	return rc.mode != RateControlCBR || rc.gfCBRBoostPct > 100
}

func (rc *rateControlState) shouldDropInterFrame() bool {
	if !rc.dropFrameAllowed || rc.mode != RateControlCBR {
		return false
	}
	return rc.bufferLevelBits < 0
}

func (rc *rateControlState) postDropFrame() {
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	rc.bufferLevelBits = saturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
	if rc.frameDropPressure > 0 {
		rc.frameDropPressure--
	}
	rc.framesSinceKeyframe++
}

func (rc *rateControlState) updateRollingBitAverages(actualBits int, targetBits int) {
	rc.rollingActualBits = libvpxRollingBits(rc.rollingActualBits, actualBits, 3, 2)
	rc.rollingTargetBits = libvpxRollingBits(rc.rollingTargetBits, targetBits, 3, 2)
	rc.longRollingActualBits = libvpxRollingBits(rc.longRollingActualBits, actualBits, 31, 5)
	rc.longRollingTargetBits = libvpxRollingBits(rc.longRollingTargetBits, targetBits, 31, 5)
}

func (rc *rateControlState) resetRollingBitAverages() {
	rc.rollingActualBits = rc.bitsPerFrame
	rc.rollingTargetBits = rc.bitsPerFrame
	rc.longRollingActualBits = rc.bitsPerFrame
	rc.longRollingTargetBits = rc.bitsPerFrame
}

type frameSizeRecodeState struct {
	qLow                int
	qHigh               int
	zbinOQLow           int
	zbinOQHigh          int
	zbinOverQuant       int
	correctionFactor    float64
	activeWorstQChanged bool
	overshootSeen       bool
	undershootSeen      bool
}

func (rc *rateControlState) newFrameSizeRecodeState(keyFrame bool, goldenFrame bool) frameSizeRecodeState {
	activeBest, activeWorst := rc.libvpxActiveQuantizerBounds(keyFrame, goldenFrame)
	return frameSizeRecodeState{
		qLow:             activeBest,
		qHigh:            activeWorst,
		zbinOQHigh:       libvpxZbinOverQuantHigh(keyFrame, goldenFrame),
		zbinOverQuant:    rc.currentZbinOverQuant,
		correctionFactor: rc.rateCorrectionFactorForFrame(keyFrame, goldenFrame),
	}
}

func (rc *rateControlState) frameSizeRecodeQuantizerWithContext(sizeBytes int, keyFrame bool, goldenFrame bool, macroblocks int, recode *frameSizeRecodeState) (int, bool) {
	if recode == nil {
		return rc.currentQuantizer, false
	}
	q := rc.currentQuantizer
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 || macroblocks <= 0 {
		return rc.clampedFrameQuantizerValue(q), false
	}
	actualBits := encodedSizeBits(sizeBytes)
	undershootLimit, overshootLimit := rc.frameSizeBoundsBits(keyFrame, goldenFrame, targetBits)
	recode.activeWorstQChanged = rc.relaxActiveWorstQuantizerForOvershoot(actualBits, overshootLimit, q, recode)
	rc.activeWorstQChanged = recode.activeWorstQChanged
	if !rc.shouldRecodeFrameSize(actualBits, undershootLimit, overshootLimit, q, keyFrame, goldenFrame, recode) {
		return rc.clampedFrameQuantizerValue(q), false
	}

	var next int
	if actualBits > targetBits {
		if q < recode.qHigh {
			recode.qLow = q + 1
		} else {
			recode.qLow = recode.qHigh
		}
		if recode.zbinOverQuant > 0 {
			recode.zbinOQLow = min(recode.zbinOverQuant+1, recode.zbinOQHigh)
		}
		if recode.undershootSeen {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, goldenFrame, macroblocks, 1, recode.correctionFactor)
			}
			next = (recode.qHigh + recode.qLow + 1) / 2
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			} else {
				recode.zbinOQLow = min(recode.zbinOverQuant+1, recode.zbinOQHigh)
				recode.zbinOverQuant = (recode.zbinOQHigh + recode.zbinOQLow) / 2
			}
		} else {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, goldenFrame, macroblocks, 0, recode.correctionFactor)
			}
			next, recode.zbinOverQuant = libvpxRegulatedQuantizerWithZbin(keyFrame, goldenFrame, targetBits, macroblocks, recode.qLow, recode.qHigh, recode.correctionFactor)
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			}
		}
		recode.overshootSeen = true
	} else {
		if recode.zbinOverQuant == 0 && q > recode.qLow {
			recode.qHigh = q - 1
		} else if recode.zbinOverQuant > 0 {
			recode.zbinOQHigh = max(recode.zbinOverQuant-1, recode.zbinOQLow)
		} else {
			recode.qHigh = recode.qLow
		}
		if recode.overshootSeen {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, goldenFrame, macroblocks, 1, recode.correctionFactor)
			}
			next = (recode.qHigh + recode.qLow) / 2
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			} else {
				recode.zbinOverQuant = (recode.zbinOQHigh + recode.zbinOQLow) / 2
			}
		} else {
			if !recode.activeWorstQChanged {
				recode.correctionFactor = rc.rateCorrectionFactorAfterFrameSize(actualBits, keyFrame, goldenFrame, macroblocks, 0, recode.correctionFactor)
			}
			next, recode.zbinOverQuant = libvpxRegulatedQuantizerWithZbin(keyFrame, goldenFrame, targetBits, macroblocks, recode.qLow, recode.qHigh, recode.correctionFactor)
			if next < vp8MaxQIndex {
				recode.zbinOverQuant = 0
			}
			if rc.mode == RateControlCQ && next < recode.qLow {
				recode.qLow = next
			}
		}
		recode.undershootSeen = true
	}
	if next > recode.qHigh {
		next = recode.qHigh
	} else if next < recode.qLow {
		next = recode.qLow
	}
	if recode.zbinOverQuant < recode.zbinOQLow {
		recode.zbinOverQuant = recode.zbinOQLow
	} else if recode.zbinOverQuant > recode.zbinOQHigh {
		recode.zbinOverQuant = recode.zbinOQHigh
	}
	if next < vp8MaxQIndex {
		recode.zbinOverQuant = 0
	}
	return rc.clampedFrameQuantizerValue(next), true
}

func (rc *rateControlState) relaxActiveWorstQuantizerForOvershoot(actualBits int, overshootLimit int, q int, recode *frameSizeRecodeState) bool {
	if recode == nil || actualBits <= overshootLimit || overshootLimit <= 0 {
		return false
	}
	if q != recode.qHigh || recode.qHigh >= rc.maxQuantizer {
		return false
	}
	overSizePercent := ((actualBits - overshootLimit) * 100) / overshootLimit
	changed := false
	for recode.qHigh < rc.maxQuantizer && overSizePercent > 0 {
		recode.qHigh++
		overSizePercent = (overSizePercent * 96) / 100
		changed = true
	}
	if recode.qHigh < recode.qLow {
		recode.qHigh = recode.qLow
	}
	return changed
}

func (rc *rateControlState) shouldRecodeFrameSize(actualBits int, undershootLimit int, overshootLimit int, q int, keyFrame bool, goldenFrame bool, recode *frameSizeRecodeState) bool {
	if (actualBits > overshootLimit && q < recode.qHigh) || (actualBits < undershootLimit && q > recode.qLow) {
		return true
	}
	if rc.mode != RateControlCQ {
		return false
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return false
	}
	if q > rc.cqLevel && actualBits < (targetBits*7)>>3 {
		return true
	}
	return !keyFrame && !goldenFrame && q > rc.cqLevel && actualBits < rc.minimumFrameBandwidthBits() && recode.qLow > rc.cqLevel
}

func (rc *rateControlState) rateCorrectionFactorAfterFrameSize(actualBits int, keyFrame bool, goldenFrame bool, macroblocks int, dampVar int, rateCorrectionFactor float64) float64 {
	if actualBits <= 0 || macroblocks <= 0 {
		return rateCorrectionFactor
	}
	q := rc.currentQuantizer
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	if q < 0 || q >= len(libvpxBitsPerMB[frameType]) {
		return rateCorrectionFactor
	}
	rateCorrectionFactor = normalizedRateCorrectionFactor(rateCorrectionFactor)
	projectedBits := libvpxEstimatedBitsAtQuantizerWithZbin(frameType, q, macroblocks, rateCorrectionFactor, rc.currentZbinOverQuant)
	if projectedBits <= 0 {
		return rateCorrectionFactor
	}
	correctionFactor := int((100 * int64(actualBits)) / int64(projectedBits))
	adjustmentLimit := 0.25
	switch dampVar {
	case 0:
		adjustmentLimit = 0.75
	case 1:
		adjustmentLimit = 0.375
	}
	switch {
	case correctionFactor > 102:
		correctionFactor = int(100.5 + float64(correctionFactor-100)*adjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor > libvpxMaxBPBFactor {
			rateCorrectionFactor = libvpxMaxBPBFactor
		}
	case correctionFactor < 99:
		correctionFactor = int(100.5 - float64(100-correctionFactor)*adjustmentLimit)
		rateCorrectionFactor *= float64(correctionFactor) / 100
		if rateCorrectionFactor < libvpxMinBPBFactor {
			rateCorrectionFactor = libvpxMinBPBFactor
		}
	}
	return rateCorrectionFactor
}

func (rc *rateControlState) minimumFrameBandwidthBits() int {
	target := rc.bitsPerFrame
	if rc.frameTargetBits > 0 {
		target = rc.frameTargetBits
	}
	if target <= 0 {
		return 0
	}
	minTarget := target / 8
	if minTarget < 1 {
		minTarget = 1
	}
	return minTarget
}

func (rc *rateControlState) clampedFrameQuantizerValue(q int) int {
	if rc.mode == RateControlCQ {
		return rc.clampedCQQuantizerValue(q)
	}
	return rc.clampedQuantizerValue(q)
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
	rc.adjustQuantizerWithContext(actualBits, targetBits, false, false)
}

func (rc *rateControlState) adjustQuantizerWithContext(actualBits int, targetBits int, keyFrame bool, goldenFrame bool) {
	if targetBits <= 0 {
		return
	}
	undershootLimit, overshootLimit := rc.frameSizeBoundsBits(keyFrame, goldenFrame, targetBits)
	switch {
	case actualBits > overshootLimit:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) {
			step = 2
		}
		rc.currentQuantizer += step
	case actualBits < undershootLimit:
		rc.currentQuantizer--
	}
}

func (rc *rateControlState) adjustCQQuantizer(actualBits int, targetBits int) {
	rc.adjustCQQuantizerWithContext(actualBits, targetBits, false, false)
}

func (rc *rateControlState) adjustCQQuantizerWithContext(actualBits int, targetBits int, keyFrame bool, goldenFrame bool) {
	if targetBits <= 0 {
		return
	}
	undershootLimit, overshootLimit := rc.frameSizeBoundsBits(keyFrame, goldenFrame, targetBits)
	switch {
	case actualBits > overshootLimit:
		step := 1
		if actualBits > saturatingAdd(overshootLimit, targetBits) {
			step = 2
		}
		rc.currentQuantizer += step
	case actualBits < undershootLimit:
		rc.currentQuantizer--
	}
	rc.currentQuantizer = rc.clampedCQQuantizerValue(rc.currentQuantizer)
}

func (rc *rateControlState) frameSizeBoundsBits(keyFrame bool, goldenFrame bool, targetBits int) (int, int) {
	if targetBits <= 0 {
		return 0, 0
	}
	target := int64(targetBits)
	if target > libvpxIntMax {
		target = libvpxIntMax
	}

	var undershootLimit int64
	var overshootLimit int64
	switch {
	case keyFrame || goldenFrame || rc.currentTemporalLayers > 1:
		overshootLimit = target * 9 / 8
		undershootLimit = target * 7 / 8
	case rc.mode == RateControlCBR:
		bufferLevel := int64(rc.bufferLevelBits)
		optimalBuffer := int64(rc.bufferOptimalBits)
		maximumBuffer := int64(rc.maximumBufferBits)
		switch {
		case bufferLevel >= (optimalBuffer+maximumBuffer)/2:
			overshootLimit = target * 12 / 8
			undershootLimit = target * 6 / 8
		case bufferLevel <= optimalBuffer/2:
			overshootLimit = target * 10 / 8
			undershootLimit = target * 4 / 8
		default:
			overshootLimit = target * 11 / 8
			undershootLimit = target * 5 / 8
		}
	case rc.mode == RateControlCQ:
		overshootLimit = target * 11 / 8
		undershootLimit = target * 2 / 8
	default:
		overshootLimit = target * 11 / 8
		undershootLimit = target * 5 / 8
	}

	overshootLimit += 200
	undershootLimit -= 200
	if undershootLimit < 0 {
		undershootLimit = 0
	}
	if undershootLimit > libvpxIntMax {
		undershootLimit = libvpxIntMax
	}
	if overshootLimit > libvpxIntMax {
		overshootLimit = libvpxIntMax
	}
	return int(undershootLimit), int(overshootLimit)
}

func (rc *rateControlState) bufferAdjustedFrameTargetBits(targetBits int) int {
	if targetBits <= 0 || rc.bufferOptimalBits <= 0 {
		return targetBits
	}
	onePercentBits := 1 + rc.bufferOptimalBits/100
	if onePercentBits <= 0 {
		return targetBits
	}
	target := int64(targetBits)
	switch {
	case rc.bufferLevelBits < rc.bufferOptimalBits:
		percentLow := (rc.bufferOptimalBits - rc.bufferLevelBits) / onePercentBits
		if percentLow > rc.undershootPct {
			percentLow = rc.undershootPct
		}
		if percentLow < 0 {
			percentLow = 0
		}
		target -= target * int64(percentLow) / 200
	case rc.bufferLevelBits > rc.bufferOptimalBits:
		percentHigh := (rc.bufferLevelBits - rc.bufferOptimalBits) / onePercentBits
		if percentHigh > rc.overshootPct {
			percentHigh = rc.overshootPct
		}
		if percentHigh < 0 {
			percentHigh = 0
		}
		target += target * int64(percentHigh) / 200
	default:
		return targetBits
	}
	if target > int64(maxInt()) {
		return maxInt()
	}
	if target < 1 {
		return 1
	}
	return int(target)
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

func libvpxRollingBits(previous int, current int, weight int, shift uint) int {
	if previous < 0 {
		previous = 0
	}
	if current < 0 {
		current = 0
	}
	if weight <= 0 {
		return current
	}
	round := 0
	if shift > 0 {
		round = 1 << (shift - 1)
	}
	if current > maxInt()-round {
		return maxInt()
	}
	limit := (maxInt() - current - round) / weight
	if previous > limit {
		return maxInt()
	}
	value := previous*weight + current + round
	return value >> shift
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

func libvpxPublicQuantizerToQIndex(q int) int {
	if q < 0 {
		return 0
	}
	if q > maxQuantizer {
		return libvpxQuantizerTranslation[maxQuantizer]
	}
	return libvpxQuantizerTranslation[q]
}

func libvpxQIndexToPublicQuantizer(qIndex int) int {
	for q, translated := range libvpxQuantizerTranslation {
		if translated >= qIndex {
			return q
		}
	}
	return maxQuantizer
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

// libvpxKeyFrameBoostQAdjustment ports vp8/encoder/ratectrl.c
// kf_boost_qadjustment.
var libvpxKeyFrameBoostQAdjustment = [128]int{
	128, 129, 130, 131, 132, 133, 134, 135,
	136, 137, 138, 139, 140, 141, 142, 143,
	144, 145, 146, 147, 148, 149, 150, 151,
	152, 153, 154, 155, 156, 157, 158, 159,
	160, 161, 162, 163, 164, 165, 166, 167,
	168, 169, 170, 171, 172, 173, 174, 175,
	176, 177, 178, 179, 180, 181, 182, 183,
	184, 185, 186, 187, 188, 189, 190, 191,
	192, 193, 194, 195, 196, 197, 198, 199,
	200, 200, 201, 201, 202, 203, 203, 203,
	204, 204, 205, 205, 206, 206, 207, 207,
	208, 208, 209, 209, 210, 210, 211, 211,
	212, 212, 213, 213, 214, 214, 215, 215,
	216, 216, 217, 217, 218, 218, 219, 219,
	220, 220, 220, 220, 220, 220, 220, 220,
	220, 220, 220, 220, 220, 220, 220, 220,
}

// libvpxKeyFrameHighMotionMinQ, libvpxGoldenFrameHighMotionMinQ, and
// libvpxInterMinQ port the one-pass conservative active-min-Q tables from
// vp8/encoder/onyx_if.c.
var libvpxKeyFrameHighMotionMinQ = [128]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3,
	3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 7, 7, 8, 8, 8, 8, 9, 9, 10, 10, 10, 10, 11, 11,
	11, 11, 12, 12, 13, 13, 13, 13, 14, 14, 15, 15, 15, 15, 16, 16,
	16, 16, 17, 17, 18, 18, 18, 18, 19, 19, 20, 20, 20, 20, 21, 21,
	21, 21, 22, 22, 23, 23, 24, 25, 25, 26, 26, 27, 28, 28, 29, 30,
}

var libvpxGoldenFrameHighMotionMinQ = [128]int{
	0, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4,
	4, 4, 5, 5, 5, 6, 6, 6, 7, 7, 7, 8, 8, 8, 9, 9,
	9, 10, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16,
	17, 17, 18, 18, 19, 19, 20, 20, 21, 21, 22, 22, 23, 23, 24, 24,
	25, 25, 26, 26, 27, 27, 28, 28, 29, 29, 30, 30, 31, 31, 32, 32,
	33, 33, 34, 34, 35, 35, 36, 36, 37, 37, 38, 38, 39, 39, 40, 40,
	41, 41, 42, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54,
	55, 56, 57, 58, 59, 60, 62, 64, 66, 68, 70, 72, 74, 76, 78, 80,
}

var libvpxInterMinQ = [128]int{
	0, 0, 1, 1, 2, 3, 3, 4, 4, 5, 6, 6, 7, 8, 8, 9,
	9, 10, 11, 11, 12, 13, 13, 14, 15, 15, 16, 17, 17, 18, 19, 20,
	20, 21, 22, 22, 23, 24, 24, 25, 26, 27, 27, 28, 29, 30, 30, 31,
	32, 33, 33, 34, 35, 36, 36, 37, 38, 39, 39, 40, 41, 42, 42, 43,
	44, 45, 46, 46, 47, 48, 49, 50, 50, 51, 52, 53, 54, 55, 55, 56,
	57, 58, 59, 60, 60, 61, 62, 63, 64, 65, 66, 67, 67, 68, 69, 70,
	71, 72, 73, 74, 75, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85,
	86, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 100,
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
	q, _ := libvpxRegulatedQuantizerWithZbin(keyFrame, false, targetBitsPerFrame, macroblocks, minQ, maxQ, correctionFactor)
	return q
}

func libvpxRegulatedQuantizerWithZbin(keyFrame bool, goldenFrame bool, targetBitsPerFrame int, macroblocks int, minQ int, maxQ int, correctionFactor float64) (int, int) {
	if macroblocks <= 0 || targetBitsPerFrame <= 0 {
		return clampQuantizerValue(minQ, minQ, maxQ), 0
	}
	if correctionFactor <= 0 {
		correctionFactor = 1.0
	}
	targetBitsPerMB := libvpxTargetBitsPerMB(targetBitsPerFrame, macroblocks)
	frameType := 1
	if keyFrame {
		frameType = 0
	}
	q := maxQ
	lastError := libvpxIntMax
	bitsAtSelectedQ := 0
	for i := minQ; i <= maxQ && i < len(libvpxBitsPerMB[frameType]); i++ {
		bitsAtQ := int(0.5 + correctionFactor*float64(libvpxBitsPerMB[frameType][i]))
		bitsAtSelectedQ = bitsAtQ
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
	q = clampQuantizerValue(q, minQ, maxQ)
	zbinOverQuant := 0
	if q >= vp8MaxQIndex {
		zbinOverQuant = libvpxZbinOverQuantForTarget(keyFrame, goldenFrame, bitsAtSelectedQ, targetBitsPerMB)
	}
	return q, zbinOverQuant
}

func libvpxTargetBitsPerMB(targetBitsPerFrame int, macroblocks int) int {
	if targetBitsPerFrame > libvpxIntMax>>libvpxBPerMBNormBits {
		temp := targetBitsPerFrame / macroblocks
		if temp > libvpxIntMax>>libvpxBPerMBNormBits {
			return libvpxIntMax
		}
		return temp << libvpxBPerMBNormBits
	}
	return (targetBitsPerFrame << libvpxBPerMBNormBits) / macroblocks
}

func libvpxZbinOverQuantForTarget(keyFrame bool, goldenFrame bool, bitsAtQ int, targetBitsPerMB int) int {
	zbinOQMax := libvpxZbinOverQuantHigh(keyFrame, goldenFrame)
	if zbinOQMax <= 0 || bitsAtQ <= 0 {
		return 0
	}
	zbinOverQuant := 0
	factor := 0.99
	factorAdjustment := 0.01 / 256.0
	for zbinOverQuant < zbinOQMax {
		zbinOverQuant++
		if zbinOverQuant > zbinOQMax {
			zbinOverQuant = zbinOQMax
		}
		bitsAtQ = int(factor * float64(bitsAtQ))
		factor += factorAdjustment
		if factor >= 0.999 {
			factor = 0.999
		}
		if bitsAtQ <= targetBitsPerMB {
			break
		}
	}
	return zbinOverQuant
}

func libvpxZbinOverQuantHigh(keyFrame bool, goldenFrame bool) int {
	if keyFrame {
		return 0
	}
	if goldenFrame {
		return 16
	}
	return libvpxZbinOverQuantMax
}

func libvpxEstimatedBitsAtQuantizer(frameType int, q int, macroblocks int, correctionFactor float64) int {
	if frameType < 0 || frameType >= len(libvpxBitsPerMB) || q < 0 || q >= len(libvpxBitsPerMB[frameType]) || macroblocks <= 0 {
		return 0
	}
	if correctionFactor <= 0 {
		correctionFactor = 1.0
	}
	bitsPerMB := int(0.5 + correctionFactor*float64(libvpxBitsPerMB[frameType][q]))
	if macroblocks > 1<<11 {
		return (bitsPerMB >> libvpxBPerMBNormBits) * macroblocks
	}
	return (bitsPerMB * macroblocks) >> libvpxBPerMBNormBits
}

// libvpxEstimatedBitsAtQuantizerWithZbin mirrors the post-encode projection in
// libvpx's vp8_update_rate_correction_factors (vp8/encoder/ratectrl.c): when
// zbin_over_quant > 0, project the frame size at this Q and then iteratively
// scale it down by a starting factor of 0.99 that walks toward 0.999 over
// `zbinOverQuant` steps. Without this scaling, frames encoded with non-zero
// zbin_oq look much larger than expected, the rate correction factor is
// damped toward 1.0, and the next frame's regulated Q is set too low.
func libvpxEstimatedBitsAtQuantizerWithZbin(frameType int, q int, macroblocks int, correctionFactor float64, zbinOverQuant int) int {
	bits := libvpxEstimatedBitsAtQuantizer(frameType, q, macroblocks, correctionFactor)
	if bits <= 0 || zbinOverQuant <= 0 {
		return bits
	}
	factor := 0.99
	const factorAdjustment = 0.01 / 256.0
	for z := zbinOverQuant; z > 0; z-- {
		bits = int(factor * float64(bits))
		factor += factorAdjustment
		if factor >= 0.999 {
			factor = 0.999
		}
		if bits <= 0 {
			return 0
		}
	}
	return bits
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

// libvpxGFBoostQAdjustment ports vp8_gf_boost_qadjustment from
// vp8/encoder/ratectrl.c. It is the GFQ_ADJUSTMENT lookup that seeds the
// one-pass GF boost computation.
var libvpxGFBoostQAdjustment = [128]int{
	80, 82, 84, 86, 88, 90, 92, 94, 96, 97, 98, 99, 100, 101, 102,
	103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115, 116, 117,
	118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132,
	133, 134, 135, 136, 137, 138, 139, 140, 141, 142, 143, 144, 145, 146, 147,
	148, 149, 150, 151, 152, 153, 154, 155, 156, 157, 158, 159, 160, 161, 162,
	163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 174, 175, 176, 177,
	178, 179, 180, 181, 182, 183, 184, 184, 185, 185, 186, 186, 187, 187, 188,
	188, 189, 189, 190, 190, 191, 191, 192, 192, 193, 193, 194, 194, 194, 194,
	195, 195, 196, 196, 197, 197, 198, 198,
}

// libvpxKFGFBoostQLimits ports kf_gf_boost_qlimits from
// vp8/encoder/ratectrl.c (one-pass upper limit on GF boost by Q).
var libvpxKFGFBoostQLimits = [128]int{
	150, 155, 160, 165, 170, 175, 180, 185, 190, 195, 200, 205, 210, 215, 220,
	225, 230, 235, 240, 245, 250, 255, 260, 265, 270, 275, 280, 285, 290, 295,
	300, 305, 310, 320, 330, 340, 350, 360, 370, 380, 390, 400, 410, 420, 430,
	440, 450, 460, 470, 480, 490, 500, 510, 520, 530, 540, 550, 560, 570, 580,
	590, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600, 600,
	600, 600, 600, 600, 600, 600, 600, 600,
}

// libvpxGFAdjustTable ports gf_adjust_table from vp8/encoder/ratectrl.c.
// Indexed by gf_frame_usage (0..100) it scales the GF boost by recent
// golden-frame usage.
var libvpxGFAdjustTable = [101]int{
	100, 115, 130, 145, 160, 175, 190, 200, 210, 220, 230, 240, 260, 270, 280,
	290, 300, 310, 320, 330, 340, 350, 360, 370, 380, 390, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
	400, 400, 400, 400, 400, 400, 400, 400, 400, 400, 400,
}

// libvpxGFIntraUsageAdjustment ports gf_intra_usage_adjustment from
// vp8/encoder/ratectrl.c. Indexed by clamp(this_frame_percent_intra, 0, 14)
// (the libvpx switch caps at 14 when percent_intra < 15).
var libvpxGFIntraUsageAdjustment = [20]int{
	125, 120, 115, 110, 105, 100, 95, 85, 80, 75,
	70, 65, 60, 55, 50, 50, 50, 50, 50, 50,
}

// libvpxGFIntervalTable ports gf_interval_table from vp8/encoder/ratectrl.c.
var libvpxGFIntervalTable = [101]int{
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
}

// gfParamsInput collects the libvpx calc_gf_params inputs that govpx must
// supply explicitly: the inter-frame Q used for the GFQ_ADJUSTMENT lookup,
// the per-MB ref-frame usage counts (intra/last/golden/altref), the count
// of macroblocks still pointing at the active golden, the number of MBs in
// the frame, percent_intra for this frame, and the maximum permitted GF
// interval (libvpx clamps to max_gf_interval).
type gfParamsInput struct {
	Q                     int
	RecentRefIntra        int
	RecentRefLast         int
	RecentRefGolden       int
	RecentRefAltRef       int
	GFActiveCount         int
	Macroblocks           int
	ThisFramePercentIntra int
	BaselineGFInterval    int
	MaxGFInterval         int
}

// gfParamsOutput is the calc_gf_params result govpx consumes: the GF boost
// (last_boost) and the next-GF interval (frames_till_gf_update_due).
type gfParamsOutput struct {
	Boost            int
	FramesTillUpdate int
	GFFrameUsage     int
}

// calcGFParams ports the one-pass branch of vp8/encoder/ratectrl.c
// calc_gf_params: it computes the GF boost from GFQ_ADJUSTMENT scaled by
// gf_intra_usage_adjustment and gf_adjust_table[gf_frame_usage], applies
// the kf_gf_boost_qlimits ceiling and a 110 floor, and computes the
// frames_till_gf_update_due interval from baseline_gf_interval, last_boost
// thresholds (>750/>1000/>1250/>=1500), gf_interval_table, and the
// max_gf_interval cap.
func calcGFParams(in gfParamsInput) gfParamsOutput {
	q := clampQuantizerValue(in.Q, 0, vp8MaxQIndex)
	totMBs := in.RecentRefIntra + in.RecentRefLast + in.RecentRefGolden + in.RecentRefAltRef
	gfFrameUsage := 0
	if totMBs > 0 {
		gfFrameUsage = (in.RecentRefGolden + in.RecentRefAltRef) * 100 / totMBs
	}
	pctGFActive := 0
	if in.Macroblocks > 0 {
		pctGFActive = (100 * in.GFActiveCount) / in.Macroblocks
	}
	if pctGFActive > gfFrameUsage {
		gfFrameUsage = pctGFActive
	}
	if gfFrameUsage < 0 {
		gfFrameUsage = 0
	}
	if gfFrameUsage > 100 {
		gfFrameUsage = 100
	}

	intraIdx := in.ThisFramePercentIntra
	if intraIdx < 0 {
		intraIdx = 0
	}
	if intraIdx >= 15 {
		intraIdx = 14
	}

	boost := libvpxGFBoostQAdjustment[q]
	boost = boost * libvpxGFIntraUsageAdjustment[intraIdx] / 100
	boost = boost * libvpxGFAdjustTable[gfFrameUsage] / 100

	if boost > libvpxKFGFBoostQLimits[q] {
		boost = libvpxKFGFBoostQLimits[q]
	} else if boost < 110 {
		boost = 110
	}

	framesTillUpdate := in.BaselineGFInterval
	if boost > 750 {
		framesTillUpdate++
	}
	if boost > 1000 {
		framesTillUpdate++
	}
	if boost > 1250 {
		framesTillUpdate++
	}
	if boost >= 1500 {
		framesTillUpdate++
	}
	if libvpxGFIntervalTable[gfFrameUsage] > framesTillUpdate {
		framesTillUpdate = libvpxGFIntervalTable[gfFrameUsage]
	}
	if in.MaxGFInterval > 0 && framesTillUpdate > in.MaxGFInterval {
		framesTillUpdate = in.MaxGFInterval
	}
	return gfParamsOutput{
		Boost:            boost,
		FramesTillUpdate: framesTillUpdate,
		GFFrameUsage:     gfFrameUsage,
	}
}

// applyGFParams stores the calc_gf_params result onto the rate-control
// state, mirroring the assignment of cpi->last_boost,
// cpi->frames_till_gf_update_due, and cpi->current_gf_interval that
// follows calc_gf_params in libvpx.
func (rc *rateControlState) applyGFParams(out gfParamsOutput) {
	rc.lastBoost = out.Boost
	rc.framesTillGFUpdateDue = out.FramesTillUpdate
	rc.currentGFInterval = out.FramesTillUpdate
}
