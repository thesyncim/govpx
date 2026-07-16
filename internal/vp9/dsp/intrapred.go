package dsp

// VP9 intra prediction kernels — DC / V / H / TM families and their DC
// fall-back variants (dc_128, dc_left, dc_top). Ported from libvpx
// v1.16.0 vpx_dsp/intrapred.c. Each predictor takes a caller-owned
// destination plane plus the row above and column to the left of the
// block; `above` is read at indices -1 .. bs (the [-1] entry is the
// top-left corner shared with `left`), `left` is read at indices
// 0 .. bs-1. Strides are in pixels.
//
// The 4x4/8x8/16x16/32x32 variants are explicit functions rather than a
// runtime-sized wrapper so the Go inliner and bounds-check elimination
// pass can specialize each size separately.

func dcPredictor(dst []uint8, stride, bs int, above, left []uint8) {
	sum := 0
	for i := range bs {
		sum += int(above[i])
		sum += int(left[i])
	}
	count := 2 * bs
	expected := uint8((sum + (count >> 1)) / count)
	fillBlockConstant(dst, stride, bs, expected)
}

func dcLeftPredictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	sum := 0
	for i := range bs {
		sum += int(left[i])
	}
	expected := uint8((sum + (bs >> 1)) / bs)
	fillBlockConstant(dst, stride, bs, expected)
}

func dcTopPredictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = left
	sum := 0
	for i := range bs {
		sum += int(above[i])
	}
	expected := uint8((sum + (bs >> 1)) / bs)
	fillBlockConstant(dst, stride, bs, expected)
}

func dc128Predictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	_ = left
	fillBlockConstant(dst, stride, bs, 128)
}

func vPredictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = left
	for r := range bs {
		copy(dst[r*stride:r*stride+bs], above[:bs])
	}
}

func hPredictor(dst []uint8, stride, bs int, above, left []uint8) {
	_ = above
	for r := range bs {
		row := dst[r*stride : r*stride+bs]
		v := left[r]
		for i := range row {
			row[i] = v
		}
	}
}

func hPredictor4(dst []uint8, stride int, left []uint8) {
	for r := range 4 {
		row := dst[r*stride:]
		_ = row[3]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
	}
}

func hPredictor8(dst []uint8, stride int, left []uint8) {
	for r := range 8 {
		row := dst[r*stride:]
		_ = row[7]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
		row[4], row[5], row[6], row[7] = v, v, v, v
	}
}

func hPredictor16(dst []uint8, stride int, left []uint8) {
	for r := range 16 {
		row := dst[r*stride:]
		_ = row[15]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
		row[4], row[5], row[6], row[7] = v, v, v, v
		row[8], row[9], row[10], row[11] = v, v, v, v
		row[12], row[13], row[14], row[15] = v, v, v, v
	}
}

func hPredictor32(dst []uint8, stride int, left []uint8) {
	for r := range 32 {
		row := dst[r*stride:]
		_ = row[31]
		v := left[r]
		row[0], row[1], row[2], row[3] = v, v, v, v
		row[4], row[5], row[6], row[7] = v, v, v, v
		row[8], row[9], row[10], row[11] = v, v, v, v
		row[12], row[13], row[14], row[15] = v, v, v, v
		row[16], row[17], row[18], row[19] = v, v, v, v
		row[20], row[21], row[22], row[23] = v, v, v, v
		row[24], row[25], row[26], row[27] = v, v, v, v
		row[28], row[29], row[30], row[31] = v, v, v, v
	}
}

func fillBlockConstant(dst []uint8, stride, bs int, v uint8) {
	row0 := dst[:bs]
	for i := range row0 {
		row0[i] = v
	}
	for r := 1; r < bs; r++ {
		copy(dst[r*stride:r*stride+bs], row0)
	}
}

// tmPredictor implements True-Motion: pixel(r,c) = clip(left[r] +
// above[c] - above[-1]). Callers must pass a slice whose backing array
// has at least one byte before `above[0]` so the -1 lookup is valid.
func tmPredictor(dst []uint8, stride, bs int, above, left []uint8) {
	// above is provided with above[0]..above[bs+...], and aboveTopLeft is
	// passed via above[-1] in libvpx. To keep that contract in Go we
	// expect callers to pass a sub-slice where index 0 is the top-left
	// corner pixel; ie. the parameter naming changes by one. To preserve
	// the same call shape as libvpx, the public entry points below
	// reslice into the right offset before invoking the helper.
	topLeft := int(above[0])
	above = above[1:]
	minAbove, maxAbove := int(above[0]), int(above[0])
	for i := 1; i < bs; i++ {
		a := int(above[i])
		if a < minAbove {
			minAbove = a
		} else if a > maxAbove {
			maxAbove = a
		}
	}
	for r := range bs {
		base := int(left[r]) - topLeft
		row := dst[r*stride : r*stride+bs]
		if base+minAbove >= 0 && base+maxAbove <= 255 {
			for c := range row {
				row[c] = uint8(base + int(above[c]))
			}
			continue
		}
		for c := range row {
			v := base + int(above[c])
			if v < 0 {
				row[c] = 0
			} else if v > 255 {
				row[c] = 255
			} else {
				row[c] = uint8(v)
			}
		}
	}
}

// The exported wrappers fix the block size at the type level so the
// compiler can constant-fold the inner loops. above is passed at index
// -1 in libvpx; the Go contract uses a 1-byte prefix so above[0] in
// these functions is the top-left corner and above[1..bs] is the row
// above. left[0..bs-1] is the column to the left.

// Vpx*Predictor4x4 etc match libvpx's vpx_<type>_predictor_4x4_c.

func VpxDcPredictor4x4(dst []uint8, stride int, above, left []uint8) {
	dcPredictor(dst, stride, 4, above[1:], left)
}
func VpxDcPredictor8x8(dst []uint8, stride int, above, left []uint8) {
	dcPredictor(dst, stride, 8, above[1:], left)
}
func VpxDcPredictor16x16(dst []uint8, stride int, above, left []uint8) {
	dcPredictor(dst, stride, 16, above[1:], left)
}
func VpxDcPredictor32x32(dst []uint8, stride int, above, left []uint8) {
	dcPredictor(dst, stride, 32, above[1:], left)
}

func VpxDcLeftPredictor4x4(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictor(dst, stride, 4, above[1:], left)
}
func VpxDcLeftPredictor8x8(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictor(dst, stride, 8, above[1:], left)
}
func VpxDcLeftPredictor16x16(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictor(dst, stride, 16, above[1:], left)
}
func VpxDcLeftPredictor32x32(dst []uint8, stride int, above, left []uint8) {
	dcLeftPredictor(dst, stride, 32, above[1:], left)
}

func VpxDcTopPredictor4x4(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictor(dst, stride, 4, above[1:], left)
}
func VpxDcTopPredictor8x8(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictor(dst, stride, 8, above[1:], left)
}
func VpxDcTopPredictor16x16(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictor(dst, stride, 16, above[1:], left)
}
func VpxDcTopPredictor32x32(dst []uint8, stride int, above, left []uint8) {
	dcTopPredictor(dst, stride, 32, above[1:], left)
}

func VpxDc128Predictor4x4(dst []uint8, stride int, above, left []uint8) {
	dc128Predictor(dst, stride, 4, above[1:], left)
}
func VpxDc128Predictor8x8(dst []uint8, stride int, above, left []uint8) {
	dc128Predictor(dst, stride, 8, above[1:], left)
}
func VpxDc128Predictor16x16(dst []uint8, stride int, above, left []uint8) {
	dc128Predictor(dst, stride, 16, above[1:], left)
}
func VpxDc128Predictor32x32(dst []uint8, stride int, above, left []uint8) {
	dc128Predictor(dst, stride, 32, above[1:], left)
}

func VpxVPredictor4x4(dst []uint8, stride int, above, left []uint8) {
	vPredictor(dst, stride, 4, above[1:], left)
}
func VpxVPredictor8x8(dst []uint8, stride int, above, left []uint8) {
	vPredictor(dst, stride, 8, above[1:], left)
}
func VpxVPredictor16x16(dst []uint8, stride int, above, left []uint8) {
	vPredictor(dst, stride, 16, above[1:], left)
}
func VpxVPredictor32x32(dst []uint8, stride int, above, left []uint8) {
	vPredictor(dst, stride, 32, above[1:], left)
}

func VpxHPredictor4x4(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor4(dst, stride, left)
}
func VpxHPredictor8x8(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor8(dst, stride, left)
}
func VpxHPredictor16x16(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor16(dst, stride, left)
}
func VpxHPredictor32x32(dst []uint8, stride int, above, left []uint8) {
	_ = above[0]
	hPredictor32(dst, stride, left)
}

func VpxTmPredictor4x4(dst []uint8, stride int, above, left []uint8) {
	tmPredictor(dst, stride, 4, above, left)
}
func VpxTmPredictor8x8(dst []uint8, stride int, above, left []uint8) {
	tmPredictor(dst, stride, 8, above, left)
}
func VpxTmPredictor16x16(dst []uint8, stride int, above, left []uint8) {
	tmPredictor(dst, stride, 16, above, left)
}
func VpxTmPredictor32x32(dst []uint8, stride int, above, left []uint8) {
	tmPredictor(dst, stride, 32, above, left)
}
