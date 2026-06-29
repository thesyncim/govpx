//go:build amd64 && !purego

// VP9 AMD64 SSE2 block-error kernel. Libvpx's
// vp9/encoder/x86/vp9_error_sse2.asm uses 16-bit subtracts because the
// encode path's transform coefficients stay within the expected
// low-bitdepth range. This kernel keeps govpx's scalar helper semantics
// for arbitrary int16 inputs by sign-extending to int32 before
// subtracting, taking abs(diff), and using PMULUDQ (Plan 9 PMULULQ)
// for exact uint32*uint32 -> uint64 products.

#include "textflag.h"

// blockErrorFPSSE2 ABI ($0-32):
//   coeff+0(FP)   *int16
//   dqcoeff+8(FP) *int16
//   n+16(FP)      int
//   ret+24(FP)    uint64
//
// n must be a positive multiple of 8.
TEXT ·blockErrorFPSSE2(SB), NOSPLIT, $0-32
	MOVQ	coeff+0(FP), AX
	MOVQ	dqcoeff+8(FP), BX
	MOVQ	n+16(FP), CX

	PXOR	X15, X15 // uint64 accumulator in two qword lanes

be_loop:
	MOVOU	(AX), X0 // 8 int16 coeffs
	MOVOU	(BX), X1 // 8 int16 dqcoeffs
	ADDQ	$16, AX
	ADDQ	$16, BX
	SUBQ	$8, CX

	// Sign-extend coeff low/high 4 int16 lanes to int32 lanes.
	MOVO	X0, X2
	MOVO	X0, X3
	PSRAW	$15, X3
	MOVO	X2, X4
	PUNPCKLWL X3, X2
	PUNPCKHWL X3, X4

	// Sign-extend dqcoeff low/high 4 int16 lanes to int32 lanes.
	MOVO	X1, X5
	MOVO	X1, X6
	PSRAW	$15, X6
	MOVO	X5, X7
	PUNPCKLWL X6, X5
	PUNPCKHWL X6, X7

	// int32 diffs for lanes 0..3 and 4..7.
	PSUBL	X5, X2
	PSUBL	X7, X4

	// abs(diff) for lanes 0..3.
	MOVO	X2, X8
	PSRAL	$31, X8
	PXOR	X8, X2
	PSUBL	X8, X2

	// abs(diff) for lanes 4..7.
	MOVO	X4, X9
	PSRAL	$31, X9
	PXOR	X9, X4
	PSUBL	X9, X4

	// Square lanes 0 and 2, then lanes 1 and 3.
	MOVO	X2, X10
	PMULULQ	X2, X2
	PSHUFD	$0xB1, X10, X10
	PMULULQ	X10, X10
	PADDQ	X2, X15
	PADDQ	X10, X15

	// Square lanes 4 and 6, then lanes 5 and 7.
	MOVO	X4, X11
	PMULULQ	X4, X4
	PSHUFD	$0xB1, X11, X11
	PMULULQ	X11, X11
	PADDQ	X4, X15
	PADDQ	X11, X15

	JNZ	be_loop

	// Horizontal qword sum.
	MOVO	X15, X0
	PSHUFD	$0x4E, X0, X1
	PADDQ	X1, X0
	MOVQ	X0, ret+24(FP)
	RET
