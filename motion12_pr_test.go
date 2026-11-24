package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func motion12ParentFillInterFrameSplitSubsetWithMode(mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector, firstMode vp8common.BPredictionMode) {
	if mode == nil || mode.Partition >= vp8tables.NumMBSplits {
		return
	}
	for block := range 16 {
		// MBSplits is [4][16]uint8 indexed by validated Partition ∈ [0,4)
		// and block ∈ [0,16). Pow2 masks elide both bounds checks.
		if int(vp8tables.MBSplits[mode.Partition&3][block&15]) != subset {
			continue
		}
		bMode := firstMode
		// block-1 ∈ [0,15) and block-4 ∈ [-4,12); the guards above
		// (block&3 != 0, block>>2 != 0) ensure block-1 ≥ 0 and
		// block-4 ≥ 0 respectively. AND-mask with 15 elides bounds
		// checks on the [16]uint8 inner array.
		if block&3 != 0 && int(vp8tables.MBSplits[mode.Partition&3][(block-1)&15]) == subset {
			bMode = vp8common.Left4x4
		} else if block>>2 != 0 && int(vp8tables.MBSplits[mode.Partition&3][(block-4)&15]) == subset {
			bMode = vp8common.Above4x4
		}
		mode.BlockMV[block&15] = mv
		mode.BModes[block&15] = bMode
	}
}

var motion12PRMode vp8enc.InterFrameMacroblockMode

func TestMotion12PRSubsetTraversal(t *testing.T) {
	for partition := 0; partition < 4; partition++ {
		for subset := -1; subset <= 17; subset++ {
			a := vp8enc.InterFrameMacroblockMode{Partition: uint8(partition)}
			for i := range a.BlockMV {
				a.BlockMV[i] = vp8enc.MotionVector{Row: int16(i * 19), Col: int16(i * 31)}
				a.BModes[i] = vp8common.Above4x4
			}
			b := a
			fillInterFrameSplitSubsetWithMode(&a, subset, vp8enc.MotionVector{Row: -32, Col: 24}, vp8common.New4x4)
			motion12ParentFillInterFrameSplitSubsetWithMode(&b, subset, vp8enc.MotionVector{Row: -32, Col: 24}, vp8common.New4x4)
			if a != b {
				t.Fatal(partition, subset, a, b)
			}
		}
	}
}
func BenchmarkMotion12PRSubsetTraversal(b *testing.B) {
	for _, p := range []int{0, 1, 2, 3} {
		b.Run(fmt.Sprint("fill", p), func(b *testing.B) {
			mode := vp8enc.InterFrameMacroblockMode{Partition: uint8(p)}
			b.ReportAllocs()
			for b.Loop() {
				fillInterFrameSplitSubsetWithMode(&mode, 0, vp8enc.MotionVector{Row: -32, Col: 24}, vp8common.New4x4)
				motion12PRMode = mode
			}
		})
	}
}
