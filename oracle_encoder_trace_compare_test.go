package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

func TestOracleEncoderTraceDecisionCompare(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace comparison")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 6
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "trace-vbr-panning", opts, targetKbps, sources, []string{"--end-usage=vbr"})
	govpxProjected := projectOracleDecisionTrace(t, govpxTrace)
	libvpxProjected := projectOracleDecisionTrace(t, libvpxTrace)
	div, err := coracle.CompareOracleTraces(bytes.NewReader(govpxProjected), bytes.NewReader(libvpxProjected), coracle.CompareOptions{
		MaxDivergences: 8,
		NumericFieldTolerances: map[string]float64{
			"projected_frame_size": 64,
		},
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 0 {
		t.Fatalf("projected encoder decision trace diverged:\n%s", formatOracleTraceDivergences(div))
	}
}

func TestOracleEncoderTraceCandidateRowsPresent(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace comparison")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	cases := []struct {
		name       string
		opts       EncoderOptions
		extraArgs  []string
		wantPicker string
	}{
		{
			name: "good-quality-rd",
			opts: EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           3,
				KeyFrameInterval:  999,
			},
			extraArgs:  []string{"--end-usage=vbr"},
			wantPicker: "rd",
		},
		{
			name: "realtime-fast",
			opts: EncoderOptions{
				Width:             width,
				Height:            height,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				Deadline:          DeadlineRealtime,
				CpuUsed:           8,
				KeyFrameInterval:  999,
			},
			extraArgs:  []string{"--end-usage=cbr"},
			wantPicker: "fast",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			govpxTrace := captureGovpxEncoderTrace(t, tc.opts, sources)
			libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "trace-candidates-"+tc.name, tc.opts, targetKbps, sources, tc.extraArgs)
			assertOracleTraceHasCandidateRows(t, "govpx", govpxTrace, tc.wantPicker)
			assertOracleTraceHasCandidateRows(t, "libvpx", libvpxTrace, tc.wantPicker)
		})
	}
}

func TestOracleEncoderTraceInterCandidateCompare(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle trace comparison")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "trace-inter-candidates-vbr-panning", opts, targetKbps, sources, []string{"--end-usage=vbr"})
	govpxProjected := projectOracleInterCandidateTrace(t, govpxTrace)
	libvpxProjected := projectOracleInterCandidateTrace(t, libvpxTrace)
	div, err := coracle.CompareOracleTraces(bytes.NewReader(govpxProjected), bytes.NewReader(libvpxProjected), coracle.CompareOptions{
		MaxDivergences: 16,
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 0 {
		t.Fatalf("projected inter-candidate trace diverged:\n%s", formatOracleTraceDivergences(div))
	}
}

func findVpxencOracle(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("GOVPX_VPXENC_ORACLE"); path != "" {
		return path
	}
	local := filepath.Join("internal", "coracle", "build", "vpxenc-oracle")
	info, err := os.Stat(local)
	if err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return local
	}
	t.Skip("set GOVPX_VPXENC_ORACLE to the patched libvpx vpxenc oracle binary")
	return ""
}

func captureGovpxEncoderTrace(t *testing.T, opts EncoderOptions, sources []Image) []byte {
	t.Helper()
	var trace bytes.Buffer
	opts.OracleTraceWriter = &trace
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, opts.Width*opts.Height*3)
	for i, source := range sources {
		result, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto frame %d dropped, want trace corpus without drops", i)
		}
	}
	return append([]byte(nil), trace.Bytes()...)
}

func captureLibvpxEncoderTrace(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) []byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	tracePath := filepath.Join(dir, name+".jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)
	deadlineArg := "--good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=4",
		"--max-q=56",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+tracePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, out)
	}
	trace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("ReadFile %s returned error: %v", tracePath, err)
	}
	return trace
}

func projectOracleDecisionTrace(t *testing.T, trace []byte) []byte {
	t.Helper()
	keep := map[string]map[string]bool{
		"rate": {
			"type":                 true,
			"frame_index":          true,
			"frame_type":           true,
			"q_index":              true,
			"active_worst_quality": true,
			"active_best_quality":  true,
			"projected_frame_size": true,
			"this_frame_target":    true,
			"zbin_over_quant":      true,
		},
		"recode": {
			"type":        true,
			"frame_index": true,
			"loop_count":  true,
			"final_q":     true,
			"reason":      true,
		},
		"frame": {
			"type":                  true,
			"frame_index":           true,
			"frame_type":            true,
			"q_index":               true,
			"base_q_index":          true,
			"loop_filter_level":     true,
			"refresh_last":          true,
			"refresh_golden":        true,
			"refresh_altref":        true,
			"sign_bias_golden":      true,
			"sign_bias_altref":      true,
			"refresh_entropy_probs": true,
			"default_coef_reset":    true,
		},
	}
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typ, _ := row["type"].(string)
		fields := keep[typ]
		if len(fields) == 0 {
			continue
		}
		projected := make(map[string]any, len(fields))
		for field := range fields {
			if v, ok := row[field]; ok {
				projected[field] = v
			}
		}
		encoded, err := json.Marshal(projected)
		if err != nil {
			t.Fatalf("Marshal projected trace row returned error: %v", err)
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out.Bytes()
}

func projectOracleInterCandidateTrace(t *testing.T, trace []byte) []byte {
	t.Helper()
	keep := map[string]bool{
		"type":        true,
		"frame_index": true,
		"mb_row":      true,
		"mb_col":      true,
		"picker":      true,
		"mode_index":  true,
		"mode":        true,
		"ref_slot":    true,
		"ref_frame":   true,
		"outcome":     true,
		"became_best": true,
		"loop_break":  true,
		"mv_row":      true,
		"mv_col":      true,
	}
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if typ, _ := row["type"].(string); typ != "inter_candidate" {
			continue
		}
		projected := make(map[string]any, len(keep))
		for field := range keep {
			if v, ok := row[field]; ok {
				projected[field] = v
			}
		}
		encoded, err := json.Marshal(projected)
		if err != nil {
			t.Fatalf("Marshal projected trace row returned error: %v", err)
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out.Bytes()
}

func TestProjectOracleDecisionTraceDropsInterCandidateRows(t *testing.T) {
	trace := []byte(
		`{"type":"rate","frame_index":0,"frame_type":"key","q_index":4}` + "\n" +
			`{"type":"inter_candidate","frame_index":1,"mb_row":0,"mb_col":0,"picker":"rd","mode_index":0}` + "\n" +
			`{"type":"frame","frame_index":0,"frame_type":"key","q_index":4}` + "\n",
	)
	projected := projectOracleDecisionTrace(t, trace)
	if bytes.Contains(projected, []byte("inter_candidate")) {
		t.Fatalf("projected decision trace retained inter_candidate row:\n%s", projected)
	}
	lines := splitNonEmptyLines(projected)
	if len(lines) != 2 {
		t.Fatalf("projected decision trace lines = %d, want 2\n%s", len(lines), projected)
	}
}

func TestProjectOracleInterCandidateTraceKeepsStagedFields(t *testing.T) {
	trace := []byte(
		`{"type":"rate","frame_index":0,"frame_type":"key","q_index":4}` + "\n" +
			`{"type":"inter_candidate","frame_index":1,"mb_row":0,"mb_col":0,"picker":"rd","mode_index":7,"mode":"NEWMV","ref_slot":1,"ref_frame":"LAST_FRAME","outcome":"tested","became_best":true,"loop_break":false,"mv_row":8,"mv_col":16,"score":99,"rate":12}` + "\n" +
			`{"type":"mb","frame_index":1,"mb_row":0,"mb_col":0,"mode":"NEWMV"}` + "\n",
	)
	projected := projectOracleInterCandidateTrace(t, trace)
	lines := splitNonEmptyLines(projected)
	if len(lines) != 1 {
		t.Fatalf("projected candidate trace lines = %d, want 1\n%s", len(lines), projected)
	}
	if !bytes.Contains(projected, []byte(`"type":"inter_candidate"`)) {
		t.Fatalf("projected candidate trace omitted candidate row:\n%s", projected)
	}
	for _, dropped := range []string{"score", "rate", "q_index", `"type":"mb"`} {
		if bytes.Contains(projected, []byte(dropped)) {
			t.Fatalf("projected candidate trace retained %q:\n%s", dropped, projected)
		}
	}
}

func assertOracleTraceHasCandidateRows(t *testing.T, side string, trace []byte, wantPicker string) {
	t.Helper()
	rows := oracleTraceRowsOfType(t, trace, "inter_candidate")
	if len(rows) == 0 {
		t.Fatalf("%s trace has no inter_candidate rows", side)
	}
	sawPicker := false
	for i, row := range rows {
		if got := row["picker"]; got == wantPicker {
			sawPicker = true
		}
		if got := row["frame_index"]; got == float64(0) {
			t.Fatalf("%s candidate[%d].frame_index = %v, want only inter-frame candidates", side, i, got)
		}
		for _, key := range []string{
			"frame_index", "mb_row", "mb_col",
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
				t.Fatalf("%s candidate[%d] missing field %q", side, i, key)
			}
		}
	}
	if !sawPicker {
		t.Fatalf("%s trace has %d candidate rows but no picker %q", side, len(rows), wantPicker)
	}
}

func oracleTraceRowsOfType(t *testing.T, trace []byte, wantType string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if typ, _ := row["type"].(string); typ == wantType {
			rows = append(rows, row)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func formatOracleTraceDivergences(div []coracle.Divergence) string {
	var buf bytes.Buffer
	for _, d := range div {
		buf.WriteString("row=")
		buf.WriteString(strconv.Itoa(d.RowIndex))
		buf.WriteString(" kind=")
		buf.WriteString(d.RowKind)
		buf.WriteString(" frame=")
		buf.WriteString(strconv.FormatInt(d.FrameIndex, 10))
		buf.WriteString(" field=")
		buf.WriteString(d.Field)
		buf.WriteString(" govpx=")
		buf.WriteString(strconv.Quote(toTraceValueString(d.Govpx)))
		buf.WriteString(" libvpx=")
		buf.WriteString(strconv.Quote(toTraceValueString(d.Libvpx)))
		buf.WriteByte('\n')
	}
	return buf.String()
}

func toTraceValueString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<invalid>"
	}
	return string(b)
}
