//go:build arm64 && !purego

// ARMv8 NEON residual gather kernels. Mirrors libvpx v1.16.0
// vp8/encoder/encodemb.c's vp8_subtract_mby / vp8_subtract_mbuv:
//
//   for each row r in 0..H-1:
//     for each col c in 0..W-1:
//       residual[r][c] = src[r][c] - pred[r][c]      // int16 in [-255,255]
//
// The govpx convention writes the residuals as 16 contiguous int16-per-block
// slabs in scan order (block 0 first, block 15 last for Y; block 0..3 for UV),
// each block laid out row-major at stride 4. This matches the scalar
// gatherMacroblockYResiduals4x4Unchecked / gatherMacroblockUVResiduals4x4Unchecked
// helpers and feeds the encoder's transform-quantize pipeline.
//
// Per row, the kernels VLD1 W src + W pred bytes, USUBL/USUBL2 widen the
// byte differences to int16, then VST1 the 64-bit lane halves to the four
// (Y) / two (UV) destination 4x4 blocks of the current block-row.
//
// USUBL/USUBL2 aren't natively in Go's arm64 assembler so they're emitted as
// raw WORD directives; encodings come from clang.

#include "textflag.h"

// residualGather16x16NEON ABI ($0-40):
//   src+0(FP)         *byte
//   srcStride+8(FP)   int
//   pred+16(FP)       *byte
//   predStride+24(FP) int
//   out+32(FP)        *int16  (must point to >= 16*16*2 bytes)
//
// Output layout: 16 contiguous 4x4 int16 blocks in scan order, each block
// row-major at stride 4 int16s (8 bytes). Block b starts at out + b*32.

TEXT ·residualGather16x16NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	out+32(FP), R4

	// R7 walks the output cursor. R5 = block-row counter (4 outer iters).
	MOVD	R4, R7
	MOVD	$4, R5

br_loop_y:
	// R6 = row-in-block counter (4 inner iters covering 4 src rows).
	MOVD	$4, R6

row_loop_y:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]

	// V2.8H = (uint16)src[0..7] - (uint16)pred[0..7] (int16 lanes in [-255,255]).
	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b
	// V3.8H = (uint16)src[8..15] - (uint16)pred[8..15].
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	// Scatter the four 64-bit halves to the four destination blocks of
	// this block-row. R7 is the current block-row base + row offset.
	VST1	V2.D[0], (R7)
	ADD	$32, R7, R8
	VST1	V2.D[1], (R8)
	ADD	$64, R7, R8
	VST1	V3.D[0], (R8)
	ADD	$96, R7, R8
	VST1	V3.D[1], (R8)

	// Advance src/pred by their strides and out cursor by 8 (next row
	// within the same set of 4 blocks).
	ADD	$8, R7, R7
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, row_loop_y

	// Inner loop wrote 4 rows = +32 to R7; jump 96 more to land on the
	// next block-row (block-row stride = 4*32 = 128 bytes).
	ADD	$96, R7, R7
	SUB	$1, R5, R5
	CBNZ	R5, br_loop_y
	RET

// residualGather8x8NEON ABI ($0-40):
//   src+0(FP)         *byte
//   srcStride+8(FP)   int
//   pred+16(FP)       *byte
//   predStride+24(FP) int
//   out+32(FP)        *int16  (must point to >= 4*16*2 bytes)
//
// Output: 4 contiguous 4x4 int16 blocks (2x2 grid in scan order) at out + b*32.

TEXT ·residualGather8x8NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	out+32(FP), R4

	MOVD	R4, R7
	MOVD	$2, R5		// block-row counter (2 outer iters)

br_loop_uv:
	MOVD	$4, R6		// row-in-block counter

row_loop_uv:
	// 8-byte loads via FMOVD (low half of V0/V1).
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	// V2.8H = (uint16)src[0..7] - (uint16)pred[0..7].
	WORD	$0x2e212002	// usubl v2.8h, v0.8b, v1.8b

	// Two blocks per row-row.
	VST1	V2.D[0], (R7)
	ADD	$32, R7, R8
	VST1	V2.D[1], (R8)

	ADD	$8, R7, R7
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, row_loop_uv

	// Inner loop wrote 4 rows = +32 to R7; jump 32 more (block-row stride
	// = 2*32 = 64 bytes) to reach the next block-row.
	ADD	$32, R7, R7
	SUB	$1, R5, R5
	CBNZ	R5, br_loop_uv
	RET
