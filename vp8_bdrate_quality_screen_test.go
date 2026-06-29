package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"image"
	"testing"
)

func TestVP8BDRate360pScreenContentSSIMCBR(t *testing.T) {
	const (
		width  = 640
		height = 360
		frames = 16
	)
	// Initial measurement: BD-rate=+2.668% / BD-PSNR=+0.070 dB
	// (govpx trails libvpx on the 360p screen-content tune=ssim CBR
	// ladder). Keep the ceiling at +4.7% (observed +2.668% plus +2.0%
	// headroom for cubic-fit jitter on the sparse screen-content rate axis)
	// until this SSIM-tune sibling gets the same retightening pass as the
	// 720p PSNR-tune fixture.
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
