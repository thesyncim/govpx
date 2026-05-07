package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

var wholeBlockIntraYModeCandidates = [...]vp8common.MBPredictionMode{
	vp8common.DCPred,
	vp8common.VPred,
	vp8common.HPred,
	vp8common.TMPred,
}

var wholeBlockIntraUVModeCandidates = [...]vp8common.MBPredictionMode{
	vp8common.DCPred,
	vp8common.VPred,
	vp8common.HPred,
	vp8common.TMPred,
}

var bPredIntraModeCandidates = [...]vp8common.BPredictionMode{
	vp8common.BDCPred,
	vp8common.BTMPred,
	vp8common.BVEPred,
	vp8common.BHEPred,
	vp8common.BLDPred,
	vp8common.BRDPred,
	vp8common.BVRPred,
	vp8common.BVLPred,
	vp8common.BHDPred,
	vp8common.BHUPred,
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) error {
	return e.buildReconstructingKeyFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols)
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficientsWithSegmentation(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) error {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return ErrInvalidConfig
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return ErrInvalidConfig
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, vp8common.QuantDeltas{}, segmentation, &quants); err != nil {
		return ErrInvalidConfig
	}
	decSegmentation := encoderSegmentationToDecoder(segmentation)
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: uint8(qIndex)}, &decSegmentation, &e.dequantTables, &e.dequants)

	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			segmentID, ok := keyFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return ErrInvalidConfig
			}
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			var above *vp8enc.KeyFrameMacroblockMode
			var left *vp8enc.KeyFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			mode, ok := predictBestKeyFrameIntraMode(src, segmentQIndex, row, col, above, left, &quants[segmentID], &e.analysis.Img, &e.reconstructScratch)
			if !ok {
				return ErrInvalidConfig
			}
			mode.SegmentID = segmentID
			modes[index] = mode
			convertKeyFrameMode(&modes[index], &e.reconstructModes[index])
			if modes[index].YMode == vp8common.BPred {
				if !buildReconstructingBPredMacroblockCoefficients(src, row, col, &e.analysis.Img, &e.reconstructModes[index], &quants[segmentID], segmentQIndex, &coeffs[index], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
				convertMacroblockCoefficients(&coeffs[index], true, &e.reconstructTokens[index])
				continue
			}
			if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
			buildPredictedMacroblockCoefficients(src, row, col, &e.analysis.Img, &quants[segmentID], segmentQIndex, 0, modes[index].YMode == vp8common.BPred, true, &coeffs[index])
			convertMacroblockCoefficients(&coeffs[index], modes[index].YMode == vp8common.BPred, &e.reconstructTokens[index])
			if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return nil
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) error {
	return e.buildReconstructingInterFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols, flags)
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsWithSegmentation(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) error {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return ErrInvalidConfig
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return ErrInvalidConfig
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, vp8common.QuantDeltas{}, segmentation, &quants); err != nil {
		return ErrInvalidConfig
	}
	decSegmentation := encoderSegmentationToDecoder(segmentation)
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: uint8(qIndex)}, &decSegmentation, &e.dequantTables, &e.dequants)

	var refs [3]interAnalysisReference
	refCount := e.interAnalysisReferences(flags, &refs)
	if refCount == 0 {
		return ErrInvalidConfig
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			segmentID, ok := interFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return ErrInvalidConfig
			}
			var above *vp8enc.InterFrameMacroblockMode
			var left *vp8enc.InterFrameMacroblockMode
			var aboveLeft *vp8enc.InterFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			decision, ok := e.selectInterFrameModeDecision(
				src, refs[:], refCount,
				row, col, rows, cols,
				qIndex, segmentation, segmentID,
				above, left, aboveLeft,
				&quants[segmentID],
			)
			if !ok {
				return ErrInvalidConfig
			}
			if segmentID != 0 && !decision.cyclicRefreshEligible() {
				segmentID = 0
				decision, ok = e.selectInterFrameModeDecision(
					src, refs[:], refCount,
					row, col, rows, cols,
					qIndex, segmentation, segmentID,
					above, left, aboveLeft,
					&quants[segmentID],
				)
				if !ok {
					return ErrInvalidConfig
				}
			}
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			quant := &quants[segmentID]

			if decision.useIntra {
				modes[index] = decision.intraMode
				modes[index].SegmentID = segmentID
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				if modes[index].Mode == vp8common.BPred {
					if !buildReconstructingBPredMacroblockCoefficients(src, row, col, &e.analysis.Img, &e.reconstructModes[index], quant, segmentQIndex, &coeffs[index], &e.reconstructScratch) {
						return ErrInvalidConfig
					}
				} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else {
				modes[index] = decision.interMode
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				predMode := e.reconstructModes[index]
				predMode.MBSkipCoeff = true
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			}
			predictionDist := macroblockImageSSE(src, &e.analysis.Img, row, col)
			breakoutSkip := modes[index].RefFrame != vp8common.IntraFrame &&
				staticInterEncodeBreakout(src, &e.analysis.Img, row, col, quant, e.opts.StaticThreshold)
			if breakoutSkip {
				clearMacroblockCoefficients(&coeffs[index])
			} else if modes[index].RefFrame != vp8common.IntraFrame || modes[index].Mode != vp8common.BPred {
				is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
				buildPredictedMacroblockCoefficients(src, row, col, &e.analysis.Img, quant, segmentQIndex, interZbinModeBoost(&modes[index]), is4x4, modes[index].RefFrame == vp8common.IntraFrame, &coeffs[index])
			}
			is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
			modes[index].MBSkipCoeff = breakoutSkip || macroblockCoefficientsEmpty(&coeffs[index], is4x4)
			convertInterFrameMode(&modes[index], &e.reconstructModes[index])
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if modes[index].RefFrame == vp8common.IntraFrame && modes[index].Mode == vp8common.BPred {
				continue
			}
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else {
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
				if !modes[index].MBSkipCoeff {
					codedDist := macroblockImageSSE(src, &e.analysis.Img, row, col)
					tokenRate := macroblockCoefficientTokenRate(&vp8tables.DefaultCoefProbs, is4x4, &coeffs[index])
					if shouldSkipInterResidual(segmentQIndex, tokenRate, predictionDist, codedDist) {
						clearMacroblockCoefficients(&coeffs[index])
						modes[index].MBSkipCoeff = true
						convertInterFrameMode(&modes[index], &e.reconstructModes[index])
						convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
						if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
							return ErrInvalidConfig
						}
					}
				}
			}
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return nil
}

func keyFrameAnalysisSegmentID(mode *vp8enc.KeyFrameMacroblockMode, segmentation vp8enc.SegmentationConfig, preserve bool) (uint8, bool) {
	if !segmentation.Enabled || !preserve {
		return 0, true
	}
	if mode.SegmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	return mode.SegmentID, true
}

func interFrameAnalysisSegmentID(mode *vp8enc.InterFrameMacroblockMode, segmentation vp8enc.SegmentationConfig, preserve bool) (uint8, bool) {
	if !segmentation.Enabled || !preserve {
		return 0, true
	}
	if mode.SegmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	return mode.SegmentID, true
}

func encoderSegmentQIndex(baseQ int, segmentation vp8enc.SegmentationConfig, segmentID uint8) int {
	if !segmentation.Enabled || segmentID >= vp8common.MaxMBSegments || !segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] {
		return vp8common.ClampQIndex(baseQ)
	}
	altQ := int(segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID])
	if segmentation.AbsDelta {
		return vp8common.ClampQIndex(altQ)
	}
	return vp8common.ClampQIndex(baseQ + altQ)
}

func encoderSegmentationToDecoder(segmentation vp8enc.SegmentationConfig) vp8dec.SegmentationHeader {
	if !segmentation.Enabled {
		return vp8dec.SegmentationHeader{}
	}
	var out vp8dec.SegmentationHeader
	out.Enabled = true
	out.UpdateMap = segmentation.UpdateMap
	out.UpdateData = segmentation.UpdateData
	out.AbsDelta = segmentation.AbsDelta
	out.TreeProbs = segmentation.TreeProbs
	for feature := 0; feature < int(vp8common.MBLvlMax); feature++ {
		for segment := 0; segment < vp8common.MaxMBSegments; segment++ {
			if segmentation.FeatureEnabled[feature][segment] {
				out.FeatureData[feature][segment] = segmentation.FeatureData[feature][segment]
			}
		}
	}
	return out
}

type interAnalysisReference struct {
	Frame vp8common.MVReferenceFrame
	Img   *vp8common.Image
}

type interAnalysisMotionCandidate struct {
	Ref interAnalysisReference
	MV  vp8enc.MotionVector
}

func (e *VP8Encoder) interAnalysisReferences(flags EncodeFlags, refs *[3]interAnalysisReference) int {
	count := 0
	if flags&EncodeNoReferenceLast == 0 {
		refs[count] = interAnalysisReference{Frame: vp8common.LastFrame, Img: &e.lastRef.Img}
		count++
	}
	if flags&EncodeNoReferenceGolden == 0 {
		refs[count] = interAnalysisReference{Frame: vp8common.GoldenFrame, Img: &e.goldenRef.Img}
		count++
	}
	if flags&EncodeNoReferenceAltRef == 0 {
		refs[count] = interAnalysisReference{Frame: vp8common.AltRefFrame, Img: &e.altRef.Img}
		count++
	}
	return count
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c fractional motion search.
// vp8_hex_search finishes with an eight-step full-pixel diamond refinement.
const interFrameFullPixelSearchRadius = 8
const interFrameMVSearchRange = interFrameFullPixelSearchRadius * 8
const interFrameMVFullPixelStep = 8
const interFrameSubpixelSearchMaxCandidates = 31
const interFrameMotionCandidateMax = 15

func interFrameFullPixelSearchCandidateCount() int {
	axis := (2*interFrameMVSearchRange)/interFrameMVFullPixelStep + 1
	return axis * axis
}

func interFrameSubpixelSearchCandidateCount() int {
	return interFrameSubpixelSearchMaxCandidates
}

func selectInterFrameReferenceMotionVector(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, qIndex int) (interAnalysisReference, vp8enc.MotionVector) {
	bestRef := refs[0]
	best, bestCost := selectInterFrameMotionVector(src, bestRef.Img, mbRow, mbCol, qIndex)
	if bestCost == 0 {
		return bestRef, best
	}
	for refIndex := 1; refIndex < refCount; refIndex++ {
		ref := refs[refIndex]
		mv, cost := selectInterFrameMotionVector(src, ref.Img, mbRow, mbCol, qIndex)
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
	ref       interAnalysisReference
	interMode vp8enc.InterFrameMacroblockMode
	useIntra  bool
	intraMode vp8enc.InterFrameMacroblockMode
}

func (d interFrameModeDecision) cyclicRefreshEligible() bool {
	return !d.useIntra && d.interMode.RefFrame == vp8common.LastFrame && d.interMode.Mode == vp8common.ZeroMV
}

func (e *VP8Encoder) selectInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	baseQIndex int, segmentation vp8enc.SegmentationConfig, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	quant *vp8enc.MacroblockQuant,
) (interFrameModeDecision, bool) {
	segmentQIndex := encoderSegmentQIndex(baseQIndex, segmentation, segmentID)
	ref, interMode, interScore, ok := e.selectBestInterFrameMode(
		src, refs, refCount,
		mbRow, mbCol, mbRows, mbCols,
		segmentQIndex, segmentID,
		above, left, aboveLeft,
		quant,
	)
	if !ok {
		return interFrameModeDecision{}, false
	}
	decision := interFrameModeDecision{
		ref:       ref,
		interMode: interMode,
		intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID},
	}
	if macroblockSAD(src, ref.Img, mbRow, mbCol, interMode.MV) <= 0 {
		return decision, true
	}
	intraMode, intraCost, ok := predictBestInterIntraModeCost(src, segmentQIndex, mbRow, mbCol, quant, &e.analysis.Img, &e.reconstructScratch)
	if !ok {
		return interFrameModeDecision{}, false
	}
	intraMode.SegmentID = segmentID
	decision.intraMode = intraMode
	decision.useIntra = intraCost < interScore
	return decision, true
}

func (e *VP8Encoder) selectBestInterFrameMode(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	quant *vp8enc.MacroblockQuant,
) (interAnalysisReference, vp8enc.InterFrameMacroblockMode, int, bool) {
	var candidates [interFrameMotionCandidateMax]interAnalysisMotionCandidate
	candidateCount := collectInterFrameMotionCandidates(src, refs, refCount, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, &candidates)
	if candidateCount == 0 {
		return interAnalysisReference{}, vp8enc.InterFrameMacroblockMode{}, 0, false
	}

	bestSet := false
	var bestRef interAnalysisReference
	var bestMode vp8enc.InterFrameMacroblockMode
	bestScore := maxInt()
	for candidateIndex := 0; candidateIndex < candidateCount; candidateIndex++ {
		candidate := candidates[candidateIndex]
		mode := vp8enc.InterFrameMotionModeForVectorAt(candidate.Ref.Frame, candidate.MV, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		mode.SegmentID = segmentID
		score, ok := e.estimateInterResidualRDScore(src, candidate.Ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, quant, qIndex, segmentID)
		if !ok {
			continue
		}
		if !bestSet || score < bestScore {
			bestSet = true
			bestRef = candidate.Ref
			bestMode = mode
			bestScore = score
		}
	}
	for refIndex := 0; refIndex < refCount && refIndex < len(refs); refIndex++ {
		ref := refs[refIndex]
		for partition := 0; partition < vp8tables.NumMBSplits; partition++ {
			mode, ok := selectInterFrameSplitMotionMode(src, ref.Img, ref.Frame, mbRow, mbCol, qIndex, partition)
			if !ok {
				continue
			}
			mode.SegmentID = segmentID
			score, ok := e.estimateInterResidualRDScore(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, quant, qIndex, segmentID)
			if !ok {
				continue
			}
			if !bestSet || score < bestScore {
				bestSet = true
				bestRef = ref
				bestMode = mode
				bestScore = score
			}
		}
	}
	if !bestSet {
		return interAnalysisReference{}, vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	return bestRef, bestMode, bestScore, true
}

func selectInterFrameSplitMotionMode(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, qIndex int, partition int) (vp8enc.InterFrameMacroblockMode, bool) {
	if ref == nil || refFrame == vp8common.IntraFrame || partition < 0 || partition >= vp8tables.NumMBSplits {
		return vp8enc.InterFrameMacroblockMode{}, false
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  refFrame,
		Mode:      vp8common.SplitMV,
		Partition: uint8(partition),
	}
	first := vp8enc.MotionVector{}
	allSame := true
	width, height := splitMotionPartitionBlockSize(partition)
	for subset := 0; subset < int(vp8tables.MBSplitCount[mode.Partition]); subset++ {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		mv, _ := selectInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, qIndex)
		if subset == 0 {
			first = mv
		} else if mv != first {
			allSame = false
		}
		fillInterFrameSplitSubset(&mode, subset, mv)
	}
	mode.MV = mode.BlockMV[15]
	if allSame {
		return vp8enc.InterFrameMacroblockMode{}, false
	}
	return mode, true
}

func splitMotionPartitionBlockSize(partition int) (int, int) {
	switch partition {
	case 0:
		return 16, 8
	case 1:
		return 8, 16
	case 2:
		return 8, 8
	default:
		return 4, 4
	}
}

func fillInterFrameSplitSubset(mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector) {
	fillCount := int(vp8tables.MBSplitFillCount[mode.Partition])
	fillStart := subset * fillCount
	for i := 0; i < fillCount; i++ {
		mode.BlockMV[vp8tables.MBSplitFillOffset[mode.Partition][fillStart+i]] = mv
	}
}

func collectInterFrameMotionCandidates(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	if candidates == nil {
		return 0
	}
	count := 0
	for refIndex := 0; refIndex < refCount && refIndex < len(refs); refIndex++ {
		ref := refs[refIndex]
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, vp8enc.MotionVector{})
		nearest, near := interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, nearest)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, near)
		fullMV, fullCost := selectInterFrameFullPixelMotionVector(src, ref.Img, mbRow, mbCol, qIndex)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, fullMV)
		if fullCost == 0 {
			continue
		}
		refinedMV, _, ok := iterativeInterFrameSubpixelMotionVector(src, ref.Img, mbRow, mbCol, fullMV, qIndex)
		if ok && refinedMV != fullMV {
			count = appendInterAnalysisMotionCandidate(candidates, count, ref, refinedMV)
		}
	}
	return count
}

func interAnalysisReferenceMotionPredictors(refFrame vp8common.MVReferenceFrame, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) (vp8enc.MotionVector, vp8enc.MotionVector) {
	return vp8enc.InterFrameNearMotionVectorsAt(above, left, aboveLeft, refFrame, mbRow, mbCol, mbRows, mbCols)
}

func appendInterAnalysisMotionCandidate(candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate, count int, ref interAnalysisReference, mv vp8enc.MotionVector) int {
	if candidates == nil || count >= len(candidates) {
		return count
	}
	for i := 0; i < count; i++ {
		if candidates[i].Ref.Frame == ref.Frame && candidates[i].Ref.Img == ref.Img && candidates[i].MV == mv {
			return count
		}
	}
	candidates[count] = interAnalysisMotionCandidate{Ref: ref, MV: mv}
	return count + 1
}

func predictBestWholeBlockIntraMode(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, bool) {
	yMode, uvMode, _, ok := predictBestWholeBlockIntraModeCost(src, vp8common.MinQ, true, mbRow, mbCol, pred, scratch)
	return yMode, uvMode, ok
}

func predictBestKeyFrameIntraMode(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8enc.KeyFrameMacroblockMode, bool) {
	wholeY, wholeUV, wholeCost, ok := predictBestWholeBlockIntraModeCost(src, qIndex, true, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, false
	}
	best := vp8enc.KeyFrameMacroblockMode{YMode: wholeY, UVMode: wholeUV}
	bModes, bPredCost, ok := predictBestBPredLumaModeCost(src, qIndex, true, mbRow, mbCol, above, left, quant, pred, scratch)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, false
	}
	bPredUV, bPredUVCost, ok := predictBestIntraChromaModeCost(src, qIndex, true, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, false
	}
	bPredRate := intraYModeRate(true, vp8common.BPred)
	if bPredCost+bPredUVCost+rdRateOnly(qIndex, bPredRate) < wholeCost {
		best = vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred, UVMode: bPredUV, BModes: bModes}
	}
	return best, true
}

func predictBestInterIntraModeCost(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8enc.InterFrameMacroblockMode, int, bool) {
	wholeY, wholeUV, wholeCost, ok := predictBestWholeBlockIntraModeCost(src, qIndex, false, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	wholeCost += rdRateOnly(qIndex, interIntraMacroblockModeRate())
	best := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: wholeY, UVMode: wholeUV}
	bestCost := wholeCost
	bModes, bPredCost, ok := predictBestBPredLumaModeCost(src, qIndex, false, mbRow, mbCol, nil, nil, quant, pred, scratch)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	bPredUV, bPredUVCost, ok := predictBestIntraChromaModeCost(src, qIndex, false, mbRow, mbCol, pred, scratch)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	bPredRate := intraYModeRate(false, vp8common.BPred)
	bPredTotal := bPredCost + bPredUVCost + rdRateOnly(qIndex, bPredRate+interIntraMacroblockModeRate())
	if bPredTotal < bestCost {
		best = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: bPredUV, BModes: bModes}
		bestCost = bPredTotal
	}
	return best, bestCost, true
}

func predictBestWholeBlockIntraModeCost(src vp8enc.SourceImage, qIndex int, keyFrame bool, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, bool) {
	bestYMode := vp8common.DCPred
	bestYCost := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, false
		}
		cost := rdModeScore(qIndex, intraYModeRate(keyFrame, yMode), macroblockLumaSSE(src, pred, mbRow, mbCol, vp8enc.MotionVector{}))
		if i == 0 || cost < bestYCost {
			bestYMode = yMode
			bestYCost = cost
		}
	}

	bestUVMode, bestUVCost, ok := predictBestIntraChromaModeCost(src, qIndex, keyFrame, mbRow, mbCol, pred, scratch)
	if !ok {
		return 0, 0, 0, false
	}
	return bestYMode, bestUVMode, bestYCost + bestUVCost, true
}

func predictBestIntraChromaModeCost(src vp8enc.SourceImage, qIndex int, keyFrame bool, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8common.MBPredictionMode, int, bool) {
	bestUVMode := vp8common.DCPred
	bestUVCost := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, false
		}
		cost := rdModeScore(qIndex, intraUVModeRate(keyFrame, uvMode), macroblockChromaSSE(src, pred, mbRow, mbCol))
		if i == 0 || cost < bestUVCost {
			bestUVMode = uvMode
			bestUVCost = cost
		}
	}
	return bestUVMode, bestUVCost, true
}

func predictBestBPredLumaModeCost(src vp8enc.SourceImage, qIndex int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) ([16]vp8common.BPredictionMode, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, false
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	var tokenAbove [4]uint8
	var tokenLeft [4]uint8
	totalCost := 0
	for block := 0; block < 16; block++ {
		bestMode := vp8common.BDCPred
		bestEOB := 0
		var bestRecon [16]byte
		bestCost := 0
		for i, candidate := range bPredIntraModeCandidates {
			var candidatePred [16]byte
			if !predictAnalysisBPredBlock(candidate, candidatePred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, false
			}
			var input [16]int16
			var dct [16]int16
			var qcoeff [16]int16
			var dqcoeff [16]int16
			fillBPredResidual4x4(src, mbRow, mbCol, block, candidatePred[:], 4, &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			tokenCtx := int(tokenAbove[block&3] + tokenLeft[(block&0x0c)>>2])
			eob := quantizeOptimizedBlock(qIndex, 3, tokenCtx, 0, 0, true, &dct, &quant.Y1, &qcoeff, &dqcoeff)
			coefRate := coefficientBlockTokenRate(&vp8tables.DefaultCoefProbs, 3, tokenCtx, 0, &qcoeff, eob)
			aboveMode := bPredAnalysisAboveMode(keyFrame, above, modes, block)
			leftMode := bPredAnalysisLeftMode(keyFrame, left, modes, block)
			rate := bPredModeRate(keyFrame, candidate, aboveMode, leftMode) + coefRate
			cost := rdModeScore(qIndex, rate, transformBlockError(&dct, &dqcoeff)>>2)
			if i == 0 || cost < bestCost {
				var candidateRecon [16]byte
				bestMode = candidate
				dsp.IDCT4x4Add(&dqcoeff, candidatePred[:], 4, candidateRecon[:], 4)
				bestRecon = candidateRecon
				bestEOB = eob
				bestCost = cost
			}
		}
		modes[block] = bestMode
		copyBPredBlock(bestRecon[:], 4, y, pred.YStride, block)
		hasCoeffs := uint8(0)
		if bestEOB > 0 {
			hasCoeffs = 1
		}
		tokenAbove[block&3] = hasCoeffs
		tokenLeft[(block&0x0c)>>2] = hasCoeffs
		totalCost += bestCost
	}
	return modes, totalCost, true
}

func bPredAnalysisAboveMode(keyFrame bool, above *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block >= 4 {
		return modes[block-4]
	}
	if above == nil {
		return vp8common.BDCPred
	}
	if above.YMode == vp8common.BPred {
		return above.BModes[block+12]
	}
	return blockModeFromKeyFrameMacroblockMode(above.YMode)
}

func bPredAnalysisLeftMode(keyFrame bool, left *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block&3 != 0 {
		return modes[block-1]
	}
	if left == nil {
		return vp8common.BDCPred
	}
	if left.YMode == vp8common.BPred {
		return left.BModes[block+3]
	}
	return blockModeFromKeyFrameMacroblockMode(left.YMode)
}

func blockModeFromKeyFrameMacroblockMode(mode vp8common.MBPredictionMode) vp8common.BPredictionMode {
	switch mode {
	case vp8common.VPred:
		return vp8common.BVEPred
	case vp8common.HPred:
		return vp8common.BHEPred
	case vp8common.TMPred:
		return vp8common.BTMPred
	default:
		return vp8common.BDCPred
	}
}

func intraYModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], int(mode))
	}
	return treeTokenCost(vp8tables.YModeTree[:], vp8tables.DefaultYModeProbs[:], int(mode))
}

func intraUVModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], int(mode))
	}
	return treeTokenCost(vp8tables.UVModeTree[:], vp8tables.DefaultUVModeProbs[:], int(mode))
}

func bPredModeRate(keyFrame bool, mode vp8common.BPredictionMode, above vp8common.BPredictionMode, left vp8common.BPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[int(above)][int(left)][:], int(mode))
	}
	return treeTokenCost(vp8tables.BModeTree[:], vp8tables.DefaultBModeProbs[:], int(mode))
}

func coefficientBlockTokenRate(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	if probs == nil || qcoeff == nil || blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return maxInt() / 4
	}
	p := (*probs)[blockType][skipDC][ctx]
	if eob <= skipDC {
		return treeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
	}

	cost := boolBitCost(p[0], 1)
	for pos := skipDC; pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		if coeff == 0 {
			cost += boolBitCost(p[1], 0)
			if pos == 15 {
				return cost
			}
			p = (*probs)[blockType][vp8tables.CoefBandsTable[pos+1]][0]
			continue
		}

		token, mag, ok := coefficientTokenMagnitude(coeff)
		if !ok {
			return maxInt() / 4
		}
		cost += nonZeroCoeffTokenRate(p, token)
		if coeff < 0 {
			cost += boolBitCost(128, 1)
		} else {
			cost += boolBitCost(128, 0)
		}
		cost += coefficientExtraBitsRate(token, mag)
		if pos == 15 {
			return cost
		}
		p = (*probs)[blockType][vp8tables.CoefBandsTable[pos+1]][vp8tables.PrevTokenClass[token]]
		if pos+1 == eob {
			cost += treeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
			return cost
		}
	}
	return cost
}

func nonZeroCoeffTokenRate(probs [vp8tables.EntropyNodes]uint8, token int) int {
	cost := treeTokenCost(vp8tables.CoefTree[:], probs[:], token)
	nonEOBRate := boolBitCost(probs[0], 1)
	if cost <= nonEOBRate {
		return maxInt() / 4
	}
	return cost - nonEOBRate
}

func coefficientTokenMagnitude(coeff int) (int, int, bool) {
	if coeff < 0 {
		coeff = -coeff
	}
	switch {
	case coeff <= 0:
		return 0, 0, false
	case coeff == 1:
		return vp8tables.OneToken, coeff, true
	case coeff == 2:
		return vp8tables.TwoToken, coeff, true
	case coeff == 3:
		return vp8tables.ThreeToken, coeff, true
	case coeff == 4:
		return vp8tables.FourToken, coeff, true
	case coeff <= 6:
		return vp8tables.DCTValCategory1, coeff, true
	case coeff <= 10:
		return vp8tables.DCTValCategory2, coeff, true
	case coeff <= 18:
		return vp8tables.DCTValCategory3, coeff, true
	case coeff <= 34:
		return vp8tables.DCTValCategory4, coeff, true
	case coeff <= 66:
		return vp8tables.DCTValCategory5, coeff, true
	case coeff <= vp8tables.DCTMaxValue:
		return vp8tables.DCTValCategory6, coeff, true
	default:
		return 0, 0, false
	}
}

func coefficientExtraBitsRate(token int, mag int) int {
	extra := vp8tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += boolBitCost(extra.Prob[i], bit)
	}
	return cost
}

func treeTokenCost(tree []int16, probs []uint8, token int) int {
	var encoded vp8enc.TreeToken
	if !vp8enc.BuildTreeToken(tree, token, &encoded) {
		return maxInt() / 4
	}
	node := int16(0)
	cost := 0
	for bitIndex := int(encoded.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(probs) || int(node)+1 >= len(tree) {
			return maxInt() / 4
		}
		prob := probs[probIndex]
		bit := int((encoded.Value >> uint(bitIndex)) & 1)
		cost += boolBitCost(prob, bit)
		next := tree[int(node)+bit]
		if next <= 0 {
			if bitIndex == 0 {
				return cost
			}
			return maxInt() / 4
		}
		node = next
	}
	return maxInt() / 4
}

func boolBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return vp8tables.ProbCost[prob]
	}
	return vp8tables.ProbCost[255-int(prob)]
}

func rdModeScore(qIndex int, rate int, distortion int) int {
	rdMult, rdDiv := libvpxRDConstants(qIndex)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func rdRateOnly(qIndex int, rate int) int {
	return rdModeScore(qIndex, rate, 0)
}

// libvpxErrorPerBit ports the encodeframe.c errorperbit derivation used by
// libvpx fractional motion searches.
func libvpxErrorPerBit(qIndex int) int {
	rdMult, rdDiv := libvpxRDConstants(qIndex)
	errorPerBit := rdMult * 100 / (110 * rdDiv)
	if errorPerBit == 0 {
		return 1
	}
	return errorPerBit
}

// libvpxSADPerBit16 ports sad_per_bit16lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts.
func libvpxSADPerBit16(qIndex int) int {
	return libvpxSADPerBit16LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxRDConstants ports vp8_initialize_rd_consts for the single-pass
// zbin_over_quant=0 path used by this encoder.
func libvpxRDConstants(qIndex int) (int, int) {
	qValue := vp8common.DCQuant(qIndex, 0)
	if qValue > 160 {
		qValue = 160
	}
	rdMult := int(2.80 * float64(qValue*qValue))
	rdDiv := 100
	if rdMult > 1000 {
		rdDiv = 1
		rdMult /= 100
	}
	return rdMult, rdDiv
}

func libvpxRDCost(rdMult int, rdDiv int, rate int, distortion int) int {
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

var libvpxSADPerBit16LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11,
	11, 11, 11, 11, 12, 12, 12, 12,
	12, 12, 13, 13, 13, 13, 14, 14,
}

func interMotionRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, qIndex int) int {
	return rdModeScore(qIndex, interMotionVectorCost(mv), macroblockLumaSSE(src, ref, mbRow, mbCol, mv))
}

func (e *VP8Encoder) estimateInterResidualRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8) (int, bool) {
	if ref == nil || mode == nil || quant == nil || segmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	var decMode vp8dec.MacroblockMode
	convertInterFrameMode(mode, &decMode)
	predMode := decMode
	predMode.MBSkipCoeff = true
	var zeroTokens vp8dec.MacroblockTokens
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref, mbRow, mbCol, &predMode, &zeroTokens, &e.dequants[segmentID], &e.reconstructScratch) {
		return 0, false
	}

	modeRate := interMotionModeRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
	predictionDist := macroblockImageSSE(src, &e.analysis.Img, mbRow, mbCol)
	skipScore := rdModeScore(qIndex, modeRate+interMacroblockSkipRate(true), predictionDist)
	if staticInterEncodeBreakout(src, &e.analysis.Img, mbRow, mbCol, quant, e.opts.StaticThreshold) {
		return skipScore, true
	}

	var coeffs vp8enc.MacroblockCoefficients
	is4x4 := interFrameModeUses4x4Tokens(mode.Mode)
	buildPredictedMacroblockCoefficients(src, mbRow, mbCol, &e.analysis.Img, quant, qIndex, interZbinModeBoost(mode), is4x4, false, &coeffs)
	if macroblockCoefficientsEmpty(&coeffs, is4x4) {
		return skipScore, true
	}

	var tokens vp8dec.MacroblockTokens
	convertMacroblockCoefficients(&coeffs, is4x4, &tokens)
	decMode.MBSkipCoeff = false
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref, mbRow, mbCol, &decMode, &tokens, &e.dequants[segmentID], &e.reconstructScratch) {
		return 0, false
	}
	codedDist := macroblockImageSSE(src, &e.analysis.Img, mbRow, mbCol)
	tokenRate := macroblockCoefficientTokenRate(&vp8tables.DefaultCoefProbs, is4x4, &coeffs)
	if shouldSkipInterResidual(qIndex, tokenRate, predictionDist, codedDist) {
		return skipScore, true
	}
	return rdModeScore(qIndex, modeRate+interMacroblockSkipRate(false)+tokenRate, codedDist), true
}

func selectInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, qIndex int) (vp8enc.MotionVector, int) {
	best, bestCost := selectInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, qIndex)
	if bestCost == 0 {
		return best, bestCost
	}
	bestRD := interMotionRDScore(src, ref, mbRow, mbCol, best, qIndex)
	if refined, _, ok := iterativeInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, qIndex); ok {
		refinedRD := interMotionRDScore(src, ref, mbRow, mbCol, refined, qIndex)
		if refinedRD < bestRD {
			best = refined
			bestRD = refinedRD
		}
	}
	return best, bestRD
}

func selectInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, qIndex int) (vp8enc.MotionVector, int) {
	best := vp8enc.MotionVector{}
	bestCost := interMotionSearchCost(src, ref, mbRow, mbCol, best, qIndex)
	if bestCost == 0 {
		return best, bestCost
	}
	return exhaustiveInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestCost, qIndex)
}

func selectInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, qIndex int) (vp8enc.MotionVector, int) {
	best := vp8enc.MotionVector{}
	bestCost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, best, qIndex)
	if bestCost == 0 {
		return best, bestCost
	}
	for row := -interFrameMVSearchRange; row <= interFrameMVSearchRange; row += interFrameMVFullPixelStep {
		for col := -interFrameMVSearchRange; col <= interFrameMVSearchRange; col += interFrameMVFullPixelStep {
			mv := vp8enc.MotionVector{Row: int16(row), Col: int16(col)}
			if mv == best {
				continue
			}
			cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, qIndex)
			if cost < bestCost {
				best = mv
				bestCost = cost
				if bestCost == 0 {
					return best, bestCost
				}
			}
		}
	}
	return best, bestCost
}

func exhaustiveInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestCost int, qIndex int) (vp8enc.MotionVector, int) {
	for row := -interFrameMVSearchRange; row <= interFrameMVSearchRange; row += interFrameMVFullPixelStep {
		for col := -interFrameMVSearchRange; col <= interFrameMVSearchRange; col += interFrameMVFullPixelStep {
			mv := vp8enc.MotionVector{Row: int16(row), Col: int16(col)}
			if mv == best {
				continue
			}
			cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestCost, qIndex)
			if cost < bestCost {
				best = mv
				bestCost = cost
			}
		}
	}
	return best, bestCost
}

func iterativeInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	bestDist, _, ok := macroblockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCost(bestMV, qIndex)

	for halfiters := 0; halfiters < 3; halfiters++ {
		leftCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc-2, qIndex)
		rightCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc+2, qIndex)
		upCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr-2, tc, qIndex)
		downCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr+2, tc, qIndex)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, diagRow, diagCol, qIndex)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for quarteriters := 0; quarteriters < 3; quarteriters++ {
		leftCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc-1, qIndex)
		rightCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc+1, qIndex)
		upCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr-1, tc, qIndex)
		downCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr+1, tc, qIndex)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, diagRow, diagCol, qIndex)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	return vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}, bestCost, true
}

func subpixelMotionSearchCandidateCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, row int, col int, qIndex int) (int, bool) {
	dist, _, ok := macroblockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, row, col)
	if !ok {
		return maxInt(), false
	}
	mv := vp8enc.MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
	return dist + interMotionSearchErrorVectorCost(mv, qIndex), true
}

func updateSubpixelSearchBest(bestCost int, bestRow int, bestCol int, candidateCost int, candidateRow int, candidateCol int) (int, int, int) {
	if candidateCost < bestCost {
		return candidateCost, candidateRow, candidateCol
	}
	return bestCost, bestRow, bestCol
}

func macroblockSubpixelVarianceForQuarterMV(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, row int, col int) (int, int, bool) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY < 0 || baseX < 0 || baseY+16 > src.Height || baseX+16 > src.Width {
		return 0, 0, false
	}
	refBaseY := baseY + (row >> 2)
	refBaseX := baseX + (col >> 2)
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	return macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset)
}

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, qIndex int) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionSearchVectorCost(mv, qIndex)
}

func interMotionSplitBlockSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, qIndex int) int {
	return splitBlockSAD(src, ref, mbRow, mbCol, block, width, height, mv) + interMotionSearchVectorCost(mv, qIndex)
}

func interMotionSearchCostLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int, qIndex int) int {
	mvCost := interMotionSearchVectorCost(mv, qIndex)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, sadLimit) + mvCost
}

func interMotionSearchVectorCost(mv vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, vp8enc.MotionVector{}, libvpxSADPerBit16(qIndex))
}

func interMotionSearchErrorVectorCost(mv vp8enc.MotionVector, qIndex int) int {
	probs := vp8tables.DefaultMVContext
	return vp8enc.MotionVectorErrorCost(mv, vp8enc.MotionVector{}, &probs, libvpxErrorPerBit(qIndex))
}

func interMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame, mbRow, mbCol, mbRows, mbCols)
	if mode.Mode == vp8common.SplitMV {
		return splitMotionModeVectorCost(mode, left, above, best)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	delta := vp8enc.MotionVector{Row: int16(int(mode.MV.Row) - int(best.Row)), Col: int16(int(mode.MV.Col) - int(best.Col))}
	return interMotionVectorCost(delta)
}

func interMacroblockSkipRate(skip bool) int {
	if skip {
		return boolBitCost(128, 1)
	}
	return boolBitCost(128, 0)
}

func interIntraMacroblockModeRate() int {
	return interMacroblockSkipRate(false) + boolBitCost(128, 0)
}

func interMotionModeRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(128, 0)
	}
	return boolBitCost(128, 1) +
		interReferenceFrameRate(mode.RefFrame) +
		interPredictionModeRate(mode.Mode, vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame)) +
		interMotionModeVectorCost(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
}

func interReferenceFrameRate(refFrame vp8common.MVReferenceFrame) int {
	switch refFrame {
	case vp8common.LastFrame:
		return boolBitCost(128, 0)
	case vp8common.GoldenFrame:
		return boolBitCost(128, 1) + boolBitCost(128, 0)
	case vp8common.AltRefFrame:
		return boolBitCost(128, 1) + boolBitCost(128, 1)
	default:
		return 1 << 30
	}
}

func interPredictionModeRate(mode vp8common.MBPredictionMode, counts vp8enc.InterModeCounts) int {
	probs := vp8tables.InterModeContexts
	switch mode {
	case vp8common.ZeroMV:
		return boolBitCost(probs[counts.Intra][0], 0)
	case vp8common.NearestMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 0)
	case vp8common.NearMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 0)
	case vp8common.NewMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 0)
	case vp8common.SplitMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 1)
	default:
		return 1 << 30
	}
}

func splitMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector) int {
	if mode.Partition >= vp8tables.NumMBSplits {
		return 1 << 30
	}
	cost := 0
	partitions := int(vp8tables.MBSplitCount[mode.Partition])
	for subset := 0; subset < partitions; subset++ {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		leftMV := analysisSplitLeftMV(mode, left, block)
		aboveMV := analysisSplitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		probs := analysisSubMVRefProbs(leftMV, aboveMV)
		if target == leftMV {
			cost += analysisBoolBitCost(probs[0], 0)
			continue
		}
		cost += analysisBoolBitCost(probs[0], 1)
		if target == aboveMV {
			cost += analysisBoolBitCost(probs[1], 0)
			continue
		}
		cost += analysisBoolBitCost(probs[1], 1)
		if target.IsZero() {
			cost += analysisBoolBitCost(probs[2], 0)
			continue
		}
		cost += analysisBoolBitCost(probs[2], 1)
		delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
		cost += interMotionVectorCost(delta)
	}
	return cost
}

func analysisSplitLeftMV(cur *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block&3 == 0 {
		if left == nil {
			return vp8enc.MotionVector{}
		}
		if left.Mode == vp8common.SplitMV {
			return left.BlockMV[block+3]
		}
		return left.MV
	}
	return cur.BlockMV[block-1]
}

func analysisSplitAboveMV(cur *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block>>2 == 0 {
		if above == nil {
			return vp8enc.MotionVector{}
		}
		if above.Mode == vp8common.SplitMV {
			return above.BlockMV[block+12]
		}
		return above.MV
	}
	return cur.BlockMV[block-4]
}

func analysisSubMVRefProbs(left vp8enc.MotionVector, above vp8enc.MotionVector) [3]uint8 {
	lez := 0
	if left.IsZero() {
		lez = 1
	}
	aez := 0
	if above.IsZero() {
		aez = 1
	}
	lea := 0
	if left == above {
		lea = 1
	}
	return vp8tables.SubMVRefProb3[(aez<<2)|(lez<<1)|lea]
}

func analysisBoolBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return vp8tables.ProbCost[prob]
	}
	return vp8tables.ProbCost[255-int(prob)]
}

func interMotionVectorCost(mv vp8enc.MotionVector) int {
	return vp8enc.MotionVectorCost(mv)
}

func macroblockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, maxInt())
}

func macroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride)
	}

	sse := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}

func macroblockSADLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if sad, ok := macroblockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset, limit); ok {
				return sad
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride, limit)
	}

	sad := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

func splitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	if baseY >= 0 && baseX >= 0 &&
		baseY+height <= src.Height && baseX+width <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+height <= ref.CodedHeight && refBaseX+width <= ref.CodedWidth {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock := ref.Y[refBaseY*ref.YStride+refBaseX:]
		switch {
		case width == 16 && height == 8:
			return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 8 && height == 16:
			return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 8 && height == 8:
			return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 4 && height == 4:
			return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
		}
	}

	sad := 0
	for row := 0; row < height; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < width; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func macroblockSubpixelSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16), true
}

func macroblockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int, limit int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16, limit), true
}

func macroblockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+17 > ref.CodedHeight+ref.YBorder ||
		refBaseX+17 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+16*ref.YStride+17 > len(ref.YFull) {
		return 0, 0, false
	}
	variance, sse := dsp.SubpelVariance16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
	return variance, sse, true
}

func macroblockChromaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	if baseY >= 0 && baseX >= 0 &&
		baseY+8 <= uvHeight && baseX+8 <= uvWidth &&
		baseY+8 <= refUVHeight && baseX+8 <= refUVWidth {
		srcOffset := baseY*src.UStride + baseX
		refOffset := baseY*ref.UStride + baseX
		return dsp.SSE8x8(src.U[srcOffset:], src.UStride, ref.U[refOffset:], ref.UStride) +
			dsp.SSE8x8(src.V[baseY*src.VStride+baseX:], src.VStride, ref.V[baseY*ref.VStride+baseX:], ref.VStride)
	}

	sse := 0
	for row := 0; row < 8; row++ {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		refY := clampEncodeCoord(baseY+row, refUVHeight)
		for col := 0; col < 8; col++ {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			refX := clampEncodeCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

func macroblockLumaVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		baseY+16 <= ref.CodedHeight && baseX+16 <= ref.CodedWidth {
		srcStart := src.Y[baseY*src.YStride+baseX:]
		refStart := ref.Y[baseY*ref.YStride+baseX:]
		return dsp.Variance16x16(srcStart, src.YStride, refStart, ref.YStride),
			dsp.SSE16x16(srcStart, src.YStride, refStart, ref.YStride)
	}

	sum := 0
	sse := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

func buildPredictedMacroblockCoefficients(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, quant *vp8enc.MacroblockQuant, qIndex int, zbinModeBoost int, is4x4 bool, intra bool, coeffs *vp8enc.MacroblockCoefficients) {
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16
	var input [16]int16
	var dct [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8

	for block := 0; block < 16; block++ {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		if is4x4 {
			eob := quantizeBlockWithZbin(&dct, &quant.Y1, qIndex, zbinModeBoost, &coeffs.QCoeff[block], &dq)
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob = optimizeQuantizedBlock(qIndex, 3, ctx, 0, intra, &dct, &quant.Y1, &coeffs.QCoeff[block], eob)
			coeffs.SetBlockEOB(block, eob)
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
		} else {
			y2Input[block] = dct[0]
			dct[0] = 0
			eob := quantizeBlockWithZbin(&dct, &quant.Y1DC, qIndex, zbinModeBoost, &coeffs.QCoeff[block], &dq)
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob = optimizeQuantizedBlock(qIndex, 0, ctx, 1, intra, &dct, &quant.Y1DC, &coeffs.QCoeff[block], eob)
			coeffs.SetBlockEOB(block, eob)
			hasCoeffs := uint8(0)
			if eob > 1 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
		}
	}
	if !is4x4 {
		vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
		eob := quantizeBlockWithZbin(&y2Coeff, &quant.Y2, qIndex, zbinModeBoost, &coeffs.QCoeff[24], &dq)
		eob = optimizeQuantizedBlock(qIndex, 1, 0, 0, intra, &y2Coeff, &quant.Y2, &coeffs.QCoeff[24], eob)
		coeffs.SetBlockEOB(24, eob)
	} else {
		coeffs.SetBlockEOB(24, 0)
	}

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		y := mbRow*8 + (block>>1)*4
		fillPredictedResidual4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := quantizeBlockWithZbin(&dct, &quant.UV, qIndex, zbinModeBoost, &coeffs.QCoeff[16+block], &dq)
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob = optimizeQuantizedBlock(qIndex, 2, ctx, 0, intra, &dct, &quant.UV, &coeffs.QCoeff[16+block], eob)
		coeffs.SetBlockEOB(16+block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob = quantizeBlockWithZbin(&dct, &quant.UV, qIndex, zbinModeBoost, &coeffs.QCoeff[20+block], &dq)
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = optimizeQuantizedBlock(qIndex, 2, ctx, 0, intra, &dct, &quant.UV, &coeffs.QCoeff[20+block], eob)
		coeffs.SetBlockEOB(20+block, eob)
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
}

func macroblockCoefficientsEmpty(coeffs *vp8enc.MacroblockCoefficients, is4x4 bool) bool {
	if coeffs.EOB[24] != 0 {
		return false
	}
	for i := 0; i < 16; i++ {
		if (is4x4 && coeffs.EOB[i] != 0) || (!is4x4 && coeffs.EOB[i] > 1) {
			return false
		}
	}
	for i := 16; i < 24; i++ {
		if coeffs.EOB[i] != 0 {
			return false
		}
	}
	return true
}

func clearMacroblockCoefficients(coeffs *vp8enc.MacroblockCoefficients) {
	*coeffs = vp8enc.MacroblockCoefficients{}
}

func shouldSkipInterResidual(qIndex int, tokenRate int, predictionDist int, codedDist int) bool {
	if tokenRate < 0 || predictionDist < 0 || codedDist < 0 {
		return false
	}
	skipCost := rdModeScore(qIndex, 0, predictionDist)
	codedCost := rdModeScore(qIndex, tokenRate, codedDist)
	return skipCost <= codedCost
}

func staticInterEncodeBreakout(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) bool {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := (yAC * yAC) >> 4
	if threshold < encodeBreakout {
		threshold = encodeBreakout
	}
	lumaVar, lumaSSE := macroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
	if lumaSSE >= threshold {
		return false
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false
	}
	return macroblockChromaSSE(src, pred, mbRow, mbCol)*2 < threshold
}

const (
	lastFrameZeroMVZbinBoost  = 6
	goldenAltZeroMVZbinBoost  = 12
	nonZeroInterModeZbinBoost = 4
	splitInterModeZbinBoost   = 0
	intraInterFrameZbinBoost  = 0
)

func interZbinModeBoost(mode *vp8enc.InterFrameMacroblockMode) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return intraInterFrameZbinBoost
	}
	switch mode.Mode {
	case vp8common.ZeroMV:
		if mode.RefFrame == vp8common.LastFrame {
			return lastFrameZeroMVZbinBoost
		}
		return goldenAltZeroMVZbinBoost
	case vp8common.SplitMV:
		return splitInterModeZbinBoost
	default:
		return nonZeroInterModeZbinBoost
	}
}

func quantizeBlockWithZbin(coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if coeff == nil || quant == nil || qcoeff == nil || dqcoeff == nil {
		return 0
	}
	eob := -1
	zeroRun := 0
	for pos := 0; pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x := z
		if x < 0 {
			x = -x
		}
		zbin := int(quant.Zbin[rc])
		zbin += int(quant.ZbinBoost[zeroRun])
		zbin += (int(quant.Dequant[1]) * zbinModeBoost) >> 7
		if x < zbin {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x += int(quant.Round[rc])
		y := ((((x * int(quant.Quant[rc])) >> 16) + x) * int(quant.QuantShift[rc])) >> 16
		if z < 0 {
			y = -y
		}
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = pos
			zeroRun = 0
		} else if zeroRun < len(quant.ZbinBoost)-1 {
			zeroRun++
		}
	}
	return eob + 1
}

func quantizeOptimizedBlock(qIndex int, blockType int, ctx int, skipDC int, zbinModeBoost int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := quantizeBlockWithZbin(coeff, quant, qIndex, zbinModeBoost, qcoeff, dqcoeff)
	eob = optimizeQuantizedBlock(qIndex, blockType, ctx, skipDC, intra, coeff, quant, qcoeff, eob)
	dequantizeQuantizedBlock(quant, qcoeff, dqcoeff)
	return eob
}

func dequantizeQuantizedBlock(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) {
	if quant == nil || qcoeff == nil || dqcoeff == nil {
		return
	}
	for i := 0; i < 16; i++ {
		dqcoeff[i] = qcoeff[i] * quant.Dequant[i]
	}
}

func optimizeQuantizedBlock(qIndex int, blockType int, ctx int, skipDC int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, eob int) int {
	if coeff == nil || quant == nil || qcoeff == nil || eob <= skipDC {
		return eob
	}
	if blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return eob
	}

	probs := vp8tables.DefaultCoefProbs
	bestRate := coefficientBlockTokenRate(&probs, blockType, ctx, skipDC, qcoeff, eob)
	bestError := quantizedBlockError(coeff, quant, qcoeff, skipDC)
	bestCost := rdBlockScore(qIndex, blockPlaneRDMultiplier(blockType), intra, bestRate, bestError)
	for pos := eob - 1; pos >= skipDC; pos-- {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		if qcoeff[rc] == 0 {
			continue
		}
		candidate := *qcoeff
		if candidate[rc] > 0 {
			candidate[rc]--
		} else {
			candidate[rc]++
		}
		candidateEOB := vp8enc.BlockCoeffEOB(&candidate, skipDC)
		candidateRate := coefficientBlockTokenRate(&probs, blockType, ctx, skipDC, &candidate, candidateEOB)
		candidateError := quantizedBlockError(coeff, quant, &candidate, skipDC)
		candidateCost := rdBlockScore(qIndex, blockPlaneRDMultiplier(blockType), intra, candidateRate, candidateError)
		if candidateCost <= bestCost {
			*qcoeff = candidate
			eob = candidateEOB
			bestCost = candidateCost
		}
	}
	return eob
}

func quantizedBlockError(coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, skipDC int) int {
	err := 0
	for pos := skipDC; pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		diff := int(coeff[rc]) - int(qcoeff[rc])*int(quant.Dequant[rc])
		err += diff * diff
	}
	return err
}

func rdBlockScore(qIndex int, planeMultiplier int, intra bool, rate int, distortion int) int {
	if planeMultiplier <= 0 {
		planeMultiplier = 1
	}
	rdMult, rdDiv := libvpxRDConstants(qIndex)
	rdMult *= planeMultiplier
	if intra {
		rdMult = (rdMult * 9) >> 4
	}
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func blockPlaneRDMultiplier(blockType int) int {
	switch blockType {
	case 1:
		return 16
	case 2:
		return 2
	default:
		return 4
	}
}

func macroblockCoefficientTokenRate(probs *vp8tables.CoefficientProbs, is4x4 bool, coeffs *vp8enc.MacroblockCoefficients) int {
	if probs == nil || coeffs == nil {
		return maxInt() / 4
	}

	rate := 0
	blockType := 0
	skipDC := 0
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if !is4x4 {
		eob := int(coeffs.EOB[24])
		rate += coefficientBlockTokenRate(probs, 1, 0, 0, &coeffs.QCoeff[24], eob)
		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for block := 0; block < 16; block++ {
		eob := int(coeffs.EOB[block])
		if eob < skipDC {
			eob = skipDC
		}
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		rate += coefficientBlockTokenRate(probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := int(coeffs.EOB[block])
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		rate += coefficientBlockTokenRate(probs, 2, ctx, 0, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate
}

func macroblockCoefficientUVContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if block&3 > 1 {
		l++
	}
	return a, l
}

func macroblockImageSSE(src vp8enc.SourceImage, img *vp8common.Image, mbRow int, mbCol int) int {
	return macroblockLumaSSE(src, img, mbRow, mbCol, vp8enc.MotionVector{}) +
		macroblockChromaSSE(src, img, mbRow, mbCol)
}

func predictAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	yOK := false
	if mode.Is4x4 || mode.Mode == vp8common.BPred {
		yOK = vp8dec.PredictIntraY4x4(&mode.BModes, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft)
	} else {
		yOK = vp8dec.PredictIntraY16x16(mode.Mode, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable)
	}
	return yOK &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
}

func predictAnalysisChroma(img *vp8common.Image, row int, col int, uvMode vp8common.MBPredictionMode, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.PredictIntraUV8x8(uvMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(uvMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
}

func predictAnalysisBPredBlock(mode vp8common.BPredictionMode, dst []byte, stride int, macroblock []byte, macroblockStride int, above []byte, left []byte, topLeft byte, block int) bool {
	blockRow := block >> 2
	blockCol := block & 3
	y := blockRow * 4
	x := blockCol * 4
	var blockAbove [8]byte
	var blockLeft [4]byte

	if blockRow == 0 {
		copy(blockAbove[:], above[x:x+8])
	} else {
		aboveOff := (y-1)*macroblockStride + x
		copy(blockAbove[:4], macroblock[aboveOff:aboveOff+4])
		if blockCol < 3 {
			copy(blockAbove[4:], macroblock[aboveOff+4:aboveOff+8])
		} else {
			copy(blockAbove[4:], above[16:20])
		}
	}

	if blockCol == 0 {
		copy(blockLeft[:], left[y:y+4])
	} else {
		for i := 0; i < 4; i++ {
			blockLeft[i] = macroblock[(y+i)*macroblockStride+x-1]
		}
	}

	blockTopLeft := topLeft
	switch {
	case blockRow == 0 && blockCol == 0:
	case blockRow == 0:
		blockTopLeft = above[x-1]
	case blockCol == 0:
		blockTopLeft = left[y-1]
	default:
		blockTopLeft = macroblock[(y-1)*macroblockStride+x-1]
	}

	return dsp.Intra4x4Predict(dst, stride, mode, blockAbove[:], blockLeft[:], blockTopLeft)
}

func copyBPredBlock(src []byte, srcStride int, dst []byte, dstStride int, block int) {
	y := (block >> 2) * 4
	x := (block & 3) * 4
	for row := 0; row < 4; row++ {
		copy(dst[(y+row)*dstStride+x:], src[row*srcStride:row*srcStride+4])
	}
}

func bPredBlockSSE(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	sse := 0
	for row := 0; row < 4; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := 0; col < 4; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col])
			sse += diff * diff
		}
	}
	return sse
}

func fillBPredResidual4x4(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	for row := 0; row < 4; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := 0; col < 4; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col]))
		}
	}
}

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	err := 0
	for i := 0; i < 16; i++ {
		diff := int(coeff[i]) - int(dqcoeff[i])
		err += diff * diff
	}
	return err
}

func buildReconstructingBPredMacroblockCoefficients(src vp8enc.SourceImage, mbRow int, mbCol int, img *vp8common.Image, mode *vp8dec.MacroblockMode, quant *vp8enc.MacroblockQuant, qIndex int, coeffs *vp8enc.MacroblockCoefficients, scratch *vp8dec.IntraReconstructionScratch) bool {
	if img == nil || mode == nil || quant == nil || coeffs == nil || scratch == nil || !mode.Is4x4 || mode.Mode != vp8common.BPred {
		return false
	}

	refs := vp8dec.BuildIntraPredictorRefs(img, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*img.YStride + mbCol*16
	uOff := mbRow*8*img.UStride + mbCol*8
	vOff := mbRow*8*img.VStride + mbCol*8
	y := img.Y[yOff:]
	u := img.U[uOff:]
	v := img.V[vOff:]

	var input [16]int16
	var dct [16]int16
	var dq [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	for block := 0; block < 16; block++ {
		blockOffset := analysisYBlockOffset(block, img.YStride)
		if !predictAnalysisBPredBlock(mode.BModes[block], y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			return false
		}
		x := mbCol*16 + (block&3)*4
		yCoord := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		eob := quantizeOptimizedBlock(qIndex, 3, ctx, 0, 0, mode.RefFrame == vp8common.IntraFrame, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
		coeffs.SetBlockEOB(block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
	}
	coeffs.QCoeff[24] = [16]int16{}
	coeffs.SetBlockEOB(24, 0)

	if !vp8dec.PredictIntraUV8x8(mode.UVMode, u, img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !vp8dec.PredictIntraUV8x8(mode.UVMode, v, img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		yCoord := mbRow*8 + (block>>1)*4

		fillPredictedResidual4x4(src.U, src.UStride, uvWidth, uvHeight, img.U, img.UStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeOptimizedBlock(qIndex, 2, ctx, 0, 0, mode.RefFrame == vp8common.IntraFrame, &dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, u[analysisUVBlockOffset(block, img.UStride):], img.UStride)

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, img.V, img.VStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = quantizeOptimizedBlock(qIndex, 2, ctx, 0, 0, mode.RefFrame == vp8common.IntraFrame, &dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, v[analysisUVBlockOffset(block, img.VStride):], img.VStride)
	}
	return true
}

func addQuantizedBlockResidual(eob int, dq *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(dq[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(dq, dst, stride, dst, stride)
}

func analysisYBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func analysisUVBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}

func reconstructInterAnalysisMacroblock(img *vp8common.Image, last *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	if mode.Mode == vp8common.SplitMV {
		return vp8dec.ReconstructSplitMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
	}
	return vp8dec.ReconstructWholeMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
}

func reconstructAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.ReconstructIntraMacroblock(mode, tokens, dequant, refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual)
}

func fillPredictedResidual4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out *[16]int16) {
	for row := 0; row < 4; row++ {
		sampleY := clampEncodeCoord(y+row, height)
		for col := 0; col < 4; col++ {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}

func clampEncodeCoord(v int, limit int) int {
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}
