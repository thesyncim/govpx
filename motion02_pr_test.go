// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"math/rand"
	"testing"
)

func motion02ParentBiasImprovedInterFrameMVSlots(slots *[8]improvedInterFrameMVSlot, count int, refFrame vp8common.MVReferenceFrame, signBias [vp8common.MaxRefFrames]bool, mbRow int, mbCol int, mbRows int, mbCols int) {
	if slots == nil || refFrame <= vp8common.IntraFrame || refFrame >= vp8common.MaxRefFrames {
		return
	}
	targetBias := signBias[refFrame]
	limit := min(count, len(slots))
	for i := range limit {
		slot := &slots[i]
		if slot.refFrame == vp8common.IntraFrame {
			continue
		}
		if slot.signBias != targetBias {
			slot.mv.Row = -slot.mv.Row
			slot.mv.Col = -slot.mv.Col
		}
		slot.mv = clampInterFrameModeMotionVector(slot.mv, mbRow, mbCol, mbRows, mbCols)
	}
}
func TestMotion02PRBias(t *testing.T) {
	r := rand.New(rand.NewSource(522))
	for i := 0; i < 1000; i++ {
		var a [8]improvedInterFrameMVSlot
		for j := range a {
			a[j] = improvedInterFrameMVSlot{mv: vp8enc.MotionVector{Row: int16(r.Uint32()), Col: int16(r.Uint32())}, signBias: j%2 != 0, refFrame: vp8common.MVReferenceFrame(r.Intn(4))}
		}
		b := a
		bias := [vp8common.MaxRefFrames]bool{false, false, true, true}
		n, rows, cols := i%10, (i%10)-1, (i%7)-1
		biasImprovedInterFrameMVSlots(&a, n, vp8common.LastFrame, bias, i%8, i%8, rows, cols)
		motion02ParentBiasImprovedInterFrameMVSlots(&b, n, vp8common.LastFrame, bias, i%8, i%8, rows, cols)
		if a != b {
			t.Fatalf("case %d differs", i)
		}
	}
}

var motion02PRSlots [8]improvedInterFrameMVSlot

func BenchmarkMotion02PRBias(b *testing.B) {
	var slots [8]improvedInterFrameMVSlot
	for i := range slots {
		slots[i] = improvedInterFrameMVSlot{refFrame: vp8common.MVReferenceFrame(1 + i%3), mv: vp8enc.MotionVector{Row: int16(30 + i*71), Col: int16(-50 - i*79)}, signBias: i&1 != 0}
	}
	bias := [vp8common.MaxRefFrames]bool{false, false, true, true}
	b.ReportAllocs()
	for b.Loop() {
		a := slots
		biasImprovedInterFrameMVSlots(&a, 8, vp8common.LastFrame, bias, 4, 4, 8, 8)
		motion02PRSlots = a
	}
}
