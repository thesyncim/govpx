package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// vp8ActivityAvgMin is the libvpx activity floor used by SSIM activity
// masking. It keeps very flat macroblocks from producing zero RD multipliers.
const vp8ActivityAvgMin = 64

// vp8ActivityAvgAltFixed is the fixed activity_avg value libvpx uses whenever
// ALT_ACT_MEASURE is enabled (the default in onyx_if.c's TuneSSIM path).
// See vp8/encoder/encodeframe.c calc_av_activity: with ALT_ACT_MEASURE=1 the
// frame-mean is always overwritten with 100000 before vp8_activity_masking
// applies the per-MB rdmult and zbin adjustments. Matching that constant is
// required for byte parity once the per-MB activity span is wide (e.g. the
// segmented checkerboard fixture).
const vp8ActivityAvgAltFixed uint32 = 100000

// prepareTuningActivityMap builds the per-frame macroblock activity map used
// by TuneSSIM.
//
// Mirrors libvpx vp8/encoder/encodeframe.c build_activity_map with
// ALT_ACT_MEASURE=1 (the compile-time default in libvpx v1.16.0):
//
//   - vp8cx_initialize_me_consts initializes the activity probe's quant
//     tables from cm->base_qindex (no segment offsets), then
//     init_encode_frame_mb_context runs vp8_setup_intra_recon to seed only the
//     new-frame reconstruction buffer's 127 top border and 129 left border.
//   - Per MB, mb_activity_measure() returns vp8_encode_intra(), which
//     predicts (DC_PRED 16x16 on top-row or left-column MBs; B_DC_PRED 4x4
//     on the (0,0) corner and on interior MBs), then returns the
//     vpx_get_mb_ss of the source-vs-predictor residue. The same call
//     quantize+IDCT-rebuilds the MB's pixels back into the recon buffer so
//     subsequent MBs see the reconstructed neighbors (vp8_extend_mb_row
//     fills the column to the right of the encoded row for prediction).
//   - calc_av_activity() then overwrites cpi->activity_avg with the fixed
//     value 100000, regardless of the per-MB sum.
//
// We reproduce the same walk: seed e.analysis as the activity probe's
// reconstruction buffer, predict from analysis-buffer neighbors, accumulate
// src-vs-pred SSE for the activity value, and quantize+IDCT-encode the residue
// back into the analysis buffer so the next MB's neighbors track libvpx's
// reconstruction. The TunePSNR path skips the whole pass.
func (e *VP8Encoder) prepareTuningActivityMap(src vp8enc.SourceImage, rows int, cols int) error {
	if e.opts.Tuning != TuneSSIM {
		e.activityMapValid = false
		return nil
	}
	required := rows * cols
	if required <= 0 {
		e.activityMapValid = false
		return ErrInvalidConfig
	}
	if cap(e.activityMap) < required {
		e.activityMap = make([]uint32, required)
	} else {
		e.activityMap = e.activityMap[:required]
	}

	// libvpx initializes the activity-probe quantizer from base_qindex
	// (vp8_initialize_rd_consts is called with cm->base_qindex right before
	// build_activity_map). Segment offsets and the loop-filter delta do not
	// participate. Use the rate controller's frame-level qindex.
	qIndex := e.rc.currentQuantizer
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	_ = vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, vp8enc.SegmentationConfig{}, &quants)

	// libvpx's build_activity_map uses cm->new_fb_idx after
	// vp8_setup_intra_recon. That setup writes only the synthetic top and
	// left predictor borders; the activity probe itself fills the coded
	// pixels as it walks the frame.
	setupIntraReconImage(&e.analysis.Img)

	for row := range rows {
		for col := range cols {
			index := row*cols + col
			e.activityMap[index] = e.ssimActivityMeasure(src, row, col, qIndex, &quants[0])
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	// encodeframe.c calls init_encode_frame_mb_context again after
	// build_activity_map; mirror the second vp8_setup_intra_recon while
	// preserving the activity probe's reconstructed interior.
	setupIntraReconImage(&e.analysis.Img)
	// libvpx's calc_av_activity sets activity_avg = 100000 unconditionally
	// when ALT_ACT_MEASURE is enabled, regardless of the per-MB sum.
	e.activityAvg = vp8ActivityAvgAltFixed
	e.activityMapValid = true
	return nil
}

func setupIntraReconImage(img *vp8common.Image) {
	if img == nil {
		return
	}
	setupIntraReconPlane(img.YFull, img.YOrigin, img.YStride, img.CodedWidth, img.CodedHeight)
	uvWidth := (img.CodedWidth + 1) >> 1
	uvHeight := (img.CodedHeight + 1) >> 1
	setupIntraReconPlane(img.UFull, img.UOrigin, img.UStride, uvWidth, uvHeight)
	setupIntraReconPlane(img.VFull, img.VOrigin, img.VStride, uvWidth, uvHeight)
}

func setupIntraReconPlane(full []byte, origin int, stride int, width int, height int) {
	if len(full) == 0 || origin <= 0 || stride <= 0 || width <= 0 || height <= 0 {
		return
	}
	top := origin - 1 - stride
	if top >= 0 {
		n := min(width+5, len(full)-top)
		for i := range n {
			full[top+i] = 127
		}
	}
	for row := range height {
		index := origin + row*stride - 1
		if uint(index) < uint(len(full)) {
			full[index] = 129
		}
	}
}

// ssimActivityMeasure mirrors libvpx's vp8_encode_intra return value for one
// 16x16 macroblock: the sum of squares of (src - intra_predictor) across
// the whole MB. Prediction follows mb_activity_measure's ALT_ACT_MEASURE
// path — DC_PRED 16x16 on the (mb_col XOR mb_row) frame edges (excluding
// the (0,0) corner) and B_DC_PRED 4x4 on the corner + interior MBs. After
// computing the activity, the function quantize+IDCT-rebuilds the residue
// back into e.analysis.Img.Y so the next MB's prediction reads from the
// reconstructed neighbors libvpx would have written there.
func (e *VP8Encoder) ssimActivityMeasure(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, quant *vp8enc.MacroblockQuant) uint32 {
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
			coefProbs: &vp8tables.DefaultCoefProbs,
			src:       src,
			mbRow:     mbRow,
			mbCol:     mbCol,
			pred:      img,
			quant:     quant,
			qIndex:    qIndex,
			intra:     true,
			fastQuant: true,
			coeffs:    &coeffs,
		})
		var tokens vp8dec.MacroblockTokens
		convertMacroblockCoefficients(&coeffs, false, &tokens)
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
			blockOffset := analysisYBlockOffset(block, img.YStride)
			if !predictAnalysisBPredBlock(vp8common.BDCPred, y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return vp8ActivityAvgMin
			}
			sse += bPredBlockSSE(src, mbRow, mbCol, block, y[blockOffset:], img.YStride)
			x := mbCol*16 + (block&3)*4
			yCoord := mbRow*16 + (block>>2)*4
			fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob := quantizeEncodedBlock(&vp8tables.DefaultCoefProbs, qIndex, 3, ctx, 0, 0, 0, true, true, false, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
			coeffs.SetBlockEOB(block, eob)
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
			addQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
		}
	}
	if sse < vp8ActivityAvgMin {
		sse = vp8ActivityAvgMin
	}
	return uint32(sse)
}

// tunedRDModeScoreWithZbin applies TuneSSIM's per-macroblock RD multiplier
// adjustment. Callers keep the default path outside this helper so PSNR mode
// does not pay the helper call inside per-MB loops.
func (e *VP8Encoder) tunedRDModeScoreWithZbin(qIndex int, zbinOverQuant int, mbRow int, mbCol int, rate int, distortion int) int {
	if !e.activityMapValid {
		return rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, distortion)
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

// tunedRDMultiplier mirrors libvpx's activity masking multiplier: textured
// blocks tolerate more distortion, while flat blocks receive a lower
// multiplier.
func (e *VP8Encoder) tunedRDMultiplier(rdMult int, mbRow int, mbCol int) int {
	if !e.activityMapValid {
		return rdMult
	}
	activity, ok := e.activityAt(mbRow, mbCol)
	if !ok {
		return rdMult
	}
	avg := max(int64(e.activityAvg), vp8ActivityAvgMin)
	act := int64(activity)
	a := act + 2*avg
	b := 2*act + avg
	if a <= 0 {
		return rdMult
	}
	adjusted := (int64(rdMult)*b + (a >> 1)) / a
	if adjusted < 1 {
		return 1
	}
	if adjusted > int64(maxInt()) {
		return maxInt()
	}
	return int(adjusted)
}

// tunedErrorPerBit mirrors libvpx's per-MB x->errorperbit lift inside
// vp8_activity_masking. After scaling x->rdmult by (act + 2*avg) /
// (2*act + avg), libvpx recomputes x->errorperbit = x->rdmult * 100 /
// (110 * x->rddiv) and floors it at 1. The recompute leans on the same rdMult
// / rdDiv split the existing libvpxRDConstantsWithZbin() helper produces, so
// the activity-adjusted rdMult divided by 110 collapses to the same
// rawRDMultiplier / 110 ratio that libvpxErrorPerBit derives on the
// PSNR-tuned path — only with the activity-masked raw multiplier.
//
// Callers pass qIndex (the base, not segment-adjusted, frame qindex libvpx
// uses for cpi->RDMULT) so the no-zbin-over-quant fractional motion search
// rate gets the same scaling libvpx applies. The fast/RD pickers run subpel
// refinement with the frame-level rd-constant pair, mirroring the libvpx
// vp8_initialize_rd_consts(cm->base_qindex) → vp8_activity_masking flow.
//
// Returns the libvpx-default libvpxErrorPerBit(qIndex) when activity masking
// is inactive so the PSNR path stays unchanged.
func (e *VP8Encoder) tunedErrorPerBit(qIndex int, mbRow int, mbCol int) int {
	if !e.activityMapValid {
		return libvpxErrorPerBit(qIndex)
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, 0)
	tuned := e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	if rdDiv <= 0 {
		return libvpxErrorPerBit(qIndex)
	}
	// x->rdmult * 100 / (110 * x->rddiv), floored at 1 to match libvpx's
	// errorperbit += (errorperbit == 0) post-fix.
	value := (int64(tuned) * 100) / (110 * int64(rdDiv))
	if value < 1 {
		return 1
	}
	if value > int64(maxInt()) {
		return maxInt()
	}
	return int(value)
}

// tunedZbinOverQuant mirrors libvpx's activity-adjusted zero-bin bias. The
// returned value is clamped to the regulator's legal zbin-over-quant range.
func (e *VP8Encoder) tunedZbinOverQuant(zbinOverQuant int, mbRow int, mbCol int) int {
	adjustment, ok := e.tunedZbinAdjustment(mbRow, mbCol)
	if !ok {
		return zbinOverQuant
	}
	zbinOverQuant += adjustment
	if zbinOverQuant < 0 {
		return 0
	}
	if zbinOverQuant > libvpxZbinOverQuantMax {
		return libvpxZbinOverQuantMax
	}
	return zbinOverQuant
}

// tunedZbinAdjustment returns the libvpx adjust_act_zbin per-MB delta
// (x->act_zbin_adj) so callers that need the libvpx (zbin_over_quant/2 +
// act_zbin_adj) Y2-block bias can apply the adjustment AFTER halving the
// base zbin-over-quant — adding the full act_zbin_adj on top, matching
// libvpx's ZBIN_EXTRA_Y2 macro. Returns ok=false on TunePSNR or out-of-
// range MB coordinates.
func (e *VP8Encoder) tunedZbinAdjustment(mbRow int, mbCol int) (int, bool) {
	if !e.activityMapValid {
		return 0, false
	}
	activity, ok := e.activityAt(mbRow, mbCol)
	if !ok {
		return 0, false
	}
	avg := max(int64(e.activityAvg), vp8ActivityAvgMin)
	act := int64(activity)
	a := act + 4*avg
	b := 4*act + avg
	if min(a, b) <= 0 {
		return 0, false
	}
	if act > avg {
		return int((b+(a>>1))/a) - 1, true
	}
	return 1 - int((a+(b>>1))/b), true
}

// activityAt returns the cached macroblock activity value for TuneSSIM.
func (e *VP8Encoder) activityAt(mbRow int, mbCol int) (uint32, bool) {
	// Single 'min < 0' folds the two negative-bounds checks; the cache-
	// valid bool is independent and stays separate.
	if min(mbRow, mbCol) < 0 || !e.activityMapValid {
		return 0, false
	}
	cols := encoderMacroblockCols(e.opts.Width)
	index := mbRow*cols + mbCol
	if uint(index) >= uint(len(e.activityMap)) {
		return 0, false
	}
	return e.activityMap[index], true
}
