package govpx

import "testing"

// TestVP9SpatialSVCEncoderInheritsSVCState pins the layer-context wiring on
// the VP9SpatialSVCEncoder: every per-layer encoder must reflect the parent's
// number_spatial_layers and the layer's own spatial_layer_id before any
// speed-features dispatch.
//
// libvpx: vp9_svc_layercontext.c vp9_init_layer_context().
func TestVP9SpatialSVCEncoderInheritsSVCState(t *testing.T) {
	const (
		baseW, baseH = 320, 240
		topW, topH   = 640, 480
	)
	opts := VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
	}
	opts.Layers[0] = VP9EncoderOptions{
		Width:    baseW,
		Height:   baseH,
		Deadline: DeadlineRealtime,
		CpuUsed:  7,
	}
	opts.Layers[1] = VP9EncoderOptions{
		Width:    topW,
		Height:   topH,
		Deadline: DeadlineRealtime,
		CpuUsed:  7,
	}
	svc, err := NewVP9SpatialSVCEncoder(opts)
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()

	for i := range 2 {
		layer, err := svc.LayerEncoder(uint8(i))
		if err != nil {
			t.Fatalf("LayerEncoder(%d): %v", i, err)
		}
		if !layer.svc.UseSvc {
			t.Errorf("layer[%d].svc.UseSvc = false, want true", i)
		}
		if layer.svc.SpatialLayerID != i {
			t.Errorf("layer[%d].svc.SpatialLayerID = %d, want %d", i, layer.svc.SpatialLayerID, i)
		}
		if layer.svc.NumberSpatialLayers != 2 {
			t.Errorf("layer[%d].svc.NumberSpatialLayers = %d, want 2", i, layer.svc.NumberSpatialLayers)
		}
		if layer.svc.NumberTemporalLayers != 1 {
			t.Errorf("layer[%d].svc.NumberTemporalLayers = %d, want 1 (no temporal scalability configured)", i, layer.svc.NumberTemporalLayers)
		}
	}
}
