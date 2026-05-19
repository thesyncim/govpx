//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestVP8Byte49Frame2DivergenceClosure pins task #200: the byte-49 of frame 2
// divergence captured by FuzzEncoderProductionStreamByteParity seed
// `regression_option_grid_a438fec8` (bytes "1200000"). Frame 2 of a
// 128x128 / best / cpu=4 / CBR / tune=SSIM clip diverged from the libvpx
// oracle at byte 49 within the entropy-coded first partition until the
// SSIM activity-map recode-rebuild fix landed in commit 2accbaaa
// ("vp8: rebuild SSIM activity_map per recode attempt (tasks #183/#201)").
//
// Pre-fix signature (now closed) — frame 2 first_diff=49, gov=0xf6 lib=0xf7,
// single coef-prob slot delta at (b=2,band=6,ctx=2,node=5) gov=180 lib=184.
// Same UV act_zbin_adj cascade as task #183 (160x96 cohort, byte 58, slot
// gov=156 lib=159). The recoded activity_map rebuild collapses both
// cascades by feeding the next attempt fresh per-MB act_zbin_adj values
// keyed off the recoded base_qindex, matching libvpx's encodeframe.c:721-
// 732 cadence inside the onyx_if.c:3962-3968 recode do-loop.
//
// Cohort + decision graph:
//
//   - 128x128 (resBucket=5) is the smallest option-grid resolution that
//     reaches inter frame 2 with this (deadline, cpu, tune) combination
//     via the (b=2,band=6,ctx=2,node=5) UV coef-prob slot.
//   - deadline=best + cpu=4 ⇒ Mode=0/Speed=0 in libvpx (onyx_if.c:814-
//     815); same path as cpu=0 with sf->improved_quant=1 +
//     use_fastquant_for_pick=0 + optimize_coefficients=1, so encode uses
//     vp8_regular_quantize_b whose zbin_extra reads x->act_zbin_adj via
//     ZBIN_EXTRA_UV (vp8_quantize.c:281-289).
//   - tune=SSIM enables build_activity_map / adjust_act_zbin per inter MB
//     (encodeframe.c:1191-1194); without SSIM the cohort would short-
//     circuit.
//   - CBR (rcBucket=0) recodes when the rate controller rejects an
//     attempt; the per-attempt rebuild matches libvpx's per-vp8_encode_
//     frame cadence in the recode do-loop.
//   - threads=0 ⇒ single-threaded reconstruction (no MT helper-row
//     ymode_count bias is applied).
//   - frame 0 (KF) and frame 1 (first inter) were byte-MATCH; divergence
//     first appeared on frame 2 because that was the first inter frame
//     whose recode attempt re-quantized against the (now corrected)
//     per-MB act_zbin_adj.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/onyx_if.c:3962-3968 recode do-loop
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:721-732 build_activity_map
//     gate inside vp8_encode_frame
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1191-1194 inter
//     adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:276-289 ZBIN_EXTRA_*
//     macros
//   - govpx commit 2accbaaa "vp8: rebuild SSIM activity_map per recode
//     attempt (tasks #183/#201)" — closes both this seed and the task
//     #183 160x96 audit (regression_option_grid_75578e9f)
//   - govpx vp8_encoder_attempts.go encodeInterFrameWithQuantizerFeedback —
//     the recode-attempt loop with the in-loop prepareTuningActivityMap
//
// Companion: TestVP8Byte58Frame2DivergenceAudit captures the historical
// 160x96 cohort.
func TestVP8Byte49Frame2DivergenceClosure(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the audit replay")
	}
	vpxencOracle := findVpxencOracle(t)

	opts := EncoderOptions{
		Width:             128,
		Height:            128,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           4,
		Tuning:            TuneSSIM,
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=cbr",
		"--tune=ssim",
	})

	sources := make([]Image, 6)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(128, 128, i)
	}

	govpxFrames := encodeFramesWithGovpx(t, opts, sources)
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task200-byte49-closure", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 6 || len(libvpxFrames) < 6 {
		t.Fatalf("expected 6 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Pin the historical metrics so future regressions don't silently
	// re-interpret what this closure captured.
	wantLens := [6]int{11797, 1883, 1059, 2249, 789, 2264}
	for i, want := range wantLens {
		if len(govpxFrames[i]) != want || len(libvpxFrames[i]) != want {
			t.Fatalf("frame %d len drift: govpx=%d libvpx=%d want=%d",
				i, len(govpxFrames[i]), len(libvpxFrames[i]), want)
		}
	}

	// All six frames must byte-match. A failure here means the SSIM
	// activity_map recode-rebuild fix has regressed.
	for i := range 6 {
		if !bytesEqualTask183(govpxFrames[i], libvpxFrames[i]) {
			diff := -1
			n := min(len(govpxFrames[i]), len(libvpxFrames[i]))
			for k := 0; k < n; k++ {
				if govpxFrames[i][k] != libvpxFrames[i][k] {
					diff = k
					break
				}
			}
			t.Fatalf("frame %d byte mismatch: first_diff=%d gov=0x%02x lib=0x%02x len=%d",
				i, diff, govpxFrames[i][diff], libvpxFrames[i][diff], len(govpxFrames[i]))
		}
	}

	// Coef-prob fingerprint: pin that the (b=2,band=6,ctx=2,node=5) slot
	// the historical divergence lived on now matches between govpx and
	// libvpx. A regression on the SSIM activity-map recode path almost
	// always re-opens this slot first (see task #183 audit).
	var govpxProbs tables.CoefficientProbs
	var libvpxProbs tables.CoefficientProbs
	prevQuant := vp8dec.QuantHeader{}
	for i := 0; i <= 2; i++ {
		gp := govpxProbs
		lp := libvpxProbs
		if i == 0 {
			gp = tables.DefaultCoefProbs
			lp = tables.DefaultCoefProbs
		}
		_, gState, _, err := vp8dec.ParseStateHeaderWithReaderAndProbs(govpxFrames[i], prevQuant, &gp)
		if err != nil {
			t.Fatalf("govpx parse frame %d: %v", i, err)
		}
		_, lState, _, err := vp8dec.ParseStateHeaderWithReaderAndProbs(libvpxFrames[i], prevQuant, &lp)
		if err != nil {
			t.Fatalf("libvpx parse frame %d: %v", i, err)
		}
		govpxProbs = gp
		libvpxProbs = lp
		prevQuant = gState.Quant
		_ = lState
	}

	// At this point every frame is byte-MATCH so the coef-prob tables
	// must agree across the entire 4x8x3x11 grid; the (b=2,band=6,ctx=2,
	// node=5) cell is the single historical divergence sentinel.
	const (
		sentinelBlock = 2
		sentinelBand  = 6
		sentinelCtx   = 2
		sentinelNode  = 5
	)
	if govpxProbs[sentinelBlock][sentinelBand][sentinelCtx][sentinelNode] !=
		libvpxProbs[sentinelBlock][sentinelBand][sentinelCtx][sentinelNode] {
		t.Fatalf("frame 2 coef-prob sentinel slot (b=%d,band=%d,ctx=%d,node=%d) diverged: gov=%d lib=%d",
			sentinelBlock, sentinelBand, sentinelCtx, sentinelNode,
			govpxProbs[sentinelBlock][sentinelBand][sentinelCtx][sentinelNode],
			libvpxProbs[sentinelBlock][sentinelBand][sentinelCtx][sentinelNode])
	}

	diffCount := 0
	for b := 0; b < tables.BlockTypes; b++ {
		for n := 0; n < tables.CoefBands; n++ {
			for c := 0; c < tables.PrevCoefContexts; c++ {
				for nd := 0; nd < tables.EntropyNodes; nd++ {
					if govpxProbs[b][n][c][nd] != libvpxProbs[b][n][c][nd] {
						diffCount++
						t.Errorf("unexpected coef-prob delta at b=%d band=%d ctx=%d node=%d gov=%d lib=%d",
							b, n, c, nd,
							govpxProbs[b][n][c][nd], libvpxProbs[b][n][c][nd])
					}
				}
			}
		}
	}
	if diffCount != 0 {
		t.Fatalf("expected 0 coef-prob deltas after frame 2; got %d", diffCount)
	}
	t.Logf("task #200 closure pinned: 128x128/best/cpu=4/CBR/SSIM byte-49 frame 2 divergence closed by commit 2accbaaa (SSIM activity_map recode-rebuild)")
}
