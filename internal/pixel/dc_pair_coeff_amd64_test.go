//go:build goexperiment.simd && amd64 && !purego

package pixel

import "testing"

func TestDCPairByteNativeSelected(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD disabled")
	}
	if !DCPairNative {
		t.Fatal("x64 paired DC SIMD owner not selected")
	}
}

func TestDCPairCoeffByteBoundaries(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD disabled")
	}
	oracle := func(c int32) (byte, byte) {
		v := int64(c) + 4
		if v > 2147483647 {
			v -= 4294967296
		}
		v >>= 3
		if v > 255 {
			v = 255
		}
		if v < -255 {
			v = -255
		}
		if v < 0 {
			return 0, byte(-v)
		}
		return byte(v), 0
	}
	check := func(c0, c1 int32) {
		plus, minus := dcPairCoeffBytes(c0, c1)
		for lane := uint8(0); lane < 16; lane++ {
			c := c0
			if lane&4 != 0 {
				c = c1
			}
			wp, wm := oracle(c)
			if gp, gm := plus.GetElem(lane), minus.GetElem(lane); gp != wp || gm != wm {
				t.Fatalf("c%d/%d lane%d plus/minus%d/%d want%d/%d", c0, c1, lane, gp, gm, wp, wm)
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
	t.Log("327680 coefficient pairs; all16 positive/negative byte lanes match independent int64 wrap/clip oracle")
}
