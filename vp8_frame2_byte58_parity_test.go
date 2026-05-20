//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// TestVP8Frame2Byte58DivergenceParity pins task #183: the byte-58 of frame 2
// divergence captured by FuzzEncoderProductionStreamByteParity seed
// `regression_option_grid_75578e9f` (bytes "21200000"). Frame 2 of a
// 160x96 / good / cpu0 / CBR / tune=SSIM / arnr=1/2/1 clip diverges
// from the libvpx oracle at byte 58 within the entropy-coded first
// partition (first_partition_size = 158 in both encoders).
//
// Bisection by header-section comparison (parseStateHeader-driven) pins the
// divergence to a SINGLE coefficient-probability update slot:
//
//	(block_type=2 (UV), band=6, prev_coef_ctx=2, entropy_node=5)
//	    govpx emits 156
//	    libvpx emits 159
//
// branch counts at this slot decode as:
//
//	govpx token counts: Three=50, Four=32, total at node 5 = 82
//	govpx newProb = (50*256 + 41) / 82 = 12841/82 = 156 (verified)
//	libvpx must have: Three=51, Four=31 → (51*256 + 41)/82 = 159
//
// A single token shifting ThreeToken↔FourToken between encoders is the
// signature of one UV coefficient tipping across the activity-adjusted ZBIN
// boundary. tune=SSIM is the only knob in this cohort that mutates the
// UV ZBIN at quantize time:
//
//   - libvpx vp8/encoder/encodeframe.c:1191 sets adjust_act_zbin() per
//     inter MB whenever cpi->oxcf.tuning == VP8_TUNE_SSIM (entry point
//     pp. 423, 726, 1105, 1191; vp8_quantize.c:281 ZBIN_EXTRA_UV expands
//     (UVdequant[QIndex][1] * (zbin_over_quant + zbin_mode_boost +
//     act_zbin_adj)) >> 7).
//
//   - govpx mirrors the per-MB act_zbin_adj via
//     VP8Encoder.tunedZbinAdjustment / vp8_encoder_tuning.go:343 and the
//     ZBIN_EXTRA_UV math via quantizeBlockWithZbinAndActivity
//     (vp8_encoder_inter_quantize.go:64). Both pipelines compute the same
//     formula symbolically.
//
//   - The residual divergence therefore lives in the per-MB activity
//     value going INTO the act_zbin_adj formula. govpx's
//     ssimActivityMeasure (vp8_encoder_tuning.go:137) ports
//     libvpx's mb_activity_measure / vp8_encode_intra (the ALT_ACT_MEASURE
//     path, encodeframe.c:1031 onward in v1.16.0): predict intra,
//     vpx_get_mb_ss of (src - predictor), then quantize+IDCT-rebuild into
//     the recon buffer so the next MB's prediction reads from the rebuilt
//     neighbours. Activity values DO depend on the recon buffer state,
//     so a residual 1-pixel reconstruction delta at any one MB can shift
//     downstream activity_measure SSE values, which in turn shifts
//     act_zbin_adj for the affected MB(s), which finally shifts one UV
//     qcoeff between 3 and 4 at one MB's band-6 ctx-2 position.
//
// Root cause (closed by task #201): govpx was building the per-MB
// activity_map ONCE before the recode loop in vp8_encoder_frame.go, while
// libvpx vp8/encoder/encodeframe.c:721-732 rebuilds it inside every
// vp8_encode_frame call (i.e. on each recode attempt) keyed off the new
// cm->base_qindex. When the recode loop reran the inter frame at a
// different Q, libvpx produced a fresh activity_map (and therefore
// fresh per-MB act_zbin_adj values) while govpx reused the stale map
// from the first attempt. The activity-adjusted ZBIN on UV blocks
// diverged on the recoded attempt by exactly one coefficient, which
// tipped a single (THREE,FOUR)→(THREE+1,FOUR-1) token count on UV and
// shifted the (b=2,band=6,ctx=2,node=5) coef prob from 156 to 159.
// Fix: call prepareTuningActivityMap at the start of each recode
// attempt in vp8_encoder_attempts.go (inter + key paths). The downstream
// byte-2 cascade on frames 3+ also closes because the seed
// regression_option_grid_a4ba465f cohort shared the same root cause.
//
// Cohort + decision graph captured here so a future fix knows the exact
// trigger surface to bisect against:
//
//   - Resolution 160x96 is the smallest in the option-grid resPool that
//     reaches inter frame 2 (smaller pools have fewer frames after the
//     keyframe).
//   - deadline=good + cpu=0 ⇒ libvpxUseFastQuant=false, libvpxUseFastQuantForPick=false
//     so both picker and encode pass through the regular quantizer
//     (the SSIM zbin path).
//   - tune=SSIM enables build_activity_map / adjust_act_zbin /
//     vp8_update_zbin_extra; without SSIM the cohort would short-circuit.
//   - arnr=1/2/1 with arnr_max_frames=1 is a no-op for buffer construction
//     in single-pass CBR (active_arnr_frames=1, no temporal blending);
//     it does NOT generate an extra ARF packet in the 6-frame output.
//   - threads=0 ⇒ single-threaded reconstruction (no MT helper-row
//     ymode_count bias is applied).
//   - frame 0 (KF) and frame 1 (first inter) are byte-MATCH between
//     govpx and libvpx; divergence first appears on frame 2.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:276-289 ZBIN_EXTRA_*
//     macros (Y / UV / Y2 formulae)
//   - libvpx v1.16.0 vp8/encoder/vp8_quantize.c:410-428
//     vp8_update_zbin_extra (called from rdopt.c:1930,
//     encodeframe.c:1107, encodeframe.c:1243)
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:726-732 build_activity_map
//     gate
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1105-1108 intra adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/encodeframe.c:1191-1194 inter adjust_act_zbin
//   - libvpx v1.16.0 vp8/encoder/bitstream.c:865-950 vp8_update_coef_probs
//     (default-path 0 < s update rule)
//   - libvpx v1.16.0 vp8/common/treecoder.c:78-102
//     vp8_tree_probs_from_distribution (Pfactor=256 Round=1 fitting)
//   - libvpx v1.16.0 vp8/encoder/bitstream.c:669-676 prob_update_savings
//   - govpx internal/vp8/encoder/probability_tokens.go:174
//     coefficientProbabilityUpdatesFromTokenCounts
//   - govpx vp8_encoder_tuning.go:47-97 prepareTuningActivityMap +
//     ssimActivityMeasure
//   - govpx vp8_encoder_tuning.go:322-362 tunedZbinAdjustment
//   - govpx vp8_encoder_inter_quantize.go:38-86 quantizeBlockWithZbinAndActivity
//     (per-position ZBIN_EXTRA computation on line 64)
//
// Closed by task #201 (per-recode activity_map rebuild): the byte 58 frame 2
// coefficient probability divergence collapsed once govpx started rebuilding
// the per-MB activity_map inside every recode attempt instead of once before
// the recode loop, matching libvpx's vp8_encode_frame call cadence (the
// activity probe runs per call). With that fix the entire 6-frame clip is
// byte-MATCH for the same cohort, so this test now pins the closed state.
func TestVP8Frame2Byte58DivergenceParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the audit replay")
	}
	vpxencOracle := findVpxencOracle(t)

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

	govpxFrames, counts := encodeFramesWithGovpxCapturingFrame2Counts(t, opts, sources, 2)
	libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "task183-byte58-audit", opts, 700, sources, extraArgs)

	if len(govpxFrames) < 6 || len(libvpxFrames) < 6 {
		t.Fatalf("expected ≥6 frames; got govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}

	// Full byte-MATCH on every frame; before task #201 the frame 2 first
	// partition diverged at byte 58 (govpx=0x73 vs libvpx=0x7f) due to a
	// single UV coefficient (b=2 band=6 ctx=2 node=5) tipping across the
	// activity-adjusted ZBIN boundary. Fix: rebuild activity_map per recode.
	for i := 0; i < 6; i++ {
		if !bytesEqualForFrame2Parity(govpxFrames[i], libvpxFrames[i]) {
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
			t.Fatalf("frame %d byte-MATCH regressed after task #201 fix: govpx_len=%d libvpx_len=%d first_diff=%d",
				i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff)
		}
	}

	// Decode coefficient probabilities for frames 0..2 and require zero
	// divergent slots. Before task #201 there was exactly one delta at
	// (b=2, band=6, ctx=2, node=5) gov=156 lib=159, driven by the
	// (b=2,band=6,ctx=2) THREE/FOUR token distribution diverging by one
	// shifted coefficient.
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
						t.Fatalf("coef-prob delta after task #201 fix at b=%d band=%d ctx=%d node=%d gov=%d lib=%d",
							b, n, c, nd, govpxProbs[b][n][c][nd], libvpxProbs[b][n][c][nd])
					}
				}
			}
		}
	}

	if counts == nil {
		t.Fatalf("encoder did not capture frame 2 token counts")
	}
	// After the fix the (b=2,band=6,ctx=2) THREE/FOUR mix matches libvpx
	// (51/31) instead of the pre-fix 50/32. Pin the post-fix counts so a
	// future quantizer/activity refactor cannot silently re-balance them.
	wantTokenThree := 51
	wantTokenFour := 31
	if got := counts[2][6][2][tables.ThreeToken]; got != wantTokenThree {
		t.Fatalf("token count drift at (b=2,band=6,ctx=2) ThreeToken: got=%d want=%d", got, wantTokenThree)
	}
	if got := counts[2][6][2][tables.FourToken]; got != wantTokenFour {
		t.Fatalf("token count drift at (b=2,band=6,ctx=2) FourToken: got=%d want=%d", got, wantTokenFour)
	}
	t.Logf("task #183 closed by task #201: 6-frame byte MATCH; (b=2,band=6,ctx=2) Three=%d Four=%d",
		wantTokenThree, wantTokenFour)
}

// encodeFramesWithGovpxCapturingFrame2Counts encodes the supplied sources
// via govpx and, after encoding frame `captureFrame`, returns a copy of the
// encoder's per-frame coefficient token counts (the same accumulator
// InterFramePacket.PrebuiltCoefCounts consumes). Subsequent frames are
// still encoded so the returned [][]byte covers the full clip.
func encodeFramesWithGovpxCapturingFrame2Counts(t *testing.T, opts EncoderOptions, sources []Image, captureFrame int) ([][]byte, *vp8enc.InterCoefficientTokenCounts) {
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

func bytesEqualForFrame2Parity(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
