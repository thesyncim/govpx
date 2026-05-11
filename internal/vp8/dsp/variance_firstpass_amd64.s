//go:build amd64 && !purego

// SSE2 16-wide first-pass bilinear filter for subpel variance.
// Mirrors libvpx v1.16.0 vpx_dsp/variance.c (first pass) and
// vpx_dsp/x86/subpel_variance_sse2.asm semantics:
//
//   for y in [0, height):
//     for x in [0, 16):
//       v = src[y*srcStride+x] * f0 + src[y*srcStride+x+1] * f1
//       dst[y*16+x] = uint16((v + 64) >> 7)
//
// Filter weights are uint16 in [0, 128] (the eight bilinear pairs sum
// to 128). Per row we load 17 src bytes via two MOVOU loads (the
// second is just the unaligned byte 16, taken from src+16), unpack
// bytes to two halves of 8 uint16 lanes, multiply each lane by f0 /
// f1 with PMULLW (low 16 bits of u16*u16 = u16 because max is
// 255*128=32640 fits in u16 signed range, and PMULLW preserves the
// low 16 bits exactly). Add the two products, add 64 (rounding bias),
// shift right by 7 with PSRLW, store the 16 uint16 result to dst.
//
// Calling convention (ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *uint16
//   height+24(FP)    int
//   f0+32(FP)        uint64 (f0 broadcast to 4 uint16 lanes)
//   f1+40(FP)        uint64 (f1 broadcast to 4 uint16 lanes)

#include "textflag.h"

TEXT ·varFilterBlock2DBilinearFirstPass16SSE2(SB), NOSPLIT, $0-48
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	height+24(FP), DX

	// X14 = f0 broadcast to 8 uint16 lanes
	MOVQ	f0+32(FP), X14
	PSHUFD	$0x44, X14, X14
	// X15 = f1 broadcast to 8 uint16 lanes
	MOVQ	f1+40(FP), X15
	PSHUFD	$0x44, X15, X15

	// X0 = zero (for byte-to-uint16 unpack)
	PXOR	X0, X0
	// X13 = round (64 broadcast to 8 uint16 lanes)
	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	PSHUFD	$0x44, X13, X13

firstpass_loop:
	// Load 16 bytes at src+0 -> X1, and 16 bytes at src+1 -> X2.
	// (We need only 17 bytes total; loading 16 from src+1 is fine
	// because callers pass a stride-padded buffer.)
	MOVOU	(AX), X1
	MOVOU	1(AX), X2

	// Unpack tap0 (X1) to two halves of 8 uint16 lanes:
	//   X3 = src[0..7] as u16
	//   X1 = src[8..15] as u16
	MOVOU	X1, X3
	PUNPCKLBW	X0, X3
	PUNPCKHBW	X0, X1

	// Unpack tap1 (X2) likewise:
	//   X4 = src[1..8], X2 = src[9..16]
	MOVOU	X2, X4
	PUNPCKLBW	X0, X4
	PUNPCKHBW	X0, X2

	// Multiply tap0 by f0 (low 16 bits is exact for u16*u16<=2^15-1
	// when the product's max is 255*128=32640 < 32768).
	PMULLW	X14, X3
	PMULLW	X14, X1
	// Multiply tap1 by f1.
	PMULLW	X15, X4
	PMULLW	X15, X2

	// Sum: X3 += X4, X1 += X2.
	PADDW	X4, X3
	PADDW	X2, X1
	// Round: add 64 to each lane.
	PADDW	X13, X3
	PADDW	X13, X1
	// Shift right by 7. Sum max = 32640+32640+64 = 65344 (u16 fits).
	PSRLW	$7, X3
	PSRLW	$7, X1

	// Store 16 uint16 lanes = 32 bytes.
	MOVOU	X3, (CX)
	MOVOU	X1, 16(CX)

	// Advance: src += srcStride, dst += 32 bytes (16 uint16).
	ADDQ	BX, AX
	ADDQ	$32, CX
	DECQ	DX
	JNZ	firstpass_loop

	RET
