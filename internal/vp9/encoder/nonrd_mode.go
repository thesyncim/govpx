package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// NonrdAllowEncodeBreakout mirrors the realtime encode-breakout gates in
// libvpx's vp9_pick_inter_mode.
func NonrdAllowEncodeBreakout(lossless, sceneChangeDetected,
	highNumBlocksWithMotion bool,
) bool {
	return !lossless && !sceneChangeDetected && !highNumBlocksWithMotion
}

// NonrdModeRDThreshold applies the skip-transform and golden-reference
// threshold boosts used by the non-RD inter-mode picker.
func NonrdModeRDThreshold(base int, bestModeSkipTxfm, biasGolden bool,
	refFrame int8, framesSinceGolden uint16,
) int {
	modeRDThresh := base
	if bestModeSkipTxfm {
		modeRDThresh <<= 1
	}
	if biasGolden && refFrame == vp9dec.GoldenFrame && framesSinceGolden > 4 {
		modeRDThresh <<= 3
	}
	return modeRDThresh
}

// NonrdForceLastReference reports whether short-circuit-low-temp-var forces
// the picker to LAST_FRAME.
func NonrdForceLastReference(shortCircuitLowTempVar int,
	useNonrdPickMode, forceSkipLowTempVar bool,
) bool {
	return useNonrdPickMode && forceSkipLowTempVar &&
		(shortCircuitLowTempVar == 1 || shortCircuitLowTempVar == 3)
}

// NonrdNormalizeSSE computes libvpx's sse_zeromv_normalized for the
// (LAST, ZEROMV) candidate: the block SSE shifted right by
// b_width_log2_lookup[bsize] + b_height_log2_lookup[bsize] — i.e. SSE per
// 4x4 sub-block, NOT per pixel (vp9/encoder/vp9_pickmode.c:2351-2353). The
// CBR GOLDEN_FRAME skip gate (vp9_pickmode.c:2122-2125) compares this against
// thresh_skip_golden=500; using the per-pixel num_pels_log2 shift instead
// (4 bits larger) makes the value 16x too small and spuriously skips the
// golden reference on blocks where libvpx still searches it.
func NonrdNormalizeSSE(sse uint64, bsize common.BlockSize) uint64 {
	if bsize < common.Block4x4 || bsize >= common.BlockSizes {
		return sse
	}
	return sse >> uint(common.BWidthLog2Lookup[bsize]+common.BHeightLog2Lookup[bsize])
}

// NonrdScreenZeroLastBias matches the screen-content bias toward zero-motion
// LAST_FRAME candidates.
func NonrdScreenZeroLastBias(screen, sceneChangeDetected,
	highNumBlocksWithMotion bool, refFrame int8, mv vp9dec.MV,
	sourceVariance uint, sseY uint64,
) bool {
	return screen && (sceneChangeDetected || highNumBlocksWithMotion) &&
		refFrame == vp9dec.LastFrame && mv == (vp9dec.MV{}) &&
		sourceVariance == 0 && sseY > 0
}

// NonrdSkipScreenContentCandidate mirrors the screen-content candidate
// pruning in libvpx's realtime non-RD inter picker.
func NonrdSkipScreenContentCandidate(screen, sourceSADReady bool,
	refFrame int8, mv vp9dec.MV, mvValid bool,
	sourceVariance uint, zeroTempSADSource bool,
) bool {
	if !screen {
		return false
	}
	nonZeroMV := mvValid && mv != (vp9dec.MV{})
	zeroMV := mvValid && mv == (vp9dec.MV{})
	if sourceSADReady {
		return (nonZeroMV && zeroTempSADSource) ||
			(zeroMV && sourceVariance == 0 &&
				refFrame == vp9dec.LastFrame && !zeroTempSADSource)
	}
	return nonZeroMV && sourceVariance == 0
}

// NonrdIntraFallbackPrecheck mirrors the inexpensive gates before the
// non-RD intra-mode fallback sweep.
func NonrdIntraFallbackPrecheck(bestInterScore, interModeThresh uint64,
	forceSkipLowTempVar bool, bsize common.BlockSize,
	contentState ContentStateSB, xSkip, sceneChangeDetected, screenFlat,
	skipLowSourceSAD, lowvarHighsumdiff bool,
) bool {
	if screenFlat || sceneChangeDetected {
		return true
	}
	if xSkip {
		return false
	}
	if bestInterScore <= interModeThresh {
		return false
	}
	if forceSkipLowTempVar && bsize >= common.Block32x32 &&
		contentState != ContentStateVeryHighSad {
		return false
	}
	// libvpx vp9_pickmode.c:2533-2534 — skip_low_source_sad and
	// lowvar_highsumdiff block the normal intra-fallback path.
	if skipLowSourceSAD || lowvarHighsumdiff {
		return false
	}
	return true
}

// NonrdIntraModeList mirrors libvpx's intra_mode_list (vp9_pickmode.c:
// 1105-1106). The realtime non-RD intra fallback walks these modes in order.
var NonrdIntraModeList = [4]common.PredictionMode{
	common.DcPred,
	common.VPred,
	common.HPred,
	common.TmPred,
}

// IntraCostPenalty ports vp9_get_intra_cost_penalty (vp9_rd.c:778-795).
//
// The reduction factor halves the penalty for BLOCK_16X16 and quarters it for
// BLOCK_8X8 or smaller unless the live noise estimate is kHigh.
func IntraCostPenalty(qindex, qdelta int, bsize common.BlockSize,
	noiseEstimateEnabled bool, noiseLevel NoiseLevel,
) int {
	reductionFac := 0
	if bsize <= common.Block16x16 {
		if bsize <= common.Block8x8 {
			reductionFac = 4
		} else {
			reductionFac = 2
		}
	}
	if noiseEstimateEnabled && noiseLevel == NoiseLevelHigh {
		reductionFac = 0
	}
	dcQ := int(vp9dec.VpxDcQuant(qindex, qdelta, vp9dec.BitDepth8))
	return (20 * dcQ) >> reductionFac
}

// NewmvDiffBiasLowvarInput extracts the low-variance/high-sum-difference
// input consumed by NewmvDiffBias.
func NewmvDiffBiasLowvarInput(contentState ContentStateSB) bool {
	return contentState == ContentStateLowVarHighSumdiff
}

// NeighborIsInter mirrors libvpx's is_inter_block(MODE_INFO *mi) helper.
//
// libvpx: vp9_blockd.h is_inter_block, ref_frame[0] > INTRA_FRAME.
func NeighborIsInter(mi *vp9dec.NeighborMi) bool {
	if mi == nil {
		return false
	}
	return mi.RefFrame[0] > vp9dec.IntraFrame
}
