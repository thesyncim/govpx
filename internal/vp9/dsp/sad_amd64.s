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
