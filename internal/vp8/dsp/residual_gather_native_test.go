package dsp

import (
	"testing"
)

var residualGatherNativeSink int16

// Identical fixture is copied to assembly/previous references by the native
// benchmark runner so B.Loop measures the same public operation in each.
func BenchmarkResidualGatherNative(b *testing.B) {
	for _, c := range []struct {
		name string
		w    int
	}{{"16x16", 16}, {"8x8", 8}} {
		b.Run(c.name, func(b *testing.B) {
			const stride = 32
			src, pred := make([]byte, (c.w-1)*stride+c.w), make([]byte, (c.w-1)*stride+c.w)
			out := make([]int16, c.w*c.w)
			for i := range src {
				src[i], pred[i] = byte(i*37+71), byte(i*13+199)
			}
			fn := ResidualGather16x16Fast
			if c.w == 8 {
				fn = ResidualGather8x8Fast
			}
			b.ReportAllocs()
			for b.Loop() {
				fn(src, stride, pred, stride, out)
			}
			residualGatherNativeSink = out[len(out)-1]
		})
	}
}
