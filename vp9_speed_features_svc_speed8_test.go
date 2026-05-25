package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

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
