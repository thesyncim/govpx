package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestInterPartitionSizeHelpers(t *testing.T) {
	horz, vert, split, ok := SquareInterPartitionSizes(common.Block64x64)
	if !ok {
		t.Fatal("SquareInterPartitionSizes(Block64x64) returned !ok")
	}
	if horz != common.Block64x32 || vert != common.Block32x64 ||
		split != common.Block32x32 {
		t.Fatalf("Block64x64 children = (%v, %v, %v), want 64x32, 32x64, 32x32",
			horz, vert, split)
	}
	if _, _, _, ok := SquareInterPartitionSizes(common.Block8x8); ok {
		t.Fatal("SquareInterPartitionSizes(Block8x8) returned ok")
	}

	horz, vert, split, ok = InterRDPartitionSizes(common.Block8x8)
	if !ok {
		t.Fatal("InterRDPartitionSizes(Block8x8) returned !ok")
	}
	if horz != common.Block8x4 || vert != common.Block4x8 ||
		split != common.Block4x4 {
		t.Fatalf("Block8x8 RD children = (%v, %v, %v), want 8x4, 4x8, 4x4",
			horz, vert, split)
	}
	if _, _, _, ok := InterRDPartitionSizes(common.Block4x4); ok {
		t.Fatal("InterRDPartitionSizes(Block4x4) returned ok")
	}
}

func TestVisibleBlockFits(t *testing.T) {
	if !VisibleBlockFits(8, 16, 32, 16, 64, 64) {
		t.Fatal("visible block entirely inside plane reported false")
	}
	tests := []struct {
		name                   string
		x0, y0, blockW, blockH int
		width, height          int
	}{
		{"negative_x", -1, 0, 16, 16, 64, 64},
		{"negative_y", 0, -1, 16, 16, 64, 64},
		{"zero_width", 0, 0, 0, 16, 64, 64},
		{"zero_height", 0, 0, 16, 0, 64, 64},
		{"past_right_edge", 49, 0, 16, 16, 64, 64},
		{"past_bottom_edge", 0, 49, 16, 16, 64, 64},
	}
	for _, tt := range tests {
		if VisibleBlockFits(tt.x0, tt.y0, tt.blockW, tt.blockH,
			tt.width, tt.height) {
			t.Fatalf("%s block reported true", tt.name)
		}
	}
}

func TestCBRVariancePartitionThresholds(t *testing.T) {
	const yAc = int16(80)
	if got := CBRVariancePartitionThreshold(yAc, 320, 240,
		common.Block64x64, 0); got != 12 {
		t.Fatalf("low-res Block64x64 threshold = %d, want 12", got)
	}
	if got := CBRVariancePartitionThreshold(yAc, 320, 240,
		common.Block16x16, 221); got != 3200 {
		t.Fatalf("low-res high-q Block16x16 threshold = %d, want 3200", got)
	}
	if got := CBRVariancePartitionThreshold(yAc, 640, 360,
		common.Block32x32, 0); got != 125 {
		t.Fatalf("mid-res Block32x32 threshold = %d, want 125", got)
	}
	if got := CBRVariancePartitionThreshold(yAc, 1920, 1080,
		common.Block32x32, 0); got != 200 {
		t.Fatalf("1080p Block32x32 threshold = %d, want 200", got)
	}
	if got := CBRVariancePartitionThreshold(0, 640, 480,
		common.Block64x64, 0); got != 0 {
		t.Fatalf("zero dequant threshold = %d, want 0", got)
	}

	if got := CBRVariancePartitionSADThreshold(yAc, 320, 240); got != 10 {
		t.Fatalf("low-res SAD threshold = %d, want 10", got)
	}
	if got := CBRVariancePartitionSADThreshold(yAc, 1280, 720); got != 1000 {
		t.Fatalf("HD SAD threshold = %d, want 1000", got)
	}
	if got := RealtimeVariancePartitionThreshold64(yAc, 640, 480); got != 100 {
		t.Fatalf("realtime 64x64 threshold = %d, want 100", got)
	}
}

func TestPartitionRateCost(t *testing.T) {
	var probs [common.PartitionContexts][common.PartitionTypes - 1]uint8
	probs[3][0] = 128
	probs[3][1] = 64
	probs[3][2] = 192

	if got, want := PartitionRateCost(&probs, 3, common.PartitionNone, true, true),
		VP9CostBit(128, 0); got != want {
		t.Fatalf("PartitionNone cost = %d, want %d", got, want)
	}
	if got, want := PartitionRateCost(&probs, 3, common.PartitionHorz, true, true),
		VP9CostBit(128, 1)+VP9CostBit(64, 0); got != want {
		t.Fatalf("PartitionHorz cost = %d, want %d", got, want)
	}
	if got, want := PartitionRateCost(&probs, 3, common.PartitionVert, true, true),
		VP9CostBit(128, 1)+VP9CostBit(64, 1)+VP9CostBit(192, 0); got != want {
		t.Fatalf("PartitionVert cost = %d, want %d", got, want)
	}
	if got, want := PartitionRateCost(&probs, 3, common.PartitionSplit, true, true),
		VP9CostBit(128, 1)+VP9CostBit(64, 1)+VP9CostBit(192, 1); got != want {
		t.Fatalf("PartitionSplit cost = %d, want %d", got, want)
	}
	if got, want := PartitionRateCost(&probs, 3, common.PartitionSplit, false, true),
		VP9CostBit(64, 1); got != want {
		t.Fatalf("right-edge split cost = %d, want %d", got, want)
	}
	if got, want := PartitionRateCost(&probs, 3, common.PartitionNone, true, false),
		VP9CostBit(192, 0); got != want {
		t.Fatalf("bottom-edge none cost = %d, want %d", got, want)
	}
	if got := PartitionRateCost(&probs, -1, common.PartitionNone, true, true); got != 0 {
		t.Fatalf("invalid context cost = %d, want 0", got)
	}
	if got := PartitionRateCost(nil, 3, common.PartitionNone, true, true); got != 0 {
		t.Fatalf("nil probs cost = %d, want 0", got)
	}
}

func TestSwitchableInterpRateCost(t *testing.T) {
	var fc vp9dec.FrameContext
	fc.SwitchableInterpProb[2][0] = 128
	fc.SwitchableInterpProb[2][1] = 64
	if got, want := SwitchableInterpRateCost(&fc, 2, vp9dec.InterpEighttap),
		VP9CostBit(128, 0); got != want {
		t.Fatalf("Eighttap cost = %d, want %d", got, want)
	}
	if got, want := SwitchableInterpRateCost(&fc, 2, vp9dec.InterpEighttapSmooth),
		VP9CostBit(128, 1)+VP9CostBit(64, 0); got != want {
		t.Fatalf("EighttapSmooth cost = %d, want %d", got, want)
	}
	if got, want := SwitchableInterpRateCost(&fc, 2, vp9dec.InterpEighttapSharp),
		VP9CostBit(128, 1)+VP9CostBit(64, 1); got != want {
		t.Fatalf("EighttapSharp cost = %d, want %d", got, want)
	}
	if got := SwitchableInterpRateCost(&fc, 2, vp9dec.InterpSwitchable); got != 0 {
		t.Fatalf("InterpSwitchable cost = %d, want 0", got)
	}
	if got := SwitchableInterpRateCost(nil, 2, vp9dec.InterpEighttap); got != 0 {
		t.Fatalf("nil frame context cost = %d, want 0", got)
	}
}
