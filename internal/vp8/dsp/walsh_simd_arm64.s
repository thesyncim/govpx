// ARMv8 NEON port of the libvpx v1.16.0
// vp8/common/arm/neon/iwalsh_neon.c (vp8_short_inv_walsh4x4_neon).
//
// Loads the 16 input int16 as 4 row-vectors of 4 lanes each, runs the row
// pass, transposes, runs the column pass, adds 3 and arithmetic-right-shifts
// by 3, then writes each result lane to mbDQCoeff at stride-16 (32 bytes).
//
// Output is byte-identical to inverseWalsh4x4Scalar for the decoder's
// coefficient range.

#include "textflag.h"

// inverseWalsh4x4NEON ABI ($0-16):
//   input+0(FP)      *int16  (16 lanes packed; treated as 4 vectors of 4 H lanes)
//   mbDQCoeff+8(FP)  *int16  (the destination is written at stride 16 elements)
TEXT ·inverseWalsh4x4NEON(SB), NOSPLIT, $0-16
	MOVD	input+0(FP), R0
	MOVD	mbDQCoeff+8(FP), R1

	// Load 4 row-vectors of 4 int16 each. Lane k of Vr corresponds to
	// scalar input[r*4 + k] where r is the implicit "row" the libvpx
	// reference iterates over with i = column index. (See pass-1 derivation
	// in walsh_simd_arm64.go.)
	VLD1	(R0), [V0.H4, V1.H4, V2.H4, V3.H4]   // V0=in[0..3], V1=[4..7], V2=[8..11], V3=[12..15]

	// === Pass 1 ===
	// Per scalar i (lane index k):
	//   a1 = in[k]  + in[k+12] = V0 + V3
	//   b1 = in[k+4] + in[k+8] = V1 + V2
	//   d1 = in[k]  - in[k+12] = V0 - V3
	//   c1 = in[k+4] - in[k+8] = V1 - V2
	// Outputs of pass 1 (each register lane k = column k):
	//   row0 = a1 + b1
	//   row1 = c1 + d1
	//   row2 = a1 - b1
	//   row3 = d1 - c1
	VADD	V0.H4, V3.H4, V4.H4    // V4 = a1
	VADD	V1.H4, V2.H4, V5.H4    // V5 = b1
	VSUB	V3.H4, V0.H4, V6.H4    // V6 = d1   (V0 - V3)
	VSUB	V2.H4, V1.H4, V7.H4    // V7 = c1   (V1 - V2)

	VADD	V4.H4, V5.H4, V8.H4    // V8  = a1+b1 = pass1 row 0
	VADD	V6.H4, V7.H4, V9.H4    // V9  = d1+c1 = pass1 row 1 (== c1+d1)
	VSUB	V5.H4, V4.H4, V10.H4   // V10 = a1-b1 = pass1 row 2
	VSUB	V7.H4, V6.H4, V11.H4   // V11 = d1-c1 = pass1 row 3

	// === Transpose 4 rows -> 4 cols (lane index becomes row index) ===
	// Step 1: 32-bit (.2s) transpose between row pairs (0,2) and (1,3).
	VTRN1	V10.S2, V8.S2, V12.S2  // V12.2s = even .2s lanes  → [r0c0||r0c1, r2c0||r2c1]
	VTRN2	V10.S2, V8.S2, V13.S2  // V13.2s = odd  .2s lanes  → [r0c2||r0c3, r2c2||r2c3]
	VTRN1	V11.S2, V9.S2, V14.S2  // V14.2s = [r1c0||r1c1, r3c0||r3c1]
	VTRN2	V11.S2, V9.S2, V15.S2  // V15.2s = [r1c2||r1c3, r3c2||r3c3]

	// Step 2: 16-bit (.4h) transpose to interleave row pair lanes.
	VTRN1	V14.H4, V12.H4, V16.H4 // V16 = [r0c0, r1c0, r2c0, r3c0] = col 0 (lane k = row k)
	VTRN2	V14.H4, V12.H4, V17.H4 // V17 = [r0c1, r1c1, r2c1, r3c1] = col 1
	VTRN1	V15.H4, V13.H4, V18.H4 // V18 = col 2
	VTRN2	V15.H4, V13.H4, V19.H4 // V19 = col 3

	// === Pass 2 (lane k = row k) ===
	// a1 = col0 + col3 ; b1 = col1 + col2 ; d1 = col0 - col3 ; c1 = col1 - col2
	// a2 = a1 + b1, b2 = c1 + d1 (== d1 + c1), c2 = a1 - b1, d2 = d1 - c1
	// out[row k][col 0..3] = ((a2|b2|c2|d2) + 3) >> 3 (signed)
	VADD	V16.H4, V19.H4, V20.H4 // V20 = a1
	VADD	V17.H4, V18.H4, V21.H4 // V21 = b1
	VSUB	V19.H4, V16.H4, V22.H4 // V22 = d1
	VSUB	V18.H4, V17.H4, V23.H4 // V23 = c1

	VADD	V20.H4, V21.H4, V24.H4 // V24 = a2 (out col 0)
	VADD	V22.H4, V23.H4, V25.H4 // V25 = b2 (out col 1)
	VSUB	V21.H4, V20.H4, V26.H4 // V26 = c2 (out col 2)
	VSUB	V23.H4, V22.H4, V27.H4 // V27 = d2 (out col 3)

	// Add 3 and arithmetic-right-shift by 3 (signed). Build a {3,3,3,3} vector.
	MOVD	$3, R5
	VDUP	R5, V28.H4
	VADD	V28.H4, V24.H4, V24.H4
	VADD	V28.H4, V25.H4, V25.H4
	VADD	V28.H4, V26.H4, V26.H4
	VADD	V28.H4, V27.H4, V27.H4

	// SSHR Vd.4H, Vn.4H, #3 (signed shift right). Go's arm64 assembler
	// does not recognise VSHR/VSSHR mnemonics, so emit raw WORD encodings.
	// Encoding: 0_0_0_011110_immh:immb_00000_1_Rn_Rd
	// For 4h with shift=3: imm = 32-3 = 29 = 0x1d.
	// 0x0f00_0400 base + (imm << 16) + (Rn << 5) + Rd.
	WORD	$0x0f1d0718  // sshr v24.4h, v24.4h, #3
	WORD	$0x0f1d0739  // sshr v25.4h, v25.4h, #3
	WORD	$0x0f1d075a  // sshr v26.4h, v26.4h, #3
	WORD	$0x0f1d077b  // sshr v27.4h, v27.4h, #3

	// Stores: mbDQCoeff[i*16] = output[i] where i = row*4 + col, byte
	// offset = i*32. So lane k of V(24+col) goes to mbDQCoeff at byte
	// offset (k*4 + col)*32 = k*128 + col*32.
	// Use VMOV V.H[idx], Rn -> MOVH Rn, off(R1). 16 lanes total.
	//
	// Row 0 (lane 0):
	VMOV	V24.H[0], R5
	MOVH	R5, 0(R1)
	VMOV	V25.H[0], R5
	MOVH	R5, 32(R1)
	VMOV	V26.H[0], R5
	MOVH	R5, 64(R1)
	VMOV	V27.H[0], R5
	MOVH	R5, 96(R1)

	// Row 1 (lane 1) at +128:
	VMOV	V24.H[1], R5
	MOVH	R5, 128(R1)
	VMOV	V25.H[1], R5
	MOVH	R5, 160(R1)
	VMOV	V26.H[1], R5
	MOVH	R5, 192(R1)
	VMOV	V27.H[1], R5
	MOVH	R5, 224(R1)

	// Row 2 (lane 2) at +256:
	VMOV	V24.H[2], R5
	MOVH	R5, 256(R1)
	VMOV	V25.H[2], R5
	MOVH	R5, 288(R1)
	VMOV	V26.H[2], R5
	MOVH	R5, 320(R1)
	VMOV	V27.H[2], R5
	MOVH	R5, 352(R1)

	// Row 3 (lane 3) at +384:
	VMOV	V24.H[3], R5
	MOVH	R5, 384(R1)
	VMOV	V25.H[3], R5
	MOVH	R5, 416(R1)
	VMOV	V26.H[3], R5
	MOVH	R5, 448(R1)
	VMOV	V27.H[3], R5
	MOVH	R5, 480(R1)
	RET
