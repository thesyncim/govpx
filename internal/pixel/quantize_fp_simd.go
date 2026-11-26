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
	eleven := archsimd.BroadcastInt16x8(11)
	zero := archsimd.Int16x8{}
	last := zero
	lut := archsimd.LoadUint8x16Array(&fpClassLUT)
	coeff = coeff[:n:n]
	iscan = iscan[:n:n]
	dqcoeff = dqcoeff[:n:n]
	hasQ := qcoeff != nil
	if hasQ {
		qcoeff = qcoeff[:n:n]
	}
	hasClasses := classes != nil
	if hasClasses {
		classes = classes[:n:n]
	}
	vr, vq, vd := dcR, dcQ, dcD
	i := 0
	for ; i+8 <= n; i += 8 {
		c := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i : i+8]))
		// Unsigned magnitude handles -32768; adding a nonnegative int16 round
		// fits uint16 exactly, then clips to the scalar signed-int16 bound.
		a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
		active := a.GreaterEqual(vd)
		// libvpx quantize_fp skips arithmetic and scan work for zero packets.
		// Use the exact public threshold, rather than its table-specific shortcut.
		activeBits := active.ToInt16x8().ToBits().ReshapeToUint64s()
		if activeBits.GetElem(0)|activeBits.GetElem(1) == 0 {
			if hasQ {
				zero.StoreArray((*[8]int16)(qcoeff[i : i+8]))
			}
			zero.StoreArray((*[8]int16)(dqcoeff[i : i+8]))
			if hasClasses {
				binary.LittleEndian.PutUint64(classes[i:i+8], 0)
			}
			vr, vq, vd = acR, acQ, acD
			continue
		}
		t := fpMulHigh16(a, vq).Masked(active)
		magnitude := t
		t = zero.Sub(t).IfElse(c.Less(zero), t)
		if hasQ {
			t.StoreArray((*[8]int16)(qcoeff[i : i+8]))
		}
		t.Mul(vd).StoreArray((*[8]int16)(dqcoeff[i : i+8]))
		last = last.Max(archsimd.LoadInt16x8Array((*[8]int16)(iscan[i : i+8])).Masked(t.NotEqual(zero)))
		if hasClasses {
			indices := fpClassIndices(magnitude.Min(eleven))
			binary.LittleEndian.PutUint64(classes[i:i+8], permute16(lut, indices).ReshapeToUint64s().GetElem(0))
		}
		vr, vq, vd = acR, acQ, acD
	}
	return max(fpReduceMax16(last), quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, i, n, r, q, d, classes))
}
