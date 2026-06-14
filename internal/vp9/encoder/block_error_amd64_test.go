//go:build amd64 && !purego

package encoder

import (
	"math/rand"
	"testing"
	"unsafe"
)

func TestBlockErrorFPSSE2MatchesScalar(t *testing.T) {
	tests := []struct {
		name string
		n    int
		seed int64
	}{
		{name: "8", n: 8, seed: 1},
		{name: "16", n: 16, seed: 2},
		{name: "64", n: 64, seed: 3},
		{name: "256", n: 256, seed: 4},
	}
	for _, tt := range tests {
		rng := rand.New(rand.NewSource(tt.seed))
		coeff := make([]int16, tt.n)
		dqcoeff := make([]int16, tt.n)
		for i := range coeff {
			coeff[i] = int16(rng.Intn(65536) - 32768)
			dqcoeff[i] = int16(rng.Intn(65536) - 32768)
		}
		got := blockErrorFPSSE2(unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), tt.n)
		want := blockErrorFPScalar(coeff, dqcoeff)
		if got != want {
			t.Fatalf("%s: blockErrorFPSSE2 = %d, want %d", tt.name, got, want)
		}
	}
}

func TestBlockErrorFPSSE2MatchesScalarMaxInt16Diff(t *testing.T) {
	const n = 256
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	for i := range coeff {
		coeff[i] = -32768
		dqcoeff[i] = 32767
	}
	got := blockErrorFPSSE2(unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), n)
	want := blockErrorFPScalar(coeff, dqcoeff)
	if got != want {
		t.Fatalf("blockErrorFPSSE2 max int16 diff = %d, want %d", got, want)
	}
}

func TestBlockErrorFPDispatchMatchesScalarWithTail(t *testing.T) {
	const n = 37
	rng := rand.New(rand.NewSource(5))
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	for i := range coeff {
		coeff[i] = int16(rng.Intn(65536) - 32768)
		dqcoeff[i] = int16(rng.Intn(65536) - 32768)
	}
	got := BlockErrorFP(coeff, dqcoeff)
	want := blockErrorFPScalar(coeff, dqcoeff)
	if got != want {
		t.Fatalf("BlockErrorFP tail dispatch = %d, want %d", got, want)
	}
}

func TestBlockErrorFPWithEnergySSE2MatchesScalar(t *testing.T) {
	tests := []struct {
		name string
		n    int
		seed int64
	}{
		{name: "8", n: 8, seed: 7},
		{name: "16", n: 16, seed: 8},
		{name: "64", n: 64, seed: 9},
		{name: "256", n: 256, seed: 10},
	}
	for _, tt := range tests {
		rng := rand.New(rand.NewSource(tt.seed))
		coeff := make([]int16, tt.n)
		dqcoeff := make([]int16, tt.n)
		for i := range coeff {
			coeff[i] = int16(rng.Intn(65536) - 32768)
			dqcoeff[i] = int16(rng.Intn(65536) - 32768)
		}
		gotErr, gotEnergy := blockErrorFPWithEnergySSE2(
			unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), tt.n,
		)
		wantErr, wantEnergy := blockErrorFPWithEnergyScalar(coeff, dqcoeff)
		if gotErr != wantErr || gotEnergy != wantEnergy {
			t.Fatalf("%s: blockErrorFPWithEnergySSE2 = (%d,%d), want (%d,%d)",
				tt.name, gotErr, gotEnergy, wantErr, wantEnergy)
		}
	}
}

func TestBlockErrorFPWithEnergySSE2MatchesScalarMaxInt16(t *testing.T) {
	const n = 256
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	for i := range coeff {
		coeff[i] = -32768
		dqcoeff[i] = 32767
	}
	gotErr, gotEnergy := blockErrorFPWithEnergySSE2(
		unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), n,
	)
	wantErr, wantEnergy := blockErrorFPWithEnergyScalar(coeff, dqcoeff)
	if gotErr != wantErr || gotEnergy != wantEnergy {
		t.Fatalf("blockErrorFPWithEnergySSE2 max int16 = (%d,%d), want (%d,%d)",
			gotErr, gotEnergy, wantErr, wantEnergy)
	}
}

func TestBlockErrorFPWithEnergyDispatchMatchesScalarWithTail(t *testing.T) {
	const n = 37
	rng := rand.New(rand.NewSource(11))
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n-3)
	for i := range coeff {
		coeff[i] = int16(rng.Intn(65536) - 32768)
	}
	for i := range dqcoeff {
		dqcoeff[i] = int16(rng.Intn(65536) - 32768)
	}
	gotErr, gotEnergy := BlockErrorFPWithEnergy(coeff, dqcoeff)
	wantErr, wantEnergy := blockErrorFPWithEnergyScalar(coeff, dqcoeff)
	if gotErr != wantErr || gotEnergy != wantEnergy {
		t.Fatalf("BlockErrorFPWithEnergy tail dispatch = (%d,%d), want (%d,%d)",
			gotErr, gotEnergy, wantErr, wantEnergy)
	}
}

func BenchmarkBlockErrorFPScalar256(b *testing.B) {
	coeff, dqcoeff := benchmarkBlockErrorInputs(256)
	var sum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum += blockErrorFPScalar(coeff, dqcoeff)
	}
	blockErrorFPSink = sum
}

func BenchmarkBlockErrorFPSSE2256(b *testing.B) {
	coeff, dqcoeff := benchmarkBlockErrorInputs(256)
	var sum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sum += blockErrorFPSSE2(unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), len(coeff))
	}
	blockErrorFPSink = sum
}

func BenchmarkBlockErrorFPWithEnergyScalar256(b *testing.B) {
	coeff, dqcoeff := benchmarkBlockErrorInputs(256)
	var sum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err, energy := blockErrorFPWithEnergyScalar(coeff, dqcoeff)
		sum += err + energy
	}
	blockErrorFPSink = sum
}

func BenchmarkBlockErrorFPWithEnergySSE2256(b *testing.B) {
	coeff, dqcoeff := benchmarkBlockErrorInputs(256)
	var sum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err, energy := blockErrorFPWithEnergySSE2(
			unsafe.SliceData(coeff), unsafe.SliceData(dqcoeff), len(coeff),
		)
		sum += err + energy
	}
	blockErrorFPSink = sum
}

var blockErrorFPSink uint64

func benchmarkBlockErrorInputs(n int) ([]int16, []int16) {
	rng := rand.New(rand.NewSource(6))
	coeff := make([]int16, n)
	dqcoeff := make([]int16, n)
	for i := range coeff {
		coeff[i] = int16(rng.Intn(4097) - 2048)
		dqcoeff[i] = int16(rng.Intn(4097) - 2048)
	}
	return coeff, dqcoeff
}
