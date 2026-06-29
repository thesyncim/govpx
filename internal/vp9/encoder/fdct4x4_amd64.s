//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0 vpx_dsp/fwd_txfm.c::vpx_fdct4x4_c.
// The kernel processes four columns in parallel, keeps the libvpx
// round_shift semantics, and stores byte-identical coefficients for the
// encoder residual range.

#include "textflag.h"

// 4 x int32 rounding bias used by fdct_round_shift().
DATA  vp9_fdct4x4_rounding+0x00(SB)/4, $8192
DATA  vp9_fdct4x4_rounding+0x04(SB)/4, $8192
DATA  vp9_fdct4x4_rounding+0x08(SB)/4, $8192
DATA  vp9_fdct4x4_rounding+0x0c(SB)/4, $8192
GLOBL vp9_fdct4x4_rounding(SB), RODATA|NOPTR, $16

// PMADDWD multiplier pairs. The second word is zero for single-input
// multiplies, so [x, 0] * [cospi16, 0] becomes x*cospi16.
DATA  vp9_fdct4x4_cospi16_zero+0x00(SB)/4, $0x00002d41
DATA  vp9_fdct4x4_cospi16_zero+0x04(SB)/4, $0x00002d41
DATA  vp9_fdct4x4_cospi16_zero+0x08(SB)/4, $0x00002d41
DATA  vp9_fdct4x4_cospi16_zero+0x0c(SB)/4, $0x00002d41
GLOBL vp9_fdct4x4_cospi16_zero(SB), RODATA|NOPTR, $16

DATA  vp9_fdct4x4_cospi16_cospi16+0x00(SB)/4, $0x2d412d41
DATA  vp9_fdct4x4_cospi16_cospi16+0x04(SB)/4, $0x2d412d41
DATA  vp9_fdct4x4_cospi16_cospi16+0x08(SB)/4, $0x2d412d41
DATA  vp9_fdct4x4_cospi16_cospi16+0x0c(SB)/4, $0x2d412d41
GLOBL vp9_fdct4x4_cospi16_cospi16(SB), RODATA|NOPTR, $16

// [cospi24, cospi8] = [6270, 15137].
DATA  vp9_fdct4x4_cospi24_cospi8+0x00(SB)/4, $0x3b21187e
DATA  vp9_fdct4x4_cospi24_cospi8+0x04(SB)/4, $0x3b21187e
DATA  vp9_fdct4x4_cospi24_cospi8+0x08(SB)/4, $0x3b21187e
DATA  vp9_fdct4x4_cospi24_cospi8+0x0c(SB)/4, $0x3b21187e
GLOBL vp9_fdct4x4_cospi24_cospi8(SB), RODATA|NOPTR, $16

// [cospi24, -cospi24] = [6270, -6270].
DATA  vp9_fdct4x4_cospi24_negcospi24+0x00(SB)/4, $0xe782187e
DATA  vp9_fdct4x4_cospi24_negcospi24+0x04(SB)/4, $0xe782187e
DATA  vp9_fdct4x4_cospi24_negcospi24+0x08(SB)/4, $0xe782187e
DATA  vp9_fdct4x4_cospi24_negcospi24+0x0c(SB)/4, $0xe782187e
GLOBL vp9_fdct4x4_cospi24_negcospi24(SB), RODATA|NOPTR, $16

// [cospi8, -cospi8] = [15137, -15137].
DATA  vp9_fdct4x4_cospi8_negcospi8+0x00(SB)/4, $0xc4df3b21
DATA  vp9_fdct4x4_cospi8_negcospi8+0x04(SB)/4, $0xc4df3b21
DATA  vp9_fdct4x4_cospi8_negcospi8+0x08(SB)/4, $0xc4df3b21
DATA  vp9_fdct4x4_cospi8_negcospi8+0x0c(SB)/4, $0xc4df3b21
GLOBL vp9_fdct4x4_cospi8_negcospi8(SB), RODATA|NOPTR, $16

// [-cospi8, cospi8] = [-15137, 15137].
DATA  vp9_fdct4x4_negcospi8_cospi8+0x00(SB)/4, $0x3b21c4df
DATA  vp9_fdct4x4_negcospi8_cospi8+0x04(SB)/4, $0x3b21c4df
DATA  vp9_fdct4x4_negcospi8_cospi8+0x08(SB)/4, $0x3b21c4df
DATA  vp9_fdct4x4_negcospi8_cospi8+0x0c(SB)/4, $0x3b21c4df
GLOBL vp9_fdct4x4_negcospi8_cospi8(SB), RODATA|NOPTR, $16

// [-cospi8, cospi24] = [-15137, 6270].
DATA  vp9_fdct4x4_negcospi8_cospi24+0x00(SB)/4, $0x187ec4df
DATA  vp9_fdct4x4_negcospi8_cospi24+0x04(SB)/4, $0x187ec4df
DATA  vp9_fdct4x4_negcospi8_cospi24+0x08(SB)/4, $0x187ec4df
DATA  vp9_fdct4x4_negcospi8_cospi24+0x0c(SB)/4, $0x187ec4df
GLOBL vp9_fdct4x4_negcospi8_cospi24(SB), RODATA|NOPTR, $16

// libvpx vpx_fdct4x4_c adds one to input[0] after the initial <<4 when
// input[0] is non-zero. This mask limits that conditional add to lane 0.
DATA  vp9_fdct4x4_lane0_one+0x00(SB)/4, $0x00000001
DATA  vp9_fdct4x4_lane0_one+0x04(SB)/4, $0x00000000
DATA  vp9_fdct4x4_lane0_one+0x08(SB)/4, $0x00000000
DATA  vp9_fdct4x4_lane0_one+0x0c(SB)/4, $0x00000000
GLOBL vp9_fdct4x4_lane0_one(SB), RODATA|NOPTR, $16

DATA  vp9_fdct4x4_one+0x00(SB)/4, $0x00010001
DATA  vp9_fdct4x4_one+0x04(SB)/4, $0x00010001
DATA  vp9_fdct4x4_one+0x08(SB)/4, $0x00010001
DATA  vp9_fdct4x4_one+0x0c(SB)/4, $0x00010001
GLOBL vp9_fdct4x4_one(SB), RODATA|NOPTR, $16

// forwardDCT4x4SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardDCT4x4SSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	stride+8(FP), DX
	MOVQ	output+16(FP), DI
	SHLQ	$1, DX                  // bytes per row = stride * 2

	// Load four input rows. Low four words are the active columns.
	MOVQ	(SI), X0
	MOVQ	(SI)(DX*1), X1
	LEAQ	(SI)(DX*2), R8
	MOVQ	(R8), X2
	MOVQ	(R8)(DX*1), X3

	PXOR	X15, X15                // zero

	PSLLW	$4, X0
	PSLLW	$4, X1
	PSLLW	$4, X2
	PSLLW	$4, X3

	// Add one to lane 0 iff input[0] was non-zero.
	MOVO	X0, X4
	PCMPEQW	X15, X4
	PANDN	vp9_fdct4x4_lane0_one(SB), X4
	PADDW	X4, X0

	// Pass 0: column transform. Lanes are columns 0..3.
	MOVO	X0, X4
	PADDW	X3, X4                  // step0
	MOVO	X1, X5
	PADDW	X2, X5                  // step1
	MOVO	X1, X6
	PSUBW	X2, X6                  // step2
	MOVO	X0, X7
	PSUBW	X3, X7                  // step3

	MOVO	X4, X8
	PADDW	X5, X8
	PUNPCKLWL	X15, X8
	PMADDWL	vp9_fdct4x4_cospi16_zero(SB), X8
	PADDL	vp9_fdct4x4_rounding(SB), X8
	PSRAL	$14, X8                 // pass0 out0

	MOVO	X4, X9
	PSUBW	X5, X9
	PUNPCKLWL	X15, X9
	PMADDWL	vp9_fdct4x4_cospi16_zero(SB), X9
	PADDL	vp9_fdct4x4_rounding(SB), X9
	PSRAL	$14, X9                 // pass0 out2

	MOVO	X6, X10
	PUNPCKLWL	X7, X10
	MOVO	X10, X11
	PMADDWL	vp9_fdct4x4_cospi24_cospi8(SB), X10
	PADDL	vp9_fdct4x4_rounding(SB), X10
	PSRAL	$14, X10                // pass0 out1

	PMADDWL	vp9_fdct4x4_negcospi8_cospi24(SB), X11
	PADDL	vp9_fdct4x4_rounding(SB), X11
	PSRAL	$14, X11                // pass0 out3

	PACKSSLW	X8, X8
	PACKSSLW	X10, X10
	PACKSSLW	X9, X9
	PACKSSLW	X11, X11

	// Transpose the pass0 coefficient vectors into the intermediate layout
	// consumed by the scalar pass1 loop.
	MOVO	X8, X0
	PUNPCKLWL	X10, X0
	MOVO	X9, X1
	PUNPCKLWL	X11, X1
	MOVO	X0, X4
	PUNPCKLLQ	X1, X0
	PUNPCKHLQ	X1, X4
	MOVO	X0, X8
	MOVO	X0, X10
	PUNPCKHQDQ	X10, X10
	MOVO	X4, X9
	MOVO	X4, X11
	PUNPCKHQDQ	X11, X11

	// Pass 1: row transform. Inputs are pass0 output vectors. Use 32-bit
	// PMADD sums here because high-amplitude residuals can exceed int16
	// during the second-pass butterflies.
	MOVO	X8, X0
	PUNPCKLWL	X11, X0
	PMADDWL	vp9_fdct4x4_cospi16_cospi16(SB), X0
	MOVO	X10, X4
	PUNPCKLWL	X9, X4
	PMADDWL	vp9_fdct4x4_cospi16_cospi16(SB), X4
	MOVO	X0, X1
	PADDL	X4, X0
	PSUBL	X4, X1
	PADDL	vp9_fdct4x4_rounding(SB), X0
	PADDL	vp9_fdct4x4_rounding(SB), X1
	PSRAL	$14, X0                 // final out0
	PSRAL	$14, X1                 // final out2

	MOVO	X10, X2
	PUNPCKLWL	X9, X2
	PMADDWL	vp9_fdct4x4_cospi24_negcospi24(SB), X2
	MOVO	X8, X4
	PUNPCKLWL	X11, X4
	PMADDWL	vp9_fdct4x4_cospi8_negcospi8(SB), X4
	PADDL	X4, X2
	PADDL	vp9_fdct4x4_rounding(SB), X2
	PSRAL	$14, X2                 // final out1

	MOVO	X10, X3
	PUNPCKLWL	X9, X3
	PMADDWL	vp9_fdct4x4_negcospi8_cospi8(SB), X3
	MOVO	X8, X4
	PUNPCKLWL	X11, X4
	PMADDWL	vp9_fdct4x4_cospi24_negcospi24(SB), X4
	PADDL	X4, X3
	PADDL	vp9_fdct4x4_rounding(SB), X3
	PSRAL	$14, X3                 // final out3

	PACKSSLW	X0, X0
	PACKSSLW	X2, X2
	PACKSSLW	X1, X1
	PACKSSLW	X3, X3

	// output[i] = (final[i] + 1) >> 2.
	PADDW	vp9_fdct4x4_one(SB), X0
	PADDW	vp9_fdct4x4_one(SB), X2
	PADDW	vp9_fdct4x4_one(SB), X1
	PADDW	vp9_fdct4x4_one(SB), X3
	PSRAW	$2, X0
	PSRAW	$2, X2
	PSRAW	$2, X1
	PSRAW	$2, X3

	// Transpose coefficient vectors back into raster-order 4x4 output.
	MOVO	X0, X4
	PUNPCKLWL	X2, X4
	MOVO	X1, X5
	PUNPCKLWL	X3, X5
	MOVO	X4, X6
	PUNPCKLLQ	X5, X4
	PUNPCKHLQ	X5, X6

	MOVOU	X4, 0(DI)
	MOVOU	X6, 16(DI)
	RET
