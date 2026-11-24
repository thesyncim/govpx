package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var uvZeroNativeSink byte

func BenchmarkUVRoundedZeroNative(b *testing.B) {
	for _, pitch := range []int{32, 672} {
		for _, vertical := range []bool{false, true} {
			for _, kind := range []int{4, 6} {
				for _, pattern := range []string{"flat", "ramp", "smooth", "noop"} {
					b.Run(fmt.Sprintf("pitch%d/vertical%v/kind%d/%s", pitch, vertical, kind, pattern), func(b *testing.B) {
						u0, v0 := make([]byte, 3+7*pitch+8+13), make([]byte, 5+7*pitch+8+13)
						for i := range u0 {
							u0[i] = 0xa7
						}
						for i := range v0 {
							v0[i] = 0xb9
						}
						for y := range 8 {
							for x := range 8 {
								u, v := byte(128), byte(123)
								switch pattern {
								case "ramp":
									u = byte(32 + x + y)
									v = byte(48 + x + y)
								case "smooth":
									u = byte(112 + (x*3+y*5)%17)
									v = byte(101 + (x*7+y*2)%17)
								case "noop":
									u = byte((y*8+x)*37 + 11)
									v = byte((y*8+x)*53 + 7)
								}
								u0[3+y*pitch+x] = u
								v0[5+y*pitch+x] = v
							}
						}
						gotU, gotV := bytes.Clone(u0), bytes.Clone(v0)
						wantU, wantV := bytes.Clone(u0), bytes.Clone(v0)
						fn := LoopFilterHorizontalEdgeUV
						oracle := loopFilterHorizontalEdgeScalar
						bl := byte(32)
						if vertical {
							fn = LoopFilterVerticalEdgeUV
							oracle = loopFilterVerticalEdgeScalar
						}
						if kind == 6 {
							bl = 64
							if vertical {
								fn = MBLoopFilterVerticalEdgeUV
								oracle = mbLoopFilterVerticalEdgeScalar
							} else {
								fn = MBLoopFilterHorizontalEdgeUV
								oracle = mbLoopFilterHorizontalEdgeScalar
							}
						}
						oracle(wantU[3:len(wantU)-13], pitch, bl, 16, 2, 1)
						oracle(wantV[5:len(wantV)-13], pitch, bl, 16, 2, 1)
						reset := !bytes.Equal(wantU, u0) || !bytes.Equal(wantV, v0)
						b.ReportAllocs()
						for b.Loop() {
							if reset {
								for y := range 8 {
									copy(gotU[3+y*pitch:3+y*pitch+8], u0[3+y*pitch:3+y*pitch+8])
									copy(gotV[5+y*pitch:5+y*pitch+8], v0[5+y*pitch:5+y*pitch+8])
								}
							}
							fn(gotU[3:len(gotU)-13], gotV[5:len(gotV)-13], pitch, bl, 16, 2)
						}
						if !bytes.Equal(gotU, wantU) || !bytes.Equal(gotV, wantV) {
							b.Fatal("public UV filter differs from scalar")
						}
						uvZeroNativeSink = gotU[3] ^ gotV[5]
					})
				}
			}
		}
	}
}
