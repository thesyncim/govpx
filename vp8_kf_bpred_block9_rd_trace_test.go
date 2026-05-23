//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"os"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8KFBPredBlock9TraceLocalizesRDCostDrift compares govpx and libvpx
// keyframe per-MB traces for the 1280x720 SSIM ARNR fixtures. The test is
// logging-only: it reports the first B_PRED block-mode drift and first MB-rate
// drift so keyframe RD parity work has a stable reproduction.
//
// To run:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_VPXENC_ORACLE=/path/to/vpxenc-oracle \
//	  go test -tags govpx_oracle_trace -run TestVP8KFBPredBlock9TraceLocalizesRDCostDrift -v
func TestVP8KFBPredBlock9TraceLocalizesRDCostDrift(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run B_PRED block-9 RD trace")
	}
	vpxencOracle := vp8test.VpxencOracle(t)

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
			runVP8KFBPredBlock9TraceLocalizesRDCostDrift(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extra)
		})
	}
}

func runVP8KFBPredBlock9TraceLocalizesRDCostDrift(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string) {
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

	libvpxTrace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(vpxencOracle, opts, len(sources), targetKbps, nil, extraArgs),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	t.Logf("bpred_block9 seed=%s govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		seedHash, govpxTraceBuf.Len(), len(libvpxTrace))

	gRows := parseMBActivityRowsForFrame(govpxTraceBuf.Bytes(), 0)
	lRows := parseMBActivityRowsForFrame(libvpxTrace, 0)
	t.Logf("bpred_block9 seed=%s frame0 govpx_mb_rows=%d libvpx_mb_rows=%d",
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
			if !mbTraceFieldsEqual(gb[i], lb[i]) {
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
		t.Logf("bpred_block9 seed=%s frame0 NO_BMODE_DIV — bitstream may match", seedHash)
	} else {
		t.Logf("bpred_block9 seed=%s frame0 FIRST_BMODE_DIV mb=(%d,%d) block=%d govpx=%s libvpx=%s",
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
		t.Logf("bpred_block9 seed=%s frame0 NO_RATE_DIV", seedHash)
	} else {
		t.Logf("bpred_block9 seed=%s frame0 FIRST_RATE_DIV mb=(%d,%d) govpx=%.0f libvpx=%.0f delta=%+.0f",
			seedHash, firstRateDiv[0], firstRateDiv[1], firstRateGov, firstRateLib,
			firstRateGov-firstRateLib)
	}

	// Detail dump of MB(0,69) block 9 — the canonical divergent attempt
	canon := [2]int{0, 69}
	if g, ok := gByKey[canon]; ok {
		l := lByKey[canon]
		t.Logf("bpred_block9 seed=%s frame0 MB(0,69) detail:", seedHash)
		for _, f := range []string{"mode", "uv_mode", "mb_rate", "aggregated_rate", "mb_activity", "act_zbin_adj", "rdmult"} {
			gv := g[f]
			lv := l[f]
			marker := ""
			if !mbTraceFieldsEqual(gv, lv) {
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
			if !mbTraceFieldsEqual(ge, le) {
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
		if !mbTraceFieldsEqual(g["b_modes"], l["b_modes"]) {
			bmodeDiff++
		}
		if !mbTraceFieldsEqual(g["mb_rate"], l["mb_rate"]) {
			rateDiff++
		}
	}
	t.Logf("bpred_block9 seed=%s frame0 b_mode_div_count=%d rate_div_count=%d (of %d MBs)",
		seedHash, bmodeDiff, rateDiff, len(keys))
}
