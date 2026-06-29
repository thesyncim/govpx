//go:build amd64 && !purego

// SSE2 SAD primitives. Mirrors libvpx v1.16.0 vpx_dsp/x86/sad_sse2.asm
// (vpx_sad{4,8,16}x{4,8,16}_sse2) plus a govpx-specific 16x16 limit-aware
// variant matching internal/vp8/dsp/sad.go's sadBlockLimit semantics.
//
//   sad = SUM_{y in [0,h), x in [0,w)} |src[y][x] - ref[y][x]|
//
// The PSADBW instruction does absolute-difference + horizontal-sum across
// 8 bytes, producing two 16-bit values in the low halves of each 64-bit
// half of the destination register. Final reduction folds via MOVHLPS +
// PADDD.

#include "textflag.h"

// sadBlock16x16SSE2 ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32
//
// Per row: load 16 src bytes (MOVOU), PSADBW with 16 ref bytes -> two
// partial sums in X1's low 16 bits of each 64-bit half. Accumulate into
// X0 across all 16 rows.

TEXT ·sadBlock16x16SSE2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	PXOR	X0, X0			// running sum
	MOVQ	$16, R8

loop16x16:
	MOVOU	(AX), X1
	MOVOU	(CX), X2
	PSADBW	X2, X1
	PADDD	X1, X0
	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	loop16x16

	// Reduce X0 to a single 32-bit value.
	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	RET

// sadBlock16x16LimitSSE2 ABI ($0-44):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   limit+32(FP)     int32
//   ret+40(FP)       int32
//
// Per row: PSADBW into running X0, reduce to scalar in DI, compare to
// limit in SI. If running sum exceeds limit, return early; the scalar
// reference does the same per-row comparison.

TEXT ·sadBlock16x16LimitSSE2(SB), NOSPLIT, $0-44
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVL	limit+32(FP), SI

	PXOR	X0, X0
	MOVQ	$16, R8

loop16x16_limit:
	MOVOU	(AX), X1
	MOVOU	(CX), X2
	PSADBW	X2, X1
	PADDD	X1, X0

	// Reduce a copy of X0 to scalar.
	MOVO	X0, X3
	MOVHLPS	X3, X4
	PADDD	X4, X3
	MOVL	X3, DI
	CMPL	DI, SI
	JA	limit_break

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	loop16x16_limit

	MOVL	DI, ret+40(FP)
	RET

limit_break:
	MOVL	DI, ret+40(FP)
	RET

// sadBlock16x16x4SSE2 ABI ($0-64):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref0+16(FP)      *byte
//   ref1+24(FP)      *byte
//   ref2+32(FP)      *byte
//   ref3+40(FP)      *byte
//   refStride+48(FP) int
//   out+56(FP)       *[4]uint32
//
// Mirrors libvpx's vpx_sad16x16x4d shape: load each source row once and
// compare it against four candidate reference rows.

TEXT ·sadBlock16x16x4SSE2(SB), NOSPLIT, $0-64
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref0+16(FP), CX
	MOVQ	ref1+24(FP), DX
	MOVQ	ref2+32(FP), SI
	MOVQ	ref3+40(FP), DI
	MOVQ	refStride+48(FP), R9

	PXOR	X10, X10
	PXOR	X11, X11
	PXOR	X12, X12
	PXOR	X13, X13
	MOVQ	$16, R8

x4_loop16x16_sse2:
	MOVOU	(AX), X1

	MOVO	X1, X6
	MOVOU	(CX), X2
	PSADBW	X2, X6
	PADDD	X6, X10

	MOVO	X1, X6
	MOVOU	(DX), X3
	PSADBW	X3, X6
	PADDD	X6, X11

	MOVO	X1, X6
	MOVOU	(SI), X4
	PSADBW	X4, X6
	PADDD	X6, X12

	MOVO	X1, X6
	MOVOU	(DI), X5
	PSADBW	X5, X6
	PADDD	X6, X13

	ADDQ	BX, AX
	ADDQ	R9, CX
	ADDQ	R9, DX
	ADDQ	R9, SI
	ADDQ	R9, DI
	SUBQ	$1, R8
	JNZ	x4_loop16x16_sse2

	MOVQ	out+56(FP), R10

	MOVHLPS	X10, X0
	PADDD	X0, X10
	MOVL	X10, 0(R10)

	MOVHLPS	X11, X0
	PADDD	X0, X11
	MOVL	X11, 4(R10)

	MOVHLPS	X12, X0
	PADDD	X0, X12
	MOVL	X12, 8(R10)

	MOVHLPS	X13, X0
	PADDD	X0, X13
	MOVL	X13, 12(R10)
	RET

// sadBlock16x8SSE2 ABI ($0-36): same shape as 16x16, 8 rows.

TEXT ·sadBlock16x8SSE2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	PXOR	X0, X0
	MOVQ	$8, R8

loop16x8:
	MOVOU	(AX), X1
	MOVOU	(CX), X2
	PSADBW	X2, X1
	PADDD	X1, X0
	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	loop16x8

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	RET

// sadBlock8x16SSE2 ABI ($0-36): 8 bytes per row, 16 rows.
//
// Per pair of rows: MOVQ src[y]/ref[y] into low halves of X1/X2,
// MOVHPD-style style isn't directly available; use PUNPCKLLQ to merge
// row y+1's 8 bytes into the high half. Then PSADBW the merged 16-byte
// vectors.

TEXT ·sadBlock8x16SSE2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	PXOR	X0, X0
	MOVQ	$8, R8	// 8 row-pairs = 16 rows

loop8x16:
	// Row y src/ref into low 8 bytes of X1/X2 (high zeroed by MOVQ).
	MOVQ	(AX), X1
	MOVQ	(CX), X2
	// Row y+1 src/ref into low 8 bytes of X3/X4.
	MOVQ	(AX)(BX*1), X3
	MOVQ	(CX)(DX*1), X4
	// Merge: X1 = [src_y | src_{y+1}], X2 = [ref_y | ref_{y+1}].
	PUNPCKLLQ	X3, X1
	PUNPCKLLQ	X4, X2
	PSADBW	X2, X1
	PADDD	X1, X0
	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	SUBQ	$1, R8
	JNZ	loop8x16

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	RET

// sadBlock8x8SSE2 ABI ($0-36): 8 bytes per row, 8 rows.

TEXT ·sadBlock8x8SSE2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	PXOR	X0, X0
	MOVQ	$4, R8	// 4 row-pairs = 8 rows

loop8x8:
	MOVQ	(AX), X1
	MOVQ	(CX), X2
	MOVQ	(AX)(BX*1), X3
	MOVQ	(CX)(DX*1), X4
	PUNPCKLLQ	X3, X1
	PUNPCKLLQ	X4, X2
	PSADBW	X2, X1
	PADDD	X1, X0
	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	SUBQ	$1, R8
	JNZ	loop8x8

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	RET

// sadBlock4x4SSE2 ABI ($0-36): 4 bytes per row, 4 rows.
//
// Mirrors libvpx SAD4XN: pack 4 rows of 4 bytes each into a single 16-byte
// register via MOVD + PUNPCKLLQ + MOVLHPS, then a single PSADBW reduces
// the whole block.

TEXT ·sadBlock4x4SSE2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	// ref: pack 4 rows of 4 bytes into X1.
	MOVL	(CX), X1
	MOVL	(CX)(DX*1), X5
	MOVL	(CX)(DX*2), X6
	LEAQ	(DX)(DX*2), R8	// R8 = 3*refStride
	MOVL	(CX)(R8*1), X7
	PUNPCKLLQ	X5, X1	// X1 low8 = [r0|r1]
	PUNPCKLLQ	X7, X6	// X6 low8 = [r2|r3]
	MOVLHPS	X6, X1		// X1 = [r0|r1|r2|r3]

	// src: pack 4 rows of 4 bytes into X2.
	MOVL	(AX), X2
	MOVL	(AX)(BX*1), X5
	MOVL	(AX)(BX*2), X6
	LEAQ	(BX)(BX*2), R9	// R9 = 3*srcStride
	MOVL	(AX)(R9*1), X7
	PUNPCKLLQ	X5, X2
	PUNPCKLLQ	X7, X6
	MOVLHPS	X6, X2

	PSADBW	X2, X1
	// X1 holds two 16-bit partial sums (low halves of the two 64-bit
	// lanes). Reduce.
	MOVHLPS	X1, X2
	PADDD	X2, X1
	MOVL	X1, ret+32(FP)
	RET
