//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"simd/archsimd"
	"testing"
)

// Every lane, including both plane halves, must keep corrections that can
// change a pixel. Enumerate the scalar rounding equations at every int8 value.
func TestUVRoundedZeroSignedRange(t *testing.T) {
	if !Enabled {
		t.Skip("SIMD backend unavailable")
	}
	clamp := func(x int) int { return max(-128, min(127, x)) }
	for _, kind := range []int{4, 6} {
		for value := -128; value <= 127; value++ {
			f1, f2 := clamp(value+4)>>3, clamp(value+3)>>3
			identity := f1 == 0 && f2 == 0
			if kind == 6 {
				for _, weight := range []int{9, 18, 27} {
					identity = identity && (weight*value+63)>>7 == 0
				}
			} else {
				identity = identity && (f1+1)>>1 == 0
			}
			for lane := range 16 {
				var values [16]int8
				values[lane] = int8(value)
				if got := uvRoundedZero(archsimd.LoadInt8x16Array(&values), kind); got != identity {
					t.Fatalf("kind%d value%d lane%d: got %v, scalar identity %v", kind, value, lane, got, identity)
				}
			}
		}
	}
}

func TestUVRoundedZeroBoundaries(t *testing.T) {
	if !Enabled {
		t.Fatal("SIMD backend unavailable")
	}
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{8, 9, 32, 672} {
			for align := range 8 {
				for _, kind := range []int{4, 6} {
					for pattern := range 10 {
						step, outer := pitch, 1
						if vertical {
							step, outer = 1, pitch
						}
						u, v := bytes.Repeat([]byte{0xa7}, align+7*step+7*outer+1+13), bytes.Repeat([]byte{0xb9}, align+7*step+7*outer+1+13)
						for axis := range 8 {
							for lane := range 8 {
								val := byte([]int{0, 127, 128, 255, 0, 0, 0, 0, 0, 0}[pattern])
								vval := val
								if pattern >= 6 {
									d := axis / 4
									if pattern >= 8 {
										d *= 2
									}
									if pattern%2 != 0 {
										d = -d
									}
									val = byte(128 + d)
									vval = byte(101 + d)
								} else if pattern >= 4 {
									val = byte(32 + lane*3 + axis)
									vval = byte(48 + lane*3 + axis)
									if pattern == 5 && axis == 4 && lane == 7 {
										vval += 6
									}
								}
								u[align+axis*step+lane*outer] = val
								v[align+axis*step+lane*outer] = vval
							}
						}
						wantU, wantV := bytes.Clone(u), bytes.Clone(v)
						loopFilterNarrow8(wantU, align+4*step, step, outer, 254, 255, 0, kind)
						loopFilterNarrow8(wantV, align+4*step, step, outer, 254, 255, 0, kind)
						if !LoopFilterUV8(u[align:len(u)-13], v[align:len(v)-13], step, outer, 254, 255, 0, kind) {
							t.Fatal("eligible UV window declined")
						}
						if !bytes.Equal(u, wantU) || !bytes.Equal(v, wantV) {
							t.Fatalf("v%v pitch%d align%d kind%d pattern%d", vertical, pitch, align, kind, pattern)
						}
					}
				}
			}
		}
	}
}
