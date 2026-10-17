//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// SSEPlane16 keeps row-major traversal and one AVX return fence for the frame.
// Four rows of at most 16384 pixels have a squared sum below 2^32; each chunk
// widens into uint64 before the next chunk can overflow it.
func SSEPlane16(src []byte, ss int, ref []byte, rs, w, h int) (uint64, bool) {
	if !Enabled || w <= 0 || w > 16384 || w&15 != 0 || h <= 0 || h > 4096 || ss < 0 || rs < 0 || len(src) < w || len(ref) < w {
		return 0, false
	}
	if h > 1 && (ss > (len(src)-w)/(h-1) || rs > (len(ref)-w)/(h-1)) {
		return 0, false
	}
	defer archsimd.ClearAVXUpperBits()
	src, ref = src[:len(src):len(src)], ref[:len(ref):len(ref)]
	var total uint64
	for y := 0; y < h; {
		var acc archsimd.Int32x8
		end := min(y+4, h)
		for ; y < end; y++ {
			so, ro := y*ss, y*rs
			a, b := src[so:so+w:so+w], ref[ro:ro+w:ro+w]
			for x := 0; x <= w-16; x += 16 {
				av := archsimd.LoadUint8x16(a[x : x+16 : x+16]).ExtendToUint16().BitsToInt16()
				bv := archsimd.LoadUint8x16(b[x : x+16 : x+16]).ExtendToUint16().BitsToInt16()
				d := av.Sub(bv)
				acc = acc.Add(d.DotProductPairs(d))
			}
		}
		total += uint64(uint32(sum32(acc.GetLo().Add(acc.GetHi()))))
	}
	return total, true
}
