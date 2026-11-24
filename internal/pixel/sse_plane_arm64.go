//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// SSEPlane16 scans complete rows instead of revisiting each 16-pixel column.
// Four-row chunks contain at most 65536 pixels, so their unsigned squared sums
// fit uint32; widening each chunk before adding preserves the full frame sum.
// Height <= 4096 also makes each original column sum fit uint32 exactly.
func SSEPlane16(src []byte, ss int, ref []byte, rs, w, h int) (uint64, bool) {
	if !Enabled || w <= 0 || w > 16384 || w&15 != 0 || h <= 0 || h > 4096 || ss < 0 || rs < 0 || len(src) < w || len(ref) < w {
		return 0, false
	}
	if h > 1 && (ss > (len(src)-w)/(h-1) || rs > (len(ref)-w)/(h-1)) {
		return 0, false
	}
	src, ref = src[:len(src):len(src)], ref[:len(ref):len(ref)]
	var total uint64
	for y := 0; y < h; {
		var lo, hi archsimd.Int32x4
		end := min(y+4, h)
		for ; y < end; y++ {
			so, ro := y*ss, y*rs
			a, b := src[so:so+w:so+w], ref[ro:ro+w:ro+w]
			for x := 0; x <= w-16; x += 16 {
				lo, hi = ssePacket(lo, hi, archsimd.LoadUint8x16(a[x:x+16:x+16]), archsimd.LoadUint8x16(b[x:x+16:x+16]))
			}
		}
		total += uint64(uint32(sum32(lo.Add(hi))))
	}
	return total, true
}
