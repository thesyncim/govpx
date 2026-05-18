package govpx

import (
	"math"
	"testing"
)

func TestVP9GoodSpeedFeaturesTwoPassGraphicsAnimationBranches(t *testing.T) {
	e := newVP9TwoPassSpeedFeatureEncoder(t, VP9FirstPassFrameStats{
		Frame:        0,
		Weight:       1,
		IntraError:   200,
		CodedError:   100,
		SRCodedError: 100,
		PcntInter:    0.9,
		IntraSkipPct: vp9FCAnimationThresh,
		Duration:     1,
		Count:        1,
	}, 3)
	defer e.Close()

	ctx := e.vp9PerFrameSpeedContext(vp9PerFrameSpeedContextArgs{
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		BaseQIndex:         100,
	})
	if ctx.frContentType != vp9FCGraphicsAnimation {
		t.Fatalf("frContentType = %d, want FC_GRAPHICS_ANIMATION", ctx.frContentType)
	}
	if ctx.internalImageEdge {
		t.Fatalf("internalImageEdge = true, want false")
	}

	var sf SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 1, ctx)
	vp9SetSpeedFeaturesFramesizeDependent(e, &sf, 1, ctx)
	if sf.UseSquarePartitionOnly != 0 {
		t.Fatalf("speed1 UseSquarePartitionOnly = %d, want 0 (libvpx vp9_speed_features.c:278-283 boosted graphics)",
			sf.UseSquarePartitionOnly)
	}
	if sf.ExhaustiveSearchesThresh != 1<<23 {
		t.Fatalf("speed1 ExhaustiveSearchesThresh = %d, want %d (libvpx vp9_speed_features.c:313-315)",
			sf.ExhaustiveSearchesThresh, 1<<23)
	}
	if sf.DisableSplitMask != sfDisableCompoundSplit {
		t.Fatalf("speed1 DisableSplitMask = %d, want %d (libvpx vp9_speed_features.c:195-199)",
			sf.DisableSplitMask, sfDisableCompoundSplit)
	}

	sf = SpeedFeatures{}
	vp9SetSpeedFeaturesFramesizeIndependent(e, &sf, 3, ctx)
	wantMesh := vp9GoodQualityMeshPatterns[2]
	for i := range sfMaxMeshSteps {
		if sf.MeshPatterns[i] != wantMesh[i] {
			t.Fatalf("speed3 MeshPatterns[%d] = %+v, want %+v (libvpx vp9_speed_features.c:374-381)",
				i, sf.MeshPatterns[i], wantMesh[i])
		}
	}
}

func TestVP9GoodSpeedFeaturesTwoPassInternalImageEdgeBranches(t *testing.T) {
	edge := newVP9TwoPassSpeedFeatureEncoder(t, VP9FirstPassFrameStats{
		Frame:            0,
		Weight:           1,
		IntraError:       200,
		CodedError:       100,
		SRCodedError:     100,
		PcntInter:        0.9,
		IntraSkipPct:     0,
		InactiveZoneRows: 1,
		Duration:         1,
		Count:            1,
	}, 1)
	defer edge.Close()

	plain := newVP9TwoPassSpeedFeatureEncoder(t, VP9FirstPassFrameStats{
		Frame:        0,
		Weight:       1,
		IntraError:   200,
		CodedError:   100,
		SRCodedError: 100,
		PcntInter:    0.9,
		IntraSkipPct: 0,
		Duration:     1,
		Count:        1,
	}, 1)
	defer plain.Close()

	args := vp9PerFrameSpeedContextArgs{
		ShowFrame:          true,
		RefreshGoldenFrame: true,
		BaseQIndex:         100,
	}
	edgeCtx := edge.vp9PerFrameSpeedContext(args)
	if edgeCtx.frContentType != vp9FCNormal || !edgeCtx.internalImageEdge {
		t.Fatalf("edge ctx content=%d internal=%v, want normal/internal edge",
			edgeCtx.frContentType, edgeCtx.internalImageEdge)
	}
	plainCtx := plain.vp9PerFrameSpeedContext(args)
	if plainCtx.frContentType != vp9FCNormal || plainCtx.internalImageEdge {
		t.Fatalf("plain ctx content=%d internal=%v, want normal/no internal edge",
			plainCtx.frContentType, plainCtx.internalImageEdge)
	}

	var edgeSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(edge, &edgeSF, 1, edgeCtx)
	vp9SetSpeedFeaturesFramesizeDependent(edge, &edgeSF, 1, edgeCtx)
	if edgeSF.UseSquarePartitionOnly != 0 {
		t.Fatalf("edge UseSquarePartitionOnly = %d, want 0 (libvpx vp9_speed_features.c:278-283 boosted internal edge)",
			edgeSF.UseSquarePartitionOnly)
	}
	if edgeSF.DisableSplitMask != sfDisableCompoundSplit {
		t.Fatalf("edge DisableSplitMask = %d, want %d (libvpx vp9_speed_features.c:195-199)",
			edgeSF.DisableSplitMask, sfDisableCompoundSplit)
	}

	var plainSF SpeedFeatures
	vp9SetSpeedFeaturesFramesizeIndependent(plain, &plainSF, 1, plainCtx)
	if plainSF.UseSquarePartitionOnly != 1 {
		t.Fatalf("plain UseSquarePartitionOnly = %d, want 1 (libvpx vp9_speed_features.c:283-287 non-edge inter)",
			plainSF.UseSquarePartitionOnly)
	}
	if plainSF.ExhaustiveSearchesThresh != math.MaxInt32 {
		t.Fatalf("plain ExhaustiveSearchesThresh = %d, want %d",
			plainSF.ExhaustiveSearchesThresh, math.MaxInt32)
	}
}

func newVP9TwoPassSpeedFeatureEncoder(t *testing.T, row VP9FirstPassFrameStats,
	cpuUsed int,
) *VP9Encoder {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		FPS:                30,
		Deadline:           DeadlineGoodQuality,
		CpuUsed:            int8(cpuUsed),
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		TwoPassStats:       FinalizeVP9FirstPassStats([]VP9FirstPassFrameStats{row}),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if !e.twoPass.enabled() {
		e.Close()
		t.Fatalf("two-pass state disabled")
	}
	return e
}
