//go:build amd64 && !purego

// SSE2 SAD primitive for VP9's vpx_sad16x16_c equivalent. This is the same
// PSADBW reduction used by libvpx's vpx_sad16x16_sse2.

#include "textflag.h"

// sad16x16SSE2 ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       uint32
TEXT ·sad16x16SSE2(SB), NOSPLIT, $0-36
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX

	PXOR	X0, X0
	MOVQ	$16, R8

loop:
	MOVOU	(AX), X1
	MOVOU	(CX), X2
	PSADBW	X2, X1
	PADDD	X1, X0
	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	loop

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+32(FP)
	RET

// sad16xNSSE2 ABI ($0-44):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   rows+32(FP)      int
//   ret+40(FP)       uint32
TEXT ·sad16xNSSE2(SB), NOSPLIT, $0-44
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	rows+32(FP), R8

	PXOR	X0, X0

loop16xN:
	MOVOU	(AX), X1
	MOVOU	(CX), X2
	PSADBW	X2, X1
	PADDD	X1, X0
	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	loop16xN

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+40(FP)
	RET

// sad8xNSSE2 ABI ($0-44): 8 bytes per row, row count supplied by caller.
TEXT ·sad8xNSSE2(SB), NOSPLIT, $0-44
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	rows+32(FP), R8

	PXOR	X0, X0

loop8xN:
	MOVQ	(AX), X1
	MOVQ	(CX), X2
	PSADBW	X2, X1
	PADDD	X1, X0
	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	loop8xN

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+40(FP)
	RET

// sad16ChunksSSE2 ABI ($0-52):
//   rows is the block height, chunks is width/16. Used for 32/64-wide SADs.
TEXT ·sad16ChunksSSE2(SB), NOSPLIT, $0-52
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	rows+32(FP), R8
	MOVQ	chunks+40(FP), R9

	PXOR	X0, X0

rowLoop16Chunks:
	MOVQ	R9, R10
	MOVQ	AX, R11
	MOVQ	CX, R12

chunkLoop16Chunks:
	MOVOU	(R11), X1
	MOVOU	(R12), X2
	PSADBW	X2, X1
	PADDD	X1, X0
	ADDQ	$16, R11
	ADDQ	$16, R12
	SUBQ	$1, R10
	JNZ	chunkLoop16Chunks

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	rowLoop16Chunks

	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+48(FP)
	RET
