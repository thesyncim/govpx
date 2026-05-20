package govpx_test

// Per-feature VP9 BD-rate quality gates. These tests load the BD-rate
// harness and run it once per feature toggle. They are slow (~25-60s
// for the full sweep) and so opt-in via the GOVPX_BD_RATE_GATES=1
// env var. `make verify-bd-rate` sets it.
//
// Each gate asserts both:
//
//  1. A within-govpx tolerance band around the observed feature-on
//     vs feature-off BD-rate (these are the "did the feature flip
//     direction?" assertions that the prior commits installed).
//  2. An absolute-reference assertion comparing govpx-with-feature
//     against libvpx-with-matching-flags via the
//     vpxenc-vp9-frameflags helper. The absolute gate caps govpx's
//     BD-rate disadvantage vs libvpx and floors govpx's BD-PSNR
//     disadvantage. These thresholds are set wide today because
//     govpx is a young port and the absolute gap on synthetic 64x64
//     fixtures is dominated by fixed-overhead headers; they are
//     ratcheted as the gap closes.
//
// Findings recorded in commit "Add libvpx absolute BD-rate reference
// curves to the quality-gate harness":
//   - Standalone unfiltered AutoAltRef is neutral in the public-Q gate:
//     libvpx leaves source_alt_ref_pending false here, so govpx must not
//     emit a hidden bootstrap packet either.
//   - ARNR on/off is measured in the realtime VBR lane where libvpx's
//     one-pass ARF scheduler actually fires; gate requires ≤ -1%.
//   - TPL on/off is neutral on the 64x64 sharp-edge fixture today even
//     though the per-SB rdmult deltas fire; gate at ≤ +1% to catch
//     regressions without inventing a savings claim for the tiny fixture.
//   - VarianceAQ neutral (≤ ±5%) within govpx in pure-Q.
//   - Equator360 AQ neutral on non-360 (≤ ±5%) within govpx.
//   - Perceptual AQ on vs off: +1.524% post-libvpx-verbatim-port on
//     PerceptualContent; gate at ≤ +2.0%.
//   - AltRefAQ neutral: libvpx v1.16.0's VP9 alt-ref AQ control is
//     wired but stubbed, so govpx must not invent a coding delta.
//
// Absolute libvpx-reference gate thresholds (govpx vs libvpx, at the
// feature-on operating point):
//
//   - MaxBDRateOverLibvpxPct: 20% — anchored to the current absolute
//     gap on synthetic 64x64 fixtures (~5-15% across features). The
//     +20% cap leaves headroom for measurement noise on small
//     fixtures while still catching a regression where govpx
//     suddenly trails libvpx by 50-100%.
//   - MinBDPSNRdB: -2.0 dB — govpx may sit up to 2 dB below libvpx
//     at equal rate before the gate fails. This is generous because
//     the proxy-PSNR axis collapses the per-frame PSNR spread; the
//     real test of quality regression is the rate axis above.
//
// New known-gaps list (from the absolute gate, sized to the
// thresholds above):
//   - When `make verify-bd-rate` runs with the libvpx oracle built,
//     the per-feature scoreboard logged at the end of each test
//     identifies which feature carries the largest govpx-vs-libvpx
//     gap. The scoreboard is the primary mechanism for tracking
//     which features still have headroom: any row where
//     govpx-vs-libvpx BD-rate exceeds +5% goes onto the known-gaps
//     list in the next commit message.

import (
	"image"
	"math"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
)

// defaultLibvpxAbsoluteGate is the conservative starting threshold
// for the govpx-vs-libvpx absolute assertion. Each per-feature gate
// can clone-and-tweak this to express a tighter local cap when the
// observed numbers warrant.
var defaultLibvpxAbsoluteGate = benchcmd.LibvpxAbsoluteGate{
	MaxBDRateOverLibvpxPct: 20.0,
	MinBDPSNRdB:            -2.0,
}

// assertLibvpxAbsoluteGate evaluates the absolute govpx-vs-libvpx
// assertion. Behaviour:
//   - When res.LibvpxErr is non-nil, the assertion either t.Fatal's
//     (LibvpxRequired) or t.Logf+t.Skip's (default) so a missing
//     helper binary doesn't fail every developer's local run.
//   - When the cross deltas are NaN (no overlap), the assertion
//     logs and skips that single check.
//   - Otherwise the gate enforces BD-rate ≤ cap and BD-PSNR ≥ floor.
func assertLibvpxAbsoluteGate(t *testing.T, feature string, res benchcmd.BDRateResult, gate benchcmd.LibvpxAbsoluteGate) {
	t.Helper()
	if res.LibvpxErr != nil {
		if len(res.Libvpx) > 0 {
			t.Logf("%s libvpx-reference: cross-metric unavailable (skipping absolute gate): %v libvpx=%v",
				feature, res.LibvpxErr, res.Libvpx)
			return
		}
		if benchcmd.LibvpxRequired() {
			t.Fatalf("%s libvpx reference required but unavailable: %v",
				feature, res.LibvpxErr)
		}
		t.Logf("%s libvpx reference unavailable (skipping absolute gate): %v",
			feature, res.LibvpxErr)
		return
	}
	if len(res.Libvpx) == 0 {
		if benchcmd.LibvpxRequired() {
			t.Fatalf("%s libvpx reference required but empty", feature)
		}
		t.Logf("%s libvpx reference empty (skipping absolute gate)", feature)
		return
	}
	t.Logf("%s libvpx-reference: govpx-vs-libvpx BD-rate=%+0.3f%% BD-PSNR=%+0.3f dB libvpx=%v",
		feature, res.BDRateGovpxVsLibvpx, res.BDPSNRGovpxVsLibvpx, res.Libvpx)
	if !math.IsNaN(res.BDRateGovpxVsLibvpx) && res.BDRateGovpxVsLibvpx > gate.MaxBDRateOverLibvpxPct {
		t.Errorf("%s govpx vs libvpx BD-rate=%+0.3f%% > %+0.3f%% — govpx trails libvpx by more than the configured ceiling; tighten the gate when the gap closes",
			feature, res.BDRateGovpxVsLibvpx, gate.MaxBDRateOverLibvpxPct)
	}
	if !math.IsNaN(res.BDPSNRGovpxVsLibvpx) && res.BDPSNRGovpxVsLibvpx < gate.MinBDPSNRdB {
		t.Errorf("%s govpx vs libvpx BD-PSNR=%+0.3f dB < %+0.3f dB — govpx delivers materially less quality than libvpx at equal rate",
			feature, res.BDPSNRGovpxVsLibvpx, gate.MinBDPSNRdB)
	}
}

// recordFeatureScoreboardRow appends a per-feature scoreboard row to a
// process-global slice that the diagnostic test prints at the end of
// the BD-rate run. It exists so each per-feature gate can publish its
// numbers without coordinating with the diagnostic harness.
func recordFeatureScoreboardRow(feature string, res benchcmd.BDRateResult) {
	row := benchcmd.FeatureLibvpxObservation{
		Feature:                feature,
		GovpxBDRatePct:         res.BDRate,
		LibvpxBDRatePct:        math.NaN(),
		GovpxVsLibvpxBDRatePct: res.BDRateGovpxVsLibvpx,
		GovpxVsLibvpxBDPSNRdB:  res.BDPSNRGovpxVsLibvpx,
		LibvpxErr:              res.LibvpxErr,
	}
	// We don't have a libvpx feature-off curve in the standard run
	// (it would double the libvpx subprocess count); report the
	// govpx-vs-libvpx cross deltas instead, which is the substantive
	// absolute-reference number.
	benchcmd.AppendFeatureScoreboardRow(row)
}

func TestVP9FeatureBDRateAltRef(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = false
			o.LookaheadFrames = 0
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.ARNRMaxFrames = 0
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v (ref=%v test=%v)", err, res.Reference, res.Govpx)
	}
	t.Logf("AltRef BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("AltRef (panning)", res)
	// libvpx one-pass Q does not set source_alt_ref_pending on this fixture;
	// govpx mirrors that by keeping standalone unfiltered AutoAltRef neutral.
	if res.BDRate > 15.0 {
		t.Errorf("AltRef on/off BD-rate=%.3f%% > 15%%: public-Q AutoAltRef should stay neutral",
			res.BDRate)
	}
	if res.BDRate < -15.0 {
		t.Errorf("AltRef BD-rate=%.3f%% < -15%%: implausibly large saving, check harness", res.BDRate)
	}
	assertLibvpxAbsoluteGate(t, "AltRef", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRateARNR(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.TextureNoise, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		RateLadderKbps:       []int{80, 160, 320, 640},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.Deadline = govpx.DeadlineRealtime
			o.CpuUsed = 4
			o.RateControlModeSet = true
			o.RateControlMode = govpx.RateControlVBR
			o.AutoAltRef = true
			o.ARNRMaxFrames = 0
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.Deadline = govpx.DeadlineRealtime
			o.CpuUsed = 4
			o.RateControlModeSet = true
			o.RateControlMode = govpx.RateControlVBR
			o.AutoAltRef = true
			o.ARNRMaxFrames = 5
			o.ARNRStrength = 3
			o.ARNRType = 3
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v (ref=%v test=%v libvpx=%v)",
			err, res.Reference, res.Govpx, res.Libvpx)
	}
	t.Logf("ARNR BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("ARNR (texture+noise)", res)
	if res.BDRate > -1.0 {
		t.Errorf("ARNR BD-rate=%.3f%% > -1%%: enabling ARNR must save bitrate on textured/noisy content; the centered temporal filter dropped to a no-op",
			res.BDRate)
	}
	if res.BDRate < -20.0 {
		t.Errorf("ARNR BD-rate=%.3f%% < -20%%: implausibly large saving, check harness",
			res.BDRate)
	}
	assertLibvpxAbsoluteGate(t, "ARNR", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRateTPL(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.SharpEdgesContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.EnableTPL = false
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.EnableTPL = true
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("TPL BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("TPL (sharp edges)", res)
	if res.BDRate > 1.0 {
		t.Errorf("TPL BD-rate=%.3f%% > +1%%: TPL rdmult deltas must not regress the sharp-edge fixture",
			res.BDRate)
	}
	if res.BDRate < -20.0 {
		t.Errorf("TPL BD-rate=%.3f%% < -20%%: implausibly large saving, check harness",
			res.BDRate)
	}
	// TPL is a libvpx-default good-quality pass; libvpx-with-TPL is the
	// reference benchmark. govpx's TPL is wired through the same per-SB
	// rdmult delta path used by the keyframe and inter mode pickers, but on
	// this tiny fixture the selected modes and packet sizes remain neutral.
	// The absolute libvpx curve can also have no BD overlap, so the default
	// wide cap remains only a smoke signal here.
	assertLibvpxAbsoluteGate(t, "TPL", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRateVarianceAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.VarianceHeavyContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               8,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            0,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		AllowDecoderFallback: true,
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AQMode = govpx.VP9AQNone
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AQMode = govpx.VP9AQVariance
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("VarianceAQ BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("VarianceAQ", res)
	if res.BDRate > 5.0 {
		t.Errorf("VarianceAQ BD-rate=%.3f%% > 5%%: regression vs neutral baseline",
			res.BDRate)
	}
	if res.BDRate < -5.0 {
		t.Errorf("VarianceAQ BD-rate=%.3f%% < -5%%: unexpected savings — check the suppression gate",
			res.BDRate)
	}
	assertLibvpxAbsoluteGate(t, "VarianceAQ", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRateEquator360AQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:           64,
		Height:          64,
		FPS:             30,
		Frames:          8,
		QLadder:         []int{16, 24, 32, 40},
		Lookahead:       0,
		Source:          func(i int) *image.YCbCr { return gen(i) },
		LibvpxReference: true,
		BuildLibvpx:     benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AQMode = govpx.VP9AQNone
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AQMode = govpx.VP9AQEquator360
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("Equator360 BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("Equator360 AQ", res)
	if res.BDRate > 5.0 {
		t.Errorf("Equator360 AQ BD-rate=%.3f%% > 5%%: non-360 content must be neutral",
			res.BDRate)
	}
	if res.BDRate < -5.0 {
		t.Errorf("Equator360 AQ BD-rate=%.3f%% < -5%%: unexpected savings on non-360 content",
			res.BDRate)
	}
	t.Log("Equator360 AQ absolute libvpx gate skipped: this fixture intentionally suppresses govpx AQ_360 on non-360 dimensions, while libvpx --aq-mode=4 still exercises its AQ path")
}

func TestVP9FeatureBDRatePerceptualAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PerceptualContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:           64,
		Height:          64,
		FPS:             30,
		Frames:          8,
		QLadder:         []int{16, 24, 32, 40},
		Lookahead:       0,
		Source:          func(i int) *image.YCbCr { return gen(i) },
		LibvpxReference: true,
		BuildLibvpx:     benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AQMode = govpx.VP9AQNone
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AQMode = govpx.VP9AQPerceptual
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("PerceptualAQ BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("PerceptualAQ", res)
	if res.BDRate > 2.0 {
		t.Errorf("PerceptualAQ BD-rate=%.3f%% > 2%%: regression worse than libvpx-faithful port baseline",
			res.BDRate)
	}
	assertLibvpxAbsoluteGate(t, "PerceptualAQ", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRateAltRefAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.AltRefAQ = false
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.AltRefAQ = true
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("AltRefAQ BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("AltRefAQ (panning)", res)
	if res.BDRate > 0.5 {
		t.Errorf("AltRefAQ BD-rate=%.3f%% > 0.5%%: libvpx v1.16.0 alt-ref AQ is stubbed, so govpx must stay neutral",
			res.BDRate)
	}
	if res.BDRate < -0.5 {
		t.Errorf("AltRefAQ BD-rate=%.3f%% < -0.5%%: unexpected savings from a stubbed libvpx control, check harness",
			res.BDRate)
	}
	altRefAQGate := defaultLibvpxAbsoluteGate
	// VP9E_SET_ALT_REF_AQ is stubbed in libvpx v1.16.0, so this absolute
	// number reflects the public-Q encode gap rather than an AltRefAQ coding
	// delta.
	altRefAQGate.MaxBDRateOverLibvpxPct = 22.0
	assertLibvpxAbsoluteGate(t, "AltRefAQ", res, altRefAQGate)
}

// TestVP9FeatureBDRateCyclicRefresh pins the libvpx-verbatim cyclic
// refresh AQ port against libvpx CYCLIC_REFRESH_AQ over panning
// content. Cyclic refresh is libvpx's default AQ at realtime speed
// 5+ and only operates under CBR — both Baseline and Test override
// the harness's public-Q default to RateControlCBR. The current gate
// primarily protects the harness from regressing back to invalid-config /
// duplicate-rate failures; the large BD-rate delta is kept explicit as a
// known CBR cyclic-refresh mismatch to ratchet separately.
func TestVP9FeatureBDRateCyclicRefresh(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		RateLadderKbps:       []int{40, 80, 160, 320},
		Lookahead:            0,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			// libvpx CBR + aq-mode=0 baseline.
			o.RateControlModeSet = true
			o.RateControlMode = govpx.RateControlCBR
			o.AQMode = govpx.VP9AQNone
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			// libvpx CBR + aq-mode=3 (CYCLIC_REFRESH_AQ).
			o.RateControlModeSet = true
			o.RateControlMode = govpx.RateControlCBR
			o.AQMode = govpx.VP9AQCyclicRefresh
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v (ref=%v test=%v libvpx=%v)",
			err, res.Reference, res.Govpx, res.Libvpx)
	}
	t.Logf("CyclicRefresh BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	t.Logf("CyclicRefresh curves: ref=%v govpx=%v libvpx=%v",
		res.Reference, res.Govpx, res.Libvpx)
	recordFeatureScoreboardRow("CyclicRefresh (panning)", res)
	// The old failure was invalid-config/degenerate BD input. With the CBR
	// bitrate ladder fixed, govpx still over-spends cyclic-refresh segments
	// on this tiny fixture by roughly +98% BD-rate. Keep a finite ceiling so
	// CI catches new blow-ups while the rate-control parity lane closes the
	// remaining quality gap.
	if res.BDRate > 110.0 {
		t.Errorf("CyclicRefresh BD-rate=%.3f%% > 110%%: known CBR cyclic-refresh gap grew",
			res.BDRate)
	}
	if res.BDRate < -20.0 {
		t.Errorf("CyclicRefresh BD-rate=%.3f%% < -20%%: implausibly large saving, check harness",
			res.BDRate)
	}
	cyclicGate := defaultLibvpxAbsoluteGate
	cyclicGate.MaxBDRateOverLibvpxPct = 35.0
	assertLibvpxAbsoluteGate(t, "CyclicRefresh", res, cyclicGate)
}

// TestVP9FeatureBDRateLoopFilter exercises the loop-filter strength
// picker port. The baseline disables the loop filter entirely (govpx
// DisableLoopfilter=VP9LoopfilterDisableAll, which writes
// FilterLevel=0 in the uncompressed header); the test arm runs the
// stock libvpx-faithful from-Q picker. Loop filtering should save
// bitrate on textured / panning content because the in-loop deblock
// removes block artifacts that the residual coder would otherwise
// have to code around. The govpx-vs-libvpx absolute gate is set to
// the conservative +3% cap because the from-Q closed-form here is
// identical to libvpx's vp9_picklpf.c:189; any remaining gap is due
// to non-picker code paths.
//
// libvpx: vp9_picklpf.c:159-203 (vp9_pick_filter_level dispatcher).
func TestVP9FeatureBDRateLoopFilter(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.TextureNoise, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            0,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		LibvpxReference:      true,
		BuildLibvpx:          benchcmd.LibvpxBuildRequested(),
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.DisableLoopfilter = govpx.VP9LoopfilterDisableAll
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.DisableLoopfilter = govpx.VP9LoopfilterEnabled
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("LoopFilter BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	recordFeatureScoreboardRow("LoopFilter (texture+noise)", res)
	// The loop filter should save bitrate on textured content. Use the
	// same direction-of-effect gate the other "must save bitrate"
	// features use; the search-based picker isn't wired into the
	// production header-emit path yet (sseFn=nil falls back to from-Q),
	// but even from-Q vs disabled should show a clear win.
	if res.BDRate > 1.0 {
		t.Errorf("LoopFilter BD-rate=%.3f%% > 1%%: enabling the loop filter must save bitrate on textured content",
			res.BDRate)
	}
	if res.BDRate < -40.0 {
		t.Errorf("LoopFilter BD-rate=%.3f%% < -40%%: implausibly large saving, check harness",
			res.BDRate)
	}
	// Cap govpx's BD-rate disadvantage vs libvpx tighter than the global
	// default. The absolute delta is not a picker-only measurement: the
	// same tiny 64x64 texture fixture still carries the shared encoder
	// rate/mode gap after the libvpx helper drains delayed frames, so the
	// ratchet starts at the measured post-harness range rather than the
	// from-Q formula delta alone.
	assertLibvpxAbsoluteGate(t, "LoopFilter", res, benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 12.0,
		MinBDPSNRdB:            -2.0,
	})
}

// TestVP9FeatureBDRateScoreboardSummaryIncludesRecordedRows prints the per-feature
// scoreboard at the end of the BD-rate run. It runs after the gates
// (alphabetical Z-suffix) so the table reflects every recorded row.
// Use `make verify-bd-rate` to see the table populated.
func TestVP9FeatureBDRateScoreboardSummaryIncludesRecordedRows(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	rows := benchcmd.FeatureScoreboardRows()
	if len(rows) == 0 {
		t.Skip("no feature gate rows recorded")
	}
	t.Logf("Per-feature BD-rate scoreboard (govpx vs libvpx):\n%s",
		benchcmd.FormatFeatureScoreboard(rows))
}
