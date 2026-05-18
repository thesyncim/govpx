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
	// Valid range is [0, 100]; zero selects the libvpx default.
	UndershootPct int
	OvershootPct  int

	// BufferSizeMs, BufferInitialSizeMs, and BufferOptimalSizeMs describe the
	// virtual rate-control buffer in milliseconds.
	BufferSizeMs        int
	BufferInitialSizeMs int
	BufferOptimalSizeMs int

	// DropFrameAllowed enables rate-control frame dropping when
	// DropFrameWaterMark is positive.
	DropFrameAllowed bool
	// DropFrameWaterMark is the buffer-level percentage at which rate
	// control begins dropping frames. Values >100 are clamped to 100; zero
	// disables dropping. Mirrors libvpx's oxcf.drop_frames_water_mark /
	// rc_dropframe_thresh.
	DropFrameWaterMark int

	// MaxIntraBitratePct caps key-frame bitrate as a percentage of target.
	MaxIntraBitratePct int
	// GFCBRBoostPct controls golden-frame boost in CBR mode.
	GFCBRBoostPct int
}

// RealtimeTarget describes a low-latency runtime target update applied
// by [VP8Encoder.SetRealtimeTarget].
//
// Each field uses its zero value as "leave the current setting alone",
// so a bandwidth-estimator update is safe to send as a sparse delta
// (typically only BitrateKbps). All non-zero fields are validated
// before any mutation, and a validation failure leaves the encoder
// fully usable at its previous configuration.
//
// Mirrors libvpx's `vpx_codec_enc_config_set` for the fields a WebRTC sender
// typically updates per BWE step. VP9 consumes BitrateKbps / FPS /
// MinQuantizer / MaxQuantizer / Width / Height through
// VP9Encoder.SetRealtimeTarget; frame-drop controls are accepted by VP9 only
// when the encoder was created with VP9 CBR rate control enabled.
type RealtimeTarget struct {
	// BitrateKbps changes the total target bitrate when non-zero.
	// Equivalent to [VP8Encoder.SetBitrateKbps]. VP9 applies it when the
	// encoder was created with explicit rate control enabled.
	BitrateKbps int
	// FPS changes the timebase to 1/FPS when non-zero. The realtime
	// adaptive-Speed timing window is reset on VP8 so the auto-speed
	// selector recomputes from cold start against the new frame budget.
	FPS int

	// Width and Height drive caller-driven runtime resolution change
	// when both are positive. Setting them to the encoder's current
	// dimensions is a no-op (accepted for sparse BWE deltas that echo
	// the active size). Setting them to a new W x H pair resizes every
	// size-dependent encoder buffer in place (capacity is reused),
	// invalidates the LAST / GOLDEN / ALTREF references, and forces
	// the next encoded frame to be a key frame at the new dimensions.
	//
	// Mirrors libvpx's `vpx_codec_enc_config_set` with a new width /
	// height. The libvpx spatial resampler ([VP8E_SET_SCALEMODE],
	// `rc_resize_*`) is not implemented; callers drive the coded size
	// directly. The decoder also handles key-frame resolution change;
	// see [DecoderOptions.RejectResolutionChange].
	//
	// VP8 resize is refused with [ErrInvalidConfig] when the lookahead
	// queue is non-empty or a hidden alt-ref input is staged; drain the
	// encoder with [VP8Encoder.FlushInto] before resizing in those
	// modes. Invalid dimensions (zero, negative, or larger than the
	// codec maximum) are likewise refused without mutating encoder
	// state.
	Width  int
	Height int

	// MinQuantizer and MaxQuantizer update the public 0..63 quantizer
	// range when non-zero. Mirrors the runtime side of
	// [EncoderOptions.MinQuantizer] / [EncoderOptions.MaxQuantizer].
	MinQuantizer int
	MaxQuantizer int

	// FrameDrop changes realtime frame dropping. The zero value
	// ([RealtimeFrameDropUnchanged]) leaves the current setting
	// unchanged, which is the right default for bitrate-only WebRTC
	// bandwidth-estimation updates that should not accidentally
	// disable dropping.
	FrameDrop RealtimeFrameDropMode
	// AllowFrameDrop is a legacy fallback used only when FrameDrop is
	// [RealtimeFrameDropUnchanged]; if true, it enables realtime frame
	// dropping. Prefer FrameDrop in new code — it can disable dropping
	// and makes the intent explicit. Kept for source compatibility.
	AllowFrameDrop bool
}

type timingState struct {
	timebaseNum   int
	timebaseDen   int
	frameDuration int
	frameRate     float64
}

type rateControlState struct {
	mode RateControlMode

	targetBitrateKbps   int
	targetBandwidthBits int
	minBitrateKbps      int
	maxBitrateKbps      int

	// effectiveBitrateKbps mirrors libvpx's internal
	// `cpi->oxcf.target_bandwidth` after the raw-target-rate clamp at
	// vp8/encoder/onyx_if.c:set_oxcf (around line 1580):
	//
	//   raw_target_rate = Width * Height * 8 * 3 * framerate / 1000
	//   if (target_bandwidth > raw_target_rate)
	//       target_bandwidth = raw_target_rate
	//
	// libvpx keeps the user-facing `cfg.rc_target_bitrate` unchanged
	// (so VPX_E_GET_LAST_PKT and the bounds APIs still see the
	// requested value); the clamp only affects the internal
	// buffer-model / per-frame-budget arithmetic. govpx mirrors that
	// split: `targetBitrateKbps` reports the user's requested rate
	// (the field validated against MinBitrateKbps / MaxBitrateKbps)
	// while `effectiveBitrateKbps` drives `targetBandwidthBits`,
	// `bitsPerFrame`, `bufferSizeBits`, `bufferInitialBits` and
	// `bufferOptimalBits` so the tight 1ms buffer model used by
	// `buffer-1-1-1` lands on the same per-frame budget as libvpx.
	//
	// frameWidth / frameHeight cache the dimensions used to compute
	// the cap; the encoder updates them at construction via
	// `setFrameDimensions` so SetBitrateKbps / SetRateControl can
	// re-derive the cap without re-running through normalizeOptions.
	effectiveBitrateKbps int
	frameWidth           int
	frameHeight          int

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

	framesSinceKeyframe    int
	currentTemporalLayers  int
	currentTemporalLayerID int
	// currentLayerPerFrameBandwidth mirrors libvpx's cpu->per_frame_bandwidth
	// after vp8_new_framerate(cpi, lc->framerate) when TS is active: it is
	// the current layer's `target_bandwidth / framerate`. It feeds the
	// post-pack overspend accumulation (vp8_adjust_key_frame_context /
	// update_golden_frame_stats), so kf/gf_overspend_bits track the
	// layer-specific overhead rather than the encoder-wide one. Zero in
	// non-TS encodes; callers fall back to rc.bitsPerFrame.
	currentLayerPerFrameBandwidth int
	// currentLayerOutputFrameRate mirrors (int)cpi->output_framerate after
	// temporal vp8_new_framerate. estimate_keyframe_frequency reads that
	// layer framerate when spreading TS key-frame overspend.
	currentLayerOutputFrameRate int
	rollingActualBits           int
	rollingTargetBits           int
	longRollingActualBits       int
	longRollingTargetBits       int
	totalActualBits             int64

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
	activeBestQuantizer      int
	// activeWorstQuantizer mirrors libvpx cpi->active_worst_quality between
	// vp8_change_config clamps and calc_pframe_target_size updates.
	activeWorstQuantizer int
	activeWorstQChanged  bool

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

	// thisKeyFrameForced mirrors libvpx's `cpi->this_key_frame_forced` for
	// the active-best-quality clamp at vp8/encoder/onyx_if.c:3636-3642
	// (pass-2 KEY_FRAME branch). When set, libvpx pins
	// `active_best_quality` into the window [avg_frame_qindex >> 2,
	// avg_frame_qindex * 7 / 8] so that a "forced" KF (one emitted because
	// we hit the maximum key-frame interval, not because the codec chose
	// it) keeps its quality close to the surrounding inter frames and does
	// not pop. The encoder driver sets this flag before
	// `selectQuantizerForFrameKind*`.
	thisKeyFrameForced bool

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
	onePassAutoGold        bool
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
	maxRateControlUndershootPct     = 100
	maxRateControlOvershootPct      = 100
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

// applyLibvpxDropFrameThresh mirrors set_vp8e_config +
// vp8_change_config drop fields (vp8_cx_iface.c:334-335, onyx_if.c:1634-1639):
// oxcf->allow_df = (rc_dropframe_thresh > 0),
// oxcf->drop_frames_water_mark = rc_dropframe_thresh,
// cpi->drop_frames_allowed = allow_df && buffered_mode.
func (rc *rateControlState) applyLibvpxDropFrameThresh(thresh int) {
	if thresh > 100 {
		thresh = 100
	}
	if thresh < 0 {
		thresh = 0
	}
	rc.dropFramesWaterMark = thresh
	rc.refreshDropFramesAllowed()
}

func libvpxDropFrameThreshFromConfig(cfg RateControlConfig) int {
	if !cfg.DropFrameAllowed {
		return 0
	}
	if cfg.DropFrameWaterMark == 0 {
		return defaultDropFramesWaterMark
	}
	return cfg.DropFrameWaterMark
}

func (rc *rateControlState) refreshDropFramesAllowed() {
	// allow_df = (rc_dropframe_thresh > 0)
	// drop_frames_allowed = allow_df && buffered_mode
	rc.dropFrameAllowed = rc.dropFramesWaterMark > 0 && rc.bufferOptimalBits > 0
}

// libvpxRateControlTiming returns a timingState keyed off cpi->framerate /
// output_framerate, not the stored g_timebase. libvpx vp8_change_config calls
// vp8_new_framerate(cpi, cpi->framerate) without recomputing framerate from a
// new enc_cfg g_timebase, so per-frame bandwidth and framerate-gated drop
// logic must use outputFrameRate even when EncoderOptions.FPS was updated.
func (rc *rateControlState) libvpxRateControlTiming() timingState {
	if rc.outputFrameRate <= 0 {
		return timingState{}
	}
	return timingState{
		timebaseNum:   1,
		timebaseDen:   rc.outputFrameRate,
		frameDuration: 1,
		frameRate:     float64(rc.outputFrameRate),
	}
}

// applyVP8ChangeConfigQuantizerClamp mirrors vp8_change_config active Q
// clamps (onyx_if.c:1618-1632) after worst_allowed_q / best_allowed_q change.
func (rc *rateControlState) applyVP8ChangeConfigQuantizerClamp() {
	if rc.activeWorstQuantizer > rc.maxQuantizer {
		rc.activeWorstQuantizer = rc.maxQuantizer
	} else if rc.activeWorstQuantizer < rc.minQuantizer {
		rc.activeWorstQuantizer = rc.minQuantizer
	}
	if rc.activeBestQuantizer < rc.minQuantizer {
		rc.activeBestQuantizer = rc.minQuantizer
	} else if rc.activeBestQuantizer > rc.maxQuantizer {
		rc.activeBestQuantizer = rc.maxQuantizer
	}
}

// applyVP8ChangeConfigRateModel mirrors the target/buffer/framerate block in
// vp8_change_config (vp8/encoder/onyx_if.c:1593-1625). Codec controls also
// enter this path through update_extracfg, even when the control value is 0 or
// unchanged.
func (rc *rateControlState) applyVP8ChangeConfigRateModel(twoPassMinPct int) {
	timing := rc.libvpxRateControlTiming()
	if timing.frameRate <= 0 {
		return
	}
	effectiveKbps := rc.libvpxClampToRawTargetRate(rc.targetBitrateKbps, timing)
	targetBits := effectiveKbps * 1000
	if effectiveKbps <= 0 || targetBits/1000 != effectiveKbps {
		return
	}
	rc.effectiveBitrateKbps = effectiveKbps
	rc.targetBandwidthBits = targetBits
	if bitsPerFrame := computeBitsPerFrame(targetBits, timing); bitsPerFrame > 0 {
		rc.bitsPerFrame = bitsPerFrame
		rc.minFrameBandwidth = vbrMinFrameBandwidthBits(bitsPerFrame, twoPassMinPct)
	}
	rc.bufferInitialBits = libvpxVP8BufferBits(rc.bufferInitialSizeMs, targetBits)
	if rc.bufferOptimalSizeMs == 0 {
		rc.bufferOptimalBits = targetBits / 8
	} else {
		rc.bufferOptimalBits = libvpxVP8BufferBits(rc.bufferOptimalSizeMs, targetBits)
	}
	if rc.bufferSizeMs == 0 {
		rc.maximumBufferBits = targetBits / 8
		rc.bufferSizeBits = rc.maximumBufferBits
	} else {
		rc.bufferSizeBits = libvpxVP8BufferBits(rc.bufferSizeMs, targetBits)
		rc.maximumBufferBits = rc.bufferSizeBits
	}
	rc.clampBuffer()
}

func libvpxVP8BufferBits(ms int, targetBandwidthBits int) int {
	if ms <= 0 || targetBandwidthBits <= 0 {
		return 0
	}
	v := int64(ms) * int64(targetBandwidthBits) / 1000
	if v > int64(maxInt()) {
		return maxInt()
	}
	return int(v)
}

func (rc *rateControlState) applyConfig(cfg RateControlConfig, timing timingState) error {
	if err := validateRateControlConfig(cfg); err != nil {
		return err
	}
	initializing := rc.targetBitrateKbps == 0
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
	rc.maxIntraBitratePct = cfg.MaxIntraBitratePct
	rc.gfCBRBoostPct = cfg.GFCBRBoostPct
	if initializing {
		rc.decimationFactor = 0
		rc.decimationCount = 0
		rc.avgFrameQuantizer = rc.maxQuantizer
		rc.normalInterQuantizerTotal = 0
		rc.normalInterFrames = 0
		rc.normalInterAvgQuantizer = rc.maxQuantizer
		rc.rateCorrectionFactor = 1.0
		rc.keyFrameCorrectionFactor = 1.0
		rc.goldenCorrectionFactor = 1.0
		rc.activeBestQuantizer = rc.minQuantizer
		rc.activeWorstQuantizer = rc.maxQuantizer
		rc.totalActualBits = 0
		rc.outputFrameRate = int(outputFrameRate(timing))
	} else {
		rc.activeBestQuantizer = clampQuantizerValue(rc.activeBestQuantizer, rc.minQuantizer, rc.maxQuantizer)
		rc.applyVP8ChangeConfigQuantizerClamp()
	}
	if rc.keyFrameCount == 0 && rc.priorKeyFrameDistance == ([keyFrameContextSize]int{}) {
		// libvpx vp8_create_compressor seeds key_frame_count=1 and every
		// prior_key_frame_distance slot to output_framerate. The first
		// forced keyframe then replaces only the newest slot with the
		// two-second bootstrap estimate, leaving the older slots at FPS.
		rc.keyFrameCount = 1
		for i := range rc.priorKeyFrameDistance {
			rc.priorKeyFrameDistance[i] = rc.outputFrameRate
		}
	}
	if err := rc.setBitrateKbps(cfg.TargetBitrateKbps, timing); err != nil {
		return err
	}
	// setBitrateKbps recomputes bufferOptimalBits; re-derive drop_frames_allowed.
	rc.applyLibvpxDropFrameThresh(libvpxDropFrameThreshFromConfig(cfg))
	if initializing {
		rc.resetRollingBitAverages()
	}
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
	// libvpx vp8/encoder/onyx_if.c set_oxcf clamps the *internal*
	// target_bandwidth to a raw 24bpp uncompressed envelope (raw rate =
	// Width * Height * 8 * 3 * framerate / 1000 kbps); the user-facing
	// rc_target_bitrate is left untouched. Mirror that split so the
	// public bitrate field (targetBitrateKbps) keeps reporting the
	// requested value while the buffer-model / per-frame-budget
	// arithmetic uses the capped effective rate.
	rcTiming := rc.libvpxRateControlTiming()
	if rcTiming.frameRate <= 0 {
		rcTiming = timing
	}
	effectiveKbps := rc.libvpxClampToRawTargetRate(kbps, rcTiming)
	targetBits := effectiveKbps * 1000
	if targetBits/1000 != effectiveKbps {
		return ErrInvalidBitrate
	}

	initializing := rc.targetBitrateKbps == 0
	rc.targetBitrateKbps = kbps
	rc.effectiveBitrateKbps = effectiveKbps
	rc.targetBandwidthBits = targetBits
	rc.bitsPerFrame = computeBitsPerFrame(targetBits, rcTiming)
	if rc.bitsPerFrame <= 0 {
		return ErrInvalidBitrate
	}

	var ok bool
	rc.bufferSizeBits, ok = checkedMul(effectiveKbps, rc.bufferSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.bufferInitialBits, ok = checkedMul(effectiveKbps, rc.bufferInitialSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.bufferOptimalBits, ok = checkedMul(effectiveKbps, rc.bufferOptimalSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	rc.maximumBufferBits = rc.bufferSizeBits
	if initializing && rc.bufferLevelBits == 0 {
		rc.bufferLevelBits = rc.bufferInitialBits
	}
	if initializing {
		rc.frameTargetBits = rc.bitsPerFrame
	}
	rc.clampBuffer()
	rc.clampQuantizer()
	rc.refreshDropFramesAllowed()
	return nil
}

func (rc *rateControlState) refreshFrameRate(timing timingState, twoPassMinPct int) {
	bitsPerFrame := computeBitsPerFrame(rc.targetBandwidthBits, timing)
	if bitsPerFrame > 0 {
		rc.bitsPerFrame = bitsPerFrame
	}
	rc.outputFrameRate = int(outputFrameRate(timing))
	rc.minFrameBandwidth = vbrMinFrameBandwidthBits(rc.bitsPerFrame, twoPassMinPct)
}

// libvpxClampToRawTargetRate returns kbps capped to the libvpx
// raw-target-rate envelope (Width*Height*8*3*fps/1000 kbps). When the
// configured frame dimensions or framerate are zero the clamp is a
// no-op so callers that have not yet configured dimensions get the
// pre-clamp value (matching the old govpx behavior the cap had to be
// retrofitted onto).
func (rc *rateControlState) libvpxClampToRawTargetRate(kbps int, timing timingState) int {
	if kbps <= 0 || rc.frameWidth <= 0 || rc.frameHeight <= 0 {
		return kbps
	}
	fps := outputFrameRate(timing)
	if fps <= 0 {
		return kbps
	}
	rawBits := int64(rc.frameWidth) * int64(rc.frameHeight) * 8 * 3
	if rawBits <= 0 {
		return kbps
	}
	rawKbpsF := float64(rawBits) * fps / 1000.0
	if rawKbpsF <= 0 {
		return kbps
	}
	// libvpx casts the raw_target_rate float through `unsigned int`,
	// which truncates toward zero. Mirror that truncation.
	rawKbps := int(rawKbpsF)
	if rawKbps <= 0 {
		return kbps
	}
	if kbps > rawKbps {
		return rawKbps
	}
	return kbps
}

// setFrameDimensions caches the configured frame width/height so
// setBitrateKbps can apply the libvpx raw-target-rate cap. The encoder
// lifecycle must call this before any setBitrateKbps; subsequent
// resolution changes (SetResolution / Reset) update the cached
// dimensions so SetBitrateKbps re-derives the cap correctly.
func (rc *rateControlState) setFrameDimensions(width int, height int) {
	rc.frameWidth = width
	rc.frameHeight = height
}

func (rc *rateControlState) beginFrame(keyFrame bool) {
	rc.beginFrameWithTargetAndContext(keyFrame, rc.bitsPerFrame, rateControlFrameContext{})
}

type rateControlFrameContext struct {
	firstFrame         bool
	forcedKeyFrame     bool
	temporalLayerCount int
	temporalLayerID    int
	// layerPerFrameBandwidth mirrors libvpx's cpi->per_frame_bandwidth after
	// vp8_new_framerate(cpi, lc->framerate) when TS is active. For non-TS or
	// when zero, callers fall back to rc.bitsPerFrame / baseTargetBits.
	layerPerFrameBandwidth int
	// layerOutputFrameRate mirrors (int)cpi->output_framerate after
	// vp8_new_framerate(cpi, lc->framerate) when TS is active.
	layerOutputFrameRate int
	timing               timingState
}

func (rc *rateControlState) beginFrameWithTargetAndContext(keyFrame bool, baseTargetBits int, ctx rateControlFrameContext) {
	rc.currentTemporalLayers = ctx.temporalLayerCount
	if ctx.temporalLayerCount > 1 {
		rc.currentTemporalLayerID = ctx.temporalLayerID
		if ctx.layerPerFrameBandwidth > 0 {
			rc.currentLayerPerFrameBandwidth = ctx.layerPerFrameBandwidth
		} else if baseTargetBits > 0 {
			rc.currentLayerPerFrameBandwidth = baseTargetBits
		} else {
			rc.currentLayerPerFrameBandwidth = 0
		}
		rc.currentLayerOutputFrameRate = ctx.layerOutputFrameRate
	} else {
		rc.currentTemporalLayerID = 0
		rc.currentLayerPerFrameBandwidth = 0
		rc.currentLayerOutputFrameRate = 0
	}
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
	if !keyFrame {
		if rc.bufferOptimalBits > 0 {
			// calc_pframe_target_size resets active_best_quality to
			// best_quality for one-pass buffered inter frames before the
			// Q regulator runs. Forced key frames can carry a higher
			// active-best from a previous CQ segment, but the first
			// following inter frame starts fresh.
			rc.activeBestQuantizer = rc.minQuantizer
		}
		// libvpx's calc_pframe_target_size always runs the
		// kf_overspend_bits / gf_overspend_bits drain; only
		// cpi->per_frame_bandwidth is swapped to the per-layer
		// avg_frame_size_for_layer when current_layer > 0
		// (vp8/encoder/ratectrl.c:557). Mirror that here so the
		// shared overspend counter drains in TS mode against the
		// layer's per-frame bandwidth. For non-TS the drain
		// consults decimationBoostedBitsPerFrame(), the pre-pick
		// mutation libvpx applies before this branch.
		perFrameBandwidth := rc.decimationBoostedBitsPerFrame()
		if ctx.temporalLayerCount > 1 {
			// libvpx swaps cpi->per_frame_bandwidth to the per-layer
			// avg_frame_size_for_layer when current_layer > 0 (line 557
			// of vp8/encoder/ratectrl.c); for current_layer == 0 it
			// keeps the value just refreshed by vp8_new_framerate
			// against lc->framerate, which equals
			// avg_frame_size_for_layer[0]. Either way the layer's own
			// per-frame bandwidth is what feeds the drain.
			switch {
			case ctx.layerPerFrameBandwidth > 0:
				perFrameBandwidth = ctx.layerPerFrameBandwidth
			case baseTargetBits > 0:
				perFrameBandwidth = baseTargetBits
			}
		}
		targetBits = rc.applyOnePassPFrameOverspendRecovery(targetBits, perFrameBandwidth)
		targetBits = rc.bufferAdjustedFrameTargetBits(targetBits)
	}
	if rc.cqFloorActive() {
		if rc.currentQuantizer < rc.cqLevel {
			rc.currentQuantizer = rc.cqLevel
		}
	}
	rc.frameTargetBits = targetBits
	rc.clampQuantizer()
}

func (rc *rateControlState) beginOnePassAltRefRefreshFrameWithTargetAndContext(baseTargetBits int, ctx rateControlFrameContext) {
	rc.currentTemporalLayers = ctx.temporalLayerCount
	rc.currentTemporalLayerID = ctx.temporalLayerID
	rc.currentLayerPerFrameBandwidth = 0
	rc.currentLayerOutputFrameRate = 0
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = baseTargetBits
	}
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	// libvpx's one-pass calc_pframe_target_size has a special
	// refresh_alt_ref_frame branch with no body. It skips normal p-frame
	// target setup, KF/GF overspend drains, and inter_frame_target refresh,
	// but still falls through the shared min_frame_target sanity clamp before
	// the one-pass buffer adjustment.
	if minFrameTarget := onePassMinFrameTarget(baseTargetBits); targetBits < minFrameTarget {
		targetBits = minFrameTarget
	}
	targetBits = rc.bufferAdjustedFrameTargetBits(targetBits)
	rc.frameTargetBits = targetBits
	rc.clampQuantizer()
}

// applyOnePassPFrameOverspendRecovery mirrors the one-pass non-ARF p-frame
// branch of libvpx's calc_pframe_target_size (vp8/encoder/ratectrl.c). It
// drains accumulated kf_overspend_bits / gf_overspend_bits into the
// per-frame target via kf_bitrate_adjustment / non_gf_bitrate_adjustment,
// clamping to min_frame_target = per_frame_bandwidth/4 in one-pass mode.
// inter_frame_target is captured after recovery (libvpx records it on every
// non-altref normal frame).
func (rc *rateControlState) applyOnePassPFrameOverspendRecovery(targetBits int, perFrameBandwidth int) int {
	if targetBits <= 0 {
		return targetBits
	}
	// Mirror libvpx: the per_frame_bandwidth used for min_frame_target's
	// quarter-floor is the just-boosted (post-vp8_check_drop_buffer) value
	// when decimation is active. Callers pass that in directly so this
	// helper can also serve the temporal-layer branch where libvpx swaps
	// per_frame_bandwidth to layer_context[current_layer].avg_frame_size_for_layer
	// (vp8/encoder/ratectrl.c:557).
	if perFrameBandwidth <= 0 {
		perFrameBandwidth = rc.decimationBoostedBitsPerFrame()
	}
	if perFrameBandwidth <= 0 {
		return targetBits
	}
	minFrameTarget := onePassMinFrameTarget(perFrameBandwidth)
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

func onePassMinFrameTarget(perFrameBandwidth int) int {
	if perFrameBandwidth <= 0 {
		return 0
	}
	return perFrameBandwidth / 4
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
	// Libvpx increments frames_since_key at the end of every visible frame,
	// including the previous key frame. govpx's rolling counter excludes the
	// key frame itself, so add one when mirroring calc_iframe_target_size's
	// short-interval dampening.
	framesSinceKey := rc.framesSinceKeyframe + 1
	if halfFrameRate := libvpxHalfFrameRate(ctx.timing); halfFrameRate > 0 && float64(framesSinceKey) < halfFrameRate {
		boost = int(float64(boost) * float64(framesSinceKey) / halfFrameRate)
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
	if timing.frameRate > 0 {
		return timing.frameRate
	}
	if min(min(timing.timebaseNum, timing.timebaseDen), timing.frameDuration) <= 0 {
		return 0
	}
	return float64(timing.timebaseDen) / (float64(timing.timebaseNum) * float64(timing.frameDuration))
}
