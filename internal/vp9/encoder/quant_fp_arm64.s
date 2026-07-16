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

// quantizeFPFullTokenNEON ABI ($0-100):
//   coeff+0(FP)    *int16  // full raster block including DC
//   iscan+8(FP)    *int16
//   qcoeff+16(FP)  *int16
//   dqcoeff+24(FP) *int16
//   classes+32(FP) *uint8  // OUT per-raster-position token energy class
//   count+40(FP)   int     // multiple of 8, >= 16
//   roundDC+48(FP) int
//   roundAC+56(FP) int
//   quantDC+64(FP) int
//   quantAC+72(FP) int
//   deqDC+80(FP)   int
//   deqAC+88(FP)   int
//   ret+96(FP)     int32
//
// quantizeFPFullNEON sibling that additionally produces the coefficient
// token classes inside the quantizer scan itself: for every raster
// position it stores vp9_pt_energy_class[token(|qcoeff|)] as one byte.
// The classifier is the saturating-index table lookup
// class = T[min(|q|, 15)] with T = {0,1,2,3,3,4,4,4,4,4,4,5,5,5,5,5},
// exactly the vp9_entropy.c:95 energy mapping of TokenForAbsCoeff:
// 0->0, 1->1, 2->2, 3..4->3, 5..10->4, >=11->5.
TEXT ·quantizeFPFullTokenNEON(SB), NOSPLIT, $0-100
	MOVD	coeff+0(FP), R0
	MOVD	iscan+8(FP), R1
	MOVD	qcoeff+16(FP), R2
	MOVD	dqcoeff+24(FP), R3
	MOVD	classes+32(FP), R11
	MOVD	count+40(FP), R4
	MOVD	roundDC+48(FP), R5
	MOVD	roundAC+56(FP), R6
	MOVD	quantDC+64(FP), R7
	MOVD	quantAC+72(FP), R8
	MOVD	deqDC+80(FP), R9
	MOVD	deqAC+88(FP), R10

	WORD	$0x4e020cd8 // dup v24.8h, w6    ; round (AC lanes)
	WORD	$0x4e021cb8 // ins v24.h[0], w5  ; round DC lane
	WORD	$0x4e020d19 // dup v25.8h, w8    ; quant (AC lanes)
	WORD	$0x4e021cf9 // ins v25.h[0], w7  ; quant DC lane
	WORD	$0x4e020d5a // dup v26.8h, w10   ; dequant / threshold (AC lanes)
	WORD	$0x4e021d3a // ins v26.h[0], w9  ; dequant DC lane
	WORD	$0x6f00e41f // movi v31.2d, #0   ; eob max

	// Token-class constants: v28 = 15 splat (index clamp), v29 = class table.
	MOVD	$15, R12
	WORD	$0x4e020d9c // dup v28.8h, w12
	MOVD	$0x0404040303020100, R12
	MOVD	$0x0505050505040404, R13
	WORD	$0x9e67019d // fmov d29, x12
	WORD	$0x4e181dbd // ins v29.d[1], x13

	// Process DC and the first seven AC coefficients.
	WORD	$0x4cdf7400 // ld1 {v0.8h}, [x0], #16
	WORD	$0x4cdf7421 // ld1 {v1.8h}, [x1], #16
	WORD	$0x4e607802 // sqabs v2.8h, v0.8h
	WORD	$0x4e780c42 // sqadd v2.8h, v2.8h, v24.8h
	WORD	$0x4e7a3c43 // cmge v3.8h, v2.8h, v26.8h
	WORD	$0x4e79b444 // sqdmulh v4.8h, v2.8h, v25.8h
	WORD	$0x4f1f0484 // sshr v4.8h, v4.8h, #1
	WORD	$0x4e60a805 // cmlt v5.8h, v0.8h, #0
	WORD	$0x6e251c86 // eor v6.16b, v4.16b, v5.16b
	WORD	$0x6e6584c6 // sub v6.8h, v6.8h, v5.8h
	WORD	$0x4e231cc6 // and v6.16b, v6.16b, v3.16b
	WORD	$0x4c9f7446 // st1 {v6.8h}, [x2], #16
	WORD	$0x4e7a9cc7 // mul v7.8h, v6.8h, v26.8h
	WORD	$0x4c9f7467 // st1 {v7.8h}, [x3], #16
	WORD	$0x4e668cc8 // cmtst v8.8h, v6.8h, v6.8h
	WORD	$0x4e211d08 // and v8.16b, v8.16b, v1.16b
	WORD	$0x6e6867ff // umax v31.8h, v31.8h, v8.8h
	WORD	$0x4e6078c9 // sqabs v9.8h, v6.8h
	WORD	$0x6e7c6d29 // umin v9.8h, v9.8h, v28.8h
	WORD	$0x0e212929 // xtn v9.8b, v9.8h
	WORD	$0x0e0903a9 // tbl v9.8b, {v29.16b}, v9.8b
	WORD	$0x0c9f7169 // st1 {v9.8b}, [x11], #8

	// update_fp_values: collapse the DC lanes to the AC constants.
	WORD	$0x4e060718 // dup v24.8h, v24.h[1]
	WORD	$0x4e060739 // dup v25.8h, v25.h[1]
	WORD	$0x4e06075a // dup v26.8h, v26.h[1]

	SUB	$8, R4
	CBZ	R4, token_done

token_loop:
	WORD	$0x4cdf7400 // ld1 {v0.8h}, [x0], #16
	WORD	$0x4cdf7421 // ld1 {v1.8h}, [x1], #16
	WORD	$0x4e607802 // sqabs v2.8h, v0.8h
	WORD	$0x4e780c42 // sqadd v2.8h, v2.8h, v24.8h
	WORD	$0x4e7a3c43 // cmge v3.8h, v2.8h, v26.8h
	WORD	$0x4e79b444 // sqdmulh v4.8h, v2.8h, v25.8h
	WORD	$0x4f1f0484 // sshr v4.8h, v4.8h, #1
	WORD	$0x4e60a805 // cmlt v5.8h, v0.8h, #0
	WORD	$0x6e251c86 // eor v6.16b, v4.16b, v5.16b
	WORD	$0x6e6584c6 // sub v6.8h, v6.8h, v5.8h
	WORD	$0x4e231cc6 // and v6.16b, v6.16b, v3.16b
	WORD	$0x4c9f7446 // st1 {v6.8h}, [x2], #16
	WORD	$0x4e7a9cc7 // mul v7.8h, v6.8h, v26.8h
	WORD	$0x4c9f7467 // st1 {v7.8h}, [x3], #16
	WORD	$0x4e668cc8 // cmtst v8.8h, v6.8h, v6.8h
	WORD	$0x4e211d08 // and v8.16b, v8.16b, v1.16b
	WORD	$0x6e6867ff // umax v31.8h, v31.8h, v8.8h
	WORD	$0x4e6078c9 // sqabs v9.8h, v6.8h
	WORD	$0x6e7c6d29 // umin v9.8h, v9.8h, v28.8h
	WORD	$0x0e212929 // xtn v9.8b, v9.8h
	WORD	$0x0e0903a9 // tbl v9.8b, {v29.16b}, v9.8b
	WORD	$0x0c9f7169 // st1 {v9.8b}, [x11], #8
	SUB	$8, R4
	CBNZ	R4, token_loop

token_done:
	WORD	$0x6e70abfe // umaxv h30, v31.8h
	WORD	$0x1e2603c0 // fmov w0, s30
	MOVW	R0, ret+96(FP)
	RET

// quantizeBACNEON ABI ($0-84):
//   coeff+0(FP)       *int16  // starts at AC coefficient 1
//   iscan+8(FP)       *int16
//   qcoeff+16(FP)     *int16
//   dqcoeff+24(FP)    *int16
//   count+32(FP)      int     // multiple of 8
//   zbinAC+40(FP)     int
//   roundAC+48(FP)    int
//   quantAC+56(FP)    int
//   quantShift+64(FP) int
//   deqAC+72(FP)      int
//   ret+80(FP)        int32
TEXT ·quantizeBACNEON(SB), NOSPLIT, $0-84
	MOVD	coeff+0(FP), R0
	MOVD	iscan+8(FP), R1
	MOVD	qcoeff+16(FP), R2
	MOVD	dqcoeff+24(FP), R3
	MOVD	count+32(FP), R4
	MOVD	zbinAC+40(FP), R5
	MOVD	roundAC+48(FP), R6
	MOVD	quantAC+56(FP), R7
	MOVD	quantShift+64(FP), R8
	MOVD	deqAC+72(FP), R9

	WORD	$0x4e020cb8	// dup v24.8h, w5    ; zbin
	WORD	$0x4e020cd9	// dup v25.8h, w6    ; round
	WORD	$0x4e020cfa	// dup v26.8h, w7    ; quant
	WORD	$0x4e020d1b	// dup v27.8h, w8    ; quant_shift
	WORD	$0x4e020d3c	// dup v28.8h, w9    ; dequant
	WORD	$0x6e3f1fff	// eor v31.16b, v31.16b, v31.16b ; eob max
	CBZ	R4, done_b

loop_b:
	WORD	$0x4cdf7400	// ld1 {v0.8h}, [x0], #16
	WORD	$0x4cdf7421	// ld1 {v1.8h}, [x1], #16
	WORD	$0x4e607802	// sqabs v2.8h, v0.8h
	WORD	$0x4e783c43	// cmge v3.8h, v2.8h, v24.8h
	WORD	$0x4e790c44	// sqadd v4.8h, v2.8h, v25.8h
	WORD	$0x4e7ab485	// sqdmulh v5.8h, v4.8h, v26.8h
	WORD	$0x4f1f04a5	// sshr v5.8h, v5.8h, #1
	WORD	$0x4e6484a5	// add v5.8h, v5.8h, v4.8h
	WORD	$0x4e7bb4a5	// sqdmulh v5.8h, v5.8h, v27.8h
	WORD	$0x4f1f04a5	// sshr v5.8h, v5.8h, #1
	WORD	$0x4e60a806	// cmlt v6.8h, v0.8h, #0
	WORD	$0x6e261ca7	// eor v7.16b, v5.16b, v6.16b
	WORD	$0x6e6684e7	// sub v7.8h, v7.8h, v6.8h
	WORD	$0x4e231ce7	// and v7.16b, v7.16b, v3.16b
	WORD	$0x4c9f7447	// st1 {v7.8h}, [x2], #16
	WORD	$0x4e7c9ce8	// mul v8.8h, v7.8h, v28.8h
	WORD	$0x4c9f7468	// st1 {v8.8h}, [x3], #16
	WORD	$0x4e678ce9	// cmtst v9.8h, v7.8h, v7.8h
	WORD	$0x4e211d29	// and v9.16b, v9.16b, v1.16b
	WORD	$0x6e6967ff	// umax v31.8h, v31.8h, v9.8h
	SUB	$8, R4
	CBNZ	R4, loop_b

done_b:
	WORD	$0x6e70abfe	// umaxv h30, v31.8h
	WORD	$0x1e2603c0	// fmov w0, s30
	MOVW	R0, ret+80(FP)
	RET

// quantizeFPFullNEON ABI ($0-92):
//   coeff+0(FP)    *int16  // full raster block including DC
//   iscan+8(FP)    *int16  // 1-based inverse scan (libvpx vp9_scan.c layout)
//   qcoeff+16(FP)  *int16
//   dqcoeff+24(FP) *int16
//   count+32(FP)   int     // multiple of 8, >= 16
//   roundDC+40(FP) int
//   roundAC+48(FP) int
//   quantDC+56(FP) int
//   quantAC+64(FP) int
//   deqDC+72(FP)   int
//   deqAC+80(FP)   int
//   ret+88(FP)     int32
//
// Full-block port of libvpx v1.16.0
// vp9/encoder/arm/neon/vp9_quantize_neon.c::vp9_quantize_fp_neon: the DC
// lane rides in lane 0 of the round/quant/dequant vectors for the first
// 8-coefficient group (load_fp_values), then the vectors collapse to the
// AC constants (update_fp_values) for the remaining groups. This removes
// the scalar DC prologue and 7-coefficient scalar tail the split AC kernel
// above requires.
TEXT ·quantizeFPFullNEON(SB), NOSPLIT, $0-92
	MOVD	coeff+0(FP), R0
	MOVD	iscan+8(FP), R1
	MOVD	qcoeff+16(FP), R2
	MOVD	dqcoeff+24(FP), R3
	MOVD	count+32(FP), R4
	MOVD	roundDC+40(FP), R5
	MOVD	roundAC+48(FP), R6
	MOVD	quantDC+56(FP), R7
	MOVD	quantAC+64(FP), R8
	MOVD	deqDC+72(FP), R9
	MOVD	deqAC+80(FP), R10

	WORD	$0x4e020cd8 // dup v24.8h, w6    ; round (AC lanes)
	WORD	$0x4e021cb8 // ins v24.h[0], w5  ; round DC lane
	WORD	$0x4e020d19 // dup v25.8h, w8    ; quant (AC lanes)
	WORD	$0x4e021cf9 // ins v25.h[0], w7  ; quant DC lane
	WORD	$0x4e020d5a // dup v26.8h, w10   ; dequant / threshold (AC lanes)
	WORD	$0x4e021d3a // ins v26.h[0], w9  ; dequant DC lane
	WORD	$0x6f00e41f // movi v31.2d, #0   ; eob max

	// Process DC and the first seven AC coefficients.
	WORD	$0x4cdf7400 // ld1 {v0.8h}, [x0], #16
	WORD	$0x4cdf7421 // ld1 {v1.8h}, [x1], #16
	WORD	$0x4e607802 // sqabs v2.8h, v0.8h
	WORD	$0x4e780c42 // sqadd v2.8h, v2.8h, v24.8h
	WORD	$0x4e7a3c43 // cmge v3.8h, v2.8h, v26.8h
	WORD	$0x4e79b444 // sqdmulh v4.8h, v2.8h, v25.8h
	WORD	$0x4f1f0484 // sshr v4.8h, v4.8h, #1
	WORD	$0x4e60a805 // cmlt v5.8h, v0.8h, #0
	WORD	$0x6e251c86 // eor v6.16b, v4.16b, v5.16b
	WORD	$0x6e6584c6 // sub v6.8h, v6.8h, v5.8h
	WORD	$0x4e231cc6 // and v6.16b, v6.16b, v3.16b
	WORD	$0x4c9f7446 // st1 {v6.8h}, [x2], #16
	WORD	$0x4e7a9cc7 // mul v7.8h, v6.8h, v26.8h
	WORD	$0x4c9f7467 // st1 {v7.8h}, [x3], #16
	WORD	$0x4e668cc8 // cmtst v8.8h, v6.8h, v6.8h
	WORD	$0x4e211d08 // and v8.16b, v8.16b, v1.16b
	WORD	$0x6e6867ff // umax v31.8h, v31.8h, v8.8h

	// update_fp_values: collapse the DC lanes to the AC constants.
	WORD	$0x4e060718 // dup v24.8h, v24.h[1]
	WORD	$0x4e060739 // dup v25.8h, v25.h[1]
	WORD	$0x4e06075a // dup v26.8h, v26.h[1]

	SUB	$8, R4
	CBZ	R4, full_done

full_loop:
	WORD	$0x4cdf7400 // ld1 {v0.8h}, [x0], #16
	WORD	$0x4cdf7421 // ld1 {v1.8h}, [x1], #16
	WORD	$0x4e607802 // sqabs v2.8h, v0.8h
	WORD	$0x4e780c42 // sqadd v2.8h, v2.8h, v24.8h
	WORD	$0x4e7a3c43 // cmge v3.8h, v2.8h, v26.8h
	WORD	$0x4e79b444 // sqdmulh v4.8h, v2.8h, v25.8h
	WORD	$0x4f1f0484 // sshr v4.8h, v4.8h, #1
	WORD	$0x4e60a805 // cmlt v5.8h, v0.8h, #0
	WORD	$0x6e251c86 // eor v6.16b, v4.16b, v5.16b
	WORD	$0x6e6584c6 // sub v6.8h, v6.8h, v5.8h
	WORD	$0x4e231cc6 // and v6.16b, v6.16b, v3.16b
	WORD	$0x4c9f7446 // st1 {v6.8h}, [x2], #16
	WORD	$0x4e7a9cc7 // mul v7.8h, v6.8h, v26.8h
	WORD	$0x4c9f7467 // st1 {v7.8h}, [x3], #16
	WORD	$0x4e668cc8 // cmtst v8.8h, v6.8h, v6.8h
	WORD	$0x4e211d08 // and v8.16b, v8.16b, v1.16b
	WORD	$0x6e6867ff // umax v31.8h, v31.8h, v8.8h
	SUB	$8, R4
	CBNZ	R4, full_loop

full_done:
	WORD	$0x6e70abfe // umaxv h30, v31.8h
	WORD	$0x1e2603c0 // fmov w0, s30
	MOVW	R0, ret+88(FP)
	RET
