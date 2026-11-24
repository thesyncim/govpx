package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

// TestVP9SpeedFeaturesSVCDefaultsSingleLayer pins the SVC consumers in the
// speed-features dispatcher when the encoder is a single-layer encoder
// (cpi->use_svc = 0, svc->number_spatial_layers = 1,
// svc->number_temporal_layers = 1). Single-layer encoders see exactly the same
// configurator output libvpx produces for cpi->use_svc==0.
//
// libvpx: vp9_speed_features.c:517 (reference_masking), :640 (bias_golden),
// :644 (nonrd_keyframe), :647 (overshoot_detection RE_ENCODE), :659
// (use_source_sad), :689-693 (temporal_layer fork), :706-716 (svc spatial /
// temporal mv fork), :721-734 (copy_partition_flag / max_copied_frame),
// :738-741 (svc_use_lowres_part), :742-747 (use_gf_temporal_ref), :754-758
// (nonrd_keyframe + max_copied_frame), :771 (low_temp_var !use_svc), :845-848
// (previous_frame_is_intra_only), :850-857 (high_num_blocks_with_motion).
func TestVP9SpeedFeaturesSVCDefaultsSingleLayer(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           w,
		Height:          h,
		Deadline:        DeadlineRealtime,
		CpuUsed:         7,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// libvpx: vp9_svc_layercontext.h:80-208 — single-layer defaults.
	if e.svc.UseSvc {
		t.Errorf("e.svc.UseSvc = true, want false (cpi->use_svc default = 0)")
	}
	if got, want := e.svc.NumberSpatialLayers, 1; got != want {
		t.Errorf("e.svc.NumberSpatialLayers = %d, want %d", got, want)
	}
	if got, want := e.svc.NumberTemporalLayers, 1; got != want {
		t.Errorf("e.svc.NumberTemporalLayers = %d, want %d", got, want)
	}
	if e.svc.SpatialLayerID != 0 || e.svc.TemporalLayerID != 0 {
		t.Errorf("e.svc.SpatialLayerID/TemporalLayerID = (%d,%d), want (0,0)",
			e.svc.SpatialLayerID, e.svc.TemporalLayerID)
	}

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:517 — reference_masking = number_spatial_layers == 1.
	if sf.ReferenceMasking != 1 {
		t.Errorf("ReferenceMasking = %d, want 1 (libvpx vp9_speed_features.c:517 single layer)", sf.ReferenceMasking)
	}

	// libvpx: vp9_speed_features.c:640 — bias_golden = !cpi->use_svc (CBR non-screen, speed 5+).
	if sf.BiasGolden != 1 {
		t.Errorf("BiasGolden = %d, want 1 (libvpx vp9_speed_features.c:640 !use_svc)", sf.BiasGolden)
	}

	// libvpx: vp9_speed_features.c:644 — nonrd_keyframe stays 0 at speed 5+ when use_svc==0.
	// Note: speed-7 doesn't toggle this; speed-8 forces nonrd_keyframe = 1.
	if sf.NonrdKeyframe != 0 {
		t.Errorf("NonrdKeyframe = %d, want 0 at speed 7 (libvpx vp9_speed_features.c:644 !use_svc)", sf.NonrdKeyframe)
	}

	// libvpx: vp9_speed_features.c:659 — use_source_sad = !external_resize.
	if sf.UseSourceSad != 1 {
		t.Errorf("UseSourceSad = %d, want 1 (libvpx vp9_speed_features.c:659 !external_resize)", sf.UseSourceSad)
	}

	// libvpx: vp9_speed_features.c:721-734 — copy_partition_flag = 1 when not
	// SVC + not external_resize + not last_frame_dropped.
	if sf.CopyPartitionFlag != 1 {
		t.Errorf("CopyPartitionFlag = %d, want 1 (libvpx vp9_speed_features.c:727 single layer)", sf.CopyPartitionFlag)
	}

	// libvpx: vp9_speed_features.c:728 — single-layer max_copied_frame = 2.
	if e.maxCopiedFrame != 2 {
		t.Errorf("maxCopiedFrame = %d, want 2 (libvpx vp9_speed_features.c:728)", e.maxCopiedFrame)
	}

	// libvpx: vp9_speed_features.c:738-741 — svc_use_lowres_part requires SVC.
	if sf.SvcUseLowresPart != 0 {
		t.Errorf("SvcUseLowresPart = %d, want 0 (libvpx vp9_speed_features.c:738-741 !use_svc; also forced 0 at line 870)", sf.SvcUseLowresPart)
	}

	// libvpx: vp9_speed_features.c:747 — ref_frame_flags must keep VP9_GOLD_FLAG
	// when !use_svc.
	if e.refFrameFlags&encoder.GoldFlag == 0 {
		t.Errorf("refFrameFlags = %x, want VP9_GOLD_FLAG set (libvpx vp9_speed_features.c:747 !use_svc)", e.refFrameFlags)
	}

	// libvpx: vp9_speed_features.c:706-711 — base spatial / temporal NSTEP
	// switch requires number_temporal_layers > 2. Single layer keeps FAST_DIAMOND.
	if sf.Mv.SearchMethod != SearchMethodFastDiamond {
		t.Errorf("Mv.SearchMethod = %d, want FAST_DIAMOND (libvpx vp9_speed_features.c:702 default)", sf.Mv.SearchMethod)
	}
	if sf.Mv.FullpelSearchStepParam != 10 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 10 (libvpx vp9_speed_features.c:703 default)", sf.Mv.FullpelSearchStepParam)
	}

	// libvpx: vp9_speed_features.c:845-848 — previous_frame_is_intra_only=false.
	// Partition search type must NOT be forced to FIXED_PARTITION.
	if sf.PartitionSearchType == FixedPartition {
		t.Errorf("PartitionSearchType = FIXED, want non-FIXED (libvpx vp9_speed_features.c:845-848 previous_frame_is_intra_only=false)")
	}
}

// TestVP9SpeedFeaturesSVCTemporalLayerFork pins libvpx's speed-6/7 forks that
// engage when svc->temporal_layer_id > 0.
//
// libvpx: vp9_speed_features.c:689-693 (speed 6),
//
//	vp9_speed_features.c:712-716 (speed 7).
func TestVP9SpeedFeaturesSVCTemporalLayerFork(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           w,
		Height:          h,
		Deadline:        DeadlineRealtime,
		CpuUsed:         6,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	// Force the SVC state directly to model libvpx cpi->svc.
	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 2
	e.svc.SpatialLayerID = 0
	e.svc.TemporalLayerID = 1

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 6, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 6, ctx)

	// libvpx: vp9_speed_features.c:690-692 — temporal_layer_id > 0 path.
	if sf.AdaptiveRdThresh != 4 {
		t.Errorf("AdaptiveRdThresh = %d, want 4 (libvpx vp9_speed_features.c:690 temporal_layer_id > 0)", sf.AdaptiveRdThresh)
	}
	if sf.LimitNewmvEarlyExit != 0 {
		t.Errorf("LimitNewmvEarlyExit = %d, want 0 (libvpx vp9_speed_features.c:691 temporal_layer_id > 0)", sf.LimitNewmvEarlyExit)
	}
	if sf.BaseMvAggressive != 1 {
		t.Errorf("BaseMvAggressive = %d, want 1 (libvpx vp9_speed_features.c:692 temporal_layer_id > 0)", sf.BaseMvAggressive)
	}

	// libvpx: vp9_speed_features.c:644 — non-base spatial layer forces nonrd_keyframe.
	// Base spatial layer (SpatialLayerID=0) does not engage this branch.
	if sf.NonrdKeyframe != 0 {
		t.Errorf("NonrdKeyframe = %d, want 0 (libvpx vp9_speed_features.c:644 spatial_layer_id == 0)", sf.NonrdKeyframe)
	}

	// Now bump SpatialLayerID = 1 and re-dispatch at speed 5+, expect
	// nonrd_keyframe=1. Rebuild the context so ctx.svc captures the updated
	// state.
	e.svc.SpatialLayerID = 1
	ctx = e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false
	sf = SpeedFeatures{}
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 5, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 5, ctx)
	if sf.NonrdKeyframe != 1 {
		t.Errorf("NonrdKeyframe = %d, want 1 (libvpx vp9_speed_features.c:644 spatial_layer_id > 0)", sf.NonrdKeyframe)
	}
}
