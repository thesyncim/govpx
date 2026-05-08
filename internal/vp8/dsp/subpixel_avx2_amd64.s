// AVX2 port of the libvpx v1.16.0 VP8 six-tap subpel predictor for
// the 16x16 size. Mirrors the SSE2 kernel in subpixel_amd64.s but
// processes 16 columns per iteration in a pair of YMM accumulators
// using VEX-encoded operations.
//
// Per row of 16 outputs:
//   v[x] = sum_{k=0..5} src[x+k] * hF[k] + 64       (x in 0..15)
//   tmp[x] = clip255(v >> 7)
// Then a vertical pass mirrors this against the tmp buffer.
//
// Schedule: hold 16 int32 partials across two YMMs:
//   Y14 covers positions 0..3 in lo 128, positions 8..11 in hi 128.
//   Y15 covers positions 4..7 in lo 128, positions 12..15 in hi 128.
// Per tap pair (k, k+1):
//   Y_a = src[k..k+15] widened to 16 int16 (lo 128 = pos0..7, hi 128 = pos8..15)
//   Y_b = src[k+1..k+16] widened to 16 int16 (same layout)
//   VPUNPCKLWD Y_b, Y_a -> 8 int32 lanes interleaved per 128-half
//                          (lo half = pos0..3 pairs; hi half = pos8..11 pairs)
//   VPUNPCKHWD Y_b, Y_a -> (lo half = pos4..7 pairs; hi half = pos12..15 pairs)
//   VPMADDWD by [hF[k], hF[k+1], ...] coefficient YMM gives the int32
//   partials that go into Y14 / Y15 respectively.
// After three pair builds + two adds we have the 16-position int32
// partials. Add bias 64, signed-shift right by 7, then:
//   VPACKSSDW Y15, Y14 -> per-128-half pack: lo half holds pos0..7 int16,
//                          hi half holds pos8..15 int16.
//   VEXTRACTI128 + VPACKUSWB folds to 16 bytes in one XMM.
//
// Calling convention (sixTapPredict16x16AVX2, ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[21*16]byte

#include "textflag.h"

DATA  sixtapAVX2Bias64<>+0x00(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x04(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x08(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x0c(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x10(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x14(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x18(SB)/4, $64
DATA  sixtapAVX2Bias64<>+0x1c(SB)/4, $64
GLOBL sixtapAVX2Bias64<>(SB), RODATA|NOPTR, $32

// LOAD_FILTER_PAIR_YMM: build a YMM coefficient vector with pattern
// [hF[k], hF[k+1], ...] repeated 8 times. Uses AX, DX as scratch.
#define LOAD_FILTER_PAIR_YMM(R_F, BYTEOFF, Y_OUT, X_OUT) \
	MOVWQZX (BYTEOFF)(R_F), AX     \
	MOVWQZX (BYTEOFF+2)(R_F), DX   \
	SHLQ    $16, DX                \
	ORQ     AX, DX                 \
	MOVQ    DX, X_OUT              \
	VPSHUFD $0, X_OUT, X_OUT       \
	VINSERTI128 $1, X_OUT, Y_OUT, Y_OUT

// HPAIR16_BUILD: horizontal pair build. Loads 16 bytes at +BYTEOFF and
// 16 bytes at +(BYTEOFF+1) (within-row byte shift) and produces the
// initial pair partials in Y_AC_LO / Y_AC_HI. Scratches X8/Y10, X9/Y11.
#define HPAIR16_BUILD(R_BASE, BYTEOFF, Y_COEFF, Y_AC_LO, Y_AC_HI) \
	VMOVDQU	(BYTEOFF)(R_BASE), X8           \
	VPMOVZXBW X8, Y10                        \
	VMOVDQU	(BYTEOFF+1)(R_BASE), X9         \
	VPMOVZXBW X9, Y11                        \
	VPUNPCKLWD Y11, Y10, Y_AC_LO             \
	VPMADDWD   Y_COEFF, Y_AC_LO, Y_AC_LO     \
	VPUNPCKHWD Y11, Y10, Y_AC_HI             \
	VPMADDWD   Y_COEFF, Y_AC_HI, Y_AC_HI

// HPAIR16_ADD: horizontal pair fold (adds into existing accumulators).
#define HPAIR16_ADD(R_BASE, BYTEOFF, Y_COEFF, Y_AC_LO, Y_AC_HI) \
	VMOVDQU	(BYTEOFF)(R_BASE), X8           \
	VPMOVZXBW X8, Y10                        \
	VMOVDQU	(BYTEOFF+1)(R_BASE), X9         \
	VPMOVZXBW X9, Y11                        \
	VPUNPCKLWD Y11, Y10, Y12                 \
	VPMADDWD   Y_COEFF, Y12, Y12             \
	VPADDD     Y12, Y_AC_LO, Y_AC_LO         \
	VPUNPCKHWD Y11, Y10, Y13                 \
	VPMADDWD   Y_COEFF, Y13, Y13             \
	VPADDD     Y13, Y_AC_HI, Y_AC_HI

// VPAIR16_BUILD: vertical pair build. Loads 16 bytes from row at
// +ROWOFF and 16 bytes from the row at +ROWOFF+16 (next tmp row).
#define VPAIR16_BUILD(R_BASE, ROWOFF, Y_COEFF, Y_AC_LO, Y_AC_HI) \
	VMOVDQU	(ROWOFF)(R_BASE), X8            \
	VPMOVZXBW X8, Y10                        \
	VMOVDQU	(ROWOFF+16)(R_BASE), X9         \
	VPMOVZXBW X9, Y11                        \
	VPUNPCKLWD Y11, Y10, Y_AC_LO             \
	VPMADDWD   Y_COEFF, Y_AC_LO, Y_AC_LO     \
	VPUNPCKHWD Y11, Y10, Y_AC_HI             \
	VPMADDWD   Y_COEFF, Y_AC_HI, Y_AC_HI

// VPAIR16_ADD: vertical pair fold.
#define VPAIR16_ADD(R_BASE, ROWOFF, Y_COEFF, Y_AC_LO, Y_AC_HI) \
	VMOVDQU	(ROWOFF)(R_BASE), X8            \
	VPMOVZXBW X8, Y10                        \
	VMOVDQU	(ROWOFF+16)(R_BASE), X9         \
	VPMOVZXBW X9, Y11                        \
	VPUNPCKLWD Y11, Y10, Y12                 \
	VPMADDWD   Y_COEFF, Y12, Y12             \
	VPADDD     Y12, Y_AC_LO, Y_AC_LO         \
	VPUNPCKHWD Y11, Y10, Y13                 \
	VPMADDWD   Y_COEFF, Y13, Y13             \
	VPADDD     Y13, Y_AC_HI, Y_AC_HI

// sixTapPredict16x16AVX2 ABI ($0-56).
TEXT ·sixTapPredict16x16AVX2(SB), NOSPLIT, $0-56
	MOVQ	dst+0(FP), DI
	MOVQ	dstStride+8(FP), BX
	MOVQ	src+16(FP), SI
	MOVQ	srcStride+24(FP), CX
	MOVQ	hFilter+32(FP), R11
	MOVQ	vFilter+40(FP), R12
	MOVQ	tmp+48(FP), R10

	// Y0..Y2 = horizontal coeff pairs; Y3..Y5 = vertical coeff pairs.
	LOAD_FILTER_PAIR_YMM(R11, 0, Y0, X0)
	LOAD_FILTER_PAIR_YMM(R11, 4, Y1, X1)
	LOAD_FILTER_PAIR_YMM(R11, 8, Y2, X2)
	LOAD_FILTER_PAIR_YMM(R12, 0, Y3, X3)
	LOAD_FILTER_PAIR_YMM(R12, 4, Y4, X4)
	LOAD_FILTER_PAIR_YMM(R12, 8, Y5, X5)

	// Y6 = bias (64 per int32 lane).
	VMOVDQU	sixtapAVX2Bias64<>(SB), Y6

	// === Horizontal pass: 21 rows of 16 cols ===
	MOVQ	$21, R13
	MOVQ	R10, R14

horiz16_avx2_loop:
	HPAIR16_BUILD(SI, 0, Y0, Y14, Y15)
	HPAIR16_ADD(SI, 2, Y1, Y14, Y15)
	HPAIR16_ADD(SI, 4, Y2, Y14, Y15)

	VPADDD	Y6, Y14, Y14
	VPADDD	Y6, Y15, Y15
	VPSRAD	$7, Y14, Y14
	VPSRAD	$7, Y15, Y15
	VPACKSSDW Y15, Y14, Y14
	VEXTRACTI128 $1, Y14, X12
	VPACKUSWB X12, X14, X14
	VMOVDQU	X14, (R14)

	ADDQ	CX, SI
	ADDQ	$16, R14
	DECQ	R13
	JNZ	horiz16_avx2_loop

	// === Vertical pass: 16 rows of 16 cols ===
	MOVQ	$16, R13
	MOVQ	R10, R14

vert16_avx2_loop:
	VPAIR16_BUILD(R14, 0, Y3, Y14, Y15)
	VPAIR16_ADD(R14, 32, Y4, Y14, Y15)
	VPAIR16_ADD(R14, 64, Y5, Y14, Y15)

	VPADDD	Y6, Y14, Y14
	VPADDD	Y6, Y15, Y15
	VPSRAD	$7, Y14, Y14
	VPSRAD	$7, Y15, Y15
	VPACKSSDW Y15, Y14, Y14
	VEXTRACTI128 $1, Y14, X12
	VPACKUSWB X12, X14, X14
	VMOVDQU	X14, (DI)

	ADDQ	BX, DI
	ADDQ	$16, R14
	DECQ	R13
	JNZ	vert16_avx2_loop

	VZEROUPPER
	RET
