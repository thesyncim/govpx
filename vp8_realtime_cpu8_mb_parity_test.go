//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8RealtimeCPU8MBParity performs the per-MB localization of any
// inter-mode picker divergence on the 720p panning realtime cpu_used=8
// CBR fixture (TestVP8FeatureBDRate720pRealtimeCpu8CBR pinned at
// +6.94% BD-rate / -0.95 dB BD-PSNR; gate is +10% / -1.0 dB so the
// fixture passes its quality-gate by design). Task #341 closed an
// analogous +36% gap on the screen-content BestQuality fixture by
// porting the tteob==0 rate2 backout into the intra-in-inter-loop RD
// picker. The RT cpu=8 path uses the FAST picker (Speed>=8 disables the
// RD path entirely via vp8_set_speed_features), so the #341 fix
// (estimateInterIntraModeRDScore in vp8_encoder_inter_modes_rd_intra.go)
// is not exercised on this fixture.
//
// TASK #348 AUDIT (not a port; insight only):
//
// Per-MB tracing pinpointed the root cause at frame 2 MB(0,0), NOT at
// frame 3 MB(0,1) as initially hypothesised. The picker enters MB(0,0)
// with `mbs_tested_so_far=0` and `mode_test_hit_counts[]=0`, so the
// rd_thresh state evolution is moot. Modes 0/1/4 (ZEROMV-LAST / DC_PRED
// / ZEROMV-GOLDEN) produce identical sse/distortion on both encoders:
// the LAST_FRAME reference buffer is byte-exact through frame 1 and
// the ZEROMV predictions match exactly. The divergence is isolated to
// mode 13 NEWMV-LAST, where govpx returns mv=(24,8) sse=3565 while
// libvpx returns mv=(8,16) sse=2305 (per the oracle inter_candidate
// trace). The motion search itself is producing different MVs.
//
// The inter_candidate trace fields `improved_mv_start` /
// `improved_mv_row` / `improved_mv_col` / `improved_mv_sr` /
// `improved_mv_near_sadidx` expose the divergent input: govpx fed
// improvedMVStart=(6,16) sr=3 sadidx=3 into the HEX search, while
// libvpx fed improvedMVStart=False (search begins from bestRefMV=(0,0)
// with default sr). Different search-start → different MV converged.
//
// The `improvedMVPrediction` config field is set by
// libvpxInterFrameImprovedMVPredictionForFeatureSpeed (encoder_inter_
// speed.go) mirroring libvpx's `sf->improved_mv_pred` gate. libvpx's
// gate fires off when `cpi->Speed > 6` (vp8/encoder/onyx_if.c:957/1009
// inside `case 2`, with `Speed = cpi->Speed` re-aliased at line 888
// before the switch). govpx's gate fires off when the autoSpeed it
// reads via libvpxCPUUsed > 6. At cpu_used=8 RT frame 2 the live
// libvpx cpi->Speed has auto-evolved to 9 (per the picker_entry trace's
// `speed` field), but govpx's autoSpeed evolution lands at a value <= 6
// (the task #278 inter-frame budget/3 wall-clock pin keeps duration in
// the libvpx Speed=0 stable region, which inhibits the Speed+=2 branch
// of vp8_auto_select_speed). The autoSpeed divergence drives the
// improved_mv_pred gate divergence which drives the NEWMV MV
// divergence which drives the bit-budget divergence which drives the
// downstream q_index / rd_thresh cascades observed at frame 3+.
//
// A unconditional "disable improved_mv_pred for all realtime cpu_used"
// rule (the naive port of `RT(cpi->Speed) > 6` always being true)
// closes ~1.06% of the +6.94% BD-rate gap (measured: +6.94% → +5.88%),
// but breaks byte-parity on the threads=4 cpu_used=0 RT VBR regression
// in TestVP8RealtimeCorpusMatchesLibvpxBytes.corpus.regression_w854h480_threads
// 4_vbr_inter_diverge -- where libvpx legitimately keeps improved_mv_
// pred ENABLED because its cpi->Speed stayed at the cold-start 4 (the
// raw cpi->Speed gate at onyx_if.c:957 fires only at cpi->Speed > 6,
// not on the line-817-scoped `RT(cpi->Speed)`).
//
// A correct port requires aligning govpx's autoSpeed evolution to
// libvpx's per-frame cpi->Speed evolution at large resolutions under
// the task #278 inter-frame timing pin. That is a separate
// vp8_auto_select_speed audit, not a single-line gate fix. Task #348
// closes with the bisect insight + the structured next-step plan
// recorded here; no port lands.
//
// libvpx fast-picker rate model (vp8/encoder/pickinter.c
// vp8_pick_inter_mode + evaluate_inter_mode, lines 471-514, 843-1102):
//
//	rate2 = frame_cost + mode_cost + mv_cost                (lines 853/874/901/1076/1100)
//	distortion2 = variance16x16(src, predictor)              (lines 875/899/995/1066)
//	this_rd = RDCOST(rdmult, rddiv, rate2, distortion2)      (lines 877/903/1102)
//
// No coefficient-level rate, no tteob==0 backout: the fast picker
// never quantizes the residual before scoring. Skip is applied later
// via check_for_encode_breakout (line 449) based on SSE thresholds.
// Govpx already mirrors this exactly in
// estimateFastInterModeScoreHot / estimateFastIntraModeScore
// (vp8_encoder_inter_rd.go:264, vp8_encoder_inter_modes_fast_helpers.go:38).
//
// AUDIT FINDING (this task):
//
// At the 2000 kbps middle rung, the picker matches BYTE-EXACT for the
// first 2 frames across all 3600 MBs (0 mode / 0 ref / 0 mv mismatches,
// q_index identical 8/106). Across all four rungs (1000/2000/4000/8000)
// frame 0 (KF) and frame 1 (first inter) are 0/0/0. Divergence begins at
// frame 2+ and cascades:
//
//  1. govpx evaluates more inter-candidate mode_indexes than libvpx (e.g.
//     frame3 MB(0,1) @ 1000 kbps: govpx tests modes 0/1/4/10/11/12/13/14,
//     libvpx tests only modes 0/1). libvpx's rd_threshes early-skip gate
//     at pickinter.c:780 (`if (best_rd <= x->rd_threshes[mode_index])
//     continue`) cuts the loop short faster than govpx's
//     `bestScore <= threshold` gate at vp8_encoder_inter_modes_fast.go:688.
//     The state difference is in the dynamic rd_threshes[] evolution,
//     not in the per-mode scoring math.
//  2. The extra candidates govpx tests (mode 13 NEWMV-LAST, mode 4/14
//     GOLDEN candidates) sometimes beat the libvpx-chosen ZEROMV-LAST,
//     flipping the picked mode (NEWMV-LAST vs ZEROMV-LAST → different
//     mb_rate 3723 vs 237) which biases the per-frame bit budget and
//     shifts q_index in subsequent frames (frame15 govpx q=74 vs libvpx
//     q=106).
//  3. The +6.935% BD-rate gap is the integral of these state-drift
//     cascades over 16 frames at 1000/4000/8000 kbps rungs. It is NOT
//     a single fixable rate-accounting bug analogous to the #341
//     tteob==0 backout: the fast picker has no coefficient-rate stage
//     where such a backout would apply.
//  4. The cpu_used=8 path runs vp8_auto_select_speed (encodeframe.c:689),
//     whose Speed evolution is wall-clock-dependent in libvpx. Govpx's
//     interFrameAutoSpeedTimingCompensation pins inter-frame duration to
//     budget/3 at ≥1500 MB resolutions (3600 MBs here) to keep
//     bytestream parity across host loads, but the pinned wall-clock
//     differs from libvpx's actual wall-clock by construction. The
//     residual rd_threshes[] state divergence is a downstream consequence
//     of this designed autoSpeed-pin trade-off.
//
// The fixture's +10% BD-rate / -1.0 dB BD-PSNR gate was explicitly sized
// to absorb this expected ~7%/-1.0 dB spread (see
// feature_quality_gates_vp8_test.go:829-838). No port from libvpx exists
// to close the gap further without unpinning the autoSpeed compensation
// (which would re-introduce the task #278 byte-parity flake) or porting
// libvpx's full per-mode rd_thresh_mult evolution path (which would
// require a separate audit of the cyclic-refresh / segment-quantizer
// interactions that govpx layers on top of libvpx's table).
//
// Logging-only — always passes. To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  GOVPX_TASK343_TARGET_KBPS=1000 \
//	  go test -tags govpx_oracle_trace -run TestVP8RealtimeCPU8MBParity -v
func TestVP8RealtimeCPU8MBParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run RT cpu=8 MB parity")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := coracletest.VpxencOracle(t)

	const (
		width      = 1280
		height     = 720
		frameCount = 16
	)
	targetKbps := 2000
	if v := os.Getenv("GOVPX_TASK343_TARGET_KBPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			targetKbps = n
		}
	}

	// Same source the BD-rate fixture uses.
	ycbcrSources := make([]*image.YCbCr, frameCount)
	govpxSources := make([]Image, frameCount)
	for i := range ycbcrSources {
		yc := makeRealtimeCPU8PanningFrame(width, height, i)
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
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
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
	yuvPath := filepath.Join(dir, "realtime_cpu8.yuv")
	ivfPath := filepath.Join(dir, "realtime_cpu8.ivf")
	libvpxTracePath := filepath.Join(dir, "realtime_cpu8.jsonl")
	writeScreenContentI420(t, yuvPath, govpxSources)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		"--rt",
		"--cpu-used=8",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--end-usage=cbr",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--threads=1",
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
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

	govpxOut := "/tmp/govpx_realtime_cpu8_rt_cpu8.jsonl"
	libvpxOut := "/tmp/libvpx_realtime_cpu8_rt_cpu8.jsonl"
	_ = os.WriteFile(govpxOut, govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile(libvpxOut, libvpxTrace, 0o644)
	t.Logf("realtime_cpu8 govpx_trace=%s libvpx_trace=%s govpx_bytes=%d libvpx_bytes=%d",
		govpxOut, libvpxOut, govpxTraceBuf.Len(), len(libvpxTrace))

	// Probe across all 16 frames so each rung's worst-case rate divergence
	// surfaces; cpu_used=8 auto_select_speed can flip after the 1st-2nd
	// frame's wall-clock measurements feed back into autoSpeed.
	frameProbeList := []uint64{0, 1, 2, 3, 7, 15}
	for _, frameIdx := range frameProbeList {
		gRows := parseMBActivityRowsForFrame(govpxTraceBuf.Bytes(), frameIdx)
		lRows := parseMBActivityRowsForFrame(libvpxTrace, frameIdx)
		t.Logf("realtime_cpu8 frame%d govpx_mb_rows=%d libvpx_mb_rows=%d", frameIdx, len(gRows), len(lRows))

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
			modePair := gm + "|" + lm
			refPair := gref + "|" + lref
			if gm != lm {
				modeMismatches++
				modePairs[modePair]++
			}
			if gref != lref {
				refMismatches++
				refPairs[refPair]++
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
		t.Logf("realtime_cpu8 frame%d mode_mismatches=%d ref_mismatches=%d mv_mismatches=%d total_mbs=%d",
			frameIdx, modeMismatches, refMismatches, mvMismatches, len(keys))

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
			t.Logf("realtime_cpu8 frame%d MODE_HIST govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		for _, e := range refHist {
			t.Logf("realtime_cpu8 frame%d REF_HIST  govpx|libvpx=%s count=%d", frameIdx, e.pair, e.count)
		}
		if firstDiv[0] >= 0 {
			t.Logf("realtime_cpu8 frame%d FIRST_DIV mb=(%d,%d):", frameIdx, firstDiv[0], firstDiv[1])
			for _, f := range []string{"mode", "ref_frame", "mv_row", "mv_col", "uv_mode", "skip", "eob_sum", "mb_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
				gv := firstGov[f]
				lv := firstLib[f]
				marker := ""
				if !mbTraceFieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("  %-15s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
			// Inter-candidate scoreboard for FIRST_DIV.
			logScreenContentInterCandidateScoreboardAt(t, govpxTraceBuf.Bytes(), libvpxTrace, frameIdx, firstDiv)
		} else {
			t.Logf("realtime_cpu8 frame%d NO_DIV — all MBs match (mode, ref, mv)", frameIdx)
		}

		// Frame-level rate/Q probe to surface autoSpeed/rate-control drift.
		gFrame := parseRealtimeCPU8FrameRow(govpxTraceBuf.Bytes(), frameIdx)
		lFrame := parseRealtimeCPU8FrameRow(libvpxTrace, frameIdx)
		if gFrame != nil && lFrame != nil {
			for _, f := range []string{"q_index", "base_q_index", "loop_filter_level", "auto_speed", "projected_frame_size"} {
				gv := gFrame[f]
				lv := lFrame[f]
				marker := ""
				if !mbTraceFieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("realtime_cpu8 frame%d FRAME %-22s govpx=%v libvpx=%v%s", frameIdx, f, gv, lv, marker)
			}
		}
	}
}

// makeRealtimeCPU8PanningFrame is a verbatim copy of makeVP8PanningFrame from
// feature_quality_gates_vp8_test.go (which lives in package govpx_test
// and is not accessible from this package-internal probe). Sync any
// updates with feature_quality_gates_vp8_test.go:162.
func makeRealtimeCPU8PanningFrame(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	xoff := idx * 2
	yoff := idx
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			sx := x + xoff
			sy := y + yoff
			gradient := 64 + realtimeCPU8Triangle(sx+sy, 256)/4
			triX := realtimeCPU8Triangle(sx, 64) / 4
			triY := realtimeCPU8Triangle(sy, 64) / 4
			texture := ((sx*1103515245+sy*12345)>>4)&0x0F - 8
			row[x] = realtimeCPU8Clamp(gradient + triX + triY + texture)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			sx := 2*x + xoff
			sy := 2*y + yoff
			cb[x] = realtimeCPU8Clamp(128 + (realtimeCPU8Triangle(sx, 128)-128)/8)
			cr[x] = realtimeCPU8Clamp(128 + (realtimeCPU8Triangle(sy, 128)-128)/8)
		}
	}
	return img
}

func realtimeCPU8Triangle(x, period int) int {
	if period <= 0 {
		period = 32
	}
	half := period / 2
	r := ((x % period) + period) % period
	if r < half {
		return r * 255 / half
	}
	return (period - r) * 255 / half
}

func realtimeCPU8Clamp(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func parseRealtimeCPU8FrameRow(trace []byte, frameIdx uint64) map[string]any {
	for _, line := range bytes.Split(trace, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		ty, _ := row["type"].(string)
		if ty != "frame" {
			continue
		}
		fi, ok := row["frame_index"].(float64)
		if !ok || uint64(fi) != frameIdx {
			continue
		}
		return row
	}
	return nil
}
