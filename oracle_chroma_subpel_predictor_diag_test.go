package govpx

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestOracleChromaSubpelPredictorDiff is a focused localization harness for
// the inter-frame chroma sub-pel rounding gap that breaks byte-identity at
// sizes >64x64 (see plan.md "Encoder Quality" first bullet). The test
// drives both encoders on the 128x128 panning fixture, captures the inter
// predictor for MB(0,0) of inter frame 1 from each side, and prints a
// per-pixel diff. Gated behind GOVPX_DEBUG=1 because it is a diagnostic,
// not a parity gate.
func TestOracleChromaSubpelPredictorDiff(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the chroma sub-pel predictor diagnostic")
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
		Width:                           width,
		Height:                          height,
		FPS:                             fps,
		RateControlMode:                 RateControlCBR,
		TargetBitrateKbps:               targetKbps,
		MinQuantizer:                    4,
		MaxQuantizer:                    56,
		Deadline:                        DeadlineRealtime,
		CpuUsed:                         8,
		KeyFrameInterval:                999,
		BufferSizeMs:                    600,
		BufferInitialSizeMs:             400,
		BufferOptimalSizeMs:             500,
		OracleTracePredictorDump:        true,
		OracleTracePredictorDumpAllRows: os.Getenv("GOVPX_DEBUG_ALL_ROWS") == "1",
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxChromaSubpelTrace(t, vpxencOracle, "chroma-subpel-128x128", opts, targetKbps, sources)

	for _, rowType := range []string{"predictor", "reconstructed"} {
		t.Run(rowType, func(t *testing.T) {
			runPredictorDiff(t, rowType, govpxTrace, libvpxTrace)
		})
	}

	t.Run("mb_decisions", func(t *testing.T) {
		diffMBDecisions(t, govpxTrace, libvpxTrace)
	})

	t.Run("last_ref_window", func(t *testing.T) {
		diffLastRefWindow(t, govpxTrace, libvpxTrace)
	})

	t.Run("frame_state", func(t *testing.T) {
		dumpFrameState(t, govpxTrace, libvpxTrace)
	})
}

func dumpFrameState(t *testing.T, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
	dump := func(label string, tr []byte) {
		scan := bufio.NewScanner(bytes.NewReader(tr))
		scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
		for scan.Scan() {
			var row map[string]any
			if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
				continue
			}
			if row["type"] != "frame" {
				continue
			}
			fi, _ := row["frame_index"].(float64)
			lvl, _ := row["loop_filter_level"].(float64)
			q, _ := row["q_index"].(float64)
			ft, _ := row["frame_type"].(string)
			t.Logf("%s frame=%d type=%s q=%d lf_level=%d",
				label, int(fi), ft, int(q), int(lvl))
		}
	}
	dump("govpx", govpxTrace)
	dump("libvpx", libvpxTrace)
}

// diffLastRefWindow compares the LAST reference chroma planes including
// border content between govpx and libvpx at each inter frame's encode
// start. A diff here confirms the residual divergence in frame N+1's
// predictor cascades from a border-content drift in frame N's
// reconstructed reference (post loop-filter + extension).
func diffLastRefWindow(t *testing.T, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
	gov := parseLastRefWindowRows(t, govpxTrace)
	lib := parseLastRefWindowRows(t, libvpxTrace)
	keys := make(map[lastRefKey]bool)
	for k := range gov {
		keys[k] = true
	}
	for k := range lib {
		keys[k] = true
	}
	keyList := make([]lastRefKey, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Slice(keyList, func(i, j int) bool {
		if keyList[i].FrameIndex != keyList[j].FrameIndex {
			return keyList[i].FrameIndex < keyList[j].FrameIndex
		}
		return keyList[i].Plane < keyList[j].Plane
	})
	var sb strings.Builder
	for _, k := range keyList {
		g, gok := gov[k]
		l, lok := lib[k]
		if !gok || !lok {
			fmt.Fprintf(&sb, "frame=%d plane=%s govpx_present=%v libvpx_present=%v\n",
				k.FrameIndex, k.Plane, gok, lok)
			continue
		}
		gb, _ := hex.DecodeString(g.Hex)
		lb, _ := hex.DecodeString(l.Hex)
		if len(gb) != len(lb) {
			fmt.Fprintf(&sb, "frame=%d plane=%s size mismatch govpx=%d libvpx=%d (govpx %dx%d border t=%d l=%d, libvpx %dx%d border t=%d l=%d)\n",
				k.FrameIndex, k.Plane, len(gb), len(lb),
				g.Width, g.Height, g.BorderTop, g.BorderLeft,
				l.Width, l.Height, l.BorderTop, l.BorderLeft)
			continue
		}
		differ := 0
		w := g.Width
		for i := range gb {
			if gb[i] != lb[i] {
				differ++
			}
		}
		fmt.Fprintf(&sb, "frame=%d plane=%s w=%d h=%d border_top=%d border_left=%d diff=%d/%d bytes\n",
			k.FrameIndex, k.Plane, g.Width, g.Height, g.BorderTop, g.BorderLeft, differ, len(gb))
		if differ == 0 {
			continue
		}
		// Show first 8 diverging rows.
		shown := 0
		for r := 0; r < g.Height && shown < 8; r++ {
			anyDiff := false
			for c := 0; c < w; c++ {
				if gb[r*w+c] != lb[r*w+c] {
					anyDiff = true
					break
				}
			}
			if !anyDiff {
				continue
			}
			fmt.Fprintf(&sb, "  row %2d (relY=%d):", r, r-g.BorderTop)
			for c := 0; c < w; c++ {
				gv := gb[r*w+c]
				lv := lb[r*w+c]
				if gv == lv {
					fmt.Fprintf(&sb, " %3d", gv)
				} else {
					fmt.Fprintf(&sb, " %3d/%3d", gv, lv)
				}
			}
			fmt.Fprintln(&sb)
			shown++
		}
	}
	t.Logf("last_ref_window diff:\n%s", sb.String())
}

type lastRefKey struct {
	FrameIndex uint64
	Plane      string
}

func parseLastRefWindowRows(t *testing.T, trace []byte) map[lastRefKey]oracleTraceLastRefWindowRow {
	t.Helper()
	out := make(map[lastRefKey]oracleTraceLastRefWindowRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if t, _ := row["type"].(string); t != "last_ref_window" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		plane, _ := row["plane"].(string)
		w, _ := row["width"].(float64)
		h, _ := row["height"].(float64)
		bt, _ := row["border_top"].(float64)
		bl, _ := row["border_left"].(float64)
		hexStr, _ := row["hex"].(string)
		key := lastRefKey{FrameIndex: uint64(fi), Plane: plane}
		out[key] = oracleTraceLastRefWindowRow{
			Type:       "last_ref_window",
			FrameIndex: uint64(fi),
			Plane:      plane,
			Width:      int(w),
			Height:     int(h),
			BorderTop:  int(bt),
			BorderLeft: int(bl),
			Hex:        hexStr,
		}
	}
	return out
}

// diffMBDecisions compares the MV / mode / ref_frame for MBs in row 0 of
// frames 1..3 between govpx and libvpx so the harness can flag whether
// any predictor divergence is rooted in different motion-vector choices.
func diffMBDecisions(t *testing.T, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
	govpxMBs := parseChromaSubpelMBRows(t, govpxTrace)
	libvpxMBs := parseChromaSubpelMBRows(t, libvpxTrace)

	keys := make(map[chromaSubpelMBKey]bool)
	for k := range govpxMBs {
		keys[k] = true
	}
	for k := range libvpxMBs {
		keys[k] = true
	}
	keyList := make([]chromaSubpelMBKey, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Slice(keyList, func(i, j int) bool {
		if keyList[i].FrameIndex != keyList[j].FrameIndex {
			return keyList[i].FrameIndex < keyList[j].FrameIndex
		}
		if keyList[i].MBRow != keyList[j].MBRow {
			return keyList[i].MBRow < keyList[j].MBRow
		}
		return keyList[i].MBCol < keyList[j].MBCol
	})

	var sb strings.Builder
	mismatches := 0
	for _, k := range keyList {
		g, gok := govpxMBs[k]
		l, lok := libvpxMBs[k]
		if !gok || !lok {
			continue
		}
		ok := g.Mode == l.Mode && g.RefFrame == l.RefFrame &&
			g.MVRow == l.MVRow && g.MVCol == l.MVCol &&
			g.Skip == l.Skip
		marker := " "
		if !ok {
			marker = "*"
			mismatches++
		}
		fmt.Fprintf(&sb,
			"%s frame=%d mb=(%d,%d) govpx[ref=%s mode=%s mv=(%d,%d) skip=%v] libvpx[ref=%s mode=%s mv=(%d,%d) skip=%v]\n",
			marker, k.FrameIndex, k.MBRow, k.MBCol,
			g.RefFrame, g.Mode, g.MVRow, g.MVCol, g.Skip,
			l.RefFrame, l.Mode, l.MVRow, l.MVCol, l.Skip)
	}
	t.Logf("MB decisions (frame 1..3, row 0):\n%s\nmismatches: %d", sb.String(), mismatches)
}

type chromaSubpelMBKey struct {
	FrameIndex uint64
	MBRow      int
	MBCol      int
}

type chromaSubpelMBDecision struct {
	Mode     string
	RefFrame string
	MVRow    int
	MVCol    int
	Skip     bool
}

func parseChromaSubpelMBRows(t *testing.T, trace []byte) map[chromaSubpelMBKey]chromaSubpelMBDecision {
	t.Helper()
	out := make(map[chromaSubpelMBKey]chromaSubpelMBDecision)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		if t, _ := row["type"].(string); t != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		mr, _ := row["mb_row"].(float64)
		mc, _ := row["mb_col"].(float64)
		if int(fi) < 1 || int(fi) > 3 || int(mr) != 0 {
			continue
		}
		mode, _ := row["mode"].(string)
		ref, _ := row["ref_frame"].(string)
		mvR, _ := row["mv_row"].(float64)
		mvC, _ := row["mv_col"].(float64)
		skip, _ := row["skip"].(bool)
		key := chromaSubpelMBKey{FrameIndex: uint64(fi), MBRow: int(mr), MBCol: int(mc)}
		out[key] = chromaSubpelMBDecision{
			Mode:     mode,
			RefFrame: ref,
			MVRow:    int(mvR),
			MVCol:    int(mvC),
			Skip:     skip,
		}
	}
	return out
}

func runPredictorDiff(t *testing.T, rowType string, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
	govpxRows := parsePredictorRowsByType(t, "govpx", govpxTrace, rowType)
	libvpxRows := parsePredictorRowsByType(t, "libvpx", libvpxTrace, rowType)

	if len(govpxRows) == 0 {
		t.Fatalf("govpx emitted no %s rows; check OracleTracePredictorDump wiring", rowType)
	}
	if len(libvpxRows) == 0 {
		t.Fatalf("libvpx emitted no %s rows; check GOVPX_ORACLE_PREDICTOR_DUMP wiring", rowType)
	}

	keys := make(map[predictorKey]bool)
	for k := range govpxRows {
		keys[k] = true
	}
	for k := range libvpxRows {
		keys[k] = true
	}
	keyList := make([]predictorKey, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Slice(keyList, func(i, j int) bool {
		if keyList[i].FrameIndex != keyList[j].FrameIndex {
			return keyList[i].FrameIndex < keyList[j].FrameIndex
		}
		if keyList[i].MBRow != keyList[j].MBRow {
			return keyList[i].MBRow < keyList[j].MBRow
		}
		if keyList[i].MBCol != keyList[j].MBCol {
			return keyList[i].MBCol < keyList[j].MBCol
		}
		return keyList[i].Plane < keyList[j].Plane
	})

	var diagBuilder strings.Builder
	totalDiffs := 0
	for _, k := range keyList {
		g, gok := govpxRows[k]
		l, lok := libvpxRows[k]
		if !gok {
			fmt.Fprintf(&diagBuilder, "frame=%d mb=(%d,%d) plane=%s govpx=MISSING libvpx=present\n",
				k.FrameIndex, k.MBRow, k.MBCol, k.Plane)
			continue
		}
		if !lok {
			fmt.Fprintf(&diagBuilder, "frame=%d mb=(%d,%d) plane=%s libvpx=MISSING govpx=present\n",
				k.FrameIndex, k.MBRow, k.MBCol, k.Plane)
			continue
		}
		gb, err := hex.DecodeString(g.Hex)
		if err != nil {
			t.Fatalf("govpx hex decode (frame=%d plane=%s): %v", k.FrameIndex, k.Plane, err)
		}
		lb, err := hex.DecodeString(l.Hex)
		if err != nil {
			t.Fatalf("libvpx hex decode (frame=%d plane=%s): %v", k.FrameIndex, k.Plane, err)
		}
		if len(gb) != len(lb) {
			fmt.Fprintf(&diagBuilder, "frame=%d mb=(%d,%d) plane=%s size mismatch govpx=%d libvpx=%d\n",
				k.FrameIndex, k.MBRow, k.MBCol, k.Plane, len(gb), len(lb))
			continue
		}
		w := g.Width
		differ := 0
		for i := range gb {
			if gb[i] != lb[i] {
				differ++
			}
		}
		if differ == 0 {
			fmt.Fprintf(&diagBuilder, "frame=%d mb=(%d,%d) plane=%s w=%d h=%d MATCH (%d bytes)\n",
				k.FrameIndex, k.MBRow, k.MBCol, k.Plane, w, g.Height, len(gb))
			continue
		}
		totalDiffs += differ
		fmt.Fprintf(&diagBuilder, "frame=%d mb=(%d,%d) plane=%s w=%d h=%d DIFFER %d/%d bytes\n",
			k.FrameIndex, k.MBRow, k.MBCol, k.Plane, w, g.Height, differ, len(gb))
		// Per-row, per-col diff. Print only rows that have any divergence.
		for r := 0; r < g.Height; r++ {
			rowDiff := false
			for c := 0; c < w; c++ {
				if gb[r*w+c] != lb[r*w+c] {
					rowDiff = true
					break
				}
			}
			if !rowDiff {
				continue
			}
			fmt.Fprintf(&diagBuilder, "  row %2d:", r)
			for c := 0; c < w; c++ {
				gv := gb[r*w+c]
				lv := lb[r*w+c]
				if gv == lv {
					fmt.Fprintf(&diagBuilder, " %3d", gv)
				} else {
					delta := int(gv) - int(lv)
					fmt.Fprintf(&diagBuilder, " %3d/%3d(%+d)", gv, lv, delta)
				}
			}
			fmt.Fprintln(&diagBuilder)
		}
	}
	t.Logf("%s diff:\n%s", rowType, diagBuilder.String())
	if totalDiffs == 0 {
		t.Logf("all %s rows MATCH", rowType)
	} else {
		t.Logf("%s: total diverging bytes across all captured planes: %d", rowType, totalDiffs)
	}
}

type predictorKey struct {
	FrameIndex uint64
	MBRow      int
	MBCol      int
	Plane      string
}

func parsePredictorRowsByType(t *testing.T, label string, trace []byte, wantType string) map[predictorKey]oracleTracePredictorRow {
	t.Helper()
	out := make(map[predictorKey]oracleTracePredictorRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue // skip non-JSON lines defensively
		}
		typ, _ := row["type"].(string)
		if typ != wantType {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		mr, _ := row["mb_row"].(float64)
		mc, _ := row["mb_col"].(float64)
		plane, _ := row["plane"].(string)
		w, _ := row["width"].(float64)
		h, _ := row["height"].(float64)
		hexStr, _ := row["hex"].(string)
		key := predictorKey{
			FrameIndex: uint64(fi),
			MBRow:      int(mr),
			MBCol:      int(mc),
			Plane:      plane,
		}
		out[key] = oracleTracePredictorRow{
			Type:       typ,
			FrameIndex: uint64(fi),
			MBRow:      int(mr),
			MBCol:      int(mc),
			Plane:      plane,
			Width:      int(w),
			Height:     int(h),
			Hex:        hexStr,
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan %s trace: %v", label, err)
	}
	return out
}

// captureLibvpxChromaSubpelTrace runs the libvpx oracle with the predictor
// dump enabled and returns the resulting JSONL trace.
func captureLibvpxChromaSubpelTrace(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image) []byte {
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
		"--end-usage=cbr",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		yuvPath,
	}
	cmd := exec.Command(vpxencOracle, args...)
	envs := append(os.Environ(),
		"GOVPX_ORACLE_TRACE_OUT="+tracePath,
		"GOVPX_ORACLE_PREDICTOR_DUMP=1",
	)
	if os.Getenv("GOVPX_DEBUG_ALL_ROWS") == "1" {
		envs = append(envs, "GOVPX_ORACLE_PREDICTOR_DUMP_ALL_ROWS=1")
	}
	cmd.Env = envs
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
