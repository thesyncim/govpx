//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

var fpClassLUT = [16]byte{0, 1, 2, 3, 3, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 5}

func quantizeFPBlockSIMD(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {

	if (n >= 64 || n == 32 && qcoeff == nil && classes == nil) && n <= 1024 && n&15 == 0 && Enabled && archsimd.X86.AVX2() && fp16WindowsDisjoint(coeff, iscan, qcoeff, dqcoeff, classes, n) {
		if archsimd.X86.AVX512() && n >= 256 && n&31 == 0 {
			return quantizeFPBlock32AVX512(coeff, iscan, qcoeff, dqcoeff, n, r, q, d, classes)
		}
		max16 := archsimd.BroadcastUint16x16(32767)
		zero := archsimd.Int16x16{}
		last := zero
		var lut archsimd.Uint8x16
		if classes != nil {
			lut = archsimd.LoadUint8x16Array(&fpClassLUT)
		}
		eleven := archsimd.BroadcastInt16x8(11)
		// First packet owns DC/AC vectors. They die before AC-only loop tables.
		{
			i := 0
			vr, vq, vd := archsimd.BroadcastInt16x16(r[1]).SetLo(archsimd.BroadcastInt16x8(r[1]).SetElem(0, r[0])), archsimd.BroadcastInt16x16(q[1]).SetLo(archsimd.BroadcastInt16x8(q[1]).SetElem(0, q[0])), archsimd.BroadcastInt16x16(d[1]).SetLo(archsimd.BroadcastInt16x8(d[1]).SetElem(0, d[0]))
			c := archsimd.LoadInt16x16(coeff[i:])
			a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
			below := vd.Greater(a)
			if below.ToInt16x16().ToBits().ReshapeToUint8s().Equal(archsimd.Uint8x32{}).ToBits() == 0 {
				if qcoeff != nil {
					zero.Store(qcoeff[i:])
				}
				zero.Store(dqcoeff[i:])
				if classes != nil {
					archsimd.Uint8x16{}.Store(classes[i:])
				}
			} else {
				magnitude := a.MulHigh(vq).ToBits().AndNot(below.ToInt16x16().ToBits()).BitsToInt16()
				sign := c.ShiftAllRight(15)
				t := magnitude.Xor(sign).Sub(sign)
				if qcoeff != nil {
					t.Store(qcoeff[i:])
				}
				t.Mul(vd).Store(dqcoeff[i:])
				last = last.Max(archsimd.LoadInt16x16(iscan[i:]).Masked(t.NotEqual(zero)))
				if classes != nil {
					lo := fpClassIndices(magnitude.GetLo().Min(eleven))
					hi := fpClassIndices(magnitude.GetHi().Min(eleven))
					binary.LittleEndian.PutUint64(classes[i:], permute16(lut, lo).ReshapeToUint64s().GetElem(0))
					binary.LittleEndian.PutUint64(classes[i+8:], permute16(lut, hi).ReshapeToUint64s().GetElem(0))
				}
			}
		}
		acR, acQ, acD := archsimd.BroadcastInt16x16(r[1]), archsimd.BroadcastInt16x16(q[1]), archsimd.BroadcastInt16x16(d[1])
		for i := 16; i < n; i += 16 {
			{

				vr, vq, vd := acR, acQ, acD
				c := archsimd.LoadInt16x16(coeff[i:])
				a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
				below := vd.Greater(a)
				if below.ToInt16x16().ToBits().ReshapeToUint8s().Equal(archsimd.Uint8x32{}).ToBits() == 0 {
					if qcoeff != nil {
						zero.Store(qcoeff[i:])
					}
					zero.Store(dqcoeff[i:])
					if classes != nil {
						archsimd.Uint8x16{}.Store(classes[i:])
					}
					continue
				}
				magnitude := a.MulHigh(vq).ToBits().AndNot(below.ToInt16x16().ToBits()).BitsToInt16()
				sign := c.ShiftAllRight(15)
				t := magnitude.Xor(sign).Sub(sign)
				if qcoeff != nil {
					t.Store(qcoeff[i:])
				}
				t.Mul(vd).Store(dqcoeff[i:])
				last = last.Max(archsimd.LoadInt16x16(iscan[i:]).Masked(t.NotEqual(zero)))
				if classes != nil {
					lo := fpClassIndices(magnitude.GetLo().Min(eleven))
					hi := fpClassIndices(magnitude.GetHi().Min(eleven))
					binary.LittleEndian.PutUint64(classes[i:], permute16(lut, lo).ReshapeToUint64s().GetElem(0))
					binary.LittleEndian.PutUint64(classes[i+8:], permute16(lut, hi).ReshapeToUint64s().GetElem(0))
				}
			}
		}
		result := fpReduceMax16(last.GetLo().Max(last.GetHi()))
		archsimd.ClearAVXUpperBits()
		return result
	}
	acR, acQ, acD := archsimd.BroadcastInt16x8(r[1]), archsimd.BroadcastInt16x8(q[1]), archsimd.BroadcastInt16x8(d[1])
	dcR, dcQ, dcD := acR.SetElem(0, r[0]), acQ.SetElem(0, q[0]), acD.SetElem(0, d[0])
	max16 := archsimd.BroadcastUint16x8(32767)
	zero := archsimd.Int16x8{}
	last := zero
	lut := archsimd.LoadUint8x16Array(&fpClassLUT)
	i := 0
	for ; i+8 <= n; i += 8 {
		vr, vq, vd := acR, acQ, acD
		if i == 0 {
			vr, vq, vd = dcR, dcQ, dcD
		}
		c := archsimd.LoadInt16x8(coeff[i:])
		// Unsigned magnitude handles -32768; adding a nonnegative int16 round
		// fits uint16 exactly, then clips to the scalar signed-int16 bound.
		a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
		below := vd.Greater(a)
		// libvpx quantize_fp skips arithmetic and scan work for zero packets.
		// Use the exact public threshold, rather than its table-specific shortcut.
		if below.ToInt16x8().ToBits().ReshapeToUint8s().Equal(archsimd.Uint8x16{}).ToBits() == 0 {
			if qcoeff != nil {
				zero.Store(qcoeff[i:])
			}
			zero.Store(dqcoeff[i:])
			if classes != nil {
				binary.LittleEndian.PutUint64(classes[i:], 0)
			}
			continue
		}
		t := fpMulHigh16(a, vq).ToBits().AndNot(below.ToInt16x8().ToBits()).BitsToInt16()
		magnitude := t
		t = zero.Sub(t).IfElse(c.Less(zero), t)
		if qcoeff != nil {
			t.Store(qcoeff[i:])
		}
		t.Mul(vd).Store(dqcoeff[i:])
		last = last.Max(archsimd.LoadInt16x8(iscan[i:]).Masked(t.NotEqual(zero)))
		if classes != nil {
			indices := fpClassIndices(magnitude.Min(archsimd.BroadcastInt16x8(11)))
			binary.LittleEndian.PutUint64(classes[i:], permute16(lut, indices).ReshapeToUint64s().GetElem(0))
		}
	}
	return max(fpReduceMax16(last), quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, i, n, r, q, d, classes))
}

// fp16WindowsDisjoint compares touched byte ranges without adding end pointers.
// The public entry has already checked every required length; n is bounded.
// Even read-only input aliases decline, keeping this specialization conservative.
func fp16WindowsDisjoint(coeff, iscan, qcoeff, dqcoeff []int16, classes []byte, n int) bool {
	if slicesOverlap(coeff, iscan, n, n) || slicesOverlap(coeff, dqcoeff, n, n) || slicesOverlap(iscan, dqcoeff, n, n) {
		return false
	}
	if qcoeff != nil {
		if slicesOverlap(qcoeff, coeff, n, n) || slicesOverlap(qcoeff, iscan, n, n) || slicesOverlap(qcoeff, dqcoeff, n, n) {
			return false
		}
	}
	return true
}
