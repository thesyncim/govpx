package bitstream

import (
	"math/rand"
	"testing"
)

// BenchmarkWriterWrite measures the boolean coder on a coefficient-like
// bit/prob stream (skewed probabilities, mixed bits).
func BenchmarkWriterWrite(b *testing.B) {
	rng := rand.New(rand.NewSource(0xb17))
	const n = 4096
	bits := make([]uint32, n)
	probs := make([]uint32, n)
	for i := range bits {
		bits[i] = uint32(rng.Intn(2))
		// Skew toward high/low probabilities like real coefficient probs.
		switch rng.Intn(3) {
		case 0:
			probs[i] = uint32(1 + rng.Intn(40))
		case 1:
			probs[i] = uint32(216 + rng.Intn(39))
		default:
			probs[i] = uint32(1 + rng.Intn(254))
		}
	}
	dst := make([]byte, 2*n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var w Writer
		w.Start(dst)
		for j := range n {
			w.Write(bits[j], probs[j])
		}
		if _, err := w.Stop(); err != nil {
			b.Fatal(err)
		}
	}
}
