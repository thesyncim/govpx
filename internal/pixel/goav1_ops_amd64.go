//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

var oddDwordsIdx = [16]int8{4, 5, 6, 7, -1, -1, -1, -1, 12, 13, 14, 15, -1, -1, -1, -1}

func widenHi8(v archsimd.Uint8x16) archsimd.Uint16x8 {
	return highBytes(v).ExtendLo8ToUint16()
}

func sqSumInt32(acc archsimd.Int64x2, v archsimd.Int32x4) archsimd.Int64x2 {
	odd := v.ToBits().ReshapeToUint8s().PermuteOrZero(archsimd.LoadInt8x16Array(&oddDwordsIdx)).
		ReshapeToUint32s().BitsToInt32()
	return acc.Add(v.MulWidenEven(v)).Add(odd.MulWidenEven(odd))
}

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

func pack2Rows(p []byte, stride int) archsimd.Uint8x16 {
	lo := binary.LittleEndian.Uint64(p)
	hi := binary.LittleEndian.Uint64(p[stride:])
	return archsimd.BroadcastUint64x2(lo).SetElem(1, hi).ReshapeToUint8s()
}

func sad8x8Slice(src []byte, srcStride int, ref []byte, refStride int) int {
	acc := archsimd.BroadcastUint64x2(0)
	for row := 0; row < 8; row += 2 {
		s := pack2Rows(src[row*srcStride:], srcStride)
		r := pack2Rows(ref[row*refStride:], refStride)
		acc = acc.Add(s.SumOf8AbsDiff(r))
	}
	return sumU64(acc)
}

func sad16ColsDualSlice(src []byte, srcStride int, ref []byte, refStride int, h int) int {
	acc := archsimd.BroadcastUint64x2(0)
	for row := 0; row < h; row++ {
		acc = acc.Add(archsimd.LoadUint8x16(src[row*srcStride:]).SumOf8AbsDiff(archsimd.LoadUint8x16(ref[row*refStride:])))
	}
	return sumU64(acc)
}

func sad16ColsX4Slice(src, p0, p1, p2, p3 []byte, srcStride, refStride int, h int) (int, int, int, int) {
	a0 := archsimd.BroadcastUint64x2(0)
	a1 := archsimd.BroadcastUint64x2(0)
	a2 := archsimd.BroadcastUint64x2(0)
	a3 := archsimd.BroadcastUint64x2(0)
	for row := 0; row < h; row++ {
		o := row * refStride
		s := archsimd.LoadUint8x16(src[row*srcStride:])
		a0 = a0.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p0[o:])))
		a1 = a1.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p1[o:])))
		a2 = a2.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p2[o:])))
		a3 = a3.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p3[o:])))
	}
	return sumU64(a0), sumU64(a1), sumU64(a2), sumU64(a3)
}

func sumU64(acc archsimd.Uint64x2) int {
	return int(acc.GetElem(0) + acc.GetElem(1))
}

// Preserve the exact first-row-over-limit result while using PSADBW's two
// independent eight-byte sums instead of byte widening and reduction.
func sad16LimitSlice(src []byte, ss int, ref []byte, rs, h, limit int) int {
	var acc archsimd.Uint64x2
	value := 0
	for y := range h {
		acc = acc.Add(archsimd.LoadUint8x16(src[y*ss:]).SumOf8AbsDiff(archsimd.LoadUint8x16(ref[y*rs:])))
		value = sumU64(acc)
		if value > limit {
			return value
		}
	}
	return value
}
