//go:build arm64 && !purego

// ARMv8 NEON 16x8 six-tap subpel prediction. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c (16x8 path). Two passes:
//
//   horizontal: for y in [0, 13), x in [0, 16):
//     v = sum_{k=0..5} src[y*srcStride+x+k] * hFilter[k] + 64
//     tmp[y*16+x] = clip255(v >> 7)
//
//   vertical: for y in [0, 8), x in [0, 16):
//     v = sum_{k=0..5} tmp[(y+k)*16+x] * vFilter[k] + 64
//     dst[y*dstStride+x] = clip255(v >> 7)
//
// Same libvpx arithmetic as subpixel_arm64.s (16x16 path): |tap|
// broadcasts as uint8 lanes, UMULL/UMLAL/UMLSL modular uint16
// accumulation with the center tap 3 kept separate, SQADD saturating
// combine, SQRSHRUN round/narrow to uint8. Bit-identical to the
// scalar reference (see the 16x16 header for the argument).
//
// Plan9 VEXT operand order: VEXT $imm, V_a, V_b, V_d encodes
// V_a -> Rm (high) and V_b -> Rn (low). For "bytes [imm..imm+15] of
// (V0:V1)" the form is VEXT $imm, V1, V0, V_d.
//
// Calling convention (ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[13*16]byte
//
// Registers:
//   R0=dst R1=dstStride R2=src R3=srcStride R4=hFilter R5=vFilter
//   R6=tmp R7,R8,R9,R10=loop scratch
//   V0,V1   src bytes (horizontal); per-row tmp slot (vertical)
//   V2      VEXT tap window (horizontal)
//   V10,V11 uint16x8 accumulators
//   V12,V13 center-tap uint16x8 products
//   V8      narrowed uint8 output
//   V16..V21 |hFilter[0..5]| broadcasts (uint8 lanes)
//   V22..V27 |vFilter[0..5]| broadcasts (uint8 lanes)

#include "textflag.h"

// ABS16X8 loads the int16 tap at off(Rf) sign-extended and leaves
// |tap| in R7 for the following dup-to-uint8 WORD.
#define ABS16X8(off, Rf) \
	MOVH	off(Rf), R7 \
	ASR	$63, R7, R10 \
	EOR	R10, R7, R7 \
	SUB	R10, R7, R7

TEXT ·sixTapPredict16x8NEON(SB), NOSPLIT, $0-56
	MOVD	dst+0(FP), R0
	MOVD	dstStride+8(FP), R1
	MOVD	src+16(FP), R2
	MOVD	srcStride+24(FP), R3
	MOVD	hFilter+32(FP), R4
	MOVD	vFilter+40(FP), R5
	MOVD	tmp+48(FP), R6

	ABS16X8(0, R4)
	WORD	$0x4e010cf0	// dup v16.16b, w7
	ABS16X8(2, R4)
	WORD	$0x4e010cf1	// dup v17.16b, w7
	ABS16X8(4, R4)
	WORD	$0x4e010cf2	// dup v18.16b, w7
	ABS16X8(6, R4)
	WORD	$0x4e010cf3	// dup v19.16b, w7
	ABS16X8(8, R4)
	WORD	$0x4e010cf4	// dup v20.16b, w7
	ABS16X8(10, R4)
	WORD	$0x4e010cf5	// dup v21.16b, w7

	ABS16X8(0, R5)
	WORD	$0x4e010cf6	// dup v22.16b, w7
	ABS16X8(2, R5)
	WORD	$0x4e010cf7	// dup v23.16b, w7
	ABS16X8(4, R5)
	WORD	$0x4e010cf8	// dup v24.16b, w7
	ABS16X8(6, R5)
	WORD	$0x4e010cf9	// dup v25.16b, w7
	ABS16X8(8, R5)
	WORD	$0x4e010cfa	// dup v26.16b, w7
	ABS16X8(10, R5)
	WORD	$0x4e010cfb	// dup v27.16b, w7

	// === Horizontal pass: 13 rows ===
	MOVD	$13, R8
	MOVD	R6, R9

horiz_loop:
	VLD1	(R2), [V0.B16, V1.B16]

	// Tap 0: bytes [0..15]; non-negative.
	WORD	$0x2e30c00a	// umull  v10.8h, v0.8b,  v16.8b
	WORD	$0x6e30c00b	// umull2 v11.8h, v0.16b, v16.16b

	// Tap 1: bytes [1..16]; non-positive.
	VEXT	$1, V1.B16, V0.B16, V2.B16
	WORD	$0x2e31a04a	// umlsl  v10.8h, v2.8b,  v17.8b
	WORD	$0x6e31a04b	// umlsl2 v11.8h, v2.16b, v17.16b

	// Tap 2: bytes [2..17]; non-negative.
	VEXT	$2, V1.B16, V0.B16, V2.B16
	WORD	$0x2e32804a	// umlal  v10.8h, v2.8b,  v18.8b
	WORD	$0x6e32804b	// umlal2 v11.8h, v2.16b, v18.16b

	// Tap 3 (center): bytes [3..18]; separate accumulator.
	VEXT	$3, V1.B16, V0.B16, V2.B16
	WORD	$0x2e33c04c	// umull  v12.8h, v2.8b,  v19.8b
	WORD	$0x6e33c04d	// umull2 v13.8h, v2.16b, v19.16b

	// Tap 4: bytes [4..19]; non-positive.
	VEXT	$4, V1.B16, V0.B16, V2.B16
	WORD	$0x2e34a04a	// umlsl  v10.8h, v2.8b,  v20.8b
	WORD	$0x6e34a04b	// umlsl2 v11.8h, v2.16b, v20.16b

	// Tap 5: bytes [5..20]; non-negative.
	VEXT	$5, V1.B16, V0.B16, V2.B16
	WORD	$0x2e35804a	// umlal  v10.8h, v2.8b,  v21.8b
	WORD	$0x6e35804b	// umlal2 v11.8h, v2.16b, v21.16b

	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun  v8.8b,  v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7

	VST1	[V8.B16], (R9)

	ADD	R3, R2, R2
	ADD	$16, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, horiz_loop

	// === Vertical pass: 8 rows, rolling 6-row window ===
	// libvpx keeps the second-pass window in registers, loading each
	// tmp row exactly once; the fully unrolled rotation avoids both
	// re-loads and register moves.
	VLD1	(R6), [V0.B16]
	ADD	$16, R6, R7
	VLD1	(R7), [V1.B16]
	ADD	$32, R6, R7
	VLD1	(R7), [V2.B16]
	ADD	$48, R6, R7
	VLD1	(R7), [V3.B16]
	ADD	$64, R6, R7
	VLD1	(R7), [V4.B16]

	// Output row 0: tmp rows 0..5
	ADD	$80, R6, R7
	VLD1	(R7), [V5.B16]
	WORD	$0x2e36c00a	// umull v10.8h, v0.8b, v22.8b
	WORD	$0x6e36c00b	// umull2 v11.8h, v0.16b, v22.16b
	WORD	$0x2e37a02a	// umlsl v10.8h, v1.8b, v23.8b
	WORD	$0x6e37a02b	// umlsl2 v11.8h, v1.16b, v23.16b
	WORD	$0x2e38804a	// umlal v10.8h, v2.8b, v24.8b
	WORD	$0x6e38804b	// umlal2 v11.8h, v2.16b, v24.16b
	WORD	$0x2e39c06c	// umull v12.8h, v3.8b, v25.8b
	WORD	$0x6e39c06d	// umull2 v13.8h, v3.16b, v25.16b
	WORD	$0x2e3aa08a	// umlsl v10.8h, v4.8b, v26.8b
	WORD	$0x6e3aa08b	// umlsl2 v11.8h, v4.16b, v26.16b
	WORD	$0x2e3b80aa	// umlal v10.8h, v5.8b, v27.8b
	WORD	$0x6e3b80ab	// umlal2 v11.8h, v5.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 1: tmp rows 1..6
	ADD	$96, R6, R7
	VLD1	(R7), [V0.B16]
	WORD	$0x2e36c02a	// umull v10.8h, v1.8b, v22.8b
	WORD	$0x6e36c02b	// umull2 v11.8h, v1.16b, v22.16b
	WORD	$0x2e37a04a	// umlsl v10.8h, v2.8b, v23.8b
	WORD	$0x6e37a04b	// umlsl2 v11.8h, v2.16b, v23.16b
	WORD	$0x2e38806a	// umlal v10.8h, v3.8b, v24.8b
	WORD	$0x6e38806b	// umlal2 v11.8h, v3.16b, v24.16b
	WORD	$0x2e39c08c	// umull v12.8h, v4.8b, v25.8b
	WORD	$0x6e39c08d	// umull2 v13.8h, v4.16b, v25.16b
	WORD	$0x2e3aa0aa	// umlsl v10.8h, v5.8b, v26.8b
	WORD	$0x6e3aa0ab	// umlsl2 v11.8h, v5.16b, v26.16b
	WORD	$0x2e3b800a	// umlal v10.8h, v0.8b, v27.8b
	WORD	$0x6e3b800b	// umlal2 v11.8h, v0.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 2: tmp rows 2..7
	ADD	$112, R6, R7
	VLD1	(R7), [V1.B16]
	WORD	$0x2e36c04a	// umull v10.8h, v2.8b, v22.8b
	WORD	$0x6e36c04b	// umull2 v11.8h, v2.16b, v22.16b
	WORD	$0x2e37a06a	// umlsl v10.8h, v3.8b, v23.8b
	WORD	$0x6e37a06b	// umlsl2 v11.8h, v3.16b, v23.16b
	WORD	$0x2e38808a	// umlal v10.8h, v4.8b, v24.8b
	WORD	$0x6e38808b	// umlal2 v11.8h, v4.16b, v24.16b
	WORD	$0x2e39c0ac	// umull v12.8h, v5.8b, v25.8b
	WORD	$0x6e39c0ad	// umull2 v13.8h, v5.16b, v25.16b
	WORD	$0x2e3aa00a	// umlsl v10.8h, v0.8b, v26.8b
	WORD	$0x6e3aa00b	// umlsl2 v11.8h, v0.16b, v26.16b
	WORD	$0x2e3b802a	// umlal v10.8h, v1.8b, v27.8b
	WORD	$0x6e3b802b	// umlal2 v11.8h, v1.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 3: tmp rows 3..8
	ADD	$128, R6, R7
	VLD1	(R7), [V2.B16]
	WORD	$0x2e36c06a	// umull v10.8h, v3.8b, v22.8b
	WORD	$0x6e36c06b	// umull2 v11.8h, v3.16b, v22.16b
	WORD	$0x2e37a08a	// umlsl v10.8h, v4.8b, v23.8b
	WORD	$0x6e37a08b	// umlsl2 v11.8h, v4.16b, v23.16b
	WORD	$0x2e3880aa	// umlal v10.8h, v5.8b, v24.8b
	WORD	$0x6e3880ab	// umlal2 v11.8h, v5.16b, v24.16b
	WORD	$0x2e39c00c	// umull v12.8h, v0.8b, v25.8b
	WORD	$0x6e39c00d	// umull2 v13.8h, v0.16b, v25.16b
	WORD	$0x2e3aa02a	// umlsl v10.8h, v1.8b, v26.8b
	WORD	$0x6e3aa02b	// umlsl2 v11.8h, v1.16b, v26.16b
	WORD	$0x2e3b804a	// umlal v10.8h, v2.8b, v27.8b
	WORD	$0x6e3b804b	// umlal2 v11.8h, v2.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 4: tmp rows 4..9
	ADD	$144, R6, R7
	VLD1	(R7), [V3.B16]
	WORD	$0x2e36c08a	// umull v10.8h, v4.8b, v22.8b
	WORD	$0x6e36c08b	// umull2 v11.8h, v4.16b, v22.16b
	WORD	$0x2e37a0aa	// umlsl v10.8h, v5.8b, v23.8b
	WORD	$0x6e37a0ab	// umlsl2 v11.8h, v5.16b, v23.16b
	WORD	$0x2e38800a	// umlal v10.8h, v0.8b, v24.8b
	WORD	$0x6e38800b	// umlal2 v11.8h, v0.16b, v24.16b
	WORD	$0x2e39c02c	// umull v12.8h, v1.8b, v25.8b
	WORD	$0x6e39c02d	// umull2 v13.8h, v1.16b, v25.16b
	WORD	$0x2e3aa04a	// umlsl v10.8h, v2.8b, v26.8b
	WORD	$0x6e3aa04b	// umlsl2 v11.8h, v2.16b, v26.16b
	WORD	$0x2e3b806a	// umlal v10.8h, v3.8b, v27.8b
	WORD	$0x6e3b806b	// umlal2 v11.8h, v3.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 5: tmp rows 5..10
	ADD	$160, R6, R7
	VLD1	(R7), [V4.B16]
	WORD	$0x2e36c0aa	// umull v10.8h, v5.8b, v22.8b
	WORD	$0x6e36c0ab	// umull2 v11.8h, v5.16b, v22.16b
	WORD	$0x2e37a00a	// umlsl v10.8h, v0.8b, v23.8b
	WORD	$0x6e37a00b	// umlsl2 v11.8h, v0.16b, v23.16b
	WORD	$0x2e38802a	// umlal v10.8h, v1.8b, v24.8b
	WORD	$0x6e38802b	// umlal2 v11.8h, v1.16b, v24.16b
	WORD	$0x2e39c04c	// umull v12.8h, v2.8b, v25.8b
	WORD	$0x6e39c04d	// umull2 v13.8h, v2.16b, v25.16b
	WORD	$0x2e3aa06a	// umlsl v10.8h, v3.8b, v26.8b
	WORD	$0x6e3aa06b	// umlsl2 v11.8h, v3.16b, v26.16b
	WORD	$0x2e3b808a	// umlal v10.8h, v4.8b, v27.8b
	WORD	$0x6e3b808b	// umlal2 v11.8h, v4.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 6: tmp rows 6..11
	ADD	$176, R6, R7
	VLD1	(R7), [V5.B16]
	WORD	$0x2e36c00a	// umull v10.8h, v0.8b, v22.8b
	WORD	$0x6e36c00b	// umull2 v11.8h, v0.16b, v22.16b
	WORD	$0x2e37a02a	// umlsl v10.8h, v1.8b, v23.8b
	WORD	$0x6e37a02b	// umlsl2 v11.8h, v1.16b, v23.16b
	WORD	$0x2e38804a	// umlal v10.8h, v2.8b, v24.8b
	WORD	$0x6e38804b	// umlal2 v11.8h, v2.16b, v24.16b
	WORD	$0x2e39c06c	// umull v12.8h, v3.8b, v25.8b
	WORD	$0x6e39c06d	// umull2 v13.8h, v3.16b, v25.16b
	WORD	$0x2e3aa08a	// umlsl v10.8h, v4.8b, v26.8b
	WORD	$0x6e3aa08b	// umlsl2 v11.8h, v4.16b, v26.16b
	WORD	$0x2e3b80aa	// umlal v10.8h, v5.8b, v27.8b
	WORD	$0x6e3b80ab	// umlal2 v11.8h, v5.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)
	ADD	R1, R0, R0

	// Output row 7: tmp rows 7..12
	ADD	$192, R6, R7
	VLD1	(R7), [V0.B16]
	WORD	$0x2e36c02a	// umull v10.8h, v1.8b, v22.8b
	WORD	$0x6e36c02b	// umull2 v11.8h, v1.16b, v22.16b
	WORD	$0x2e37a04a	// umlsl v10.8h, v2.8b, v23.8b
	WORD	$0x6e37a04b	// umlsl2 v11.8h, v2.16b, v23.16b
	WORD	$0x2e38806a	// umlal v10.8h, v3.8b, v24.8b
	WORD	$0x6e38806b	// umlal2 v11.8h, v3.16b, v24.16b
	WORD	$0x2e39c08c	// umull v12.8h, v4.8b, v25.8b
	WORD	$0x6e39c08d	// umull2 v13.8h, v4.16b, v25.16b
	WORD	$0x2e3aa0aa	// umlsl v10.8h, v5.8b, v26.8b
	WORD	$0x6e3aa0ab	// umlsl2 v11.8h, v5.16b, v26.16b
	WORD	$0x2e3b800a	// umlal v10.8h, v0.8b, v27.8b
	WORD	$0x6e3b800b	// umlal2 v11.8h, v0.16b, v27.16b
	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x4e6d0d6b	// sqadd v11.8h, v11.8h, v13.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7
	WORD	$0x6f098d68	// sqrshrun2 v8.16b, v11.8h, #7
	VST1	[V8.B16], (R0)

	RET
