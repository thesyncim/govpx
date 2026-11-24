//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestVP8OracleTraceRecodeIterEmitsInterCandidates(t *testing.T) {
	requireOracleTraceBuild(t)
	var buf bytes.Buffer
	e := &VP8Encoder{frameCount: 7}
	e.SetOracleTraceWriter(&buf)
	e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
		Picker:          "rd",
		MBRow:           2,
		MBCol:           3,
		ModeIndex:       1,
		Mode:            vp8common.DCPred,
		RefFrame:        vp8common.IntraFrame,
		Threshold:       0,
		BestScoreBefore: 123,
		BestYRDBefore:   456,
		BestSSEBefore:   789,
		Score:           42,
		YRD:             40,
		Rate:            12,
		RateY:           10,
		RateUV:          2,
		Distortion:      30,
		DistortionUV:    4,
		SSE:             35,
	})
	e.emitOracleRecodeIterTrace(oracleTraceRecodeIterSummary{Iter: 23, Q: 94})
	e.flushOracleMBTraceBuffer()

	lines := splitNonEmptyLines(buf.Bytes())
	if len(lines) != 2 {
		t.Fatalf("trace rows = %d, want recode_iter + one inter_candidate\n%s", len(lines), buf.String())
	}
	var candidate map[string]any
	if err := json.Unmarshal(lines[1], &candidate); err != nil {
		t.Fatalf("candidate row invalid JSON: %v\n%s", err, lines[1])
	}
	if got := candidate["type"]; got != "inter_candidate" {
		t.Fatalf("row[1].type = %v, want inter_candidate", got)
	}
	if got := candidate["iter"]; got != float64(23) {
		t.Fatalf("candidate.iter = %v, want 23", got)
	}
	if got := candidate["q"]; got != float64(94) {
		t.Fatalf("candidate.q = %v, want 94", got)
	}
	if got := candidate["mb_row"]; got != float64(2) {
		t.Fatalf("candidate.mb_row = %v, want 2", got)
	}
	if got := candidate["mode"]; got != "DC_PRED" {
		t.Fatalf("candidate.mode = %v, want DC_PRED", got)
	}
}

func TestVP8OracleTracePickerUVQuantizeRow(t *testing.T) {
	requireOracleTraceBuild(t)
	var buf bytes.Buffer
	e := &VP8Encoder{}
	e.SetOracleTraceWriter(&buf)
	e.SetOracleTracePickerUVQuantizeDump(true)
	e.incrementOracleTraceRecodeLoop()
	e.rc.currentQuantizer = 94

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.GoldenFrame,
		Mode:     vp8common.ZeroMV,
		MV:       vp8enc.MotionVector{Row: -4, Col: 8},
	}
	var coeff, qcoeff, dqcoeff [16]int16
	coeff[0] = 23
	qcoeff[0] = 1
	dqcoeff[0] = 149
	var quant vp8enc.BlockQuant
	quant.Zbin[0] = 55
	quant.Round[0] = 33
	quant.Quant[0] = -17873
	quant.QuantShift[0] = 1024
	quant.ZbinBoost[2] = 9
	quant.Dequant[0] = 149

	e.emitOraclePickerUVQuantizeTrace(5, 2, 16, &mode, "regular", &coeff, &qcoeff, &dqcoeff, &quant, 1, 13, 0)

	lines := splitNonEmptyLines(buf.Bytes())
	if len(lines) != 1 {
		t.Fatalf("trace rows = %d, want 1", len(lines))
	}
	var row map[string]any
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("trace row invalid JSON: %v", err)
	}
	if row["type"] != "picker_uv_quantize" || row["iter"] != float64(1) || row["q"] != float64(94) {
		t.Fatalf("picker UV row header = %#v", row)
	}
	if row["mode"] != "ZEROMV" || row["ref_frame"] != "GOLDEN_FRAME" || row["quant_path"] != "regular" {
		t.Fatalf("picker UV row mode/ref/path = %v/%v/%v", row["mode"], row["ref_frame"], row["quant_path"])
	}
	if row["zbin_extra"] != float64(13) {
		t.Fatalf("zbin_extra = %v, want 13", row["zbin_extra"])
	}
	q := row["qcoeff"].([]any)
	if q[0] != float64(1) {
		t.Fatalf("qcoeff[0] = %v, want 1", q[0])
	}
}

func TestVP8OracleTraceCoefficientFactoriesRequireActiveWriter(t *testing.T) {
	requireOracleTraceBuild(t)

	e := &VP8Encoder{}
	e.SetOracleTracePretrellisUVDump(true)
	e.SetOracleTraceChromaOptimizeBDump(true)
	e.SetOracleTracePickerUVQuantizeDump(true)

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame: vp8common.GoldenFrame,
		Mode:     vp8common.NewMV,
		MV:       vp8enc.MotionVector{Row: -2, Col: 3},
	}

	pre := newPretrellisUVTrace(e)
	if pre.pretrellisUVTrace != nil {
		t.Fatalf("newPretrellisUVTrace retained encoder without an active writer")
	}
	pick := newPickerUVQuantizeTrace(e, &mode)
	if pick.pickerUVQuantizeTrace != nil {
		t.Fatalf("newPickerUVQuantizeTrace retained encoder without an active writer")
	}
	if pick.pickerUVQuantizeMode.Mode != 0 || pick.pickerUVQuantizeMode.RefFrame != 0 || pick.pickerUVQuantizeMode.MV != (vp8enc.MotionVector{}) {
		t.Fatalf("newPickerUVQuantizeTrace copied mode without an active writer: %+v", pick.pickerUVQuantizeMode)
	}

	var buf bytes.Buffer
	e.SetOracleTraceWriter(&buf)
	e.SetOracleTracePretrellisUVDump(true)
	e.SetOracleTraceChromaOptimizeBDump(true)
	e.SetOracleTracePickerUVQuantizeDump(true)
	pre = newPretrellisUVTrace(e)
	if pre.pretrellisUVTrace != e {
		t.Fatalf("newPretrellisUVTrace did not retain encoder with active UV dumps")
	}
	pick = newPickerUVQuantizeTrace(e, &mode)
	if pick.pickerUVQuantizeTrace != e {
		t.Fatalf("newPickerUVQuantizeTrace did not retain encoder with active picker dump")
	}
	if pick.pickerUVQuantizeMode.Mode != mode.Mode || pick.pickerUVQuantizeMode.RefFrame != mode.RefFrame || pick.pickerUVQuantizeMode.MV != mode.MV {
		t.Fatalf("newPickerUVQuantizeTrace copied mode = %+v, want %+v", pick.pickerUVQuantizeMode, mode)
	}
}

func TestVP8OracleTraceInterCandidateFilterScopesIterRows(t *testing.T) {
	requireOracleTraceBuild(t)
	t.Setenv("GOVPX_ORACLE_INTER_CANDIDATE_FRAME", "7")
	t.Setenv("GOVPX_ORACLE_INTER_CANDIDATE_ITER", "23")
	t.Setenv("GOVPX_ORACLE_INTER_CANDIDATE_MB_ROW", "2")
	t.Setenv("GOVPX_ORACLE_INTER_CANDIDATE_MB_COL", "3")

	var buf bytes.Buffer
	e := &VP8Encoder{frameCount: 7}
	e.SetOracleTraceWriter(&buf)
	for _, col := range []int{3, 4} {
		e.emitOracleInterCandidateTrace(oracleTraceInterCandidateSummary{
			Picker:    "rd",
			MBRow:     2,
			MBCol:     col,
			ModeIndex: 1,
			Mode:      vp8common.DCPred,
			RefFrame:  vp8common.IntraFrame,
		})
	}
	e.emitOracleRecodeIterTrace(oracleTraceRecodeIterSummary{Iter: 23, Q: 94})

	candidateRows := 0
	for i, line := range splitNonEmptyLines(buf.Bytes()) {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("trace line %d invalid JSON: %v", i, err)
		}
		if row["type"] != "inter_candidate" {
			continue
		}
		candidateRows++
		if row["mb_col"] != float64(3) {
			t.Fatalf("filtered candidate mb_col = %v, want only col 3", row["mb_col"])
		}
		if row["iter"] != float64(23) || row["q"] != float64(94) {
			t.Fatalf("filtered candidate iter/q = %v/%v, want 23/94", row["iter"], row["q"])
		}
	}
	if candidateRows != 1 {
		t.Fatalf("candidate rows = %d, want exactly one filtered row\n%s", candidateRows, buf.String())
	}
}
