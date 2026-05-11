//go:build amd64 && !purego

// SSE2 bilinear filter kernels for subpel variance widths 8 and 4.
// Mirrors libvpx v1.16.0 vpx_dsp/x86/subpel_variance_sse2.asm:
//
// First-pass (per row):
//   for x in [0, w): v = src[x] * f0 + src[x+1] * f1
//                    dst[x] = uint16((v + 64) >> 7)
// Second-pass (per row):
//   for x in [0, w): v = src[y*w+x] * f0 + src[(y+1)*w+x] * f1
//                    dst[x] = byte((v + 64) >> 7)
//
// Filter weights are uint16 in [0, 128]; products max=255*128=32640
// fit in u16 signed range, so PMULLW (low 16 bits) is exact.
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
//   f0+24(FP)        uint64 (f0 broadcast to 4 uint16 lanes)
//   f1+32(FP)        uint64 (f1 broadcast to 4 uint16 lanes)

#include "textflag.h"

// varFilterBlock2DBilinearFirstPass8SSE2: 8 columns per row.
TEXT ·varFilterBlock2DBilinearFirstPass8SSE2(SB), NOSPLIT, $0-48
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	height+24(FP), DX

	// X14 = f0 broadcast to 8 uint16 lanes.
	MOVQ	f0+32(FP), X14
	PSHUFD	$0x44, X14, X14
	// X15 = f1 broadcast to 8 uint16 lanes.
	MOVQ	f1+40(FP), X15
	PSHUFD	$0x44, X15, X15

	PXOR	X0, X0
	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	PSHUFD	$0x44, X13, X13

fp8_loop:
	// Load 8 src bytes for tap0, 8 src bytes shifted by 1 for tap1.
	MOVQ	(AX), X1
	MOVQ	1(AX), X2

	// Unpack to 8 uint16 lanes.
	PUNPCKLBW	X0, X1
	PUNPCKLBW	X0, X2

	// Multiply.
	PMULLW	X14, X1
	PMULLW	X15, X2

	// Sum + round + shift right 7.
	PADDW	X2, X1
	PADDW	X13, X1
	PSRLW	$7, X1

	// Store 8 uint16 lanes = 16 bytes.
	MOVOU	X1, (CX)

	ADDQ	BX, AX
	ADDQ	$16, CX
	DECQ	DX
	JNZ	fp8_loop

	RET

// varFilterBlock2DBilinearFirstPass4SSE2: 4 columns per row.
TEXT ·varFilterBlock2DBilinearFirstPass4SSE2(SB), NOSPLIT, $0-48
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	dst+16(FP), CX
	MOVQ	height+24(FP), DX

	MOVQ	f0+32(FP), X14
	PSHUFD	$0x44, X14, X14
	MOVQ	f1+40(FP), X15
	PSHUFD	$0x44, X15, X15

	PXOR	X0, X0
	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	PSHUFD	$0x44, X13, X13

fp4_loop:
	// Load 4 src bytes (need 5; loading 8 from src and 8 from src+1 is
	// fine because callers pass a stride-padded buffer).
	MOVL	(AX), X1
	MOVL	1(AX), X2

	PUNPCKLBW	X0, X1
	PUNPCKLBW	X0, X2

	PMULLW	X14, X1
	PMULLW	X15, X2

	PADDW	X2, X1
	PADDW	X13, X1
	PSRLW	$7, X1

	// Store 4 uint16 lanes = 8 bytes.
	MOVQ	X1, (CX)

	ADDQ	BX, AX
	ADDQ	$8, CX
	DECQ	DX
	JNZ	fp4_loop

	RET

// varFilterBlock2DBilinearSecondPass8SSE2: 8 columns per row.
TEXT ·varFilterBlock2DBilinearSecondPass8SSE2(SB), NOSPLIT, $0-40
	MOVQ	src+0(FP), AX
	MOVQ	dst+8(FP), BX
	MOVQ	height+16(FP), CX

	MOVQ	f0+24(FP), X14
	PSHUFD	$0x44, X14, X14
	MOVQ	f1+32(FP), X15
	PSHUFD	$0x44, X15, X15

	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	PSHUFD	$0x44, X13, X13

sp8_loop:
	// Row A = 8 uint16 = 16 bytes at (AX).
	MOVOU	(AX), X1
	// Row B at AX+16.
	MOVOU	16(AX), X2

	PMULLW	X14, X1
	PMULLW	X15, X2

	PADDW	X2, X1
	PADDW	X13, X1
	PSRLW	$7, X1

	// Pack u16 -> u8 with unsigned saturation. PACKUSWB takes high
	// half from src; we only care about low 8 lanes, so source is X1
	// for both halves but we MOVQ-store the low 64 bits.
	PACKUSWB	X1, X1

	MOVQ	X1, (BX)

	ADDQ	$16, AX
	ADDQ	$8, BX
	DECQ	CX
	JNZ	sp8_loop

	RET

// varFilterBlock2DBilinearSecondPass4SSE2: 4 columns per row.
TEXT ·varFilterBlock2DBilinearSecondPass4SSE2(SB), NOSPLIT, $0-40
	MOVQ	src+0(FP), AX
	MOVQ	dst+8(FP), BX
	MOVQ	height+16(FP), CX

	MOVQ	f0+24(FP), X14
	PSHUFD	$0x44, X14, X14
	MOVQ	f1+32(FP), X15
	PSHUFD	$0x44, X15, X15

	MOVQ	$0x0040004000400040, R10
	MOVQ	R10, X13
	PSHUFD	$0x44, X13, X13

sp4_loop:
	// Row A = 4 uint16 = 8 bytes at (AX).
	MOVQ	(AX), X1
	// Row B at AX+8.
	MOVQ	8(AX), X2

	PMULLW	X14, X1
	PMULLW	X15, X2

	PADDW	X2, X1
	PADDW	X13, X1
	PSRLW	$7, X1

	PACKUSWB	X1, X1

	MOVL	X1, (BX)

	ADDQ	$8, AX
	ADDQ	$4, BX
	DECQ	CX
	JNZ	sp4_loop

	RET
