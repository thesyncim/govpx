//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import "simd/archsimd"

func diffMetricsSIMD(src []byte, ss int, ref []byte, rs, w, h int) (sad, sum int, sse uint32) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0
	}
	var abs archsimd.Uint32x4
	var squares, signed archsimd.Int32x4
	if (uint(ss)|uint(rs))>>57 == 0 && (h-1)*ss+w <= len(src) && (h-1)*rs+w <= len(ref) {
		switch w {
		case 16:
			for y := range h {
				av := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
				bv := archsimd.LoadUint8x16Array((*[16]byte)(ref[0:16]))
				abs = sadAdd(abs, av, bv)
				squares, signed = pixelDiffAcc(squares, signed, av, bv)
				if y+1 < h {
					src = src[ss:]
					ref = ref[rs:]
				}
			}
			return int(sumU32(abs)), int(sum32(signed)), uint32(sum32(squares))
		case 32:
			for y := range h {
				a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
				b0 := archsimd.LoadUint8x16Array((*[16]byte)(ref[0:16]))
				abs = sadAdd(abs, a0, b0)
				squares, signed = pixelDiffAcc(squares, signed, a0, b0)
				a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[16:32]))
				b1 := archsimd.LoadUint8x16Array((*[16]byte)(ref[16:32]))
				abs = sadAdd(abs, a1, b1)
				squares, signed = pixelDiffAcc(squares, signed, a1, b1)
				if y+1 < h {
					src = src[ss:]
					ref = ref[rs:]
				}
			}
			return int(sumU32(abs)), int(sum32(signed)), uint32(sum32(squares))
		case 64:
			for y := range h {
				for x := 0; x < 64; x += 16 {
					av := archsimd.LoadUint8x16Array((*[16]byte)(src[x : x+16]))
					bv := archsimd.LoadUint8x16Array((*[16]byte)(ref[x : x+16]))
					abs = sadAdd(abs, av, bv)
					squares, signed = pixelDiffAcc(squares, signed, av, bv)
				}
				if y+1 < h {
					src = src[ss:]
					ref = ref[rs:]
				}
			}
			return int(sumU32(abs)), int(sum32(signed)), uint32(sum32(squares))
		}
	}
	for y := range h {
		a, b := src[y*ss:y*ss+w], ref[y*rs:y*rs+w]
		x := 0
		for ; x+16 <= w; x += 16 {
			av := archsimd.LoadUint8x16Array((*[16]byte)(a[x : x+16]))
			bv := archsimd.LoadUint8x16Array((*[16]byte)(b[x : x+16]))
			abs = sadAdd(abs, av, bv)
			squares, signed = pixelDiffAcc(squares, signed, av, bv)
		}
		if x+8 <= w {
			av, bv := load8(a[x:]), load8(b[x:])
			abs = sadAdd(abs, av, bv)
			squares, signed = pixelDiffAcc(squares, signed, av, bv)
			x += 8
		}
		for ; x < w; x++ {
			d := int(a[x]) - int(b[x])
			sad += max(d, -d)
			sum += d
			sse += uint32(d * d)
		}
	}
	return sad + int(sumU32(abs)), sum + int(sum32(signed)), sse + uint32(sum32(squares))
}
