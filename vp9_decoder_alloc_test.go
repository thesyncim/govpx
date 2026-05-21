package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderDecodeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("Decode steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode loop-filtered keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderThreadedLoopFilteredKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 3})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if d.vp9LoopFilterPool == nil {
		t.Fatal("threaded VP9 decoder did not initialize loop-filter pool")
	}
	if got, want := d.vp9LoopFilterPool.helperCount, int8(2); got != want {
		t.Fatalf("VP9 loop-filter helper count = %d, want %d", got, want)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode threaded loop-filtered keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode threaded loop-filtered keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("threaded loop-filtered keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderSegmentedAltQKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9SegmentedAltQKeyframeForTest(t)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode segmented alt-q keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode segmented alt-q keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented alt-q keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9DecoderDecodeIntoSteadyStateAlloc keeps caller-owned VP9 output
// allocation-free after the decoder and reference slots are warm.
func TestVP9DecoderDecodeIntoSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
	if _, err := d.DecodeInto(packet, &dst); err != nil {
		t.Fatalf("warm DecodeInto err = %v, want nil", err)
	}

	var info VP9FrameInfo
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		info, err = d.DecodeInto(packet, &dst)
	})
	if err != nil {
		t.Fatalf("DecodeInto err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame {
		t.Fatalf("DecodeInto info = %+v, want visible 96x96 frame", info)
	}
	if allocs != 0 {
		t.Fatalf("DecodeInto steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterTileParseSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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

func TestVP9DecoderCompoundGoldenAltrefNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	inter := vp9CompoundInterGoldenAltrefNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound golden/altref newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound golden/altref newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound golden/altref newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundSignBiasLayoutsSteadyStateAlloc(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame func(*testing.T) []byte
	}{
		{"fixed-golden", vp9CompoundFixedGoldenSignBiasNewMvFrameForTest},
		{"fixed-last", vp9CompoundFixedLastSignBiasNewMvFrameForTest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
			inter := tc.frame(t)
			if err := d.Decode(inter); err != nil {
				t.Fatalf("warm Decode compound %s sign-bias err = %v, want nil",
					tc.name, err)
			}

			allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
				err = d.Decode(inter)
			})
			if err != nil {
				t.Fatalf("Decode compound %s sign-bias err = %v, want nil",
					tc.name, err)
			}
			if allocs != 0 {
				t.Fatalf("compound %s sign-bias steady state: got %v allocs/op, want 0",
					tc.name, allocs)
			}
		})
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

func TestVP9DecoderScaledCompoundInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)
	inter := vp9ScaledCompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter nearestmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledCompoundInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)
	inter := vp9ScaledCompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9CompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter nearmv steady state: got %v allocs/op, want 0", allocs)
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
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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

func TestVP9DecoderInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter nearestmv steady state: got %v allocs/op, want 0", allocs)
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
