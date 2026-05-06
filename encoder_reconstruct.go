package libgopx

import (
	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/libgopx/internal/vp8/tables"
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
				if !buildReconstructingBPredMacroblockCoefficients(src, row, col, &e.analysis.Img, &e.reconstructModes[index], &quants[segmentID], &coeffs[index], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
				convertMacroblockCoefficients(&coeffs[index], true, &e.reconstructTokens[index])
				continue
			}
			if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
			buildPredictedMacroblockCoefficients(src, row, col, &e.analysis.Img, &quants[segmentID], modes[index].YMode == vp8common.BPred, &coeffs[index])
			convertMacroblockCoefficients(&coeffs[index], modes[index].YMode == vp8common.BPred, &e.reconstructTokens[index])
			if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
		}
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
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			ref, mv := selectInterFrameReferenceMotionVector(src, refs[:], refCount, row, col)
			interCost := interMotionSearchCost(src, ref.Img, row, col, mv)
			interScore := interMotionRDScore(src, ref.Img, row, col, mv, segmentQIndex)
			useIntra := false
			intraMode := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
			if interCost > 0 {
				var intraCost int
				var ok bool
				intraMode, intraCost, ok = predictBestInterIntraModeCost(src, segmentQIndex, row, col, &quants[segmentID], &e.analysis.Img, &e.reconstructScratch)
				if !ok {
					return ErrInvalidConfig
				}
				useIntra = intraCost < interScore
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

			if useIntra {
				modes[index] = intraMode
				modes[index].SegmentID = segmentID
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				if modes[index].Mode == vp8common.BPred {
					if !buildReconstructingBPredMacroblockCoefficients(src, row, col, &e.analysis.Img, &e.reconstructModes[index], &quants[segmentID], &coeffs[index], &e.reconstructScratch) {
						return ErrInvalidConfig
					}
				} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else {
				modes[index] = vp8enc.InterFrameMotionModeForVectorAt(ref.Frame, mv, above, left, aboveLeft, row, col, rows, cols)
				modes[index].SegmentID = segmentID
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				predMode := e.reconstructModes[index]
				predMode.MBSkipCoeff = true
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			}
			if modes[index].RefFrame != vp8common.IntraFrame || modes[index].Mode != vp8common.BPred {
				buildPredictedMacroblockCoefficients(src, row, col, &e.analysis.Img, &quants[segmentID], modes[index].Mode == vp8common.BPred, &coeffs[index])
			}
			modes[index].MBSkipCoeff = macroblockCoefficientsEmpty(&coeffs[index], modes[index].Mode == vp8common.BPred)
			convertInterFrameMode(&modes[index], &e.reconstructModes[index])
			convertMacroblockCoefficients(&coeffs[index], modes[index].Mode == vp8common.BPred, &e.reconstructTokens[index])
			if modes[index].RefFrame == vp8common.IntraFrame && modes[index].Mode == vp8common.BPred {
				continue
			}
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
		}
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

const interFrameMVSearchRange = 4 * 8
const interFrameMVFullPixelStep = 8
const interFrameMVSubpixelStep = 2

func interFrameFullPixelSearchCandidateCount() int {
	axis := (2*interFrameMVSearchRange)/interFrameMVFullPixelStep + 1
	return axis * axis
}

func interFrameSubpixelSearchCandidateCount() int {
	axis := (2*interFrameMVSearchRange)/interFrameMVSubpixelStep + 1
	return axis * axis
}

func selectInterFrameReferenceMotionVector(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int) (interAnalysisReference, vp8enc.MotionVector) {
	bestRef := refs[0]
	best, bestCost := selectInterFrameMotionVector(src, bestRef.Img, mbRow, mbCol)
	if bestCost == 0 {
		return bestRef, best
	}
	for refIndex := 1; refIndex < refCount; refIndex++ {
		ref := refs[refIndex]
		mv, cost := selectInterFrameMotionVector(src, ref.Img, mbRow, mbCol)
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
	bPredTotal := bPredCost + bPredUVCost + rdRateOnly(qIndex, bPredRate)
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
			eob := vp8enc.FastQuantizeBlock(&dct, &quant.Y1, &qcoeff, &dqcoeff)
			tokenCtx := int(tokenAbove[block&3] + tokenLeft[(block&0x0c)>>2])
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
	if qIndex < vp8common.MinQ {
		qIndex = vp8common.MinQ
	}
	if qIndex > 160 {
		qIndex = 160
	}
	rdMult := (14 * qIndex * qIndex) / 5
	rdDiv := 100
	if rdMult > 1000 {
		rdMult /= 100
		rdDiv = 1
	}
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

func rdRateOnly(qIndex int, rate int) int {
	return rdModeScore(qIndex, rate, 0)
}

func interMotionRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, qIndex int) int {
	return rdModeScore(qIndex, interMotionVectorCost(mv), macroblockLumaSSE(src, ref, mbRow, mbCol, mv))
}

func selectInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (vp8enc.MotionVector, int) {
	best := vp8enc.MotionVector{}
	bestCost := interMotionSearchCost(src, ref, mbRow, mbCol, best)
	if bestCost == 0 {
		return best, bestCost
	}
	best, bestCost = exhaustiveInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestCost)
	return exhaustiveInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestCost)
}

func exhaustiveInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestCost int) (vp8enc.MotionVector, int) {
	for row := -interFrameMVSearchRange; row <= interFrameMVSearchRange; row += interFrameMVFullPixelStep {
		for col := -interFrameMVSearchRange; col <= interFrameMVSearchRange; col += interFrameMVFullPixelStep {
			mv := vp8enc.MotionVector{Row: int16(row), Col: int16(col)}
			if mv == best {
				continue
			}
			cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestCost)
			if cost < bestCost {
				best = mv
				bestCost = cost
			}
		}
	}
	return best, bestCost
}

func exhaustiveInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestCost int) (vp8enc.MotionVector, int) {
	for row := -interFrameMVSearchRange; row <= interFrameMVSearchRange; row += interFrameMVSubpixelStep {
		for col := -interFrameMVSearchRange; col <= interFrameMVSearchRange; col += interFrameMVSubpixelStep {
			mv := vp8enc.MotionVector{Row: int16(row), Col: int16(col)}
			if mv == best {
				continue
			}
			cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestCost)
			if cost < bestCost {
				best = mv
				bestCost = cost
			}
		}
	}
	return best, bestCost
}

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionVectorCost(mv)
}

func interMotionSearchCostLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int) int {
	mvCost := interMotionVectorCost(mv)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, sadLimit) + mvCost
}

func interMotionVectorCost(mv vp8enc.MotionVector) int {
	row := int(mv.Row)
	if row < 0 {
		row = -row
	}
	col := int(mv.Col)
	if col < 0 {
		col = -col
	}
	return (row + col) >> 3
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

func buildPredictedMacroblockCoefficients(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, quant *vp8enc.MacroblockQuant, is4x4 bool, coeffs *vp8enc.MacroblockCoefficients) {
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16
	var input [16]int16
	var dct [16]int16

	for block := 0; block < 16; block++ {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		if is4x4 {
			coeffs.SetBlockEOB(block, vp8enc.FastQuantizeBlock(&dct, &quant.Y1, &coeffs.QCoeff[block], &dq))
		} else {
			y2Input[block] = dct[0]
			dct[0] = 0
			coeffs.SetBlockEOB(block, vp8enc.FastQuantizeBlock(&dct, &quant.Y1DC, &coeffs.QCoeff[block], &dq))
		}
	}
	if !is4x4 {
		vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
		coeffs.SetBlockEOB(24, vp8enc.FastQuantizeBlock(&y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq))
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
		coeffs.SetBlockEOB(16+block, vp8enc.FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[16+block], &dq))

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		coeffs.SetBlockEOB(20+block, vp8enc.FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[20+block], &dq))
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

func buildReconstructingBPredMacroblockCoefficients(src vp8enc.SourceImage, mbRow int, mbCol int, img *vp8common.Image, mode *vp8dec.MacroblockMode, quant *vp8enc.MacroblockQuant, coeffs *vp8enc.MacroblockCoefficients, scratch *vp8dec.IntraReconstructionScratch) bool {
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
	for block := 0; block < 16; block++ {
		blockOffset := analysisYBlockOffset(block, img.YStride)
		if !predictAnalysisBPredBlock(mode.BModes[block], y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			return false
		}
		x := mbCol*16 + (block&3)*4
		yCoord := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := vp8enc.FastQuantizeBlock(&dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
		coeffs.SetBlockEOB(block, eob)
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
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		yCoord := mbRow*8 + (block>>1)*4

		fillPredictedResidual4x4(src.U, src.UStride, uvWidth, uvHeight, img.U, img.UStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := vp8enc.FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		addQuantizedBlockResidual(eob, &dq, u[analysisUVBlockOffset(block, img.UStride):], img.UStride)

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, img.V, img.VStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob = vp8enc.FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
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
