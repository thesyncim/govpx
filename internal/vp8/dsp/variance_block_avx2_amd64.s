// AVX2 variance kernels for VP8 picker block sizes 16x16, 16x8, 16xN
// (even N) and 8x16. Mirrors libvpx v1.16.0 vpx_dsp/x86/variance_avx2.c
// variance16_kernel_avx2 / variance8_kernel_avx2:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// For width 16 we process two rows per iteration so each YMM register
// holds 32 (16+16) bytes of src/ref. We use the libvpx
// PMADDUBSW(unpack8(src,ref), [1,-1]...) trick to compute the 16-bit
// (src-ref) diffs in a single instruction. For width 8 we pack two
// rows of 8 bytes into a single XMM and zero-extend to a YMM of 16
// uint16 lanes, then PSUBW. Both kernels accumulate into Y6 (16-bit
// sum) and Y7 (32-bit SSE).
//
// Callers guarantee even height for the 16xN kernel (16x16 and 16x8
// are the only sizes ≥16-wide in the VP8 picker, both even), and
// height=16 for the 8xN kernel.
//
// Calling convention (16xN, ABI0, $0-56):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   height+32(FP)    int (even)
//   sumOut+40(FP)    *int32
//   sseOut+48(FP)    *uint32
//
// Calling convention (8x16, ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   sumOut+32(FP)    *int32
//   sseOut+40(FP)    *uint32

#include "textflag.h"

DATA	·avx2AdjacentSub<>+0x00(SB)/8, $0xff01ff01ff01ff01
DATA	·avx2AdjacentSub<>+0x08(SB)/8, $0xff01ff01ff01ff01
DATA	·avx2AdjacentSub<>+0x10(SB)/8, $0xff01ff01ff01ff01
DATA	·avx2AdjacentSub<>+0x18(SB)/8, $0xff01ff01ff01ff01
GLOBL	·avx2AdjacentSub<>(SB), RODATA|NOPTR, $32

// REDUCE_AVX2: takes Y6 (16-bit sum, 16 int16 lanes) and Y7 (32-bit
// sse, 8 int32 lanes) and stores reduced int32 sum to (DI), uint32
// sse to (SI). Mirrors libvpx variance_final_from_16bit_sum_avx2:
//   - Reduce Y6 to 4 int32 lanes by adding the two 128-bit halves and
//     then sign-extending to int32, then horizontal-add.
//   - Reduce Y7 to 1 int32 by adding the two 128-bit halves then
//     horizontal-add.
#define REDUCE_AVX2 \
	VEXTRACTI128	$1, Y6, X3 \
	PADDW	X3, X6 \
	MOVOU	X6, X3 \
	PSRLDQ	$8, X3 \
	PADDW	X3, X6 \
	PMOVSXWD	X6, X6 \
	PSHUFD	$0xee, X6, X3 \
	PADDL	X3, X6 \
	PSHUFD	$0x55, X6, X3 \
	PADDL	X3, X6 \
	MOVL	X6, (DI) \
	VEXTRACTI128	$1, Y7, X3 \
	PADDL	X3, X7 \
	PSHUFD	$0xee, X7, X3 \
	PADDL	X3, X7 \
	PSHUFD	$0x55, X7, X3 \
	PADDL	X3, X7 \
	MOVL	X7, (SI) \
	VZEROUPPER

// varianceBlock16xNAVX2: 16 columns, even height (>=2). Two rows per
// loop iteration.
TEXT ·varianceBlock16xNAVX2(SB), NOSPLIT, $0-56
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	height+32(FP), R8
	MOVQ	sumOut+40(FP), DI
	MOVQ	sseOut+48(FP), SI

	// Y0 = adjacent-subtract pattern [1,-1,...] for PMADDUBSW.
	VMOVDQU	·avx2AdjacentSub<>(SB), Y0
	VPXOR	Y6, Y6, Y6
	VPXOR	Y7, Y7, Y7

	SHRQ	$1, R8       // pair count = height / 2
	JZ	w16avx2_done

w16avx2_loop:
	// Load 16 src bytes from row 0 into the low 128 lane of Y1, then
	// insert 16 src bytes from row 1 into the high 128 lane.
	VMOVDQU	(AX), X1
	VINSERTI128	$1, (AX)(BX*1), Y1, Y1
	VMOVDQU	(CX), X2
	VINSERTI128	$1, (CX)(DX*1), Y2, Y2

	// Pack (src,ref) byte pairs.
	VPUNPCKLBW	Y2, Y1, Y3
	VPUNPCKHBW	Y2, Y1, Y4

	// PMADDUBSW + [1,-1,...] = signed 16-bit (src - ref) per pair.
	VPMADDUBSW	Y0, Y3, Y3
	VPMADDUBSW	Y0, Y4, Y4

	// SSE += pmaddwd(diff, diff)
	VPMADDWD	Y3, Y3, Y5
	VPADDD	Y5, Y7, Y7
	VPMADDWD	Y4, Y4, Y5
	VPADDD	Y5, Y7, Y7

	// SUM += diff (int16, sign-extend later).
	VPADDW	Y3, Y4, Y3
	VPADDW	Y3, Y6, Y6

	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	DECQ	R8
	JNZ	w16avx2_loop

w16avx2_done:
	REDUCE_AVX2
	RET

// varianceBlock8x16AVX2: 8 columns, 16 rows. Two rows per iteration.
TEXT ·varianceBlock8x16AVX2(SB), NOSPLIT, $0-48
	MOVQ	src+0(FP), AX
	MOVQ	srcStride+8(FP), BX
	MOVQ	ref+16(FP), CX
	MOVQ	refStride+24(FP), DX
	MOVQ	sumOut+32(FP), DI
	MOVQ	sseOut+40(FP), SI

	VPXOR	Y6, Y6, Y6
	VPXOR	Y7, Y7, Y7

	MOVQ	$8, R8

w8avx2_loop:
	// Pack two 8-byte rows of src into a 16-byte XMM, then zero-extend
	// to a 16-lane uint16 YMM. Same for ref.
	MOVQ	(AX), X1
	MOVQ	(AX)(BX*1), X2
	PUNPCKLLQ	X2, X1
	MOVQ	(CX), X3
	MOVQ	(CX)(DX*1), X4
	PUNPCKLLQ	X4, X3

	VPMOVZXBW	X1, Y1
	VPMOVZXBW	X3, Y3

	VPSUBW	Y3, Y1, Y1

	VPADDW	Y1, Y6, Y6
	VPMADDWD	Y1, Y1, Y1
	VPADDD	Y1, Y7, Y7

	LEAQ	(AX)(BX*2), AX
	LEAQ	(CX)(DX*2), CX
	DECQ	R8
	JNZ	w8avx2_loop

	REDUCE_AVX2
	RET
