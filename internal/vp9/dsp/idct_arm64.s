//go:build arm64 && !purego

// ARMv8 NEON inverse-transform DC-only adds. These mirror the
// vpx_idct{4,8,16,32}x{4,8,16,32}_1_add_neon kernels in libvpx
// v1.16.0 vpx_dsp/arm/idct{,1024,135,34,1}_*.c — the kernels that
// dominate the inverse-transform path for "skip" / "eob == 1" blocks.
//
// Algorithm: for each row, widen N dest bytes to int16 lanes, add a
// broadcast a1 (precomputed by the Go caller — see idctDcA1), then
// narrow back to uint8 with signed-to-unsigned saturating extract
// (SQXTUN), which implements clip_pixel_add's clamp to [0, 255] in a
// single instruction.
//
// Encodings (Go's arm64 assembler does not yet expose USHLL / SQXTUN
// for vector forms, so these go through WORD literals):
//   USHLL  Vd.8H, Vn.8B,  #0 -> 0x2f08a400 | (Rn<<5) | Rd
//   USHLL2 Vd.8H, Vn.16B, #0 -> 0x6f08a400 | (Rn<<5) | Rd
//   SQXTUN  Vd.8B,  Vn.8H    -> 0x2e212800 | (Rn<<5) | Rd
//   SQXTUN2 Vd.16B, Vn.8H    -> 0x6e212800 | (Rn<<5) | Rd
//   ADD     Vd.8H, Vn.8H, Vm.8H -> 0x4e608400 | (Rm<<16) | (Rn<<5) | Rd
//   SRSHR Vd.8H, Vn.8H, #n  -> 0x4f002400 | ((32-n)<<16) | (Rn<<5) | Rd
//     (shift n in [1,16] for 8H form; #4 -> 0x4f1c2400; #5 -> 0x4f1b2400;
//      #6 -> 0x4f1a2400)
//
// Register convention across all kernels:
//   R0  -> dest
//   R1  -> stride
//   R2..R5 -> per-row dest cursors
//   V30 -> a1 broadcast as int16x8

#include "textflag.h"

// idct4x4DcAddNEON applies the 4x4 broadcast-add. Four rows of 4 bytes
// each are loaded into S-lanes of V0/V1/V2/V3 (one row per S register),
// widened together via USHLL on the packed 8-byte halves, added with
// the broadcast a1 in V30, narrowed back with SQXTUN, and stored.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct4x4DcAddNEON(SB), NOSPLIT, $0-18
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVH	a1+16(FP), R2
	VDUP	R2, V30.H8

	MOVD	R0, R3              // row 0
	ADD	R1, R0, R4          // row 1
	ADD	R1, R4, R5          // row 2
	ADD	R1, R5, R6          // row 3

	// Load 4 bytes per row into S lanes of V0.4S so we have 8 bytes in
	// V0.8B (rows 0+1) and V1.8B (rows 2+3). The byte layout in V0.8B
	// after the FMOVS pair is row0[0..3] row1[0..3], which is what
	// USHLL widens to V0.8H lanes 0..7.
	FMOVS	(R3), F0            // V0.S[0] = row 0 4 bytes
	FMOVS	(R4), F2
	VMOV	V2.S[0], V0.S[1]    // V0.S[1] = row 1 4 bytes
	FMOVS	(R5), F1            // V1.S[0] = row 2
	FMOVS	(R6), F2
	VMOV	V2.S[0], V1.S[1]    // V1.S[1] = row 3

	WORD	$0x2f08a400         // ushll v0.8h, v0.8b, #0  (rows 0,1)
	WORD	$0x2f08a421         // ushll v1.8h, v1.8b, #0  (rows 2,3)

	WORD	$0x4e7e8400         // add v0.8h, v0.8h, v30.8h
	WORD	$0x4e7e8421         // add v1.8h, v1.8h, v30.8h

	WORD	$0x2e212800         // sqxtun v0.8b, v0.8h     (low 4 bytes -> row 0, high 4 -> row 1)
	WORD	$0x2e212821         // sqxtun v1.8b, v1.8h

	// Store low half (row 0) then high half (row 1) by extracting via
	// VMOV from S lanes.
	VMOV	V0.S[0], R7
	MOVW	R7, (R3)
	VMOV	V0.S[1], R7
	MOVW	R7, (R4)
	VMOV	V1.S[0], R7
	MOVW	R7, (R5)
	VMOV	V1.S[1], R7
	MOVW	R7, (R6)

	RET

// idct8x8DcAddNEON applies the 8x8 broadcast-add. Each row is loaded
// as 8 contiguous bytes via FMOVD into a D lane, widened via USHLL,
// added with V30 (broadcast a1), narrowed back via SQXTUN, and
// stored. Eight rows -> eight iterations of the same 4-instruction
// sequence.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct8x8DcAddNEON(SB), NOSPLIT, $0-18
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVH	a1+16(FP), R2
	VDUP	R2, V30.H8

	MOVD	$8, R7

loop8x8:
	FMOVD	(R0), F0            // 8 bytes
	WORD	$0x2f08a400         // ushll v0.8h, v0.8b, #0
	WORD	$0x4e7e8400         // add v0.8h, v0.8h, v30.8h
	WORD	$0x2e212800         // sqxtun v0.8b, v0.8h
	FMOVD	F0, (R0)

	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, loop8x8

	RET

// idct16x16DcAddNEON applies the 16x16 broadcast-add. Each row loads
// 16 bytes via VLD1, widens both halves with USHLL/USHLL2, adds V30
// to each, narrows back with SQXTUN/SQXTUN2, and stores.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct16x16DcAddNEON(SB), NOSPLIT, $0-18
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVH	a1+16(FP), R2
	VDUP	R2, V30.H8

	MOVD	$16, R7

loop16x16:
	VLD1	(R0), [V0.B16]
	WORD	$0x2f08a401         // ushll  v1.8h, v0.8b,  #0   (low)
	WORD	$0x6f08a402         // ushll2 v2.8h, v0.16b, #0   (high)
	WORD	$0x4e7e8421         // add v1.8h, v1.8h, v30.8h
	WORD	$0x4e7e8442         // add v2.8h, v2.8h, v30.8h
	WORD	$0x2e212820         // sqxtun  v0.8b, v1.8h
	WORD	$0x6e212840         // sqxtun2 v0.16b, v2.8h
	VST1	[V0.B16], (R0)

	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, loop16x16

	RET

// idct32x32DcAddNEON applies the 32x32 broadcast-add. Each row loads
// 32 bytes as two 16-byte halves (V0 low, V3 high), widens each half
// with USHLL/USHLL2, adds V30, narrows back, and stores both halves.
//
// ABI ($0-18):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   a1+16(FP)     int16
TEXT ·idct32x32DcAddNEON(SB), NOSPLIT, $0-18
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVH	a1+16(FP), R2
	VDUP	R2, V30.H8

	MOVD	$32, R7

loop32x32:
	// VLD1 multi-register requires consecutive Vn registers, so we use
	// V0 + V1 for the two 16-byte halves of the 32-byte row.
	VLD1	(R0), [V0.B16, V1.B16]
	// Low 16 bytes (V0) -> V2, V3; high 16 bytes (V1) -> V4, V5.
	WORD	$0x2f08a402         // ushll  v2.8h, v0.8b,  #0
	WORD	$0x6f08a403         // ushll2 v3.8h, v0.16b, #0
	WORD	$0x2f08a424         // ushll  v4.8h, v1.8b,  #0
	WORD	$0x6f08a425         // ushll2 v5.8h, v1.16b, #0

	WORD	$0x4e7e8442         // add v2.8h, v2.8h, v30.8h
	WORD	$0x4e7e8463         // add v3.8h, v3.8h, v30.8h
	WORD	$0x4e7e8484         // add v4.8h, v4.8h, v30.8h
	WORD	$0x4e7e84a5         // add v5.8h, v5.8h, v30.8h

	WORD	$0x2e212840         // sqxtun  v0.8b,  v2.8h
	WORD	$0x6e212860         // sqxtun2 v0.16b, v3.8h
	WORD	$0x2e212881         // sqxtun  v1.8b,  v4.8h
	WORD	$0x6e2128a1         // sqxtun2 v1.16b, v5.8h

	VST1	[V0.B16, V1.B16], (R0)

	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, loop32x32

	RET

// idctAddResidualRow4NEON adds 4 int16 residuals to a 4-byte dest row,
// applying SRSHR #4 (round-power-of-two by 4) to the residuals before
// the saturating add+clip.
//
// Mirrors the row-add half of the VP9 inverse-transform column pass:
// each int16 residual is rounded by shift then added to the existing
// uint8 pixel; the signed-saturating narrow (SQXTUN) implements
// clip_pixel_add's [0,255] clamp.
//
// ABI ($0-24):
//   dest+0(FP)    *byte    base of N rows
//   stride+8(FP)  int      bytes per row
//   residual+16(FP)    *int16   nRows*4 int16 residuals (row-major)
//   nRows+24(FP)  int      number of rows to process
TEXT ·idctAddResidualRows4NEON(SB), NOSPLIT, $0-32
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	residual+16(FP), R2
	MOVD	nRows+24(FP), R7

	CBZ	R7, rows4_done

rows4_loop:
	// Load 4 int16 (8 bytes) residuals into V1.4H (low D lane).
	FMOVD	(R2), F1
	// SRSHR v1.4h, v1.4h, #4  -> equivalent 4H form: Q=0 of 0x4f1c2400 = 0x0f1c2400
	WORD	$0x0f1c2421         // srshr v1.4h, v1.4h, #4
	// Load 4 dest bytes into S0 (lower lane), zero rest. Widen to int16
	// in V0.4H via UXTL (USHLL #0 on 8B).
	FMOVS	(R0), F0
	WORD	$0x2f08a400         // ushll v0.8h, v0.8b, #0 (only low 4H valid; high are zero-byte pad)
	// Signed add (in int16 lanes).
	WORD	$0x4e618400         // add v0.8h, v0.8h, v1.8h (we only use low 4)
	// SQXTUN narrow back to uint8: lanes 0..3 of V0.8H -> V0.8B[0..3].
	WORD	$0x2e212800         // sqxtun v0.8b, v0.8h
	// Store low 4 bytes (S lane 0).
	VMOV	V0.S[0], R8
	MOVW	R8, (R0)

	ADD	$8, R2, R2          // advance 4 int16 = 8 bytes
	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, rows4_loop

rows4_done:
	RET

// idctAddResidualRows8NEON adds 8 int16 residuals per row across nRows,
// applying SRSHR #5 (8x8 normalization).
//
// ABI ($0-32):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   residual+16(FP)    *int16
//   nRows+24(FP)  int
TEXT ·idctAddResidualRows8NEON(SB), NOSPLIT, $0-32
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	residual+16(FP), R2
	MOVD	nRows+24(FP), R7

	CBZ	R7, rows8_done

rows8_loop:
	// Load 8 int16 (16 bytes) residuals into V1.8H.
	VLD1	(R2), [V1.H8]
	// SRSHR v1.8h, v1.8h, #5
	WORD	$0x4f1b2421         // srshr v1.8h, v1.8h, #5
	// Load 8 dest bytes -> widen.
	FMOVD	(R0), F0
	WORD	$0x2f08a400         // ushll v0.8h, v0.8b, #0
	WORD	$0x4e618400         // add v0.8h, v0.8h, v1.8h
	WORD	$0x2e212800         // sqxtun v0.8b, v0.8h
	FMOVD	F0, (R0)

	ADD	$16, R2, R2         // advance 8 int16
	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, rows8_loop

rows8_done:
	RET

// idctAddResidualRows16NEON adds 16 int16 residuals per row across
// nRows, applying SRSHR #6 (16x16 normalization).
//
// ABI ($0-32):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   residual+16(FP)    *int16
//   nRows+24(FP)  int
TEXT ·idctAddResidualRows16NEON(SB), NOSPLIT, $0-32
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	residual+16(FP), R2
	MOVD	nRows+24(FP), R7

	CBZ	R7, rows16_done

rows16_loop:
	// Load 16 int16 (32 bytes) residuals into V1, V2.
	VLD1	(R2), [V1.H8, V2.H8]
	// SRSHR by 6.
	WORD	$0x4f1a2421         // srshr v1.8h, v1.8h, #6
	WORD	$0x4f1a2442         // srshr v2.8h, v2.8h, #6
	// Load 16 dest bytes.
	VLD1	(R0), [V0.B16]
	WORD	$0x2f08a410         // ushll  v16.8h, v0.8b,  #0  -> low 8
	WORD	$0x6f08a411         // ushll2 v17.8h, v0.16b, #0  -> high 8
	WORD	$0x4e618610         // add v16.8h, v16.8h, v1.8h
	WORD	$0x4e628631         // add v17.8h, v17.8h, v2.8h
	WORD	$0x2e212a00         // sqxtun  v0.8b,  v16.8h
	WORD	$0x6e212a20         // sqxtun2 v0.16b, v17.8h
	VST1	[V0.B16], (R0)

	ADD	$32, R2, R2
	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, rows16_loop

rows16_done:
	RET

// idctAddResidualRows32NEON adds 32 int16 residuals per row across
// nRows, applying SRSHR #6 (32x32 normalization).
//
// ABI ($0-32):
//   dest+0(FP)    *byte
//   stride+8(FP)  int
//   residual+16(FP)    *int16
//   nRows+24(FP)  int
TEXT ·idctAddResidualRows32NEON(SB), NOSPLIT, $0-32
	MOVD	dest+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	residual+16(FP), R2
	MOVD	nRows+24(FP), R7

	CBZ	R7, rows32_done

rows32_loop:
	// Load 32 int16 (64 bytes) residuals into V1..V4.
	VLD1	(R2), [V1.H8, V2.H8, V3.H8, V4.H8]
	// SRSHR each by 6.
	WORD	$0x4f1a2421         // srshr v1.8h, v1.8h, #6
	WORD	$0x4f1a2442         // srshr v2.8h, v2.8h, #6
	WORD	$0x4f1a2463         // srshr v3.8h, v3.8h, #6
	WORD	$0x4f1a2484         // srshr v4.8h, v4.8h, #6
	// Load 32 dest bytes into V5..V6 (two 16-byte halves).
	VLD1	(R0), [V5.B16, V6.B16]
	WORD	$0x2f08a4b0         // ushll  v16.8h, v5.8b,  #0
	WORD	$0x6f08a4b1         // ushll2 v17.8h, v5.16b, #0
	WORD	$0x2f08a4d2         // ushll  v18.8h, v6.8b,  #0
	WORD	$0x6f08a4d3         // ushll2 v19.8h, v6.16b, #0
	WORD	$0x4e618610         // add v16.8h, v16.8h, v1.8h
	WORD	$0x4e628631         // add v17.8h, v17.8h, v2.8h
	WORD	$0x4e638652         // add v18.8h, v18.8h, v3.8h
	WORD	$0x4e648673         // add v19.8h, v19.8h, v4.8h
	WORD	$0x2e212a05         // sqxtun  v5.8b,  v16.8h
	WORD	$0x6e212a25         // sqxtun2 v5.16b, v17.8h
	WORD	$0x2e212a46         // sqxtun  v6.8b,  v18.8h
	WORD	$0x6e212a66         // sqxtun2 v6.16b, v19.8h
	VST1	[V5.B16, V6.B16], (R0)

	ADD	$64, R2, R2
	ADD	R1, R0, R0
	SUB	$1, R7, R7
	CBNZ	R7, rows32_loop

rows32_done:
	RET
