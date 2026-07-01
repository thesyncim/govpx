//go:build arm64 && !purego

// ARMv8 NEON 8x16 six-tap subpel prediction. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c (8x16 path). Two passes:
//
//   horizontal: for y in [0, 21), x in [0, 8):
//     v = sum_{k=0..5} src[y*srcStride+x+k] * hFilter[k] + 64
//     tmp[y*8+x] = clip255(v >> 7)
//
//   vertical: for y in [0, 16), x in [0, 8):
//     v = sum_{k=0..5} tmp[(y+k)*8+x] * vFilter[k] + 64
//     dst[y*dstStride+x] = clip255(v >> 7)
//
// Same libvpx arithmetic as subpixel_arm64.s (16x16 path): |tap|
// broadcasts as uint8 lanes, UMULL/UMLAL/UMLSL modular uint16
// accumulation with the center tap 3 kept separate, SQADD saturating
// combine, SQRSHRUN round/narrow to uint8. Bit-identical to the
// scalar reference (see the 16x16 header for the argument).
//
// Plan9 VEXT operand order: VEXT $imm, V_a, V_b, V_d encodes
// V_a -> Rm (high) and V_b -> Rn (low). For "bytes [imm..imm+7] of
// (V0:V1)" the form is VEXT $imm, V1, V0, V_d.
//
// Calling convention (ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[21*8]byte
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

// ABS8X16 loads the int16 tap at off(Rf) sign-extended and leaves
// |tap| in R7 for the following dup-to-uint8 WORD.
#define ABS8X16(off, Rf) \
	MOVH	off(Rf), R7 \
	ASR	$63, R7, R10 \
	EOR	R10, R7, R7 \
	SUB	R10, R7, R7

TEXT ·sixTapPredict8x16NEON(SB), NOSPLIT, $0-56
	MOVD	dst+0(FP), R0
	MOVD	dstStride+8(FP), R1
	MOVD	src+16(FP), R2
	MOVD	srcStride+24(FP), R3
	MOVD	hFilter+32(FP), R4
	MOVD	vFilter+40(FP), R5
	MOVD	tmp+48(FP), R6

	ABS8X16(0, R4)
	WORD	$0x4e010cf0	// dup v16.16b, w7
	ABS8X16(2, R4)
	WORD	$0x4e010cf1	// dup v17.16b, w7
	ABS8X16(4, R4)
	WORD	$0x4e010cf2	// dup v18.16b, w7
	ABS8X16(6, R4)
	WORD	$0x4e010cf3	// dup v19.16b, w7
	ABS8X16(8, R4)
	WORD	$0x4e010cf4	// dup v20.16b, w7
	ABS8X16(10, R4)
	WORD	$0x4e010cf5	// dup v21.16b, w7

	ABS8X16(0, R5)
	WORD	$0x4e010cf6	// dup v22.16b, w7
	ABS8X16(2, R5)
	WORD	$0x4e010cf7	// dup v23.16b, w7
	ABS8X16(4, R5)
	WORD	$0x4e010cf8	// dup v24.16b, w7
	ABS8X16(6, R5)
	WORD	$0x4e010cf9	// dup v25.16b, w7
	ABS8X16(8, R5)
	WORD	$0x4e010cfa	// dup v26.16b, w7
	ABS8X16(10, R5)
	WORD	$0x4e010cfb	// dup v27.16b, w7

	// === Horizontal pass: 21 rows ===
	MOVD	$21, R8
	MOVD	R6, R9

horiz_loop:
	VLD1	(R2), [V0.B16, V1.B16]

	// Tap 0: bytes [0..7]; non-negative.
	WORD	$0x2e30c00a	// umull v10.8h, v0.8b, v16.8b

	// Tap 1: bytes [1..8]; non-positive.
	VEXT	$1, V1.B16, V0.B16, V2.B16
	WORD	$0x2e31a04a	// umlsl v10.8h, v2.8b, v17.8b

	// Tap 2: bytes [2..9]; non-negative.
	VEXT	$2, V1.B16, V0.B16, V2.B16
	WORD	$0x2e32804a	// umlal v10.8h, v2.8b, v18.8b

	// Tap 3 (center): bytes [3..10]; separate accumulator.
	VEXT	$3, V1.B16, V0.B16, V2.B16
	WORD	$0x2e33c04c	// umull v12.8h, v2.8b, v19.8b

	// Tap 4: bytes [4..11]; non-positive.
	VEXT	$4, V1.B16, V0.B16, V2.B16
	WORD	$0x2e34a04a	// umlsl v10.8h, v2.8b, v20.8b

	// Tap 5: bytes [5..12]; non-negative.
	VEXT	$5, V1.B16, V0.B16, V2.B16
	WORD	$0x2e35804a	// umlal v10.8h, v2.8b, v21.8b

	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7

	VST1	[V8.B8], (R9)

	ADD	R3, R2, R2
	ADD	$8, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, horiz_loop

	// === Vertical pass: 16 rows ===
	MOVD	$16, R8
	MOVD	R6, R9

vert_loop:
	// Tap 0
	VLD1	(R9), [V0.B8]
	WORD	$0x2e36c00a	// umull v10.8h, v0.8b, v22.8b

	ADD	$8, R9, R7
	VLD1	(R7), [V0.B8]
	WORD	$0x2e37a00a	// umlsl v10.8h, v0.8b, v23.8b

	ADD	$16, R9, R7
	VLD1	(R7), [V0.B8]
	WORD	$0x2e38800a	// umlal v10.8h, v0.8b, v24.8b

	ADD	$24, R9, R7
	VLD1	(R7), [V0.B8]
	WORD	$0x2e39c00c	// umull v12.8h, v0.8b, v25.8b

	ADD	$32, R9, R7
	VLD1	(R7), [V0.B8]
	WORD	$0x2e3aa00a	// umlsl v10.8h, v0.8b, v26.8b

	ADD	$40, R9, R7
	VLD1	(R7), [V0.B8]
	WORD	$0x2e3b800a	// umlal v10.8h, v0.8b, v27.8b

	WORD	$0x4e6c0d4a	// sqadd v10.8h, v10.8h, v12.8h
	WORD	$0x2f098d48	// sqrshrun v8.8b, v10.8h, #7

	VST1	[V8.B8], (R0)

	ADD	R1, R0, R0
	ADD	$8, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, vert_loop

	RET
