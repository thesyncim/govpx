package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var gatherPointerNativeSink int16

func BenchmarkGatherPointerNative(b *testing.B) {
	for _, w := range []int{8, 16} {
		for _, pitch := range []int{32, 1312} {
			for _, extreme := range []bool{false, true} {
				b.Run(fmt.Sprintf("w%d/pitch%d/extreme%v", w, pitch, extreme), func(b *testing.B) {
					ss, ps := pitch, pitch+7
					src, pred := make([]byte, 3+(w-1)*ss+w), make([]byte, 5+(w-1)*ps+w)
					out := make([]int16, w*w+16)
					for i := range src {
						src[i] = byte(i*37 + 71)
						if extreme {
							src[i] = 255
						}
					}
					for i := range pred {
						pred[i] = byte(i*13 + 199)
						if extreme {
							pred[i] = 0
						}
					}
					beforeSrc, beforePred := bytes.Clone(src), bytes.Clone(pred)
					for i := range out {
						out[i] = 12345
					}
					fn := ResidualGather16x16PtrFast
					if w == 8 {
						fn = ResidualGather8x8PtrFast
					}
					a, p, o := &src[3], &pred[5], &out[8]
					b.ReportAllocs()
					for b.Loop() {
						fn(a, ss, p, ps, o)
					}
					for y := range w {
						for x := range w {
							at := 8 + ((y/4)*(w/4)+x/4)*16 + (y%4)*4 + x%4
							want := int16(src[3+y*ss+x]) - int16(pred[5+y*ps+x])
							if out[at] != want {
								b.Fatal("gather differs from scalar")
							}
						}
					}
					for i := range 8 {
						if out[i] != 12345 || out[len(out)-1-i] != 12345 {
							b.Fatal("output guards changed")
						}
					}
					if !bytes.Equal(src, beforeSrc) || !bytes.Equal(pred, beforePred) {
						b.Fatal("input changed")
					}
					gatherPointerNativeSink = out[8+w*w-1]
				})
			}
		}
	}
}

func BenchmarkGatherPackedPredictionNative(b *testing.B) {
	for _, w := range []int{8, 16} {
		for _, pitch := range []int{32, 1312} {
			for _, extreme := range []bool{false, true} {
				b.Run(fmt.Sprintf("w%d/pitch%d/extreme%v", w, pitch, extreme), func(b *testing.B) {
					ss, ps := pitch, w
					src, pred := make([]byte, 3+(w-1)*ss+w), make([]byte, 5+(w-1)*ps+w)
					out := make([]int16, w*w+16)
					for i := range src {
						src[i] = byte(i*37 + 71)
						if extreme {
							src[i] = 255
						}
					}
					for i := range pred {
						pred[i] = byte(i*13 + 199)
						if extreme {
							pred[i] = 0
						}
					}
					beforeSrc, beforePred := bytes.Clone(src), bytes.Clone(pred)
					for i := range out {
						out[i] = 12345
					}
					fn := ResidualGather16x16PtrFast
					if w == 8 {
						fn = ResidualGather8x8PtrFast
					}
					a, p, o := &src[3], &pred[5], &out[8]
					b.ReportAllocs()
					for b.Loop() {
						fn(a, ss, p, ps, o)
					}
					for y := range w {
						for x := range w {
							at := 8 + ((y/4)*(w/4)+x/4)*16 + (y%4)*4 + x%4
							want := int16(src[3+y*ss+x]) - int16(pred[5+y*ps+x])
							if out[at] != want {
								b.Fatal("gather differs from scalar")
							}
						}
					}
					for i := range 8 {
						if out[i] != 12345 || out[len(out)-1-i] != 12345 {
							b.Fatal("output guards changed")
						}
					}
					if !bytes.Equal(src, beforeSrc) || !bytes.Equal(pred, beforePred) {
						b.Fatal("input changed")
					}
					gatherPointerNativeSink = out[8+w*w-1]
				})
			}
		}
	}
}
