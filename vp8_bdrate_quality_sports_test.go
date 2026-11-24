package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"image"
	"testing"
)

func TestVP8BDRate720pSportsCBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Current measurement: govpx-vs-libvpx BD-rate=-2.662%. Keep the gate
	// slightly negative so the sports-motion path must remain ahead of
	// libvpx while allowing normal cubic-fit jitter on the short ladder.
	sportsGate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: -2.1,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p sports-motion (CBR ladder 1000/2000/4000/8000 kbps)",
		"VP8 720p sports (CBR 1000/2000/4000/8000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1000, 2000, 4000, 8000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline:       func(*govpx.EncoderOptions) {},
			Test:           func(*govpx.EncoderOptions) {},
		},
		sportsGate)
}

// TestVP8BDRate1080pStaticMotionVBR drives a 1080p
// static-then-motion fixture under a VBR ladder. The first half of the
// sequence is perfectly still (encoder should spend ~zero bits on
// inter residual), then the content suddenly translates and the rate
// controller has to absorb the motion ramp. Frame count is kept at 12
// because 1920x1080 at 4 ladder points x 2 oracles is the most
// expensive fixture here.
//
// The VBR target rungs (300/600/1200/2400 kbps) are deliberately well
// below the encoder's natural rate for synthetic 1080p content so the
// rate-control loop has room to actually distinguish them — at higher
// targets (4 Mbps+) the encoder saturates near min-q on this fixture
// and all rungs collapse to nearly identical produced rates, killing
// the BD-rate fit. Higher VBR rungs such as 2M/4M/8M/16M kbps cannot
// be honored by this synthetic fixture; the rate axis the cubic
// fit operates on is the produced rate, not the target, so the rung
// values are validation ballast as long as the actual produced rates
// span enough range for a well-conditioned fit.
//
// After the tteob==0 rate2 backout port into
// estimateInterIntraModeRDScore, the per-MB inter-vs-intra RD picker
// dropped the rate2 inflation for flat-Y inter-loop intra candidates. On
// this static-then-motion fixture the static phase has long stretches of
// flat-Y MBs where libvpx's tteob==0 backout fires; govpx now matches that
// path, and the current BD-rate measurement sits near parity at -0.849%.
// Any future regression that loses the static-phase intra-skip flow on this
// fixture should move well beyond the near-parity gate.

func TestVP8BDRate480pSportsSSIMVBR(t *testing.T) {
	const (
		width  = 854
		height = 480
		frames = 16
	)
	// Initial measurement: BD-rate=-0.339% / BD-PSNR=+0.019 dB
	// (govpx essentially at parity with libvpx on the 480p sports VBR
	// SSIM ladder). Govpx trails by less than 1% in absolute terms, so set
	// the ceiling at +1.7% (observed +|-0.339|
	// plus +1.4% headroom for cubic-fit jitter on a 16-frame VBR ladder)
	// and the BD-PSNR floor at -0.5 dB (standard).
	ssim480Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.7,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 480p sports-motion tune=ssim (VBR ladder 500/1000/2000/4000 kbps)",
		"VP8 480p sports (tune=ssim, VBR 500/1000/2000/4000)",
		benchcmd.BDRateOptionsVP8{
			Width:                  width,
			Height:                 height,
			FPS:                    30,
			Frames:                 frames,
			QLadder:                []int{16, 28, 40, 52},
			RateLadderKbps:         []int{500, 1000, 2000, 4000},
			RateControlOverride:    govpx.RateControlVBR,
			RateControlOverrideSet: true,
			Source:                 func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Tuning = govpx.TuneSSIM
				o.Deadline = govpx.DeadlineGoodQuality
			},
		},
		ssim480Gate)
}

// TestVP8BDRate1080pPanningSSIMCBR fills the larger-resolution
// SSIM-tune coverage case. The 1080p panning source has the same MV pattern as
// the 720p SSIM fixture but at 2.25x the pixel count, so cubic-fit jitter from
// activity-masking on bigger frames is observable. CBR ladder mirrors
// F5's ladder so the larger-resolution measurement is comparable.

func TestVP8BDRate720pTokenParts4CBR(t *testing.T) {
	const (
		width  = 1280
		height = 720
		frames = 12
	)
	// Current measurement: govpx-vs-libvpx BD-rate=+0.154%. The gate is a
	// near-parity guard for the 4-token-partition path; any material
	// per-partition byte-budget drift should still exceed this ceiling.
	tokenParts4Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: 1.2,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 720p sports-motion token-parts=4 (CBR ladder 1500/3000/6000/12000 kbps)",
		"VP8 720p sports-motion token-parts=4 (CBR 1500/3000/6000/12000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{1500, 3000, 6000, 12000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				// libvpx --token-parts=2 maps to TokenPartitions=2
				// which is the 4-partition VP8 layout.
				o.TokenPartitions = 2
			},
			Test: func(o *govpx.EncoderOptions) {
				o.TokenPartitions = 2
			},
		},
		tokenParts4Gate)
}

// Four fixtures broaden BD-rate coverage over the original 10-fixture set:
//
//   - F11 1080p sports CBR cpu=-3: pairs with the existing 720p sports
//     CBR fixture on a higher-resolution rung with slower-RD
//     cpu=-3, closing the high-res / slower-RD coverage gap. The
//     original set had no 1080p CBR coverage.
//   - F12 480p mixed-motion VBR: covers the bursty static<->motion
//     rate-control adaptation axis. The 1080p static-then-motion VBR
//     static-then-motion fixture has a single transition; this one alternates every
//     4 frames so the rate controller has to absorb repeated phase
//     boundaries.
//   - F13 720p RT cpu=4 CBR: fills the mid-realtime gap between the
//     cpu=8 fixture (max-speed RT) and the good-quality default
//     (cpu=0). cpu=4 sits at the libvpx "balanced" realtime preset.
//   - F14 640p denoise-heavy VBR: exercises the YUV temporal denoiser
//     (NoiseSensitivity=3 aggressive YUV denoise) + Sharpness=4
//     loop-filter tuning over a noisy sports-motion source. No earlier
//     fixture engages NoiseSensitivity or Sharpness against the libvpx
//     oracle, leaving the camera-noise / loop-filter axes unmonitored.
//     ARNR (LookaheadFrames+AutoAltRef path) is covered separately by
//     TestVP8BDRate720pARNRHeavyVBR after the BD-rate harness learned
//     to skip hidden alt-ref packets in the per-frame PSNR pairing pass
//     (PeekVP8StreamInfo show_frame check) and to pair visible packets to
//     source frames by the encoder-echoed PTS, which is robust to the
//     alt-ref scheduler's hidden/visible interleaving.

// TestVP8BDRate1080pSportsCpu3CBR drives a 1080p sports-motion
// fixture through a CBR ladder with cpu_used=-3 (slower good-quality
// preset, more RD iterations than cpu=0). Higher-resolution and
// slower-RD counterpart to the 720p sports CBR cpu=0 fixture.

func TestVP8BDRate1080pSportsCpu3CBR(t *testing.T) {
	const (
		width  = 1920
		height = 1080
		frames = 8
	)
	// Initial measurement: govpx-vs-libvpx BD-rate=-3.427% /
	// BD-PSNR=+0.176 dB on the 1080p sports CBR cpu=-3 ladder. govpx
	// beats libvpx by ~3.4%, so the gate is set negative: -2.9% (observed
	// -3.427% plus +0.5% headroom for cubic-fit jitter on the 8-frame
	// ladder). Any regression that loses the cpu=-3 slower-RD picker flow at
	// 1080p trips the gate immediately. Frame count is fixed at 8 because
	// 1080p x 4 rungs x 2 oracles is the most expensive fixture in the suite
	// (~7 min wall-clock per gate run).
	sportsCpu3Gate := benchcmd.LibvpxAbsoluteGate{
		MaxBDRateOverLibvpxPct: -2.9,
		MinBDPSNRdB:            -0.5,
	}
	runVP8BDRateFixture(t,
		"VP8 1080p sports-motion cpu=-3 (CBR ladder 2000/4000/8000/16000 kbps)",
		"VP8 1080p sports cpu=-3 (CBR 2000/4000/8000/16000)",
		benchcmd.BDRateOptionsVP8{
			Width:          width,
			Height:         height,
			FPS:            30,
			Frames:         frames,
			QLadder:        []int{16, 28, 40, 52},
			RateLadderKbps: []int{2000, 4000, 8000, 16000},
			Source:         func(i int) *image.YCbCr { return vp8test.NewSportsMotionYCbCr(width, height, i) },
			Baseline: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineGoodQuality
				o.CpuUsed = -3
			},
			Test: func(o *govpx.EncoderOptions) {
				o.Deadline = govpx.DeadlineGoodQuality
				o.CpuUsed = -3
			},
		},
		sportsCpu3Gate)
}

// TestVP8BDRate480pMixedMotionVBR drives a 480p mixed-motion
// fixture (alternating slow/fast phases every 4 frames) through a VBR
// ladder. Tests the rate controller's per-frame adaptation across
// repeated motion-energy phase boundaries, an axis the original set only
// touched once with a single static->motion transition.
