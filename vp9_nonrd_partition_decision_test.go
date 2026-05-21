package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9NonrdPickPartitionSplitSize confirms vp9MLSplitSize maps each
// ML-eligible parent bsize to its libvpx subsize_lookup
// (vp9/common/vp9_common_data.c subsize_lookup[PARTITION_SPLIT]).
func TestVP9NonrdPickPartitionSplitSize(t *testing.T) {
	cases := []struct {
		in   common.BlockSize
		want common.BlockSize
		ok   bool
	}{
		{common.Block64x64, common.Block32x32, true},
		{common.Block32x32, common.Block16x16, true},
		{common.Block16x16, common.Block8x8, true},
		{common.Block8x8, common.BlockInvalid, false},
	}
	for _, tc := range cases {
		got, ok := vp9MLSplitSize(tc.in)
		if ok != tc.ok {
			t.Errorf("vp9MLSplitSize(%v) ok = %v, want %v", tc.in, ok, tc.ok)
		}
		if got != tc.want {
			t.Errorf("vp9MLSplitSize(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestVP9MLPickPartitionEntryUsesLastBufferWhenLastRefMasked(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		CpuUsed: 8,
	})
	e.sf.PartitionSearchType = MlBasedPartition

	ref := vp9test.NewMotionYCbCr(width, height)
	src := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(ref), 0, 0)
	e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)
	e.vp9ResetMLPartitionCache(8, 8)

	var dq vp9dec.DequantTables
	inter := &vp9InterEncodeState{
		img:        src,
		dq:         &dq,
		refMask:    1 << uint(vp9dec.GoldenFrame),
		baseQindex: e.vp9EncoderModeDecisionQIndex(),
	}
	ctx := e.vp9MLPickPartitionEntry(inter, 8, 8, 0, 0)
	if ctx == nil {
		t.Fatal("ML partition entry returned nil when LAST buffer exists but LAST coding ref is masked")
	}
	if !ctx.ready || !ctx.frameValid {
		t.Fatalf("ML partition ctx ready/frameValid = %v/%v, want true/true",
			ctx.ready, ctx.frameValid)
	}
	if ctx.sbMiRow != 0 || ctx.sbMiCol != 0 {
		t.Fatalf("ML partition ctx SB = (%d,%d), want (0,0)", ctx.sbMiRow, ctx.sbMiCol)
	}
}
