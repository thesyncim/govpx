//go:build amd64 && !purego

// SSE2 residual gather kernels. They mirror vp8_subtract_mby /
// vp8_subtract_mbuv and write govpx's block-major residual layout:
// contiguous 4x4 int16 blocks in scan order, each row occupying 8 bytes.

#include "textflag.h"

// residualGather16x16SSE2 ABI ($0-40):
//   src+0(FP)         *byte
//   srcStride+8(FP)   int
//   pred+16(FP)       *byte
//   predStride+24(FP) int
//   out+32(FP)        *int16
TEXT ·residualGather16x16SSE2(SB), NOSPLIT, $0-40
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	pred+16(FP), CX
	MOVQ	predStride+24(FP), DX
	MOVQ	out+32(FP), DI

	PXOR	X15, X15
	MOVQ	$4, R8

rg16_block_row:
	MOVQ	$4, R9

rg16_row:
	MOVOU	(AX), X0
	MOVOU	(CX), X1

	MOVO	X0, X2
	MOVO	X1, X3
	PUNPCKLBW	X15, X2
	PUNPCKHBW	X15, X0
	PUNPCKLBW	X15, X3
	PUNPCKHBW	X15, X1
	PSUBW	X3, X2
	PSUBW	X1, X0

	MOVQ	X2, (DI)
	MOVO	X2, X4
	PSRLDQ	$8, X4
	MOVQ	X4, 32(DI)
	MOVQ	X0, 64(DI)
	MOVO	X0, X4
	PSRLDQ	$8, X4
	MOVQ	X4, 96(DI)

	ADDQ	$8, DI
	ADDQ	BX, AX
	ADDQ	DX, CX
	DECQ	R9
	JNZ	rg16_row

	ADDQ	$96, DI
	DECQ	R8
	JNZ	rg16_block_row
	RET

// residualGather8x8SSE2 ABI ($0-40):
//   src+0(FP)         *byte
//   srcStride+8(FP)   int
//   pred+16(FP)       *byte
//   predStride+24(FP) int
//   out+32(FP)        *int16
TEXT ·residualGather8x8SSE2(SB), NOSPLIT, $0-40
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	pred+16(FP), CX
	MOVQ	predStride+24(FP), DX
	MOVQ	out+32(FP), DI

	PXOR	X15, X15
	MOVQ	$2, R8

rg8_block_row:
	MOVQ	$4, R9

rg8_row:
	MOVQ	(AX), X0
	MOVQ	(CX), X1
	PUNPCKLBW	X15, X0
	PUNPCKLBW	X15, X1
	PSUBW	X1, X0

	MOVQ	X0, (DI)
	MOVO	X0, X2
	PSRLDQ	$8, X2
	MOVQ	X2, 32(DI)

	ADDQ	$8, DI
	ADDQ	BX, AX
	ADDQ	DX, CX
	DECQ	R9
	JNZ	rg8_row

	ADDQ	$32, DI
	DECQ	R8
	JNZ	rg8_block_row
	RET
