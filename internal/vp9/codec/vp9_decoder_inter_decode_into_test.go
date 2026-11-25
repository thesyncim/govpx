package codec

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

func TestVP9DecoderReconstructsCompoundInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
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

func TestVP9DecoderDecodeIntoCopiesInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.ColumnResidueKeyframe(t, 64, 64, 0, 32)
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
