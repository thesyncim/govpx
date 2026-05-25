package govpx_test

// VP8 BD-rate quality gates. These tests drive the VP8 encoder via
// the BD-rate VP8 harness in cmd/govpx-bench/benchcmd/bdrate_vp8.go.
//
// Two checks fire per call:
//
//  1. Within-govpx case-vs-baseline BD-rate stays inside a sane
//     tolerance band, catching broken option plumbing and rate-control
//     regressions.
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
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
	"math"
	"testing"
)

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
