//go:build arm64 && !purego

// VP9 ARMv8.6 i8mm (USDOT) 8-tap convolve kernels. Ported from libvpx
// v1.16.0 vpx_dsp/arm/vpx_convolve8_neon_i8mm.c (the 8-tap w>=8
// branches of convolve_8tap_horiz_neon_i8mm / convolve_8tap_vert_
// neon_i8mm).
//
// Each USDOT instruction computes four 4-tap dot products of unsigned
// source bytes against signed int8 filter taps, accumulating into
// int32 lanes. An 8-tap output is two chained USDOTs (low taps then
// high taps, selected via the 4B lane index on the filter register).
// The int32 sums are exact, so the final
//
//   out = clamp_uint8((sum + 64) >> 7)
//
// is realized as SHRN #1 (exact halving of the even/odd-safe sum:
// floor((sum>>1 + 32) / 64) == floor((sum + 64) / 128) for all
// integers) followed by SQRSHRUN #6, matching libvpx's
// vshrn_n_s32(sum, 1) + vqrshrun_n_s16(sum, FILTER_BITS - 1).
//
// Filter taps must fit int8 (the Go wrapper checks; all VP9 subpel
// kernels for fractions 1..15 do). The filter register V31 holds the
// 8 taps as int8 in its low 8 bytes; USDOT lane 0 covers taps 0-3 and
// lane 1 taps 4-7.
//
// WORD-encoded instructions (Go's arm64 assembler has no USDOT/TBL2/
// SHRN/SQRSHRUN spellings):
//   usdot vD.4s, vN.16b, v31.4b[0] -> 0x4f9ff000 | N<<5 | D
//   usdot vD.4s, vN.16b, v31.4b[1] -> 0x4fbff000 | N<<5 | D
//   tbl   vD.16b, {vN.16b}, vM.16b -> 0x4e000000 | M<<16 | N<<5 | D
//   tbl   vD.16b, {vN.16b, vN+1.16b}, vM.16b
//                                  -> 0x4e002000 | M<<16 | N<<5 | D
//   shrn  vD.4h, vN.4s, #1         -> 0x0f1f8400 | N<<5 | D
//   shrn2 vD.8h, vN.4s, #1         -> 0x4f1f8400 | N<<5 | D
//   sqrshrun vD.8b, vN.8h, #6      -> 0x2f0a8c00 | N<<5 | D

#include "textflag.h"

// dot_prod_permute_tbl from vpx_convolve8_neon_i8mm.c: byte shuffles
// producing the sliding 4-byte tap windows for USDOT.
DATA dotProdPermuteTbl<>+0(SB)/8, $0x0403020103020100
DATA dotProdPermuteTbl<>+8(SB)/8, $0x0605040305040302
DATA dotProdPermuteTbl<>+16(SB)/8, $0x0807060507060504
DATA dotProdPermuteTbl<>+24(SB)/8, $0x0a09080709080706
DATA dotProdPermuteTbl<>+32(SB)/8, $0x0c0b0a090b0a0908
DATA dotProdPermuteTbl<>+40(SB)/8, $0x0e0d0c0b0d0c0b0a
GLOBL dotProdPermuteTbl<>(SB), RODATA|NOPTR, $48

// dot_prod_merge_block_tbl: shift the transposed 4x4 sample blocks
// left by one/two/three columns, pulling new columns from the second
// table register.
DATA dotProdMergeBlockTbl<>+0(SB)/8, $0x1407060510030201
DATA dotProdMergeBlockTbl<>+8(SB)/8, $0x1c0f0e0d180b0a09
DATA dotProdMergeBlockTbl<>+16(SB)/8, $0x1514070611100302
DATA dotProdMergeBlockTbl<>+24(SB)/8, $0x1d1c0f0e19180b0a
DATA dotProdMergeBlockTbl<>+32(SB)/8, $0x1615140712111003
DATA dotProdMergeBlockTbl<>+40(SB)/8, $0x1e1d1c0f1a19180b
GLOBL dotProdMergeBlockTbl<>(SB), RODATA|NOPTR, $48

// convolveHoriz8I8MM ABI ($0-56): horizontal 8-tap convolve, w cols
// (multiple of 8), h rows (any h >= 1). src points to src - 3 (the
// kernel window base for output column 0). filter points to 8 int8
// taps.
//
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   dstStride+24(FP) int
//   filter+32(FP)    *int8 (8 taps, int8)
//   w+40(FP)         int
//   h+48(FP)         int
TEXT ·convolveHoriz8I8MM(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	dstStride+24(FP), R3
	MOVD	filter+32(FP), R4
	MOVD	w+40(FP), R5
	MOVD	h+48(FP), R6

	FMOVD	(R4), F31              // 8 int8 taps in V31 low half

	MOVD	$dotProdPermuteTbl<>(SB), R7
	VLD1	(R7), [V28.B16, V29.B16, V30.B16]

	// Persistent zero for accumulator seeding. VMOV from a fixed zero
	// register is rename-eliminated and dependency-free; EOR v,v,v is
	// NOT a zero idiom on ARM cores and would serialize iterations on
	// the accumulator's previous value.
	VEOR	V26.B16, V26.B16, V26.B16

hi_rowLoop:
	MOVD	R0, R7      // src cursor for this row
	MOVD	R2, R8      // dst cursor for this row
	MOVD	R5, R10     // columns left this row

hi_colLoop:
	VLD1	(R7), [V0.B16]

	// Permute the 16 source bytes into the three sliding 4-byte
	// window layouts.
	WORD	$0x4e1c0001    // tbl v1.16b, {v0.16b}, v28.16b
	WORD	$0x4e1d0002    // tbl v2.16b, {v0.16b}, v29.16b
	WORD	$0x4e1e0003    // tbl v3.16b, {v0.16b}, v30.16b

	// First 4 outputs: sum0 = usdot(perm0, taps0-3) + usdot(perm1, taps4-7).
	VMOV	V26.B16, V4.B16
	WORD	$0x4f9ff024    // usdot v4.4s, v1.16b, v31.4b[0]
	WORD	$0x4fbff044    // usdot v4.4s, v2.16b, v31.4b[1]

	// Second 4 outputs.
	VMOV	V26.B16, V5.B16
	WORD	$0x4f9ff045    // usdot v5.4s, v2.16b, v31.4b[0]
	WORD	$0x4fbff065    // usdot v5.4s, v3.16b, v31.4b[1]

	// Narrow and round: (sum >> 1) then rounding shift by 6.
	WORD	$0x0f1f8484    // shrn  v4.4h, v4.4s, #1
	WORD	$0x4f1f84a4    // shrn2 v4.8h, v5.4s, #1
	WORD	$0x2f0a8c84    // sqrshrun v4.8b, v4.8h, #6

	FMOVD	F4, (R8)

	ADD	$8, R7, R7
	ADD	$8, R8, R8
	SUB	$8, R10, R10
	CBNZ	R10, hi_colLoop

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, hi_rowLoop

	RET

// convolveVert8I8MM ABI ($0-56): vertical 8-tap convolve, w cols
// (multiple of 8), h rows (multiple of 4). src points to row 0 of the
// tap window (caller subtracted 3*srcStride). filter points to 8 int8
// taps.
//
// Register bank while looping over one 8-column band:
//   V0..V3   s0123_lo, s1234_lo, s2345_lo, s3456_lo
//   V4       s78910_lo   (pair {V3,V4} feeds TBL2)
//   V5..V8   s0123_hi, s1234_hi, s2345_hi, s3456_hi
//   V9       s78910_hi   (pair {V8,V9} feeds TBL2)
//   V10..V12 s4567_lo, s5678_lo, s6789_lo
//   V13..V15 s4567_hi, s5678_hi, s6789_hi
//   V16,V17  int32 sums / narrowed output
//   V20..V23 row loads
//   V24,V25  transpose temps
//   V26      zero
//   V28..V30 merge tables
//   V31      filter taps
//
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   dstStride+24(FP) int
//   filter+32(FP)    *int8 (8 taps, int8)
//   w+40(FP)         int
//   h+48(FP)         int
TEXT ·convolveVert8I8MM(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	dstStride+24(FP), R3
	MOVD	filter+32(FP), R4
	MOVD	w+40(FP), R5
	MOVD	h+48(FP), R6

	FMOVD	(R4), F31

	MOVD	$dotProdMergeBlockTbl<>(SB), R7
	VLD1	(R7), [V28.B16, V29.B16, V30.B16]

	VEOR	V26.B16, V26.B16, V26.B16

vi_colBand:
	MOVD	R0, R7      // src cursor for this band
	MOVD	R2, R8      // dst cursor for this band
	MOVD	R6, R9      // remaining rows

	// Load 7 source rows s0..s6 into V16..V22 (low 8 bytes each).
	FMOVD	(R7), F16
	ADD	R1, R7, R7
	FMOVD	(R7), F17
	ADD	R1, R7, R7
	FMOVD	(R7), F18
	ADD	R1, R7, R7
	FMOVD	(R7), F19
	ADD	R1, R7, R7
	FMOVD	(R7), F20
	ADD	R1, R7, R7
	FMOVD	(R7), F21
	ADD	R1, R7, R7
	FMOVD	(R7), F22
	ADD	R1, R7, R7

	// transpose_concat_u8_8x4(s0,s1,s2,s3) -> V0 (lo), V5 (hi)
	VZIP1	V18.B16, V16.B16, V24.B16   // a02 = zip1(s0, s2)
	VZIP1	V19.B16, V17.B16, V25.B16   // a13 = zip1(s1, s3)
	VZIP1	V25.B16, V24.B16, V0.B16    // lo = zip1(a02, a13)
	VZIP2	V25.B16, V24.B16, V5.B16    // hi = zip2(a02, a13)

	// transpose_concat_u8_8x4(s1,s2,s3,s4) -> V1, V6
	VZIP1	V19.B16, V17.B16, V24.B16
	VZIP1	V20.B16, V18.B16, V25.B16
	VZIP1	V25.B16, V24.B16, V1.B16
	VZIP2	V25.B16, V24.B16, V6.B16

	// transpose_concat_u8_8x4(s2,s3,s4,s5) -> V2, V7
	VZIP1	V20.B16, V18.B16, V24.B16
	VZIP1	V21.B16, V19.B16, V25.B16
	VZIP1	V25.B16, V24.B16, V2.B16
	VZIP2	V25.B16, V24.B16, V7.B16

	// transpose_concat_u8_8x4(s3,s4,s5,s6) -> V3, V8
	VZIP1	V21.B16, V19.B16, V24.B16
	VZIP1	V22.B16, V20.B16, V25.B16
	VZIP1	V25.B16, V24.B16, V3.B16
	VZIP2	V25.B16, V24.B16, V8.B16

vi_rowLoop:
	// Load rows s7..s10.
	FMOVD	(R7), F20
	ADD	R1, R7, R7
	FMOVD	(R7), F21
	ADD	R1, R7, R7
	FMOVD	(R7), F22
	ADD	R1, R7, R7
	FMOVD	(R7), F23
	ADD	R1, R7, R7

	// transpose_concat_u8_8x4(s7,s8,s9,s10) -> V4 (lo), V9 (hi)
	VZIP1	V22.B16, V20.B16, V24.B16
	VZIP1	V23.B16, V21.B16, V25.B16
	VZIP1	V25.B16, V24.B16, V4.B16
	VZIP2	V25.B16, V24.B16, V9.B16

	// Merge new columns into the previous transposed blocks.
	WORD	$0x4e1c206a    // tbl v10.16b, {v3.16b, v4.16b}, v28.16b
	WORD	$0x4e1d206b    // tbl v11.16b, {v3.16b, v4.16b}, v29.16b
	WORD	$0x4e1e206c    // tbl v12.16b, {v3.16b, v4.16b}, v30.16b
	WORD	$0x4e1c210d    // tbl v13.16b, {v8.16b, v9.16b}, v28.16b
	WORD	$0x4e1d210e    // tbl v14.16b, {v8.16b, v9.16b}, v29.16b
	WORD	$0x4e1e210f    // tbl v15.16b, {v8.16b, v9.16b}, v30.16b

	// d0 = convolve8_8_v(s0123, s4567)
	VMOV	V26.B16, V16.B16
	WORD	$0x4f9ff010    // usdot v16.4s, v0.16b, v31.4b[0]
	WORD	$0x4fbff150    // usdot v16.4s, v10.16b, v31.4b[1]
	VMOV	V26.B16, V17.B16
	WORD	$0x4f9ff0b1    // usdot v17.4s, v5.16b, v31.4b[0]
	WORD	$0x4fbff1b1    // usdot v17.4s, v13.16b, v31.4b[1]
	WORD	$0x0f1f8610    // shrn  v16.4h, v16.4s, #1
	WORD	$0x4f1f8630    // shrn2 v16.8h, v17.4s, #1
	WORD	$0x2f0a8e10    // sqrshrun v16.8b, v16.8h, #6
	FMOVD	F16, (R8)
	ADD	R3, R8, R8

	// d1 = convolve8_8_v(s1234, s5678)
	VMOV	V26.B16, V16.B16
	WORD	$0x4f9ff030    // usdot v16.4s, v1.16b, v31.4b[0]
	WORD	$0x4fbff170    // usdot v16.4s, v11.16b, v31.4b[1]
	VMOV	V26.B16, V17.B16
	WORD	$0x4f9ff0d1    // usdot v17.4s, v6.16b, v31.4b[0]
	WORD	$0x4fbff1d1    // usdot v17.4s, v14.16b, v31.4b[1]
	WORD	$0x0f1f8610    // shrn  v16.4h, v16.4s, #1
	WORD	$0x4f1f8630    // shrn2 v16.8h, v17.4s, #1
	WORD	$0x2f0a8e10    // sqrshrun v16.8b, v16.8h, #6
	FMOVD	F16, (R8)
	ADD	R3, R8, R8

	// d2 = convolve8_8_v(s2345, s6789)
	VMOV	V26.B16, V16.B16
	WORD	$0x4f9ff050    // usdot v16.4s, v2.16b, v31.4b[0]
	WORD	$0x4fbff190    // usdot v16.4s, v12.16b, v31.4b[1]
	VMOV	V26.B16, V17.B16
	WORD	$0x4f9ff0f1    // usdot v17.4s, v7.16b, v31.4b[0]
	WORD	$0x4fbff1f1    // usdot v17.4s, v15.16b, v31.4b[1]
	WORD	$0x0f1f8610    // shrn  v16.4h, v16.4s, #1
	WORD	$0x4f1f8630    // shrn2 v16.8h, v17.4s, #1
	WORD	$0x2f0a8e10    // sqrshrun v16.8b, v16.8h, #6
	FMOVD	F16, (R8)
	ADD	R3, R8, R8

	// d3 = convolve8_8_v(s3456, s78910)
	VMOV	V26.B16, V16.B16
	WORD	$0x4f9ff070    // usdot v16.4s, v3.16b, v31.4b[0]
	WORD	$0x4fbff090    // usdot v16.4s, v4.16b, v31.4b[1]
	VMOV	V26.B16, V17.B16
	WORD	$0x4f9ff111    // usdot v17.4s, v8.16b, v31.4b[0]
	WORD	$0x4fbff131    // usdot v17.4s, v9.16b, v31.4b[1]
	WORD	$0x0f1f8610    // shrn  v16.4h, v16.4s, #1
	WORD	$0x4f1f8630    // shrn2 v16.8h, v17.4s, #1
	WORD	$0x2f0a8e10    // sqrshrun v16.8b, v16.8h, #6
	FMOVD	F16, (R8)
	ADD	R3, R8, R8

	// Shuffle the transposed blocks up four rows.
	VMOV	V10.B16, V0.B16
	VMOV	V11.B16, V1.B16
	VMOV	V12.B16, V2.B16
	VMOV	V4.B16, V3.B16
	VMOV	V13.B16, V5.B16
	VMOV	V14.B16, V6.B16
	VMOV	V15.B16, V7.B16
	VMOV	V9.B16, V8.B16

	SUB	$4, R9, R9
	CBNZ	R9, vi_rowLoop

	// Next 8-column band.
	ADD	$8, R0, R0
	ADD	$8, R2, R2
	SUB	$8, R5, R5
	CBNZ	R5, vi_colBand

	RET
