//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// TestVP8Task352ScreenContentResidualBisect extends the task #341 per-MB
// bisect across the full 12-frame BD-rate fixture window. Task #341
// reduced TestVP8FeatureBDRate720pScreenContentCBR's gap from +36% to
// +9.7% via the calculate_final_rd_costs tteob==0 rate2 backout port,
// and confirmed frames 0-1 are byte-exact. This probe walks frames 2-11
// to identify the first frame where per-MB picks diverge post-#341.
//
// Findings (recorded 2026-05-19, govpx@5f9805a3):
//
//   - Frames 0 and 1: ZERO mode/ref/mv mismatch (confirmed task #341).
//   - Frame 2: 1326/3600 MBs diverge (37%). Dominant pattern:
//     govpx ZEROMV+GOLDEN_FRAME (1220 MBs) where libvpx picks
//     DC_PRED+INTRA_FRAME (1174 MBs) or V_PRED/NEWMV/NEARESTMV/H_PRED.
//   - First divergent MB: (0,1). govpx ZEROMV+GOLDEN skip=true rate=245
//     vs libvpx DC_PRED+INTRA skip=true rate=1349. Both encoders agree
//     on rate_y=18819, distortion_uv=309, but the per-candidate score
//     diverges: DC_PRED govpx score=2925 vs libvpx 1638 (78% higher in
//     govpx); ZEROMV-GOLDEN govpx 2417 vs libvpx 2350. Govpx picks the
//     wrong winner because DC_PRED's rate is inflated by ~1177 bits.
//
// Root cause: prob_intra_coded self-reinforcing equilibrium. The
// frame-trace prob_intra_coded values capture the post-recode-loop
// final values:
//
//   - govpx frame 2: prob_intra_coded=1, prob_last_coded=1.
//   - libvpx frame 2: prob_intra_coded=87, prob_last_coded=11.
//
// Both encoders enter frame 2 inheriting frame 1's prob_intra_coded=1
// (a degenerate value from frame 1's all-inter picks). With prob_intra=1
// the INTRA ref_frame_cost via vp8_calc_ref_frame_costs (bitstream.c:786)
// is `vp8_cost_zero(1)` ~8 bits/MB — every intra candidate sees a +8
// bit per-MB penalty in rate2. On a screen-content MB where DC_PRED's
// distortion (1314) beats ZEROMV-GOLDEN (2324), the rate gap normally
// wouldn't overcome it; with prob_intra=1 it does, so all 3600 MBs
// pick inter, intra count stays 0, vp8_convert_rfct_to_prob
// (bitstream.c:394) clamps prob_intra back to 1 → recode-loop
// equilibrium.
//
// libvpx breaks this equilibrium somehow (final prob_intra=87, with
// ~1326 intra MBs in the last iteration). The mechanism is not yet
// localized — candidates include:
//   - vp8_drop_encodedframe_overshoot interaction on screen content
//   - encode_mb_row's rfct accumulation differing per recode iter
//   - cyclic-refresh / segmentation Q delta that's not exercised in
//     govpx's frame-2 picker context (seg_id=0 for all 3600 MBs in
//     both encoders, so cyclic refresh isn't the driver)
//   - the active_map / x->active_ptr path
//
// Govpx's recode loop terminates at iter=53 q=64, projected_frame_size
// stuck at 693 bytes (target ~20772). The rate_correction_factor hits
// the 0.01 floor at iter 4 and stays clamped — the picker output is
// identical across iters 4-53 because every Q produces the same
// all-skip ZEROMV-GOLDEN pattern. libvpx runs 91 iters to q=26 with
// projected_frame_size=14186 (close to target).
//
// Next-step plan: instrument govpx to capture per-recode-iter
// rfct counts (intra/last/golden/alt) and prob_intra evolution
// across all 53 iterations, then run the same probe under libvpx
// (extending the oracle trace's recode_iter row with rfct + computed
// prob_intra). The first iter where libvpx's rfct[INTRA] > 0 while
// govpx's stays 0 isolates the picker site where libvpx admits an
// intra candidate against prob_intra=1. Candidates to audit:
//   - libvpx active_map / x->active_ptr semantics (govpx
//     interMacroblockInactive only fires when an active-map is set)
//   - rdmult / rddiv per-MB activity-masking interaction with
//     ScreenContentMode=1 (libvpx Tune=SSIM-only, but our encoder
//     enables activity-masking unconditionally for screen content
//     via the cyclic-refresh segmentation map)
//   - encode_mb_row's first-row vs first-col speed-feature gates
//     that suppress some inter candidates on the top frame edge.
//
// This test is logging-only (always passes); it pins the localization
// state on stdout and to /tmp/govpx_task352_summary.log.
//
// To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  go test -tags govpx_oracle_trace -run TestVP8Task352ScreenContentResidualBisect -v
func TestVP8Task352ScreenContentResidualBisect(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #352 screen-content residual bisect")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		frameCount = 12
		targetKbps = 2000
	)

	ycbcrSources := make([]*image.YCbCr, frameCount)
	govpxSources := make([]Image, frameCount)
	for i := range ycbcrSources {
		yc := task341MakeScreenTextWindowFrame(width, height, i)
		ycbcrSources[i] = yc
		govpxSources[i] = Image{
			Width:   width,
			Height:  height,
			Y:       yc.Y,
			U:       yc.Cb,
			V:       yc.Cr,
			YStride: yc.YStride,
			UStride: yc.CStride,
			VStride: yc.CStride,
		}
	}

	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      63,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           0,
		ScreenContentMode: 1,
		Threads:           1,
	}

	// govpx side.
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range govpxSources {
		if _, err := enc.EncodeInto(packet, src, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	// libvpx side via the patched vpxenc-oracle.
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "task352.yuv")
	ivfPath := filepath.Join(dir, "task352.ivf")
	libvpxTracePath := filepath.Join(dir, "task352.jsonl")
	task341WriteI420(t, yuvPath, govpxSources)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--best",
		"--cpu-used=0",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--screen-content-mode=1",
		"--threads=1",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=" + libvpxOracleTimebaseArg(opts),
		"--fps=" + libvpxOracleFPSArg(opts),
		"--limit=" + strconv.Itoa(len(govpxSources)),
		"--output=" + ivfPath,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		yuvPath,
	}
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(), "GOVPX_ORACLE_TRACE_OUT="+libvpxTracePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("vpxenc-oracle args: %v", args)
		t.Logf("vpxenc-oracle output:\n%s", out)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}
	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	govpxOut := "/tmp/govpx_task352_screen_content.jsonl"
	libvpxOut := "/tmp/libvpx_task352_screen_content.jsonl"
	_ = os.WriteFile(govpxOut, govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile(libvpxOut, libvpxTrace, 0o644)
	t.Logf("task352 govpx_trace=%s libvpx_trace=%s govpx_bytes=%d libvpx_bytes=%d",
		govpxOut, libvpxOut, govpxTraceBuf.Len(), len(libvpxTrace))

	// Open a summary log we can read after the test.
	summaryPath := "/tmp/govpx_task352_summary.log"
	summary, err := os.Create(summaryPath)
	if err != nil {
		t.Fatalf("create summary: %v", err)
	}
	defer summary.Close()
	logf := func(format string, args ...any) {
		t.Logf(format, args...)
		fmt.Fprintf(summary, format+"\n", args...)
	}
	_ = logf // appears below

	// Walk all 12 frames; emit per-frame divergence summary.
	for frameIdx := uint64(0); frameIdx < frameCount; frameIdx++ {
		gRows := task210ParseMBRowsForFrame(govpxTraceBuf.Bytes(), frameIdx)
		lRows := task210ParseMBRowsForFrame(libvpxTrace, frameIdx)

		gByKey := map[[2]int]map[string]any{}
		lByKey := map[[2]int]map[string]any{}
		keys := [][2]int{}
		for _, r := range gRows {
			row, _ := r["mb_row"].(float64)
			col, _ := r["mb_col"].(float64)
			k := [2]int{int(row), int(col)}
			gByKey[k] = r
			keys = append(keys, k)
		}
		for _, r := range lRows {
			row, _ := r["mb_row"].(float64)
			col, _ := r["mb_col"].(float64)
			k := [2]int{int(row), int(col)}
			lByKey[k] = r
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})

		modePairs := map[string]int{}
		refPairs := map[string]int{}
		modeMismatches := 0
		refMismatches := 0
		mvMismatches := 0
		firstDiv := [2]int{-1, -1}
		var firstGov, firstLib map[string]any
		for _, k := range keys {
			g, gok := gByKey[k]
			l, lok := lByKey[k]
			if !gok || !lok {
				continue
			}
			gm, _ := g["mode"].(string)
			lm, _ := l["mode"].(string)
			gref, _ := g["ref_frame"].(string)
			lref, _ := l["ref_frame"].(string)
			if gm != lm {
				modeMismatches++
				modePairs[gm+"|"+lm]++
			}
			if gref != lref {
				refMismatches++
				refPairs[gref+"|"+lref]++
			}
			grow, _ := g["mv_row"].(float64)
			gcol, _ := g["mv_col"].(float64)
			lrow, _ := l["mv_row"].(float64)
			lcol, _ := l["mv_col"].(float64)
			if grow != lrow || gcol != lcol {
				mvMismatches++
			}
			if firstDiv[0] < 0 && (gm != lm || gref != lref || grow != lrow || gcol != lcol) {
				firstDiv = k
				firstGov = g
				firstLib = l
			}
		}
		logf("task352 frame%d mbs=%d mode_mm=%d ref_mm=%d mv_mm=%d",
			frameIdx, len(keys), modeMismatches, refMismatches, mvMismatches)

		if modeMismatches == 0 && refMismatches == 0 && mvMismatches == 0 {
			continue
		}

		// Sort histograms for stability.
		type histEntry struct {
			pair  string
			count int
		}
		var modeHist []histEntry
		for p, c := range modePairs {
			modeHist = append(modeHist, histEntry{p, c})
		}
		sort.Slice(modeHist, func(i, j int) bool { return modeHist[i].count > modeHist[j].count })
		var refHist []histEntry
		for p, c := range refPairs {
			refHist = append(refHist, histEntry{p, c})
		}
		sort.Slice(refHist, func(i, j int) bool { return refHist[i].count > refHist[j].count })
		for _, e := range modeHist {
			logf("task352 frame%d MODE_HIST govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		for _, e := range refHist {
			logf("task352 frame%d REF_HIST  govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		if firstDiv[0] >= 0 {
			logf("task352 frame%d FIRST_DIV mb=(%d,%d):", frameIdx, firstDiv[0], firstDiv[1])
			for _, f := range []string{"mode", "ref_frame", "mv_row", "mv_col", "uv_mode", "skip", "eob_sum", "mb_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
				gv := firstGov[f]
				lv := firstLib[f]
				marker := ""
				if !task210FieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				logf("  %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
			// Dump candidate scoreboard to summary file too.
			task352DumpInterCandidateScoreboardToFile(summary, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
			task341LogInterCandidateScoreboardAt(t, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
		}

		// Once we've found the first divergent frame, stop dumping
		// scoreboards (the downstream frames inherit the divergence).
		logf("task352 FIRST_DIVERGENT_FRAME=%d", frameIdx)
		break
	}
}

func task352DumpInterCandidateScoreboardToFile(w *os.File, gov, lib []byte, frameIdx uint64, mb [2]int) {
	gCands := task341ParseInterCandidatesForMB(gov, frameIdx, mb)
	lCands := task341ParseInterCandidatesForMB(lib, frameIdx, mb)
	fmt.Fprintf(w, "task352 frame%d MB(%d,%d) inter_candidate scoreboard: govpx=%d libvpx=%d\n",
		frameIdx, mb[0], mb[1], len(gCands), len(lCands))
	gByIdx := map[int]map[string]any{}
	lByIdx := map[int]map[string]any{}
	idxs := map[int]struct{}{}
	for _, c := range gCands {
		mi, _ := c["mode_index"].(float64)
		gByIdx[int(mi)] = c
		idxs[int(mi)] = struct{}{}
	}
	for _, c := range lCands {
		mi, _ := c["mode_index"].(float64)
		lByIdx[int(mi)] = c
		idxs[int(mi)] = struct{}{}
	}
	orderedIdx := make([]int, 0, len(idxs))
	for i := range idxs {
		orderedIdx = append(orderedIdx, i)
	}
	sort.Ints(orderedIdx)
	for _, mi := range orderedIdx {
		g, gok := gByIdx[mi]
		l, lok := lByIdx[mi]
		if !gok && !lok {
			continue
		}
		fmt.Fprintf(w, "  mode_index=%d:\n", mi)
		fields := []string{"mode", "ref_frame", "ref_slot", "threshold", "outcome", "became_best", "rate", "rate_y", "rate_uv", "distortion", "distortion_uv", "score", "yrd", "sse"}
		for _, f := range fields {
			var gv, lv any
			if gok {
				gv = g[f]
			}
			if lok {
				lv = l[f]
			}
			marker := ""
			if gok && lok && !task210FieldsEqual(gv, lv) {
				marker = " <DIFF>"
			}
			fmt.Fprintf(w, "    %-15s govpx=%v libvpx=%v%s\n", f, gv, lv, marker)
		}
	}
}
