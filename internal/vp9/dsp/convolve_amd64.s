//go:build amd64 && !purego

// VP9 AMD64 SSE2 8-tap convolve kernels. Mirrors libvpx v1.16.0
// vpx_dsp/x86/vpx_subpixel_8t_sse2.asm (the h>=8 width>=8 branches).
//
// Each output pixel is a dot product of 8 source pixels with the 8-tap
// int16 subpel filter:
//
//   acc = sum_{k=0..7} src[k] * filter[k]
//   out = clamp_uint8((acc + 64) >> 7)
//
// Strategy: PMADDWD on the int16-widened src window + int16 filter
// produces 4 int32 lanes that, when summed, equal `acc`. We process
// one output pixel per inner iteration (8 outputs per 8-column band)
// but each iteration touches only one MOVQ + one PMADDWD + a small
// PSHUFD+PADDD reduction — well under the per-pixel scalar cost.
//
// Filter loaded once into X14 (int16x8). Round bias 64 broadcast in X15.

#include "textflag.h"

// convolveHoriz8wSSE2 ABI ($0-56): horizontal 8-tap convolve, w cols
// (multiple of 8), h rows. src points to src - 3 (caller adjusted so
// src[0..7] is the kernel window for output column 0).
//
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   dstStride+24(FP) int
//   filter+32(FP)    *int16   (8 taps)
//   w+40(FP)         int
//   h+48(FP)         int
TEXT ·convolveHoriz8wSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	dstStride+24(FP), DX
	MOVQ	filter+32(FP), SI
	MOVQ	w+40(FP), R8
	MOVQ	h+48(FP), R9

	// Load 8 int16 filter taps into X14.
	MOVOU	(SI), X14

	// Broadcast round bias = 64 across 4 int32 lanes of X15.
	MOVQ	$64, R10
	MOVQ	R10, X15
	PSHUFD	$0, X15, X15

	PXOR	X13, X13          // zero

	TESTQ	R9, R9
	JZ	h_done

h_rowLoop:
	MOVQ	AX, R10           // src cursor (row)
	MOVQ	CX, R11           // dst cursor (row)
	MOVQ	R8, R12           // columns remaining this row

h_colLoop:
	// Each iteration produces 1 output pixel.
	MOVQ	(R10), X0         // load 8 bytes src[j..j+7] into low 8 lanes
	PUNPCKLBW X13, X0         // widen to 8 int16 (0..7)
	PMADDWL	X14, X0           // 4 int32 lanes: pair-sums of src*filter
	// Horizontal reduce 4 int32 lanes -> 1
	PSHUFD	$0x4E, X0, X1     // swap high/low 64-bit halves
	PADDL	X1, X0
	PSHUFD	$0xB1, X0, X1     // swap high/low 32-bit within each 64-bit
	PADDL	X1, X0
	// Round + arithmetic shift right by 7
	PADDL	X15, X0
	PSRAL	$7, X0
	// Pack int32 -> int16 -> uint8 with saturation
	PACKSSLW X0, X0
	PACKUSWB X0, X0
	// Store low byte (extract via GPR — SSE2 lacks direct MOVB from xmm).
	MOVD	X0, R13
	MOVB	R13, (R11)

	ADDQ	$1, R10
	ADDQ	$1, R11
	SUBQ	$1, R12
	JNZ	h_colLoop

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R9
	JNZ	h_rowLoop

h_done:
	RET

// convolveVert8wSSE2 ABI ($0-56): vertical 8-tap convolve, w cols
// (multiple of 8), h rows. src points to row 0 of the tap window
// (caller subtracted 3*srcStride for the centered convolve).
//
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   dstStride+24(FP) int
//   filter+32(FP)    *int16   (8 taps)
//   w+40(FP)         int
//   h+48(FP)         int
TEXT ·convolveVert8wSSE2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	dstStride+24(FP), DX
	MOVQ	filter+32(FP), SI
	MOVQ	w+40(FP), R8
	MOVQ	h+48(FP), R9

	MOVOU	(SI), X14

	MOVQ	$64, R10
	MOVQ	R10, X15
	PSHUFD	$0, X15, X15

	PXOR	X13, X13

	// Process 1 output pixel per inner iteration. Outer loop over rows,
	// inner over columns. Per pixel we load 8 bytes -- 1 per row from
	// 8 consecutive rows at the same column.
	//
	// Strategy: per (row, col) output, manually gather src[(row+k)*stride + col]
	// for k=0..7 into a single uint64 in a GPR, then move to xmm.
	// That's cheap relative to the scalar loop's 8 multiplications.

	TESTQ	R9, R9
	JZ	v_done

v_rowLoop:
	MOVQ	AX, R10           // src cursor for this output row (row 0 of tap window)
	MOVQ	CX, R11           // dst cursor for this output row
	MOVQ	R8, R12           // columns remaining

v_colLoop:
	// Gather 8 source bytes vertically: src[k*stride + 0] for k=0..7.
	// Use BP and R13/R14/R15/SI? We've used SI for filter. Use R14, R15
	// scratch and re-derive filter pointer if needed. Actually filter is
	// already in X14, so SI is free after load. But we keep it pointed to
	// filter for safety; use R13, R14, R15 as scratch.
	//
	// Build the 8 bytes via repeated MOVB into a stack temp -- simpler
	// and lets PMADDWD operate. Stack frame zero so we use a single
	// local: stash 8 bytes at -8(SP).
	MOVQ	R10, R13
	MOVBQZX (R13), R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$8, R15
	ORQ	R15, R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$16, R15
	ORQ	R15, R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$24, R15
	ORQ	R15, R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$32, R15
	ORQ	R15, R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$40, R15
	ORQ	R15, R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$48, R15
	ORQ	R15, R14
	ADDQ	BX, R13
	MOVBQZX (R13), R15
	SHLQ	$56, R15
	ORQ	R15, R14

	MOVQ	R14, X0
	PUNPCKLBW X13, X0
	PMADDWL	X14, X0
	PSHUFD	$0x4E, X0, X1
	PADDL	X1, X0
	PSHUFD	$0xB1, X0, X1
	PADDL	X1, X0
	PADDL	X15, X0
	PSRAL	$7, X0
	PACKSSLW X0, X0
	PACKUSWB X0, X0
	MOVD	X0, R14
	MOVB	R14, (R11)

	ADDQ	$1, R10
	ADDQ	$1, R11
	SUBQ	$1, R12
	JNZ	v_colLoop

	ADDQ	BX, AX
	ADDQ	DX, CX
	SUBQ	$1, R9
	JNZ	v_rowLoop

v_done:
	RET
