package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

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

func TestVP9SpatialSVCTemporalEnablesErrorResilientLikeLibvpx(t *testing.T) {
	temporal := TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringThreeLayers,
	}
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
			TemporalScalability:   temporal,
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
			t.Fatalf("layer %d ErrorResilient = false, want libvpx temporal-SVC default true",
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

func TestVP9SpatialSVCSetTemporalScalabilityEnablesErrorResilientLikeLibvpx(t *testing.T) {
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{
				Width:              32,
				Height:             32,
				FPS:                30,
				Deadline:           DeadlineRealtime,
				CpuUsed:            8,
				RateControlModeSet: true,
				RateControlMode:    RateControlCBR,
				TargetBitrateKbps:  120,
			},
			{
				Width:              64,
				Height:             64,
				FPS:                30,
				Deadline:           DeadlineRealtime,
				CpuUsed:            8,
				RateControlModeSet: true,
				RateControlMode:    RateControlCBR,
				TargetBitrateKbps:  240,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()
	for i := 0; i < int(svc.layerCount); i++ {
		if svc.layers[i].opts.ErrorResilient {
			t.Fatalf("layer %d ErrorResilient = true before temporal SVC enable",
				i)
		}
	}

	err = svc.SetTemporalScalability(TemporalScalabilityConfig{
		Enabled: true,
		Mode:    TemporalLayeringThreeLayers,
	})
	if err != nil {
		t.Fatalf("SetTemporalScalability: %v", err)
	}
	for i := 0; i < int(svc.layerCount); i++ {
		if !svc.layers[i].opts.ErrorResilient {
			t.Fatalf("layer %d ErrorResilient = false after temporal SVC enable",
				i)
		}
	}
}
