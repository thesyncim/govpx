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
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
	e.oracleTraceInterCandidateBuffer = e.oracleTraceInterCandidateBuffer[:0]
}

// flushOracleMBTraceBuffer writes the buffered per-MB rows to the configured
// writer in scan order and clears the buffer.
func (e *VP8Encoder) flushOracleMBTraceBuffer() {
	if !e.oracleTraceEnabled() {
		return
	}
	w := e.opts.OracleTraceWriter
	for i := range e.oracleTraceInterCandidateBuffer {
		emitOracleTraceRow(w, &e.oracleTraceInterCandidateBuffer[i])
	}
	for i := range e.oracleTraceMBBuffer {
		emitOracleTraceRow(w, &e.oracleTraceMBBuffer[i])
	}
	e.oracleTraceInterCandidateBuffer = e.oracleTraceInterCandidateBuffer[:0]
	e.oracleTraceMBBuffer = e.oracleTraceMBBuffer[:0]
}

func (e *VP8Encoder) emitOracleInterCandidateTrace(summary oracleTraceInterCandidateSummary) {
	if !e.oracleTraceEnabled() {
		return
	}
	mv := summary.MV
	improvedMVNearSADIndex := oracleTraceInterCandidateUnknown
	improvedMVSR := oracleTraceInterCandidateUnknown
	var improvedMVPredictor vp8enc.MotionVector
	if summary.HasModeTrace {
		mv = summary.ModeTrace.MV
		if summary.ModeTrace.ImprovedMVStart {
			improvedMVNearSADIndex = int(summary.ModeTrace.ImprovedMVNearSADIndex)
			improvedMVSR = int(summary.ModeTrace.ImprovedMVSR)
			improvedMVPredictor = summary.ModeTrace.ImprovedMVPredictor
		}
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

		ImprovedMVStart:        summary.HasModeTrace && summary.ModeTrace.ImprovedMVStart,
		ImprovedMVNearSADIndex: improvedMVNearSADIndex,
		ImprovedMVRow:          improvedMVPredictor.Row,
		ImprovedMVCol:          improvedMVPredictor.Col,
		ImprovedMVSR:           improvedMVSR,
	}
	e.oracleTraceInterCandidateBuffer = append(e.oracleTraceInterCandidateBuffer, row)
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
	mbRate int, aggregatedRate int,
) {
	if !e.oracleTraceEnabled() || mode == nil || coeffs == nil {
		return
	}
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
	}
	if mode.ImprovedMVStart {
		row.ImprovedMVStart = true
		row.ImprovedMVNearSADIndex = int(mode.ImprovedMVNearSADIndex)
		row.ImprovedMVRow = mode.ImprovedMVPredictor.Row
		row.ImprovedMVCol = mode.ImprovedMVPredictor.Col
		row.ImprovedMVSR = int(mode.ImprovedMVSR)
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
		// for inter-frame B_PRED MBs so the R11-J / R12-C diagnostic can
		// compare 4x4 picks at the col-7 right-edge MBs on 128x128 frame 1.
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
	if segID >= 0 && segID < len(e.dequants) {
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
	e.oracleTraceMBBuffer = append(e.oracleTraceMBBuffer, row)
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
	if segID >= 0 && segID < len(e.dequants) {
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
	e.oracleTraceMBBuffer = append(e.oracleTraceMBBuffer, row)
}

// applyOracleEOBAdjust mirrors libvpx's per-Y-block eob bump for the per-MB
// oracle trace. There are two libvpx code paths that can leave eob=1 with
// an all-zero qcoeff[0] in xd->eobs / xd->block[i].qcoeff at oracle-capture
// time:
//
//  1. vp8_quantize_mb runs vp8_fast_quantize_b_c (or
//     vp8_regular_quantize_b_c) on the Y block with the original (un-zeroed)
//     dct[0] against Y1DC's zbin/round/quant. If that DC quantizes to
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
	// Path 1: bump from libvpx Y1DC quantize on the original dct[0] of each
	// Y block. coeffs.OracleY1DCEOB1[block] was populated at quantize time
	// from the same dct[0] that fed the Y2 forward Walsh.
	for js := range 16 {
		if eob[js] == 0 && coeffs.OracleY1DCEOB1[js] != 0 {
			eob[js] = 1
		}
	}
	// Path 2: bump from libvpx eob_adjust against the inverse-Walsh DC of
	// the Y2 block. This is the residual case where the post-Walsh DC is
	// non-zero even though Y1DC quantize produced zero.
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

// emitOracleLFTrial writes a single per-trial-level row for the fast
// loop-filter picker. Each call corresponds to one libvpx-side
// calc_partial_ssl_err invocation inside vp8cx_pick_filter_level_fast,
// at one of three phases: "seed" (initial cm->filter_level scoring),
// "down" (decreasing-level loop body), "up" (increasing-level loop
// body). The libvpx-side oracle patch in
// internal/coracle/build_vpxenc_oracle.sh emits the matching row from
// govpx_oracle_emit_lf_trial after each calc_partial_ssl_err call.
