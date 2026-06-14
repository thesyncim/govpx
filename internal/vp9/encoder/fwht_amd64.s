//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0 vp9/encoder/x86/vp9_dct_sse2.asm
// vp9_fwht4x4_sse2. The arithmetic is the same two-pass 4x4 reversible
// Walsh-Hadamard transform used by forwardWHT4x4Scalar; output is 16 int16
// coefficients in raster order.

#include "textflag.h"

// forwardWHT4x4SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardWHT4x4SSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	stride+8(FP), DX
	MOVQ	output+16(FP), DI
	SHLQ	$1, DX                  // bytes per row = stride * 2

	// Load 4 rows of 4 int16 each into the low halves of X0..X3.
	MOVQ	(SI), X0
	MOVQ	(SI)(DX*1), X1
	LEAQ	(SI)(DX*2), BX
	MOVQ	(BX), X2
	MOVQ	(BX)(DX*1), X3

	// Pass 0: column WHT on the four loaded rows.
	//   a = r0 + r1
	//   d = r3 - r2
	//   e = (a - d) >> 1
	//   b = e - r1
	//   c = e - r2
	//   a -= c
	//   d += b
	PADDW	X1, X0
	MOVO	X0, X4
	PSUBW	X2, X3
	PSUBW	X3, X4
	PSRAW	$1, X4
	MOVO	X4, X5
	PSUBW	X1, X5
	PSUBW	X2, X4
	PSUBW	X4, X0
	PADDW	X5, X3
	MOVO	X4, X1
	MOVO	X3, X2
	MOVO	X5, X3

	// Transpose (a, c, d, b). X0 becomes cols 0/1, X1 becomes cols 2/3.
	PUNPCKLWL	X1, X0
	PUNPCKLWL	X3, X2
	MOVO	X0, X1
	PUNPCKLLQ	X2, X0
	PUNPCKHLQ	X2, X1

	// Split the two packed vectors back into four low-half column vectors.
	MOVO	X1, X2
	MOVO	X0, X1
	PSRLDQ	$8, X1
	MOVO	X2, X3
	PSRLDQ	$8, X3

	// Pass 1: row WHT, now operating on the transposed columns.
	PADDW	X1, X0
	MOVO	X0, X4
	PSUBW	X2, X3
	PSUBW	X3, X4
	PSRAW	$1, X4
	MOVO	X4, X5
	PSUBW	X1, X5
	PSUBW	X2, X4
	PSUBW	X4, X0
	PADDW	X5, X3
	MOVO	X4, X1
	MOVO	X3, X2
	MOVO	X5, X3

	// Transpose back to raster-order rows and apply UNIT_QUANT_FACTOR (4).
	PUNPCKLWL	X1, X0
	PUNPCKLWL	X3, X2
	MOVO	X0, X1
	PUNPCKLLQ	X2, X0
	PUNPCKHLQ	X2, X1
	PSLLW	$2, X0
	PSLLW	$2, X1

	MOVOU	X0, 0(DI)
	MOVOU	X1, 16(DI)
	RET
