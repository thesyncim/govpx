// SSE2 port of the libvpx v1.16.0 VP8 two-tap (bilinear) subpel
// predictor. Mirrors vp8/common/filter.c bilinear_predict 16x16 / 8x8.
// Filter taps are non-negative and sum to 128, so all intermediate
// values fit in uint16 lanes (max horizontal output = 255). tmp is
// stored as bytes; vertical re-widens to uint16 for the multiply.
//
//   horizontal: for y in [0, H+1), x in [0, W):
//     tmp[y*W+x] = (src[y*S+x]*hFilter[0] + src[y*S+x+1]*hFilter[1] + 64) >> 7
//
//   vertical: for y in [0, H), x in [0, W):
//     dst[y*S+x] = (tmp[y*W+x]*vFilter[0] + tmp[(y+1)*W+x]*vFilter[1] + 64) >> 7
//
// Each row's horizontal load must be able to safely read 16 bytes
// (16x16: read 32, take [0..16] window) or 8 bytes (8x8: read 16,
// take [0..8] window) from src starting at the row origin. Callers
// (bilinearPredict*Maybe) are responsible for ensuring the src
// stride / buffer satisfy this.

#include "textflag.h"

DATA  bilinBias64<>+0x00(SB)/4, $64
DATA  bilinBias64<>+0x04(SB)/4, $64
DATA  bilinBias64<>+0x08(SB)/4, $64
DATA  bilinBias64<>+0x0c(SB)/4, $64
GLOBL bilinBias64<>(SB), RODATA|NOPTR, $16

// LOAD_BILIN_FILTER builds an int16x8 coefficient vector with lanes
// [f0, f1, f0, f1, f0, f1, f0, f1] for PMADDWL pair multiplication.
// Uses AX, DX as scratch.
#define LOAD_BILIN_FILTER(R_F, X_OUT) \
	MOVWQZX (R_F), AX              \
	MOVWQZX 2(R_F), DX             \
	SHLQ    $16, DX                \
	ORQ     AX, DX                 \
	MOVQ    DX, X_OUT              \
	PSHUFD  $0, X_OUT, X_OUT

// BILIN_PACK_8: packs two int32x4 accumulators X_LO/X_HI down to
// 8 unsigned bytes at (R_DST). Adds bias X_BIAS, shifts right by 7
// (logical), packs int32 -> uint16 (via signed-saturation int16 path
// since values are < 32768), then int16 -> uint8.
#define BILIN_PACK_8(X_LO, X_HI, X_BIAS, X_TMP, R_DST) \
	PADDD     X_BIAS, X_LO   \
	PADDD     X_BIAS, X_HI   \
	PSRLL     $7, X_LO       \
	PSRLL     $7, X_HI       \
	PACKSSLW  X_HI, X_LO     \
	PXOR      X_TMP, X_TMP   \
	PACKUSWB  X_TMP, X_LO    \
	MOVQ      X_LO, (R_DST)

// BILIN_PACK_16: packs four int32x4 accumulators down to 16 unsigned
// bytes at (R_DST). X_LO0/X_HI0 hold positions 0..7 lo/hi int32;
// X_LO1/X_HI1 hold positions 8..15 lo/hi int32.
#define BILIN_PACK_16(X_LO0, X_HI0, X_LO1, X_HI1, X_BIAS, R_DST) \
	PADDD     X_BIAS, X_LO0  \
	PADDD     X_BIAS, X_HI0  \
	PADDD     X_BIAS, X_LO1  \
	PADDD     X_BIAS, X_HI1  \
	PSRLL     $7, X_LO0      \
	PSRLL     $7, X_HI0      \
	PSRLL     $7, X_LO1      \
	PSRLL     $7, X_HI1      \
	PACKSSLW  X_HI0, X_LO0   \
	PACKSSLW  X_HI1, X_LO1   \
	PACKUSWB  X_LO1, X_LO0   \
	MOVOU     X_LO0, (R_DST)

// HPAIR_16 implements one PMADDWL pair for a 16-column horizontal
// row covering the bilinear's two adjacent taps. Reads 17 source
// bytes from (R_SRC).. (R_SRC+16). Writes int32x4 partials to
// X_LO0,X_HI0 (positions 0..7) and X_LO1,X_HI1 (positions 8..15).
#define HPAIR_16(R_SRC, X_COEFF, X_ZERO, X_LO0, X_HI0, X_LO1, X_HI1) \
	MOVOU     (R_SRC), X8                  \
	MOVOU     1(R_SRC), X9                 \
	MOVO      X8, X10                      \
	PUNPCKLBW X_ZERO, X10                  \
	MOVO      X9, X11                      \
	PUNPCKLBW X_ZERO, X11                  \
	MOVO      X10, X_LO0                   \
	PUNPCKLWL X11, X_LO0                   \
	MOVO      X10, X_HI0                   \
	PUNPCKHWL X11, X_HI0                   \
	PMADDWL   X_COEFF, X_LO0               \
	PMADDWL   X_COEFF, X_HI0               \
	MOVO      X8, X10                      \
	PUNPCKHBW X_ZERO, X10                  \
	MOVO      X9, X11                      \
	PUNPCKHBW X_ZERO, X11                  \
	MOVO      X10, X_LO1                   \
	PUNPCKLWL X11, X_LO1                   \
	MOVO      X10, X_HI1                   \
	PUNPCKHWL X11, X_HI1                   \
	PMADDWL   X_COEFF, X_LO1               \
	PMADDWL   X_COEFF, X_HI1

// HPAIR_8 implements one PMADDWL pair for an 8-column horizontal
// row. Reads 9 source bytes from (R_SRC)..(R_SRC+8). Writes int32x4
// partials to X_LO,X_HI (positions 0..7).
#define HPAIR_8(R_SRC, X_COEFF, X_ZERO, X_LO, X_HI) \
	MOVQ      (R_SRC), X8                  \
	MOVQ      1(R_SRC), X9                 \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X_LO                     \
	PUNPCKLWL X9, X_LO                     \
	MOVO      X8, X_HI                     \
	PUNPCKHWL X9, X_HI                     \
	PMADDWL   X_COEFF, X_LO                \
	PMADDWL   X_COEFF, X_HI

// VPAIR_16 implements the vertical PMADDWL pair for 16 columns. Reads
// 16 bytes each from (R_LO) (row y) and (R_HI) (row y+1). Writes
// int32x4 partials to X_LO0,X_HI0 (positions 0..7) and X_LO1,X_HI1
// (positions 8..15).
#define VPAIR_16(R_LO, R_HI, X_COEFF, X_ZERO, X_LO0, X_HI0, X_LO1, X_HI1) \
	MOVOU     (R_LO), X8                   \
	MOVOU     (R_HI), X9                   \
	MOVO      X8, X10                      \
	PUNPCKLBW X_ZERO, X10                  \
	MOVO      X9, X11                      \
	PUNPCKLBW X_ZERO, X11                  \
	MOVO      X10, X_LO0                   \
	PUNPCKLWL X11, X_LO0                   \
	MOVO      X10, X_HI0                   \
	PUNPCKHWL X11, X_HI0                   \
	PMADDWL   X_COEFF, X_LO0               \
	PMADDWL   X_COEFF, X_HI0               \
	MOVO      X8, X10                      \
	PUNPCKHBW X_ZERO, X10                  \
	MOVO      X9, X11                      \
	PUNPCKHBW X_ZERO, X11                  \
	MOVO      X10, X_LO1                   \
	PUNPCKLWL X11, X_LO1                   \
	MOVO      X10, X_HI1                   \
	PUNPCKHWL X11, X_HI1                   \
	PMADDWL   X_COEFF, X_LO1               \
	PMADDWL   X_COEFF, X_HI1

// VPAIR_8 implements the vertical PMADDWL pair for 8 columns. Reads
// 8 bytes each from (R_LO) (row y) and (R_HI) (row y+1). Writes
// int32x4 partials to X_LO,X_HI (positions 0..7).
#define VPAIR_8(R_LO, R_HI, X_COEFF, X_ZERO, X_LO, X_HI) \
	MOVQ      (R_LO), X8                   \
	MOVQ      (R_HI), X9                   \
	PUNPCKLBW X_ZERO, X8                   \
	PUNPCKLBW X_ZERO, X9                   \
	MOVO      X8, X_LO                     \
	PUNPCKLWL X9, X_LO                     \
	MOVO      X8, X_HI                     \
	PUNPCKHWL X9, X_HI                     \
	PMADDWL   X_COEFF, X_LO                \
	PMADDWL   X_COEFF, X_HI

// func bilinearPredict16x16SSE2(dst *byte, dstStride int, src *byte, srcStride int,
//     hFilter *[2]int16, vFilter *[2]int16, tmp *[17*16]byte)
TEXT ·bilinearPredict16x16SSE2(SB), NOSPLIT, $0-56
	MOVQ    dst+0(FP), DI
	MOVQ    dstStride+8(FP), SI
	MOVQ    src+16(FP), BX
	MOVQ    srcStride+24(FP), CX
	MOVQ    hFilter+32(FP), R8
	MOVQ    vFilter+40(FP), R9
	MOVQ    tmp+48(FP), R10

	LOAD_BILIN_FILTER(R8, X0)   // X0 = h-coefficient pair
	LOAD_BILIN_FILTER(R9, X1)   // X1 = v-coefficient pair
	MOVOU   bilinBias64<>(SB), X7
	PXOR    X12, X12            // X12 = zero

	// === Horizontal pass: 17 rows of 16 cols ===
	MOVQ    $17, R11
	MOVQ    R10, R12            // R12 = tmp cursor

bilin16_h_loop:
	HPAIR_16(BX, X0, X12, X2, X3, X4, X5)
	BILIN_PACK_16(X2, X3, X4, X5, X7, R12)

	ADDQ    CX, BX
	ADDQ    $16, R12
	DECQ    R11
	JNZ     bilin16_h_loop

	// === Vertical pass: 16 rows of 16 cols ===
	MOVQ    $16, R11
	MOVQ    R10, R12            // R12 = tmp[y]
	MOVQ    R10, R13
	ADDQ    $16, R13            // R13 = tmp[y+1]

bilin16_v_loop:
	VPAIR_16(R12, R13, X1, X12, X2, X3, X4, X5)
	BILIN_PACK_16(X2, X3, X4, X5, X7, DI)

	ADDQ    SI, DI
	ADDQ    $16, R12
	ADDQ    $16, R13
	DECQ    R11
	JNZ     bilin16_v_loop

	RET

// func bilinearPredict8x8SSE2(dst *byte, dstStride int, src *byte, srcStride int,
//     hFilter *[2]int16, vFilter *[2]int16, tmp *[9*8]byte)
TEXT ·bilinearPredict8x8SSE2(SB), NOSPLIT, $0-56
	MOVQ    dst+0(FP), DI
	MOVQ    dstStride+8(FP), SI
	MOVQ    src+16(FP), BX
	MOVQ    srcStride+24(FP), CX
	MOVQ    hFilter+32(FP), R8
	MOVQ    vFilter+40(FP), R9
	MOVQ    tmp+48(FP), R10

	LOAD_BILIN_FILTER(R8, X0)
	LOAD_BILIN_FILTER(R9, X1)
	MOVOU   bilinBias64<>(SB), X7
	PXOR    X12, X12

	// === Horizontal pass: 9 rows of 8 cols ===
	MOVQ    $9, R11
	MOVQ    R10, R12

bilin8_h_loop:
	HPAIR_8(BX, X0, X12, X2, X3)
	BILIN_PACK_8(X2, X3, X7, X13, R12)

	ADDQ    CX, BX
	ADDQ    $8, R12
	DECQ    R11
	JNZ     bilin8_h_loop

	// === Vertical pass: 8 rows of 8 cols ===
	MOVQ    $8, R11
	MOVQ    R10, R12
	MOVQ    R10, R13
	ADDQ    $8, R13

bilin8_v_loop:
	VPAIR_8(R12, R13, X1, X12, X2, X3)
	BILIN_PACK_8(X2, X3, X7, X13, DI)

	ADDQ    SI, DI
	ADDQ    $8, R12
	ADDQ    $8, R13
	DECQ    R11
	JNZ     bilin8_v_loop

	RET
