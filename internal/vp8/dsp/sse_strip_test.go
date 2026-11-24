package dsp

import "testing"

func BenchmarkSSE16x720Fast(b *testing.B) {
	a, ref := make([]byte, 720*1312), make([]byte, 720*1344)
	for i := range a {
		a[i] = byte(i*37 + 11)
	}
	for i := range ref {
		ref[i] = byte(i*19 + 53)
	}
	b.ReportAllocs()
	for b.Loop() {
		SSE16xNFast(a, 1312, ref, 1344, 720)
	}
}
