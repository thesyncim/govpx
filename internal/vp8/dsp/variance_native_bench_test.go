package dsp

import "testing"

var nativeVarianceResult int

func BenchmarkVariance16x16Native(b *testing.B) {
	const stride = 19
	var src, ref [15*stride + 16]byte
	var sum, sse int
	for i := range src {
		src[i] = byte(i*73 + 19)
		ref[i] = byte(i*131 + 53)
	}
	for y := range 16 {
		for x := range 16 {
			d := int(src[y*stride+x]) - int(ref[y*stride+x])
			sum += d
			sse += d * d
		}
	}
	want := sse - (sum * sum >> 8)
	if got := Variance16x16(src[:], stride, ref[:], stride); got != want {
		b.Fatalf("variance %d want %d", got, want)
	}
	b.ReportAllocs()
	var result int
	for b.Loop() {
		result = Variance16x16(src[:], stride, ref[:], stride)
	}
	nativeVarianceResult = result
	if result != want {
		b.Fatal("timed variance changed")
	}
}
