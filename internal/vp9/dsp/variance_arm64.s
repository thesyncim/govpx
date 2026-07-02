//go:build arm64 && !purego

// VP9 ARMv8 NEON variance kernels. Mirrors libvpx v1.16.0
// vpx_dsp/arm/variance_neon.c.
//
// Per row, USUBL widens (src - ref) byte pairs to 8 int16 diff lanes.
// The diff lanes are folded into V20 (4 int32 lanes) via SADALP for
// the sum (sign-extending int16 pairs into int32 accumulators -- this
// avoids the int16 overflow risk on 64-wide blocks with extreme
// pixel differences), and squared-accumulated via SMLAL/SMLAL2 into
// V22/V23 (4 int32 lanes each) for the SSE.
//
// At end-of-block:
//   - Sum lanes: VADDV.S4 reduces 4 int32 lanes in V20 to a single
//     scalar in S20.
//   - SSE lanes: V22 += V23, then VADDV.S4 reduces 4 int32 lanes to a
//     single int32 in S22.
//
// USUBL/SADALP/SMLAL/SMLAL2 aren't natively in Go's arm64 assembler,
// so they're emitted as raw WORD directives; encodings come from
// clang.

#include "textflag.h"

// varianceBlock16xNNEON ABI ($0-56):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   sumOut+40(FP)    *int32
//   sseOut+48(FP)    *uint32
TEXT ·varianceBlock16xNNEON(SB), NOSPLIT, $0-56
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

	CBZ	R6, w16_done

w16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b,  v1.8b
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	WORD	$0x4e606854	// sadalp v20.4s, v2.8h
	WORD	$0x4e606874	// sadalp v20.4s, v3.8h

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, w16_loop

w16_done:
	VADDV	V20.S4, V20
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// varianceBlock16ChunksNEON ABI ($0-64):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   chunks+40(FP)    int   (width / 16: 2 for w=32, 4 for w=64)
//   sumOut+48(FP)    *int32
//   sseOut+56(FP)    *uint32
TEXT ·varianceBlock16ChunksNEON(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	chunks+40(FP), R7
	MOVD	sumOut+48(FP), R4
	MOVD	sseOut+56(FP), R5

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	CBZ	R6, wC_done

wC_rowLoop:
	MOVD	R7, R8
	MOVD	R0, R9
	MOVD	R2, R10

wC_chunkLoop:
	VLD1	(R9), [V0.B16]
	VLD1	(R10), [V1.B16]

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b,  v1.8b
	WORD	$0x6e212003	// usubl2 v3.8h, v0.16b, v1.16b

	WORD	$0x4e606854	// sadalp v20.4s, v2.8h
	WORD	$0x4e606874	// sadalp v20.4s, v3.8h

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	$16, R9, R9
	ADD	$16, R10, R10
	SUB	$1, R8, R8
	CBNZ	R8, wC_chunkLoop

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, wC_rowLoop

wC_done:
	VADDV	V20.S4, V20
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// varianceBlock8xNNEON: 8 columns, height rows.
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

	WORD	$0x4e606854	// sadalp v20.4s, v2.8h

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, w8_loop

w8_done:
	VADDV	V20.S4, V20
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// varianceBlock4xNNEON: 4 columns, even height rows. Two rows are packed
// into one 8-byte lane register before USUBL.
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
	VEOR	V23.B16, V23.B16, V23.B16

	// R6 -> pair count.
	LSR	$1, R6, R6
	CBZ	R6, w4_done

w4_loop:
	// Row y src -> V0.S[0]; row y+1 src -> V0.S[1] (via INS).
	FMOVS	(R0), F0
	ADD	R1, R0, R0
	FMOVS	(R0), F4
	ADD	R1, R0, R0
	WORD	$0x6e0c0480	// mov v0.s[1], v4.s[0]

	FMOVS	(R2), F1
	ADD	R3, R2, R2
	FMOVS	(R2), F5
	ADD	R3, R2, R2
	WORD	$0x6e0c04a1	// mov v1.s[1], v5.s[0]

	WORD	$0x2e212002	// usubl  v2.8h, v0.8b, v1.8b

	WORD	$0x4e606854	// sadalp v20.4s, v2.8h

	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h

	SUB	$1, R6, R6
	CBNZ	R6, w4_loop

w4_done:
	VADDV	V20.S4, V20
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// varianceDotChunksNEON ABI ($0-64): FEAT_DotProd variance for widths
// that are multiples of 16 (libvpx v1.16.0
// vpx_dsp/arm/variance_neon_dotprod.c variance_16xh/variance_large
// _neon_dotprod). Per 16-byte chunk: UDOT the src and ref rows against
// an all-ones vector for the two sums, and UDOT the UABD abs-diff with
// itself for the SSE ((src-ref)^2 == |src-ref|^2). sum = sum(src) -
// sum(ref) exactly as upstream (vsubq_u32 then int32 reduction).
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   chunks+40(FP)    int   (width / 16)
//   sumOut+48(FP)    *int32
//   sseOut+56(FP)    *uint32
TEXT ·varianceDotChunksNEON(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	chunks+40(FP), R7
	MOVD	sumOut+48(FP), R4
	MOVD	sseOut+56(FP), R5

	WORD	$0x6f00e414	// movi v20.2d, #0 (src sum)
	WORD	$0x6f00e415	// movi v21.2d, #0 (ref sum)
	WORD	$0x6f00e416	// movi v22.2d, #0 (sse)
	WORD	$0x4f00e43f	// movi v31.16b, #1

	CBZ	R6, vdot_done

vdot_row:
	MOVD	R7, R8
	MOVD	R0, R9
	MOVD	R2, R10

vdot_chunk:
	WORD	$0x3dc00120	// ldr q0, [x9]
	WORD	$0x3dc00141	// ldr q1, [x10]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	WORD	$0x6e9f9414	// udot v20.4s, v0.16b, v31.16b
	WORD	$0x6e9f9435	// udot v21.4s, v1.16b, v31.16b
	ADD	$16, R9, R9
	ADD	$16, R10, R10
	SUB	$1, R8, R8
	CBNZ	R8, vdot_chunk

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, vdot_row

vdot_done:
	WORD	$0x6eb58694	// sub v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// varianceDot16xNNEON ABI ($0-56): FEAT_DotProd variance for 16-wide
// blocks (libvpx vpx_dsp/arm/variance_neon_dotprod.c
// variance_16xh_neon_dotprod): single row loop, no chunk loop.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int
//   sumOut+40(FP)    *int32
//   sseOut+48(FP)    *uint32
TEXT ·varianceDot16xNNEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sumOut+40(FP), R4
	MOVD	sseOut+48(FP), R5

	WORD	$0x6f00e414	// movi v20.2d, #0 (src sum)
	WORD	$0x6f00e415	// movi v21.2d, #0 (ref sum)
	WORD	$0x6f00e416	// movi v22.2d, #0 (sse)
	WORD	$0x4f00e43f	// movi v31.16b, #1

	CBZ	R6, vdot16_done

vdot16_row:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	WORD	$0x6e9f9414	// udot v20.4s, v0.16b, v31.16b
	WORD	$0x6e9f9435	// udot v21.4s, v1.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, vdot16_row

vdot16_done:
	WORD	$0x6eb58694	// sub v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET
