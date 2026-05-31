package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderInterTileParseSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter tile parse steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledZeroMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled zero-mv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled zero-mv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled zero-mv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledNewMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	inter := vp9ScaledNewMvInterFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled newmv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled newmv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled newmv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledNearestMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled nearest seed keyframe: %v", err)
	}
	inter := vp9ScaledInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled nearestmv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled nearestmv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled nearestmv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledNearMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled near seed keyframe: %v", err)
	}
	inter := vp9ScaledInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled nearmv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled nearmv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled nearmv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterIntraSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter-intra err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter-intra err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter-intra steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9CompoundInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderSegmentedAltrefInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9SegmentedAltrefInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode segmented altref inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode segmented altref inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented altref inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderSegmentedAltrefInterMapReuseSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	if err := d.Decode(vp9SegmentedAltrefInterSkipFrameForTest(t)); err != nil {
		t.Fatalf("Decode segmented altref inter map seed err = %v, want nil", err)
	}
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode segmented altref inter map-reuse err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode segmented altref inter map-reuse err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented altref inter map-reuse steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterSubpelNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 96, 96)
	inter := vp9CompoundInterSubpelNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledCompoundInterNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9ScaledCompoundInterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9DecoderFrameContextSlotsTrackInterHeaderUpdates keeps VP9's
// four entropy-context slots separate. A valid inter frame may update
// the compressed-header probabilities while reconstructing through the
// skipped zero-MV path; that update belongs only to the selected
// frame_context_idx.

func TestVP9DecoderInterResidueSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterResidueFrameForTest(t, 64, 64, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter residue err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter residue err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter residue steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredInterResidueSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterResidueFrameLoopFilterForTest(t, 64, 64, 32, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode loop-filtered inter residue err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered inter residue err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered inter residue steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredInterMotionSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode loop-filtered inter motion err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered inter motion err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered inter motion steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterSubpelNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSubpelNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterSubpelBorderSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSubpelTopRightBorderNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter border subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter border subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter border subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterSubpelSwitchableSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter switchable subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter switchable subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter switchable subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderDecodeIntoInterSubpelNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	dst := newTestImage(96, 96)
	if _, err := d.DecodeInto(key, &dst); err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	inter := vp9InterSubpelNewMvFrameForTest(t)
	if _, err := d.DecodeInto(inter, &dst); err != nil {
		t.Fatalf("warm DecodeInto inter subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		_, err = d.DecodeInto(inter, &dst)
	})
	if err != nil {
		t.Fatalf("DecodeInto inter subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("DecodeInto inter subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}
