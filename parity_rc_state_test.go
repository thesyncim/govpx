//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// qdriftBaseline is the on-disk shape recorded by
// TestOracle128x128InterQDriftScoreboard. It tracks per-frame Q
// regulation drift and post-encode size drift between govpx and the
// libvpx oracle for the 128x128 realtime CBR cpu8 fixture, where govpx
// historically picks lower Q than libvpx on inter frames and ships
// noticeably larger payloads as a result.
type qdriftBaseline struct {
	MaxAbsQDelta    int     `json:"max_abs_q_delta"`
	MeanAbsQDelta   float64 `json:"mean_abs_q_delta"`
	MaxSizeDeltaPct float64 `json:"max_size_delta_pct"`
	KeyframeQMatch  bool    `json:"keyframe_q_match"`
}

type qdriftFrameSample struct {
	frameIndex   int
	frameType    string
	qGovpx       int
	qLibvpx      int
	sizeGovpx    int
	sizeLibvpx   int
	sizeDeltaPct float64
}

// TestOracle128x128InterQDriftScoreboard exercises the 128x128 panning
// realtime CBR cpu8 fixture and reports per-frame quantizer/size drift
// against the libvpx oracle. The intent is to convert what used to be a
// noisy debug Logf into a measurement gate: govpx's inter-frame Q is
// known to undershoot libvpx by ~8 quantizer steps on this fixture, and
// the resulting payloads are 30-50% larger; the assertions guard against
// future regressions while letting fixes that lower the deltas pass
// freely.
func TestOracle128x128InterQDriftScoreboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle Q-drift scoreboard")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 128
		height     = 128
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
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "qdrift-128", opts, targetKbps, sources, []string{"--end-usage=cbr"})

	samples := make([]qdriftFrameSample, 0, frames)
	for fi := range frames {
		gv := scanFrameRowQ(t, govpxTrace, fi)
		lv := scanFrameRowQ(t, libvpxTrace, fi)
		if gv == nil || lv == nil {
			t.Fatalf("missing frame row for frame_index=%d (govpx=%v libvpx=%v)", fi, gv, lv)
		}
		var pct float64
		if lv.size > 0 {
			pct = 100.0 * float64(gv.size-lv.size) / float64(lv.size)
		}
		samples = append(samples, qdriftFrameSample{
			frameIndex:   fi,
			frameType:    gv.frameType,
			qGovpx:       gv.q,
			qLibvpx:      lv.q,
			sizeGovpx:    gv.size,
			sizeLibvpx:   lv.size,
			sizeDeltaPct: pct,
		})
	}

	// Per-frame markdown table.
	var tbl bytes.Buffer
	fmt.Fprintln(&tbl, "| frame | type | q_govpx | q_libvpx | q_delta | size_govpx | size_libvpx | size_delta_pct |")
	fmt.Fprintln(&tbl, "|---|---|---|---|---|---|---|---|")
	for _, s := range samples {
		fmt.Fprintf(&tbl, "| %d | %s | %d | %d | %+d | %d | %d | %+.2f |\n",
			s.frameIndex, s.frameType, s.qGovpx, s.qLibvpx, s.qGovpx-s.qLibvpx,
			s.sizeGovpx, s.sizeLibvpx, s.sizeDeltaPct)
	}
	t.Logf("\n%s", tbl.String())

	// Summary scalars.
	maxAbsQ := 0
	sumAbsQ := 0
	maxSizePct := 0.0
	keyframeQMatch := true
	keyframeSizePctDelta := 0.0
	keyframeSeen := false
	for _, s := range samples {
		d := s.qGovpx - s.qLibvpx
		if d < 0 {
			d = -d
		}
		if d > maxAbsQ {
			maxAbsQ = d
		}
		sumAbsQ += d
		ap := math.Abs(s.sizeDeltaPct)
		if ap > maxSizePct {
			maxSizePct = ap
		}
		if s.frameType == "key" || s.frameIndex == 0 {
			keyframeSeen = true
			if s.qGovpx != s.qLibvpx {
				keyframeQMatch = false
			}
			keyframeSizePctDelta = s.sizeDeltaPct
		}
	}
	if !keyframeSeen {
		// Default to "match" if we somehow saw no keyframe, so the
		// assertion below treats absence of evidence as "no regression".
		keyframeQMatch = true
	}
	meanAbsQ := 0.0
	if len(samples) > 0 {
		meanAbsQ = float64(sumAbsQ) / float64(len(samples))
	}

	t.Logf("summary: max_abs_q_delta=%d mean_abs_q_delta=%.3f max_size_delta_pct=%.3f keyframe_q_match=%v keyframe_size_pct_delta=%+.3f",
		maxAbsQ, meanAbsQ, maxSizePct, keyframeQMatch, keyframeSizePctDelta)

	current := qdriftBaseline{
		MaxAbsQDelta:    maxAbsQ,
		MeanAbsQDelta:   meanAbsQ,
		MaxSizeDeltaPct: maxSizePct,
		KeyframeQMatch:  keyframeQMatch,
	}

	baselinePath := filepath.Join("testdata", "qdrift_128_baseline.json")
	_, statErr := os.Stat(baselinePath)
	updateBaselines := os.Getenv("GOVPX_UPDATE_BASELINES") == "1"
	if updateBaselines || os.IsNotExist(statErr) {
		buf, err := json.MarshalIndent(current, "", "  ")
		if err != nil {
			t.Fatalf("Marshal qdrift baseline: %v", err)
		}
		buf = append(buf, '\n')
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", filepath.Dir(baselinePath), err)
		}
		if err := os.WriteFile(baselinePath, buf, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", baselinePath, err)
		}
		t.Logf("wrote baseline %s", baselinePath)
		return
	}
	if statErr != nil {
		t.Fatalf("stat %s: %v", baselinePath, statErr)
	}

	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", baselinePath, err)
	}
	var base qdriftBaseline
	if err := json.Unmarshal(raw, &base); err != nil {
		t.Fatalf("Unmarshal baseline: %v", err)
	}

	t.Logf("max_abs_q_delta = %d (baseline %d, %+d)", current.MaxAbsQDelta, base.MaxAbsQDelta, current.MaxAbsQDelta-base.MaxAbsQDelta)
	t.Logf("mean_abs_q_delta = %.3f (baseline %.3f, %+.3f)", current.MeanAbsQDelta, base.MeanAbsQDelta, current.MeanAbsQDelta-base.MeanAbsQDelta)
	t.Logf("max_size_delta_pct = %.3f (baseline %.3f, %+.3f)", current.MaxSizeDeltaPct, base.MaxSizeDeltaPct, current.MaxSizeDeltaPct-base.MaxSizeDeltaPct)
	t.Logf("keyframe_q_match = %v (baseline %v)", current.KeyframeQMatch, base.KeyframeQMatch)

	if current.MaxAbsQDelta > base.MaxAbsQDelta+1 {
		t.Errorf("max_abs_q_delta=%d exceeds baseline %d + 1; rerun with GOVPX_UPDATE_BASELINES=1 if intended",
			current.MaxAbsQDelta, base.MaxAbsQDelta)
	}
	if current.MeanAbsQDelta > base.MeanAbsQDelta+0.5 {
		t.Errorf("mean_abs_q_delta=%.3f exceeds baseline %.3f + 0.5; rerun with GOVPX_UPDATE_BASELINES=1 if intended",
			current.MeanAbsQDelta, base.MeanAbsQDelta)
	}
	if current.MaxSizeDeltaPct > base.MaxSizeDeltaPct+5.0 {
		t.Errorf("max_size_delta_pct=%.3f exceeds baseline %.3f + 5.0; rerun with GOVPX_UPDATE_BASELINES=1 if intended",
			current.MaxSizeDeltaPct, base.MaxSizeDeltaPct)
	}
	if base.KeyframeQMatch && !current.KeyframeQMatch {
		t.Errorf("keyframe_q_match regressed from true to false; rerun with GOVPX_UPDATE_BASELINES=1 if intended")
	}
}

type qdriftFrameRow struct {
	q         int
	size      int
	frameType string
}

// scanFrameRowQ extracts q_index, size_bytes, and frame_type for the
// "frame" row matching frameIndex from a JSONL trace stream. Returns nil
// if the frame is missing.
func scanFrameRowQ(t *testing.T, trace []byte, frameIndex int) *qdriftFrameRow {
	t.Helper()
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<20)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if typ, _ := row["type"].(string); typ != "frame" {
			continue
		}
		fIdx, _ := row["frame_index"].(float64)
		if int(fIdx) != frameIndex {
			continue
		}
		q, _ := row["q_index"].(float64)
		size, _ := row["size_bytes"].(float64)
		ft, _ := row["frame_type"].(string)
		return &qdriftFrameRow{q: int(q), size: int(size), frameType: ft}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return nil
}
