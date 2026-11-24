package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

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
