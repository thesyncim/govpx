package dsp

import "testing"

func BenchmarkDCOnlyPairInPlace(b *testing.B) {
	var pixels [4 * 32]byte
	for i := range pixels {
		pixels[i] = byte(i*37 + 11)
	}
	b.ReportAllocs()
	for b.Loop() {
		DCOnlyIDCT4x4AddPairInt32(87, -99, pixels[:], 32, pixels[:], 32)
	}
}
