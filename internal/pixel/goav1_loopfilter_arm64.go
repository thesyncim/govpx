//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

func loopFilterWide(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) bool {
	return loopFilterByteWide(p, q0, step, outer, n, blimit, limit, thresh, kind)
}

var lfByteRight1 = archsimd.BroadcastInt8x16(-1)
var lfByteRight3 = archsimd.BroadcastInt8x16(-3)

func lfByteShift1(v archsimd.Int8x16) archsimd.Int8x16  { return v.Shift(lfByteRight1) }
func lfByteShift3(v archsimd.Int8x16) archsimd.Int8x16  { return v.Shift(lfByteRight3) }
func lfByteHalf(v archsimd.Uint8x16) archsimd.Uint8x16  { return v.Shift(lfByteRight1) }
func lfByteLE(a, b archsimd.Uint8x16) archsimd.Mask8x16 { return a.LessEqual(b) }
func lfByteGT(a, b archsimd.Uint8x16) archsimd.Mask8x16 { return a.Greater(b) }
func lfTruncSigned(v archsimd.Int16x8) archsimd.Int8x16 { return v.TruncToInt8() }

func lfByteNot(v archsimd.Mask8x16) archsimd.Mask8x16 { return v.Not() }
