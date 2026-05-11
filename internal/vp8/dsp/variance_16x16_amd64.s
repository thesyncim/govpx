//go:build amd64 && !purego

// SSE2 16-wide second-pass bilinear filter for subpel variance.
// Mirrors libvpx v1.16.0 vpx_dsp/variance.c (second pass) and
// vpx_dsp/x86/subpel_variance_sse2.asm semantics:
//
//   for y in [0, height):
//     for x in [0, 16):
//       v = src[y*16+x]   * f0
//         + src[(y+1)*16+x] * f1
//       dst[y*16+x] = byte((v + 64) >> 7)
//
// Filter weights are uint16 in [0, 128] with f0+f1=128. Inputs are
// the uint16 outputs of the first pass; their values fit in
// [0, 255]. Per row we load 16 uint16 from row A (src+y*32) and 16
// uint16 from row B (src+(y+1)*32), multiply each by f0 / f1
// (PMULLW low 16 bits is exact because each product max=255*128
// =32640 < 32768), add the two products, add 64 (round bias), shift
// right by 7 (PSRLW), then PACKUSWB the two halves into a single
// 16-byte XMM and MOVOU-store to dst.
//
// Calling convention (ABI0, $0-40):
//   src+0(FP)        *[17*16]uint16
//   dst+8(FP)        *byte
//   height+16(FP)    int
//   f0+24(FP)        uint64 (f0 broadcast to 4 uint16 lanes)
//   f1+32(FP)        uint64 (f1 broadcast to 4 uint16 lanes)

#include "textflag.h"

TEXT ·varFilterBlock2DBilinearSecondPass16SSE2(SB), NOSPLIT, $0-40
	MOVQ	src+0(FP), AX
	MOVQ	dst+8(FP), BX
	MOVQ	height+16(FP), CX

	// X14 = f0 broadcast, X15 = f1 broadcast.
	MOVQ	f0+24(FP), X14
	PSHUFD	$0x44, X14, X14
	MOVQ	f1+32(FP), X15
	PSHUFD	$0x44, X15, X15

	// X13 = round constant 64 broadcast across 8 uint16 lanes.
	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	PSHUFD	$0x44, X13, X13

secondpass_loop:
	// Row A: load 16 uint16 = 32 bytes from (AX), into X1+X2.
	MOVOU	(AX), X1
	MOVOU	16(AX), X2

	// Row B: load 16 uint16 from (AX+32).
	MOVOU	32(AX), X3
	MOVOU	48(AX), X4

	// X1 *= f0, X2 *= f0
	PMULLW	X14, X1
	PMULLW	X14, X2
	// X3 *= f1, X4 *= f1
	PMULLW	X15, X3
	PMULLW	X15, X4

	// Sum: X1 += X3, X2 += X4
	PADDW	X3, X1
	PADDW	X4, X2

	// Round: +64 each
	PADDW	X13, X1
	PADDW	X13, X2

	// Shift right by 7 (logical) - intermediate sum fits in u16.
	PSRLW	$7, X1
	PSRLW	$7, X2

	// Pack u16 -> u8 with unsigned saturation (values are already in
	// [0, 255], so saturation is a no-op).
	PACKUSWB	X2, X1

	// Store 16 bytes.
	MOVOU	X1, (BX)

	// Advance: src += 32 bytes (16 uint16); dst += 16 bytes.
	ADDQ	$32, AX
	ADDQ	$16, BX
	DECQ	CX
	JNZ	secondpass_loop

	RET
