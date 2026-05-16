//go:build arm64 && !purego

// ARMv8 NEON port of the libvpx v1.16.0 vp9/encoder/arm/neon/
// vp9_dct_neon.c vp9_fwht4x4_neon kernel. Output is byte-identical to
// forwardWHT4x4Scalar for the encoder's residual range.
//
// Algorithm (lossless 4x4 WHT):
//   for k in {column-pass, row-pass}:
//     a += b
//     d -= c
//     e  = (a - d) >> 1
//     b  = e - b
//     c  = e - c
//     a -= c
//     d += b
//     out = {a, c, d, b}
// Final outputs are left-shifted by 2 once at the end of the row pass.
//
// The kernel runs the same butterfly twice with a transpose between
// passes. Values fit comfortably in int16 across both passes for the
// encoder's residual range.

#include "textflag.h"

// forwardWHT4x4NEON ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardWHT4x4NEON(SB), NOSPLIT, $0-24
	MOVD	input+0(FP), R0
	MOVD	stride+8(FP), R3
	MOVD	output+16(FP), R1
	LSL	$1, R3, R3              // bytes per row = stride * 2

	// Load 4 rows of 4 int16: v0=row0, v1=row1, v2=row2, v3=row3.
	WORD	$0x0c407400             // ld1 {v0.4h}, [x0]
	ADD	R3, R0, R4
	WORD	$0x0c407481             // ld1 {v1.4h}, [x4]
	ADD	R3, R4, R4
	WORD	$0x0c407482             // ld1 {v2.4h}, [x4]
	ADD	R3, R4, R4
	WORD	$0x0c407483             // ld1 {v3.4h}, [x4]

	// Pass 0 column WHT across the 4 registers; lane c = column c.
	//   a = v0 + v1
	//   d = v3 - v2
	//   e = (a - d) >> 1
	//   b = e - v1
	//   c = e - v2
	//   a -= c
	//   d += b
	WORD	$0x0e618404             // add v4.4h, v0.4h, v1.4h     ; a = r0+r1
	WORD	$0x2e628465             // sub v5.4h, v3.4h, v2.4h     ; d = r3-r2
	WORD	$0x2e658486             // sub v6.4h, v4.4h, v5.4h     ; a-d
	WORD	$0x0f1f04c6             // sshr v6.4h, v6.4h, #1       ; e
	WORD	$0x2e6184c7             // sub v7.4h, v6.4h, v1.4h     ; b = e - r1
	WORD	$0x2e6284c8             // sub v8.4h, v6.4h, v2.4h     ; c = e - r2
	WORD	$0x2e688484             // sub v4.4h, v4.4h, v8.4h     ; a -= c
	WORD	$0x0e6784a5             // add v5.4h, v5.4h, v7.4h     ; d += b
	// Now: v4 = a-row (freq 0), v8 = c-row (freq 1), v5 = d-row (freq 2),
	//      v7 = b-row (freq 3).

	// Transpose (v4, v8, v5, v7) so lane f of T_c = freq-f of column-c.
	// Standard 4x4 NEON transpose pairing (v4,v5) and (v8,v7):
	WORD	$0x0e852890             // trn1 v16.2s, v4.2s, v5.2s
	WORD	$0x0e856891             // trn2 v17.2s, v4.2s, v5.2s
	WORD	$0x0e872912             // trn1 v18.2s, v8.2s, v7.2s
	WORD	$0x0e876913             // trn2 v19.2s, v8.2s, v7.2s
	WORD	$0x0e522a14             // trn1 v20.4h, v16.4h, v18.4h   ; T0: lane f = freq-f, col-0
	WORD	$0x0e526a15             // trn2 v21.4h, v16.4h, v18.4h   ; T1: col-1
	WORD	$0x0e532a36             // trn1 v22.4h, v17.4h, v19.4h   ; T2: col-2
	WORD	$0x0e536a37             // trn2 v23.4h, v17.4h, v19.4h   ; T3: col-3

	// Pass 1: WHT on (T0, T1, T2, T3), lane f = freq from pass 0.
	// Treating T0..T3 as the new "v0..v3":
	WORD	$0x0e758684             // add v4.4h, v20.4h, v21.4h    ; a = T0+T1
	WORD	$0x2e7686e5             // sub v5.4h, v23.4h, v22.4h    ; d = T3-T2
	WORD	$0x2e658486             // sub v6.4h, v4.4h, v5.4h
	WORD	$0x0f1f04c6             // sshr v6.4h, v6.4h, #1
	WORD	$0x2e7584c7             // sub v7.4h, v6.4h, v21.4h     ; b = e - T1
	WORD	$0x2e7684c8             // sub v8.4h, v6.4h, v22.4h     ; c = e - T2
	WORD	$0x2e688484             // sub v4.4h, v4.4h, v8.4h
	WORD	$0x0e6784a5             // add v5.4h, v5.4h, v7.4h
	// After pass 1: v4 = output a (freq 0 of row-WHT), v8 = c, v5 = d, v7 = b.
	// Lane f = freq from pass 0 = row index of the input matrix to pass 1.
	// In scalar terms, after pass 1:
	//   output[row=i, col=0] = a (when i=freq from pass-0)
	//   output[row=i, col=1] = c
	//   output[row=i, col=2] = d
	//   output[row=i, col=3] = b
	// So lane i of v4 = output[i][0], lane i of v8 = output[i][1],
	// lane i of v5 = output[i][2], lane i of v7 = output[i][3].

	// Final shift by 2.
	WORD	$0x0f125484             // shl v4.4h, v4.4h, #2
	WORD	$0x0f125508             // shl v8.4h, v8.4h, #2
	WORD	$0x0f1254a5             // shl v5.4h, v5.4h, #2
	WORD	$0x0f1254e7             // shl v7.4h, v7.4h, #2

	// Transpose (v4, v8, v5, v7) so lane c of R_i = output[i][c].
	// Pair (v4, v5) and (v8, v7) — same pattern as pre-pass-1 transpose:
	WORD	$0x0e852890             // trn1 v16.2s, v4.2s, v5.2s
	WORD	$0x0e856891             // trn2 v17.2s, v4.2s, v5.2s
	WORD	$0x0e872912             // trn1 v18.2s, v8.2s, v7.2s
	WORD	$0x0e876913             // trn2 v19.2s, v8.2s, v7.2s
	WORD	$0x0e522a14             // trn1 v20.4h, v16.4h, v18.4h   ; out row 0
	WORD	$0x0e526a15             // trn2 v21.4h, v16.4h, v18.4h   ; out row 1
	WORD	$0x0e532a36             // trn1 v22.4h, v17.4h, v19.4h   ; out row 2
	WORD	$0x0e536a37             // trn2 v23.4h, v17.4h, v19.4h   ; out row 3

	// Store rows contiguously.
	WORD	$0x0c9f7434             // st1 {v20.4h}, [x1], #8
	WORD	$0x0c9f7435             // st1 {v21.4h}, [x1], #8
	WORD	$0x0c9f7436             // st1 {v22.4h}, [x1], #8
	WORD	$0x0c9f7437             // st1 {v23.4h}, [x1], #8
	RET
