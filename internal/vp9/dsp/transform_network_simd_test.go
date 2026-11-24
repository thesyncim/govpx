//go:build goexperiment.simd && (arm64 || amd64) && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"math/rand/v2"
	"simd/archsimd"
	"testing"
)

func TestInverseNetworksFullSignedDomain(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("native SIMD unavailable")
	}
	r := rand.New(rand.NewPCG(0x3a15, 0x8964))
	cases := []struct {
		n    int
		adst bool
		fn   func([]int16, []int16)
	}{{4, false, idct4}, {8, false, idct8}, {16, false, idct16}, {32, false, idct32}, {4, true, Iadst4}, {8, true, iadst8}, {16, true, iadst16}}
	for _, c := range cases {
		for trial := range 500 {
			var input [4][32]int16
			var in, out [32]archsimd.Int32x4
			for i := range c.n {
				var v [4]int32
				for lane := range 4 {
					s := int16(r.Uint32())
					if trial < 4 {
						switch trial {
						case 0:
							s = -32768
						case 1:
							s = 32767
						case 2:
							if i%2 == 0 {
								s = -32768
							} else {
								s = 32767
							}
						case 3:
							s = 0
						}
					}
					input[lane][i] = s
					v[lane] = int32(s)
				}
				in[i] = archsimd.LoadInt32x4(v[:])
			}
			inverseNetwork(in[:], out[:], c.n, c.adst)
			for lane := range 4 {
				var want [32]int16
				c.fn(input[lane][:], want[:])
				for i := range c.n {
					if got := int16(out[i].GetElem(uint8(lane))); got != want[i] {
						t.Fatalf("n=%d adst=%v trial=%d lane=%d index=%d got=%d want=%d", c.n, c.adst, trial, lane, i, got, want[i])
					}
				}
			}
		}
	}
}
