//go:build goexperiment.simd && (arm64 || amd64) && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

func vecRound(v archsimd.Int32x4) archsimd.Int32x4 { return pixel.RoundTransform4(v) }
func vecWrap(v archsimd.Int32x4) archsimd.Int32x4  { return pixel.WrapTransform4(v) }
func inverseNetwork(in, out []archsimd.Int32x4, n int, adst bool) {
	switch n {
	case 4:
		if adst {
			Iadst4Vec(in, out)
		} else {
			idct4Vec(in, out)
		}
	case 8:
		if adst {
			iadst8Vec(in, out)
		} else {
			idct8Vec(in, out)
		}
	case 16:
		if adst {
			iadst16Vec(in, out)
		} else {
			idct16Vec(in, out)
		}
	case 32:
		idct32Vec(in, out)
	}
}
func inverseTransformGo(input []int16, dst []byte, stride, n, rows, tx int) bool {
	if !pixel.Enabled || len(input) < n*rows || stride < n || len(dst) < (n-1)*stride+n {
		return false
	}
	if tx == 0 {
		if inverseDCTWideGo(input, dst, stride, n, rows) {
			return true
		}
		switch n {
		case 4:
			return inverseDCT4Go(input, dst, stride, rows)
		case 8:
			return inverseDCT8Go(input, dst, stride, rows)
		case 16:
			return inverseDCT16Go(input, dst, stride, rows)
		}
	}
	return inverseTransformGenericGo(input, dst, stride, n, rows, tx)
}

func inverseTransformGenericGo(input []int16, dst []byte, stride, n, rows, tx int) bool {
	var tmp [1024]int32
	var in, out [32]archsimd.Int32x4
	for row := 0; row < rows; row += 4 {
		for j := 0; j < n; j += 4 {
			in[j], in[j+1], in[j+2], in[j+3] = pixel.Transpose4(pixel.Load4(input[(row+0)*n+j:]), pixel.Load4(input[(row+1)*n+j:]), pixel.Load4(input[(row+2)*n+j:]), pixel.Load4(input[(row+3)*n+j:]))
		}
		inverseNetwork(in[:], out[:], n, tx == 2 || tx == 3)
		for j := 0; j < n; j += 4 {
			a, b, c, d := pixel.Transpose4(out[j], out[j+1], out[j+2], out[j+3])
			a.Store(tmp[(row+0)*n+j:])
			b.Store(tmp[(row+1)*n+j:])
			c.Store(tmp[(row+2)*n+j:])
			d.Store(tmp[(row+3)*n+j:])
		}
	}
	shift := uint64(6)
	if n == 4 {
		shift = 4
	} else if n == 8 {
		shift = 5
	}
	bias := archsimd.BroadcastInt32x4(int32(1 << (shift - 1)))
	for col := 0; col < n; col += 4 {
		for row := range n {
			in[row] = archsimd.LoadInt32x4(tmp[row*n+col:])
		}
		inverseNetwork(in[:], out[:], n, tx == 1 || tx == 3)
		for row := range n {
			d := dst[row*stride+col:]
			v := out[row].Add(bias).ShiftAllRight(shift).Add(pixel.LoadPixels4(d))
			pixel.StorePixels4(d, v)
		}
	}
	return true
}

// Inverse lossless WHT uses reversible lifting and wraps after the row pass.
func inverseWHTGo(input []int16, dst []byte, stride int, dc bool) bool {
	if !pixel.Enabled {
		return false
	}
	var onlyDC [16]int16
	if dc {
		onlyDC[0] = input[0]
		input = onlyDC[:]
	}
	a, c, d, b := pixel.Transpose4(pixel.Load4(input).ShiftAllRight(2), pixel.Load4(input[4:]).ShiftAllRight(2), pixel.Load4(input[8:]).ShiftAllRight(2), pixel.Load4(input[12:]).ShiftAllRight(2))
	a = a.Add(c)
	d = d.Sub(b)
	e := a.Sub(d).ShiftAllRight(1)
	b = e.Sub(b)
	c = e.Sub(c)
	a = a.Sub(b)
	d = d.Add(c)
	a, c, d, b = pixel.Transpose4(vecWrap(a), vecWrap(b), vecWrap(c), vecWrap(d))
	a = a.Add(c)
	d = d.Sub(b)
	e = a.Sub(d).ShiftAllRight(1)
	b = e.Sub(b)
	c = e.Sub(c)
	a = a.Sub(b)
	d = d.Add(c)
	pixel.StorePixels4(dst, a.Add(pixel.LoadPixels4(dst)))
	pixel.StorePixels4(dst[stride:], b.Add(pixel.LoadPixels4(dst[stride:])))
	pixel.StorePixels4(dst[2*stride:], c.Add(pixel.LoadPixels4(dst[2*stride:])))
	pixel.StorePixels4(dst[3*stride:], d.Add(pixel.LoadPixels4(dst[3*stride:])))
	return true
}
