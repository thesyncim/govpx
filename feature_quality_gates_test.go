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
//   - AltRef on/off (panning) saves ~3.6% bitrate within govpx; gate
//     at ≤ 0%.
//   - ARNR on/off saves ~1.4% bitrate on textured/noisy content
//     within govpx; gate requires ≤ -1%.
//   - TPL on/off saves ~1.1% bitrate on sharp-edge content within
//     govpx; gate at ≤ -1% (must save bitrate) with -20% sanity
//     floor.
//   - VarianceAQ neutral (≤ ±5%) within govpx in pure-Q.
//   - Equator360 AQ neutral on non-360 (≤ ±5%) within govpx.
//   - Perceptual AQ on vs off: +1.524% post-libvpx-verbatim-port on
//     PerceptualContent; gate at ≤ +2.0%.
//   - AltRefAQ on vs off: -0.7% post-fix.
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
		Codec:                "vp9",
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
	// Expectation: AltRef should save bitrate on panning content.
	// Observed -3.6%; require <= 0% with a 1% slack so noise in the
	// regulator does not flip the sign on minor refactors.
	if res.BDRate > 1.0 {
		t.Errorf("AltRef on/off BD-rate=%.3f%% > 1%%: AltRef must save bitrate on panning content",
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
		Codec:                "vp9",
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
		Codec:                "vp9",
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
	if res.BDRate > -1.0 {
		t.Errorf("TPL BD-rate=%.3f%% > -1%%: TPL must save bitrate on sharp-edge content",
			res.BDRate)
	}
	if res.BDRate < -20.0 {
		t.Errorf("TPL BD-rate=%.3f%% < -20%%: implausibly large saving, check harness",
			res.BDRate)
	}
	// TPL is a libvpx-default good-quality pass; libvpx-with-TPL is
	// the reference benchmark. govpx's TPL implementation today only
	// applies a scalar frame-mean qindex bias (per-SB segmentation
	// routing is in flight); the absolute gap to libvpx-with-TPL is
	// therefore wider than the within-govpx gap. Use the default
	// 20% cap for now; the gate will be ratcheted as per-SB routing
	// lands.
	assertLibvpxAbsoluteGate(t, "TPL", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRateVarianceAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.VarianceHeavyContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:           "vp9",
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
		Codec:           "vp9",
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
	assertLibvpxAbsoluteGate(t, "Equator360", res, defaultLibvpxAbsoluteGate)
}

func TestVP9FeatureBDRatePerceptualAQ(t *testing.T) {
	if !benchcmd.FeatureGatesEnabled() {
		t.Skip("GOVPX_BD_RATE_GATES=1 not set")
	}
	gen := benchcmd.FeatureGateGenerator(benchcmd.PerceptualContent, 64, 64)
	res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
		Codec:           "vp9",
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
		Codec:                "vp9",
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
	if res.BDRate > -0.5 {
		t.Errorf("AltRefAQ BD-rate=%.3f%% > -0.5%%: AltRefAQ must save bitrate",
			res.BDRate)
	}
	if res.BDRate < -10.0 {
		t.Errorf("AltRefAQ BD-rate=%.3f%% < -10%%: implausibly large saving, check harness",
			res.BDRate)
	}
	assertLibvpxAbsoluteGate(t, "AltRefAQ", res, defaultLibvpxAbsoluteGate)
}

// TestVP9FeatureBDRateScoreboardSummary prints the per-feature
// scoreboard at the end of the BD-rate run. It runs after the gates
// (alphabetical Z-suffix) so the table reflects every recorded row.
// Use `make verify-bd-rate` to see the table populated.
func TestVP9FeatureBDRateZScoreboardSummary(t *testing.T) {
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
