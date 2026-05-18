package govpx_test

// VP8 BD-rate quality gate. Mirrors the VP9 per-feature BD-rate
// scoreboard in feature_quality_gates_test.go but drives the VP8
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
	"math/rand"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
)

// defaultLibvpxVP8AbsoluteGate is the absolute govpx-vs-libvpx VP8
// BD-rate gate threshold. Set wide enough to absorb cubic-fit jitter
// on the synthetic 64x64 short-ladder fixtures while tight enough to
// catch a real ~10% bitrate regression. Tighten when the gap narrows.
var defaultLibvpxVP8AbsoluteGate = benchcmd.LibvpxAbsoluteGate{
	MaxBDRateOverLibvpxPct: 5.0,
	MinBDPSNRdB:            -0.5,
}

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

// makeVP8BDFrame builds a textured, slowly-translating 4:2:0 frame so
// the VP8 encoder has real inter-prediction work to do across the
// short ladder. Using a fixture local to this test keeps the gate
// independent of the VP9-specific generators in benchcmd
// (FeatureGateGenerator), which are tuned for VP9 feature paths.
func makeVP8BDFrame(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	r := rand.New(rand.NewSource(int64(idx) + 7919))
	shift := idx * 2
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			base := ((x + shift) ^ (y * 5)) & 0xFF
			// Per-frame deterministic noise so the encoder has
			// realistic content (textured + translating).
			noise := r.Intn(33) - 16
			v := min(max(base+noise, 0), 255)
			row[x] = byte(v)
		}
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			cb[x] = byte(128 + ((x+idx)*3)&0x3F)
			cr[x] = byte(128 + ((y+idx*2)*5)&0x3F)
		}
	}
	return img
}

// TestVP8FeatureBDRateBaseline is the headline VP8 BD-rate gate. It
// drives two encode passes — Baseline = stock VP8 encoder, Test =
// stock VP8 encoder — at a 4-point CBR ladder, computes the
// within-govpx BD-rate (which must be near zero because the configs
// are identical: this is the wiring-regression smoke), and then
// compares govpx-VP8 against libvpx-VP8 on the same ladder.
//
// The libvpx absolute gate is the substantive assertion: govpx VP8 is
// byte-exact against libvpx for the matching configuration, so the
// BD-rate cubic-fit difference must sit within a few percent.
func TestVP8FeatureBDRateBaseline(t *testing.T) {
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
	res, err := benchcmd.ComputeBDRateVP8(t, benchcmd.BDRateOptionsVP8{
		Width:           width,
		Height:          height,
		FPS:             30,
		Frames:          frames,
		QLadder:         []int{16, 28, 40, 52},
		RateLadderKbps:  []int{100, 200, 400, 800},
		Source:          func(i int) *image.YCbCr { return makeVP8BDFrame(width, height, i) },
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
	assertLibvpxVP8AbsoluteGate(t, "VP8 baseline (QCIF, CBR ladder 100/200/400/800 kbps)", res, defaultLibvpxVP8AbsoluteGate)
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
