//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0
// vpx_dsp/x86/fwd_txfm_impl_sse2.h::vpx_fdct8x8_sse2.
// The kernel calls one source-shaped 1-D DCT+transpose helper twice,
// then applies libvpx's signed divide-by-two post-condition.

#include "textflag.h"

DATA  vp9_fdct8x8_rounding<>+0x00(SB)/4, $8192
DATA  vp9_fdct8x8_rounding<>+0x04(SB)/4, $8192
DATA  vp9_fdct8x8_rounding<>+0x08(SB)/4, $8192
DATA  vp9_fdct8x8_rounding<>+0x0c(SB)/4, $8192
GLOBL vp9_fdct8x8_rounding<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_p16p16<>+0x00(SB)/4, $0x2d412d41
DATA  vp9_fdct8x8_p16p16<>+0x04(SB)/4, $0x2d412d41
DATA  vp9_fdct8x8_p16p16<>+0x08(SB)/4, $0x2d412d41
DATA  vp9_fdct8x8_p16p16<>+0x0c(SB)/4, $0x2d412d41
GLOBL vp9_fdct8x8_p16p16<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_p16m16<>+0x00(SB)/4, $0xd2bf2d41
DATA  vp9_fdct8x8_p16m16<>+0x04(SB)/4, $0xd2bf2d41
DATA  vp9_fdct8x8_p16m16<>+0x08(SB)/4, $0xd2bf2d41
DATA  vp9_fdct8x8_p16m16<>+0x0c(SB)/4, $0xd2bf2d41
GLOBL vp9_fdct8x8_p16m16<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_p24p08<>+0x00(SB)/4, $0x3b21187e
DATA  vp9_fdct8x8_p24p08<>+0x04(SB)/4, $0x3b21187e
DATA  vp9_fdct8x8_p24p08<>+0x08(SB)/4, $0x3b21187e
DATA  vp9_fdct8x8_p24p08<>+0x0c(SB)/4, $0x3b21187e
GLOBL vp9_fdct8x8_p24p08<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_m08p24<>+0x00(SB)/4, $0x187ec4df
DATA  vp9_fdct8x8_m08p24<>+0x04(SB)/4, $0x187ec4df
DATA  vp9_fdct8x8_m08p24<>+0x08(SB)/4, $0x187ec4df
DATA  vp9_fdct8x8_m08p24<>+0x0c(SB)/4, $0x187ec4df
GLOBL vp9_fdct8x8_m08p24<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_p28p04<>+0x00(SB)/4, $0x3ec50c7c
DATA  vp9_fdct8x8_p28p04<>+0x04(SB)/4, $0x3ec50c7c
DATA  vp9_fdct8x8_p28p04<>+0x08(SB)/4, $0x3ec50c7c
DATA  vp9_fdct8x8_p28p04<>+0x0c(SB)/4, $0x3ec50c7c
GLOBL vp9_fdct8x8_p28p04<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_m04p28<>+0x00(SB)/4, $0x0c7cc13b
DATA  vp9_fdct8x8_m04p28<>+0x04(SB)/4, $0x0c7cc13b
DATA  vp9_fdct8x8_m04p28<>+0x08(SB)/4, $0x0c7cc13b
DATA  vp9_fdct8x8_m04p28<>+0x0c(SB)/4, $0x0c7cc13b
GLOBL vp9_fdct8x8_m04p28<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_p12p20<>+0x00(SB)/4, $0x238e3537
DATA  vp9_fdct8x8_p12p20<>+0x04(SB)/4, $0x238e3537
DATA  vp9_fdct8x8_p12p20<>+0x08(SB)/4, $0x238e3537
DATA  vp9_fdct8x8_p12p20<>+0x0c(SB)/4, $0x238e3537
GLOBL vp9_fdct8x8_p12p20<>(SB), RODATA|NOPTR, $16

DATA  vp9_fdct8x8_m20p12<>+0x00(SB)/4, $0x3537dc72
DATA  vp9_fdct8x8_m20p12<>+0x04(SB)/4, $0x3537dc72
DATA  vp9_fdct8x8_m20p12<>+0x08(SB)/4, $0x3537dc72
DATA  vp9_fdct8x8_m20p12<>+0x0c(SB)/4, $0x3537dc72
GLOBL vp9_fdct8x8_m20p12<>(SB), RODATA|NOPTR, $16

#define FDCT8_MADD_PACK(X_LO, X_HI, C, X_OUT, X_TMP) \
	MOVO     X_LO, X_OUT                    \
	PMADDWL  C<>(SB), X_OUT                 \
	PADDL    vp9_fdct8x8_rounding<>(SB), X_OUT \
	PSRAL    $14, X_OUT                     \
	MOVO     X_HI, X_TMP                    \
	PMADDWL  C<>(SB), X_TMP                 \
	PADDL    vp9_fdct8x8_rounding<>(SB), X_TMP \
	PSRAL    $14, X_TMP                     \
	PACKSSLW X_TMP, X_OUT

#define FDCT8_FINAL_STORE(X_IN, OFF, X_TMP, R_DST) \
	MOVO  X_IN, X_TMP     \
	PSRAW $15, X_TMP      \
	PSUBW X_TMP, X_IN     \
	PSRAW $1, X_IN        \
	MOVOU X_IN, OFF(R_DST)

// forwardDCT8x8SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardDCT8x8SSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	stride+8(FP), DX
	MOVQ	output+16(FP), DI
	SHLQ	$1, DX

	MOVOU	(SI), X0
	MOVOU	(SI)(DX*1), X1
	LEAQ	(SI)(DX*2), R8
	MOVOU	(R8), X2
	MOVOU	(R8)(DX*1), X3
	LEAQ	(R8)(DX*2), R9
	MOVOU	(R9), X4
	MOVOU	(R9)(DX*1), X5
	LEAQ	(R9)(DX*2), R10
	MOVOU	(R10), X6
	MOVOU	(R10)(DX*1), X7

	PSLLW	$2, X0
	PSLLW	$2, X1
	PSLLW	$2, X2
	PSLLW	$2, X3
	PSLLW	$2, X4
	PSLLW	$2, X5
	PSLLW	$2, X6
	PSLLW	$2, X7

	CALL	·forwardDCT8x8PassSSE2(SB)
	CALL	·forwardDCT8x8PassSSE2(SB)

	FDCT8_FINAL_STORE(X0, 0, X8, DI)
	FDCT8_FINAL_STORE(X1, 16, X8, DI)
	FDCT8_FINAL_STORE(X2, 32, X8, DI)
	FDCT8_FINAL_STORE(X3, 48, X8, DI)
	FDCT8_FINAL_STORE(X4, 64, X8, DI)
	FDCT8_FINAL_STORE(X5, 80, X8, DI)
	FDCT8_FINAL_STORE(X6, 96, X8, DI)
	FDCT8_FINAL_STORE(X7, 112, X8, DI)
	RET

// forwardDCT8x8PassSSE2 consumes rows in X0..X7, computes one libvpx
// FDCT8x8 1-D pass, and returns the transposed results in X0..X7.
// Stack layout:
//   0..127    q0..q7
//   128..255  res0..res7
TEXT ·forwardDCT8x8PassSSE2(SB), NOSPLIT, $256-0
	MOVO	X0, X8
	PADDW	X7, X8
	MOVOU	X8, 0(SP)
	MOVO	X1, X8
	PADDW	X6, X8
	MOVOU	X8, 16(SP)
	MOVO	X2, X8
	PADDW	X5, X8
	MOVOU	X8, 32(SP)
	MOVO	X3, X8
	PADDW	X4, X8
	MOVOU	X8, 48(SP)
	MOVO	X3, X8
	PSUBW	X4, X8
	MOVOU	X8, 64(SP)
	MOVO	X2, X8
	PSUBW	X5, X8
	MOVOU	X8, 80(SP)
	MOVO	X1, X8
	PSUBW	X6, X8
	MOVOU	X8, 96(SP)
	MOVO	X0, X8
	PSUBW	X7, X8
	MOVOU	X8, 112(SP)

	// Even outputs res0/res2/res4/res6.
	MOVOU	0(SP), X0
	MOVOU	48(SP), X3
	MOVO	X0, X8
	PADDW	X3, X8          // r0
	MOVO	X0, X11
	PSUBW	X3, X11         // r3
	MOVOU	16(SP), X1
	MOVOU	32(SP), X2
	MOVO	X1, X9
	PADDW	X2, X9          // r1
	MOVO	X1, X10
	PSUBW	X2, X10         // r2

	MOVO	X8, X0
	PUNPCKLWL X9, X0
	MOVO	X8, X1
	PUNPCKHWL X9, X1
	FDCT8_MADD_PACK(X0, X1, vp9_fdct8x8_p16p16, X12, X13)
	MOVOU	X12, 128(SP)
	FDCT8_MADD_PACK(X0, X1, vp9_fdct8x8_p16m16, X12, X13)
	MOVOU	X12, 192(SP)

	MOVO	X10, X2
	PUNPCKLWL X11, X2
	MOVO	X10, X3
	PUNPCKHWL X11, X3
	FDCT8_MADD_PACK(X2, X3, vp9_fdct8x8_p24p08, X12, X13)
	MOVOU	X12, 160(SP)
	FDCT8_MADD_PACK(X2, X3, vp9_fdct8x8_m08p24, X12, X13)
	MOVOU	X12, 224(SP)

	// Odd outputs res1/res3/res5/res7.
	MOVOU	96(SP), X0      // q6
	MOVOU	80(SP), X1      // q5
	MOVO	X0, X2
	PUNPCKLWL X1, X2
	MOVO	X0, X3
	PUNPCKHWL X1, X3
	FDCT8_MADD_PACK(X2, X3, vp9_fdct8x8_p16m16, X4, X5)
	FDCT8_MADD_PACK(X2, X3, vp9_fdct8x8_p16p16, X6, X7)

	MOVOU	64(SP), X0      // q4
	MOVO	X0, X8
	PADDW	X4, X8          // x0
	MOVO	X0, X9
	PSUBW	X4, X9          // x1
	MOVOU	112(SP), X1     // q7
	MOVO	X1, X10
	PSUBW	X6, X10         // x2
	MOVO	X1, X11
	PADDW	X6, X11         // x3

	MOVO	X8, X0
	PUNPCKLWL X11, X0
	MOVO	X8, X1
	PUNPCKHWL X11, X1
	FDCT8_MADD_PACK(X0, X1, vp9_fdct8x8_p28p04, X12, X13)
	MOVOU	X12, 144(SP)
	FDCT8_MADD_PACK(X0, X1, vp9_fdct8x8_m04p28, X12, X13)
	MOVOU	X12, 240(SP)

	MOVO	X9, X2
	PUNPCKLWL X10, X2
	MOVO	X9, X3
	PUNPCKHWL X10, X3
	FDCT8_MADD_PACK(X2, X3, vp9_fdct8x8_p12p20, X12, X13)
	MOVOU	X12, 208(SP)
	FDCT8_MADD_PACK(X2, X3, vp9_fdct8x8_m20p12, X12, X13)
	MOVOU	X12, 176(SP)

	// Transpose res0..res7 into the next pass' row vectors.
	MOVOU	128(SP), X0
	MOVOU	144(SP), X1
	MOVOU	160(SP), X2
	MOVOU	176(SP), X3
	MOVOU	192(SP), X4
	MOVOU	208(SP), X5
	MOVOU	224(SP), X6
	MOVOU	240(SP), X7

	MOVO	X0, X8
	PUNPCKLWL X1, X8
	MOVO	X2, X9
	PUNPCKLWL X3, X9
	MOVO	X0, X10
	PUNPCKHWL X1, X10
	MOVO	X2, X11
	PUNPCKHWL X3, X11
	MOVO	X4, X12
	PUNPCKLWL X5, X12
	MOVO	X6, X13
	PUNPCKLWL X7, X13
	MOVO	X4, X14
	PUNPCKHWL X5, X14
	MOVO	X6, X15
	PUNPCKHWL X7, X15

	MOVOU	X8, 0(SP)
	MOVOU	X9, 16(SP)
	MOVOU	X10, 32(SP)
	MOVOU	X11, 48(SP)
	MOVOU	X12, 64(SP)
	MOVOU	X13, 80(SP)
	MOVOU	X14, 96(SP)
	MOVOU	X15, 112(SP)

	MOVOU	0(SP), X8
	MOVOU	16(SP), X9
	MOVO	X8, X0
	PUNPCKLLQ X9, X0       // tr1_0
	MOVO	X8, X2
	PUNPCKHLQ X9, X2       // tr1_2
	MOVOU	64(SP), X12
	MOVOU	80(SP), X13
	MOVO	X12, X4
	PUNPCKLLQ X13, X4      // tr1_4
	MOVO	X12, X6
	PUNPCKHLQ X13, X6      // tr1_6
	MOVO	X0, X1
	PUNPCKHQDQ X4, X1
	PUNPCKLQDQ X4, X0
	MOVO	X2, X3
	PUNPCKHQDQ X6, X3
	PUNPCKLQDQ X6, X2

	MOVOU	32(SP), X8
	MOVOU	48(SP), X9
	MOVO	X8, X4
	PUNPCKLLQ X9, X4       // tr1_1
	MOVO	X8, X6
	PUNPCKHLQ X9, X6       // tr1_3
	MOVOU	96(SP), X12
	MOVOU	112(SP), X13
	MOVO	X12, X5
	PUNPCKLLQ X13, X5      // tr1_5
	MOVO	X12, X7
	PUNPCKHLQ X13, X7      // tr1_7
	MOVO	X4, X8
	PUNPCKHQDQ X5, X8
	PUNPCKLQDQ X5, X4
	MOVO	X6, X9
	PUNPCKHQDQ X7, X9
	PUNPCKLQDQ X7, X6
	MOVO	X8, X5
	MOVO	X9, X7
	RET
