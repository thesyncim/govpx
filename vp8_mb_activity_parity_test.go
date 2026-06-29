//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8MBActivitySeedsMatchLibvpx compares govpx and libvpx per-MB
// activity-masking trace rows for the 1280x720 SSIM fixtures that exercise
// ARNR residual parity. It reports the first frame row whose mb_activity,
// act_zbin_adj, rdmult, or activity_avg value differs.
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
	vp8test.RequireOracle(t, "per-MB activity parity")
	vpxencOracle := vp8test.VpxencOracle(t)

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

	ivfData, libvpxTrace, diag, err := vp8test.VpxencVP8OracleEncodeTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(vpxencOracle, opts, len(sources), targetKbps, nil, extraArgs),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v (skipping rest of parity run; full output above)", err)
	}

	libvpxFrames, err := testutil.IVFFramePayloads(ivfData)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}

	t.Logf("mb_activity seed=%s govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		seedHash, govpxTraceBuf.Len(), len(libvpxTrace))

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
	// quartet (mb_activity, act_zbin_adj, rdmult, activity_avg).
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
		// Also locate the first MB-row of canonical mode/ref/MV divergence.
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
	return parseMBActivityRowsByFrame(trace)[frameIndex]
}

func parseMBActivityRowsByFrame(trace []byte) map[uint64][]map[string]any {
	byFrame := map[uint64][]map[string]any{}
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
		if !ok {
			continue
		}
		frameIndex := uint64(fi)
		byFrame[frameIndex] = append(byFrame[frameIndex], row)
	}
	return byFrame
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
