//go:build arm64 && !purego

// ARMv8 NEON 16-wide first-pass bilinear filter for subpel variance.
// Mirrors libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// var_filter_block2d_bilinear_w16:
//
//   for y in [0, height):
//     for x in [0, 16):
//       v = src[y*srcStride+x] * f0 + src[y*srcStride+x+1] * f1
//       dst[y*16+x] = uint16((v + 64) >> 7)
//
// Filter values are uint16 in [0, 128] (the eight bilinear pairs sum
// to 128). Multiplication uses UMULL/UMLAL into 32-bit lanes, then
// RSHRN narrows back to uint16 with rounding shift right by 7.
// UMULL/UMLAL/RSHRN aren't in Go's arm64 assembler so they're emitted
// as raw WORD directives; encodings come from clang.
//
// Calling convention (ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *uint16
//   height+24(FP)    int
//   f0+32(FP)        uint64 (only low 16 bits used)
//   f1+40(FP)        uint64 (only low 16 bits used)
//
// Registers:
//   R0=src R1=srcStride R2=dst R3=height R4=f0 R5=f1
//   V0,V1   loaded src bytes
//   V2      VEXT shifted-by-1 tap
//   V8,V9   widened uint8 -> uint16 (8 lanes each)
//   V10..V13  uint32x4 accumulators
//   V14,V15 narrowed uint16 outputs (8 lanes each)
//   V30     f0 broadcast (8 lanes uint16)
//   V31     f1 broadcast

#include "textflag.h"

TEXT ·varFilterBlock2DBilinearFirstPass16NEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	height+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8

loop:
	// Load 32 bytes (need 17 but 32 is fine inside a stride-paddded buffer).
	VLD1	(R0), [V0.B16, V1.B16]

	// Tap 0: V_t = V0
	VUXTL	V0.B8, V8.H8
	VUXTL2	V0.B16, V9.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h
	WORD	$0x2e7ec12c	// umull  v12.4s, v9.4h, v30.4h
	WORD	$0x6e7ec12d	// umull2 v13.4s, v9.8h, v30.8h

	// Tap 1: V_t = bytes 1..16 = VEXT $1, V0, V1
	VEXT	$1, V1.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	VUXTL2	V2.B16, V9.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h
	WORD	$0x2e7f812c	// umlal  v12.4s, v9.4h, v31.4h
	WORD	$0x6e7f812d	// umlal2 v13.4s, v9.8h, v31.8h

	// Round + shift right 7, narrow uint32 -> uint16.
	WORD	$0x0f198d4e	// rshrn  v14.4h, v10.4s, #7
	WORD	$0x4f198d6e	// rshrn2 v14.8h, v11.4s, #7
	WORD	$0x0f198d8f	// rshrn  v15.4h, v12.4s, #7
	WORD	$0x4f198daf	// rshrn2 v15.8h, v13.4s, #7

	// Store 16 uint16 lanes = 32 bytes.
	VST1	[V14.H8, V15.H8], (R2)

	// Advance: src += srcStride, dst += 32 bytes (16 uint16).
	ADD	R1, R0, R0
	ADD	$32, R2, R2
	SUB	$1, R3, R3
	CBNZ	R3, loop

	RET
