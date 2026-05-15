//go:build arm64 && !purego

// ARMv8.4-A DOTPROD SAD kernels. Ports libvpx v1.16.0
// vpx_dsp/arm/sad_neon_dotprod.c and sad4d_neon_dotprod.c for the
// 16-wide VP8 motion-search kernels govpx calls most often.

#include "textflag.h"

// func sadBlock16x16DotProd(src *byte, srcStride int, ref *byte, refStride int) int32
TEXT ·sadBlock16x16DotProd(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	MOVD	$1, R4
	VDUP	R4, V31.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	MOVD	$8, R5

sad_dp16x16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9454	// udot v20.4s, v2.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9455	// udot v21.4s, v2.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	SUB	$1, R5, R5
	CBNZ	R5, sad_dp16x16_loop

	WORD	$0x4eb58694	// add v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// func sadBlock16x8DotProd(src *byte, srcStride int, ref *byte, refStride int) int32
TEXT ·sadBlock16x8DotProd(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	MOVD	$1, R4
	VDUP	R4, V31.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	MOVD	$4, R5

sad_dp16x8_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9454	// udot v20.4s, v2.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9455	// udot v21.4s, v2.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2

	SUB	$1, R5, R5
	CBNZ	R5, sad_dp16x8_loop

	WORD	$0x4eb58694	// add v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// func sadBlock16x16x4DotProd(src *byte, srcStride int, ref0 *byte, ref1 *byte, ref2 *byte, ref3 *byte, refStride int, out *[4]uint32)
TEXT ·sadBlock16x16x4DotProd(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref0+16(FP), R2
	MOVD	ref1+24(FP), R3
	MOVD	ref2+32(FP), R4
	MOVD	ref3+40(FP), R5
	MOVD	refStride+48(FP), R6

	MOVD	$1, R7
	VDUP	R7, V31.B16
	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16
	VEOR	V24.B16, V24.B16, V24.B16
	VEOR	V25.B16, V25.B16, V25.B16
	VEOR	V26.B16, V26.B16, V26.B16
	VEOR	V27.B16, V27.B16, V27.B16
	MOVD	$8, R7

sad_dp16x16x4_loop:
	VLD1	(R0), [V0.B16]

	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9454	// udot v20.4s, v2.16b, v31.16b

	VLD1	(R3), [V4.B16]
	WORD	$0x6e247402	// uabd v2.16b, v0.16b, v4.16b
	WORD	$0x6e9f9455	// udot v21.4s, v2.16b, v31.16b

	VLD1	(R4), [V5.B16]
	WORD	$0x6e257402	// uabd v2.16b, v0.16b, v5.16b
	WORD	$0x6e9f9456	// udot v22.4s, v2.16b, v31.16b

	VLD1	(R5), [V6.B16]
	WORD	$0x6e267402	// uabd v2.16b, v0.16b, v6.16b
	WORD	$0x6e9f9457	// udot v23.4s, v2.16b, v31.16b

	ADD	R1, R0, R0
	ADD	R6, R2, R2
	ADD	R6, R3, R3
	ADD	R6, R4, R4
	ADD	R6, R5, R5

	VLD1	(R0), [V0.B16]

	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9458	// udot v24.4s, v2.16b, v31.16b

	VLD1	(R3), [V4.B16]
	WORD	$0x6e247402	// uabd v2.16b, v0.16b, v4.16b
	WORD	$0x6e9f9459	// udot v25.4s, v2.16b, v31.16b

	VLD1	(R4), [V5.B16]
	WORD	$0x6e257402	// uabd v2.16b, v0.16b, v5.16b
	WORD	$0x6e9f945a	// udot v26.4s, v2.16b, v31.16b

	VLD1	(R5), [V6.B16]
	WORD	$0x6e267402	// uabd v2.16b, v0.16b, v6.16b
	WORD	$0x6e9f945b	// udot v27.4s, v2.16b, v31.16b

	ADD	R1, R0, R0
	ADD	R6, R2, R2
	ADD	R6, R3, R3
	ADD	R6, R4, R4
	ADD	R6, R5, R5
	SUB	$1, R7, R7
	CBNZ	R7, sad_dp16x16x4_loop

	MOVD	out+56(FP), R7
	VADD	V24.S4, V20.S4, V20.S4
	VADD	V25.S4, V21.S4, V21.S4
	VADD	V26.S4, V22.S4, V22.S4
	VADD	V27.S4, V23.S4, V23.S4
	VADDV	V20.S4, V20
	VADDV	V21.S4, V21
	VADDV	V22.S4, V22
	VADDV	V23.S4, V23
	VMOV	V20.S[0], R8
	VMOV	V21.S[0], R9
	VMOV	V22.S[0], R10
	VMOV	V23.S[0], R11
	MOVW	R8, 0(R7)
	MOVW	R9, 4(R7)
	MOVW	R10, 8(R7)
	MOVW	R11, 12(R7)
	RET
