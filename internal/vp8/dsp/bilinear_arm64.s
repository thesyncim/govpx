// ARMv8 NEON two-tap (bilinear) subpel prediction. Mirrors libvpx v1.16.0
// vp8/common/filter.c bilinear_predict 16x16 and 8x8 paths.
//
//   horizontal: for y in [0, height+1), x in [0, width):
//     tmp[y*width+x] = (src[y*srcStride+x]*hFilter[0]
//                       + src[y*srcStride+x+1]*hFilter[1]
//                       + 64) >> 7
//
//   vertical: for y in [0, height), x in [0, width):
//     dst[y*dstStride+x] = (tmp[y*width+x]*vFilter[0]
//                           + tmp[(y+1)*width+x]*vFilter[1]
//                           + 64) >> 7
//
// Filter taps are non-negative and sum to 128, so all intermediates fit
// in uint16 lanes (max value of horizontal output is 255). tmp is stored
// as bytes; vertical re-widens to uint16 for the multiply.
//
// Calling convention (ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[2]int16
//   vFilter+40(FP)   *[2]int16
//   tmp+48(FP)       *[17*16]byte (16x16) or *[9*8]byte (8x8)
//
// Plan9 VEXT operand order: VEXT $imm, V_a, V_b, V_d encodes
// V_a -> Rm (high) and V_b -> Rn (low). For "bytes [imm..imm+15] of
// (V0:V1)" the form is VEXT $imm, V1, V0, V_d.
//
// Registers:
//   R0=dst R1=dstStride R2=src R3=srcStride R4=hFilter R5=vFilter
//   R6=tmp R7,R8=loop scratch R9=tmp cursor
//   V0,V1     src bytes (horizontal); per-row tmp slot (vertical)
//   V2        VEXT tap window (horizontal)
//   V8,V9     widened uint8 -> uint16 lanes
//   V10..V13  uint32 accumulators (horizontal/vertical)
//   V14,V15   saturate intermediates
//   V16,V17   hFilter[0..1] broadcasts (uint16)
//   V18,V19   vFilter[0..1] broadcasts (uint16)

#include "textflag.h"

// func bilinearPredict16x16NEON(dst *byte, dstStride int, src *byte, srcStride int,
//     hFilter *[2]int16, vFilter *[2]int16, tmp *[17*16]byte)
TEXT ·bilinearPredict16x16NEON(SB), NOSPLIT, $0-56
	MOVD	dst+0(FP), R0
	MOVD	dstStride+8(FP), R1
	MOVD	src+16(FP), R2
	MOVD	srcStride+24(FP), R3
	MOVD	hFilter+32(FP), R4
	MOVD	vFilter+40(FP), R5
	MOVD	tmp+48(FP), R6

	MOVHU	(R4), R7
	VDUP	R7, V16.H8
	MOVHU	2(R4), R7
	VDUP	R7, V17.H8

	MOVHU	(R5), R7
	VDUP	R7, V18.H8
	MOVHU	2(R5), R7
	VDUP	R7, V19.H8

	// === Horizontal pass: 17 rows of 16 cols ===
	MOVD	$17, R8
	MOVD	R6, R9

bilin16_horiz_loop:
	// Load 32 bytes from src[y][0..31] (we only need [0..16]).
	VLD1	(R2), [V0.B16, V1.B16]

	// Tap 0: bytes [0..15].
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e70c10a	// umull  v10.4s, v8.4h, v16.4h
	WORD	$0x6e70c10b	// umull2 v11.4s, v8.8h, v16.8h
	WORD	$0x2e70c12c	// umull  v12.4s, v9.4h, v16.4h
	WORD	$0x6e70c12d	// umull2 v13.4s, v9.8h, v16.8h

	// Tap 1: bytes [1..16]. Plan9 form: VEXT $1, V1, V0.
	VEXT	$1, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x2e71810a	// umlal  v10.4s, v8.4h, v17.4h
	WORD	$0x6e71810b	// umlal2 v11.4s, v8.8h, v17.8h
	WORD	$0x2e71812c	// umlal  v12.4s, v9.4h, v17.4h
	WORD	$0x6e71812d	// umlal2 v13.4s, v9.8h, v17.8h

	// uqrshrn: round (val + 64) >> 7 from uint32 to uint16, saturate.
	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7

	// uqxtn: narrow uint16 to uint8 with saturation. Values <= 255 so no clip.
	WORD	$0x2e2149c8	// uqxtn  v8.8b,  v14.8h
	WORD	$0x6e2149e8	// uqxtn2 v8.16b, v15.8h

	VST1	[V8.B16], (R9)

	ADD	R3, R2, R2
	ADD	$16, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, bilin16_horiz_loop

	// === Vertical pass: 16 rows of 16 cols ===
	MOVD	$16, R8
	MOVD	R6, R9

bilin16_vert_loop:
	// Tap 0: tmp[y]
	VLD1	(R9), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e72c10a	// umull  v10.4s, v8.4h, v18.4h
	WORD	$0x6e72c10b	// umull2 v11.4s, v8.8h, v18.8h
	WORD	$0x2e72c12c	// umull  v12.4s, v9.4h, v18.4h
	WORD	$0x6e72c12d	// umull2 v13.4s, v9.8h, v18.8h

	// Tap 1: tmp[y+1]
	ADD	$16, R9, R7
	VLD1	(R7), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e73810a	// umlal  v10.4s, v8.4h, v19.4h
	WORD	$0x6e73810b	// umlal2 v11.4s, v8.8h, v19.8h
	WORD	$0x2e73812c	// umlal  v12.4s, v9.4h, v19.4h
	WORD	$0x6e73812d	// umlal2 v13.4s, v9.8h, v19.8h

	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7
	WORD	$0x2f199d8f	// uqrshrn  v15.4h, v12.4s, #7
	WORD	$0x6f199daf	// uqrshrn2 v15.8h, v13.4s, #7

	WORD	$0x2e2149c8	// uqxtn  v8.8b,  v14.8h
	WORD	$0x6e2149e8	// uqxtn2 v8.16b, v15.8h

	VST1	[V8.B16], (R0)

	ADD	R1, R0, R0
	ADD	$16, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, bilin16_vert_loop

	RET

// func bilinearPredict8x8NEON(dst *byte, dstStride int, src *byte, srcStride int,
//     hFilter *[2]int16, vFilter *[2]int16, tmp *[9*8]byte)
TEXT ·bilinearPredict8x8NEON(SB), NOSPLIT, $0-56
	MOVD	dst+0(FP), R0
	MOVD	dstStride+8(FP), R1
	MOVD	src+16(FP), R2
	MOVD	srcStride+24(FP), R3
	MOVD	hFilter+32(FP), R4
	MOVD	vFilter+40(FP), R5
	MOVD	tmp+48(FP), R6

	MOVHU	(R4), R7
	VDUP	R7, V16.H8
	MOVHU	2(R4), R7
	VDUP	R7, V17.H8

	MOVHU	(R5), R7
	VDUP	R7, V18.H8
	MOVHU	2(R5), R7
	VDUP	R7, V19.H8

	// === Horizontal pass: 9 rows of 8 cols ===
	MOVD	$9, R8
	MOVD	R6, R9

bilin8_horiz_loop:
	// Load 16 bytes from src[y][0..15] (we only need [0..8]).
	VLD1	(R2), [V0.B16]

	// Tap 0: bytes [0..7].
	VUXTL	V0.B8, V8.H8
	WORD	$0x2e70c10a	// umull  v10.4s, v8.4h, v16.4h
	WORD	$0x6e70c10b	// umull2 v11.4s, v8.8h, v16.8h

	// Tap 1: bytes [1..8]. Plan9 form: VEXT $1, V0, V0 to shift right.
	VEXT	$1, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x2e71810a	// umlal  v10.4s, v8.4h, v17.4h
	WORD	$0x6e71810b	// umlal2 v11.4s, v8.8h, v17.8h

	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7

	WORD	$0x2e2149c8	// uqxtn v8.8b, v14.8h

	VST1	[V8.B8], (R9)

	ADD	R3, R2, R2
	ADD	$8, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, bilin8_horiz_loop

	// === Vertical pass: 8 rows of 8 cols ===
	MOVD	$8, R8
	MOVD	R6, R9

bilin8_vert_loop:
	// Tap 0: tmp[y]
	VLD1	(R9), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x2e72c10a	// umull  v10.4s, v8.4h, v18.4h
	WORD	$0x6e72c10b	// umull2 v11.4s, v8.8h, v18.8h

	// Tap 1: tmp[y+1]
	ADD	$8, R9, R7
	VLD1	(R7), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x2e73810a	// umlal  v10.4s, v8.4h, v19.4h
	WORD	$0x6e73810b	// umlal2 v11.4s, v8.8h, v19.8h

	WORD	$0x2f199d4e	// uqrshrn  v14.4h, v10.4s, #7
	WORD	$0x6f199d6e	// uqrshrn2 v14.8h, v11.4s, #7

	WORD	$0x2e2149c8	// uqxtn v8.8b, v14.8h

	VST1	[V8.B8], (R0)

	ADD	R1, R0, R0
	ADD	$8, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, bilin8_vert_loop

	RET
