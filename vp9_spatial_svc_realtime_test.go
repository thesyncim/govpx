package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"image"
	"reflect"
	"testing"
)

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

func TestVP9SpatialSVCEncoderLayerRateControl(t *testing.T) {
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
		}
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			cbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	if err := svc.SetLayerBitrateKbps(1, 900); err != nil {
		t.Fatalf("SetLayerBitrateKbps: %v", err)
	}
	if err := svc.SetLayerRateControl(0, RateControlConfig{
		Mode:              RateControlVBR,
		TargetBitrateKbps: 250,
		MinQuantizer:      6,
		MaxQuantizer:      50,
	}); err != nil {
		t.Fatalf("SetLayerRateControl: %v", err)
	}
	if err := svc.SetLayerMaxIntraBitratePct(0, 180); err != nil {
		t.Fatalf("SetLayerMaxIntraBitratePct: %v", err)
	}
	if err := svc.SetLayerMaxInterBitratePct(1, 220); err != nil {
		t.Fatalf("SetLayerMaxInterBitratePct: %v", err)
	}
	if err := svc.SetLayerGFCBRBoostPct(1, 45); err != nil {
		t.Fatalf("SetLayerGFCBRBoostPct: %v", err)
	}
	if err := svc.SetLayerBitrateKbps(2, 100); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerBitrateKbps invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerRateControl(2, RateControlConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerRateControl invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerMaxIntraBitratePct(2, 100); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerMaxIntraBitratePct invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerMaxInterBitratePct(1, -1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerMaxInterBitratePct negative err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.SetLayerGFCBRBoostPct(0, 45); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerGFCBRBoostPct on VBR layer err = %v, want ErrInvalidConfig", err)
	}

	base, err := svc.LayerEncoder(0)
	if err != nil {
		t.Fatalf("LayerEncoder(0): %v", err)
	}
	enh, err := svc.LayerEncoder(1)
	if err != nil {
		t.Fatalf("LayerEncoder(1): %v", err)
	}
	if base.opts.RateControlMode != RateControlVBR ||
		base.opts.TargetBitrateKbps != 250 ||
		base.opts.MinQuantizer != 6 ||
		base.opts.MaxQuantizer != 50 ||
		base.opts.MaxIntraBitratePct != 180 ||
		base.rc.maxIntraBitratePct != 180 ||
		base.opts.GFCBRBoostPct != 0 ||
		base.rc.gfCBRBoostPct != 0 ||
		enh.opts.TargetBitrateKbps != 900 ||
		enh.opts.MaxInterBitratePct != 220 ||
		enh.rc.maxInterBitratePct != 220 ||
		enh.opts.GFCBRBoostPct != 45 ||
		enh.rc.gfCBRBoostPct != 45 {
		t.Fatalf("layer RC opts base=%+v enh=%+v", base.opts, enh.opts)
	}

	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 70, 128, 128),
		vp9test.NewYCbCr(64, 64, 90, 128, 128),
	}, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	if result.Layers[0].TargetBitrateKbps != 250 ||
		result.Layers[1].TargetBitrateKbps != 900 {
		t.Fatalf("result layer bitrates = %d/%d, want 250/900",
			result.Layers[0].TargetBitrateKbps,
			result.Layers[1].TargetBitrateKbps)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerBitrateKbps(0, 300); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerBitrateKbps after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerRateControl(0, RateControlConfig{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerRateControl after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerMaxIntraBitratePct(0, 300); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerMaxIntraBitratePct after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerMaxInterBitratePct(0, 300); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerMaxInterBitratePct after close err = %v, want ErrClosed", err)
	}
	if err := svc.SetLayerGFCBRBoostPct(0, 10); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerGFCBRBoostPct after close err = %v, want ErrClosed", err)
	}
}

func TestVP9SpatialSVCRealtimeCPUUsedZeroUsesFastPath(t *testing.T) {
	temporal := TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringThreeLayers,
	}
	var layers [VP9MaxSpatialLayers]VP9EncoderOptions
	for i, dim := range [3][2]int{{32, 32}, {64, 64}, {128, 128}} {
		layers[i] = VP9EncoderOptions{
			Width:                    dim[0],
			Height:                   dim[1],
			FPS:                      30,
			Deadline:                 DeadlineRealtime,
			CpuUsed:                  0,
			RateControlModeSet:       true,
			RateControlMode:          RateControlCBR,
			TargetBitrateKbps:        100 * (i + 1),
			TemporalScalability:      temporal,
			ErrorResilient:           true,
			FrameParallelDecodingSet: true,
			FrameParallelDecoding:    true,
		}
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           3,
		InterLayerPrediction: true,
		Layers:               layers,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()

	for i := 0; i < int(svc.layerCount); i++ {
		layer := svc.layers[i]
		if layer == nil {
			t.Fatalf("layer %d encoder is nil", i)
		}
		if layer.opts.Deadline != DeadlineRealtime ||
			layer.opts.CpuUsed != vp9DefaultCPUUsed {
			t.Fatalf("layer %d normalized speed = deadline:%d cpu:%d, want realtime/%d",
				i, layer.opts.Deadline, layer.opts.CpuUsed,
				vp9DefaultCPUUsed)
		}
		if got := layer.vp9SpeedFeatureCPUUsed(); got != int(vp9DefaultCPUUsed) {
			t.Fatalf("layer %d speed-feature cpu-used = %d, want %d",
				i, got, vp9DefaultCPUUsed)
		}
		if layer.sf.UseNonrdPickMode != 1 {
			t.Fatalf("layer %d initial UseNonrdPickMode = %d, want 1",
				i, layer.sf.UseNonrdPickMode)
		}

		ctx := layer.vp9DefaultSpeedFrameContext()
		ctx.frameType = common.InterFrame
		ctx.intraOnly = false
		layer.vp9ApplySpeedFeatures(ctx)
		if layer.sf.UseNonrdPickMode != 1 {
			t.Fatalf("layer %d inter UseNonrdPickMode = %d, want 1",
				i, layer.sf.UseNonrdPickMode)
		}
		if layer.sf.PartitionSearchType == SearchPartition {
			t.Fatalf("layer %d inter PartitionSearchType = SearchPartition, want non-RD partitioning",
				i)
		}
	}
}

func TestVP9SpatialSVCEnablesErrorResilientLikeLibvpx(t *testing.T) {
	var layers [VP9MaxSpatialLayers]VP9EncoderOptions
	for i, dim := range [2][2]int{{32, 32}, {64, 64}} {
		layers[i] = VP9EncoderOptions{
			Width:                 dim[0],
			Height:                dim[1],
			FPS:                   30,
			Deadline:              DeadlineRealtime,
			CpuUsed:               8,
			RateControlModeSet:    true,
			RateControlMode:       RateControlCBR,
			TargetBitrateKbps:     120 * (i + 1),
			MaxKeyframeInterval:   128,
			FrameParallelDecoding: true,
		}
	}
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers:               layers,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()
	for i := 0; i < int(svc.layerCount); i++ {
		if !svc.layers[i].opts.ErrorResilient {
			t.Fatalf("layer %d ErrorResilient = false, want libvpx spatial-SVC default true",
				i)
		}
	}

	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		image.NewYCbCr(image.Rect(0, 0, 32, 32),
			image.YCbCrSubsampleRatio420),
		image.NewYCbCr(image.Rect(0, 0, 64, 64),
			image.YCbCrSubsampleRatio420),
	}, make([]byte, 1<<20))
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	for i := 0; i < int(result.LayerCount); i++ {
		info, err := PeekVP9StreamInfo(result.Layers[i].Data)
		if err != nil {
			t.Fatalf("PeekVP9StreamInfo layer %d: %v", i, err)
		}
		if !info.ErrorResilient {
			t.Fatalf("layer %d VP9 header ErrorResilient = false, want true",
				i)
		}
	}
}
