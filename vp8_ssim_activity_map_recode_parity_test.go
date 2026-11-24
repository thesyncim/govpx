//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestVP8SSIMActivityMapRecodeBestQualityParity pins the 128x128 BestQuality
// SSIM cohort that first exposed stale per-MB activity maps across VP8 recode
// attempts. Frame 2 of seed regression_option_grid_a438fec8 diverged from
// libvpx until govpx rebuilt the activity map at the start of each recode
// attempt, matching libvpx's vp8_encode_frame cadence.
//
// Pre-fix signature (now closed): frame 2 first_diff=49, gov=0xf6 lib=0xf7,
// single coef-prob slot delta at (b=2,band=6,ctx=2,node=5) gov=180 lib=184.
// Same UV act_zbin_adj cascade as the companion 160x96 cohort (byte 58,
// slot gov=156 lib=159). The recoded activity_map rebuild collapses both
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
//   - govpx vp8_encoder_attempts.go encodeInterFrameWithQuantizerFeedback —
//     the recode-attempt loop with the in-loop prepareTuningActivityMap
func TestVP8SSIMActivityMapRecodeBestQualityParity(t *testing.T) {
	vp8test.RequireOracle(t, "the VP8 SSIM activity-map recode parity replay")
	vpxencOracle := vp8test.VpxencOracle(t)

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
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "vp8-ssim-activity-recode-best-128x128", opts, 700, sources, extraArgs)

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
		if !bytes.Equal(govpxFrames[i], libvpxFrames[i]) {
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
	// always re-opens this slot first (see the companion 160x96 parity test).
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
	t.Logf("VP8 SSIM activity-map recode parity pinned: 128x128/best/cpu=4/CBR")
}

// TestVP8SSIMActivityMapRecodeGoodQualityParity pins the 160x96 GoodQuality
// SSIM cohort from seed regression_option_grid_75578e9f. It exercises the
// same stale activity-map failure mode as the 128x128 BestQuality cohort, but
// adds the GoodQuality/cpu=0/ARNR option surface that previously shifted one UV
// coefficient across the activity-adjusted zbin boundary on frame 2.
//
// Pre-fix signature (now closed): frame 2 first_diff=58, matching first
// partition sizes, and exactly one coefficient-probability update delta at:
//
//	(block_type=2 (UV), band=6, prev_coef_ctx=2, entropy_node=5)
//	    govpx emits 156
//	    libvpx emits 159
//
// The token-count shape at that slot was govpx Three=50/Four=32 versus the
// libvpx Three=51/Four=31 distribution. Rebuilding the activity map for every
// recode attempt gives the quantizer fresh per-MB act_zbin_adj values keyed to
// the recoded base_qindex, so the full six-frame clip now byte-matches libvpx.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:276-289 ZBIN_EXTRA_* macros
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:410-428
//     vp8_update_zbin_extra
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:726-732 build_activity_map gate
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1105-1108 intra adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1191-1194 inter adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/bitstream.c:865-950 vp8_update_coef_probs
//   - libvpx v1.16.0 vp8/common/treecoder.c:78-102
//     vp8_tree_probs_from_distribution
//   - govpx vp8_encoder_tuning.go prepareTuningActivityMap and
//     tunedZbinAdjustment
//   - govpx vp8_encoder_attempts.go encodeInterFrameWithQuantizerFeedback
func TestVP8SSIMActivityMapRecodeGoodQualityParity(t *testing.T) {
	vp8test.RequireOracle(t, "the VP8 SSIM activity-map recode parity replay")
	vpxencOracle := vp8test.VpxencOracle(t)

	opts := EncoderOptions{
		Width:             160,
		Height:            96,
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
	}
	extraArgs := libvpxEndUsageArgs([]string{
		"--end-usage=cbr",
		"--tune=ssim",
		"--arnr-maxframes=1",
		"--arnr-strength=2",
		"--arnr-type=1",
	})

	sources := make([]Image, 6)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(160, 96, i)
	}

	govpxFrames, counts := encodeVP8FramesCapturingInterTokenCounts(t, opts, sources, 2)
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "vp8-ssim-activity-recode-good-160x96", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 6 || len(libvpxFrames) < 6 {
		t.Fatalf("expected at least 6 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	for i := range 6 {
		if !bytes.Equal(govpxFrames[i], libvpxFrames[i]) {
			firstDiff := -1
			maxLen := len(govpxFrames[i])
			if len(libvpxFrames[i]) < maxLen {
				maxLen = len(libvpxFrames[i])
			}
			for j := 0; j < maxLen; j++ {
				if govpxFrames[i][j] != libvpxFrames[i][j] {
					firstDiff = j
					break
				}
			}
			t.Fatalf("frame %d byte match regressed after activity-map rebuild: govpx_len=%d libvpx_len=%d first_diff=%d",
				i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff)
		}
	}

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

	for b := 0; b < tables.BlockTypes; b++ {
		for n := 0; n < tables.CoefBands; n++ {
			for c := 0; c < tables.PrevCoefContexts; c++ {
				for nd := 0; nd < tables.EntropyNodes; nd++ {
					if govpxProbs[b][n][c][nd] != libvpxProbs[b][n][c][nd] {
						t.Fatalf("coef-prob delta after activity-map rebuild at b=%d band=%d ctx=%d node=%d gov=%d lib=%d",
							b, n, c, nd, govpxProbs[b][n][c][nd], libvpxProbs[b][n][c][nd])
					}
				}
			}
		}
	}

	if counts == nil {
		t.Fatalf("encoder did not capture frame 2 token counts")
	}
	wantTokenThree := 51
	wantTokenFour := 31
	if got := counts[2][6][2][tables.ThreeToken]; got != wantTokenThree {
		t.Fatalf("token count drift at (b=2,band=6,ctx=2) ThreeToken: got=%d want=%d", got, wantTokenThree)
	}
	if got := counts[2][6][2][tables.FourToken]; got != wantTokenFour {
		t.Fatalf("token count drift at (b=2,band=6,ctx=2) FourToken: got=%d want=%d", got, wantTokenFour)
	}
	t.Logf("VP8 SSIM activity-map recode parity pinned: 160x96/good/cpu=0/CBR Three=%d Four=%d",
		wantTokenThree, wantTokenFour)
}

// encodeVP8FramesCapturingInterTokenCounts encodes sources with govpx and
// returns a copy of the encoder's inter-frame coefficient token counts after
// captureFrame. Subsequent frames are still encoded so callers can assert the
// full stream shape.
func encodeVP8FramesCapturingInterTokenCounts(t *testing.T, opts EncoderOptions, sources []Image, captureFrame int) ([][]byte, *vp8enc.InterCoefficientTokenCounts) {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	var captured *vp8enc.InterCoefficientTokenCounts
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
		if i == captureFrame {
			c := enc.interCoefTokenCounts
			captured = &c
		}
	}
	for {
		result, err := enc.FlushInto(buf)
		if err != nil {
			break
		}
		if !result.Dropped {
			out = append(out, append([]byte(nil), result.Data...))
		}
	}
	return out, captured
}
