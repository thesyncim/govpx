package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
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
