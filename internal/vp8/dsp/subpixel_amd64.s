// SSE2 port of the libvpx v1.16.0 VP8 six-tap subpel predictor.
// Mirrors vp8/common/x86/subpixel_sse2.asm conceptually; the kernel
// decomposes the inner product into three PMADDWL pairs over byte
// sources widened to int16 lanes, then saturated-pack int32 -> int16
// (PACKSSLW) and int16 -> uint8 (PACKUSWB).
//
// Two passes:
//
//   horizontal: for y in [0, H+5), x in [0, W):
//     v = sum_{k=0..5} src[y*srcStride+x+k] * hFilter[k] + 64
//     tmp[y*W+x] = clip255(v >> 7)
//
//   vertical: for y in [0, H), x in [0, W):
//     v = sum_{k=0..5} tmp[(y+k)*W+x] * vFilter[k] + 64
//     dst[y*dstStride+x] = clip255(v >> 7)
//
// Each row's horizontal load must be able to safely read 16 bytes
// from src starting at the row origin. Callers (sixTapPredict*Maybe)
// are responsible for ensuring the src stride / buffer satisfy this.

#include "textflag.h"

DATA  sixtapBias64<>+0x00(SB)/4, $64
DATA  sixtapBias64<>+0x04(SB)/4, $64
DATA  sixtapBias64<>+0x08(SB)/4, $64
DATA  sixtapBias64<>+0x0c(SB)/4, $64
GLOBL sixtapBias64<>(SB), RODATA|NOPTR, $16

// loadFilterPair builds a coefficient vector for PMADDWL covering
// two adjacent taps of a 6-tap filter. Given filter pointer in R_F
// and pair offset BYTEOFF (0, 4, or 8 — i.e. taps (0,1), (2,3),
// (4,5)), produces an XMM with int16 lanes [f0, f1, f0, f1, ...]
// repeated four times. Uses AX, DX as scratch.
#define LOAD_FILTER_PAIR(R_F, BYTEOFF, X_OUT) \
	MOVWQZX (BYTEOFF)(R_F), AX     \
	MOVWQZX (BYTEOFF+2)(R_F), DX   \
	SHLQ    $16, DX                \
	ORQ     AX, DX                 \
	MOVQ    DX, X_OUT              \
	PSHUFD  $0, X_OUT, X_OUT

// SIXTAP_PACK_AND_STORE_LOW8 packs two int32x4 accumulators in
// X_LO32, X_HI32 down to 8 unsigned bytes (positions 0..7), storing
// to (R_DST). Adds bias X_BIAS and shifts right by 7 with arithmetic
// shift before signed-int16 saturation and unsigned-uint8 saturation.
// Scratches X_LO32, X_HI32, X_TMP.
#define SIXTAP_PACK_8(X_LO32, X_HI32, X_BIAS, X_TMP, R_DST) \
	PADDD     X_BIAS, X_LO32   \
	PADDD     X_BIAS, X_HI32   \
	PSRAL     $7, X_LO32       \
	PSRAL     $7, X_HI32       \
	PACKSSLW  X_HI32, X_LO32   \
	PXOR      X_TMP, X_TMP     \
	PACKUSWB  X_TMP, X_LO32    \
	MOVQ      X_LO32, (R_DST)

// HPAIR_8 implements one PMADDWL pair for an 8-column horizontal
// row. Inputs:
//   R_SRC      pointer to row start (8 columns + 5-byte tail in this row)
//   BYTEOFF    pair start offset in bytes (0, 2, or 4 within the row)
//   X_COEFF    coefficient pair vector (built by LOAD_FILTER_PAIR)
//   X_ZERO     zero register
// Outputs (overwrites): X_LO32 += pair partials for positions 0..3,
// X_HI32 += pair partials for positions 4..7. (Caller initializes
// X_LO32/X_HI32 before calling this for the first pair, then uses
// PADDD-merge variants thereafter.)
//
// HPAIR_8_INIT: same as HPAIR_8 but stores (no add) to X_LO32/X_HI32.
#define HPAIR_8_BUILD(R_SRC, BYTEOFF, X_COEFF, X_ZERO, X_OUT_LO, X_OUT_HI) \
	MOVOU     (BYTEOFF)(R_SRC), X8        \
	MOVOU     (BYTEOFF+1)(R_SRC), X9      \
	MOVO      X8, X10                     \
	PUNPCKLBW X_ZERO, X10                  \
	MOVO      X9, X11                     \
	PUNPCKLBW X_ZERO, X11                  \
	MOVO      X10, X_OUT_LO                \
	PUNPCKLWL X11, X_OUT_LO                \
	MOVO      X10, X_OUT_HI                \
	PUNPCKHWL X11, X_OUT_HI                \
	PMADDWL   X_COEFF, X_OUT_LO            \
	PMADDWL   X_COEFF, X_OUT_HI

#define HPAIR_8_ADD(R_SRC, BYTEOFF, X_COEFF, X_ZERO, X_ACC_LO, X_ACC_HI) \
	MOVOU     (BYTEOFF)(R_SRC), X8        \
	MOVOU     (BYTEOFF+1)(R_SRC), X9      \
	MOVO      X8, X10                     \
	PUNPCKLBW X_ZERO, X10                  \
	MOVO      X9, X11                     \
	PUNPCKLBW X_ZERO, X11                  \
	MOVO      X10, X12                     \
	PUNPCKLWL X11, X12                     \
	PUNPCKHWL X11, X10                     \
	PMADDWL   X_COEFF, X12                 \
	PMADDWL   X_COEFF, X10                 \
	PADDD     X12, X_ACC_LO                \
	PADDD     X10, X_ACC_HI

// VPAIR_8 — like HPAIR_8 but for 8x8 vertical pass. Reads 8 bytes
// from each of two adjacent tmp rows.
//   R_TMP      pointer to current tmp row pair start
//   BYTEOFF    pair start offset (0, 16, or 32 — i.e. row-pairs in tmp)
#define VPAIR_8_BUILD(R_TMP, BYTEOFF, X_COEFF, X_ZERO, X_OUT_LO, X_OUT_HI) \
	MOVQ      (BYTEOFF)(R_TMP), X8        \
	MOVQ      (BYTEOFF+8)(R_TMP), X9      \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X_OUT_LO                 \
	PUNPCKLWL X9, X_OUT_LO                 \
	MOVO      X8, X_OUT_HI                 \
	PUNPCKHWL X9, X_OUT_HI                 \
	PMADDWL   X_COEFF, X_OUT_LO            \
	PMADDWL   X_COEFF, X_OUT_HI

#define VPAIR_8_ADD(R_TMP, BYTEOFF, X_COEFF, X_ZERO, X_ACC_LO, X_ACC_HI) \
	MOVQ      (BYTEOFF)(R_TMP), X8        \
	MOVQ      (BYTEOFF+8)(R_TMP), X9      \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X10                      \
	PUNPCKLWL X9, X10                      \
	PUNPCKHWL X9, X8                       \
	PMADDWL   X_COEFF, X10                 \
	PMADDWL   X_COEFF, X8                  \
	PADDD     X10, X_ACC_LO                \
	PADDD     X8, X_ACC_HI

// HPAIR_4 / VPAIR_4: 4-column variants of HPAIR_8 / VPAIR_8 — only
// produce a single int32x4 accumulator (positions 0..3). HPAIR_4
// reads 16 bytes of source via MOVOU but only consumes the low 5
// (the BUILD form's two MOVOUs mean a load at BYTEOFF+1 must also be
// safe — callers ensure 16 bytes from the row origin are readable).
// VPAIR_4 loads 4 bytes per row via MOVL (zero-extended into XMM).
#define HPAIR_4_BUILD(R_SRC, BYTEOFF, X_COEFF, X_ZERO, X_OUT) \
	MOVOU     (BYTEOFF)(R_SRC), X8        \
	MOVOU     (BYTEOFF+1)(R_SRC), X9      \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X_OUT                    \
	PUNPCKLWL X9, X_OUT                    \
	PMADDWL   X_COEFF, X_OUT

#define HPAIR_4_ADD(R_SRC, BYTEOFF, X_COEFF, X_ZERO, X_ACC) \
	MOVOU     (BYTEOFF)(R_SRC), X8        \
	MOVOU     (BYTEOFF+1)(R_SRC), X9      \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X10                      \
	PUNPCKLWL X9, X10                      \
	PMADDWL   X_COEFF, X10                 \
	PADDD     X10, X_ACC

#define VPAIR_4_BUILD(R_TMP, BYTEOFF, X_COEFF, X_ZERO, X_OUT) \
	MOVL      (BYTEOFF)(R_TMP), X8        \
	MOVL      (BYTEOFF+4)(R_TMP), X9      \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X_OUT                    \
	PUNPCKLWL X9, X_OUT                    \
	PMADDWL   X_COEFF, X_OUT

#define VPAIR_4_ADD(R_TMP, BYTEOFF, X_COEFF, X_ZERO, X_ACC) \
	MOVL      (BYTEOFF)(R_TMP), X8        \
	MOVL      (BYTEOFF+4)(R_TMP), X9      \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X10                      \
	PUNPCKLWL X9, X10                      \
	PMADDWL   X_COEFF, X10                 \
	PADDD     X10, X_ACC

// SIXTAP_PACK_4 packs a single int32x4 accumulator down to 4 unsigned
// bytes (positions 0..3), storing to (R_DST). Adds bias and shifts
// before signed-int16 then unsigned-uint8 saturation. Scratches X_AC.
#define SIXTAP_PACK_4(X_AC, X_BIAS, X_TMP, R_DST) \
	PADDD    X_BIAS, X_AC   \
	PSRAL    $7, X_AC       \
	PACKSSLW X_AC, X_AC     \
	PXOR     X_TMP, X_TMP   \
	PACKUSWB X_TMP, X_AC    \
	MOVL     X_AC, (R_DST)

// sixTapPredict8x8SSE2 ABI ($0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[13*8]byte
//
// Registers:
//   DI=dst, SI=src, R10=tmp, R11=hFilter, R12=vFilter
//   BX=dstStride, CX=srcStride
//   X0..X2 = horizontal coeff pairs (k=0,1; 2,3; 4,5)
//   X3..X5 = vertical   coeff pairs
//   X6 = bias (64 per int32 lane)
//   X7 = zero
//   X8..X12 = scratch
//   X13, X14 = accumulators for low / high outputs

TEXT ·sixTapPredict8x8SSE2(SB), NOSPLIT, $0-56
	MOVQ	dst+0(FP), DI
	MOVQ	dstStride+8(FP), BX
	MOVQ	src+16(FP), SI
	MOVQ	srcStride+24(FP), CX
	MOVQ	hFilter+32(FP), R11
	MOVQ	vFilter+40(FP), R12
	MOVQ	tmp+48(FP), R10

	LOAD_FILTER_PAIR(R11, 0, X0)
	LOAD_FILTER_PAIR(R11, 4, X1)
	LOAD_FILTER_PAIR(R11, 8, X2)
	LOAD_FILTER_PAIR(R12, 0, X3)
	LOAD_FILTER_PAIR(R12, 4, X4)
	LOAD_FILTER_PAIR(R12, 8, X5)

	MOVOU	sixtapBias64<>(SB), X6
	PXOR	X7, X7

	// === Horizontal pass: 13 rows ===
	MOVQ	$13, R13
	MOVQ	R10, R14
horiz8_loop:
	HPAIR_8_BUILD(SI, 0, X0, X7, X13, X14)
	HPAIR_8_ADD(SI, 2, X1, X7, X13, X14)
	HPAIR_8_ADD(SI, 4, X2, X7, X13, X14)

	SIXTAP_PACK_8(X13, X14, X6, X15, R14)

	ADDQ	CX, SI
	ADDQ	$8, R14
	DECQ	R13
	JNZ	horiz8_loop

	// === Vertical pass: 8 rows ===
	MOVQ	$8, R13
	MOVQ	R10, R14
vert8_loop:
	VPAIR_8_BUILD(R14, 0, X3, X7, X13, X14)
	VPAIR_8_ADD(R14, 16, X4, X7, X13, X14)
	VPAIR_8_ADD(R14, 32, X5, X7, X13, X14)

	SIXTAP_PACK_8(X13, X14, X6, X15, DI)

	ADDQ	BX, DI
	ADDQ	$8, R14
	DECQ	R13
	JNZ	vert8_loop

	RET

// sixTapPredict16x16SSE2 ABI ($0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[21*16]byte
//
// 16-column kernel: each row produces four int32x4 accumulators
// (positions 0..3, 4..7, 8..11, 12..15) before pack-and-store.
//
// We have only 16 xmm registers and need to keep X0..X5 (filter
// pairs), X6 (bias), X7 (zero) live throughout, leaving X8..X15 for
// scratch. The four-accumulator-per-row schedule doesn't fit, so two
// of them spill to the function frame's 32-byte scratch slot:
//   X14 = ac_c (positions  8..11)
//   X11 = ac_d (positions 12..15)
//   ac_a (0..3)  at  0(SP)
//   ac_b (4..7)  at 16(SP)
// Per pair we stage a freshly-built partial in X13/X15, fold into
// the spilled lanes via a load + PADDD + store, and PADDD directly
// into the live X14 / X11 lanes.

TEXT ·sixTapPredict16x16SSE2(SB), NOSPLIT, $32-56
	MOVQ	dst+0(FP), DI
	MOVQ	dstStride+8(FP), BX
	MOVQ	src+16(FP), SI
	MOVQ	srcStride+24(FP), CX
	MOVQ	hFilter+32(FP), R11
	MOVQ	vFilter+40(FP), R12
	MOVQ	tmp+48(FP), R10

	LOAD_FILTER_PAIR(R11, 0, X0)
	LOAD_FILTER_PAIR(R11, 4, X1)
	LOAD_FILTER_PAIR(R11, 8, X2)
	LOAD_FILTER_PAIR(R12, 0, X3)
	LOAD_FILTER_PAIR(R12, 4, X4)
	LOAD_FILTER_PAIR(R12, 8, X5)

	MOVOU	sixtapBias64<>(SB), X6
	PXOR	X7, X7

	// === Horizontal pass: 21 rows of 16 cols ===
	MOVQ	$21, R13
	MOVQ	R10, R14
horiz16_loop:
	// Pair 0 (taps 0,1) — BUILD into X_AC_A=mem[0], X_AC_B=mem[16]
	// We can't fit 4 accumulators + 4 byte/int16 staging + scratch in
	// 16 xmms while keeping X0..X7 live, so we spill ac_c, ac_d to
	// the function frame and reload during ADDs.
	MOVOU	0(SI), X8
	MOVOU	1(SI), X9
	MOVOA	X8, X10
	PUNPCKLBW X7, X10
	MOVOA	X8, X11
	PUNPCKHBW X7, X11
	MOVOA	X9, X12
	PUNPCKLBW X7, X12
	MOVOA	X9, X13
	PUNPCKHBW X7, X13
	MOVOA	X10, X14
	PUNPCKLWL X12, X14
	PMADDWL	X0, X14         // X14 = pair0 partials, positions 0..3
	MOVOA	X14, 0(SP)
	MOVOA	X10, X14
	PUNPCKHWL X12, X14
	PMADDWL	X0, X14         // X14 = positions 4..7
	MOVOA	X14, 16(SP)
	MOVOA	X11, X14
	PUNPCKLWL X13, X14
	PMADDWL	X0, X14         // X14 = positions 8..11
	PUNPCKHWL X13, X11
	PMADDWL	X0, X11         // X11 = positions 12..15
	// Holds: X14 = ac_c (8..11), X11 = ac_d (12..15);
	//        ac_a (0..3) at 0(SP), ac_b (4..7) at 16(SP).

	// Pair 1 (taps 2,3) — ADD to all four
	MOVOU	2(SI), X8
	MOVOU	3(SI), X9
	MOVOA	X8, X10
	PUNPCKLBW X7, X10
	PUNPCKHBW X7, X8
	MOVOA	X9, X12
	PUNPCKLBW X7, X12
	PUNPCKHBW X7, X9
	// positions 0..3
	MOVOA	X10, X13
	PUNPCKLWL X12, X13
	PMADDWL	X1, X13
	MOVOA	0(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 0(SP)
	// positions 4..7
	MOVOA	X10, X13
	PUNPCKHWL X12, X13
	PMADDWL	X1, X13
	MOVOA	16(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 16(SP)
	// positions 8..11
	MOVOA	X8, X13
	PUNPCKLWL X9, X13
	PMADDWL	X1, X13
	PADDD	X13, X14
	// positions 12..15
	PUNPCKHWL X9, X8
	PMADDWL	X1, X8
	PADDD	X8, X11

	// Pair 2 (taps 4,5)
	MOVOU	4(SI), X8
	MOVOU	5(SI), X9
	MOVOA	X8, X10
	PUNPCKLBW X7, X10
	PUNPCKHBW X7, X8
	MOVOA	X9, X12
	PUNPCKLBW X7, X12
	PUNPCKHBW X7, X9
	MOVOA	X10, X13
	PUNPCKLWL X12, X13
	PMADDWL	X2, X13
	MOVOA	0(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 0(SP)
	MOVOA	X10, X13
	PUNPCKHWL X12, X13
	PMADDWL	X2, X13
	MOVOA	16(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 16(SP)
	MOVOA	X8, X13
	PUNPCKLWL X9, X13
	PMADDWL	X2, X13
	PADDD	X13, X14
	PUNPCKHWL X9, X8
	PMADDWL	X2, X8
	PADDD	X8, X11

	// Pack and store 16 bytes at R14
	MOVOA	0(SP), X12
	MOVOA	16(SP), X13
	PADDD	X6, X12
	PADDD	X6, X13
	PADDD	X6, X14
	PADDD	X6, X11
	PSRAL	$7, X12
	PSRAL	$7, X13
	PSRAL	$7, X14
	PSRAL	$7, X11
	PACKSSLW X13, X12
	PACKSSLW X11, X14
	PACKUSWB X14, X12
	MOVOU	X12, (R14)

	ADDQ	CX, SI
	ADDQ	$16, R14
	DECQ	R13
	JNZ	horiz16_loop

	// === Vertical pass: 16 rows of 16 cols ===
	MOVQ	$16, R13
	MOVQ	R10, R14
vert16_loop:
	// Pair 0 (vFilter taps 0,1): tmp[y..y+1]
	MOVOU	0(R14), X8
	MOVOU	16(R14), X9
	MOVOA	X8, X10
	PUNPCKLBW X7, X10
	MOVOA	X8, X11
	PUNPCKHBW X7, X11
	MOVOA	X9, X12
	PUNPCKLBW X7, X12
	MOVOA	X9, X13
	PUNPCKHBW X7, X13
	MOVOA	X10, X14
	PUNPCKLWL X12, X14
	PMADDWL	X3, X14
	MOVOA	X14, 0(SP)
	MOVOA	X10, X14
	PUNPCKHWL X12, X14
	PMADDWL	X3, X14
	MOVOA	X14, 16(SP)
	MOVOA	X11, X14
	PUNPCKLWL X13, X14
	PMADDWL	X3, X14
	PUNPCKHWL X13, X11
	PMADDWL	X3, X11

	// Pair 1 (vFilter taps 2,3): tmp[y+2..y+3]
	MOVOU	32(R14), X8
	MOVOU	48(R14), X9
	MOVOA	X8, X10
	PUNPCKLBW X7, X10
	PUNPCKHBW X7, X8
	MOVOA	X9, X12
	PUNPCKLBW X7, X12
	PUNPCKHBW X7, X9
	MOVOA	X10, X13
	PUNPCKLWL X12, X13
	PMADDWL	X4, X13
	MOVOA	0(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 0(SP)
	MOVOA	X10, X13
	PUNPCKHWL X12, X13
	PMADDWL	X4, X13
	MOVOA	16(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 16(SP)
	MOVOA	X8, X13
	PUNPCKLWL X9, X13
	PMADDWL	X4, X13
	PADDD	X13, X14
	PUNPCKHWL X9, X8
	PMADDWL	X4, X8
	PADDD	X8, X11

	// Pair 2 (vFilter taps 4,5): tmp[y+4..y+5]
	MOVOU	64(R14), X8
	MOVOU	80(R14), X9
	MOVOA	X8, X10
	PUNPCKLBW X7, X10
	PUNPCKHBW X7, X8
	MOVOA	X9, X12
	PUNPCKLBW X7, X12
	PUNPCKHBW X7, X9
	MOVOA	X10, X13
	PUNPCKLWL X12, X13
	PMADDWL	X5, X13
	MOVOA	0(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 0(SP)
	MOVOA	X10, X13
	PUNPCKHWL X12, X13
	PMADDWL	X5, X13
	MOVOA	16(SP), X15
	PADDD	X13, X15
	MOVOA	X15, 16(SP)
	MOVOA	X8, X13
	PUNPCKLWL X9, X13
	PMADDWL	X5, X13
	PADDD	X13, X14
	PUNPCKHWL X9, X8
	PMADDWL	X5, X8
	PADDD	X8, X11

	// Pack and store 16 bytes at DI
	MOVOA	0(SP), X12
	MOVOA	16(SP), X13
	PADDD	X6, X12
	PADDD	X6, X13
	PADDD	X6, X14
	PADDD	X6, X11
	PSRAL	$7, X12
	PSRAL	$7, X13
	PSRAL	$7, X14
	PSRAL	$7, X11
	PACKSSLW X13, X12
	PACKSSLW X11, X14
	PACKUSWB X14, X12
	MOVOU	X12, (DI)

	ADDQ	BX, DI
	ADDQ	$16, R14
	DECQ	R13
	JNZ	vert16_loop

	RET

// sixTapPredict8x4SSE2 ABI ($0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[9*8]byte
//
// Same kernel as sixTapPredict8x8SSE2; only the H+5=9 horizontal row
// count and H=4 vertical row count differ.

TEXT ·sixTapPredict8x4SSE2(SB), NOSPLIT, $0-56
	MOVQ	dst+0(FP), DI
	MOVQ	dstStride+8(FP), BX
	MOVQ	src+16(FP), SI
	MOVQ	srcStride+24(FP), CX
	MOVQ	hFilter+32(FP), R11
	MOVQ	vFilter+40(FP), R12
	MOVQ	tmp+48(FP), R10

	LOAD_FILTER_PAIR(R11, 0, X0)
	LOAD_FILTER_PAIR(R11, 4, X1)
	LOAD_FILTER_PAIR(R11, 8, X2)
	LOAD_FILTER_PAIR(R12, 0, X3)
	LOAD_FILTER_PAIR(R12, 4, X4)
	LOAD_FILTER_PAIR(R12, 8, X5)

	MOVOU	sixtapBias64<>(SB), X6
	PXOR	X7, X7

	// === Horizontal pass: 9 rows ===
	MOVQ	$9, R13
	MOVQ	R10, R14
horiz8x4_loop:
	HPAIR_8_BUILD(SI, 0, X0, X7, X13, X14)
	HPAIR_8_ADD(SI, 2, X1, X7, X13, X14)
	HPAIR_8_ADD(SI, 4, X2, X7, X13, X14)

	SIXTAP_PACK_8(X13, X14, X6, X15, R14)

	ADDQ	CX, SI
	ADDQ	$8, R14
	DECQ	R13
	JNZ	horiz8x4_loop

	// === Vertical pass: 4 rows ===
	MOVQ	$4, R13
	MOVQ	R10, R14
vert8x4_loop:
	VPAIR_8_BUILD(R14, 0, X3, X7, X13, X14)
	VPAIR_8_ADD(R14, 16, X4, X7, X13, X14)
	VPAIR_8_ADD(R14, 32, X5, X7, X13, X14)

	SIXTAP_PACK_8(X13, X14, X6, X15, DI)

	ADDQ	BX, DI
	ADDQ	$8, R14
	DECQ	R13
	JNZ	vert8x4_loop

	RET

// sixTapPredict4x4SSE2 ABI ($0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[9*4]byte
//
// 4-column kernel: each row produces a single int32x4 accumulator
// (positions 0..3) before pack-and-store. Horizontal pass loads 16
// bytes per row via MOVOU at base and base+1 (only the low 5 bytes
// are consumed); vertical pass loads 4 bytes per row via MOVL. tmp
// rows are 4 bytes apart.

TEXT ·sixTapPredict4x4SSE2(SB), NOSPLIT, $0-56
	MOVQ	dst+0(FP), DI
	MOVQ	dstStride+8(FP), BX
	MOVQ	src+16(FP), SI
	MOVQ	srcStride+24(FP), CX
	MOVQ	hFilter+32(FP), R11
	MOVQ	vFilter+40(FP), R12
	MOVQ	tmp+48(FP), R10

	LOAD_FILTER_PAIR(R11, 0, X0)
	LOAD_FILTER_PAIR(R11, 4, X1)
	LOAD_FILTER_PAIR(R11, 8, X2)
	LOAD_FILTER_PAIR(R12, 0, X3)
	LOAD_FILTER_PAIR(R12, 4, X4)
	LOAD_FILTER_PAIR(R12, 8, X5)

	MOVOU	sixtapBias64<>(SB), X6
	PXOR	X7, X7

	// === Horizontal pass: 9 rows ===
	MOVQ	$9, R13
	MOVQ	R10, R14
horiz4_loop:
	HPAIR_4_BUILD(SI, 0, X0, X7, X13)
	HPAIR_4_ADD(SI, 2, X1, X7, X13)
	HPAIR_4_ADD(SI, 4, X2, X7, X13)

	SIXTAP_PACK_4(X13, X6, X15, R14)

	ADDQ	CX, SI
	ADDQ	$4, R14
	DECQ	R13
	JNZ	horiz4_loop

	// === Vertical pass: 4 rows ===
	MOVQ	$4, R13
	MOVQ	R10, R14
vert4_loop:
	VPAIR_4_BUILD(R14, 0, X3, X7, X13)
	VPAIR_4_ADD(R14, 8, X4, X7, X13)
	VPAIR_4_ADD(R14, 16, X5, X7, X13)

	SIXTAP_PACK_4(X13, X6, X15, DI)

	ADDQ	BX, DI
	ADDQ	$4, R14
	DECQ	R13
	JNZ	vert4_loop

	RET
