package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func defaultInterFrameSignBias() [vp8common.MaxRefFrames]bool {
	return [vp8common.MaxRefFrames]bool{}
}

func selectInterFrameReferenceMotionVector(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbRows int, mbCols int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (interAnalysisReference, vp8enc.MotionVector) {
	return selectInterFrameReferenceMotionVectorWithSearch(src, refs, refCount, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, qIndex, defaultInterAnalysisSearchConfig(), mvProbs)
}

func selectInterFrameReferenceMotionVectorWithSearch(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbRows int, mbCols int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (interAnalysisReference, vp8enc.MotionVector) {
	bestRef := refs[0]
	signBias := defaultInterFrameSignBias()
	bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, bestRef.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	best, bestCost := selectInterFrameMotionVectorWithSearch(src, bestRef.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, mvProbs)
	if bestCost == 0 {
		return bestRef, best
	}
	for refIndex := 1; refIndex < refCount; refIndex++ {
		ref := refs[refIndex]
		refMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
		mv, cost := selectInterFrameMotionVectorWithSearch(src, ref.Img, mbRow, mbCol, mbRows, mbCols, refMV, qIndex, search, mvProbs)
		if cost < bestCost {
			bestRef = ref
			best = mv
			bestCost = cost
			if bestCost == 0 {
				return bestRef, best
			}
		}
	}
	return bestRef, best
}

type interFrameModeDecision struct {
	ref           interAnalysisReference
	interMode     vp8enc.InterFrameMacroblockMode
	useIntra      bool
	intraMode     vp8enc.InterFrameMacroblockMode
	projectedRate int
	staleY2       staleY2Snapshot
	// predictionError is the picker `distortion` scalar returned through
	// vp8_encode_inter_macroblock and accumulated into mb.prediction_error.
	predictionError int
}

func (d interFrameModeDecision) cyclicRefreshEligible() bool {
	return !d.useIntra && d.interMode.RefFrame == vp8common.LastFrame && d.interMode.Mode == vp8common.ZeroMV
}

func libvpxAddProjectedMacroblockRate(total int, rate int) int {
	if rate <= 0 {
		return total
	}
	if total > maxInt()-rate {
		return maxInt()
	}
	return total + rate
}

func (e *VP8Encoder) selectInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	baseQIndex int, segmentation vp8enc.SegmentationConfig, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
	sourceAltRefZeroMVOnly bool,
) (interFrameModeDecision, bool) {
	segmentQIndex := encoderSegmentQIndex(baseQIndex, segmentation, segmentID)
	if !e.interAnalysisUsesRDModeDecision() {
		// Libvpx encodeframe.c resets x->rdmult/x->rddiv from the
		// frame-level cpi->RDMULT/RDDIV before vp8cx_mb_init_quantizer()
		// applies per-segment quant tables. The fast picker therefore uses
		// base_qindex for RD-cost and motion-search rate scaling, while the
		// supplied quant still reflects the candidate segment for breakout
		// and final residual coding.
		return e.selectFastInterFrameModeDecision(
			src, refs, refCount,
			mbRow, mbCol, mbRows, mbCols,
			baseQIndex, segmentID,
			above, left, aboveLeft,
			quant,
			sourceAltRefZeroMVOnly,
		)
	}
	return e.selectRDInterFrameModeDecision(
		src, refs, refCount,
		mbRow, mbCol, mbRows, mbCols,
		segmentQIndex, segmentID,
		above, left, aboveLeft,
		aboveTok, leftTok,
		quant,
		sourceAltRefZeroMVOnly,
	)
}

func (e *VP8Encoder) sourceAltRefZeroMVOnly(flags EncodeFlags) bool {
	return e != nil &&
		flags&EncodeInvisibleFrame == 0 &&
		e.opts.ARNRMaxFrames == 0 &&
		e.isSrcFrameAltRef(e.currentSourcePTS)
}

func (e *VP8Encoder) interMacroblockInactive(mbRow int, mbCol int, mbCols int) bool {
	if e == nil || !e.activeMapEnabled || mbCols <= 0 {
		return false
	}
	index := mbRow*mbCols + mbCol
	return index >= 0 && index < len(e.activeMap) && e.activeMap[index] == 0
}

func libvpxSourceAltRefCandidate(onlyAltRefZeroMV bool, refFrame vp8common.MVReferenceFrame, mode vp8common.MBPredictionMode) bool {
	return !onlyAltRefZeroMV || (mode == vp8common.ZeroMV && refFrame == vp8common.AltRefFrame)
}
