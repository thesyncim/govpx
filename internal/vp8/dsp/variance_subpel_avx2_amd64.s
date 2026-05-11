//go:build amd64 && !purego

// AVX2 bilinear filter kernels for VP8 subpel variance, widths 16+.
// Mirrors libvpx v1.16.0 vpx_dsp/x86/subpel_variance_sse2.asm but
// processes two rows per loop iteration via 256-bit operands. VP8
// uses a 7-bit bilinear filter (weights in [0, 128], sum=128), so
// PMADDUBSW would overflow int16; we use VPMULLW + VPADDW instead.
//
// First-pass (per pair of rows):
//   for each row:
//     for x in [0, w):
//       v = src[x] * f0 + src[x+1] * f1
//       dst[x] = uint16((v + 64) >> 7)
//
// Second-pass (per pair of rows):
//   for each row:
//     for x in [0, w):
//       v = src[y*w+x] * f0 + src[(y+1)*w+x] * f1
//       dst[x] = byte((v + 64) >> 7)
//
// First-pass calling convention (ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *uint16
//   height+24(FP)    int
//   f0+32(FP)        uint64 (f0 broadcast to 4 uint16 lanes)
//   f1+40(FP)        uint64 (f1 broadcast to 4 uint16 lanes)
//
// Second-pass calling convention (ABI0, $0-40):
//   src+0(FP)        *uint16
//   dst+8(FP)        *byte
//   height+16(FP)    int
//   f0+24(FP)        uint64
//   f1+32(FP)        uint64

#include "textflag.h"

// varFilterBlock2DBilinearFirstPass16AVX2: 16 columns per row. We
// process one row per iteration but each row is 16 bytes -> 16 uint16
// lanes covering one full YMM. (Two-row interleaving is harder here
// because output is uint16 not byte; the YMM already holds the entire
// row width, so two-row batching would require two YMMs anyway.)
TEXT ·varFilterBlock2DBilinearFirstPass16AVX2(SB), NOSPLIT, $0-48
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	height+24(FP), DX

	// Y14 = f0 broadcast to 16 uint16 lanes, Y15 = f1 broadcast.
	// f0/f1 args already have the 16-bit weight broadcast to 4 lanes
	// via the *0x0001000100010001 multiplication in Go.
	MOVQ	f0+32(FP), X14
	VPBROADCASTQ	X14, Y14
	MOVQ	f1+40(FP), X15
	VPBROADCASTQ	X15, Y15

	// Y13 = round constant 64 broadcast across 16 uint16 lanes.
	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	VPBROADCASTQ	X13, Y13

fp16avx2_loop:
	// Load 16 src bytes at +0 and 16 at +1, each into a YMM via
	// VPMOVZXBW (zero-extend bytes -> 16 uint16 lanes).
	VMOVDQU	(AX), X1
	VPMOVZXBW	X1, Y1
	VMOVDQU	1(AX), X2
	VPMOVZXBW	X2, Y2

	VPMULLW	Y14, Y1, Y1
	VPMULLW	Y15, Y2, Y2

	VPADDW	Y2, Y1, Y1
	VPADDW	Y13, Y1, Y1
	VPSRLW	$7, Y1, Y1

	// Store 16 uint16 lanes = 32 bytes.
	VMOVDQU	Y1, (CX)

	ADDQ	BX, AX
	ADDQ	$32, CX
	DECQ	DX
	JNZ	fp16avx2_loop

	VZEROUPPER
	RET

// varFilterBlock2DBilinearSecondPass16AVX2: 16 columns per row, two
// rows per iteration. The YMM holds row[y] in lo lane and row[y+1]
// in hi lane; we PMULLW with f0/f1 and add. Since the second pass
// reads (row y * f0 + row y+1 * f1) for each output row, we can't
// batch two output rows via a single pair load (output rows y and
// y+1 share row y+1). So we keep one output row per iter but use
// the YMM width to process all 16 columns at once.
TEXT ·varFilterBlock2DBilinearSecondPass16AVX2(SB), NOSPLIT, $0-40
	MOVQ	src+0(FP), AX
	MOVQ	dst+8(FP), BX
	MOVQ	height+16(FP), CX

	MOVQ	f0+24(FP), X14
	VPBROADCASTQ	X14, Y14
	MOVQ	f1+32(FP), X15
	VPBROADCASTQ	X15, Y15

	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	VPBROADCASTQ	X13, Y13

sp16avx2_loop:
	// Y1 = 16 uint16 from row y, Y2 = 16 uint16 from row y+1.
	VMOVDQU	(AX), Y1
	VMOVDQU	32(AX), Y2

	VPMULLW	Y14, Y1, Y1
	VPMULLW	Y15, Y2, Y2

	VPADDW	Y2, Y1, Y1
	VPADDW	Y13, Y1, Y1
	VPSRLW	$7, Y1, Y1

	// Pack u16 -> u8. VPACKUSWB on YMM packs WITHIN each 128-bit lane,
	// so we get [packed_lo(8 bytes), zeros(8), packed_hi(8 bytes), zeros(8)]
	// — wrong layout. Use the trick from libvpx: extract the high 128
	// to an XMM, then PACKUSWB the two XMM halves into a single XMM.
	VEXTRACTI128	$1, Y1, X2
	PACKUSWB	X2, X1
	MOVOU	X1, (BX)

	ADDQ	$32, AX
	ADDQ	$16, BX
	DECQ	CX
	JNZ	sp16avx2_loop

	VZEROUPPER
	RET
