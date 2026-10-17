//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// A full 16-byte row becomes one AVX2 packet. At 1..64 rows, signed
// differences stay within 16320 per int16 lane; squares accumulate in int32.
func pixelStats16Narrow(src []byte, ss int, ref []byte, rs, h int) (uint32, int32) {
	if (uint(ss)|uint(rs))>>57 != 0 {
		return pixelStatsRows16(src, ss, ref, rs, 16, h)
	}
	_ = src[(h-1)*ss+15]
	_ = ref[(h-1)*rs+15]
	sp, rp := unsafe.Pointer(unsafe.SliceData(src)), unsafe.Pointer(unsafe.SliceData(ref))
	var squares archsimd.Int32x8
	var signed archsimd.Int16x16
	for y := range h {
		a := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, y*ss))).ExtendToUint16().BitsToInt16()
		b := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(rp, y*rs))).ExtendToUint16().BitsToInt16()
		d := a.Sub(b)
		squares = squares.Add(d.DotProductPairs(d))
		signed = signed.Add(d)
	}
	sums := signed.DotProductPairs(archsimd.BroadcastInt16x16(1))
	sse := uint32(sum32(squares.GetLo().Add(squares.GetHi())))
	sum := sum32(sums.GetLo().Add(sums.GetHi()))
	archsimd.ClearAVXUpperBits()
	return sse, sum
}
