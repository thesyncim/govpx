package govpx_test

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
)

// TestVP9PerceptualAQBDRateContentSweep is an always-on (under -short
// it is skipped) BD-rate diagnostic for the libvpx-faithful Perceptual
// AQ port. It iterates the synthetic content classes the BD-rate gate
// suite uses so reviewers can re-derive the post-port numbers without
// instrumenting the gate tests. Unlike the gate test, this one does
// not gate on GOVPX_BD_RATE_GATES so a single `go test -run
// TestVP9PerceptualAQBDRateContentSweep` is enough to read the BD-rate
// for each content class.
//
// Numbers from this test feed the libvpx-port commit message and the
// new gate threshold. The expectation per the project rule against
// hand-tuned magic clamps is: the libvpx-faithful port performs
// neutrally-or-better than the previous hand-rolled cluster-0-anchored
// implementation on the BD-rate gate content, and rate-saving on
// perceptually-masked content classes specifically.
func TestVP9PerceptualAQBDRateContentSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BD-rate diagnostic under -short")
	}
	t.Parallel()
	type scenario struct {
		name    string
		content benchcmd.FeatureGateContent
	}
	scenarios := []scenario{
		{"PerceptualContent", benchcmd.PerceptualContent},
		{"VarianceHeavyContent", benchcmd.VarianceHeavyContent},
		{"TextureNoise", benchcmd.TextureNoise},
		{"SharpEdgesContent", benchcmd.SharpEdgesContent},
		{"PanningContent", benchcmd.PanningContent},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			gen := benchcmd.FeatureGateGenerator(sc.content, 64, 64)
			res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
				Codec:                "vp9",
				Width:                64,
				Height:               64,
				FPS:                  30,
				Frames:               8,
				QLadder:              []int{16, 24, 32, 40},
				Lookahead:            0,
				AllowDecoderFallback: true,
				Source: func(i int) *image.YCbCr {
					return gen(i)
				},
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
			t.Logf("%s PerceptualAQ BD-rate=%.3f%% BD-PSNR=%.3f dB",
				sc.name, res.BDRate, res.BDPSNR)
		})
	}
}

// TestVP9PerceptualAQBDRateLargerFrameSweep mirrors the content sweep
// but at a 256x256 frame size where each frame holds 16 BLOCK_64X64
// SBs — enough for k-means with k=8 to be statistically meaningful
// (the 64x64 path has a single SB and degrades to the
// fewer-SBs-than-clusters fallback). This is the size the BD-rate
// threshold should be calibrated against.
func TestVP9PerceptualAQBDRateLargerFrameSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BD-rate diagnostic under -short")
	}
	t.Parallel()
	type scenario struct {
		name    string
		content benchcmd.FeatureGateContent
	}
	scenarios := []scenario{
		{"PerceptualContent256", benchcmd.PerceptualContent},
		{"VarianceHeavyContent256", benchcmd.VarianceHeavyContent},
		{"TextureNoise256", benchcmd.TextureNoise},
		{"SharpEdgesContent256", benchcmd.SharpEdgesContent},
		{"PanningContent256", benchcmd.PanningContent},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			gen := benchcmd.FeatureGateGenerator(sc.content, 256, 256)
			res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
				Codec:                "vp9",
				Width:                256,
				Height:               256,
				FPS:                  30,
				Frames:               4,
				QLadder:              []int{16, 24, 32, 40},
				Lookahead:            0,
				AllowDecoderFallback: true,
				Source: func(i int) *image.YCbCr {
					return gen(i)
				},
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
			t.Logf("%s PerceptualAQ BD-rate=%.3f%% BD-PSNR=%.3f dB",
				sc.name, res.BDRate, res.BDPSNR)
		})
	}
}
