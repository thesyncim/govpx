package govpx_test

import (
	"image"
	"os"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/cmd/govpx-bench/benchcmd"
)

// vp9PerceptualAQBDRateFullSweepEnv gates the 256x256 BD-rate diagnostic
// sweep. The default 128x128 fixture finishes under 60s on purego (no
// SIMD); the 256x256 fixture needs a SIMD build (or a generous walltime
// budget) to complete inside the standard 10-minute test timeout.
const vp9PerceptualAQBDRateFullSweepEnv = "GOVPX_VP9_AQ_BDRATE_FULL_SWEEP"

// vp9PerceptualAQBDRateLargerSweepDim returns the frame dimension used by
// TestVP9PerceptualAQBDRateLargerFrameSweep. Defaults to 128 (one
// BLOCK_64X64 SB per axis, four SBs per frame — still enough for the
// k-means classifier to exercise k=8 fallback paths since 4 < 8). Setting
// GOVPX_VP9_AQ_BDRATE_FULL_SWEEP=1 raises the dimension back to 256 so
// reviewers can rerun the original calibration sweep on SIMD builds.
func vp9PerceptualAQBDRateLargerSweepDim() int {
	if os.Getenv(vp9PerceptualAQBDRateFullSweepEnv) == "1" {
		return 256
	}
	return 128
}

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
// but at a larger frame size where each frame holds multiple
// BLOCK_64X64 SBs — enough for k-means with k=8 to exercise the
// fewer-SBs-than-clusters fallback in the libvpx-faithful AQ port.
//
// Defaults to a 128x128 fixture (4 SBs / frame) so it runs inside the
// 10-minute purego CI budget. Setting GOVPX_VP9_AQ_BDRATE_FULL_SWEEP=1
// restores the original 256x256 calibration sweep for SIMD-builds
// where the encoder is fast enough to finish 32 encodes inside the
// timeout. The 256x256 size is the one the BD-rate threshold was
// originally calibrated against, but the BD-rate signal direction is
// preserved at 128x128 (verified empirically and by the surrounding
// quality gates).
func TestVP9PerceptualAQBDRateLargerFrameSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BD-rate diagnostic under -short")
	}
	t.Parallel()
	dim := vp9PerceptualAQBDRateLargerSweepDim()
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
			gen := benchcmd.FeatureGateGenerator(sc.content, dim, dim)
			res, err := benchcmd.ComputeBDRate(t, benchcmd.BDRateOptions{
				Width:                dim,
				Height:               dim,
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
			t.Logf("%s%d PerceptualAQ BD-rate=%.3f%% BD-PSNR=%.3f dB",
				sc.name, dim, res.BDRate, res.BDPSNR)
		})
	}
}
