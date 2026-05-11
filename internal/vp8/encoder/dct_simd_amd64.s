//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0 vp8/encoder/x86/dct_sse2.asm
// vp8_short_fdct4x4_sse2 kernel.
//
// Loads 4 rows of 4 int16 from input (stride is in int16 elements),
// runs the row-pass butterfly + odd-coefficient (cospi/sinpi) MAC via
// PMADDWD, transposes via PUNPCK + PSHUFD/PSHUF{HW,LW}, runs the
// column-pass butterfly with the +7 / +12000 / +51000 rounding biases
// and the (d != 0) ? 1 : 0 correction, then stores the 16 int16 outputs.
//
// Output is byte-identical to forwardDCT4x4Scalar for the encoder's
// residual range.
//
// stride is signed but VP8 always passes positive strides so the load
// math is fine with unsigned ADDQ.

#include "textflag.h"

// (5352, 2217) low/high int16 pairs — PMADDWD multiplier: when paired
// with (d, c) input pairs gives d*5352 + c*2217 = c*2217 + d*5352.
//   5352 = 0x14e8 ; 2217 = 0x08a9 ; little-endian dword = 0x08a914e8.
DATA  fdct4x4_5352_2217+0x00(SB)/4, $0x08a914e8
DATA  fdct4x4_5352_2217+0x04(SB)/4, $0x08a914e8
DATA  fdct4x4_5352_2217+0x08(SB)/4, $0x08a914e8
DATA  fdct4x4_5352_2217+0x0c(SB)/4, $0x08a914e8
GLOBL fdct4x4_5352_2217(SB), RODATA|NOPTR, $16

// (2217, -5352) low/high int16 pairs — paired with (d, c) gives
// d*2217 + c*(-5352) = d*2217 - c*5352.
//   2217 = 0x08a9 ; -5352 = 0xeb18 ; little-endian dword = 0xeb1808a9.
DATA  fdct4x4_2217_neg5352+0x00(SB)/4, $0xeb1808a9
DATA  fdct4x4_2217_neg5352+0x04(SB)/4, $0xeb1808a9
DATA  fdct4x4_2217_neg5352+0x08(SB)/4, $0xeb1808a9
DATA  fdct4x4_2217_neg5352+0x0c(SB)/4, $0xeb1808a9
GLOBL fdct4x4_2217_neg5352(SB), RODATA|NOPTR, $16

// 8 x int16 of 1, used as PMADDWD multiplier for "a + b".
DATA  fdct4x4_mult_add+0x00(SB)/4, $0x00010001
DATA  fdct4x4_mult_add+0x04(SB)/4, $0x00010001
DATA  fdct4x4_mult_add+0x08(SB)/4, $0x00010001
DATA  fdct4x4_mult_add+0x0c(SB)/4, $0x00010001
GLOBL fdct4x4_mult_add(SB), RODATA|NOPTR, $16

// 8 x int16 alternating 1,-1, used as PMADDWD multiplier for "a - b".
DATA  fdct4x4_mult_sub+0x00(SB)/4, $0xffff0001
DATA  fdct4x4_mult_sub+0x04(SB)/4, $0xffff0001
DATA  fdct4x4_mult_sub+0x08(SB)/4, $0xffff0001
DATA  fdct4x4_mult_sub+0x0c(SB)/4, $0xffff0001
GLOBL fdct4x4_mult_sub(SB), RODATA|NOPTR, $16

// 4 x i32 = 14500 / 7500 / 12000 / 51000 rounding biases.
DATA  fdct4x4_14500+0x00(SB)/4, $14500
DATA  fdct4x4_14500+0x04(SB)/4, $14500
DATA  fdct4x4_14500+0x08(SB)/4, $14500
DATA  fdct4x4_14500+0x0c(SB)/4, $14500
GLOBL fdct4x4_14500(SB), RODATA|NOPTR, $16

DATA  fdct4x4_7500+0x00(SB)/4, $7500
DATA  fdct4x4_7500+0x04(SB)/4, $7500
DATA  fdct4x4_7500+0x08(SB)/4, $7500
DATA  fdct4x4_7500+0x0c(SB)/4, $7500
GLOBL fdct4x4_7500(SB), RODATA|NOPTR, $16

DATA  fdct4x4_12000+0x00(SB)/4, $12000
DATA  fdct4x4_12000+0x04(SB)/4, $12000
DATA  fdct4x4_12000+0x08(SB)/4, $12000
DATA  fdct4x4_12000+0x0c(SB)/4, $12000
GLOBL fdct4x4_12000(SB), RODATA|NOPTR, $16

DATA  fdct4x4_51000+0x00(SB)/4, $51000
DATA  fdct4x4_51000+0x04(SB)/4, $51000
DATA  fdct4x4_51000+0x08(SB)/4, $51000
DATA  fdct4x4_51000+0x0c(SB)/4, $51000
GLOBL fdct4x4_51000(SB), RODATA|NOPTR, $16

// 4 x i32 of 7, the column-pass +7 bias before >>4 (PSRAD by 4).
DATA  fdct4x4_7+0x00(SB)/4, $7
DATA  fdct4x4_7+0x04(SB)/4, $7
DATA  fdct4x4_7+0x08(SB)/4, $7
DATA  fdct4x4_7+0x0c(SB)/4, $7
GLOBL fdct4x4_7(SB), RODATA|NOPTR, $16

// 8 x int16 mask: low 4 lanes = 1, high 4 lanes = 0. ANDN result with
// (d == 0) compare result -> low 4 lanes get 1 where d != 0, high zeros.
DATA  fdct4x4_cmp_mask+0x00(SB)/4, $0x00010001
DATA  fdct4x4_cmp_mask+0x04(SB)/4, $0x00010001
DATA  fdct4x4_cmp_mask+0x08(SB)/4, $0x00000000
DATA  fdct4x4_cmp_mask+0x0c(SB)/4, $0x00000000
GLOBL fdct4x4_cmp_mask(SB), RODATA|NOPTR, $16

// forwardDCT4x4SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardDCT4x4SSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	stride+8(FP), AX
	MOVQ	output+16(FP), DI
	SHLQ	$1, AX                  // bytes per row = stride * 2

	// Load 4 rows of 4 int16 each (8 bytes per row).
	MOVQ	(SI), X0                // row 0: 03 02 01 00 (low 64 bits)
	LEAQ	(SI)(AX*1), BX
	MOVQ	(BX), X2                // row 1: 13 12 11 10
	LEAQ	(BX)(AX*1), BX
	MOVQ	(BX), X1                // row 2: 23 22 21 20
	LEAQ	(BX)(AX*1), BX
	MOVQ	(BX), X3                // row 3: 33 32 31 30

	// Pack rows pairwise.
	PUNPCKLQDQ	X2, X0          // X0 = 13 12 11 10 03 02 01 00
	PUNPCKLQDQ	X3, X1          // X1 = 33 32 31 30 23 22 21 20

	// Transpose into "pairs per row" lane layout.
	MOVO	X0, X2
	PUNPCKLLQ	X1, X0          // X0 = 23 22 03 02 21 20 01 00
	PUNPCKHLQ	X1, X2          // X2 = 33 32 13 12 31 30 11 10
	MOVO	X0, X1
	PUNPCKLLQ	X2, X0          // X0 = 31 21 30 20 11 10 01 00
	PSHUFHW	$0xb1, X1, X1           // X1 = 22 23 02 03 xx xx xx xx
	PSHUFHW	$0xb1, X2, X2           // X2 = 32 33 12 13 xx xx xx xx

	PUNPCKHLQ	X2, X1          // X1 = 32 33 22 23 12 13 02 03
	MOVO	X0, X3
	PADDW	X1, X0                  // X0 = b1 a1 b1 a1 b1 a1 b1 a1
	PSUBW	X1, X3                  // X3 = c1 d1 c1 d1 c1 d1 c1 d1
	PSLLW	$3, X0
	PSLLW	$3, X3

	// op[0] = a1+b1 ; op[2] = a1-b1 (via PMADDWD with [1,1] / [1,-1]).
	MOVO	X0, X1
	PMADDWL	fdct4x4_mult_add(SB), X0
	PMADDWL	fdct4x4_mult_sub(SB), X1
	MOVO	X3, X4
	PMADDWL	fdct4x4_5352_2217(SB), X3      // c1*2217 + d1*5352
	PMADDWL	fdct4x4_2217_neg5352(SB), X4   // d1*2217 - c1*5352

	PADDL	fdct4x4_14500(SB), X3
	PADDL	fdct4x4_7500(SB), X4
	PSRAL	$12, X3
	PSRAL	$12, X4

	PACKSSLW	X1, X0                  // X0 = op[2] op[0]
	PACKSSLW	X4, X3                  // X3 = op[3] op[1]

	// Transpose intermediate (rows of 4 lanes interleaved across xmm0/xmm3).
	MOVO	X0, X2
	PUNPCKLQDQ	X3, X0                  // X0 = 13 12 11 10 03 02 01 00
	PUNPCKHQDQ	X3, X2                  // X2 = 23 22 21 20 33 32 31 30

	MOVO	X0, X3
	PUNPCKLWL	X2, X0                  // X0 = 32 30 22 20 12 10 02 00
	PUNPCKHWL	X2, X3                  // X3 = 33 31 23 21 13 11 03 01
	MOVO	X0, X2
	PUNPCKLWL	X3, X0                  // X0 = 13 12 11 10 03 02 01 00
	PUNPCKHWL	X3, X2                  // X2 = 33 32 31 30 23 22 21 20

	PSHUFD	$0x4e, X2, X2                   // swap halves: 23 22 21 20 33 32 31 30
	MOVO	X0, X3
	PADDW	X2, X0                          // X0 = b1 b1 b1 b1 a1 a1 a1 a1
	PSUBW	X2, X3                          // X3 = c1 c1 c1 c1 d1 d1 d1 d1

	PSHUFD	$0xd8, X0, X0                   // X0 = b1 b1 a1 a1 b1 b1 a1 a1
	MOVO	X3, X2                          // save d1 for compare later
	PSHUFD	$0xd8, X3, X3                   // X3 = c1 c1 d1 d1 c1 c1 d1 d1
	PSHUFLW	$0xd8, X0, X0
	PSHUFLW	$0xd8, X3, X3
	PSHUFHW	$0xd8, X0, X0                   // X0 = b1 a1 b1 a1 b1 a1 b1 a1
	PSHUFHW	$0xd8, X3, X3                   // X3 = c1 d1 c1 d1 c1 d1 c1 d1

	MOVO	X0, X1
	PMADDWL	fdct4x4_mult_add(SB), X0      // a1+b1
	PMADDWL	fdct4x4_mult_sub(SB), X1      // a1-b1

	PXOR	X4, X4
	PADDL	fdct4x4_7(SB), X0
	PADDL	fdct4x4_7(SB), X1
	PCMPEQW	X4, X2                          // X2 = -1 in lanes where d == 0
	PSRAL	$4, X0                          // (a1+b1+7)>>4
	PSRAL	$4, X1                          // (a1-b1+7)>>4
	// Build (d != 0) mask in low 4 lanes:
	// PANDN dst, src does dst = (~dst) & src. With X2 = -1 where d == 0,
	// we want (~X2) & cmp_mask -> 1 in lanes where d != 0.
	PANDN	fdct4x4_cmp_mask(SB), X2

	MOVO	X3, X4
	PMADDWL	fdct4x4_5352_2217(SB), X3     // c1*2217 + d1*5352
	PMADDWL	fdct4x4_2217_neg5352(SB), X4  // d1*2217 - c1*5352
	PADDL	fdct4x4_12000(SB), X3
	PADDL	fdct4x4_51000(SB), X4
	PACKSSLW	X1, X0                  // X0 = op[8] op[0]
	PSRAL	$16, X3
	PSRAL	$16, X4

	PACKSSLW	X4, X3                  // X3 = op[12] op[4]
	MOVO	X0, X1
	PADDW	X2, X3                          // op[4] += (d1 != 0)
	PUNPCKLQDQ	X3, X0                  // X0 = op[4..7] op[0..3]
	PUNPCKHQDQ	X3, X1                  // X1 = op[12..15] op[8..11]

	MOVOU	X0, 0(DI)
	MOVOU	X1, 16(DI)
	RET
