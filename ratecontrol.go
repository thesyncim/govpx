package govpx

import "math"

// RateControlMode selects the encoder bitrate-control strategy.
type RateControlMode int

const (
	// RateControlVBR selects variable bitrate mode.
	RateControlVBR RateControlMode = iota
	// RateControlCBR selects constant bitrate mode.
	RateControlCBR
	// RateControlCQ selects constrained-quality mode.
	RateControlCQ
	// RateControlQ selects libvpx VPX_Q constant-quality mode.
	RateControlQ
)

// RealtimeFrameDropMode selects how SetRealtimeTarget changes frame dropping.
type RealtimeFrameDropMode int

const (
	// RealtimeFrameDropUnchanged leaves the current frame-drop setting intact.
	RealtimeFrameDropUnchanged RealtimeFrameDropMode = iota
	// RealtimeFrameDropDisabled disables realtime frame dropping.
	RealtimeFrameDropDisabled
	// RealtimeFrameDropEnabled enables realtime frame dropping.
	RealtimeFrameDropEnabled
)

// RateControlConfig is the runtime-updatable subset of encoder rate-control
// options.
type RateControlConfig struct {
	// Mode selects VBR, CBR, constrained-quality, or VPX_Q behavior.
	Mode RateControlMode

	// TargetBitrateKbps is the total target bitrate.
	TargetBitrateKbps int
	// MinBitrateKbps and MaxBitrateKbps optionally bound runtime bitrate
	// updates.
	MinBitrateKbps int
	MaxBitrateKbps int

	// MinQuantizer and MaxQuantizer bound the public 0..63 quantizer range.
	MinQuantizer int
	MaxQuantizer int
	// CQLevel is the public quantizer level for RateControlCQ and
	// RateControlQ. RateControlCQ applies it as a floor; RateControlQ
	// mirrors libvpx's VPX_Q validation without applying the CQ floor.
	CQLevel int

	// UndershootPct and OvershootPct cap libvpx-style rate adjustment.
	UndershootPct int
	OvershootPct  int

	// BufferSizeMs, BufferInitialSizeMs, and BufferOptimalSizeMs describe the
	// virtual rate-control buffer in milliseconds.
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int

	// DropFrameAllowed enables rate-control frame dropping.
	DropFrameAllowed bool
	// DropFrameWaterMark mirrors libvpx's oxcf.drop_frames_water_mark
	// (driven by the public --drop-frame=N CLI knob and
	// rc_dropframe_thresh in vpx_codec_enc_cfg_t). It is the buffer-level
	// percentage threshold at which the decimation drop branch in
	// vp8_check_drop_buffer (vp8/encoder/onyx_if.c) starts dropping
	// frames; values from 1..100 are valid. Zero is treated as "use 60"
	// when DropFrameAllowed is true so the historical govpx behavior
	// (buffer-underrun-only) does not regress for callers that toggle
	// DropFrameAllowed without setting an explicit threshold.
	DropFrameWaterMark int

	// MaxIntraBitratePct caps key-frame bitrate as a percentage of target.
	MaxIntraBitratePct int
	// GFCBRBoostPct controls golden-frame boost in CBR mode.
	GFCBRBoostPct int
}

// RealtimeTarget describes a low-latency runtime target update.
type RealtimeTarget struct {
	// BitrateKbps changes the total target bitrate when non-zero.
	BitrateKbps int
	// FPS changes the timebase to 1/FPS when non-zero.
	FPS int

	// Width and Height must match the encoder dimensions when set.
	Width  int
	Height int

	// MinQuantizer and MaxQuantizer update the public quantizer range when
	// non-zero.
	MinQuantizer int
	MaxQuantizer int

	// FrameDrop changes realtime frame dropping. The zero value leaves the
	// current setting unchanged, which is the right default for bitrate-only
	// WebRTC bandwidth-estimation updates.
	FrameDrop RealtimeFrameDropMode
	// AllowFrameDrop enables realtime frame dropping when true. It is kept for
	// source compatibility with older callers; use FrameDrop when disabling or
	// when the update must be explicit.
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

	// libvpx vp8/encoder/onyx_if.c vp8_check_drop_buffer state. When the
	// buffer dips through the per-25/50/75% drop_marks the decimation
	// factor is bumped up (1->2->3) so every other (or every third)
	// inter frame is dropped to defend the buffer; the factor decays
	// back to 0 as the buffer recovers. dropFramesWaterMark mirrors
	// cpi->oxcf.drop_frames_water_mark (percent of optimal_buffer_level)
	// and gates the entire decimation branch.
	decimationFactor    int
	decimationCount     int
	dropFramesWaterMark int

	framesSinceKeyframe   int
	currentTemporalLayers int
	rollingActualBits     int
	rollingTargetBits     int
	longRollingActualBits int
	longRollingTargetBits int
	totalActualBits       int64

	maxIntraBitratePct int
	gfCBRBoostPct      int

	avgFrameQuantizer         int
	normalInterQuantizerTotal int
	normalInterFrames         int
	normalInterAvgQuantizer   int

	// framesSinceLastDropOvershoot mirrors libvpx
	// `cpi->frames_since_last_drop_overshoot` (vp8/encoder/onyx_int.h). It
	// is incremented on every `vp8_drop_encodedframe_overshoot` non-drop
	// return and reset to 0 when an overshoot drop fires; the post-encode
	// drop gate at vp8/encoder/ratectrl.c
	// `vp8_drop_encodedframe_overshoot` requires both
	// `frames_since_last_drop_overshoot > framerate` AND
	// `rate_correction_factor < 8 * MIN_BPB_FACTOR` before it engages
	// outside screen-content-mode==2.
	framesSinceLastDropOvershoot int

	rateCorrectionFactor     float64
	keyFrameCorrectionFactor float64
	goldenCorrectionFactor   float64
	currentZbinOverQuant     int
	activeWorstQChanged      bool

	// pass2ActiveWorstQOverride mirrors libvpx's
	// `cpi->active_worst_quality` after vp8_second_pass runs
	// estimate_max_q on the first frame (and damps it on subsequent
	// frames). When pass2ActiveWorstQValid is true, the regulator's
	// `libvpxActiveWorstQuantizer` returns this value (clamped to
	// [minQuantizer, maxQuantizer]) instead of `maxQuantizer`. The
	// encoder pushes the value via `setPass2ActiveWorstQ` before
	// `selectQuantizerForFrameKindWithScreenContent`. Without this
	// the regulator picks a much lower Q than libvpx for the same
	// per-frame target on real-content pass-2 fixtures (q_match=8%
	// on desktopqvga while target_match=100%).
	pass2ActiveWorstQOverride int
	pass2ActiveWorstQValid    bool

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
	autoKeyFrames          bool
	outputFrameRate        int
	priorKeyFrameDistance  [keyFrameContextSize]int

	// libvpx vp8/encoder/onyx_if.c update_golden_frame_stats accumulates
	// per-MB ref-frame usage across the GF section so calc_gf_params and
	// the calc_pframe_target_size auto_gold decision can read it.
	recentRefFrameUsageIntra  int
	recentRefFrameUsageLast   int
	recentRefFrameUsageGolden int
	recentRefFrameUsageAltRef int
	gfActiveCount             int
	thisFramePercentIntra     int
}

const (
	// defaultRateControlUndershootPct mirrors libvpx vp8/vp8_cx_iface.c
	// vpx_codec_enc_cfg defaults: `rc_undershoot_pct = 100`. The undershoot
	// pct caps `percent_low` in the buffer-aware bandwidth shrink branch of
	// calc_pframe_target_size: target -= target * percent_low / 200, where
	// percent_low = (optimal_buffer_level - buffer_level) /
	// (1 + optimal_buffer_level/100). On a tight CBR buffer (post-kf or
	// post-drop) percent_low naturally lands around 70-90, so a lower cap
	// here makes govpx shrink LESS than libvpx does, leaving an inflated
	// per-frame target that pulls the regulated Q below libvpx's. Closing
	// this gap is required for post_drop_q parity on the 30f tight-buffer
	// CBR fixture; see the panning-30f-80kbps drop scoreboard.
	defaultRateControlUndershootPct = 100
	defaultRateControlOvershootPct  = 100
	defaultCQLevel                  = 10
	libvpxDefaultBufferSizeMs       = 6000
	libvpxDefaultBufferInitialMs    = 4000
	libvpxDefaultBufferOptimalMs    = 5000
	libvpxVBRBufferSizeMs           = 240000
	libvpxVBRBufferInitialMs        = 60000
	libvpxVBRBufferOptimalMs        = 60000
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

const defaultDropFramesWaterMark = 60

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
	if rc.mode == RateControlVBR {
		// libvpx maps VPX_VBR to USAGE_LOCAL_FILE_PLAYBACK and forces a
		// relaxed buffer model inside init_config, ignoring the public buffer
		// controls for this mode.
		rc.bufferSizeMs = libvpxVBRBufferSizeMs
		rc.bufferInitialSizeMs = libvpxVBRBufferInitialMs
		rc.bufferOptimalSizeMs = libvpxVBRBufferOptimalMs
	}
	rc.dropFrameAllowed = cfg.DropFrameAllowed
	// Mirror libvpx vp8_cx_iface.c set_vp8e_config: oxcf->allow_df is
	// (rc_dropframe_thresh > 0). govpx splits the toggle and the
	// threshold so callers can opt into drop semantics with the libvpx
	// default percentage (60) without rummaging through CLI parity. Any
	// non-zero DropFrameWaterMark wins; zero with DropFrameAllowed=true
	// inherits libvpx's current default of 60 from
	// vpxenc.c (rc_dropframe_thresh defaults to 0 there, so users must
	// pass --drop-frame to opt in; here we make the toggle alone enough).
	rc.dropFramesWaterMark = cfg.DropFrameWaterMark
	if rc.dropFrameAllowed && rc.dropFramesWaterMark <= 0 {
		rc.dropFramesWaterMark = defaultDropFramesWaterMark
	}
	if rc.dropFramesWaterMark > 100 {
		rc.dropFramesWaterMark = 100
	}
	if !rc.dropFrameAllowed {
		rc.dropFramesWaterMark = 0
	}
	rc.decimationFactor = 0
	rc.decimationCount = 0
	rc.maxIntraBitratePct = cfg.MaxIntraBitratePct
	rc.gfCBRBoostPct = cfg.GFCBRBoostPct
	rc.avgFrameQuantizer = rc.maxQuantizer
	rc.normalInterQuantizerTotal = 0
	rc.normalInterFrames = 0
	rc.normalInterAvgQuantizer = rc.maxQuantizer
	rc.rateCorrectionFactor = 1.0
	rc.keyFrameCorrectionFactor = 1.0
	rc.goldenCorrectionFactor = 1.0
	rc.totalActualBits = 0
	rc.outputFrameRate = int(outputFrameRate(timing))
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
	if !keyFrame && ctx.temporalLayerCount <= 1 {
		targetBits = rc.applyOnePassPFrameOverspendRecovery(targetBits)
		targetBits = rc.bufferAdjustedFrameTargetBits(targetBits)
	}
	if rc.mode == RateControlCQ {
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
	// Mirror libvpx: the per_frame_bandwidth used for min_frame_target's
	// quarter-floor is the just-boosted (post-vp8_check_drop_buffer) value
	// when decimation is active. We mirror that by consulting
	// decimationBoostedBitsPerFrame() instead of the raw bitsPerFrame.
	perFrameBandwidth := rc.decimationBoostedBitsPerFrame()
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
		adjustment := max(min(min(rc.kfBitrateAdjustment, rc.kfOverspendBits), perFrameBandwidth-minFrameTarget), 0)
		rc.kfOverspendBits -= adjustment
		thisFrameTarget = max(targetBits-adjustment, minFrameTarget)
	}
	if rc.gfOverspendBits > 0 && thisFrameTarget > minFrameTarget {
		adjustment := max(min(min(rc.nonGFBitrateAdjustment, rc.gfOverspendBits), thisFrameTarget-minFrameTarget), 0)
		rc.gfOverspendBits -= adjustment
		thisFrameTarget -= adjustment
	}
	// libvpx also applies a small +/- last_boost adjustment for non-gf
	// frames inside long GF intervals.
	if rc.lastBoost > 150 && rc.framesTillGFUpdateDue > 0 &&
		rc.currentGFInterval >= (libvpxMinGFInterval<<1) {
		adjustment := max(min((rc.lastBoost-100)>>5, 10), 1)
		adjustment = max(min((thisFrameTarget*adjustment)/100, thisFrameTarget-minFrameTarget), 0)
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
	if uint(q) >= uint(len(libvpxKeyFrameBoostQAdjustment)) {
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
		boost = min(max(initialBoost, libvpxKeyFrameBoostForFrameRate(ctx.timing)), maxKeyBoost)
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
