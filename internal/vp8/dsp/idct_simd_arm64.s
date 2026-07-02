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

// dcOnlyIDCT4x4AddPairNEON ABI ($0-48):
//   delta0+0(FP)     int (already (inputDC0 + 4) >> 3, clamped)
//   delta1+8(FP)     int (already (inputDC1 + 4) >> 3, clamped)
//   pred+16(FP)      *uint8
//   predStride+24(FP) int
//   dst+32(FP)       *uint8
//   dstStride+40(FP) int
//
// Adds two horizontally adjacent DC-only 4x4 blocks as a single 8x4 row
// operation. Lanes 0..3 use delta0 and lanes 4..7 use delta1.
TEXT ·dcOnlyIDCT4x4AddPairNEON(SB), NOSPLIT, $0-48
	MOVD	delta0+0(FP), R0
	MOVD	delta1+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	dst+32(FP), R4
	MOVD	dstStride+40(FP), R5

	WORD	$0x4e020c1e	// dup v30.8h, w0
	WORD	$0x4e020c3f	// dup v31.8h, w1
	WORD	$0x6e1807fe	// mov v30.d[1], v31.d[0]

	VLD1	(R2), [V0.B8]
	WORD	$0x2f08a400	// ushll  v0.8h, v0.8b, #0
	WORD	$0x4e7e8400	// add    v0.8h, v0.8h, v30.8h
	WORD	$0x2e212800	// sqxtun v0.8b, v0.8h
	VST1	[V0.B8], (R4)

	ADD	R3, R2, R2
	ADD	R5, R4, R4
	VLD1	(R2), [V0.B8]
	WORD	$0x2f08a400	// ushll  v0.8h, v0.8b, #0
	WORD	$0x4e7e8400	// add    v0.8h, v0.8h, v30.8h
	WORD	$0x2e212800	// sqxtun v0.8b, v0.8h
	VST1	[V0.B8], (R4)

	ADD	R3, R2, R2
	ADD	R5, R4, R4
	VLD1	(R2), [V0.B8]
	WORD	$0x2f08a400	// ushll  v0.8h, v0.8b, #0
	WORD	$0x4e7e8400	// add    v0.8h, v0.8h, v30.8h
	WORD	$0x2e212800	// sqxtun v0.8b, v0.8h
	VST1	[V0.B8], (R4)

	ADD	R3, R2, R2
	ADD	R5, R4, R4
	VLD1	(R2), [V0.B8]
	WORD	$0x2f08a400	// ushll  v0.8h, v0.8b, #0
	WORD	$0x4e7e8400	// add    v0.8h, v0.8h, v30.8h
	WORD	$0x2e212800	// sqxtun v0.8b, v0.8h
	VST1	[V0.B8], (R4)
	RET

// idctDequantAddFull2xNEON ABI ($0-32):
//   q+0(FP)      *int16 (32 lanes: two adjacent 4x4 blocks)
//   dq+8(FP)     *int16 (16 lanes)
//   dst+16(FP)   *byte  (left block top-left; right block at dst+4)
//   stride+24(FP) int
//
// Direct port of libvpx v1.16.0 vp8/common/arm/neon/idct_blk_neon.c
// idct_dequant_full_2x_neon: dequantizes both blocks with plain int16
// (wrapping) multiplies and runs the 4x4 inverse transform for both
// blocks simultaneously in 8-lane vectors (lanes 0-3 = left block,
// lanes 4-7 = right block), adding onto the prediction already in dst.
// libvpx's vswp stage is folded into the loads: coefficient rows load
// interleaved (left-block row in D[0], right-block row in D[1]) so each
// vector is one row index across both blocks, and the dq rows are
// duplicated across halves; the multiply results are lane-identical to
// libvpx's post-vswp registers. Unlike libvpx, q is left untouched
// (govpx clears coefficients at the next token decode via the dirty
// mask).
TEXT ·idctDequantAddFull2xNEON(SB), NOSPLIT, $0-32
	MOVD	q+0(FP), R0
	MOVD	dq+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	stride+24(FP), R3

	// dq rows duplicated across both halves.
	VLD1	(R1), [V0.B16, V1.B16]
	WORD	$0x4e080412	// dup v18.2d, v0.d[0]  ; [dq row0, dq row0]
	WORD	$0x4e180413	// dup v19.2d, v0.d[1]  ; [dq row1, dq row1]
	WORD	$0x4e080434	// dup v20.2d, v1.d[0]  ; [dq row2, dq row2]
	WORD	$0x4e180435	// dup v21.2d, v1.d[1]  ; [dq row3, dq row3]

	// Coefficient rows interleaved: D[0] = left block row, D[1] = right
	// block row (right block is 32 bytes further into q).
	VLD1	(R0), V2.D[0]               // left row0 (q+0)
	ADD	$32, R0, R5
	VLD1	(R5), V2.D[1]               // right row0
	ADD	$8, R0, R5
	VLD1	(R5), V4.D[0]               // left row1
	ADD	$40, R0, R5
	VLD1	(R5), V4.D[1]               // right row1
	ADD	$16, R0, R5
	VLD1	(R5), V3.D[0]               // left row2
	ADD	$48, R0, R5
	VLD1	(R5), V3.D[1]               // right row2
	ADD	$24, R0, R5
	VLD1	(R5), V5.D[0]               // left row3
	ADD	$56, R0, R5
	VLD1	(R5), V5.D[1]               // right row3

	// Prediction rows: the two blocks are horizontally adjacent, so
	// each row is 8 contiguous bytes.
	VLD1	(R2), [V28.B8]
	ADD	R3, R2, R7
	VLD1	(R7), [V29.B8]
	ADD	R3, R7, R7
	VLD1	(R7), [V30.B8]
	ADD	R3, R7, R7
	VLD1	(R7), [V31.B8]

	// Dequantize (vmulq_s16: modular int16).
	WORD	$0x4e729c42	// mul v2.8h, v2.8h, v18.8h  ; row0 * dq row0
	WORD	$0x4e739c84	// mul v4.8h, v4.8h, v19.8h  ; row1 * dq row1
	WORD	$0x4e749c63	// mul v3.8h, v3.8h, v20.8h  ; row2 * dq row2
	WORD	$0x4e759ca5	// mul v5.8h, v5.8h, v21.8h  ; row3 * dq row3

	// Transform constants (same as idct4x4AddNEON, 8h broadcasts).
	MOVD	$17734, R5
	MOVD	$20091, R6
	WORD	$0x4e020cba	// dup v26.8h, w5  ; sinpi8sqrt2 (doubling mul form)
	WORD	$0x4e020cdb	// dup v27.8h, w6  ; cospi8sqrt2minus1

	// Pass 1 (v2=row0, v4=row1, v3=row2, v5=row3).
	WORD	$0x4e7ab486	// sqdmulh v6.8h, v4.8h, v26.8h
	WORD	$0x4e7ab4a7	// sqdmulh v7.8h, v5.8h, v26.8h
	WORD	$0x4e7bb488	// sqdmulh v8.8h, v4.8h, v27.8h
	WORD	$0x4e7bb4a9	// sqdmulh v9.8h, v5.8h, v27.8h
	WORD	$0x4e630c4a	// sqadd v10.8h, v2.8h, v3.8h    ; a1
	WORD	$0x4e632c4b	// sqsub v11.8h, v2.8h, v3.8h    ; b1
	WORD	$0x4f1f0508	// sshr  v8.8h, v8.8h, #1
	WORD	$0x4f1f0529	// sshr  v9.8h, v9.8h, #1
	WORD	$0x4e680c84	// sqadd v4.8h, v4.8h, v8.8h
	WORD	$0x4e690ca5	// sqadd v5.8h, v5.8h, v9.8h
	WORD	$0x4e652cc2	// sqsub v2.8h, v6.8h, v5.8h     ; c1
	WORD	$0x4e640ce3	// sqadd v3.8h, v7.8h, v4.8h     ; d1
	WORD	$0x4e630d44	// sqadd v4.8h, v10.8h, v3.8h    ; a1+d1
	WORD	$0x4e620d65	// sqadd v5.8h, v11.8h, v2.8h    ; b1+c1
	WORD	$0x4e622d66	// sqsub v6.8h, v11.8h, v2.8h    ; b1-c1
	WORD	$0x4e632d47	// sqsub v7.8h, v10.8h, v3.8h    ; a1-d1

	// Transpose (vtrnq_s32 + vtrnq_s16 pairs, as libvpx).
	WORD	$0x4e86288c	// trn1 v12.4s, v4.4s, v6.4s
	WORD	$0x4e86688d	// trn2 v13.4s, v4.4s, v6.4s
	WORD	$0x4e8728ae	// trn1 v14.4s, v5.4s, v7.4s
	WORD	$0x4e8768af	// trn2 v15.4s, v5.4s, v7.4s
	WORD	$0x4e4e2982	// trn1 v2.8h, v12.8h, v14.8h    ; q2tmp2.val[0]
	WORD	$0x4e4e6984	// trn2 v4.8h, v12.8h, v14.8h    ; q2tmp2.val[1]
	WORD	$0x4e4f29a3	// trn1 v3.8h, v13.8h, v15.8h    ; q2tmp3.val[0]
	WORD	$0x4e4f69a5	// trn2 v5.8h, v13.8h, v15.8h    ; q2tmp3.val[1]

	// Pass 2.
	WORD	$0x4e7ab488	// sqdmulh v8.8h, v4.8h, v26.8h
	WORD	$0x4e7ab4a9	// sqdmulh v9.8h, v5.8h, v26.8h
	WORD	$0x4e7bb48a	// sqdmulh v10.8h, v4.8h, v27.8h
	WORD	$0x4e7bb4ab	// sqdmulh v11.8h, v5.8h, v27.8h
	WORD	$0x4e630c46	// sqadd v6.8h, v2.8h, v3.8h
	WORD	$0x4e632c47	// sqsub v7.8h, v2.8h, v3.8h
	WORD	$0x4f1f054a	// sshr  v10.8h, v10.8h, #1
	WORD	$0x4f1f056b	// sshr  v11.8h, v11.8h, #1
	WORD	$0x4e6a0c8a	// sqadd v10.8h, v4.8h, v10.8h
	WORD	$0x4e6b0cab	// sqadd v11.8h, v5.8h, v11.8h
	WORD	$0x4e6b2d08	// sqsub v8.8h, v8.8h, v11.8h
	WORD	$0x4e6a0d29	// sqadd v9.8h, v9.8h, v10.8h
	WORD	$0x4e690cc4	// sqadd v4.8h, v6.8h, v9.8h
	WORD	$0x4e680ce5	// sqadd v5.8h, v7.8h, v8.8h
	WORD	$0x4e682cec	// sqsub v12.8h, v7.8h, v8.8h
	WORD	$0x4e692ccd	// sqsub v13.8h, v6.8h, v9.8h
	WORD	$0x4f1d2484	// srshr v4.8h, v4.8h, #3
	WORD	$0x4f1d24a5	// srshr v5.8h, v5.8h, #3
	WORD	$0x4f1d258c	// srshr v12.8h, v12.8h, #3
	WORD	$0x4f1d25ad	// srshr v13.8h, v13.8h, #3

	// Transpose back to rows.
	WORD	$0x4e8c288e	// trn1 v14.4s, v4.4s, v12.4s
	WORD	$0x4e8c688f	// trn2 v15.4s, v4.4s, v12.4s
	WORD	$0x4e8d28b0	// trn1 v16.4s, v5.4s, v13.4s
	WORD	$0x4e8d68b1	// trn2 v17.4s, v5.4s, v13.4s
	WORD	$0x4e5029c2	// trn1 v2.8h, v14.8h, v16.8h    ; row0
	WORD	$0x4e5069c4	// trn2 v4.8h, v14.8h, v16.8h    ; row1
	WORD	$0x4e5129e3	// trn1 v3.8h, v15.8h, v17.8h    ; row2
	WORD	$0x4e5169e5	// trn2 v5.8h, v15.8h, v17.8h    ; row3

	// Add prediction (uaddw), narrow (sqxtun), store 8 bytes per row.
	WORD	$0x2e3c1042	// uaddw v2.8h, v2.8h, v28.8b
	WORD	$0x2e3d1084	// uaddw v4.8h, v4.8h, v29.8b
	WORD	$0x2e3e1063	// uaddw v3.8h, v3.8h, v30.8b
	WORD	$0x2e3f10a5	// uaddw v5.8h, v5.8h, v31.8b
	WORD	$0x2e212842	// sqxtun v2.8b, v2.8h
	WORD	$0x2e212884	// sqxtun v4.8b, v4.8h
	WORD	$0x2e212863	// sqxtun v3.8b, v3.8h
	WORD	$0x2e2128a5	// sqxtun v5.8b, v5.8h

	VST1	[V2.B8], (R2)
	ADD	R3, R2, R7
	VST1	[V4.B8], (R7)
	ADD	R3, R7, R7
	VST1	[V3.B8], (R7)
	ADD	R3, R7, R7
	VST1	[V5.B8], (R7)

	RET
