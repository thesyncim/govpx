//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// ResidualSSEActivity computes the residual SSE = sum((src-pred)^2) and
// spatial gradient activity = sum(|d[y,x]-d[y,x-1]| + |d[y,x]-d[y-1,x]|)
// for a w x h block (w in {8, 16, 32, 64}, h > 0). Bit-exact with the scalar
// reference in vp9InterTxResidualStats.
func ResidualSSEActivity(src []byte, srcStride int, pred []byte, predStride int, w, h int) (sse, activity uint64, ok bool) {
	if !Enabled || h <= 0 || w < 8 || w > 64 || w&7 != 0 || srcStride < w || predStride < w {
		return 0, 0, false
	}
	_ = src[(h-1)*srcStride+w-1]
	_ = pred[(h-1)*predStride+w-1]

	var prevRow [8]archsimd.Int16x8
	var sseAcc archsimd.Int32x4
	var actAcc archsimd.Uint32x4
	chunks := w >> 3

	for y := range h {
		sOff := y * srcStride
		pOff := y * predStride
		var prevVec archsimd.Int16x8
		for c := range chunks {
			x := c << 3
			a := load8(src[sOff+x:]).ExtendLo8ToUint16().BitsToInt16()
			b := load8(pred[pOff+x:]).ExtendLo8ToUint16().BitsToInt16()
			diff := a.Sub(b)
			sseAcc = sseAcc.Add(square8(diff))

			var hAbs archsimd.Uint16x8
			if c == 0 {
				left := diff.ToBits().ReshapeToUint8s().ConcatShiftBytesRight(diff.ToBits().ReshapeToUint8s(), 14).ReshapeToUint16s().BitsToInt16()
				hAbs = diff.Sub(left).Abs().SetElem(0, 0).ToBits()
			} else {
				left := diff.ToBits().ReshapeToUint8s().ConcatShiftBytesRight(prevVec.ToBits().ReshapeToUint8s(), 14).ReshapeToUint16s().BitsToInt16()
				hAbs = diff.Sub(left).Abs().ToBits()
			}
			prevVec = diff

			if y > 0 {
				vAbs := diff.Sub(prevRow[c]).Abs().ToBits()
				sum16 := hAbs.Add(vAbs)
				actAcc = actAcc.Add(sum16.ExtendLo4ToUint32()).Add(sum16.HiToLo().ExtendLo4ToUint32())
			} else {
				actAcc = actAcc.Add(hAbs.ExtendLo4ToUint32()).Add(hAbs.HiToLo().ExtendLo4ToUint32())
			}
			prevRow[c] = diff
		}
	}
	return uint64(uint32(sseAcc.ReduceSum())), uint64(actAcc.ReduceSum()), true
}
