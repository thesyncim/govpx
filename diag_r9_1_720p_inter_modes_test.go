package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestDiagR91Inter720pModeDispersal captures per-MB inter-frame mode/ref/mv
// rows from govpx and libvpx at 1280x720 cpu=8 RT CBR over a 30-frame
// noise/panning fixture (matches the cmd/govpx-bench harness scale) and
// prints the mode distribution, EOB-sum / mb-rate aggregates, and the top
// divergent (mb, mode) tuples. The r9-1 sweep used this to confirm the
// NEAR/NEW dispersal called out in r7-b had already closed under R8 and to
// localize the residual ZEROMV<->NEARESTMV swap (~0.8pp at 720p noise) for
// future tightening. Skipped by default; run with:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_DIAG_R91=1 \
//	  [GOVPX_DIAG_R91_NOISE=1] \
//	  go test -run TestDiagR91Inter720pModeDispersal -v
func TestDiagR91Inter720pModeDispersal(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle inter-mode diag")
	}
	if os.Getenv("GOVPX_DIAG_R91") != "1" {
		t.Skip("set GOVPX_DIAG_R91=1 to enable the r9-1 720p diag")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 1500
		frames     = 30
		cpuUsed    = 8
	)
	useNoise := os.Getenv("GOVPX_DIAG_R91_NOISE") == "1"
	opts := EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    999,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}

	sources := make([]Image, frames)
	for i := range sources {
		if useNoise {
			sources[i] = scoreboardBenchNoiseFrame(width, height, i)
		} else {
			sources[i] = encoderValidationPanningFrame(width, height, i)
		}
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	extra := []string{
		"--end-usage=cbr",
		"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
		"--undershoot-pct=100", "--overshoot-pct=15",
		"--threads=1", "--noise-sensitivity=0",
	}
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-r91-720p", opts, targetKbps, sources, extra)

	type mbKey struct {
		Frame int
		MBRow int
		MBCol int
	}
	type mbRow struct {
		Mode     string
		RefFrame string
		MVRow    int
		MVCol    int
		Skip     bool
	}
	parseTrace := func(trace []byte) (map[mbKey]mbRow, map[string]int, int) {
		out := map[mbKey]mbRow{}
		modeCounts := map[string]int{}
		total := 0
		scan := bufio.NewScanner(bytes.NewReader(trace))
		scan.Buffer(make([]byte, 0, 64*1024), 256*1024*1024)
		for scan.Scan() {
			var row map[string]any
			if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
				continue
			}
			if typ, _ := row["type"].(string); typ != "mb" {
				continue
			}
			fi, _ := row["frame_index"].(float64)
			if int(fi) <= 0 {
				continue
			}
			mr, _ := row["mb_row"].(float64)
			mc, _ := row["mb_col"].(float64)
			m, _ := row["mode"].(string)
			ref, _ := row["ref_frame"].(string)
			mvR, _ := row["mv_row"].(float64)
			mvC, _ := row["mv_col"].(float64)
			var s bool
			if sv, ok := row["skip"].(bool); ok {
				s = sv
			} else if sv, ok := row["skip"].(float64); ok {
				s = sv != 0
			}
			key := mbKey{Frame: int(fi), MBRow: int(mr), MBCol: int(mc)}
			out[key] = mbRow{Mode: m, RefFrame: ref, MVRow: int(mvR), MVCol: int(mvC), Skip: s}
			if ref == "INTRA_FRAME" {
				modeCounts["INTRA"]++
			} else {
				modeCounts[m]++
			}
			total++
		}
		if err := scan.Err(); err != nil {
			t.Fatalf("scan trace: %v", err)
		}
		return out, modeCounts, total
	}

	gov, govModes, govTotal := parseTrace(govpxTrace)
	lib, libModes, libTotal := parseTrace(libvpxTrace)

	t.Logf("govpx total inter MB rows: %d", govTotal)
	t.Logf("libvpx total inter MB rows: %d", libTotal)

	// Walk MB rows again, this time accumulating EOB sums and skip counts.
	walkEOB := func(trace []byte) (eob int, skips int, mbRate int) {
		scan := bufio.NewScanner(bytes.NewReader(trace))
		scan.Buffer(make([]byte, 0, 64*1024), 256*1024*1024)
		for scan.Scan() {
			var row map[string]any
			if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
				continue
			}
			if typ, _ := row["type"].(string); typ != "mb" {
				continue
			}
			fi, _ := row["frame_index"].(float64)
			if int(fi) <= 0 {
				continue
			}
			if v, ok := row["eob_sum"].(float64); ok {
				eob += int(v)
			}
			var s bool
			if sv, ok := row["skip"].(bool); ok {
				s = sv
			} else if sv, ok := row["skip"].(float64); ok {
				s = sv != 0
			}
			if s {
				skips++
			}
			if v, ok := row["mb_rate"].(float64); ok {
				mbRate += int(v)
			}
		}
		return
	}
	gEOB, gSkips, gMBR := walkEOB(govpxTrace)
	lEOB, lSkips, lMBR := walkEOB(libvpxTrace)
	t.Logf("EOB sum     govpx=%d libvpx=%d ratio=%.3f", gEOB, lEOB, float64(gEOB)/float64(maxInt2(lEOB, 1)))
	t.Logf("skip count  govpx=%d libvpx=%d", gSkips, lSkips)
	t.Logf("MB rate sum govpx=%d libvpx=%d ratio=%.3f", gMBR, lMBR, float64(gMBR)/float64(maxInt2(lMBR, 1)))

	// Walk MB rows for mv-magnitude sum + qcoeff abs-sum to localize divergent
	// residual / MV cost.
	walkResidual := func(trace []byte) (qSum int64, mvSum int64) {
		scan := bufio.NewScanner(bytes.NewReader(trace))
		scan.Buffer(make([]byte, 0, 64*1024), 256*1024*1024)
		for scan.Scan() {
			var row map[string]any
			if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
				continue
			}
			if typ, _ := row["type"].(string); typ != "mb" {
				continue
			}
			fi, _ := row["frame_index"].(float64)
			if int(fi) <= 0 {
				continue
			}
			if mvR, ok := row["mv_row"].(float64); ok {
				if mvR < 0 {
					mvR = -mvR
				}
				mvSum += int64(mvR)
			}
			if mvC, ok := row["mv_col"].(float64); ok {
				if mvC < 0 {
					mvC = -mvC
				}
				mvSum += int64(mvC)
			}
			if qc, ok := row["qcoeff"].([]any); ok {
				for _, blk := range qc {
					blk2, _ := blk.([]any)
					for _, c := range blk2 {
						if cv, ok := c.(float64); ok {
							v := int64(cv)
							if v < 0 {
								v = -v
							}
							qSum += v
						}
					}
				}
			}
		}
		return
	}
	gQ, gMV := walkResidual(govpxTrace)
	lQ, lMV := walkResidual(libvpxTrace)
	t.Logf("qcoeff abs-sum  govpx=%d libvpx=%d ratio=%.3f", gQ, lQ, float64(gQ)/float64(maxInt2(int(lQ), 1)))
	t.Logf("mv abs-sum      govpx=%d libvpx=%d ratio=%.3f", gMV, lMV, float64(gMV)/float64(maxInt2(int(lMV), 1)))

	// Dump qcoeff of a single MB for sanity check.
	dumpFirstMB := func(trace []byte, label string) {
		scan := bufio.NewScanner(bytes.NewReader(trace))
		scan.Buffer(make([]byte, 0, 64*1024), 256*1024*1024)
		for scan.Scan() {
			var row map[string]any
			if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
				continue
			}
			if typ, _ := row["type"].(string); typ != "mb" {
				continue
			}
			fi, _ := row["frame_index"].(float64)
			if int(fi) != 1 {
				continue
			}
			mr, _ := row["mb_row"].(float64)
			mc, _ := row["mb_col"].(float64)
			if int(mr) != 5 || int(mc) != 5 {
				continue
			}
			t.Logf("%s frame=1 mb=(5,5) row: %s", label, scan.Text())
			return
		}
	}
	dumpFirstMB(govpxTrace, "govpx")
	dumpFirstMB(libvpxTrace, "libvpx")

	keys := []string{"ZEROMV", "NEARESTMV", "NEARMV", "NEWMV", "SPLITMV", "INTRA"}
	for _, k := range keys {
		gov := 100.0 * float64(govModes[k]) / float64(maxInt2(govTotal, 1))
		lib := 100.0 * float64(libModes[k]) / float64(maxInt2(libTotal, 1))
		t.Logf("mode %-10s govpx=%6.2f%%  libvpx=%6.2f%%  delta=%+6.2fpp", k, gov, lib, gov-lib)
	}

	// Tabulate per-(govpxMode, libvpxMode) pair counts.
	type pair struct {
		Gov string
		Lib string
	}
	pairCounts := map[pair]int{}
	divergent := []mbKey{}
	for key, lvRow := range lib {
		gvRow, ok := gov[key]
		if !ok {
			continue
		}
		gMode := gvRow.Mode
		lMode := lvRow.Mode
		if gvRow.RefFrame == "INTRA_FRAME" {
			gMode = "INTRA"
		}
		if lvRow.RefFrame == "INTRA_FRAME" {
			lMode = "INTRA"
		}
		pairCounts[pair{Gov: gMode, Lib: lMode}]++
		if gMode != lMode {
			divergent = append(divergent, key)
		}
	}
	t.Logf("divergent MBs (mode mismatch): %d", len(divergent))

	// Print pair table.
	type pairEntry struct {
		Pair  pair
		Count int
	}
	entries := make([]pairEntry, 0, len(pairCounts))
	for p, c := range pairCounts {
		entries = append(entries, pairEntry{Pair: p, Count: c})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Count > entries[j].Count })
	var pairBuf bytes.Buffer
	fmt.Fprintln(&pairBuf, "gov_mode,lib_mode,count")
	for _, e := range entries {
		fmt.Fprintf(&pairBuf, "%s,%s,%d\n", e.Pair.Gov, e.Pair.Lib, e.Count)
	}
	t.Logf("pair distribution:\n%s", pairBuf.String())

	// Identify the top divergent (gov NEARMV, lib NEARESTMV) and (gov NEWMV, lib NEARESTMV) examples.
	probe := func(gMode, lMode string, max int) {
		count := 0
		for _, key := range divergent {
			gv := gov[key]
			lv := lib[key]
			gm := gv.Mode
			lm := lv.Mode
			if gv.RefFrame == "INTRA_FRAME" {
				gm = "INTRA"
			}
			if lv.RefFrame == "INTRA_FRAME" {
				lm = "INTRA"
			}
			if gm != gMode || lm != lMode {
				continue
			}
			t.Logf("  frame=%d mb=(%d,%d) gov:%s/%s/(%d,%d) lib:%s/%s/(%d,%d)",
				key.Frame, key.MBRow, key.MBCol,
				gv.Mode, gv.RefFrame, gv.MVRow, gv.MVCol,
				lv.Mode, lv.RefFrame, lv.MVRow, lv.MVCol)
			count++
			if count >= max {
				return
			}
		}
	}
	t.Log("top (govNEARMV, libNEARESTMV) examples:")
	probe("NEARMV", "NEARESTMV", 8)
	t.Log("top (govNEWMV, libNEARESTMV) examples:")
	probe("NEWMV", "NEARESTMV", 8)
	t.Log("top (govNEARMV, libZEROMV) examples:")
	probe("NEARMV", "ZEROMV", 8)
	t.Log("top (govNEWMV, libZEROMV) examples:")
	probe("NEWMV", "ZEROMV", 8)
}

func maxInt2(a, b int) int {
	if a > b {
		return a
	}
	return b
}
