//go:build arm64 && !purego

// VP9 ARMv8 NEON 8-tap convolve kernels. Mirrors libvpx v1.16.0
// vpx_dsp/arm/vpx_convolve8_neon.c (the h>=8 width>=8 branches).
//
// Each output pixel is a dot product of 8 source pixels with the 8-tap
// int16 subpel filter:
//
//   acc = sum_{k=0..7} src[k] * filter[k]
//   out = clamp_uint8((acc + 64) >> 7)
//
// Filter values are int16 with up to 4 negative taps. Pixels are
// widened to int16, multiplied/accumulated with int16 saturating
// arithmetic (matching libvpx convolve8_8 -- the two biggest taps
// 3 and 4 are added with SQADD to bound partial-sum overflow). The
// final SQRSHRUN narrows int16 -> uint8 with rounding + saturation.
//
// Filter taps are broadcast into V16..V23 (one tap per register, H8
// lane fan-out) so we can use plain three-same MUL/MLA on int16x8
// without needing lane-form helpers (which Go's arm64 assembler does
// not expose).
//
// For widths > 8 we loop over 8-column chunks per row. For horizontal
// the 16-byte VLD1 load gives us 16 contiguous bytes (we use VEXT $k$
// for the kth tap window), so the read window per row is
// [src - 3, src - 3 + w + 8).
//
// Three-same int16x8 (Q=1 U=0 size=01) encoding:
//   base = 0x4e600000; OR (Rm<<16) | (op6<<10) | (Rn<<5) | Rd
//   op6 = (opcode5 << 1) | 1:
//     MUL    -> op6 = 0x27  (opcode5 = 10011)
//     MLA    -> op6 = 0x25  (opcode5 = 10010)
//     SQADD  -> op6 = 0x03  (opcode5 = 00001)

#include "textflag.h"

// convolveVert8wNEON ABI ($0-56): vertical 8-tap convolve, w cols
// (multiple of 8), h rows. src points to row 0 of the tap window
// (caller subtracted 3*srcStride for the centered convolve).
//
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   dstStride+24(FP) int
//   filter+32(FP)    *int16  (8 taps)
//   w+40(FP)         int
//   h+48(FP)         int
TEXT ·convolveVert8wNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	dstStride+24(FP), R3
	MOVD	filter+32(FP), R4
	MOVD	w+40(FP), R5
	MOVD	h+48(FP), R6

	VLD1	(R4), [V25.H8]
	VDUP	V25.H[0], V16.H8
	VDUP	V25.H[1], V17.H8
	VDUP	V25.H[2], V18.H8
	VDUP	V25.H[3], V19.H8
	VDUP	V25.H[4], V20.H8
	VDUP	V25.H[5], V21.H8
	VDUP	V25.H[6], V22.H8
	VDUP	V25.H[7], V23.H8

v_colBand:
	MOVD	R0, R7         // row src cursor for this column band
	MOVD	R2, R8         // row dst cursor
	MOVD	R6, R9         // remaining rows

	// Pre-load 7 rows, widened to int16x8 in V0..V6.
	FMOVD	(R7), F0
	WORD	$0x2f08a400    // ushll v0.8h, v0.8b, #0
	ADD	R1, R7, R7
	FMOVD	(R7), F1
	WORD	$0x2f08a421    // ushll v1.8h, v1.8b, #0
	ADD	R1, R7, R7
	FMOVD	(R7), F2
	WORD	$0x2f08a442    // ushll v2.8h, v2.8b, #0
	ADD	R1, R7, R7
	FMOVD	(R7), F3
	WORD	$0x2f08a463    // ushll v3.8h, v3.8b, #0
	ADD	R1, R7, R7
	FMOVD	(R7), F4
	WORD	$0x2f08a484    // ushll v4.8h, v4.8b, #0
	ADD	R1, R7, R7
	FMOVD	(R7), F5
	WORD	$0x2f08a4a5    // ushll v5.8h, v5.8b, #0
	ADD	R1, R7, R7
	FMOVD	(R7), F6
	WORD	$0x2f08a4c6    // ushll v6.8h, v6.8b, #0
	ADD	R1, R7, R7

v_rowLoop:
	// Load next row -> V7 widened.
	FMOVD	(R7), F7
	WORD	$0x2f08a4e7    // ushll v7.8h, v7.8b, #0

	// acc(V24) = V0 * V16
	WORD	$0x4e709c18    // mul v24.8h, v0.8h, v16.8h
	// acc += V1 * V17
	WORD	$0x4e719438    // mla v24.8h, v1.8h, v17.8h
	// acc += V2 * V18
	WORD	$0x4e729458    // mla v24.8h, v2.8h, v18.8h
	// acc += V5 * V21
	WORD	$0x4e7594b8    // mla v24.8h, v5.8h, v21.8h
	// acc += V6 * V22
	WORD	$0x4e7694d8    // mla v24.8h, v6.8h, v22.8h
	// acc += V7 * V23
	WORD	$0x4e7794f8    // mla v24.8h, v7.8h, v23.8h
	// tmp(V26) = V3 * V19; acc = sqadd(acc, tmp)
	WORD	$0x4e739c7a    // mul v26.8h, v3.8h, v19.8h
	WORD	$0x4e7a0f18    // sqadd v24.8h, v24.8h, v26.8h
	// tmp(V26) = V4 * V20; acc = sqadd(acc, tmp)
	WORD	$0x4e749c9a    // mul v26.8h, v4.8h, v20.8h
	WORD	$0x4e7a0f18    // sqadd v24.8h, v24.8h, v26.8h

	// out = sqrshrun(V24.8h, #7) -> V24.8b
	WORD	$0x2f098f18    // sqrshrun v24.8b, v24.8h, #7

	FMOVD	F24, (R8)

	// Slide window down by one row: V0..V5 = V1..V6, V6 = V7.
	VMOV	V1.B16, V0.B16
	VMOV	V2.B16, V1.B16
	VMOV	V3.B16, V2.B16
	VMOV	V4.B16, V3.B16
	VMOV	V5.B16, V4.B16
	VMOV	V6.B16, V5.B16
	VMOV	V7.B16, V6.B16

	ADD	R1, R7, R7
	ADD	R3, R8, R8
	SUB	$1, R9, R9
	CBNZ	R9, v_rowLoop

	// Next 8-column band.
	ADD	$8, R0, R0
	ADD	$8, R2, R2
	SUB	$8, R5, R5
	CBNZ	R5, v_colBand

	RET

// convolveHoriz8wNEON ABI ($0-56): horizontal 8-tap convolve, w cols
// (multiple of 8), h rows. src points to src - 3 (caller adjusted so
// src[0..7] is the kernel window for output column 0).
//
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   dstStride+24(FP) int
//   filter+32(FP)    *int16
//   w+40(FP)         int
//   h+48(FP)         int
TEXT ·convolveHoriz8wNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	dstStride+24(FP), R3
	MOVD	filter+32(FP), R4
	MOVD	w+40(FP), R5
	MOVD	h+48(FP), R6

	VLD1	(R4), [V25.H8]
	VDUP	V25.H[0], V16.H8
	VDUP	V25.H[1], V17.H8
	VDUP	V25.H[2], V18.H8
	VDUP	V25.H[3], V19.H8
	VDUP	V25.H[4], V20.H8
	VDUP	V25.H[5], V21.H8
	VDUP	V25.H[6], V22.H8
	VDUP	V25.H[7], V23.H8

h_rowLoop:
	MOVD	R0, R7      // src cursor for this row
	MOVD	R2, R8      // dst cursor for this row
	MOVD	R5, R10     // columns left this row

h_colLoop:
	// Load 16 bytes (need w+8=16 for w=8; for w>8 we'll load again next iter).
	VLD1	(R7), [V0.B16, V1.B16]

	// Build 8 shifted byte windows via VEXT and widen each to int16x8:
	//   V2.B16 = bytes[1..16] -> V2.8h widened
	//   V3.B16 = bytes[2..17] -> V3.8h
	//   ...
	//   V7.B16 = bytes[6..21] -> V7.8h
	//   V14.B16 = bytes[7..22] -> V14.8h (only low 8 bytes matter)
	// V0 stays as the original first 16 bytes for tap 0.
	WORD	$0x2f08a40c    // ushll v12.8h, v0.8b,  #0       (tap0)
	VEXT	$1, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a44d    // ushll v13.8h, v2.8b,  #0       (tap1)
	VEXT	$2, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a44e    // ushll v14.8h, v2.8b,  #0       (tap2)
	VEXT	$3, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a44f    // ushll v15.8h, v2.8b,  #0       (tap3)
	VEXT	$4, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a44a    // ushll v10.8h, v2.8b,  #0       (tap4)
	VEXT	$5, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a44b    // ushll v11.8h, v2.8b,  #0       (tap5)
	VEXT	$6, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a449    // ushll v9.8h,  v2.8b,  #0       (tap6)
	VEXT	$7, V1.B16, V0.B16, V2.B16
	WORD	$0x2f08a448    // ushll v8.8h,  v2.8b,  #0       (tap7)

	// acc(V24) = tap0 * filt[0]  = V12 * V16
	WORD	$0x4e709d98    // mul   v24.8h, v12.8h, v16.8h
	// acc += tap1 * filt[1]  = V13 * V17
	WORD	$0x4e7195b8    // mla   v24.8h, v13.8h, v17.8h
	// acc += tap2 * filt[2]  = V14 * V18
	WORD	$0x4e7295d8    // mla   v24.8h, v14.8h, v18.8h
	// acc += tap5 * filt[5]  = V11 * V21
	WORD	$0x4e759578    // mla   v24.8h, v11.8h, v21.8h
	// acc += tap6 * filt[6]  = V9  * V22
	WORD	$0x4e769538    // mla   v24.8h, v9.8h,  v22.8h
	// acc += tap7 * filt[7]  = V8  * V23
	WORD	$0x4e779518    // mla   v24.8h, v8.8h,  v23.8h
	// tmp(V26) = tap3 * filt[3]  = V15 * V19;  acc = sqadd(acc, tmp)
	WORD	$0x4e739dfa    // mul   v26.8h, v15.8h, v19.8h
	WORD	$0x4e7a0f18    // sqadd v24.8h, v24.8h, v26.8h
	// tmp(V26) = tap4 * filt[4]  = V10 * V20;  acc = sqadd(acc, tmp)
	WORD	$0x4e749d5a    // mul   v26.8h, v10.8h, v20.8h
	WORD	$0x4e7a0f18    // sqadd v24.8h, v24.8h, v26.8h

	// out = sqrshrun(V24.8h, #7) -> V24.8b
	WORD	$0x2f098f18    // sqrshrun v24.8b, v24.8h, #7

	FMOVD	F24, (R8)

	ADD	$8, R7, R7
	ADD	$8, R8, R8
	SUB	$8, R10, R10
	CBNZ	R10, h_colLoop

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, h_rowLoop

	RET
