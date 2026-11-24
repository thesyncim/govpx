//go:build goexperiment.simd && arm64 && !purego

package pixel

import "testing"

func TestDCPairCoeffVectorBoundaries(t *testing.T) {
	oracle := func(c int32) int16 {
		v := int64(c) + 4
		if v > 2147483647 {
			v -= 4294967296
		}
		v >>= 3
		if v < -32768 {
			v = -32768
		}
		if v > 32767 {
			v = 32767
		}
		return int16(v)
	}
	check := func(c0, c1 int32) {
		v := dcPairSignedCoeff8(c0, c1)
		for lane := uint8(0); lane < 8; lane++ {
			c := c0
			if lane >= 4 {
				c = c1
			}
			if got, want := v.GetElem(lane), oracle(c); got != want {
				t.Fatalf("coefficients %d,%d lane %d: got %d want %d", c0, c1, lane, got, want)
			}
		}
	}
	for i := int32(0); i < 65536; i++ {
		check(int32(int16(i)), -int32(int16(i)))
		check(-2147483648+i, 2147483647-i)
		check(2147483647-i, -2147483648+i)
	}
	var x uint32 = 0x1794a23b
	for range 131072 {
		x = x*1664525 + 1013904223
		c0 := int32(x)
		x = x*1664525 + 1013904223
		check(c0, int32(x))
	}
}
