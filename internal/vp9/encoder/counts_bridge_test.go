package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestFrameCountsForDecoderMapsEncoderCounts(t *testing.T) {
	var src FrameCounts
	src.YMode[1][2] = 3
	src.Partition[4][1] = 5
	src.SwitchableInterp[2][1] = 7
	src.InterMode[3][2] = 11
	src.IntraInter[1] = [2]uint32{13, 17}
	src.ReferenceMode.CompInter[2] = [2]uint32{19, 23}
	src.ReferenceMode.SingleRef[1][0] = [2]uint32{29, 31}
	src.ReferenceMode.SingleRef[1][1] = [2]uint32{37, 41}
	src.ReferenceMode.CompRef[3] = [2]uint32{43, 47}
	src.Skip[2] = [2]uint32{53, 59}
	src.TxMode.P8x8[1] = [2]uint32{61, 67}
	src.TxMode.P16x16[1] = [3]uint32{71, 73, 79}
	src.TxMode.P32x32[1] = [4]uint32{83, 89, 97, 101}
	src.Mv.Joints = [4]uint32{103, 107, 109, 113}
	src.Mv.Comps[1].Sign = [2]uint32{127, 131}
	src.Mv.Comps[1].Classes[3] = 137
	src.Mv.Comps[1].Class0[1] = 139
	src.Mv.Comps[1].Bits[4] = [2]uint32{149, 151}
	src.Mv.Comps[1].Class0Fp[1][2] = 157
	src.Mv.Comps[1].Fp[2] = 163
	src.Mv.Comps[1].Class0Hp = [2]uint32{167, 173}
	src.Mv.Comps[1].Hp = [2]uint32{179, 181}

	slot := &src.CoefBranchStats[common.Tx8x8][1][1][2][3]
	slot[0] = [2]uint32{191, 193}
	slot[1] = [2]uint32{197, 199}
	slot[2] = [2]uint32{211, 223}

	got := FrameCountsForDecoder(&src)
	if got.YMode != src.YMode {
		t.Fatalf("YMode did not map")
	}
	if got.Partition != src.Partition {
		t.Fatalf("Partition did not map")
	}
	if got.SwitchableInterp != src.SwitchableInterp {
		t.Fatalf("SwitchableInterp did not map")
	}
	if got.InterMode != src.InterMode {
		t.Fatalf("InterMode did not map")
	}
	if got.IntraInter != src.IntraInter {
		t.Fatalf("IntraInter did not map")
	}
	if got.CompInter != src.ReferenceMode.CompInter {
		t.Fatalf("CompInter did not map")
	}
	if got.SingleRef != src.ReferenceMode.SingleRef {
		t.Fatalf("SingleRef did not map")
	}
	if got.CompRef != src.ReferenceMode.CompRef {
		t.Fatalf("CompRef did not map")
	}
	if got.Skip != src.Skip {
		t.Fatalf("Skip did not map")
	}
	if got.Tx.P8x8[1] != src.TxMode.P8x8[1] ||
		got.Tx.P16x16[1] != src.TxMode.P16x16[1] ||
		got.Tx.P32x32[1] != src.TxMode.P32x32[1] {
		t.Fatalf("Tx counts did not map")
	}
	if got.Mv.Joints != src.Mv.Joints {
		t.Fatalf("MV joints did not map")
	}
	if got.Mv.Comps[1].Sign != src.Mv.Comps[1].Sign ||
		got.Mv.Comps[1].Classes != src.Mv.Comps[1].Classes ||
		got.Mv.Comps[1].Class0 != src.Mv.Comps[1].Class0 ||
		got.Mv.Comps[1].Bits != src.Mv.Comps[1].Bits ||
		got.Mv.Comps[1].Class0Fp != src.Mv.Comps[1].Class0Fp ||
		got.Mv.Comps[1].Fp != src.Mv.Comps[1].Fp ||
		got.Mv.Comps[1].Class0Hp != src.Mv.Comps[1].Class0Hp ||
		got.Mv.Comps[1].Hp != src.Mv.Comps[1].Hp {
		t.Fatalf("MV component counts did not map")
	}

	coef := got.Coef.Coef[common.Tx8x8][1][1][2][3]
	if coef[0] != 197 || coef[1] != 211 || coef[2] != 223 || coef[3] != 191 {
		t.Fatalf("Coef token counts = %v, want [197 211 223 191]", coef)
	}
	if eob := got.Coef.EobBranch[common.Tx8x8][1][1][2][3]; eob != 384 {
		t.Fatalf("EobBranch = %d, want 384", eob)
	}
}

func TestFrameCountsForDecoderNilIsZero(t *testing.T) {
	got := FrameCountsForDecoder(nil)
	if got != (vp9dec.FrameCounts{}) {
		t.Fatalf("FrameCountsForDecoder(nil) returned non-zero counts: %+v", got)
	}
}
