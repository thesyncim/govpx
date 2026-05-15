//go:build arm64 && !purego

// ARMv8 NEON one-pass 16-wide byte bilinear filters for the specialized
// sub-pel variance branches. These mirror libvpx v1.16.0
// vpx_dsp/arm/subpel_variance_neon.c var_filter_block2d_bil_w16 for the
// cases where either xoffset or yoffset is zero, avoiding the full two-pass
// intermediate pipeline.

#include "textflag.h"

// func bilinearFilter16x16HorizontalNEON(src *byte, srcStride int, dst *byte, height int, f0 uint64, f1 uint64)
TEXT ·bilinearFilter16x16HorizontalNEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	height+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8

bilbyte16_h_loop:
	// Load bytes [0..31]; the filter reads [0..16].
	VLD1	(R0), [V0.B16, V1.B16]

	// Tap 0: bytes [0..15].
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h

	// Tap 1: bytes [1..16].
	VEXT	$1, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h
	WORD	$0x2e7f812c	// umlal  v12.4s, v9.4h, v31.4h
	WORD	$0x6e7f812d	// umlal2 v13.4s, v9.8h, v31.8h

	// Round (val + 64) >> 7, then narrow uint32 -> uint16 -> uint8.
	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7
	WORD	$0x2e2149c8	// uqxtn  v8.8b,  v14.8h
	WORD	$0x6e2149e8	// uqxtn2 v8.16b, v15.8h

	VST1	[V8.B16], (R2)

	ADD	R1, R0, R0
	ADD	$16, R2, R2
	SUB	$1, R3, R3
	CBNZ	R3, bilbyte16_h_loop
	RET

// func bilinearFilter16x16VerticalNEON(src *byte, srcStride int, dst *byte, height int, f0 uint64, f1 uint64)
TEXT ·bilinearFilter16x16VerticalNEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	height+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8

bilbyte16_v_loop:
	// Tap 0: row y.
	VLD1	(R0), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h

	// Tap 1: row y+1.
	ADD	R1, R0, R6
	VLD1	(R6), [V2.B16]
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h
	WORD	$0x2e7f812c	// umlal  v12.4s, v9.4h, v31.4h
	WORD	$0x6e7f812d	// umlal2 v13.4s, v9.8h, v31.8h

	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7
	WORD	$0x2e2149c8	// uqxtn  v8.8b,  v14.8h
	WORD	$0x6e2149e8	// uqxtn2 v8.16b, v15.8h

	VST1	[V8.B16], (R2)

	ADD	R1, R0, R0
	ADD	$16, R2, R2
	SUB	$1, R3, R3
	CBNZ	R3, bilbyte16_v_loop
	RET
