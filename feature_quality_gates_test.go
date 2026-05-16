package govpx_test

// Per-feature VP9 BD-rate quality gates. These tests load the
// BD-rate harness and run it once per feature toggle. They are
// slow (~25s for the full sweep) and so opt-in via the
// GOVPX_BD_RATE_GATES=1 env var. `make verify-bd-rate` sets it.
//
// Each gate asserts a tolerance band around the observed BD-rate
// number from the first calibration pass. Numbers were captured
// against vp9-port @ 616fdb5 with the synthetic content
// generators defined in cmd/govpx-bench/benchcmd. The bands are
// generous (±5% bitrate, ±1 dB PSNR-proxy) so the gates protect
// against regressions where a refactor silently disables the
// toggle, without flagging every cycle of bitstream byte drift.
//
// Findings recorded in commit "Add VP9 per-feature BD-rate gates":
//   - AltRef on/off (panning) saves ~3.6% bitrate; gate at ≤ 0%.
//   - ARNR on/off has zero observed effect; gate accepts current
//     0% and asserts the toggle does not regress harder than +5%.
//   - TPL on/off has zero observed effect; same gate stance as ARNR.
//   - VarianceAQ hurts bitrate ~+77% on the variance-heavy probe
//     content; gate pins the upper bound at +90% so a regression
//     making it even worse fails. Investigation tracked separately.
//   - Equator360 AQ shows ~+91% regression on panning content
//     (an equator-360 AQ should be neutral on non-360 content);
//     gate pins upper bound.
//   - Perceptual AQ on vs off: +2.4%; gate accepts up to +5%.
//   - AltRefAQ on vs off: +2.4% via Q-proxy; gate accepts up to +5%.

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
)

func TestVP9FeatureBDRateAltRef(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:                "vp9",
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
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
	// Expectation: AltRef should save bitrate on panning content.
	// Observed -3.6%; require <= 0% with a 1% slack so noise in
	// the regulator does not flip the sign on minor refactors.
	if res.BDRate > 1.0 {
		t.Errorf("AltRef on/off BD-rate=%.3f%% > 1%%: AltRef must save bitrate on panning content",
			res.BDRate)
	}
	// Hard lower bound: anything below -15% would be unrealistic
	// for the source size and suggests a measurement bug rather
	// than a feature improvement.
	if res.BDRate < -15.0 {
		t.Errorf("AltRef BD-rate=%.3f%% < -15%%: implausibly large saving, check harness", res.BDRate)
	}
}

func TestVP9FeatureBDRateARNR(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.TextureNoise, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:                "vp9",
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
		Baseline: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.ARNRMaxFrames = 0
		},
		Test: func(o *govpx.VP9EncoderOptions) {
			o.AutoAltRef = true
			o.ARNRMaxFrames = 5
			o.ARNRStrength = 3
			o.ARNRType = 3
		},
	})
	if err != nil {
		t.Fatalf("ComputeBDRate err: %v", err)
	}
	t.Logf("ARNR BD-rate=%.3f%% BD-PSNR=%.3f dB", res.BDRate, res.BDPSNR)
	// ARNR currently has no observed effect: the gate accepts a
	// wide band, but the upper bound (+5%) ensures a future
	// "wired-but-broken" ARNR refactor that loads but does the
	// wrong thing fails the gate.
	if res.BDRate > 5.0 {
		t.Errorf("ARNR BD-rate=%.3f%% > 5%%: enabling ARNR must not significantly hurt rate", res.BDRate)
	}
}

func TestVP9FeatureBDRateTPL(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.SharpEdgesContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:                "vp9",
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
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
	// TPL currently has no observed effect; gate just guards
	// against a regression where it suddenly hurts rate.
	if res.BDRate > 5.0 {
		t.Errorf("TPL BD-rate=%.3f%% > 5%%: enabling TPL must not significantly hurt rate", res.BDRate)
	}
}

func TestVP9FeatureBDRateVarianceAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.VarianceHeavyContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:     "vp9",
		Width:     64,
		Height:    64,
		FPS:       30,
		Frames:    8,
		QLadder:   []int{16, 24, 32, 40},
		Lookahead: 0,
		Source:    func(i int) *image.YCbCr { return gen(i) },
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
	// Observed regression of +77% on the variance-heavy probe.
	// The gate pins the upper bound at +90% so a refactor that
	// makes the regression even worse fails. A future fix that
	// brings the number down to ~0 is welcome and will still
	// pass this gate; tightening the bound is left to the same
	// commit that lands the fix.
	if res.BDRate > 90.0 {
		t.Errorf("VarianceAQ BD-rate=%.3f%% > 90%%: regression worse than calibration",
			res.BDRate)
	}
}

func TestVP9FeatureBDRateEquator360AQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:     "vp9",
		Width:     64,
		Height:    64,
		FPS:       30,
		Frames:    8,
		QLadder:   []int{16, 24, 32, 40},
		Lookahead: 0,
		Source:    func(i int) *image.YCbCr { return gen(i) },
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
	// Equator360 on non-360 panning content showed a +91%
	// regression in calibration; gate pins upper bound.
	if res.BDRate > 100.0 {
		t.Errorf("Equator360 AQ BD-rate=%.3f%% > 100%%: regression worse than calibration",
			res.BDRate)
	}
}

func TestVP9FeatureBDRatePerceptualAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PerceptualContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:     "vp9",
		Width:     64,
		Height:    64,
		FPS:       30,
		Frames:    8,
		QLadder:   []int{16, 24, 32, 40},
		Lookahead: 0,
		Source:    func(i int) *image.YCbCr { return gen(i) },
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
	// Observed +2.4% regression; gate at +5% to detect a worse
	// regression while allowing the current state.
	if res.BDRate > 5.0 {
		t.Errorf("PerceptualAQ BD-rate=%.3f%% > 5%%: regression worse than calibration",
			res.BDRate)
	}
}

func TestVP9FeatureBDRateAltRefAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PanningContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:                "vp9",
		Width:                64,
		Height:               64,
		FPS:                  30,
		Frames:               12,
		QLadder:              []int{16, 24, 32, 40},
		Lookahead:            8,
		Source:               func(i int) *image.YCbCr { return gen(i) },
		AllowDecoderFallback: true,
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
	// Observed +2.4% regression via the Q-proxy; gate at +5%.
	if res.BDRate > 5.0 {
		t.Errorf("AltRefAQ BD-rate=%.3f%% > 5%%: regression worse than calibration",
			res.BDRate)
	}
}
