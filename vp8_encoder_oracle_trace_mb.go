//go:build govpx_oracle_trace

package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) resetOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	state := e.oracleTraceState()
	state.mbBuffer = state.mbBuffer[:0]
	state.interCandidateBuffer = state.interCandidateBuffer[:0]
}

// flushOracleMBTraceBuffer writes the buffered accepted-path rows to the
// configured writer in scan order and clears the buffer. Inter-candidate rows
// normally flush from emitOracleRecodeIterTrace with iter/Q; the candidate loop
// below remains as a fallback for direct trace tests and non-loop captures.
func (e *VP8Encoder) flushOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	state := e.oracleTraceState()
	w := state.writer
	for i := range state.interCandidateBuffer {
		candidate := &state.interCandidateBuffer[i]
		if !state.interCandidateTraceAllowed(candidate.FrameIndex, candidate.Iter, candidate.MBRow, candidate.MBCol) {
			continue
		}
		emitOracleTraceRow(w, &state.interCandidateBuffer[i])
	}
	for i := range state.mbBuffer {
		emitOracleTraceRow(w, &state.mbBuffer[i])
	}
	state.interCandidateBuffer = state.interCandidateBuffer[:0]
	state.mbBuffer = state.mbBuffer[:0]
}

func (e *VP8Encoder) emitOracleInterCandidateTrace(summary oracleTraceInterCandidateSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	mv := summary.MV
	if summary.HasModeTrace {
		mv = summary.ModeTrace.MV
	}
	if summary.RefFrame == vp8common.IntraFrame || summary.Mode == vp8common.SplitMV {
		mv = vp8enc.MotionVector{}
	}
	outcome := summary.Outcome
	if outcome == "" {
		outcome = "tested"
	}
	row := oracleTraceInterCandidateRow{
		Type:       "inter_candidate",
		FrameIndex: e.frameCount,
		MBRow:      summary.MBRow,
		MBCol:      summary.MBCol,

		Picker:    summary.Picker,
		ModeIndex: summary.ModeIndex,
		Mode:      oracleTraceModeName(summary.Mode),
		RefSlot:   summary.RefSlot,
		RefFrame:  oracleTraceRefName(summary.RefFrame),

		Threshold:       summary.Threshold,
		BestScoreBefore: summary.BestScoreBefore,
		BestYRDBefore:   summary.BestYRDBefore,
		BestSSEBefore:   summary.BestSSEBefore,
		Outcome:         outcome,
		BecameBest:      summary.BecameBest,
		LoopBreak:       summary.LoopBreak,

		Score:        summary.Score,
		YRD:          summary.YRD,
		Rate:         summary.Rate,
		RateY:        summary.RateY,
		RateUV:       summary.RateUV,
		Distortion:   summary.Distortion,
		DistortionUV: summary.DistortionUV,
		SSE:          summary.SSE,
		Skip:         summary.Skip,

		MVRow: mv.Row,
		MVCol: mv.Col,

		ImprovedMVStart:        summary.ImprovedMVStart,
		ImprovedMVNearSADIndex: summary.ImprovedMVNearSADIndex,
		ImprovedMVRow:          summary.ImprovedMVRow,
		ImprovedMVCol:          summary.ImprovedMVCol,
		ImprovedMVSR:           summary.ImprovedMVSR,
	}
	if !summary.ImprovedMVStart {
		row.ImprovedMVNearSADIndex = oracleTraceInterCandidateUnknown
		row.ImprovedMVSR = oracleTraceInterCandidateUnknown
	}
	state := e.oracleTraceState()
	state.interCandidateBuffer = append(state.interCandidateBuffer, row)
}

// emitOracleMBTrace appends a per-macroblock trace row to the encoder's
// internal buffer. The row is flushed to the writer when the surrounding
// frame is committed; rows from intermediate (recoded) attempts are
// discarded by resetOracleMBTraceBuffer. mode and coeffs must reference the
// freshly written entries for (mbRow, mbCol). The caller already holds these
// values in govpx's per-MB inter loop, so this function performs no
// additional VP8 computation.
func (e *VP8Encoder) emitOracleMBTrace(
	mbRow int, mbCol int,
	mode *vp8enc.InterFrameMacroblockMode,
	coeffs *vp8enc.MacroblockCoefficients,
	improvedStart interFrameSearchStart,
	mbRate int, aggregatedRate int,
) {
	if !e.oracleTraceEnabled() || mode == nil || coeffs == nil {
		return
	}
	activity, actZbinAdj, rdmult, activityAvg := e.oracleTraceActivityState(mbRow, mbCol)
	row := oracleTraceMBRow{
		Type:       "mb",
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		SegmentID:  int(mode.SegmentID),
		Mode:       oracleTraceModeName(mode.Mode),
		RefFrame:   oracleTraceRefName(mode.RefFrame),
		MVRow:      mode.MV.Row,
		MVCol:      mode.MV.Col,
		Skip:       mode.MBSkipCoeff,

		ImprovedMVNearSADIndex: -1,
		ImprovedMVSR:           -1,

		MBRate:         mbRate,
		AggregatedRate: aggregatedRate,

		MBActivity:  activity,
		ActZbinAdj:  actZbinAdj,
		RDMult:      rdmult,
		ActivityAvg: activityAvg,
	}
	if improvedStart.ok() {
		row.ImprovedMVStart = true
		row.ImprovedMVNearSADIndex = improvedStart.nearSADIndexInt()
		row.ImprovedMVRow = improvedStart.mv.Row
		row.ImprovedMVCol = improvedStart.mv.Col
		row.ImprovedMVSR = improvedStart.searchRange()
	}
	if mode.Mode == vp8common.SplitMV {
		partition := int(mode.Partition)
		row.Partition = &partition
		row.BlockMVRow = make([]int16, len(mode.BlockMV))
		row.BlockMVCol = make([]int16, len(mode.BlockMV))
		for i := range mode.BlockMV {
			row.BlockMVRow[i] = mode.BlockMV[i].Row
			row.BlockMVCol[i] = mode.BlockMV[i].Col
		}
	}
	if mode.RefFrame == vp8common.IntraFrame && mode.Mode == vp8common.BPred {
		// Mirror the libvpx oracle dump: emit per-sub-block intra mode picks
		// for inter-frame B_PRED MBs so parity tooling can compare 4x4 picks.
		row.BModes = make([]string, len(mode.BModes))
		for i, bMode := range mode.BModes {
			row.BModes[i] = oracleTraceBModeName(bMode)
		}
	}
	sum := 0
	for i := range 25 {
		row.EOB[i] = coeffs.EOB[i]
		row.QCoeff[i] = coeffs.QCoeff[i]
	}
	is4x4 := false
	if mode.RefFrame != vp8common.IntraFrame {
		is4x4 = mode.Mode == vp8common.SplitMV
	} else {
		is4x4 = mode.Mode == vp8common.BPred
	}
	segID := int(mode.SegmentID)
	if uint(segID) < uint(len(e.dequants)) {
		applyOracleEOBAdjust(coeffs, &e.dequants[segID].Y2, is4x4, &row.EOB)
	}
	if is4x4 && coeffs.OracleStaleY2Set {
		// libvpx's vp8_quantize_mb skips block 24 for SPLITMV/B_PRED,
		// so xd->block[24].qcoeff/eobs[24] retain stale data from the
		// last RD-pick mode that quantized Y2. Mirror that trace-only
		// contribution without modifying the actual encoder block-24 state.
		row.EOB[24] = coeffs.OracleStaleY2EOB
		row.QCoeff[24] = coeffs.OracleStaleY2QCoeff
	}
	for i := range 25 {
		sum += int(row.EOB[i])
	}
	row.EOBSum = sum
	state := e.oracleTraceState()
	state.mbBuffer = append(state.mbBuffer, row)
}

func (e *VP8Encoder) emitOracleKeyFrameMBTrace(
	mbRow int, mbCol int,
	mode *vp8enc.KeyFrameMacroblockMode,
	coeffs *vp8enc.MacroblockCoefficients,
	mbRate int, aggregatedRate int,
) {
	if !e.oracleTraceEnabled() || mode == nil || coeffs == nil {
		return
	}
	activity, actZbinAdj, rdmult, activityAvg := e.oracleTraceActivityState(mbRow, mbCol)
	row := oracleTraceMBRow{
		Type:       "mb",
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		SegmentID:  int(mode.SegmentID),
		Mode:       oracleTraceModeName(mode.YMode),
		RefFrame:   oracleTraceRefName(vp8common.IntraFrame),
		UVMode:     oracleTraceModeName(mode.UVMode),

		ImprovedMVNearSADIndex: -1,
		ImprovedMVSR:           -1,

		MBRate:         mbRate,
		AggregatedRate: aggregatedRate,

		MBActivity:  activity,
		ActZbinAdj:  actZbinAdj,
		RDMult:      rdmult,
		ActivityAvg: activityAvg,
	}
	if mode.YMode == vp8common.BPred {
		row.BModes = make([]string, len(mode.BModes))
		for i, bMode := range mode.BModes {
			row.BModes[i] = oracleTraceBModeName(bMode)
		}
	}
	sum := 0
	for i := range 25 {
		row.EOB[i] = coeffs.EOB[i]
		row.QCoeff[i] = coeffs.QCoeff[i]
	}
	is4x4 := mode.YMode == vp8common.BPred
	segID := int(mode.SegmentID)
	if uint(segID) < uint(len(e.dequants)) {
		applyOracleEOBAdjust(coeffs, &e.dequants[segID].Y2, is4x4, &row.EOB)
	}
	if is4x4 && coeffs.OracleStaleY2Set {
		row.EOB[24] = coeffs.OracleStaleY2EOB
		row.QCoeff[24] = coeffs.OracleStaleY2QCoeff
	}
	for i := range 25 {
		sum += int(row.EOB[i])
	}
	row.EOBSum = sum
	state := e.oracleTraceState()
	state.mbBuffer = append(state.mbBuffer, row)
}

// applyOracleEOBAdjust mirrors libvpx's per-Y-block eob bump for the per-MB
// oracle trace. There are two libvpx code paths that can leave eob=1 with
// an all-zero qcoeff[0] in xd->eobs / xd->block[i].qcoeff at oracle-capture
// time:
//
//  1. vp8_quantize_mb runs vp8_fast_quantize_b_c (or
//     vp8_regular_quantize_b_c) on the Y block with the original (un-zeroed)
//     dct[0] against Y1's zbin/round/quant. If that DC quantizes to
//     non-zero, *d->eob is set to 1 even when every other position is zero.
//     vp8_dequant_idct_add_y_block later memsets qcoeff[0..1] back to zero,
//     but eob=1 survives. govpx tracks the would-have-been bit per Y block
//     in coeffs.OracleY1DCEOB1[block].
//
//  2. vp8_inverse_transform_mby runs the inverse Walsh on the Y2 block,
//     writing a per-Y-block DC value into xd->qcoeff[i*16]. eob_adjust then
//     bumps eobs[i] from 0 to 1 if that DC is non-zero, so the IDCT path
//     doesn't skip the block. The same memset clears qcoeff[0..1] later.
//
// The adjustment is purely cosmetic for the trace (bitstream tokenize,
// reconstruction, and the parity decoder all already handle the eob=0 vs
// eob=1 distinction correctly because the qcoeff payload is identical). It
// only happens when the macroblock has a Y2 second-order block (i.e. the
// non-4x4 / non-SPLITMV / non-B_PRED case).
//
// y2Dequant is the segment-specific Y2 dequant table (cpi->common.Y2dequant
// in libvpx). is4x4 mirrors libvpx's `mode != SPLITMV` (or `mode != B_PRED`
// for keyframes) gate that skips the eob_adjust.
func applyOracleEOBAdjust(coeffs *vp8enc.MacroblockCoefficients, y2Dequant *[16]int16, is4x4 bool, eob *[25]uint8) {
	if coeffs == nil || y2Dequant == nil || eob == nil || is4x4 {
		return
	}
	// Path 1: bump from libvpx Y1 quantize on the original dct[0] of each
	// Y block. coeffs.OracleY1DCEOB1[block] was populated at quantize time
	// from the same dct[0] that fed the Y2 forward Walsh.
	for js := range 16 {
		if eob[js] == 0 && coeffs.OracleY1DCEOB1[js] != 0 {
			eob[js] = 1
		}
	}
	// Path 2: bump from libvpx eob_adjust against the inverse-Walsh DC of
	// the Y2 block. This is the residual case where the post-Walsh DC is
	// non-zero even though Y1 quantize produced zero.
	var y2DQ [16]int16
	for i := range 16 {
		y2DQ[i] = int16(int(coeffs.QCoeff[24][i]) * int(y2Dequant[i]))
	}
	var dcSlots [16 * 16]int16
	if eob[24] > 1 {
		dsp.InverseWalsh4x4(&y2DQ, dcSlots[:])
	} else {
		dsp.DCOnlyInverseWalsh4x4(y2DQ[0], dcSlots[:])
	}
	for js := range 16 {
		if eob[js] == 0 && dcSlots[js*16] != 0 {
			eob[js] = 1
		}
	}
}

// oracleTraceActivityState mirrors libvpx v1.16.0 vp8/encoder/encodeframe.c
// per-MB TuneSSIM activity-masking snapshot at the govpx_oracle_capture_mb
// call site. Returns:
//
//   - mb_activity : cpi->mb_activity_map[idx]. Zero outside TuneSSIM (the
//     map is allocated via vpx_calloc inside vp8_alloc_compressor_data
//     at onyx_if.c:1188-1192 and never written outside build_activity_map).
//   - act_zbin_adj: x->act_zbin_adj. Zero outside TuneSSIM (initialized at
//     encodeframe.c:588 and only re-derived under tuning==SSIM at
//     encodeframe.c:1106 / 1193 via adjust_act_zbin).
//   - rdmult     : x->rdmult after vp8_activity_masking (line 307). Outside
//     TuneSSIM this equals cpi->RDMULT (the base reassignment at
//     encodeframe.c:406 with no subsequent activity scaling).
//   - activity_avg: cpi->activity_avg. Defaults to 90<<12 from
//     vp8_create_compressor (onyx_if.c:1906) and is overwritten by
//     calc_av_activity (encodeframe.c:156 / 164) when build_activity_map
//     runs under TuneSSIM.
//
// The base qindex for the rdmult derivation is e.interRDFrameBaseQIndex,
// which mirrors libvpx's vp8_initialize_rd_consts(cm->base_qindex) seed
// (rdopt.c:163-227 / encodeframe.c:721-722).
func (e *VP8Encoder) oracleTraceActivityState(mbRow int, mbCol int) (mbActivity uint32, actZbinAdj int, rdmult uint32, activityAvg uint32) {
	// libvpx onyx_if.c:1906 vp8_create_compressor seeds cpi->activity_avg
	// at 90<<12. calc_av_activity overwrites it under TuneSSIM only.
	const libvpxActivityAvgDefault uint32 = 90 << 12
	activityAvg = libvpxActivityAvgDefault
	if e.activityMapValid {
		// e.activityAvg mirrors the post-calc_av_activity value (either the
		// median path or the fixed 100000 under ALT_ACT_MEASURE=1).
		activityAvg = e.activityAvg
		if activity, ok := e.activityAt(mbRow, mbCol); ok {
			mbActivity = activity
		}
		if adj, ok := e.tunedZbinAdjustment(mbRow, mbCol); ok {
			actZbinAdj = adj
		}
	}
	// Base cpi->RDMULT seed: vp8_initialize_rd_consts uses cm->base_qindex
	// (rdopt.c:163-227, called from encodeframe.c:721-722). For TuneSSIM
	// the per-MB x->rdmult is then scaled by vp8_activity_masking
	// (encodeframe.c:307). PSNR tuning keeps x->rdmult == cpi->RDMULT.
	//
	// The qindex to consume is the *frame-level* base qindex active at
	// encode_mb_row time. govpx tracks the regulator-chosen value in
	// e.rc.currentQuantizer, which is set per-frame (keyframe or inter)
	// before the macroblock loop runs. e.interRDFrameBaseQIndex is the
	// inter-only snapshot taken at beginInterRDModeDecisionFrame and is
	// stale on keyframes (initialized to 0 by vp8_encoder_lifecycle.go:182).
	qIndex := vp8common.ClampQIndex(e.rc.currentQuantizer)
	baseRDMult, _ := vp8enc.RDConstants(qIndex)
	rdmultInt := baseRDMult
	if e.activityMapValid {
		rdmultInt = e.tunedRDMultiplier(baseRDMult, mbRow, mbCol)
	}
	if rdmultInt < 0 {
		rdmultInt = 0
	}
	rdmult = uint32(rdmultInt)
	return mbActivity, actZbinAdj, rdmult, activityAvg
}

// emitOracleLFTrial writes a single per-trial-level row for the fast
// loop-filter picker. Each call corresponds to one libvpx-side
// calc_partial_ssl_err invocation inside vp8cx_pick_filter_level_fast,
// at one of three phases: "seed" (initial cm->filter_level scoring),
// "down" (decreasing-level loop body), "up" (increasing-level loop
// body). The libvpx-side oracle patch in
// internal/coracle/build_vpxenc_oracle.sh emits the matching row from
// govpx_oracle_emit_lf_trial after each calc_partial_ssl_err call.
