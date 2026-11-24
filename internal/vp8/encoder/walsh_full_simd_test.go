//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"math/rand/v2"
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

func TestWalshFullSignedNative(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("native SIMD unavailable")
	}
	r := rand.New(rand.NewPCG(1327, 5731))
	for _, stride := range []int{4, 7, 16} {
		for trial := range 2000 {
			input := make([]int16, 3*stride+4)
			for i := range input {
				input[i] = int16(r.Uint32())
				if trial == 0 {
					input[i] = -32768
				} else if trial == 1 {
					input[i] = 32767
				}
			}
			var got, want [16]int16
			if !forward4Go(input, stride, &got, true) {
				t.Fatal("Walsh unexpectedly fell back to scalar")
			}
			forwardWalsh4x4Scalar(input, stride, &want)
			if got != want {
				t.Fatalf("stride%d trial%d: %v != %v", stride, trial, got, want)
			}
		}
	}
}
