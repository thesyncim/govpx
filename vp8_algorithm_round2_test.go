// Frozen b57aac772af29e85f4b5b34ae33600cc84cd6a0f entrypoints are exact-state
// oracles for the algorithmic work-elision paths. Benchmarks call the live
// production entrypoints with identical fixtures on both revisions.
package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"math/bits"
	"reflect"
	"testing"
)

func (e *VP8Encoder) frozen2ssimActivityMeasure(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, zbinOverQuant int, actZbinAdj int, quant *vp8enc.MacroblockQuant, fastQuant bool, optimize bool, rdMult int, rdDiv int) uint32 {
	useDC16 := (mbCol != 0 || mbRow != 0) && (mbCol == 0 || mbRow == 0)
	img := &e.analysis.Img
	refs := vp8dec.BuildIntraPredictorRefs(img, mbRow, mbCol, &e.reconstructScratch.Refs)
	yOff := mbRow*16*img.YStride + mbCol*16
	y := img.Y[yOff:]
	sse := 0
	if useDC16 {
		// vp8_encode_intra16x16mby: build 16x16 DC predictor into the recon
		// buffer, then accumulate SSE of (src - predictor). The quantize +
		// IDCT path that follows in libvpx leaves the recon ≈ predictor +
		// quantized-residue. We replicate that with the existing first-pass
		// helper, which already implements the same predictAnalysis +
		// build/quantize + reconstructAnalysis pipeline.
		mode := vp8dec.MacroblockMode{
			RefFrame: vp8common.IntraFrame,
			Mode:     vp8common.DCPred,
			UVMode:   vp8common.DCPred,
		}
		if !vp8dec.PredictIntraY16x16(mode.Mode, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) {
			return vp8ActivityAvgMin
		}
		sse = macroblockLumaSSE(src, img, mbRow, mbCol, vp8enc.MotionVector{})
		var coeffs vp8enc.MacroblockCoefficients
		buildPredictedMacroblockCoefficients(predictedMacroblockCoefficientArgs{
			coefProbs:     &e.coefProbs,
			src:           src,
			mbRow:         mbRow,
			mbCol:         mbCol,
			pred:          img,
			quant:         quant,
			qIndex:        qIndex,
			zbinOverQuant: zbinOverQuant,
			actZbinAdj:    actZbinAdj,
			rdMult:        rdMult,
			rdDiv:         rdDiv,
			intra:         true,
			fastQuant:     fastQuant,
			optimize:      optimize,
			coeffs:        &coeffs,
		})
		var tokens vp8dec.MacroblockTokens
		vp8enc.ConvertMacroblockCoefficients(&coeffs, false, &tokens)
		var dequantTables vp8common.FrameDequantTables
		var dequant vp8common.MacroblockDequant
		quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
		vp8common.BuildFrameDequantTables(quantDeltas, &dequantTables)
		vp8common.InitMacroblockDequant(&dequantTables, qIndex, &dequant)
		_ = reconstructAnalysisMacroblock(img, mbRow, mbCol, &mode, &tokens, &dequant, &e.reconstructScratch)
	} else {
		// vp8_encode_intra4x4block, run 16 times: predict each 4x4 sub-block
		// from the as-of-this-sub-block recon (which is built up inside the
		// MB itself), accumulate SSE, then quantize+IDCT the residue back so
		// the next sub-block sees the reconstruction.
		var coeffs vp8enc.MacroblockCoefficients
		var input [16]int16
		var dct [16]int16
		var dq [16]int16
		var yAbove [4]uint8
		var yLeft [4]uint8
		for block := range 16 {
			blockOffset := vp8enc.AnalysisYBlockOffset(block, img.YStride)
			if !predictAnalysisBPredBlock(vp8common.BDCPred, y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return vp8ActivityAvgMin
			}
			sse += vp8enc.BPredBlockSSE(src, mbRow, mbCol, block, y[blockOffset:], img.YStride)
			x := mbCol*16 + (block&3)*4
			yCoord := mbRow*16 + (block>>2)*4
			vp8enc.FillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob := vp8enc.QuantizeEncodedBlockWithRDZbinAndActivity(&e.coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, 0, actZbinAdj, zbinOverQuant, rdMult, rdDiv, true, fastQuant, false, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
			coeffs.SetBlockEOB(block, eob)
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
			vp8enc.AddQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
		}
	}
	if sse < vp8ActivityAvgMin {
		sse = vp8ActivityAvgMin
	}
	return uint32(sse)
}
func (e *VP8Encoder) frozen2estimateFastBPredIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, bestSSE int, quant *vp8enc.MacroblockQuant, ctx *fastInterModeLoopContext) (vp8enc.InterFrameMacroblockMode, int, int, int, int, bool) {
	if quant == nil {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
	}
	// e is always non-nil on the inter picker entry path; the prior nil
	// guard was dead code.
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := 0
	if e.activityMapValid {
		if adjustment, ok := e.tunedZbinAdjustment(mbRow, mbCol); ok {
			actZbinAdj = adjustment
		}
	}
	fastQuant := e.libvpxUseFastQuantForPick()
	analysisImg := &e.analysis.Img
	refs := ctx.lumaIntraRefs(e, mbRow, mbCol)
	yStride := analysisImg.YStride
	yOff := mbRow*16*yStride + mbCol*16
	y := analysisImg.Y[yOff:]
	// Hoist refs slices once: predictAnalysisBPredBlock reads YAbove/YLeft/
	// YTopLeft on every sub-block iteration, but they are derived from the
	// MB's neighbor stripes and never mutated across the 16-block walk.
	refsYAbove := refs.YAbove
	refsYLeft := refs.YLeft
	refsYTopLeft := refs.YTopLeft
	// Hoist RD constants once: rdModeScoreWithZbin recomputes (rdMult, rdDiv)
	// from qIndex/zbinOverQuant, both invariant across the 64-iteration
	// {16 blocks} x {4 modes} inner cost loop. Shared with the rest of the
	// picker invocation via the loop-context cache (same derivation).
	rdMult, rdDiv := ctx.rdConstants(e, qIndex, zbinOverQuant, mbRow, mbCol)
	quantY1 := &quant.Y1
	var modes [16]vp8common.BPredictionMode
	var predictor [256]byte
	rate := e.interIntraReferenceRate() + e.interIntraYModeRate(vp8common.BPred)
	distortion := 0
	for block := range 16 {
		bestMode := vp8common.BModeCount
		bestRate := 0
		bestDist := 0
		bestCost := maxInt()
		var bestPred [16]byte
		for _, bMode := range fastBPredIntraModeCandidates {
			var blockPred [16]byte
			if !predictAnalysisBPredBlock(bMode, blockPred[:], 4, y, yStride, refsYAbove, refsYLeft, refsYTopLeft, block) {
				return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
			}
			modeRate := libvpxInterFastBpredModeCostWithProbs(bMode, e.modeProbs.BMode[:])
			modeDist := vp8enc.BPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			modeCost := vp8enc.RDCost(rdMult, rdDiv, modeRate, modeDist)
			if modeCost < bestCost {
				bestMode = bMode
				bestRate = modeRate
				bestDist = modeDist
				bestCost = modeCost
				bestPred = blockPred
			}
		}
		modes[block] = bestMode
		vp8enc.CopyBPredBlock(bestPred[:], predictor[:], 16, block)

		// Mirror libvpx vp8_encode_intra4x4block: re-predict, residual,
		// DCT, quantize/dequant, IDCT-add into the analysis Y plane so the
		// next sub-block's predictor neighbors come from reconstructed
		// pixels (not raw predictor). pick_intra4x4block calls this at
		// the end of each block iteration (encodeintra.c:45).
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		vp8enc.FillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, quantY1, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
		var recon [16]byte
		if eob > 1 {
			dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		} else {
			dsp.DCOnlyIDCT4x4Add(dqcoeff[0], bestPred[:], 4, recon[:], 4)
		}
		vp8enc.CopyBPredBlock(recon[:], y, yStride, block)

		rate += bestRate
		distortion += bestDist
		if distortion > bestSSE {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, 0, 0, false
		}
	}
	variance, sse := macroblockLumaVarianceSSEFromPredictor(src, mbRow, mbCol, &predictor)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: vp8common.DCPred, BModes: modes}, vp8enc.RDCost(rdMult, rdDiv, rate, variance), variance, sse, rate, true
}
func frozen2pickFastBPredLumaModeKFWithRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool, rdMult int, rdDiv int) ([16]vp8common.BPredictionMode, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	totalRate := 0
	totalDist := 0
	for block := range 16 {
		bestMode := vp8common.BDCPred
		bestRate := 0
		bestDist := 0
		bestCost := 0
		var bestPred [16]byte
		aboveMode := bPredAnalysisAboveMode(true, above, modes, block)
		leftMode := bPredAnalysisLeftMode(true, left, modes, block)
		for i, candidate := range fastBPredIntraModeCandidates {
			var blockPred [16]byte
			if !predictAnalysisBPredBlock(candidate, blockPred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, false
			}
			modeRate := bPredModeRate(true, candidate, aboveMode, leftMode)
			modeDist := vp8enc.BPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			cost := vp8enc.RDCost(rdMult, rdDiv, modeRate, modeDist)
			if i == 0 || cost < bestCost {
				bestMode = candidate
				bestRate = modeRate
				bestDist = modeDist
				bestCost = cost
				bestPred = blockPred
			}
		}
		modes[block] = bestMode

		// Mirror libvpx vp8_encode_intra4x4block: re-predict, residual,
		// DCT, quantize/dequant, IDCT-add into the analysis Y plane so the
		// next block's predictor neighbors come from reconstructed pixels.
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		vp8enc.FillBPredResidual4x4(src, mbRow, mbCol, block, bestPred[:], &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		eob := vp8enc.QuantizeDecisionBlock(fastQuant, &dct, &quant.Y1, zbinOverQuant, &qcoeff, &dqcoeff)
		var recon [16]byte
		if eob > 1 {
			dsp.IDCT4x4Add(&dqcoeff, bestPred[:], 4, recon[:], 4)
		} else {
			dsp.DCOnlyIDCT4x4Add(dqcoeff[0], bestPred[:], 4, recon[:], 4)
		}
		vp8enc.CopyBPredBlock(recon[:], y, pred.YStride, block)

		totalRate += bestRate
		totalDist += bestDist
	}
	mbModeRate := intraYModeRate(true, vp8common.BPred)
	rd := vp8enc.RDCost(rdMult, rdDiv, mbModeRate+totalRate, totalDist)
	return modes, totalRate, rd, true
}
func frozen2predictBestBPredLumaModeRDWithRDConstantsAndEOBs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, bestRD int, coefProbs *vp8tables.CoefficientProbs, interBModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int, coefTokenCosts *vp8enc.CoefficientTokenCostTable) ([16]vp8common.BPredictionMode, int, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
	}
	if coefProbs == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	var tokenAbove [4]uint8
	var tokenLeft [4]uint8
	if aboveTok != nil {
		tokenAbove = aboveTok.Y1
	}
	if leftTok != nil {
		tokenLeft = leftTok.Y1
	}
	totalRate := 0
	totalDist := 0
	totalBlockRD := int64(0)
	bPredEOBCount := 0
	for block := range 16 {
		bestMode := vp8common.BDCPred
		bestEOB := 0
		var bestRecon [16]byte
		bestRate := 0
		bestDist := 0
		bestCost := 0
		for i, candidate := range bPredIntraModeCandidates {
			var candidatePred [16]byte
			if !predictAnalysisBPredBlock(candidate, candidatePred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
			}
			var input [16]int16
			var dct [16]int16
			var qcoeff [16]int16
			var dqcoeff [16]int16
			vp8enc.FillBPredResidual4x4(src, mbRow, mbCol, block, candidatePred[:], &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			tokenCtx := int(tokenAbove[block&3] + tokenLeft[(block&0x0c)>>2])
			eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, &quant.Y1, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
			coefRate := coefficientBlockTokenRate(coefProbs, coefTokenCosts, 3, tokenCtx, 0, &qcoeff, eob)
			aboveMode := bPredAnalysisAboveMode(keyFrame, above, modes, block)
			leftMode := bPredAnalysisLeftMode(keyFrame, left, modes, block)
			rate := bPredModeRateWithProbs(keyFrame, candidate, aboveMode, leftMode, interBModeProbs) + coefRate
			dist := vp8enc.TransformBlockError(&dct, &dqcoeff) >> 2
			cost := vp8enc.RDCost(rdMult, rdDiv, rate, dist)
			if i == 0 || cost < bestCost {
				var candidateRecon [16]byte
				bestMode = candidate
				if eob > 1 {
					dsp.IDCT4x4Add(&dqcoeff, candidatePred[:], 4, candidateRecon[:], 4)
				} else {
					dsp.DCOnlyIDCT4x4Add(dqcoeff[0], candidatePred[:], 4, candidateRecon[:], 4)
				}
				bestRecon = candidateRecon
				bestEOB = eob
				bestRate = rate
				bestDist = dist
				bestCost = cost
			}
		}
		modes[block] = bestMode
		vp8enc.CopyBPredBlock(bestRecon[:], y, pred.YStride, block)
		hasCoeffs := uint8(0)
		if bestEOB > 0 {
			hasCoeffs = 1
			bPredEOBCount++
		}
		tokenAbove[block&3] = hasCoeffs
		tokenLeft[(block&0x0c)>>2] = hasCoeffs
		totalRate += bestRate
		totalDist += bestDist
		// libvpx rd_pick_intra4x4mby_modes accumulates the already-rounded
		// RD of each selected 4x4 block and compares that sum to best_rd.
		// It does not compare RDCOST(sum(rate), sum(distortion)).
		totalBlockRD += int64(bestCost)
		if bestRD > 0 && totalBlockRD >= int64(bestRD) {
			return [16]vp8common.BPredictionMode{}, 0, 0, 0, false
		}
	}
	return modes, totalRate, totalDist, bPredEOBCount, true
}

func round2Inputs(tb testing.TB, pattern int) (*VP8Encoder, vp8enc.SourceImage, vp8enc.MacroblockQuant) {
	e := newSizedTestEncoder(tb, 48, 48)
	img := testImage(48, 48)
	for i := range img.Y {
		if pattern == 0 {
			img.Y[i] = 128
		} else {
			img.Y[i] = byte(i*37 + pattern*19)
		}
	}
	for i := range img.U {
		img.U[i] = byte(i*13 + pattern*7)
		img.V[i] = byte(i*29 + pattern*3)
	}
	for i := range e.analysis.Img.YFull {
		e.analysis.Img.YFull[i] = byte(i*23 + pattern*31)
	}
	for i := range e.analysis.Img.UFull {
		e.analysis.Img.UFull[i] = byte(i*17 + pattern*7)
		e.analysis.Img.VFull[i] = byte(i*31 + pattern*3)
	}
	var qt [4]vp8enc.MacroblockQuant
	if err := vp8enc.InitSegmentMacroblockQuants(40, vp8common.QuantDeltas{}, vp8enc.SegmentationConfig{}, &qt); err != nil {
		tb.Fatal(err)
	}
	return e, sourceImageFromPublic(img), qt[0]
}
func round2Call(e *VP8Encoder, src vp8enc.SourceImage, q *vp8enc.MacroblockQuant, kind string, old bool, row, col int) (out [8]int, mode vp8enc.InterFrameMacroblockMode) {
	switch kind {
	case "activity-edge", "activity-inner":
		if old {
			out[0] = int(e.frozen2ssimActivityMeasure(src, row, col, 40, 0, 0, q, true, false, 128, 1))
		} else {
			out[0] = int(e.ssimActivityMeasure(src, row, col, 40, 0, 0, q, true, false, 128, 1))
		}
	case "bpred":
		var ctx fastInterModeLoopContext
		var ok bool
		if old {
			mode, out[0], out[1], out[2], out[3], ok = e.frozen2estimateFastBPredIntraModeScore(src, row, col, 40, maxInt(), q, &ctx)
		} else {
			mode, out[0], out[1], out[2], out[3], ok = e.estimateFastBPredIntraModeScore(src, row, col, 40, maxInt(), q, &ctx)
		}
		if ok {
			out[7] = 1
		}
	case "keyfast":
		var scratch vp8dec.IntraReconstructionScratch
		var modes [16]vp8common.BPredictionMode
		var ok bool
		if old {
			modes, out[0], out[1], ok = frozen2pickFastBPredLumaModeKFWithRDConstants(src, 40, 0, row, col, nil, nil, q, &e.analysis.Img, &scratch, true, 128, 1)
		} else {
			modes, out[0], out[1], ok = pickFastBPredLumaModeKFWithRDConstants(src, 40, 0, row, col, nil, nil, q, &e.analysis.Img, &scratch, true, 128, 1)
		}
		mode.BModes = modes
		if ok {
			out[7] = 1
		}
	case "whole":
		var scratch vp8dec.IntraReconstructionScratch
		var ym, uv vp8common.MBPredictionMode
		var ok bool
		if old {
			ym, uv, out[0], out[1], out[2], out[3], ok = frozen2predictBestWholeBlockIntraModeRDWithProbsAndRDConstants(src, 40, 0, 0, true, row, col, nil, nil, q, &e.analysis.Img, &scratch, &e.coefProbs, nil, nil, true, 128, 1)
		} else {
			ym, uv, out[0], out[1], out[2], out[3], ok = predictBestWholeBlockIntraModeRDWithProbsAndRDConstants(src, 40, 0, 0, true, row, col, nil, nil, q, &e.analysis.Img, &scratch, &e.coefProbs, nil, nil, true, 128, 1)
		}
		out[4] = int(ym)
		out[5] = int(uv)
		if ok {
			out[7] = 1
		}
	case "chroma":
		var scratch vp8dec.IntraReconstructionScratch
		var uv vp8common.MBPredictionMode
		var ok bool
		if old {
			uv, out[0], out[1], out[2], ok = frozen2predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src, 40, 0, 0, true, row, col, nil, nil, q, &e.analysis.Img, &scratch, &e.coefProbs, nil, true, 128, 1, nil)
		} else {
			uv, out[0], out[1], out[2], ok = predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src, 40, 0, 0, true, row, col, nil, nil, q, &e.analysis.Img, &scratch, &e.coefProbs, nil, true, 128, 1, nil)
		}
		out[4] = int(uv)
		if ok {
			out[7] = 1
		}
	case "yrd":
		var eob uint8
		var coeff [16]int16
		if old {
			out[0], out[1], eob, coeff, out[2] = frozen2wholeBlockYTransformRDWithEOBs(src, &e.analysis.Img, row, col, 0, 0, nil, nil, q, &e.coefProbs, true, nil)
		} else {
			out[0], out[1], eob, coeff, out[2] = wholeBlockYTransformRDWithEOBs(src, &e.analysis.Img, row, col, 0, 0, nil, nil, q, &e.coefProbs, true, nil)
		}
		out[3] = int(eob)
		for i, v := range coeff {
			out[4+i/4] |= int(uint64(uint16(v)) << uint((i%4)*16))
		}
	case "uvrd":
		if old {
			out[0], out[1], out[2] = frozen2wholeBlockChromaTransformRDWithEOBs(src, &e.analysis.Img, row, col, 0, 0, nil, nil, q, &e.coefProbs, true, nil)
		} else {
			out[0], out[1], out[2] = wholeBlockChromaTransformRDWithEOBs(src, &e.analysis.Img, row, col, 0, 0, nil, nil, q, &e.coefProbs, true, nil)
		}
	case "keyrd":
		var scratch vp8dec.IntraReconstructionScratch
		var modes [16]vp8common.BPredictionMode
		var ok bool
		if old {
			modes, out[0], out[1], out[2], ok = frozen2predictBestBPredLumaModeRDWithRDConstantsAndEOBs(src, 40, 0, 0, true, row, col, nil, nil, nil, nil, q, &e.analysis.Img, &scratch, maxInt(), &e.coefProbs, nil, true, 128, 1, nil)
		} else {
			modes, out[0], out[1], out[2], ok = predictBestBPredLumaModeRDWithRDConstantsAndEOBs(src, 40, 0, 0, true, row, col, nil, nil, nil, nil, q, &e.analysis.Img, &scratch, maxInt(), &e.coefProbs, nil, true, 128, 1, nil)
		}
		mode.BModes = modes
		if ok {
			out[7] = 1
		}
	}
	return
}
func TestVP8Algorithm2(t *testing.T) {
	for pat := 0; pat < 3; pat++ {
		for _, kind := range []string{"activity-edge", "activity-inner", "bpred", "keyfast", "keyrd", "yrd", "uvrd", "whole", "chroma"} {
			for _, rc := range [][2]int{{0, 0}, {0, 1}, {1, 0}, {1, 1}, {2, 2}} {
				if kind == "activity-edge" && rc[0] == rc[1] {
					continue
				}
				if kind == "activity-inner" && rc[0] != rc[1] {
					continue
				}
				a, src, q := round2Inputs(t, pat)
				b, _, _ := round2Inputs(t, pat)
				x, m := round2Call(a, src, &q, kind, false, rc[0], rc[1])
				y, n := round2Call(b, src, &q, kind, true, rc[0], rc[1])
				if x != y || m != n || !reflect.DeepEqual(a.analysis.Img.YFull, b.analysis.Img.YFull) || !reflect.DeepEqual(a.analysis.Img.UFull, b.analysis.Img.UFull) || !reflect.DeepEqual(a.analysis.Img.VFull, b.analysis.Img.VFull) {
					t.Fatalf("%s pat%d @%v got%v/%v want%v/%v", kind, pat, rc, x, m, y, n)
				}
			}
		}
	}
}

var round2Sink [8]int

func BenchmarkVP8Algorithm2(b *testing.B) {
	for _, kind := range []string{"activity-edge", "activity-inner", "bpred", "keyfast", "keyrd", "yrd", "uvrd", "whole", "chroma"} {
		for _, pat := range []int{0, 1} {
			b.Run(fmt.Sprintf("%s/p%d", kind, pat), func(b *testing.B) {
				e, src, q := round2Inputs(b, pat)
				row, col := 1, 1
				if kind == "activity-edge" {
					row = 0
				}
				b.ReportAllocs()
				for b.Loop() {
					round2Sink, _ = round2Call(e, src, &q, kind, false, row, col)
				}
			})
		}
	}
}

func frozen2wholeBlockYTransformRDWithEOBs(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool, coefTokenCosts *vp8enc.CoefficientTokenCostTable) (int, int, uint8, [16]int16, int) {
	if coefProbs == nil {
		return 0, 0, 0, [16]int16{}, 0
	}
	var dct [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	var y2Input [16]int16
	var y2Coeff [16]int16
	var y2Q [16]int16
	var y2DQ [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		y2Left = leftTok.Y2
	}

	rate := 0
	mbblockError := 0
	yACEOBCount := 0
	// Whole-MB residual+DCT batch — mirrors libvpx vp8_transform_intra_mby's
	// fdct8x4 chain. The per-block rate/distortion accumulation still runs
	// serially because token-context (yAbove/yLeft) and the regular-quantize
	// zbin-zerorun are block-sequential.
	var residuals [16 * 16]int16
	var dcts [16 * 16]int16
	vp8enc.GatherMacroblockYResiduals4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, mbCol*16, mbRow*16, residuals[:])
	vp8enc.ForwardDCT4x4Batch(residuals[:], dcts[:], 16)
	for block := range 16 {
		copy(dct[:], dcts[block*16:block*16+16])
		y2Input[block] = dct[0]
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		// libvpx quantizes the Y1 DC coefficient before Y_NO_DC token
		// costing. The DC token itself is skipped because Y2 carries it,
		// but the quantized DC still affects zbin zero-run and EOB state.
		eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, &quant.Y1, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
		dct[0] = 0
		dqcoeff[0] = 0
		rate += coefficientBlockTokenRate(coefProbs, coefTokenCosts, 0, ctx, 1, &qcoeff, eob)
		mbblockError += vp8enc.TransformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 1 {
			hasCoeffs = 1
			yACEOBCount++
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}
	vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	y2Ctx := int(y2Above + y2Left)
	y2EOB := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &y2Coeff, &quant.Y2, zbinOverQuant/2, actZbinAdj, &y2Q, &y2DQ)
	rate += coefficientBlockTokenRate(coefProbs, coefTokenCosts, 1, y2Ctx, 0, &y2Q, y2EOB)
	y2Error := vp8enc.TransformBlockError(&y2Coeff, &y2DQ)
	distortion := ((mbblockError << 2) + y2Error) >> 4
	return rate, distortion, uint8(y2EOB), y2Q, yACEOBCount
}

func frozen2wholeBlockChromaTransformRDWithEOBs(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, zbinOverQuant int, actZbinAdj int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool, coefTokenCosts *vp8enc.CoefficientTokenCostTable) (int, int, int) {
	if pred == nil || quant == nil || coefProbs == nil {
		return maxInt() / 4, maxInt() / 4, 0
	}
	uvWidth, uvHeight := vp8enc.SourceImageUVDimensions(src)
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if aboveTok != nil {
		uvAbove = vp8enc.TokenUVContextArray(aboveTok)
	}
	if leftTok != nil {
		uvLeft = vp8enc.TokenUVContextArray(leftTok)
	}

	rate := 0
	distortion := 0
	uvEOBSum := 0
	// Whole-UV residual+DCT batch — mirrors libvpx vp8_transform_mbuv's
	// pair of fdct8x4 calls. Token-context updates and the
	// regular-quantize zbin-zerorun keep the per-block accumulation
	// loop serial.
	var residuals [8 * 16]int16
	var dcts [8 * 16]int16
	vp8enc.GatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, mbCol*8, mbRow*8, residuals[0:64])
	vp8enc.GatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, mbCol*8, mbRow*8, residuals[64:128])
	vp8enc.ForwardDCT4x4Batch(residuals[:], dcts[:], 8)
	for slot := range 8 {
		block := 16 + slot
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		copy(dct[:], dcts[slot*16:slot*16+16])
		a, l := vp8enc.MacroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := vp8enc.QuantizeDecisionBlockWithActivity(fastQuant, &dct, &quant.UV, zbinOverQuant, actZbinAdj, &qcoeff, &dqcoeff)
		rate += coefficientBlockTokenRate(coefProbs, coefTokenCosts, 2, ctx, 0, &qcoeff, eob)
		distortion += vp8enc.TransformBlockError(&dct, &dqcoeff)
		uvEOBSum += eob
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate, distortion >> 2, uvEOBSum
}

func frozen2SegmentQuants(baseQIndex int, deltas vp8common.QuantDeltas, segmentation vp8enc.SegmentationConfig, out *[vp8common.MaxMBSegments]vp8enc.MacroblockQuant) error {
	// uint range check: vp8common.MinQ==0, so (baseQIndex < 0 || > MaxQ)
	// collapses to uint(baseQIndex) > uint(MaxQ).
	if uint(baseQIndex) > uint(vp8common.MaxQ) || out == nil || false {
		return vp8enc.ErrInvalidPacketConfig
	}
	var tables vp8common.FrameDequantTables
	var dequant vp8common.MacroblockDequant
	vp8common.BuildFrameDequantTables(deltas, &tables)
	for segment := range vp8common.MaxMBSegments {
		qIndex := baseQIndex
		if segmentation.Enabled && segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segment] {
			altQ := int(segmentation.FeatureData[vp8common.MBLvlAltQ][segment])
			if segmentation.AbsDelta {
				qIndex = altQ
			} else {
				qIndex = baseQIndex + altQ
			}
		}
		vp8common.InitMacroblockDequant(&tables, qIndex, &dequant)
		frozen2InitRegularMacroblockQuant(qIndex, &dequant, &out[segment])
	}
	return nil
}

var round2QuantSink [4]vp8enc.MacroblockQuant

func TestVP8Algorithm2SegmentSetup(t *testing.T) {
	for q := 0; q < 128; q++ {
		var a, b [4]vp8enc.MacroblockQuant
		d := vp8common.QuantDeltas{Y1DC: -3, Y2DC: 2, Y2AC: 4, UVDC: 1, UVAC: -2}
		s := vp8enc.SegmentationConfig{}
		if q&1 != 0 {
			s.Enabled = true
			s.FeatureEnabled[vp8common.MBLvlAltQ] = [4]bool{true, true, true, true}
			s.FeatureData[vp8common.MBLvlAltQ] = [4]int8{-10, 0, 10, 20}
		}
		if err := vp8enc.InitSegmentMacroblockQuants(q, d, s, &a); err != nil {
			t.Fatal(err)
		}
		if err := frozen2SegmentQuants(q, d, s, &b); err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("q%d mismatch", q)
		}
	}
}
func BenchmarkVP8Algorithm2SegmentSetup(b *testing.B) {
	for b.Loop() {
		if err := vp8enc.InitSegmentMacroblockQuants(40, vp8common.QuantDeltas{}, vp8enc.SegmentationConfig{}, &round2QuantSink); err != nil {
			b.Fatal(err)
		}
	}
}

func frozen2predictBestWholeBlockIntraModeRDWithProbsAndRDConstants(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interYModeProbs []uint8, interUVModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	if quant == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if coefProbs == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	bestYMode := vp8common.DCPred
	bestYRate := 0
	bestYDist := 0
	bestYCost := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, 0, 0, 0, false
		}
		yRate, yDist, _, _ := wholeBlockYTransformRD(src, pred, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, coefProbs, fastQuant)
		rate := intraYModeRateWithProbs(keyFrame, yMode, interYModeProbs) + yRate
		cost := vp8enc.RDCost(rdMult, rdDiv, rate, yDist)
		if i == 0 || cost < bestYCost {
			bestYMode = yMode
			bestYRate = rate
			bestYDist = yDist
			bestYCost = cost
		}
	}

	bestUVMode, bestUVRate, bestUVDist, ok := predictBestIntraChromaModeRDWithProbsAndRDConstants(src, qIndex, zbinOverQuant, actZbinAdj, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, interUVModeProbs, fastQuant, rdMult, rdDiv)
	if !ok {
		return 0, 0, 0, 0, 0, 0, false
	}
	return bestYMode, bestUVMode, bestYRate, bestYDist, bestUVRate, bestUVDist, true
}

func frozen2predictBestIntraChromaModeRDWithProbsAndRDConstantsAndEOBs(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, actZbinAdj int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, interUVModeProbs []uint8, fastQuant bool, rdMult int, rdDiv int, coefTokenCosts *vp8enc.CoefficientTokenCostTable) (vp8common.MBPredictionMode, int, int, int, bool) {
	if quant == nil || coefProbs == nil {
		return 0, 0, 0, 0, false
	}
	if rdMult <= 0 {
		rdMult, rdDiv = vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
	}
	bestUVMode := vp8common.DCPred
	bestUVRate := 0
	bestUVDist := 0
	bestUVCost := 0
	liveUVEOBSum := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, 0, 0, false
		}
		tokenRate, dist, uvEOBSum := wholeBlockChromaTransformRDWithEOBs(src, pred, mbRow, mbCol, zbinOverQuant, actZbinAdj, aboveTok, leftTok, quant, coefProbs, fastQuant, coefTokenCosts)
		liveUVEOBSum = uvEOBSum
		rate := intraUVModeRateWithProbs(keyFrame, uvMode, interUVModeProbs) + tokenRate
		cost := vp8enc.RDCost(rdMult, rdDiv, rate, dist)
		if i == 0 || cost < bestUVCost {
			bestUVMode = uvMode
			bestUVRate = rate
			bestUVDist = dist
			bestUVCost = cost
		}
	}
	return bestUVMode, bestUVRate, bestUVDist, liveUVEOBSum, true
}

func frozen2InitFastBlockQuant(dequant *[16]int16, out *vp8enc.BlockQuant) {
	for i := range 16 {
		d := int(dequant[i])
		out.QuantFast[i] = int16((1 << 16) / d)
		out.Round[i] = int16((48 * d) >> 7)
		out.Dequant[i] = dequant[i]
	}
}

func frozen2InitRegularBlockQuant(qIndex int, dequant *[16]int16, out *vp8enc.BlockQuant) {
	frozen2InitFastBlockQuant(dequant, out)
	q := vp8common.ClampQIndex(qIndex)
	zbinFactor := 80
	if q < 48 {
		zbinFactor = 84
	}
	for i := range 16 {
		d := int(dequant[i])
		out.Zbin[i] = int16((zbinFactor*d + 64) >> 7)
		out.ZbinBoost[i] = int16((d * round2ZbinBoost[i]) >> 7)
		frozen2invertRegularQuant(d, &out.Quant[i], &out.QuantShift[i])
	}
}

func frozen2InitRegularMacroblockQuant(qIndex int, dequant *vp8common.MacroblockDequant, out *vp8enc.MacroblockQuant) {
	frozen2InitRegularBlockQuant(qIndex, &dequant.Y1, &out.Y1)
	frozen2InitRegularBlockQuant(qIndex, &dequant.Y1DC, &out.Y1DC)
	frozen2InitRegularBlockQuant(qIndex, &dequant.Y2, &out.Y2)
	frozen2InitRegularBlockQuant(qIndex, &dequant.UV, &out.UV)
}

func frozen2invertRegularQuant(dequant int, quant *int16, shift *int16) {
	l := bits.Len(uint(dequant)) - 1
	m := 1 + (1<<(16+l))/dequant
	*quant = int16(m - (1 << 16))
	*shift = int16(1 << (16 - l))
}

var round2ZbinBoost = [16]int{0, 0, 8, 10, 12, 14, 16, 20, 24, 28, 32, 36, 40, 44, 44, 44}

func TestVP8Algorithm2BlockSetup(t *testing.T) {
	for pattern := 0; pattern < 4; pattern++ {
		var d [16]int16
		for i := range d {
			switch pattern {
			case 0:
				d[i] = 100
			case 1:
				d[i] = int16(i + 1)
			case 2:
				d[i] = int16(32767 - i)
			case 3:
				d[i] = int16(10 + i%3)
			}
		}
		for _, regular := range []bool{false, true} {
			for _, alias := range []int{0, 1, 2, 3} {
				var a, b vp8enc.BlockQuant
				a.Dequant = d
				a.Round = d
				a.QuantFast = d
				b = a
				da, db := &d, &d
				switch alias {
				case 1:
					da = &a.Dequant
					db = &b.Dequant
				case 2:
					da = &a.Round
					db = &b.Round
				case 3:
					da = &a.QuantFast
					db = &b.QuantFast
				}
				var pa, pb any
				func() {
					defer func() { pa = recover() }()
					if regular {
						vp8enc.InitRegularBlockQuant(40, da, &a)
					} else {
						vp8enc.InitFastBlockQuant(da, &a)
					}
				}()
				func() {
					defer func() { pb = recover() }()
					if regular {
						frozen2InitRegularBlockQuant(40, db, &b)
					} else {
						frozen2InitFastBlockQuant(db, &b)
					}
				}()
				if (pa == nil) != (pb == nil) || a != b {
					t.Fatalf("pattern%d regular%v alias%d", pattern, regular, alias)
				}
			}
		}
	}
}

var round2BlockSink vp8enc.BlockQuant

func BenchmarkVP8Algorithm2BlockSetup(b *testing.B) {
	var d [16]int16
	for i := range d {
		d[i] = 100
	}
	d[0] = 30
	for _, kind := range []string{"fast", "regular"} {
		b.Run(kind, func(b *testing.B) {
			for b.Loop() {
				if kind == "fast" {
					vp8enc.InitFastBlockQuant(&d, &round2BlockSink)
				} else {
					vp8enc.InitRegularBlockQuant(40, &d, &round2BlockSink)
				}
			}
		})
	}
}

func TestVP8Algorithm2ClampedEdges(t *testing.T) {
	for _, kind := range []string{"activity-edge", "activity-inner", "bpred", "keyfast", "keyrd", "yrd", "uvrd", "whole", "chroma"} {
		for _, rc := range [][2]int{{0, 2}, {2, 0}, {2, 2}} {
			a, src, q := round2Inputs(t, 2)
			b, _, _ := round2Inputs(t, 2)
			src.Width = 43
			src.Height = 41
			x, m := round2Call(a, src, &q, kind, false, rc[0], rc[1])
			y, n := round2Call(b, src, &q, kind, true, rc[0], rc[1])
			if x != y || m != n || !reflect.DeepEqual(a.analysis.Img.YFull, b.analysis.Img.YFull) || !reflect.DeepEqual(a.analysis.Img.UFull, b.analysis.Img.UFull) || !reflect.DeepEqual(a.analysis.Img.VFull, b.analysis.Img.VFull) {
				t.Fatalf("%s @%v got%v/%v want%v/%v", kind, rc, x, m, y, n)
			}
		}
	}
}
