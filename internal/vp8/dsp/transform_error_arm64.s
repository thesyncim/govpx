// ARMv8 NEON sum-of-squared-differences for a 4x4 coefficient block. Mirrors
// libvpx v1.16.0 vp8_block_error (vp8/encoder/encodemb.c) which computes
//
//   err = SUM_{i in [0,16)} (coeff[i] - dqcoeff[i])^2
//
// over a single 4x4 block (16 int16 lanes total = 32 bytes per buffer).
//
// Strategy: load both buffers as two int16x8 vectors each, take int16 SUB
// of pair-wise vectors, widen-multiply via SMULL/SMULL2 into int32x4
// accumulators (squared diffs are non-negative but the sources are signed),
// horizontally sum the four int32 lanes, then SADDLP + ADDP-d to widen the
// final reduction to int64 so the caller sees a sum-of-squares matching
// scalar int * int summation.
//
// SMULL/SMULL2/SADDLP/ADDP-scalar aren't natively in Go's arm64 assembler
// so they're emitted as raw WORD directives. Encodings derived from the
// ARMv8-A architecture reference manual.
//
// ABI ($0-24):
//   coeff+0(FP)   *[16]int16
//   dqcoeff+8(FP) *[16]int16
//   ret+16(FP)    int64

#include "textflag.h"

// func transformBlockErrorNEON(coeff *[16]int16, dqcoeff *[16]int16) int64
TEXT ·transformBlockErrorNEON(SB), NOSPLIT, $0-24
	MOVD	coeff+0(FP), R0
	MOVD	dqcoeff+8(FP), R1

	// Load 16 int16 = 32 bytes from each buffer.
	VLD1	(R0), [V0.H8, V1.H8]
	VLD1	(R1), [V2.H8, V3.H8]

	// Signed int16 subtract: Vk = Vk - V(k+2).
	VSUB	V2.H8, V0.H8, V0.H8
	VSUB	V3.H8, V1.H8, V1.H8

	// Square via widening signed multiply -> int32x4.
	WORD	$0x0e60c004	// smull  v4.4s, v0.4h, v0.4h
	WORD	$0x4e60c005	// smull2 v5.4s, v0.8h, v0.8h
	WORD	$0x0e61c026	// smull  v6.4s, v1.4h, v1.4h
	WORD	$0x4e61c027	// smull2 v7.4s, v1.8h, v1.8h

	// Reduce four int32x4 squared-diff accumulators down to one.
	VADD	V5.S4, V4.S4, V4.S4
	VADD	V7.S4, V6.S4, V6.S4
	VADD	V6.S4, V4.S4, V4.S4

	// Widen each adjacent int32 pair to int64 (int32x4 -> int64x2),
	// then ADDP across the two int64 lanes for a scalar in D4.
	WORD	$0x4ea02884	// saddlp v4.2d, v4.4s
	WORD	$0x5ef1b884	// addp   d4, v4.2d

	FMOVD	F4, ret+16(FP)
	RET
