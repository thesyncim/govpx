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
	var lo, hi archsimd.Int32x4
	for y := range h {
		lo, hi = ssePacket(lo, hi, archsimd.LoadUint8x16(src[y*ss:]), archsimd.LoadUint8x16(ref[y*rs:]))
	}
	return uint32(sum32(lo.Add(hi)))
}
