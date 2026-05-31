package vp9oracle

import (
	"slices"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

func TestNormalizedPublicQuantizersMirrorsVP9Defaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    govpx.VP9EncoderOptions
		minQ    int
		maxQ    int
		cqLevel int
	}{
		{name: "encoder defaults", minQ: 4, maxQ: 56, cqLevel: 32},
		{
			name:    "fixed quantizer becomes cq level",
			opts:    govpx.VP9EncoderOptions{MinQuantizer: 20, MaxQuantizer: 20},
			minQ:    20,
			maxQ:    20,
			cqLevel: 20,
		},
		{
			name:    "default cq clamps upward",
			opts:    govpx.VP9EncoderOptions{MinQuantizer: 40, MaxQuantizer: 56},
			minQ:    40,
			maxQ:    56,
			cqLevel: 40,
		},
		{
			name:    "explicit cq survives",
			opts:    govpx.VP9EncoderOptions{MinQuantizer: 4, MaxQuantizer: 56, CQLevel: 18},
			minQ:    4,
			maxQ:    56,
			cqLevel: 18,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			minQ, maxQ, cqLevel := NormalizedPublicQuantizers(tc.opts)
			if minQ != tc.minQ || maxQ != tc.maxQ || cqLevel != tc.cqLevel {
				t.Fatalf("NormalizedPublicQuantizers = (%d,%d,%d), want (%d,%d,%d)",
					minQ, maxQ, cqLevel, tc.minQ, tc.maxQ, tc.cqLevel)
			}
		})
	}
}

func TestNormalizeFuzzOptionsForLibvpxCLIResetsUnsupportedKnobs(t *testing.T) {
	opts := NormalizeFuzzOptionsForLibvpxCLI(govpx.VP9EncoderOptions{
		DeltaQUV:           3,
		ColorRange:         govpx.VP9ColorRangeFull,
		MinBitrateKbps:     10,
		MaxBitrateKbps:     100,
		AdaptiveKeyFrames:  true,
		AQMode:             govpx.VP9AQVariance,
		NoiseSensitivity:   4,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
	})
	if opts.DeltaQUV != 0 ||
		opts.ColorRange != govpx.VP9ColorRangeStudio ||
		opts.MinBitrateKbps != 0 ||
		opts.MaxBitrateKbps != 0 ||
		opts.AdaptiveKeyFrames ||
		opts.AQMode != govpx.VP9AQNone ||
		opts.NoiseSensitivity != 0 {
		t.Fatalf("normalized options kept unsupported oracle knobs: %+v", opts)
	}
	if opts.TargetBitrateKbps != 700 || opts.RateControlMode != govpx.RateControlCBR {
		t.Fatalf("normalized options changed comparable rate-control fields: %+v", opts)
	}
}

func TestLibvpxArgsFromOptionsIncludesEffectiveOverrides(t *testing.T) {
	args := LibvpxArgsFromOptions(govpx.VP9EncoderOptions{
		FPS:                      30,
		Threads:                  4,
		Deadline:                 govpx.DeadlineRealtime,
		RateControlModeSet:       true,
		RateControlMode:          govpx.RateControlCQ,
		TargetBitrateKbps:        700,
		MinQuantizer:             40,
		MaxQuantizer:             56,
		Log2TileRows:             1,
		AQMode:                   govpx.VP9AQNone,
		ColorSpace:               govpx.VP9ColorSpaceBT709,
		ScreenContentMode:        int8(govpx.VP9ScreenContentFilm),
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    false,
		Lossless:                 true,
		ErrorResilient:           true,
		MinKeyframeInterval:      2,
		MaxKeyframeInterval:      16,
		BufferSizeMs:             600,
		BufferInitialSizeMs:      400,
		BufferOptimalSizeMs:      500,
		UndershootPct:            20,
		OvershootPct:             30,
		MaxIntraBitratePct:       100,
		MaxInterBitratePct:       200,
		TargetLevel:              31,
	})
	for _, want := range []string{
		"--rt",
		"--end-usage=cq",
		"--min-q=40",
		"--max-q=56",
		"--cq-level=40",
		"--threads=4",
		"--target-bitrate=700",
		"--tile-rows=1",
		"--color-space=bt709",
		"--tune-content=film",
		"--frame-parallel=0",
		"--lossless=1",
		"--error-resilient=1",
		"--kf-min-dist=2",
		"--kf-max-dist=16",
		"--fps=30/1",
		"--target-level=31",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("LibvpxArgsFromOptions missing %q in %v", want, args)
		}
	}
}
