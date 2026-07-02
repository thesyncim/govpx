//go:build arm64 && !purego

// ARMv8 NEON fused 16x16 two-axis sub-pixel variance.
//
// This mirrors libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// SPECIALIZED_SUBPEL_VARIANCE_WXH_NEON(16, 16, 1), but fuses the
// bilinear passes with the variance accumulation:
//
//   h0 = round((src[y][x]   * x0 + src[y][x+1]   * x1) / 128)
//   h1 = round((src[y+1][x] * x0 + src[y+1][x+1] * x1) / 128)
//   p  = round((h0 * y0 + h1 * y1) / 128)
//   diff = p - ref[y][x]
//   sum += diff
//   sse += diff * diff
//
// The VP8 bilinear taps sum to 128 and each fit in a u8, so every
// filter stage runs in u8*u8 -> u16 arithmetic (umull/umlal + rshrn #7)
// exactly like libvpx's var_filter_block2d_bil_w16. The half-pel taps
// {64, 64} reduce to round((a + b) / 2), so those stages dispatch to
// urhadd like libvpx's var_filter_block2d_avg; both forms produce
// bit-identical results. The two-axis kernel computes the horizontal
// result for source row 0 once, then carries the previous horizontal
// row in V16 while each loop computes exactly one new horizontal row
// into V17. That preserves the original 17 horizontal evaluations
// without writing the 17x16 temp.

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

	VDUP	R4, V30.B16
	VDUP	R5, V31.B16

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	MOVD	$16, R8

	// Half-pel taps {64, 64}: round((a + b) / 2) == urhadd.
	CMP	$64, R4
	BEQ	horizontal_avg_loop

horizontal_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a	// umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b	// umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a	// umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b	// umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f098d52	// rshrn  v18.8b,  v10.8h, #7
	WORD	$0x4f098d72	// rshrn2 v18.16b, v11.8h, #7

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
	B	horizontal_done

horizontal_avg_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x6e241412	// urhadd v18.16b, v0.16b, v4.16b

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
	CBNZ	R8, horizontal_avg_loop

horizontal_done:
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

	VDUP	R4, V30.B16
	VDUP	R5, V31.B16

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	MOVD	$16, R8

	// Row 0 is loaded once and carried in V0; each iteration loads only
	// the next row into V2 and then moves it into V0.
	VLD1	(R0), [V0.B16]
	ADD	R1, R0, R0

	// Half-pel taps {64, 64}: round((a + b) / 2) == urhadd.
	CMP	$64, R4
	BEQ	vertical_avg_loop

vertical_loop:
	VLD1	(R0), [V2.B16]
	WORD	$0x2e3ec00a	// umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b	// umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f804a	// umlal  v10.8h, v2.8b,  v31.8b
	WORD	$0x6e3f804b	// umlal2 v11.8h, v2.16b, v31.16b
	WORD	$0x0f098d52	// rshrn  v18.8b,  v10.8h, #7
	WORD	$0x4f098d72	// rshrn2 v18.16b, v11.8h, #7
	VORR	V2.B16, V2.B16, V0.B16

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332244	// usubl  v4.8h, v18.8b,  v19.8b
	WORD	$0x6e332245	// usubl2 v5.8h, v18.16b, v19.16b
	WORD	$0x4e648694	// add v20.8h, v20.8h, v4.8h
	WORD	$0x4e6586b5	// add v21.8h, v21.8h, v5.8h
	WORD	$0x0e648096	// smlal  v22.4s, v4.4h, v4.4h
	WORD	$0x4e648097	// smlal2 v23.4s, v4.8h, v4.8h
	WORD	$0x0e6580b6	// smlal  v22.4s, v5.4h, v5.4h
	WORD	$0x4e6580b7	// smlal2 v23.4s, v5.8h, v5.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R8, R8
	CBNZ	R8, vertical_loop
	B	vertical_done

vertical_avg_loop:
	VLD1	(R0), [V2.B16]
	WORD	$0x6e221412	// urhadd v18.16b, v0.16b, v2.16b
	VORR	V2.B16, V2.B16, V0.B16

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332244	// usubl  v4.8h, v18.8b,  v19.8b
	WORD	$0x6e332245	// usubl2 v5.8h, v18.16b, v19.16b
	WORD	$0x4e648694	// add v20.8h, v20.8h, v4.8h
	WORD	$0x4e6586b5	// add v21.8h, v21.8h, v5.8h
	WORD	$0x0e648096	// smlal  v22.4s, v4.4h, v4.4h
	WORD	$0x4e648097	// smlal2 v23.4s, v4.8h, v4.8h
	WORD	$0x0e6580b6	// smlal  v22.4s, v5.4h, v5.4h
	WORD	$0x4e6580b7	// smlal2 v23.4s, v5.8h, v5.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R8, R8
	CBNZ	R8, vertical_avg_loop

vertical_done:
	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22

	FMOVS	F20, (R6)
	FMOVS	F22, (R7)
	RET

// func subpelVariance16x16BilinearNEON(src *byte, srcStride int, ref *byte, refStride int, x0 uint64, x1 uint64, y0 uint64, y1 uint64, sumOut *int32, sseOut *uint32)
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

	VDUP	R4, V30.B16
	VDUP	R5, V31.B16
	VDUP	R6, V28.B16
	VDUP	R7, V29.B16

	VEOR	V20.B16, V20.B16, V20.B16
	VEOR	V21.B16, V21.B16, V21.B16
	VEOR	V22.B16, V22.B16, V22.B16
	VEOR	V23.B16, V23.B16, V23.B16

	MOVD	$16, R10

	// Route half-pel axes to urhadd bodies. An x half-pel with a
	// quarter-pel y (rare) stays on the general path: the {64, 64}
	// taps produce bit-identical results either way.
	CMP	$64, R6
	BNE	bilinear_gen_gen
	CMP	$64, R4
	BEQ	bilinear_avg_avg
	B	bilinear_gen_avg

bilinear_gen_gen:
	// Horizontal bilinear on source row 0 -> V16.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a	// umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b	// umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a	// umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b	// umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f098d50	// rshrn  v16.8b,  v10.8h, #7
	WORD	$0x4f098d70	// rshrn2 v16.16b, v11.8h, #7

	ADD	R1, R0, R0

gen_gen_loop:
	// Horizontal bilinear on the next source row -> V17.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a	// umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b	// umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a	// umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b	// umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f098d51	// rshrn  v17.8b,  v10.8h, #7
	WORD	$0x4f098d71	// rshrn2 v17.16b, v11.8h, #7

	// Vertical bilinear on previous/current horizontal rows -> V18.B16.
	WORD	$0x2e3cc20a	// umull  v10.8h, v16.8b,  v28.8b
	WORD	$0x6e3cc20b	// umull2 v11.8h, v16.16b, v28.16b
	WORD	$0x2e3d822a	// umlal  v10.8h, v17.8b,  v29.8b
	WORD	$0x6e3d822b	// umlal2 v11.8h, v17.16b, v29.16b
	WORD	$0x0f098d52	// rshrn  v18.8b,  v10.8h, #7
	WORD	$0x4f098d72	// rshrn2 v18.16b, v11.8h, #7

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
	VORR	V17.B16, V17.B16, V16.B16

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, gen_gen_loop
	B	bilinear_done

bilinear_gen_avg:
	// Horizontal bilinear on source row 0 -> V16.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a	// umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b	// umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a	// umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b	// umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f098d50	// rshrn  v16.8b,  v10.8h, #7
	WORD	$0x4f098d70	// rshrn2 v16.16b, v11.8h, #7

	ADD	R1, R0, R0

gen_avg_loop:
	// Horizontal bilinear on the next source row -> V17.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a	// umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b	// umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a	// umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b	// umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f098d51	// rshrn  v17.8b,  v10.8h, #7
	WORD	$0x4f098d71	// rshrn2 v17.16b, v11.8h, #7

	// Vertical half-pel average -> V18.B16.
	WORD	$0x6e311612	// urhadd v18.16b, v16.16b, v17.16b

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
	VORR	V17.B16, V17.B16, V16.B16

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, gen_avg_loop
	B	bilinear_done

bilinear_avg_avg:
	// Horizontal half-pel average on source row 0 -> V16.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x6e241410	// urhadd v16.16b, v0.16b, v4.16b

	ADD	R1, R0, R0

avg_avg_loop:
	// Horizontal half-pel average on the next source row -> V17.B16.
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x6e241411	// urhadd v17.16b, v0.16b, v4.16b

	// Vertical half-pel average -> V18.B16.
	WORD	$0x6e311612	// urhadd v18.16b, v16.16b, v17.16b

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
	VORR	V17.B16, V17.B16, V16.B16

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, avg_avg_loop

bilinear_done:
	WORD	$0x4e758694	// add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	WORD	$0x4eb786d6	// add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22

	FMOVS	F20, (R8)
	FMOVS	F22, (R9)

	RET
