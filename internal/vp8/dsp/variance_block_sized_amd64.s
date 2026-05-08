// SSE2 variance kernels for non-16x16 block sizes. Mirrors libvpx
// v1.16.0 vpx_dsp/x86/variance_sse2.c variance_kernel_sse2 plus the
// non-16-wide vpx_get_mb_ss-style reductions. Each kernel returns
// (sum, sse) of (src - ref) over the whole block:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// Per row we unpack bytes to int16 lanes (PUNPCKLBW with a zero
// register), PSUBW the diffs, PADDW into the sum accumulator and
// PMADDWD into the sse accumulator. After the loop we sign-extend the
// 16-bit sum lanes to int32 (PCMPGTW + PUNPCK) and horizontally
// reduce both accumulators to scalars. The 16x16 path keeps its own
// kernel in variance_block_amd64.s — these helpers cover everything
// else.
//
// Calling convention (ABI0, $0-56):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   sumOut+40(FP)    *int32
//   sseOut+48(FP)    *uint32

#include "textflag.h"

// reduce_sum_sse expects:
//   X6 = 16-bit sum accumulator (8 int16 lanes)
//   X7 = 32-bit sse accumulator (4 int32 lanes)
// and stores reduced scalars to (DI) and (SI).
#define REDUCE_AND_STORE \
	PXOR	X4, X4 \
	PCMPGTW	X6, X4 \
	MOVOU	X6, X5 \
	PUNPCKLWL	X4, X5 \
	PUNPCKHWL	X4, X6 \
	PADDL	X5, X6 \
	PSHUFD	$0xee, X6, X3 \
	PADDL	X3, X6 \
	PSHUFD	$0x55, X6, X3 \
	PADDL	X3, X6 \
	MOVL	X6, (DI) \
	PSHUFD	$0xee, X7, X3 \
	PADDL	X3, X7 \
	PSHUFD	$0x55, X7, X3 \
	PADDL	X3, X7 \
	MOVL	X7, (SI)

// varianceBlock16xNSSE2: 16 columns, height rows. Same kernel as the
// 16x16 specialisation but with a runtime row count.
TEXT ·varianceBlock16xNSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8
	MOVQ	sumOut+40(FP), DI
	MOVQ	sseOut+48(FP), SI

	PXOR	X0, X0
	PXOR	X6, X6
	PXOR	X7, X7

	TESTQ	R8, R8
	JZ	w16_done

w16_loop:
	MOVOU	(AX), X1
	MOVOU	(CX), X2

	MOVOU	X1, X3
	PUNPCKLBW	X0, X3
	PUNPCKHBW	X0, X1

	MOVOU	X2, X4
	PUNPCKLBW	X0, X4
	PUNPCKHBW	X0, X2

	PSUBW	X4, X3
	PSUBW	X2, X1

	PADDW	X3, X6
	PADDW	X1, X6

	PMADDWL	X3, X3
	PMADDWL	X1, X1
	PADDL	X3, X7
	PADDL	X1, X7

	ADDQ	BX, AX
	ADDQ	DX, CX
	DECQ	R8
	JNZ	w16_loop

w16_done:
	REDUCE_AND_STORE
	RET

// varianceBlock8xNSSE2: 8 columns, height rows. We load 8 src bytes
// + 8 ref bytes via MOVQ into the low halves of X1/X2, unpack to 8
// int16 lanes (the upper half stays zero from MOVQ semantics), then
// PSUBW + PADDW (sum) + PMADDWL (sse).
TEXT ·varianceBlock8xNSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8
	MOVQ	sumOut+40(FP), DI
	MOVQ	sseOut+48(FP), SI

	PXOR	X0, X0
	PXOR	X6, X6
	PXOR	X7, X7

	TESTQ	R8, R8
	JZ	w8_done

w8_loop:
	// MOVQ (mem) -> XMM zeroes the upper 64 bits.
	MOVQ	(AX), X1
	MOVQ	(CX), X2

	PUNPCKLBW	X0, X1	// X1 = 8 uint16 lanes from src
	PUNPCKLBW	X0, X2	// X2 = 8 uint16 lanes from ref

	PSUBW	X2, X1		// X1 = 8 int16 diffs

	PADDW	X1, X6
	PMADDWL	X1, X1
	PADDL	X1, X7

	ADDQ	BX, AX
	ADDQ	DX, CX
	DECQ	R8
	JNZ	w8_loop

w8_done:
	REDUCE_AND_STORE
	RET

// varianceBlock4xNSSE2: 4 columns, height rows. We pair two rows of 4
// bytes into one MOVQ load (lo word = row 0, hi word = row 1) so that
// PUNPCKLBW yields 8 int16 lanes covering both rows. The encoder
// always passes an even height for the 4-wide variants used by the
// VP8 picker (4x4 / 4x8); a tail handles odd heights for completeness.
TEXT ·varianceBlock4xNSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8
	MOVQ	sumOut+40(FP), DI
	MOVQ	sseOut+48(FP), SI

	PXOR	X0, X0
	PXOR	X6, X6
	PXOR	X7, X7

	TESTQ	R8, R8
	JZ	w4_done

	MOVQ	R8, R9
	SHRQ	$1, R9		// R9 = pair count = height / 2
	JZ	w4_tail

w4_pair_loop:
	// Load 4 bytes from src row 0 -> X1 lo word.
	MOVL	(AX), X1
	// Load 4 bytes from src row 1 -> X3 lo word, then PUNPCKLDQ to
	// merge: X1 = [row0[0..3], row1[0..3]] in 8 bytes.
	MOVL	(AX)(BX*1), X3
	PUNPCKLLQ	X3, X1

	MOVL	(CX), X2
	MOVL	(CX)(DX*1), X4
	PUNPCKLLQ	X4, X2

	PUNPCKLBW	X0, X1
	PUNPCKLBW	X0, X2

	PSUBW	X2, X1

	PADDW	X1, X6
	PMADDWL	X1, X1
	PADDL	X1, X7

	// Advance two rows.
	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	DECQ	R9
	JNZ	w4_pair_loop

w4_tail:
	TESTQ	$1, R8
	JZ	w4_done

	MOVL	(AX), X1
	MOVL	(CX), X2

	PUNPCKLBW	X0, X1
	PUNPCKLBW	X0, X2

	PSUBW	X2, X1

	PADDW	X1, X6
	PMADDWL	X1, X1
	PADDL	X1, X7

w4_done:
	REDUCE_AND_STORE
	RET
