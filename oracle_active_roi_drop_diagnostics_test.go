//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

func TestOracleActiveROIDropTraceDiagnostic(t *testing.T) {
	if os.Getenv("GOVPX_ACTIVE_ROI_DROP_TRACE") != "1" {
		t.Skip("set GOVPX_ACTIVE_ROI_DROP_TRACE=1")
	}
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		frames     = 30
		width      = 64
		height     = 64
		targetKbps = 50
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             -3,
		Tuning:              TunePSNR,
		BufferSizeMs:        200,
		BufferInitialSizeMs: 100,
		BufferOptimalSizeMs: 150,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  60,
	}
	apply := func(t *testing.T, e *VP8Encoder) {
		t.Helper()
		activeMapApply("checker")(t, e)
		roiMapApply("border1")(t, e)
	}
	govpxTrace, govpxFrames := captureGovpxActiveROIDropTrace(t, opts, sources, apply)
	tracePath := filepath.Join(t.TempDir(), "frameflags-active-roi-drop.jsonl")
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", tracePath)
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "trace-active-roi-drop", opts, targetKbps, sources, nil, []string{
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--buf-sz=200",
		"--buf-initial-sz=100",
		"--buf-optimal-sz=150",
		"--drop-frame=60",
		"--active-map=checker",
		"--roi-map=border1",
	})
	libvpxTrace, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}
	t.Logf("encoded packets: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	for i := 0; i < len(govpxFrames) && i < len(libvpxFrames); i++ {
		if !bytes.Equal(govpxFrames[i], libvpxFrames[i]) {
			t.Logf("first packet mismatch index=%d govpx_len=%d libvpx_len=%d", i, len(govpxFrames[i]), len(libvpxFrames[i]))
			break
		}
	}
	govpxProjected := projectActiveROIDropTrace(t, govpxTrace)
	libvpxProjected := projectActiveROIDropTrace(t, libvpxTrace)
	div, err := coracle.CompareOracleTraces(bytes.NewReader(govpxProjected), bytes.NewReader(libvpxProjected), coracle.CompareOptions{
		MaxDivergences: 32,
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces returned error: %v", err)
	}
	if len(div) != 0 {
		t.Logf("projected trace divergences:\n%s", formatOracleTraceDivergences(div))
	}
	govpxMB := projectActiveROIDropMBTrace(t, govpxTrace, 11)
	libvpxMB := projectActiveROIDropMBTrace(t, libvpxTrace, 11)
	mbDiv, err := coracle.CompareOracleTraces(bytes.NewReader(govpxMB), bytes.NewReader(libvpxMB), coracle.CompareOptions{
		MaxDivergences: 32,
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces MB returned error: %v", err)
	}
	if len(mbDiv) != 0 {
		t.Logf("frame 11 MB divergences:\n%s", formatOracleTraceDivergences(mbDiv))
	}
	govpxKeyMB := projectActiveROIDropMBTrace(t, govpxTrace, 0)
	libvpxKeyMB := projectActiveROIDropMBTrace(t, libvpxTrace, 0)
	keyMBDiv, err := coracle.CompareOracleTraces(bytes.NewReader(govpxKeyMB), bytes.NewReader(libvpxKeyMB), coracle.CompareOptions{
		MaxDivergences: 32,
	})
	if err != nil {
		t.Fatalf("CompareOracleTraces key MB returned error: %v", err)
	}
	if len(keyMBDiv) != 0 {
		t.Logf("frame 0 MB divergences:\n%s", formatOracleTraceDivergences(keyMBDiv))
	}
	t.Logf("govpx packet headers:\n%s", formatActiveROIDropPacketHeaders(t, govpxFrames, 8, 15))
	t.Logf("libvpx packet headers:\n%s", formatActiveROIDropPacketHeaders(t, libvpxFrames, 8, 15))
	t.Logf("govpx frame/rate/drop rows:\n%s", formatActiveROIDropRows(t, govpxProjected, 0, 15))
	t.Logf("libvpx frame/rate/drop rows:\n%s", formatActiveROIDropRows(t, libvpxProjected, 0, 15))
}

func captureGovpxActiveROIDropTrace(t *testing.T, opts EncoderOptions, sources []Image, apply0 func(*testing.T, *VP8Encoder)) ([]byte, [][]byte) {
	t.Helper()
	requireOracleTraceBuild(t)
	var trace bytes.Buffer
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(&trace)
	if apply0 != nil {
		apply0(t, enc)
	}
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, source := range sources {
		result, err := enc.EncodeInto(packet, source, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return append([]byte(nil), trace.Bytes()...), out
}

func projectActiveROIDropTrace(t *testing.T, trace []byte) []byte {
	t.Helper()
	keep := map[string]map[string]bool{
		"rate": {
			"type":                 true,
			"frame_index":          true,
			"frame_type":           true,
			"q_index":              true,
			"active_worst_quality": true,
			"active_best_quality":  true,
			"buffer_level":         true,
			"projected_frame_size": true,
			"this_frame_target":    true,
			"kf_overspend_bits":    true,
			"gf_overspend_bits":    true,
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
			"type":                      true,
			"frame_index":               true,
			"frame_type":                true,
			"dropped":                   true,
			"force_maxqp":               true,
			"buffer_level":              true,
			"this_frame_target":         true,
			"reason":                    true,
			"q_index":                   true,
			"base_q_index":              true,
			"loop_filter_level":         true,
			"segmentation_enabled":      true,
			"refresh_last":              true,
			"refresh_golden":            true,
			"refresh_altref":            true,
			"coef_probs_adler":          true,
			"ymode_probs_adler":         true,
			"uv_mode_probs_adler":       true,
			"mv_probs_adler":            true,
			"prob_intra_coded":          true,
			"prob_last_coded":           true,
			"prob_gf_coded":             true,
			"mode_ref_lf_delta_enabled": true,
			"mode_ref_lf_delta_update":  true,
		},
	}
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
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
			if value, ok := row[field]; ok {
				projected[field] = value
			}
		}
		data, err := json.Marshal(projected)
		if err != nil {
			t.Fatalf("marshal projected trace row: %v", err)
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out.Bytes()
}

func formatActiveROIDropRows(t *testing.T, trace []byte, start int, end int) string {
	t.Helper()
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		idx := int(traceFloat(row["frame_index"]))
		if idx < start || idx > end {
			continue
		}
		typ, _ := row["type"].(string)
		switch typ {
		case "rate":
			fmt.Fprintf(&out, "rate  f=%02d q=%v awq=%v abq=%v target=%v projected=%v buffer=%v zbin=%v\n",
				idx, row["q_index"], row["active_worst_quality"], row["active_best_quality"], row["this_frame_target"], row["projected_frame_size"], row["buffer_level"], row["zbin_over_quant"])
		case "frame":
			fmt.Fprintf(&out, "frame f=%02d dropped=%v q=%v lf=%v size? target=%v buffer=%v reason=%v refresh=%v/%v/%v probs=%v/%v/%v\n",
				idx, row["dropped"], row["q_index"], row["loop_filter_level"], row["this_frame_target"], row["buffer_level"], row["reason"], row["refresh_last"], row["refresh_golden"], row["refresh_altref"], row["prob_intra_coded"], row["prob_last_coded"], row["prob_gf_coded"])
		case "recode":
			fmt.Fprintf(&out, "recode f=%02d loops=%v final_q=%v reason=%v\n",
				idx, row["loop_count"], row["final_q"], row["reason"])
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out.String()
}

func projectActiveROIDropMBTrace(t *testing.T, trace []byte, frame int) []byte {
	t.Helper()
	keep := map[string]bool{
		"type":            true,
		"frame_index":     true,
		"mb_row":          true,
		"mb_col":          true,
		"segment_id":      true,
		"mode":            true,
		"ref_frame":       true,
		"mv_row":          true,
		"mv_col":          true,
		"skip":            true,
		"uv_mode":         true,
		"eob_sum":         true,
		"eob":             true,
		"mb_rate":         true,
		"aggregated_rate": true,
	}
	var out bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typ, _ := row["type"].(string)
		if typ != "mb" || int(traceFloat(row["frame_index"])) != frame {
			continue
		}
		projected := make(map[string]any, len(keep))
		for field := range keep {
			if value, ok := row[field]; ok {
				projected[field] = value
			}
		}
		data, err := json.Marshal(projected)
		if err != nil {
			t.Fatalf("marshal projected MB row: %v", err)
		}
		out.Write(data)
		out.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out.Bytes()
}

func formatActiveROIDropPacketHeaders(t *testing.T, packets [][]byte, start int, end int) string {
	t.Helper()
	dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder: %v", err)
	}
	var out bytes.Buffer
	for i, pkt := range packets {
		if err := dec.Decode(pkt); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if i < start || i > end {
			continue
		}
		fmt.Fprintf(&out, "packet=%02d len=%d first_part=%d key=%t q=%d lf=%d skip=%t skip_false=%d prob_ref=%d/%d/%d seg=%t map=%t data=%t\n",
			i,
			len(pkt),
			dec.frameHeader.FirstPartitionSize,
			dec.frameHeader.KeyFrame(),
			dec.state.Quant.BaseQIndex,
			dec.state.LoopFilter.Level,
			dec.state.Mode.MBNoCoeffSkip,
			dec.state.Mode.ProbSkipFalse,
			dec.state.Mode.ProbIntra,
			dec.state.Mode.ProbLast,
			dec.state.Mode.ProbGolden,
			dec.state.Segmentation.Enabled,
			dec.state.Segmentation.UpdateMap,
			dec.state.Segmentation.UpdateData)
	}
	return out.String()
}
