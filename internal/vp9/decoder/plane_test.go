package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestSetupBlockPlanesAssignsSubsampling(t *testing.T) {
	var planes [MaxMbPlane]MacroblockdPlane
	SetupBlockPlanes(&planes, 1, 1)
	if planes[0].SubsamplingX != 0 || planes[0].SubsamplingY != 0 {
		t.Errorf("luma plane got (%d,%d), want (0,0)",
			planes[0].SubsamplingX, planes[0].SubsamplingY)
	}
	for i := 1; i < MaxMbPlane; i++ {
		if planes[i].SubsamplingX != 1 || planes[i].SubsamplingY != 1 {
			t.Errorf("plane %d got (%d,%d), want (1,1)",
				i, planes[i].SubsamplingX, planes[i].SubsamplingY)
		}
	}
}

func TestGetPlaneBlockSizeRouting(t *testing.T) {
	// 4:4:4 luma plane: identity.
	pd := &MacroblockdPlane{}
	if got := GetPlaneBlockSize(common.Block32x32, pd); got != common.SsSizeLookup[common.Block32x32][0][0] {
		t.Errorf("4:4:4 got %d", got)
	}
	// 4:2:0 chroma: Block32x32 projects to Block16x16.
	pd = &MacroblockdPlane{SubsamplingX: 1, SubsamplingY: 1}
	if got := GetPlaneBlockSize(common.Block32x32, pd); got != common.SsSizeLookup[common.Block32x32][1][1] {
		t.Errorf("4:2:0 got %d", got)
	}
}

func TestGetPlaneBlockSizeRejectsInvalidInputs(t *testing.T) {
	if got := GetPlaneBlockSize(common.BlockInvalid, &MacroblockdPlane{}); got != common.BlockInvalid {
		t.Errorf("invalid block got %d, want BlockInvalid", got)
	}
	if got := GetPlaneBlockSize(common.Block8x8,
		&MacroblockdPlane{SubsamplingX: 2}); got != common.BlockInvalid {
		t.Errorf("invalid subsampling got %d, want BlockInvalid", got)
	}
}

func TestGetUvTxSizeRouting(t *testing.T) {
	// Block8x8 with luma Tx8x8 + 4:2:0 chroma → libvpx's UvTxsizeLookup
	// returns the matching chroma tx size.
	pd := &MacroblockdPlane{SubsamplingX: 1, SubsamplingY: 1}
	want := common.UvTxsizeLookup[common.Block8x8][common.Tx8x8][1][1]
	if got := GetUvTxSize(common.Block8x8, common.Tx8x8, pd); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestGetUvTxSizeRejectsInvalidInputs(t *testing.T) {
	if got := GetUvTxSize(common.BlockInvalid, common.Tx8x8,
		&MacroblockdPlane{}); got != common.Tx4x4 {
		t.Errorf("invalid block tx got %d, want Tx4x4", got)
	}
	if got := GetUvTxSize(common.Block8x8, common.TxSizes,
		&MacroblockdPlane{}); got != common.Tx4x4 {
		t.Errorf("invalid luma tx got %d, want Tx4x4", got)
	}
}

func TestResetSkipContextZeros(t *testing.T) {
	above := make([]uint8, 16)
	left := make([]uint8, 16)
	for i := range above {
		above[i] = 1
		left[i] = 1
	}
	planes := []MacroblockdPlane{
		{AboveContext: above, LeftContext: left},
	}
	// Block8x8 → bw=bh=2 (in 4x4 blocks).
	ResetSkipContext(planes, common.Block8x8, []int{4}, []int{6})
	if above[4] != 0 || above[5] != 0 {
		t.Errorf("above window got [%d,%d]", above[4], above[5])
	}
	// Slots outside the window stay 1.
	if above[3] != 1 || above[6] != 1 {
		t.Errorf("above outside-window dirtied: [%d,%d]", above[3], above[6])
	}
	if left[6] != 0 || left[7] != 0 {
		t.Errorf("left window got [%d,%d]", left[6], left[7])
	}
}

func TestResetSkipContextIgnoresInvalidBlockSize(t *testing.T) {
	above := []uint8{1, 1, 1, 1}
	left := []uint8{1, 1, 1, 1}
	planes := []MacroblockdPlane{
		{AboveContext: above, LeftContext: left},
	}
	ResetSkipContext(planes, common.BlockInvalid, []int{0}, []int{0})
	for i := range above {
		if above[i] != 1 || left[i] != 1 {
			t.Fatalf("invalid block reset context at %d: above=%v left=%v", i, above, left)
		}
	}
}

func TestFramePlaneDimsCeilDivide(t *testing.T) {
	uvW, uvH := FramePlaneDims(17, 18, 1, 1)
	if uvW != 9 || uvH != 9 {
		t.Errorf("4:2:0 17x18: got (%d,%d) want (9,9)", uvW, uvH)
	}
	uvW, uvH = FramePlaneDims(16, 16, 0, 0)
	if uvW != 16 || uvH != 16 {
		t.Errorf("4:4:4 identity: got (%d,%d) want (16,16)", uvW, uvH)
	}
	uvW, uvH = FramePlaneDims(16, 16, 1, 0)
	if uvW != 8 || uvH != 16 {
		t.Errorf("4:2:2 16x16: got (%d,%d) want (8,16)", uvW, uvH)
	}
}
