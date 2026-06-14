//go:build amd64 && !purego

// Exact-domain SSE2 BlockErrorFP for VP9 transform coefficients. This is
// shaped after libvpx v1.16.0 vp9/encoder/x86/vp9_error_sse2.asm
// vp9_block_error_fp_sse2, but sign-extends each int16 input to int32
// before subtracting so govpx preserves its broader tested helper contract
// even when coeff-dqcoeff exceeds int16 range.

#include "textflag.h"

// blockErrorFPSSE2 ABI ($0-32):
//   coeff+0(FP)   *int16
//   dqcoeff+8(FP) *int16
//   n+16(FP)      int (multiple of 8)
//   ret+24(FP)    uint64
TEXT ·blockErrorFPSSE2(SB), NOSPLIT, $0-32
	MOVQ	coeff+0(FP), SI
	MOVQ	dqcoeff+8(FP), DI
	MOVQ	n+16(FP), CX
	SHRQ	$3, CX                  // eight coefficients per loop

	PXOR	X6, X6                  // uint64 accumulator lanes

loop:
	MOVOU	(SI), X0                // 8 x int16 coeff
	MOVOU	(DI), X1                // 8 x int16 dqcoeff
	ADDQ	$16, SI
	ADDQ	$16, DI

	// Sign-extend coeff into two 4-lane int32 vectors.
	MOVO	X0, X2
	PSRAW	$15, X2
	MOVO	X0, X3
	PUNPCKLWL	X2, X0
	PUNPCKHWL	X2, X3

	// Sign-extend dqcoeff into two 4-lane int32 vectors.
	MOVO	X1, X4
	PSRAW	$15, X4
	MOVO	X1, X5
	PUNPCKLWL	X4, X1
	PUNPCKHWL	X4, X5

	// diff = dqcoeff - coeff; the square is sign-invariant.
	PSUBL	X0, X1
	PSUBL	X3, X5

	// abs(diff) for low 4 lanes: (x ^ sign) - sign.
	MOVO	X1, X0
	PSRAL	$31, X0
	PXOR	X0, X1
	PSUBL	X0, X1

	// PMULULQ squares even int32 lanes. Shift once to square odd lanes.
	MOVO	X1, X2
	PMULULQ	X1, X1
	PSRLDQ	$4, X2
	PMULULQ	X2, X2
	PADDQ	X1, X6
	PADDQ	X2, X6

	// abs(diff) and square for high 4 lanes.
	MOVO	X5, X0
	PSRAL	$31, X0
	PXOR	X0, X5
	PSUBL	X0, X5

	MOVO	X5, X2
	PMULULQ	X5, X5
	PSRLDQ	$4, X2
	PMULULQ	X2, X2
	PADDQ	X5, X6
	PADDQ	X2, X6

	DECQ	CX
	JNZ	loop

	MOVHLPS	X6, X0
	PADDQ	X0, X6
	MOVQ	X6, ret+24(FP)
	RET

// blockErrorFPWithEnergySSE2 ABI ($0-40):
//   coeff+0(FP)   *int16
//   dqcoeff+8(FP) *int16
//   n+16(FP)      int (multiple of 8)
//   err+24(FP)    uint64
//   energy+32(FP) uint64
TEXT ·blockErrorFPWithEnergySSE2(SB), NOSPLIT, $0-40
	MOVQ	coeff+0(FP), SI
	MOVQ	dqcoeff+8(FP), DI
	MOVQ	n+16(FP), CX
	SHRQ	$3, CX                  // eight coefficients per loop

	PXOR	X6, X6                  // uint64 error accumulator lanes
	PXOR	X7, X7                  // uint64 coefficient-energy accumulator lanes

combined_loop:
	MOVOU	(SI), X0                // 8 x int16 coeff
	MOVOU	(DI), X1                // 8 x int16 dqcoeff
	ADDQ	$16, SI
	ADDQ	$16, DI

	// Sign-extend coeff into two 4-lane int32 vectors.
	MOVO	X0, X2
	PSRAW	$15, X2
	MOVO	X0, X3
	PUNPCKLWL	X2, X0
	PUNPCKHWL	X2, X3

	// energy += coeff * coeff for low 4 lanes.
	MOVO	X0, X8
	PSRAL	$31, X8
	MOVO	X0, X9
	PXOR	X8, X9
	PSUBL	X8, X9
	MOVO	X9, X8
	PMULULQ	X9, X9
	PSRLDQ	$4, X8
	PMULULQ	X8, X8
	PADDQ	X9, X7
	PADDQ	X8, X7

	// energy += coeff * coeff for high 4 lanes.
	MOVO	X3, X8
	PSRAL	$31, X8
	MOVO	X3, X9
	PXOR	X8, X9
	PSUBL	X8, X9
	MOVO	X9, X8
	PMULULQ	X9, X9
	PSRLDQ	$4, X8
	PMULULQ	X8, X8
	PADDQ	X9, X7
	PADDQ	X8, X7

	// Sign-extend dqcoeff into two 4-lane int32 vectors.
	MOVO	X1, X4
	PSRAW	$15, X4
	MOVO	X1, X5
	PUNPCKLWL	X4, X1
	PUNPCKHWL	X4, X5

	// diff = dqcoeff - coeff; the square is sign-invariant.
	PSUBL	X0, X1
	PSUBL	X3, X5

	// err += diff * diff for low 4 lanes.
	MOVO	X1, X8
	PSRAL	$31, X8
	PXOR	X8, X1
	PSUBL	X8, X1
	MOVO	X1, X8
	PMULULQ	X1, X1
	PSRLDQ	$4, X8
	PMULULQ	X8, X8
	PADDQ	X1, X6
	PADDQ	X8, X6

	// err += diff * diff for high 4 lanes.
	MOVO	X5, X8
	PSRAL	$31, X8
	PXOR	X8, X5
	PSUBL	X8, X5
	MOVO	X5, X8
	PMULULQ	X5, X5
	PSRLDQ	$4, X8
	PMULULQ	X8, X8
	PADDQ	X5, X6
	PADDQ	X8, X6

	DECQ	CX
	JNZ	combined_loop

	MOVHLPS	X6, X0
	PADDQ	X0, X6
	MOVQ	X6, err+24(FP)
	MOVHLPS	X7, X0
	PADDQ	X0, X7
	MOVQ	X7, energy+32(FP)
	RET

// squareSumSSE2 ABI ($0-24):
//   values+0(FP) *int16
//   n+8(FP)      int (multiple of 8)
//   ret+16(FP)   uint64
TEXT ·squareSumSSE2(SB), NOSPLIT, $0-24
	MOVQ	values+0(FP), SI
	MOVQ	n+8(FP), CX
	SHRQ	$3, CX                  // eight coefficients per loop

	PXOR	X6, X6                  // uint64 accumulator lanes

square_loop:
	MOVOU	(SI), X0                // 8 x int16 values
	ADDQ	$16, SI

	// Sign-extend values into two 4-lane int32 vectors.
	MOVO	X0, X2
	PSRAW	$15, X2
	MOVO	X0, X3
	PUNPCKLWL	X2, X0
	PUNPCKHWL	X2, X3

	// abs(value) for low 4 lanes: (x ^ sign) - sign.
	MOVO	X0, X1
	PSRAL	$31, X1
	PXOR	X1, X0
	PSUBL	X1, X0

	// PMULULQ squares even int32 lanes. Shift once to square odd lanes.
	MOVO	X0, X2
	PMULULQ	X0, X0
	PSRLDQ	$4, X2
	PMULULQ	X2, X2
	PADDQ	X0, X6
	PADDQ	X2, X6

	// abs(value) and square for high 4 lanes.
	MOVO	X3, X1
	PSRAL	$31, X1
	PXOR	X1, X3
	PSUBL	X1, X3

	MOVO	X3, X2
	PMULULQ	X3, X3
	PSRLDQ	$4, X2
	PMULULQ	X2, X2
	PADDQ	X3, X6
	PADDQ	X2, X6

	DECQ	CX
	JNZ	square_loop

	MOVHLPS	X6, X0
	PADDQ	X0, X6
	MOVQ	X6, ret+16(FP)
	RET
