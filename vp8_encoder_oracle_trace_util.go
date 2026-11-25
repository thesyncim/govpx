//go:build govpx_oracle_trace

package govpx

import (
	"encoding/json"
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"hash/adler32"
	"io"
)

type staleY2Snapshot struct {
	set    bool
	eob    uint8
	qcoeff [16]int16
}

func makeOracleStaleY2Snapshot(eob uint8, qcoeff [16]int16) staleY2Snapshot {
	return staleY2Snapshot{set: true, eob: eob, qcoeff: qcoeff}
}

func oracleStaleY2SnapshotSet(snapshot staleY2Snapshot) bool {
	return snapshot.set
}

func applyOracleStaleY2Snapshot(coeffs *vp8enc.MacroblockCoefficients, snapshot staleY2Snapshot) {
	if coeffs == nil || !snapshot.set {
		return
	}
	coeffs.OracleStaleY2Set = true
	coeffs.OracleStaleY2EOB = snapshot.eob
	coeffs.OracleStaleY2QCoeff = snapshot.qcoeff
}

func recordOracleY1DCEOB1(coeffs *vp8enc.MacroblockCoefficients, block int, value uint8) {
	if coeffs == nil || uint(block) >= uint(len(coeffs.OracleY1DCEOB1)) {
		return
	}
	coeffs.OracleY1DCEOB1[block] = value
}

func recordOracleStaleY2(coeffs *vp8enc.MacroblockCoefficients, eob uint8, qcoeff [16]int16) {
	if coeffs == nil {
		return
	}
	coeffs.OracleStaleY2EOB = eob
	coeffs.OracleStaleY2QCoeff = qcoeff
	coeffs.OracleStaleY2Set = true
}

func libvpxY1DCWouldQuantizeNonzero(dct0 int16, quant *vp8enc.BlockQuant, zbinOverQuant int, zbinModeBoost int, actZbinAdj int, fastQuant bool) uint8 {
	if quant == nil {
		return 0
	}
	z := int(dct0)
	if z == 0 {
		return 0
	}
	mask := z >> mvKernelSignShift
	x := (z ^ mask) - mask
	if fastQuant {
		y := ((x + int(quant.Round[0])) * int(quant.QuantFast[0])) >> 16
		if y != 0 {
			return 1
		}
		return 0
	}
	zbin := int(quant.Zbin[0])
	zbin += int(quant.ZbinBoost[0])
	zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost + actZbinAdj)) >> 7
	if x < zbin {
		return 0
	}
	x += int(quant.Round[0])
	y := ((((x * int(quant.Quant[0])) >> 16) + x) * int(quant.QuantShift[0])) >> 16
	if y != 0 {
		return 1
	}
	return 0
}

func (e *VP8Encoder) emitOracleLFTrial(phase string, trialLevel int, trialYSSE int) {
	if !e.oracleTraceEnabled() {
		return
	}
	emitOracleTraceRow(e.oracleTraceState().writer, &oracleTraceLFTrialRow{
		Type:       "lf_trial",
		FrameIndex: e.frameCount,
		Phase:      phase,
		TrialLevel: trialLevel,
		TrialYSSE:  trialYSSE,
	})
}

// emitOracleInterPredictorTrace writes "predictor" rows for the supplied
// macroblock's Y/U/V predictor planes, encoded as ASCII hex. Emission is
// gated by SetOracleTracePredictorDump so the regular oracle trace stream
// stays compact; when enabled the writer receives one row per plane keyed
// by (frame_index, mb_row, mb_col, plane). Mirrors the
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
	if !e.oracleTraceEnabled() || ref == nil {
		return
	}
	state := e.oracleTraceState()
	if !state.predictorDump {
		return
	}
	w := state.writer
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
	if min(min(yStart, uStart), vStart) < 0 {
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
	if !e.oracleTraceEnabled() || img == nil {
		return
	}
	state := e.oracleTraceState()
	if !state.predictorDump {
		return
	}
	if !state.predictorDumpAllRows && mbRow != 0 {
		// Default scope: row 0 only (8 MBs at 128 px wide). Use
		// SetOracleTracePredictorDump(enabled, allRows=true) to capture
		// every row when tracking down a divergence beyond row 0. The
		// libvpx-side helper applies the same gate via
		// GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS so the captured key
		// sets line up.
		return
	}
	w := state.writer
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

func oracleTraceHexEncodePlane(plane []byte, width int, height int, stride int) string {
	const hex = "0123456789abcdef"
	if min(min(width, height), stride) <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*width*height)
	for row := range height {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		for _, b := range plane[start:end] {
			out = append(out, hex[(b>>4)&0xf], hex[b&0xf])
		}
	}
	return string(out)
}

// emitOracleTraceRow marshals a row to JSON, appends a newline, and writes a
// single payload to the configured writer. Marshal errors are silently
// ignored to avoid disturbing the encode path; the trace is a debugging aid.
func emitOracleTraceRow(w io.Writer, row any) {
	if w == nil {
		return
	}
	buf, err := json.Marshal(row)
	if err != nil {
		return
	}
	buf = append(buf, '\n')
	_, _ = w.Write(buf)
}

// oracleTraceReferenceChecksums computes Adler32 checksums over the visible
// region of the supplied reconstruction image (Y/U/V planes). Adler32 is
// chosen because it is cheap, deterministic, available in the standard
// library, and aligns with libvpx's existing checksum tooling.
func oracleTraceReferenceChecksums(img *vp8common.Image) (uint32, uint32, uint32) {
	if img == nil {
		return 0, 0, 0
	}
	yChecksum := planeAdler32(img.Y, img.Width, img.Height, img.YStride)
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	uChecksum := planeAdler32(img.U, uvWidth, uvHeight, img.UStride)
	vChecksum := planeAdler32(img.V, uvWidth, uvHeight, img.VStride)
	return yChecksum, uChecksum, vChecksum
}

func planeAdler32(plane []byte, width int, height int, stride int) uint32 {
	if min(min(width, height), stride) <= 0 {
		return 0
	}
	h := adler32.New()
	for row := range height {
		start := row * stride
		end := start + width
		if end > len(plane) {
			break
		}
		_, _ = h.Write(plane[start:end])
	}
	return h.Sum32()
}

func oracleTraceModeName(mode vp8common.MBPredictionMode) string {
	switch mode {
	case vp8common.DCPred:
		return "DC_PRED"
	case vp8common.VPred:
		return "V_PRED"
	case vp8common.HPred:
		return "H_PRED"
	case vp8common.TMPred:
		return "TM_PRED"
	case vp8common.BPred:
		return "B_PRED"
	case vp8common.NearestMV:
		return "NEARESTMV"
	case vp8common.NearMV:
		return "NEARMV"
	case vp8common.ZeroMV:
		return "ZEROMV"
	case vp8common.NewMV:
		return "NEWMV"
	case vp8common.SplitMV:
		return "SPLITMV"
	default:
		return fmt.Sprintf("MODE_%d", int(mode))
	}
}

func oracleTraceBModeName(mode vp8common.BPredictionMode) string {
	switch mode {
	case vp8common.BDCPred:
		return "B_DC_PRED"
	case vp8common.BTMPred:
		return "B_TM_PRED"
	case vp8common.BVEPred:
		return "B_VE_PRED"
	case vp8common.BHEPred:
		return "B_HE_PRED"
	case vp8common.BLDPred:
		return "B_LD_PRED"
	case vp8common.BRDPred:
		return "B_RD_PRED"
	case vp8common.BVRPred:
		return "B_VR_PRED"
	case vp8common.BVLPred:
		return "B_VL_PRED"
	case vp8common.BHDPred:
		return "B_HD_PRED"
	case vp8common.BHUPred:
		return "B_HU_PRED"
	case vp8common.Left4x4:
		return "LEFT4X4"
	case vp8common.Above4x4:
		return "ABOVE4X4"
	case vp8common.Zero4x4:
		return "ZERO4X4"
	case vp8common.New4x4:
		return "NEW4X4"
	default:
		return fmt.Sprintf("B_MODE_%d", int(mode))
	}
}

func oracleTraceRefName(ref vp8common.MVReferenceFrame) string {
	switch ref {
	case vp8common.IntraFrame:
		return "INTRA_FRAME"
	case vp8common.LastFrame:
		return "LAST_FRAME"
	case vp8common.GoldenFrame:
		return "GOLDEN_FRAME"
	case vp8common.AltRefFrame:
		return "ALTREF_FRAME"
	default:
		return fmt.Sprintf("REF_%d", int(ref))
	}
}
