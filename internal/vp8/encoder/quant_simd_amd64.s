// SSE2 port of libvpx v1.16.0 vp8/encoder/x86/vp8_quantize_sse2.c
// vp8_fast_quantize_b_sse2.
//
// Per 16-lane block:
//   sz = z >> 15          (sign bits via PSRAW arithmetic by 15)
//   x  = abs(z) = (z ^ sz) - sz
//   x  = x + round
//   y  = (x * quantFast) >> 16   (PMULHW)
//   x  = (y ^ sz) - sz
//   qcoeff  = x
//   dqcoeff = x * dequant        (PMULLW)
//   eob = max over 16 lanes of ((x != 0) ? invZigZag : 0), reduced.

#include "textflag.h"

// Inverse zigzag: 1-indexed scan position for natural-order coefficient
// slot (matches tables.DefaultInvZigZag).
DATA  invZigZagSSE2+0x00(SB)/2, $1
DATA  invZigZagSSE2+0x02(SB)/2, $2
DATA  invZigZagSSE2+0x04(SB)/2, $6
DATA  invZigZagSSE2+0x06(SB)/2, $7
DATA  invZigZagSSE2+0x08(SB)/2, $3
DATA  invZigZagSSE2+0x0a(SB)/2, $5
DATA  invZigZagSSE2+0x0c(SB)/2, $8
DATA  invZigZagSSE2+0x0e(SB)/2, $13
DATA  invZigZagSSE2+0x10(SB)/2, $4
DATA  invZigZagSSE2+0x12(SB)/2, $9
DATA  invZigZagSSE2+0x14(SB)/2, $12
DATA  invZigZagSSE2+0x16(SB)/2, $14
DATA  invZigZagSSE2+0x18(SB)/2, $10
DATA  invZigZagSSE2+0x1a(SB)/2, $11
DATA  invZigZagSSE2+0x1c(SB)/2, $15
DATA  invZigZagSSE2+0x1e(SB)/2, $16
GLOBL invZigZagSSE2(SB), RODATA|NOPTR, $32

// fastQuantizeBlockSSE2 ABI ($0-52):
//   coeff+0(FP)      *int16
//   round+8(FP)      *int16
//   quantFast+16(FP) *int16
//   dequant+24(FP)   *int16
//   qcoeff+32(FP)    *int16
//   dqcoeff+40(FP)   *int16
//   ret+48(FP)       int32
TEXT ·fastQuantizeBlockSSE2(SB), NOSPLIT, $0-52
	MOVQ	coeff+0(FP), AX
	MOVQ	round+8(FP), BX
	MOVQ	quantFast+16(FP), CX
	MOVQ	dequant+24(FP), DX
	MOVQ	qcoeff+32(FP), SI
	MOVQ	dqcoeff+40(FP), DI

	// X0/X1: z (16 lanes)
	MOVOU	(AX), X0
	MOVOU	16(AX), X1
	// X2/X3: round
	MOVOU	(BX), X2
	MOVOU	16(BX), X3
	// X4/X5: quantFast
	MOVOU	(CX), X4
	MOVOU	16(CX), X5
	// X6/X7: dequant
	MOVOU	(DX), X6
	MOVOU	16(DX), X7

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
	MOVOU	X10, (SI)
	MOVOU	X11, 16(SI)

	// dqcoeff = x * dequant (low 16 bits)
	MOVO	X10, X12
	PMULLW	X6, X12
	MOVO	X11, X13
	PMULLW	X7, X13
	MOVOU	X12, (DI)
	MOVOU	X13, 16(DI)

	// EOB: build mask of (x != 0) lanes via PCMPEQW with zero, XOR with all-ones,
	// AND with invZigZag, reduce via max.
	PXOR	X14, X14
	MOVO	X10, X12
	PCMPEQW	X14, X12
	MOVO	X11, X13
	PCMPEQW	X14, X13

	// X12 is now -1 where x == 0; flip to get -1 where x != 0.
	PCMPEQL	X15, X15        // all ones
	PXOR	X15, X12
	PXOR	X15, X13

	// invZigZag mask
	LEAQ	invZigZagSSE2(SB), AX
	MOVOU	(AX), X4
	MOVOU	16(AX), X5
	PAND	X4, X12
	PAND	X5, X13

	// max over 16 lanes
	PMAXSW	X13, X12
	// X12 has 8 lanes — fold pairs:
	MOVO	X12, X13
	PSHUFD	$0xE, X13, X13   // shift high 64 bits to low
	PMAXSW	X13, X12
	MOVO	X12, X13
	PSHUFLW	$0xE, X13, X13   // 0b00001110
	PMAXSW	X13, X12
	MOVO	X12, X13
	PSHUFLW	$0x1, X13, X13   // 0b00000001
	PMAXSW	X13, X12

	MOVD	X12, AX
	MOVWLZX	AX, AX            // zero-extend low 16 bits (eob fits in uint16)
	MOVL	AX, ret+48(FP)
	RET
