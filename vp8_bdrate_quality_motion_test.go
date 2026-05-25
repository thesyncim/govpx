package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"image"
	"testing"
)

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
