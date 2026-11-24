//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import "simd/archsimd"

// Stats16x16Const computes (sum, sse) of (src - c) over a 16x16 byte block
// against a constant predictor byte c, eliminating the 256-byte DC_PRED
// scratch fill and reload during intra mode evaluation.
func Stats16x16Const(src []byte, ss int, c byte) (int, int) {
	if !Enabled || ss < 16 || len(src) < 15*ss+16 {
		return stats16x16ConstScalar(src, ss, c)
	}
	cv := archsimd.BroadcastInt16x8(int16(c))
	var s0, s1 archsimd.Int32x4
	var sum archsimd.Int16x8
	if len(src) >= 16*ss {
		for range 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(src[:16]))
			lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(cv)
			hi := widenHi8(a).BitsToInt16().Sub(cv)
			sum = sum.Add(lo).Add(hi)
			s0 = s0.Add(square8(lo))
			s1 = s1.Add(square8(hi))
			src = src[ss:]
		}
	} else {
		for y := range 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(src[y*ss : y*ss+16]))
			lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(cv)
			hi := widenHi8(a).BitsToInt16().Sub(cv)
			sum = sum.Add(lo).Add(hi)
			s0 = s0.Add(square8(lo))
			s1 = s1.Add(square8(hi))
		}
	}
	return int(sum32(sumDiff16(sum))), int(uint32(sum32(s0.Add(s1))))
}

func stats16x16ConstScalar(src []byte, ss int, c byte) (int, int) {
	sum, sse := 0, 0
	ci := int(c)
	for y := range 16 {
		row := src[y*ss : y*ss+16]
		for x := range 16 {
			d := int(row[x]) - ci
			sum += d
			sse += d * d
		}
	}
	return sum, sse
}
