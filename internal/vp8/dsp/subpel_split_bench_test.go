package dsp

import "testing"

var splitVarianceSink, splitSSESink int

func BenchmarkSplitSubpelPhases(b *testing.B) {
	for _, tc := range []struct {
		name string
		fn   func([]byte, int, int, int, []byte, int) (int, int)
	}{
		{"16x8", SubpelVariance16x8}, {"8x16", SubpelVariance8x16}, {"8x8", SubpelVariance8x8}, {"8x4", SubpelVariance8x4}, {"4x8", SubpelVariance4x8}, {"4x4", SubpelVariance4x4},
	} {
		b.Run(tc.name, func(b *testing.B) {
			src, ref := makeSubpelVarianceSource(), makeSubpelVarianceReference()
			b.ReportAllocs()
			for b.Loop() {
				splitVarianceSink, splitSSESink = tc.fn(src, 40, 3, 5, ref, 40)
			}
		})
	}
}
