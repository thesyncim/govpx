//go:build arm64 && !purego

// ARMv8 NEON 4x4 six-tap subpel prediction. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c sixtap_filter4d_neon's
// w4 path. Two passes:
//
//   horizontal: for y in [0, 9), x in [0, 4):
//     v = sum_{k=0..5} src[y*srcStride+x+k] * hFilter[k] + 64
//     tmp[y*4+x] = clip255(v >> 7)
//
//   vertical: for y in [0, 4), x in [0, 4):
//     v = sum_{k=0..5} tmp[(y+k)*4+x] * vFilter[k] + 64
//     dst[y*dstStride+x] = clip255(v >> 7)
//
// Same SMULL/SMLAL accumulation strategy as the 8x8/16x16 paths but
// only the low 4 lanes are kept. Only V10.4S (and not V11) is used
// per row. Horizontal loads pull a single 16-byte chunk (we only
// need bytes [0..8] but a 16-byte load amortises better than smaller
// loads, and VEXT $imm, V0, V0 is fine because the bottom 4 lanes
// of the rotated vector still come from bytes [imm..imm+3] which
// live in V0). The 4-byte horizontal stores use FMOVS on V8.S[0]
// (after SQXTUN narrows to V8.8B). The vertical pass loads 4 bytes
// per row via FMOVS into V0.S[0], then VUXTL widens — the upper 4
// lanes contain garbage but are unused.
//
// Plan9 VEXT operand order: VEXT $imm, V_a, V_b, V_d encodes
// V_a -> Rm (high) and V_b -> Rn (low). For "bytes [imm..imm+7] of
// (V0:V1)" the form is VEXT $imm, V1, V0, V_d.
//
// Calling convention (ABI0, $0-56):
//   dst+0(FP)        *byte
//   dstStride+8(FP)  int
//   src+16(FP)       *byte
//   srcStride+24(FP) int
//   hFilter+32(FP)   *[6]int16
//   vFilter+40(FP)   *[6]int16
//   tmp+48(FP)       *[9*4]byte
//
// Registers:
//   R0=dst R1=dstStride R2=src R3=srcStride R4=hFilter R5=vFilter
//   R6=tmp R7,R8,R9=loop scratch
//   V0,V1   src bytes (horizontal); per-row tmp slot (vertical)
//   V2      VEXT tap window (horizontal)
//   V8      widened uint8 -> int16 lanes
//   V10     int32x4 accumulator (low 4 lanes only)
//   V14     saturate intermediate
//   V16..V21 hFilter[0..5] broadcasts
//   V22..V27 vFilter[0..5] broadcasts

#include "textflag.h"

TEXT ·sixTapPredict4x4NEON(SB), NOSPLIT, $0-56
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
	VLD1	(R2), [V0.B16]

	// Tap 0: bytes [0..3] (low 4 of V0)
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e70c10a	// smull  v10.4s, v8.4h, v16.4h

	// Tap 1: bytes [1..4]. VEXT against V0:V0 — the bottom 4
	// lanes are bytes [1..4] of V0, which is what we need.
	VEXT	$1, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e71810a	// smlal  v10.4s, v8.4h, v17.4h

	// Tap 2: bytes [2..5]
	VEXT	$2, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e72810a	// smlal  v10.4s, v8.4h, v18.4h

	// Tap 3: bytes [3..6]
	VEXT	$3, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e73810a	// smlal  v10.4s, v8.4h, v19.4h

	// Tap 4: bytes [4..7]
	VEXT	$4, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e74810a	// smlal  v10.4s, v8.4h, v20.4h

	// Tap 5: bytes [5..8]
	VEXT	$5, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x0e75810a	// smlal  v10.4s, v8.4h, v21.4h

	WORD	$0x2f198d4e	// sqrshrun  v14.4h, v10.4s, #7
	WORD	$0x2e2129c8	// sqxtun    v8.8b,  v14.8h (low 4 valid)

	FMOVS	F8, (R9)

	ADD	R3, R2, R2
	ADD	$4, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, horiz_loop

	// === Vertical pass: 4 rows ===
	MOVD	$4, R8
	MOVD	R6, R9

vert_loop:
	// Tap 0
	FMOVS	(R9), F0
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e76c10a	// smull  v10.4s, v8.4h, v22.4h

	ADD	$4, R9, R7
	FMOVS	(R7), F0
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e77810a	// smlal  v10.4s, v8.4h, v23.4h

	ADD	$8, R9, R7
	FMOVS	(R7), F0
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e78810a	// smlal  v10.4s, v8.4h, v24.4h

	ADD	$12, R9, R7
	FMOVS	(R7), F0
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e79810a	// smlal  v10.4s, v8.4h, v25.4h

	ADD	$16, R9, R7
	FMOVS	(R7), F0
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e7a810a	// smlal  v10.4s, v8.4h, v26.4h

	ADD	$20, R9, R7
	FMOVS	(R7), F0
	VUXTL	V0.B8, V8.H8
	WORD	$0x0e7b810a	// smlal  v10.4s, v8.4h, v27.4h

	WORD	$0x2f198d4e	// sqrshrun  v14.4h, v10.4s, #7
	WORD	$0x2e2129c8	// sqxtun    v8.8b,  v14.8h

	FMOVS	F8, (R0)

	ADD	R1, R0, R0
	ADD	$4, R9, R9
	SUB	$1, R8, R8
	CBNZ	R8, vert_loop

	RET
