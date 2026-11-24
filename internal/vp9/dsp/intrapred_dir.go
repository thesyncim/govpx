package dsp

// Directional intra predictors (D45 / D63 / D117 / D135 / D153 / D207)
// for VP9 block sizes 8 / 16 / 32. Ported from libvpx v1.16.0
// vpx_dsp/intrapred.c — the parametric implementations that the
// intra_pred_no_4x4 macro instantiates per size. The hand-coded 4x4
// variants live alongside in a follow-up to keep this file focused.
//
// AVG2(a,b) = (a + b + 1) >> 1 — rounded mean
// AVG3(a,b,c) = (a + 2*b + c + 2) >> 2 — rounded weighted mean
//
// `above` is passed with the [-1] top-left corner stored at index 0;
// the per-helper code re-slices to above[1:] to match libvpx's signed
// index access pattern. `left` is read 0..bs-1.

func avg2(a, b int) uint8 { return uint8((a + b + 1) >> 1) }
func avg3(a, b, c int) uint8 {
	return uint8((a + 2*b + c + 2) >> 2)
}

// d207Predictor reproduces vpx_dsp/intrapred.c:d207_predictor.
// `left` only — synthesises a 207° diagonal from the column to the
// left of the block.
func d207Predictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	// first column
	for r := 0; r < bs-1; r++ {
		dst[r*stride] = avg2(int(left[r]), int(left[r+1]))
	}
	dst[(bs-1)*stride] = left[bs-1]

	// second column
	for r := 0; r < bs-2; r++ {
		dst[r*stride+1] = avg3(int(left[r]), int(left[r+1]), int(left[r+2]))
	}
	dst[(bs-2)*stride+1] = avg3(int(left[bs-2]), int(left[bs-1]), int(left[bs-1]))
	dst[(bs-1)*stride+1] = left[bs-1]

	// rest of last row
	for c := 0; c < bs-2; c++ {
		dst[(bs-1)*stride+2+c] = left[bs-1]
	}

	// rest fills in via copy-up-2 from the bottom-right corner.
	for r := bs - 2; r >= 0; r-- {
		for c := 0; c < bs-2; c++ {
			dst[r*stride+2+c] = dst[(r+1)*stride+c]
		}
	}
}

// d63Predictor reproduces vpx_dsp/intrapred.c:d63_predictor. Above-only
// 63° direction. Needs above[0..2*bs-1].
func d63Predictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = left
	// First two rows are direct averages of the above row.
	for c := range bs {
		dst[c] = avg2(int(above[c]), int(above[c+1]))
		dst[stride+c] = avg3(int(above[c]), int(above[c+1]), int(above[c+2]))
	}
	for r, size := 2, bs-2; r < bs; r, size = r+2, size-1 {
		// Even row: copy from row 0 starting at offset r/2, then pad with above[bs-1].
		copy(dst[(r+0)*stride:(r+0)*stride+size], dst[r>>1:(r>>1)+size])
		for c := size; c < bs; c++ {
			dst[(r+0)*stride+c] = above[bs-1]
		}
		// Odd row: copy from row 1 starting at offset r/2, then pad.
		copy(dst[(r+1)*stride:(r+1)*stride+size], dst[stride+(r>>1):stride+(r>>1)+size])
		for c := size; c < bs; c++ {
			dst[(r+1)*stride+c] = above[bs-1]
		}
	}
}

// d45Predictor reproduces vpx_dsp/intrapred.c:d45_predictor. Above-only
// 45° direction.
func d45Predictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = left
	aboveRight := above[bs-1]
	for x := 0; x < bs-1; x++ {
		dst[x] = avg3(int(above[x]), int(above[x+1]), int(above[x+2]))
	}
	dst[bs-1] = aboveRight
	for x, size := 1, bs-2; x < bs; x, size = x+1, size-1 {
		row := dst[x*stride : x*stride+bs]
		copy(row[:size], dst[x:x+size])
		for c := size; c < bs; c++ {
			row[c] = aboveRight
		}
	}
}

// d117Predictor reproduces vpx_dsp/intrapred.c:d117_predictor. Mixed
// above + left; reads above[-1] via the corner byte. The Go signature
// receives `aboveFull` with the corner byte at index 0 and the actual
// row above[0..2*bs-1] at indices 1..2*bs.
func d117Predictor(dst []uint8, stride, bs int, aboveFull, left []uint8) {
	cornerByte := aboveFull[0]
	above := aboveFull[1:]
	// first row
	for c := range bs {
		var a0 uint8
		if c == 0 {
			a0 = cornerByte
		} else {
			a0 = above[c-1]
		}
		dst[c] = avg2(int(a0), int(above[c]))
	}
	// second row
	dst[stride+0] = avg3(int(left[0]), int(cornerByte), int(above[0]))
	for c := 1; c < bs; c++ {
		var a0 uint8
		if c == 1 {
			a0 = cornerByte
		} else {
			a0 = above[c-2]
		}
		dst[stride+c] = avg3(int(a0), int(above[c-1]), int(above[c]))
	}
	// rest of first col — libvpx walks dst from the row-2 base; we
	// keep dst at the buffer root and account for the offset by adding
	// 2 to the row index inside this loop.
	dst[2*stride] = avg3(int(cornerByte), int(left[0]), int(left[1]))
	for r := 3; r < bs; r++ {
		dst[r*stride] = avg3(int(left[r-3]), int(left[r-2]), int(left[r-1]))
	}
	// the rest of the block
	for r := 2; r < bs; r++ {
		for c := 1; c < bs; c++ {
			dst[r*stride+c] = dst[(r-2)*stride+c-1]
		}
	}
}

// d135Predictor reproduces vpx_dsp/intrapred.c:d135_predictor. The Go
// signature receives `aboveFull` with the corner byte at index 0.
func d135Predictor(dst []uint8, stride, bs int, aboveFull, left []uint8) {
	cornerByte := aboveFull[0]
	above := aboveFull[1:]
	var border [69]uint8 // 32+32+padding
	for i := 0; i < bs-2; i++ {
		border[i] = avg3(int(left[bs-3-i]), int(left[bs-2-i]), int(left[bs-1-i]))
	}
	border[bs-2] = avg3(int(cornerByte), int(left[0]), int(left[1]))
	border[bs-1] = avg3(int(left[0]), int(cornerByte), int(above[0]))
	border[bs+0] = avg3(int(cornerByte), int(above[0]), int(above[1]))
	for i := 0; i < bs-2; i++ {
		border[bs+1+i] = avg3(int(above[i]), int(above[i+1]), int(above[i+2]))
	}
	for i := range bs {
		copy(dst[i*stride:i*stride+bs], border[bs-1-i:])
	}
}

// d153Predictor reproduces vpx_dsp/intrapred.c:d153_predictor. The Go
// signature receives `aboveFull` with the corner byte at index 0.
func d153Predictor(dst []uint8, stride, bs int, aboveFull, left []uint8) {
	cornerByte := aboveFull[0]
	above := aboveFull[1:]
	// first column
	dst[0] = avg2(int(cornerByte), int(left[0]))
	for r := 1; r < bs; r++ {
		dst[r*stride] = avg2(int(left[r-1]), int(left[r]))
	}
	// second column
	dst[1] = avg3(int(left[0]), int(cornerByte), int(above[0]))
	dst[stride+1] = avg3(int(cornerByte), int(left[0]), int(left[1]))
	for r := 2; r < bs; r++ {
		dst[r*stride+1] = avg3(int(left[r-2]), int(left[r-1]), int(left[r]))
	}
	// third column onward, row 0
	for c := 0; c < bs-2; c++ {
		var a0 uint8
		if c == 0 {
			a0 = cornerByte
		} else {
			a0 = above[c-1]
		}
		dst[2+c] = avg3(int(a0), int(above[c]), int(above[c+1]))
	}
	// rest of the block: shift-from-prior-row-by-2 columns
	for r := 1; r < bs; r++ {
		for c := 0; c < bs-2; c++ {
			dst[r*stride+2+c] = dst[(r-1)*stride+c]
		}
	}
}

// VpxD207Predictor{8x8,16x16,32x32} wrap the parametric helper. The 4x4
// path uses a dedicated hand-coded predictor (added separately when the
// decoder needs the 4x4 mode dispatch).

func VpxD207Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	d207Predictor(dst, stride, 8, above[1:], left)
}
func VpxD207Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	d207Predictor(dst, stride, 16, above[1:], left)
}
func VpxD207Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	d207Predictor(dst, stride, 32, above[1:], left)
}

func VpxD63Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	d63Predictor(dst, stride, 8, above[1:], left)
}
func VpxD63Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	d63Predictor(dst, stride, 16, above[1:], left)
}
func VpxD63Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	d63Predictor(dst, stride, 32, above[1:], left)
}

func VpxD45Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	d45Predictor(dst, stride, 8, above[1:], left)
}
func VpxD45Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	d45Predictor(dst, stride, 16, above[1:], left)
}
func VpxD45Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	d45Predictor(dst, stride, 32, above[1:], left)
}

func VpxD117Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	d117Predictor(dst, stride, 8, above, left)
}
func VpxD117Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	d117Predictor(dst, stride, 16, above, left)
}
func VpxD117Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	d117Predictor(dst, stride, 32, above, left)
}

func VpxD135Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	d135Predictor(dst, stride, 8, above, left)
}
func VpxD135Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	d135Predictor(dst, stride, 16, above, left)
}
func VpxD135Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	d135Predictor(dst, stride, 32, above, left)
}

func VpxD153Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	d153Predictor(dst, stride, 8, above, left)
}
func VpxD153Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	d153Predictor(dst, stride, 16, above, left)
}
func VpxD153Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	d153Predictor(dst, stride, 32, above, left)
}
