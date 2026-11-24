package dsp

import (
	"slices"
	"testing"
)

var dsp05PRSink int

func dsp05PRData(n int) []byte {
	b := make([]byte, n)
	x := uint32(73241)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}
func TestDSP05PRWalsh(t *testing.T) {
	for k := 0; k < 3000; k++ {
		var c [16]int16
		for i := range c {
			c[i] = int16(k*1517 + i*3791)
		}
		a, z := make([]int16, 256), make([]int16, 256)
		InverseWalsh4x4(&c, a)
		inverseWalsh4x4Scalar(&c, z)
		if !slices.Equal(a, z) {
			t.Fatalf("k%d", k)
		}
	}
}
func BenchmarkDSP05PRWalsh(b *testing.B) {
	var c [16]int16
	for i := range c {
		c[i] = int16(i * 141)
	}
	o := make([]int16, 256)
	b.ReportAllocs()
	for b.Loop() {
		InverseWalsh4x4(&c, o)
	}
	dsp05PRSink = int(o[16])
}
