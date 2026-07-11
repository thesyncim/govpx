//go:build arm64 && !purego

// VP9 ARMv8 NEON sub-pixel variance bilinear filter kernels. Mirrors
// libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c.
//
// libvpx scales the bilinear filter from {128 - 16k, 16k} (FILTER_BITS=7
// rounding) down to {8 - k, k} with shift=3 so the entire filter chain
// stays in uint8. Per row:
//
//   blend  = vmlal_u8(vmull_u8(s0, f0), s1, f1)  // uint16x8 accumulator
//   out_u8 = vrshrn_n_u16(blend, 3)
//
// First-pass advances along the row axis (pixel_step = 1) so the
// second tap reads src[x+1]. Second-pass advances along the column
// axis (pixel_step = width) so the second tap reads src[(y+1)*w + x];
// the intermediate buffer is tightly packed at width=W.
//
// The {x,y}-offset==0 fast path is handled in Go (just copies the
// source row to the temp buffer).
//
// UMULL/UMLAL/RSHRN aren't natively known to Go's arm64 assembler, so
// they're emitted as raw WORD directives.

#include "textflag.h"

// subpelVarFilter4NEON ABI ($0-56): w=4 single-tap-step bilinear.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte  (tightly packed, width=4)
//   pixelStep+24(FP) int    (1 for horiz, 4 for vert)
//   height+32(FP)    int
//   f0+40(FP)        uint64 (low byte used, value 8 - offset/16)
//   f1+48(FP)        uint64 (low byte used, value offset/16)
TEXT ·subpelVarFilter4NEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	height+32(FP), R4
	MOVD	f0+40(FP), R5
	MOVD	f1+48(FP), R6

	VDUP	R5, V30.B16
	VDUP	R6, V31.B16

	CBZ	R4, w4_done

w4_loop:
	// Load 8 bytes per row; only low 4 matter for output (high 4 are slop
	// or padding present in the buffer). Tap0 = src[x], tap1 = src[x+step].
	FMOVD	(R0), F0          // row y, 8 bytes
	ADD	R3, R0, R7
	FMOVD	(R7), F1          // row y + pixel_step, 8 bytes

	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x2e3f802a       // umlal  v10.8h, v1.8b,  v31.8b
	WORD	$0x0f0d8d4e       // rshrn  v14.8b, v10.8h, #3

	// Store 4 bytes (low lanes).
	FMOVS	F14, (R2)

	ADD	R1, R0, R0
	ADD	$4, R2, R2
	SUB	$1, R4, R4
	CBNZ	R4, w4_loop

w4_done:
	RET

// subpelVarFilter8NEON ABI ($0-56): w=8.
TEXT ·subpelVarFilter8NEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	height+32(FP), R4
	MOVD	f0+40(FP), R5
	MOVD	f1+48(FP), R6

	VDUP	R5, V30.B16
	VDUP	R6, V31.B16

	CBZ	R4, w8_done

w8_loop:
	FMOVD	(R0), F0          // row y, 8 bytes
	ADD	R3, R0, R7
	FMOVD	(R7), F1          // row y + pixel_step

	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x2e3f802a       // umlal  v10.8h, v1.8b,  v31.8b
	WORD	$0x0f0d8d4e       // rshrn  v14.8b, v10.8h, #3

	FMOVD	F14, (R2)

	ADD	R1, R0, R0
	ADD	$8, R2, R2
	SUB	$1, R4, R4
	CBNZ	R4, w8_loop

w8_done:
	RET

// subpelVarFilter16NEON ABI ($0-56): w=16.
TEXT ·subpelVarFilter16NEON(SB), NOSPLIT, $0-56
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	height+32(FP), R4
	MOVD	f0+40(FP), R5
	MOVD	f1+48(FP), R6

	VDUP	R5, V30.B16
	VDUP	R6, V31.B16

	CBZ	R4, w16_done

w16_loop:
	VLD1	(R0), [V0.B16]    // row y, 16 bytes
	ADD	R3, R0, R7
	VLD1	(R7), [V1.B16]    // row y + pixel_step

	// uint16 accumulators V10, V11 for low/high halves.
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f802a       // umlal  v10.8h, v1.8b,  v31.8b
	WORD	$0x6e3f802b       // umlal2 v11.8h, v1.16b, v31.16b

	WORD	$0x0f0d8d4e       // rshrn  v14.8b,  v10.8h, #3
	WORD	$0x4f0d8d6e       // rshrn2 v14.16b, v11.8h, #3

	VST1	[V14.B16], (R2)

	ADD	R1, R0, R0
	ADD	$16, R2, R2
	SUB	$1, R4, R4
	CBNZ	R4, w16_loop

w16_done:
	RET

// subpelVarFilter16ChunksNEON ABI ($0-64): chunks * 16 wide, height
// rows. Repeats the w16 loop body across chunks per row.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   pixelStep+24(FP) int     (1 for horiz, w=16*chunks for vert)
//   width+32(FP)     int     (16 * chunks)
//   height+40(FP)    int
//   f0+48(FP)        uint64
//   f1+56(FP)        uint64
TEXT ·subpelVarFilter16ChunksNEON(SB), NOSPLIT, $0-64
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	width+32(FP), R4
	MOVD	height+40(FP), R5
	MOVD	f0+48(FP), R6
	MOVD	f1+56(FP), R7

	VDUP	R6, V30.B16
	VDUP	R7, V31.B16

	CBZ	R5, chunks_done

chunks_rowLoop:
	MOVD	R4, R8         // remaining columns this row
	MOVD	R0, R9         // row src cursor
	MOVD	R2, R10        // row dst cursor

chunks_colLoop:
	VLD1	(R9), [V0.B16]
	ADD	R3, R9, R11
	VLD1	(R11), [V1.B16]

	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f802a       // umlal  v10.8h, v1.8b,  v31.8b
	WORD	$0x6e3f802b       // umlal2 v11.8h, v1.16b, v31.16b

	WORD	$0x0f0d8d4e       // rshrn  v14.8b,  v10.8h, #3
	WORD	$0x4f0d8d6e       // rshrn2 v14.16b, v11.8h, #3

	VST1	[V14.B16], (R10)

	ADD	$16, R9, R9
	ADD	$16, R10, R10
	SUB	$16, R8, R8
	CBNZ	R8, chunks_colLoop

	ADD	R1, R0, R0
	ADD	R4, R2, R2     // dst stride == width since temp is tightly packed
	SUB	$1, R5, R5
	CBNZ	R5, chunks_rowLoop

chunks_done:
	RET

// subpelVarAvg8NEON ABI ($0-40): w=8 rounded average filter.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   pixelStep+24(FP) int
//   height+32(FP)    int
TEXT ·subpelVarAvg8NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	height+32(FP), R4

	CBZ	R4, avg8_done

avg8_loop:
	FMOVD	(R0), F0
	ADD	R3, R0, R7
	FMOVD	(R7), F1
	WORD	$0x6e211400       // urhadd.16b v0, v0, v1
	FMOVD	F0, (R2)

	ADD	R1, R0, R0
	ADD	$8, R2, R2
	SUB	$1, R4, R4
	CBNZ	R4, avg8_loop

avg8_done:
	RET

// subpelVarAvg16NEON ABI ($0-40): w=16 rounded average filter.
TEXT ·subpelVarAvg16NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	height+32(FP), R4

	CBZ	R4, avg16_done

avg16_loop:
	VLD1	(R0), [V0.B16]
	ADD	R3, R0, R7
	VLD1	(R7), [V1.B16]
	WORD	$0x6e211400       // urhadd.16b v0, v0, v1
	VST1	[V0.B16], (R2)

	ADD	R1, R0, R0
	ADD	$16, R2, R2
	SUB	$1, R4, R4
	CBNZ	R4, avg16_loop

avg16_done:
	RET

// subpelVarAvg16ChunksNEON ABI ($0-48): chunks * 16 wide rounded average.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *byte
//   pixelStep+24(FP) int
//   width+32(FP)     int
//   height+40(FP)    int
TEXT ·subpelVarAvg16ChunksNEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	pixelStep+24(FP), R3
	MOVD	width+32(FP), R4
	MOVD	height+40(FP), R5

	CBZ	R5, avg_chunks_done

avg_chunks_rowLoop:
	MOVD	R4, R8
	MOVD	R0, R9
	MOVD	R2, R10

avg_chunks_colLoop:
	VLD1	(R9), [V0.B16]
	ADD	R3, R9, R11
	VLD1	(R11), [V1.B16]
	WORD	$0x6e211400       // urhadd.16b v0, v0, v1
	VST1	[V0.B16], (R10)

	ADD	$16, R9, R9
	ADD	$16, R10, R10
	SUB	$16, R8, R8
	CBNZ	R8, avg_chunks_colLoop

	ADD	R1, R0, R0
	ADD	R4, R2, R2
	SUB	$1, R5, R5
	CBNZ	R5, avg_chunks_rowLoop

avg_chunks_done:
	RET

// Fused one-axis 16x16 filters accumulate variance directly without a
// temporary prediction block.
//
// func subpelVariance16x16HorizontalNEON(src *byte, srcStride int, ref *byte,
//   refStride int, f0 uint64, f1 uint64, sumOut *int32, sseOut *uint32)
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

	CMP	$4, R4
	BEQ	vp9_horizontal_avg_loop

vp9_horizontal_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a       // umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b       // umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f0d8d52       // rshrn  v18.8b,  v10.8h, #3
	WORD	$0x4f0d8d72       // rshrn2 v18.16b, v11.8h, #3
	B	vp9_horizontal_accumulate

vp9_horizontal_avg_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x6e241412       // urhadd v18.16b, v0.16b, v4.16b

vp9_horizontal_accumulate:
	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242       // usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243       // usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694       // add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5       // add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056       // smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057       // smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076       // smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077       // smlal2 v23.4s, v3.8h, v3.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R8, R8
	CBZ	R8, vp9_horizontal_done
	CMP	$4, R4
	BEQ	vp9_horizontal_avg_loop
	B	vp9_horizontal_loop

vp9_horizontal_done:
	WORD	$0x4e758694       // add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94       // saddlv s20, v20.8h
	WORD	$0x4eb786d6       // add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R6)
	FMOVS	F22, (R7)
	RET

// func subpelVariance16x16VerticalNEON(src *byte, srcStride int, ref *byte,
//   refStride int, f0 uint64, f1 uint64, sumOut *int32, sseOut *uint32)
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

	VLD1	(R0), [V0.B16]
	ADD	R1, R0, R0
	CMP	$4, R4
	BEQ	vp9_vertical_avg_loop

vp9_vertical_loop:
	VLD1	(R0), [V2.B16]
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f804a       // umlal  v10.8h, v2.8b,  v31.8b
	WORD	$0x6e3f804b       // umlal2 v11.8h, v2.16b, v31.16b
	WORD	$0x0f0d8d52       // rshrn  v18.8b,  v10.8h, #3
	WORD	$0x4f0d8d72       // rshrn2 v18.16b, v11.8h, #3
	B	vp9_vertical_accumulate

vp9_vertical_avg_loop:
	VLD1	(R0), [V2.B16]
	WORD	$0x6e221412       // urhadd v18.16b, v0.16b, v2.16b

vp9_vertical_accumulate:
	VORR	V2.B16, V2.B16, V0.B16
	VLD1	(R2), [V19.B16]
	WORD	$0x2e332244       // usubl  v4.8h, v18.8b,  v19.8b
	WORD	$0x6e332245       // usubl2 v5.8h, v18.16b, v19.16b
	WORD	$0x4e648694       // add v20.8h, v20.8h, v4.8h
	WORD	$0x4e6586b5       // add v21.8h, v21.8h, v5.8h
	WORD	$0x0e648096       // smlal  v22.4s, v4.4h, v4.4h
	WORD	$0x4e648097       // smlal2 v23.4s, v4.8h, v4.8h
	WORD	$0x0e6580b6       // smlal  v22.4s, v5.4h, v5.4h
	WORD	$0x4e6580b7       // smlal2 v23.4s, v5.8h, v5.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R8, R8
	CBZ	R8, vp9_one_axis_done
	CMP	$4, R4
	BEQ	vp9_vertical_avg_loop
	B	vp9_vertical_loop

vp9_one_axis_done:
	WORD	$0x4e758694       // add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94       // saddlv s20, v20.8h
	WORD	$0x4eb786d6       // add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22
	FMOVS	F20, (R6)
	FMOVS	F22, (R7)
	RET

// subpelVariance16x16BilinearNEON fuses both bilinear passes with the
// variance accumulation. VP9's scaled taps are in [0,8] and each stage
// rounds by 3 bits. The previous horizontal row stays in V16, avoiding the
// 17x16 first-pass buffer and the 16x16 second-pass buffer.
//
// func subpelVariance16x16BilinearNEON(src *byte, srcStride int, ref *byte,
//   refStride int, x0 uint64, x1 uint64, y0 uint64, y1 uint64,
//   sumOut *int32, sseOut *uint32)
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

	// Offset 4 has scaled taps {4,4}, exactly rounded-average.
	CMP	$4, R6
	BNE	vp9_bilinear_gen_gen
	CMP	$4, R4
	BEQ	vp9_bilinear_avg_avg
	B	vp9_bilinear_gen_avg

vp9_bilinear_gen_gen:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a       // umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b       // umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f0d8d50       // rshrn  v16.8b,  v10.8h, #3
	WORD	$0x4f0d8d70       // rshrn2 v16.16b, v11.8h, #3

	ADD	R1, R0, R0

vp9_gen_gen_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a       // umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b       // umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f0d8d51       // rshrn  v17.8b,  v10.8h, #3
	WORD	$0x4f0d8d71       // rshrn2 v17.16b, v11.8h, #3

	WORD	$0x2e3cc20a       // umull  v10.8h, v16.8b, v28.8b
	WORD	$0x6e3cc20b       // umull2 v11.8h, v16.16b, v28.16b
	WORD	$0x2e3d822a       // umlal  v10.8h, v17.8b, v29.8b
	WORD	$0x6e3d822b       // umlal2 v11.8h, v17.16b, v29.16b
	WORD	$0x0f0d8d52       // rshrn  v18.8b,  v10.8h, #3
	WORD	$0x4f0d8d72       // rshrn2 v18.16b, v11.8h, #3

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242       // usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243       // usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694       // add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5       // add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056       // smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057       // smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076       // smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077       // smlal2 v23.4s, v3.8h, v3.8h

	VORR	V17.B16, V17.B16, V16.B16
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, vp9_gen_gen_loop
	B	vp9_bilinear_done

vp9_bilinear_gen_avg:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a       // umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b       // umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f0d8d50       // rshrn  v16.8b,  v10.8h, #3
	WORD	$0x4f0d8d70       // rshrn2 v16.16b, v11.8h, #3

	ADD	R1, R0, R0

vp9_gen_avg_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x2e3ec00a       // umull  v10.8h, v0.8b,  v30.8b
	WORD	$0x6e3ec00b       // umull2 v11.8h, v0.16b, v30.16b
	WORD	$0x2e3f808a       // umlal  v10.8h, v4.8b,  v31.8b
	WORD	$0x6e3f808b       // umlal2 v11.8h, v4.16b, v31.16b
	WORD	$0x0f0d8d51       // rshrn  v17.8b,  v10.8h, #3
	WORD	$0x4f0d8d71       // rshrn2 v17.16b, v11.8h, #3

	WORD	$0x6e311612       // urhadd v18.16b, v16.16b, v17.16b

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242       // usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243       // usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694       // add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5       // add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056       // smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057       // smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076       // smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077       // smlal2 v23.4s, v3.8h, v3.8h

	VORR	V17.B16, V17.B16, V16.B16
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, vp9_gen_avg_loop
	B	vp9_bilinear_done

vp9_bilinear_avg_avg:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x6e241410       // urhadd v16.16b, v0.16b, v4.16b

	ADD	R1, R0, R0

vp9_avg_avg_loop:
	VLD1	(R0), [V0.B16, V1.B16]
	VEXT	$1, V1.B16, V0.B16, V4.B16
	WORD	$0x6e241411       // urhadd v17.16b, v0.16b, v4.16b
	WORD	$0x6e311612       // urhadd v18.16b, v16.16b, v17.16b

	VLD1	(R2), [V19.B16]
	WORD	$0x2e332242       // usubl  v2.8h, v18.8b,  v19.8b
	WORD	$0x6e332243       // usubl2 v3.8h, v18.16b, v19.16b
	WORD	$0x4e628694       // add v20.8h, v20.8h, v2.8h
	WORD	$0x4e6386b5       // add v21.8h, v21.8h, v3.8h
	WORD	$0x0e628056       // smlal  v22.4s, v2.4h, v2.4h
	WORD	$0x4e628057       // smlal2 v23.4s, v2.8h, v2.8h
	WORD	$0x0e638076       // smlal  v22.4s, v3.4h, v3.4h
	WORD	$0x4e638077       // smlal2 v23.4s, v3.8h, v3.8h

	VORR	V17.B16, V17.B16, V16.B16
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R10, R10
	CBNZ	R10, vp9_avg_avg_loop

vp9_bilinear_done:
	WORD	$0x4e758694       // add v20.8h, v20.8h, v21.8h
	WORD	$0x4e703a94       // saddlv s20, v20.8h
	WORD	$0x4eb786d6       // add v22.4s, v22.4s, v23.4s
	VADDV	V22.S4, V22

	FMOVS	F20, (R8)
	FMOVS	F22, (R9)
	RET
