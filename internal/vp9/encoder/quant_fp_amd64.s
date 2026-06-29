//go:build amd64 && !purego

// SSE2 bulk AC loop for libvpx v1.16.0
// vp9/encoder/x86/vp9_quantize_sse2.c::vp9_quantize_fp_sse2.
// Go handles DC and the non-multiple-of-8 tail; this kernel processes
// raster-order AC coefficients in 8-lane chunks and max-reduces iscan for EOB.

#include "textflag.h"

// quantizeFPACSSE2 ABI ($0-68):
//   coeff+0(FP)    *int16
//   iscan+8(FP)    *int16
//   qcoeff+16(FP)  *int16
//   dqcoeff+24(FP) *int16
//   count+32(FP)   int     // multiple of 8
//   roundAC+40(FP) int
//   quantAC+48(FP) int
//   deqAC+56(FP)   int
//   ret+64(FP)     int32
TEXT ·quantizeFPACSSE2(SB), NOSPLIT, $0-68
	MOVQ	coeff+0(FP), SI
	MOVQ	iscan+8(FP), R8
	MOVQ	qcoeff+16(FP), DI
	MOVQ	dqcoeff+24(FP), R9
	MOVQ	count+32(FP), CX

	// Broadcast AC tables as int16 lanes.
	MOVQ	roundAC+40(FP), R10
	MOVQ	R10, X8
	PSHUFLW	$0, X8, X8
	PSHUFD	$0, X8, X8

	MOVQ	quantAC+48(FP), R10
	MOVQ	R10, X9
	PSHUFLW	$0, X9, X9
	PSHUFD	$0, X9, X9

	MOVQ	deqAC+56(FP), R10
	MOVQ	R10, X10
	PSHUFLW	$0, X10, X10
	PSHUFD	$0, X10, X10

	PXOR	X7, X7                 // zero
	PCMPEQL	X15, X15              // all ones
	MOVO	X15, X11
	PSRLW	$15, X11              // 8 x int16(1)
	MOVO	X11, X12
	PSLLW	$15, X12              // 8 x uint16(0x8000), for -32768 abs fixup
	PXOR	X13, X13              // running eob max

	TESTQ	CX, CX
	JZ	quant_fp_ac_done

quant_fp_ac_loop:
	MOVOU	(SI), X0               // coeff
	MOVOU	(R8), X1               // iscan

	// sign = coeff >> 15
	MOVO	X0, X2
	PSRAW	$15, X2

	// abs(coeff) = (coeff ^ sign) - sign. For coeff == -32768, this yields
	// 0x8000; subtract one so the later saturated add matches clampInt16().
	MOVO	X0, X3
	PXOR	X2, X3
	PSUBW	X2, X3
	MOVO	X3, X4
	PCMPEQW	X12, X4
	PAND	X11, X4
	PSUBW	X4, X3

	// sum = clampInt16(abs + round), then keep only lanes where sum >= deq.
	PADDSW	X8, X3
	MOVO	X10, X4
	PCMPGTW	X3, X4                // X4 = deq > sum
	PXOR	X15, X4               // X4 = sum >= deq

	// tmp = (sum * quant) >> 16
	MOVO	X3, X5
	PMULHW	X9, X5

	// q = sign(tmp, coeff), masked to zero for sub-threshold lanes.
	MOVO	X5, X6
	PXOR	X2, X6
	PSUBW	X2, X6
	PAND	X4, X6
	MOVOU	X6, (DI)

	// dq = q * deq (low 16 bits, matching int16 cast semantics).
	MOVO	X6, X14
	PMULLW	X10, X14
	MOVOU	X14, (R9)

	// EOB: max(iscan[rc] where qcoeff[rc] != 0).
	MOVO	X6, X14
	PCMPEQW	X7, X14
	PXOR	X15, X14
	PAND	X1, X14
	PMAXSW	X14, X13

	ADDQ	$16, SI
	ADDQ	$16, R8
	ADDQ	$16, DI
	ADDQ	$16, R9
	SUBQ	$8, CX
	JNZ	quant_fp_ac_loop

quant_fp_ac_done:
	MOVO	X13, X14
	PSHUFD	$0xE, X14, X14
	PMAXSW	X14, X13
	MOVO	X13, X14
	PSHUFLW	$0xE, X14, X14
	PMAXSW	X14, X13
	MOVO	X13, X14
	PSHUFLW	$0x1, X14, X14
	PMAXSW	X14, X13

	MOVD	X13, AX
	MOVWLZX	AX, AX
	MOVL	AX, ret+64(FP)
	RET
