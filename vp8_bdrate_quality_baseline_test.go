package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
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
