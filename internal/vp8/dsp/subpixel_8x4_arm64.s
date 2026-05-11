//go:build arm64 && !purego

// ARMv8 NEON 8x4 six-tap subpel prediction. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c sixtap_filter4d_neon's
// w8 path with H=4 (so the horizontal pass runs H+5=9 rows). Two
// passes:
//
//   horizontal: for y in [0, 9), x in [0, 8):
//     v = sum_{k=0..5} src[y*srcStride+x+k] * hFilter[k] + 64
//     tmp[y*8+x] = clip255(v >> 7)
//
//   vertical: for y in [0, 4), x in [0, 8):
//     v = sum_{k=0..5} tmp[(y+k)*8+x] * vFilter[k] + 64
//     dst[y*dstStride+x] = clip255(v >> 7)
//
// Identical encoding strategy to subpixel_8x8_arm64.s — just shorter
// loop counts and a smaller tmp buffer (9*8 bytes).
//
// Calling convention (ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[9*8]byte

#include "textflag.h"

TEXT ·sixTapPredict8x4NEON(SB), NOSPLIT, $0-56
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
	MOVHU	4(R4), R7
	VDUP	R7, V18.H8
	MOVHU	6(R4), R7
	VDUP	R7, V19.H8
	MOVHU	8(R4), R7
	VDUP	R7, V20.H8
	MOVHU	10(R4), R7
	VDUP	R7, V21.H8

	MOVHU	(R5), R7
	VDUP	R7, V22.H8
	MOVHU	2(R5), R7
	VDUP	R7, V23.H8
	MOVHU	4(R5), R7
	VDUP	R7, V24.H8
	MOVHU	6(R5), R7
	VDUP	R7, V25.H8
	MOVHU	8(R5), R7
	VDUP	R7, V26.H8
	MOVHU	10(R5), R7
	VDUP	R7, V27.H8

	// === Horizontal pass: 9 rows ===
	MOVD	$9, R8
	MOVD	R6, R9

horiz_loop:
	VLD1	(R2), [V0.B16, V1.B16]

	// Tap 0: bytes [0..7]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e70c10a	// smull  v10.4s, v8.4h, v16.4h
	WORD	$0x4e70c10b	// smull2 v11.4s, v8.8h, v16.8h

	// Tap 1: bytes [1..8]
	VEXT	$1, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e71810a	// smlal  v10.4s, v8.4h, v17.4h
	WORD	$0x4e71810b	// smlal2 v11.4s, v8.8h, v17.8h

	// Tap 2: bytes [2..9]
	VEXT	$2, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e72810a	// smlal  v10.4s, v8.4h, v18.4h
	WORD	$0x4e72810b	// smlal2 v11.4s, v8.8h, v18.8h

	// Tap 3: bytes [3..10]
	VEXT	$3, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e73810a	// smlal  v10.4s, v8.4h, v19.4h
	WORD	$0x4e73810b	// smlal2 v11.4s, v8.8h, v19.8h

	// Tap 4: bytes [4..11]
	VEXT	$4, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e74810a	// smlal  v10.4s, v8.4h, v20.4h
	WORD	$0x4e74810b	// smlal2 v11.4s, v8.8h, v20.8h

	// Tap 5: bytes [5..12]
	VEXT	$5, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e75810a	// smlal  v10.4s, v8.4h, v21.4h
	WORD	$0x4e75810b	// smlal2 v11.4s, v8.8h, v21.8h

	WORD	$0x2f198d4e	// sqrshrun  v14.4h, v10.4s, #7
	WORD	$0x6f198d6e	// sqrshrun2 v14.8h, v11.4s, #7
	WORD	$0x2e2129c8	// sqxtun    v8.8b,  v14.8h

	VST1	[V8.B8], (R9)

	ADD	R3, R2, R2
	ADD	$8, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, horiz_loop

	// === Vertical pass: 4 rows ===
	MOVD	$4, R8
	MOVD	R6, R9

vert_loop:
	// Tap 0
	VLD1	(R9), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e76c10a	// smull  v10.4s, v8.4h, v22.4h
	WORD	$0x4e76c10b	// smull2 v11.4s, v8.8h, v22.8h

	ADD	$8, R9, R7
	VLD1	(R7), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e77810a	// smlal  v10.4s, v8.4h, v23.4h
	WORD	$0x4e77810b	// smlal2 v11.4s, v8.8h, v23.8h

	ADD	$16, R9, R7
	VLD1	(R7), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e78810a	// smlal  v10.4s, v8.4h, v24.4h
	WORD	$0x4e78810b	// smlal2 v11.4s, v8.8h, v24.8h

	ADD	$24, R9, R7
	VLD1	(R7), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e79810a	// smlal  v10.4s, v8.4h, v25.4h
	WORD	$0x4e79810b	// smlal2 v11.4s, v8.8h, v25.8h

	ADD	$32, R9, R7
	VLD1	(R7), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e7a810a	// smlal  v10.4s, v8.4h, v26.4h
	WORD	$0x4e7a810b	// smlal2 v11.4s, v8.8h, v26.8h

	ADD	$40, R9, R7
	VLD1	(R7), [V0.B8]
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e7b810a	// smlal  v10.4s, v8.4h, v27.4h
	WORD	$0x4e7b810b	// smlal2 v11.4s, v8.8h, v27.8h

	WORD	$0x2f198d4e	// sqrshrun  v14.4h, v10.4s, #7
	WORD	$0x6f198d6e	// sqrshrun2 v14.8h, v11.4s, #7
	WORD	$0x2e2129c8	// sqxtun    v8.8b,  v14.8h

	VST1	[V8.B8], (R0)

	ADD	R1, R0, R0
	ADD	$8, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, vert_loop

	RET
