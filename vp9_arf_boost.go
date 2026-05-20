package govpx

import "github.com/thesyncim/govpx/internal/vp9/encoder"

// VP9 ARF/temporal-filter constants from libvpx v1.16.0.
//
// libvpx: vp9/encoder/vp9_firstpass.c:53-69
const (
	vp9NormalBoost         = encoder.NormalBoost
	vp9MinARFGFBoost       = encoder.MinARFGFBoost
	vp9MinDecayFactor      = encoder.MinDecayFactor
	vp9DefaultDecayLimit   = encoder.DefaultDecayLimit
	vp9BaselineErrPerMB    = encoder.BaselineErrPerMB
	vp9GFMaxFrameBoost     = encoder.GFMaxFrameBoost
	vp9DefaultZMFactor     = encoder.DefaultZMFactor
	vp9IntraPart           = encoder.IntraPart
	vp9LowSRDiffThresh     = encoder.LowSRDiffThresh
	vp9LowCodedErrPerMB    = encoder.LowCodedErrPerMB
	vp9NCountFrameIIThresh = encoder.NCountFrameIIThresh
	vp9MinActiveAreaFP     = encoder.MinActiveAreaFP
	vp9MaxActiveAreaFP     = encoder.MaxActiveAreaFP
	vp9DoubleDivideEpsilon = encoder.DoubleDivideEpsilon
)

// VP9TemporalFilterAdjustment carries the libvpx adaptive temporal-filter
// strength/window decision produced by [VP9AdjustARNRFilter].
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
type VP9TemporalFilterAdjustment = encoder.TemporalFilterAdjustment

// VP9AdjustARNRFilterInput aggregates the libvpx inputs `adjust_arnr_filter`
// consumes. Each field is named after its libvpx counterpart so the port can
// be reviewed against the reference implementation line by line.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
type VP9AdjustARNRFilterInput = encoder.AdjustARNRFilterInput

// VP9AdjustARNRFilter is a pure-function port of libvpx's
// `adjust_arnr_filter`. It mirrors the libvpx control flow verbatim so the
// adaptive temporal-filter strength tracks the reference encoder.
//
// libvpx: vp9/encoder/vp9_temporal_filter.c:1255 adjust_arnr_filter
func VP9AdjustARNRFilter(in VP9AdjustARNRFilterInput) VP9TemporalFilterAdjustment {
	return encoder.AdjustARNRFilter(in)
}

func vp9DoubleDivideCheck(x float64) float64 {
	if x < 0 {
		if x > -vp9DoubleDivideEpsilon {
			return -vp9DoubleDivideEpsilon
		}
		return x
	}
	if x < vp9DoubleDivideEpsilon {
		return vp9DoubleDivideEpsilon
	}
	return x
}

func vp9ConvertQIndexToQ(qindex int) float64 {
	return encoder.ConvertQIndexToQ(qindex)
}

func vp9CalculateActiveArea(mbRows int, frame VP9FirstPassFrameStats) float64 {
	return encoder.CalculateActiveArea(mbRows, frame)
}

func vp9GetSRDecayRate(frame VP9FirstPassFrameStats, srDiffFactor, srDefaultDecayLimit float64) float64 {
	return encoder.GetSRDecayRate(frame, srDiffFactor, srDefaultDecayLimit)
}

func vp9GetPredictionDecayRate(frame VP9FirstPassFrameStats, srDiffFactor, srDefaultDecayLimit, zmFactor float64) float64 {
	return encoder.GetPredictionDecayRate(frame, srDiffFactor, srDefaultDecayLimit, zmFactor)
}

func vp9DetectFlashFromFrameStats(frame *VP9FirstPassFrameStats) bool {
	return encoder.DetectFlashFromFrameStats(frame)
}

func vp9AccumulateFrameMotionStats(stats VP9FirstPassFrameStats,
	mvInOut, mvInOutAccumulator, absMvInOutAccumulator, mvRatioAccumulator *float64,
) {
	encoder.AccumulateFrameMotionStats(stats,
		mvInOut, mvInOutAccumulator, absMvInOutAccumulator, mvRatioAccumulator)
}

func vp9CalcFrameBoost(frame VP9FirstPassFrameStats,
	errPerMB, gfFrameMaxBoost float64, mbRows int,
	avgFrameQIndex int, thisFrameMvInOut float64,
) float64 {
	return encoder.CalcFrameBoost(frame, errPerMB, gfFrameMaxBoost, mbRows,
		avgFrameQIndex, thisFrameMvInOut)
}

// VP9ARFBoostParams aggregates the libvpx TWO_PASS / FRAME_INFO fields
// `compute_arf_boost` consults.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1936 compute_arf_boost
type VP9ARFBoostParams = encoder.ARFBoostParams

// VP9DefaultARFBoostParams returns libvpx's default TWO_PASS parameter set,
// matching the values applied in `setup_two_pass_state` at construction time.
//
// libvpx: vp9/encoder/vp9_firstpass.c:3568-3577
func VP9DefaultARFBoostParams(mbRows int) VP9ARFBoostParams {
	return encoder.DefaultARFBoostParams(mbRows)
}

// VP9ComputeARFBoost is a pure-function port of libvpx's `compute_arf_boost`.
// It returns the cumulative ARF boost used by `define_gf_group` to compute
// `rc->gfu_boost`.
//
// libvpx: vp9/encoder/vp9_firstpass.c:1936 compute_arf_boost
func VP9ComputeARFBoost(stats []VP9FirstPassFrameStats, arfShowIdx, fFrames, bFrames, avgFrameQIndex int, params VP9ARFBoostParams) int {
	return encoder.ComputeARFBoost(stats, arfShowIdx, fFrames, bFrames,
		avgFrameQIndex, params)
}
