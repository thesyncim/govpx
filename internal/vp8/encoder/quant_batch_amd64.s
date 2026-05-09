// SSE2 batched port of libvpx v1.16.0
// vp8/encoder/x86/vp8_quantize_sse2.c vp8_fast_quantize_b_sse2.
// Same per-block kernel as fastQuantizeBlockSSE2; quant tables and
// the inv-zigzag mask are loaded once before the outer loop so a
// single Go<->asm transition handles every block that shares the
// BlockQuant.
//
// Per-block per-lane logic:
//   sz = z >> 15
//   x  = abs(z) = (z ^ sz) - sz
//   x  = x + round
//   y  = (x * quantFast) >> 16   (PMULHW)
//   x  = (y ^ sz) - sz
//   qcoeff  = x
//   dqcoeff = x * dequant        (PMULLW)
//   eob = max over 16 lanes of ((x != 0) ? invZigZag : 0)

#include "textflag.h"

// fastQuantizeBlockBatchSSE2 ABI ($0-64):
//   coeff+0(FP)      *int16
//   round+8(FP)      *int16
//   quantFast+16(FP) *int16
//   dequant+24(FP)   *int16
//   qcoeff+32(FP)    *int16
//   dqcoeff+40(FP)   *int16
//   eobs+48(FP)      *uint8
//   count+56(FP)     int
TEXT ·fastQuantizeBlockBatchSSE2(SB), NOSPLIT, $0-64
	MOVQ	coeff+0(FP),     SI
	MOVQ	round+8(FP),     R8
	MOVQ	quantFast+16(FP), R9
	MOVQ	dequant+24(FP),  R10
	MOVQ	qcoeff+32(FP),   DI
	MOVQ	dqcoeff+40(FP),  R11
	MOVQ	eobs+48(FP),     R12
	MOVQ	count+56(FP),    CX

	// Hoist quant-table loads (constant across the batch):
	// X2/X3 = round, X4/X5 = quantFast, X6/X7 = dequant.
	MOVOU	(R8),    X2
	MOVOU	16(R8),  X3
	MOVOU	(R9),    X4
	MOVOU	16(R9),  X5
	MOVOU	(R10),   X6
	MOVOU	16(R10), X7

	// inv-zigzag lives in the per-block kernel's RODATA pool
	// (invZigZagSSE2), reused here for the eob reduction.
	LEAQ	invZigZagSSE2(SB), AX

batchLoop:
	// X0/X1: z (this block).
	MOVOU	(SI),   X0
	MOVOU	16(SI), X1

	// X8/X9 = sz = z >> 15
	MOVO	X0, X8
	PSRAW	$15, X8
	MOVO	X1, X9
	PSRAW	$15, X9

	// abs(z) = (z ^ sz) - sz
	MOVO	X0, X10
	PXOR	X8, X10
	PSUBW	X8, X10
	MOVO	X1, X11
	PXOR	X9, X11
	PSUBW	X9, X11

	// x += round
	PADDW	X2, X10
	PADDW	X3, X11

	// y = (x * quantFast) >> 16
	PMULHW	X4, X10
	PMULHW	X5, X11

	// x = (y ^ sz) - sz
	PXOR	X8, X10
	PXOR	X9, X11
	PSUBW	X8, X10
	PSUBW	X9, X11

	// qcoeff = x
	MOVOU	X10, (DI)
	MOVOU	X11, 16(DI)

	// dqcoeff = x * dequant (low 16 bits)
	MOVO	X10, X12
	PMULLW	X6, X12
	MOVO	X11, X13
	PMULLW	X7, X13
	MOVOU	X12, (R11)
	MOVOU	X13, 16(R11)

	// EOB: build (x != 0) mask, AND with invZigZag, max-reduce.
	PXOR	X14, X14
	MOVO	X10, X12
	PCMPEQW	X14, X12
	MOVO	X11, X13
	PCMPEQW	X14, X13

	PCMPEQL	X15, X15
	PXOR	X15, X12
	PXOR	X15, X13

	MOVOU	(AX),   X0
	MOVOU	16(AX), X1
	PAND	X0, X12
	PAND	X1, X13

	PMAXSW	X13, X12
	MOVO	X12, X13
	PSHUFD	$0xE, X13, X13
	PMAXSW	X13, X12
	MOVO	X12, X13
	PSHUFLW	$0xE, X13, X13
	PMAXSW	X13, X12
	MOVO	X12, X13
	PSHUFLW	$0x1, X13, X13
	PMAXSW	X13, X12

	MOVD	X12, BX
	MOVB	BL, (R12)
	INCQ	R12

	ADDQ	$32, SI
	ADDQ	$32, DI
	ADDQ	$32, R11
	DECQ	CX
	JNZ	batchLoop
	RET
