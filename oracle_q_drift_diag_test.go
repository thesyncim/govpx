package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestOracle96x96QDriftDiag prints the rate/frame trace rows for the
// 96x96 realtime CBR cpu8 panning case. It is intentionally debug-gated:
// the scoreboard tracks the regression surface, while this diagnostic keeps
// the next Q-drift investigation pointed at the exact rate-control field
// that first diverges.
func TestOracle96x96QDriftDiag(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the 96x96 Q-drift diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 96
		height     = 96
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	opts := EncoderOptions{
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
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "qdrift-96-diag", opts, targetKbps, sources, []string{"--end-usage=cbr"})
	govpxRows := qDriftDecisionRows(t, govpxTrace)
	libvpxRows := qDriftDecisionRows(t, libvpxTrace)

	keys := qDriftDecisionKeys(govpxRows, libvpxRows)
	var b strings.Builder
	fmt.Fprintln(&b, "\n| row | field | govpx | libvpx |")
	fmt.Fprintln(&b, "|---|---|---:|---:|")
	for _, key := range keys {
		g, gOK := govpxRows[key]
		l, lOK := libvpxRows[key]
		fields := qDriftDecisionFields(g, l)
		for _, field := range fields {
			gv := "-"
			lv := "-"
			if gOK {
				gv = fmt.Sprint(g[field])
			}
			if lOK {
				lv = fmt.Sprint(l[field])
			}
			if gv == lv {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", key, field, gv, lv)
		}
	}
	t.Log(b.String())
}

func qDriftDecisionRows(t *testing.T, trace []byte) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("q-drift diag row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		typ, _ := row["type"].(string)
		if typ != "rate" && typ != "recode" && typ != "frame" {
			continue
		}
		frame := int(traceFloat(row["frame_index"]))
		key := fmt.Sprintf("%02d/%s", frame, typ)
		out[key] = row
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan q-drift diag rows: %v", err)
	}
	return out
}

func qDriftDecisionKeys(a, b map[string]map[string]any) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func qDriftDecisionFields(rows ...map[string]any) []string {
	seen := map[string]bool{}
	for _, row := range rows {
		for k := range row {
			if k != "type" && k != "frame_index" {
				seen[k] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
