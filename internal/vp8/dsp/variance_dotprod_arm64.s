//go:build arm64 && !purego

// ARMv8.4-A DOTPROD variance/SSE kernels. Ports libvpx v1.16.0
// vpx_dsp/arm/variance_neon_dotprod.c and sse_neon_dotprod.c for
// the 16- and 8-wide VP8 blocks used by motion search, loop-filter
// scoring, and inter-mode rate estimation.

#include "textflag.h"

// func varianceBlock16x16DotProd(src *byte, srcStride int, ref *byte, refStride int, sumOut *int32, sseOut *uint32)
TEXT ·varianceBlock16x16DotProd(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	sumOut+32(FP), R4
	MOVD	sseOut+40(FP), R5

	MOVD	$1, R6
	VDUP	R6, V31.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16
	VEOR	V24.B16, V24.B16, V24.B16
	VEOR	V25.B16, V25.B16, V25.B16
	MOVD	$8, R6

var_dp16x16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	WORD	$0x6e9f9414	// udot v20.4s, v0.16b, v31.16b
	WORD	$0x6e9f9435	// udot v21.4s, v1.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829459	// udot v25.4s, v2.16b, v2.16b
	WORD	$0x6e9f9417	// udot v23.4s, v0.16b, v31.16b
	WORD	$0x6e9f9438	// udot v24.4s, v1.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, var_dp16x16_loop

	VADD	V23.S4, V20.S4, V20.S4
	VADD	V24.S4, V21.S4, V21.S4
	VADD	V25.S4, V22.S4, V22.S4
	WORD	$0x6eb58694	// sub v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// func varianceBlock16xNDotProd(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)
TEXT ·varianceBlock16xNDotProd(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sumOut+40(FP), R4
	MOVD	sseOut+48(FP), R5

	MOVD	$1, R7
	VDUP	R7, V31.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	CBZ	R6, var_dp16xN_done

var_dp16xN_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	WORD	$0x6e9f9414	// udot v20.4s, v0.16b, v31.16b
	WORD	$0x6e9f9435	// udot v21.4s, v1.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, var_dp16xN_loop

var_dp16xN_done:
	WORD	$0x6eb58694	// sub v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// func sseBlock16xNDotProd(src *byte, srcStride int, ref *byte, refStride int, height int, sseOut *uint32)
TEXT ·sseBlock16xNDotProd(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sseOut+40(FP), R4

	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16
	LSR	$1, R6, R7
	CBZ	R7, sse_dp16_tail

sse_dp16_pair_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829457	// udot v23.4s, v2.16b, v2.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	SUB	$1, R7, R7
	CBNZ	R7, sse_dp16_pair_loop

sse_dp16_tail:
	AND	$1, R6, R7
	CBZ	R7, sse_dp16_done
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b

sse_dp16_done:
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F22, (R4)
	RET

// func varianceBlock8xNDotProd(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)
TEXT ·varianceBlock8xNDotProd(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sumOut+40(FP), R4
	MOVD	sseOut+48(FP), R5

	MOVD	$1, R7
	VDUP	R7, V31.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	LSR	$1, R6, R7
	CBZ	R7, var_dp8_tail

var_dp8_pair_loop:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	ADD	R1, R0, R8
	ADD	R3, R2, R9
	FMOVD	(R8), F4
	FMOVD	(R9), F5
	WORD	$0x6e180480	// mov v0.d[1], v4.d[0]
	WORD	$0x6e1804a1	// mov v1.d[1], v5.d[0]

	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	WORD	$0x6e9f9414	// udot v20.4s, v0.16b, v31.16b
	WORD	$0x6e9f9435	// udot v21.4s, v1.16b, v31.16b

	ADD	R1, R0, R0
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	R3, R2, R2
	SUB	$1, R7, R7
	CBNZ	R7, var_dp8_pair_loop

var_dp8_tail:
	AND	$1, R6, R7
	CBZ	R7, var_dp8_done
	VEOR	V0.B16, V0.B16, V0.B16
	VEOR	V1.B16, V1.B16, V1.B16
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b
	WORD	$0x6e9f9414	// udot v20.4s, v0.16b, v31.16b
	WORD	$0x6e9f9435	// udot v21.4s, v1.16b, v31.16b

var_dp8_done:
	WORD	$0x6eb58694	// sub v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	VADDV	V22.S4, V22
	FMOVS	F20, (R4)
	FMOVS	F22, (R5)
	RET

// func sseBlock8xNDotProd(src *byte, srcStride int, ref *byte, refStride int, height int, sseOut *uint32)
TEXT ·sseBlock8xNDotProd(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	height+32(FP), R6
	MOVD	sseOut+40(FP), R4

	VEOR	V22.B16, V22.B16, V22.B16
	LSR	$1, R6, R7
	CBZ	R7, sse_dp8_tail

sse_dp8_pair_loop:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	ADD	R1, R0, R8
	ADD	R3, R2, R9
	FMOVD	(R8), F4
	FMOVD	(R9), F5
	WORD	$0x6e180480	// mov v0.d[1], v4.d[0]
	WORD	$0x6e1804a1	// mov v1.d[1], v5.d[0]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b

	ADD	R1, R0, R0
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	R3, R2, R2
	SUB	$1, R7, R7
	CBNZ	R7, sse_dp8_pair_loop

sse_dp8_tail:
	AND	$1, R6, R7
	CBZ	R7, sse_dp8_done
	VEOR	V0.B16, V0.B16, V0.B16
	VEOR	V1.B16, V1.B16, V1.B16
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e829456	// udot v22.4s, v2.16b, v2.16b

sse_dp8_done:
	VADDV	V22.S4, V22
	FMOVS	F22, (R4)
	RET
