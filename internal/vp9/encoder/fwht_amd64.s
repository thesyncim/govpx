//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0 vp9/encoder/x86/vp9_dct_sse2.c
// vp9_fwht4x4_sse2. Output is byte-identical to forwardWHT4x4Scalar for
// the encoder's residual range.

#include "textflag.h"

// forwardWHT4x4SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int  (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardWHT4x4SSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	stride+8(FP), DX
	MOVQ	output+16(FP), DI
	SHLQ	$1, DX                  // bytes per row = stride * 2

	// Load 4 rows of 4 int16 into low halves.
	MOVQ	(SI), X0
	MOVQ	(SI)(DX*1), X1
	LEAQ	(SI)(DX*2), SI
	MOVQ	(SI), X2
	MOVQ	(SI)(DX*1), X3

	// Pass 0 column WHT across row registers; lane c = column c.
	//   a = r0 + r1
	//   d = r3 - r2
	//   e = (a - d) >> 1
	//   b = e - r1
	//   c = e - r2
	//   a -= c
	//   d += b
	MOVO	X0, X4
	PADDW	X1, X4                  // a
	MOVO	X3, X5
	PSUBW	X2, X5                  // d
	MOVO	X4, X6
	PSUBW	X5, X6
	PSRAW	$1, X6                  // e
	MOVO	X6, X7
	PSUBW	X1, X7                  // b
	MOVO	X6, X8
	PSUBW	X2, X8                  // c
	PSUBW	X8, X4                  // a
	PADDW	X7, X5                  // d

	// Transpose (a, c, d, b) so low 4 lanes of X10..X13 are columns 0..3.
	MOVO	X4, X0
	PUNPCKLWL	X8, X0          // [a0,c0,a1,c1,a2,c2,a3,c3]
	MOVO	X5, X1
	PUNPCKLWL	X7, X1          // [d0,b0,d1,b1,d2,b2,d3,b3]
	MOVO	X0, X2
	PUNPCKLLQ	X1, X0          // col0 in low64, col1 in high64
	PUNPCKHLQ	X1, X2          // col2 in low64, col3 in high64
	MOVO	X0, X10
	MOVO	X0, X11
	PUNPCKHQDQ	X11, X11        // col1 in low64
	MOVO	X2, X12
	MOVO	X2, X13
	PUNPCKHQDQ	X13, X13        // col3 in low64

	// Pass 1 row WHT on columns, lane f = frequency from pass 0.
	MOVO	X10, X4
	PADDW	X11, X4                 // a
	MOVO	X13, X5
	PSUBW	X12, X5                 // d
	MOVO	X4, X6
	PSUBW	X5, X6
	PSRAW	$1, X6                  // e
	MOVO	X6, X7
	PSUBW	X11, X7                 // b
	MOVO	X6, X8
	PSUBW	X12, X8                 // c
	PSUBW	X8, X4                  // a
	PADDW	X7, X5                  // d

	// Final scale by 4.
	PSLLW	$2, X4
	PSLLW	$2, X8
	PSLLW	$2, X5
	PSLLW	$2, X7

	// Transpose (a, c, d, b) back to row-major output.
	MOVO	X4, X0
	PUNPCKLWL	X8, X0          // [r0c0,r0c1,r1c0,r1c1,...]
	MOVO	X5, X1
	PUNPCKLWL	X7, X1          // [r0c2,r0c3,r1c2,r1c3,...]
	MOVO	X0, X2
	PUNPCKLLQ	X1, X0          // rows 0 and 1
	PUNPCKHLQ	X1, X2          // rows 2 and 3

	MOVOU	X0, 0(DI)
	MOVOU	X2, 16(DI)
	RET
