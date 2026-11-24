package encoder

import (
	"bytes"
	"testing"
)

// Reset signal pixels inside the timed operation so filtering cannot turn
// later iterations into an already-converged block. The scalar result and
// both observable output buffers are checked after the live benchmark loop.
func BenchmarkDenoiserFilterYNative(b *testing.B) {
	for _, mode := range []string{"smooth", "mixed", "saturating"} {
		b.Run(mode, func(b *testing.B) {
			const stride = 19
			size := 15*stride + 16
			mc, seed, avg := make([]byte, size), make([]byte, size), bytes.Repeat([]byte{71}, size)
			pairs := [][2]byte{{0, 255}, {255, 0}, {127, 128}, {121, 128}, {120, 128}, {113, 128}, {112, 128}, {255, 253}}
			for y := range 16 {
				for x := range 16 {
					i := y*stride + x
					switch mode {
					case "smooth":
						seed[i] = byte(96 + x%8)
						mc[i] = seed[i] + 2
					case "mixed":
						p := pairs[(x+y)%len(pairs)]
						mc[i], seed[i] = p[0], p[1]
					case "saturating":
						mc[i], seed[i] = 255, 0
					}
				}
			}
			wantSig, wantAvg := bytes.Clone(seed), bytes.Clone(avg)
			want := DenoiserFilterYScalar(mc, stride, wantAvg, stride, wantSig, stride, 0, true)
			sig := bytes.Clone(seed)
			got := -1
			b.ReportAllocs()
			b.SetBytes(256)
			for b.Loop() {
				copy(sig, seed)
				got = DenoiserFilterY(mc, stride, avg, stride, sig, stride, 0, true)
			}
			if got != want || !bytes.Equal(avg, wantAvg) || !bytes.Equal(sig, wantSig) {
				b.Fatalf("decision%d/%d or output differs", got, want)
			}
		})
	}
}
