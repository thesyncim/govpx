//go:build amd64 && !purego

// SSE2 temporal-filter apply kernel for VP8 ARNR. Mirrors the structure of
// libvpx v1.16.0 vp8/encoder/x86/temporal_filter_apply_sse2.asm, adapted to
// govpx's uint32 count buffer and guarded so the 16-bit square path preserves
// the scalar large-difference saturation semantics for strengths 0..6.

#include "textflag.h"

DATA temporalFilterSSE2Three<>+0x00(SB)/8, $0x0003000300030003
DATA temporalFilterSSE2Three<>+0x08(SB)/8, $0x0003000300030003
GLOBL temporalFilterSSE2Three<>(SB), RODATA|NOPTR, $16

DATA temporalFilterSSE2Sixteen<>+0x00(SB)/8, $0x0010001000100010
DATA temporalFilterSSE2Sixteen<>+0x08(SB)/8, $0x0010001000100010
GLOBL temporalFilterSSE2Sixteen<>(SB), RODATA|NOPTR, $16

// Input registers for TEMPORAL_FILTER_APPLY_16:
//   X0  source bytes, X2 predictor bytes
//   R11 accumulator uint32*, R12 count uint32*
// Constants:
//   X8 zero, X9 3, X10 16, X11 rounding, X12 filter_weight,
//   X13 max absdiff before scalar saturation, X14 shift count.
#define TEMPORAL_FILTER_APPLY_16 \
	MOVO X0, X1; \
	PUNPCKLBW X8, X0; \
	PUNPCKHBW X8, X1; \
	MOVO X2, X3; \
	PUNPCKLBW X8, X2; \
	PUNPCKHBW X8, X3; \
	PSUBW X2, X0; \
	PSUBW X3, X1; \
	MOVO X0, X4; \
	PSRAW $15, X4; \
	MOVO X0, X5; \
	PXOR X4, X5; \
	PSUBW X4, X5; \
	MOVO X5, X4; \
	PCMPGTW X13, X4; \
	PMULLW X0, X0; \
	PMULLW X9, X0; \
	PADDW X11, X0; \
	PSRLW X14, X0; \
	MOVO X4, X6; \
	PAND X10, X6; \
	PANDN X0, X4; \
	POR X6, X4; \
	MOVO X4, X0; \
	MOVO X1, X4; \
	PSRAW $15, X4; \
	MOVO X1, X5; \
	PXOR X4, X5; \
	PSUBW X4, X5; \
	MOVO X5, X4; \
	PCMPGTW X13, X4; \
	PMULLW X1, X1; \
	PMULLW X9, X1; \
	PADDW X11, X1; \
	PSRLW X14, X1; \
	MOVO X4, X6; \
	PAND X10, X6; \
	PANDN X1, X4; \
	POR X6, X4; \
	MOVO X4, X1; \
	MOVO X10, X4; \
	PSUBUSW X0, X4; \
	PMULLW X12, X4; \
	MOVO X4, X0; \
	MOVO X10, X5; \
	PSUBUSW X1, X5; \
	PMULLW X12, X5; \
	MOVO X5, X1; \
	MOVO X0, X4; \
	PUNPCKLWL X8, X4; \
	MOVOU (R12), X7; \
	PADDL X7, X4; \
	MOVOU X4, (R12); \
	MOVO X0, X6; \
	PUNPCKHWL X8, X6; \
	MOVOU 16(R12), X7; \
	PADDL X7, X6; \
	MOVOU X6, 16(R12); \
	MOVO X1, X4; \
	PUNPCKLWL X8, X4; \
	MOVOU 32(R12), X7; \
	PADDL X7, X4; \
	MOVOU X4, 32(R12); \
	MOVO X1, X6; \
	PUNPCKHWL X8, X6; \
	MOVOU 48(R12), X7; \
	PADDL X7, X6; \
	MOVOU X6, 48(R12); \
	PMULLW X0, X2; \
	PMULLW X1, X3; \
	MOVO X2, X4; \
	PUNPCKLWL X8, X4; \
	MOVOU (R11), X7; \
	PADDL X7, X4; \
	MOVOU X4, (R11); \
	MOVO X2, X6; \
	PUNPCKHWL X8, X6; \
	MOVOU 16(R11), X7; \
	PADDL X7, X6; \
	MOVOU X6, 16(R11); \
	MOVO X3, X4; \
	PUNPCKLWL X8, X4; \
	MOVOU 32(R11), X7; \
	PADDL X7, X4; \
	MOVOU X4, 32(R11); \
	MOVO X3, X6; \
	PUNPCKHWL X8, X6; \
	MOVOU 48(R11), X7; \
	PADDL X7, X6; \
	MOVOU X6, 48(R11)

// func applyTemporalFilterSSE2(src *byte, srcStride int, pred *byte, predStride int, blockSize int, strength uint64, rounding uint64, filterWeight uint64, maxDiff uint64, accumulator *uint32, count *uint32)
TEXT ·applyTemporalFilterSSE2(SB), NOSPLIT, $0-88
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	pred+16(FP), CX
	MOVQ	predStride+24(FP), DX
	MOVQ	blockSize+32(FP), R10
	MOVQ	accumulator+72(FP), R11
	MOVQ	count+80(FP), R12

	MOVOU	temporalFilterSSE2Three<>(SB), X9
	MOVOU	temporalFilterSSE2Sixteen<>(SB), X10
	MOVQ	rounding+48(FP), X11
	PUNPCKLQDQ	X11, X11
	MOVQ	filterWeight+56(FP), X12
	PUNPCKLQDQ	X12, X12
	MOVQ	maxDiff+64(FP), X13
	PUNPCKLQDQ	X13, X13
	MOVQ	strength+40(FP), X14
	PXOR	X8, X8

	CMPQ	R10, $8
	JE	temporal_filter_8

	MOVQ	$16, R9
temporal_filter_16_loop:
	MOVOU	(AX), X0
	MOVOU	(CX), X2
	TEMPORAL_FILTER_APPLY_16
	ADDQ	BX, AX
	ADDQ	DX, CX
	ADDQ	$64, R11
	ADDQ	$64, R12
	DECQ	R9
	JNZ	temporal_filter_16_loop
	RET

temporal_filter_8:
	MOVQ	$4, R9
temporal_filter_8_loop:
	MOVQ	(AX), X0
	MOVQ	(AX)(BX*1), X1
	PUNPCKLQDQ	X1, X0
	MOVQ	(CX), X2
	MOVQ	(CX)(DX*1), X3
	PUNPCKLQDQ	X3, X2
	TEMPORAL_FILTER_APPLY_16
	ADDQ	BX, AX
	ADDQ	BX, AX
	ADDQ	DX, CX
	ADDQ	DX, CX
	ADDQ	$64, R11
	ADDQ	$64, R12
	DECQ	R9
	JNZ	temporal_filter_8_loop
	RET
