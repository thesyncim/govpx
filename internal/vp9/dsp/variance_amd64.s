//go:build amd64 && !purego

// VP9 AMD64 SSE2 variance kernels. Mirrors libvpx v1.16.0
// vpx_dsp/x86/variance_sse2.c variance_kernel_sse2.
//
// Per 16 bytes of input:
//   abs_lo = psubusb(src, ref)          // max(0, src - ref)
//   abs_hi = psubusb(ref, src)          // max(0, ref - src)
//   absdiff = abs_lo | abs_hi           // |src - ref| in unsigned bytes
//   sum_pos += psadbw(abs_lo, 0)        // sum of positive diffs
//   sum_neg += psadbw(abs_hi, 0)        // sum of absolute negative diffs
//   widen absdiff -> int16 (low & high halves of the xmm)
//   sse += pmaddwd(diff, diff)          // sum of squared diffs (int32 lanes)
//
// At end-of-block, sum = sum_pos - sum_neg; final sum/sse are
// horizontally reduced to scalars and stored.
//
// PSADBW lanes: each 64-bit lane gets a uint16 sum in its low word.
// For widths up to 64 over 64 rows, max accumulated value per lane is
// 64 * 4 chunks * 255 = 65,280 across both halves (2 lanes total per
// PSADBW call) which fits comfortably in int32.
//
// PMADDWD lanes: 4 int32 lanes. Each gets one pair of int16*int16 per
// 16-byte input chunk → 2 pairs per chunk * up to 256 chunks (64x64)
// = 512 * (255*255) = 33,292,800 per lane, well within INT32_MAX.

#include "textflag.h"

// varianceBlock16xNSSE2 ABI ($0-56):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   sumOut+40(FP)    *int32
//   sseOut+48(FP)    *uint32
TEXT ·varianceBlock16xNSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8

	PXOR	X0, X0          // zero
	PXOR	X4, X4          // sum_pos accumulator
	PXOR	X5, X5          // sum_neg accumulator
	PXOR	X6, X6          // sse accumulator

	TESTQ	R8, R8
	JZ	w16_done

w16_loop:
	MOVOU	(AX), X1        // src
	MOVOU	(CX), X2        // ref

	MOVO	X1, X3
	PSUBUSB	X2, X3          // X3 = max(0, src - ref)
	PSUBUSB	X1, X2          // X2 = max(0, ref - src)

	// sum accumulation via PSADBW against zero
	MOVO	X3, X7
	PSADBW	X0, X7
	PADDQ	X7, X4
	MOVO	X2, X7
	PSADBW	X0, X7
	PADDQ	X7, X5

	// |src - ref| (mutually exclusive, OR safe)
	POR	X2, X3

	// widen absdiff to int16 (lo, hi) and PMADDWD with self
	MOVO	X3, X1
	PUNPCKLBW X0, X1        // int16 low half
	PUNPCKHBW X0, X3        // int16 high half
	PMADDWL	X1, X1
	PMADDWL	X3, X3
	PADDD	X1, X6
	PADDD	X3, X6

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	w16_loop

w16_done:
	// horizontal reduce sum_pos and sum_neg (each holds 2 uint16 sums in
	// low words of the 64-bit lanes); collapse to scalar int32
	MOVHLPS	X4, X1
	PADDQ	X1, X4
	MOVHLPS	X5, X1
	PADDQ	X1, X5
	MOVD	X4, R9          // sum_pos
	MOVD	X5, R10         // sum_neg
	SUBL	R10, R9         // sum = sum_pos - sum_neg

	// horizontal reduce sse (4 int32 lanes)
	PSHUFD	$0x4E, X6, X1   // swap high/low 64-bit halves
	PADDD	X1, X6
	PSHUFD	$0xB1, X6, X1   // swap high/low 32-bit within each 64-bit
	PADDD	X1, X6

	MOVQ	sumOut+40(FP), R11
	MOVQ	sseOut+48(FP), R12
	MOVL	R9, (R11)
	MOVD	X6, (R12)
	RET

// varianceBlock16ChunksSSE2 ABI ($0-64):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   chunks+40(FP)    int   (width / 16: 2 for w=32, 4 for w=64)
//   sumOut+48(FP)    *int32
//   sseOut+56(FP)    *uint32
TEXT ·varianceBlock16ChunksSSE2(SB), NOSPLIT, $0-64
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8
	MOVQ	chunks+40(FP), R9

	PXOR	X0, X0
	PXOR	X4, X4
	PXOR	X5, X5
	PXOR	X6, X6

	TESTQ	R8, R8
	JZ	wC_done

wC_rowLoop:
	MOVQ	R9, R13         // remaining chunks this row
	MOVQ	AX, R11         // row src cursor
	MOVQ	CX, R12         // row ref cursor

wC_chunkLoop:
	MOVOU	(R11), X1
	MOVOU	(R12), X2

	MOVO	X1, X3
	PSUBUSB	X2, X3          // X3 = max(0, src - ref)
	PSUBUSB	X1, X2          // X2 = max(0, ref - src)

	MOVO	X3, X7
	PSADBW	X0, X7
	PADDQ	X7, X4
	MOVO	X2, X7
	PSADBW	X0, X7
	PADDQ	X7, X5

	POR	X2, X3

	MOVO	X3, X1
	PUNPCKLBW X0, X1
	PUNPCKHBW X0, X3
	PMADDWL	X1, X1
	PMADDWL	X3, X3
	PADDD	X1, X6
	PADDD	X3, X6

	ADDQ	$16, R11
	ADDQ	$16, R12
	SUBQ	$1, R13
	JNZ	wC_chunkLoop

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	wC_rowLoop

wC_done:
	MOVHLPS	X4, X1
	PADDQ	X1, X4
	MOVHLPS	X5, X1
	PADDQ	X1, X5
	MOVD	X4, R10
	MOVD	X5, R13
	SUBL	R13, R10

	PSHUFD	$0x4E, X6, X1
	PADDD	X1, X6
	PSHUFD	$0xB1, X6, X1
	PADDD	X1, X6

	MOVQ	sumOut+48(FP), R11
	MOVQ	sseOut+56(FP), R12
	MOVL	R10, (R11)
	MOVD	X6, (R12)
	RET

// varianceBlock8xNSSE2 ABI ($0-56): 8 columns per row, h rows.
TEXT ·varianceBlock8xNSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8

	PXOR	X0, X0
	PXOR	X4, X4
	PXOR	X5, X5
	PXOR	X6, X6

	TESTQ	R8, R8
	JZ	w8_done

w8_loop:
	MOVQ	(AX), X1        // 8 bytes src, zero-extended into xmm
	MOVQ	(CX), X2        // 8 bytes ref

	MOVO	X1, X3
	PSUBUSB	X2, X3
	PSUBUSB	X1, X2

	MOVO	X3, X7
	PSADBW	X0, X7
	PADDQ	X7, X4
	MOVO	X2, X7
	PSADBW	X0, X7
	PADDQ	X7, X5

	POR	X2, X3
	// only low 8 bytes carry data; high half is zero
	PUNPCKLBW X0, X3
	PMADDWL	X3, X3
	PADDD	X3, X6

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R8
	JNZ	w8_loop

w8_done:
	MOVHLPS	X4, X1
	PADDQ	X1, X4
	MOVHLPS	X5, X1
	PADDQ	X1, X5
	MOVD	X4, R9
	MOVD	X5, R10
	SUBL	R10, R9

	PSHUFD	$0x4E, X6, X1
	PADDD	X1, X6
	PSHUFD	$0xB1, X6, X1
	PADDD	X1, X6

	MOVQ	sumOut+40(FP), R11
	MOVQ	sseOut+48(FP), R12
	MOVL	R9, (R11)
	MOVD	X6, (R12)
	RET

// varianceBlock4xNSSE2 ABI ($0-56): 4 columns per row, h (even) rows.
// Two rows are packed into one xmm via MOVD per row.
TEXT ·varianceBlock4xNSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8

	PXOR	X0, X0
	PXOR	X4, X4
	PXOR	X5, X5
	PXOR	X6, X6

	SHRQ	$1, R8          // pair count
	TESTQ	R8, R8
	JZ	w4_done

w4_loop:
	// Pack two src rows into X1 (low 8 bytes), two ref rows into X2.
	MOVL	(AX), X1
	MOVL	(CX), X2
	ADDQ	BX, AX
	ADDQ	DX, CX
	MOVL	(AX), X8
	MOVL	(CX), X9
	ADDQ	BX, AX
	ADDQ	DX, CX

	PUNPCKLLQ X8, X1        // src: [r0_4bytes | r1_4bytes | 0 | 0]
	PUNPCKLLQ X9, X2

	MOVO	X1, X3
	PSUBUSB	X2, X3
	PSUBUSB	X1, X2

	MOVO	X3, X7
	PSADBW	X0, X7
	PADDQ	X7, X4
	MOVO	X2, X7
	PSADBW	X0, X7
	PADDQ	X7, X5

	POR	X2, X3
	PUNPCKLBW X0, X3
	PMADDWL	X3, X3
	PADDD	X3, X6

	SUBQ	$1, R8
	JNZ	w4_loop

w4_done:
	MOVHLPS	X4, X1
	PADDQ	X1, X4
	MOVHLPS	X5, X1
	PADDQ	X1, X5
	MOVD	X4, R9
	MOVD	X5, R10
	SUBL	R10, R9

	PSHUFD	$0x4E, X6, X1
	PADDD	X1, X6
	PSHUFD	$0xB1, X6, X1
	PADDD	X1, X6

	MOVQ	sumOut+40(FP), R11
	MOVQ	sseOut+48(FP), R12
	MOVL	R9, (R11)
	MOVD	X6, (R12)
	RET
