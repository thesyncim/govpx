// ARMv8 NEON implementation of the 16-wide second-pass bilinear
// filter for subpel variance. Mirrors the math in
// varFilterBlock2DBilinearSecondPass16Scalar:
//
//   for y in [0, height):
//     for x in [0, 16):
//       v = src[y*16+x]   * f0
//         + src[(y+1)*16+x] * f1
//       dst[y*16+x] = byte((v + 64) >> 7)
//
// The Go arm64 assembler only natively knows ~39 SIMD opcodes; the
// integer multiply-long, rounding-shift-narrow, and saturate-narrow
// forms used here aren't on that list, so they're emitted as raw
// WORD directives. The encodings were obtained by assembling the
// equivalent native ARMv8 mnemonics with clang and reading off the
// resulting object bytes.
//
// Calling convention (ABI0 / stack-passed args, $0-40 frame):
//   src+0(FP)    *[17*16]uint16
//   dst+8(FP)    *byte
//   height+16(FP) int
//   f0+24(FP)    uint64 (only low 16 bits used)
//   f1+32(FP)    uint64 (only low 16 bits used)
//
// Register usage:
//   R0 src ptr; R1 dst ptr; R2 height counter; R3 f0; R4 f1; R5 scratch
//   V0,V1  src row A (16 uint16)
//   V2,V3  src row B (16 uint16)
//   V4..V7 multiply-long accumulators (4x uint32 each)
//   V8,V9  rounded-shifted-narrowed (8x uint16 each)
//   V10    final saturated-narrowed dst (16x uint8)
//   V30    f0 broadcast (8x uint16)
//   V31    f1 broadcast (8x uint16)

#include "textflag.h"

TEXT ·varFilterBlock2DBilinearSecondPass16NEON(SB), NOSPLIT, $0-40
	MOVD src+0(FP), R0
	MOVD dst+8(FP), R1
	MOVD height+16(FP), R2
	MOVD f0+24(FP), R3
	MOVD f1+32(FP), R4

	VDUP	R3, V30.H8
	VDUP	R4, V31.H8

loop:
	// Load 16 uint16 from src row A (R0): V0 = lanes 0..7, V1 = 8..15.
	VLD1	(R0), [V0.H8, V1.H8]
	// Load 16 uint16 from src row B (R0 + 32 bytes): V2 + V3.
	ADD	$32, R0, R5
	VLD1	(R5), [V2.H8, V3.H8]

	// V4 = V0[0..3] * V30[0..3]                 // umull v4.4s, v0.4h, v30.4h
	WORD	$0x2e7ec004
	// V5 = V0[4..7] * V30[4..7]                 // umull2 v5.4s, v0.8h, v30.8h
	WORD	$0x6e7ec005
	// V4 += V2[0..3] * V31[0..3]                // umlal v4.4s, v2.4h, v31.4h
	WORD	$0x2e7f8044
	// V5 += V2[4..7] * V31[4..7]                // umlal2 v5.4s, v2.8h, v31.8h
	WORD	$0x6e7f8045

	// V6 = V1[0..3] * V30[0..3]                 // umull v6.4s, v1.4h, v30.4h
	WORD	$0x2e7ec026
	// V7 = V1[4..7] * V30[4..7]                 // umull2 v7.4s, v1.8h, v30.8h
	WORD	$0x6e7ec027
	// V6 += V3[0..3] * V31[0..3]                // umlal v6.4s, v3.4h, v31.4h
	WORD	$0x2e7f8066
	// V7 += V3[4..7] * V31[4..7]                // umlal2 v7.4s, v3.8h, v31.8h
	WORD	$0x6e7f8067

	// Rounding shift right narrow by 7: uint32 lanes -> uint16 lanes
	// (rounding adds 1<<6 = 64 before the >>7).
	WORD	$0x0f198c88	// rshrn  v8.4h, v4.4s, #7
	WORD	$0x4f198ca8	// rshrn2 v8.8h, v5.4s, #7
	WORD	$0x0f198cc9	// rshrn  v9.4h, v6.4s, #7
	WORD	$0x4f198ce9	// rshrn2 v9.8h, v7.4s, #7

	// Saturate-narrow uint16 lanes -> uint8 lanes.
	WORD	$0x2e21490a	// uqxtn  v10.8b, v8.8h
	WORD	$0x6e21492a	// uqxtn2 v10.16b, v9.8h

	// Store 16 bytes to dst.
	VST1	[V10.B16], (R1)

	// Advance: src += 32 bytes (16 uint16); dst += 16 bytes.
	ADD	$32, R0, R0
	ADD	$16, R1, R1
	SUB	$1, R2, R2
	CBNZ	R2, loop

	RET
