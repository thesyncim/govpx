// SSE2 batched port of libvpx v1.16.0 vp8/encoder/x86/dct_sse2.asm
// vp8_short_fdct4x4_sse2 wrapped in an outer assembly loop so one
// Go<->asm transition processes up to `count` 4x4 blocks at block
// stride 4. Same per-block arithmetic as forwardDCT4x4SSE2; the
// constants and stride-2-bytes math match the libvpx reference, so
// output is byte-identical to the scalar reference on the encoder's
// residual range.

#include "textflag.h"

// Per-block kernel constants live in the same RODATA pool as the
// non-batch kernel (dct_simd_amd64.s); the linker dedupes by symbol
// name so re-declaring here is unnecessary.

// forwardDCT4x4BatchSSE2 ABI ($0-24):
//   input+0(FP)   *int16 (count contiguous 4x4 blocks, block stride 4)
//   output+8(FP)  *int16 (count contiguous 4x4 blocks, block stride 4)
//   count+16(FP)  int    (block count, must be > 0)
TEXT ·forwardDCT4x4BatchSSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	output+8(FP), DI
	MOVQ	count+16(FP), CX

batchLoop:
	// Load 4 rows of 4 int16 (stride = 4 * sizeof(int16) = 8 bytes).
	MOVQ	(SI), X0                // row 0
	MOVQ	8(SI), X2               // row 1
	MOVQ	16(SI), X1              // row 2
	MOVQ	24(SI), X3              // row 3

	PUNPCKLQDQ	X2, X0
	PUNPCKLQDQ	X3, X1

	MOVO	X0, X2
	PUNPCKLLQ	X1, X0
	PUNPCKHLQ	X1, X2
	MOVO	X0, X1
	PUNPCKLLQ	X2, X0
	PSHUFHW	$0xb1, X1, X1
	PSHUFHW	$0xb1, X2, X2

	PUNPCKHLQ	X2, X1
	MOVO	X0, X3
	PADDW	X1, X0
	PSUBW	X1, X3
	PSLLW	$3, X0
	PSLLW	$3, X3

	MOVO	X0, X1
	PMADDWL	fdct4x4_mult_add(SB), X0
	PMADDWL	fdct4x4_mult_sub(SB), X1
	MOVO	X3, X4
	PMADDWL	fdct4x4_5352_2217(SB), X3
	PMADDWL	fdct4x4_2217_neg5352(SB), X4

	PADDL	fdct4x4_14500(SB), X3
	PADDL	fdct4x4_7500(SB), X4
	PSRAL	$12, X3
	PSRAL	$12, X4

	PACKSSLW	X1, X0
	PACKSSLW	X4, X3

	MOVO	X0, X2
	PUNPCKLQDQ	X3, X0
	PUNPCKHQDQ	X3, X2

	MOVO	X0, X3
	PUNPCKLWL	X2, X0
	PUNPCKHWL	X2, X3
	MOVO	X0, X2
	PUNPCKLWL	X3, X0
	PUNPCKHWL	X3, X2

	PSHUFD	$0x4e, X2, X2
	MOVO	X0, X3
	PADDW	X2, X0
	PSUBW	X2, X3

	PSHUFD	$0xd8, X0, X0
	MOVO	X3, X2
	PSHUFD	$0xd8, X3, X3
	PSHUFLW	$0xd8, X0, X0
	PSHUFLW	$0xd8, X3, X3
	PSHUFHW	$0xd8, X0, X0
	PSHUFHW	$0xd8, X3, X3

	MOVO	X0, X1
	PMADDWL	fdct4x4_mult_add(SB), X0
	PMADDWL	fdct4x4_mult_sub(SB), X1

	PXOR	X4, X4
	PADDL	fdct4x4_7(SB), X0
	PADDL	fdct4x4_7(SB), X1
	PCMPEQW	X4, X2
	PSRAL	$4, X0
	PSRAL	$4, X1
	PANDN	fdct4x4_cmp_mask(SB), X2

	MOVO	X3, X4
	PMADDWL	fdct4x4_5352_2217(SB), X3
	PMADDWL	fdct4x4_2217_neg5352(SB), X4
	PADDL	fdct4x4_12000(SB), X3
	PADDL	fdct4x4_51000(SB), X4
	PACKSSLW	X1, X0
	PSRAL	$16, X3
	PSRAL	$16, X4

	PACKSSLW	X4, X3
	MOVO	X0, X1
	PADDW	X2, X3
	PUNPCKLQDQ	X3, X0
	PUNPCKHQDQ	X3, X1

	MOVOU	X0, 0(DI)
	MOVOU	X1, 16(DI)

	ADDQ	$32, SI
	ADDQ	$32, DI
	DECQ	CX
	JNZ	batchLoop
	RET
