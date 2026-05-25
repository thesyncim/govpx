package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"reflect"
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
	if err := svc.SetLayerFrameDropAllowed(0, true); err != nil {
		t.Fatalf("SetLayerFrameDropAllowed: %v", err)
	}
	if err := svc.SetLayerRateControlBuffer(0, 320, 160, 240); err != nil {
		t.Fatalf("SetLayerRateControlBuffer: %v", err)
	}
	if err := svc.SetLayerPostEncodeDrop(0, true); err != nil {
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
		!base.opts.PostEncodeDrop ||
		!base.rc.postEncodeDrop ||
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

func TestVP9SpatialSVCEncoderLayerRuntimeControls(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: 32, Height: 32, TargetBitrateKbps: 300},
			{Width: 64, Height: 64, TargetBitrateKbps: 700},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	if err := svc.SetLayerDeadline(1, DeadlineGoodQuality); err != nil {
		t.Fatalf("SetLayerDeadline: %v", err)
	}
	if err := svc.SetLayerCPUUsed(1, -4); err != nil {
		t.Fatalf("SetLayerCPUUsed: %v", err)
	}
	if err := svc.SetLayerNoiseSensitivity(1, 3); err != nil {
		t.Fatalf("SetLayerNoiseSensitivity: %v", err)
	}
	activeMap := []uint8{0, 1, 1, 0}
	if err := svc.SetLayerActiveMap(0, activeMap, 2, 2); err != nil {
		t.Fatalf("SetLayerActiveMap: %v", err)
	}
	activeMap[0] = 1
	gotActive := make([]uint8, 4)
	if err := svc.GetLayerActiveMap(0, gotActive, 2, 2); err != nil {
		t.Fatalf("GetLayerActiveMap: %v", err)
	}
	if want := []uint8{0, 1, 1, 0}; !reflect.DeepEqual(gotActive, want) {
		t.Fatalf("GetLayerActiveMap = %v, want %v", gotActive, want)
	}
	roi := ROIMap{
		Enabled:   true,
		Rows:      8,
		Cols:      8,
		SegmentID: make([]uint8, 64),
	}
	for i := range roi.SegmentID {
		roi.SegmentID[i] = 1
	}
	roi.DeltaQuantizer[1] = -8
	roi.DeltaLoopFilter[1] = -2
	if err := svc.SetLayerROIMap(1, &roi); err != nil {
		t.Fatalf("SetLayerROIMap: %v", err)
	}
	roi.SegmentID[0] = 0

	base, err := svc.LayerEncoder(0)
	if err != nil {
		t.Fatalf("LayerEncoder(0): %v", err)
	}
	enh, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if base.opts.Deadline == DeadlineGoodQuality ||
		base.opts.CpuUsed == -4 ||
		base.opts.NoiseSensitivity != 0 ||
		!base.activeMapEnabled ||
		len(base.activeMap) != 16 ||
		base.activeMap[0] != vp9ActiveMapSegmentInactive ||
		base.roi.enabled {
		t.Fatalf("base layer controls leaked or missing: opts=%+v active=%t/%d roi=%t",
			base.opts, base.activeMapEnabled, len(base.activeMap),
			base.roi.enabled)
	}
	if enh.opts.Deadline != DeadlineGoodQuality ||
		enh.opts.CpuUsed != -4 ||
		enh.opts.NoiseSensitivity != 3 ||
		enh.denoiser.sensitivity != 3 ||
		enh.activeMapEnabled ||
		!enh.roi.enabled ||
		len(enh.roi.segmentID) != 64 ||
		enh.roi.segmentID[0] != 1 {
		t.Fatalf("enhancement layer controls = opts:%+v denoise:%d active:%t roi:%t/%d/%d",
			enh.opts, enh.denoiser.sensitivity, enh.activeMapEnabled,
			enh.roi.enabled, len(enh.roi.segmentID), enh.roi.segmentID[0])
	}
	if err := svc.SetLayerCPUUsed(2, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerCPUUsed invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerActiveMap(0, []uint8{1}, 1, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerActiveMap wrong geometry err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.GetLayerActiveMap(0, gotActive[:1], 2, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("GetLayerActiveMap short buffer err = %v, want ErrInvalidConfig", err)
	}
	if !reflect.DeepEqual(gotActive, []uint8{0, 1, 1, 0}) {
		t.Fatalf("invalid GetLayerActiveMap mutated output to %v", gotActive)
	}
	if !base.activeMapEnabled || base.activeMap[0] != vp9ActiveMapSegmentInactive {
		t.Fatal("invalid active-map update mutated base layer")
	}
	if err := svc.SetLayerROIMap(1, nil); err != nil {
		t.Fatalf("SetLayerROIMap(nil): %v", err)
	}
	if enh.roi.enabled {
		t.Fatal("SetLayerROIMap(nil) did not disable enhancement ROI")
	}

	dst := make([]byte, 1<<20)
	if _, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 70, 128, 128),
		vp9test.NewYCbCr(64, 64, 90, 128, 128),
	}, dst); err != nil {
		t.Fatalf("EncodeIntoWithResult after layer controls: %v", err)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerDeadline(0, DeadlineRealtime); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerDeadline after close err = %v, want ErrClosed", err)
	}
	if err := svc.GetLayerActiveMap(0, gotActive, 2, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetLayerActiveMap after close err = %v, want ErrClosed", err)
	}
	var nilSVC *VP9SpatialSVCEncoder
	if err := nilSVC.SetLayerNoiseSensitivity(0, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerNoiseSensitivity on nil err = %v, want ErrClosed", err)
	}
	if err := nilSVC.GetLayerActiveMap(0, gotActive, 2, 2); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetLayerActiveMap on nil err = %v, want ErrClosed", err)
	}
}

func TestVP9SpatialSVCEncoderLayerReferenceControls(t *testing.T) {
	const baseW, baseH = 32, 32
	const enhW, enhH = 64, 64
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: baseW, Height: baseH},
			{Width: enhW, Height: enhH},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	baseRefYCbCr := vp9test.NewMotionYCbCr(baseW, baseH)
	baseRef := vp9ImageFromYCbCrForTest(baseRefYCbCr)
	baseWant := clonePublicImage(baseRef)
	if err := svc.SetLayerReferenceFrame(0, ReferenceGolden, baseRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame base: %v", err)
	}
	baseRef.Y[0] ^= 0xff
	baseDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(baseW, baseH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(0, ReferenceGolden, &baseDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame base: %v", err)
	}
	assertImagesEqual(t, "base layer copied GOLDEN reference", baseWant, baseDst)

	enhRefYCbCr := vp9test.NewMotionYCbCr(enhW, enhH)
	enhRef := vp9ImageFromYCbCrForTest(enhRefYCbCr)
	if err := svc.SetLayerReferenceFrame(1, ReferenceLast, enhRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame enhancement: %v", err)
	}
	enhDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(enhW, enhH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(1, ReferenceLast, &enhDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame enhancement: %v", err)
	}
	assertImagesEqual(t, "enhancement layer copied LAST reference", enhRef, enhDst)

	if err := svc.SetLayerReferenceFrame(2, ReferenceLast, enhRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.CopyLayerReferenceFrame(2, ReferenceLast, &enhDst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerReferenceFrame(0, ReferenceLast, baseWant); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
	if err := svc.CopyLayerReferenceFrame(0, ReferenceLast, &baseDst); !errors.Is(err, ErrClosed) {
		t.Fatalf("CopyLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
}
