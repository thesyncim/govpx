package govpx

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9EncoderInterFilterRDCollectionGate(t *testing.T) {
	var counts vp9enc.FrameCounts
	e := &VP9Encoder{}
	e.sf.FrameParameterUpdate = 1
	inter := &vp9InterEncodeState{
		counts:       &counts,
		interpFilter: vp9dec.InterpSwitchable,
	}
	if !e.vp9ShouldCollectInterFilterRD(inter, false) {
		t.Fatal("full-RD counts pass with switchable filter did not collect")
	}
	if e.vp9ShouldCollectInterFilterRD(inter, true) {
		t.Fatal("nonrd pass collected filter RD scores")
	}
	e.sf.FrameParameterUpdate = 0
	if e.vp9ShouldCollectInterFilterRD(inter, false) {
		t.Fatal("frame_parameter_update=0 collected filter RD scores")
	}
	e.sf.FrameParameterUpdate = 1
	inter.counts = nil
	if e.vp9ShouldCollectInterFilterRD(inter, false) {
		t.Fatal("tile write without counts collected filter RD scores")
	}
	inter.counts = &counts
	inter.interpFilter = vp9dec.InterpEighttap
	if e.vp9ShouldCollectInterFilterRD(inter, false) {
		t.Fatal("fixed frame interpolation filter collected switchable RD scores")
	}
}

func TestVP9EncoderInterFilterDiffAccumulatesFinalLeafScores(t *testing.T) {
	var scores [vp9dec.SwitchableFilterContexts]uint64
	vp9InitFilterRDScores(&scores)
	vp9RecordFilterRDScore(&scores, vp9dec.InterpEighttap, 100, 116)
	vp9RecordFilterRDScore(&scores, vp9dec.InterpEighttapSmooth, 95, 110)
	vp9RecordFilterRDScore(&scores, vp9dec.InterpEighttapSharp, 130, 140)

	var counts vp9enc.FrameCounts
	e := &VP9Encoder{}
	inter := &vp9InterEncodeState{counts: &counts}
	e.vp9StoreBlockFilterRDScores(&scores)
	e.vp9AccumulateBlockFilterDiff(inter, 110, false)

	want := [vp9dec.SwitchableFilterContexts]int64{10, 15, -20, 0}
	if e.vp9FilterDiff != want {
		t.Fatalf("filter diff = %v, want %v", e.vp9FilterDiff, want)
	}
	if e.vp9BlockFilterRDValid {
		t.Fatal("block filter RD scores remained valid after accumulation")
	}
	e.vp9AccumulateBlockFilterDiff(inter, 110, false)
	if e.vp9FilterDiff != want {
		t.Fatalf("second accumulation changed filter diff to %v, want %v",
			e.vp9FilterDiff, want)
	}
}

func TestVP9EncoderInterFilterDiffSkipsDiscardedScores(t *testing.T) {
	var scores [vp9dec.SwitchableFilterContexts]uint64
	vp9InitFilterRDScores(&scores)
	vp9RecordFilterRDScore(&scores, vp9dec.InterpEighttap, 100, 112)

	var counts vp9enc.FrameCounts
	e := &VP9Encoder{}
	inter := &vp9InterEncodeState{counts: &counts}
	e.vp9StoreBlockFilterRDScores(&scores)
	e.vp9AccumulateBlockFilterDiff(inter, 110, true)

	if e.vp9FilterDiff != ([vp9dec.SwitchableFilterContexts]int64{}) {
		t.Fatalf("skipped filter diff = %v, want zero", e.vp9FilterDiff)
	}
	if e.vp9BlockFilterRDValid {
		t.Fatal("skipped block filter RD scores remained valid")
	}
}

func TestVP9EncoderInterFilterDiffSkipsFinalIntraOrSkippedBlocks(t *testing.T) {
	var scores [vp9dec.SwitchableFilterContexts]uint64
	vp9InitFilterRDScores(&scores)
	vp9RecordFilterRDScore(&scores, vp9dec.InterpEighttap, 100, 112)
	vp9RecordFilterRDScore(&scores, vp9dec.InterpEighttapSmooth, 120, 132)

	var counts vp9enc.FrameCounts
	e := &VP9Encoder{}
	inter := &vp9InterEncodeState{counts: &counts}

	e.vp9StoreBlockFilterRDScores(&scores)
	e.vp9AccumulateBlockFilterDiff(inter, 90, true)
	if e.vp9FilterDiff != ([vp9dec.SwitchableFilterContexts]int64{}) {
		t.Fatalf("final intra/skipped filter diff = %v, want zero", e.vp9FilterDiff)
	}

	e.vp9StoreBlockFilterRDScores(&scores)
	e.vp9AccumulateBlockFilterDiff(inter, 90, false)
	if e.vp9FilterDiff == ([vp9dec.SwitchableFilterContexts]int64{}) {
		t.Fatal("inter-coded block did not accumulate filter diff")
	}
}

func TestVP9EncoderFilterDiffMergesTileWorkers(t *testing.T) {
	dst := [vp9dec.SwitchableFilterContexts]int64{1, 2, 3, 4}
	src := [vp9dec.SwitchableFilterContexts]int64{5, -1, 0, 7}
	addVP9FilterDiff(&dst, &src)

	want := [vp9dec.SwitchableFilterContexts]int64{6, 1, 3, 11}
	if dst != want {
		t.Fatalf("merged filter diff = %v, want %v", dst, want)
	}
}
