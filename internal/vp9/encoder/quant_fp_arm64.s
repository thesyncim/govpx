//go:build arm64 && !purego

// ARMv8 NEON bulk AC loop for libvpx v1.16.0
// vp9/encoder/arm/neon/vp9_quantize_neon.c::vp9_quantize_fp_neon.
// Go handles DC and the non-multiple-of-8 tail; this kernel processes
// raster-order AC coefficients in 8-lane chunks and max-reduces iscan for EOB.

#include "textflag.h"

// quantizeFPACNEON ABI ($0-68):
//   coeff+0(FP)    *int16  // starts at AC coefficient 1
//   iscan+8(FP)    *int16
//   qcoeff+16(FP)  *int16
//   dqcoeff+24(FP) *int16
//   count+32(FP)   int     // multiple of 8
//   roundAC+40(FP) int
//   quantAC+48(FP) int
//   deqAC+56(FP)   int
//   ret+64(FP)     int32
TEXT ·quantizeFPACNEON(SB), NOSPLIT, $0-68
	MOVD	coeff+0(FP), R0
	MOVD	iscan+8(FP), R1
	MOVD	qcoeff+16(FP), R2
	MOVD	dqcoeff+24(FP), R3
	MOVD	count+32(FP), R4
	MOVD	roundAC+40(FP), R5
	MOVD	quantAC+48(FP), R6
	MOVD	deqAC+56(FP), R7

	WORD	$0x4e020cb8	// dup v24.8h, w5    ; round
	WORD	$0x4e020cd9	// dup v25.8h, w6    ; quant
	WORD	$0x4e020cfa	// dup v26.8h, w7    ; dequant / threshold
	WORD	$0x6e3f1fff	// eor v31.16b, v31.16b, v31.16b ; eob max
	CBZ	R4, done

loop:
	WORD	$0x4cdf7400	// ld1 {v0.8h}, [x0], #16
	WORD	$0x4cdf7421	// ld1 {v1.8h}, [x1], #16
	WORD	$0x4e607802	// sqabs v2.8h, v0.8h
	WORD	$0x4e780c42	// sqadd v2.8h, v2.8h, v24.8h
	WORD	$0x4e7a3c43	// cmge v3.8h, v2.8h, v26.8h
	WORD	$0x4e79b444	// sqdmulh v4.8h, v2.8h, v25.8h
	WORD	$0x04f1f0484	// sshr v4.8h, v4.8h, #1
	WORD	$0x4e60a805	// cmlt v5.8h, v0.8h, #0
	WORD	$0x6e251c86	// eor v6.16b, v4.16b, v5.16b
	WORD	$0x6e6584c6	// sub v6.8h, v6.8h, v5.8h
	WORD	$0x4e231cc6	// and v6.16b, v6.16b, v3.16b
	WORD	$0x4c9f7446	// st1 {v6.8h}, [x2], #16
	WORD	$0x4e7a9cc7	// mul v7.8h, v6.8h, v26.8h
	WORD	$0x4c9f7467	// st1 {v7.8h}, [x3], #16
	WORD	$0x4e668cc8	// cmtst v8.8h, v6.8h, v6.8h
	WORD	$0x4e211d08	// and v8.16b, v8.16b, v1.16b
	WORD	$0x6e6867ff	// umax v31.8h, v31.8h, v8.8h
	SUB	$8, R4
	CBNZ	R4, loop

done:
	WORD	$0x6e70abfe	// umaxv h30, v31.8h
	WORD	$0x1e2603c0	// fmov w0, s30
	MOVW	R0, ret+64(FP)
	RET
