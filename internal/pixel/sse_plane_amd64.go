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
	if ss == w && rs == w && w < 128 {
		return ssePlaneContiguousR2(src, ref, w*h), true
	}
	if w >= 128 && w&31 == 0 && archsimd.X86.AVX512() {
		return ssePlane32AVX512(src, ss, ref, rs, w, h), true
	}
	defer archsimd.ClearAVXUpperBits()
	src, ref = src[:len(src):len(src)], ref[:len(ref):len(ref)]
	var total uint64
	for y := 0; y < h; {
		var acc archsimd.Int32x8
		end := min(y+4, h)
		for ; y < end; y++ {
			so, ro := y*ss, y*rs
			for x := 0; x <= w-16; x += 16 {
				av := archsimd.LoadUint8x16(src[so+x:]).ExtendToUint16().BitsToInt16()
				bv := archsimd.LoadUint8x16(ref[ro+x:]).ExtendToUint16().BitsToInt16()
				d := av.Sub(bv)
				acc = acc.Add(d.DotProductPairs(d))
			}
		}
		total += uint64(uint32(sum32(acc.GetLo().Add(acc.GetHi()))))
	}
	return total, true
}

// ssePlane32AVX512 requires the complete-window checks in SSEPlane16.
// At most 65536 pixels enter each chunk, so its complete sum fits uint32.
func ssePlane32AVX512(src []byte, ss int, ref []byte, rs, w, h int) uint64 {
	defer archsimd.ClearAVXUpperBits()
	var total uint64
	for y := 0; y < h; {
		var acc archsimd.Int32x16
		end := min(y+4, h)
		for ; y < end; y++ {
			so, ro := y*ss, y*rs
			for x := 0; x <= w-32; x += 32 {
				av := archsimd.LoadUint8x32(src[so+x:]).ExtendToUint16().BitsToInt16()
				bv := archsimd.LoadUint8x32(ref[ro+x:]).ExtendToUint16().BitsToInt16()
				d := av.Sub(bv)
				acc = acc.Add(d.DotProductPairs(d))
			}
		}
		v := acc.GetLo().Add(acc.GetHi())
		total += uint64(uint32(sum32(v.GetLo().Add(v.GetHi()))))
	}
	return total
}

// Contiguous planes need neither row slicing nor four-row reductions.
func ssePlaneContiguousR2(src, ref []byte, n int) uint64 {
	defer archsimd.ClearAVXUpperBits()
	var total uint64
	for start := 0; start < n; {
		end := min(start+65536, n)
		var acc archsimd.Int32x8
		for ; start < end; start += 16 {
			a := archsimd.LoadUint8x16(src[start:]).ExtendToUint16().BitsToInt16()
			b := archsimd.LoadUint8x16(ref[start:]).ExtendToUint16().BitsToInt16()
			d := a.Sub(b)
			acc = acc.Add(d.DotProductPairs(d))
		}
		total += uint64(uint32(sum32(acc.GetLo().Add(acc.GetHi()))))
	}
	return total
}
