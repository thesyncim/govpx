package govpx

import (
	"testing"
)

// TestVP8Task307PickerSourceBufferAudit pins task #307's NEGATIVE finding for
// the BestARNR (-5 byte) / GoodARNR (-6 byte) ARNR audit pin-hold residual
// (see vp8_byte0_kf_1280x720_ssim_best_arnr_audit_test.go and
// vp8_byte0_kf_1280x720_ssim_good_arnr_audit_test.go).
//
// HYPOTHESIS (orthogonal to task #304's residual / predictor probe):
//
//	The govpx and libvpx RD pickers may read the MB(0,0) frame-1 NEWMV
//	source samples from DIFFERENT in-memory buffers. libvpx routes the
//	picker through `cpi->Source`, which the encode-driver loop in
//	vp8/encoder/onyx_if.c:4862-4903 redirects to `&cpi->alt_ref_buffer`
//	(the temporal-filter output) whenever
//	`oxcf.arnr_max_frames > 0 && source_alt_ref_pending`. govpx's
//	preprocessSource (encoder_preprocess.go:57-71) redirects through
//	`e.arnrScratch.Img` under the gate
//	`ARNRMaxFrames > 1 && lookaheadEnabled() && hiddenAltRefFrame`.
//	The two gates differ in their first conjunct (`> 0` vs `> 1`), so
//	for the BestARNR/GoodARNR cohort config `ARNRMaxFrames=1`, the
//	hypothesized failure mode is: libvpx reads the picker source from a
//	temporally filtered alt_ref_buffer while govpx reads from the raw
//	lookahead frame, producing a per-pixel source delta that propagates
//	through the residual into the NEWMV picker's all-zero Y qcoeff
//	(task #298).
//
// AUDIT RESULT: the hypothesis is INCORRECT. For the BestARNR / GoodARNR
// cohort the ARNR temporal filter does NOT fire on either side; both
// pickers read the raw lookahead source byte-identically at MB(0,0)
// frame 1. The two short-circuits below close the gate on each side:
//
//  1. libvpx onyx_if.c:4862 gate is
//     `cpi->oxcf.error_resilient_mode == 0 && cpi->oxcf.play_alternate &&
//     cpi->source_alt_ref_pending`. For a one-pass BestQuality / GoodQuality
//     encode with no `--lag-in-frames` on the vpxenc-oracle command line,
//     vp8/vp8_cx_iface.c:326-332 forces `oxcf->lag_in_frames = 0` and
//     `oxcf->allow_lag = 0`. `cpi->source_alt_ref_pending` is set ONLY when
//     two-pass stats schedule an alt-ref (firstpass.c:
//     define_gf_group, calc_arf_boost) — with `lag_in_frames=0` there is no
//     lookahead and `source_alt_ref_pending` never becomes 1. The gate
//     therefore never fires for these seeds; libvpx does not call
//     `vp8_temporal_filter_prepare_c`, and `force_src_buffer` stays NULL,
//     leaving `cpi->Source = &cpi->source->img` (the raw lookahead entry's
//     image). The picker reads the raw input frame.
//
//  2. govpx preprocessSource (encoder_preprocess.go:57-71) gate is
//     `hiddenAltRefFrame && ARNRMaxFrames > 1 && lookaheadEnabled()`. For
//     this cohort:
//     - `hiddenAltRefFrame` requires the `EncodeInvisibleFrame |
//     EncodeForceAltRefFrame` flag pair, which only fires when the
//     auto-alt-ref driver schedules a hidden ARF emission
//     (encoder_altref_driver.go), gated on
//     `e.opts.AutoAltRef && lookaheadEnabled() && !error_resilient`.
//     - `ARNRMaxFrames > 1` is false (cohort sets ARNRMaxFrames=1).
//     - `lookaheadEnabled()` requires `opts.LookaheadFrames > 0`; the
//     cohort EncoderOptions in the BestARNR/GoodARNR test never sets it,
//     defaulting to zero.
//     All three sub-gates fail; `preprocessSource` returns the raw
//     `source` unchanged, so the picker reads the raw input frame.
//
//  3. Even if both gates had fired with maxFrames=1, applyARNRFilter
//     (encoder_arnr.go:42-46) hard-rejects `maxFrames <= 1` before
//     writing the scratch, matching libvpx's `vp8_temporal_filter_iterate_c`
//     identity behavior at strength=1, mv=0, single frame
//     (vp8/encoder/temporal_filter.c:80-108: per-pixel `src_byte ==
//     pixel_value` ⇒ `modifier=0` ⇒ `count=32, accumulator=32*src` ⇒
//     `pval = (32*src + 16) * fixed_divide[32] >> 19 = (32*src + 16) *
//     16384 >> 19 = (32*src + 16) >> 5 = src` for src in [0,255]). The
//     filter is the identity for maxFrames=1 strength=1, so the alt-ref
//     buffer copy would equal the raw source even if the redirect fired.
//
// Cross-check at the picker entry point: at MB(0,0) frame 1 of the
// 1280x720 BestARNR/GoodARNR cohort, the source samples both sides read
// are produced by encoderValidationPanningFrame(1280, 720, 1) which
// emits `byte(32 + ((srcY*7 + srcX*11 + (srcX/8)*(srcY/8)*13) & 191))`
// for `(srcX, srcY) = (x+2, y+1)` over `(x, y) ∈ [0,16)×[0,16)` (frame
// index 1, xoff=2, yoff=1). The first row Y values are deterministic and
// reproduced below as a pin so any future change to the input wiring
// surfaces an explicit test break instead of a silent picker source
// drift.
//
// CONSTRAINT for task #304 (residual / predictor probe): the MB(0,0)
// frame 1 NEWMV picker source samples MATCH byte-for-byte between
// govpx and libvpx. The all-zero Y qcoeff divergence task #298 surfaced
// is NOT explained by a source-buffer routing skew; it must lie in
// the residual computation (predictor reference pixels, sub-pel filter,
// or the predicted-vs-source subtract) or the FDCT.
//
// References:
//   - libvpx v1.16.0 vp8/encoder/onyx_if.c:4862-4903 (ARNR src redirect)
//   - libvpx v1.16.0 vp8/encoder/temporal_filter.c:264-332 (alt_ref_buffer
//     write)
//   - libvpx v1.16.0 vp8/encoder/temporal_filter.c:348-433 (gate / blur
//     window selection)
//   - libvpx v1.16.0 vp8/vp8_cx_iface.c:326-332 (lag_in_frames forced to
//     zero for one-pass)
//   - govpx encoder_preprocess.go:57-71 (mirror gate)
//   - govpx encoder_arnr.go:42-75 (maxFrames<=1 short-circuit)
//   - govpx encoder_lookahead.go:47-49 (lookaheadEnabled gate)
func TestVP8Task307PickerSourceBufferAudit(t *testing.T) {
	t.Run("BestARNRCohortGateClosed", testTask307BestARNRCohortGateClosed)
	t.Run("GoodARNRCohortGateClosed", testTask307GoodARNRCohortGateClosed)
	t.Run("PickerSourceSamplesAtMBOriginFrame1", testTask307PickerSourceSamplesAtMBOriginFrame1)
	t.Run("ApplyARNRFilterRejectsMaxFramesOne", testTask307ApplyARNRFilterRejectsMaxFramesOne)
}

func testTask307BestARNRCohortGateClosed(t *testing.T) {
	// Cohort config from vp8_byte0_kf_1280x720_ssim_best_arnr_audit_test.go.
	// LookaheadFrames is unset (zero) so lookaheadEnabled() returns false.
	opts := EncoderOptions{
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
		Threads:           4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	if e.lookaheadEnabled() {
		t.Fatalf("expected lookaheadEnabled=false for ARNRMaxFrames=1 cohort with LookaheadFrames=0; got true")
	}
	if e.opts.ARNRMaxFrames > 1 {
		t.Fatalf("expected ARNRMaxFrames<=1, got %d", e.opts.ARNRMaxFrames)
	}
	// The preprocessSource gate at encoder_preprocess.go:59 is
	// `hiddenAltRefFrame && ARNRMaxFrames > 1 && lookaheadEnabled()`.
	// All three sub-gates must be false for picker source = raw source.
	src := sourceImageFromImage(encoderValidationPanningFrame(1280, 720, 1))
	meta := encodeSourceMetadata{lookaheadDepth: 0, internalInvisible: false}
	out, outMeta := e.preprocessSource(src, EncodeFlags(0), meta)
	if outMeta.arnrFiltered {
		t.Fatalf("expected arnrFiltered=false; got true")
	}
	// The returned pointer-equivalent source must be byte-identical to
	// the input source on the first row of MB(0,0).
	for x := range 16 {
		if out.Y[x] != src.Y[x] {
			t.Fatalf("picker source diverges at (x=%d,y=0): preprocessed=%d raw=%d", x, out.Y[x], src.Y[x])
		}
	}
}

func testTask307GoodARNRCohortGateClosed(t *testing.T) {
	// Identical cohort except Deadline=GoodQuality (matches
	// vp8_byte0_kf_1280x720_ssim_good_arnr_audit_test.go).
	opts := EncoderOptions{
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
		Threads:           4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	if e.lookaheadEnabled() {
		t.Fatalf("expected lookaheadEnabled=false for GoodARNR cohort; got true")
	}
	src := sourceImageFromImage(encoderValidationPanningFrame(1280, 720, 1))
	meta := encodeSourceMetadata{lookaheadDepth: 0, internalInvisible: false}
	out, outMeta := e.preprocessSource(src, EncodeFlags(0), meta)
	if outMeta.arnrFiltered {
		t.Fatalf("expected arnrFiltered=false; got true")
	}
	for x := range 16 {
		if out.Y[x] != src.Y[x] {
			t.Fatalf("picker source diverges at (x=%d,y=0): preprocessed=%d raw=%d", x, out.Y[x], src.Y[x])
		}
	}
}

func testTask307PickerSourceSamplesAtMBOriginFrame1(t *testing.T) {
	// Pin the first row of MB(0,0) frame 1 source Y samples that both
	// govpx and libvpx pickers consume. Formula reproduced from
	// encoderValidationPanningFrame(1280, 720, 1) with xoff=2, yoff=1.
	//
	// Values for (srcX, srcY) = (x+2, 0+1) for x in [0..15):
	//   y=0: ((1*7 + (x+2)*11 + ((x+2)/8)*0*13) & 191) + 32
	//      = ((7 + 11*x + 22) & 191) + 32 = ((11*x + 29) & 191) + 32
	want := [16]byte{
		61, 72, 83, 94, 105, 116, 127, 138,
		149, 160, 171, 182, 193, 204, 215, 32, // (11*15+29)&191 = 194; (11*15+29)=194; 194&191=192? recompute below
	}
	// recompute precisely
	for x := range 16 {
		srcX := x + 2
		srcY := 1
		v := byte(32 + ((srcY*7 + srcX*11 + (srcX/8)*(srcY/8)*13) & 191))
		want[x] = v
	}
	src := encoderValidationPanningFrame(1280, 720, 1)
	for x := range 16 {
		if src.Y[x] != want[x] {
			t.Fatalf("picker source pin at (x=%d,y=0): got=%d want=%d", x, src.Y[x], want[x])
		}
	}
	// Verify the full 16x16 MB(0,0) block matches the formula. This is the
	// exact set of Y samples both pickers MUST read at NEWMV MB(0,0).
	for y := range 16 {
		for x := range 16 {
			srcX := x + 2
			srcY := y + 1
			v := byte(32 + ((srcY*7 + srcX*11 + (srcX/8)*(srcY/8)*13) & 191))
			if got := src.Y[y*src.YStride+x]; got != v {
				t.Fatalf("picker source diverges from panning formula at (x=%d,y=%d): got=%d want=%d", x, y, got, v)
			}
		}
	}
}

func testTask307ApplyARNRFilterRejectsMaxFramesOne(t *testing.T) {
	// Verify applyARNRFilter short-circuits at maxFrames<=1 even if the
	// preprocessSource gate were bypassed. This pins the second line of
	// defense against a hypothetical source-buffer drift on this cohort.
	opts := EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		TargetBitrateKbps: 800,
		LookaheadFrames:   4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}
	e, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	plane := make([]byte, 64*64)
	for i := range plane {
		plane[i] = byte(i & 0xff)
	}
	ok := e.applyARNRFilter(syntheticSource(64, 64, plane), 0)
	if ok {
		t.Fatalf("applyARNRFilter unexpectedly succeeded with ARNRMaxFrames=1")
	}
}
