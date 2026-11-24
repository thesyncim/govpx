//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

var fpClassLUT = [16]byte{0, 1, 2, 3, 3, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 5}

func quantizeFPBlockSIMD(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	// QuantizeFPBlock validates all n-element windows before dispatch. Each
	// packet accesses exactly eight elements; optional outputs retain the
	// original qcoeff, dqcoeff, classes store order even when windows alias.
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
		active := a.GreaterEqual(vd)
		// libvpx quantize_fp skips arithmetic and scan work for zero packets.
		// Use the exact public threshold, rather than its table-specific shortcut.
		if active.ToInt16x8().ToBits().ReduceMax() == 0 {
			if qcoeff != nil {
				zero.Store(qcoeff[i:])
			}
			zero.Store(dqcoeff[i:])
			if classes != nil {
				binary.LittleEndian.PutUint64(classes[i:], 0)
			}
			continue
		}
		t := fpMulHigh16(a, vq).Masked(active)
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
