//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"simd/archsimd"
)

func vecRound(v archsimd.Int32x4) archsimd.Int32x4 { return pixel.RoundTransform4(v) }
func vecWrap(v archsimd.Int32x4) archsimd.Int32x4  { return pixel.WrapTransform4(v) }
func forwardNetwork(in, out []archsimd.Int32x4, n int, adst bool) {
	switch n {
	case 4:
		if adst {
			forwardADST4Vec(in, out)
		} else {
			forwardDCT4Vec(in, out)
		}
	case 8:
		if adst {
			forwardADST8Vec(in, out)
		} else {
			forwardDCT8Vec(in, out)
		}
	case 16:
		if adst {
			forwardADST16Vec(in, out)
		} else {
			forwardDCT16Vec(in, out)
		}
	}
}
func forwardTransformGo(input []int16, stride int, output []int16, n int, tx common.TxType) bool {
	if tx == common.DctDct {
		if forwardDCTWideGo(input, stride, output, n) {
			return true
		}
		switch n {
		case 4:
			return forwardDCT4Go(input, stride, output)
		case 8:
			return forwardDCT8Go(input, stride, output)
		case 16:
			return forwardDCT16Go(input, stride, output)
		}
	}

	if !pixel.Enabled || stride < n || len(input) < (n-1)*stride+n || len(output) < n*n {
		return false
	}
	var tmp [256]int32
	var in, out [16]archsimd.Int32x4
	one := archsimd.BroadcastInt32x4(1)
	for col := 0; col < n; col += 4 {
		var magnitude archsimd.Int32x4
		for row := range n {
			v := pixel.Load4(input[row*stride+col:])
			magnitude = magnitude.Max(v.Abs())
			if n == 4 {
				v = v.ShiftAllLeft(4)
			} else {
				v = v.ShiftAllLeft(2)
			}
			in[row] = v
		}
		// Preserve the public int16 API outside the codec residual range. Larger
		// inputs can overflow the int32 multiply networks and use scalar Go.
		if pixel.Max4(magnitude) > 255 {
			return false
		}
		if n == 4 && col == 0 {
			v := in[0].GetElem(0)
			if v != 0 {
				in[0] = in[0].SetElem(0, v+1)
			}
		}
		forwardNetwork(in[:], out[:], n, tx == common.AdstDct || tx == common.AdstAdst)
		if n == 16 {
			for j := range n {
				v := out[j].Add(one)
				if tx != common.DctDct {
					v = v.Sub(out[j].ShiftAllRight(31))
				}
				out[j] = v.ShiftAllRight(2)
			}
		}
		for j := 0; j < n; j += 4 {
			a, b, c, d := pixel.Transpose4(out[j], out[j+1], out[j+2], out[j+3])
			a.Store(tmp[(col+0)*n+j:])
			b.Store(tmp[(col+1)*n+j:])
			c.Store(tmp[(col+2)*n+j:])
			d.Store(tmp[(col+3)*n+j:])
		}
	}
	for col := 0; col < n; col += 4 {
		for row := range n {
			in[row] = archsimd.LoadInt32x4(tmp[row*n+col:])
		}
		forwardNetwork(in[:], out[:], n, tx == common.DctAdst || tx == common.AdstAdst)
		for j := range n {
			v := out[j]
			if n == 4 {
				v = v.Add(one).ShiftAllRight(2)
			} else if n == 8 {
				v = v.Sub(v.ShiftAllRight(31)).ShiftAllRight(1)
			}
			out[j] = v
		}
		for j := 0; j < n; j += 4 {
			a, b, c, d := pixel.Transpose4(out[j], out[j+1], out[j+2], out[j+3])
			pixel.Store4(output[(col+0)*n+j:], a)
			pixel.Store4(output[(col+1)*n+j:], b)
			pixel.Store4(output[(col+2)*n+j:], c)
			pixel.Store4(output[(col+3)*n+j:], d)
		}
	}
	return true
}
