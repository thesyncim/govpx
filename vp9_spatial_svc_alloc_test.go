package govpx_test

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9SpatialSVCEncoderLayerRuntimeControlSettersNoAlloc(t *testing.T) {
	cbrLayer := func(width, height, kbps int) govpx.VP9EncoderOptions {
		return govpx.VP9EncoderOptions{
			Width:               width,
			Height:              height,
			RateControlModeSet:  true,
			RateControlMode:     govpx.RateControlCBR,
			TargetBitrateKbps:   kbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
			Threads:             2,
		}
	}
	vbrLayer := func(width, height, kbps int) govpx.VP9EncoderOptions {
		opts := cbrLayer(width, height, kbps)
		opts.RateControlMode = govpx.RateControlVBR
		return opts
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			vbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	stats := finalizedVP9TwoPassStatsForTest(100, 120, 90, 110)
	activeOut := make([]uint8, 4)

	tests := []struct {
		name string
		fn   func() error
	}{
		{name: "SetInterLayerPrediction", fn: func() error { return svc.SetInterLayerPrediction(true) }},
		{name: "SetLayerCQLevel", fn: func() error { return svc.SetLayerCQLevel(1, 24) }},
		{name: "SetLayerAQMode", fn: func() error { return svc.SetLayerAQMode(1, govpx.VP9AQComplexity) }},
		{name: "SetLayerAutoAltRefFalse", fn: func() error { return svc.SetLayerAutoAltRef(0, false) }},
		{name: "SetLayerFrameParallelDecoding", fn: func() error {
			return svc.SetLayerFrameParallelDecoding(0, false)
		}},
		{name: "SetLayerFrameParallelEncoderThreads", fn: func() error {
			return svc.SetLayerFrameParallelEncoderThreads(0, 1)
		}},
		{name: "SetLayerFrameDropAllowed", fn: func() error { return svc.SetLayerFrameDropAllowed(0, true) }},
		{name: "SetLayerRateControlBuffer", fn: func() error { return svc.SetLayerRateControlBuffer(0, 320, 160, 240) }},
		{name: "SetLayerPostEncodeDrop", fn: func() error { return svc.SetLayerPostEncodeDrop(0, true) }},
		{name: "SetLayerDisableOvershootMaxQCBR", fn: func() error {
			return svc.SetLayerDisableOvershootMaxQCBR(0, true)
		}},
		{name: "SetLayerMaxIntraBitratePct", fn: func() error { return svc.SetLayerMaxIntraBitratePct(0, 180) }},
		{name: "SetLayerMaxInterBitratePct", fn: func() error { return svc.SetLayerMaxInterBitratePct(1, 220) }},
		{name: "SetLayerGFCBRBoostPct", fn: func() error { return svc.SetLayerGFCBRBoostPct(0, 45) }},
		{name: "SetLayerRealtimeTarget", fn: func() error {
			return svc.SetLayerRealtimeTarget(0, govpx.RealtimeTarget{
				BitrateKbps:  360,
				FPS:          24,
				Width:        32,
				Height:       32,
				MinQuantizer: 6,
				MaxQuantizer: 54,
				FrameDrop:    govpx.RealtimeFrameDropEnabled,
			})
		}},
		{name: "SetLayerTwoPassStats", fn: func() error { return svc.SetLayerTwoPassStats(1, stats) }},
		{name: "SetLayerDeadline", fn: func() error { return svc.SetLayerDeadline(0, govpx.DeadlineRealtime) }},
		{name: "SetLayerCPUUsed", fn: func() error { return svc.SetLayerCPUUsed(0, 4) }},
		{name: "SetLayerTuning", fn: func() error { return svc.SetLayerTuning(0, govpx.TuneSSIM) }},
		{name: "SetLayerColorSpace", fn: func() error { return svc.SetLayerColorSpace(0, govpx.VP9ColorSpaceBT709) }},
		{name: "SetLayerColorRange", fn: func() error { return svc.SetLayerColorRange(0, govpx.VP9ColorRangeFull) }},
		{name: "SetLayerRenderSize", fn: func() error { return svc.SetLayerRenderSize(0, 30, 28) }},
		{name: "SetLayerTargetLevel", fn: func() error { return svc.SetLayerTargetLevel(0, 31) }},
		{name: "SetLayerDisableLoopfilter", fn: func() error {
			return svc.SetLayerDisableLoopfilter(0, govpx.VP9LoopfilterDisableInter)
		}},
		{name: "SetLayerLossless", fn: func() error { return svc.SetLayerLossless(0, true) }},
		{name: "SetLayerScreenContentMode", fn: func() error { return svc.SetLayerScreenContentMode(0, 1) }},
		{name: "SetLayerNoiseSensitivityZero", fn: func() error { return svc.SetLayerNoiseSensitivity(0, 0) }},
		{name: "SetLayerSharpness", fn: func() error { return svc.SetLayerSharpness(0, 4) }},
		{name: "SetLayerStaticThreshold", fn: func() error { return svc.SetLayerStaticThreshold(0, 512) }},
		{name: "SetLayerKeyFrameInterval", fn: func() error { return svc.SetLayerKeyFrameInterval(0, 7) }},
		{name: "SetLayerKeyFrameIntervalRange", fn: func() error {
			return svc.SetLayerKeyFrameIntervalRange(0, 1, 7)
		}},
		{name: "SetLayerAdaptiveKeyFrames", fn: func() error {
			return svc.SetLayerAdaptiveKeyFrames(0, true)
		}},
		{name: "SetLayerRTCExternalRateControl", fn: func() error {
			return svc.SetLayerRTCExternalRateControl(0, true)
		}},
		{name: "SetLayerRowMT", fn: func() error { return svc.SetLayerRowMT(0, true) }},
		{name: "SetLayerARNR", fn: func() error { return svc.SetLayerARNR(1, 3, 4, 2) }},
		{name: "SetLayerMinGFInterval", fn: func() error { return svc.SetLayerMinGFInterval(1, 4) }},
		{name: "SetLayerMaxGFInterval", fn: func() error { return svc.SetLayerMaxGFInterval(1, 12) }},
		{name: "SetLayerFramePeriodicBoost", fn: func() error { return svc.SetLayerFramePeriodicBoost(1, true) }},
		{name: "SetLayerAltRefAQ", fn: func() error { return svc.SetLayerAltRefAQ(1, true) }},
		{name: "SetLayerEnableKeyFrameFiltering", fn: func() error {
			return svc.SetLayerEnableKeyFrameFiltering(1, true)
		}},
		{name: "SetLayerEnableTPLFalse", fn: func() error { return svc.SetLayerEnableTPL(0, false) }},
		{name: "SetLayerNextFrameQIndex", fn: func() error { return svc.SetLayerNextFrameQIndex(0, 96) }},
		{name: "SetLayerDeltaQUV", fn: func() error { return svc.SetLayerDeltaQUV(1, 4) }},
		{name: "GetLayerActiveMap", fn: func() error { return svc.GetLayerActiveMap(0, activeOut, 2, 2) }},
	}
	for _, tc := range tests {
		allocs := testing.AllocsPerRun(100, func() {
			if err := tc.fn(); err != nil {
				t.Fatalf("%s returned error: %v", tc.name, err)
			}
		})
		if allocs != 0 {
			t.Fatalf("%s allocations = %v, want 0", tc.name, allocs)
		}
	}
}

func TestVP9SpatialSVCEncoderSteadyStateNoAlloc(t *testing.T) {
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{Width: 32, Height: 32},
			{Width: 64, Height: 64},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	for i := range 3 {
		if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
			t.Fatalf("warmup EncodeIntoWithResult %d: %v", i, err)
		}
	}
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
			t.Fatalf("alloc run EncodeIntoWithResult: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 spatial SVC steady state allocs = %f, want 0", allocs)
	}
}
