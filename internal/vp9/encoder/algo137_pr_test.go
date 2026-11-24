package encoder

import (
	"fmt"
	"testing"
)

func algo137R3sCostOutcome(fn func() int) (v int, p bool) {
	defer func() { p = recover() != nil }()
	v = fn()
	return
}

func TestAlgo137Round3ProbCost(t *testing.T) {
	for a := 0; a <= 255; a++ {
		for b := 0; b <= 255; b++ {
			x, xp := algo137R3sCostOutcome(func() int { return ProbDiffUpdateCost(uint8(a), uint8(b)) })
			y, yp := algo137R3sCostOutcome(func() int { return algo137R3sFrozenProbCost(uint8(a), uint8(b)) })
			if x != y || xp != yp {
				t.Fatalf("%d/%d got%d/%v want%d/%v", a, b, x, xp, y, yp)
			}
		}
	}
}

var algo137R3sCost int

func BenchmarkAlgo137Round3ProbCost(b *testing.B) {
	for _, pair := range [][2]byte{{128, 127}, {30, 230}, {220, 30}, {73, 192}} {
		b.Run(fmt.Sprint(pair), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				algo137R3sCost = ProbDiffUpdateCost(pair[0], pair[1])
			}
		})
	}
}

func algo137R3sFrozenProbCost(newp, oldp uint8) int {
	delp := remapProb(int(newp), int(oldp))
	return int(updateBits[delp]) << VP9ProbCostShift
}

// Frozen870699ce implementation.
