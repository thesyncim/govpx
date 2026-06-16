package govpx

import (
	"errors"
	"testing"
)

func TestVP9SpatialSVCEncoderLayerAdvancedRuntimeControls(t *testing.T) {
	cbrLayer := func(width, height, kbps int) VP9EncoderOptions {
		return VP9EncoderOptions{
			Width:               width,
			Height:              height,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			TargetBitrateKbps:   kbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
			Threads:             2,
		}
	}
	vbrLayer := func(width, height, kbps int) VP9EncoderOptions {
		opts := cbrLayer(width, height, kbps)
		opts.RateControlMode = RateControlVBR
		return opts
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			vbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	stats := finalizedVP9TwoPassTestStats(100, 120, 90, 110)
	if err := svc.SetLayerFrameDropAllowed(0, false); err != nil {
		t.Fatalf("SetLayerFrameDropAllowed: %v", err)
	}
	if err := svc.SetLayerRateControlBuffer(0, 320, 160, 240); err != nil {
		t.Fatalf("SetLayerRateControlBuffer: %v", err)
	}
	if err := svc.SetLayerPostEncodeDrop(0, false); err != nil {
		t.Fatalf("SetLayerPostEncodeDrop: %v", err)
	}
	if err := svc.SetLayerDisableOvershootMaxQCBR(0, true); err != nil {
		t.Fatalf("SetLayerDisableOvershootMaxQCBR: %v", err)
	}
	if err := svc.SetLayerNextFrameQIndex(0, 96); err != nil {
		t.Fatalf("SetLayerNextFrameQIndex: %v", err)
	}
	if err := svc.SetLayerRealtimeTarget(0, RealtimeTarget{
		BitrateKbps:  360,
		FPS:          24,
		Width:        32,
		Height:       32,
		MinQuantizer: 6,
		MaxQuantizer: 54,
		FrameDrop:    RealtimeFrameDropDisabled,
	}); err != nil {
		t.Fatalf("SetLayerRealtimeTarget: %v", err)
	}
	if err := svc.SetLayerTuning(0, TuneSSIM); err != nil {
		t.Fatalf("SetLayerTuning: %v", err)
	}
	if err := svc.SetLayerColorSpace(0, VP9ColorSpaceBT709); err != nil {
		t.Fatalf("SetLayerColorSpace: %v", err)
	}
	if err := svc.SetLayerColorRange(0, VP9ColorRangeFull); err != nil {
		t.Fatalf("SetLayerColorRange: %v", err)
	}
	if err := svc.SetLayerRenderSize(0, 30, 28); err != nil {
		t.Fatalf("SetLayerRenderSize: %v", err)
	}
	if err := svc.SetLayerTargetLevel(0, 31); err != nil {
		t.Fatalf("SetLayerTargetLevel: %v", err)
	}
	if err := svc.SetLayerDisableLoopfilter(0, VP9LoopfilterDisableInter); err != nil {
		t.Fatalf("SetLayerDisableLoopfilter: %v", err)
	}
	if err := svc.SetLayerLossless(0, true); err != nil {
		t.Fatalf("SetLayerLossless: %v", err)
	}
	if err := svc.SetLayerScreenContentMode(0, 1); err != nil {
		t.Fatalf("SetLayerScreenContentMode: %v", err)
	}
	if err := svc.SetLayerSharpness(0, 4); err != nil {
		t.Fatalf("SetLayerSharpness: %v", err)
	}
	if err := svc.SetLayerStaticThreshold(0, 512); err != nil {
		t.Fatalf("SetLayerStaticThreshold: %v", err)
	}
	if err := svc.SetLayerKeyFrameInterval(0, 7); err != nil {
		t.Fatalf("SetLayerKeyFrameInterval: %v", err)
	}
	if err := svc.SetLayerKeyFrameIntervalRange(0, 1, 7); err != nil {
		t.Fatalf("SetLayerKeyFrameIntervalRange: %v", err)
	}
	if err := svc.SetLayerAdaptiveKeyFrames(0, true); err != nil {
		t.Fatalf("SetLayerAdaptiveKeyFrames: %v", err)
	}
	if err := svc.SetLayerCQLevel(1, 24); err != nil {
		t.Fatalf("SetLayerCQLevel: %v", err)
	}
	if err := svc.SetLayerAQMode(1, VP9AQComplexity); err != nil {
		t.Fatalf("SetLayerAQMode: %v", err)
	}
	if err := svc.SetLayerAutoAltRef(0, false); err != nil {
		t.Fatalf("SetLayerAutoAltRef(false): %v", err)
	}
	if err := svc.SetLayerFrameParallelDecoding(0, false); err != nil {
		t.Fatalf("SetLayerFrameParallelDecoding: %v", err)
	}
	if err := svc.SetLayerFrameParallelEncoderThreads(0, 1); err != nil {
		t.Fatalf("SetLayerFrameParallelEncoderThreads: %v", err)
	}
	if err := svc.SetLayerRTCExternalRateControl(0, true); err != nil {
		t.Fatalf("SetLayerRTCExternalRateControl: %v", err)
	}
	if err := svc.SetLayerRowMT(0, true); err != nil {
		t.Fatalf("SetLayerRowMT: %v", err)
	}
	if err := svc.SetLayerTwoPassStats(1, stats); err != nil {
		t.Fatalf("SetLayerTwoPassStats: %v", err)
	}
	if err := svc.SetLayerARNR(1, 3, 4, 2); err != nil {
		t.Fatalf("SetLayerARNR: %v", err)
	}
	if err := svc.SetLayerMinGFInterval(1, 4); err != nil {
		t.Fatalf("SetLayerMinGFInterval: %v", err)
	}
	if err := svc.SetLayerMaxGFInterval(1, 12); err != nil {
		t.Fatalf("SetLayerMaxGFInterval: %v", err)
	}
	if err := svc.SetLayerFramePeriodicBoost(1, true); err != nil {
		t.Fatalf("SetLayerFramePeriodicBoost: %v", err)
	}
	if err := svc.SetLayerAltRefAQ(1, true); err != nil {
		t.Fatalf("SetLayerAltRefAQ: %v", err)
	}
	if err := svc.SetLayerEnableKeyFrameFiltering(1, true); err != nil {
		t.Fatalf("SetLayerEnableKeyFrameFiltering: %v", err)
	}
	if err := svc.SetLayerEnableTPL(0, false); err != nil {
		t.Fatalf("SetLayerEnableTPL(false): %v", err)
	}
	if err := svc.SetLayerDeltaQUV(1, 4); err != nil {
		t.Fatalf("SetLayerDeltaQUV: %v", err)
	}

	base, err := svc.LayerEncoder(0)
	if err != nil {
		t.Fatalf("LayerEncoder(0): %v", err)
	}
	enh, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if base.opts.DropFrameAllowed ||
		base.rc.dropFrameAllowed ||
		base.opts.TargetBitrateKbps != 360 ||
		base.opts.FPS != 24 ||
		base.opts.TimebaseNum != 1 ||
		base.opts.TimebaseDen != 24 ||
		base.opts.MinQuantizer != 6 ||
		base.opts.MaxQuantizer != 54 ||
		base.opts.BufferSizeMs != 320 ||
		base.opts.BufferInitialSizeMs != 160 ||
		base.opts.BufferOptimalSizeMs != 240 ||
		base.opts.PostEncodeDrop ||
		base.rc.postEncodeDrop ||
		!base.opts.DisableOvershootMaxQCBR ||
		!base.rc.disableOvershootMaxQCBR ||
		!base.opts.NextFrameQIndexSet ||
		base.opts.NextFrameQIndex != 96 ||
		!base.rc.nextFrameQIndexSet ||
		base.rc.nextFrameQIndex != 96 ||
		base.opts.Tuning != TuneSSIM ||
		base.opts.ColorSpace != VP9ColorSpaceBT709 ||
		base.opts.ColorRange != VP9ColorRangeFull ||
		base.opts.RenderWidth != 30 ||
		base.opts.RenderHeight != 28 ||
		base.opts.TargetLevel != 31 ||
		base.opts.DisableLoopfilter != VP9LoopfilterDisableInter ||
		!base.opts.FrameParallelDecodingSet ||
		base.opts.FrameParallelDecoding ||
		base.opts.FrameParallelEncoderThreads != 1 ||
		base.opts.AutoAltRef ||
		base.opts.EnableTPL ||
		!base.opts.RTCExternalRateControl ||
		!base.opts.RowMT ||
		!base.opts.Lossless ||
		base.opts.ScreenContentMode != 1 ||
		base.opts.Sharpness != 4 ||
		base.opts.StaticThreshold != 512 ||
		base.opts.MinKeyframeInterval != 1 ||
		base.opts.MaxKeyframeInterval != 7 ||
		!base.opts.AdaptiveKeyFrames {
		t.Fatalf("base layer advanced controls missing: opts=%+v rc=%+v",
			base.opts, base.rc)
	}
	if enh.opts.CQLevel != 24 ||
		enh.opts.AQMode != VP9AQComplexity ||
		len(enh.opts.TwoPassStats) != len(stats) ||
		!enh.twoPass.enabled() ||
		enh.opts.ARNRMaxFrames != 3 ||
		enh.opts.ARNRStrength != 4 ||
		enh.opts.ARNRType != 2 ||
		enh.opts.MinGFInterval != 4 ||
		enh.rc.minGFInterval != 4 ||
		enh.opts.MaxGFInterval != 12 ||
		enh.rc.maxGFInterval != 12 ||
		!enh.opts.FramePeriodicBoost ||
		!enh.rc.framePeriodicBoost ||
		!enh.opts.AltRefAQ ||
		!enh.rc.altRefAQ ||
		!enh.opts.EnableKeyFrameFiltering ||
		enh.opts.DeltaQUV != 4 ||
		enh.opts.DropFrameAllowed ||
		enh.opts.Lossless {
		t.Fatalf("enhancement layer advanced controls missing/leaked: opts=%+v twoPass=%t",
			enh.opts, enh.twoPass.enabled())
	}
	if err := svc.SetLayerSharpness(0, 8); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerSharpness invalid err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.Sharpness != 4 {
		t.Fatalf("invalid sharpness mutated base layer to %d", base.opts.Sharpness)
	}
	if err := svc.SetLayerColorSpace(0, VP9ColorSpaceSRGB); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerColorSpace(SRGB) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.ColorSpace != VP9ColorSpaceBT709 {
		t.Fatalf("invalid color space mutated base layer to %d", base.opts.ColorSpace)
	}
	if err := svc.SetLayerColorRange(0, VP9ColorRange(2)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerColorRange(2) err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRenderSize(0, 640, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRenderSize invalid err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.RenderWidth != 30 || base.opts.RenderHeight != 28 {
		t.Fatalf("invalid render size mutated base layer to %dx%d",
			base.opts.RenderWidth, base.opts.RenderHeight)
	}
	if err := svc.SetLayerTargetLevel(0, 12); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerTargetLevel(12) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.TargetLevel != 31 {
		t.Fatalf("invalid target level mutated base layer to %d", base.opts.TargetLevel)
	}
	if err := svc.SetLayerDisableLoopfilter(0, VP9DisableLoopfilter(3)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerDisableLoopfilter invalid err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.DisableLoopfilter != VP9LoopfilterDisableInter {
		t.Fatalf("invalid disable-loopfilter mutated base layer to %d",
			base.opts.DisableLoopfilter)
	}
	if err := svc.SetLayerFrameParallelDecoding(2, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerFrameParallelDecoding invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerFrameParallelEncoderThreads(0, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerFrameParallelEncoderThreads(2) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.FrameParallelEncoderThreads != 1 {
		t.Fatalf("invalid frame-parallel encoder threads mutated base layer to %d",
			base.opts.FrameParallelEncoderThreads)
	}
	if err := svc.SetLayerAutoAltRef(0, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerAutoAltRef(true) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.AutoAltRef {
		t.Fatal("invalid auto-alt-ref update mutated base layer")
	}
	if err := svc.SetLayerEnableTPL(0, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerEnableTPL(true) err = %v, want ErrInvalidConfig", err)
	}
	if base.opts.EnableTPL {
		t.Fatal("invalid TPL update mutated base layer")
	}
	singleThreadSVC, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, TargetBitrateKbps: 300},
			{Width: 64, Height: 64, TargetBitrateKbps: 700},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder(single thread): %v", err)
	}
	if err := singleThreadSVC.SetLayerRowMT(0, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRowMT with Threads<=1 err = %v, want ErrInvalidConfig", err)
	}
	if err := singleThreadSVC.Close(); err != nil {
		t.Fatalf("singleThreadSVC.Close: %v", err)
	}
	if err := svc.SetLayerRealtimeTarget(1, RealtimeTarget{
		Width:  32,
		Height: 32,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRealtimeTarget resize err = %v, want ErrInvalidConfig", err)
	}
	if enh.opts.Width != 64 || enh.opts.Height != 64 {
		t.Fatalf("rejected enhancement resize mutated dimensions to %dx%d",
			enh.opts.Width, enh.opts.Height)
	}
	if err := svc.SetLayerRealtimeTarget(1, RealtimeTarget{
		Width:  64,
		Height: 64,
	}); err != nil {
		t.Fatalf("same-size SetLayerRealtimeTarget: %v", err)
	}
	if err := svc.SetLayerRateControlBuffer(1, 320, 160, 240); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRateControlBuffer on VBR err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerPostEncodeDrop(1, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerPostEncodeDrop on VBR err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerDisableOvershootMaxQCBR(1, true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerDisableOvershootMaxQCBR on VBR err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerTwoPassStats(0, stats); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerTwoPassStats on CBR err = %v, want ErrInvalidConfig", err)
	}
	if base.twoPass.enabled() {
		t.Fatal("invalid base two-pass update enabled CBR layer")
	}
	if err := svc.SetLayerMinGFInterval(1, 13); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerMinGFInterval above max err = %v, want ErrInvalidConfig", err)
	}
	if enh.opts.MinGFInterval != 4 || enh.rc.minGFInterval != 4 {
		t.Fatalf("invalid min-gf update mutated enhancement to opts=%d rc=%d",
			enh.opts.MinGFInterval, enh.rc.minGFInterval)
	}
	if err := svc.SetLayerNextFrameQIndex(1, 300); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLayerNextFrameQIndex invalid err = %v, want ErrInvalidQuantizer", err)
	}
	if err := svc.SetLayerDeltaQUV(0, 1); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLayerDeltaQUV on lossless err = %v, want ErrInvalidQuantizer", err)
	}
	if base.opts.DeltaQUV != 0 {
		t.Fatalf("invalid delta-q-uv mutated base layer to %d", base.opts.DeltaQUV)
	}
	if err := svc.SetLayerDeltaQUV(1, 16); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("SetLayerDeltaQUV invalid err = %v, want ErrInvalidQuantizer", err)
	}
	if enh.opts.DeltaQUV != 4 {
		t.Fatalf("invalid delta-q-uv mutated enhancement layer to %d", enh.opts.DeltaQUV)
	}
	if err := svc.SetLayerCQLevel(2, 24); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerCQLevel invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerAQMode(2, VP9AQNone); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerAQMode invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerKeyFrameIntervalRange(0, 8, 7); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerKeyFrameIntervalRange invalid err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerAdaptiveKeyFrames(2, false); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerAdaptiveKeyFrames invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRealtimeTarget(2, RealtimeTarget{
		BitrateKbps: 100,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRealtimeTarget invalid layer err = %v, want ErrInvalidConfig", err)
	}

	for _, tc := range []struct {
		name string
		fn   func() error
	}{
		{"SetLayerFrameDropAllowed", func() error {
			return svc.SetLayerFrameDropAllowed(0, true)
		}},
		{"SetLayerPostEncodeDrop", func() error {
			return svc.SetLayerPostEncodeDrop(0, true)
		}},
		{"SetLayerRealtimeTargetFrameDrop", func() error {
			return svc.SetLayerRealtimeTarget(0, RealtimeTarget{
				FrameDrop: RealtimeFrameDropEnabled,
			})
		}},
		{"SetLayerRateControlFrameDrop", func() error {
			return svc.SetLayerRateControl(0, RateControlConfig{
				Mode:              RateControlCBR,
				TargetBitrateKbps: 300,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				DropFrameAllowed:  true,
			})
		}},
		{"direct SetFrameDropAllowed", func() error {
			return base.SetFrameDropAllowed(true)
		}},
		{"direct SetPostEncodeDrop", func() error {
			return base.SetPostEncodeDrop(true)
		}},
		{"direct SetRealtimeTargetFrameDrop", func() error {
			return base.SetRealtimeTarget(RealtimeTarget{
				FrameDrop: RealtimeFrameDropEnabled,
			})
		}},
		{"direct SetRateControlFrameDrop", func() error {
			return base.SetRateControl(RateControlConfig{
				Mode:              RateControlCBR,
				TargetBitrateKbps: 300,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				DropFrameAllowed:  true,
			})
		}},
	} {
		if err := tc.fn(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s err = %v, want ErrInvalidConfig", tc.name, err)
		}
	}
	if base.opts.DropFrameAllowed || base.rc.dropFrameAllowed ||
		base.opts.PostEncodeDrop || base.rc.postEncodeDrop {
		t.Fatalf("rejected frame-drop controls mutated base layer: opts=%+v rc=%+v",
			base.opts, base.rc)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerRealtimeTarget(0, RealtimeTarget{
		BitrateKbps: 360,
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRealtimeTarget after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerARNR(0, 3, 4, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerARNR after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerMinGFInterval(0, 4); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerMinGFInterval after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerPostEncodeDrop(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerPostEncodeDrop after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerNextFrameQIndex(0, 96); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerNextFrameQIndex after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerColorSpace(0, VP9ColorSpaceBT709); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerColorSpace after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerFrameParallelDecoding(0, true); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerFrameParallelDecoding after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerFrameParallelEncoderThreads(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerFrameParallelEncoderThreads after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerAutoAltRef(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAutoAltRef after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerEnableTPL(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerEnableTPL after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerRowMT(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRowMT after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerRenderSize(0, 30, 28); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRenderSize after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerDeltaQUV(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerDeltaQUV after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerAQMode(0, VP9AQNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAQMode after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerAdaptiveKeyFrames(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAdaptiveKeyFrames after close err = %v, want ErrClosed", err)
	}
	var nilSVC *VP9SpatialSVCEncoder
	if err := nilSVC.SetLayerRealtimeTarget(0, RealtimeTarget{
		BitrateKbps: 360,
	}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRealtimeTarget on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.SetLayerTuning(0, TunePSNR); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerTuning on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.SetLayerAQMode(0, VP9AQNone); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAQMode on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.SetLayerAdaptiveKeyFrames(0, false); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerAdaptiveKeyFrames on nil err = %v, want ErrClosed", err)
	}
}
