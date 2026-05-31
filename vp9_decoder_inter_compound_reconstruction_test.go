package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
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
