//go:build amd64 && !purego

// SSE2 first pass for the VP8 denoiser. Mirrors libvpx v1.16.0
// vp8/encoder/x86/denoising_sse2.c up to the first sum-diff threshold check.

#include "textflag.h"

DATA denoiseSSE2K1<>+0x00(SB)/8, $0x0101010101010101
DATA denoiseSSE2K1<>+0x08(SB)/8, $0x0101010101010101
GLOBL denoiseSSE2K1<>(SB), RODATA|NOPTR, $16

DATA denoiseSSE2K2<>+0x00(SB)/8, $0x0202020202020202
DATA denoiseSSE2K2<>+0x08(SB)/8, $0x0202020202020202
GLOBL denoiseSSE2K2<>(SB), RODATA|NOPTR, $16

DATA denoiseSSE2K7<>+0x00(SB)/8, $0x0707070707070707
DATA denoiseSSE2K7<>+0x08(SB)/8, $0x0707070707070707
GLOBL denoiseSSE2K7<>(SB), RODATA|NOPTR, $16

DATA denoiseSSE2K15<>+0x00(SB)/8, $0x0f0f0f0f0f0f0f0f
DATA denoiseSSE2K15<>+0x08(SB)/8, $0x0f0f0f0f0f0f0f0f
GLOBL denoiseSSE2K15<>(SB), RODATA|NOPTR, $16

DATA denoiseSSE2K16<>+0x00(SB)/8, $0x1010101010101010
DATA denoiseSSE2K16<>+0x08(SB)/8, $0x1010101010101010
GLOBL denoiseSSE2K16<>(SB), RODATA|NOPTR, $16

DATA denoiseSSE2W1<>+0x00(SB)/8, $0x0001000100010001
DATA denoiseSSE2W1<>+0x08(SB)/8, $0x0001000100010001
GLOBL denoiseSSE2W1<>(SB), RODATA|NOPTR, $16

// func denoiserFilterYFirstPassSSE2(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)
TEXT ·denoiserFilterYFirstPassSSE2(SB), NOSPLIT, $0-72
	MOVQ	mc+0(FP), AX
	MOVQ	mcStride+8(FP), BX
	MOVQ	avg+16(FP), CX
	MOVQ	avgStride+24(FP), DX
	MOVQ	sig+32(FP), SI
	MOVQ	sigStride+40(FP), DI
	MOVQ	sumOut+64(FP), R8

	MOVQ	level1Adjustment+48(FP), X12
	PUNPCKLQDQ	X12, X12
	MOVQ	level1Threshold+56(FP), X13
	PUNPCKLQDQ	X13, X13
	MOVOU	denoiseSSE2K1<>(SB), X9
	MOVOU	denoiseSSE2K2<>(SB), X10
	MOVOU	denoiseSSE2K7<>(SB), X11
	MOVOU	denoiseSSE2K15<>(SB), X14
	MOVOU	denoiseSSE2K16<>(SB), X8
	PXOR	X15, X15
	PXOR	X7, X7
	MOVQ	$16, R9

denoise_y_sse2_loop:
	MOVOU	(SI), X0	// sig
	MOVOU	(AX), X1	// mc_running_avg

	MOVO	X1, X2
	PSUBUSB	X0, X2		// positive diff: max(mc - sig, 0)
	MOVO	X0, X3
	PSUBUSB	X1, X3		// negative diff magnitude: max(sig - mc, 0)
	MOVO	X2, X4
	POR	X3, X4		// absdiff
	PMINUB	X8, X4		// clamp to 16 so signed byte compares are valid

	MOVO	X4, X5
	PCMPGTB	X11, X5		// absdiff >= 8
	PAND	X9, X5
	MOVO	X4, X6
	PCMPGTB	X14, X6		// absdiff >= 16
	PAND	X10, X6
	PADDUSB	X6, X5
	PADDUSB	X12, X5		// level-adjusted candidate

	MOVO	X13, X6
	PCMPGTB	X4, X6		// absdiff < level1 threshold
	PAND	X6, X4		// raw absdiff candidate
	PANDN	X5, X6		// adjusted candidate when not raw
	POR	X4, X6		// final unsigned adjustment

	MOVO	X2, X4
	PCMPEQB	X15, X4
	PCMPEQB	X1, X1
	PXOR	X1, X4
	PAND	X6, X4		// positive adjustment

	MOVO	X3, X5
	PCMPEQB	X15, X5
	PCMPEQB	X1, X1
	PXOR	X1, X5
	PAND	X6, X5		// negative adjustment

	MOVO	X0, X2
	PADDUSB	X4, X2
	PSUBUSB	X5, X2
	MOVOU	X2, (CX)

	PADDSB	X4, X7
	PSUBSB	X5, X7

	ADDQ	BX, AX
	ADDQ	DX, CX
	ADDQ	DI, SI
	DECQ	R9
	JNZ	denoise_y_sse2_loop

	MOVO	X7, X0
	PUNPCKLBW	X0, X0
	PSRAW	$8, X0
	MOVO	X7, X1
	PUNPCKHBW	X1, X1
	PSRAW	$8, X1
	PADDW	X1, X0
	PMADDWL	denoiseSSE2W1<>(SB), X0
	PSHUFD	$0xee, X0, X1
	PADDL	X1, X0
	PSHUFD	$0x55, X0, X1
	PADDL	X1, X0
	MOVL	X0, (R8)
	RET

// func denoiserFilterUVFirstPassSSE2(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)
TEXT ·denoiserFilterUVFirstPassSSE2(SB), NOSPLIT, $0-72
	MOVQ	mc+0(FP), AX
	MOVQ	mcStride+8(FP), BX
	MOVQ	avg+16(FP), CX
	MOVQ	avgStride+24(FP), DX
	MOVQ	sig+32(FP), SI
	MOVQ	sigStride+40(FP), DI
	MOVQ	sumOut+64(FP), R8

	MOVQ	level1Adjustment+48(FP), X12
	PUNPCKLQDQ	X12, X12
	MOVQ	level1Threshold+56(FP), X13
	PUNPCKLQDQ	X13, X13
	MOVOU	denoiseSSE2K1<>(SB), X9
	MOVOU	denoiseSSE2K2<>(SB), X10
	MOVOU	denoiseSSE2K7<>(SB), X11
	MOVOU	denoiseSSE2K15<>(SB), X14
	MOVOU	denoiseSSE2K16<>(SB), X8
	PXOR	X15, X15
	PXOR	X7, X7
	MOVQ	$4, R9

denoise_uv_sse2_loop:
	MOVQ	(SI), X0
	MOVQ	(SI)(DI*1), X1
	PUNPCKLQDQ	X1, X0		// two 8-byte sig rows
	MOVQ	(AX), X1
	MOVQ	(AX)(BX*1), X2
	PUNPCKLQDQ	X2, X1		// two 8-byte mc rows

	MOVO	X1, X2
	PSUBUSB	X0, X2
	MOVO	X0, X3
	PSUBUSB	X1, X3
	MOVO	X2, X4
	POR	X3, X4
	PMINUB	X8, X4

	MOVO	X4, X5
	PCMPGTB	X11, X5
	PAND	X9, X5
	MOVO	X4, X6
	PCMPGTB	X14, X6
	PAND	X10, X6
	PADDUSB	X6, X5
	PADDUSB	X12, X5

	MOVO	X13, X6
	PCMPGTB	X4, X6
	PAND	X6, X4
	PANDN	X5, X6
	POR	X4, X6

	MOVO	X2, X4
	PCMPEQB	X15, X4
	PCMPEQB	X1, X1
	PXOR	X1, X4
	PAND	X6, X4

	MOVO	X3, X5
	PCMPEQB	X15, X5
	PCMPEQB	X1, X1
	PXOR	X1, X5
	PAND	X6, X5

	MOVO	X0, X2
	PADDUSB	X4, X2
	PSUBUSB	X5, X2
	MOVQ	X2, (CX)
	MOVHLPS	X2, X3
	MOVQ	X3, (CX)(DX*1)

	PADDSB	X4, X7
	PSUBSB	X5, X7

	ADDQ	BX, AX
	ADDQ	BX, AX
	ADDQ	DX, CX
	ADDQ	DX, CX
	ADDQ	DI, SI
	ADDQ	DI, SI
	DECQ	R9
	JNZ	denoise_uv_sse2_loop

	MOVO	X7, X0
	PUNPCKLBW	X0, X0
	PSRAW	$8, X0
	MOVO	X7, X1
	PUNPCKHBW	X1, X1
	PSRAW	$8, X1
	PADDW	X1, X0
	PMADDWL	denoiseSSE2W1<>(SB), X0
	PSHUFD	$0xee, X0, X1
	PADDL	X1, X0
	PSHUFD	$0x55, X0, X1
	PADDL	X1, X0
	MOVL	X0, (R8)
	RET
