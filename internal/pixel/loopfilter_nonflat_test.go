//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func TestLoopFilterNonFlatPacketParity(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	rng := rand.New(rand.NewPCG(9173, 2214))
	for _, kind := range []int{8, 16} {
		for _, n := range []int{8, 16} {
			for _, shape := range [][2]int{{n, 1}, {n + 3, 1}, {1, 16}, {1, 19}, {4, 1}, {1, 4}} {
				step, outer := shape[0], shape[1]
				for align := range 4 {
					off := align + 8*step
					for trial := range 32 {
						original := make([]byte, off+7*step+(n-1)*outer+1)
						for i := range original {
							original[i] = byte(rng.Uint32())
						}
						if trial%4 == 0 {
							for i := range original {
								original[i] = 128
							}
						}
						if trial%4 == 1 {
							for i := range original {
								original[i] = byte(120 + rng.IntN(17))
							}
						}
						if trial%4 == 2 {
							// Exactly one flat position must reject the whole fast packet.
							for i := -8; i <= 7; i++ {
								original[off+i*step] = 128
							}
						}
						for _, blimit := range []int{0, 64, 128, 254, 255} {
							got, want := bytes.Clone(original), bytes.Clone(original)
							LoopFilter(got, off, step, outer, n, blimit, 32, 8, kind)
							for base := 0; base < n; base += 8 {
								loopFilter8(want, off+base*outer, step, outer, blimit, 32, 8, kind)
							}
							if !bytes.Equal(got, want) {
								t.Fatalf("kind=%d n=%d shape=%v align=%d trial=%d blimit=%d", kind, n, shape, align, trial, blimit)
							}
						}
					}
				}
			}
		}
	}
}

func TestLoopFilterNonFlatRejectDoesNotWrite(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	original := bytes.Repeat([]byte{128}, 128)
	for _, limit := range []int{0, 32, 255} {
		got := bytes.Clone(original)
		if loopFilterNonFlat(got, 64, 16, 1, 64, limit, 0) || !bytes.Equal(got, original) {
			t.Fatal("flat packet changed before fallback")
		}
	}
}
