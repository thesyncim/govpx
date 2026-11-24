package dsp

// Residual builders for the VP9 inverse transforms. Each builder runs
// the same row + column passes as the scalar reference but lays the
// post-column output back into a row-major int16 buffer. The SIMD
// add+clip kernels in idct_arm64.s / idct_amd64.s consume that buffer
// directly, row by row.
//
// Byte parity vs the scalar reference is structural: the 1-D
// transforms (idct4/idct8/idct16/idct32/Iadst4/iadst8/iadst16) are
// invoked exactly as in *_AddScalar; only the per-row clip_pixel_add
// loop is moved out of Go and into SIMD.

// idct4x4Residual fills residual[16] (row-major) with the int16 result
// of the full 4x4 inverse DCT, pre-shift (the SIMD kernel applies
// SRSHR #4 to match ROUND_POWER_OF_TWO(., 4)).
func idct4x4Residual(input []int16, residual *[16]int16) {
	var out [16]int16
	for i := range 4 {
		idct4(input[i*4:i*4+4], out[i*4:i*4+4])
	}
	var tempIn, tempOut [4]int16
	for i := range 4 {
		for j := range 4 {
			tempIn[j] = out[j*4+i]
		}
		idct4(tempIn[:], tempOut[:])
		// Column pass produces (residual at column i, rows 0..3) =
		// tempOut[0..3]. Write into row-major residual buffer.
		residual[0*4+i] = tempOut[0]
		residual[1*4+i] = tempOut[1]
		residual[2*4+i] = tempOut[2]
		residual[3*4+i] = tempOut[3]
	}
}

// idct8x8Residual fills residual[64] (row-major) with the int16 result
// of the 8x8 inverse DCT. rowLimit selects how many input rows are
// run through the row pass (8 for _64Add, 4 for _12Add).
func idct8x8Residual(input []int16, residual *[64]int16, rowLimit int) {
	var out [64]int16
	for i := range rowLimit {
		idct8(input[i*8:i*8+8], out[i*8:i*8+8])
	}
	var tempIn, tempOut [8]int16
	for i := range 8 {
		for j := range 8 {
			tempIn[j] = out[j*8+i]
		}
		idct8(tempIn[:], tempOut[:])
		residual[0*8+i] = tempOut[0]
		residual[1*8+i] = tempOut[1]
		residual[2*8+i] = tempOut[2]
		residual[3*8+i] = tempOut[3]
		residual[4*8+i] = tempOut[4]
		residual[5*8+i] = tempOut[5]
		residual[6*8+i] = tempOut[6]
		residual[7*8+i] = tempOut[7]
	}
}

// idct16x16Residual fills residual[256] (row-major) with the int16
// result of the 16x16 inverse DCT.
func idct16x16Residual(input []int16, residual *[256]int16, rowLimit int) {
	var out [256]int16
	for i := range rowLimit {
		idct16(input[i*16:i*16+16], out[i*16:i*16+16])
	}
	var tempIn, tempOut [16]int16
	for i := range 16 {
		for j := range 16 {
			tempIn[j] = out[j*16+i]
		}
		idct16(tempIn[:], tempOut[:])
		for j := range 16 {
			residual[j*16+i] = tempOut[j]
		}
	}
}

// idct32x32Residual fills residual[1024] (row-major) with the int16
// result of the 32x32 inverse DCT.
func idct32x32Residual(input []int16, residual *[1024]int16, rowLimit int) {
	var out [1024]int16
	for i := range rowLimit {
		idct32(input[i*32:i*32+32], out[i*32:i*32+32])
	}
	var tempIn, tempOut [32]int16
	for i := range 32 {
		for j := range 32 {
			tempIn[j] = out[j*32+i]
		}
		idct32(tempIn[:], tempOut[:])
		for j := range 32 {
			residual[j*32+i] = tempOut[j]
		}
	}
}

// iht4x4Residual fills residual[16] (row-major) with the int16 result
// of a 2-D hybrid 4x4 inverse transform. txType selects the
// (row, column) kernel pair the same way iht4x4_16AddScalar does.
func iht4x4Residual(input []int16, residual *[16]int16, txType int) {
	rowAdst := txType == 2 || txType == 3
	colAdst := txType == 1 || txType == 3

	var out [16]int16
	for i := range 4 {
		if rowAdst {
			Iadst4(input[i*4:i*4+4], out[i*4:i*4+4])
		} else {
			idct4(input[i*4:i*4+4], out[i*4:i*4+4])
		}
	}
	var tempIn, tempOut [4]int16
	for i := range 4 {
		for j := range 4 {
			tempIn[j] = out[j*4+i]
		}
		if colAdst {
			Iadst4(tempIn[:], tempOut[:])
		} else {
			idct4(tempIn[:], tempOut[:])
		}
		residual[0*4+i] = tempOut[0]
		residual[1*4+i] = tempOut[1]
		residual[2*4+i] = tempOut[2]
		residual[3*4+i] = tempOut[3]
	}
}

// iht8x8Residual fills residual[64] (row-major) with the int16 result
// of a 2-D hybrid 8x8 inverse transform.
func iht8x8Residual(input []int16, residual *[64]int16, txType int) {
	rowAdst := txType == 2 || txType == 3
	colAdst := txType == 1 || txType == 3

	var out [64]int16
	for i := range 8 {
		if rowAdst {
			iadst8(input[i*8:i*8+8], out[i*8:i*8+8])
		} else {
			idct8(input[i*8:i*8+8], out[i*8:i*8+8])
		}
	}
	var tempIn, tempOut [8]int16
	for i := range 8 {
		for j := range 8 {
			tempIn[j] = out[j*8+i]
		}
		if colAdst {
			iadst8(tempIn[:], tempOut[:])
		} else {
			idct8(tempIn[:], tempOut[:])
		}
		for j := range 8 {
			residual[j*8+i] = tempOut[j]
		}
	}
}

// iht16x16Residual fills residual[256] (row-major) with the int16
// result of a 2-D hybrid 16x16 inverse transform.
func iht16x16Residual(input []int16, residual *[256]int16, txType int) {
	rowAdst := txType == 2 || txType == 3
	colAdst := txType == 1 || txType == 3

	var out [256]int16
	for i := range 16 {
		if rowAdst {
			iadst16(input[i*16:i*16+16], out[i*16:i*16+16])
		} else {
			idct16(input[i*16:i*16+16], out[i*16:i*16+16])
		}
	}
	var tempIn, tempOut [16]int16
	for i := range 16 {
		for j := range 16 {
			tempIn[j] = out[j*16+i]
		}
		if colAdst {
			iadst16(tempIn[:], tempOut[:])
		} else {
			idct16(tempIn[:], tempOut[:])
		}
		for j := range 16 {
			residual[j*16+i] = tempOut[j]
		}
	}
}
