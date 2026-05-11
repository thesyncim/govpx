//go:build amd64 && !purego

// SSE2 intra-prediction kernels. Mirrors libvpx v1.16.0
// vp8/common/x86/vp8_intrapred_sse2.asm per-mode primitives for the
// Y16x16 and UV8x8 whole-block predictors:
//
//   intraSum16SSE2         : sum of 16 bytes -> int32 (used by DC).
//   intraSum8SSE2          : sum of 8  bytes -> int32 (used by DC).
//   intraFill16x16SSE2     : broadcast a byte to a 16x16 stride-aware block.
//   intraFill8x8SSE2       : broadcast a byte to an  8x8  stride-aware block.
//   intraHPredict16x16SSE2 : per-row broadcast of left[y] across 16 cols.
//   intraHPredict8x8SSE2   : per-row broadcast of left[y] across  8 cols.
//   intraTMPredict16x16SSE2: per-row clip255(left[y] - topLeft + above[x]).
//   intraTMPredict8x8SSE2  : same, 8x8 form.
//
// Sum kernels use PSADBW against a zero register: |b - 0| collapses to
// b, and PSADBW horizontally sums 8 bytes to a 16-bit value in the low
// half of each 64-bit lane. For 16-byte inputs we PSADBW once on the
// full 128-bit register and reduce the two 16-bit fields with
// MOVHLPS+PADDD; for 8-byte inputs we MOVQ-load and PSADBW once.
//
// Byte-broadcast: load byte to a 16-bit lane via PINSRW, then
// PUNPCKLBW with self to spread to 16 byte lanes (16 copies of the byte
// in two halves of an XMM register), or to int16 lanes by PUNPCKLBW
// against zero. Go's amd64 assembler does not accept 64-bit
// IMUL-immediates, so we avoid the IMUL trick.
//
// V-prediction (per-row copy of `above`) is left to the pure-Go path —
// at 16/8-byte rows the compiler already emits a single MOVOU/STORE,
// so a SIMD wrapper would only add call overhead.

#include "textflag.h"

// intraSum16SSE2 ABI ($0-12):
//   src+0(FP) *byte
//   ret+8(FP) int32
TEXT ·intraSum16SSE2(SB), NOSPLIT, $0-12
	MOVQ	src+0(FP), AX
	PXOR	X1, X1
	MOVOU	(AX), X0
	PSADBW	X1, X0
	MOVHLPS	X0, X1
	PADDD	X1, X0
	MOVL	X0, ret+8(FP)
	RET

// intraSum8SSE2 ABI ($0-12):
//   src+0(FP) *byte
//   ret+8(FP) int32
TEXT ·intraSum8SSE2(SB), NOSPLIT, $0-12
	MOVQ	src+0(FP), AX
	PXOR	X1, X1
	MOVQ	(AX), X0
	PSADBW	X1, X0
	MOVL	X0, ret+8(FP)
	RET

// intraFill16x16SSE2 ABI ($0-17):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   val+16(FP)   byte
//
// Broadcast: PINSRW val|val into low 16-bit lane of X0, then
// PUNPCKLBW X0,X0 spreads to a 16-byte vector (the same byte filled
// across all 16 lanes). Then store 16 rows.
TEXT ·intraFill16x16SSE2(SB), NOSPLIT, $0-17
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVBQZX	val+16(FP), AX
	// Pack two copies of the byte into AX[15:0] = val|val.
	MOVQ	AX, CX
	SHLQ	$8, CX
	ORQ	CX, AX
	PINSRW	$0, AX, X0
	// Splat low 16 bits across the register: PUNPCKLBW X0,X0 copies
	// each byte; PSHUFLW + PSHUFD broadcasts the low word.
	PUNPCKLBW X0, X0
	PSHUFLW	$0, X0, X0
	PSHUFD	$0, X0, X0

	MOVQ	$16, CX
fill16_loop:
	MOVOU	X0, (DI)
	ADDQ	BX, DI
	SUBQ	$1, CX
	JNZ	fill16_loop
	RET

// intraFill8x8SSE2 ABI ($0-17):
TEXT ·intraFill8x8SSE2(SB), NOSPLIT, $0-17
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVBQZX	val+16(FP), AX
	MOVQ	AX, CX
	SHLQ	$8, CX
	ORQ	CX, AX
	PINSRW	$0, AX, X0
	PUNPCKLBW X0, X0
	PSHUFLW	$0, X0, X0

	MOVQ	$8, CX
fill8_loop:
	MOVQ	X0, (DI)
	ADDQ	BX, DI
	SUBQ	$1, CX
	JNZ	fill8_loop
	RET

// intraHPredict16x16SSE2 ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   left+16(FP)  *byte
TEXT ·intraHPredict16x16SSE2(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	left+16(FP), SI

	MOVQ	$16, CX
hpred16_loop:
	MOVBQZX	(SI), AX
	MOVQ	AX, R10
	SHLQ	$8, R10
	ORQ	R10, AX
	PINSRW	$0, AX, X0
	PUNPCKLBW X0, X0
	PSHUFLW	$0, X0, X0
	PSHUFD	$0, X0, X0
	MOVOU	X0, (DI)
	ADDQ	$1, SI
	ADDQ	BX, DI
	SUBQ	$1, CX
	JNZ	hpred16_loop
	RET

// intraHPredict8x8SSE2 ABI ($0-24):
TEXT ·intraHPredict8x8SSE2(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	left+16(FP), SI

	MOVQ	$8, CX
hpred8_loop:
	MOVBQZX	(SI), AX
	MOVQ	AX, R10
	SHLQ	$8, R10
	ORQ	R10, AX
	PINSRW	$0, AX, X0
	PUNPCKLBW X0, X0
	PSHUFLW	$0, X0, X0
	MOVQ	X0, (DI)
	ADDQ	$1, SI
	ADDQ	BX, DI
	SUBQ	$1, CX
	JNZ	hpred8_loop
	RET

// intraTMPredict16x16SSE2 ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// Plan: PUNPCK above bytes against zero into two int16x8 halves; broadcast
// topLeft as int16x8 once and subtract. Per row, broadcast left[y] as
// int16x8, add to both halves, PACKUSWB to bytes (PACKUSWB saturates
// signed int16 to unsigned uint8), store.
TEXT ·intraTMPredict16x16SSE2(SB), NOSPLIT, $0-33
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9
	MOVBQZX	topLeft+32(FP), AX

	// Broadcast topLeft into 8 int16 lanes -> X3.
	PINSRW	$0, AX, X3
	PSHUFLW	$0, X3, X3
	PSHUFD	$0, X3, X3

	// Load 16 above bytes; widen to two int16x8 in X1 (low 8) and X2 (high 8).
	PXOR	X4, X4
	MOVOU	(R8), X0
	MOVO	X0, X1
	PUNPCKLBW X4, X1
	MOVO	X0, X2
	PUNPCKHBW X4, X2

	// Subtract topLeft once.
	PSUBW	X3, X1
	PSUBW	X3, X2

	MOVQ	$16, CX
tm16_loop:
	MOVBQZX	(R9), AX
	PINSRW	$0, AX, X5
	PSHUFLW	$0, X5, X5
	PSHUFD	$0, X5, X5

	MOVO	X1, X6
	PADDW	X5, X6
	MOVO	X2, X7
	PADDW	X5, X7
	PACKUSWB X7, X6
	MOVOU	X6, (DI)

	ADDQ	$1, R9
	ADDQ	BX, DI
	SUBQ	$1, CX
	JNZ	tm16_loop
	RET

// intraTMPredict8x8SSE2 ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
TEXT ·intraTMPredict8x8SSE2(SB), NOSPLIT, $0-33
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9
	MOVBQZX	topLeft+32(FP), AX

	PINSRW	$0, AX, X3
	PSHUFLW	$0, X3, X3
	PSHUFD	$0, X3, X3

	PXOR	X4, X4
	MOVQ	(R8), X1
	PUNPCKLBW X4, X1
	PSUBW	X3, X1

	MOVQ	$8, CX
tm8_loop:
	MOVBQZX	(R9), AX
	PINSRW	$0, AX, X5
	PSHUFLW	$0, X5, X5
	PSHUFD	$0, X5, X5

	MOVO	X1, X6
	PADDW	X5, X6
	PACKUSWB X6, X6
	MOVQ	X6, (DI)

	ADDQ	$1, R9
	ADDQ	BX, DI
	SUBQ	$1, CX
	JNZ	tm8_loop
	RET
