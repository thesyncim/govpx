package libgopx

import (
	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	"github.com/thesyncim/libgopx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
)

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) error {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return ErrInvalidConfig
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return ErrInvalidConfig
	}

	var dequant vp8common.MacroblockDequant
	var quant vp8enc.MacroblockQuant
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &e.dequantTables)
	vp8common.InitMacroblockDequant(&e.dequantTables, qIndex, &dequant)
	vp8enc.InitFastMacroblockQuant(&dequant, &quant)
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: uint8(qIndex)}, nil, &e.dequantTables, &e.dequants)

	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			modes[index] = vp8enc.KeyFrameMacroblockMode{YMode: vp8common.DCPred, UVMode: vp8common.DCPred}
			coeffs[index] = vp8enc.MacroblockCoefficients{}
			convertKeyFrameMode(&modes[index], &e.reconstructModes[index])
			if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
			buildDCPredMacroblockCoefficients(src, row, col, &e.analysis.Img, &quant, &coeffs[index])
			convertMacroblockCoefficients(&coeffs[index], false, &e.reconstructTokens[index])
			if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[0], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
		}
	}
	e.analysis.ExtendBorders()
	return nil
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) error {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return ErrInvalidConfig
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return ErrInvalidConfig
	}

	var dequant vp8common.MacroblockDequant
	var quant vp8enc.MacroblockQuant
	vp8common.BuildFrameDequantTables(vp8common.QuantDeltas{}, &e.dequantTables)
	vp8common.InitMacroblockDequant(&e.dequantTables, qIndex, &dequant)
	vp8enc.InitFastMacroblockQuant(&dequant, &quant)
	vp8dec.InitSegmentDequants(vp8dec.QuantHeader{BaseQIndex: uint8(qIndex)}, nil, &e.dequantTables, &e.dequants)

	var refs [3]interAnalysisReference
	refCount := e.interAnalysisReferences(flags, &refs)
	if refCount == 0 {
		return ErrInvalidConfig
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			index := row*cols + col
			coeffs[index] = vp8enc.MacroblockCoefficients{}
			ref, mv := selectInterFrameReferenceMotionVector(src, refs[:], refCount, row, col)
			interCost := interMotionSearchCost(src, ref.Img, row, col, mv)
			intraCost, ok := predictInterFrameIntraDCPredCost(src, row, col, &e.analysis.Img, &e.reconstructScratch)
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

			if intraCost < interCost {
				modes[index] = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
			} else {
				modes[index] = vp8enc.InterFrameMotionModeForVector(ref.Frame, mv, above, left, aboveLeft)
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				predMode := e.reconstructModes[index]
				predMode.MBSkipCoeff = true
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[0], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			}
			buildDCPredMacroblockCoefficients(src, row, col, &e.analysis.Img, &quant, &coeffs[index])
			modes[index].MBSkipCoeff = macroblockCoefficientsEmpty(&coeffs[index])
			convertInterFrameMode(&modes[index], &e.reconstructModes[index])
			convertMacroblockCoefficients(&coeffs[index], modes[index].Mode == vp8common.BPred, &e.reconstructTokens[index])
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[0], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[0], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
		}
	}
	e.analysis.ExtendBorders()
	return nil
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

var interFrameMVCandidates = [...]vp8enc.MotionVector{
	{},
	{Col: -8},
	{Row: -8},
	{Row: 8},
	{Col: 8},
	// First full-pixel hex ring from libvpx v1.16.0 vp8/encoder/mcomp.c.
	{Row: -8, Col: -16},
	{Row: 8, Col: -16},
	{Row: 16},
	{Row: 8, Col: 16},
	{Row: -8, Col: 16},
	{Row: -16},
}

var interFrameMVRefineDeltas = [...]vp8enc.MotionVector{
	{Col: -8},
	{Row: -8},
	{Row: 8},
	{Col: 8},
}

const interFrameMVSearchRange = 4 * 8

func selectInterFrameReferenceMotionVector(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int) (interAnalysisReference, vp8enc.MotionVector) {
	bestRef := refs[0]
	best, bestCost := selectInterFrameMotionVector(src, bestRef.Img, mbRow, mbCol)
	for refIndex := 1; refIndex < refCount; refIndex++ {
		ref := refs[refIndex]
		mv, cost := selectInterFrameMotionVector(src, ref.Img, mbRow, mbCol)
		if cost < bestCost {
			bestRef = ref
			best = mv
			bestCost = cost
		}
	}
	return bestRef, best
}

func predictInterFrameIntraDCPredCost(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (int, bool) {
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
		return 0, false
	}
	return macroblockSAD(src, pred, mbRow, mbCol, vp8enc.MotionVector{}), true
}

func selectInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (vp8enc.MotionVector, int) {
	best := vp8enc.MotionVector{}
	bestCost := interMotionSearchCost(src, ref, mbRow, mbCol, best)
	for i := 1; i < len(interFrameMVCandidates); i++ {
		mv := interFrameMVCandidates[i]
		cost := interMotionSearchCost(src, ref, mbRow, mbCol, mv)
		if cost < bestCost {
			best = mv
			bestCost = cost
		}
	}
	return refineInterFrameMotionVector(src, ref, mbRow, mbCol, best, bestCost)
}

func refineInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestCost int) (vp8enc.MotionVector, int) {
	for {
		improved := false
		for i := 0; i < len(interFrameMVRefineDeltas); i++ {
			mv := addInterMotionVector(best, interFrameMVRefineDeltas[i])
			if !interMotionVectorInSearchRange(mv) {
				continue
			}
			cost := interMotionSearchCost(src, ref, mbRow, mbCol, mv)
			if cost < bestCost {
				best = mv
				bestCost = cost
				improved = true
			}
		}
		if !improved {
			return best, bestCost
		}
	}
}

func addInterMotionVector(a vp8enc.MotionVector, b vp8enc.MotionVector) vp8enc.MotionVector {
	return vp8enc.MotionVector{Row: a.Row + b.Row, Col: a.Col + b.Col}
}

func interMotionVectorInSearchRange(mv vp8enc.MotionVector) bool {
	return absInterMotionVectorComponent(mv.Row) <= interFrameMVSearchRange &&
		absInterMotionVectorComponent(mv.Col) <= interFrameMVSearchRange
}

func absInterMotionVectorComponent(v int16) int {
	n := int(v)
	if n < 0 {
		return -n
	}
	return n
}

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionVectorCost(mv)
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
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SAD16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride)
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
	}
	return sad
}

func buildDCPredMacroblockCoefficients(src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, quant *vp8enc.MacroblockQuant, coeffs *vp8enc.MacroblockCoefficients) {
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16

	for block := 0; block < 16; block++ {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		var input [16]int16
		var dct [16]int16
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		y2Input[block] = dct[0]
		dct[0] = 0
		vp8enc.FastQuantizeBlock(&dct, &quant.Y1DC, &coeffs.QCoeff[block], &dq)
	}
	vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	vp8enc.FastQuantizeBlock(&y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq)

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		y := mbRow*8 + (block>>1)*4
		var input [16]int16
		var dct [16]int16
		fillPredictedResidual4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		vp8enc.FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		vp8enc.FastQuantizeBlock(&dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
	}
}

func macroblockCoefficientsEmpty(coeffs *vp8enc.MacroblockCoefficients) bool {
	if vp8enc.BlockCoeffEOB(&coeffs.QCoeff[24], 0) > 0 {
		return false
	}
	for i := 0; i < 16; i++ {
		if vp8enc.BlockCoeffEOB(&coeffs.QCoeff[i], 1) > 1 {
			return false
		}
	}
	for i := 16; i < 24; i++ {
		if vp8enc.BlockCoeffEOB(&coeffs.QCoeff[i], 0) > 0 {
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
	return vp8dec.PredictIntraY16x16(mode.Mode, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
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
