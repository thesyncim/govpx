// ARMv8 NEON variance kernels for non-16x16 block sizes. Mirrors
// libvpx v1.16.0 vpx_dsp/arm/variance_neon.c variance_neon_w8 and
// variance_neon_w4 plus a height-parameterised variance_neon_w16. Each
// kernel returns (sum, sse) of (src - ref) over the whole block.
//
// USUBL/SADDLV/SMLAL/SMLAL2 aren't natively in Go's arm64 assembler,
// so they're emitted as raw WORD directives; encodings come from
// clang.
//
// Calling convention (ABI0, $0-56):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   sumOut+40(FP)    *int32
//   sseOut+48(FP)    *uint32

#include "textflag.h"

// varianceBlock16xNNEON: 16 columns, height rows. Same kernel as the
// 16x16 specialisation but with a runtime row count.
TEXT ·varianceBlock16xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sumOut+40(FP), R4
	MOVD	sseOut+48(FP), R5

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	CBZ	R6, w16_done

w16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	WORD	$0x4e628694	// add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5	// add v21.8h, v21.8h, v3.8h

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, w16_loop

w16_done:
	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// sseBlock16xNNEON: 16 columns, height rows. Same diff^2 reduction as
// varianceBlock16xNNEON, but skips the sum accumulator for callers that
// only need SSE.
TEXT ·sseBlock16xNNEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sseOut+40(FP), R4

	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	CBZ	R6, sse_w16_done

sse_w16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, sse_w16_loop

sse_w16_done:
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F22, (R4)
	RET

// varianceBlock8xNNEON: 8 columns, height rows. Per row we load 8
// src bytes + 8 ref bytes via FMOVD (which zeroes the upper half of
// the V register), USUBL into V2.8H, ADD into V20 (sum), and
// SMLAL/SMLAL2 into split V22/V23 accumulators (sse).
TEXT ·varianceBlock8xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sumOut+40(FP), R4
	MOVD	sseOut+48(FP), R5

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	CBZ	R6, w8_done

w8_loop:
	FMOVD	(R0), F0
	FMOVD	(R2), F1

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b

	WORD	$0x4e628694	// add v20.8h, v20.8h, v2.8h

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, w8_loop

w8_done:
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// sseBlock8xNNEON: 8 columns, height rows. SSE-only companion for hot
// scoring paths that do not need the variance sum.
TEXT ·sseBlock8xNNEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sseOut+40(FP), R4

	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	CBZ	R6, sse_w8_done

sse_w8_loop:
	FMOVD	(R0), F0
	FMOVD	(R2), F1

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, sse_w8_loop

sse_w8_done:
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F22, (R4)
	RET

// varianceBlock4xNNEON: 4 columns, height rows. We pack two rows of 4
// bytes into one 8-byte register via FMOVS + VLD1 lane insert so
// USUBL on V0.8B/V1.8B yields V2.8H = the diffs for both rows. The
// caller always passes an even height (variance picker only uses 4x8
// and 4x4).
TEXT ·varianceBlock4xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sumOut+40(FP), R4
	MOVD	sseOut+48(FP), R5

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V22.B16, V22.B16, V22.B16

	CBZ	R6, w4_done

	// R7 = pair count (height / 2). Tail handles odd height.
	LSR	$1, R6, R7
	CBZ	R7, w4_tail

w4_pair_loop:
	// Load 4 bytes from row 0 -> S0 (zeroes upper bits of V0).
	FMOVS	(R0), F0
	// Lane-insert 4 bytes from row 1 into V0.S[1].
	ADD	R1, R0, R8
	VLD1	(R8), V0.S[1]

	// Same for ref.
	FMOVS	(R2), F1
	ADD	R3, R2, R8
	VLD1	(R8), V1.S[1]

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b

	WORD	$0x4e606854	// sadalp v20.4s, v2.8h
	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628056	// smlal2 v22.4s, v2.8h, v2.8h

	// Advance two rows.
	ADD	R1, R0, R0
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	R3, R2, R2
	SUB	$1, R7, R7
	CBNZ	R7, w4_pair_loop

w4_tail:
	// Handle a leftover odd row if any. FMOVS zeroes the rest of V0/V1
	// so USUBL produces zero diffs in unused lanes.
	AND	$1, R6, R7
	CBZ	R7, w4_done

	VEOR	V0.B16, V0.B16, V0.B16
	VEOR	V1.B16, V1.B16, V1.B16
	FMOVS	(R0), F0
	FMOVS	(R2), F1
	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b
	WORD	$0x4e606854	// sadalp v20.4s, v2.8h
	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628056	// smlal2 v22.4s, v2.8h, v2.8h

w4_done:
	VADDV	V20.S4, V20
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET
