package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/arith"
)

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
	maxInterBitratePct int
	gfCBRBoostPct      int

	// minGFInterval and maxGFInterval mirror libvpx's
	// VP9E_SET_MIN_GF_INTERVAL / VP9E_SET_MAX_GF_INTERVAL controls. Zero
	// leaves the framerate-derived libvpx default in place.
	minGFInterval uint8
	maxGFInterval uint8

	// framePeriodicBoost mirrors VP9E_SET_FRAME_PERIODIC_BOOST. When set,
	// the active-best Q is reduced harder on periodic GF/ALTREF refreshes.
	framePeriodicBoost bool
	// altRefAQ mirrors VP9E_SET_ALT_REF_AQ. libvpx v1.16.0 wires the
	// control but its VP9 alt-ref AQ implementation is a no-op.
	altRefAQ bool
	// postEncodeDrop mirrors VP9E_SET_POSTENCODE_DROP_CBR. When set,
	// inter frames overshooting the target while the buffer is below the
	// drop watermark are dropped from the visible output.
	postEncodeDrop bool
	// disableOvershootMaxQCBR mirrors
	// VP9E_SET_DISABLE_OVERSHOOT_MAXQ_CBR. When set, the CBR active-worst
	// promotion to worstQuality in the critical buffer region is
	// suppressed.
	disableOvershootMaxQCBR bool
	// nextFrameQIndexSet / nextFrameQIndex mirror
	// VP9E_SET_QUANTIZER_ONE_PASS. The qindex is consumed by the next
	// encode call and then cleared.
	nextFrameQIndexSet bool
	nextFrameQIndex    uint8

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

	rateCorrectionFactors [encoder.RateFactorLevels]float64
	dampedAdjustment      [encoder.RateFactorLevels]bool
	q1Frame               uint8
	q2Frame               uint8
	rc1Frame              int8
	rc2Frame              int8

	framesSinceKey uint16
	// framesSinceGolden mirrors libvpx RATE_CONTROL::frames_since_golden.
	// Realtime nonrd pickmode uses it to cap usable_ref_frame to LAST on
	// the first frame after a golden/altref refresh.
	framesSinceGolden uint16
	framesTillGF      uint8
	// altRefGFGroup mirrors libvpx RATE_CONTROL::alt_ref_gf_group.
	// vp9_pick_inter_mode's usable_ref_frame and VBR/lag candidate gates
	// read it to keep ARF groups from being treated like ordinary GF-only
	// refresh intervals.
	//
	// libvpx: vp9_ratectrl.h:182 alt_ref_gf_group.
	altRefGFGroup bool
	// lastFrameIsSrcAltRef mirrors libvpx
	// RATE_CONTROL::last_frame_is_src_altref, updated at one-pass
	// postencode from is_src_frame_alt_ref.
	//
	// libvpx: vp9_ratectrl.h:183 last_frame_is_src_altref,
	// vp9_ratectrl.c:1995 assignment.
	lastFrameIsSrcAltRef bool

	// avgSourceSAD / highSourceSAD / highNumBlocksWithMotion mirror the
	// one-pass scene-detection state consumed by realtime partitioning,
	// cyclic refresh, and drop gating.
	//
	// libvpx: vp9_ratectrl.h:178 avg_source_sad,
	// vp9_ratectrl.h:181 high_num_blocks_with_motion,
	// vp9_ratectrl.h:184 high_source_sad.
	avgSourceSAD            [vp9MaxLookaheadFrames]uint64
	highSourceSAD           bool
	highNumBlocksWithMotion bool

	afRatioOnePassVBR   uint8
	baselineGFInterval  uint8
	facActiveWorstInter uint16
	facActiveWorstGF    uint16

	// gfuBoost mirrors libvpx's rc->gfu_boost, the cumulative ARF/GF group
	// boost computed by define_gf_group via compute_arf_boost. govpx
	// consumes it as the gating signal in adjust_arnr_filter for adaptive
	// temporal-filter strength + frame-count selection.
	//
	// Feed sites:
	//   - NewVP9Encoder seeds DEFAULT_GF_BOOST when LookaheadFrames>0
	//     (vp9_encoder.go), mirroring libvpx vp9_ratectrl.c:2082.
	//   - refreshVP9GFGroupIfDue (vp9_twopass.go) refreshes from
	//     encoder.DefineGFGroup at each GF boundary when two-pass stats are
	//     available, mirroring libvpx vp9_firstpass.c:2761 define_gf_group.
	//
	// libvpx: vp9/encoder/vp9_ratectrl.h RATE_CONTROL::gfu_boost
	gfuBoost uint16

	// rdmult / rddiv mirror libvpx's RD_OPT::RDMULT / RD_OPT::RDDIV.
	// vp9_initialize_rd_consts populates them once at frame start from
	// vp9_compute_rd_mult(base_qindex + y_dc_delta_q) and the constant
	// RDDIV_BITS=7 (vp9/encoder/vp9_rd.c:396-407).  Per-block paths
	// (mode pickers, AQ deltas) read rdmult here and optionally scale it
	// per-SB via cbRdmult before invoking RDCOST.
	rdmult int
	rddiv  int

	// percArfUsage mirrors libvpx's RATE_CONTROL::perc_arf_usage: a smoothed
	// ARF-usage percentage that update_altref_usage refreshes from the
	// per-SB count_arf_frame_usage / count_lastgolden_frame_usage slabs at
	// the end of every non-overlay, non-refresh ARF-group frame. Consumed
	// by the one-pass VBR ARF disable gate at vp9_ratectrl.c:3007-3010
	// (`cpi->rc.perc_arf_usage < 15 && cpi->oxcf.speed >= 5`).
	//
	// libvpx: vp9_ratectrl.h:192 perc_arf_usage,
	// vp9_ratectrl.c:1814-1815 update,
	// vp9_ratectrl.c:3007-3010 consumer.
	percArfUsage float64

	// isSrcFrameAltRef mirrors libvpx's RATE_CONTROL::is_src_frame_alt_ref.
	// Set by vp9_configure_buffer_updates (vp9_ratectrl.c:1655-1697) or
	// check_src_altref (vp9_encoder.c:5810-5822) when the lookahead pops
	// the frame that was scheduled as the ARF source. Read by the speed-5
	// VBR VAR_BASED_PARTITION switch (vp9_speed_features.c:597-600), the
	// altref-onepass FIXED_PARTITION BLOCK_64X64 override
	// (vp9_speed_features.c:828-832), update_altref_usage
	// (vp9_ratectrl.c:1802), and a long list of further rate-control /
	// encoder consumers.
	//
	// libvpx: vp9_ratectrl.h:118 is_src_frame_alt_ref.
	isSrcFrameAltRef bool
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
	if err := validateVP9InterRateBound(opts.MaxInterBitratePct); err != nil {
		return err
	}
	if err := validateVP9GFIntervalBounds(opts.MinGFInterval,
		opts.MaxGFInterval); err != nil {
		return err
	}
	if err := validateVP9NextFrameQIndex(opts.NextFrameQIndex,
		opts.NextFrameQIndexSet, opts.AQMode); err != nil {
		return err
	}
	if opts.PostEncodeDrop && opts.RateControlMode != RateControlCBR {
		return ErrInvalidConfig
	}
	if opts.DisableOvershootMaxQCBR && opts.RateControlMode != RateControlCBR {
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
	if err := validateVP9InterRateBound(opts.MaxInterBitratePct); err != nil {
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

// validateVP9InterRateBound enforces the libvpx VP9 max_inter_bitrate_pct
// invariant: non-negative.
func validateVP9InterRateBound(maxInterPct int) error {
	if maxInterPct < 0 {
		return ErrInvalidConfig
	}
	return nil
}

// validateVP9GFIntervalBounds enforces the libvpx
// VP9E_SET_MIN_GF_INTERVAL / VP9E_SET_MAX_GF_INTERVAL invariants. Both
// values must lie in [0, encoder.MaxGFInterval]; when both are non-zero, the
// minimum must not exceed the maximum.
func validateVP9GFIntervalBounds(minGF, maxGF int) error {
	if minGF < 0 || maxGF < 0 {
		return ErrInvalidConfig
	}
	if minGF > encoder.MaxGFInterval || maxGF > encoder.MaxGFInterval {
		return ErrInvalidConfig
	}
	if minGF > 0 && maxGF > 0 && minGF > maxGF {
		return ErrInvalidConfig
	}
	return nil
}

// validateVP9NextFrameQIndex enforces the libvpx
// VP9E_SET_QUANTIZER_ONE_PASS invariants. The qindex must lie in
// [0, 255]; the override is mutually exclusive with cyclic-refresh and
// perceptual AQ because both rewrite the qindex through segmentation.
func validateVP9NextFrameQIndex(qindex int, set bool, aqMode VP9AQMode) error {
	if !set {
		if qindex != 0 {
			return ErrInvalidQuantizer
		}
		return nil
	}
	if qindex < 0 || qindex > 255 {
		return ErrInvalidQuantizer
	}
	if aqMode == VP9AQCyclicRefresh || aqMode == VP9AQPerceptual {
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
		// NextFrameQIndex still applies to the public-Q (RC-off) path so
		// callers can drive the next encode's qindex directly.
		if opts.NextFrameQIndexSet {
			rc.nextFrameQIndexSet = true
			q := min(max(opts.NextFrameQIndex, 0), 255)
			rc.nextFrameQIndex = uint8(q)
		}
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
	rc.maxInterBitratePct = opts.MaxInterBitratePct
	rc.gfCBRBoostPct = 0
	if rc.mode == RateControlCBR {
		rc.gfCBRBoostPct = opts.GFCBRBoostPct
	}
	rc.minGFInterval = uint8(opts.MinGFInterval)
	rc.maxGFInterval = uint8(opts.MaxGFInterval)
	rc.framePeriodicBoost = opts.FramePeriodicBoost
	rc.altRefAQ = opts.AltRefAQ
	rc.postEncodeDrop = opts.PostEncodeDrop && rc.mode == RateControlCBR
	rc.disableOvershootMaxQCBR = opts.DisableOvershootMaxQCBR &&
		rc.mode == RateControlCBR
	if opts.NextFrameQIndexSet {
		rc.nextFrameQIndexSet = true
		q := min(max(opts.NextFrameQIndex, 0), 255)
		rc.nextFrameQIndex = uint8(q)
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
	targetBits, ok := arith.CheckedMul(kbps, 1000)
	if !ok {
		return ErrInvalidBitrate
	}
	bitsPerFrame := computeVP9BitsPerFrame(targetBits, timing)
	if bitsPerFrame <= 0 {
		return ErrInvalidBitrate
	}
	bufferSizeBits, ok := arith.CheckedMul(kbps, rc.bufferSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	bufferInitialBits, ok := arith.CheckedMul(kbps, rc.bufferInitialSizeMs)
	if !ok {
		return ErrInvalidBitrate
	}
	bufferOptimalBits, ok := arith.CheckedMul(kbps, rc.bufferOptimalSizeMs)
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
	sizeBits, ok := arith.CheckedMul(rc.targetBitrateKbps, sizeMs)
	if !ok {
		return ErrInvalidConfig
	}
	initialBits, ok := arith.CheckedMul(rc.targetBitrateKbps, initialMs)
	if !ok {
		return ErrInvalidConfig
	}
	optimalBits, ok := arith.CheckedMul(rc.targetBitrateKbps, optimalMs)
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
	rc.afRatioOnePassVBR = encoder.DefaultAFRatioOnePassVBR
	rc.facActiveWorstInter = encoder.DefaultActiveWorstInterPct
	rc.facActiveWorstGF = encoder.DefaultActiveWorstGFPct
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
	rc.afRatioOnePassVBR = encoder.DefaultAFRatioOnePassVBR
	if rc.mode == RateControlQ {
		rc.baselineGFInterval = encoder.FixedGFInterval
		return
	}
	minInterval := vp9DefaultMinGFInterval(timing)
	maxInterval := vp9DefaultMaxGFInterval(timing, minInterval)
	if rc.minGFInterval > 0 {
		minInterval = int(rc.minGFInterval)
	}
	if rc.maxGFInterval > 0 {
		maxInterval = max(int(rc.maxGFInterval), minInterval)
	} else if rc.minGFInterval > 0 && maxInterval < minInterval {
		maxInterval = minInterval
	}
	rc.baselineGFInterval = uint8((minInterval + maxInterval) >> 1)
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
	if rc.mode == RateControlCBR {
		rc.frameTargetBits = rc.onePassCBRInterFrameTargetBits(refreshFlags)
		return
	}
	rc.frameTargetBits = rc.interFrameTargetBits()
}

func (rc *vp9RateControlState) preEncodeFrame(showFrame bool) {
	if !rc.enabled || !showFrame {
		return
	}
	rc.bufferLevelBits = arith.SaturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
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

func (rc *vp9RateControlState) postEncodeFrame(sizeBytes int, showFrame bool, qindex int, intraOnly bool, refreshFlags uint8, macroblocks int, altRefEnabled bool) {
	if !rc.enabled {
		return
	}
	encodedBits := encodedSizeBits(sizeBytes)
	rc.updateRateCorrectionFactor(encodedBits, qindex, intraOnly, refreshFlags, macroblocks)
	rc.updateQHistoryWithAltRef(qindex, intraOnly, refreshFlags, showFrame, altRefEnabled)
	rc.lastFrameIsSrcAltRef = rc.isSrcFrameAltRef
	rc.postOnePassVBRRefresh(refreshFlags)
	rc.totalActualBits += int64(encodedBits)
	if showFrame {
		rc.totalTargetBits += int64(rc.bitsPerFrame)
	}
	rc.bufferLevelBits = vp9PostEncodeBufferLevel(rc.bufferLevelBits,
		rc.bufferSizeBits, encodedBits)
}

// vp9PostEncodeDropOvershootFactor scales frameTargetBits when deciding
// whether to drop a CBR inter frame after encoding it. Mirrors libvpx
// VP9E_SET_POSTENCODE_DROP_CBR's overshoot trigger which fires when the
// actual frame bits exceed roughly 8x the frame target while the buffer
// dropped below the configured watermark.
const vp9PostEncodeDropOvershootFactor = 8

// shouldPostEncodeDrop mirrors libvpx's CBR post-encode drop check. The
// drop fires for inter frames when (a) post-encode drop is enabled, (b)
// CBR drop is allowed with a configured watermark, (c) the encoded
// bits exceed the frame-target overshoot threshold, and (d) the
// post-encode buffer level has fallen below the drop watermark or
// turned negative.
func (rc *vp9RateControlState) shouldPostEncodeDrop(intraOnly bool, showFrame bool, encodedBits int) bool {
	if rc == nil || !rc.enabled || !rc.postEncodeDrop || intraOnly || !showFrame {
		return false
	}
	if rc.mode != RateControlCBR || !rc.dropFrameAllowed ||
		rc.dropFramesWaterMark == 0 || rc.bufferOptimalBits <= 0 {
		return false
	}
	target := rc.frameTargetBits
	if target <= 0 {
		return false
	}
	overshootBits := target * vp9PostEncodeDropOvershootFactor
	if encodedBits <= overshootBits {
		return false
	}
	dropMark := int(rc.dropFramesWaterMark) * rc.bufferOptimalBits / 100
	level := vp9PostEncodeBufferLevel(rc.bufferLevelBits, rc.bufferSizeBits,
		encodedBits)
	return level < 0 || level < dropMark
}

// postEncodeDropFrame rolls rate-control state forward as if the
// just-encoded frame had been dropped: the buffer level credits the
// per-frame bandwidth, the visible frame counter advances, and the
// rate-correction / qindex history reverts to the pre-encode snapshot.
// Mirrors libvpx's CBR post-encode drop bookkeeping.
func (rc *vp9RateControlState) postEncodeDropFrame() {
	if rc == nil || !rc.enabled {
		return
	}
	rc.rc2Frame = 0
	rc.rc1Frame = 0
	rc.lastQInter = rc.q1Frame
	rc.bufferLevelBits = arith.SaturatingAdd(rc.bufferLevelBits, rc.bitsPerFrame)
	rc.clampBuffer()
	rc.incrementFramesSinceKey()
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
	rc.minFrameBandwidth = encoder.FrameOverhead
	if rc.bitsPerFrame > 0 && rc.bitsPerFrame>>5 > rc.minFrameBandwidth {
		rc.minFrameBandwidth = rc.bitsPerFrame >> 5
	}
	miRows := (int(rc.codedHeight) + 7) >> 3
	miCols := (int(rc.codedWidth) + 7) >> 3
	mbs := encoder.MacroblockCount(miRows, miCols)
	maxByMB := 0
	if mbs > 0 {
		if mbs > maxInt()/encoder.MaxMBRateBits {
			maxByMB = maxInt()
		} else {
			maxByMB = mbs * encoder.MaxMBRateBits
		}
	}
	maxBits := max(maxByMB, encoder.MaxRate1080PBits)
	if rc.bitsPerFrame > 0 {
		vbrMax := arith.PercentOf(rc.bitsPerFrame, encoder.DefaultVBRMaxSectionPct)
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
