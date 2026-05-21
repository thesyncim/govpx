//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
	"github.com/thesyncim/govpx/internal/testutil"
)

// TestVP8MBActivitySeedsMatchLibvpx drives the two residual 1280x720
// SSIM fuzz seeds (94eb71d5, 19981bff) through the extended per-MB
// tracer and reports the first MB-row whose mb_activity / act_zbin_adj /
// rdmult / activity_avg quartet diverges between govpx and libvpx.
//
// Mission per task #210: extend the per-MB oracle tracer with
// the libvpx activity-masking diagnostic (cpi->mb_activity_map[idx],
// x->act_zbin_adj, x->rdmult, cpi->activity_avg). The existing tracer
// already exposes per-MB mode/ref/MV/EOB/qcoeff/rate fields; this test
// re-runs both seeds through the new tracer infrastructure and logs the
// first row of divergence so the next fix-commit can identify the
// libvpx computation path that govpx is missing.
//
// The test is logging-only (always passes) so it can stay in the tree as
// a long-lived audit anchor. Run with:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  go test -tags govpx_oracle_trace -run TestVP8MBActivitySeedsMatchLibvpx -v
//
// libvpx source references (v1.16.0):
//
//   - vp8/encoder/encodeframe.c:225-289  build_activity_map
//   - vp8/encoder/encodeframe.c:293-314  vp8_activity_masking
//   - vp8/encoder/encodeframe.c:1074-1092 adjust_act_zbin
//   - vp8/encoder/encodeframe.c:1094-1128 vp8cx_encode_intra_macroblock
//     (adjust_act_zbin call line 1106)
//   - vp8/encoder/encodeframe.c:1135-1300 vp8cx_encode_inter_macroblock
//     (adjust_act_zbin call line 1193)
//   - vp8/encoder/encodeframe.c:406      x->rdmult = cpi->RDMULT seed
//   - vp8/encoder/encodeframe.c:423      vp8_activity_masking gate
//     (cpi->oxcf.tuning == VP8_TUNE_SSIM)
//   - vp8/encoder/encodeframe.c:588      x->act_zbin_adj = 0 base init
//   - vp8/encoder/onyx_if.c:1906         cpi->activity_avg = 90 << 12
func TestVP8MBActivitySeedsMatchLibvpx(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run per-MB activity parity")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	cases := []struct {
		name       string
		seedHash   string
		opts       EncoderOptions
		extra      []string
		targetKbps int
	}{
		{
			name:     "seed_94eb71d5_good_cpu0_ssim_arnr_1_2_1",
			seedHash: "94eb71d5",
			opts: EncoderOptions{
				Width:             1280,
				Height:            720,
				FPS:               30,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
				Tuning:            TuneSSIM,
				ARNRMaxFrames:     1,
				ARNRStrength:      2,
				ARNRType:          1,
			},
			extra: libvpxEndUsageArgs([]string{
				"--end-usage=cbr",
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=2",
				"--arnr-type=1",
			}),
			targetKbps: 700,
		},
		{
			// Matches TestVP8KF1280x720SSIMBestARNRParity (task #207):
			// 1280x720 BestQuality / cpu=0 / VBR / SC=1 / threads=4 /
			// TuneSSIM / ARNR=1/1/2 / token-parts=1.
			//
			// NOTE: this seed normally runs at threads=4, but the libvpx
			// oracle TU's inter-candidate realloc path is not thread-safe
			// (govpx_oracle_capture_inter_candidate's shared
			// govpx_oracle_state.candidate_rows realloc races across the
			// helper threads, aborting with
			// `pointer being freed was not allocated`). We probe the
			// same parameter cohort here with threads=1 so the per-MB
			// activity quartet emit still surfaces in the trace; the
			// pre-existing oracle-trace thread-safety gap is outside
			// task #210's scope.
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
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			runVP8MBActivityParity(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extra)
		})
	}
}

func runVP8MBActivityParity(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string) {
	t.Helper()
	requireOracleTraceBuild(t)

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	// govpx side
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	govpxFrames := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeInto(packet, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if !result.Dropped {
			govpxFrames = append(govpxFrames, append([]byte(nil), result.Data...))
		}
	}
	enc.Close()

	// libvpx oracle side
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
		// The oracle TU's inter-candidate realloc path is not thread-safe
		// (govpx_oracle_capture_inter_candidate's shared
		// govpx_oracle_state.candidate_rows realloc races across the
		// helper threads when threads>1, aborting with `pointer being
		// freed was not allocated`). Skip rather than fail so the probe
		// still surfaces partial findings; the thread-safety gap is a
		// separate pre-existing oracle-trace issue outside task #210's
		// scope.
		t.Logf("vpxenc-oracle args: %v", args)
		t.Logf("vpxenc-oracle output:\n%s", out)
		t.Skipf("vpxenc-oracle failed: %v (skipping rest of parity run; full output above)", err)
	}

	ivfData, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read libvpx ivf: %v", err)
	}
	libvpxFrames, err := testutil.IVFFramePayloads(ivfData)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	// Persist traces for offline inspection.
	govpxOut := "/tmp/govpx_mb_activity_" + seedHash + ".jsonl"
	libvpxOut := "/tmp/libvpx_mb_activity_" + seedHash + ".jsonl"
	_ = os.WriteFile(govpxOut, govpxTraceBuf.Bytes(), 0o644)
	_ = os.WriteFile(libvpxOut, libvpxTrace, 0o644)
	t.Logf("mb_activity seed=%s govpx_trace=%s libvpx_trace=%s", seedHash, govpxOut, libvpxOut)

	// Frame-size summary so the byte-gap is co-located with the
	// per-MB findings.
	for fi := 0; fi < len(govpxFrames) && fi < len(libvpxFrames); fi++ {
		gf, lf := govpxFrames[fi], libvpxFrames[fi]
		minLen := len(gf)
		if len(lf) < minLen {
			minLen = len(lf)
		}
		firstByteDiff := -1
		for i := 0; i < minLen; i++ {
			if gf[i] != lf[i] {
				firstByteDiff = i
				break
			}
		}
		if firstByteDiff == -1 && len(gf) != len(lf) {
			firstByteDiff = minLen
		}
		t.Logf("mb_activity seed=%s frame%d govpx_len=%d libvpx_len=%d size_delta=%d first_byte_diff=%d",
			seedHash, fi, len(gf), len(lf), len(gf)-len(lf), firstByteDiff)
	}

	// Locate the first diverging MB in each frame on the activity-masking
	// quartet (mb_activity, act_zbin_adj, rdmult, activity_avg). These are
	// the new fields task #210 added; any divergence here surfaces the
	// activity-masking computation gap that the libvpx oracle now exposes.
	for fi := 0; fi < 2; fi++ {
		gRows := parseMBActivityRowsForFrame(govpxTraceBuf.Bytes(), uint64(fi))
		lRows := parseMBActivityRowsForFrame(libvpxTrace, uint64(fi))
		t.Logf("mb_activity seed=%s frame%d govpx_mb_rows=%d libvpx_mb_rows=%d",
			seedHash, fi, len(gRows), len(lRows))
		minRows := len(gRows)
		if len(lRows) < minRows {
			minRows = len(lRows)
		}
		reportFields := []string{
			"mb_activity", "act_zbin_adj", "rdmult", "activity_avg",
			"mode", "ref_frame", "segment_id", "mv_row", "mv_col",
			"skip", "eob_sum", "mb_rate", "aggregated_rate",
		}
		firstFieldDiv := -1
		var firstDivField string
		for i := 0; i < minRows; i++ {
			g, l := gRows[i], lRows[i]
			for _, f := range []string{"mb_activity", "act_zbin_adj", "rdmult", "activity_avg"} {
				if !mbTraceFieldsEqual(g[f], l[f]) {
					firstFieldDiv = i
					firstDivField = f
					break
				}
			}
			if firstFieldDiv >= 0 {
				break
			}
		}
		if firstFieldDiv >= 0 {
			g, l := gRows[firstFieldDiv], lRows[firstFieldDiv]
			t.Logf("mb_activity seed=%s frame%d FIRST_ACTIVITY_DIV idx=%d mb_row=%v mb_col=%v field=%s",
				seedHash, fi, firstFieldDiv, g["mb_row"], g["mb_col"], firstDivField)
			for _, f := range reportFields {
				gv, gok := g[f]
				lv, lok := l[f]
				if !gok && !lok {
					continue
				}
				marker := ""
				if !mbTraceFieldsEqual(gv, lv) {
					marker = " <DIFF>"
				}
				t.Logf("  %-16s govpx=%v libvpx=%v%s", f, gv, lv, marker)
			}
		} else if minRows > 0 {
			t.Logf("mb_activity seed=%s frame%d ACTIVITY_MATCH first_%d_mbs all 4 fields equal",
				seedHash, fi, minRows)
		}
		// Also locate the first MB-row of CANONICAL divergence so we keep
		// continuity with the task-206 probe semantics.
		firstCanonDiv := -1
		canonFields := []string{"mode", "ref_frame", "mv_row", "mv_col", "skip", "eob_sum"}
		for i := 0; i < minRows; i++ {
			g, l := gRows[i], lRows[i]
			for _, f := range canonFields {
				if !mbTraceFieldsEqual(g[f], l[f]) {
					firstCanonDiv = i
					break
				}
			}
			if firstCanonDiv >= 0 {
				break
			}
		}
		if firstCanonDiv >= 0 {
			g := gRows[firstCanonDiv]
			t.Logf("mb_activity seed=%s frame%d FIRST_CANON_DIV idx=%d mb_row=%v mb_col=%v",
				seedHash, fi, firstCanonDiv, g["mb_row"], g["mb_col"])
		}
	}
}

func parseMBActivityRowsForFrame(trace []byte, frameIndex uint64) []map[string]any {
	rows := []map[string]any{}
	scanner := bufio.NewScanner(bytes.NewReader(trace))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			continue
		}
		t, _ := row["type"].(string)
		if t != "mb" {
			continue
		}
		fi, ok := row["frame_index"].(float64)
		if !ok || uint64(fi) != frameIndex {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// mbTraceFieldsEqual compares two JSON-decoded fields. JSON numbers all
// decode to float64; bools and strings compare directly. nil vs non-nil
// is treated as inequality. Arrays compare element-wise.
func mbTraceFieldsEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !mbTraceFieldsEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}
