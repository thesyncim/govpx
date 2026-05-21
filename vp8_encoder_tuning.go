package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
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
	fastQuant := e.libvpxUseFastQuant()
	// libvpx vp8/encoder/vp8_quantize.c vp8cx_mb_init_quantizer (ZBIN_EXTRA_Y
	// macro at :276-279) folds (zbin_over_quant + zbin_mode_boost +
	// act_zbin_adj) into b->zbin_extra before build_activity_map runs.
	// vp8cx_frame_init_quantizer (vp8_quantize.c:433) is invoked at the head
	// of every vp8_encode_frame call (encodeframe.c:719); the recode loop in
	// onyx_if.c re-enters vp8_encode_frame after updating
	// cpi->mb.zbin_over_quant (onyx_if.c:4115-4140), and frame_init_quantizer
	// resets cpi->mb.zbin_mode_boost to 0 (vp8_quantize.c:435) but
	// cpi->mb.act_zbin_adj is left at the value stored by the previous
	// attempt's last-MB adjust_act_zbin call (encodeframe.c:1071-1090). The
	// activity probe's vp8_quantize_mby therefore observes a non-zero
	// zbin_extra whenever (a) zbin_over_quant>0 from the rate controller, or
	// (b) the previous attempt's last MB produced a non-zero act_zbin_adj.
	// Both shifts the per-block quantize result, which moves the Walsh-DC
	// and per-block IDCT-add recon written into xd->dst.y_buffer. Without
	// folding both terms into govpx's activity probe quantize, the recon
	// drifts from libvpx on every non-first recode that touches either.
	zbinOverQuant := e.rc.currentZbinOverQuant
	actZbinAdj := e.activityProbeStaleActZbinAdj
	// libvpx vp8/encoder/encodemb.c:436-438 vp8_optimize_mby short-circuits
	// when xd->above_context == NULL. cm->above_context is allocated at
	// frame-buffer setup but xd->above_context is assigned only inside
	// encode_mb_row (vp8/encoder/encodeframe.c:357), which runs AFTER
	// build_activity_map (encodeframe.c:731). The very first activity probe
	// of the encoder's lifetime therefore runs with xd->above_context == NULL
	// and skips the optimize trellis on its DC16 edge MBs. Every subsequent
	// probe — same-frame recodes (which call build_activity_map again after
	// the first attempt's encode_mb_row has already seeded above_context),
	// every later frame's probes — sees a non-NULL pointer because libvpx
	// never resets it. Mirror that gate here: force optimize=false on the
	// first probe, leave libvpxOptimizeCoefficients() in place otherwise.
	optimize := e.libvpxOptimizeCoefficients() && e.activityProbeAboveContextSeeded
	activityRDMult, activityRDDiv := e.activityProbeRDConstants(qIndex, 0)

	// libvpx's build_activity_map uses cm->new_fb_idx after
	// vp8_setup_intra_recon. That setup writes only the synthetic top and
	// left predictor borders; the activity probe itself fills the coded
	// pixels as it walks the frame.
	setupIntraReconImage(&e.analysis.Img)

	for row := range rows {
		for col := range cols {
			index := row*cols + col
			e.activityMap[index] = e.ssimActivityMeasure(src, row, col, qIndex, zbinOverQuant, actZbinAdj, &quants[0], fastQuant, optimize, activityRDMult, activityRDDiv)
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
	// After the first probe runs, the next encode_mb_row equivalent will
	// assign xd->above_context = cm->above_context (libvpx
	// vp8/encoder/encodeframe.c:357). Subsequent probes (same-frame recodes
	// and every later frame) therefore see a non-NULL pointer and run
	// vp8_optimize_mby. Flip the seed flag here so the next call to
	// prepareTuningActivityMap honors libvpxOptimizeCoefficients() without
	// the NULL-context override.
	e.activityProbeAboveContextSeeded = true
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
func (e *VP8Encoder) ssimActivityMeasure(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, zbinOverQuant int, actZbinAdj int, quant *vp8enc.MacroblockQuant, fastQuant bool, optimize bool, rdMult int, rdDiv int) uint32 {
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
			blockOffset := analysisYBlockOffset(block, img.YStride)
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
			eob := quantizeEncodedBlockWithRDZbinAndActivity(&e.coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, 0, actZbinAdj, zbinOverQuant, rdMult, rdDiv, true, fastQuant, false, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
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
// does not pay the helper call inside per-MB loops. The pass-2 iiratio
// lift from vp8_initialize_rd_consts (rdopt.c:189-196) is applied via
// libvpxRDConstantsWithZbinForFrame, mirroring libvpx's frame-level
// cpi->RDMULT seen by the per-MB activity-masking step.
func (e *VP8Encoder) tunedRDModeScoreWithZbin(qIndex int, zbinOverQuant int, mbRow int, mbCol int, rate int, distortion int) int {
	if !e.activityMapValid {
		return e.rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, distortion)
	}
	rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
	rdMult = e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	return vp8enc.RDCost(rdMult, rdDiv, rate, distortion)
}

// rdModeScoreWithZbin is the encoder-aware analog of the package-level
// rdModeScoreWithZbin: it threads the pass-2 iiratio lift from
// vp8_initialize_rd_consts (rdopt.c:189-196) through the bare
// vp8enc.RDCost shape used by the inter-frame mode picker. When the
// encoder is in single-pass (or on a KEY_FRAME) the lift collapses to
// the same value the bare helper produces.
func (e *VP8Encoder) rdModeScoreWithZbin(qIndex int, zbinOverQuant int, rate int, distortion int) int {
	rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
	return vp8enc.RDCost(rdMult, rdDiv, rate, distortion)
}

func (e *VP8Encoder) activityProbeRDConstants(qIndex int, zbinOverQuant int) (int, int) {
	if e.activityProbeRDValid {
		return e.activityProbeRDMult, e.activityProbeRDDiv
	}
	return vp8enc.RDConstantsWithZbin(qIndex, zbinOverQuant)
}

// libvpxRDConstantsWithZbinForFrame ports vp8_initialize_rd_consts including
// the pass==2 && !KEY_FRAME iiratio lift at rdopt.c:189-196. When the encoder
// is mid-pass-2 on a non-KEY_FRAME and rateControlState.passNextIIRatioValid is
// armed, the lift uses `cpi->twopass.next_iiratio` (clamped to [0, 31]).
// Otherwise the helper collapses to the bare vp8enc.RDConstantsWithZbin.
//
// The per-frame iiratio value is set by setPassNextIIRatioForFrame at the top
// of pass-2 setup (mirroring vp8_second_pass at firstpass.c:2310-2317). Hot
// callers on the inter-frame production path should prefer this method over
// the bare helper so the lifted RDMULT propagates into trellis / mode-score
// callers, matching libvpx's frame-level cpi->RDMULT semantics.
func (e *VP8Encoder) libvpxRDConstantsWithZbinForFrame(qIndex int, zbinOverQuant int) (int, int) {
	iiRatio := -1
	if e.rc.passNextIIRatioValid {
		iiRatio = int(e.rc.passNextIIRatio)
	}
	return vp8enc.RDConstantsWithZbinAndIIRatio(qIndex, zbinOverQuant, iiRatio)
}

func (e *VP8Encoder) updateActivityProbeRDState(qIndex int, zbinOverQuant int, rows int, cols int) {
	rdMult, rdDiv := e.libvpxRDConstantsWithZbinForFrame(qIndex, zbinOverQuant)
	if e.activityMapValid && rows > 0 && cols > 0 {
		rdMult = e.tunedRDMultiplier(rdMult, rows-1, cols-1)
	}
	e.activityProbeRDMult = rdMult
	e.activityProbeRDDiv = rdDiv
	e.activityProbeRDValid = true
	e.captureActivityProbeStaleActZbinAdj(rows, cols)
}

// captureActivityProbeAttemptCarry mirrors libvpx's per-attempt carry of
// cpi->mb.rdmult / cpi->mb.rddiv / cpi->mb.act_zbin_adj across recode
// iterations. After every encode_mb_row pass, vp8_activity_masking
// (encodeframe.c:307) leaves x->rdmult at the activity-masked value of the
// last MB. The next vp8_encode_frame call's vp8_initialize_rd_consts updates
// cpi->RDMULT for the new Q but does NOT reset cpi->mb.rdmult; the activity
// probe trellis (vp8_optimize_mby) reads mb->rdmult directly, so it sees
// the stale activity-masked value from the previous attempt's last MB.
// govpx must mirror that exact carry — not just at frame end (which is the
// only place updateActivityProbeRDState fires today), but at every
// completed attempt — so the next prepareTuningActivityMap call's trellis
// scoring matches libvpx's.
func (e *VP8Encoder) captureActivityProbeAttemptCarry(qIndex int, zbinOverQuant int, rows int, cols int) {
	e.updateActivityProbeRDState(qIndex, zbinOverQuant, rows, cols)
}

// captureActivityProbeStaleActZbinAdj caches the libvpx-faithful value of
// cpi->mb.act_zbin_adj left over from the last MB of the previous
// vp8_encode_frame call. libvpx invokes adjust_act_zbin
// (encodeframe.c:1071-1090) once per MB during encode_mb_row; after the
// last MB (mb_rows-1, mb_cols-1) finishes, x->act_zbin_adj is the value
// derived from that MB's activity. vp8cx_mb_init_quantizer
// (vp8_quantize.c:291) then folds it into b->zbin_extra at the head of the
// NEXT vp8_encode_frame call, before build_activity_map runs. The recode
// loop and every later frame inherit the same bias.
//
// govpx must mirror this carry so the activity probe's quantize sees the
// same zbin_extra bias libvpx does — without it, the probe's recon drifts
// from libvpx on every non-first attempt that touches a non-flat last MB.
func (e *VP8Encoder) captureActivityProbeStaleActZbinAdj(rows int, cols int) {
	if !e.activityMapValid || rows <= 0 || cols <= 0 {
		e.activityProbeStaleActZbinAdj = 0
		return
	}
	if adj, ok := e.tunedZbinAdjustment(rows-1, cols-1); ok {
		e.activityProbeStaleActZbinAdj = adj
		return
	}
	e.activityProbeStaleActZbinAdj = 0
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
// / rdDiv split the existing vp8enc.RDConstantsWithZbin() helper produces, so
// the activity-adjusted rdMult divided by 110 collapses to the same
// rawRDMultiplier / 110 ratio that vp8enc.ErrorPerBit derives on the
// PSNR-tuned path — only with the activity-masked raw multiplier.
//
// Callers pass qIndex (the base, not segment-adjusted, frame qindex libvpx
// uses for cpi->RDMULT) so the no-zbin-over-quant fractional motion search
// rate gets the same scaling libvpx applies. The fast/RD pickers run subpel
// refinement with the frame-level rd-constant pair, mirroring the libvpx
// vp8_initialize_rd_consts(cm->base_qindex) → vp8_activity_masking flow.
//
// Returns the libvpx-default vp8enc.ErrorPerBit(qIndex) when activity masking
// is inactive so the PSNR path stays unchanged.
func (e *VP8Encoder) tunedErrorPerBit(qIndex int, mbRow int, mbCol int) int {
	iiRatio := -1
	if e.rc.passNextIIRatioValid {
		iiRatio = int(e.rc.passNextIIRatio)
	}
	if !e.activityMapValid {
		return vp8enc.ErrorPerBitWithZbinAndIIRatio(qIndex, 0, iiRatio)
	}
	rdMult, rdDiv := vp8enc.RDConstantsWithZbinAndIIRatio(qIndex, 0, iiRatio)
	tuned := e.tunedRDMultiplier(rdMult, mbRow, mbCol)
	if rdDiv <= 0 {
		return vp8enc.ErrorPerBitWithZbinAndIIRatio(qIndex, 0, iiRatio)
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
	cols := geometry.MacroblockCols(e.opts.Width)
	index := mbRow*cols + mbCol
	if uint(index) >= uint(len(e.activityMap)) {
		return 0, false
	}
	return e.activityMap[index], true
}
