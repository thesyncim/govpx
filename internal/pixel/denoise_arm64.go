//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

const byteDenoise16 = true

// denoiseFirst16Bytes uses the existing denoiser's byte arithmetic for a
// complete luma block. The caller restricts inc to 0..1 and level to 3..5.
// Pointer values only detect aliases; row loads and stores use checked slices.
func denoiseFirst16Bytes(mc []byte, ms int, avg []byte, as int, sig []byte, ss, inc, level int) (int, bool) {
	if ms < 16 || as < 16 || ss < 16 || len(mc) < 16 || len(avg) < 16 || len(sig) < 16 || ms > (len(mc)-16)/15 || as > (len(avg)-16)/15 || ss > (len(sig)-16)/15 {
		return 0, false
	}
	ap := uintptr(unsafe.Pointer(unsafe.SliceData(avg)))
	independent := func(in []byte, stride int) bool {
		ip := uintptr(unsafe.Pointer(unsafe.SliceData(in)))
		return ap == ip && as == stride || ap <= ip && ip-ap >= uintptr(len(avg)) || ip < ap && ap-ip >= uintptr(len(in))
	}
	if !independent(mc, ms) || !independent(sig, ss) {
		return 0, false
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
	return int(columns.ExtendLo8ToInt16().Add(columns.HiToLo().ExtendLo8ToInt16()).ReduceSum()), true
}
