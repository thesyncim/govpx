//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestLoopFilterUVNativeSelected(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD disabled")
	}
	for _, vertical := range []bool{false, true} {
		for _, stride := range []int{8, 9, 17} {
			step, outer := stride, 1
			if vertical {
				step, outer = 1, stride
			}
			for align := range 8 {
				for _, kind := range []int{4, 6} {
					u, v := make([]byte, align+7*stride+8), make([]byte, align+7*stride+8)
					for i := range u {
						u[i] = byte(112 + i%17)
						v[i] = byte(117 + i%13)
					}
					if !LoopFilterUV8(u[align:], v[align:], step, outer, 64, 32, 0, kind) {
						t.Fatal("native paired filter not selected")
					}
					saved := bytes.Clone(u)
					if LoopFilterUV8(u[align:], u[align:], step, outer, 64, 32, 0, kind) || !bytes.Equal(u, saved) {
						t.Fatal("overlap fallback modified plane")
					}
					savedV := bytes.Clone(v)
					if LoopFilterUV8(u[align:], v[align:], step, outer, 255, 32, 0, kind) || !bytes.Equal(u, saved) || !bytes.Equal(v, savedV) {
						t.Fatal("255 fallback modified planes")
					}
				}
			}
		}
	}
}

func TestLoopFilterUVRejectOversizedStrides(t *testing.T) {
	u, v := make([]byte, 64), make([]byte, 64)
	maxInt := int(^uint(0) >> 1)
	for _, stride := range []int{maxInt, maxInt/7 + 1} {
		if LoopFilterUV8(u, v, stride, 1, 64, 32, 0, 4) || LoopFilterUV8(u, v, 1, stride, 64, 32, 0, 6) {
			t.Fatal("oversized stride accepted")
		}
	}
}
