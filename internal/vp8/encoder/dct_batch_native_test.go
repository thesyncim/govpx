package encoder

import (
	"math/rand/v2"
	"testing"
)

func TestForwardDCTBatchIndependentScalar(t *testing.T) {
	r := rand.New(rand.NewPCG(187, 621))
	for _, count := range []int{0, 1, 2, 3, 8, 16, 25} {
		for trial := range 80 {
			input := make([]int16, count*16)
			for i := range input {
				input[i] = int16(r.IntN(511) - 255)
				if trial%7 == 0 {
					input[i] = int16(r.Uint32())
				}
			}
			want, got := make([]int16, len(input)), make([]int16, len(input))
			for block := range count {
				forwardDCT4x4Scalar(input[block*16:block*16+16], 4, (*[16]int16)(want[block*16:block*16+16]))
			}
			ForwardDCT4x4Batch(input, got, count)
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("count%d trial%d lane%d: %d != %d", count, trial, i, got[i], want[i])
				}
			}
			alias := append([]int16(nil), input...)
			ForwardDCT4x4Batch(alias, alias, count)
			for i := range alias {
				if alias[i] != want[i] {
					t.Fatalf("alias count%d trial%d lane%d: %d != %d", count, trial, i, alias[i], want[i])
				}
			}
		}
	}
}

func BenchmarkForwardDCTBatchResidual25(b *testing.B) {
	var input, output [25 * 16]int16
	for i := range input {
		input[i] = int16((i*37+11)%511 - 255)
	}
	b.ReportAllocs()
	for b.Loop() {
		ForwardDCT4x4Batch(input[:], output[:], 25)
	}
}
