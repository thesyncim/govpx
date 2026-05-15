//go:build arm64 && !purego

// ARMv8 NEON fused 16x16 two-axis sub-pixel variance.
//
// This mirrors libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c for
// SPECIALIZED_SUBPEL_VARIANCE_WXH_NEON(16, 16, 1), but fuses the two
// bilinear passes with the variance accumulation:
//
//   h0 = round((src[y][x]   * x0 + src[y][x+1]   * x1) / 128)
//   h1 = round((src[y+1][x] * x0 + src[y+1][x+1] * x1) / 128)
//   p  = round((h0 * y0 + h1 * y1) / 128)
//   diff = p - ref[y][x]
//   sum += diff
//   sse += diff * diff
//
// The kernel computes the horizontal result for source row 0 once,
// then carries the previous horizontal row in V16 while each loop
// computes exactly one new horizontal row into V17. That preserves the
// original 17 horizontal evaluations without writing the 17x16 temp.

#include "textflag.h"

// func subpelVariance16x16HorizontalNEON(src *byte, srcStride int, ref *byte, refStride int, f0 uint64, f1 uint64, sumOut *int32, sseOut *uint32)
TEXT ·subpelVariance16x16HorizontalNEON(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5
	MOVD	sumOut+48(FP), R6
	MOVD	sseOut+56(FP), R7

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	MOVD	$16, R8

horizontal_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h
	VEXT	$1, V1.B16, V0.B16, V4.B16
	VUXTL	V4.B8, V8.H8
	VUXTL2	V4.B16, V9.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h
	WORD	$0x2e7f812c	// umlal  v12.4s, v9.4h, v31.4h
	WORD	$0x6e7f812d	// umlal2 v13.4s, v9.8h, v31.8h
	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7
	WORD	$0x2e2149d2	// uqxtn  v18.8b,  v14.8h
	WORD	$0x6e2149f2	// uqxtn2 v18.16b, v15.8h

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242	// usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243	// usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694	// add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5	// add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R8, R8
	CBNZ	R8, horizontal_loop

	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22

	FMOVS	F20, (R6)
	FMOVS	F22, (R7)
	RET

// func subpelVariance16x16VerticalNEON(src *byte, srcStride int, ref *byte, refStride int, f0 uint64, f1 uint64, sumOut *int32, sseOut *uint32)
TEXT ·subpelVariance16x16VerticalNEON(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5
	MOVD	sumOut+48(FP), R6
	MOVD	sseOut+56(FP), R7

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	MOVD	$16, R8

vertical_loop:
	VLD1	(R0), [V0.B16]
	ADD	R1, R0, R9
	VLD1	(R9), [V2.B16]

	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h
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
	WORD	$0x2e2149d2	// uqxtn  v18.8b,  v14.8h
	WORD	$0x6e2149f2	// uqxtn2 v18.16b, v15.8h

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242	// usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243	// usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694	// add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5	// add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R8, R8
	CBNZ	R8, vertical_loop

	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22

	FMOVS	F20, (R6)
	FMOVS	F22, (R7)
	RET

TEXT ·subpelVariance16x16BilinearNEON(SB), NOSPLIT, $0-80
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	x0+32(FP), R4
	MOVD	x1+40(FP), R5
	MOVD	y0+48(FP), R6
	MOVD	y1+56(FP), R7
	MOVD	sumOut+64(FP), R8
	MOVD	sseOut+72(FP), R9

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8
	VDUP	R6, V28.H8
	VDUP	R7, V29.H8

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	// Horizontal bilinear on source row 0 -> V16.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h
	VEXT	$1, V1.B16, V0.B16, V4.B16
	VUXTL	V4.B8, V8.H8
	VUXTL2	V4.B16, V9.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h
	WORD	$0x2e7f812c	// umlal  v12.4s, v9.4h, v31.4h
	WORD	$0x6e7f812d	// umlal2 v13.4s, v9.8h, v31.8h
	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7
	WORD	$0x2e2149d0	// uqxtn  v16.8b,  v14.8h
	WORD	$0x6e2149f0	// uqxtn2 v16.16b, v15.8h

	ADD	R1, R0, R0
	MOVD	$16, R10

loop:
	// Horizontal bilinear on the next source row -> V17.B16.
	VLD1	(R0), [V2.B16, V3.B16]
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h
	VEXT	$1, V3.B16, V2.B16, V4.B16
	VUXTL	V4.B8, V8.H8
	VUXTL2	V4.B16, V9.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h
	WORD	$0x2e7f812c	// umlal  v12.4s, v9.4h, v31.4h
	WORD	$0x6e7f812d	// umlal2 v13.4s, v9.8h, v31.8h
	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7
	WORD	$0x2e2149d1	// uqxtn  v17.8b,  v14.8h
	WORD	$0x6e2149f1	// uqxtn2 v17.16b, v15.8h

	// Vertical bilinear on previous/current horizontal rows -> V18.B16.
	VUXTL	V16.B8, V8.H8
	VUXTL2	V16.B16, V9.H8
	WORD	$0x2e7cc10a	// umull  v10.4s, v8.4h, v28.4h
	WORD	$0x6e7cc10b	// umull2 v11.4s, v8.8h, v28.8h
	WORD	$0x2e7cc12c	// umull  v12.4s, v9.4h, v28.4h
	WORD	$0x6e7cc12d	// umull2 v13.4s, v9.8h, v28.8h
	VUXTL	V17.B8, V8.H8
	VUXTL2	V17.B16, V9.H8
	WORD	$0x2e7d810a	// umlal  v10.4s, v8.4h, v29.4h
	WORD	$0x6e7d810b	// umlal2 v11.4s, v8.8h, v29.8h
	WORD	$0x2e7d812c	// umlal  v12.4s, v9.4h, v29.4h
	WORD	$0x6e7d812d	// umlal2 v13.4s, v9.8h, v29.8h
	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7
	WORD	$0x2e2149d2	// uqxtn  v18.8b,  v14.8h
	WORD	$0x6e2149f2	// uqxtn2 v18.16b, v15.8h

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242	// usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243	// usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694	// add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5	// add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056	// smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057	// smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076	// smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077	// smlal2 v23.4s, v3.8h, v3.8h

	// V16 = V17 for the next output row.
	VEXT	$0, V17.B16, V17.B16, V16.B16

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, loop

	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22

	FMOVS	F20, (R8)
	FMOVS	F22, (R9)

	RET
