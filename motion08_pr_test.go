// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"math/rand"
	"testing"
)

func motion08ParentImprovedInterFrameMVMedian(slots [8]improvedInterFrameMVSlot, count int) vp8enc.MotionVector {
	if count <= 0 {
		return vp8enc.MotionVector{}
	}
	var rows [8]int
	var cols [8]int
	// limit ∈ [0, 8] by construction (min with array length); using
	// limit instead of count for both the sort range and the median
	// index avoids a per-call IsSliceInBounds on rows[:count]/
	// cols[:count] (count is unbounded from the compiler's view) and
	// also closes a latent panic when count > 8.
	limit := min(count, len(slots))
	for i := range limit {
		rows[i] = int(slots[i].mv.Row)
		cols[i] = int(slots[i].mv.Col)
	}
	insertionSortInts(rows[:limit])
	insertionSortInts(cols[:limit])
	return vp8enc.MotionVector{Row: int16(rows[limit/2]), Col: int16(cols[limit/2])}
}
func TestMotion08PRMedian(t *testing.T) {
	r := rand.New(rand.NewSource(588))
	for i := 0; i < 1000; i++ {
		var a [8]improvedInterFrameMVSlot
		for j := range a {
			a[j].mv = vp8enc.MotionVector{Row: int16(r.Uint32()), Col: int16(r.Uint32())}
		}
		for n := 0; n <= 9; n++ {
			if got, want := improvedInterFrameMVMedian(a, n), motion08ParentImprovedInterFrameMVMedian(a, n); got != want {
				t.Fatal(n, got, want)
			}
		}
	}
}

var motion08PRMV vp8enc.MotionVector

func BenchmarkMotion08PRMedian(b *testing.B) {
	var a [8]improvedInterFrameMVSlot
	for i := range a {
		a[i].mv = vp8enc.MotionVector{Row: int16(i*47 - 70), Col: int16(20 - i*72)}
	}
	b.ReportAllocs()
	for b.Loop() {
		motion08PRMV = improvedInterFrameMVMedian(a, 3)
	}
}
