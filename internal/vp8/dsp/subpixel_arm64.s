//go:build arm64 && !purego

// ARMv8 NEON 16x16 six-tap subpel prediction. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c sixtap_filter4d_neon's
// w16 path. Two passes:
//
//   horizontal: for y in [0, 21), x in [0, 16):
//     v = sum_{k=0..5} src[y*srcStride+x+k] * hFilter[k] + 64
//     tmp[y*16+x] = clip255(v >> 7)
//
//   vertical: for y in [0, 16), x in [0, 16):
//     v = sum_{k=0..5} tmp[(y+k)*16+x] * vFilter[k] + 64
//     dst[y*dstStride+x] = clip255(v >> 7)
//
// Filter rows are *[6]int16 (signed; values in roughly [-16, 123]).
// Each tap is broadcast into a Q register of 8 int16 lanes.
// Multiplication uses signed-long forms (SMULL/SMLAL); saturation
// back to uint8 uses SQRSHRUN (int32 -> uint16, rounding) followed
// by SQXTUN (int16 -> uint8, saturate). Those mnemonics aren't in
// Go's arm64 assembler, so they're emitted as raw WORD directives.
// Encodings come from clang.
//
// Plan9 VEXT operand order subtlety: VEXT $imm, V_a, V_b, V_d
// maps V_a -> Rm and V_b -> Rn in ARM EXT, where the resulting
// concatenation has Rm as the HIGH part. For "bytes [imm..imm+15] of
// (V0:V1)" with V0 holding the low 16 bytes of source and V1 holding
// the next 16, the correct Plan9 form is VEXT $imm, V1, V0, V_d.
//
// Calling convention (ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[21*16]byte
//
// Registers:
//   R0=dst R1=dstStride R2=src R3=srcStride R4=hFilter R5=vFilter
//   R6=tmp R7,R8,R9=loop scratch
//   V0,V1   src bytes (horizontal); per-row tmp slot (vertical)
//   V2      VEXT tap window (horizontal)
//   V8,V9   widened uint8 -> int16 lanes
//   V10..V13  int32x4 accumulators
//   V14,V15 saturate intermediates
//   V16..V21 hFilter[0..5] broadcasts
//   V22..V27 vFilter[0..5] broadcasts

#include "textflag.h"

TEXT ·sixTapPredict16x16NEON(SB), NOSPLIT, $0-56
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

	// === Horizontal pass: 21 rows ===
	MOVD	$21, R8
	MOVD	R6, R9

horiz_loop:
	VLD1	(R2), [V0.B16, V1.B16]

	// Tap 0: V_tap = V0 (bytes [0..15])
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e70c10a	// smull  v10.4s, v8.4h, v16.4h
	WORD	$0x4e70c10b	// smull2 v11.4s, v8.8h, v16.8h
	WORD	$0x0e70c12c	// smull  v12.4s, v9.4h, v16.4h
	WORD	$0x4e70c12d	// smull2 v13.4s, v9.8h, v16.8h

	// Tap 1: V_tap = bytes [1..16]. Plan9 order: VEXT $1, V1, V0.
	VEXT	$1, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x0e71810a	// smlal  v10.4s, v8.4h, v17.4h
	WORD	$0x4e71810b	// smlal2 v11.4s, v8.8h, v17.8h
	WORD	$0x0e71812c	// smlal  v12.4s, v9.4h, v17.4h
	WORD	$0x4e71812d	// smlal2 v13.4s, v9.8h, v17.8h

	// Tap 2: bytes [2..17]
	VEXT	$2, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x0e72810a	// smlal  v10.4s, v8.4h, v18.4h
	WORD	$0x4e72810b	// smlal2 v11.4s, v8.8h, v18.8h
	WORD	$0x0e72812c	// smlal  v12.4s, v9.4h, v18.4h
	WORD	$0x4e72812d	// smlal2 v13.4s, v9.8h, v18.8h

	// Tap 3: bytes [3..18]
	VEXT	$3, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x0e73810a	// smlal  v10.4s, v8.4h, v19.4h
	WORD	$0x4e73810b	// smlal2 v11.4s, v8.8h, v19.8h
	WORD	$0x0e73812c	// smlal  v12.4s, v9.4h, v19.4h
	WORD	$0x4e73812d	// smlal2 v13.4s, v9.8h, v19.8h

	// Tap 4: bytes [4..19]
	VEXT	$4, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x0e74810a	// smlal  v10.4s, v8.4h, v20.4h
	WORD	$0x4e74810b	// smlal2 v11.4s, v8.8h, v20.8h
	WORD	$0x0e74812c	// smlal  v12.4s, v9.4h, v20.4h
	WORD	$0x4e74812d	// smlal2 v13.4s, v9.8h, v20.8h

	// Tap 5: bytes [5..20]
	VEXT	$5, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x0e75810a	// smlal  v10.4s, v8.4h, v21.4h
	WORD	$0x4e75810b	// smlal2 v11.4s, v8.8h, v21.8h
	WORD	$0x0e75812c	// smlal  v12.4s, v9.4h, v21.4h
	WORD	$0x4e75812d	// smlal2 v13.4s, v9.8h, v21.8h

	WORD	$0x2f198d4e	// sqrshrun  v14.4h, v10.4s, #7
	WORD	$0x6f198d6e	// sqrshrun2 v14.8h, v11.4s, #7
	WORD	$0x2f198d8f	// sqrshrun  v15.4h, v12.4s, #7
	WORD	$0x6f198daf	// sqrshrun2 v15.8h, v13.4s, #7

	WORD	$0x2e2129c8	// sqxtun   v8.8b, v14.8h
	WORD	$0x6e2129e8	// sqxtun2  v8.16b, v15.8h

	VST1	[V8.B16], (R9)

	ADD	R3, R2, R2
	ADD	$16, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, horiz_loop

	// === Vertical pass: 16 rows ===
	MOVD	$16, R8
	MOVD	R6, R9

vert_loop:
	// Tap 0: tmp[y]
	VLD1	(R9), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e76c10a	// smull  v10.4s, v8.4h, v22.4h
	WORD	$0x4e76c10b	// smull2 v11.4s, v8.8h, v22.8h
	WORD	$0x0e76c12c	// smull  v12.4s, v9.4h, v22.4h
	WORD	$0x4e76c12d	// smull2 v13.4s, v9.8h, v22.8h

	ADD	$16, R9, R7
	VLD1	(R7), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e77810a	// smlal  v10.4s, v8.4h, v23.4h
	WORD	$0x4e77810b	// smlal2 v11.4s, v8.8h, v23.8h
	WORD	$0x0e77812c	// smlal  v12.4s, v9.4h, v23.4h
	WORD	$0x4e77812d	// smlal2 v13.4s, v9.8h, v23.8h

	ADD	$32, R9, R7
	VLD1	(R7), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e78810a	// smlal  v10.4s, v8.4h, v24.4h
	WORD	$0x4e78810b	// smlal2 v11.4s, v8.8h, v24.8h
	WORD	$0x0e78812c	// smlal  v12.4s, v9.4h, v24.4h
	WORD	$0x4e78812d	// smlal2 v13.4s, v9.8h, v24.8h

	ADD	$48, R9, R7
	VLD1	(R7), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e79810a	// smlal  v10.4s, v8.4h, v25.4h
	WORD	$0x4e79810b	// smlal2 v11.4s, v8.8h, v25.8h
	WORD	$0x0e79812c	// smlal  v12.4s, v9.4h, v25.4h
	WORD	$0x4e79812d	// smlal2 v13.4s, v9.8h, v25.8h

	ADD	$64, R9, R7
	VLD1	(R7), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e7a810a	// smlal  v10.4s, v8.4h, v26.4h
	WORD	$0x4e7a810b	// smlal2 v11.4s, v8.8h, v26.8h
	WORD	$0x0e7a812c	// smlal  v12.4s, v9.4h, v26.4h
	WORD	$0x4e7a812d	// smlal2 v13.4s, v9.8h, v26.8h

	ADD	$80, R9, R7
	VLD1	(R7), [V0.B16]
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x0e7b810a	// smlal  v10.4s, v8.4h, v27.4h
	WORD	$0x4e7b810b	// smlal2 v11.4s, v8.8h, v27.8h
	WORD	$0x0e7b812c	// smlal  v12.4s, v9.4h, v27.4h
	WORD	$0x4e7b812d	// smlal2 v13.4s, v9.8h, v27.8h

	WORD	$0x2f198d4e	// sqrshrun  v14.4h, v10.4s, #7
	WORD	$0x6f198d6e	// sqrshrun2 v14.8h, v11.4s, #7
	WORD	$0x2f198d8f	// sqrshrun  v15.4h, v12.4s, #7
	WORD	$0x6f198daf	// sqrshrun2 v15.8h, v13.4s, #7
	WORD	$0x2e2129c8	// sqxtun    v8.8b,  v14.8h
	WORD	$0x6e2129e8	// sqxtun2   v8.16b, v15.8h

	VST1	[V8.B16], (R0)

	ADD	R1, R0, R0
	ADD	$16, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, vert_loop

	RET
