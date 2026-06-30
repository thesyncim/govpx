//go:build arm64 && !purego

// ARMv8 NEON VP9 block subtract kernels. Mirrors libvpx v1.16.0
// vpx_dsp/arm/subtract_neon.c, with a fused scalar OR reduction of the
// int16 residual chunks so encoder callers can skip zero-residual blocks.
//
// Per row, USUBL/USUBL2 widen byte differences to int16 residual lanes:
//   residual = (uint16)src - (uint16)pred, reinterpreted as int16.
// The kernels store contiguous row-major int16 output. USUBL/USUBL2 are
// emitted as raw WORD directives because Go's arm64 assembler does not expose
// those mnemonics directly.

#include "textflag.h"

// subtractBlock16xNNEON ABI ($0-56):
//   src+0(FP)         *byte
//   srcStride+8(FP)   int
//   pred+16(FP)       *byte
//   predStride+24(FP) int
//   out+32(FP)        *int16
//   rows+40(FP)       int
//   ret+48(FP)        uint64
TEXT ·subtractBlock16xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	out+32(FP), R4
	MOVD	rows+40(FP), R5
	MOVD	$0, R11
	CBZ	R5, sub16_done

sub16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x2e212002	// usubl  v2.8h, v0.8b,  v1.8b
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	VST1	[V2.H8], (R4)
	ADD	$16, R4, R8
	VST1	[V3.H8], (R8)

	VMOV	V2.D[0], R8
	VMOV	V2.D[1], R9
	ORR	R8, R11, R11
	ORR	R9, R11, R11
	VMOV	V3.D[0], R8
	VMOV	V3.D[1], R9
	ORR	R8, R11, R11
	ORR	R9, R11, R11

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	$32, R4, R4
	SUB	$1, R5, R5
	CBNZ	R5, sub16_loop

sub16_done:
	MOVD	R11, ret+48(FP)
	RET

// subtractBlock16ChunksNEON ABI ($0-64):
//   chunks is width / 16; govpx currently calls this with chunks=2 for 32x32.
TEXT ·subtractBlock16ChunksNEON(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	out+32(FP), R4
	MOVD	rows+40(FP), R5
	MOVD	chunks+48(FP), R6
	MOVD	$0, R11
	CBZ	R5, sub16c_done

sub16c_row:
	MOVD	R6, R7
	MOVD	R0, R8
	MOVD	R2, R9
	MOVD	R4, R10

sub16c_chunk:
	VLD1	(R8), [V0.B16]
	VLD1	(R9), [V1.B16]
	WORD	$0x2e212002	// usubl  v2.8h, v0.8b,  v1.8b
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	VST1	[V2.H8], (R10)
	ADD	$16, R10, R12
	VST1	[V3.H8], (R12)

	VMOV	V2.D[0], R12
	VMOV	V2.D[1], R13
	ORR	R12, R11, R11
	ORR	R13, R11, R11
	VMOV	V3.D[0], R12
	VMOV	V3.D[1], R13
	ORR	R12, R11, R11
	ORR	R13, R11, R11

	ADD	$16, R8, R8
	ADD	$16, R9, R9
	ADD	$32, R10, R10
	SUB	$1, R7, R7
	CBNZ	R7, sub16c_chunk

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	MOVD	R10, R4
	SUB	$1, R5, R5
	CBNZ	R5, sub16c_row

sub16c_done:
	MOVD	R11, ret+56(FP)
	RET

// subtractBlock8xNNEON ABI ($0-56).
TEXT ·subtractBlock8xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	out+32(FP), R4
	MOVD	rows+40(FP), R5
	MOVD	$0, R11
	CBZ	R5, sub8_done

sub8_loop:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x2e212002	// usubl v2.8h, v0.8b, v1.8b

	VST1	[V2.H8], (R4)
	VMOV	V2.D[0], R8
	VMOV	V2.D[1], R9
	ORR	R8, R11, R11
	ORR	R9, R11, R11

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	$16, R4, R4
	SUB	$1, R5, R5
	CBNZ	R5, sub8_loop

sub8_done:
	MOVD	R11, ret+48(FP)
	RET

// subtractBlock4xNNEON ABI ($0-56).
TEXT ·subtractBlock4xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	out+32(FP), R4
	MOVD	rows+40(FP), R5
	MOVD	$0, R11
	CBZ	R5, sub4_done

sub4_loop:
	FMOVS	(R0), F0
	FMOVS	(R2), F1
	WORD	$0x2e212002	// usubl v2.8h, v0.8b, v1.8b

	VST1	V2.D[0], (R4)
	VMOV	V2.D[0], R8
	ORR	R8, R11, R11

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	$8, R4, R4
	SUB	$1, R5, R5
	CBNZ	R5, sub4_loop

sub4_done:
	MOVD	R11, ret+48(FP)
	RET
