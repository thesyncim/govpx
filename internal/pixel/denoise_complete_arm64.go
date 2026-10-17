//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// DenoiseLuma16 filters a complete disjoint luma block, including the weaker
// correction pass. It returns (filtered, handled). Declines do not write.
// Column sums clamp to127 before the first decision and again after correction.
func DenoiseLuma16(mc []byte, ms int, avg []byte, as int, sig []byte, ss, inc, level, thresholdSum int) (bool, bool) {
	if inc < 0 || inc > 1 || level < 3 || level > 5 || (thresholdSum != 512 && thresholdSum != 600) || ms < 16 || as < 16 || ss < 16 || len(mc) < 16 || len(avg) < 16 || len(sig) < 16 || ms > (len(mc)-16)/15 || as > (len(avg)-16)/15 || ss > (len(sig)-16)/15 {
		return false, false
	}
	ap := uintptr(unsafe.Pointer(unsafe.SliceData(avg)))
	independent := func(in []byte) bool {
		ip := uintptr(unsafe.Pointer(unsafe.SliceData(in)))
		return ap <= ip && ip-ap >= uintptr(len(avg)) || ip < ap && ap-ip >= uintptr(len(in))
	}
	if !independent(mc) || !independent(sig) {
		return false, false
	}
	l1, l2, l3 := archsimd.BroadcastUint8x16(byte(level)), archsimd.BroadcastUint8x16(byte(level+1)), archsimd.BroadcastUint8x16(byte(level+3))
	seven, fifteen, threshold := archsimd.BroadcastUint8x16(7), archsimd.BroadcastUint8x16(15), archsimd.BroadcastUint8x16(byte(3+inc))
	var columns archsimd.Int8x16
	for y := range 16 {
		m, s := archsimd.LoadUint8x16(mc[y*ms:]), archsimd.LoadUint8x16(sig[y*ss:])
		a := m.Max(s).Sub(m.Min(s))
		magnitude := l2.IfElse(a.LessEqual(fifteen), l3)
		magnitude = l1.IfElse(a.LessEqual(seven), magnitude)
		magnitude = a.IfElse(a.LessEqual(threshold), magnitude)
		negative := m.Less(s)
		plus, minus := magnitude.Masked(negative.Not()), magnitude.Masked(negative)
		// Every adjustment is in [-8,8]. Before the last of sixteen rows the
		// column sum is in [-120,120], so signed saturation can only occur on
		// the final addition, exactly implementing the required upper clamp to 127.
		columns = columns.AddSaturated(plus.BitsToInt8().Sub(minus.BitsToInt8()))
		s.AddSaturated(plus).SubSaturated(minus).Store(avg[y*as:])
	}
	c0, c1 := columns.ExtendLo8ToInt16(), columns.HiToLo().ExtendLo8ToInt16()
	sum := int(c0.Add(c1).ReduceSum())
	if abs := max(sum, -sum); abs > thresholdSum {
		delta := ((abs - thresholdSum) >> 8) + 1
		if delta >= 4 {
			return false, true
		}
		limit := archsimd.BroadcastUint8x16(byte(delta))
		for y := range 16 {
			m, s := archsimd.LoadUint8x16(mc[y*ms:]), archsimd.LoadUint8x16(sig[y*ss:])
			a := m.Max(s).Sub(m.Min(s)).Min(limit)
			negative := m.Less(s)
			plus, minus := a.Masked(negative), a.Masked(negative.Not())
			correction := plus.BitsToInt8().Sub(minus.BitsToInt8())
			c0 = c0.Add(correction.ExtendLo8ToInt16())
			c1 = c1.Add(correction.HiToLo().ExtendLo8ToInt16())
			archsimd.LoadUint8x16(avg[y*as:]).SubSaturated(minus).AddSaturated(plus).Store(avg[y*as:])
		}
		upper := archsimd.BroadcastInt16x8(127)
		sum = int(c0.Min(upper).Add(c1.Min(upper)).ReduceSum())
		if max(sum, -sum) > thresholdSum {
			return false, true
		}
	}
	for y := range 16 {
		archsimd.LoadUint8x16(avg[y*as:]).Store(sig[y*ss:])
	}
	return true, true
}
