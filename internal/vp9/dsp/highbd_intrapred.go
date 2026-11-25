package dsp

import "github.com/thesyncim/govpx/internal/vp9/common"

// High-bit-depth (10-bit / 12-bit) VP9 intra predictors.
// Ported verbatim from libvpx v1.16.0 vpx_dsp/intrapred.c.
//
// Convention: `above` has top-left at index 0, above[0..bs-1] at indices
// 1..bs, and (for 4x4) above-right at indices 5..8. `left` has left[0..bs-1]
// at indices 0..bs-1.

func highbdAvg2(a, b uint16) uint16 {
	return uint16((int(a) + int(b) + 1) >> 1)
}

func highbdAvg3(a, b, c uint16) uint16 {
	return uint16((int(a) + 2*int(b) + int(c) + 2) >> 2)
}

func highbdFill16(dst []uint16, val uint16) {
	for i := range dst {
		dst[i] = val
	}
}

// HighbdIntraPredict runs one VP9 intra predictor on a bs x bs uint16 block
// (bs in {4, 8, 16, 32}). aboveFull[0] is top-left, aboveFull[1..] is above.
func HighbdIntraPredict(dst []uint16, dstOff, stride, bs int,
	mode common.PredictionMode, aboveFull []uint16, left []uint16,
	haveTop, haveLeft bool, bd int,
) {
	above := aboveFull[1:]
	topLeft := aboveFull[0]
	switch mode {
	case common.DcPred:
		switch {
		case haveTop && haveLeft:
			sum := 0
			for i := range bs {
				sum += int(above[i]) + int(left[i])
			}
			val := uint16((sum + bs) / (2 * bs))
			for r := range bs {
				highbdFill16(dst[dstOff+r*stride:dstOff+r*stride+bs], val)
			}
		case haveTop:
			sum := 0
			for i := range bs {
				sum += int(above[i])
			}
			val := uint16((sum + (bs >> 1)) / bs)
			for r := range bs {
				highbdFill16(dst[dstOff+r*stride:dstOff+r*stride+bs], val)
			}
		case haveLeft:
			sum := 0
			for i := range bs {
				sum += int(left[i])
			}
			val := uint16((sum + (bs >> 1)) / bs)
			for r := range bs {
				highbdFill16(dst[dstOff+r*stride:dstOff+r*stride+bs], val)
			}
		default:
			val := uint16(128 << uint(bd-8))
			for r := range bs {
				highbdFill16(dst[dstOff+r*stride:dstOff+r*stride+bs], val)
			}
		}
	case common.VPred:
		for r := range bs {
			copy(dst[dstOff+r*stride:dstOff+r*stride+bs], above[:bs])
		}
	case common.HPred:
		for r := range bs {
			highbdFill16(dst[dstOff+r*stride:dstOff+r*stride+bs], left[r])
		}
	case common.TmPred:
		tl := int(topLeft)
		for r := range bs {
			lr := int(left[r]) - tl
			row := dst[dstOff+r*stride:]
			for c := range bs {
				row[c] = clipPixelHighbd(lr+int(above[c]), bd)
			}
		}
	case common.D45Pred:
		if bs == 4 {
			highbdD45Pred4x4(dst, dstOff, stride, above)
			return
		}
		highbdD45Pred(dst, dstOff, stride, bs, above)
	case common.D63Pred:
		if bs == 4 {
			highbdD63Pred4x4(dst, dstOff, stride, above)
			return
		}
		highbdD63Pred(dst, dstOff, stride, bs, above)
	case common.D117Pred:
		if bs == 4 {
			highbdD117Pred4x4(dst, dstOff, stride, topLeft, above, left)
			return
		}
		highbdD117Pred(dst, dstOff, stride, bs, topLeft, above, left)
	case common.D135Pred:
		if bs == 4 {
			highbdD135Pred4x4(dst, dstOff, stride, topLeft, above, left)
			return
		}
		highbdD135Pred(dst, dstOff, stride, bs, topLeft, above, left)
	case common.D153Pred:
		if bs == 4 {
			highbdD153Pred4x4(dst, dstOff, stride, topLeft, above, left)
			return
		}
		highbdD153Pred(dst, dstOff, stride, bs, topLeft, above, left)
	case common.D207Pred:
		if bs == 4 {
			highbdD207Pred4x4(dst, dstOff, stride, left)
			return
		}
		highbdD207Pred(dst, dstOff, stride, bs, left)
	}
}

func highbdD207Pred4x4(dst []uint16, off, stride int, left []uint16) {
	I, J, K, L := left[0], left[1], left[2], left[3]
	dst[off+0*stride+0] = highbdAvg2(I, J)
	v := highbdAvg2(J, K)
	dst[off+0*stride+2] = v
	dst[off+1*stride+0] = v
	v = highbdAvg2(K, L)
	dst[off+1*stride+2] = v
	dst[off+2*stride+0] = v
	dst[off+0*stride+1] = highbdAvg3(I, J, K)
	v = highbdAvg3(J, K, L)
	dst[off+0*stride+3] = v
	dst[off+1*stride+1] = v
	v = highbdAvg3(K, L, L)
	dst[off+1*stride+3] = v
	dst[off+2*stride+1] = v
	dst[off+2*stride+3] = L
	dst[off+2*stride+2] = L
	dst[off+3*stride+0] = L
	dst[off+3*stride+1] = L
	dst[off+3*stride+2] = L
	dst[off+3*stride+3] = L
}

func highbdD63Pred4x4(dst []uint16, off, stride int, above []uint16) {
	A, B, C, D := above[0], above[1], above[2], above[3]
	E, F, G := above[4], above[5], above[6]
	dst[off+0*stride+0] = highbdAvg2(A, B)
	v := highbdAvg2(B, C)
	dst[off+0*stride+1] = v
	dst[off+2*stride+0] = v
	v = highbdAvg2(C, D)
	dst[off+0*stride+2] = v
	dst[off+2*stride+1] = v
	v = highbdAvg2(D, E)
	dst[off+0*stride+3] = v
	dst[off+2*stride+2] = v
	dst[off+2*stride+3] = highbdAvg2(E, F)

	dst[off+1*stride+0] = highbdAvg3(A, B, C)
	v = highbdAvg3(B, C, D)
	dst[off+1*stride+1] = v
	dst[off+3*stride+0] = v
	v = highbdAvg3(C, D, E)
	dst[off+1*stride+2] = v
	dst[off+3*stride+1] = v
	v = highbdAvg3(D, E, F)
	dst[off+1*stride+3] = v
	dst[off+3*stride+2] = v
	dst[off+3*stride+3] = highbdAvg3(E, F, G)
}

func highbdD45Pred4x4(dst []uint16, off, stride int, above []uint16) {
	A, B, C, D := above[0], above[1], above[2], above[3]
	E, F, G, H := above[4], above[5], above[6], above[7]
	dst[off+0*stride+0] = highbdAvg3(A, B, C)
	v := highbdAvg3(B, C, D)
	dst[off+0*stride+1] = v
	dst[off+1*stride+0] = v
	v = highbdAvg3(C, D, E)
	dst[off+0*stride+2] = v
	dst[off+1*stride+1] = v
	dst[off+2*stride+0] = v
	v = highbdAvg3(D, E, F)
	dst[off+0*stride+3] = v
	dst[off+1*stride+2] = v
	dst[off+2*stride+1] = v
	dst[off+3*stride+0] = v
	v = highbdAvg3(E, F, G)
	dst[off+1*stride+3] = v
	dst[off+2*stride+2] = v
	dst[off+3*stride+1] = v
	v = highbdAvg3(F, G, H)
	dst[off+2*stride+3] = v
	dst[off+3*stride+2] = v
	dst[off+3*stride+3] = H
}

func highbdD117Pred4x4(dst []uint16, off, stride int, X uint16, above, left []uint16) {
	I, J, K := left[0], left[1], left[2]
	A, B, C, D := above[0], above[1], above[2], above[3]
	v := highbdAvg2(X, A)
	dst[off+0*stride+0] = v
	dst[off+2*stride+1] = v
	v = highbdAvg2(A, B)
	dst[off+0*stride+1] = v
	dst[off+2*stride+2] = v
	v = highbdAvg2(B, C)
	dst[off+0*stride+2] = v
	dst[off+2*stride+3] = v
	dst[off+0*stride+3] = highbdAvg2(C, D)

	dst[off+3*stride+0] = highbdAvg3(K, J, I)
	dst[off+2*stride+0] = highbdAvg3(J, I, X)
	v = highbdAvg3(I, X, A)
	dst[off+1*stride+0] = v
	dst[off+3*stride+1] = v
	v = highbdAvg3(X, A, B)
	dst[off+1*stride+1] = v
	dst[off+3*stride+2] = v
	v = highbdAvg3(A, B, C)
	dst[off+1*stride+2] = v
	dst[off+3*stride+3] = v
	dst[off+1*stride+3] = highbdAvg3(B, C, D)
}

func highbdD135Pred4x4(dst []uint16, off, stride int, X uint16, above, left []uint16) {
	I, J, K, L := left[0], left[1], left[2], left[3]
	A, B, C, D := above[0], above[1], above[2], above[3]
	dst[off+3*stride+0] = highbdAvg3(J, K, L)
	v := highbdAvg3(I, J, K)
	dst[off+3*stride+1] = v
	dst[off+2*stride+0] = v
	v = highbdAvg3(X, I, J)
	dst[off+3*stride+2] = v
	dst[off+2*stride+1] = v
	dst[off+1*stride+0] = v
	v = highbdAvg3(A, X, I)
	dst[off+3*stride+3] = v
	dst[off+2*stride+2] = v
	dst[off+1*stride+1] = v
	dst[off+0*stride+0] = v
	v = highbdAvg3(B, A, X)
	dst[off+2*stride+3] = v
	dst[off+1*stride+2] = v
	dst[off+0*stride+1] = v
	v = highbdAvg3(C, B, A)
	dst[off+1*stride+3] = v
	dst[off+0*stride+2] = v
	dst[off+0*stride+3] = highbdAvg3(D, C, B)
}

func highbdD153Pred4x4(dst []uint16, off, stride int, X uint16, above, left []uint16) {
	I, J, K, L := left[0], left[1], left[2], left[3]
	A, B, C := above[0], above[1], above[2]
	v := highbdAvg2(I, X)
	dst[off+0*stride+0] = v
	dst[off+1*stride+2] = v
	v = highbdAvg2(J, I)
	dst[off+1*stride+0] = v
	dst[off+2*stride+2] = v
	v = highbdAvg2(K, J)
	dst[off+2*stride+0] = v
	dst[off+3*stride+2] = v
	dst[off+3*stride+0] = highbdAvg2(L, K)

	dst[off+0*stride+3] = highbdAvg3(A, B, C)
	dst[off+0*stride+2] = highbdAvg3(X, A, B)
	v = highbdAvg3(I, X, A)
	dst[off+0*stride+1] = v
	dst[off+1*stride+3] = v
	v = highbdAvg3(J, I, X)
	dst[off+1*stride+1] = v
	dst[off+2*stride+3] = v
	v = highbdAvg3(K, J, I)
	dst[off+2*stride+1] = v
	dst[off+3*stride+3] = v
	dst[off+3*stride+1] = highbdAvg3(L, K, J)
}

func highbdD207Pred(dst []uint16, off, stride, bs int, left []uint16) {
	for r := range bs - 1 {
		dst[off+r*stride] = highbdAvg2(left[r], left[r+1])
	}
	dst[off+(bs-1)*stride] = left[bs-1]
	off++
	for r := range bs - 2 {
		dst[off+r*stride] = highbdAvg3(left[r], left[r+1], left[r+2])
	}
	dst[off+(bs-2)*stride] = highbdAvg3(left[bs-2], left[bs-1], left[bs-1])
	dst[off+(bs-1)*stride] = left[bs-1]
	off++
	for c := range bs - 2 {
		dst[off+(bs-1)*stride+c] = left[bs-1]
	}
	for r := bs - 2; r >= 0; r-- {
		for c := range bs - 2 {
			dst[off+r*stride+c] = dst[off+(r+1)*stride+c-2]
		}
	}
}

func highbdD63Pred(dst []uint16, off, stride, bs int, above []uint16) {
	for c := range bs {
		dst[off+c] = highbdAvg2(above[c], above[c+1])
		dst[off+stride+c] = highbdAvg3(above[c], above[c+1], above[c+2])
	}
	for r, size := 2, bs-2; r < bs; r, size = r+2, size-1 {
		copy(dst[off+r*stride:off+r*stride+size], dst[off+(r>>1):off+(r>>1)+size])
		highbdFill16(dst[off+r*stride+size:off+r*stride+bs], above[bs-1])
		copy(dst[off+(r+1)*stride:off+(r+1)*stride+size], dst[off+stride+(r>>1):off+stride+(r>>1)+size])
		highbdFill16(dst[off+(r+1)*stride+size:off+(r+1)*stride+bs], above[bs-1])
	}
}

func highbdD45Pred(dst []uint16, off, stride, bs int, above []uint16) {
	aboveRight := above[bs-1]
	row0 := off
	for x := range bs - 1 {
		dst[off+x] = highbdAvg3(above[x], above[x+1], above[x+2])
	}
	dst[off+bs-1] = aboveRight
	off += stride
	for x, size := 1, bs-2; x < bs; x, size = x+1, size-1 {
		copy(dst[off:off+size], dst[row0+x:row0+x+size])
		highbdFill16(dst[off+size:off+bs], aboveRight)
		off += stride
	}
}

func highbdD117Pred(dst []uint16, off, stride, bs int, topLeft uint16, above, left []uint16) {
	dst[off+0] = highbdAvg2(topLeft, above[0])
	for c := 1; c < bs; c++ {
		dst[off+c] = highbdAvg2(above[c-1], above[c])
	}
	off += stride

	dst[off+0] = highbdAvg3(left[0], topLeft, above[0])
	dst[off+1] = highbdAvg3(topLeft, above[0], above[1])
	for c := 2; c < bs; c++ {
		dst[off+c] = highbdAvg3(above[c-2], above[c-1], above[c])
	}
	off += stride

	dst[off+0] = highbdAvg3(topLeft, left[0], left[1])
	for r := 3; r < bs; r++ {
		dst[off+(r-2)*stride] = highbdAvg3(left[r-3], left[r-2], left[r-1])
	}

	for r := 2; r < bs; r++ {
		for c := 1; c < bs; c++ {
			dst[off+c] = dst[off-2*stride+c-1]
		}
		off += stride
	}
}

func highbdD135Pred(dst []uint16, off, stride, bs int, topLeft uint16, above, left []uint16) {
	var border [32 + 32 - 1]uint16
	for i := range bs - 2 {
		border[i] = highbdAvg3(left[bs-3-i], left[bs-2-i], left[bs-1-i])
	}
	border[bs-2] = highbdAvg3(topLeft, left[0], left[1])
	border[bs-1] = highbdAvg3(left[0], topLeft, above[0])
	border[bs] = highbdAvg3(topLeft, above[0], above[1])
	for i := range bs - 2 {
		border[bs+1+i] = highbdAvg3(above[i], above[i+1], above[i+2])
	}
	for i := range bs {
		copy(dst[off+i*stride:off+i*stride+bs], border[bs-1-i:bs-1-i+bs])
	}
}

func highbdD153Pred(dst []uint16, off, stride, bs int, topLeft uint16, above, left []uint16) {
	dst[off+0] = highbdAvg2(topLeft, left[0])
	for r := 1; r < bs; r++ {
		dst[off+r*stride] = highbdAvg2(left[r-1], left[r])
	}
	off++

	dst[off+0] = highbdAvg3(left[0], topLeft, above[0])
	dst[off+stride] = highbdAvg3(topLeft, left[0], left[1])
	for r := 2; r < bs; r++ {
		dst[off+r*stride] = highbdAvg3(left[r-2], left[r-1], left[r])
	}
	off++

	dst[off+0] = highbdAvg3(topLeft, above[0], above[1])
	for c := 1; c < bs-2; c++ {
		dst[off+c] = highbdAvg3(above[c-1], above[c], above[c+1])
	}
	off += stride

	for r := 1; r < bs; r++ {
		for c := range bs - 2 {
			dst[off+c] = dst[off-stride+c-2]
		}
		off += stride
	}
}
