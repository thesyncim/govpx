//go:build arm64 && !purego

// ARMv8 NEON 16x16 variance block. Mirrors libvpx v1.16.0
// vpx_dsp/arm/variance_neon.c variance_neon_w16:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// Per row: load 16 src + 16 ref bytes, USUBL the byte pairs into two
// vectors of 8 int16 diffs, accumulate those diffs into two int16 sum
// vectors, and SMLAL/SMLAL2 the diff^2 into two int32 SSE accumulators.
// This mirrors libvpx's variance_16xh_neon dependency shape and defers
// horizontal reduction until the end.
//
// USUBL/SADDLV/SMLAL aren't natively in Go's arm64 assembler, so
// they're emitted as raw WORD directives; encodings come from clang.
//
// Calling convention (ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   sumOut+32(FP)    *int32
//   sseOut+40(FP)    *uint32
//
// Registers:
//   R0=src R1=srcStride R2=ref R3=refStride R4=sumOut R5=sseOut
//   R6=loop counter
//   V0     loaded src bytes
//   V1     loaded ref bytes
//   V2     diff lo (8 int16 lanes = src[0..7] - ref[0..7])
//   V3     diff hi (8 int16 lanes = src[8..15] - ref[8..15])
//   V20/V21 sum accumulators (8 int16 lanes each)
//   V22/V23 sse accumulators (4 int32 lanes each)

#include "textflag.h"

TEXT ·varianceBlock16x16NEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	sumOut+32(FP), R4
	MOVD	sseOut+40(FP), R5

	// Zero the sum (V20/V21) and sse (V22/V23) accumulators.
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	MOVD	$16, R6

loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]

	// V2.8H = src[0..7] - ref[0..7] as int16 (signed widening subtract).
	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b
	// V3.8H = src[8..15] - ref[8..15] as int16.
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	// Sum: keep the low/high int16 lanes independent until the final
	// horizontal reduction, matching libvpx variance_16xh_neon.
	WORD	$0x4e628694	// add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5	// add v21.8h, v21.8h, v3.8h

	// SSE: square diffs and accumulate into int32 sse accumulator.
	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop

	// Horizontal-reduce sum and SSE to scalars.
	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22	// V22.S[0] = sum of V22 lanes

	// Store the low 32 bits of V20 to *sumOut, V22 to *sseOut.
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)

	RET
