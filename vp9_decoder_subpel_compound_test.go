package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9DecoderReconstructsInterSubpelNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var want [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, want[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	inter := vp9InterSubpelNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel newmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride]; got != want[0] {
		t.Fatalf("middle-left subpel newmv Y[32,0] = %d, want filtered reference %d",
			got, want[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsInterSubpelNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var topWant, middleWant [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, topWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0, 32)
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, middleWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	inter := vp9InterSubpelNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel nearestmv frame did not publish output")
	}
	if got := frame.Y[0]; got != topWant[0] {
		t.Fatalf("top-left subpel newmv Y[0,0] = %d, want filtered reference %d",
			got, topWant[0])
	}
	if got := frame.Y[32*frame.YStride]; got != middleWant[0] {
		t.Fatalf("middle-left subpel nearestmv Y[32,0] = %d, want filtered reference %d",
			got, middleWant[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsInterSubpelBilinearNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsInterSubpelNewMvFilter(t,
		vp9InterSubpelBilinearNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpBilinear])
}

func TestVP9DecoderReconstructsInterSubpelSwitchableSmoothNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsInterSubpelNewMvFilter(t,
		vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpEighttapSmooth])
}

func TestVP9DecoderReconstructsCompoundInterSubpelBilinearNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t,
		vp9CompoundInterSubpelBilinearNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpBilinear])
}

func TestVP9DecoderReconstructsCompoundInterSubpelSwitchableSmoothNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t,
		vp9CompoundInterSubpelSwitchableSmoothNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpEighttapSmooth])
}

func TestVP9DecoderReconstructsInterSubpelSwitchableSharpNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var topWant, middleWant [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, topWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttapSharp],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0, 32)
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, middleWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttapSharp],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	inter := vp9InterSubpelSwitchableSharpNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel switchable sharp nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel switchable sharp nearestmv frame did not publish output")
	}
	if got := frame.Y[0]; got != topWant[0] {
		t.Fatalf("top-left switchable sharp newmv Y[0,0] = %d, want %d",
			got, topWant[0])
	}
	if got := frame.Y[32*frame.YStride]; got != middleWant[0] {
		t.Fatalf("middle-left switchable sharp nearestmv Y[32,0] = %d, want %d",
			got, middleWant[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsInterSubpelTopRightBorderNewMvFrame(t *testing.T) {
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
	var want [32 * 32]byte
	vp9InterPredictorWithBorderForTest(keyFrame.Y, keyFrame.YStride,
		keyFrame.Width, keyFrame.Height, want[:], 32,
		0, 4, common.Block32x32, vp9dec.MV{Row: -4, Col: 260},
		tables.FilterKernels[vp9dec.InterpEighttap])

	inter := vp9InterSubpelTopRightBorderNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter top-right border subpel newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter top-right border subpel newmv frame did not publish output")
	}
	if got := frame.Y[32]; got != want[0] {
		t.Fatalf("top-right border subpel newmv Y[0,32] = %d, want %d",
			got, want[0])
	}
	if got := frame.Y[32]; got <= 128 {
		t.Fatalf("top-right border subpel newmv Y[0,32] = %d, want residue-driven edge prediction", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterIntegerTopRightBorderNewMvFrame(t *testing.T) {
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
	var want [32 * 32]byte
	vp9InterPredictorWithBorderForTest(keyFrame.Y, keyFrame.YStride,
		keyFrame.Width, keyFrame.Height, want[:], 32,
		0, 4, common.Block32x32, vp9dec.MV{Col: 256},
		tables.FilterKernels[vp9dec.InterpEighttap])

	inter := vp9InterIntegerTopRightBorderNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter top-right border integer newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter top-right border integer newmv frame did not publish output")
	}
	if got := frame.Y[32]; got != want[0] {
		t.Fatalf("top-right border integer newmv Y[0,32] = %d, want %d",
			got, want[0])
	}
	if got := frame.Y[32]; got <= 128 {
		t.Fatalf("top-right border integer newmv Y[0,32] = %d, want residue-driven edge prediction", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func assertVP9DecoderReconstructsInterSubpelNewMvFilter(t *testing.T,
	inter []byte,
	kernel *[tables.SubpelShifts][tables.SubpelTaps]int16,
) {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var want [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, want[:], 32,
		8, 8, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel filtered newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel filtered newmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride]; got != want[0] {
		t.Fatalf("middle-left filtered subpel newmv Y[32,0] = %d, want %d",
			got, want[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t *testing.T,
	inter []byte,
	kernel *[tables.SubpelShifts][tables.SubpelTaps]int16,
) {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 96, 96)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	var lastWant, altWant [32 * 32]byte
	vp9dec.InterPredictor(lastRef.Y, lastRef.YStride, lastWant[:], 32,
		8, 8, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*lastRef.YStride+32)
	vp9dec.InterPredictor(altRef.Y, altRef.YStride, altWant[:], 32,
		8, 8, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*altRef.YStride+32)
	if lastWant[0] == altWant[0] {
		t.Fatalf("compound subpel reference test pattern missing: LAST=%d ALTREF=%d",
			lastWant[0], altWant[0])
	}
	want := byte((int(lastWant[0]) + int(altWant[0]) + 1) >> 1)

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter subpel filtered newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter subpel filtered newmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride]; got != want {
		t.Fatalf("middle-left compound filtered subpel newmv Y[32,0] = %d, want average of LAST %d and ALTREF %d -> %d",
			got, lastWant[0], altWant[0], want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsScaledCompoundInterNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)

	zero := vp9ScaledCompoundInterZeroMvFrameForTest(t)
	if err := d.Decode(zero); err != nil {
		t.Fatalf("Decode scaled compound zero-mv frame: %v", err)
	}
	zeroFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound zero-mv frame did not publish output")
	}
	zeroI420 := appendVP9I420(nil, zeroFrame)

	inter := vp9ScaledCompoundInterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled compound inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound inter newmv frame did not publish output")
	}
	if frame.Width != 32 || frame.Height != 32 {
		t.Fatalf("scaled compound newmv frame = %dx%d, want 32x32",
			frame.Width, frame.Height)
	}
	if bytes.Equal(appendVP9I420(nil, frame), zeroI420) {
		t.Fatal("scaled compound newmv frame matched the zero-mv compound predictor")
	}
}

func TestVP9DecoderReconstructsScaledCompoundInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)

	inter := vp9ScaledCompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled compound inter nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound inter nearestmv frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled compound nearestmv frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearestMv {
		t.Fatalf("bottom-right compound inter mode = %v, want NEARESTMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsScaledCompoundInterNearMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)

	inter := vp9ScaledCompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled compound inter nearmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound inter nearmv frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled compound nearmv frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearMv {
		t.Fatalf("bottom-right compound inter mode = %v, want NEARMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderDecodeIntoInterSubpelNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if _, err := d.DecodeInto(key, &dst); err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	var want [32 * 32]byte
	vp9dec.InterPredictor(dst.Y, dst.YStride, want[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*dst.YStride+32)

	inter := vp9InterSubpelNewMvFrameForTest(t)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter subpel newmv frame: %v", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter subpel newmv info = %+v, want visible non-key 96x96 frame", info)
	}
	if got := dst.Y[32*dst.YStride]; got != want[0] {
		t.Fatalf("DecodeInto middle-left subpel newmv Y[32,0] = %d, want %d",
			got, want[0])
	}
	assertVP9PlaneFilled(t, "U", dst.U, dst.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", dst.V, dst.VStride, 48, 48, 128)
}

func TestVP9DecoderDecodeIntoCompoundInterSubpelNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 96, 96)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	var lastWant, altWant [32 * 32]byte
	vp9dec.InterPredictor(lastRef.Y, lastRef.YStride, lastWant[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*lastRef.YStride+32)
	vp9dec.InterPredictor(altRef.Y, altRef.YStride, altWant[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*altRef.YStride+32)
	want := byte((int(lastWant[0]) + int(altWant[0]) + 1) >> 1)

	dst := newTestImage(96, 96)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(vp9CompoundInterSubpelNewMvFrameForTest(t), &dst)
	if err != nil {
		t.Fatalf("DecodeInto compound inter subpel newmv frame: %v", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto compound inter subpel newmv info = %+v, want visible non-key 96x96 frame", info)
	}
	if got := dst.Y[32*dst.YStride]; got != want {
		t.Fatalf("DecodeInto middle-left compound subpel newmv Y[32,0] = %d, want %d",
			got, want)
	}
	assertVP9PlaneFilled(t, "U", dst.U, dst.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", dst.V, dst.VStride, 48, 48, 128)
}

func TestVP9DecoderDecodeIntoInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(64, 64)
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if _, err := d.DecodeInto(key, &dst); err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	topRight := dst.Y[32]
	bottomRight := dst.Y[32*dst.YStride+32]
	if topRight <= dst.Y[0] || bottomRight <= dst.Y[32*dst.YStride] {
		t.Fatalf("keyframe motion pattern missing: topRight=%d bottomRight=%d",
			topRight, bottomRight)
	}

	inter := vp9InterNearestMvFrameForTest(t)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter nearestmv frame: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter nearestmv info = %+v, want visible non-key 64x64 frame", info)
	}
	if got := dst.Y[0]; got != topRight {
		t.Fatalf("DecodeInto top-left newmv Y[0,0] = %d, want %d", got, topRight)
	}
	if got := dst.Y[32*dst.YStride]; got != bottomRight {
		t.Fatalf("DecodeInto bottom-left nearestmv Y[32,0] = %d, want %d", got, bottomRight)
	}
	assertVP9PlaneFilled(t, "U", dst.U, dst.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", dst.V, dst.VStride, 32, 32, 128)
}
