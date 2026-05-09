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

// TestDiagR11JPreLFReconScoreboard008 is a focused R11-J localizer for
// the 128x128 panning realtime CBR cpu8 fixture using the SCOREBOARD's
// exact option set (no buffer sizing -> libvpx defaults). Under those
// options the chroma-subpel scoreboard reports y_adler / u_adler /
// v_adler mismatch on every inter frame; the predictor-diag test (which
// sets custom buffer sizes) shows frame 1 fully byte-identical and
// divergence starting at frame 2 chroma. R11-J is about closing the
// picker-driven pre-LF reconstruction divergence under the scoreboard
// config.
//
// This test enables OracleTracePredictorDump on the govpx side and
// GOVPX_ORACLE_PREDICTOR_DUMP on the libvpx side, and walks frame 1's
// per-MB mb / predictor / reconstructed rows in MB scan order to find
// the first divergent MB and dump its qcoeff (decoder-visible
// post-quant coefficients), eob, mode, ref, and MV from both sides.
//
// Gated behind GOVPX_DEBUG=1 because it is a diagnostic, not a parity
// gate. Set GOVPX_DEBUG_ALL_ROWS=1 to widen predictor/reconstructed
// emission past MB row 0 (matches the chroma-subpel diag harness).
func TestDiagR11JPreLFReconScoreboard008(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the R11-J pre-LF reconstruction diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 128
		height     = 128
		fps        = 30
		targetKbps = 700
		frames     = 4
	)
	// Scoreboard config: realtime CBR cpu8, no buffer overrides.
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
		OracleTracePredictorDump:        true,
		OracleTracePredictorDumpAllRows: os.Getenv("GOVPX_DEBUG_ALL_ROWS") == "1",
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxR11JTrace(t, vpxencOracle, "r11j-128x128", opts, targetKbps, sources)

	t.Run("frame_state", func(t *testing.T) {
		dumpFrameState(t, govpxTrace, libvpxTrace)
	})

	t.Run("lf_trial_diff", func(t *testing.T) {
		dumpR11JLFTrialDiff(t, govpxTrace, libvpxTrace)
	})

	t.Run("predictor_first_divergent_mb", func(t *testing.T) {
		findFirstDivergentMBR11J(t, "predictor", govpxTrace, libvpxTrace)
	})

	t.Run("reconstructed_first_divergent_mb", func(t *testing.T) {
		findFirstDivergentMBR11J(t, "reconstructed", govpxTrace, libvpxTrace)
	})

	t.Run("mb_qcoeff_first_divergent_mb", func(t *testing.T) {
		findFirstDivergentMBQCoeff(t, govpxTrace, libvpxTrace)
	})

	t.Run("frame1_mb_decisions", func(t *testing.T) {
		diffMBDecisionsAllRows(t, govpxTrace, libvpxTrace, 1)
	})

	t.Run("frame1_col7_bpred_bmodes", func(t *testing.T) {
		diffR12CCol7BPredBModes(t, govpxTrace, libvpxTrace, 1)
	})
}

// diffR12CCol7BPredBModes dumps the per-sub-block intra mode picks for the
// 4 col-7 right-edge B_PRED MBs (mb=(2..5,7)) on the 128x128 frame 1 inter
// frame and reports per-block mismatches. This is the R12-C focused diag
// for closing the col-7 B_PRED Y reconstruction gap.
func diffR12CCol7BPredBModes(t *testing.T, govpxTrace []byte, libvpxTrace []byte, frameIdx uint64) {
	t.Helper()
	gov := parseR12CMBRowsWithBModes(t, govpxTrace, frameIdx)
	lib := parseR12CMBRowsWithBModes(t, libvpxTrace, frameIdx)

	col := 7
	rows := []int{2, 3, 4, 5}
	for _, row := range rows {
		k := r11jMBKey{MBRow: row, MBCol: col}
		g, gok := gov[k]
		l, lok := lib[k]
		if !gok || !lok {
			t.Logf("mb=(%d,%d): missing trace (govpx=%v libvpx=%v)", row, col, gok, lok)
			continue
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "mb=(%d,%d) govpx[mode=%s ref=%s mv=(%d,%d)] libvpx[mode=%s ref=%s mv=(%d,%d)]",
			row, col,
			g.Mode, g.RefFrame, g.MVRow, g.MVCol,
			l.Mode, l.RefFrame, l.MVRow, l.MVCol)
		fmt.Fprintln(&sb)
		if g.Mode == "B_PRED" && l.Mode == "B_PRED" {
			diffCount := 0
			for blk := range 16 {
				gB := ""
				lB := ""
				if blk < len(g.BModes) {
					gB = g.BModes[blk]
				}
				if blk < len(l.BModes) {
					lB = l.BModes[blk]
				}
				marker := ""
				if gB != lB {
					marker = "  *"
					diffCount++
				}
				fmt.Fprintf(&sb, "  blk=%2d govpx=%-10s libvpx=%-10s%s\n", blk, gB, lB, marker)
			}
			fmt.Fprintf(&sb, "  total b_mode mismatches: %d/16\n", diffCount)
		}
		t.Log(sb.String())
	}
}

type r12cMBRow struct {
	Mode     string
	RefFrame string
	MVRow    int
	MVCol    int
	BModes   []string
}

func parseR12CMBRowsWithBModes(t *testing.T, trace []byte, frameIdx uint64) map[r11jMBKey]r12cMBRow {
	t.Helper()
	out := make(map[r11jMBKey]r12cMBRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if uint64(fi) != frameIdx {
			continue
		}
		mr, _ := row["mb_row"].(float64)
		mc, _ := row["mb_col"].(float64)
		mode, _ := row["mode"].(string)
		ref, _ := row["ref_frame"].(string)
		mvR, _ := row["mv_row"].(float64)
		mvC, _ := row["mv_col"].(float64)
		var bModes []string
		if bms, ok := row["b_modes"].([]any); ok {
			bModes = make([]string, len(bms))
			for i, v := range bms {
				bModes[i], _ = v.(string)
			}
		}
		out[r11jMBKey{MBRow: int(mr), MBCol: int(mc)}] = r12cMBRow{
			Mode:     mode,
			RefFrame: ref,
			MVRow:    int(mvR),
			MVCol:    int(mvC),
			BModes:   bModes,
		}
	}
	return out
}

// dumpR11JLFTrialDiff prints a side-by-side LF fast-picker per-trial
// table for each frame. Reuses the helpers from
// oracle_lf_trial_diag_test.go.
func dumpR11JLFTrialDiff(t *testing.T, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
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
	for _, frame := range frameIndexes {
		var b strings.Builder
		fmt.Fprintf(&b, "\n=== R11-J LF fast-picker per-trial table for frame %d ===\n", frame)
		fmt.Fprintf(&b, "  govpx: chosen_level=%d   q_index=%d\n", govpxFrames[frame], govpxQ[frame])
		fmt.Fprintf(&b, "  libvpx: chosen_level=%d  q_index=%d\n", libvpxFrames[frame], libvpxQ[frame])
		gRows := govpxRows[frame]
		lRows := libvpxRows[frame]
		fmt.Fprintf(&b, "  govpx eval order: ")
		for i, r := range gRows {
			if i > 0 {
				fmt.Fprintf(&b, " -> ")
			}
			fmt.Fprintf(&b, "%s:%d=%d", r.Phase, r.Level, r.YSSE)
		}
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "  libvpx eval order: ")
		for i, r := range lRows {
			if i > 0 {
				fmt.Fprintf(&b, " -> ")
			}
			fmt.Fprintf(&b, "%s:%d=%d", r.Phase, r.Level, r.YSSE)
		}
		fmt.Fprintln(&b)
		t.Log(b.String())
	}
}

// captureLibvpxR11JTrace mirrors captureLibvpxChromaSubpelTrace but
// without the --buf-* overrides so the libvpx oracle uses defaults
// matching the chroma-subpel scoreboard's encoder options.
func captureLibvpxR11JTrace(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image) []byte {
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

type r11jMBKeyXY struct {
	row int
	col int
}

type r11jMBDiff struct {
	yDiff          int
	yLen           int
	uDiff          int
	uLen           int
	vDiff          int
	vLen           int
	firstDiffPlane string
	firstDiffPos   int
	gByte          byte
	lByte          byte
}

// findFirstDivergentMBR11J walks frame 1 in MB scan order and reports
// the first MB whose predictor/reconstructed bytes differ between
// govpx and libvpx.
func findFirstDivergentMBR11J(t *testing.T, rowType string, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
	gov := parsePredictorRowsByType(t, "govpx", govpxTrace, rowType)
	lib := parsePredictorRowsByType(t, "libvpx", libvpxTrace, rowType)
	if len(gov) == 0 || len(lib) == 0 {
		t.Fatalf("%s rows missing govpx=%d libvpx=%d", rowType, len(gov), len(lib))
	}

	keys := make([]predictorKey, 0)
	seen := make(map[predictorKey]bool)
	add := func(k predictorKey) {
		if k.FrameIndex != 1 {
			return
		}
		if seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	for k := range gov {
		add(k)
	}
	for k := range lib {
		add(k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].MBRow != keys[j].MBRow {
			return keys[i].MBRow < keys[j].MBRow
		}
		if keys[i].MBCol != keys[j].MBCol {
			return keys[i].MBCol < keys[j].MBCol
		}
		return r11jPlaneOrder(keys[i].Plane) < r11jPlaneOrder(keys[j].Plane)
	})

	mbs := make(map[r11jMBKeyXY]*r11jMBDiff)
	mbOrder := make([]r11jMBKeyXY, 0)
	for _, k := range keys {
		mk := r11jMBKeyXY{row: k.MBRow, col: k.MBCol}
		md, ok := mbs[mk]
		if !ok {
			md = &r11jMBDiff{}
			mbs[mk] = md
			mbOrder = append(mbOrder, mk)
		}
		g, gok := gov[k]
		l, lok := lib[k]
		if !gok || !lok {
			continue
		}
		gb, _ := hex.DecodeString(g.Hex)
		lb, _ := hex.DecodeString(l.Hex)
		if len(gb) != len(lb) {
			continue
		}
		differ := 0
		firstPos := -1
		for i := range gb {
			if gb[i] != lb[i] {
				differ++
				if firstPos < 0 {
					firstPos = i
				}
			}
		}
		switch k.Plane {
		case "y":
			md.yDiff = differ
			md.yLen = len(gb)
		case "u":
			md.uDiff = differ
			md.uLen = len(gb)
		case "v":
			md.vDiff = differ
			md.vLen = len(gb)
		}
		if differ > 0 && md.firstDiffPlane == "" {
			md.firstDiffPlane = k.Plane
			md.firstDiffPos = firstPos
			if firstPos >= 0 && firstPos < len(gb) {
				md.gByte = gb[firstPos]
				md.lByte = lb[firstPos]
			}
		}
	}

	sort.Slice(mbOrder, func(i, j int) bool {
		if mbOrder[i].row != mbOrder[j].row {
			return mbOrder[i].row < mbOrder[j].row
		}
		return mbOrder[i].col < mbOrder[j].col
	})

	var firstDivergentMB *r11jMBKeyXY
	var sb strings.Builder
	divergentCount := 0
	for _, mk := range mbOrder {
		md := mbs[mk]
		total := md.yDiff + md.uDiff + md.vDiff
		if total == 0 {
			continue
		}
		divergentCount++
		if firstDivergentMB == nil {
			cp := mk
			firstDivergentMB = &cp
		}
		fmt.Fprintf(&sb, "  mb=(%d,%d) y_diff=%d/%d u_diff=%d/%d v_diff=%d/%d first_plane=%s first_pos=%d (govpx=%d libvpx=%d delta=%+d)\n",
			mk.row, mk.col, md.yDiff, md.yLen, md.uDiff, md.uLen, md.vDiff, md.vLen,
			md.firstDiffPlane, md.firstDiffPos, md.gByte, md.lByte, int(md.gByte)-int(md.lByte))
	}
	if firstDivergentMB == nil {
		t.Logf("%s: frame 1 byte-identical on all sampled MBs (%d MBs total)", rowType, len(mbOrder))
		return
	}
	md := mbs[*firstDivergentMB]
	t.Logf("%s frame 1 first divergent MB: mb=(%d,%d) plane=%s pos=%d (govpx=%d libvpx=%d delta=%+d)",
		rowType, firstDivergentMB.row, firstDivergentMB.col,
		md.firstDiffPlane, md.firstDiffPos,
		md.gByte, md.lByte, int(md.gByte)-int(md.lByte))
	t.Logf("%s frame 1 all divergent MBs (%d total):\n%s", rowType, divergentCount, sb.String())
}

func r11jPlaneOrder(plane string) int {
	switch plane {
	case "y":
		return 0
	case "u":
		return 1
	case "v":
		return 2
	default:
		return 3
	}
}

// findFirstDivergentMBQCoeff walks frame 1 mb rows in scan order and
// reports the first MB whose qcoeff/eob/mode/ref/mv differ between
// govpx and libvpx.
func findFirstDivergentMBQCoeff(t *testing.T, govpxTrace []byte, libvpxTrace []byte) {
	t.Helper()
	gov := parseR11JMBRows(t, govpxTrace, 1)
	lib := parseR11JMBRows(t, libvpxTrace, 1)

	keys := make(map[r11jMBKey]bool)
	for k := range gov {
		keys[k] = true
	}
	for k := range lib {
		keys[k] = true
	}
	keyList := make([]r11jMBKey, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Slice(keyList, func(i, j int) bool {
		if keyList[i].MBRow != keyList[j].MBRow {
			return keyList[i].MBRow < keyList[j].MBRow
		}
		return keyList[i].MBCol < keyList[j].MBCol
	})

	type qDiff struct {
		key            r11jMBKey
		modeDiff       bool
		refDiff        bool
		mvDiff         bool
		skipDiff       bool
		eobDiff        bool
		qcoeffDiff     bool
		firstDiffBlock int
		firstDiffPos   int
		gQ             int16
		lQ             int16
		gMode          string
		lMode          string
		gRef           string
		lRef           string
		gMV            [2]int
		lMV            [2]int
		gSkip          bool
		lSkip          bool
		gEOB           [25]int
		lEOB           [25]int
	}

	diffs := make([]qDiff, 0)
	for _, k := range keyList {
		g, gok := gov[k]
		l, lok := lib[k]
		if !gok || !lok {
			continue
		}
		d := qDiff{key: k}
		d.gMode, d.lMode = g.Mode, l.Mode
		d.gRef, d.lRef = g.RefFrame, l.RefFrame
		d.gMV = [2]int{g.MVRow, g.MVCol}
		d.lMV = [2]int{l.MVRow, l.MVCol}
		d.gSkip, d.lSkip = g.Skip, l.Skip
		d.gEOB, d.lEOB = g.EOB, l.EOB
		d.modeDiff = g.Mode != l.Mode
		d.refDiff = g.RefFrame != l.RefFrame
		d.mvDiff = g.MVRow != l.MVRow || g.MVCol != l.MVCol
		d.skipDiff = g.Skip != l.Skip
		for i := range 25 {
			if g.EOB[i] != l.EOB[i] {
				d.eobDiff = true
				break
			}
		}
		d.firstDiffBlock = -1
		d.firstDiffPos = -1
		for b := range 25 {
			differ := false
			for p := range 16 {
				if g.QCoeff[b][p] != l.QCoeff[b][p] {
					if d.firstDiffBlock < 0 {
						d.firstDiffBlock = b
						d.firstDiffPos = p
						d.gQ = g.QCoeff[b][p]
						d.lQ = l.QCoeff[b][p]
					}
					differ = true
				}
			}
			if differ {
				d.qcoeffDiff = true
			}
			if d.qcoeffDiff && d.firstDiffBlock >= 0 {
				break
			}
		}
		if d.modeDiff || d.refDiff || d.mvDiff || d.skipDiff || d.eobDiff || d.qcoeffDiff {
			diffs = append(diffs, d)
		}
	}

	if len(diffs) == 0 {
		t.Logf("frame 1 qcoeff/mb decision byte-identical across all %d MBs", len(keyList))
		return
	}

	first := diffs[0]
	t.Logf("frame 1 first divergent qcoeff MB: mb=(%d,%d) mode=%s/%s ref=%s/%s mv=(%d,%d)/(%d,%d) skip=%v/%v",
		first.key.MBRow, first.key.MBCol, first.gMode, first.lMode, first.gRef, first.lRef,
		first.gMV[0], first.gMV[1], first.lMV[0], first.lMV[1], first.gSkip, first.lSkip)
	if first.eobDiff {
		t.Logf("  EOB diff: govpx=%v libvpx=%v", first.gEOB, first.lEOB)
	}
	if first.qcoeffDiff {
		t.Logf("  first qcoeff diff at block=%d pos=%d govpx=%d libvpx=%d delta=%+d",
			first.firstDiffBlock, first.firstDiffPos, first.gQ, first.lQ, int(first.gQ)-int(first.lQ))
	}

	// Detailed per-block dump for the first divergent MB.
	if g, gok := gov[first.key]; gok {
		if l, lok := lib[first.key]; lok {
			var sb strings.Builder
			fmt.Fprintf(&sb, "  per-block qcoeff for first divergent MB(%d,%d):\n", first.key.MBRow, first.key.MBCol)
			for b := range 25 {
				anyDiff := false
				for p := range 16 {
					if g.QCoeff[b][p] != l.QCoeff[b][p] {
						anyDiff = true
						break
					}
				}
				gEOB := g.EOB[b]
				lEOB := l.EOB[b]
				if !anyDiff && gEOB == lEOB {
					continue
				}
				fmt.Fprintf(&sb, "    blk=%2d eob=%d/%d", b, gEOB, lEOB)
				for p := range 16 {
					if g.QCoeff[b][p] != l.QCoeff[b][p] {
						fmt.Fprintf(&sb, " [%d]=%d/%d", p, g.QCoeff[b][p], l.QCoeff[b][p])
					}
				}
				fmt.Fprintln(&sb)
			}
			t.Logf("%s", sb.String())
		}
	}

	var sb strings.Builder
	for _, d := range diffs {
		fmt.Fprintf(&sb, "  mb=(%d,%d) mode_diff=%v ref_diff=%v mv_diff=%v skip_diff=%v eob_diff=%v qcoeff_diff=%v",
			d.key.MBRow, d.key.MBCol, d.modeDiff, d.refDiff, d.mvDiff, d.skipDiff, d.eobDiff, d.qcoeffDiff)
		if d.qcoeffDiff {
			fmt.Fprintf(&sb, " first_block=%d first_pos=%d govpx=%d libvpx=%d",
				d.firstDiffBlock, d.firstDiffPos, d.gQ, d.lQ)
		}
		fmt.Fprintln(&sb)
	}
	t.Logf("frame 1 qcoeff/mb divergent MBs (%d total):\n%s", len(diffs), sb.String())
}

type r11jMBKey struct {
	MBRow int
	MBCol int
}

type r11jMBRow struct {
	Mode     string
	RefFrame string
	MVRow    int
	MVCol    int
	Skip     bool
	EOB      [25]int
	QCoeff   [25][16]int16
}

func parseR11JMBRows(t *testing.T, trace []byte, frameIdx uint64) map[r11jMBKey]r11jMBRow {
	t.Helper()
	out := make(map[r11jMBKey]r11jMBRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		if typ != "mb" {
			continue
		}
		fi, _ := row["frame_index"].(float64)
		if uint64(fi) != frameIdx {
			continue
		}
		mr, _ := row["mb_row"].(float64)
		mc, _ := row["mb_col"].(float64)
		mode, _ := row["mode"].(string)
		ref, _ := row["ref_frame"].(string)
		mvR, _ := row["mv_row"].(float64)
		mvC, _ := row["mv_col"].(float64)
		skip, _ := row["skip"].(bool)
		var mb r11jMBRow
		mb.Mode = mode
		mb.RefFrame = ref
		mb.MVRow = int(mvR)
		mb.MVCol = int(mvC)
		mb.Skip = skip
		if eobs, ok := row["eob"].([]any); ok {
			for i := 0; i < 25 && i < len(eobs); i++ {
				v, _ := eobs[i].(float64)
				mb.EOB[i] = int(v)
			}
		}
		if qc, ok := row["qcoeff"].([]any); ok {
			for b := 0; b < 25 && b < len(qc); b++ {
				blk, ok := qc[b].([]any)
				if !ok {
					continue
				}
				for p := 0; p < 16 && p < len(blk); p++ {
					v, _ := blk[p].(float64)
					mb.QCoeff[b][p] = int16(v)
				}
			}
		}
		out[r11jMBKey{MBRow: int(mr), MBCol: int(mc)}] = mb
	}
	return out
}

// diffMBDecisionsAllRows is like diffMBDecisions but covers all MB rows
// for a single frame.
func diffMBDecisionsAllRows(t *testing.T, govpxTrace []byte, libvpxTrace []byte, frameIdx uint64) {
	t.Helper()
	gov := parseR11JMBRows(t, govpxTrace, frameIdx)
	lib := parseR11JMBRows(t, libvpxTrace, frameIdx)

	keys := make(map[r11jMBKey]bool)
	for k := range gov {
		keys[k] = true
	}
	for k := range lib {
		keys[k] = true
	}
	keyList := make([]r11jMBKey, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	sort.Slice(keyList, func(i, j int) bool {
		if keyList[i].MBRow != keyList[j].MBRow {
			return keyList[i].MBRow < keyList[j].MBRow
		}
		return keyList[i].MBCol < keyList[j].MBCol
	})

	mismatches := 0
	var sb strings.Builder
	for _, k := range keyList {
		g, gok := gov[k]
		l, lok := lib[k]
		if !gok || !lok {
			continue
		}
		ok := g.Mode == l.Mode && g.RefFrame == l.RefFrame &&
			g.MVRow == l.MVRow && g.MVCol == l.MVCol && g.Skip == l.Skip
		if !ok {
			mismatches++
			fmt.Fprintf(&sb,
				"* mb=(%d,%d) govpx[ref=%s mode=%s mv=(%d,%d) skip=%v] libvpx[ref=%s mode=%s mv=(%d,%d) skip=%v]\n",
				k.MBRow, k.MBCol,
				g.RefFrame, g.Mode, g.MVRow, g.MVCol, g.Skip,
				l.RefFrame, l.Mode, l.MVRow, l.MVCol, l.Skip)
		}
	}
	t.Logf("frame %d MB decision mismatches: %d/%d\n%s", frameIdx, mismatches, len(keyList), sb.String())
}
