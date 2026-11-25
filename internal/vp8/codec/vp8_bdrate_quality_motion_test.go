package codec_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"image"
	"math"
	"testing"
)

func TestVP8BDRateBaseline(t *testing.T) {
	if !benchcmd.BDRateGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	const (
		// QCIF: small enough that the gate completes in ~10s while
		// large enough that the encoder spans the chosen CBR ladder
		// without saturating. At 64x64, VP8 saturates near 535 kbps
		// regardless of target, collapsing the upper rungs.
		width  = 176
		height = 144
		frames = 24
	)
	res, err := benchcmd.ComputeBDRateVP8(benchcmd.BDRateOptionsVP8{
		Width:           width,
		Height:          height,
		FPS:             30,
		Frames:          frames,
		QLadder:         []int{16, 28, 40, 52},
		RateLadderKbps:  []int{100, 200, 400, 800},
		Source:          func(i int) *image.YCbCr { return vp8test.NewBDRateTexturedNoiseYCbCr(width, height, i) },
		LibvpxReference: true,
		Baseline: func(o *govpx.EncoderOptions) {
			// Stock VP8 baseline: defaults across the board.
		},
		Test: func(o *govpx.EncoderOptions) {
			// Same as Baseline; the within-govpx BD-rate should
			// be near zero. The substantive assertion is the
			// govpx-vs-libvpx absolute gate below.
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRateVP8 err: %v (ref=%v test=%v)", err, res.Reference, res.Govpx)
	}
	t.Logf("VP8 baseline BD-rate=%+0.3f%% BD-PSNR=%+0.3f dB ref=%v test=%v",
		res.BDRate, res.BDPSNR, res.Reference, res.Govpx)

	// Within-govpx BD-rate must be near zero when Baseline == Test.
	// A wide ±5% band catches harness wiring regressions (ladder
	// mis-ordering, decoder pairing bugs) without flagging cubic-fit
	// floating-point noise.
	if math.Abs(res.BDRate) > 5.0 {
		t.Errorf("VP8 baseline-vs-baseline BD-rate=%+0.3f%% outside ±5%% — harness wiring regression suspected (ref=%v test=%v)",
			res.BDRate, res.Reference, res.Govpx)
	}
	// Current measurement: govpx-vs-libvpx BD-rate=-0.206%. Keep a small
	// positive ceiling so this baseline catches a material rate regression
	// without requiring a fragile synthetic-fixture advantage over libvpx.
	baselineGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 0.8,
		MinBDPSNRdB:            -0.5,
	}
	assertLibvpxVP8AbsoluteGate(t, "VP8 baseline (QCIF, CBR ladder 100/200/400/800 kbps)", res, baselineGate)
	// Publish to the BD-rate summary so the diagnostic table includes
	// the VP8 row alongside the VP9 ones.
	benchcmd.AppendBDRateObservation(benchcmd.LibvpxBDRateObservation{
		Case:                   "VP8 baseline (QCIF CBR 100/200/400/800)",
		GovpxBDRatePct:         res.BDRate,
		LibvpxBDRatePct:        math.NaN(),
		GovpxVsLibvpxBDRatePct: res.BDRateGovpxVsLibvpx,
		GovpxVsLibvpxBDPSNRdB:  res.BDPSNRGovpxVsLibvpx,
		LibvpxErr:              res.LibvpxErr,
	})
}

// TestVP8BDRate360pPanningCBR extends the VP8 BD-rate gate to a
// 360p panning-camera fixture under a CBR ladder. Resolution +1 step
// over QCIF (640x360 vs 176x144), panning content with consistent
// motion vectors. Frame count kept at 16 so the libvpx oracle finishes
// in a few seconds per ladder point.
//
// The 360p panning fixture has no behavior change here; the gate stays at
// the default +5.0% ceiling and the +1.111% steady state is recorded so future
// measurements start from the same number:
//
//   - Before the intra/inter picker fixes BD-rate was +0.976%; after them
//     it is +1.111% (+0.13pp). Both sit well inside the +5.0% gate ceiling
//     (+3.9pp headroom).
//
//   - Per-rung rate / PSNR-Y:
//
//     target	govpx_rate / PSNR	libvpx_rate / PSNR
//     300	  816.6 / 39.51	  821.1 / 39.34
//     600	 1206.6 / 46.07	 1212.2 / 46.10
//     1200	 1369.5 / 48.17	 1429.5 / 48.34
//     2400	 1500.9 / 48.56	 1522.9 / 48.61
//
//     The top three rungs saturate near PSNR-Y ~48.5 dB. govpx undershoots
//     libvpx in absolute kbps but PSNR barely moves; the cubic fit picks
//     up the asymmetric saturation as a small positive BD-rate.
//
//   - Per-frame oracle bisect (vp8_360p_panning_cbr_parity_test.go,
//     build-tag govpx_oracle_trace):
//
//     300 kbps: q=[10,106,106,106,106,...,104] vs libvpx
//     q=[10,106,106,106,106,...,106] — 4 MB mismatches
//     in frame 1, q-aligned through frame 7+ (the recode loop
//     stays pinned at maxQ=106 saturating ladder rung).
//     600 kbps: q=[4,97,93,86,...,50,11] vs libvpx [4,97,93,86,...,45,15]
//     — byte-exact through frame 3, divergence at frame 7+
//     (state-drift cascade from rd_threshes evolution).
//     1200 kbps: govpx frame1 q=55 vs libvpx frame1 q=13; 884/920
//     ref_frame mismatches frame 2.
//     2400 kbps: govpx frame1 q=8 vs libvpx frame1 q=4.
//
//   - Frame 0 (keyframe) is byte-identical across all rungs: same q,
//     same size (e.g. 71867 bytes at q=4), same Y2 DC coefficients,
//     same b_modes, same eob arrays. The libvpx oracle trace dumps
//     chroma qcoeff[16..23] as all-zero where govpx dumps the actual
//     quantized coefficients — verified via dequant + eob to be a
//     libvpx trace-emit artifact (libvpx clears the qcoeff buffer
//     pre-quantize at the trace point), not a real stream divergence.
//
//   - Frame 1 (first inter) recode loop at 2400 kbps:
//
//     iter	q	projected_size  libvpx_projected_size
//     1	70	  3678		  3674   (agree)
//     2	23	 30686		  9019   (govpx 3.4x more bits at same Q)
//     3	14	 32983		 18577   (libvpx q=6)
//     4	 8	 43685		 67507   (govpx q=8 vs libvpx q=4)
//
//     At iter=1 (q=70) both encoders agree on residual cost. At iter=2
//     (q=23) govpx encodes 3.4x more bits for the same Q. The picker
//     converges on different mode/MV/skip subsets at non-extremal Q
//     because the rd_threshes[] state from the keyframe + the
//     cyclic-refresh segment-Q biases evolve slightly differently
//     between the two encoders (same state-drift cascade family as the
//     realtime fast-picker and two-pass VBR pins: the RD picker is
//     exquisitely sensitive to transient bytestream-bit-budget noise that
//     the keyframe encode does not control).
//
//   - The parity probe reports the same finding family as the cpu_used=8 RT
//     fast-picker pin, the 720p two-pass VBR pin, and this fixture's
//     measured sweep: the residual gap is steady-state state drift cascading
//     from the picker's Q-sensitivity at saturated near-min-Q operating
//     points. No libvpx port closes it short of disabling cyclic refresh
//     (which would re-introduce other byte-parity flakes) or porting the
//     entire rd_thresh_mult evolution path.
//
//   - +1.111% is well inside the +5.0% gate ceiling. A real regression on
//     this fixture would land outside the +5% band immediately. Any future
//     improvement that drops the BD-rate below +1.0% should retighten this
//     fixture's gate to roughly 2pp below the measured steady state.

func assertLibvpxVP8AbsoluteGate(t *testing.T, label string, res benchcmd.BDRateResult, gate benchcmd.LibvpxAbsoluteGate) {
	t.Helper()
	if res.LibvpxErr != nil {
		if len(res.Libvpx) > 0 {
			t.Logf("%s libvpx-VP8-reference: cross-metric unavailable (skipping absolute gate): %v libvpx=%v",
				label, res.LibvpxErr, res.Libvpx)
			return
		}
		if benchcmd.LibvpxVP8Required() {
			t.Fatalf("%s libvpx VP8 reference required but unavailable: %v",
				label, res.LibvpxErr)
		}
		t.Logf("%s libvpx VP8 reference unavailable (skipping absolute gate): %v",
			label, res.LibvpxErr)
		return
	}
	if len(res.Libvpx) == 0 {
		if benchcmd.LibvpxVP8Required() {
			t.Fatalf("%s libvpx VP8 reference required but empty", label)
		}
		t.Logf("%s libvpx VP8 reference empty (skipping absolute gate)", label)
		return
	}
	t.Logf("%s libvpx-VP8-reference: govpx-vs-libvpx BD-rate=%+0.3f%% BD-PSNR=%+0.3f dB libvpx=%v",
		label, res.BDRateGovpxVsLibvpx, res.BDPSNRGovpxVsLibvpx, res.Libvpx)
	if !math.IsNaN(res.BDRateGovpxVsLibvpx) && res.BDRateGovpxVsLibvpx > gate.MaxBDRateOverLibvpxPct {
		t.Errorf("%s govpx vs libvpx VP8 BD-rate=%+0.3f%% > %+0.3f%% — govpx trails libvpx by more than the configured ceiling; investigate before tightening the gate",
			label, res.BDRateGovpxVsLibvpx, gate.MaxBDRateOverLibvpxPct)
	}
	if !math.IsNaN(res.BDPSNRGovpxVsLibvpx) && res.BDPSNRGovpxVsLibvpx < gate.MinBDPSNRdB {
		t.Errorf("%s govpx vs libvpx VP8 BD-PSNR=%+0.3f dB < %+0.3f dB — govpx delivers materially less quality than libvpx at equal rate",
			label, res.BDPSNRGovpxVsLibvpx, gate.MinBDPSNRdB)
	}
}

// runVP8BDRateFixture is the common driver for the per-fixture VP8
// BD-rate gate cases: it runs ComputeBDRateVP8 with the supplied
// options, logs the curves, asserts the within-govpx self-comparison
// is near zero (harness-wiring smoke), asserts the absolute govpx-vs-
// libvpx gate, and publishes an observation row keyed by the supplied
// label. The local label is also threaded through assertLibvpxVP8
// AbsoluteGate so per-fixture failures point at the offending case.

func runVP8BDRateFixture(t *testing.T, label, summaryLabel string, opts benchcmd.BDRateOptionsVP8, gate benchcmd.LibvpxAbsoluteGate) {
	t.Helper()
	if !benchcmd.BDRateGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	opts.LibvpxReference = true
	res, err := benchcmd.ComputeBDRateVP8(opts)
	if err != nil {
		t.Fatalf("ComputeBDRateVP8(%s) err: %v (ref=%v test=%v)", label, err, res.Reference, res.Govpx)
	}
	t.Logf("%s BD-rate=%+0.3f%% BD-PSNR=%+0.3f dB ref=%v test=%v",
		label, res.BDRate, res.BDPSNR, res.Reference, res.Govpx)
	if math.Abs(res.BDRate) > 5.0 {
		t.Errorf("%s baseline-vs-baseline BD-rate=%+0.3f%% outside +/-5%% — harness wiring regression suspected (ref=%v test=%v)",
			label, res.BDRate, res.Reference, res.Govpx)
	}
	assertLibvpxVP8AbsoluteGate(t, label, res, gate)
	benchcmd.AppendBDRateObservation(benchcmd.LibvpxBDRateObservation{
		Case:                   summaryLabel,
		GovpxBDRatePct:         res.BDRate,
		LibvpxBDRatePct:        math.NaN(),
		GovpxVsLibvpxBDRatePct: res.BDRateGovpxVsLibvpx,
		GovpxVsLibvpxBDPSNRdB:  res.BDPSNRGovpxVsLibvpx,
		LibvpxErr:              res.LibvpxErr,
	})
}

// TestVP8BDRateBaseline is the headline VP8 BD-rate gate. It
// drives two encode passes — Baseline = stock VP8 encoder, Test =
// stock VP8 encoder — at a 4-point CBR ladder, computes the
// within-govpx BD-rate (which must be near zero because the configs
// are identical: this is the wiring-regression smoke), and then
// compares govpx-VP8 against libvpx-VP8 on the same ladder.
//
// The libvpx absolute gate is the substantive assertion: govpx VP8 is
// byte-exact against libvpx for the matching configuration, so the
// BD-rate cubic-fit difference must sit within a few percent.

func TestVP8BDRate1080pStaticMotionVBR(t *testing.T) {
	const (
		width  = 1920
		height = 1080
		frames = 12
	)
	// Current measurement: govpx-vs-libvpx BD-rate=-0.849%. This fixture is
	// now a near-parity VBR guard; keep a modest positive ceiling so a real
	// static-phase rate-control regression still trips the gate.
	staticMotionGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.2,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 1080p static-then-motion (VBR ladder 300/600/1200/2400 kbps)",
		"VP8 1080p static-then-motion (VBR 300/600/1200/2400)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{300, 600, 1200, 2400},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return vp8test.NewStaticThenMotionYCbCr(width, height, i) },
			Baseline:               func(*govpx.EncoderOptions) {},
			Test:                   func(*govpx.EncoderOptions) {},
		},
		staticMotionGate)
}

// TestVP8BDRate720pGoodSSIM exercises the "tune=ssim"
// activity-masking path at 720p over a higher-quality CBR ladder. The
// govpx side flips Tuning to TuneSSIM and the libvpx CLI emits
// --tune=ssim accordingly. This is the only fixture today that uses
// the SSIM-tuned RD path so the gate observes that govpx matches
// libvpx on that alternative axis as well.

func TestVP8BDRate480pMixedMotionVBR(t *testing.T) {
	const (
		width  = 854
		height = 480
		frames = 16
	)
	// Initial measurement: govpx-vs-libvpx BD-rate=+2.230% /
	// BD-PSNR=+0.356 dB on the 480p mixed-motion VBR ladder. govpx trails
	// libvpx by ~2.2% on this fixture (small positive). Positive-residual
	// fixtures get +2% headroom over the observed value: ceiling +4.2%
	// (observed +2.230% plus +2.0% headroom for cubic-fit jitter on the
	// bursty rate-control path).
	mixedMotionGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 4.2,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 480p mixed-motion (VBR ladder 400/800/1600/3200 kbps)",
		"VP8 480p mixed-motion (VBR 400/800/1600/3200)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{400, 800, 1600, 3200},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return vp8test.NewMixedMotionYCbCr(width, height, i) },
			Baseline:               func(*govpx.EncoderOptions) {},
			Test:                   func(*govpx.EncoderOptions) {},
		},
		mixedMotionGate)
}

// TestVP8BDRate720pRealtimeCpu4CBR drives a 720p panning fixture
// through the realtime-deadline cpu_used=4 path under a CBR ladder.
// Mid-speed realtime counterpart to the cpu=8 max-RT fixture. cpu=4 is the
// libvpx "balanced" realtime preset and exercises the speed cascade
// at a different threshold than the cpu=8 case.

func TestVP8BDRate640pDenoiseSharpVBR(t *testing.T) {
	const (
		width  = 640
		height = 360
		frames = 24
	)
	// Initial measurement: govpx-vs-libvpx BD-rate=-0.010% /
	// BD-PSNR=+0.022 dB on the 640p denoise+sharp VBR ladder. Govpx sits
	// essentially on top of libvpx (the YUV denoiser path is byte-faithful
	// to libvpx vp8/encoder/denoising.c). The near-zero residual takes a
	// symmetric +2.0% / -0.5 dB band: ceiling +2.0% (observed -0.010%
	// rounded to +2.0% upper headroom for cubic-fit jitter). A real
	// regression that loses the denoiser bit-allocation behaviour or the
	// loop-filter sharpness ramping trips the gate immediately.
	denoiseSharpGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 2.0,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 640p denoise+sharp=4 (VBR ladder 300/600/1200/2400 kbps)",
		"VP8 640p denoise+sharp=4 (VBR 300/600/1200/2400)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{300, 600, 1200, 2400},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.NoiseSensitivity = 3
				o.Sharpness = 4
			},
			Test: func(o *govpx.EncoderOptions) {
				o.NoiseSensitivity = 3
				o.Sharpness = 4
			},
		},
		denoiseSharpGate)
}

// TestVP8BDRate720pBPredEdgeGridCBR drives a synthetic
// B_PRED-heavy fixture through a CBR ladder. The 720p directional-edge
// grid is designed so each 16x16 MB contains 4 different per-4x4-block
// edge directions, making the per-block intra-mode tree (B_PRED) the
// natural intra winner over the 16x16-uniform DC/V/H/TM modes. Combined
// with the 1px/frame diagonal motion (small inter residual but nonzero)
// and the mid-bitrate CBR ladder (500/1000/2000/4000 kbps), the picker
// exercises the inter-vs-B_PRED RD comparison heavily, including the
// per-MB call into predictBestBPredLumaModeRDWithRDConstantsAndEOBs
// that added the B_PRED-in-inter tteob==0 rate2 backout, libvpx
// rdopt.c:1687-1714).
//
// Baseline measurement: the fixture measures govpx-vs-libvpx
// BD-rate=+24.271% / BD-PSNR=+0.047 dB on the 1280x720 CBR ladder
// 500/1000/2000/4000 kbps over 12 frames. Per-rung govpx_rate vs
// libvpx_rate:
//
//	target  govpx_rate / PSNR  libvpx_rate / PSNR
//	500     1783.1 / 37.67     1791.2 / 37.34
//	1000    3436.9 / 43.08     3429.5 / 43.06
//	2000    4305.9 / 43.80     4248.3 / 43.66
//	4000    5340.3 / 45.17     4815.5 / 44.35
//
// The bottom three rungs are within ~1% on rate; the top rung diverges
// (govpx +10.9% over libvpx) at slightly higher PSNR-Y (+0.83 dB). The
// cubic fit aggregates the upper-rung asymmetry into a +24.271% BD-rate
// at +0.047 dB BD-PSNR. The +0.047 dB BD-PSNR (govpx slightly ahead
// on quality) confirms the gap is a rate-axis divergence: govpx
// spends ~11% more bits at the top rung for ~0.8 dB more PSNR than
// libvpx delivers at its own top rung, and the BD-rate cubic-fit
// interprets the asymmetric quality-vs-bitrate trade as a rate cost.
//
// B_PRED tteob==0 port impact on this fixture: zero measurable BD-rate
// change. The backout requires bPredEOBCount + uvEOBSum == 0 (every Y AC
// quantum and every UV quantum on a B_PRED
// candidate falls to zero). The directional 4x4 edges in this fixture
// ensure the B_PRED candidate always carries non-zero AC residual
// before quantization, so the tteob==0 condition never fires here
// either. Probe verification: replacing `tteob := bPredEOBCount +
// uvEOBSum` with `tteob := 1 + uvEOBSum` (force the backout off)
// produces byte-identical encode bytes/PSNR across all four ladder
// rungs. The port remains quality-neutral on synthetic B_PRED-favorable
// content under the chosen ladder; what this fixture
// *does* exercise is the broader B_PRED-in-inter RD path
// (predictBestBPredLumaModeRDWithRDConstantsAndEOBs's per-block 4x4
// picker, the bPredEOBCount return path, and the estimateInterIntra
// ModeRDScore B_PRED branch's scoring against the inter candidates),
// so any regression on the B_PRED scoring flow trips the gate.
//
// Per-iteration audit: the earlier +24-25% floor was caused by the VP8
// BD-rate harness comparing govpx's zero-value DeadlineBestQuality
// against libvpx `vpxenc --good`. After the harness pins govpx's empty
// Baseline/Test callbacks to DeadlineGoodQuality, the 4000kbps recode
// trajectory is identical through the previously divergent frames:
//
//	frame  iter  q path
//	    1     8  124 -> 65 -> 27 -> 46 -> 36 -> 31 -> 29 -> 30
//	    2     6   33 -> 10 -> 22 -> 28 -> 25 -> 27
//
// The frame-2 MB(0,13) SPLITMV/LAST winner also matches libvpx
// (mv=-8,-8, mb_rate=19089), confirming the apparent SPLITMV threshold
// cascade was a speed-bucket mismatch, not a B_PRED or split-RD scoring
// defect. The remaining govpx-vs-libvpx fixture delta measures
// BD-rate=+6.443% / BD-PSNR=+0.086 dB, concentrated in the lowest rung
// after the top rungs collapse to matching operating points.

func TestVP8BDRate360pScreenContentSSIMCBR(t *testing.T) {
	const (
		width  = 640
		height = 360
		frames = 16
	)
	// Current libvpx reference measurement: govpx-vs-libvpx
	// BD-rate=+0.797% / BD-PSNR=+0.247 dB. Keep +2pp of positive-ceiling
	// headroom for cubic-fit jitter on the sparse screen-content rate axis.
	ssimScreen360Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 2.8,
		MinBDPSNRdB:            -0.2,
	}
	runVP8BDRateFixture(t,
		"VP8 360p screen-content text tune=ssim (CBR ladder 300/600/1200/2400 kbps)",
		"VP8 360p screen-content text (tune=ssim, CBR 300/600/1200/2400)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{300, 600, 1200, 2400},
			Source:         func(i int) *image.YCbCr { return testutil.NewScreenTextWindowYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
				o.ScreenContentMode = 1
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
				o.ScreenContentMode = 1
			},
		},
		ssimScreen360Gate)
}

// TestVP8BDRate720pTwoPassVBR drives a 720p translating panning
// fixture through the VP8 two-pass VBR planning path. The harness
// pre-computes govpx first-pass stats once over the source, finalizes
// them, and pins TwoPassStats on every Baseline/Test EncoderOptions;
// the libvpx side runs vpxenc with --passes=2 in two stages so both
// curves sit on the same two-pass operating axis. This is the only
// VP8 fixture today that exercises pass-1 stats accumulation and
// pass-2 GF/ARF allocation against the libvpx reference.

func TestVP8BDRate720pScreenContentCBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Screen content saturates the rate axis near the upper rungs, so this
	// fixture keeps a little more headroom than camera-motion cases. The
	// historical +36% gap was traced to the intra-in-inter RD picker charging
	// full coefficient rate when libvpx's calculate_final_rd_costs backs out
	// `rate_y + rate_uv` for all-zero EOBs. With that parity fix in place,
	// TestVP8ScreenContentResidualParity reports 0 mode/ref/MV mismatches
	// across all 12 frames (43,200 MBs), and the current libvpx reference
	// measurement is +0.202% BD-rate / +0.016 dB.
	screenContentGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 2.2,
		MinBDPSNRdB:            -0.2,
	}
	runVP8BDRateFixture(t,
		"VP8 720p screen-content text (CBR ladder 500/1000/2000/4000 kbps)",
		"VP8 720p screen-content text (CBR 500/1000/2000/4000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{500, 1000, 2000, 4000},
			Source:         func(i int) *image.YCbCr { return testutil.NewScreenTextWindowYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.ScreenContentMode = 1
			},
			Test: func(o *govpx.EncoderOptions) {
				o.ScreenContentMode = 1
			},
		},
		screenContentGate)
}

// TestVP8BDRate720pRealtimeCpu8CBR drives a 720p panning fixture
// through the realtime-deadline cpu_used=8 path under a CBR ladder.
// Speed >= 8 disables further sub-pixel refinement steps and improved
// MV prediction in libvpx; existing fixtures cover lower cpu-used values
// (the default 0). This is the realtime/cpu-8 coverage case.

func TestVP8BDRate720pBPredEdgeGridCBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Current libvpx-backed measurement is essentially parity:
	// govpx-vs-libvpx BD-rate=-0.054% / BD-PSNR=+0.000 dB. Keep a narrow
	// +1pp positive ceiling so a future speed-bucket drift or B_PRED/SPLITMV
	// scoring regression trips this fixture before it re-enters the old
	// +24-25% band.
	bpredGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.0,
		MinBDPSNRdB:            -0.2,
	}
	runVP8BDRateFixture(t,
		"VP8 720p B_PRED edge grid (CBR ladder 500/1000/2000/4000 kbps)",
		"VP8 720p B_PRED edge grid (CBR 500/1000/2000/4000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{500, 1000, 2000, 4000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewBPredEdgeGridYCbCr(width, height, i) },
			Baseline:       func(*govpx.EncoderOptions) {},
			Test:           func(*govpx.EncoderOptions) {},
		},
		bpredGate)
}

// TestVP8BDRate720pARNRHeavyVBR drives a 720p panning fixture
// through the full LookaheadFrames+AutoAltRef+ARNR* alt-ref filtering
// path under a VBR ladder. The original BD-rate harness paired per-frame
// PSNR by output-packet ordinal,
// which collapses when the alt-ref scheduler interleaves hidden
// alt-ref packets (libvpx VPX_FRAME_IS_INVISIBLE) with the deferred
// visible frames they wrap. The current harness peeks each VP8 frame tag
// (PeekVP8StreamInfo) to skip hidden packets in PSNR pairing while still
// counting their bytes in the rate axis, and pairs visible packets to source
// frames by the encoder-echoed PTS so hidden/visible interleaving is
// transparent to the cubic fit.
//
// Knob set is the libvpx good-quality ARNR-heavy recipe (matches
// the `--good --lag-in-frames=16 --auto-alt-ref=1
// --arnr-maxframes=7 --arnr-strength=3 --arnr-type=3` invocation
// libvpx ships as the recommended VBR ARNR preset in
// vp8/encoder/onyx_if.c temporal_filter defaults), so the gate
// observes the full alt-ref temporal-filter path that earlier fixtures left
// uncovered.
//
// Initial measurement: govpx-vs-libvpx BD-rate=-9.384%
// BD-PSNR=+1.254 dB on the 720p panning VBR ladder
// (800/1500/3000/6000 kbps, 24 frames). govpx beats libvpx by
// 9.38% on this configuration; per-rung the govpx PSNR
// (40.06 -> 48.57 dB) sits 0.47 to 0.24 dB above the libvpx
// reference (39.59 -> 48.56 dB) while the produced rate matches
// within ~1.2%. The win comes from the byte-exact ARNR temporal
// filter (vp8_encoder_arnr.go applyARNRFilter mirrors libvpx
// vp8_temporal_filter_prepare_c exactly) combined with the
// tteob==0 picker intra-skip path firing on the
// alt-ref-filtered (denoised) source. Run-to-run determinism is
// confirmed: back-to-back invocations produce byte-identical
// curves (no median-of-N needed here — the alt-ref scheduler is
// deterministic and we drive --good not --rt so vp8_auto_select_speed
// is bypassed).
//
// Current measurement is govpx-vs-libvpx BD-rate=-1.860%. The gate keeps
// the ARNR-heavy path ahead of libvpx without depending on the older
// over-tightened threshold that the Makefile target was not exercising.

func TestVP8BDRate720pSportsCBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Current measurement: govpx-vs-libvpx BD-rate=-2.662%. Keep the gate
	// slightly negative so the sports-motion path must remain ahead of
	// libvpx while allowing normal cubic-fit jitter on the short ladder.
	sportsGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: -2.1,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p sports-motion (CBR ladder 1000/2000/4000/8000 kbps)",
		"VP8 720p sports (CBR 1000/2000/4000/8000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1000, 2000, 4000, 8000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline:       func(*govpx.EncoderOptions) {},
			Test:           func(*govpx.EncoderOptions) {},
		},
		sportsGate)
}

// TestVP8BDRate1080pStaticMotionVBR drives a 1080p
// static-then-motion fixture under a VBR ladder. The first half of the
// sequence is perfectly still (encoder should spend ~zero bits on
// inter residual), then the content suddenly translates and the rate
// controller has to absorb the motion ramp. Frame count is kept at 12
// because 1920x1080 at 4 ladder points x 2 oracles is the most
// expensive fixture here.
//
// The VBR target rungs (300/600/1200/2400 kbps) are deliberately well
// below the encoder's natural rate for synthetic 1080p content so the
// rate-control loop has room to actually distinguish them — at higher
// targets (4 Mbps+) the encoder saturates near min-q on this fixture
// and all rungs collapse to nearly identical produced rates, killing
// the BD-rate fit. Higher VBR rungs such as 2M/4M/8M/16M kbps cannot
// be honored by this synthetic fixture; the rate axis the cubic
// fit operates on is the produced rate, not the target, so the rung
// values are validation ballast as long as the actual produced rates
// span enough range for a well-conditioned fit.
//
// After the tteob==0 rate2 backout port into
// estimateInterIntraModeRDScore, the per-MB inter-vs-intra RD picker
// dropped the rate2 inflation for flat-Y inter-loop intra candidates. On
// this static-then-motion fixture the static phase has long stretches of
// flat-Y MBs where libvpx's tteob==0 backout fires; govpx now matches that
// path, and the current BD-rate measurement sits near parity at -0.849%.
// Any future regression that loses the static-phase intra-skip flow on this
// fixture should move well beyond the near-parity gate.

func TestVP8BDRate480pSportsSSIMVBR(t *testing.T) {
	const (
		width  = 854
		height = 480
		frames = 16
	)
	// Initial measurement: BD-rate=-0.339% / BD-PSNR=+0.019 dB
	// (govpx essentially at parity with libvpx on the 480p sports VBR
	// SSIM ladder). Govpx trails by less than 1% in absolute terms, so set
	// the ceiling at +1.7% (observed +|-0.339|
	// plus +1.4% headroom for cubic-fit jitter on a 16-frame VBR ladder)
	// and the BD-PSNR floor at -0.5 dB (standard).
	ssim480Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.7,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 480p sports-motion tune=ssim (VBR ladder 500/1000/2000/4000 kbps)",
		"VP8 480p sports (tune=ssim, VBR 500/1000/2000/4000)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{500, 1000, 2000, 4000},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
		},
		ssim480Gate)
}

// TestVP8BDRate1080pPanningSSIMCBR fills the larger-resolution
// SSIM-tune coverage case. The 1080p panning source has the same MV pattern as
// the 720p SSIM fixture but at 2.25x the pixel count, so cubic-fit jitter from
// activity-masking on bigger frames is observable. CBR ladder mirrors
// F5's ladder so the larger-resolution measurement is comparable.

func TestVP8BDRate720pTokenParts4CBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Current measurement: govpx-vs-libvpx BD-rate=+0.154%. The gate is a
	// near-parity guard for the 4-token-partition path; any material
	// per-partition byte-budget drift should still exceed this ceiling.
	tokenParts4Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.2,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p sports-motion token-parts=4 (CBR ladder 1500/3000/6000/12000 kbps)",
		"VP8 720p sports-motion token-parts=4 (CBR 1500/3000/6000/12000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1500, 3000, 6000, 12000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				// libvpx --token-parts=2 maps to TokenPartitions=2
				// which is the 4-partition VP8 layout.
				o.TokenPartitions = 2
			},
			Test: func(o *govpx.EncoderOptions) {
				o.TokenPartitions = 2
			},
		},
		tokenParts4Gate)
}

// Four fixtures broaden BD-rate coverage over the original 10-fixture set:
//
//   - F11 1080p sports CBR cpu=-3: pairs with the existing 720p sports
//     CBR fixture on a higher-resolution rung with slower-RD
//     cpu=-3, closing the high-res / slower-RD coverage gap. The
//     original set had no 1080p CBR coverage.
//   - F12 480p mixed-motion VBR: covers the bursty static<->motion
//     rate-control adaptation axis. The 1080p static-then-motion VBR
//     static-then-motion fixture has a single transition; this one alternates every
//     4 frames so the rate controller has to absorb repeated phase
//     boundaries.
//   - F13 720p RT cpu=4 CBR: fills the mid-realtime gap between the
//     cpu=8 fixture (max-speed RT) and the good-quality default
//     (cpu=0). cpu=4 sits at the libvpx "balanced" realtime preset.
//   - F14 640p denoise-heavy VBR: exercises the YUV temporal denoiser
//     (NoiseSensitivity=3 aggressive YUV denoise) + Sharpness=4
//     loop-filter tuning over a noisy sports-motion source. No earlier
//     fixture engages NoiseSensitivity or Sharpness against the libvpx
//     oracle, leaving the camera-noise / loop-filter axes unmonitored.
//     ARNR (LookaheadFrames+AutoAltRef path) is covered separately by
//     TestVP8BDRate720pARNRHeavyVBR after the BD-rate harness learned
//     to skip hidden alt-ref packets in the per-frame PSNR pairing pass
//     (PeekVP8StreamInfo show_frame check) and to pair visible packets to
//     source frames by the encoder-echoed PTS, which is robust to the
//     alt-ref scheduler's hidden/visible interleaving.

// TestVP8BDRate1080pSportsCpu3CBR drives a 1080p sports-motion
// fixture through a CBR ladder with cpu_used=-3 (slower good-quality
// preset, more RD iterations than cpu=0). Higher-resolution and
// slower-RD counterpart to the 720p sports CBR cpu=0 fixture.

func TestVP8BDRate1080pSportsCpu3CBR(t *testing.T) {
	const (
		width  = 1920
		height = 1080
		frames = 8
	)
	// Initial measurement: govpx-vs-libvpx BD-rate=-3.427% /
	// BD-PSNR=+0.176 dB on the 1080p sports CBR cpu=-3 ladder. govpx
	// beats libvpx by ~3.4%, so the gate is set negative: -2.9% (observed
	// -3.427% plus +0.5% headroom for cubic-fit jitter on the 8-frame
	// ladder). Any regression that loses the cpu=-3 slower-RD picker flow at
	// 1080p trips the gate immediately. Frame count is fixed at 8 because
	// 1080p x 4 rungs x 2 oracles is the most expensive fixture in the suite
	// (~7 min wall-clock per gate run).
	sportsCpu3Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: -2.9,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 1080p sports-motion cpu=-3 (CBR ladder 2000/4000/8000/16000 kbps)",
		"VP8 1080p sports cpu=-3 (CBR 2000/4000/8000/16000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{2000, 4000, 8000, 16000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineGoodQuality
				o.CpuUsed = -3
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineGoodQuality
				o.CpuUsed = -3
			},
		},
		sportsCpu3Gate)
}

// TestVP8BDRate480pMixedMotionVBR drives a 480p mixed-motion
// fixture (alternating slow/fast phases every 4 frames) through a VBR
// ladder. Tests the rate controller's per-frame adaptation across
// repeated motion-energy phase boundaries, an axis the original set only
// touched once with a single static->motion transition.
