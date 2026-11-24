package common

// Ported from libvpx v1.16.0 VP8 common mechanics:
//   - vp8/common/findnearmv.h left_block_mode and above_block_mode.
//   - vp8/common/blockd.c vp8_block2left and vp8_block2above.
//   - vp8/common/blockd.h prediction-mode enum values.

// BlockModeFromMacroblockMode maps VP8 macroblock prediction modes to the
// block-prediction mode used by neighboring B_PRED context lookups.
func BlockModeFromMacroblockMode(mode MBPredictionMode) BPredictionMode {
	switch mode {
	case VPred:
		return BVEPred
	case HPred:
		return BHEPred
	case TMPred:
		return BTMPred
	default:
		return BDCPred
	}
}

// IsWholeInterMacroblockMode reports whether mode predicts the whole
// macroblock with one inter vector rather than SPLITMV sub-block vectors.
func IsWholeInterMacroblockMode(mode MBPredictionMode) bool {
	switch mode {
	case ZeroMV, NearestMV, NearMV, NewMV:
		return true
	default:
		return false
	}
}

// UVTokenContextIndex returns the above/left context slots for a VP8 chroma
// token block. Blocks 16..19 are U and 20..23 are V.
func UVTokenContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if (block & 3) > 1 {
		l++
	}
	return a, l
}
