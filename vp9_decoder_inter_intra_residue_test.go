package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderReconstructsInterIntraSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
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
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
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

func TestVP9DecoderReconstructsInterResidueFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
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
	key := vp9test.StubPacket(t, 96, 96, 0, common.DcPred)
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
