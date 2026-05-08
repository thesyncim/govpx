// ARMv8 NEON port of the libvpx v1.16.0
// vp8/encoder/arm/neon/vp8_shortwalsh4x4_neon.c
// (vp8_short_walsh4x4_neon).
//
// Loads 4 rows of 4 int16 (row stride is in int16 elements), transposes to
// per-column 4h vectors, runs pass 1 in int16 with the (a1 != 0) ? 1 : 0
// correction, transposes back to per-row vectors, runs pass 2 widened to
// int32 with the (x < 0) ? 1 : 0 sign-correction, then narrows back to int16
// via shrn (arithmetic shift right by 3, halfword-narrow).
//
// Output is byte-identical to forwardWalsh4x4Scalar for the encoder's
// residual range.
//
// A few NEON ops aren't natively supported by Go's arm64 assembler (saddl,
// ssubl, shrn, cmlt-imm0, ld1/st1 multi-reg), so they are emitted as raw
// WORD encodings.

#include "textflag.h"

// forwardWalsh4x4NEON ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardWalsh4x4NEON(SB), NOSPLIT, $0-24
	MOVD	input+0(FP), R0
	MOVD	stride+8(FP), R3
	MOVD	output+16(FP), R1
	LSL	$1, R3, R3              // bytes per row = stride * 2

	// Load rows 0..3 of 4 int16 each into V0..V3.
	VLD1	(R0), [V0.H4]
	ADD	R3, R0, R4
	VLD1	(R4), [V1.H4]
	ADD	R3, R4, R4
	VLD1	(R4), [V2.H4]
	ADD	R3, R4, R4
	VLD1	(R4), [V3.H4]

	// === Transpose 4x4 (lane-major rows -> column registers) ===
	// After: V8=col0, V9=col1, V10=col2, V11=col3 (lane k = row k).
	VTRN1	V2.S2, V0.S2, V4.S2     // tA.4h = (r0c0, r0c1, r2c0, r2c1)
	VTRN2	V2.S2, V0.S2, V6.S2     // tB.4h = (r0c2, r0c3, r2c2, r2c3)
	VTRN1	V3.S2, V1.S2, V5.S2     // tC.4h = (r1c0, r1c1, r3c0, r3c1)
	VTRN2	V3.S2, V1.S2, V7.S2     // tD.4h = (r1c2, r1c3, r3c2, r3c3)
	VTRN1	V5.H4, V4.H4, V8.H4     // V8  = (r0c0, r1c0, r2c0, r3c0) = col 0
	VTRN2	V5.H4, V4.H4, V9.H4     // V9  = col 1
	VTRN1	V7.H4, V6.H4, V10.H4    // V10 = col 2
	VTRN2	V7.H4, V6.H4, V11.H4    // V11 = col 3

	// === Pass 1 (lane-parallel across rows; lane k = row k) ===
	//   a1 = (col0 + col2) << 2
	//   d1 = (col1 + col3) << 2
	//   c1 = (col1 - col3) << 2
	//   b1 = (col0 - col2) << 2
	//   tmp_row0 = a1 + d1 + (a1 != 0 ? 1 : 0)
	//   tmp_row1 = b1 + c1
	//   tmp_row2 = b1 - c1
	//   tmp_row3 = a1 - d1
	VADD	V8.H4, V10.H4, V12.H4   // V12 = col0 + col2
	VADD	V9.H4, V11.H4, V13.H4   // V13 = col1 + col3
	VSUB	V11.H4, V9.H4, V14.H4   // V14 = col1 - col3
	VSUB	V10.H4, V8.H4, V15.H4   // V15 = col0 - col2
	VSHL	$2, V12.H4, V12.H4      // V12 = a1 (shifted)
	VSHL	$2, V13.H4, V13.H4      // V13 = d1
	VSHL	$2, V14.H4, V14.H4      // V14 = c1
	VSHL	$2, V15.H4, V15.H4      // V15 = b1

	// (a1 != 0) sign correction. VCMEQ -> -1 where a1==0, 0 elsewhere.
	// adjust = mask + 1 ∈ {0, 1}. tmp_row0 = a1 + d1 + adjust.
	VEOR	V20.B8, V20.B8, V20.B8  // V20 = 0
	VCMEQ	V20.H4, V12.H4, V21.H4  // V21 = vceq(a1, 0)
	MOVD	$1, R5
	VDUP	R5, V22.H4              // V22 = {1,1,1,1}
	VADD	V21.H4, V22.H4, V21.H4  // V21 = adjust ∈ {0,1}

	VADD	V12.H4, V13.H4, V16.H4  // V16 = a1 + d1
	VADD	V21.H4, V16.H4, V16.H4  // V16 = tmp_row0
	VADD	V15.H4, V14.H4, V17.H4  // V17 = b1 + c1 = tmp_row1
	VSUB	V14.H4, V15.H4, V18.H4  // V18 = b1 - c1 = tmp_row2
	VSUB	V13.H4, V12.H4, V19.H4  // V19 = a1 - d1 = tmp_row3
	// Layout: pX.4h lane k = tmp[k*4 + X] for X=0..3 (V16..V19).

	// === Transpose pass-1 output to row-major (qY.4h lane c = tmp[Y*4 + c]) ===
	// Standard 4x4 NEON transpose pairing (p0,p2) and (p1,p3):
	//   step1 (.2s):
	//     tA = trn1.2s(p0, p2)  ; tB = trn2.2s(p0, p2)
	//     tC = trn1.2s(p1, p3)  ; tD = trn2.2s(p1, p3)
	//   step2 (.4h):
	//     q0 = trn1.4h(tA, tC) ; q1 = trn2.4h(tA, tC)
	//     q2 = trn1.4h(tB, tD) ; q3 = trn2.4h(tB, tD)
	VTRN1	V18.S2, V16.S2, V4.S2   // tA = trn1.2s(V16, V18) -> .4h = (tmp[0], tmp[4], tmp[2], tmp[6])
	VTRN2	V18.S2, V16.S2, V6.S2   // tB -> .4h = (tmp[8], tmp[12], tmp[10], tmp[14])
	VTRN1	V19.S2, V17.S2, V5.S2   // tC -> .4h = (tmp[1], tmp[5], tmp[3], tmp[7])
	VTRN2	V19.S2, V17.S2, V7.S2   // tD -> .4h = (tmp[9], tmp[13], tmp[11], tmp[15])
	VTRN1	V5.H4, V4.H4, V24.H4    // V24 = (tmp[0..3]) = row 0
	VTRN2	V5.H4, V4.H4, V25.H4    // V25 = (tmp[4..7]) = row 1
	VTRN1	V7.H4, V6.H4, V26.H4    // V26 = (tmp[8..11]) = row 2
	VTRN2	V7.H4, V6.H4, V27.H4    // V27 = (tmp[12..15]) = row 3

	// === Pass 2 (lane-parallel across cols; lane c = col c, widened to s32) ===
	// SADDL Vd.4S, Vn.4H, Vm.4H : 0x0e60_0000 + (Rm<<16) + (Rn<<5) + Rd
	// SSUBL Vd.4S, Vn.4H, Vm.4H : 0x0e60_2000 + (Rm<<16) + (Rn<<5) + Rd
	WORD	$0x0e7a0308              // saddl v8.4s,  v24.4h, v26.4h    ; q8  = a1 = row0+row2
	WORD	$0x0e7b0329              // saddl v9.4s,  v25.4h, v27.4h    ; q9  = d1 = row1+row3
	WORD	$0x0e7b232a              // ssubl v10.4s, v25.4h, v27.4h    ; q10 = c1 = row1-row3
	WORD	$0x0e7a230b              // ssubl v11.4s, v24.4h, v26.4h    ; q11 = b1 = row0-row2

	VADD	V8.S4, V9.S4, V0.S4     // V0 = a2 = a1 + d1
	VADD	V11.S4, V10.S4, V1.S4   // V1 = b2 = b1 + c1
	VSUB	V10.S4, V11.S4, V2.S4   // V2 = c2 = b1 - c1
	VSUB	V9.S4, V8.S4, V3.S4     // V3 = d2 = a1 - d1

	// Sign correction: x += (x < 0) ? 1 : 0
	// CMLT Vd.4S, Vn.4S, #0 : 0x4ea0_a800 + (Rn<<5) + Rd  (yields -1 mask in neg lanes)
	WORD	$0x4ea0a808              // cmlt v8.4s,  v0.4s,  #0
	WORD	$0x4ea0a829              // cmlt v9.4s,  v1.4s,  #0
	WORD	$0x4ea0a84a              // cmlt v10.4s, v2.4s,  #0
	WORD	$0x4ea0a86b              // cmlt v11.4s, v3.4s,  #0
	VSUB	V8.S4, V0.S4, V0.S4
	VSUB	V9.S4, V1.S4, V1.S4
	VSUB	V10.S4, V2.S4, V2.S4
	VSUB	V11.S4, V3.S4, V3.S4

	// Add 3 then arithmetic-right-shift-narrow by 3 (s32 -> s16, low 4 lanes).
	MOVD	$3, R5
	VDUP	R5, V28.S4
	VADD	V28.S4, V0.S4, V0.S4
	VADD	V28.S4, V1.S4, V1.S4
	VADD	V28.S4, V2.S4, V2.S4
	VADD	V28.S4, V3.S4, V3.S4

	// SHRN Vd.4H, Vn.4S, #3 : 0x0f1d_8400 + (Rn<<5) + Rd
	WORD	$0x0f1d8400              // shrn v0.4h, v0.4s, #3
	WORD	$0x0f1d8421              // shrn v1.4h, v1.4s, #3
	WORD	$0x0f1d8442              // shrn v2.4h, v2.4s, #3
	WORD	$0x0f1d8463              // shrn v3.4h, v3.4s, #3

	// Store: output[0..3]=V0 (a2), output[4..7]=V1 (b2),
	//        output[8..11]=V2 (c2), output[12..15]=V3 (d2).
	VST1	[V0.H4, V1.H4, V2.H4, V3.H4], (R1)
	RET
