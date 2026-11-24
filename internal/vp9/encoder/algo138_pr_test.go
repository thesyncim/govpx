package encoder

import (
	"fmt"
	"testing"
)

var algo138R3sCost int

func TestAlgo138Round3ExtraCost(t *testing.T) {
	for a := -1; a <= 65536; a++ {
		for _, sign := range []int{0, 1, 3} {
			x, y := CoeffTokenExtraCost(a, sign)
			u, v := algo138R3sFrozenExtraCost(a, sign)
			if x != u || y != v {
				t.Fatalf("a%d sign%d got%d/%d want%d/%d", a, sign, x, y, u, v)
			}
		}
	}
}

var algo138R3sToken int

func BenchmarkAlgo138Round3ExtraCost(b *testing.B) {
	for _, a := range []int{0, 1, 17, 4095, 4096, 8192, 32767, 32768, 32769} {
		b.Run(fmt.Sprint(a), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				algo138R3sToken, algo138R3sCost = CoeffTokenExtraCost(a, 1)
			}
		})
	}
}

func algo138R3sFrozenExtraCost(absVal, sign int) (token int, cost int) {
	if uint(absVal) < coeffTokenExtraCostTableSize {
		entry := coeffTokenExtraCostTable[absVal]
		return int(entry.token), int(entry.cost)
	}
	return coeffTokenExtraCostSlow(absVal, sign)
}

// Frozen870699ce implementation.
