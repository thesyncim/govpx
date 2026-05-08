package govpx

// Loop-filter fast-picker per-trial-level diagnostic.
//
// TestOracleLFTrialDiag drives the 128x128 panning realtime CBR cpu8
// fixture through both encoders and prints a side-by-side per-trial-level
// SSE table. The libvpx-side oracle patch in
// internal/coracle/build_vpxenc_oracle.sh emits one
// {"type":"lf_trial",...} row per calc_partial_ssl_err call; govpx emits
// the matching row from emitOracleLFTrial. The harness localizes a
// divergence in vp8cx_pick_filter_level_fast vs pickLoopFilterLevelFast
// to one of:
//
//   - the LF function applied to the trial buffer (different filter math),
//   - the SSE region (different rows sampled),
//   - the trial-level set (different seed/step/bounds).
//
// Gated behind GOVPX_DEBUG=1 because it is a diagnostic, not a parity gate.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestOracleLFTrialDiag(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the loop-filter trial diagnostic")
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
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    999,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "lf-trial-128x128", opts, targetKbps, sources, []string{"--end-usage=cbr", "--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500"})

	govpxRows := parseLFTrialRows(t, "govpx", govpxTrace)
	libvpxRows := parseLFTrialRows(t, "libvpx", libvpxTrace)
	govpxFrames := parseLFFrameLevels(t, "govpx", govpxTrace)
	libvpxFrames := parseLFFrameLevels(t, "libvpx", libvpxTrace)
	govpxQ := parseFrameQIndex(t, "govpx", govpxTrace)
	libvpxQ := parseFrameQIndex(t, "libvpx", libvpxTrace)

	t.Logf("govpx lf_trial rows: %d frames, libvpx: %d frames", len(govpxRows), len(libvpxRows))
	t.Logf("govpx frame levels: %v", govpxFrames)
	t.Logf("libvpx frame levels: %v", libvpxFrames)

	frameIndexes := unionFrameIndexes(govpxRows, libvpxRows)
	if len(frameIndexes) == 0 {
		t.Fatal("no lf_trial rows in either trace; check picker invocation")
	}
	for _, frame := range frameIndexes {
		t.Run(fmt.Sprintf("frame_%d", frame), func(t *testing.T) {
			renderLFTrialDiff(t, frame, govpxRows[frame], libvpxRows[frame],
				govpxFrames[frame], libvpxFrames[frame],
				govpxQ[frame], libvpxQ[frame])
		})
	}
}

type lfTrialKey struct {
	Phase string
	Level int
}

type lfTrialRow struct {
	Phase string
	Level int
	YSSE  int
	Order int
}

func parseLFTrialRows(t *testing.T, label string, trace []byte) map[uint64][]lfTrialRow {
	t.Helper()
	out := make(map[uint64][]lfTrialRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	order := 0
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "lf_trial" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		phase, _ := row["phase"].(string)
		lvl, _ := row["trial_level"].(float64)
		sse, _ := row["trial_y_sse"].(float64)
		key := uint64(fi)
		out[key] = append(out[key], lfTrialRow{
			Phase: phase,
			Level: int(lvl),
			YSSE:  int(sse),
			Order: order,
		})
		order++
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan %s trace: %v", label, err)
	}
	return out
}

func parseLFFrameLevels(t *testing.T, label string, trace []byte) map[uint64]int {
	t.Helper()
	out := make(map[uint64]int)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "frame" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		lvl, ok := row["loop_filter_level"].(float64)
		if !ok {
			continue
		}
		out[uint64(fi)] = int(lvl)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan %s frame levels: %v", label, err)
	}
	return out
}

func parseFrameQIndex(t *testing.T, label string, trace []byte) map[uint64]int {
	t.Helper()
	out := make(map[uint64]int)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "frame" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		q, ok := row["q_index"].(float64)
		if !ok {
			continue
		}
		out[uint64(fi)] = int(q)
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan %s q index: %v", label, err)
	}
	return out
}

func unionFrameIndexes(a, b map[uint64][]lfTrialRow) []uint64 {
	seen := map[uint64]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]uint64, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func renderLFTrialDiff(t *testing.T, frame uint64, govpxRows, libvpxRows []lfTrialRow,
	govpxLevel, libvpxLevel int, govpxQ, libvpxQ int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== loop-filter fast-picker per-trial table for frame %d ===\n", frame)
	fmt.Fprintf(&b, "  govpx: chosen_level=%d   q_index=%d\n", govpxLevel, govpxQ)
	fmt.Fprintf(&b, "  libvpx: chosen_level=%d  q_index=%d\n", libvpxLevel, libvpxQ)
	fmt.Fprintf(&b, "  %-6s %-6s | %-12s %-12s %s\n", "phase", "level", "govpx_y_sse", "libvpx_y_sse", "delta")

	keys := unionLFKeys(govpxRows, libvpxRows)
	for _, k := range keys {
		gv, gOK := lookupLF(govpxRows, k)
		lv, lOK := lookupLF(libvpxRows, k)
		gStr := "-"
		lStr := "-"
		if gOK {
			gStr = strconv.Itoa(gv.YSSE)
		}
		if lOK {
			lStr = strconv.Itoa(lv.YSSE)
		}
		delta := ""
		if gOK && lOK {
			delta = strconv.Itoa(gv.YSSE - lv.YSSE)
		} else if gOK {
			delta = "(govpx-only)"
		} else if lOK {
			delta = "(libvpx-only)"
		}
		fmt.Fprintf(&b, "  %-6s %-6d | %-12s %-12s %s\n", k.Phase, k.Level, gStr, lStr, delta)
	}
	t.Log(b.String())

	if govpxLevel != libvpxLevel {
		t.Errorf("frame %d: govpx LF=%d libvpx LF=%d (q_idx %d/%d)", frame, govpxLevel, libvpxLevel, govpxQ, libvpxQ)
	}
}

func unionLFKeys(a, b []lfTrialRow) []lfTrialKey {
	seen := map[lfTrialKey]int{}
	rank := 0
	for _, r := range a {
		k := lfTrialKey{r.Phase, r.Level}
		if _, ok := seen[k]; !ok {
			seen[k] = rank
			rank++
		}
	}
	for _, r := range b {
		k := lfTrialKey{r.Phase, r.Level}
		if _, ok := seen[k]; !ok {
			seen[k] = rank
			rank++
		}
	}
	out := make([]lfTrialKey, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		// Sort by phase order: seed, down, up; then by level desc within phase.
		pa := phaseRank(out[i].Phase)
		pb := phaseRank(out[j].Phase)
		if pa != pb {
			return pa < pb
		}
		return out[i].Level > out[j].Level
	})
	return out
}

func phaseRank(p string) int {
	switch p {
	case "seed":
		return 0
	case "down":
		return 1
	case "up":
		return 2
	}
	return 99
}

func lookupLF(rows []lfTrialRow, k lfTrialKey) (lfTrialRow, bool) {
	for _, r := range rows {
		if r.Phase == k.Phase && r.Level == k.Level {
			return r, true
		}
	}
	return lfTrialRow{}, false
}
