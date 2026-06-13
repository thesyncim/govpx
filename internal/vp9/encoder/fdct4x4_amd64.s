//go:build amd64 && !purego

// SSE2 residual-domain VP9 4x4 forward DCT. The arithmetic mirrors
// libvpx v1.16.0 vpx_dsp/fwd_txfm.c vpx_fdct4x4_c for 8-bit encoder
// residuals (input samples in [-255, 255]). The Go dispatcher falls back
// to the scalar reference outside that domain so the public helper keeps
// its broader int16 contract.

#include "textflag.h"

// 8 x int16 pairs [cospi16_64, 0]. PMADDWL with values interleaved with
// zero produces four int32 products, one per lane.
DATA  vp9Fdct4x4Cos16Zero<>+0x00(SB)/4, $0x00002d41
DATA  vp9Fdct4x4Cos16Zero<>+0x04(SB)/4, $0x00002d41
DATA  vp9Fdct4x4Cos16Zero<>+0x08(SB)/4, $0x00002d41
DATA  vp9Fdct4x4Cos16Zero<>+0x0c(SB)/4, $0x00002d41
GLOBL vp9Fdct4x4Cos16Zero<>(SB), RODATA|NOPTR, $16

// 8 x int16 pairs [cospi24_64, cospi8_64].
DATA  vp9Fdct4x4Cos24Cos8<>+0x00(SB)/4, $0x3b21187e
DATA  vp9Fdct4x4Cos24Cos8<>+0x04(SB)/4, $0x3b21187e
DATA  vp9Fdct4x4Cos24Cos8<>+0x08(SB)/4, $0x3b21187e
DATA  vp9Fdct4x4Cos24Cos8<>+0x0c(SB)/4, $0x3b21187e
GLOBL vp9Fdct4x4Cos24Cos8<>(SB), RODATA|NOPTR, $16

// 8 x int16 pairs [-cospi8_64, cospi24_64].
DATA  vp9Fdct4x4NegCos8Cos24<>+0x00(SB)/4, $0x187ec4df
DATA  vp9Fdct4x4NegCos8Cos24<>+0x04(SB)/4, $0x187ec4df
DATA  vp9Fdct4x4NegCos8Cos24<>+0x08(SB)/4, $0x187ec4df
DATA  vp9Fdct4x4NegCos8Cos24<>+0x0c(SB)/4, $0x187ec4df
GLOBL vp9Fdct4x4NegCos8Cos24<>(SB), RODATA|NOPTR, $16

DATA  vp9Fdct4x4Round<>+0x00(SB)/4, $8192
DATA  vp9Fdct4x4Round<>+0x04(SB)/4, $8192
DATA  vp9Fdct4x4Round<>+0x08(SB)/4, $8192
DATA  vp9Fdct4x4Round<>+0x0c(SB)/4, $8192
GLOBL vp9Fdct4x4Round<>(SB), RODATA|NOPTR, $16

DATA  vp9Fdct4x4FinalRound<>+0x00(SB)/4, $1
DATA  vp9Fdct4x4FinalRound<>+0x04(SB)/4, $1
DATA  vp9Fdct4x4FinalRound<>+0x08(SB)/4, $1
DATA  vp9Fdct4x4FinalRound<>+0x0c(SB)/4, $1
GLOBL vp9Fdct4x4FinalRound<>(SB), RODATA|NOPTR, $16

DATA  vp9Fdct4x4Lane0One<>+0x00(SB)/4, $0x00000001
DATA  vp9Fdct4x4Lane0One<>+0x04(SB)/4, $0x00000000
DATA  vp9Fdct4x4Lane0One<>+0x08(SB)/4, $0x00000000
DATA  vp9Fdct4x4Lane0One<>+0x0c(SB)/4, $0x00000000
GLOBL vp9Fdct4x4Lane0One<>(SB), RODATA|NOPTR, $16

#define VP9_FDCT4_ROUND_MUL_1(X_IN, X_OUT) \
	MOVO      X_IN, X_OUT                  \
	PUNPCKLWL X7, X_OUT                    \
	PMADDWL   vp9Fdct4x4Cos16Zero<>(SB), X_OUT \
	PADDL     vp9Fdct4x4Round<>(SB), X_OUT \
	PSRAL     $14, X_OUT

#define VP9_FDCT4_ROUND_MUL_2(X_A, X_B, X_OUT, X_CONST) \
	MOVO      X_A, X_OUT                    \
	PUNPCKLWL X_B, X_OUT                    \
	PMADDWL   X_CONST, X_OUT                \
	PADDL     vp9Fdct4x4Round<>(SB), X_OUT  \
	PSRAL     $14, X_OUT

#define VP9_FDCT4_PASS(X_IN0, X_IN1, X_IN2, X_IN3, X_OUT0, X_OUT1, X_OUT2, X_OUT3) \
	MOVO  X_IN0, X4                    \
	PADDW X_IN3, X4                    \
	MOVO  X_IN1, X5                    \
	PADDW X_IN2, X5                    \
	PSUBW X_IN2, X_IN1                 \
	PSUBW X_IN3, X_IN0                 \
	MOVO  X4, X6                       \
	PADDW X5, X6                       \
	VP9_FDCT4_ROUND_MUL_1(X6, X_OUT0)  \
	PSUBW X5, X4                       \
	VP9_FDCT4_ROUND_MUL_1(X4, X_OUT2)  \
	VP9_FDCT4_ROUND_MUL_2(X_IN1, X_IN0, X_OUT1, vp9Fdct4x4Cos24Cos8<>(SB)) \
	VP9_FDCT4_ROUND_MUL_2(X_IN1, X_IN0, X_OUT3, vp9Fdct4x4NegCos8Cos24<>(SB))

// forwardDCT4x4SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardDCT4x4SSE2(SB), NOSPLIT, $0-24
	MOVQ input+0(FP), SI
	MOVQ stride+8(FP), AX
	MOVQ output+16(FP), DI
	SHLQ $1, AX                    // bytes per row = stride * 2

	PXOR X7, X7

	// Load four 4x int16 rows, then apply the libvpx first-pass *16 scale.
	MOVQ (SI), X0
	LEAQ (SI)(AX*1), BX
	MOVQ (BX), X1
	LEAQ (BX)(AX*1), BX
	MOVQ (BX), X2
	LEAQ (BX)(AX*1), BX
	MOVQ (BX), X3
	PSLLW $4, X0
	PSLLW $4, X1
	PSLLW $4, X2
	PSLLW $4, X3

	// vpx_fdct4x4_c: if (i == 0 && in[0] != 0) in[0] += 1.
	MOVWQZX (SI), CX
	TESTQ CX, CX
	JZ no_top_left_bias
	PADDW vp9Fdct4x4Lane0One<>(SB), X0
no_top_left_bias:

	// First pass over the four columns. X8..X11 are intermediate rows 0..3.
	VP9_FDCT4_PASS(X0, X1, X2, X3, X8, X9, X10, X11)

	PACKSSLW X9, X8                // X8  = row0 | row1
	PACKSSLW X11, X10              // X10 = row2 | row3

	// Transpose the pass-0 frequency rows into the scalar pass-1 input
	// layout: X0..X3 become original columns, with vertical frequency in
	// the low four lanes.
	MOVO X8, X0
	MOVO X8, X1
	PSRLDQ $8, X1
	MOVO X10, X2
	MOVO X10, X3
	PSRLDQ $8, X3
	PUNPCKLWL X1, X0
	PUNPCKLWL X3, X2
	MOVO X0, X1
	PUNPCKLLQ X2, X0
	PUNPCKHLQ X2, X1
	MOVO X0, X2
	PSRLDQ $8, X2
	MOVO X1, X3
	PSRLDQ $8, X3
	MOVO X1, X4
	MOVO X2, X1
	MOVO X4, X2

	// Second pass over rows, one column per lane.
	VP9_FDCT4_PASS(X0, X1, X2, X3, X8, X9, X10, X11)

	PADDL vp9Fdct4x4FinalRound<>(SB), X8
	PADDL vp9Fdct4x4FinalRound<>(SB), X9
	PADDL vp9Fdct4x4FinalRound<>(SB), X10
	PADDL vp9Fdct4x4FinalRound<>(SB), X11
	PSRAL $2, X8
	PSRAL $2, X9
	PSRAL $2, X10
	PSRAL $2, X11

	PACKSSLW X9, X8
	PACKSSLW X11, X10

	// X8/X10 hold row-frequency vectors. Transpose back to the scalar
	// vpx_fdct4x4_c memory order: [f0c0,f1c0,f2c0,f3c0, f0c1,...].
	MOVO X8, X0
	MOVO X8, X1
	PSRLDQ $8, X1
	MOVO X10, X2
	MOVO X10, X3
	PSRLDQ $8, X3
	PUNPCKLWL X1, X0
	PUNPCKLWL X3, X2
	MOVO X0, X1
	PUNPCKLLQ X2, X0
	PUNPCKHLQ X2, X1

	MOVOU X0, 0(DI)
	MOVOU X1, 16(DI)
	RET
