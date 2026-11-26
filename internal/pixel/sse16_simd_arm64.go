//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// SSE16 computes squared error over sixteen columns, without the signed
// sum needed by variance. Only the requested rows and columns are read;
// accumulation wraps modulo2^32, as in Stats.
func SSE16(src []byte, ss int, ref []byte, rs, h int) uint32 {
	if !Enabled {
		return SSE16Scalar(src, ss, ref, rs, h)
	}
	if h <= 0 {
		return 0
	}
	if h == 16 && (uint(ss)|uint(rs))>>30 == 0 && 15*ss+16 <= len(src) && 15*rs+16 <= len(ref) {
		_ = src[15*ss+15]
		_ = ref[15*rs+15]
		var s0, s1, s2, s3 archsimd.Int32x4
		so, ro := 0, 0
		ss2, ss3, ss4 := 2*ss, 3*ss, 4*ss
		rs2, rs3, rs4 := 2*rs, 3*rs, 4*rs
		for range 4 {
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss : so+ss+16]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+rs : ro+rs+16]))
			a2 := archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss2 : so+ss2+16]))
			b2 := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+rs2 : ro+rs2+16]))
			a3 := archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss3 : so+ss3+16]))
			b3 := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+rs3 : ro+rs3+16]))
			d0 := a0.Max(b0).Sub(a0.Min(b0))
			d1 := a1.Max(b1).Sub(a1.Min(b1))
			d2 := a2.Max(b2).Sub(a2.Min(b2))
			d3 := a3.Max(b3).Sub(a3.Min(b3))
			s0 = s0.Add(square8(d0.ExtendLo8ToUint16().BitsToInt16()))
			s1 = s1.Add(square8(d0.HiToLo().ExtendLo8ToUint16().BitsToInt16()))
			s2 = s2.Add(square8(d1.ExtendLo8ToUint16().BitsToInt16()))
			s3 = s3.Add(square8(d1.HiToLo().ExtendLo8ToUint16().BitsToInt16()))
			s0 = s0.Add(square8(d2.ExtendLo8ToUint16().BitsToInt16()))
			s1 = s1.Add(square8(d2.HiToLo().ExtendLo8ToUint16().BitsToInt16()))
			s2 = s2.Add(square8(d3.ExtendLo8ToUint16().BitsToInt16()))
			s3 = s3.Add(square8(d3.HiToLo().ExtendLo8ToUint16().BitsToInt16()))
			so += ss4
			ro += rs4
		}
		return uint32(sum32(s0.Add(s1).Add(s2.Add(s3))))
	}
	var lo, hi archsimd.Int32x4
	for y := range h {
		lo, hi = ssePacket(lo, hi, archsimd.LoadUint8x16(src[y*ss:]), archsimd.LoadUint8x16(ref[y*rs:]))
	}
	return uint32(sum32(lo.Add(hi)))
}
