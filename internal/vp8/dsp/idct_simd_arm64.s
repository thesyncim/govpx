//go:build arm64 && !purego

// ARMv8 NEON port of the libvpx v1.16.0
// vp8/common/arm/neon/shortidct4x4llm_neon.c (vp8_short_idct4x4llm_neon)
// plus a NEON DC-only fast path mirroring DCOnlyIDCT4x4Add.
//
// Most NEON ops aren't natively supported by Go's arm64 assembler so they
// are emitted as raw WORD encodings.

#include "textflag.h"

// idct4x4AddNEON ABI ($0-40):
//   input+0(FP)      *int16 (16 lanes)
//   pred+8(FP)       *uint8
//   predStride+16(FP) int
//   dst+24(FP)       *uint8
//   dstStride+32(FP) int
TEXT ·idct4x4AddNEON(SB), NOSPLIT, $0-40
	MOVD	input+0(FP), R0
	MOVD	pred+8(FP), R1
	MOVD	predStride+16(FP), R2
	MOVD	dst+24(FP), R3
	MOVD	dstStride+32(FP), R4

	WORD	$0x0c402400	// ld1 {v0.4h, v1.4h, v2.4h, v3.4h}, [x0]

	// Pass 1 (lanes = column).
	WORD	$0x0e620c08	// sqadd v8.4h, v0.4h, v2.4h        ; a1
	WORD	$0x0e622c09	// sqsub v9.4h, v0.4h, v2.4h        ; b1

	MOVD	$17734, R5
	MOVD	$20091, R6
	WORD	$0x0e020cbe	// dup v30.4h, w5    ; sinpi8sqrt2 / 2
	WORD	$0x0e020cdf	// dup v31.4h, w6    ; cospi8sqrt2m1

	WORD	$0x0e7eb42a	// sqdmulh v10.4h, v1.4h, v30.4h    ; sin*row1
	WORD	$0x0e7eb46b	// sqdmulh v11.4h, v3.4h, v30.4h    ; sin*row3
	WORD	$0x0e7fb42c	// sqdmulh v12.4h, v1.4h, v31.4h
	WORD	$0x0e7fb46d	// sqdmulh v13.4h, v3.4h, v31.4h
	WORD	$0x0f1f058c	// sshr v12.4h, v12.4h, #1
	WORD	$0x0f1f05ad	// sshr v13.4h, v13.4h, #1
	WORD	$0x0e610d8c	// sqadd v12.4h, v12.4h, v1.4h      ; row1 + (cos*row1>>16)
	WORD	$0x0e630dad	// sqadd v13.4h, v13.4h, v3.4h      ; row3 + (cos*row3>>16)

	WORD	$0x0e6d2d4e	// sqsub v14.4h, v10.4h, v13.4h     ; c1
	WORD	$0x0e6b0d8f	// sqadd v15.4h, v12.4h, v11.4h     ; d1

	WORD	$0x0e6f0d10	// sqadd v16.4h, v8.4h, v15.4h      ; tmp_row0 = a1+d1
	WORD	$0x0e6e0d31	// sqadd v17.4h, v9.4h, v14.4h      ; tmp_row1 = b1+c1
	WORD	$0x0e6e2d32	// sqsub v18.4h, v9.4h, v14.4h      ; tmp_row2 = b1-c1
	WORD	$0x0e6f2d13	// sqsub v19.4h, v8.4h, v15.4h      ; tmp_row3 = a1-d1

	// Transpose 4x4 (rows -> columns in lanes).
	WORD	$0x0e922a04	// trn1 v4.2s, v16.2s, v18.2s
	WORD	$0x0e926a06	// trn2 v6.2s, v16.2s, v18.2s
	WORD	$0x0e932a25	// trn1 v5.2s, v17.2s, v19.2s
	WORD	$0x0e936a27	// trn2 v7.2s, v17.2s, v19.2s
	WORD	$0x0e452894	// trn1 v20.4h, v4.4h, v5.4h        ; col 0
	WORD	$0x0e456895	// trn2 v21.4h, v4.4h, v5.4h        ; col 1
	WORD	$0x0e4728d6	// trn1 v22.4h, v6.4h, v7.4h        ; col 2
	WORD	$0x0e4768d7	// trn2 v23.4h, v6.4h, v7.4h        ; col 3

	// Pass 2 (lanes = row).
	WORD	$0x0e760e88	// sqadd v8.4h, v20.4h, v22.4h
	WORD	$0x0e762e89	// sqsub v9.4h, v20.4h, v22.4h
	WORD	$0x0e7eb6aa	// sqdmulh v10.4h, v21.4h, v30.4h
	WORD	$0x0e7eb6eb	// sqdmulh v11.4h, v23.4h, v30.4h
	WORD	$0x0e7fb6ac	// sqdmulh v12.4h, v21.4h, v31.4h
	WORD	$0x0e7fb6ed	// sqdmulh v13.4h, v23.4h, v31.4h
	WORD	$0x0f1f058c	// sshr v12.4h, v12.4h, #1
	WORD	$0x0f1f05ad	// sshr v13.4h, v13.4h, #1
	WORD	$0x0e750d8c	// sqadd v12.4h, v12.4h, v21.4h
	WORD	$0x0e770dad	// sqadd v13.4h, v13.4h, v23.4h
	WORD	$0x0e6d2d4e	// sqsub v14.4h, v10.4h, v13.4h
	WORD	$0x0e6b0d8f	// sqadd v15.4h, v12.4h, v11.4h

	WORD	$0x0e6f0d18	// sqadd v24.4h, v8.4h, v15.4h      ; out col 0 across rows
	WORD	$0x0e6e0d39	// sqadd v25.4h, v9.4h, v14.4h      ; out col 1
	WORD	$0x0e6e2d3a	// sqsub v26.4h, v9.4h, v14.4h      ; out col 2
	WORD	$0x0e6f2d1b	// sqsub v27.4h, v8.4h, v15.4h      ; out col 3

	WORD	$0x0f1d2718	// srshr v24.4h, v24.4h, #3        ; (x + 4) >> 3 arith
	WORD	$0x0f1d2739	// srshr v25.4h, v25.4h, #3
	WORD	$0x0f1d275a	// srshr v26.4h, v26.4h, #3
	WORD	$0x0f1d277b	// srshr v27.4h, v27.4h, #3

	// Transpose back so each vector represents one row's 4 columns.
	WORD	$0x0e9a2b04	// trn1 v4.2s, v24.2s, v26.2s
	WORD	$0x0e9a6b06	// trn2 v6.2s, v24.2s, v26.2s
	WORD	$0x0e9b2b25	// trn1 v5.2s, v25.2s, v27.2s
	WORD	$0x0e9b6b27	// trn2 v7.2s, v25.2s, v27.2s
	WORD	$0x0e452894	// trn1 v20.4h, v4.4h, v5.4h        ; row 0
	WORD	$0x0e456895	// trn2 v21.4h, v4.4h, v5.4h        ; row 1
	WORD	$0x0e4728d6	// trn1 v22.4h, v6.4h, v7.4h        ; row 2
	WORD	$0x0e4768d7	// trn2 v23.4h, v6.4h, v7.4h        ; row 3

	// Pack pairs of rows into 8-lane vectors for combined load/store.
	WORD	$0x6e1806b4	// ins v20.d[1], v21.d[0]
	WORD	$0x6e1806f6	// ins v22.d[1], v23.d[0]

	// Load 4 pred bytes from each of 4 rows. Pack rows 0,1 into low/high
	// half of v0 (8 bytes total), rows 2,3 into v1.
	WORD	$0xbd400020	// ldr s0, [x1]
	ADD	R2, R1, R5
	WORD	$0x0d4090a0	// ld1 {v0.s}[1], [x5]
	ADD	R2, R5, R5
	WORD	$0xbd4000a1	// ldr s1, [x5]
	ADD	R2, R5, R5
	WORD	$0x0d4090a1	// ld1 {v1.s}[1], [x5]

	// Widen pred bytes to int16 (zero-extend), add output (signed),
	// saturate-narrow to uint8.
	WORD	$0x2f08a400	// ushll v0.8h, v0.8b, #0
	WORD	$0x2f08a421	// ushll v1.8h, v1.8b, #0
	WORD	$0x4e748400	// add v0.8h, v0.8h, v20.8h
	WORD	$0x4e768421	// add v1.8h, v1.8h, v22.8h
	WORD	$0x2e212800	// sqxtun v0.8b, v0.8h
	WORD	$0x2e212821	// sqxtun v1.8b, v1.8h

	// Store 4 bytes to each of 4 dst rows.
	WORD	$0xbd000060	// str s0, [x3]
	ADD	R4, R3, R5
	WORD	$0x0d0090a0	// st1 {v0.s}[1], [x5]
	ADD	R4, R5, R5
	WORD	$0xbd0000a1	// str s1, [x5]
	ADD	R4, R5, R5
	WORD	$0x0d0090a1	// st1 {v1.s}[1], [x5]
	RET

// dcOnlyIDCT4x4AddNEON ABI ($0-40):
//   inputDC+0(FP)    int16 (passed as full word)
//   pred+8(FP)       *uint8
//   predStride+16(FP) int
//   dst+24(FP)       *uint8
//   dstStride+32(FP) int
//
// Computes a1 = (inputDC + 4) >> 3 (signed arithmetic), broadcasts across
// 8 lanes, loads 4 pred bytes per row (4 rows total), widens, adds a1,
// saturates to uint8, stores. Mirrors DCOnlyIDCT4x4Add scalar.
TEXT ·dcOnlyIDCT4x4AddNEON(SB), NOSPLIT, $0-40
	MOVH	inputDC+0(FP), R0
	MOVD	pred+8(FP), R1
	MOVD	predStride+16(FP), R2
	MOVD	dst+24(FP), R3
	MOVD	dstStride+32(FP), R4

	// (dc + 4) >> 3, signed arithmetic shift.
	ADD	$4, R0, R0
	ASR	$3, R0, R0
	WORD	$0x4e020c1e	// dup v30.8h, w0

	WORD	$0xbd400020	// ldr s0, [x1]
	ADD	R2, R1, R5
	WORD	$0x0d4090a0	// ld1 {v0.s}[1], [x5]
	ADD	R2, R5, R5
	WORD	$0xbd4000a1	// ldr s1, [x5]
	ADD	R2, R5, R5
	WORD	$0x0d4090a1	// ld1 {v1.s}[1], [x5]

	WORD	$0x2f08a400	// ushll v0.8h, v0.8b, #0
	WORD	$0x2f08a421	// ushll v1.8h, v1.8b, #0
	WORD	$0x4e7e8400	// add v0.8h, v0.8h, v30.8h
	WORD	$0x4e7e8421	// add v1.8h, v1.8h, v30.8h
	WORD	$0x2e212800	// sqxtun v0.8b, v0.8h
	WORD	$0x2e212821	// sqxtun v1.8b, v1.8h

	WORD	$0xbd000060	// str s0, [x3]
	ADD	R4, R3, R5
	WORD	$0x0d0090a0	// st1 {v0.s}[1], [x5]
	ADD	R4, R5, R5
	WORD	$0xbd0000a1	// str s1, [x5]
	ADD	R4, R5, R5
	WORD	$0x0d0090a1	// st1 {v1.s}[1], [x5]
	RET
