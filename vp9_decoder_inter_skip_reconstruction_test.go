package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9DecoderParsesInterSkipModeTile(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	err = d.Decode(inter)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if len(d.miGrid) == 0 {
		t.Fatal("inter tile parse left miGrid empty")
	}
	mi := d.miGrid[0]
	if mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
		t.Fatalf("inter ref frames = %v, want Last/NoRef", mi.RefFrame)
	}
	if mi.Mode != common.ZeroMv || mi.Skip != 1 {
		t.Fatalf("inter MI = mode %d skip %d, want ZeroMv/skip", mi.Mode, mi.Skip)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (64, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter skip frame did not publish reconstructed output")
	}
	assertVP9NeutralFrame(t, frame, 64, 64)
}

func TestVP9DecoderReconstructsInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	want := appendVP9I420(nil, keyFrame)

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter skip frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter skip frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("inter skip frame did not copy the LAST reference pixels")
	}
}

func TestVP9DecoderReconstructsScaledZeroMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9SegmentedAltQKeyframeForTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}

	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled zero-mv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled zero-mv inter frame did not publish output")
	}
	if frame.Width != 32 || frame.Height != 32 {
		t.Fatalf("scaled zero-mv inter frame = %dx%d, want 32x32",
			frame.Width, frame.Height)
	}
	left := frame.Y[8*frame.YStride+8]
	right := frame.Y[8*frame.YStride+24]
	if right <= left {
		t.Fatalf("scaled zero-mv inter right sample = %d, want above left sample %d",
			right, left)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 16, 16, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 16, 16, 128)
}

func TestVP9DecoderReconstructsScaledNewMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9SegmentedAltQKeyframeForTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}

	zero := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	if err := d.Decode(zero); err != nil {
		t.Fatalf("Decode scaled zero-mv inter frame: %v", err)
	}
	zeroFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled zero-mv inter frame did not publish output")
	}
	zeroI420 := appendVP9I420(nil, zeroFrame)

	inter := vp9ScaledNewMvInterFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled newmv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled newmv inter frame did not publish output")
	}
	if frame.Width != 32 || frame.Height != 32 {
		t.Fatalf("scaled newmv inter frame = %dx%d, want 32x32",
			frame.Width, frame.Height)
	}
	if bytes.Equal(appendVP9I420(nil, frame), zeroI420) {
		t.Fatal("scaled newmv inter frame matched the zero-mv scaled predictor")
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 16, 16, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 16, 16, 128)
}

func TestVP9DecoderReconstructsScaledNearestMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled nearest seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled nearest seed keyframe did not publish output")
	}

	zero := vp9ScaledZeroMvInterFrameForTest(t, 64, 64, 128, 128)
	if err := d.Decode(zero); err != nil {
		t.Fatalf("Decode scaled zero-mv inter frame: %v", err)
	}
	zeroFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled zero-mv inter frame did not publish output")
	}
	zeroI420 := appendVP9I420(nil, zeroFrame)

	inter := vp9ScaledInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled nearestmv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled nearestmv inter frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled nearestmv inter frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	if bytes.Equal(appendVP9I420(nil, frame), zeroI420) {
		t.Fatal("scaled nearestmv inter frame matched the zero-mv scaled predictor")
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols].Mode; got != common.NearestMv {
		t.Fatalf("bottom-left inter mode = %v, want NEARESTMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsScaledNearMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled nearmv seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled nearmv seed keyframe did not publish output")
	}

	inter := vp9ScaledInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled nearmv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled nearmv inter frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled nearmv inter frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearMv {
		t.Fatalf("bottom-right inter mode = %v, want NEARMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsSegmentedAltrefInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	want := appendVP9I420(nil, altRef)
	if bytes.Equal(appendVP9I420(nil, lastRef), want) {
		t.Fatal("segmented ref-frame test setup left LAST and ALTREF identical")
	}

	inter := vp9SegmentedAltrefInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode segmented altref inter skip frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("segmented altref inter skip frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("segmented altref inter skip frame did not copy the segment-forced ALTREF pixels")
	}
}

func TestVP9DecoderReconstructsSegmentedAltrefInterMapReuseFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	want := appendVP9I420(nil, altRef)

	if err := d.Decode(vp9SegmentedAltrefInterSkipFrameForTest(t)); err != nil {
		t.Fatalf("Decode segmented altref inter map seed frame: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("segmented altref inter map seed frame did not publish output")
	}
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode segmented altref inter map-reuse frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("segmented altref inter map-reuse frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("segmented altref inter map-reuse frame did not preserve the forced ALTREF segment map")
	}
}
