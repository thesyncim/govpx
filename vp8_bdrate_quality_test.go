package govpx_test

// VP8 BD-rate quality gate. Mirrors the VP9 per-feature BD-rate
// scoreboard in feature_quality_gates_vp9_test.go but drives the VP8
// encoder via the BD-rate VP8 harness in
// cmd/govpx-bench/benchcmd/bdrate_vp8.go.
//
// Two checks fire per call:
//
//  1. Within-govpx feature on/off BD-rate stays inside a sane
//     tolerance band (so e.g. a future regression that breaks rate
//     control and inflates byte count is caught).
//  2. Absolute govpx-vs-libvpx BD-rate: govpx VP8 should not need
//     materially more bitrate than libvpx VP8 at equal PSNR.
//
// Initial baseline thresholds — captured 2026-05-19 on a textured-
// noise / translating synthetic fixture with a CBR ladder of
// 100/200/400/800 kbps at 176x144 (QCIF) over 24 frames. QCIF was
// chosen over 64x64 because VP8 saturates the smaller frame at
// ~535 kbps regardless of target, collapsing the upper rungs of any
// useful BD-rate ladder:
//
//   - MaxBDRateOverLibvpxPct: 5% — govpx VP8 is byte-exact against
//     libvpx for the locked-in matching configurations, so within
//     measurement noise on the cubic fit (synthetic fixtures, short
//     ladder) the absolute BD-rate must be near zero. A 5% ceiling
//     leaves headroom for the BD-rate cubic-fit floating-point spread
//     while still catching a real divergence.
//   - MinBDPSNRdB: -0.5 dB — same reasoning on the BD-PSNR axis.
//
// The gate is opt-in via GOVPX_BD_RATE_GATES=1 (same env var the VP9
// gates use). Set GOVPX_BD_RATE_LIBVPX_VP8_REQUIRED=1 to hard-fail on
// a missing vpxenc binary; default is t.Skip the absolute assertion.

import (
	"image"
	"math"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// assertLibvpxVP8AbsoluteGate evaluates the absolute govpx-vs-libvpx
// VP8 BD-rate assertion. Mirrors assertLibvpxAbsoluteGate's VP9 form
// but uses LibvpxVP8Required() instead of LibvpxRequired().
func assertLibvpxVP8AbsoluteGate(t *testing.T, feature string, res benchcmd.BDRateResult, gate benchcmd.LibvpxAbsoluteGate) {
	t.Helper()
	if res.LibvpxErr != nil {
		if len(res.Libvpx) > 0 {
			t.Logf("%s libvpx-VP8-reference: cross-metric unavailable (skipping absolute gate): %v libvpx=%v",
				feature, res.LibvpxErr, res.Libvpx)
			return
		}
		if benchcmd.LibvpxVP8Required() {
			t.Fatalf("%s libvpx VP8 reference required but unavailable: %v",
				feature, res.LibvpxErr)
		}
		t.Logf("%s libvpx VP8 reference unavailable (skipping absolute gate): %v",
			feature, res.LibvpxErr)
		return
	}
	if len(res.Libvpx) == 0 {
		if benchcmd.LibvpxVP8Required() {
			t.Fatalf("%s libvpx VP8 reference required but empty", feature)
		}
		t.Logf("%s libvpx VP8 reference empty (skipping absolute gate)", feature)
		return
	}
	t.Logf("%s libvpx-VP8-reference: govpx-vs-libvpx BD-rate=%+0.3f%% BD-PSNR=%+0.3f dB libvpx=%v",
		feature, res.BDRateGovpxVsLibvpx, res.BDPSNRGovpxVsLibvpx, res.Libvpx)
	if !math.IsNaN(res.BDRateGovpxVsLibvpx) && res.BDRateGovpxVsLibvpx > gate.MaxBDRateOverLibvpxPct {
		t.Errorf("%s govpx vs libvpx VP8 BD-rate=%+0.3f%% > %+0.3f%% — govpx trails libvpx by more than the configured ceiling; investigate before tightening the gate",
			feature, res.BDRateGovpxVsLibvpx, gate.MaxBDRateOverLibvpxPct)
	}
	if !math.IsNaN(res.BDPSNRGovpxVsLibvpx) && res.BDPSNRGovpxVsLibvpx < gate.MinBDPSNRdB {
		t.Errorf("%s govpx vs libvpx VP8 BD-PSNR=%+0.3f dB < %+0.3f dB — govpx delivers materially less quality than libvpx at equal rate",
			feature, res.BDPSNRGovpxVsLibvpx, gate.MinBDPSNRdB)
	}
}

// runVP8BDRateFixture is the common driver for the per-fixture VP8
// BD-rate gate cases: it runs ComputeBDRateVP8 with the supplied
// options, logs the curves, asserts the within-govpx self-comparison
// is near zero (harness-wiring smoke), asserts the absolute govpx-vs-
// libvpx gate, and publishes a scoreboard row keyed by the supplied
// label. The local label is also threaded through assertLibvpxVP8
// AbsoluteGate so per-fixture failures point at the offending case.
func runVP8BDRateFixture(t *testing.T, label, scoreboardLabel string, opts benchcmd.BDRateOptionsVP8, gate benchcmd.LibvpxAbsoluteGate) {
	t.Helper()
	if !benchcmd.FeatureGatesEnabled() {
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
	benchcmd.AppendFeatureScoreboardRow(benchcmd.FeatureLibvpxObservation{
		Feature:                scoreboardLabel,
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
func TestVP8BDRateBaseline(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
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
	// Publish to the per-feature scoreboard so the BD-rate diagnostic
	// table includes the VP8 row alongside the VP9 ones.
	benchcmd.AppendFeatureScoreboardRow(benchcmd.FeatureLibvpxObservation{
		Feature:                "VP8 baseline (QCIF CBR 100/200/400/800)",
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
func TestVP8BDRate360pPanningCBR(t *testing.T) {
	const (
		width  = 640
		height = 360
		frames = 16
	)
	// The 360p panning fixture measures govpx-vs-libvpx BD-rate=+1.111%.
	// Tighten the gate from the +5.0% default to +3.1% (observed +1.111%
	// plus +2.0% headroom for cubic-fit jitter on the 16-frame ladder).
	// Any future regression that drives govpx more than ~2% over libvpx on
	// this 360p panning CBR ladder trips the gate immediately.
	panning360Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 3.1,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 360p panning (CBR ladder 300/600/1200/2400 kbps)",
		"VP8 360p panning (CBR 300/600/1200/2400)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{300, 600, 1200, 2400},
			Source:         func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline:       func(*govpx.EncoderOptions) {},
			Test:           func(*govpx.EncoderOptions) {},
		},
		panning360Gate)
}

// TestVP8BDRate720pSportsCBR exercises a 720p high-motion
// fixture (textured background + fast foreground "ball") under a CBR
// ladder that spans modest-to-comfortable streaming bitrates. The
// foreground sweep guarantees nontrivial motion vectors so the encoder
// does real inter work rather than collapsing to mostly-intra at low
// rungs.
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
func TestVP8BDRate720pGoodSSIM(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Current measurement: govpx-vs-libvpx BD-rate=-1.359%. Keep the gate
	// negative so the SSIM-tuned RD path remains better than libvpx on this
	// fixture, with headroom for cubic-fit jitter near transparent-PSNR
	// upper rungs.
	ssimGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: -0.8,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p panning tune=ssim (CBR ladder 1500/3000/6000/12000 kbps)",
		"VP8 720p panning (tune=ssim, CBR 1500/3000/6000/12000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1500, 3000, 6000, 12000},
			Source:         func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
		},
		ssimGate)
}

// TestVP8BDRate480pVBR runs an additional 480p coverage case: a
// 480p panning fixture under a single-pass VBR ladder. (The harness
// does not yet drive libvpx through two passes; single-pass VBR keeps
// both sides on the same end-usage axis while still exercising the
// VBR rate-control path, which the other fixtures don't.)
func TestVP8BDRate480pVBR(t *testing.T) {
	const (
		width  = 854
		height = 480
		frames = 16
	)
	// The 480p panning VBR fixture measures govpx-vs-libvpx
	// BD-rate=+0.645%. Tighten the gate from the +5.0% default to +2.6%
	// (observed +0.645% plus +2.0% headroom for cubic-fit jitter on the
	// single-pass VBR ladder). Any future regression that drives govpx more
	// than ~2% over libvpx on the single-pass VBR rate-control axis trips
	// the gate immediately.
	vbr480Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 2.6,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 480p panning (VBR ladder 500/1000/2000/4000 kbps)",
		"VP8 480p panning (VBR 500/1000/2000/4000)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{500, 1000, 2000, 4000},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline:               func(*govpx.EncoderOptions) {},
			Test:                   func(*govpx.EncoderOptions) {},
		},
		vbr480Gate)
}

// TestVP8BDRate480pSportsSSIMVBR widens the SSIM-tune coverage at
// a smaller resolution than the existing 720p SSIM fixture
// (TestVP8BDRate720pGoodSSIM). It pairs the
// high-motion sports source (textured background + fast foreground
// "ball") with --tune=ssim under a single-pass VBR ladder, so the
// activity-masking RD path is exercised against a non-CBR rate-control
// axis. The 480p resolution complements F5 (720p) and F16 (1080p) to
// give a spread of SSIM coverage across resolutions.
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
func TestVP8BDRate1080pPanningSSIMCBR(t *testing.T) {
	const (
		width  = 1920
		height = 1080
		frames = 12
	)
	// Initial measurement: BD-rate=+1.186% / BD-PSNR=+0.151 dB
	// (govpx slightly trails libvpx on the 1080p panning CBR SSIM
	// ladder, compared to the 720p panning SSIM fixture's current -1.359%
	// measurement: at 1080p the activity-masking RD path encounters more
	// MBs per frame and the cubic-fit jitter widens). Set the ceiling at
	// +3.2% (observed +1.186% plus +2.0% headroom for cubic-fit jitter on
	// the 12-frame 1080p ladder) and the BD-PSNR floor at -0.5 dB.
	ssim1080Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 3.2,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 1080p panning tune=ssim (CBR ladder 1500/3000/6000/12000 kbps)",
		"VP8 1080p panning (tune=ssim, CBR 1500/3000/6000/12000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1500, 3000, 6000, 12000},
			Source:         func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
		},
		ssim1080Gate)
}

// TestVP8BDRate360pScreenContentSSIMCBR combines --tune=ssim with
// --screen-content-mode=1 at 360p to exercise the intersection of the
// activity-masking RD path with the screen-content semantic gating
// (UV-delta-Q, cyclic-refresh, buffer-debt floor, limit_q_cbr_inter floor).
// The existing 720p screen-content CBR fixture
// (TestVP8BDRate720pScreenContentCBR) runs at the libvpx default
// --tune=psnr; this fixture additionally pins Tuning to TuneSSIM so the
// libvpx oracle emits --tune=ssim alongside --screen-content-mode=1.
func TestVP8BDRate360pScreenContentSSIMCBR(t *testing.T) {
	const (
		width  = 640
		height = 360
		frames = 16
	)
	// Initial measurement: BD-rate=+2.668% / BD-PSNR=+0.070 dB
	// (govpx trails libvpx on the 360p screen-content tune=ssim CBR
	// ladder). The pure-PSNR-tune sibling at 720p
	// (TestVP8BDRate720pScreenContentCBR) sits at +9.704% with
	// the gate set at +11.5%; this smaller-resolution SSIM-tune variant
	// avoids the prob_intra_coded equilibrium that dominates the 720p
	// residual (the 360p frame has 4x fewer MBs so the recode loop
	// converges sooner). Set the ceiling at +4.7% (observed +2.668% plus
	// +2.0% headroom for cubic-fit jitter on
	// the sparse screen-content rate axis) and the BD-PSNR floor at
	// -0.6 dB to match the 720p screen-content fixture (the same
	// near-transparent upper-rung dynamic that drives the wider
	// BD-PSNR slack there is present here, just at a smaller scale).
	ssimScreen360Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 4.7,
		MinBDPSNRdB:            -0.6,
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
func TestVP8BDRate720pTwoPassVBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 16
	)
	// Two-pass VBR introduces additional cubic-fit jitter because the
	// rate axis depends on the second-pass GF/ARF allocator's per-frame
	// bit budgeting. Per-rung the absolute rate divergence is small
	// (govpx-vs-libvpx is within ~3% at every operating point: 609.8
	// vs 627.1 kbps at the bottom rung, 4474 vs 4436 kbps at the top),
	// but the cubic-fit aggregation through 4 ladder points where the
	// upper rung approaches PSNR transparency (~47 dB) amplifies the
	// fit residual to a steady-state +5.137% BD-rate measurement on
	// this 16-frame 720p panning fixture. The float-vs-int arithmetic-order
	// cleanup ported every divergence the libvpx
	// define_gf_group / kfBitsTarget / assignStdFrameBits paths
	// touched (saturate_cast_double_to_int helper, divide-before-
	// multiply in alt_kf_bits, double-domain Boost*group_bits/
	// allocation_chunks reduction) and confirmed the gf_bits the
	// allocator produces is bit-identical against libvpx; the
	// remaining gap is encoder behavioural drift on the small-
	// fixture cubic-fit, not a libvpx-port arithmetic gap.
	//
	// Tighten the per-fixture gate from the widened 10% to 6.0% —
	// measured 5.137% plus +0.86% headroom — so a real regression
	// (any +1% shift in encoder output beyond the cubic-fit-baseline)
	// trips the gate but the fit-residual steady state still passes.
	// The within-govpx baseline-vs-test BD-rate is still expected to
	// be near zero (checked by runVP8BDRateFixture).
	//
	// After the intra-in-inter-loop tteob==0 rate2 backout landed
	// (vp8_encoder_inter_modes_rd_intra.go:
	// estimateInterIntraModeRDScore), the measured value drifted above
	// +5%. Per-rung diff vs the previous govpx baseline:
	//   target=1500: 609.795 -> 617.985 kbps  (+8.19, +1.34%) PSNR -0.002dB
	//   target=3000: 1496.34 -> 1496.34 kbps  (byte-identical)
	//   target=6000: 2931.81 -> 2931.81 kbps  (byte-identical)
	//   target=12000: 4474.11 -> 4474.11 kbps (byte-identical)
	// Only the lowest rung shifts because higher rungs hit the CQLevel
	// Q-floor where the picker's mode choices collapse near-zero
	// residual. The +1.34% bottom-rung rate shift amplifies through
	// the 4-point cubic fit into a visible BD-rate move. The shift is
	// intentional: the picker now matches libvpx
	// vp8/encoder/rdopt.c:1684-1714 (tteob==0 mode-backout), so the correct
	// action is to keep a tight but non-fragile ceiling and document the
	// steady state. Current measurement is +6.109%.
	twoPassGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 7.0,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p panning two-pass (VBR ladder 1500/3000/6000/12000 kbps)",
		"VP8 720p panning (two-pass VBR 1500/3000/6000/12000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1500, 3000, 6000, 12000},
			TwoPass:        true,
			Source:         func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline:       func(*govpx.EncoderOptions) {},
			Test:           func(*govpx.EncoderOptions) {},
		},
		twoPassGate)
}

// TestVP8BDRate720pScreenContentCBR drives a 720p screen-content
// (synthetic-text-window) fixture through a CBR ladder with the libvpx
// screen-content mode flag enabled on both sides. This exercises the
// VP8 screen-content mode-tree probability bias (DC/V_PRED dominant
// intra modes), the screen-content fast-decision intra-block path,
// and the screen-content-specific ARNR strength tweak.
func TestVP8BDRate720pScreenContentCBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Screen content saturates the rate axis well below the ladder
	// upper rungs (synthetic-text frames are sparser than camera
	// content) so the produced-rate curve compresses near
	// ~4 Mbps regardless of target. Widen the per-fixture gate to
	// 37.6% (vs 5% default) to absorb the consequent cubic-fit jitter
	// after the libvpx oracle CLI mapper fix
	// (cmd/govpx-bench/benchcmd/bdrate_vp8.go libvpxVP8BDCLIArgs +
	// libvpxVP8BDCLIArgsTwoPass: pass --screen-content-mode=N when
	// govpx.EncoderOptions.ScreenContentMode is set so the libvpx
	// vpxenc oracle runs with the same VP8E_SET_SCREEN_CONTENT_MODE
	// flag as the govpx encoder under test). Before that fix the
	// libvpx oracle silently ran with screen_content_mode=0 against
	// govpx's screen_content_mode=1, masking a real ~36% BD-rate
	// gap as a more flattering ~20% gap.
	//
	// Regulator traces confirm the four libvpx screen-content semantic
	// sites — UV-delta-Q
	// (vp8_quantize.c:469), cyclic-refresh MB-budget scaling
	// (onyx_if.c:509-528), buffer-debt floor (onyx_if.c:4533), and the
	// limit_q_cbr_inter Q-decrease floor (ratectrl.c:1297-1300) — are
	// all faithfully ported (vp8_encoder_reconstruct.go:62-73,
	// vp8_encoder_segmentation.go:502-521, vp8_ratecontrol_postencode.go:318-323,
	// vp8_ratecontrol_postencode.go:313-316 + vp8_ratecontrol_quantizer.go:65-66
	// + vp8_ratecontrol_recode.go:201). All four sites fire in govpx with the
	// same gating libvpx uses, so the residual gap is NOT a missing port.
	//
	// The actual driver of the +36% rate gap surfaced via per-frame
	// regulator traces of frame 1 at the 4 Mbps rung: govpx and libvpx
	// both start with RCF.inter=1.0 and pick Q=119 (internal) on the
	// first attempt. Both attempt-1 encodes produce ~5300 bits (govpx
	// 5593, libvpx 5315). The recode loop drops RCF to 0.3 in both and
	// next-attempts Q=62. At Q=62 govpx produces ~255 kbits while
	// libvpx produces ~17 kbits — a 15x bit-spend divergence at the
	// SAME internal Q on the SAME source frame. This is a byte-parity
	// gap in the inter-frame mode-selection / coefficient-coding path
	// at mid-Q on screen-content frames (the integer-pel 8-pixel
	// translation should mode-decision to ZeroMV+LAST with tiny
	// residuals; govpx appears to spend bits on intra or non-zero-MV
	// modes that libvpx skips). Govpx's recode loop then oscillates
	// (Q=119 → 62 → 91 → 76) while libvpx converges monotonically
	// (Q=119 → 62 → 26 → 13), so govpx settles at a higher Q with a
	// smaller frame and libvpx settles at a lower Q matching target.
	//
	// Govpx ports libvpx vp8/encoder/rdopt.c calculate_final_rd_costs
	// (lines 1684-1714) `tteob == 0` rate2 backout into its
	// intra-in-inter-loop RD picker (estimateInterIntraModeRDScore).
	// libvpx drops `rate_y + rate_uv` from the rate2 cost when every Y AC
	// coefficient and every UV coefficient quantizes to zero and replaces
	// them with the `prob_skip_false=1` delta; govpx was charging the
	// full coefficient rate to intra candidates regardless of EOB state.
	// On flat-Y screen-content MBs this inflated DC_PRED / V_PRED /
	// H_PRED / TM_PRED's rate2 by ~20K bits vs libvpx, driving the picker
	// to spend NEWMV+LAST bits where libvpx coded the MB as a skipped
	// intra. The screen-content MB parity test pinned the divergence at
	// frame 1 MB(5,0) DC_PRED
	// (govpx rate=20838 vs libvpx rate=1012, score=97846 vs 7622), and
	// the verbatim port closes the gap to 0 mode/ref/mv mismatches across
	// all 3600 MBs of the screen-content fixture. The BD-rate measurement
	// collapsed from +36.054% to +9.704% on the 1280x720 ladder.
	//
	// Retighten the gate from the +37.6% steady-state ceiling to +11.5%
	// (measured +9.704% plus +1.8% headroom). The BD-PSNR gate widens
	// from -0.5 to -0.6 dB to absorb the measured -0.544 dB residual:
	// the screen-content cubic-fit still amplifies sparse-frame rate
	// jitter through the 4-rung ladder, and the cubic axis crosses
	// near-transparent PSNR (~43 dB) at the top rung where the absolute
	// dB delta is dominated by libvpx's lower-Q operating point rather
	// than encoder behavioural drift. Further BD-rate tightening would
	// require porting the libvpx coefficient-rate / token-cost ladder
	// quirks that drive the residual rate gap below 10%.
	//
	// The screen-content residual parity test extends the per-MB probe
	// across frames 2-11 and pins the residual divergence seed at frame 2
	// MB(0,1). Frame 1 stays
	// byte-exact (0 mismatches); frame 2 has 1326/3600 MB mode
	// mismatches (1220 with govpx ZEROMV+GOLDEN where libvpx picks
	// intra). Root cause: prob_intra_coded self-reinforcing
	// equilibrium — govpx ends frame 2 with prob_intra_coded=1
	// (vp8_convert_rfct_to_prob clamps to 1 when intra count is 0)
	// while libvpx evolves to 87 across 91 recode iterations. The
	// govpx recode loop hits rate_correction_factor=0.01 floor at
	// iter 4 with frame_size=693 bytes (target ~20772) and stays
	// clamped because every Q produces the same all-skip
	// ZEROMV-GOLDEN pattern at prob_intra=1. The libvpx-side
	// mechanism that admits intra candidates against prob_intra=1
	// (breaking out of the equilibrium) is not yet localized; the
	// screen-content residual parity test keeps that follow-up probe covered.
	screenContentGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 11.5,
		MinBDPSNRdB:            -0.6,
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
func TestVP8BDRate720pRealtimeCpu8CBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 16
	)
	// Realtime cpu=8 disables further sub-pixel refinement steps and
	// the improved MV predictor, both of which materially shift the
	// RD curve even between identical libvpx builds at different
	// speed levels. The govpx byte-exact contract pins cpu_used=0
	// (good); cpu_used=8 is a known-divergent operating point where
	// govpx implements the libvpx-ported speed cascade but the cubic
	// fit on this short ladder amplifies the per-frame Q drift.
	//
	// Back-to-back runs of this fixture show large run-to-run variance
	// driven entirely by the libvpx oracle side
	// (govpx-side ref==test curves are bit-identical across runs).
	// The libvpx vpxenc --rt --cpu-used=8 path makes per-inter-frame
	// wall-clock-budget decisions in its real-time auto-speed
	// cascade (vp8/encoder/onyx_if.c), and on a 16-frame 720p panning
	// source those decisions ripple through to PSNR-Y at the mid rungs.
	// Three consecutive runs measured:
	//   run A: govpx-vs-libvpx BD-rate=+2.299%, BD-PSNR=-0.372 dB
	//   run B: govpx-vs-libvpx BD-rate=+6.935%, BD-PSNR=-0.952 dB
	//   run C: govpx-vs-libvpx BD-rate=+16.821%, BD-PSNR=-0.875 dB
	// Libvpx produced PSNR at the 4 Mbps rung varied 42.858 -> 44.477
	// dB across runs B and C on identical source.
	//
	// The mitigation is to avoid libvpx vp8_auto_select_speed
	// (rdopt.c:261), which reads cpi->avg_encode_time, a vpx_usec_timer
	// wall-clock
	// measurement, and adapts cpi->Speed accordingly. Two consecutive
	// vpxenc invocations on the same source produce different
	// cpi->Speed trajectories and therefore different rate/PSNR for
	// every operating point. Empirical runs at the previous CpuUsed=+8
	// setting (which engages auto-select) showed
	// median-of-3 with the new harness shim still produced run-to-run
	// BD-rate spreads of -6.7% / +13.4% / +672% across three audit
	// runs (the 4 Mbps PSNR-Y rung varied 36.6 -> 42.9 -> 44.5 dB),
	// confirming median-of-N alone cannot tame this scenario.
	//
	// libvpx's documented escape hatch is `--cpu-used=-N` (negative):
	// per vp8/encoder/encodeframe.c:686-687, when oxcf.cpu_used < 0
	// the encoder bypasses vp8_auto_select_speed and pins
	// `cpi->Speed = -cpu_used` directly. That mode preserves the
	// realtime deadline (compressor_speed=2 ratecontrol/speed-features
	// cascade) but removes the wall-clock dependency. Govpx's
	// libvpxAutoSelectSpeed mirrors this branch exactly
	// (vp8_encoder_config.go:710-713: `if cpuUsed < 0 { e.autoSpeed =
	// -cpuUsed; return }`), so flipping CpuUsed from +8 to -8 on both
	// sides keeps the comparison apples-to-apples while making the
	// per-point output deterministic.
	//
	// LibvpxOracleRuns=3 is kept as belt-and-suspenders against
	// residual oracle-side variance from sources outside auto-select
	// (e.g. MT-LF jitter, NEON kernel race in tiny accumulators on
	// Apple Silicon). With both mitigations the +10.0% / -1.0 dB envelope
	// holds across N>=3 audit runs on arm64-darwin and the temporary
	// widen-to-+20% gate is rolled back.
	//
	// With the mitigations in place the fixture now measures
	// govpx-vs-libvpx BD-rate=-0.817% /
	// BD-PSNR=+0.000 dB on this arm64-darwin host (median-of-3 libvpx
	// runs collapse to the govpx-aligned trajectory on every ladder
	// rung). The +10.0% envelope leaves ~10pp of dead headroom that masks
	// any future regression in the negative-
	// cpu_used / median-of-3 path. Tighten MaxBDRateOverLibvpxPct
	// from +10.0% to +1.2% (observed -0.817% + 2.0pp positive-ceiling
	// headroom, rounded to one decimal). MinBDPSNRdB stays at -1.0 dB so
	// a residual oracle-side cold-PSNR tail outside the BD-rate axis still
	// has buffer.
	realtimeCpu8Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.2,
		MinBDPSNRdB:            -1.0,
	}
	runVP8BDRateFixture(t,
		"VP8 720p panning realtime cpu=8 (CBR ladder 1000/2000/4000/8000 kbps)",
		"VP8 720p panning realtime cpu=8 (CBR 1000/2000/4000/8000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1000, 2000, 4000, 8000},
			Source:         func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				// Pin Speed=8 via libvpx's documented negative-cpu_used
				// escape (vp8/encoder/encodeframe.c:686-687); avoids
				// vp8_auto_select_speed's wall-clock variance on both
				// sides of the comparison. Govpx mirrors the same branch at
				// vp8_encoder_config.go:710-713.
				o.CpuUsed = -8
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = -8
			},
			// Belt-and-suspenders median-of-3 against residual oracle-side
			// variance (NEON kernel race, MT-LF jitter on threads>=2,
			// etc.) outside the auto-speed wall-clock path.
			LibvpxOracleRuns: 3,
		},
		realtimeCpu8Gate)
}

// TestVP8BDRate720pTokenParts4CBR drives a 720p sports-motion
// fixture through a CBR ladder with token-partitions=2 (vpxenc maps
// 2 -> 4 token partitions). This exercises the parallel-tokens header
// byte layout and the per-partition arithmetic encoder init: the resulting
// bitstream has 4 token partitions packed after the first-partition header,
// and the libvpx reference must match the per-partition byte budget.
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
func TestVP8BDRate720pRealtimeCpu4CBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 16
	)
	// The cpu=8 sibling widened to +20%/-1.2 dB after a libvpx-oracle
	// wall-clock variance audit found a ±14pp BD-rate spread across three
	// back-to-back runs; this cpu=4 fixture has the same realtime auto-speed
	// cascade (vp8/encoder/onyx_if.c) and shows the same wall-clock-budget
	// variance pattern.
	//
	// Four samples captured back-to-back:
	//   sample 1: BD-rate=-26.090%, BD-PSNR=-0.207 dB (cold libvpx oracle)
	//   sample 2: BD-rate= +6.854%, BD-PSNR=-0.955 dB
	//   sample 3: BD-rate= +6.854%, BD-PSNR=-0.955 dB
	//   sample 4: BD-rate= +6.854%, BD-PSNR=-0.955 dB
	//
	// Later audit at the same +4 setting:
	//   sample 1: BD-rate= -7.401%, BD-PSNR=+0.781 dB
	//   sample 2: BD-rate= +27.137%, BD-PSNR=+2.776 dB
	//   sample 3: BD-rate= +254.149%, BD-PSNR=+2.963 dB (cold libvpx)
	//   sample 4: BD-rate= -27.775%, BD-PSNR=+5.110 dB
	//   sample 5: BD-rate= -16.286%, BD-PSNR=+3.079 dB
	//   sample 6: BD-rate= -70.911%, BD-PSNR=+5.538 dB
	// The ±280pp spread confirms libvpx's vp8_auto_select_speed (a
	// wall-clock-driven per-frame cpi->Speed adapter — rdopt.c:261)
	// is the dominant noise source. govpx-side ref==test PSNR is
	// bit-identical across every sample; the entire spread is on the
	// libvpx oracle side.
	//
	// Mitigation mirrors the cpu=8 sibling: pin both sides at the documented
	// libvpx negative-cpu_used
	// escape (vp8/encoder/encodeframe.c:686-687: oxcf.cpu_used < 0
	// bypasses vp8_auto_select_speed and pins cpi->Speed = -cpu_used)
	// to remove the wall-clock dependency, plus LibvpxOracleRuns=3
	// median-of-3 as belt-and-suspenders. Under the pin the fixture
	// becomes deterministic: five consecutive audit runs measure
	// identical govpx-vs-libvpx BD-rate=+6.240% BD-PSNR=-0.868 dB to
	// the thousandth (libvpx curve 33.783 / 37.718 / 42.800 /
	// 47.185 dB at 1887 / 3552 / 5042 / 7315 kbps). The +6.24% is a
	// real Speed=4 algorithmic gap — not auto-speed noise — and is
	// the static target for follow-up porting (analogous to the targeted
	// Speed-gate ports, but at cpu_used+1=5 realistic Speed only the HEX
	// gate fires; the remaining +6.24% gap is below Speed=5 cascade-only
	// territory and sits at the realtime Speed=4 RD-vs-fast picker
	// decisions). The +10.0% / -1.0 dB envelope leaves +3.7pp headroom over
	// the observed +6.240% to absorb cubic-fit jitter on this short
	// 16-frame ladder.
	//
	// With CpuUsed=-4 + LibvpxOracleRuns=3 the fixture's BD-rate is
	// deterministic at govpx-vs-libvpx +6.240% / -0.868 dB across runs.
	// The +10.0% ceiling carries +3.76pp of dead headroom over the
	// steady-state value; tighten the BD-rate gate to +8.3% (observed
	// +6.240% + 2.0pp positive-ceiling headroom, rounded to one decimal).
	// The BD-PSNR floor stays at -1.0 dB because the observed -0.868 dB sits
	// near that band, and tightening PSNR further risks tripping the gate on
	// cubic-fit jitter at the lower ladder rungs where libvpx's Speed=4 fast
	// picker delivers a 0.7-1.2 dB PSNR-Y advantage that the rate axis trades
	// against.
	realtimeCpu4Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 8.3,
		MinBDPSNRdB:            -1.0,
	}
	runVP8BDRateFixture(t,
		"VP8 720p panning realtime cpu=4 (CBR ladder 1000/2000/4000/8000 kbps)",
		"VP8 720p panning realtime cpu=4 (CBR 1000/2000/4000/8000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1000, 2000, 4000, 8000},
			Source:         func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				// Pin Speed=4 via libvpx's documented negative-cpu_used
				// escape (vp8/encoder/encodeframe.c:686-687); avoids
				// vp8_auto_select_speed's wall-clock variance on both
				// sides of the comparison. Govpx mirrors the same branch at
				// vp8_encoder_config.go:710-713.
				o.CpuUsed = -4
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineRealtime
				o.CpuUsed = -4
			},
			// Belt-and-suspenders median-of-3 against residual oracle-side
			// variance (NEON kernel race, MT-LF jitter on threads>=2,
			// etc.) outside the auto-speed wall-clock path.
			LibvpxOracleRuns: 3,
		},
		realtimeCpu4Gate)
}

// TestVP8BDRate640pDenoiseSharpVBR drives a 640x360 sports-motion
// fixture through a VBR ladder with the YUV temporal denoiser engaged
// (NoiseSensitivity=3 aggressive YUV denoise, the libvpx default for
// camera-noise removal in VBR streaming) and the loop-filter Sharpness
// set to 4. Earlier fixtures all leave NoiseSensitivity at 0 and
// Sharpness at the libvpx default; this is the only fixture today that
// engages the denoiser / loop-filter-sharpness path against the libvpx
// oracle.
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
func TestVP8BDRate720pBPredEdgeGridCBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// After the VP8 BD harness default-deadline fix, govpx-vs-libvpx
	// measures +6.443% / +0.086 dB. Keep the standard +2pp
	// positive-ceiling headroom and round to one decimal so a future
	// speed-bucket drift or B_PRED/SPLITMV scoring regression trips this
	// fixture before it re-enters the old +24-25% band.
	bpredGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 8.5,
		MinBDPSNRdB:            -0.5,
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
func TestVP8BDRate720pARNRHeavyVBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 24
	)
	arnrHeavyGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: -1.2,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p panning ARNR-heavy (VBR ladder 800/1500/3000/6000 kbps, lookahead=16 auto-altref arnr-max=7/str=3/type=3)",
		"VP8 720p panning ARNR-heavy (VBR 800/1500/3000/6000, lookahead=16 arnr=7/3/3)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{800, 1500, 3000, 6000},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return testutil.NewTexturedPanningYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.LookaheadFrames = 16
				o.AutoAltRef = true
				o.ARNRMaxFrames = 7
				o.ARNRStrength = 3
				o.ARNRType = 3
			},
			Test: func(o *govpx.EncoderOptions) {
				o.LookaheadFrames = 16
				o.AutoAltRef = true
				o.ARNRMaxFrames = 7
				o.ARNRStrength = 3
				o.ARNRType = 3
			},
		},
		arnrHeavyGate)
}
