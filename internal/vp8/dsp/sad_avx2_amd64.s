//go:build amd64 && !purego

// AVX2 SAD primitives for the VP8 picker. Mirrors libvpx v1.16.0
// vpx_dsp/x86/sad4d_avx2.c / sad_avx2.c — VPSADBW on YMM does
// abs-diff + horizontal-sum across 32 bytes, producing four 16-bit
// partial sums (one per 64-bit lane). For 16-wide SADs we pack two
// rows of 16 src/ref bytes into a single YMM via VINSERTI128, run
// VPSADBW, and accumulate. For 8-wide SADs we pack four rows of
// 8 bytes into a single YMM via two PUNPCKLQDQ + VINSERTI128.
//
// Calling convention (sadBlock16x16AVX2, ABI0, $0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32
//
// All four AVX2 SAD entry points share the same signature.

#include "textflag.h"

// Each kernel folds Y0 (running 4x int64 partial sums) down to a
// single XMM via VEXTRACTI128 + PADDD, then reduces with MOVHLPS +
// PADDD to one int32 in X0 lane 0, writes ret+32(FP), and VZEROUPPER.
// We inline the tail rather than use a macro so vet's frame-pointer
// flow analysis sees the explicit MOVL into ret+32(FP).

// sadBlock16x16AVX2: 16 rows of 16 bytes. Two rows per iteration —
// each iteration packs 32 src/ref bytes into a YMM, runs VPSADBW.
TEXT ·sadBlock16x16AVX2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	VPXOR	Y0, Y0, Y0           // running sum (4x int64)
	MOVQ	$8, R8               // 8 row-pairs = 16 rows

loop16x16_avx2:
	// Pack rows y, y+1 of src into Y1.
	VMOVDQU	(AX), X1
	VINSERTI128	$1, (AX)(BX*1), Y1, Y1
	// Pack rows y, y+1 of ref into Y2.
	VMOVDQU	(CX), X2
	VINSERTI128	$1, (CX)(DX*1), Y2, Y2

	VPSADBW	Y2, Y1, Y1
	VPADDQ	Y1, Y0, Y0

	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	DECQ	R8
	JNZ	loop16x16_avx2

	// Reduce Y0 (4x int64 partial sums) → single int32 in X0 lane 0.
	VEXTRACTI128	$1, Y0, X1
	PADDD	X1, X0
	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	VZEROUPPER
	RET

// sadBlock16x16x4AVX2 ABI ($0-64):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref0+16(FP)      *byte
//   ref1+24(FP)      *byte
//   ref2+32(FP)      *byte
//   ref3+40(FP)      *byte
//   refStride+48(FP) int
//   out+56(FP)       *[4]uint32
//
// Load each source row-pair once, compare it against four candidate reference
// row-pairs, and accumulate four SAD lanes.
TEXT ·sadBlock16x16x4AVX2(SB), NOSPLIT, $0-64
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref0+16(FP), CX
	MOVQ	ref1+24(FP), DX
	MOVQ	ref2+32(FP), SI
	MOVQ	ref3+40(FP), DI
	MOVQ	refStride+48(FP), R9

	VPXOR	Y10, Y10, Y10
	VPXOR	Y11, Y11, Y11
	VPXOR	Y12, Y12, Y12
	VPXOR	Y13, Y13, Y13
	MOVQ	$8, R8

x4_loop16x16_avx2:
	VMOVDQU	(AX), X1
	VINSERTI128	$1, (AX)(BX*1), Y1, Y1

	VMOVDQU	(CX), X2
	VINSERTI128	$1, (CX)(R9*1), Y2, Y2
	VPSADBW	Y2, Y1, Y4
	VPADDQ	Y4, Y10, Y10

	VMOVDQU	(DX), X2
	VINSERTI128	$1, (DX)(R9*1), Y2, Y2
	VPSADBW	Y2, Y1, Y4
	VPADDQ	Y4, Y11, Y11

	VMOVDQU	(SI), X2
	VINSERTI128	$1, (SI)(R9*1), Y2, Y2
	VPSADBW	Y2, Y1, Y4
	VPADDQ	Y4, Y12, Y12

	VMOVDQU	(DI), X2
	VINSERTI128	$1, (DI)(R9*1), Y2, Y2
	VPSADBW	Y2, Y1, Y4
	VPADDQ	Y4, Y13, Y13

	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(R9*2), CX
	LEAQ	(DX)(R9*2), DX
	LEAQ	(SI)(R9*2), SI
	LEAQ	(DI)(R9*2), DI
	DECQ	R8
	JNZ	x4_loop16x16_avx2

	MOVQ	out+56(FP), R10

	VEXTRACTI128	$1, Y10, X0
	PADDD	X0, X10
	MOVHLPS	X10, X0
	PADDD	X0, X10
	MOVL	X10, 0(R10)

	VEXTRACTI128	$1, Y11, X0
	PADDD	X0, X11
	MOVHLPS	X11, X0
	PADDD	X0, X11
	MOVL	X11, 4(R10)

	VEXTRACTI128	$1, Y12, X0
	PADDD	X0, X12
	MOVHLPS	X12, X0
	PADDD	X0, X12
	MOVL	X12, 8(R10)

	VEXTRACTI128	$1, Y13, X0
	PADDD	X0, X13
	MOVHLPS	X13, X0
	PADDD	X0, X13
	MOVL	X13, 12(R10)
	VZEROUPPER
	RET

// sadBlock16x8AVX2: 8 rows of 16 bytes.
TEXT ·sadBlock16x8AVX2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	VPXOR	Y0, Y0, Y0
	MOVQ	$4, R8               // 4 row-pairs = 8 rows

loop16x8_avx2:
	VMOVDQU	(AX), X1
	VINSERTI128	$1, (AX)(BX*1), Y1, Y1
	VMOVDQU	(CX), X2
	VINSERTI128	$1, (CX)(DX*1), Y2, Y2

	VPSADBW	Y2, Y1, Y1
	VPADDQ	Y1, Y0, Y0

	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	DECQ	R8
	JNZ	loop16x8_avx2

	VEXTRACTI128	$1, Y0, X1
	PADDD	X1, X0
	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	VZEROUPPER
	RET

// sadBlock8x16AVX2: 16 rows of 8 bytes. Four rows per iteration —
// pack two pairs of (row, row+1) into low/high 128-bit halves of a
// YMM via PUNPCKLQDQ on each XMM half, then VINSERTI128.
TEXT ·sadBlock8x16AVX2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	VPXOR	Y0, Y0, Y0
	MOVQ	$4, R8               // 4 row-quads = 16 rows

loop8x16_avx2:
	// Low half: pack src rows y (low 8B) and y+1 (high 8B) into X1.
	MOVQ	(AX), X1
	MOVQ	(AX)(BX*1), X3
	PUNPCKLQDQ	X3, X1
	// High half: rows y+2, y+3 into X3.
	LEAQ	(AX)(BX*2), R10
	MOVQ	(R10), X3
	MOVQ	(R10)(BX*1), X4
	PUNPCKLQDQ	X4, X3
	// Combine into Y1.
	VINSERTI128	$1, X3, Y1, Y1

	// Same for ref into Y2.
	MOVQ	(CX), X2
	MOVQ	(CX)(DX*1), X5
	PUNPCKLQDQ	X5, X2
	LEAQ	(CX)(DX*2), R11
	MOVQ	(R11), X5
	MOVQ	(R11)(DX*1), X6
	PUNPCKLQDQ	X6, X5
	VINSERTI128	$1, X5, Y2, Y2

	VPSADBW	Y2, Y1, Y1
	VPADDQ	Y1, Y0, Y0

	// Advance by 4 rows.
	LEAQ	(R10)(BX*2), AX
	LEAQ	(R11)(DX*2), CX
	DECQ	R8
	JNZ	loop8x16_avx2

	VEXTRACTI128	$1, Y0, X1
	PADDD	X1, X0
	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	VZEROUPPER
	RET

// sadBlock8x8AVX2: 8 rows of 8 bytes.
TEXT ·sadBlock8x8AVX2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	VPXOR	Y0, Y0, Y0
	MOVQ	$2, R8               // 2 row-quads = 8 rows

loop8x8_avx2:
	MOVQ	(AX), X1
	MOVQ	(AX)(BX*1), X3
	PUNPCKLQDQ	X3, X1
	LEAQ	(AX)(BX*2), R10
	MOVQ	(R10), X3
	MOVQ	(R10)(BX*1), X4
	PUNPCKLQDQ	X4, X3
	VINSERTI128	$1, X3, Y1, Y1

	MOVQ	(CX), X2
	MOVQ	(CX)(DX*1), X5
	PUNPCKLQDQ	X5, X2
	LEAQ	(CX)(DX*2), R11
	MOVQ	(R11), X5
	MOVQ	(R11)(DX*1), X6
	PUNPCKLQDQ	X6, X5
	VINSERTI128	$1, X5, Y2, Y2

	VPSADBW	Y2, Y1, Y1
	VPADDQ	Y1, Y0, Y0

	LEAQ	(R10)(BX*2), AX
	LEAQ	(R11)(DX*2), CX
	DECQ	R8
	JNZ	loop8x8_avx2

	VEXTRACTI128	$1, Y0, X1
	PADDD	X1, X0
	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	VZEROUPPER
	RET
