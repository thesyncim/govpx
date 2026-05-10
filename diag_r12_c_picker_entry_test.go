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

// TestDiagR12CPickerEntry720pNoise captures libvpx-side per-MB picker_entry
// rows on the 1280x720 rt-cpu8 CBR noise fixture (R11-N/R12-C trigger
// fixture) and prints the picker-entry state at the suspect MBs. The
// libvpx oracle is patched to emit a {"type":"picker_entry",...} row at
// vp8_pick_inter_mode entry, after vp8_find_near_mvs_bias has populated
// mode_mv_sb / mdcounts and before the rd_threshes loop. The govpx side
// already records inter_candidate rows; missing NEARESTMV iterations on
// the libvpx side are the smoking gun for the ZEROMV<->NEARESTMV swap.
//
// Gated behind GOVPX_DEBUG=1.
func TestDiagR12CPickerEntry720pNoise(t *testing.T) {
	if os.Getenv("GOVPX_DEBUG") != "1" {
		t.Skip("set GOVPX_DEBUG=1 to run the R12-C picker_entry diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)
	const (
		width      = 1280
		height     = 720
		fps        = 30
		targetKbps = 1500
		frames     = 4
		cpuUsed    = 8
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
		CpuUsed:             cpuUsed,
		KeyFrameInterval:    999,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = scoreboardBenchNoiseFrame(width, height, i)
	}
	extra := []string{
		"--end-usage=cbr",
		"--buf-sz=600", "--buf-initial-sz=400", "--buf-optimal-sz=500",
		"--undershoot-pct=100", "--overshoot-pct=15",
		"--threads=1", "--noise-sensitivity=0",
	}
	// Encode with govpx (debug picker trace fires inside encode if env is set).
	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "diag-r12-c-picker-entry", opts, targetKbps, sources, extra)

	govRows := parseR12CPickerEntryRows(t, govpxTrace)
	rows := parseR12CPickerEntryRows(t, libvpxTrace)
	t.Logf("govpx picker_entry rows captured: %d", len(govRows))
	t.Logf("libvpx picker_entry rows captured: %d", len(rows))

	// Focus on frame=1, the first inter frame, around the R11-N trigger MB
	// (frame=1 MB(0,3) onward).
	keys := make([]r12cPickerEntryKey, 0, len(rows))
	for k := range rows {
		if k.Frame == 1 && k.MBRow == 0 && k.MBCol < 8 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].MBRow != keys[j].MBRow {
			return keys[i].MBRow < keys[j].MBRow
		}
		return keys[i].MBCol < keys[j].MBCol
	})

	var sb strings.Builder
	for _, k := range keys {
		g := govRows[k]
		l := rows[k]
		fmt.Fprintf(&sb, "frame=%d mb=(%d,%d)\n", k.Frame, k.MBRow, k.MBCol)
		fmt.Fprintf(&sb, "  gov  sign_bias=%d ref_last=%d nearest=(%d,%d) near=(%d,%d) zero=(%d,%d) new=(%d,%d) cnt=[I=%d,Nst=%d,Nr=%d,Spl=%d] rd_thr=%d rd_mult=%d rd_baseline=%d sf_mult=%d RDMULT=%d RDDIV=%d Speed=%d Q=%d y1dc_delta=%d zbin_oq=%d epb=%d freq=%d hits=%d mbs_tested=%d best_rd=%d\n",
			g.SignBias, g.RefFrameLast,
			g.ModeMV.Nearest[0], g.ModeMV.Nearest[1],
			g.ModeMV.Near[0], g.ModeMV.Near[1],
			g.ModeMV.Zero[0], g.ModeMV.Zero[1],
			g.ModeMV.New[0], g.ModeMV.New[1],
			g.Cnt.Intra, g.Cnt.Nearest, g.Cnt.Near, g.Cnt.SplitMV,
			g.RDThreshesNearest, g.RDThreshMultNearest,
			g.RDBaselineThreshNearest, g.SFThreshMultNearest,
			g.RDMult, g.RDDiv, g.Speed, g.BaseQIndex, g.Y1DCDeltaQ,
			g.ZbinOverQuant, g.ErrorPerBit,
			g.ModeCheckFreqNearest, g.ModeTestHitCountsNearest,
			g.MBsTestedSoFar, g.BestRD)
		fmt.Fprintf(&sb, "  lib  sign_bias=%d ref_last=%d nearest=(%d,%d) near=(%d,%d) zero=(%d,%d) new=(%d,%d) cnt=[I=%d,Nst=%d,Nr=%d,Spl=%d] rd_thr=%d rd_mult=%d rd_baseline=%d sf_mult=%d RDMULT=%d RDDIV=%d Speed=%d Q=%d y1dc_delta=%d zbin_oq=%d epb=%d freq=%d hits=%d mbs_tested=%d best_rd=%d\n",
			l.SignBias, l.RefFrameLast,
			l.ModeMV.Nearest[0], l.ModeMV.Nearest[1],
			l.ModeMV.Near[0], l.ModeMV.Near[1],
			l.ModeMV.Zero[0], l.ModeMV.Zero[1],
			l.ModeMV.New[0], l.ModeMV.New[1],
			l.Cnt.Intra, l.Cnt.Nearest, l.Cnt.Near, l.Cnt.SplitMV,
			l.RDThreshesNearest, l.RDThreshMultNearest,
			l.RDBaselineThreshNearest, l.SFThreshMultNearest,
			l.RDMult, l.RDDiv, l.Speed, l.BaseQIndex, l.Y1DCDeltaQ,
			l.ZbinOverQuant, l.ErrorPerBit,
			l.ModeCheckFreqNearest, l.ModeTestHitCountsNearest,
			l.MBsTestedSoFar, l.BestRD)
	}
	t.Logf("\n=== R12-C picker_entry frame=1 MB row 0 (govpx vs libvpx) ===\n%s", sb.String())

	// Also dump frame=1 row 1 cols 0..3 to capture the cascade.
	keys2 := make([]r12cPickerEntryKey, 0)
	for k := range rows {
		if k.Frame == 1 && k.MBRow == 1 && k.MBCol < 4 {
			keys2 = append(keys2, k)
		}
	}
	sort.Slice(keys2, func(i, j int) bool { return keys2[i].MBCol < keys2[j].MBCol })
	var sb2 strings.Builder
	for _, k := range keys2 {
		g := govRows[k]
		l := rows[k]
		fmt.Fprintf(&sb2, "frame=%d mb=(%d,%d)\n", k.Frame, k.MBRow, k.MBCol)
		fmt.Fprintf(&sb2, "  gov nearest=(%d,%d) cnt=[I=%d,Nst=%d,Nr=%d,Spl=%d] rd_thr=%d rd_mult=%d hits=%d best_rd=%d\n",
			g.ModeMV.Nearest[0], g.ModeMV.Nearest[1],
			g.Cnt.Intra, g.Cnt.Nearest, g.Cnt.Near, g.Cnt.SplitMV,
			g.RDThreshesNearest, g.RDThreshMultNearest,
			g.ModeTestHitCountsNearest, g.BestRD)
		fmt.Fprintf(&sb2, "  lib nearest=(%d,%d) cnt=[I=%d,Nst=%d,Nr=%d,Spl=%d] rd_thr=%d rd_mult=%d hits=%d best_rd=%d\n",
			l.ModeMV.Nearest[0], l.ModeMV.Nearest[1],
			l.Cnt.Intra, l.Cnt.Nearest, l.Cnt.Near, l.Cnt.SplitMV,
			l.RDThreshesNearest, l.RDThreshMultNearest,
			l.ModeTestHitCountsNearest, l.BestRD)
	}
	t.Logf("\n=== R12-C picker_entry frame=1 MB row 1 (cascade, govpx vs libvpx) ===\n%s", sb2.String())

	// Dump per-MB chosen mode/ref/MV for both encoders at the cascade
	// trigger MBs so we can correlate which one actually picks NEARESTMV
	// vs ZEROMV.
	govMBs := parseR11JMBRows(t, govpxTrace, 1)
	libMBs := parseR11JMBRows(t, libvpxTrace, 1)
	var sbDec strings.Builder
	for _, k := range []r11jMBKey{
		{0, 3}, {0, 7}, {1, 2}, {1, 3}, {1, 4}, {2, 0}, {2, 3},
	} {
		g := govMBs[k]
		l := libMBs[k]
		fmt.Fprintf(&sbDec,
			"  mb=(%d,%d) gov[mode=%-9s ref=%-12s mv=(%d,%d) skip=%v] lib[mode=%-9s ref=%-12s mv=(%d,%d) skip=%v]\n",
			k.MBRow, k.MBCol,
			g.Mode, g.RefFrame, g.MVRow, g.MVCol, g.Skip,
			l.Mode, l.RefFrame, l.MVRow, l.MVCol, l.Skip)
	}
	t.Logf("\n=== R12-C MB decision diff frame=1 ===\n%s", sbDec.String())

	// Dump frame qIndex from libvpx and govpx traces.
	govQ := parseFrameQIndex(t, "govpx", govpxTrace)
	libQ := parseFrameQIndex(t, "libvpx", libvpxTrace)
	t.Logf("frame qIndex govpx=%v libvpx=%v", govQ, libQ)

	// Dump iteration_outcome rows for frame=1 MB(0,3) and the cascade row 1
	// MBs to see exactly which gate libvpx uses to skip NEARESTMV.
	iters := parseR12CIterationOutcomes(t, libvpxTrace)
	for _, target := range []r12cPickerEntryKey{
		{1, 0, 3}, {1, 0, 7}, {1, 1, 2}, {1, 1, 3},
	} {
		var sb3 strings.Builder
		for _, it := range iters {
			if it.Frame == target.Frame && it.MBRow == target.MBRow && it.MBCol == target.MBCol {
				fmt.Fprintf(&sb3, "  mode_index=%2d mode=%-9s ref=%d mv=(%d,%d) gate=%-18s best_rd=%d rd_thr=%d\n",
					it.ModeIndex, it.Mode, it.RefFrame, it.MVRow, it.MVCol, it.Gate, it.BestRDAtGate, it.RDThreshesAtGate)
			}
		}
		t.Logf("\n=== R12-C libvpx iteration_outcome frame=%d mb=(%d,%d) ===\n%s",
			target.Frame, target.MBRow, target.MBCol, sb3.String())
	}
}

type r12cIterationOutcome struct {
	Frame            int
	MBRow            int
	MBCol            int
	ModeIndex        int
	Mode             string
	RefFrame         int
	MVRow            int
	MVCol            int
	Gate             string
	ThisRD           int
	BestRDAtGate     int
	RDThreshesAtGate int
}

func parseR12CIterationOutcomes(t *testing.T, trace []byte) []r12cIterationOutcome {
	t.Helper()
	out := make([]r12cIterationOutcome, 0)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			continue
		}
		typ, _ := raw["type"].(string)
		if typ != "iteration_outcome" {
			continue
		}
		rec := r12cIterationOutcome{}
		if v, ok := raw["frame_index"].(float64); ok {
			rec.Frame = int(v)
		}
		if v, ok := raw["mb_row"].(float64); ok {
			rec.MBRow = int(v)
		}
		if v, ok := raw["mb_col"].(float64); ok {
			rec.MBCol = int(v)
		}
		if v, ok := raw["mode_index"].(float64); ok {
			rec.ModeIndex = int(v)
		}
		if v, ok := raw["mode"].(string); ok {
			rec.Mode = v
		}
		if v, ok := raw["ref_frame"].(float64); ok {
			rec.RefFrame = int(v)
		}
		if v, ok := raw["mv"].([]any); ok && len(v) == 2 {
			r, _ := v[0].(float64)
			c, _ := v[1].(float64)
			rec.MVRow = int(r)
			rec.MVCol = int(c)
		}
		if v, ok := raw["gate"].(string); ok {
			rec.Gate = v
		}
		if v, ok := raw["this_rd"].(float64); ok {
			rec.ThisRD = int(v)
		}
		if v, ok := raw["best_rd_at_gate"].(float64); ok {
			rec.BestRDAtGate = int(v)
		}
		if v, ok := raw["rd_threshes_at_gate"].(float64); ok {
			rec.RDThreshesAtGate = int(v)
		}
		out = append(out, rec)
	}
	return out
}

type r12cPickerEntryKey struct {
	Frame int
	MBRow int
	MBCol int
}

type r12cModeMV struct {
	Nearest [2]int
	Near    [2]int
	Zero    [2]int
	New     [2]int
}

type r12cCnt struct {
	Intra   int
	Nearest int
	Near    int
	SplitMV int
}

type r12cPickerEntryRow struct {
	SignBias                 int
	RefFrameLast             int
	ModeMV                   r12cModeMV
	Cnt                      r12cCnt
	RDThreshesNearest        int
	RDThreshMultNearest      int
	RDBaselineThreshNearest  int
	SFThreshMultNearest      int
	RDMult                   int
	RDDiv                    int
	Speed                    int
	BaseQIndex               int
	Y1DCDeltaQ               int
	ZbinOverQuant            int
	ErrorPerBit              int
	ModeCheckFreqNearest     int
	ModeTestHitCountsNearest int
	MBsTestedSoFar           int
	BestRD                   int
}

func parseR12CPickerEntryRows(t *testing.T, trace []byte) map[r12cPickerEntryKey]r12cPickerEntryRow {
	t.Helper()
	out := make(map[r12cPickerEntryKey]r12cPickerEntryRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 4*1024*1024), 64*1024*1024)
	for scan.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(scan.Bytes(), &raw); err != nil {
			continue
		}
		typ, _ := raw["type"].(string)
		if typ != "picker_entry" {
			continue
		}
		fi, _ := raw["frame_index"].(float64)
		mr, _ := raw["mb_row"].(float64)
		mc, _ := raw["mb_col"].(float64)
		k := r12cPickerEntryKey{Frame: int(fi), MBRow: int(mr), MBCol: int(mc)}
		row := r12cPickerEntryRow{}
		if v, ok := raw["sign_bias"].(float64); ok {
			row.SignBias = int(v)
		}
		if v, ok := raw["ref_frame_last"].(float64); ok {
			row.RefFrameLast = int(v)
		}
		if mm, ok := raw["mode_mv"].(map[string]any); ok {
			row.ModeMV.Nearest = parseR12CMVPair(mm["nearest"])
			row.ModeMV.Near = parseR12CMVPair(mm["near"])
			row.ModeMV.Zero = parseR12CMVPair(mm["zero"])
			row.ModeMV.New = parseR12CMVPair(mm["new"])
		}
		if cm, ok := raw["cnt"].(map[string]any); ok {
			if v, ok := cm["intra"].(float64); ok {
				row.Cnt.Intra = int(v)
			}
			if v, ok := cm["nearest"].(float64); ok {
				row.Cnt.Nearest = int(v)
			}
			if v, ok := cm["near"].(float64); ok {
				row.Cnt.Near = int(v)
			}
			if v, ok := cm["splitmv"].(float64); ok {
				row.Cnt.SplitMV = int(v)
			}
		}
		if v, ok := raw["rd_threshes_nearest"].(float64); ok {
			row.RDThreshesNearest = int(v)
		}
		if v, ok := raw["rd_thresh_mult_nearest"].(float64); ok {
			row.RDThreshMultNearest = int(v)
		}
		if v, ok := raw["rd_baseline_thresh_nearest"].(float64); ok {
			row.RDBaselineThreshNearest = int(v)
		}
		if v, ok := raw["sf_thresh_mult_nearest"].(float64); ok {
			row.SFThreshMultNearest = int(v)
		}
		if v, ok := raw["rdmult"].(float64); ok {
			row.RDMult = int(v)
		}
		if v, ok := raw["rddiv"].(float64); ok {
			row.RDDiv = int(v)
		}
		if v, ok := raw["speed"].(float64); ok {
			row.Speed = int(v)
		}
		if v, ok := raw["base_qindex"].(float64); ok {
			row.BaseQIndex = int(v)
		}
		if v, ok := raw["y1dc_delta_q"].(float64); ok {
			row.Y1DCDeltaQ = int(v)
		}
		if v, ok := raw["zbin_over_quant"].(float64); ok {
			row.ZbinOverQuant = int(v)
		}
		if v, ok := raw["errorperbit"].(float64); ok {
			row.ErrorPerBit = int(v)
		}
		if v, ok := raw["mode_check_freq_nearest"].(float64); ok {
			row.ModeCheckFreqNearest = int(v)
		}
		if v, ok := raw["mode_test_hit_counts_nearest"].(float64); ok {
			row.ModeTestHitCountsNearest = int(v)
		}
		if v, ok := raw["mbs_tested_so_far"].(float64); ok {
			row.MBsTestedSoFar = int(v)
		}
		if v, ok := raw["best_rd"].(float64); ok {
			row.BestRD = int(v)
		}
		out[k] = row
	}
	return out
}

func parseR12CMVPair(v any) [2]int {
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		return [2]int{0, 0}
	}
	r, _ := arr[0].(float64)
	c, _ := arr[1].(float64)
	return [2]int{int(r), int(c)}
}
