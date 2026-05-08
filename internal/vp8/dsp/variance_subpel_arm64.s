// ARMv8 NEON bilinear filter kernels for subpel variance widths 8
// and 4. Mirrors libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c.
//
// First-pass (per row):
//   for x in [0, w): v = src[x] * f0 + src[x+1] * f1
//                    dst[x] = uint16((v + 64) >> 7)
// Second-pass (per row):
//   for x in [0, w): v = src[y*w+x] * f0 + src[(y+1)*w+x] * f1
//                    dst[x] = byte((v + 64) >> 7)
//
// Filter weights are uint16 in [0, 128]; products fit in u32. UMULL +
// UMLAL widen u16*u16 into u32 lanes; RSHRN narrows back to u16 with
// rounding shift right by 7. UQXTN saturates u16 -> u8 for second-pass
// output.
//
// UMULL/UMLAL/RSHRN/UQXTN aren't natively in Go's arm64 assembler so
// they're emitted as raw WORD directives; encodings come from clang.
//
// First-pass calling convention (ABI0, $0-48):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   dst+16(FP)       *uint16
//   height+24(FP)    int
//   f0+32(FP)        uint64 (only low 16 bits used)
//   f1+40(FP)        uint64 (only low 16 bits used)
//
// Second-pass calling convention (ABI0, $0-40):
//   src+0(FP)        *uint16
//   dst+8(FP)        *byte
//   height+16(FP)    int
//   f0+24(FP)        uint64 (only low 16 bits used)
//   f1+32(FP)        uint64 (only low 16 bits used)

#include "textflag.h"

// varFilterBlock2DBilinearFirstPass8NEON: 8 columns per row.
TEXT ·varFilterBlock2DBilinearFirstPass8NEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	height+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5

	VDUP	R4, V30.H8
	VDUP	R5, V31.H8

fp8_loop:
	// Load 16 bytes (need 9 but the buffer is stride-padded). V0.B16
	// holds bytes 0..15; tap0 = bytes 0..7, tap1 = bytes 1..8.
	VLD1	(R0), [V0.B16]

	// Tap 0: widen lo 8 bytes to uint16x8 in V8.H8.
	VUXTL	V0.B8, V8.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h
	WORD	$0x6e7ec10b	// umull2 v11.4s, v8.8h, v30.8h

	// Tap 1: VEXT $1 V0.B16 -> V2.B16 (bytes 1..16). Widen lo 8 to V8.H8.
	VEXT	$1, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h
	WORD	$0x6e7f810b	// umlal2 v11.4s, v8.8h, v31.8h

	// Round + shift right 7, narrow uint32 -> uint16: V14.8H.
	WORD	$0x0f198d4e	// rshrn  v14.4h, v10.4s, #7
	WORD	$0x4f198d6e	// rshrn2 v14.8h, v11.4s, #7

	// Store 8 uint16 lanes = 16 bytes.
	VST1	[V14.H8], (R2)

	ADD	R1, R0, R0
	ADD	$16, R2, R2
	SUB	$1, R3, R3
	CBNZ	R3, fp8_loop

	RET

// varFilterBlock2DBilinearFirstPass4NEON: 4 columns per row.
TEXT ·varFilterBlock2DBilinearFirstPass4NEON(SB), NOSPLIT, $0-48
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	dst+16(FP), R2
	MOVD	height+24(FP), R3
	MOVD	f0+32(FP), R4
	MOVD	f1+40(FP), R5

	VDUP	R4, V30.H4
	VDUP	R5, V31.H4

fp4_loop:
	// Load 8 bytes (need 5; buffer is stride-padded). V0 lo 8 bytes hold
	// bytes 0..7. Tap0 = bytes 0..3, tap1 = bytes 1..4.
	FMOVD	(R0), F0

	// Tap 0: widen lo 4 bytes to uint16 lanes in V8.H4 (lower half of V8).
	VUXTL	V0.B8, V8.H8
	WORD	$0x2e7ec10a	// umull  v10.4s, v8.4h, v30.4h

	// Tap 1: VEXT $1 V0.B16 -> V2 (bytes 1..). Widen lo 4 bytes.
	VEXT	$1, V0.B16, V0.B16, V2.B16
	VUXTL	V2.B8, V8.H8
	WORD	$0x2e7f810a	// umlal  v10.4s, v8.4h, v31.4h

	// Round + shift right 7, narrow uint32x4 -> uint16x4 in V14.4H.
	WORD	$0x0f198d4e	// rshrn  v14.4h, v10.4s, #7

	// Store 4 uint16 lanes = 8 bytes.
	FMOVD	F14, (R2)

	ADD	R1, R0, R0
	ADD	$8, R2, R2
	SUB	$1, R3, R3
	CBNZ	R3, fp4_loop

	RET

// varFilterBlock2DBilinearSecondPass8NEON: 8 columns per row, height
// rows. Per row: load 8 uint16 from row A, 8 uint16 from row B, weighted
// add via UMULL/UMLAL/RSHRN, saturate-narrow to uint8, store 8 bytes.
TEXT ·varFilterBlock2DBilinearSecondPass8NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	dst+8(FP), R1
	MOVD	height+16(FP), R2
	MOVD	f0+24(FP), R3
	MOVD	f1+32(FP), R4

	VDUP	R3, V30.H8
	VDUP	R4, V31.H8

sp8_loop:
	// Row A = 8 uint16 = 16 bytes at (R0).
	VLD1	(R0), [V0.H8]
	// Row B at R0 + 16.
	ADD	$16, R0, R5
	VLD1	(R5), [V2.H8]

	WORD	$0x2e7ec004	// umull  v4.4s, v0.4h, v30.4h
	WORD	$0x6e7ec005	// umull2 v5.4s, v0.8h, v30.8h
	WORD	$0x2e7f8044	// umlal  v4.4s, v2.4h, v31.4h
	WORD	$0x6e7f8045	// umlal2 v5.4s, v2.8h, v31.8h

	WORD	$0x0f198c88	// rshrn  v8.4h, v4.4s, #7
	WORD	$0x4f198ca8	// rshrn2 v8.8h, v5.4s, #7

	// Saturate-narrow uint16x8 -> uint8x8.
	WORD	$0x2e21490a	// uqxtn  v10.8b, v8.8h

	// Store 8 bytes.
	FMOVD	F10, (R1)

	ADD	$16, R0, R0
	ADD	$8, R1, R1
	SUB	$1, R2, R2
	CBNZ	R2, sp8_loop

	RET

// varFilterBlock2DBilinearSecondPass4NEON: 4 columns per row.
TEXT ·varFilterBlock2DBilinearSecondPass4NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	dst+8(FP), R1
	MOVD	height+16(FP), R2
	MOVD	f0+24(FP), R3
	MOVD	f1+32(FP), R4

	VDUP	R3, V30.H4
	VDUP	R4, V31.H4

sp4_loop:
	// Row A = 4 uint16 = 8 bytes at (R0).
	FMOVD	(R0), F0
	// Row B at R0 + 8.
	ADD	$8, R0, R5
	FMOVD	(R5), F2

	WORD	$0x2e7ec004	// umull  v4.4s, v0.4h, v30.4h
	WORD	$0x2e7f8044	// umlal  v4.4s, v2.4h, v31.4h

	WORD	$0x0f198c88	// rshrn  v8.4h, v4.4s, #7

	// Saturate-narrow uint16x4 -> uint8x4 (V10 low 4 bytes, but UQXTN
	// requires .8b form so we feed it 8 lanes; only low 4 matter).
	// Pad upper 4 lanes of V8 with zero by clearing V8 high half via
	// "ins v8.d[1], xzr"-equivalent trick; simpler: just use UQXTN .8b
	// since rshrn already left upper 4H lanes undefined—but we only
	// store 4 bytes so an undefined upper half is fine.
	WORD	$0x2e21490a	// uqxtn  v10.8b, v8.8h

	// Store 4 bytes.
	FMOVS	F10, (R1)

	ADD	$8, R0, R0
	ADD	$4, R1, R1
	SUB	$1, R2, R2
	CBNZ	R2, sp4_loop

	RET
