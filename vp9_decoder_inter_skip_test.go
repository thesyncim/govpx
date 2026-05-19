package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9DecoderParsesInterSkipModeTile(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
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

func TestVP9DecoderDecodeIntoScaledZeroMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}
	dst := newTestImage(32, 32)
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto scaled zero-mv inter frame: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || !info.ShowFrame {
		t.Fatalf("DecodeInto scaled zero-mv info = %+v, want visible 32x32 frame", info)
	}
	left := dst.Y[8*dst.YStride+8]
	right := dst.Y[8*dst.YStride+24]
	if right <= left {
		t.Fatalf("DecodeInto scaled zero-mv right sample = %d, want above left sample %d",
			right, left)
	}
}

func TestVP9DecoderDecodeIntoScaledNewMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}
	dst := newTestImage(32, 32)
	if _, err := d.DecodeInto(vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64), &dst); err != nil {
		t.Fatalf("DecodeInto scaled zero-mv inter frame: %v", err)
	}
	zeroI420 := appendVP9I420(nil, dst)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(vp9ScaledNewMvInterFrameForTest(t), &dst)
	if err != nil {
		t.Fatalf("DecodeInto scaled newmv inter frame: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || !info.ShowFrame {
		t.Fatalf("DecodeInto scaled newmv info = %+v, want visible 32x32 frame", info)
	}
	if bytes.Equal(appendVP9I420(nil, dst), zeroI420) {
		t.Fatal("DecodeInto scaled newmv frame matched the zero-mv scaled predictor")
	}
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

func TestVP9DecoderReconstructsCompoundInterSkipFrame(t *testing.T) {
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

	inter := vp9CompoundInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter skip frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter skip frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("compound inter skip frame did not average matching references back to the source pixels")
	}
}

func TestVP9DecoderReconstructsCompoundInterNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32]
	altPix := altRef.Y[32]
	if lastPix == altPix {
		t.Fatalf("compound reference test pattern missing: LAST=%d ALTREF=%d", lastPix, altPix)
	}
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound newmv Y[0,0] = %d, want average of LAST %d and ALTREF %d -> %d",
			got, lastPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundGoldenAltrefNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	goldenRef := d.refFrames[vp9CompoundGoldenSlotForTest].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	goldenPix := goldenRef.Y[32]
	altPix := altRef.Y[32]
	if goldenPix == altPix {
		t.Fatalf("compound reference test pattern missing: GOLDEN=%d ALTREF=%d",
			goldenPix, altPix)
	}
	want := byte((int(goldenPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterGoldenAltrefNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound golden/altref newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound golden/altref newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound golden/altref newmv Y[0,0] = %d, want average of GOLDEN %d and ALTREF %d -> %d",
			got, goldenPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundFixedGoldenSignBiasNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	goldenRef := d.refFrames[vp9CompoundGoldenSlotForTest].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	goldenPix := goldenRef.Y[32]
	altPix := altRef.Y[32]
	if goldenPix == altPix {
		t.Fatalf("compound fixed-GOLDEN pattern missing: GOLDEN=%d ALTREF=%d",
			goldenPix, altPix)
	}
	want := byte((int(goldenPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundFixedGoldenSignBiasNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound fixed-GOLDEN sign-bias frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound fixed-GOLDEN sign-bias frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound fixed-GOLDEN Y[0,0] = %d, want average of GOLDEN %d and ALTREF %d -> %d",
			got, goldenPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundFixedLastSignBiasNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32]
	altPix := altRef.Y[32]
	if lastPix == altPix {
		t.Fatalf("compound fixed-LAST pattern missing: LAST=%d ALTREF=%d",
			lastPix, altPix)
	}
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundFixedLastSignBiasNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound fixed-LAST sign-bias frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound fixed-LAST sign-bias frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound fixed-LAST Y[0,0] = %d, want average of LAST %d and ALTREF %d -> %d",
			got, lastPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterReferenceModeSelectNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32]
	altPix := altRef.Y[32]
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterReferenceModeSelectNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode reference-mode-select compound inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("reference-mode-select compound inter newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left reference-mode-select compound newmv Y[0,0] = %d, want %d",
			got, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[16*lastRef.YStride+32]
	altPix := altRef.Y[16*altRef.YStride+32]
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter nearestmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride+32]; got != want {
		t.Fatalf("bottom-right compound nearestmv Y[32,32] = %d, want %d", got, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterNearMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32*lastRef.YStride+32]
	altPix := altRef.Y[32*altRef.YStride+32]
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter nearmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter nearmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride+32]; got != want {
		t.Fatalf("bottom-right compound nearmv Y[32,32] = %d, want %d", got, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterSubpelNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t,
		vp9CompoundInterSubpelNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpEighttap])
}

func TestVP9DecoderReconstructsInterIntraSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter-intra skip frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter-intra skip frame did not publish output")
	}
	assertVP9FilledFrame(t, frame, 64, 64, 127, 128, 128)
}

func TestVP9DecoderReconstructsInterIntraResidueFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterIntraFrameForTest(t, common.DcPred, common.DcPred, false, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter-intra residue frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter-intra residue frame did not publish output")
	}
	if got := frame.Y[0]; got <= 128 {
		t.Fatalf("inter-intra residue Y[0,0] = %d, want residual above predictor", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderDecodeIntoCopiesInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	dst := newTestImage(64, 64)
	info, err := d.DecodeInto(key, &dst)
	if err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	if !info.ShowFrame {
		t.Fatalf("DecodeInto keyframe info = %+v, want visible frame", info)
	}
	want := appendVP9I420(nil, dst)

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	fillVP9PublicImage(&dst, 77)
	info, err = d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter skip frame: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter info = %+v, want visible non-key 64x64 frame", info)
	}
	got := appendVP9I420(nil, dst)
	if !bytes.Equal(got, want) {
		t.Fatal("DecodeInto inter skip frame did not copy the LAST reference pixels")
	}
}

func TestVP9DecoderReconstructsInterResidueFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	if got := keyFrame.Y[0]; got != 128 {
		t.Fatalf("keyframe Y[0,0] = %d, want neutral predictor", got)
	}
	refY0 := keyFrame.Y[0]

	inter := vp9InterResidueFrameForTest(t, 64, 64, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter residue frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter residue frame did not publish output")
	}
	if got := frame.Y[0]; got <= refY0 {
		t.Fatalf("inter residue Y[0,0] = %d, want above copied reference %d",
			got, refY0)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterResidueEdgeFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 96, 96, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode edge keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	refY0 := keyFrame.Y[0]

	inter := vp9InterResidueFrameForTest(t, 96, 96, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode edge inter residue frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("edge inter residue frame did not publish output")
	}
	if got := frame.Y[0]; got <= refY0 {
		t.Fatalf("edge inter residue Y[0,0] = %d, want above copied reference %d",
			got, refY0)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderAppliesLoopFilterInterMotionFrame(t *testing.T) {
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	unfilteredInter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 0)
	filteredInter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)

	unfiltered := vp9DecodeLastVisibleFrameForTest(t, key, unfilteredInter)
	filtered := vp9DecodeLastVisibleFrameForTest(t, key, filteredInter)
	if !vp9YRectDiffers(unfiltered, filtered, 28, 32, 12, 32) {
		t.Fatal("loop-filtered inter motion luma matches unfiltered prediction edge")
	}
	if bytes.Equal(appendVP9YForTest(nil, unfiltered), appendVP9YForTest(nil, filtered)) {
		t.Fatal("loop-filtered inter motion luma matches unfiltered luma")
	}
	assertVP9PlaneFilled(t, "U", filtered.U, filtered.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", filtered.V, filtered.VStride, 32, 32, 128)
}

func TestVP9DecoderInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderReconstructsInterNewMvFrame(t *testing.T) {
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
	refY32 := keyFrame.Y[32]
	if refY32 <= keyFrame.Y[0] {
		t.Fatalf("keyframe test pattern missing: Y[32]=%d Y[0]=%d",
			refY32, keyFrame.Y[0])
	}

	inter := vp9InterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != refY32 {
		t.Fatalf("top-left newmv Y[0,0] = %d, want copied reference Y[0,32] %d",
			got, refY32)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterNearestMvFrame(t *testing.T) {
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
	topRight := keyFrame.Y[32]
	bottomRight := keyFrame.Y[32*keyFrame.YStride+32]
	if topRight <= keyFrame.Y[0] || bottomRight <= keyFrame.Y[32*keyFrame.YStride] {
		t.Fatalf("keyframe motion pattern missing: topRight=%d bottomRight=%d",
			topRight, bottomRight)
	}

	inter := vp9InterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter nearestmv frame did not publish output")
	}
	if got := frame.Y[0]; got != topRight {
		t.Fatalf("top-left newmv Y[0,0] = %d, want copied reference Y[0,32] %d",
			got, topRight)
	}
	if got := frame.Y[32*frame.YStride]; got != bottomRight {
		t.Fatalf("bottom-left nearestmv Y[32,0] = %d, want copied reference Y[32,32] %d",
			got, bottomRight)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterNearMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter nearmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter nearmv frame did not publish output")
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearMv {
		t.Fatalf("bottom-right inter mode = %v, want NEARMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}
