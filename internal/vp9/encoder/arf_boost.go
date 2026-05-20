package encoder

import "math"

// VP9 ARF/temporal-filter constants from libvpx v1.16.0.
//
// libvpx: vp9/encoder/vp9_firstpass.c:53-69
const (
	NormalBoost         = 100
	MinARFGFBoost       = 250
	MinDecayFactor      = 0.01
	DefaultDecayLimit   = 0.75
	BaselineErrPerMB    = 12500.0
	GFMaxFrameBoost     = 96.0
	DefaultZMFactor     = 0.5
	IntraPart           = 0.005
	LowSRDiffThresh     = 0.1
	LowCodedErrPerMB    = 10.0
	NCountFrameIIThresh = 6.0
	MinActiveAreaFP     = 0.5
	MaxActiveAreaFP     = 1.0
	DoubleDivideEpsilon = 1e-15
)

// doubleDivideCheck mirrors libvpx's DOUBLE_DIVIDE_CHECK(x) which returns x
// if |x| >= epsilon, else returns the small epsilon to avoid divide-by-zero.
//
// libvpx: vpx_dsp/vpx_dsp_common.h DOUBLE_DIVIDE_CHECK
func doubleDivideCheck(x float64) float64 {
	if x < 0 {
		if x > -DoubleDivideEpsilon {
			return -DoubleDivideEpsilon
		}
		return x
	}
	if x < DoubleDivideEpsilon {
		return DoubleDivideEpsilon
	}
	return x
}

// ConvertQIndexToQ ports libvpx's vp9_convert_qindex_to_q for 8-bit Profile
// 0: ac_quant_lookup[qindex] / 4.0.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:170 vp9_convert_qindex_to_q
func ConvertQIndexToQ(qindex int) float64 {
	if qindex < 0 {
		qindex = 0
	} else if qindex > 255 {
		qindex = 255
	}
	return QIndexToQ(int16(qindex))
}

// TemporalFilterAdjustment carries the libvpx adaptive temporal-filter
// strength/window decision produced by [AdjustARNRFilter].
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
type TemporalFilterAdjustment struct {
	// ARNRFrames is the total number of frames participating in the
	// temporal average (= FramesBackward + 1 + FramesForward).
	ARNRFrames int
	// FramesBackward is the backward window size (frames preceding the
	// alt-ref source frame).
	FramesBackward int
	// FramesForward is the forward window size.
	FramesForward int
	// ARNRStrength is the adaptive temporal-filter strength in [0,6].
	ARNRStrength int
}

// AdjustARNRFilterInput aggregates the libvpx inputs `adjust_arnr_filter`
// consumes. Each field is named after its libvpx counterpart so the port can
// be reviewed against the reference implementation line by line.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
type AdjustARNRFilterInput struct {
	// LookaheadDepth is `vp9_lookahead_depth(cpi->lookahead)`.
	LookaheadDepth int
	// Distance is the alt-ref distance argument forwarded from
	// `vp9_temporal_filter(cpi, distance)`.
	Distance int
	// GroupBoost is `rc->gfu_boost` (the cumulative GF/ARF group boost
	// produced by `compute_arf_boost`).
	GroupBoost int
	// ARNRMaxFrames is `oxcf->arnr_max_frames`.
	ARNRMaxFrames int
	// ARNRStrengthBase is `oxcf->arnr_strength`.
	ARNRStrengthBase int
	// ARNRStrengthAdjustment is
	// `cpi->twopass.arnr_strength_adjustment` (libvpx only consults this
	// on pass==2; pass-1 callers must pass 0).
	ARNRStrengthAdjustment int
	// Pass is the libvpx encoder pass (1 or 2).
	Pass int
	// CurrentVideoFrame is `cm->current_video_frame`.
	CurrentVideoFrame int
	// AvgFrameQIndexInter is `rc->avg_frame_qindex[INTER_FRAME]`.
	AvgFrameQIndexInter int
	// AvgFrameQIndexKey is `rc->avg_frame_qindex[KEY_FRAME]`.
	AvgFrameQIndexKey int
}

// AdjustARNRFilter is a pure-function port of libvpx's
// `adjust_arnr_filter`. It mirrors the libvpx control flow verbatim so the
// adaptive temporal-filter strength tracks the reference encoder.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
func AdjustARNRFilter(in AdjustARNRFilterInput) TemporalFilterAdjustment {
	// const VP9EncoderConfig *const oxcf = &cpi->oxcf;
	//
	// int max_fwd =
	//     VPXMAX((int)vp9_lookahead_depth(cpi->lookahead) - distance - 1, 0);
	maxFwd := max(in.LookaheadDepth-in.Distance-1, 0)
	// int max_bwd = VPXMAX(distance, 0);
	maxBwd := max(in.Distance, 0)
	// int frames = VPXMAX(oxcf->arnr_max_frames, 1);
	frames := max(in.ARNRMaxFrames, 1)

	// int q, base_strength, strength;
	//
	// Context dependent two pass adjustment to strength.
	// if (oxcf->pass == 2) {
	//   base_strength = oxcf->arnr_strength + cpi->twopass.arnr_strength_adjustment;
	//   base_strength = clamp(base_strength, 0, 6);
	// } else {
	//   base_strength = oxcf->arnr_strength;
	// }
	var baseStrength int
	if in.Pass == 2 {
		baseStrength = min(max(in.ARNRStrengthBase+in.ARNRStrengthAdjustment, 0), 6)
	} else {
		baseStrength = in.ARNRStrengthBase
	}

	// Adjust the strength based on active max q.
	// if (cpi->common.current_video_frame > 1)
	//   q = ((int)vp9_convert_qindex_to_q(rc->avg_frame_qindex[INTER_FRAME], ...));
	// else
	//   q = ((int)vp9_convert_qindex_to_q(rc->avg_frame_qindex[KEY_FRAME], ...));
	var q int
	if in.CurrentVideoFrame > 1 {
		q = int(ConvertQIndexToQ(in.AvgFrameQIndexInter))
	} else {
		q = int(ConvertQIndexToQ(in.AvgFrameQIndexKey))
	}
	// if (q > 16) {
	//   strength = base_strength;
	// } else {
	//   strength = base_strength - ((16 - q) / 2);
	//   if (strength < 0) strength = 0;
	// }
	var strength int
	if q > 16 {
		strength = baseStrength
	} else {
		strength = max(baseStrength-((16-q)/2), 0)
	}

	// Adjust number of frames in filter and strength based on gf boost level.
	// frames = VPXMIN(frames, group_boost / 150);
	if cap := in.GroupBoost / 150; cap < frames {
		frames = cap
	}
	// if (strength > group_boost / 300) {
	//   strength = group_boost / 300;
	// }
	if cap := in.GroupBoost / 300; strength > cap {
		strength = cap
	}

	// Even/odd window placement.
	// if (VPXMIN(max_fwd, max_bwd) >= frames / 2) {
	//   *frames_backward = frames / 2;
	//   *frames_forward = (frames - 1) / 2;
	// } else {
	//   if (max_fwd < frames / 2) {
	//     *frames_forward = max_fwd;
	//     *frames_backward = VPXMIN(frames - 1 - *frames_forward, max_bwd);
	//   } else {
	//     *frames_backward = max_bwd;
	//     *frames_forward = VPXMIN(frames - 1 - *frames_backward, max_fwd);
	//   }
	// }
	var framesBackward, framesForward int
	minSide := min(maxBwd, maxFwd)
	if minSide >= frames/2 {
		framesBackward = frames / 2
		framesForward = (frames - 1) / 2
	} else if maxFwd < frames/2 {
		framesForward = maxFwd
		fb := min(maxBwd, frames-1-framesForward)
		framesBackward = fb
	} else {
		framesBackward = maxBwd
		ff := min(maxFwd, frames-1-framesBackward)
		framesForward = ff
	}

	// Set the baseline active filter size.
	// frames = *frames_backward + 1 + *frames_forward;
	frames = framesBackward + 1 + framesForward

	// if (frames <= 1) {
	//   frames = 1;
	//   *frames_backward = 0;
	//   *frames_forward = 0;
	// }
	if frames <= 1 {
		frames = 1
		framesBackward = 0
		framesForward = 0
	}

	return TemporalFilterAdjustment{
		ARNRFrames:     frames,
		FramesBackward: framesBackward,
		FramesForward:  framesForward,
		ARNRStrength:   strength,
	}
}

// CalculateActiveArea ports libvpx's `calculate_active_area`. The result is
// clamped to [MIN_ACTIVE_AREA, MAX_ACTIVE_AREA].
//
// libvpx: vp9/encoder/vp9_firstpass.c:239 calculate_active_area
func CalculateActiveArea(mbRows int, frame FirstPassFrameStats) float64 {
	if mbRows <= 0 {
		return MinActiveAreaFP
	}
	active := 1.0 - (frame.IntraSkipPct/2.0 +
		(frame.InactiveZoneRows*2.0)/float64(mbRows))
	if active < MinActiveAreaFP {
		return MinActiveAreaFP
	}
	if active > MaxActiveAreaFP {
		return MaxActiveAreaFP
	}
	return active
}

// GetSRDecayRate ports libvpx's get_sr_decay_rate.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1741 get_sr_decay_rate
func GetSRDecayRate(frame FirstPassFrameStats, srDiffFactor, srDefaultDecayLimit float64) float64 {
	srDiff := frame.SRCodedError - frame.CodedError
	srDecay := 1.0
	if srDiff > LowSRDiffThresh {
		srDiffPart := srDiffFactor * ((srDiff * 0.25) / doubleDivideCheck(frame.IntraError))
		modifiedPctInter := frame.PcntInter
		if frame.CodedError > LowCodedErrPerMB &&
			(frame.IntraError/doubleDivideCheck(frame.CodedError)) < NCountFrameIIThresh {
			modifiedPctInter = frame.PcntInter + frame.PcntIntraLow - frame.PcntNeutral
		}
		modifiedPcntIntra := 100.0 * (1.0 - modifiedPctInter)
		srDecay = 1.0 - srDiffPart - (IntraPart * modifiedPcntIntra)
	}
	if srDecay < srDefaultDecayLimit {
		return srDefaultDecayLimit
	}
	return srDecay
}

// GetPredictionDecayRate ports libvpx's get_prediction_decay_rate.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1777 get_prediction_decay_rate
func GetPredictionDecayRate(frame FirstPassFrameStats, srDiffFactor, srDefaultDecayLimit, zmFactor float64) float64 {
	srDecayRate := GetSRDecayRate(frame, srDiffFactor, srDefaultDecayLimit)
	zeroMotionFactor := zmFactor * (frame.PcntInter - frame.PcntMotion)
	// libvpx asserts 0 <= zeroMotionFactor <= 1.0; clamp defensively here
	// because govpx first-pass stats can drift if accumulators were not
	// finalized exactly the libvpx way.
	if zeroMotionFactor < 0 {
		zeroMotionFactor = 0
	}
	if zeroMotionFactor > 1.0 {
		zeroMotionFactor = 1.0
	}
	other := srDecayRate + (1.0-srDecayRate)*zeroMotionFactor
	if zeroMotionFactor > other {
		return zeroMotionFactor
	}
	return other
}

// DetectFlashFromFrameStats ports libvpx's detect_flash_from_frame_stats.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1818 detect_flash_from_frame_stats
func DetectFlashFromFrameStats(frame *FirstPassFrameStats) bool {
	if frame == nil {
		return false
	}
	if frame.SRCodedError < frame.CodedError {
		return true
	}
	if frame.PcntSecondRef > frame.PcntInter && frame.PcntSecondRef >= 0.5 {
		return true
	}
	return false
}

// AccumulateFrameMotionStats ports libvpx's accumulate_frame_motion_stats.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1839 accumulate_frame_motion_stats
func AccumulateFrameMotionStats(stats FirstPassFrameStats,
	mvInOut, mvInOutAccumulator, absMvInOutAccumulator, mvRatioAccumulator *float64,
) {
	pct := stats.PcntMotion

	// Accumulate Motion In/Out of frame stats.
	*mvInOut = stats.MVInOutCount * pct
	*mvInOutAccumulator += *mvInOut
	*absMvInOutAccumulator += math.Abs(*mvInOut)

	// Accumulate a measure of how uniform (or conversely how random) the
	// motion field is (a ratio of abs(mv) / mv).
	if pct > 0.05 {
		mvrRatio := math.Abs(stats.MVrAbs) / doubleDivideCheck(math.Abs(stats.MVr))
		mvcRatio := math.Abs(stats.MVcAbs) / doubleDivideCheck(math.Abs(stats.MVc))
		// libvpx: mv_ratio_accumulator +=
		//   pct * (mvr_ratio < stats->mvr_abs ? mvr_ratio : stats->mvr_abs);
		var rPick, cPick float64
		if mvrRatio < stats.MVrAbs {
			rPick = mvrRatio
		} else {
			rPick = stats.MVrAbs
		}
		if mvcRatio < stats.MVcAbs {
			cPick = mvcRatio
		} else {
			cPick = stats.MVcAbs
		}
		*mvRatioAccumulator += pct * rPick
		*mvRatioAccumulator += pct * cPick
	}
}

// CalcFrameBoost ports libvpx's calc_frame_boost.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1866 calc_frame_boost
func CalcFrameBoost(frame FirstPassFrameStats,
	errPerMB, gfFrameMaxBoost float64, mbRows int,
	avgFrameQIndex int, thisFrameMvInOut float64,
) float64 {
	lq := ConvertQIndexToQ(avgFrameQIndex)
	boostQCorrection := 0.5 + lq*0.015
	if boostQCorrection > 1.5 {
		boostQCorrection = 1.5
	}
	activeArea := CalculateActiveArea(mbRows, frame)

	// Frame boost is based on inter error.
	frameBoost := (errPerMB * activeArea) / doubleDivideCheck(frame.CodedError)

	// Small adjustment for cases where there is a zoom out.
	if thisFrameMvInOut > 0.0 {
		frameBoost += frameBoost * (thisFrameMvInOut * 2.0)
	}

	// Q correction and scaling.
	frameBoost *= boostQCorrection

	cap := gfFrameMaxBoost * boostQCorrection
	if frameBoost < cap {
		return frameBoost
	}
	return cap
}

// ARFBoostParams aggregates the libvpx TWO_PASS / FRAME_INFO fields
// `compute_arf_boost` consults. Tests pass in libvpx defaults
// (SRDiffFactor=1.0, SRDefaultDecayLimit=DEFAULT_DECAY_LIMIT=0.75,
// ZMFactor=DEFAULT_ZM_FACTOR=0.5, ErrPerMB=BASELINE_ERR_PER_MB=12500.0,
// GFFrameMaxBoost=GF_MAX_FRAME_BOOST=96.0).
//
// libvpx: vp9/encoder/vp9_firstpass.c:1936 compute_arf_boost
type ARFBoostParams struct {
	// MBRows is `frame_info->mb_rows`.
	MBRows int
	// ErrPerMB is `twopass->err_per_mb`. libvpx initializes this to
	// BASELINE_ERR_PER_MB and then scales by the active-area normalization.
	ErrPerMB float64
	// GFFrameMaxBoost is `twopass->gf_frame_max_boost`. libvpx initializes
	// it to GF_MAX_FRAME_BOOST.
	GFFrameMaxBoost float64
	// SRDiffFactor is `twopass->sr_diff_factor`.
	SRDiffFactor float64
	// SRDefaultDecayLimit is `twopass->sr_default_decay_limit`.
	SRDefaultDecayLimit float64
	// ZMFactor is `twopass->zm_factor`.
	ZMFactor float64
}

// DefaultARFBoostParams returns libvpx's default TWO_PASS parameter set,
// matching the values applied in `setup_two_pass_state` at construction time.
//
// libvpx: vp9/encoder/vp9_firstpass.c:3568-3577
func DefaultARFBoostParams(mbRows int) ARFBoostParams {
	return ARFBoostParams{
		MBRows:              mbRows,
		ErrPerMB:            BaselineErrPerMB,
		GFFrameMaxBoost:     GFMaxFrameBoost,
		SRDiffFactor:        1.0,
		SRDefaultDecayLimit: DefaultDecayLimit,
		ZMFactor:            DefaultZMFactor,
	}
}

// ComputeARFBoost is a pure-function port of libvpx's `compute_arf_boost`.
// It returns the cumulative ARF boost used by `define_gf_group` to compute
// `rc->gfu_boost`.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1936 compute_arf_boost
//
// Inputs:
//   - stats: first-pass per-frame stats (display order, no terminal total
//     row). Pass `arfShowIdx` selects the alt-ref location.
//   - fFrames: number of frames to scan forward from arfShowIdx.
//   - bFrames: number of frames to scan backward from arfShowIdx.
//   - avgFrameQIndex: `rc->avg_frame_qindex[INTER_FRAME]` at the time the
//     group is defined.
//   - params: libvpx TWO_PASS / FRAME_INFO defaults.
func ComputeARFBoost(stats []FirstPassFrameStats, arfShowIdx, fFrames, bFrames, avgFrameQIndex int, params ARFBoostParams) int {
	getFrame := func(i int) *FirstPassFrameStats {
		if i < 0 || i >= len(stats) {
			return nil
		}
		return &stats[i]
	}

	boostScore := 0.0
	mvRatioAccumulator := 0.0
	decayAccumulator := 1.0
	thisFrameMvInOut := 0.0
	mvInOutAccumulator := 0.0
	absMvInOutAccumulator := 0.0
	flashDetected := false
	// libvpx accumulates mv_ratio_accumulator and abs_mv_in_out_accumulator
	// here but the result is not consumed by compute_arf_boost itself
	// (calc_arf_boost only needs the per-frame mv_in_out_count weighting
	// inside calc_frame_boost). They are kept for fidelity with the
	// in-place mutation semantics.
	_ = mvRatioAccumulator
	_ = absMvInOutAccumulator

	// Search forward from the proposed arf/next gf position.
	for i := range fFrames {
		thisFrame := getFrame(arfShowIdx + i)
		nextFrame := getFrame(arfShowIdx + i + 1)
		if thisFrame == nil {
			break
		}

		AccumulateFrameMotionStats(*thisFrame, &thisFrameMvInOut,
			&mvInOutAccumulator, &absMvInOutAccumulator, &mvRatioAccumulator)

		flashDetected = DetectFlashFromFrameStats(thisFrame) ||
			DetectFlashFromFrameStats(nextFrame)

		if !flashDetected {
			decayAccumulator *= GetPredictionDecayRate(*thisFrame,
				params.SRDiffFactor, params.SRDefaultDecayLimit, params.ZMFactor)
			if decayAccumulator < MinDecayFactor {
				decayAccumulator = MinDecayFactor
			}
		}
		boostScore += decayAccumulator * CalcFrameBoost(*thisFrame,
			params.ErrPerMB, params.GFFrameMaxBoost, params.MBRows,
			avgFrameQIndex, thisFrameMvInOut)
	}

	arfBoost := int(boostScore)

	// Reset for backward looking loop.
	boostScore = 0.0
	mvRatioAccumulator = 0.0
	decayAccumulator = 1.0
	thisFrameMvInOut = 0.0
	mvInOutAccumulator = 0.0
	absMvInOutAccumulator = 0.0

	// Search backward towards last gf position.
	for i := -1; i >= -bFrames; i-- {
		thisFrame := getFrame(arfShowIdx + i)
		nextFrame := getFrame(arfShowIdx + i + 1)
		if thisFrame == nil {
			break
		}
		AccumulateFrameMotionStats(*thisFrame, &thisFrameMvInOut,
			&mvInOutAccumulator, &absMvInOutAccumulator, &mvRatioAccumulator)

		flashDetected = DetectFlashFromFrameStats(thisFrame) ||
			DetectFlashFromFrameStats(nextFrame)

		if !flashDetected {
			decayAccumulator *= GetPredictionDecayRate(*thisFrame,
				params.SRDiffFactor, params.SRDefaultDecayLimit, params.ZMFactor)
			if decayAccumulator < MinDecayFactor {
				decayAccumulator = MinDecayFactor
			}
		}
		boostScore += decayAccumulator * CalcFrameBoost(*thisFrame,
			params.ErrPerMB, params.GFFrameMaxBoost, params.MBRows,
			avgFrameQIndex, thisFrameMvInOut)
	}
	arfBoost += int(boostScore)

	// Floor at 40 per scanned frame, then clamp to MIN_ARF_GF_BOOST.
	if floor := (bFrames + fFrames) * 40; arfBoost < floor {
		arfBoost = floor
	}
	if arfBoost < MinARFGFBoost {
		arfBoost = MinARFGFBoost
	}

	return arfBoost
}
