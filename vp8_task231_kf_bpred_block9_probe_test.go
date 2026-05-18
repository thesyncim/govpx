//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// TestVP8Task231KFBPredBlock9Probe pins the per-MB tracer-based localization
// of the residual byte divergence on the two task #227 1280x720 SSIM seeds
// (regression_option_grid_19981bff BestQuality/VBR and
// regression_option_grid_788d442c GoodQuality/VBR). Task #213 closed seed
// 22f3d67c (CBR cohort) but the VBR variants remain open.
//
// Findings from the task #210 tracer (re-replayed here):
//
// Frame 0 (keyframe), seed 19981bff, threads=1 reproduction (the threads=4
// oracle TU has a known thread-safety bug — see vp8_task210_mb_activity_tracer
// _test.go for the workaround):
//
//   - mb_activity / act_zbin_adj / rdmult / activity_avg: MATCH all 3600 MBs.
//   - Y qcoeff / eob / b_modes / uv_mode / mode / mv: MATCH MBs 0..(0,68).
//   - mb_rate: FIRST diverges at MB(0,8) with +5448 govpx-vs-libvpx delta
//     (govpx=86382 libvpx=80934). At this MB Y qcoeffs, eobs, b_modes, mode,
//     uv_mode, and activity quartet ALL match — the divergence is in the
//     RD-picker's INTERNAL rate accounting, not the committed bitstream
//     state. The govpx-side libvpx oracle clears UV qcoeffs via
//     vp8_dequant_idct_add_uv_block (vp8/common/idct_blk.c:36-72 +
//     vp8/common/dequantize.c:26-37), so the libvpx UV qcoeff snapshot
//     captured by govpx_oracle_capture_mb is all-zeros — the comparator
//     cannot rule out UV qcoeff value drift at this MB (eobs match but
//     non-zero VALUES are unobservable on the libvpx side).
//   - b_modes (sub-MB intra modes): FIRST diverges at MB(0,69), block 9.
//     govpx picks B_DC_PRED; libvpx picks B_LD_PRED. Both have eob[9]=16.
//     Blocks 0..8 of MB(0,69) match exactly (modes, eobs, qcoeffs), so the
//     reconstruction feeding block 9's predictor inputs is byte-identical.
//     Given identical predictor inputs, the fdct/quantize/cost_coeffs/
//     distortion/RDCOST chain SHOULD produce identical RDCOSTs for each
//     B_PRED candidate. The strict-less-than (this_rd < best_rd) tiebreak
//     in libvpx rdopt.c:568 would then resolve to the earlier-iterated
//     mode (B_DC_PRED, mode_index=0). The fact that libvpx picks B_LD_PRED
//     (mode_index=4) implies B_LD_PRED's RDCOST is STRICTLY LESS than
//     B_DC_PRED's in libvpx but EQUAL or GREATER in govpx, so the residual
//     divergence is in one of: predictor pixel calculation
//     (vp8_intra4x4_predict for B_LD_PRED), fdct rounding/saturation,
//     quantize_b for the specific signed-residual pattern at this block,
//     cost_coeffs token-cost table indexing, vp8_block_error squared-sum
//     overflow handling, or RDCOST mb->rdmult / mb->rddiv at this picker
//     state.
//
// 1592 of 3600 MBs in frame 0 have at least one diverging b_mode; 6 MBs
// pick different top-level intra modes (TM/DC_PRED vs B_PRED). These compound
// to a +47-byte total length and +31-byte first_partition_size delta vs
// libvpx, exactly the gap pinned in TestVP8Byte0KF1280x720SSIM{Best,Good}
// ARNRAudit.
//
// libvpx source references (v1.16.0):
//
//   - vp8/encoder/rdopt.c:519-585  rd_pick_intra4x4block (per-block 4x4
//     mode picker; loop over B_DC_PRED..B_HU_PRED, strict-less-than RDCOST
//     comparison, deferred vp8_short_idct4x4llm reconstruction with best
//     mode's dqcoeff).
//   - vp8/encoder/rdopt.c:587-644  rd_pick_intra4x4mby_modes (per-MB 4x4
//     driver; accumulates total_rd, bailouts on total_rd >= best_rd).
//   - vp8/encoder/rdopt.c:417-444  cost_coeffs (per-block token-cost sum).
//   - vp8/encoder/rdopt.c:319-329  vp8_block_error_c (per-block dist).
//   - vp8/encoder/rdopt.h:20       RDCOST macro
//     ((128 + R*RM) >> 8) + DM*D.
//   - vp8/common/reconintra4x4.h:19-32 intra_prediction_down_copy (replicates
//     above-right 4 pixels into rows 3, 7, 11 col 16..19 of dst).
//   - vp8/common/idct_blk.c:36-72  vp8_dequant_idct_add_uv_block_c
//     (clears UV qcoeff post-IDCT, explaining the libvpx-side trace's
//     all-zero UV qcoeffs).
//   - vp8/common/dequantize.c:26-37 vp8_dequant_idct_add_c
//     (memsets qcoeff to zero at the end of IDCT-add).
//
// govpx source references:
//
//   - encoder_intra_pick.go:498-597 predictBestBPredLumaModeRD /
//     predictBestBPredLumaModeRDWithRDConstants (govpx's port of
//     rd_pick_intra4x4block + rd_pick_intra4x4mby_modes).
//   - encoder_analysis_reconstruct.go:74-114 predictAnalysisBPredBlock
//     (govpx's port of vp8_intra4x4_predict's above/left/topleft setup;
//     does the moral-equivalent of intra_prediction_down_copy by reading
//     above[16:20] for the rightmost-column blocks instead of replicating
//     in-place into dst).
//
// This test is logging-only (always passes); it pins the localization so
// the next fix iteration can target rd_pick_intra4x4block's specific
// RDCOST-tiebreak path on block 9 of MB(0,69).
//
// To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  go test -tags govpx_oracle_trace -run TestVP8Task231KFBPredBlock9Probe -v
func TestVP8Task231KFBPredBlock9Probe(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #231 per-MB B_PRED block-9 probe")
	}
	vpxencOracle := findVpxencOracle(t)

	cases := []struct {
		name       string
		seedHash   string
		opts       EncoderOptions
		extra      []string
		targetKbps int
	}{
		{
			name:     "seed_19981bff_best_cpu0_ssim_arnr_1_1_2_threads1",
			seedHash: "19981bff",
			opts: EncoderOptions{
				Width:             1280,
				Height:            720,
				FPS:               30,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineBestQuality,
				CpuUsed:           0,
				Tuning:            TuneSSIM,
				ScreenContentMode: 1,
				TokenPartitions:   1,
				Threads:           1,
				ARNRMaxFrames:     1,
				ARNRStrength:      1,
				ARNRType:          2,
			},
			extra: libvpxEndUsageArgs([]string{
				"--end-usage=vbr",
				"--screen-content-mode=1",
				"--token-parts=1",
				"--threads=1",
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=1",
				"--arnr-type=2",
			}),
			targetKbps: 700,
		},
		{
			name:     "seed_788d442c_good_cpu0_ssim_arnr_1_1_2_threads1",
			seedHash: "788d442c",
			opts: EncoderOptions{
				Width:             1280,
				Height:            720,
				FPS:               30,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
				Tuning:            TuneSSIM,
				ScreenContentMode: 1,
				TokenPartitions:   1,
				Threads:           1,
				ARNRMaxFrames:     1,
				ARNRStrength:      1,
				ARNRType:          2,
			},
			extra: libvpxEndUsageArgs([]string{
				"--end-usage=vbr",
				"--screen-content-mode=1",
				"--token-parts=1",
				"--threads=1",
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=1",
				"--arnr-type=2",
			}),
			targetKbps: 700,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			runTask231BPredBlock9Probe(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extra)
		})
	}
}

func runTask231BPredBlock9Probe(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string) {
	t.Helper()
	requireOracleTraceBuild(t)

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		_, err := enc.EncodeInto(packet, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, seedHash+".yuv")
	ivfPath := filepath.Join(dir, seedHash+".ivf")
	libvpxTracePath := filepath.Join(dir, seedHash+".jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)

	deadlineArg := "--good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	autoAltRefArg := "--auto-alt-ref=0"
	if opts.AutoAltRef {
		autoAltRefArg = "--auto-alt-ref=1"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--lag-in-frames=" + strconv.Itoa(opts.LookaheadFrames),
		autoAltRefArg,
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=" + libvpxOracleTimebaseArg(opts),
		"--fps=" + libvpxOracleFPSArg(opts),
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
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

	govpxOut := "/tmp/govpx_task231_" + seedHash + ".jsonl"
	libvpxOut := "/tmp/libvpx_task231_" + seedHash + ".jsonl"
	_ = os.WriteFile(govpxOut, govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile(libvpxOut, libvpxTrace, 0o644)
	t.Logf("task231 seed=%s govpx_trace=%s libvpx_trace=%s", seedHash, govpxOut, libvpxOut)

	gRows := task210ParseMBRowsForFrame(govpxTraceBuf.Bytes(), 0)
	lRows := task210ParseMBRowsForFrame(libvpxTrace, 0)
	t.Logf("task231 seed=%s frame0 govpx_mb_rows=%d libvpx_mb_rows=%d",
		seedHash, len(gRows), len(lRows))

	gByKey := map[[2]int]map[string]any{}
	lByKey := map[[2]int]map[string]any{}
	keys := [][2]int{}
	for _, r := range gRows {
		row, _ := r["mb_row"].(float64)
		col, _ := r["mb_col"].(float64)
		k := [2]int{int(row), int(col)}
		gByKey[k] = r
		if _, ok := lByKey[k]; !ok {
			keys = append(keys, k)
		}
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

	// Locate first b_modes divergence
	firstBModeDiv := [2]int{-1, -1}
	firstBModeBlock := -1
	var firstBModeGov, firstBModeLib string
	for _, k := range keys {
		g, lok := gByKey[k], lByKey[k]
		l := lok
		gb, ok1 := g["b_modes"].([]any)
		lb, ok2 := l["b_modes"].([]any)
		if !ok1 || !ok2 {
			continue
		}
		if len(gb) != len(lb) {
			continue
		}
		for i := range gb {
			if !task210FieldsEqual(gb[i], lb[i]) {
				firstBModeDiv = k
				firstBModeBlock = i
				firstBModeGov, _ = gb[i].(string)
				firstBModeLib, _ = lb[i].(string)
				break
			}
		}
		if firstBModeDiv[0] >= 0 {
			break
		}
	}
	if firstBModeDiv[0] < 0 {
		t.Logf("task231 seed=%s frame0 NO_BMODE_DIV — bitstream may match", seedHash)
	} else {
		t.Logf("task231 seed=%s frame0 FIRST_BMODE_DIV mb=(%d,%d) block=%d govpx=%s libvpx=%s",
			seedHash, firstBModeDiv[0], firstBModeDiv[1], firstBModeBlock, firstBModeGov, firstBModeLib)
	}

	// Locate first mb_rate divergence (picker-internal accounting)
	firstRateDiv := [2]int{-1, -1}
	var firstRateGov, firstRateLib float64
	for _, k := range keys {
		g, l := gByKey[k], lByKey[k]
		gr, ok1 := g["mb_rate"].(float64)
		lr, ok2 := l["mb_rate"].(float64)
		if !ok1 || !ok2 {
			continue
		}
		if gr != lr {
			firstRateDiv = k
			firstRateGov = gr
			firstRateLib = lr
			break
		}
	}
	if firstRateDiv[0] < 0 {
		t.Logf("task231 seed=%s frame0 NO_RATE_DIV", seedHash)
	} else {
		t.Logf("task231 seed=%s frame0 FIRST_RATE_DIV mb=(%d,%d) govpx=%.0f libvpx=%.0f delta=%+.0f",
			seedHash, firstRateDiv[0], firstRateDiv[1], firstRateGov, firstRateLib,
			firstRateGov-firstRateLib)
	}

	// Detail dump of MB(0,69) block 9 — the canonical divergent attempt
	canon := [2]int{0, 69}
	if g, ok := gByKey[canon]; ok {
		l := lByKey[canon]
		t.Logf("task231 seed=%s frame0 MB(0,69) detail:", seedHash)
		for _, f := range []string{"mode", "uv_mode", "mb_rate", "aggregated_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
			gv := g[f]
			lv := l[f]
			marker := ""
			if !task210FieldsEqual(gv, lv) {
				marker = " <DIFF>"
			}
			t.Logf("  %-18s govpx=%v libvpx=%v%s", f, gv, lv, marker)
		}
		gb, _ := g["b_modes"].([]any)
		lb, _ := l["b_modes"].([]any)
		geb, _ := g["eob"].([]any)
		leb, _ := l["eob"].([]any)
		for i := range gb {
			gm, _ := gb[i].(string)
			lm, _ := lb[i].(string)
			marker := ""
			if gm != lm {
				marker = " <BMODE_DIFF>"
			}
			ge := geb[i]
			le := leb[i]
			emarker := ""
			if !task210FieldsEqual(ge, le) {
				emarker = " <EOB_DIFF>"
			}
			t.Logf("  block %2d: bmode govpx=%-12s libvpx=%-12s eob govpx=%v libvpx=%v%s%s",
				i, gm, lm, ge, le, marker, emarker)
		}
	}

	// Count divergent MBs (b_modes only)
	bmodeDiff := 0
	rateDiff := 0
	for _, k := range keys {
		g, l := gByKey[k], lByKey[k]
		if !task210FieldsEqual(g["b_modes"], l["b_modes"]) {
			bmodeDiff++
		}
		if !task210FieldsEqual(g["mb_rate"], l["mb_rate"]) {
			rateDiff++
		}
	}
	t.Logf("task231 seed=%s frame0 b_mode_div_count=%d rate_div_count=%d (of %d MBs)",
		seedHash, bmodeDiff, rateDiff, len(keys))
}
