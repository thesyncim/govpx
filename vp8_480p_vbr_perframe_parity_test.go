package govpx_test

// This audit pins the 480p panning VBR fixture. At
// GOVPX_BD_RATE_GATES=1 the fixture's absolute govpx-vs-
// libvpx BD-rate measures +0.645% / -0.043 dB BD-PSNR — the smallest
// remaining positive in the 10-fixture cohort and the only positive
// fixture in that cohort (the others either match or beat libvpx).
//
// Per-rung curves at the time of this pin:
//
//   target=500:   govpx 1465.785 kbps vs libvpx 1463.340 kbps (Δ +2.45)
//   target=1000:  govpx 2187.000 kbps vs libvpx 2150.685 kbps (Δ +36.32)  <-- dominant
//   target=2000:  govpx 2393.280 kbps vs libvpx 2382.780 kbps (Δ +10.50)
//   target=4000:  govpx 2439.930 kbps vs libvpx 2427.570 kbps (Δ +12.36)
//
// Per-frame bisect at the dominant rung (target=1000 kbps), comparing
// emitted IVF frame sizes and decoded base_qindex (libvpx via vpxdec
// --framestats, govpx via EncodeResult.InternalQuantizer):
//
//   f# size_govpx size_libvpx Δ      iQ_govpx iQ_libvpx note
//   0  127426     127426      +0     4        4         keyframe — byte-identical
//   1  189        189         +0     127      127       inter Q=63 (max) — byte-identical
//   2  1158       1158        +0     13       13        inter — byte-identical
//   3  730        689         +41    17       17        first divergence at SAME Q
//   4  970        1145        -175   16       15        govpx picks higher Q
//   5  1137       971         +166   16       16        same Q, govpx +166 bytes
//   6  1004       1091        -87    16       16        same Q
//   7  1232       743         +489   12       18        govpx picks Q=12 vs libvpx Q=18
//   8  1423       1081        +342   10       18        Q regulator divergence widens
//   9  1771       1077        +694   10       16
//   10 1107       1101        +6     10       14
//   11 1718       1496        +222   9        13
//   12 1165       1002        +163   10       13
//   13 1522       1576        -54    9        11
//   14 1125       1238        -113   11       11
//   15 2123       1396        +727   8        10
//
// Root-cause localization:
//
//   1. Frames 0-2 are byte-identical between govpx and libvpx (matching
//      sizes AND matching internal qindex), confirming the keyframe and
//      first two inter frames are byte-exact at every level — quantizer
//      regulator, MB mode picker, motion search, and coef coding all
//      agree through f2.
//
//   2. Frame 3 is the FIRST divergent frame. Both encoders pick
//      iQ=17 from the same rate-control state, but the packed bitstream
//      sizes differ by +41 bytes (govpx 730 vs libvpx 689). With same Q,
//      same reference frames (f2 was byte-identical), and same source,
//      this gap can only come from an MB-level decision divergence (mode
//      picker, MV search, or coef/trellis), not from rate control.
//
//   3. The downstream Q drift (f4, f7, f8 picking lower Q than libvpx)
//      is a CONSEQUENCE of (2): the +41-byte overshoot at f3 perturbs
//      bits_off_target, rate_correction_factor, and the buffer-level
//      shrink/grow branch of calc_pframe_target_size. By f7 the two
//      regulators have diverged enough that govpx picks iQ=12 vs
//      libvpx's iQ=18 — a 6-step gap that costs ~500 bytes per frame.
//
//   4. The same-Q +41-byte gap at f3 is consistent with the existing
//      ARNR chroma-trellis divergence: an inter-frame MB-decision gap
//      at high-Q boundary conditions. The 480p panning VBR fixture
//      exposes the same gap through the VBR regulator's bigger downstream
//      amplification, but the underlying MB divergence is upstream of
//      rate control.
//
// Closing this fixture's +0.645% therefore requires resolving the
// existing same-Q-bytes-differ ARNR pin-hold, not VBR rate-control
// tuning. The harness sees the gap via the cubic-fit projection at
// the 4-rung ladder; any same-Q MB-decision narrowing on the ARNR
// chain will close it without touching rate control.
//
// The libvpx side of this fixture is enforced by the VP8 BD-rate gate.
// This file keeps the cheap govpx-side per-frame pin in the default suite.

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil"
)

// vp8480pVBRPerFrameRow pins one (frame_index, govpx_size, govpx_iQ) row
// from the per-frame bisect at target=1000 kbps. The govpx side is the
// authoritative pin because the libvpx side requires vpxenc/vpxdec
// binaries on PATH; the libvpx values are documented in the file
// header and re-derived from --framestats when the binary is wired up.
type vp8480pVBRPerFrameRow struct {
	frame     int
	govpxSize int
	govpxIQ   int
}

// vp8480pVBR1000KbpsPin pins the govpx side of the per-frame bisect at
// target=1000 kbps. Any drift here (size or Q) flags either an
// improvement (narrowing toward the libvpx column in the header table)
// or a regression, both of which warrant a fresh pin/audit.
var vp8480pVBR1000KbpsPin = []vp8480pVBRPerFrameRow{
	{frame: 0, govpxSize: 127426, govpxIQ: 4},
	{frame: 1, govpxSize: 189, govpxIQ: 127},
	{frame: 2, govpxSize: 1158, govpxIQ: 13},
	{frame: 3, govpxSize: 730, govpxIQ: 17},
	{frame: 4, govpxSize: 970, govpxIQ: 16},
	{frame: 5, govpxSize: 1137, govpxIQ: 16},
	{frame: 6, govpxSize: 1004, govpxIQ: 16},
	{frame: 7, govpxSize: 1232, govpxIQ: 12},
	{frame: 8, govpxSize: 1423, govpxIQ: 10},
	{frame: 9, govpxSize: 1771, govpxIQ: 10},
	{frame: 10, govpxSize: 1107, govpxIQ: 10},
	{frame: 11, govpxSize: 1718, govpxIQ: 9},
	{frame: 12, govpxSize: 1165, govpxIQ: 10},
	{frame: 13, govpxSize: 1522, govpxIQ: 9},
	{frame: 14, govpxSize: 1125, govpxIQ: 11},
	{frame: 15, govpxSize: 2123, govpxIQ: 8},
}

func runVP8Panning480pGovpx(t *testing.T, target int) (sizes []int, internalQs []int, kbps float64) {
	t.Helper()
	const (
		width  = 854
		height = 480
		frames = 16
		fps    = 30
		minQ   = 4
		maxQ   = 63
	)
	opts := govpx.EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		TargetBitrateKbps: target,
		MinQuantizer:      minQ,
		MaxQuantizer:      maxQ,
		QuantizerRangeSet: true,
		CQLevel:           28, // ignored by VBR; set for parity with the harness
		RateControlMode:   govpx.RateControlVBR,
	}
	enc, err := govpx.NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	bufSize := width * height * 6
	dst := make([]byte, bufSize)
	totalBytes := 0
	for i := range frames {
		src := testutil.NewTexturedPanningYCbCr(width, height, i)
		result, err := enc.EncodeInto(dst, govpxImageFromVBRPanningYCbCr(src), uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		sizes = append(sizes, result.SizeBytes)
		internalQs = append(internalQs, result.InternalQuantizer)
		totalBytes += result.SizeBytes
	}
	kbps = float64(totalBytes) * 8 * float64(fps) / float64(frames) / 1000.0
	return sizes, internalQs, kbps
}

// TestVP8VBR480pPerFrameParity verifies the govpx-side per-
// frame (size, iQ) pin at the target=1000 kbps rung. Any deviation
// flags either an MB-decision improvement that's narrowing the +0.645%
// BD-rate gap (good — re-pin to the new values and confirm the gate
// tightens) or a regression (investigate before merging). This test
// does NOT require libvpx binaries and runs by default.
func TestVP8VBR480pPerFrameParity(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	sizes, iqs, kbps := runVP8Panning480pGovpx(t, 1000)
	if len(sizes) != len(vp8480pVBR1000KbpsPin) {
		t.Fatalf("got %d frames, want %d", len(sizes), len(vp8480pVBR1000KbpsPin))
	}
	const wantKbps = 2187.0
	const kbpsTol = 0.5
	if diff := kbps - wantKbps; diff > kbpsTol || diff < -kbpsTol {
		t.Errorf("target=1000 govpx kbps=%.3f want %.3f (tol ±%.1f)", kbps, wantKbps, kbpsTol)
	}
	for _, row := range vp8480pVBR1000KbpsPin {
		if sizes[row.frame] != row.govpxSize {
			t.Errorf("f%d govpx size=%d want %d (re-pin if narrowing toward libvpx)",
				row.frame, sizes[row.frame], row.govpxSize)
		}
		if iqs[row.frame] != row.govpxIQ {
			t.Errorf("f%d govpx iQ=%d want %d (re-pin if narrowing toward libvpx)",
				row.frame, iqs[row.frame], row.govpxIQ)
		}
	}
}

func govpxImageFromVBRPanningYCbCr(src *image.YCbCr) govpx.Image {
	return govpx.Image{
		Width:   src.Rect.Dx(),
		Height:  src.Rect.Dy(),
		Y:       src.Y,
		U:       src.Cb,
		V:       src.Cr,
		YStride: src.YStride,
		UStride: src.CStride,
		VStride: src.CStride,
	}
}
