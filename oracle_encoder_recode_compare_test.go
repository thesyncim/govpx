//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
)

// TestOracleRecodeRowParity gates the libvpx VP8 recode loop semantics by
// driving a tight rate-control fixture that should force frame-size recodes
// on at least one side and comparing the emitted recode rows.
func TestOracleRecodeRowParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle recode comparison")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 200
		frames     = 8
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      8,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "recode-vbr-tight", opts, targetKbps, sources, []string{"--end-usage=vbr"})

	gRows := oracleTraceRecodeRowsByFrame(t, govpxTrace)
	lRows := oracleTraceRecodeRowsByFrame(t, libvpxTrace)
	if len(gRows) == 0 && len(lRows) == 0 {
		t.Skipf("no recode rows on either side; fixture needs tightening")
	}

	asymmetric := 0
	matched := 0
	// Build the union of frame indices.
	seen := make(map[int64]struct{}, len(gRows)+len(lRows))
	for fi := range gRows {
		seen[fi] = struct{}{}
	}
	for fi := range lRows {
		seen[fi] = struct{}{}
	}
	frameIndices := make([]int64, 0, len(seen))
	for fi := range seen {
		frameIndices = append(frameIndices, fi)
	}
	// Sort for stable output.
	sortInt64s(frameIndices)

	for _, fi := range frameIndices {
		g, gOK := gRows[fi]
		l, lOK := lRows[fi]
		if gOK && !lOK {
			t.Logf("recode asymmetry at frame %d: govpx emitted but libvpx did not (govpx reason=%v final_q=%v loop_count=%v)", fi, g["reason"], g["final_q"], g["loop_count"])
			asymmetric++
			continue
		}
		if lOK && !gOK {
			t.Logf("recode asymmetry at frame %d: libvpx emitted but govpx did not (libvpx reason=%v final_q=%v loop_count=%v)", fi, l["reason"], l["final_q"], l["loop_count"])
			asymmetric++
			continue
		}
		// Both present.
		matched++
		gReason, _ := g["reason"].(string)
		lReason, _ := l["reason"].(string)
		if gReason != lReason {
			t.Errorf("frame %d recode reason govpx=%q libvpx=%q", fi, gReason, lReason)
		}
		gFinal := traceFloat(g["final_q"])
		lFinal := traceFloat(l["final_q"])
		if math.Abs(gFinal-lFinal) > 2 {
			t.Errorf("frame %d recode final_q govpx=%v libvpx=%v exceeds 2 qindex", fi, gFinal, lFinal)
		}
		gLoop := traceFloat(g["loop_count"])
		lLoop := traceFloat(l["loop_count"])
		if math.Abs(gLoop-lLoop) > 1 {
			t.Errorf("frame %d recode loop_count govpx=%v libvpx=%v exceeds 1", fi, gLoop, lLoop)
		}
	}
	t.Logf("recode rows matched=%d asymmetric=%d (govpx_total=%d libvpx_total=%d)", matched, asymmetric, len(gRows), len(lRows))
}

// oracleTraceRecodeRowsByFrame indexes recode rows by frame_index. If a frame
// has multiple recode rows we keep the last one (final iteration).
func oracleTraceRecodeRowsByFrame(t *testing.T, trace []byte) map[int64]map[string]any {
	t.Helper()
	out := make(map[int64]map[string]any)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row is not valid JSON: %v\n%s", err, scan.Bytes())
		}
		if typ, _ := row["type"].(string); typ != "recode" {
			continue
		}
		fi := int64(traceFloat(row["frame_index"]))
		out[fi] = row
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return out
}

func sortInt64s(s []int64) {
	// Insertion sort: trace fixtures have small frame counts.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
