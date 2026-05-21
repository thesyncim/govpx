package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
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

// TestVP9SpeedFeaturesSVCSpeed7BetterMvSearch pins the speed-7 base-temporal
// / base-spatial NSTEP override (libvpx vp9_speed_features.c:706-711) when SVC
// uses 3+ temporal layers and the current layer is the base temporal layer.
func TestVP9SpeedFeaturesSVCSpeed7BetterMvSearch(t *testing.T) {
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

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 1
	e.svc.NumberTemporalLayers = 3
	e.svc.SpatialLayerID = 0
	e.svc.TemporalLayerID = 0

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:706-711 — base temporal + base spatial =>
	// NSTEP + step_param = 6.
	if sf.Mv.SearchMethod != SearchMethodNStep {
		t.Errorf("Mv.SearchMethod = %d, want NSTEP (libvpx vp9_speed_features.c:709)", sf.Mv.SearchMethod)
	}
	if sf.Mv.FullpelSearchStepParam != 6 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 6 (libvpx vp9_speed_features.c:710)", sf.Mv.FullpelSearchStepParam)
	}
}

// TestVP9SpeedFeaturesSVCSpeed7NonReferenceFrame pins the speed-7 non-reference
// pruning that engages when svc->non_reference_frame is true.
//
// libvpx: vp9_speed_features.c:712-716.
func TestVP9SpeedFeaturesSVCSpeed7NonReferenceFrame(t *testing.T) {
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

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 2
	e.svc.SpatialLayerID = 0
	e.svc.TemporalLayerID = 1
	e.svc.NonReferenceFrame = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:713 — temporal_layer_id > 0 => use_simple_block_yrd = 1.
	if sf.UseSimpleBlockYrd != 1 {
		t.Errorf("UseSimpleBlockYrd = %d, want 1 (libvpx vp9_speed_features.c:713 temporal_layer_id > 0)", sf.UseSimpleBlockYrd)
	}

	// libvpx: vp9_speed_features.c:714-715 — non_reference_frame =>
	// subpel_search_method = SUBPEL_TREE_PRUNED_EVENMORE.
	if sf.Mv.SubpelSearchMethod != SubpelTreePrunedEvenMore {
		t.Errorf("Mv.SubpelSearchMethod = %d, want SUBPEL_TREE_PRUNED_EVENMORE (libvpx vp9_speed_features.c:715)", sf.Mv.SubpelSearchMethod)
	}
}

// TestVP9SpeedFeaturesSVCSpeed7CopyPartitionTopLayer pins the speed-7
// max_copied_frame and copy_partition_flag behaviour for SVC top spatial
// layer + top temporal layer.
//
// libvpx: vp9_speed_features.c:721-734.
func TestVP9SpeedFeaturesSVCSpeed7CopyPartitionTopLayer(t *testing.T) {
	const w, h = 320, 240
	cases := []struct {
		name             string
		spatialID        int
		numSpatial       int
		temporalID       int
		numTemporal      int
		lastLayerDropped bool
		wantCopyFlag     int
		wantMaxCopied    int
	}{
		{
			// libvpx: top spatial + top temporal => 255.
			name:          "top-spatial-top-temporal",
			spatialID:     1,
			numSpatial:    2,
			temporalID:    1,
			numTemporal:   2,
			wantCopyFlag:  1,
			wantMaxCopied: 255,
		},
		{
			// libvpx: top spatial + base temporal => 2.
			name:          "top-spatial-base-temporal",
			spatialID:     1,
			numSpatial:    2,
			temporalID:    0,
			numTemporal:   2,
			wantCopyFlag:  1,
			wantMaxCopied: 2,
		},
		{
			// libvpx: non-top spatial => copy_partition_flag stays 0,
			// max_copied_frame stays 0.
			name:          "non-top-spatial",
			spatialID:     0,
			numSpatial:    2,
			temporalID:    0,
			numTemporal:   2,
			wantCopyFlag:  0,
			wantMaxCopied: 0,
		},
		{
			// libvpx: top spatial but last_layer_dropped[top] => skip.
			name:             "top-spatial-but-dropped",
			spatialID:        1,
			numSpatial:       2,
			temporalID:       1,
			numTemporal:      2,
			lastLayerDropped: true,
			wantCopyFlag:     0,
			wantMaxCopied:    0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

			e.svc.UseSvc = true
			e.svc.NumberSpatialLayers = tc.numSpatial
			e.svc.NumberTemporalLayers = tc.numTemporal
			e.svc.SpatialLayerID = tc.spatialID
			e.svc.TemporalLayerID = tc.temporalID
			e.svc.LastLayerDropped[tc.numSpatial-1] = tc.lastLayerDropped

			ctx := e.vp9DefaultSpeedFrameContext()
			ctx.frameType = common.InterFrame
			ctx.intraOnly = false

			var sf SpeedFeatures
			vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

			if sf.CopyPartitionFlag != tc.wantCopyFlag {
				t.Errorf("CopyPartitionFlag = %d, want %d (libvpx vp9_speed_features.c:727)", sf.CopyPartitionFlag, tc.wantCopyFlag)
			}
			if e.maxCopiedFrame != tc.wantMaxCopied {
				t.Errorf("maxCopiedFrame = %d, want %d (libvpx vp9_speed_features.c:728-733)", e.maxCopiedFrame, tc.wantMaxCopied)
			}
		})
	}
}

// TestVP9SpeedFeaturesSVCSpeed7LowresPart pins
// svc_use_lowres_part: requires SVC, use_partition_reuse, 3 spatial layers,
// temporal_layer_id > 0, top-resolution > VGA. libvpx forces it back to 0 at
// the end of the dispatcher (line 870), so it is never observable as 1
// downstream — but the configurator output must clear cleanly through both
// gates. We exercise the inner gate and verify the post-dispatch zero.
//
// libvpx: vp9_speed_features.c:738-741, vp9_speed_features.c:870.
func TestVP9SpeedFeaturesSVCSpeed7LowresPartForcedOff(t *testing.T) {
	const w, h = 1280, 720
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

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 3
	e.svc.NumberTemporalLayers = 2
	e.svc.SpatialLayerID = 2
	e.svc.TemporalLayerID = 1
	e.svc.UsePartitionReuse = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:870 — forced back to 0 after dispatch.
	if sf.SvcUseLowresPart != 0 {
		t.Errorf("SvcUseLowresPart = %d, want 0 (libvpx vp9_speed_features.c:870 force-off)", sf.SvcUseLowresPart)
	}
}

// TestVP9SpeedFeaturesSVCSpeed7GoldenTemporalRef pins the speed-7
// use_gf_temporal_ref_current_layer fork that clears VP9_GOLD_FLAG from
// cpi->ref_frame_flags on non-base temporal layers.
//
// libvpx: vp9_speed_features.c:742-747.
func TestVP9SpeedFeaturesSVCSpeed7GoldenTemporalRef(t *testing.T) {
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

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 2
	e.svc.SpatialLayerID = 0
	e.svc.TemporalLayerID = 1
	e.svc.UseGfTemporalRefCurrentLayer = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)
	_ = sf

	// libvpx: vp9_speed_features.c:747 — cpi->ref_frame_flags &= ~VP9_GOLD_FLAG.
	if e.refFrameFlags&encoder.GoldFlag != 0 {
		t.Errorf("refFrameFlags = %#x, want VP9_GOLD_FLAG cleared (libvpx vp9_speed_features.c:747)", e.refFrameFlags)
	}
	if e.refFrameFlags&encoder.LastFlag == 0 {
		t.Errorf("refFrameFlags = %#x, want VP9_LAST_FLAG preserved", e.refFrameFlags)
	}
}

// TestVP9SpeedFeaturesSVCSpeed8MaxCopiedFrame pins libvpx's speed-8
// max_copied_frame override that skips for SVC encoders.
//
// libvpx: vp9_speed_features.c:758.
func TestVP9SpeedFeaturesSVCSpeed8MaxCopiedFrame(t *testing.T) {
	t.Run("single-layer", func(t *testing.T) {
		const w, h = 320, 240
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:           w,
			Height:          h,
			Deadline:        DeadlineRealtime,
			CpuUsed:         8,
			RateControlMode: RateControlCBR,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()

		ctx := e.vp9DefaultSpeedFrameContext()
		ctx.frameType = common.InterFrame
		ctx.intraOnly = false

		var sf SpeedFeatures
		vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
		vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)

		// libvpx: vp9_speed_features.c:758 — !use_svc => max_copied_frame = 4.
		if e.maxCopiedFrame != 4 {
			t.Errorf("maxCopiedFrame = %d, want 4 (libvpx vp9_speed_features.c:758 !use_svc)", e.maxCopiedFrame)
		}
	})

	t.Run("svc-non-top-spatial", func(t *testing.T) {
		const w, h = 320, 240
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:           w,
			Height:          h,
			Deadline:        DeadlineRealtime,
			CpuUsed:         8,
			RateControlMode: RateControlCBR,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()

		e.svc.UseSvc = true
		e.svc.NumberSpatialLayers = 2
		e.svc.SpatialLayerID = 0

		ctx := e.vp9DefaultSpeedFrameContext()
		ctx.frameType = common.InterFrame
		ctx.intraOnly = false

		var sf SpeedFeatures
		vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
		vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)

		// libvpx: vp9_speed_features.c:758 — use_svc=true => skip setter, stays 0.
		if e.maxCopiedFrame != 0 {
			t.Errorf("maxCopiedFrame = %d, want 0 (libvpx vp9_speed_features.c:758 use_svc skip)", e.maxCopiedFrame)
		}
	})
}

// TestVP9SpeedFeaturesSVCSpeed8NonrdKeyframeFork pins libvpx's speed-8
// nonrd_keyframe fork that engages when SVC has multiple spatial layers and
// simulcast_mode is off.
//
// libvpx: vp9_speed_features.c:754-757.
func TestVP9SpeedFeaturesSVCSpeed8NonrdKeyframeFork(t *testing.T) {
	cases := []struct {
		name              string
		numSpatial        int
		simulcastMode     bool
		wantNonrdKeyframe int
	}{
		{"single-layer", 1, false, 1},
		{"svc-simulcast", 2, true, 1},
		{"svc-non-simulcast", 2, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const w, h = 320, 240
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:           w,
				Height:          h,
				Deadline:        DeadlineRealtime,
				CpuUsed:         8,
				RateControlMode: RateControlCBR,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()
			if tc.numSpatial > 1 {
				e.svc.UseSvc = true
				e.svc.NumberSpatialLayers = tc.numSpatial
			}
			e.svc.SimulcastMode = tc.simulcastMode

			ctx := e.vp9DefaultSpeedFrameContext()
			ctx.frameType = common.InterFrame
			ctx.intraOnly = false

			var sf SpeedFeatures
			vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 8, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 8, ctx)

			if sf.NonrdKeyframe != tc.wantNonrdKeyframe {
				t.Errorf("NonrdKeyframe = %d, want %d (libvpx vp9_speed_features.c:754-757)", sf.NonrdKeyframe, tc.wantNonrdKeyframe)
			}
		})
	}
}

// TestVP9SpeedFeaturesSVCPreviousFrameIntraOnly pins the post-cascade fixup
// that forces FIXED_PARTITION + BLOCK_64X64 when the previous frame was an
// intra-only SVC frame.
//
// libvpx: vp9_speed_features.c:845-848.
func TestVP9SpeedFeaturesSVCPreviousFrameIntraOnly(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           w,
		Height:          h,
		Deadline:        DeadlineRealtime,
		CpuUsed:         5,
		RateControlMode: RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 1
	e.svc.SpatialLayerID = 0
	e.svc.PreviousFrameIsIntraOnly = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 5, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 5, ctx)

	// libvpx: vp9_speed_features.c:846-847.
	if sf.PartitionSearchType != FixedPartition {
		t.Errorf("PartitionSearchType = %d, want FIXED (libvpx vp9_speed_features.c:846 previous_frame_is_intra_only)", sf.PartitionSearchType)
	}
	if sf.AlwaysThisBlockSize != common.Block64x64 {
		t.Errorf("AlwaysThisBlockSize = %d, want BLOCK_64X64 (libvpx vp9_speed_features.c:847)", sf.AlwaysThisBlockSize)
	}
}

// TestVP9SpeedFeaturesSVCScreenContentHighMotion pins the screen-content
// fixup that switches base-spatial motion search to NSTEP step_param=2 when
// high_num_blocks_with_motion or last_layer_dropped[0] is set.
//
// libvpx: vp9_speed_features.c:849-857.
func TestVP9SpeedFeaturesSVCScreenContentHighMotion(t *testing.T) {
	const w, h = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             w,
		Height:            h,
		Deadline:          DeadlineRealtime,
		CpuUsed:           7,
		ScreenContentMode: int8(VP9ScreenContentScreen),
		RateControlMode:   RateControlCBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	e.svc.UseSvc = true
	e.svc.NumberSpatialLayers = 2
	e.svc.NumberTemporalLayers = 1
	e.svc.SpatialLayerID = 0
	e.svc.HighNumBlocksWithMotion = true

	ctx := e.vp9DefaultSpeedFrameContext()
	ctx.frameType = common.InterFrame
	ctx.intraOnly = false

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 7, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 7, ctx)

	// libvpx: vp9_speed_features.c:853-856.
	if sf.Mv.SearchMethod != SearchMethodNStep {
		t.Errorf("Mv.SearchMethod = %d, want NSTEP (libvpx vp9_speed_features.c:853 screen content high motion)", sf.Mv.SearchMethod)
	}
	if sf.Mv.FullpelSearchStepParam != 2 {
		t.Errorf("Mv.FullpelSearchStepParam = %d, want 2 (libvpx vp9_speed_features.c:856)", sf.Mv.FullpelSearchStepParam)
	}
}

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
