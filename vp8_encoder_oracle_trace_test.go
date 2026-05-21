//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func requireOracleTraceBuild(t *testing.T) {
	t.Helper()
	if !oracleTraceBuild {
		t.Skip("oracle tracing is compiled out; run with -tags govpx_oracle_trace")
	}
}

// TestVP8OracleTraceWriterEmitsFrameAndMBRows encodes a 32x32 keyframe followed
// by an inter frame with the trace writer enabled and asserts the JSONL
// stream has 1 frame row for the keyframe, 1 frame row for the inter frame,
// inter-candidate rows for the inter frame, and 8 MB rows (32x32 = 2x2
// macroblocks for each frame).
func TestVP8OracleTraceWriterEmitsFrameAndMBRows(t *testing.T) {
	requireOracleTraceBuild(t)
	const w, h = 32, 32
	var buf bytes.Buffer
	e, err := NewVP8Encoder(EncoderOptions{
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
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	e.SetOracleTraceWriter(&buf)

	keyImg := testImage(w, h)
	// Vary content so the encoder makes nontrivial decisions instead of
	// taking the zero-reference fast path.
	for i := range keyImg.Y {
		keyImg.Y[i] = byte((i*7 + 11) & 0xff)
	}
	for i := range keyImg.U {
		keyImg.U[i] = byte((i*3 + 5) & 0xff)
	}
	for i := range keyImg.V {
		keyImg.V[i] = byte((i*5 + 23) & 0xff)
	}

	dst := make([]byte, 1<<16)
	if _, err := e.EncodeInto(dst, keyImg, 0, 1, EncodeForceKeyFrame); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}

	// Inter frame: shift content slightly so it is not a perfect copy of
	// the previous reference (avoids the zero-reference shortcut path).
	interImg := testImage(w, h)
	for row := range h {
		for col := range w {
			interImg.Y[row*interImg.YStride+col] = keyImg.Y[((row+1)%h)*keyImg.YStride+((col+2)%w)]
		}
	}
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	for row := range uvH {
		for col := range uvW {
			interImg.U[row*interImg.UStride+col] = keyImg.U[((row+1)%uvH)*keyImg.UStride+((col+1)%uvW)]
			interImg.V[row*interImg.VStride+col] = keyImg.V[((row+1)%uvH)*keyImg.VStride+((col+1)%uvW)]
		}
	}
	if _, err := e.EncodeInto(dst, interImg, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}

	lines := splitNonEmptyLines(buf.Bytes())
	var frameRows []map[string]any
	var mbRows []map[string]any
	var candidateRows []map[string]any
	var rateRows []map[string]any
	var recodeRows []map[string]any
	for i, line := range lines {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("trace line %d not valid JSON: %v\nline=%q", i, err, line)
		}
		typ, ok := row["type"].(string)
		if !ok {
			t.Fatalf("trace line %d missing string type field: %v", i, row)
		}
		switch typ {
		case "frame":
			frameRows = append(frameRows, row)
		case "mb":
			mbRows = append(mbRows, row)
		case "inter_candidate":
			candidateRows = append(candidateRows, row)
		case "rate":
			rateRows = append(rateRows, row)
		case "recode":
			recodeRows = append(recodeRows, row)
		case "lf_trial":
			// Per-trial-level loop-filter picker rows are emitted from
			// loopFilterPickContext.pickFast / pickFull and are
			// not relevant to the per-frame / per-MB invariants this test
			// exercises; ignore them here.
			continue
		case "recode_iter", "mb_iter_rate":
			// Per-recode-iteration trace rows (and their per-MB
			// rate mirrors) are emitted from the size_recode loop
			// hook used by the libvpx oracle diff. They are not
			// part of the per-frame / per-MB invariants this test
			// exercises; ignore them here.
			continue
		default:
			t.Fatalf("trace line %d has unexpected type %q", i, typ)
		}
	}

	if len(frameRows) != 2 {
		t.Fatalf("frame rows = %d, want 2", len(frameRows))
	}
	if len(mbRows) != 8 {
		t.Fatalf("mb rows = %d, want 8 (2x2 key frame + 2x2 inter frame)", len(mbRows))
	}
	if len(candidateRows) == 0 {
		t.Fatalf("inter_candidate rows = 0, want tested candidates for inter frame")
	}
	// Each committed frame emits exactly one rate row; recode rows only
	// appear when the frame's recode loop iterated more than once.
	if len(rateRows) != 2 {
		t.Fatalf("rate rows = %d, want 2", len(rateRows))
	}
	for i, row := range rateRows {
		for _, key := range []string{
			"frame_index", "frame_type", "q_index",
			"active_worst_quality", "active_best_quality",
			"buffer_level", "total_byte_count",
			"projected_frame_size", "this_frame_target",
			"kf_overspend_bits", "gf_overspend_bits",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("rate[%d] missing field %q", i, key)
			}
		}
	}
	for i, row := range recodeRows {
		for _, key := range []string{
			"frame_index", "loop_count", "final_q", "reason",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("recode[%d] missing field %q", i, key)
			}
		}
	}
	for i, row := range candidateRows {
		if got := row["frame_index"]; got != float64(1) {
			t.Fatalf("candidate[%d].frame_index = %v, want inter frame 1", i, got)
		}
		picker, ok := row["picker"].(string)
		if !ok || (picker != "rd" && picker != "fast") {
			t.Fatalf("candidate[%d].picker = %v, want rd or fast", i, row["picker"])
		}
		if got := row["outcome"]; got != "tested" {
			t.Fatalf("candidate[%d].outcome = %v, want tested", i, got)
		}
		for _, key := range []string{
			"frame_index", "mb_row", "mb_col",
			"iter", "q",
			"picker", "mode_index", "mode", "ref_slot", "ref_frame",
			"threshold", "best_score_before", "best_yrd_before", "best_sse_before",
			"outcome", "became_best", "loop_break",
			"score", "yrd", "rate", "rate_y", "rate_uv",
			"distortion", "distortion_uv", "sse", "skip",
			"mv_row", "mv_col",
			"improved_mv_start", "improved_mv_near_sadidx",
			"improved_mv_row", "improved_mv_col", "improved_mv_sr",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("candidate[%d] missing field %q", i, key)
			}
		}
		if got := row["iter"].(float64); got < 1 {
			t.Fatalf("candidate[%d].iter = %v, want per-recode-iter candidate row", i, got)
		}
		if got := row["q"].(float64); got < 0 {
			t.Fatalf("candidate[%d].q = %v, want non-negative quantizer", i, got)
		}
	}

	// Frame-row schema sanity.
	if got, want := frameRows[0]["frame_type"], "key"; got != want {
		t.Fatalf("frame[0].frame_type = %v, want %v", got, want)
	}
	if got, want := frameRows[1]["frame_type"], "inter"; got != want {
		t.Fatalf("frame[1].frame_type = %v, want %v", got, want)
	}
	if frameRows[0]["frame_index"].(float64) != 0 {
		t.Fatalf("frame[0].frame_index = %v, want 0", frameRows[0]["frame_index"])
	}
	if frameRows[1]["frame_index"].(float64) != 1 {
		t.Fatalf("frame[1].frame_index = %v, want 1", frameRows[1]["frame_index"])
	}
	for i, row := range frameRows {
		for _, key := range []string{
			"q_index", "base_q_index", "loop_filter_level",
			"refresh_last", "refresh_golden", "refresh_altref",
			"sign_bias_golden", "sign_bias_altref",
			"segmentation_enabled",
			"y_adler32", "u_adler32", "v_adler32",
			"size_bytes",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("frame[%d] missing field %q", i, key)
			}
		}
	}

	// MB-row schema sanity. Expect raster scan order across 2x2 MBs for
	// key frame 0, then raster scan order for inter frame 1.
	wantCells := [][3]float64{
		{0, 0, 0}, {0, 0, 1}, {0, 1, 0}, {0, 1, 1},
		{1, 0, 0}, {1, 0, 1}, {1, 1, 0}, {1, 1, 1},
	}
	for i, row := range mbRows {
		if got, want := row["frame_index"].(float64), wantCells[i][0]; got != want {
			t.Fatalf("mb[%d].frame_index = %v, want %v", i, got, want)
		}
		if got, want := row["mb_row"].(float64), wantCells[i][1]; got != want {
			t.Fatalf("mb[%d].mb_row = %v, want %v", i, got, want)
		}
		if got, want := row["mb_col"].(float64), wantCells[i][2]; got != want {
			t.Fatalf("mb[%d].mb_col = %v, want %v", i, got, want)
		}
		for _, key := range []string{
			"segment_id", "mode", "ref_frame",
			"mv_row", "mv_col", "skip",
			"eob", "eob_sum", "qcoeff",
			"improved_mv_start", "improved_mv_near_sadidx",
			"improved_mv_row", "improved_mv_col", "improved_mv_sr",
		} {
			if _, ok := row[key]; !ok {
				t.Fatalf("mb[%d] missing field %q", i, key)
			}
		}
		eob, ok := row["eob"].([]any)
		if !ok {
			t.Fatalf("mb[%d].eob is not an array: %T", i, row["eob"])
		}
		if len(eob) != 25 {
			t.Fatalf("mb[%d].eob length = %d, want 25", i, len(eob))
		}
		if row["frame_index"].(float64) == 0 {
			if got := row["ref_frame"]; got != "INTRA_FRAME" {
				t.Fatalf("mb[%d].ref_frame = %v, want INTRA_FRAME for key MB", i, got)
			}
			if _, ok := row["uv_mode"]; !ok {
				t.Fatalf("mb[%d] missing uv_mode for key MB", i)
			}
		}
		qcoeff, ok := row["qcoeff"].([]any)
		if !ok {
			t.Fatalf("mb[%d].qcoeff is not an array: %T", i, row["qcoeff"])
		}
		if len(qcoeff) != 25 {
			t.Fatalf("mb[%d].qcoeff length = %d, want 25", i, len(qcoeff))
		}
		firstBlock, ok := qcoeff[0].([]any)
		if !ok || len(firstBlock) != 16 {
			t.Fatalf("mb[%d].qcoeff[0] shape = %T/%d, want 16 coefficients", i, qcoeff[0], len(firstBlock))
		}
	}
}

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

func TestVP8OracleKeyFrameMBTraceIncludesIntraModes(t *testing.T) {
	requireOracleTraceBuild(t)
	var buf bytes.Buffer
	e := &VP8Encoder{}
	e.SetOracleTraceWriter(&buf)
	mode := vp8enc.KeyFrameMacroblockMode{
		YMode:  vp8common.BPred,
		UVMode: vp8common.TMPred,
	}
	for i := range mode.BModes {
		mode.BModes[i] = vp8common.BPredictionMode(i % int(vp8common.VP8BIntraModes))
	}
	var coeffs vp8enc.MacroblockCoefficients
	coeffs.QCoeff[24][0] = 3
	coeffs.SetBlockEOB(24, 1)

	e.emitOracleKeyFrameMBTrace(2, 3, &mode, &coeffs, 0, 0)
	e.flushOracleMBTraceBuffer()

	var row map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &row); err != nil {
		t.Fatalf("trace row not valid JSON: %v\n%s", err, buf.String())
	}
	if got := row["mode"]; got != "B_PRED" {
		t.Fatalf("mode = %v, want B_PRED", got)
	}
	if got := row["uv_mode"]; got != "TM_PRED" {
		t.Fatalf("uv_mode = %v, want TM_PRED", got)
	}
	if got := row["ref_frame"]; got != "INTRA_FRAME" {
		t.Fatalf("ref_frame = %v, want INTRA_FRAME", got)
	}
	bModes, ok := row["b_modes"].([]any)
	if !ok || len(bModes) != 16 {
		t.Fatalf("b_modes shape = %T/%d, want 16", row["b_modes"], len(bModes))
	}
	if bModes[0] != "B_DC_PRED" || bModes[9] != "B_HU_PRED" {
		t.Fatalf("b_modes edge values = %v/%v, want B_DC_PRED/B_HU_PRED", bModes[0], bModes[9])
	}
}

func TestVP8OracleMBTraceIncludesSplitMVPartitionAndBlocks(t *testing.T) {
	requireOracleTraceBuild(t)
	var buf bytes.Buffer
	e := &VP8Encoder{}
	e.SetOracleTraceWriter(&buf)
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 1,
		MV:        vp8enc.MotionVector{Row: 8, Col: 16},
	}
	for i := range mode.BlockMV {
		mode.BlockMV[i] = vp8enc.MotionVector{Row: int16(i), Col: int16(-i)}
	}
	mode.MV = mode.BlockMV[15]
	var coeffs vp8enc.MacroblockCoefficients

	e.emitOracleMBTrace(0, 0, &mode, &coeffs, interFrameSearchStart{}, 0, 0)
	e.flushOracleMBTraceBuffer()

	lines := splitNonEmptyLines(buf.Bytes())
	if len(lines) != 1 {
		t.Fatalf("trace rows = %d, want 1", len(lines))
	}
	var row map[string]any
	if err := json.Unmarshal(lines[0], &row); err != nil {
		t.Fatalf("trace row is not valid JSON: %v", err)
	}
	if got := row["partition"].(float64); got != 1 {
		t.Fatalf("partition = %v, want 1", got)
	}
	rows := row["block_mv_rows"].([]any)
	cols := row["block_mv_cols"].([]any)
	if len(rows) != 16 || len(cols) != 16 {
		t.Fatalf("block MV arrays lengths = %d/%d, want 16/16", len(rows), len(cols))
	}
	if rows[15].(float64) != 15 || cols[15].(float64) != -15 {
		t.Fatalf("block 15 MV = %v,%v, want 15,-15", rows[15], cols[15])
	}
}

func TestVP8OracleTraceIncludesInterFrameBPredMacroblocks(t *testing.T) {
	requireOracleTraceBuild(t)
	const w, h = 16, 32
	var buf bytes.Buffer
	e := newSizedTestEncoder(t, w, h)
	e.SetOracleTraceWriter(&buf)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(w, h)
	fillImage(first, 0, 90, 170)
	second := rateControlTestFrame(w, h, 0)

	packet := make([]byte, 8192)
	if _, err := e.EncodeInto(packet, first, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if _, err := e.EncodeInto(packet, second, 1, 1, 0); err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if e.interFrameModes[1].RefFrame != vp8common.IntraFrame || e.interFrameModes[1].Mode != vp8common.BPred {
		t.Fatalf("inter mode[1] = %+v, want INTRA_FRAME/B_PRED", e.interFrameModes[1])
	}

	mbRows := 0
	sawBPred := false
	for i, line := range splitNonEmptyLines(buf.Bytes()) {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("trace line %d invalid JSON: %v", i, err)
		}
		if row["type"] != "mb" {
			continue
		}
		if row["frame_index"] != float64(1) {
			continue
		}
		mbRows++
		if row["mode"] == "B_PRED" && row["ref_frame"] == "INTRA_FRAME" {
			sawBPred = true
		}
	}
	if mbRows != 2 {
		t.Fatalf("inter MB trace rows = %d, want 2 for 16x32 frame", mbRows)
	}
	if !sawBPred {
		t.Fatalf("trace did not include inter-frame INTRA_FRAME/B_PRED macroblock")
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

func splitNonEmptyLines(b []byte) [][]byte {
	var out [][]byte
	for line := range bytes.SplitSeq(b, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}
