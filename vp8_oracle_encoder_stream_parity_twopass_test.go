//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	"testing"
)

func TestVP8OracleEncoderStreamByteParityTwoPassEndToEnd(t *testing.T) {
	vp8test.RequireOracle(t, "two-pass stream byte-parity gate")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		width      = 32
		height     = 32
		fps        = 30
		targetKbps = 400
		frames     = 8
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = firstPassOracleRampFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
	}
	encodeTwoPass := func(name string, caseOpts EncoderOptions, caseSources []Image) ([][]byte, [][]byte) {
		t.Helper()
		govpxOpts := caseOpts
		govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, caseOpts, caseSources)
		govpxFrames := encodeFramesWithGovpx(t, govpxOpts, caseSources)
		libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, name, caseOpts, caseOpts.TargetBitrateKbps, caseSources)
		return govpxFrames, libvpxFrames
	}

	govpxOpts := opts
	govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, opts, sources)
	govpxFrames := encodeFramesWithGovpx(t, govpxOpts, sources)
	libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-ramp", opts, targetKbps, sources)
	assertSegmentByteParity(t, "twopass-e2e", govpxFrames, libvpxFrames, 0)

	setterGovpxFrames := encodeFramesWithGovpxTwoPassStatsSetter(t, opts, govpxOpts.TwoPassStats, sources, false)
	assertSegmentByteParity(t, "twopass-e2e-setter-vs-options", setterGovpxFrames, govpxFrames, 0)
	assertSegmentByteParity(t, "twopass-e2e-setter", setterGovpxFrames, libvpxFrames, 0)

	disabledGovpxFrames := encodeFramesWithGovpxTwoPassStatsSetter(t, govpxOpts, nil, sources, true)
	onePassGovpxFrames := encodeFramesWithGovpx(t, opts, sources)
	disabledLibvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, "twopass-e2e-disabled-before-frame0", opts, targetKbps, sources, []string{"--end-usage=vbr"})
	assertSegmentByteParity(t, "twopass-e2e-disabled-vs-one-pass-govpx", disabledGovpxFrames, onePassGovpxFrames, 0)
	assertSegmentByteParity(t, "twopass-e2e-disabled-before-frame0", disabledGovpxFrames, disabledLibvpxFrames, 0)

	sectionOpts := opts
	sectionOpts.TwoPassVBRBiasPct = 80
	sectionOpts.TwoPassMinPct = 50
	sectionOpts.TwoPassMaxPct = 200
	sectionGovpxOpts := sectionOpts
	sectionGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, sectionOpts, sources)
	sectionGovpxFrames := encodeFramesWithGovpx(t, sectionGovpxOpts, sources)
	sectionLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-ramp-sections", sectionOpts, targetKbps, sources)
	assertSegmentByteParity(t, "twopass-e2e-sections", sectionGovpxFrames, sectionLibvpxFrames, 0)

	panningSources := makePanningSources(64, 64, frames, 0)
	panningOpts := opts
	panningOpts.Width = 64
	panningOpts.Height = 64
	panningOpts.TargetBitrateKbps = 700
	panningGovpxOpts := panningOpts
	panningGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, panningOpts, panningSources)
	panningGovpxFrames := encodeFramesWithGovpx(t, panningGovpxOpts, panningSources)
	panningLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-panning64", panningOpts, panningOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-panning64", panningGovpxFrames, panningLibvpxFrames, 0)

	kf4Opts := opts
	kf4Opts.KeyFrameInterval = 4
	kf4GovpxOpts := kf4Opts
	kf4GovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, kf4Opts, sources)
	kf4GovpxFrames := encodeFramesWithGovpx(t, kf4GovpxOpts, sources)
	kf4LibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-kf4", kf4Opts, targetKbps, sources)
	assertSegmentByteParity(t, "twopass-e2e-kf4", kf4GovpxFrames, kf4LibvpxFrames, 0)

	segmentedSources := make([]Image, frames)
	for i := range segmentedSources {
		segmentedSources[i] = encoderValidationSegmentedFrame(64, 64, i)
	}
	segmentedOpts := opts
	segmentedOpts.Width = 64
	segmentedOpts.Height = 64
	segmentedOpts.TargetBitrateKbps = 700
	segmentedGovpxOpts := segmentedOpts
	segmentedGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, segmentedOpts, segmentedSources)
	segmentedGovpxFrames := encodeFramesWithGovpx(t, segmentedGovpxOpts, segmentedSources)
	segmentedLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-segmented64", segmentedOpts, segmentedOpts.TargetBitrateKbps, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64", segmentedGovpxFrames, segmentedLibvpxFrames, 0)

	segmentedDropOpts := segmentedOpts
	segmentedDropOpts.DropFrameAllowed = true
	segmentedDropOpts.DropFrameWaterMark = 60
	segmentedDropGovpxFrames, segmentedDropLibvpxFrames := encodeTwoPass("twopass-e2e-segmented64-drop-frame60", segmentedDropOpts, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-drop-frame60", segmentedDropGovpxFrames, segmentedDropLibvpxFrames, 0)

	segmentedMaxIntraOpts := segmentedOpts
	segmentedMaxIntraOpts.MaxIntraBitratePct = 500
	segmentedMaxIntraGovpxFrames, segmentedMaxIntraLibvpxFrames := encodeTwoPass("twopass-e2e-segmented64-max-intra-rate500", segmentedMaxIntraOpts, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-max-intra-rate500", segmentedMaxIntraGovpxFrames, segmentedMaxIntraLibvpxFrames, 0)

	segmentedGFBoostOpts := segmentedOpts
	segmentedGFBoostOpts.GFCBRBoostPct = 500
	segmentedGFBoostGovpxFrames, segmentedGFBoostLibvpxFrames := encodeTwoPass("twopass-e2e-segmented64-gf-cbr-boost500", segmentedGFBoostOpts, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-gf-cbr-boost500", segmentedGFBoostGovpxFrames, segmentedGFBoostLibvpxFrames, 0)

	segmentedSectionOpts := segmentedOpts
	segmentedSectionOpts.TwoPassVBRBiasPct = 80
	segmentedSectionOpts.TwoPassMinPct = 50
	segmentedSectionOpts.TwoPassMaxPct = 200
	segmentedSectionGovpxOpts := segmentedSectionOpts
	segmentedSectionGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, segmentedSectionOpts, segmentedSources)
	segmentedSectionGovpxFrames := encodeFramesWithGovpx(t, segmentedSectionGovpxOpts, segmentedSources)
	segmentedSectionSetterFrames := encodeFramesWithGovpxTwoPassStatsSetter(t, segmentedSectionOpts, segmentedSectionGovpxOpts.TwoPassStats, segmentedSources, false)
	segmentedSectionLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-segmented64-sections", segmentedSectionOpts, segmentedSectionOpts.TargetBitrateKbps, segmentedSources)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-sections", segmentedSectionGovpxFrames, segmentedSectionLibvpxFrames, 0)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-sections-setter-vs-options", segmentedSectionSetterFrames, segmentedSectionGovpxFrames, 0)
	assertSegmentByteParity(t, "twopass-e2e-segmented64-sections-setter", segmentedSectionSetterFrames, segmentedSectionLibvpxFrames, 0)

	tokenOpts := panningOpts
	tokenOpts.TokenPartitions = 2
	tokenGovpxOpts := tokenOpts
	tokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, tokenOpts, panningSources)
	tokenGovpxFrames := encodeFramesWithGovpx(t, tokenGovpxOpts, panningSources)
	tokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-token-parts2", tokenOpts, tokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-token-parts2", tokenGovpxFrames, tokenLibvpxFrames, 0)

	erTokenOpts := panningOpts
	erTokenOpts.ErrorResilient = true
	erTokenOpts.ErrorResilientPartitions = true
	erTokenOpts.TokenPartitions = 3
	erTokenGovpxOpts := erTokenOpts
	erTokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, erTokenOpts, panningSources)
	erTokenGovpxFrames := encodeFramesWithGovpx(t, erTokenGovpxOpts, panningSources)
	erTokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-er3-token-parts3", erTokenOpts, erTokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-er3-token-parts3", erTokenGovpxFrames, erTokenLibvpxFrames, 0)

	threadTokenOpts := panningOpts
	threadTokenOpts.Threads = 2
	threadTokenOpts.TokenPartitions = 3
	threadTokenGovpxOpts := threadTokenOpts
	threadTokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, threadTokenOpts, panningSources)
	threadTokenGovpxFrames := encodeFramesWithGovpx(t, threadTokenGovpxOpts, panningSources)
	threadTokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-threads2-token-parts3-panning64", threadTokenOpts, threadTokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-threads2-token-parts3-panning64", threadTokenGovpxFrames, threadTokenLibvpxFrames, 0)

	erThreadTokenOpts := panningOpts
	erThreadTokenOpts.ErrorResilient = true
	erThreadTokenOpts.ErrorResilientPartitions = true
	erThreadTokenOpts.Threads = 2
	erThreadTokenOpts.TokenPartitions = 3
	erThreadTokenGovpxOpts := erThreadTokenOpts
	erThreadTokenGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, erThreadTokenOpts, panningSources)
	erThreadTokenGovpxFrames := encodeFramesWithGovpx(t, erThreadTokenGovpxOpts, panningSources)
	erThreadTokenLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-er3-threads2-token-parts3", erThreadTokenOpts, erThreadTokenOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-er3-threads2-token-parts3", erThreadTokenGovpxFrames, erThreadTokenLibvpxFrames, 0)

	screenStaticOpts := panningOpts
	screenStaticOpts.ScreenContentMode = 2
	screenStaticOpts.StaticThreshold = 500
	screenStaticGovpxOpts := screenStaticOpts
	screenStaticGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, screenStaticOpts, panningSources)
	screenStaticGovpxFrames := encodeFramesWithGovpx(t, screenStaticGovpxOpts, panningSources)
	screenStaticLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-screen-content2-static-thresh500", screenStaticOpts, screenStaticOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-screen-content2-static-thresh500", screenStaticGovpxFrames, screenStaticLibvpxFrames, 0)

	sharpNoiseOpts := panningOpts
	sharpNoiseOpts.Sharpness = 4
	sharpNoiseOpts.NoiseSensitivity = 3
	sharpNoiseGovpxOpts := sharpNoiseOpts
	sharpNoiseGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, sharpNoiseOpts, panningSources)
	sharpNoiseGovpxFrames := encodeFramesWithGovpx(t, sharpNoiseGovpxOpts, panningSources)
	sharpNoiseLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-sharpness4-noise3", sharpNoiseOpts, sharpNoiseOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-sharpness4-noise3", sharpNoiseGovpxFrames, sharpNoiseLibvpxFrames, 0)

	speedOpts := panningOpts
	speedOpts.CpuUsed = -3
	speedGovpxOpts := speedOpts
	speedGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, speedOpts, panningSources)
	speedGovpxFrames := encodeFramesWithGovpx(t, speedGovpxOpts, panningSources)
	speedLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-cpu-3", speedOpts, speedOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-cpu-3", speedGovpxFrames, speedLibvpxFrames, 0)

	ssimOpts := panningOpts
	ssimOpts.Tuning = TuneSSIM
	ssimGovpxOpts := ssimOpts
	ssimGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, ssimOpts, panningSources)
	ssimGovpxFrames := encodeFramesWithGovpx(t, ssimGovpxOpts, panningSources)
	ssimLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-tune-ssim", ssimOpts, ssimOpts.TargetBitrateKbps, panningSources)
	assertSegmentByteParity(t, "twopass-e2e-tune-ssim", ssimGovpxFrames, ssimLibvpxFrames, 0)

	arnrSources := makePanningSources(64, 64, 16, 0)
	altRefOpts := panningOpts
	altRefOpts.LookaheadFrames = 8
	altRefOpts.AutoAltRef = true
	altRefGovpxOpts := altRefOpts
	altRefGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, altRefOpts, arnrSources)
	altRefGovpxFrames := encodeFramesWithGovpx(t, altRefGovpxOpts, arnrSources)
	altRefLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-auto-alt-ref-no-arnr", altRefOpts, altRefOpts.TargetBitrateKbps, arnrSources)
	assertSegmentByteParity(t, "twopass-e2e-auto-alt-ref-no-arnr", altRefGovpxFrames, altRefLibvpxFrames, 0)

	arnrOpts := panningOpts
	arnrOpts.LookaheadFrames = 8
	arnrOpts.AutoAltRef = true
	arnrOpts.ARNRMaxFrames = 5
	arnrOpts.ARNRStrength = 3
	arnrOpts.ARNRType = 3
	arnrGovpxOpts := arnrOpts
	arnrGovpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, arnrOpts, arnrSources)
	arnrGovpxFrames := encodeFramesWithGovpx(t, arnrGovpxOpts, arnrSources)
	arnrLibvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-e2e-auto-alt-ref-arnr", arnrOpts, arnrOpts.TargetBitrateKbps, arnrSources)
	assertSegmentByteParity(t, "twopass-e2e-auto-alt-ref-arnr", arnrGovpxFrames, arnrLibvpxFrames, 0)
}

func TestVP8OracleEncoderStreamByteParityTwoPassSegmentedControlCrosses(t *testing.T) {
	vp8test.RequireOracle(t, "two-pass control-cross byte-parity gate")
	vpxenc := vp8test.Vpxenc(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(64, 64, i)
	}
	baseOpts := EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name       string
		opts       EncoderOptions
		matchLimit int
	}{
		{
			name: "token-parts4",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.TokenPartitions = 2
				return opts
			}(),
		},
		{
			name: "token-parts8",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.TokenPartitions = 3
				return opts
			}(),
		},
		{
			name: "er1-token-parts4",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.ErrorResilient = true
				opts.TokenPartitions = 2
				return opts
			}(),
		},
		{
			name: "er3-token-parts8",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.ErrorResilient = true
				opts.ErrorResilientPartitions = true
				opts.TokenPartitions = 3
				return opts
			}(),
		},
		{
			name: "threads2-token-parts8",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.Threads = 2
				opts.TokenPartitions = 3
				return opts
			}(),
		},
		{
			name: "tune-ssim",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.Tuning = TuneSSIM
				return opts
			}(),
		},
		{
			name: "screen-content2-static-thresh500",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.ScreenContentMode = 2
				opts.StaticThreshold = 500
				return opts
			}(),
		},
		{
			name: "sharpness4-noise3",
			opts: func() EncoderOptions {
				opts := baseOpts
				opts.Sharpness = 4
				opts.NoiseSensitivity = 3
				return opts
			}(),
			matchLimit: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			govpxOpts := tc.opts
			govpxOpts.TwoPassStats = captureGovpxFirstPassStats(t, tc.opts, sources)
			govpxFrames := encodeFramesWithGovpx(t, govpxOpts, sources)
			libvpxFrames := encodeFramesWithLibvpxTwoPassOracle(t, vpxenc, vpxencOracle, "twopass-segmented-"+tc.name, tc.opts, tc.opts.TargetBitrateKbps, sources)
			assertSegmentByteParity(t, "twopass-segmented-"+tc.name, govpxFrames, libvpxFrames, tc.matchLimit)
		})
	}
}
