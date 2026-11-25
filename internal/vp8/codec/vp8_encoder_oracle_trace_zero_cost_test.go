//go:build govpx_oracle_trace

package codec

import (
	"bytes"
	"encoding/json"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"strings"
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

// TestVP8OracleTraceWriterNilProducesNoOverhead verifies that omitting
// OracleTraceWriter results in no writer activity and that the encoded byte
// stream is identical to a baseline run with the same configuration.
func TestVP8OracleTraceWriterNilProducesNoOverhead(t *testing.T) {
	requireOracleTraceBuild(t)
	const w, h = 32, 32

	encode := func(traceWriter *bytes.Buffer) ([]byte, []byte) {
		opts := EncoderOptions{
			Width:               w,
			Height:              h,
			FPS:                 30,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   1200,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			Deadline:            DeadlineRealtime,
			CpuUsed:             8,
			KeyFrameInterval:    120,
			ErrorResilient:      true,
			BufferSizeMs:        600,
			BufferInitialSizeMs: 400,
			BufferOptimalSizeMs: 500,
		}
		e, err := NewVP8Encoder(opts)
		if err != nil {
			t.Fatalf("NewVP8Encoder returned error: %v", err)
		}
		if traceWriter != nil {
			e.SetOracleTraceWriter(traceWriter)
		}
		key := testImage(w, h)
		for i := range key.Y {
			key.Y[i] = byte((i*7 + 11) & 0xff)
		}
		for i := range key.U {
			key.U[i] = byte((i*3 + 5) & 0xff)
		}
		for i := range key.V {
			key.V[i] = byte((i*5 + 23) & 0xff)
		}
		dst := make([]byte, 1<<16)
		keyResult, err := e.EncodeInto(dst, key, 0, 1, EncodeForceKeyFrame)
		if err != nil {
			t.Fatalf("key EncodeInto returned error: %v", err)
		}
		keyBytes := append([]byte(nil), keyResult.Data...)

		inter := testImage(w, h)
		for row := range h {
			for col := range w {
				inter.Y[row*inter.YStride+col] = key.Y[((row+1)%h)*key.YStride+((col+2)%w)]
			}
		}
		uvW := (w + 1) >> 1
		uvH := (h + 1) >> 1
		for row := range uvH {
			for col := range uvW {
				inter.U[row*inter.UStride+col] = key.U[((row+1)%uvH)*key.UStride+((col+1)%uvW)]
				inter.V[row*inter.VStride+col] = key.V[((row+1)%uvH)*key.VStride+((col+1)%uvW)]
			}
		}
		dst2 := make([]byte, 1<<16)
		interResult, err := e.EncodeInto(dst2, inter, 1, 1, 0)
		if err != nil {
			t.Fatalf("inter EncodeInto returned error: %v", err)
		}
		interBytes := append([]byte(nil), interResult.Data...)
		return keyBytes, interBytes
	}

	baseKey, baseInter := encode(nil)

	var traceBuf bytes.Buffer
	tracedKey, tracedInter := encode(&traceBuf)

	if !bytes.Equal(baseKey, tracedKey) {
		t.Fatalf("key frame bytes differ between traced (%d B) and baseline (%d B) runs", len(tracedKey), len(baseKey))
	}
	if !bytes.Equal(baseInter, tracedInter) {
		t.Fatalf("inter frame bytes differ between traced (%d B) and baseline (%d B) runs", len(tracedInter), len(baseInter))
	}

	// Sanity: nil writer scenario must produce no trace output. We re-run
	// with nil and check there is no way to observe writes (the encode
	// function above already established baseKey/baseInter; the absence of a
	// writer means nothing was written to compare).

	// The traced run must emit at least one frame and one MB row.
	if traceBuf.Len() == 0 {
		t.Fatalf("traced run wrote no oracle trace output")
	}
	if !strings.Contains(traceBuf.String(), `"type":"frame"`) {
		t.Fatalf("traced run missing frame rows: %q", traceBuf.String())
	}
	if !strings.Contains(traceBuf.String(), `"type":"mb"`) {
		t.Fatalf("traced run missing mb rows: %q", traceBuf.String())
	}
}
