package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// VP9 intra-prediction predictor-need flags. Ported from libvpx
// v1.16.0 vp9/common/vp9_reconintra.c — the extend_modes[INTRA_MODES]
// table that drives whether the per-mode predictor reads the above
// row, the left column, or the above-right extension.
//
// The companion BuildIntraPredictors driver in vp9_predict_intra_block
// uses these flags to decide which border pixels to materialize before
// dispatching the DSP kernel.

const (
	// NeedLeft, NeedAbove, NeedAboveRight mirror libvpx's anonymous
	// enum at the top of vp9_reconintra.c.
	NeedLeft       = 1 << 1
	NeedAbove      = 1 << 2
	NeedAboveRight = 1 << 3
)

// ExtendModes mirrors libvpx's extend_modes[INTRA_MODES] table.
// Indexed by PredictionMode in the intra range (DcPred..TmPred).
var ExtendModes = [common.IntraModes]uint8{
	NeedAbove | NeedLeft, // DcPred
	NeedAbove,            // VPred
	NeedLeft,             // HPred
	NeedAboveRight,       // D45Pred
	NeedLeft | NeedAbove, // D135Pred
	NeedLeft | NeedAbove, // D117Pred
	NeedLeft | NeedAbove, // D153Pred
	NeedLeft,             // D207Pred
	NeedAboveRight,       // D63Pred
	NeedLeft | NeedAbove, // TmPred
}

// IntraNeedsLeft reports whether the predictor mode reads the left
// column of border pixels.
func IntraNeedsLeft(mode common.PredictionMode) bool {
	return ExtendModes[mode]&NeedLeft != 0
}

// IntraNeedsAbove reports whether the predictor reads the above row.
// True for DC/V/D135/D117/D153/TM; false for H/D207. The D45/D63
// modes go through the NeedAboveRight path which also implies above.
func IntraNeedsAbove(mode common.PredictionMode) bool {
	return ExtendModes[mode]&NeedAbove != 0
}

// IntraNeedsAboveRight reports whether the predictor reads the
// above-right row extension. True only for D45 and D63.
func IntraNeedsAboveRight(mode common.PredictionMode) bool {
	return ExtendModes[mode]&NeedAboveRight != 0
}
