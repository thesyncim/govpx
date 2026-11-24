//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

// The libvpx v1.16.0 32-point forward network has a wider output than
// the smaller transforms. Keep every stage in int32 and wrap only when
// storing final int16 coefficients. Larger public inputs use scalar Go.
// For residuals within +/-255, affine coefficient bounds including each
// rounding error bound the first pass at 378134400 before normalization,
// intermediate samples at 5771, and second-pass arithmetic at 2139425120.
// Adding the 8192 rounding bias still fits int32 (2139433312 < 2147483647).
func forwardDCT32Go(input []int16, stride int, output []int16, rd bool) bool {
	if !pixel.Enabled || stride < 32 || len(input) < 31*stride+32 || len(output) < 1024 {
		return false
	}
	var tmp [1024]int32
	var in, out [32]archsimd.Int32x4
	one := archsimd.BroadcastInt32x4(1)
	zero := archsimd.Int32x4{}
	for col := 0; col < 32; col += 4 {
		var magnitude archsimd.Int32x4
		for row := range 32 {
			v := pixel.Load4(input[row*stride+col:])
			magnitude = magnitude.Max(v.Abs())
			in[row] = v.ShiftAllLeft(2)
		}
		if pixel.Max4(magnitude) > 255 {
			return false
		}
		forwardDCT32Vec(in[:], out[:], false)
		for row := range 32 {
			v := out[row]
			out[row] = v.Add(one).Sub(v.Greater(zero).ToInt32x4()).ShiftAllRight(2)
		}
		for row := 0; row < 32; row += 4 {
			a, b, c, d := pixel.Transpose4(out[row], out[row+1], out[row+2], out[row+3])
			a.Store(tmp[col*32+row:])
			b.Store(tmp[(col+1)*32+row:])
			c.Store(tmp[(col+2)*32+row:])
			d.Store(tmp[(col+3)*32+row:])
		}
	}
	for col := 0; col < 32; col += 4 {
		for row := range 32 {
			in[row] = archsimd.LoadInt32x4(tmp[row*32+col:])
		}
		forwardDCT32Vec(in[:], out[:], rd)
		if !rd {
			for row := range 32 {
				out[row] = vecHalfRound(out[row])
			}
		}
		for row := 0; row < 32; row += 4 {
			a, b, c, d := pixel.Transpose4(out[row], out[row+1], out[row+2], out[row+3])
			pixel.Store4(output[col*32+row:], a)
			pixel.Store4(output[(col+1)*32+row:], b)
			pixel.Store4(output[(col+2)*32+row:], c)
			pixel.Store4(output[(col+3)*32+row:], d)
		}
	}
	return true
}
func vecHalfRound(v archsimd.Int32x4) archsimd.Int32x4 {
	return v.Add(archsimd.BroadcastInt32x4(1)).Sub(v.ShiftAllRight(31)).ShiftAllRight(2)
}
