package dsp

import (
	"bytes"
	"testing"
)

func TestDCOnlyPairLayouts(t *testing.T) {
	for _, strides := range [][2]int{{4, 4}, {5, 7}, {8, 8}, {9, 13}, {32, 17}} {
		ps, ds := strides[0], strides[1]
		for align := range 8 {
			for _, offset := range []int{-1, 0, 1, 4, 128} {
				srcOff, dstOff := align+1, align+1+offset
				if offset < 0 {
					srcOff -= offset
					dstOff -= offset
				}
				n := max(srcOff+3*ps+8, dstOff+3*ds+8) + 7
				original := make([]byte, n)
				for i := range original {
					original[i] = byte(i*37 + 11)
				}
				for _, dcs := range [][2]int32{{0, 0}, {-2040, 2040}, {8, -8}, {128 * 132, -64 * 132}, {32767, -32768}, {2147483647, -2147483648}} {
					got, want := bytes.Clone(original), bytes.Clone(original)
					DCOnlyIDCT4x4AddPairInt32(dcs[0], dcs[1], got[srcOff:srcOff+3*ps+8], ps, got[dstOff:dstOff+3*ds+8], ds)
					DCOnlyIDCT4x4AddInt32(dcs[0], want[srcOff:], ps, want[dstOff:], ds)
					DCOnlyIDCT4x4AddInt32(dcs[1], want[srcOff+4:], ps, want[dstOff+4:], ds)
					if !bytes.Equal(got, want) {
						t.Fatalf("strides%v align%d offset%d dcs%v", strides, align, offset, dcs)
					}
				}
			}
		}
	}
}
