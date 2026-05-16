//go:build amd64 && !purego

// VP9 AMD64 SSE2 sub-pixel variance bilinear filter kernels. Mirrors
// libvpx v1.16.0 vpx_dsp/x86/subpel_variance_sse2.asm (the bilinear
// pre-filter portion -- the variance reduction is shared with
// variance_amd64.s through finalVarianceFromBlock in Go).
//
// libvpx scales the bilinear filter from {128 - 16k, 16k} (FILTER_BITS=7)
// down to {8 - k, k} with shift=3 so the entire chain stays in uint8.
// Per row:
//
//   blend  = src0 * f0 + src1 * f1   (uint16 accumulator)
//   out_u8 = (blend + 4) >> 3        (rounding right shift)
//
// First pass advances along the row axis (pixel_step = 1) so the second
// tap reads src[x+1]. Second pass advances along the column axis
// (pixel_step = width) so the second tap reads src[(y+1)*w + x]; the
// intermediate buffer is tightly packed at width=W.
//
// The {x,y}-offset==0 fast path is handled in Go (just copies the
// source row).
//
// The filter taps arrive as uint64 with the [0, 8] value in the low
// byte; we broadcast it as int16 across all 8 lanes of an xmm via
// MOVQ + PSHUFLW + PSHUFD.

#include "textflag.h"

// Helper macro register convention:
//   X14 = zero
//   X15 = round bias (8 x int16 of 4)
//   X12 = f0 broadcast (8 x int16)
//   X13 = f1 broadcast (8 x int16)

// subpelVarFilter4SSE2 ABI ($0-56): w=4 single-tap-step bilinear.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte  (tightly packed, width=4)
//   pixelStep+24(FP) int    (1 for horiz, 4 for vert)
//   height+32(FP)    int
//   f0+40(FP)        uint64 (low byte used, value 8 - offset/16)
//   f1+48(FP)        uint64 (low byte used, value offset/16)
TEXT ·subpelVarFilter4SSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	pixelStep+24(FP), DX
	MOVQ	height+32(FP), R8

	// Broadcast f0 / f1 as int16 lanes.
	MOVQ	f0+40(FP), R9
	MOVQ	R9, X12
	PSHUFLW	$0, X12, X12
	PSHUFD	$0, X12, X12

	MOVQ	f1+48(FP), R9
	MOVQ	R9, X13
	PSHUFLW	$0, X13, X13
	PSHUFD	$0, X13, X13

	PXOR	X14, X14
	// round bias = 4 broadcast as int16
	MOVQ	$4, R9
	MOVQ	R9, X15
	PSHUFLW	$0, X15, X15
	PSHUFD	$0, X15, X15

	TESTQ	R8, R8
	JZ	w4_done

w4_loop:
	// Load 4 bytes from src and 4 bytes from src+pixelStep.
	MOVL	(AX), X0
	LEAQ	(AX)(DX*1), R10
	MOVL	(R10), X1

	// Widen to 4 int16 lanes (low half of xmm).
	PUNPCKLBW X14, X0
	PUNPCKLBW X14, X1

	PMULLW	X12, X0
	PMULLW	X13, X1
	PADDW	X1, X0
	PADDW	X15, X0
	PSRLW	$3, X0

	// Pack the 4 int16 lanes back to 4 bytes (lower 4 bytes of result).
	PACKUSWB X14, X0
	MOVL	X0, (CX)

	ADDQ	BX, AX
	ADDQ	$4, CX
	SUBQ	$1, R8
	JNZ	w4_loop

w4_done:
	RET

// subpelVarFilter8SSE2 ABI ($0-56): w=8.
TEXT ·subpelVarFilter8SSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	pixelStep+24(FP), DX
	MOVQ	height+32(FP), R8

	MOVQ	f0+40(FP), R9
	MOVQ	R9, X12
	PSHUFLW	$0, X12, X12
	PSHUFD	$0, X12, X12

	MOVQ	f1+48(FP), R9
	MOVQ	R9, X13
	PSHUFLW	$0, X13, X13
	PSHUFD	$0, X13, X13

	PXOR	X14, X14
	MOVQ	$4, R9
	MOVQ	R9, X15
	PSHUFLW	$0, X15, X15
	PSHUFD	$0, X15, X15

	TESTQ	R8, R8
	JZ	w8_done

w8_loop:
	MOVQ	(AX), X0
	LEAQ	(AX)(DX*1), R10
	MOVQ	(R10), X1

	PUNPCKLBW X14, X0
	PUNPCKLBW X14, X1

	PMULLW	X12, X0
	PMULLW	X13, X1
	PADDW	X1, X0
	PADDW	X15, X0
	PSRLW	$3, X0

	PACKUSWB X14, X0
	MOVQ	X0, (CX)

	ADDQ	BX, AX
	ADDQ	$8, CX
	SUBQ	$1, R8
	JNZ	w8_loop

w8_done:
	RET

// subpelVarFilter16SSE2 ABI ($0-56): w=16.
TEXT ·subpelVarFilter16SSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	pixelStep+24(FP), DX
	MOVQ	height+32(FP), R8

	MOVQ	f0+40(FP), R9
	MOVQ	R9, X12
	PSHUFLW	$0, X12, X12
	PSHUFD	$0, X12, X12

	MOVQ	f1+48(FP), R9
	MOVQ	R9, X13
	PSHUFLW	$0, X13, X13
	PSHUFD	$0, X13, X13

	PXOR	X14, X14
	MOVQ	$4, R9
	MOVQ	R9, X15
	PSHUFLW	$0, X15, X15
	PSHUFD	$0, X15, X15

	TESTQ	R8, R8
	JZ	w16_done

w16_loop:
	MOVOU	(AX), X0
	LEAQ	(AX)(DX*1), R10
	MOVOU	(R10), X1

	// low half
	MOVO	X0, X2
	MOVO	X1, X3
	PUNPCKLBW X14, X2
	PUNPCKLBW X14, X3
	PMULLW	X12, X2
	PMULLW	X13, X3
	PADDW	X3, X2
	PADDW	X15, X2
	PSRLW	$3, X2

	// high half
	PUNPCKHBW X14, X0
	PUNPCKHBW X14, X1
	PMULLW	X12, X0
	PMULLW	X13, X1
	PADDW	X1, X0
	PADDW	X15, X0
	PSRLW	$3, X0

	PACKUSWB X0, X2
	MOVOU	X2, (CX)

	ADDQ	BX, AX
	ADDQ	$16, CX
	SUBQ	$1, R8
	JNZ	w16_loop

w16_done:
	RET

// subpelVarFilter16ChunksSSE2 ABI ($0-64): chunks * 16 wide, height rows.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   pixelStep+24(FP) int     (1 for horiz, w=16*chunks for vert)
//   width+32(FP)     int     (16 * chunks)
//   height+40(FP)    int
//   f0+48(FP)        uint64
//   f1+56(FP)        uint64
TEXT ·subpelVarFilter16ChunksSSE2(SB), NOSPLIT, $0-64
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	pixelStep+24(FP), DX
	MOVQ	width+32(FP), R8
	MOVQ	height+40(FP), R9

	MOVQ	f0+48(FP), R10
	MOVQ	R10, X12
	PSHUFLW	$0, X12, X12
	PSHUFD	$0, X12, X12

	MOVQ	f1+56(FP), R10
	MOVQ	R10, X13
	PSHUFLW	$0, X13, X13
	PSHUFD	$0, X13, X13

	PXOR	X14, X14
	MOVQ	$4, R10
	MOVQ	R10, X15
	PSHUFLW	$0, X15, X15
	PSHUFD	$0, X15, X15

	TESTQ	R9, R9
	JZ	chunks_done

chunks_rowLoop:
	MOVQ	R8, R11       // columns remaining this row
	MOVQ	AX, R12       // row src cursor
	MOVQ	CX, R13       // row dst cursor

chunks_colLoop:
	MOVOU	(R12), X0
	LEAQ	(R12)(DX*1), R14
	MOVOU	(R14), X1

	MOVO	X0, X2
	MOVO	X1, X3
	PUNPCKLBW X14, X2
	PUNPCKLBW X14, X3
	PMULLW	X12, X2
	PMULLW	X13, X3
	PADDW	X3, X2
	PADDW	X15, X2
	PSRLW	$3, X2

	PUNPCKHBW X14, X0
	PUNPCKHBW X14, X1
	PMULLW	X12, X0
	PMULLW	X13, X1
	PADDW	X1, X0
	PADDW	X15, X0
	PSRLW	$3, X0

	PACKUSWB X0, X2
	MOVOU	X2, (R13)

	ADDQ	$16, R12
	ADDQ	$16, R13
	SUBQ	$16, R11
	JNZ	chunks_colLoop

	ADDQ	BX, AX
	ADDQ	R8, CX          // dst stride == width (tightly packed)
	SUBQ	$1, R9
	JNZ	chunks_rowLoop

chunks_done:
	RET
