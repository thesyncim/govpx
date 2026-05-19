package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

const interFrameFullPixelSearchRadius = 16
const interFrameMVFullPixelStep = 8
const interFrameSubpixelSearchMaxCandidates = 31
const interFrameMotionCandidateMax = 15
const interFrameMaxMVSearchSteps = 8
const interFrameMaxFirstStep = 1 << (interFrameMaxMVSearchSteps - 1)
const interFrameSplitMVFullSearchThreshold = 4000
const interFrameMaxFullPelVal = (1 << interFrameMaxMVSearchSteps) - 1
const interFrameUMVBorderPixels = 32
const libvpxFastNewMVBitCostWeight = 128
const libvpxRDNewMVBitCostWeight = 96

type interAnalysisFullPixelSearchMethod uint8

const (
	interAnalysisFullPixelSearchExhaustive interAnalysisFullPixelSearchMethod = iota
	interAnalysisFullPixelSearchNstep
	interAnalysisFullPixelSearchDiamond
	interAnalysisFullPixelSearchHex
)

type interAnalysisFractionalSearchMethod uint8

const (
	interAnalysisFractionalSearchIterative interAnalysisFractionalSearchMethod = iota
	interAnalysisFractionalSearchStep
	interAnalysisFractionalSearchHalf
	interAnalysisFractionalSearchSkip
)

type interAnalysisSearchConfig struct {
	fullPixelSearch       interAnalysisFullPixelSearchMethod
	fullPixelSearchParam  int8
	fullPixelFurtherSteps int8
	fullPixelFinalRefine  bool
	fullPixelSpeed        int8
	fullPixelSpeedAdjust  int8
	improvedMVPrediction  bool
	fractionalSearch      interAnalysisFractionalSearchMethod
}

var (
	interAnalysisBestQualitySplitPartitionOrder = [vp8tables.NumMBSplits]int{0, 1, 2, 3}
	interAnalysisSpeedSplitPartitionOrder       = [vp8tables.NumMBSplits]int{2, 1, 0, 3}
)

func defaultInterAnalysisSearchConfig() interAnalysisSearchConfig {
	return interAnalysisSearchConfig{
		fullPixelSearch:  interAnalysisFullPixelSearchExhaustive,
		fractionalSearch: interAnalysisFractionalSearchIterative,
	}
}

// interAnalysisSearchConfig mirrors the VP8 speed-feature branch in
// onyx_if.c: realtime speed > 4 uses vp8_hex_search and disables the
// iterative sub-pixel function pointer.
func (e *VP8Encoder) interAnalysisSearchConfig() interAnalysisSearchConfig {
	cfg := defaultInterAnalysisSearchConfig()
	speed := e.libvpxCPUUsed()
	cfg.fullPixelSearch = interAnalysisFullPixelSearchNstep
	cfg.fullPixelSearchParam = int8(libvpxInterFrameSearchParamForFeatureSpeed(e.opts.Deadline, speed))
	cfg.fullPixelFinalRefine = e.interAnalysisUsesRDModeDecision()
	cfg.fullPixelSpeed = int8(speed)
	cfg.fullPixelSpeedAdjust = int8(libvpxInterFrameSpeedAdjust(speed))
	furtherStepsSpeed := speed
	if e.interAnalysisUsesRDModeDecision() {
		cfg.fullPixelSearchParam = int8(libvpxInterFrameFirstStepForFeatureSpeed(e.opts.Deadline, speed))
		cfg.fullPixelSpeedAdjust = 0
		if e.opts.Deadline == DeadlineBestQuality {
			furtherStepsSpeed = 0
		}
	}
	cfg.fullPixelFurtherSteps = int8(libvpxInterFrameFurtherSteps(furtherStepsSpeed, int(cfg.fullPixelSearchParam)))
	// Task #350: improved_mv_pred uses the libvpx-realistic cpi->Speed
	// (cpu_used+1 at cpu_used > 0 RT after frame 0) rather than the pin-
	// suppressed e.autoSpeed. See libvpxRealtimeCPISpeedForImprovedMVPred
	// Gate comment for the rationale: clamping autoSpeed itself would
	// cascade every other Speed-conditioned feature simultaneously and
	// crater BD-rate (~+28923% on the cpu=8 RT 720p fixture). Targeting
	// only this gate matches the audit-observed Speed=9 → Speed > 6
	// transition without disturbing the rest of the speed cascade.
	cfg.improvedMVPrediction = libvpxInterFrameImprovedMVPredictionForFeatureSpeed(e.opts.Deadline, e.libvpxRealtimeCPISpeedForImprovedMVPredGate())
	if e.opts.Deadline != DeadlineRealtime {
		return cfg
	}
	// Task #361: search_method=HEX / iterative_sub_pixel=0 gate uses the
	// libvpx-realistic cpi->Speed (cpu_used+1 at cpu_used > 0 RT after
	// frame 0) rather than the pin-suppressed e.autoSpeed. See
	// libvpxRealtimeCPISpeedForHEXSearchGate comment for the rationale:
	// clamping autoSpeed itself would cascade every other Speed-conditioned
	// feature simultaneously and crater BD-rate. Targeting only this gate
	// matches the audit-observed Speed=9 → Speed > 4 transition without
	// disturbing the rest of the speed cascade. The cpu_used == 0 RT path
	// (byte-parity gate) keeps the realistic Speed at libvpxCPUUsed()=4,
	// below the Speed > 4 threshold, so NSTEP+iterative are preserved
	// and the task #272 campaign sentinel + task #332 threads validation
	// sentinel byte-parity hold.
	hexSearchSpeed := e.libvpxRealtimeCPISpeedForHEXSearchGate()
	if hexSearchSpeed > 4 {
		cfg.fullPixelSearch = interAnalysisFullPixelSearchHex
		cfg.fractionalSearch = interAnalysisFractionalSearchStep
	}
	if speed > 8 {
		cfg.fractionalSearch = interAnalysisFractionalSearchHalf
	}
	if speed >= 15 {
		cfg.fractionalSearch = interAnalysisFractionalSearchSkip
	}
	return cfg
}

func (e *VP8Encoder) interAnalysisCompressorSpeed() int {
	if e.opts.Deadline == DeadlineBestQuality {
		return 0
	}
	if e.opts.Deadline == DeadlineRealtime {
		return 2
	}
	return 1
}

func (e *VP8Encoder) interAnalysisUsesRDModeDecision() bool {
	switch e.opts.Deadline {
	case DeadlineBestQuality:
		return true
	case DeadlineGoodQuality, DeadlineRealtime:
		return e.libvpxCPUUsed() <= 3
	default:
		return true
	}
}

func (e *VP8Encoder) libvpxOptimizeCoefficients() bool {
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() <= 0
	default:
		return true
	}
}

func (e *VP8Encoder) libvpxUseFastQuant() bool {
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return e.libvpxCPUUsed() > 0
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() > 2
	default:
		return false
	}
}

func (e *VP8Encoder) libvpxUseFastQuantForPick() bool {
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return e.libvpxCPUUsed() > 0
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() > 0
	default:
		return false
	}
}

func (e *VP8Encoder) currentMotionVectorCostTables() *vp8enc.MotionVectorCostTables {
	if !e.mvCostTablesValid {
		e.resetMotionVectorCostTablesFromModeProbs()
	}
	return &e.mvCostTables
}

func (e *VP8Encoder) resetMotionVectorCostTablesFromModeProbs() {
	e.mvCostTables.Build(&e.modeProbs.MV)
	e.mvCostProbs = e.modeProbs.MV
	e.mvCostTablesValid = true
}

func (e *VP8Encoder) updateMotionVectorCostTablesFromInterAttempt(attempt interFrameEncodeAttempt) {
	if !e.mvCostTablesValid {
		e.resetMotionVectorCostTablesFromModeProbs()
	}
	var update [2]bool
	for component := range 2 {
		for i := range vp8tables.MVPCount {
			if attempt.Config.MVUpdate[component][i] {
				update[component] = true
				break
			}
		}
	}
	if !update[0] && !update[1] {
		return
	}
	e.mvCostTables.BuildComponents(&attempt.FrameMVProbs, update)
	for component := range 2 {
		if update[component] {
			e.mvCostProbs[component] = attempt.FrameMVProbs[component]
		}
	}
	e.mvCostTablesValid = true
}

// libvpxUseFastIntraPick returns true when libvpx would pick a keyframe
// macroblock via the pixel-domain pickinter.c vp8_pick_intra_mode helper
// rather than the full transform-domain rdopt.c vp8_rd_pick_intra_mode. The
// dispatch is `cpi->sf.RD == 0 || cpi->compressor_speed == 2 (realtime)`,
// which means realtime always uses the fast picker, and good-quality
// switches to it once cpu-used > 3 (when sf->RD is turned off).
func (e *VP8Encoder) libvpxUseFastIntraPick() bool {
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return true
	case DeadlineGoodQuality:
		return e.libvpxCPUUsed() > 3
	default:
		return false
	}
}

func (e *VP8Encoder) interAnalysisSplitPartitionOrder() [vp8tables.NumMBSplits]int {
	if e.interAnalysisCompressorSpeed() == 0 {
		return interAnalysisBestQualitySplitPartitionOrder
	}
	return interAnalysisSpeedSplitPartitionOrder
}

func (e *VP8Encoder) interAnalysisNoSkipBlock4x4Search() bool {
	return e.interAnalysisCompressorSpeed() == 0 || e.libvpxCPUUsed() <= 0
}

func libvpxInterFrameSearchParamForFeatureSpeed(deadline Deadline, speed int) int {
	firstStep := libvpxInterFrameFirstStepForFeatureSpeed(deadline, speed)
	stepParam := firstStep + libvpxInterFrameSpeedAdjust(speed)
	if stepParam < 0 {
		return 0
	}
	if stepParam >= interFrameMaxMVSearchSteps {
		return interFrameMaxMVSearchSteps - 1
	}
	return stepParam
}

func libvpxInterFrameFirstStepForFeatureSpeed(deadline Deadline, speed int) int {
	if deadline != DeadlineBestQuality && speed > 0 {
		return 1
	}
	return 0
}

func libvpxInterFrameSpeedAdjust(speed int) int {
	if speed > 5 {
		if speed >= 8 {
			return 3
		}
		return 2
	}
	return 1
}

func libvpxInterFrameFurtherSteps(speed int, stepParam int) int {
	if speed >= 8 {
		return 0
	}
	further := interFrameMaxMVSearchSteps - 1 - stepParam
	if further < 0 {
		return 0
	}
	return further
}

func libvpxInterFrameImprovedMVPrediction(deadline Deadline, speed int) bool {
	speed = libvpxSpeedFeatureCPUUsed(deadline, speed)
	return libvpxInterFrameImprovedMVPredictionForFeatureSpeed(deadline, speed)
}

func libvpxInterFrameImprovedMVPredictionForFeatureSpeed(deadline Deadline, speed int) bool {
	// libvpx vp8_set_speed_features (vp8/encoder/onyx_if.c:802/888/957/
	// 1009): improved_mv_pred defaults to 1, then case 2 (realtime,
	// Mode==2) gates it off when `Speed > 6`. The local Speed is reset to
	// the raw cpi->Speed at line 888 BEFORE the case-2 cascade runs (the
	// line-817 `RT(cpi->Speed) = cpi->Speed + 7` mapping is in scope only
	// between lines 817-887 for the thresh_mult / mode_check_freq
	// speed_map lookups), so the gate evaluates against the raw
	// cpi->Speed. govpx mirrors this with `speed <= 6` where `speed` is
	// the autoSpeed value flowing through libvpxCPUUsed -- which tracks
	// libvpx's cpi->Speed evolution per vp8_auto_select_speed
	// (rdopt.c:261-316), modulo the task #278 inter-frame wall-clock pin.
	// Task #348 audit: a residual divergence at the task-343 720p RT
	// cpu=8 fixture frame 2 MB(0,0) NEWMV traces back to govpx's
	// autoSpeed staying in the Speed=0 stable region while libvpx's
	// cpi->Speed auto-evolved to 9 — the line-957 gate sees different
	// inputs on the two sides and improved_mv_pred ends up enabled on
	// govpx but disabled on libvpx. Closing that gap requires aligning
	// the autoSpeed evolution under the budget/3 wall-clock pin, not the
	// improved_mv_pred gate semantics itself.
	return deadline != DeadlineRealtime || speed <= 6
}

func (e *VP8Encoder) interModeRDThresholds(qIndex int) [libvpxInterModeCount]int {
	return e.interModeRDThresholdsForReferences(qIndex, nil, 0)
}

// interRDThreshBaselineSlotCount caps the per-frame qIndex cache used by the
// fast/RD inter-mode picker thresholds. VP8 segmentation produces at most 4
// distinct quantizers per frame; a 4-slot LRU is enough to absorb the entire
// per-MB call sequence without falling back to the heavy
// libvpxInterModeRDThresholdsForContext recompute.
const interRDThreshBaselineSlotCount = 4

// interRDThreshBaselineSlot caches one (qIndex, refSig, baseline) entry. gen
// matches the encoder's interRDThreshBaselineGen at the time the slot was
// filled; a stale gen invalidates the slot without an explicit clear at frame
// start. refSig packs the threshold-context inputs that depend on the refs
// list (refCount, lastEnabled, goldenEnabled, closestRef, refFrameCount) plus
// zbinOverQuant — so the cache stays correct if a caller drives the picker
// with shifting refs without an intervening beginInterRDModeDecisionFrame.
type interRDThreshBaselineSlot struct {
	gen      uint32
	qIndex   int32
	refSig   uint32
	valid    bool
	baseline [libvpxInterModeCount]int
}

// interRDThreshBaselineRefSig packs the refs-derived threshold-context inputs
// into a uint32 fingerprint. The packing is dense — refCount fits in 8 bits,
// closestRef in 8 bits, refFrameCount in 8 bits, lastEnabled+goldenEnabled in
// 2 bits, leaving 6 bits for zbinOverQuant. Within VP8 zbinOverQuant is
// bounded to a few small values, so 6 bits is plenty; if it ever overflows
// the bit field the cache simply collides with another zbinOverQuant value
// (which forces a recompute on the next call — correct, just unhelpful).
func interRDThreshBaselineRefSig(refCount int, lastEnabled bool, goldenEnabled bool, closestRef vp8common.MVReferenceFrame, refFrameCount int, zbinOverQuant int) uint32 {
	var sig uint32
	sig |= uint32(uint8(refCount))
	sig |= uint32(uint8(closestRef)) << 8
	sig |= uint32(uint8(refFrameCount)) << 16
	if lastEnabled {
		sig |= 1 << 24
	}
	if goldenEnabled {
		sig |= 1 << 25
	}
	sig |= uint32(uint8(zbinOverQuant)&0x3F) << 26
	return sig
}

// interModeRDThresholdsBaseline returns the picker-threshold baseline for
// (qIndex, current frame refs/error-bins/speed) and caches the result by
// (qIndex, refSig) within the current frame. Within a frame the only per-MB-
// variable input is qIndex (via cyclic-refresh segmentation); the rest of
// the threshold-context inputs (refs, errorBins, speed, deadline, totalMBs,
// staticThreshold, temporalLayers, zbinOverQuant) are frame-stable, so the
// expensive libvpxInterModeRDThresholdsForContext math (math.Pow, 8 speed
// maps, 1024-bin error scan) runs at most 4× per frame instead of per-MB.
//
// The refSig fingerprint captures (refCount, lastEnabled, goldenEnabled,
// closestRef, refFrameCount, zbinOverQuant) so the cache stays correct under
// callers that mutate refs / referenceFrameNumbers between calls within the
// same generation (e.g. test fixtures that re-call the helper with shifted
// closest-ref distances). Building the fingerprint is cheap relative to the
// cached body and only walks the refs slice once.
func (e *VP8Encoder) interModeRDThresholdsBaseline(qIndex int, refs []interAnalysisReference, refCount int) [libvpxInterModeCount]int {
	context := libvpxInterModeThresholdContext{}
	if refCount > 0 {
		context.temporalLayers = e.libvpxTemporalLayerCount()
		context.lastEnabled = interAnalysisReferencesInclude(refs, refCount, vp8common.LastFrame)
		context.goldenEnabled = interAnalysisReferencesInclude(refs, refCount, vp8common.GoldenFrame)
		context.closestRef = e.closestInterAnalysisReference(refs, refCount)
		context.refFrameCount = 1 + interAnalysisValidReferenceCount(refs, refCount)
	}
	context.totalMBs = e.interAnalysisMacroblockCount()
	context.staticThreshold = e.opts.StaticThreshold
	context.errorBins = &e.interModeSpeedErrorBins
	zbinOverQuant := e.rc.currentZbinOverQuant
	gen := e.interRDThreshBaselineGen
	q32 := int32(qIndex)
	refSig := interRDThreshBaselineRefSig(refCount, context.lastEnabled, context.goldenEnabled, context.closestRef, context.refFrameCount, zbinOverQuant)
	slots := &e.interRDThreshBaselineSlots
	for i := range slots {
		slot := &slots[i]
		if slot.valid && slot.gen == gen && slot.qIndex == q32 && slot.refSig == refSig {
			return slot.baseline
		}
	}
	var baseline [libvpxInterModeCount]int
	if e.libvpxAutoSelectSpeedActive() {
		// libvpx vp8_initialize_rd_consts (rdopt.c:163, called from
		// vp8_encode_frame each frame after vp8_auto_select_speed has run)
		// invokes vp8_set_speed_features, so the thresh_mult tables track the
		// auto-evolved cpi->Speed every frame rather than being frozen at
		// init. Mirror that by feeding the dynamic autoSpeed (via
		// libvpxCPUUsed) straight into the *ForCPISpeed helper — the legacy
		// negate-pass-through trick (`cpuUsedForThresholds = -libvpxCPUUsed()`
		// fed through libvpxSpeedFeatureCPUUsed) collides on autoSpeed=0
		// because `-0` is non-negative and is interpreted as "raw cpu_used 0"
		// (the cold-start default of 4) rather than the actual Speed=0.
		baseline = libvpxInterModeRDThresholdsForCPISpeed(qIndex, zbinOverQuant, e.opts.Deadline, e.libvpxCPUUsed(), context)
	} else {
		baseline = libvpxInterModeRDThresholdsForContext(qIndex, zbinOverQuant, e.opts.Deadline, e.opts.CpuUsed, context)
	}
	// Pick the first invalid/stale slot, else replace slot 0 (LRU is fine
	// here — at most 4 distinct (qIndex, refSig) pairs per frame so
	// collisions are rare).
	victim := 0
	for i := range slots {
		slot := &slots[i]
		if !slot.valid || slot.gen != gen {
			victim = i
			break
		}
	}
	// victim is 0 or a loop index in [0, interRDThreshBaselineSlotCount=4),
	// so AND-mask with 3 elides the bounds check on the [4]slot array.
	slot := &slots[victim&3]
	slot.valid = true
	slot.gen = gen
	slot.qIndex = q32
	slot.refSig = refSig
	slot.baseline = baseline
	return baseline
}

func (e *VP8Encoder) interModeRDThresholdsForReferences(qIndex int, refs []interAnalysisReference, refCount int) [libvpxInterModeCount]int {
	thresholds, _ := e.interModeRDThresholdsAndBaselineForReferences(qIndex, refs, refCount)
	return thresholds
}

func (e *VP8Encoder) interModeRDThresholdsAndBaselineForReferences(qIndex int, refs []interAnalysisReference, refCount int) ([libvpxInterModeCount]int, [libvpxInterModeCount]int) {
	baselineQIndex := e.interModeRDThresholdQIndex(qIndex)
	baseline := e.interModeRDThresholdsBaseline(baselineQIndex, refs, refCount)
	if !e.interRDFrameActive {
		return baseline, baseline
	}
	thresholds := baseline
	touched := &e.interRDThreshTouched
	mult := &e.interRDThreshMult
	for i := range thresholds {
		v := thresholds[i]
		if v == libvpxInterModeThresholdDisabled {
			continue
		}
		if touched[i] {
			thresholds[i] = (v >> 7) * mult[i]
		}
	}
	return thresholds, baseline
}

func interModeRDBestThresholdLowerAllowed(baseline [libvpxInterModeCount]int, modeIndex int) bool {
	if uint(modeIndex) >= uint(libvpxInterModeCount) {
		return false
	}
	threshold := baseline[modeIndex]
	return threshold > 0 && threshold < (maxInt()>>2)
}

func (e *VP8Encoder) interModeRDThresholdQIndex(qIndex int) int {
	if !e.interRDFrameActive {
		return qIndex
	}
	return e.interRDFrameBaseQIndex
}

func (e *VP8Encoder) libvpxTemporalLayerCount() int {
	if !e.opts.TemporalScalability.Enabled {
		return 1
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	if !ok {
		return 1
	}
	return pattern.Layers
}

func (e *VP8Encoder) resetInterRDThresholdMultipliers() {
	for i := range e.interRDThreshMult {
		e.interRDThreshMult[i] = libvpxRDThreshMultStart
	}
	e.interRDThreshTouched = [libvpxInterModeCount]bool{}
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFar = 0
	// Bump the baseline cache generation so a follow-up frame doesn't
	// reuse a stale entry whose context inputs may have shifted.
	e.interRDThreshBaselineGen++
	e.interRDFrameRefSearchOrderValid = false
}

func (e *VP8Encoder) beginInterRDModeDecisionFrame() {
	e.interRDFrameBaseQIndex = vp8common.ClampQIndex(e.rc.currentQuantizer)
	for i, mult := range e.interRDThreshMult {
		if mult == 0 {
			e.interRDThreshMult[i] = libvpxRDThreshMultStart
		}
	}
	e.interRDThreshTouched = [libvpxInterModeCount]bool{}
	if e.libvpxAutoSelectSpeedActive() {
		// Same per-frame vp8_set_speed_features re-run as above; feed the
		// auto-evolved Speed straight into the mode-check-freq speed_map
		// lookups via the *ForCPISpeed helper. The legacy
		// `cpuUsedForFreq = -libvpxCPUUsed()` negate-pass-through path
		// collapses Speed=0 to the cold-start default of 4 (see
		// libvpxInterModeThresholdMultipliersForCPISpeed).
		e.interModeCheckFreq = libvpxInterModeCheckFrequenciesForCPISpeed(e.opts.Deadline, e.libvpxCPUUsed())
	} else {
		e.interModeCheckFreq = libvpxInterModeCheckFrequencies(e.opts.Deadline, e.opts.CpuUsed)
	}
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFar = 0
	e.interModeSpeedErrorBins = e.interModeErrorBins
	e.interModeErrorBins = [1024]uint32{}
	e.interRDFrameActive = true
	// Bump the per-frame baseline-threshold cache generation so the prior
	// frame's cached entries miss without an explicit clear.
	e.interRDThreshBaselineGen++
	// Also invalidate the per-frame ref-search-order pre-bind so the next
	// picker call recomputes it from this frame's refs.
	e.interRDFrameRefSearchOrderValid = false
}

func (e *VP8Encoder) endInterRDModeDecisionFrame() {
	e.interRDFrameActive = false
}

func (e *VP8Encoder) beginInterRDModeDecisionMacroblock() {
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionFrame()
	}
	e.interMBsTestedSoFar++
}

// interRDModeTestAllowed gates per-mode candidate evaluation by libvpx's
// rd_threshes hit-count throttle. The hot path passes a live encoder and a
// 0..libvpxInterModeCount-1 modeIndex (callers iterate
// libvpxFastInterModeOrder); the range check remains for package-level tests
// that exercise the safe entry point directly.
func (e *VP8Encoder) interRDModeTestAllowed(modeIndex int) bool {
	if !e.interRDFrameActive {
		return true
	}
	if uint(modeIndex) >= uint(libvpxInterModeCount) {
		return true
	}
	return e.interRDModeTestAllowedFast(modeIndex)
}

// interRDModeTestAllowedFast is the picker hot-path variant: e is non-nil,
// e.interRDFrameActive is true, and modeIndex is in
// [0, libvpxInterModeCount). Splitting the cheap predicate from the safe
// public entry point keeps both small enough that the picker can inline the
// fast path while tests keep the nil-/range-tolerant entry.
func (e *VP8Encoder) interRDModeTestAllowedFast(modeIndex int) bool {
	hits := e.interModeTestHitCounts[modeIndex]
	freq := e.interModeCheckFreq[modeIndex]
	if hits == 0 || freq <= 1 || e.interMBsTestedSoFar > freq*hits {
		return true
	}
	e.raiseInterRDThreshold(modeIndex)
	return false
}

func (e *VP8Encoder) recordInterRDModeTest(modeIndex int) {
	if !e.interRDFrameActive || uint(modeIndex) >= uint(libvpxInterModeCount) {
		return
	}
	e.interModeTestHitCounts[modeIndex]++
}

func (e *VP8Encoder) lowerInterRDThresholdForImprovement(modeIndex int) {
	if uint(modeIndex) >= uint(libvpxInterModeCount) {
		return
	}
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+2 {
		e.interRDThreshMult[modeIndex] -= 2
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) raiseInterRDThreshold(modeIndex int) {
	if uint(modeIndex) >= uint(libvpxInterModeCount) {
		return
	}
	e.interRDThreshMult[modeIndex] += 4
	if e.interRDThreshMult[modeIndex] > libvpxMaxThreshMult {
		e.interRDThreshMult[modeIndex] = libvpxMaxThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) lowerBestInterRDThreshold(modeIndex int) {
	if uint(modeIndex) >= uint(libvpxInterModeCount) {
		return
	}
	bestAdjustment := e.interRDThreshMult[modeIndex] >> 2
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+bestAdjustment {
		e.interRDThreshMult[modeIndex] -= bestAdjustment
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) lowerBestInterFastThreshold(modeIndex int) {
	if uint(modeIndex) >= uint(libvpxInterModeCount) {
		return
	}
	bestAdjustment := e.interRDThreshMult[modeIndex] >> 3
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+bestAdjustment {
		e.interRDThreshMult[modeIndex] -= bestAdjustment
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) recordFastInterModeErrorBin(distortion int) {
	if distortion < 0 {
		distortion = 0
	}
	bin := distortion >> 7
	if bin >= len(e.interModeErrorBins) {
		bin = len(e.interModeErrorBins) - 1
	}
	// interModeErrorBins is [1024]uint32 (pow2); bin is in [0, 1024) by
	// the guards above. AND-mask with 1023 elides the bounds check on
	// the indexed increment.
	e.interModeErrorBins[bin&1023]++
}

func libvpxInterModeRDThresholds(qIndex int, zbinOverQuant int, deadline Deadline, speed int) [libvpxInterModeCount]int {
	return libvpxInterModeRDThresholdsForContext(qIndex, zbinOverQuant, deadline, speed, libvpxInterModeThresholdContext{})
}

// libvpxInterModeRDThresholdsForContext computes per-mode RD thresholds. The
// `speed` parameter is interpreted via libvpxSpeedFeatureCPUUsed (raw
// configured cpu_used or `-autoSelectedSpeed` sentinel for the negate-pass-
// through realtime path). For the explicit autoSpeed=0 case (post-
// vp8_change_config Speed reset, before the next vp8_auto_select_speed cold-
// start fires) use libvpxInterModeRDThresholdsForCPISpeed which bypasses the
// SpeedFeatureCPUUsed translation — `-0 = 0` collides with the "raw
// cpu_used 0 → 4 default" mapping.
func libvpxInterModeRDThresholdsForContext(qIndex int, zbinOverQuant int, deadline Deadline, speed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	multipliers := libvpxInterModeThresholdMultipliersForContext(deadline, speed, context)
	return libvpxInterModeRDThresholdsFromMultipliers(qIndex, zbinOverQuant, multipliers)
}

// libvpxInterModeRDThresholdsForCPISpeed mirrors
// libvpxInterModeRDThresholdsForContext but with cpiSpeed already in hand.
// See libvpxInterModeThresholdMultipliersForCPISpeed for the rationale: the
// legacy negate-pass-through `-libvpxCPUUsed()` argument collides on
// autoSpeed=0 because `-0` is not negative and is interpreted as "raw
// cpu_used 0" by libvpxSpeedFeatureCPUUsed.
func libvpxInterModeRDThresholdsForCPISpeed(qIndex int, zbinOverQuant int, deadline Deadline, cpiSpeed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	multipliers := libvpxInterModeThresholdMultipliersForCPISpeed(deadline, cpiSpeed, context)
	return libvpxInterModeRDThresholdsFromMultipliers(qIndex, zbinOverQuant, multipliers)
}

func libvpxInterModeRDThresholdsFromMultipliers(qIndex int, zbinOverQuant int, multipliers [libvpxInterModeCount]int) [libvpxInterModeCount]int {
	qValue := min(vp8common.DCQuant(qIndex, 0), 160)
	q := max(int(math.Pow(float64(qValue), 1.25)), 8)
	_, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	var thresholds [libvpxInterModeCount]int
	for i, mult := range multipliers {
		if mult == libvpxInterModeThresholdDisabled {
			thresholds[i] = libvpxInterModeThresholdDisabled
			continue
		}
		if rdDiv == 1 {
			thresholds[i] = mult * q / 100
		} else {
			thresholds[i] = mult * q
		}
	}
	return thresholds
}

type libvpxInterModeThresholdContext struct {
	temporalLayers  int
	lastEnabled     bool
	goldenEnabled   bool
	closestRef      vp8common.MVReferenceFrame
	refFrameCount   int
	totalMBs        int
	staticThreshold int
	errorBins       *[1024]uint32
}

func libvpxInterModeThresholdMultipliers(deadline Deadline, speed int) [libvpxInterModeCount]int {
	return libvpxInterModeThresholdMultipliersForContext(deadline, speed, libvpxInterModeThresholdContext{})
}

func libvpxInterModeThresholdMultipliersForContext(deadline Deadline, speed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	speed = libvpxSpeedFeatureCPUUsed(deadline, speed)
	return libvpxInterModeThresholdMultipliersForCPISpeed(deadline, speed, context)
}

// libvpxInterModeThresholdMultipliersForCPISpeed mirrors libvpx's
// vp8_set_speed_features RD threshold lookup with the actual cpi->Speed
// value already in hand. The caller is responsible for passing the post-
// vp8_change_config / post-vp8_auto_select_speed cpi->Speed; this helper
// computes continuousSpeed = cpiSpeed + 7 (realtime) or the corresponding
// good-quality / best-quality mapping, then runs the per-mode speed_map
// lookups. Use this instead of the legacy negate-pass-through path when the
// caller already knows cpi->Speed (in particular when it can be 0 after a
// runtime config-set reset — the legacy path collapses 0 to the 4 cold-
// start default and skews thresholds by 4 Speed steps).
func libvpxInterModeThresholdMultipliersForCPISpeed(deadline Deadline, cpiSpeed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	continuousSpeed := libvpxInterFrameContinuousSpeedForFeatureSpeed(deadline, cpiSpeed)
	znn := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapZNN[:])
	vhPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapVHPred[:])
	bPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapBPred[:])
	tmPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapTM[:])
	new1 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapNew1[:])
	new2 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapNew2[:])
	split1 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapSplit1[:])
	split2 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapSplit2[:])

	var mult [libvpxInterModeCount]int
	mult[libvpxThrZero1] = 0
	mult[libvpxThrNearest1] = 0
	mult[libvpxThrNear1] = 0
	mult[libvpxThrDC] = 0
	mult[libvpxThrZero2] = znn
	mult[libvpxThrZero3] = znn
	mult[libvpxThrNearest2] = znn
	mult[libvpxThrNearest3] = znn
	mult[libvpxThrNear2] = znn
	mult[libvpxThrNear3] = znn
	mult[libvpxThrVPred] = vhPred
	mult[libvpxThrHPred] = vhPred
	mult[libvpxThrBPred] = bPred
	mult[libvpxThrTMPred] = tmPred
	mult[libvpxThrNew1] = new1
	mult[libvpxThrNew2] = new2
	mult[libvpxThrNew3] = new2
	mult[libvpxThrSplit1] = split1
	mult[libvpxThrSplit2] = split2
	mult[libvpxThrSplit3] = split2
	if context.temporalLayers > 1 && cpiSpeed <= 6 && context.lastEnabled && context.goldenEnabled {
		shift := 1
		if context.closestRef == vp8common.GoldenFrame {
			shift = 3
		}
		mult[libvpxThrZero2] >>= shift
		mult[libvpxThrNearest2] >>= shift
		mult[libvpxThrNear2] >>= shift
	}
	if deadline == DeadlineRealtime && cpiSpeed > 6 && context.errorBins != nil && context.totalMBs > 0 {
		thresh := libvpxRealtimeAdaptiveInterModeThreshold(context.errorBins, context.totalMBs, cpiSpeed, context.staticThreshold)
		if context.refFrameCount > 1 {
			mult[libvpxThrNew1] = thresh
			mult[libvpxThrNearest1] = thresh >> 1
			mult[libvpxThrNear1] = thresh >> 1
		}
		if context.refFrameCount > 2 {
			mult[libvpxThrNew2] = thresh << 1
			mult[libvpxThrNearest2] = thresh
			mult[libvpxThrNear2] = thresh
		}
		if context.refFrameCount > 3 {
			mult[libvpxThrNew3] = thresh << 1
			mult[libvpxThrNearest3] = thresh
			mult[libvpxThrNear3] = thresh
		}
	}
	return mult
}

func libvpxRealtimeAdaptiveInterModeThreshold(errorBins *[1024]uint32, totalMBs int, speed int, staticThreshold int) int {
	if errorBins == nil || totalMBs <= 0 || speed <= 6 {
		return 2000
	}
	minimum := max(staticThreshold, 2000)
	minimum >>= 7
	if minimum < 0 {
		minimum = 0
	}
	if minimum > len(errorBins) {
		minimum = len(errorBins)
	}
	totalSkip := 0
	for i := 0; i < minimum; i++ {
		totalSkip += int(errorBins[i])
	}
	remaining := max(totalMBs-totalSkip, 0)
	sum := 0
	i := minimum
	for ; i < len(errorBins); i++ {
		sum += int(errorBins[i])
		if int64(10*sum) >= int64(speed-6)*int64(remaining) {
			break
		}
	}
	i--
	thresh := max(i<<7, 2000)
	return thresh
}

func libvpxInterModeCheckFrequencies(deadline Deadline, speed int) [libvpxInterModeCount]int {
	speed = libvpxSpeedFeatureCPUUsed(deadline, speed)
	return libvpxInterModeCheckFrequenciesForCPISpeed(deadline, speed)
}

// libvpxInterModeCheckFrequenciesForCPISpeed mirrors
// libvpxInterModeCheckFrequencies with the actual cpi->Speed in hand. See
// libvpxInterModeThresholdMultipliersForCPISpeed for why the negate-pass-
// through legacy path breaks at autoSpeed=0.
func libvpxInterModeCheckFrequenciesForCPISpeed(deadline Deadline, speed int) [libvpxInterModeCount]int {
	continuousSpeed := libvpxInterFrameContinuousSpeedForFeatureSpeed(deadline, speed)
	new1Speed := continuousSpeed
	if deadline == DeadlineRealtime && speed == 10 {
		new1Speed = 16
	}
	zn2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapZN2[:])
	near2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapNear2[:])
	vhBPred := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapVHBPred[:])
	new1 := libvpxSpeedMap(new1Speed, libvpxModeCheckFreqMapNew1[:])
	new2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapNew2[:])
	split1 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapSplit1[:])
	split2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapSplit2[:])

	var freq [libvpxInterModeCount]int
	freq[libvpxThrZero1] = 0
	freq[libvpxThrNearest1] = 0
	freq[libvpxThrNear1] = 0
	freq[libvpxThrDC] = 0
	freq[libvpxThrTMPred] = 0
	freq[libvpxThrZero2] = zn2
	freq[libvpxThrZero3] = zn2
	freq[libvpxThrNearest2] = zn2
	freq[libvpxThrNearest3] = zn2
	freq[libvpxThrNear2] = near2
	freq[libvpxThrNear3] = near2
	freq[libvpxThrVPred] = vhBPred
	freq[libvpxThrHPred] = vhBPred
	freq[libvpxThrBPred] = vhBPred
	freq[libvpxThrNew1] = new1
	freq[libvpxThrNew2] = new2
	freq[libvpxThrNew3] = new2
	freq[libvpxThrSplit1] = split1
	freq[libvpxThrSplit2] = split2
	freq[libvpxThrSplit3] = split2
	return freq
}

func libvpxInterFrameContinuousSpeedForFeatureSpeed(deadline Deadline, speed int) int {
	switch deadline {
	case DeadlineBestQuality:
		return 0
	case DeadlineRealtime:
		return speed + 7
	default:
		if speed > 5 {
			speed = 5
		}
		return speed + 1
	}
}

func libvpxSpeedMap(speed int, entries []int) int {
	for i := 0; i+1 < len(entries); i += 2 {
		result := entries[i]
		limit := entries[i+1]
		if speed < limit {
			return result
		}
	}
	return 0
}

var libvpxThreshMultMapZNN = [...]int{
	0, 3, 1500, 4, 2000, 7, 1000, 9, 2000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapVHPred = [...]int{
	1000, 3, 1500, 4, 2000, 7, 1000, 8, 2000, 14, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapBPred = [...]int{
	2000, 1, 2500, 3, 5000, 4, 7500, 7, 2500, 8, 5000, 13, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapTM = [...]int{
	1000, 3, 1500, 4, 2000, 7, 0, 8, 1000, 9, 2000, 14, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapNew1 = [...]int{
	1000, 3, 2000, 7, 2000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapNew2 = [...]int{
	1000, 3, 2000, 4, 2500, 6, 4000, 7, 2000, 9, 2500, 12, 4000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapSplit1 = [...]int{
	2500, 1, 1700, 3, 10000, 4, 25000, 5, libvpxInterModeThresholdDisabled, 7, 5000, 8, 10000, 9, 25000, 10, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapSplit2 = [...]int{
	5000, 1, 4500, 3, 20000, 4, 50000, 5, libvpxInterModeThresholdDisabled, 7, 10000, 8, 20000, 9, 50000, 10, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapZN2 = [...]int{
	0, 17, 2, 18, 4, 19, 8, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapVHBPred = [...]int{
	0, 6, 2, 7, 0, 10, 2, 12, 4, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNear2 = [...]int{
	0, 6, 2, 7, 0, 10, 2, 17, 4, 18, 8, 19, 16, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNew1 = [...]int{
	0, 17, 2, 18, 4, 19, 8, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNew2 = [...]int{
	0, 6, 4, 7, 0, 10, 4, 17, 8, 18, 16, 19, 32, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapSplit1 = [...]int{
	0, 3, 2, 4, 7, 8, 2, 9, 7, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapSplit2 = [...]int{
	0, 2, 2, 3, 4, 4, 15, 8, 4, 9, 15, libvpxSpeedMapMax,
}

func interFrameSubpixelSearchCandidateCount() int {
	return interFrameSubpixelSearchMaxCandidates
}
