// SSE2 port of libvpx v1.16.0 vp8/common/idctllm.c vp8_short_idct4x4llm
// (single 4x4 block) and DC-only fast path. The libvpx SSE2 reference
// processes pairs of blocks (vp8_idct_dequant_full_2x_sse2 in
// vp8/common/x86/idctllm_sse2.asm); govpx's API is single-block, so we
// keep the same butterfly + PMULHW MAC logic but lay out 4 rows of 4
// int16 in 4 separate XMM registers (4 lanes per register, high lanes
// don't-care). Pass 1 is lane-parallel across the 4 columns; we
// transpose between passes; pass 2 is lane-parallel across the 4 rows.
//
// Constants:
//   sinPI8Sqrt2     = 35468 = 0x8A8C ; treated as int16 = -30068. Then
//                     (a*sin)>>16  = a + (a*(sin-2^16))>>16
//                                  = a + pmulhw(a, 0x8A8C).
//   cosPI8Sqrt2m1   = 20091 = 0x4E7B (positive int16); (a*cos_m1)>>16
//                     = pmulhw(a, 0x4E7B).
//
// Output is byte-identical to scalar reference for VP8 coefficient
// ranges.

#include "textflag.h"

// 8 x int16 of 0x8A8C (sin_pi/8 * sqrt(2) - 2^16, signed = -30068).
DATA  idct_x_s1sqr2<>+0x00(SB)/4, $0x8a8c8a8c
DATA  idct_x_s1sqr2<>+0x04(SB)/4, $0x8a8c8a8c
DATA  idct_x_s1sqr2<>+0x08(SB)/4, $0x8a8c8a8c
DATA  idct_x_s1sqr2<>+0x0c(SB)/4, $0x8a8c8a8c
GLOBL idct_x_s1sqr2<>(SB), RODATA|NOPTR, $16

// 8 x int16 of 0x4E7B (cos_pi/8 * sqrt(2) - 1 = 20091, positive int16).
DATA  idct_x_c1sqr2less1<>+0x00(SB)/4, $0x4e7b4e7b
DATA  idct_x_c1sqr2less1<>+0x04(SB)/4, $0x4e7b4e7b
DATA  idct_x_c1sqr2less1<>+0x08(SB)/4, $0x4e7b4e7b
DATA  idct_x_c1sqr2less1<>+0x0c(SB)/4, $0x4e7b4e7b
GLOBL idct_x_c1sqr2less1<>(SB), RODATA|NOPTR, $16

// 8 x int16 of 4 (column-pass +4 bias before >>3).
DATA  idct_fours<>+0x00(SB)/4, $0x00040004
DATA  idct_fours<>+0x04(SB)/4, $0x00040004
DATA  idct_fours<>+0x08(SB)/4, $0x00040004
DATA  idct_fours<>+0x0c(SB)/4, $0x00040004
GLOBL idct_fours<>(SB), RODATA|NOPTR, $16

// idct4x4AddSSE2 ABI ($0-40):
//   input+0(FP)       *int16 (16 lanes, packed row-major)
//   pred+8(FP)        *byte
//   predStride+16(FP) int
//   dst+24(FP)        *byte
//   dstStride+32(FP)  int
//
// Layout: 4 rows of 4 int16 each loaded into X0..X3 (low 4 lanes per
// register; high 4 lanes don't-care for arithmetic, ignored at store).
// Pass 1 computes the per-column 1D IDCT lane-parallel (each lane is
// one column). Then transpose so each register represents one column
// (lanes are rows). Pass 2 computes the per-row 1D IDCT the same way,
// then transpose back to row layout, add to pred, saturate, store.
TEXT ·idct4x4AddSSE2(SB), NOSPLIT, $0-40
	MOVQ	input+0(FP), AX
	MOVQ	pred+8(FP), BX
	MOVQ	predStride+16(FP), CX
	MOVQ	dst+24(FP), DI
	MOVQ	dstStride+32(FP), DX

	// Load 4 rows of 4 int16 each into low 4 lanes of X0..X3.
	MOVQ	0(AX), X0          // X0 lanes[0..3] = row 0 (cols 0..3)
	MOVQ	8(AX), X1          // X1 = row 1
	MOVQ	16(AX), X2         // X2 = row 2
	MOVQ	24(AX), X3         // X3 = row 3

	// === Pass 1: per-column 1D IDCT, lane-parallel across 4 columns ===
	// For each lane k (= column k):
	//   a1 = row0[k] + row2[k]   ; b1 = row0[k] - row2[k]
	//   temp1_d = row1[k] + (row1[k] * cos_m1)>>16
	//   temp2_d = (row3[k] * sin)>>16 = row3[k] + (row3[k]*-30068)>>16
	//   d1      = temp1_d + temp2_d
	//   temp1_c = (row1[k] * sin)>>16 = row1[k] + (row1[k]*-30068)>>16
	//   temp2_c = row3[k] + (row3[k] * cos_m1)>>16
	//   c1      = temp1_c - temp2_c
	//   tmp_row0[k] = a1 + d1 ; tmp_row1[k] = b1 + c1
	//   tmp_row2[k] = b1 - c1 ; tmp_row3[k] = a1 - d1
	MOVO	X0, X4             // a1 = row0
	PADDW	X2, X4             // X4 = a1
	MOVO	X0, X5             // b1 = row0
	PSUBW	X2, X5             // X5 = b1

	// d1
	MOVO	X1, X6
	PMULHW	idct_x_c1sqr2less1<>(SB), X6   // (row1 * 20091)>>16
	PADDW	X1, X6                          // temp1_d
	MOVO	X3, X7
	PMULHW	idct_x_s1sqr2<>(SB), X7         // (row3 * -30068)>>16
	PADDW	X3, X7                          // temp2_d
	PADDW	X7, X6                          // X6 = d1

	// c1
	MOVO	X1, X7
	PMULHW	idct_x_s1sqr2<>(SB), X7         // (row1 * -30068)>>16
	PADDW	X1, X7                          // temp1_c
	MOVO	X3, X2                          // reuse X2 (no longer holds row 2 — saved into X4/X5)
	PMULHW	idct_x_c1sqr2less1<>(SB), X2    // (row3 * 20091)>>16
	PADDW	X3, X2                          // temp2_c
	PSUBW	X2, X7                          // X7 = c1

	// tmp_row0 = a1+d1 ; tmp_row3 = a1-d1
	MOVO	X4, X0                          // X0 = a1 (copy)
	PADDW	X6, X0                          // X0 = tmp_row0
	PSUBW	X6, X4                          // X4 = tmp_row3
	// tmp_row1 = b1+c1 ; tmp_row2 = b1-c1
	MOVO	X5, X1                          // X1 = b1 (copy)
	PADDW	X7, X1                          // X1 = tmp_row1
	PSUBW	X7, X5                          // X5 = tmp_row2

	// Now: X0 = tmp_row0, X1 = tmp_row1, X5 = tmp_row2, X4 = tmp_row3
	// (each 4 valid lanes in low half).

	// === Transpose 4 rows -> 4 cols ===
	// PUNPCKLWD X1, X0 -> X0 = [r0c0,r1c0,r0c1,r1c1,r0c2,r1c2,r0c3,r1c3]
	// PUNPCKLWD X4, X5 -> X5 = [r2c0,r3c0,r2c1,r3c1,r2c2,r3c2,r2c3,r3c3]
	PUNPCKLWL	X1, X0
	PUNPCKLWL	X4, X5
	// PUNPCKLDQ X5, X0 -> X0 = [r0c0,r1c0,r2c0,r3c0,r0c1,r1c1,r2c1,r3c1] (cols 0+1)
	// PUNPCKHDQ X5, copy -> [r0c2,r1c2,r2c2,r3c2, r0c3,r1c3,r2c3,r3c3] (cols 2+3)
	MOVO	X0, X2
	PUNPCKLLQ	X5, X0          // X0 = cols 0+1 (col0 in low, col1 in high)
	PUNPCKHLQ	X5, X2          // X2 = cols 2+3 (col2 in low, col3 in high)
	// Split into 4 separate per-col registers (each in low 4 lanes):
	MOVO	X0, X1
	PUNPCKHQDQ	X1, X1          // X1 = col 1 in low 4 lanes
	MOVO	X2, X3
	PUNPCKHQDQ	X3, X3          // X3 = col 3 in low 4 lanes
	// X0 = col 0 in low; X2 = col 2 in low.

	// Rename for pass 2 clarity: X0=col0, X1=col1, X4=col2, X3=col3
	MOVO	X2, X4

	// === Pass 2: per-row 1D IDCT, lane-parallel across 4 rows ===
	// For each lane k (= row k):
	//   a1 = col0[k] + col2[k] ; b1 = col0[k] - col2[k]
	//   temp1_d, temp2_d, d1 (using col1 and col3) — same butterfly
	//   temp1_c, temp2_c, c1
	//   out[row k][col 0] = (a1 + d1 + 4) >> 3
	//   out[row k][col 1] = (b1 + c1 + 4) >> 3
	//   out[row k][col 2] = (b1 - c1 + 4) >> 3
	//   out[row k][col 3] = (a1 - d1 + 4) >> 3
	MOVO	X0, X5
	PADDW	X4, X0                          // X0 = a1
	PSUBW	X4, X5                          // X5 = b1

	// d1
	MOVO	X1, X6
	PMULHW	idct_x_c1sqr2less1<>(SB), X6
	PADDW	X1, X6                          // temp1_d
	MOVO	X3, X7
	PMULHW	idct_x_s1sqr2<>(SB), X7
	PADDW	X3, X7                          // temp2_d
	PADDW	X7, X6                          // X6 = d1

	// c1
	MOVO	X1, X7
	PMULHW	idct_x_s1sqr2<>(SB), X7
	PADDW	X1, X7                          // temp1_c
	MOVO	X3, X2
	PMULHW	idct_x_c1sqr2less1<>(SB), X2
	PADDW	X3, X2                          // temp2_c
	PSUBW	X2, X7                          // X7 = c1

	// out_col0 = a1 + d1 ; out_col3 = a1 - d1
	MOVO	X0, X4
	PADDW	X6, X0                          // X0 = a1+d1
	PSUBW	X6, X4                          // X4 = a1-d1
	// out_col1 = b1 + c1 ; out_col2 = b1 - c1
	MOVO	X5, X1
	PADDW	X7, X1                          // X1 = b1+c1
	PSUBW	X7, X5                          // X5 = b1-c1

	// Apply (x + 4) >> 3.
	MOVO	idct_fours<>(SB), X6
	PADDW	X6, X0
	PADDW	X6, X1
	PADDW	X6, X5
	PADDW	X6, X4
	PSRAW	$3, X0                          // out col 0 (lanes = rows 0..3)
	PSRAW	$3, X1                          // out col 1
	PSRAW	$3, X5                          // out col 2
	PSRAW	$3, X4                          // out col 3

	// === Transpose back: 4 cols -> 4 rows ===
	// X0=col0, X1=col1, X5=col2, X4=col3 (each lanes = rows).
	// PUNPCKLWD X1, X0 -> [r0c0,r0c1,r1c0,r1c1, r2c0,r2c1,r3c0,r3c1]
	// PUNPCKLWD X4, X5 -> [r0c2,r0c3,r1c2,r1c3, r2c2,r2c3,r3c2,r3c3]
	PUNPCKLWL	X1, X0
	PUNPCKLWL	X4, X5
	// PUNPCKLDQ X5, X0 -> [r0c0,r0c1,r0c2,r0c3, r1c0,r1c1,r1c2,r1c3]   = rows 0+1
	// PUNPCKHDQ X5, X2 -> [r2c0,...,r3c3]                              = rows 2+3
	MOVO	X0, X2
	PUNPCKLLQ	X5, X0          // X0 = rows 0+1 (8 int16)
	PUNPCKHLQ	X5, X2          // X2 = rows 2+3

	// === Add to pred and clip to uint8 ===
	PXOR	X7, X7                  // zero for unpack and packus

	// Rows 0 + 1.
	MOVL	0(BX), X1                // 4 pred bytes, row 0
	PUNPCKLBW	X7, X1           // widen to int16: X1 lanes[0..3] = pred row 0 zero-extended; lanes[4..7] = 0
	LEAQ	0(BX)(CX*1), R8
	MOVL	0(R8), X3                // 4 pred bytes, row 1
	PUNPCKLBW	X7, X3
	PUNPCKLQDQ	X3, X1           // X1 = [pred row0 (4 int16), pred row1 (4 int16)]
	PADDW	X0, X1                   // add transformed values
	PACKUSWB	X7, X1           // X1 low 8 bytes = saturated uint8 (rows 0+1 packed)

	MOVL	X1, 0(DI)                // store row 0 (low 4 bytes)
	PSRLO	$4, X1                   // shift right by 4 bytes
	LEAQ	0(DI)(DX*1), R8
	MOVL	X1, 0(R8)                // store row 1

	// Rows 2 + 3.
	LEAQ	0(R8)(DX*1), R8          // R8 = dst + 2*dstStride (row 2 dst)
	LEAQ	0(BX)(CX*2), R9          // R9 = pred + 2*predStride (row 2 pred)
	MOVL	0(R9), X1
	PUNPCKLBW	X7, X1
	LEAQ	0(R9)(CX*1), R9          // pred row 3
	MOVL	0(R9), X3
	PUNPCKLBW	X7, X3
	PUNPCKLQDQ	X3, X1
	PADDW	X2, X1
	PACKUSWB	X7, X1
	MOVL	X1, 0(R8)                // store row 2
	PSRLO	$4, X1
	LEAQ	0(R8)(DX*1), R8
	MOVL	X1, 0(R8)                // store row 3
	RET

// dcOnlyIDCT4x4AddSSE2 ABI ($0-40):
//   inputDC+0(FP)     int16
//   pred+8(FP)        *byte
//   predStride+16(FP) int
//   dst+24(FP)        *byte
//   dstStride+32(FP)  int
//
// a1 = (inputDC + 4) >> 3 (signed) ; broadcast across 8 int16 lanes.
// For each of 4 rows: load 4 pred bytes, widen to int16, add a1, pack
// to uint8 (saturating), store.
TEXT ·dcOnlyIDCT4x4AddSSE2(SB), NOSPLIT, $0-40
	MOVWQSX	inputDC+0(FP), AX
	MOVQ	pred+8(FP), BX
	MOVQ	predStride+16(FP), CX
	MOVQ	dst+24(FP), DI
	MOVQ	dstStride+32(FP), DX

	// (DC + 4) >> 3, signed.
	ADDQ	$4, AX
	SARQ	$3, AX

	// Broadcast int16(AX) to all 8 lanes of X0.
	MOVD	AX, X0
	PSHUFLW	$0, X0, X0
	PSHUFD	$0, X0, X0

	PXOR	X7, X7

	// Rows 0 + 1.
	MOVL	0(BX), X1
	PUNPCKLBW	X7, X1
	LEAQ	0(BX)(CX*1), R8
	MOVL	0(R8), X3
	PUNPCKLBW	X7, X3
	PUNPCKLQDQ	X3, X1
	PADDW	X0, X1
	PACKUSWB	X7, X1
	MOVL	X1, 0(DI)
	PSRLO	$4, X1
	LEAQ	0(DI)(DX*1), R8
	MOVL	X1, 0(R8)

	// Rows 2 + 3.
	LEAQ	0(R8)(DX*1), R8
	LEAQ	0(BX)(CX*2), R9
	MOVL	0(R9), X1
	PUNPCKLBW	X7, X1
	LEAQ	0(R9)(CX*1), R9
	MOVL	0(R9), X3
	PUNPCKLBW	X7, X3
	PUNPCKLQDQ	X3, X1
	PADDW	X0, X1
	PACKUSWB	X7, X1
	MOVL	X1, 0(R8)
	PSRLO	$4, X1
	LEAQ	0(R8)(DX*1), R8
	MOVL	X1, 0(R8)
	RET
