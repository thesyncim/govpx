// SPDX-License-Identifier: BSD-2-Clause
// Integer-projection dataflow extracted from goav1 ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// ProjectionCols16 batches independent row sums before their normalization.
// Each accepted row totals at most 64*255=16320, so int16 conversion is exact.
func ProjectionCols16(src []byte, ss, w, rows, norm int, out []int16) bool {
	if !Enabled || (w != 16 && w != 32 && w != 64) || rows < 0 || ss < 0 || norm < 0 || norm > 14 || len(out) < rows {
		return false
	}
	if rows == 0 {
		return true
	}
	if len(src) < w || (rows > 1 && ss > (len(src)-w)/(rows-1)) {
		return false
	}
	zero := archsimd.Uint8x16{}
	y := 0
	for ; y+4 <= rows; y += 4 {
		a0 := archsimd.Uint64x2{}
		a1 := archsimd.Uint64x2{}
		a2 := archsimd.Uint64x2{}
		a3 := archsimd.Uint64x2{}
		for x := 0; x < w; x += 16 {
			r0 := archsimd.LoadUint8x16(src[(y+0)*ss+x:])
			a0 = a0.Add(r0.SumOf8AbsDiff(zero))
			r1 := archsimd.LoadUint8x16(src[(y+1)*ss+x:])
			a1 = a1.Add(r1.SumOf8AbsDiff(zero))
			r2 := archsimd.LoadUint8x16(src[(y+2)*ss+x:])
			a2 = a2.Add(r2.SumOf8AbsDiff(zero))
			r3 := archsimd.LoadUint8x16(src[(y+3)*ss+x:])
			a3 = a3.Add(r3.SumOf8AbsDiff(zero))
		}
		out[y+0] = int16(a0.GetElem(0)+a0.GetElem(1)) >> uint(norm)
		out[y+1] = int16(a1.GetElem(0)+a1.GetElem(1)) >> uint(norm)
		out[y+2] = int16(a2.GetElem(0)+a2.GetElem(1)) >> uint(norm)
		out[y+3] = int16(a3.GetElem(0)+a3.GetElem(1)) >> uint(norm)
	}
	for ; y < rows; y++ {
		a0 := archsimd.Uint64x2{}
		for x := 0; x < w; x += 16 {
			r0 := archsimd.LoadUint8x16(src[(y+0)*ss+x:])
			a0 = a0.Add(r0.SumOf8AbsDiff(zero))
		}
		out[y] = int16(a0.GetElem(0)+a0.GetElem(1)) >> uint(norm)
	}
	return true
}
