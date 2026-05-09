// SSE2 sum-of-squared-differences for a 4x4 coefficient block. Mirrors
// libvpx v1.16.0 vp8_block_error_sse2 (vp8/encoder/x86/block_error_sse2.asm)
// which computes
//
//   err = SUM_{i in [0,16)} (coeff[i] - dqcoeff[i])^2
//
// over a single 4x4 block (16 int16 lanes total = 32 bytes per buffer).
//
// Per-lane diff is int16; squared diff fits in int32. PMADDWD multiplies
// adjacent int16 pairs and sums them into a single int32 lane, so feeding
// it (diff,diff) yields diff^2+diff^2 partial sums which add cleanly into
// the four int32 lanes of XMM. After both 16-byte halves are accumulated,
// punpckldq/punpckhdq with a zero register zero-extends int32 -> int64 so
// the final scalar matches int * int (= int64) accumulation.
//
// The input buffers come from Go arrays so 16-byte alignment is not
// guaranteed; use MOVOU for the loads.
//
// ABI ($0-24):
//   coeff+0(FP)   *[16]int16
//   dqcoeff+8(FP) *[16]int16
//   ret+16(FP)    int64

#include "textflag.h"

// func transformBlockErrorSSE2(coeff *[16]int16, dqcoeff *[16]int16) int64
TEXT ·transformBlockErrorSSE2(SB), NOSPLIT, $0-24
	MOVQ	coeff+0(FP), AX
	MOVQ	dqcoeff+8(FP), CX

	MOVOU	(AX), X0
	MOVOU	(CX), X1
	MOVOU	16(AX), X2
	MOVOU	16(CX), X3

	PSUBW	X1, X0
	PSUBW	X3, X2

	// PMADDWD X0, X0 — Go's amd64 assembler does not register the
	// non-VEX SSE2 form (only VPMADDWD). Encode the SSE2 byte sequence
	// directly: 66 0F F5 /r with ModR/M selecting (xmm0, xmm0) = 0xC0
	// and (xmm2, xmm2) = 0xD2.
	BYTE $0x66; BYTE $0x0f; BYTE $0xf5; BYTE $0xc0  // PMADDWD X0, X0
	BYTE $0x66; BYTE $0x0f; BYTE $0xf5; BYTE $0xd2  // PMADDWD X2, X2

	PADDD	X2, X0

	// Reduce four int32 lanes to a single int64 in the low qword of X0.
	// Mirrors libvpx's pattern: zero-extend each int32 to int64 by
	// interleaving with a zero register, sum the two halves, then fold
	// the high half into the low half.
	PXOR	X5, X5
	MOVO	X0, X1
	PUNPCKLLQ	X5, X0	// [s0|0|s1|0]
	PUNPCKHLQ	X5, X1	// [s2|0|s3|0]
	PADDD	X1, X0	// [s0+s2|0|s1+s3|0]

	MOVO	X0, X1
	PSRLDQ	$8, X0	// [s1+s3|0|0|0]
	PADDD	X1, X0	// X0[lo64] = (s0+s2) + (s1+s3)

	MOVQ	X0, ret+16(FP)
	RET
