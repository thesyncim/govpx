//go:build arm64 && !purego

// ARMv8 NEON 8x8-average quad kernel. Computes the four vpx_avg_8x8
// results of a 16x16 region (libvpx v1.16.0 vpx_dsp/arm/avg_neon.c
// vpx_avg_8x8_neon arithmetic, batched over the 2x2 sub-block layout
// fill_variance_8x8avg walks).
//
// Per 8-row half: each 16-byte row is pairwise-widened with
// UADDLP/UADALP into an 8x uint16 accumulator whose low four lanes
// hold the left 8x8 column-pair sums and high four lanes the right
// 8x8 sums (max 8 rows * 2 * 255 = 4080 per lane). UADDLP to 4x
// uint32 then leaves left = s0+s1, right = s2+s3. Rounding is the C
// reference's (sum + 32) >> 6.
//
// UADDLP/UADALP aren't natively in Go's arm64 assembler so they're
// emitted as raw WORD directives; encodings come from clang.

#include "textflag.h"

// avg8x8QuadNEON ABI ($0-24):
//   src+0(FP)    *byte (16x16 region, row 0)
//   stride+8(FP) int
//   out+16(FP)   *[4]int32 (k order: (0,0), (8,0), (0,8), (8,8))

TEXT ·avg8x8QuadNEON(SB), NOSPLIT, $0-24
	MOVD	src+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	out+16(FP), R2

	// Top half: rows 0..7 into V20.8H.
	VLD1	(R0), [V0.B16]
	ADD	R1, R0, R0
	WORD	$0x6e202814	// uaddlp v20.8h, v0.16b
	MOVD	$7, R3

topLoopA8Q:
	VLD1	(R0), [V1.B16]
	ADD	R1, R0, R0
	WORD	$0x6e206834	// uadalp v20.8h, v1.16b
	SUB	$1, R3, R3
	CBNZ	R3, topLoopA8Q

	// Bottom half: rows 8..15 into V21.8H.
	VLD1	(R0), [V2.B16]
	ADD	R1, R0, R0
	WORD	$0x6e202855	// uaddlp v21.8h, v2.16b
	MOVD	$7, R3

botLoopA8Q:
	VLD1	(R0), [V3.B16]
	ADD	R1, R0, R0
	WORD	$0x6e206875	// uadalp v21.8h, v3.16b
	SUB	$1, R3, R3
	CBNZ	R3, botLoopA8Q

	WORD	$0x6e602a96	// uaddlp v22.4s, v20.8h
	WORD	$0x6e602ab7	// uaddlp v23.4s, v21.8h

	// out[0] = (V22.S[0]+V22.S[1]+32)>>6, out[1] = (V22.S[2]+V22.S[3]+32)>>6
	VMOV	V22.S[0], R4
	VMOV	V22.S[1], R5
	ADD	R5, R4, R4
	ADD	$32, R4, R4
	LSR	$6, R4, R4
	MOVW	R4, (R2)
	VMOV	V22.S[2], R4
	VMOV	V22.S[3], R5
	ADD	R5, R4, R4
	ADD	$32, R4, R4
	LSR	$6, R4, R4
	MOVW	R4, 4(R2)

	// out[2], out[3] from the bottom half.
	VMOV	V23.S[0], R4
	VMOV	V23.S[1], R5
	ADD	R5, R4, R4
	ADD	$32, R4, R4
	LSR	$6, R4, R4
	MOVW	R4, 8(R2)
	VMOV	V23.S[2], R4
	VMOV	V23.S[3], R5
	ADD	R5, R4, R4
	ADD	$32, R4, R4
	LSR	$6, R4, R4
	MOVW	R4, 12(R2)
	RET
