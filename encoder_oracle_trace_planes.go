package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

func (e *VP8Encoder) emitOracleLFTrial(phase string, trialLevel int, trialYSSE int) {
	if !e.oracleTraceEnabled() {
		return
	}
	emitOracleTraceRow(e.opts.OracleTraceWriter, &oracleTraceLFTrialRow{
		Type:       "lf_trial",
		FrameIndex: e.frameCount,
		Phase:      phase,
		TrialLevel: trialLevel,
		TrialYSSE:  trialYSSE,
	})
}

// emitOracleInterPredictorTrace writes "predictor" rows for the supplied
// macroblock's Y/U/V predictor planes, encoded as ASCII hex. Emission is
// gated by EncoderOptions.OracleTracePredictorDump so the regular oracle
// trace stream stays compact; when enabled the writer receives one row per
// plane keyed by (frame_index, mb_row, mb_col, plane). Mirrors the
// libvpx-side `govpx_oracle_emit_predictor` C helper in
// internal/coracle/build_vpxenc_oracle.sh which captures
// `xd->dst.{y,u,v}_buffer` between `vp8_encode_inter16x16` and
// `vp8_inverse_transform_mby`. govpx captures the same value via
// `reconstructInterAnalysisMacroblock(MBSkipCoeff=1)` which writes only the
// predictor into the analysis image.
func (e *VP8Encoder) emitOracleInterPredictorTrace(mbRow int, mbCol int, img *vp8common.Image) {
	e.emitOracleInterMBPlanesTrace("predictor", mbRow, mbCol, img)
}

// emitOracleInterReconstructedTrace mirrors emitOracleInterPredictorTrace
// but captures the post-residual-add buffer (i.e. the final reconstructed
// MB output that becomes part of the LAST reference for the next frame).
// The libvpx-side counterpart lives at the tail of
// vp8cx_encode_inter_macroblock, after vp8_dequant_idct_add_uv_block / the
// invtrans_mby step.
func (e *VP8Encoder) emitOracleInterReconstructedTrace(mbRow int, mbCol int, img *vp8common.Image) {
	e.emitOracleInterMBPlanesTrace("reconstructed", mbRow, mbCol, img)
}

// emitOracleLastRefWindow writes "last_ref_window" rows capturing the
// LAST reference's Y/U/V planes including the border bytes the chroma
// sub-pel filter taps reach for MB row 0. Mirrors the libvpx-side
// `govpx_oracle_emit_last_ref_window` helper. Called once per inter
// frame, before the first MB is encoded, to localize whether border
// content matches between encoders.
func (e *VP8Encoder) emitOracleLastRefWindow(ref *vp8common.Image) {
	if !e.oracleTraceEnabled() || !e.opts.OracleTracePredictorDump || ref == nil {
		return
	}
	w := e.opts.OracleTraceWriter
	border := ref.YBorder
	uvBorder := ref.UVBorder
	yWindowH := border + 16
	uvWindowH := uvBorder + 8
	yWindowW := border + ref.CodedWidth
	uvWindowW := uvBorder + (ref.CodedWidth+1)>>1
	// Step back by border rows and border columns to reach top-left of
	// captured window.
	yStart := ref.YOrigin - border*ref.YStride - border
	uStart := ref.UOrigin - uvBorder*ref.UStride - uvBorder
	vStart := ref.VOrigin - uvBorder*ref.VStride - uvBorder
	if yStart < 0 || uStart < 0 || vStart < 0 {
		return
	}
	emitOracleTraceRow(w, &oracleTraceLastRefWindowRow{
		Type:       "last_ref_window",
		FrameIndex: e.frameCount,
		Plane:      "y",
		Width:      yWindowW,
		Height:     yWindowH,
		BorderTop:  border,
		BorderLeft: border,
		Hex:        oracleTraceHexEncodePlane(ref.YFull[yStart:], yWindowW, yWindowH, ref.YStride),
	})
	emitOracleTraceRow(w, &oracleTraceLastRefWindowRow{
		Type:       "last_ref_window",
		FrameIndex: e.frameCount,
		Plane:      "u",
		Width:      uvWindowW,
		Height:     uvWindowH,
		BorderTop:  uvBorder,
		BorderLeft: uvBorder,
		Hex:        oracleTraceHexEncodePlane(ref.UFull[uStart:], uvWindowW, uvWindowH, ref.UStride),
	})
	emitOracleTraceRow(w, &oracleTraceLastRefWindowRow{
		Type:       "last_ref_window",
		FrameIndex: e.frameCount,
		Plane:      "v",
		Width:      uvWindowW,
		Height:     uvWindowH,
		BorderTop:  uvBorder,
		BorderLeft: uvBorder,
		Hex:        oracleTraceHexEncodePlane(ref.VFull[vStart:], uvWindowW, uvWindowH, ref.VStride),
	})
}

func (e *VP8Encoder) emitOracleInterMBPlanesTrace(rowType string, mbRow int, mbCol int, img *vp8common.Image) {
	if !e.oracleTraceEnabled() || !e.opts.OracleTracePredictorDump || img == nil {
		return
	}
	if !e.opts.OracleTracePredictorDumpAllRows && mbRow != 0 {
		// Default scope: row 0 only (8 MBs at 128 px wide). Set
		// EncoderOptions.OracleTracePredictorDumpAllRows to capture
		// every row when tracking down a divergence beyond row 0
		// (e.g., the partial-frame loop-filter trial reads MB row
		// rows/2). The libvpx-side helper applies the same gate via
		// GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS so the captured key
		// sets line up.
		return
	}
	w := e.opts.OracleTraceWriter
	yOff := mbRow*16*img.YStride + mbCol*16
	uOff := mbRow*8*img.UStride + mbCol*8
	vOff := mbRow*8*img.VStride + mbCol*8
	emitOracleTraceRow(w, &oracleTracePredictorRow{
		Type:       rowType,
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Plane:      "y",
		Width:      16,
		Height:     16,
		Hex:        oracleTraceHexEncodePlane(img.Y[yOff:], 16, 16, img.YStride),
	})
	emitOracleTraceRow(w, &oracleTracePredictorRow{
		Type:       rowType,
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Plane:      "u",
		Width:      8,
		Height:     8,
		Hex:        oracleTraceHexEncodePlane(img.U[uOff:], 8, 8, img.UStride),
	})
	emitOracleTraceRow(w, &oracleTracePredictorRow{
		Type:       rowType,
		FrameIndex: e.frameCount,
		MBRow:      mbRow,
		MBCol:      mbCol,
		Plane:      "v",
		Width:      8,
		Height:     8,
		Hex:        oracleTraceHexEncodePlane(img.V[vOff:], 8, 8, img.VStride),
	})
}

// oracleTraceHexEncodePlane returns a width*height-byte ASCII-hex
// (lowercase) encoding of a plane region. Matches the C-side
// govpx_oracle_emit_plane_hex helper exactly so the resulting JSON rows
// are byte-comparable across encoders.
