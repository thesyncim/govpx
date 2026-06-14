//go:build amd64 && !purego

package encoder

import (
	"math/rand"
	"testing"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestSquareSumSSE2MatchesScalar(t *testing.T) {
	tests := []struct {
		name string
		n    int
		seed int64
	}{
		{name: "8", n: 8, seed: 11},
		{name: "16", n: 16, seed: 12},
		{name: "64", n: 64, seed: 13},
		{name: "1024", n: 1024, seed: 14},
	}
	for _, tt := range tests {
		rng := rand.New(rand.NewSource(tt.seed))
		values := make([]int16, tt.n)
		for i := range values {
			values[i] = int16(rng.Intn(65536) - 32768)
		}
		got := squareSumSSE2(unsafe.SliceData(values), tt.n)
		want := squareSumScalar(values)
		if got != want {
			t.Fatalf("%s: squareSumSSE2 = %d, want %d", tt.name, got, want)
		}
	}
}

func TestSquareSumSSE2MatchesScalarMaxInt16(t *testing.T) {
	const n = 1024
	values := make([]int16, n)
	for i := range values {
		values[i] = -32768
	}
	got := squareSumSSE2(unsafe.SliceData(values), n)
	want := squareSumScalar(values)
	if got != want {
		t.Fatalf("squareSumSSE2 max int16 = %d, want %d", got, want)
	}
}

func TestRDBlockDispatchMatchesScalarWithTail(t *testing.T) {
	const n = 37
	rng := rand.New(rand.NewSource(15))
	coeffs := make([]int16, n)
	dqcoeffs := make([]int16, n-2)
	residue := make([]int16, n)
	for i := range coeffs {
		coeffs[i] = int16(rng.Intn(65536) - 32768)
		residue[i] = int16(rng.Intn(65536) - 32768)
	}
	for i := range dqcoeffs {
		dqcoeffs[i] = int16(rng.Intn(65536) - 32768)
	}

	if got, want := TransformBlockError(coeffs, dqcoeffs, common.Tx4x4),
		transformBlockErrorScalar(coeffs, dqcoeffs)>>2; got != want {
		t.Fatalf("TransformBlockError tail tx4x4 = %d, want %d", got, want)
	}
	if got, want := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32),
		transformBlockErrorScalar(coeffs, dqcoeffs); got != want {
		t.Fatalf("TransformBlockError tail tx32x32 = %d, want %d", got, want)
	}
	if got, want := TransformBlockEnergy(coeffs, common.Tx8x8),
		transformBlockEnergyScalar(coeffs)>>2; got != want {
		t.Fatalf("TransformBlockEnergy tail tx8x8 = %d, want %d", got, want)
	}
	if got, want := ResidualSSE(residue), residualSSEScalar(residue); got != want {
		t.Fatalf("ResidualSSE tail = %d, want %d", got, want)
	}
}

func BenchmarkSquareSumScalar1024(b *testing.B) {
	values := benchmarkSquareSumInput(1024)
	var sum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum += squareSumScalar(values)
	}
	squareSumSink = sum
}

func BenchmarkSquareSumSSE21024(b *testing.B) {
	values := benchmarkSquareSumInput(1024)
	var sum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum += squareSumSSE2(unsafe.SliceData(values), len(values))
	}
	squareSumSink = sum
}

var squareSumSink uint64

func benchmarkSquareSumInput(n int) []int16 {
	rng := rand.New(rand.NewSource(16))
	values := make([]int16, n)
	for i := range values {
		values[i] = int16(rng.Intn(4097) - 2048)
	}
	return values
}
