package govpx_test

// This audit pins the 480p panning VBR fixture. The zbin-aware
// motion-search errorperbit path closed the previous +0.645% govpx-vs-libvpx
// BD-rate residual; with GOVPX_BD_RATE_GATES=1 the fixture now measures
// -0.012% / +0.001 dB BD-PSNR against stock libvpx.
//
// Per-rung curves at the time of this pin, from the stock-libvpx BD-rate gate:
//
//   target=500:   govpx 1467.390 kbps vs libvpx 1467.390 kbps (Δ +0.00)
//   target=1000:  govpx 2155.485 kbps vs libvpx 2155.485 kbps (Δ +0.00)
//   target=2000:  govpx 2382.780 kbps vs libvpx 2382.780 kbps (Δ +0.00)
//   target=4000:  govpx 2427.570 kbps vs libvpx 2427.570 kbps (Δ +0.00)
//
// Per-frame govpx pin at target=1000 kbps. This cheap default-suite audit
// catches drift in the same path without requiring libvpx binaries:
//
//   f# size_govpx iQ_govpx
//   0  127426     4
//   1  189        127
//   2  1158       13
//   3  610        21
//   4  515        19
//   5  1105       16
//   6  911        16
//   7  1066       15
//   8  1016       15
//   9  1750       13
//   10 982        14
//   11 1392       12
//   12 1016       12
//   13 1163       11
//   14 1550       9
//   15 1216       9
//
// Root-cause localization:
//
//   1. The stale +0.645% pin was not a VBR regulator problem. The first
//      same-Q divergence was upstream in the inter MB path.
//
//   2. The VP8 fast NEWMV search was using no-zbin errorperbit for the
//      full-pel return score and subpel refinement. Libvpx's
//      vp8_initialize_rd_consts includes cpi->mb.zbin_over_quant in
//      x->errorperbit, so low-bitrate/high-zbin frames over-valued distant
//      motion candidates in govpx.
//
//   3. Carrying currentZbinOverQuant into tunedErrorPerBit aligns that MV
//      search scoring with libvpx and removes the downstream VBR Q drift.
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
	{frame: 3, govpxSize: 610, govpxIQ: 21},
	{frame: 4, govpxSize: 515, govpxIQ: 19},
	{frame: 5, govpxSize: 1105, govpxIQ: 16},
	{frame: 6, govpxSize: 911, govpxIQ: 16},
	{frame: 7, govpxSize: 1066, govpxIQ: 15},
	{frame: 8, govpxSize: 1016, govpxIQ: 15},
	{frame: 9, govpxSize: 1750, govpxIQ: 13},
	{frame: 10, govpxSize: 982, govpxIQ: 14},
	{frame: 11, govpxSize: 1392, govpxIQ: 12},
	{frame: 12, govpxSize: 1016, govpxIQ: 12},
	{frame: 13, govpxSize: 1163, govpxIQ: 11},
	{frame: 14, govpxSize: 1550, govpxIQ: 9},
	{frame: 15, govpxSize: 1216, govpxIQ: 9},
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
	const wantKbps = 2145.975
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
