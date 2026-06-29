//go:build amd64 && !purego

// VP9 AMD64 SSE2 inverse-transform add kernels. The DC-only kernels
// mirror libvpx v1.16.0 vpx_dsp/x86/inv_txfm_sse2.c
// vpx_idct{4,8,16,32}x{4,8,16,32}_1_add_sse2: the Go caller computes
// the source-shaped scalar DC prelude, then these kernels widen dest
// bytes, add the signed int16 broadcast, and clamp with PACKUSWB.
//
// The residual-row kernels consume a row-major buffer of int16
// residuals produced by the scalar 1-D butterflies
// (idct4/idct8/idct16/idct32/iadst*), apply the per-block
// normalization shift (ROUND_POWER_OF_TWO by 4 / 5 / 6 depending on
// block size), and fold the result into a uint8 dest buffer with
// clip_pixel_add via PACKUSWB's signed-to-unsigned saturation.
//
// Per row:
//   PADDW   half_const, residual   // rounding bias before shift
//   PSRAW   #shift,     residual
//   PUNPCKLBW xmm_zero, dest_bytes // widen 8 dest bytes to 8 int16
//   PADDW   residual, widened
//   PACKUSWB widened, widened      // saturate to [0,255]
//
// Mirrors the libvpx SSE2 path in vpx_dsp/x86/inv_txfm_sse2.c (the
// dct_const_round_shift_sse2 + recon_and_store helpers).

#include "textflag.h"

// idct4x4DcAddSSE2 applies a broadcast signed int16 add across a 4x4
// destination block, clamping each pixel to [0,255].
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct4x4DcAddSSE2(SB), NOSPLIT, $0-18
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVWQSX	a1+16(FP), CX

	MOVD	CX, X0
	PSHUFLW	$0, X0, X0
	PSHUFD	$0, X0, X0
	PXOR	X7, X7

	// Rows 0 + 1.
	MOVL	(AX), X1
	PUNPCKLBW X7, X1
	LEAQ	(AX)(BX*1), R8
	MOVL	(R8), X3
	PUNPCKLBW X7, X3
	PUNPCKLQDQ X3, X1
	PADDW	X0, X1
	PACKUSWB X7, X1
	MOVL	X1, (AX)
	PSRLO	$4, X1
	MOVL	X1, (R8)

	// Rows 2 + 3.
	LEAQ	(AX)(BX*2), R9
	MOVL	(R9), X1
	PUNPCKLBW X7, X1
	LEAQ	(R9)(BX*1), R10
	MOVL	(R10), X3
	PUNPCKLBW X7, X3
	PUNPCKLQDQ X3, X1
	PADDW	X0, X1
	PACKUSWB X7, X1
	MOVL	X1, (R9)
	PSRLO	$4, X1
	MOVL	X1, (R10)
	RET

// idct8x8DcAddSSE2 applies a broadcast signed int16 add across an 8x8
// destination block.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct8x8DcAddSSE2(SB), NOSPLIT, $0-18
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVWQSX	a1+16(FP), CX

	MOVD	CX, X6
	PSHUFLW	$0, X6, X6
	PSHUFD	$0, X6, X6
	PXOR	X7, X7

	MOVQ	$8, R8

dc8_loop:
	MOVQ	(AX), X1
	PUNPCKLBW X7, X1
	PADDW	X6, X1
	PACKUSWB X1, X1
	MOVQ	X1, (AX)

	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	dc8_loop
	RET

// idct16x16DcAddSSE2 applies a broadcast signed int16 add across a
// 16x16 destination block.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct16x16DcAddSSE2(SB), NOSPLIT, $0-18
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVWQSX	a1+16(FP), CX

	MOVD	CX, X6
	PSHUFLW	$0, X6, X6
	PSHUFD	$0, X6, X6
	PXOR	X7, X7

	MOVQ	$16, R8

dc16_loop:
	MOVOU	(AX), X1
	MOVOU	X1, X2
	PUNPCKLBW X7, X1
	PUNPCKHBW X7, X2
	PADDW	X6, X1
	PADDW	X6, X2
	PACKUSWB X2, X1
	MOVOU	X1, (AX)

	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	dc16_loop
	RET

// idct32x32DcAddSSE2 applies a broadcast signed int16 add across a
// 32x32 destination block.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct32x32DcAddSSE2(SB), NOSPLIT, $0-18
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVWQSX	a1+16(FP), CX

	MOVD	CX, X6
	PSHUFLW	$0, X6, X6
	PSHUFD	$0, X6, X6
	PXOR	X7, X7

	MOVQ	$32, R8

dc32_loop:
	MOVOU	(AX), X1
	MOVOU	X1, X2
	PUNPCKLBW X7, X1
	PUNPCKHBW X7, X2
	PADDW	X6, X1
	PADDW	X6, X2
	PACKUSWB X2, X1
	MOVOU	X1, (AX)

	MOVOU	16(AX), X3
	MOVOU	X3, X4
	PUNPCKLBW X7, X3
	PUNPCKHBW X7, X4
	PADDW	X6, X3
	PADDW	X6, X4
	PACKUSWB X4, X3
	MOVOU	X3, 16(AX)

	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	dc32_loop
	RET

// idctAddResidualRows4SSE2 adds 4 int16 residuals per row to a 4-byte
// dest row, with rounded shift right by 4. nRows rows are processed.
//
// ABI ($0-32):
//   dest+0(FP)         *byte
//   stride+8(FP)       int
//   residual+16(FP)    *int16
//   nRows+24(FP)       int
TEXT ·idctAddResidualRows4SSE2(SB), NOSPLIT, $0-32
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVQ	residual+16(FP), CX
	MOVQ	nRows+24(FP), R8

	PXOR	X7, X7

	// Broadcast round-bias = 8 (i.e. 1<<3 for shift=4) across 8 int16 lanes of X6.
	MOVQ	$0x0008000800080008, R9
	MOVQ	R9, X6
	PSHUFD	$0x44, X6, X6   // duplicate the 64 bits into high half: lanes 0..7

	TESTQ	R8, R8
	JZ	r4_done

r4_loop:
	// Load 4 int16 residuals (8 bytes) into low half of X0.
	MOVQ	(CX), X0
	PADDW	X6, X0
	PSRAW	$4, X0
	// Load 4 dest bytes into low DW of X1.
	MOVL	(AX), R9
	MOVQ	R9, X1
	PUNPCKLBW X7, X1        // widen to 8 int16
	PADDW	X0, X1
	PACKUSWB X1, X1         // saturate to uint8 (low 8 bytes valid)
	MOVD	X1, R9
	MOVL	R9, (AX)        // store 4 bytes

	ADDQ	$8, CX
	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	r4_loop

r4_done:
	RET

// idctAddResidualRows8SSE2 adds 8 int16 residuals per row with shift 5.
//
// ABI ($0-32):
//   dest+0(FP)         *byte
//   stride+8(FP)       int
//   residual+16(FP)    *int16
//   nRows+24(FP)       int
TEXT ·idctAddResidualRows8SSE2(SB), NOSPLIT, $0-32
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVQ	residual+16(FP), CX
	MOVQ	nRows+24(FP), R8

	PXOR	X7, X7

	// Round bias = 16 (1<<4) per int16 lane.
	MOVQ	$0x0010001000100010, R9
	MOVQ	R9, X6
	PSHUFD	$0x44, X6, X6

	TESTQ	R8, R8
	JZ	r8_done

r8_loop:
	MOVOU	(CX), X0        // 8 int16 = 16 bytes
	PADDW	X6, X0
	PSRAW	$5, X0
	MOVQ	(AX), X1        // 8 dest bytes (low 64 bits of X1)
	PUNPCKLBW X7, X1
	PADDW	X0, X1
	PACKUSWB X1, X1
	MOVQ	X1, (AX)

	ADDQ	$16, CX
	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	r8_loop

r8_done:
	RET

// idctAddResidualRows16SSE2 adds 16 int16 residuals per row with
// shift 6. Two SSE2 lanes per row (two 8-wide groups).
//
// ABI ($0-32):
//   dest+0(FP)         *byte
//   stride+8(FP)       int
//   residual+16(FP)    *int16
//   nRows+24(FP)       int
TEXT ·idctAddResidualRows16SSE2(SB), NOSPLIT, $0-32
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVQ	residual+16(FP), CX
	MOVQ	nRows+24(FP), R8

	PXOR	X7, X7

	// Round bias = 32 (1<<5) per int16 lane.
	MOVQ	$0x0020002000200020, R9
	MOVQ	R9, X6
	PSHUFD	$0x44, X6, X6

	TESTQ	R8, R8
	JZ	r16_done

r16_loop:
	// First 8 residuals + first 8 dest bytes.
	MOVOU	(CX), X0
	PADDW	X6, X0
	PSRAW	$6, X0
	MOVQ	(AX), X1
	PUNPCKLBW X7, X1
	PADDW	X0, X1
	// Second 8 residuals + second 8 dest bytes.
	MOVOU	16(CX), X2
	PADDW	X6, X2
	PSRAW	$6, X2
	MOVQ	8(AX), X3
	PUNPCKLBW X7, X3
	PADDW	X2, X3
	// Pack both halves into one 16-byte vector and store.
	PACKUSWB X3, X1         // X1 low = lanes 0..7 from X1, high = lanes 0..7 from X3
	MOVOU	X1, (AX)

	ADDQ	$32, CX
	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	r16_loop

r16_done:
	RET

// idctAddResidualRows32SSE2 adds 32 int16 residuals per row with
// shift 6. Four SSE2 groups per row.
//
// ABI ($0-32):
//   dest+0(FP)         *byte
//   stride+8(FP)       int
//   residual+16(FP)    *int16
//   nRows+24(FP)       int
TEXT ·idctAddResidualRows32SSE2(SB), NOSPLIT, $0-32
	MOVQ	dest+0(FP), AX
	MOVQ	stride+8(FP), BX
	MOVQ	residual+16(FP), CX
	MOVQ	nRows+24(FP), R8

	PXOR	X7, X7

	MOVQ	$0x0020002000200020, R9
	MOVQ	R9, X6
	PSHUFD	$0x44, X6, X6

	TESTQ	R8, R8
	JZ	r32_done

r32_loop:
	// Pair 1: residuals 0..7 + dest bytes 0..7.
	MOVOU	(CX), X0
	PADDW	X6, X0
	PSRAW	$6, X0
	MOVQ	(AX), X1
	PUNPCKLBW X7, X1
	PADDW	X0, X1
	// Pair 2: residuals 8..15 + dest bytes 8..15.
	MOVOU	16(CX), X2
	PADDW	X6, X2
	PSRAW	$6, X2
	MOVQ	8(AX), X3
	PUNPCKLBW X7, X3
	PADDW	X2, X3
	PACKUSWB X3, X1
	MOVOU	X1, (AX)
	// Pair 3: residuals 16..23 + dest bytes 16..23.
	MOVOU	32(CX), X0
	PADDW	X6, X0
	PSRAW	$6, X0
	MOVQ	16(AX), X1
	PUNPCKLBW X7, X1
	PADDW	X0, X1
	// Pair 4: residuals 24..31 + dest bytes 24..31.
	MOVOU	48(CX), X2
	PADDW	X6, X2
	PSRAW	$6, X2
	MOVQ	24(AX), X3
	PUNPCKLBW X7, X3
	PADDW	X2, X3
	PACKUSWB X3, X1
	MOVOU	X1, 16(AX)

	ADDQ	$64, CX
	ADDQ	BX, AX
	SUBQ	$1, R8
	JNZ	r32_loop

r32_done:
	RET
