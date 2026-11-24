//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func TestInternalEdgesPreserveSequentialInt16Results(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	rng := rand.New(rand.NewPCG(0x152684, 0x736209))
	for _, pitch := range []int{8, 9, 15, 16, 17, 32} {
		for _, vertical := range []bool{false, true} {
			if !vertical && pitch < 16 {
				continue
			}
			step, outer := pitch, 1
			if vertical {
				step, outer = 1, pitch
			}
			for _, align := range []int{0, 1, 7} {
				for trial := range 128 {
					original := make([]byte, align+15*pitch+16)
					for i := range original {
						if trial%2 == 0 {
							original[i] = 112 + byte(rng.UintN(17))
						} else {
							original[i] = byte(rng.Uint32())
						}
					}
					blimit := []int{0, 16, 64, 128, 254}[trial%5]
					limit := []int{0, 4, 32, 255}[(trial/5)%4]
					thresh := []int{0, 1, 8, 255}[(trial/20)%4]
					got, want := bytes.Clone(original), bytes.Clone(original)
					if !LoopFilterInternal16(got[align:], step, outer, blimit, limit, thresh) {
						t.Fatal("native internal dispatch unavailable")
					}
					// The independent int16 network executes each original edge in order.
					// Eight-position halves cannot affect the next half at these pitches.
					for edge := 1; edge <= 3; edge++ {
						for base := 0; base < 16; base += 8 {
							loopFilterNarrow8(want, align+4*edge*step+base*outer, step, outer, blimit, limit, thresh, 4)
						}
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("pitch%d vertical%v align%d trial%d blimit%d limit%d thresh%d", pitch, vertical, align, trial, blimit, limit, thresh)
					}
				}
			}
		}
	}
}
