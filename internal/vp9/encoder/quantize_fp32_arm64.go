//go:build goexperiment.simd && arm64 && !purego

package encoder

import (
	"simd/archsimd"

	"github.com/thesyncim/govpx/internal/pixel"
)

// quantizeFP32x32RasterSIMD vectorizes quantizeFP32x32Raster across rc = 8..1023
// using 8-wide NEON Int16x8 operations. Bit-exact with the scalar loop in
// transform_quant.go.
func quantizeFP32x32RasterSIMD(coeff []int16, dequant [2]int16, quant, round [2]int,
	iscan []int16, qcoeff, dqcoeff []int16,
) (int, bool) {
	if !pixel.Enabled || len(coeff) < 1024 || len(iscan) < 1024 || len(dqcoeff) < 1024 {
		return 0, false
	}
	if qcoeff != nil && len(qcoeff) < 1024 {
		return 0, false
	}
	deqAC := int(dequant[1])
	quantAC := quant[1]
	roundAC := round[1]
	if deqAC < 0 || deqAC > 32767 || quantAC < 0 || quantAC > 32767 || roundAC < 0 || roundAC > 32767 {
		return 0, false
	}

	// Handle rc = 0..7 scalar (DC at rc=0 + first 7 AC).
	deqDC := int(dequant[0])
	quantDC, roundDC := quant[0], round[0]
	eob := 0
	for rc := range 8 {
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		deq, qv, rv := deqAC, quantAC, roundAC
		if rc == 0 {
			deq, qv, rv = deqDC, quantDC, roundDC
		}
		tmp := 0
		if absCoeff >= deq>>2 {
			tmp = clampInt16(absCoeff + rv)
			tmp = (tmp * qv) >> 15
			q := tmp
			if c < 0 {
				q = -q
			}
			if qcoeff != nil {
				qcoeff[rc] = int16(q)
			}
			dqcoeff[rc] = int16(q * deq / 2)
		} else {
			if qcoeff != nil {
				qcoeff[rc] = 0
			}
			dqcoeff[rc] = 0
		}
		if tmp != 0 && int(iscan[rc]) > eob {
			eob = int(iscan[rc])
		}
	}

	_ = coeff[1023]
	_ = iscan[1023]
	_ = dqcoeff[1023]
	if qcoeff != nil {
		_ = qcoeff[1023]
	}

	negMax := archsimd.BroadcastInt16x8(-32767)
	threshVec := archsimd.BroadcastInt16x8(int16(deqAC >> 2))
	roundVec := archsimd.BroadcastInt16x8(int16(roundAC))
	quantVec := archsimd.BroadcastInt16x8(int16(quantAC))
	deqU16 := archsimd.BroadcastUint16x8(uint16(deqAC))
	var zero archsimd.Int16x8
	eobVec := archsimd.BroadcastInt16x8(int16(eob))

	if qcoeff == nil {
		for rc := 8; rc < 1024; rc += 8 {
			c := archsimd.LoadInt16x8Array((*[8]int16)(coeff[rc : rc+8]))
			absC := c.Max(negMax).Abs()
			keep := absC.GreaterEqual(threshVec)
			tmp := absC.AddSaturated(roundVec)
			lo := tmp.MulWidenLo(quantVec).ShiftAllRight(15)
			hi := tmp.HiToLo().MulWidenLo(quantVec).ShiftAllRight(15)
			mag := lo.ToBits().ReshapeToUint16s().ConcatEven(hi.ToBits().ReshapeToUint16s()).BitsToInt16().Masked(keep)
			neg := c.Less(zero)
			dqMag := mag.ToBits().Mul(deqU16).ShiftAllRight(1).BitsToInt16()
			dq := zero.Sub(dqMag).IfElse(neg, dqMag)
			dq.StoreArray((*[8]int16)(dqcoeff[rc : rc+8]))
			isc := archsimd.LoadInt16x8Array((*[8]int16)(iscan[rc : rc+8]))
			eobVec = eobVec.Max(isc.AndNot(mag.Equal(zero).ToInt16x8()))
		}
		return int(eobVec.ReduceMax()), true
	}

	for rc := 8; rc < 1024; rc += 8 {
		c := archsimd.LoadInt16x8Array((*[8]int16)(coeff[rc : rc+8]))
		absC := c.Max(negMax).Abs()
		keep := absC.GreaterEqual(threshVec)
		tmp := absC.AddSaturated(roundVec)
		lo := tmp.MulWidenLo(quantVec).ShiftAllRight(15)
		hi := tmp.HiToLo().MulWidenLo(quantVec).ShiftAllRight(15)
		mag := lo.ToBits().ReshapeToUint16s().ConcatEven(hi.ToBits().ReshapeToUint16s()).BitsToInt16().Masked(keep)
		neg := c.Less(zero)
		q := zero.Sub(mag).IfElse(neg, mag)
		dqMag := mag.ToBits().Mul(deqU16).ShiftAllRight(1).BitsToInt16()
		dq := zero.Sub(dqMag).IfElse(neg, dqMag)
		q.StoreArray((*[8]int16)(qcoeff[rc : rc+8]))
		dq.StoreArray((*[8]int16)(dqcoeff[rc : rc+8]))
		isc := archsimd.LoadInt16x8Array((*[8]int16)(iscan[rc : rc+8]))
		eobVec = eobVec.Max(isc.AndNot(mag.Equal(zero).ToInt16x8()))
	}
	return int(eobVec.ReduceMax()), true
}
