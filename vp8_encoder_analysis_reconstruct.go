package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func macroblockImageSSE(src vp8enc.SourceImage, img *vp8common.Image, mbRow int, mbCol int) int {
	return macroblockLumaSSE(src, img, mbRow, mbCol, vp8enc.MotionVector{}) +
		vp8enc.MacroblockChromaSSE(src, img, mbRow, mbCol)
}

func macroblockImageBlockSAD(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int) int {
	if img == nil {
		return maxInt()
	}
	baseY := srcMbRow * 16
	baseX := srcMbCol * 16
	refBaseY := refMbRow * 16
	refBaseX := refMbCol * 16
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) &&
		uint(refBaseY) <= uint(img.CodedHeight-16) && uint(refBaseX) <= uint(img.CodedWidth-16) {
		return dsp.SAD16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, img.Y[refBaseY*img.YStride+refBaseX:], img.YStride)
	}
	if uint(refBaseY) <= uint(img.CodedHeight-16) && uint(refBaseX) <= uint(img.CodedWidth-16) {
		var srcScratch [16 * 16]byte
		vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
		return dsp.SAD16x16(srcScratch[:], 16, img.Y[refBaseY*img.YStride+refBaseX:], img.YStride)
	}

	sad := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, img.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, img.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(img.Y[refY*img.YStride+refX])
			// Branchless |diff| via sign-mask splat.
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
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
		for i := range 4 {
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

func buildReconstructingBPredMacroblockCoefficients(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, img *vp8common.Image, mode *vp8dec.MacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, actZbinAdj int, rdMult int, rdDiv int, fastQuant bool, optimize bool, collectOracle bool, coeffs *vp8enc.MacroblockCoefficients, scratch *vp8dec.IntraReconstructionScratch) bool {
	collectOracle = oracleTraceBuild && collectOracle
	if img == nil || mode == nil || quant == nil || coeffs == nil || scratch == nil || !mode.Is4x4 || mode.Mode != vp8common.BPred {
		return false
	}
	if coefProbs == nil {
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
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		y2Left = leftTok.Y2
	}
	// libvpx vp8/encoder/rdopt.c rd_pick_intra16x16mby_mode (rdopt.c:646-682)
	// iterates the whole-block intra Y candidates DC_PRED .. TM_PRED in
	// MB_PREDICTION_MODE enum order. Each candidate calls macro_block_yrd
	// (rdopt.c:471-517) -> vp8_quantize_mby (vp8_quantize.c:99-107) which
	// writes xd->block[24].qcoeff via x->quantize_b(&x->block[24], ...).
	// The LAST iteration is TM_PRED, so after the loop xd->block[24] holds
	// TM_PRED's Y2 quantize state.
	//
	// When B_PRED later wins (vp8_rd_pick_intra_mode rdopt.c:2397),
	// rd_pick_intra4x4mby_modes calls rd_pick_intra4x4block per sub-block
	// which only touches its own Y4x4 block, vp8cx_encode_intra_macroblock
	// runs vp8_encode_intra4x4mby (encodeintra.c:70-78) which also doesn't
	// touch Y2, and vp8_tokenize_mb (tokenize.c:353-380) skips the Y2
	// tokenizer because has_y2_block=false. So xd->block[24].qcoeff is
	// still TM_PRED's quantize when govpx_oracle_capture_mb fires at
	// encodeframe.c:553.
	//
	// govpx's whole-block picker (predictBestWholeBlockIntraModeRDWithProbs*)
	// iterates the same DC/V/H/TM candidates via wholeBlockYTransformRD
	// (vp8_encoder_intra_pick.go:309-370) but the y2EOB/y2Q outputs are
	// discarded. To make the oracle trace dump match libvpx byte-exact on
	// the residual 1280x720 SSIM seed `regression_option_grid_19981bff`
	// (FIRST_CANON_DIV idx=2 / MB(0,2) eob_sum=109 vs 108 on origin/main),
	// rebuild TM_PRED's stale Y2 here using the same DC[0]-from-Y4x4 path
	// that wholeBlockYTransformRD runs internally for TM_PRED's candidate
	// scoring.
	//
	// The neighbor pixels feeding TM_PRED (refs.YAbove, refs.YLeft,
	// refs.YTopLeft) are captured from the analysis frame BEFORE the
	// per-sub-block B_PRED loop below starts writing reconstructed pixels
	// into img.Y, mirroring libvpx's xd->dst.y_buffer state at MB head
	// before rd_pick_intra16x16mby_mode starts (rdopt.c:660-662).
	var staleTMPredY2Input [16]int16
	if collectOracle {
		var tmPred [16 * 16]byte
		if vp8dec.PredictIntraY16x16(vp8common.TMPred, tmPred[:], 16, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable) {
			var tmResiduals [16 * 16]int16
			vp8enc.GatherMacroblockYResiduals4x4FromPredBuffer(src.Y, src.YStride, src.Width, src.Height, tmPred[:], 16, mbCol*16, mbRow*16, tmResiduals[:])
			var tmDCTs [16 * 16]int16
			vp8enc.ForwardDCT4x4Batch(tmResiduals[:], tmDCTs[:], 16)
			for block := range 16 {
				staleTMPredY2Input[block] = tmDCTs[block*16]
			}
		}
	}
	for block := range 16 {
		blockOffset := vp8enc.AnalysisYBlockOffset(block, img.YStride)
		if !predictAnalysisBPredBlock(mode.BModes[block], y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			return false
		}
		x := mbCol*16 + (block&3)*4
		yCoord := mbRow*16 + (block>>2)*4
		vp8enc.FillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		// libvpx vp8_encode_intra4x4mby (encodeintra.c) never invokes the
		// trellis optimizer for B_PRED Y sub-blocks: it calls
		// vp8_encode_intra4x4block which runs only x->quantize_b before the
		// IDCT-add. The frame-level vp8_optimize_mby pass is wired only
		// from vp8_encode_intra16x16mby. So the Y plane of any B_PRED MB
		// (keyframe or inter intra-coded) must be quantized without
		// trellising regardless of the encoder-level optimize flag; only
		// the UV blocks below pick up the optimizer (they go through
		// vp8_encode_intra16x16mbuv -> vp8_optimize_mbuv). Without this
		// gate the BestQuality keyframe Y reconstruction byte-diverges
		// from libvpx on B_PRED MBs (see r9-4 SplitMV-quadrant fixture).
		eob := vp8enc.QuantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, 0, actZbinAdj, zbinOverQuant, rdMult, rdDiv, mode.RefFrame == vp8common.IntraFrame, fastQuant, false, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
		coeffs.SetBlockEOB(block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
		vp8enc.AddQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
	}
	coeffs.QCoeff[24] = [16]int16{}
	coeffs.SetBlockEOB(24, 0)
	// Mirror libvpx's TM_PRED-derived stale Y2 snapshot for the oracle
	// trace. The TM_PRED iteration of rd_pick_intra16x16mby_mode is the
	// LAST whole-Y candidate, and xd->block[24].qcoeff retains TM_PRED's
	// Y2 quantize state when B_PRED wins (see the long comment above the
	// staleTMPredY2Input block for the libvpx call-chain reasoning). The
	// pre-task-225 govpx implementation built this stale Y2 from each
	// B_PRED winning sub-block's DCT[0], which produced a different value
	// because B_PRED uses per-sub-block predictors (each 4x4 uses its
	// own neighbors that include reconstructed previous sub-blocks)
	// instead of TM_PRED's uniform whole-MB predictor (above + left +
	// top-left lifted into a single MB-wide prediction).
	if collectOracle {
		var staleY2Coeff [16]int16
		var staleY2Q [16]int16
		var staleY2DQ [16]int16
		intra := mode.RefFrame == vp8common.IntraFrame
		vp8enc.ForwardWalsh4x4(staleTMPredY2Input[:], 4, &staleY2Coeff)
		staleEOB := min(max(vp8enc.QuantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, 0, actZbinAdj, zbinOverQuant, rdMult, rdDiv, intra, fastQuant, optimize, &staleY2Coeff, &quant.Y2, &staleY2Q, &staleY2DQ), 0), 16)
		recordOracleStaleY2(coeffs, uint8(staleEOB), staleY2Q)
	}

	if !vp8dec.PredictIntraUV8x8(mode.UVMode, u, img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !vp8dec.PredictIntraUV8x8(mode.UVMode, v, img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
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
	// Whole-UV residual+DCT batch — prediction was already written
	// into img.U / img.V above so all 8 chroma 4x4 residuals are
	// independent and can be transformed in a single dispatched call,
	// matching libvpx v1.16.0 vp8_transform_mbuv's two fdct8x4 calls.
	var uvResiduals [8 * 16]int16
	var uvDcts [8 * 16]int16
	vp8enc.GatherMacroblockUVResiduals4x4(src.U, src.UStride, uvWidth, uvHeight, img.U, img.UStride, mbCol*8, mbRow*8, uvResiduals[0:64])
	vp8enc.GatherMacroblockUVResiduals4x4(src.V, src.VStride, uvWidth, uvHeight, img.V, img.VStride, mbCol*8, mbRow*8, uvResiduals[64:128])
	vp8enc.ForwardDCT4x4Batch(uvResiduals[:], uvDcts[:], 8)
	for block := range 4 {
		copy(dct[:], uvDcts[block*16:block*16+16])
		a, l := vp8enc.MacroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := vp8enc.QuantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, 0, actZbinAdj, zbinOverQuant, rdMult, rdDiv, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		vp8enc.AddQuantizedBlockResidual(eob, &dq, u[vp8enc.AnalysisUVBlockOffset(block, img.UStride):], img.UStride)

		copy(dct[:], uvDcts[(4+block)*16:(4+block)*16+16])
		a, l = vp8enc.MacroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = vp8enc.QuantizeEncodedBlockWithRDZbinAndActivity(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, 0, actZbinAdj, zbinOverQuant, rdMult, rdDiv, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		vp8enc.AddQuantizedBlockResidual(eob, &dq, v[vp8enc.AnalysisUVBlockOffset(block, img.VStride):], img.VStride)
	}
	return true
}

func reconstructInterAnalysisMacroblock(img *vp8common.Image, last *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	return reconstructInterAnalysisMacroblockWithState(img, last, nil, row, col, mode, tokens, dequant, scratch)
}

func reconstructInterAnalysisMacroblockWithState(img *vp8common.Image, last *vp8common.Image, state *vp8dec.InterFrameRefState, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	if mode.Mode == vp8common.SplitMV {
		return vp8dec.ReconstructSplitMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
	}
	if vp8dec.ReconstructWholeMVInterMacroblockWithState(state, mode, tokens, dequant, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col) {
		return true
	}
	return vp8dec.ReconstructWholeMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
}

func predictInterAnalysisSplitMVChroma(img *vp8common.Image, last *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode) bool {
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.PredictSplitMVInterChroma(mode, last, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, row, col, vp8dec.InterPredictionConfig{})
}

// addInterResidualToAnalysisMacroblock assumes img already contains the
// matching inter predictor for mode at row/col. The residual is applied with
// the fused dequant+IDCT+add pair kernels — the same
// vp8_inverse_transform_mby + vp8_dequant_idct_add_uv_block pair libvpx's
// vp8cx_encode_inter_macroblock runs (encodeframe.c:1288-1291). The tokens
// come from ConvertMacroblockCoefficients, whose EOB >= 2 blocks are full
// copies of quantizer output (all 16 slots written, zeros beyond EOB) and
// whose EOB <= 1 blocks are covered by the fused kernels' sanitize/DC-gate
// guards; see vp8dec.DequantIDCTAddMacroblock for the audit.
func addInterResidualToAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	if img == nil || mode == nil || tokens == nil || dequant == nil || scratch == nil || mode.RefFrame == vp8common.IntraFrame {
		return false
	}
	switch mode.Mode {
	case vp8common.ZeroMV, vp8common.NearestMV, vp8common.NearMV, vp8common.NewMV, vp8common.SplitMV:
	default:
		return false
	}
	if mode.MBSkipCoeff {
		return true
	}
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	is4x4 := mode.Is4x4 || mode.Mode == vp8common.SplitMV
	vp8dec.DequantIDCTAddMacroblock(tokens, dequant, is4x4, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride)
	return true
}

func reconstructAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	if !vp8dec.ReconstructIntraMacroblock(mode, tokens, dequant, refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual) {
		return false
	}
	is4x4 := mode.Is4x4 || mode.Mode == vp8common.BPred
	applyLibvpxY2EobAdjustToAnalysisMacroblock(tokens, is4x4, &scratch.Residual, img.Y[yOff:], img.YStride)
	return true
}

// applyLibvpxY2EobAdjustToAnalysisMacroblock mirrors libvpx's
// vp8_dequant_idct_add_y_block_c eob<=1 path for non-SPLITMV non-B_PRED
// 16x16 macroblocks. libvpx unconditionally runs the inverse Walsh in
// vp8_inverse_transform_mby (vp8/common/invtrans.h), writes per-Y-block
// qcoeff[0] from xd->block[24].dqcoeff[], and applies DC-only IDCT for
// every Y block with eob<=1 using q[0]*dq[0] (dq[0]=1 via the
// dequant_y1_dc override). vp8enc.ConvertMacroblockCoefficients's
// max(src.EOB[i], 1) promotion on the !is4x4 path lets
// AddMacroblockResidual cover this case for the production convert
// pipeline; this helper is the catch-all keeping the analysis-image
// mirror explicit so future refactors of the convert pass do not
// silently lose libvpx parity (see dc16770 stale-Y2-DC diagnosis).
func applyLibvpxY2EobAdjustToAnalysisMacroblock(tokens *vp8dec.MacroblockTokens, is4x4 bool, scratch *vp8dec.MacroblockResidual, y []byte, yStride int) {
	if tokens == nil || scratch == nil || is4x4 || tokens.EOB[24] == 0 {
		return
	}
	for block := range 16 {
		if tokens.EOB[block] != 0 {
			continue
		}
		dc := scratch.DQCoeff[block*16]
		if dc == 0 {
			continue
		}
		offset := vp8enc.AnalysisYBlockOffset(block, yStride)
		dsp.DCOnlyIDCT4x4Add(dc, y[offset:], yStride, y[offset:], yStride)
	}
}

func clampEncodeCoord(v int, limit int) int {
	return min(max(v, 0), limit-1)
}
