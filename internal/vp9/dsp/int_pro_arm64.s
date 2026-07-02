//go:build arm64 && !purego

// ARMv8 NEON integer-projection kernels for the VP9 realtime
// variance-partition motion estimate. Mirrors libvpx v1.16.0
// vpx_dsp/arm/avg_neon.c:
//
//   vpx_int_pro_row_neon — accumulate |height| rows of a 16-byte-wide
//   strip into 8x uint16 lane pairs via UADDL/UADDL2 chains, then
//   normalise with SSHL by -((height >> 5) + 3). For the heights the
//   encoder uses (16/32/64) the shift equals the C reference's
//   division by (height >> 1) because the accumulators are
//   non-negative.
//
//   vpx_int_pro_col_neon — UADDLP/UADALP pairwise-accumulate each
//   16-byte chunk of a row into 8x uint16 lanes, then UADDLV reduces
//   to a scalar.
//
// Both kernels here batch the caller loops from
// vp9_int_pro_motion_estimation (vp9/encoder/vp9_mcomp.c:2264): the
// row kernel walks `strips` consecutive 16-column strips (libvpx call
// sites step ref_buf += 16 per call) and the col kernel walks `rows`
// consecutive rows (libvpx steps ref_buf += ref_stride per call),
// applying the caller's `>> norm_factor` on each row sum. Batching
// only fuses the surrounding Go loops — per-element arithmetic is
// identical to the scalar port.
//
// UADDL/UADDL2/UADDLP/UADALP/UADDLV/SSHL/DUP(general) aren't natively
// in Go's arm64 assembler so they're emitted as raw WORD directives;
// encodings come from clang.

#include "textflag.h"

// intProRowStripsNEON ABI ($0-40):
//   hbuf+0(FP)       *int16 (strips*16 entries)
//   ref+8(FP)        *byte  (strip 0, row 0)
//   refStride+16(FP) int
//   height+24(FP)    int    (16, 32 or 64; multiple of 4)
//   strips+32(FP)    int    (>= 1; each strip is 16 columns)

TEXT ·intProRowStripsNEON(SB), NOSPLIT, $0-40
	MOVD	hbuf+0(FP), R0
	MOVD	ref+8(FP), R1
	MOVD	refStride+16(FP), R2
	MOVD	height+24(FP), R3
	MOVD	strips+32(FP), R5

	// Negative normalisation shift: -((height >> 5) + 3).
	LSR	$5, R3, R4
	ADD	$3, R4, R4
	NEG	R4, R4
	WORD	$0x4e020c9f	// dup v31.8h, w4

stripLoopIPR:
	MOVD	R1, R6	// row cursor for this strip
	MOVD	R3, R7	// rows remaining

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16

rowLoopIPR:
	VLD1	(R6), [V0.B16]
	ADD	R2, R6, R6
	VLD1	(R6), [V1.B16]
	ADD	R2, R6, R6
	VLD1	(R6), [V2.B16]
	ADD	R2, R6, R6
	VLD1	(R6), [V3.B16]
	ADD	R2, R6, R6
	WORD	$0x2e210004	// uaddl  v4.8h, v0.8b,  v1.8b
	WORD	$0x6e210005	// uaddl2 v5.8h, v0.16b, v1.16b
	WORD	$0x2e230046	// uaddl  v6.8h, v2.8b,  v3.8b
	WORD	$0x6e230047	// uaddl2 v7.8h, v2.16b, v3.16b
	VADD	V4.H8, V20.H8, V20.H8
	VADD	V5.H8, V21.H8, V21.H8
	VADD	V6.H8, V20.H8, V20.H8
	VADD	V7.H8, V21.H8, V21.H8
	SUB	$4, R7, R7
	CBNZ	R7, rowLoopIPR

	WORD	$0x4e7f4694	// sshl v20.8h, v20.8h, v31.8h
	WORD	$0x4e7f46b5	// sshl v21.8h, v21.8h, v31.8h
	VST1.P	[V20.H8, V21.H8], 32(R0)

	ADD	$16, R1, R1
	SUB	$1, R5, R5
	CBNZ	R5, stripLoopIPR
	RET

// intProColsNEON ABI ($0-48):
//   vbuf+0(FP)       *int16 (rows entries)
//   ref+8(FP)        *byte  (row 0, column 0)
//   refStride+16(FP) int
//   width+24(FP)     int    (multiple of 16, <= 64)
//   rows+32(FP)      int    (>= 1)
//   shift+40(FP)     int    (caller norm_factor, applied per row sum)

TEXT ·intProColsNEON(SB), NOSPLIT, $0-48
	MOVD	vbuf+0(FP), R0
	MOVD	ref+8(FP), R1
	MOVD	refStride+16(FP), R2
	MOVD	width+24(FP), R3
	MOVD	rows+32(FP), R5
	MOVD	shift+40(FP), R4

rowLoopIPC:
	MOVD	R1, R6	// chunk cursor for this row
	MOVD	R3, R7	// bytes remaining

	VLD1.P	16(R6), [V0.B16]
	WORD	$0x6e202814	// uaddlp v20.8h, v0.16b
	SUB	$16, R7, R7
	CBZ	R7, sumDoneIPC

chunkLoopIPC:
	VLD1.P	16(R6), [V1.B16]
	WORD	$0x6e206834	// uadalp v20.8h, v1.16b
	SUB	$16, R7, R7
	CBNZ	R7, chunkLoopIPC

sumDoneIPC:
	WORD	$0x6e703a80	// uaddlv s0, v20.8h
	FMOVS	F0, R8
	ASR	R4, R8, R8
	MOVH	R8, (R0)

	ADD	$2, R0, R0
	ADD	R2, R1, R1
	SUB	$1, R5, R5
	CBNZ	R5, rowLoopIPC
	RET

// vectorVarNEON ABI ($0-32): libvpx v1.16.0 vpx_dsp/arm/avg_neon.c
// vpx_vector_var_neon arithmetic body. Returns the raw (sse, mean)
// pair; the Go wrapper applies `sse - ((mean * mean) >> (bwl + 2))`.
//   ref+0(FP)   *int16
//   src+8(FP)   *int16
//   width+16(FP) int (multiple of 8, >= 8)
//   sse+24(FP)  int32
//   mean+28(FP) int32
//
// The upstream kernel accumulates the running total in int16 lanes
// (safe for the int_pro domain); this port folds each iteration's
// diff into int32 lanes via SADALP instead, which is exact for all
// int16 inputs at the same instruction count.
TEXT ·vectorVarNEON(SB), NOSPLIT, $0-32
	MOVD	ref+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	width+16(FP), R2

	WORD	$0x6f00e414	// movi v20.2d, #0 (sse)
	WORD	$0x6f00e415	// movi v21.2d, #0 (total)

vvar_loop:
	WORD	$0x3cc10400	// ldr q0, [x0], #16
	WORD	$0x3cc10421	// ldr q1, [x1], #16
	WORD	$0x6e618402	// sub v2.8h, v0.8h, v1.8h
	WORD	$0x0e628054	// smlal  v20.4s, v2.4h, v2.4h
	WORD	$0x4e628054	// smlal2 v20.4s, v2.8h, v2.8h
	WORD	$0x4e606855	// sadalp v21.4s, v2.8h
	SUB	$8, R2, R2
	CBNZ	R2, vvar_loop

	VADDV	V20.S4, V20
	VADDV	V21.S4, V21
	WORD	$0x1e260280	// fmov w0, s20
	WORD	$0x1e2602a1	// fmov w1, s21
	MOVW	R0, sse+24(FP)
	MOVW	R1, mean+28(FP)
	RET
