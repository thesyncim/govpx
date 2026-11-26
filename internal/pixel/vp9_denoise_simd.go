//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
)

// VP9DenoisePlane performs the VP9 temporal denoiser first pass and optional
// weaker correction pass using 16-byte SIMD vectors (NEON on arm64, AVX2 on
// amd64). It is bit-exact with the scalar vp9DenoiserFilterPlane loop for all
// blockW multiples of 16 (16, 32, 64) and blockW == 8.
//
// Returns (filterBlock, handled). When handled is false the caller falls back
// to the scalar implementation.
func VP9DenoisePlane(
	sig []byte, sigStride int,
	mcAvg []byte, mcAvgStride int,
	avg []byte, avgStride int,
	blockW, blockH, numPelsLog2 int,
	adj0, adj1, adj2, absdiffThresh, strongThresh, weakThresh int,
) (bool, bool) {
	if blockH <= 0 || blockH > 64 || (blockW != 8 && (blockW < 16 || blockW%16 != 0)) {
		return false, false
	}
	if sigStride < blockW || mcAvgStride < blockW || avgStride < blockW {
		return false, false
	}
	if len(sig) < (blockH-1)*sigStride+blockW ||
		len(mcAvg) < (blockH-1)*mcAvgStride+blockW ||
		len(avg) < (blockH-1)*avgStride+blockW {
		return false, false
	}
	// Ensure avg does not partially alias sig or mcAvg in an overlapping way.
	if slicesOverlap(avg, sig, (blockH-1)*avgStride+blockW, (blockH-1)*sigStride+blockW) ||
		slicesOverlap(avg, mcAvg, (blockH-1)*avgStride+blockW, (blockH-1)*mcAvgStride+blockW) {
		return false, false
	}

	_ = sig[(blockH-1)*sigStride+blockW-1]
	_ = mcAvg[(blockH-1)*mcAvgStride+blockW-1]
	_ = avg[(blockH-1)*avgStride+blockW-1]

	l1 := archsimd.BroadcastUint8x16(byte(adj0))
	l2 := archsimd.BroadcastUint8x16(byte(adj1))
	l3 := archsimd.BroadcastUint8x16(byte(adj2))
	seven := archsimd.BroadcastUint8x16(7)
	fifteen := archsimd.BroadcastUint8x16(15)
	thresh := archsimd.BroadcastUint8x16(byte(absdiffThresh))

	totalAdj := 0
	if blockW >= 16 {
		smallAdj := adj0 >= 0 && adj0 <= 15 && adj1 >= 0 && adj1 <= 15 && adj2 >= 0 && adj2 <= 15 && absdiffThresh >= 0 && absdiffThresh <= 15
		for x := 0; x < blockW; x += 16 {
			var accLo, accHi archsimd.Int16x8
			mo, so, ao := x, x, x
			y := 0
			if smallAdj {
				// Each lane of plus-minus is in [-15, +15], so 8 rows fit in int8 ([-120, +120]).
				for ; y+8 <= blockH; y += 8 {
					var b0, b1 archsimd.Int8x16
					for range 4 {
						m0 := archsimd.LoadUint8x16Array((*[16]byte)(mcAvg[mo : mo+16]))
						s0 := archsimd.LoadUint8x16Array((*[16]byte)(sig[so : so+16]))
						m1 := archsimd.LoadUint8x16Array((*[16]byte)(mcAvg[mo+mcAvgStride : mo+mcAvgStride+16]))
						s1 := archsimd.LoadUint8x16Array((*[16]byte)(sig[so+sigStride : so+sigStride+16]))
						a0 := m0.Max(s0).Sub(m0.Min(s0))
						a1 := m1.Max(s1).Sub(m1.Min(s1))
						mag0 := a0.IfElse(a0.LessEqual(thresh), l1.IfElse(a0.LessEqual(seven), l2.IfElse(a0.LessEqual(fifteen), l3)))
						mag1 := a1.IfElse(a1.LessEqual(thresh), l1.IfElse(a1.LessEqual(seven), l2.IfElse(a1.LessEqual(fifteen), l3)))
						neg0, neg1 := m0.Less(s0), m1.Less(s1)
						plus0, minus0 := mag0.Masked(neg0.Not()), mag0.Masked(neg0)
						plus1, minus1 := mag1.Masked(neg1.Not()), mag1.Masked(neg1)
						s0.AddSaturated(plus0).SubSaturated(minus0).StoreArray((*[16]byte)(avg[ao : ao+16]))
						s1.AddSaturated(plus1).SubSaturated(minus1).StoreArray((*[16]byte)(avg[ao+avgStride : ao+avgStride+16]))
						b0 = b0.Add(plus0.BitsToInt8().Sub(minus0.BitsToInt8()))
						b1 = b1.Add(plus1.BitsToInt8().Sub(minus1.BitsToInt8()))
						mo += 2 * mcAvgStride
						so += 2 * sigStride
						ao += 2 * avgStride
					}
					sum8 := b0.Add(b1)
					accLo = accLo.Add(sum8.ExtendLo8ToInt16())
					accHi = accHi.Add(sum8.HiToLo().ExtendLo8ToInt16())
				}
			}
			for ; y < blockH; y++ {
				m := archsimd.LoadUint8x16Array((*[16]byte)(mcAvg[mo : mo+16]))
				s := archsimd.LoadUint8x16Array((*[16]byte)(sig[so : so+16]))
				a := m.Max(s).Sub(m.Min(s))
				mag := l2.IfElse(a.LessEqual(fifteen), l3)
				mag = l1.IfElse(a.LessEqual(seven), mag)
				mag = a.IfElse(a.LessEqual(thresh), mag)
				neg := m.Less(s)
				plus := mag.Masked(neg.Not())
				minus := mag.Masked(neg)
				s.AddSaturated(plus).SubSaturated(minus).StoreArray((*[16]byte)(avg[ao : ao+16]))
				diff8 := plus.BitsToInt8().Sub(minus.BitsToInt8())
				accLo = accLo.Add(diff8.ExtendLo8ToInt16())
				accHi = accHi.Add(diff8.HiToLo().ExtendLo8ToInt16())
				mo += mcAvgStride
				so += sigStride
				ao += avgStride
			}
			totalAdj += int(accLo.Add(accHi).ReduceSum())
		}
	} else {
		// blockW == 8: process 8 bytes per row using load8 / storeBytes8.
		var accLo archsimd.Int16x8
		for y := range blockH {
			m := load8(mcAvg[y*mcAvgStride:])
			s := load8(sig[y*sigStride:])
			a := m.Max(s).Sub(m.Min(s))
			mag := l2.IfElse(a.LessEqual(fifteen), l3)
			mag = l1.IfElse(a.LessEqual(seven), mag)
			mag = a.IfElse(a.LessEqual(thresh), mag)
			neg := m.Less(s)
			plus := mag.Masked(neg.Not())
			minus := mag.Masked(neg)
			storeBytes8(avg[y*avgStride:], s.AddSaturated(plus).SubSaturated(minus))
			diff8 := plus.BitsToInt8().Sub(minus.BitsToInt8())
			accLo = accLo.Add(diff8.ExtendLo8ToInt16())
		}
		totalAdj = int(accLo.ReduceSum())
	}

	absTotal := totalAdj
	if absTotal < 0 {
		absTotal = -absTotal
	}
	if absTotal <= strongThresh {
		return true, true
	}

	delta := ((absTotal - strongThresh) >> uint(numPelsLog2)) + 1
	if delta >= 4 {
		return false, true
	}

	limit := archsimd.BroadcastUint8x16(byte(delta))
	if blockW >= 16 {
		for x := 0; x < blockW; x += 16 {
			var accLo, accHi archsimd.Int16x8
			for y := range blockH {
				m := archsimd.LoadUint8x16(mcAvg[y*mcAvgStride+x:])
				s := archsimd.LoadUint8x16(sig[y*sigStride+x:])
				a := m.Max(s).Sub(m.Min(s)).Min(limit)
				pos := s.Less(m)
				minus := a.Masked(pos)
				plus := a.Masked(pos.Not())
				cur := archsimd.LoadUint8x16(avg[y*avgStride+x:])
				cur.AddSaturated(plus).SubSaturated(minus).Store(avg[y*avgStride+x:])
				corr8 := plus.BitsToInt8().Sub(minus.BitsToInt8())
				accLo = accLo.Add(corr8.ExtendLo8ToInt16())
				accHi = accHi.Add(corr8.HiToLo().ExtendLo8ToInt16())
			}
			totalAdj += int(accLo.Add(accHi).ReduceSum())
		}
	} else {
		var accLo archsimd.Int16x8
		for y := range blockH {
			m := load8(mcAvg[y*mcAvgStride:])
			s := load8(sig[y*sigStride:])
			a := m.Max(s).Sub(m.Min(s)).Min(limit)
			pos := s.Less(m)
			minus := a.Masked(pos)
			plus := a.Masked(pos.Not())
			cur := load8(avg[y*avgStride:])
			storeBytes8(avg[y*avgStride:], cur.AddSaturated(plus).SubSaturated(minus))
			corr8 := plus.BitsToInt8().Sub(minus.BitsToInt8())
			accLo = accLo.Add(corr8.ExtendLo8ToInt16())
		}
		totalAdj += int(accLo.ReduceSum())
	}

	absTotal = totalAdj
	if absTotal < 0 {
		absTotal = -absTotal
	}
	return absTotal <= weakThresh, true
}
