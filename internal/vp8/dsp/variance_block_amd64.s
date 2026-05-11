//go:build amd64 && !purego

// SSE2 16x16 variance block. Mirrors libvpx v1.16.0
// vpx_dsp/x86/variance_sse2.c variance16_kernel_sse2 / variance16_sse2:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// Per row: load 16 src + 16 ref bytes (MOVOU), unpack to two halves
// of 8 uint16 lanes, PSUBW to get signed 16-bit diffs, PADDW into the
// 16-bit sum accumulator, PMADDWD into the 32-bit sse accumulator.
// After 16 rows, sign-extend the sum lanes (max abs value is
// 16*16*255 = 65280, exceeds int16 signed range) and horizontal-
// reduce both accumulators to scalars.
//
// Calling convention (ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   sumOut+32(FP)    *int32
//   sseOut+40(FP)    *uint32

#include "textflag.h"

TEXT ·varianceBlock16x16SSE2(SB), NOSPLIT, $0-48
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	sumOut+32(FP), DI
	MOVQ	sseOut+40(FP), SI

	// X0: zero (used for unpacking bytes -> uint16).
	PXOR	X0, X0
	// X6: sum accumulator (8 int16 lanes).
	PXOR	X6, X6
	// X7: sse accumulator (4 int32 lanes).
	PXOR	X7, X7

	MOVQ	$16, R8

variance_loop:
	// Load 16 src bytes -> X1, 16 ref bytes -> X2.
	MOVOU	(AX), X1
	MOVOU	(CX), X2

	// Unpack src bytes to two halves of 8 uint16 lanes:
	//   X3 = lo (lanes 0..7)
	//   X1 = hi (lanes 8..15)  // X1 reused
	MOVOU	X1, X3
	PUNPCKLBW	X0, X3
	PUNPCKHBW	X0, X1

	// Same for ref:
	//   X4 = lo, X2 = hi
	MOVOU	X2, X4
	PUNPCKLBW	X0, X4
	PUNPCKHBW	X0, X2

	// X3 = src_lo - ref_lo (8 int16 diffs)
	// X1 = src_hi - ref_hi
	PSUBW	X4, X3
	PSUBW	X2, X1

	// Sum: PADDW into X6.
	PADDW	X3, X6
	PADDW	X1, X6

	// SSE: PMADDWD squares pairs of int16 lanes into int32 lanes,
	// then PADDD into X7.
	PMADDWL	X3, X3
	PMADDWL	X1, X1
	PADDL	X3, X7
	PADDL	X1, X7

	// Advance src/ref by their strides.
	ADDQ	BX, AX
	ADDQ	DX, CX
	DECQ	R8
	JNZ	variance_loop

	// Reduce sum: X6 holds 8 int16 lanes whose sum we want as signed
	// int32. Sign-extend each lane to int32 by combining with
	// a "0 > x" mask (PCMPGTW), then add 4 lanes of int32 horizontally.
	//   X4 = sign mask (0xffff for negative, 0 otherwise)
	PXOR	X4, X4
	PCMPGTW	X6, X4

	//   X5 = lo 4 lanes sign-extended to int32
	//   X6 = hi 4 lanes sign-extended to int32
	MOVOU	X6, X5
	PUNPCKLWL	X4, X5
	PUNPCKHWL	X4, X6
	PADDL	X5, X6

	// X6 now has 4 int32 lanes; horizontal reduce to a scalar.
	PSHUFD	$0xee, X6, X3 // X3 = X6 high 64 -> low 64
	PADDL	X3, X6
	PSHUFD	$0x55, X6, X3 // X3 lane 0 = X6 lane 1
	PADDL	X3, X6
	MOVL	X6, (DI) // store sum (int32)

	// Reduce sse: X7 has 4 uint32 lanes, sum them.
	PSHUFD	$0xee, X7, X3
	PADDL	X3, X7
	PSHUFD	$0x55, X7, X3
	PADDL	X3, X7
	MOVL	X7, (SI) // store sse (uint32)

	RET
