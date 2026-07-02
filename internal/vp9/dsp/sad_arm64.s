//go:build arm64 && !purego

// ARMv8 NEON SAD primitives for VP9. Ports libvpx v1.16.0
// vpx_dsp/arm/sad_neon.c (base kernels) and
// vpx_dsp/arm/sad_neon_dotprod.c (FEAT_DotProd kernels, dispatched when
// internal/cpu.HasARM64DotProd is set). All kernels return uint32 matching
// the vpx_sad{W}x{H}_c contract.
//
// Base kernels follow the upstream accumulator scheme:
//
//   sad16xNNEON  sad16xh_neon:  per row UABD.16B + UADALP into one u16x8
//                accumulator (2*255*64 max per lane fits u16).
//   sad32xNNEON  sad32xh_neon:  per row two UABD.16B abs-diff vectors are
//                UADDLP-widened to u16 and UADALP-folded into a u32x4.
//   sad64xNNEON  sad64xh_neon:  four u16x8 accumulators, one per 16-byte
//                chunk, merged through UADDLP/UADALP into u32 at the end.
//   sad8xNNEON   sad8xh_neon:   UABAL.8B into a u16x8 accumulator.
//   sad4xNNEON   sad4xh_neon:   two rows packed per 8-byte vector, UABAL.
//
// Dotprod kernels mirror sad_neon_dotprod.c: UABD.16B + UDOT against an
// all-ones vector into two u32x4 accumulators (the upstream-documented
// optimum for 2- and 4-pipe NEON cores).
//
// UABD/UADALP/UADDLP/UABAL/UDOT/MOVI are emitted as raw WORD directives.

#include "textflag.h"

// sad16xNNEON ABI ($0-44):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   rows+32(FP)      int
//   ret+40(FP)       uint32

TEXT ·sad16xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	WORD	$0x6f00e414	// movi v20.2d, #0

loop16xN:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd   v2.16b, v0.16b, v1.16b
	WORD	$0x6e206854	// uadalp v20.8h, v2.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop16xN

	WORD	$0x6e703a94	// uaddlv s20, v20.8h
	FMOVS	F20, ret+40(FP)
	RET

// sad32xNNEON ABI ($0-44): 32 bytes per row (libvpx sad32xh_neon).
TEXT ·sad32xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	WORD	$0x6f00e414	// movi v20.2d, #0

loop32xN:
	WORD	$0x3dc00000	// ldr q0, [x0]
	WORD	$0x3dc00041	// ldr q1, [x2]
	WORD	$0x6e217402	// uabd   v2.16b, v0.16b, v1.16b
	WORD	$0x6e202844	// uaddlp v4.8h, v2.16b
	WORD	$0x3dc00405	// ldr q5, [x0, #16]
	WORD	$0x3dc00446	// ldr q6, [x2, #16]
	WORD	$0x6e2674a7	// uabd   v7.16b, v5.16b, v6.16b
	WORD	$0x6e2028f0	// uaddlp v16.8h, v7.16b
	WORD	$0x6e606894	// uadalp v20.4s, v4.8h
	WORD	$0x6e606a14	// uadalp v20.4s, v16.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop32xN

	VADDV	V20.S4, V20
	FMOVS	F20, ret+40(FP)
	RET

// sad64xNNEON ABI ($0-44): 64 bytes per row (libvpx sad64xh_neon).
TEXT ·sad64xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	WORD	$0x6f00e414	// movi v20.2d, #0
	WORD	$0x6f00e415	// movi v21.2d, #0
	WORD	$0x6f00e416	// movi v22.2d, #0
	WORD	$0x6f00e417	// movi v23.2d, #0

loop64xN:
	WORD	$0x3dc00000	// ldr q0, [x0]
	WORD	$0x3dc00041	// ldr q1, [x2]
	WORD	$0x6e217404	// uabd   v4.16b, v0.16b, v1.16b
	WORD	$0x6e206894	// uadalp v20.8h, v4.16b
	WORD	$0x3dc00402	// ldr q2, [x0, #16]
	WORD	$0x3dc00443	// ldr q3, [x2, #16]
	WORD	$0x6e237445	// uabd   v5.16b, v2.16b, v3.16b
	WORD	$0x6e2068b5	// uadalp v21.8h, v5.16b
	WORD	$0x3dc00800	// ldr q0, [x0, #32]
	WORD	$0x3dc00841	// ldr q1, [x2, #32]
	WORD	$0x6e217406	// uabd   v6.16b, v0.16b, v1.16b
	WORD	$0x6e2068d6	// uadalp v22.8h, v6.16b
	WORD	$0x3dc00c02	// ldr q2, [x0, #48]
	WORD	$0x3dc00c43	// ldr q3, [x2, #48]
	WORD	$0x6e237447	// uabd   v7.16b, v2.16b, v3.16b
	WORD	$0x6e2068f7	// uadalp v23.8h, v7.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop64xN

	WORD	$0x6e602a98	// uaddlp v24.4s, v20.8h
	WORD	$0x6e606ab8	// uadalp v24.4s, v21.8h
	WORD	$0x6e606ad8	// uadalp v24.4s, v22.8h
	WORD	$0x6e606af8	// uadalp v24.4s, v23.8h
	VADDV	V24.S4, V24
	FMOVS	F24, ret+40(FP)
	RET

// sadDot16xNNEON ABI ($0-44): 16 bytes per row, rows even
// (libvpx sad16xh_neon_dotprod). Requires FEAT_DotProd.
TEXT ·sadDot16xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	WORD	$0x6f00e414	// movi v20.2d, #0
	WORD	$0x6f00e415	// movi v21.2d, #0
	WORD	$0x4f00e43f	// movi v31.16b, #1

loopDot16xN:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9454	// udot v20.4s, v2.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	VLD1	(R0), [V3.B16]
	VLD1	(R2), [V4.B16]
	WORD	$0x6e247465	// uabd v5.16b, v3.16b, v4.16b
	WORD	$0x6e9f94b5	// udot v21.4s, v5.16b, v31.16b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$2, R6, R6
	CBNZ	R6, loopDot16xN

	WORD	$0x4eb58694	// add v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	FMOVS	F20, ret+40(FP)
	RET

// sadDotWideNEON ABI ($0-52): groups of 32 bytes per row
// (libvpx sadwxh_neon_dotprod; groups = width/32). Requires FEAT_DotProd.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   rows+32(FP)      int
//   groups+40(FP)    int   (width / 32)
//   ret+48(FP)       uint32
TEXT ·sadDotWideNEON(SB), NOSPLIT, $0-52
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6
	MOVD	groups+40(FP), R7

	WORD	$0x6f00e414	// movi v20.2d, #0
	WORD	$0x6f00e415	// movi v21.2d, #0
	WORD	$0x4f00e43f	// movi v31.16b, #1

rowLoopDotWide:
	MOVD	R7, R8
	MOVD	R0, R9
	MOVD	R2, R10

groupLoopDotWide:
	WORD	$0x3dc00120	// ldr q0, [x9]
	WORD	$0x3dc00141	// ldr q1, [x10]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9454	// udot v20.4s, v2.16b, v31.16b
	WORD	$0x3dc00523	// ldr q3, [x9, #16]
	WORD	$0x3dc00544	// ldr q4, [x10, #16]
	WORD	$0x6e247465	// uabd v5.16b, v3.16b, v4.16b
	WORD	$0x6e9f94b5	// udot v21.4s, v5.16b, v31.16b
	ADD	$32, R9, R9
	ADD	$32, R10, R10
	SUB	$1, R8, R8
	CBNZ	R8, groupLoopDotWide

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, rowLoopDotWide

	WORD	$0x4eb58694	// add v20.4s, v20.4s, v21.4s
	VADDV	V20.S4, V20
	FMOVS	F20, ret+48(FP)
	RET

// sad16ChunksNEON ABI ($0-52): generic chunked fallback used by callers
// that need an arbitrary chunk count (kept for the 4D scalar fallback
// shape; the 32/64-wide single-ref dispatch uses the dedicated kernels).
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   rows+32(FP)      int
//   chunks+40(FP)    int   (width / 16)
//   ret+48(FP)       uint32

TEXT ·sad16ChunksNEON(SB), NOSPLIT, $0-52
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6
	MOVD	chunks+40(FP), R7

	WORD	$0x6f00e414	// movi v20.2d, #0

rowLoop16Chunks:
	MOVD	R7, R8
	MOVD	R0, R9
	MOVD	R2, R10

chunkLoop16Chunks:
	VLD1	(R9), [V0.B16]
	VLD1	(R10), [V1.B16]
	WORD	$0x6e217402	// uabd   v2.16b, v0.16b, v1.16b
	WORD	$0x6e202844	// uaddlp v4.8h, v2.16b
	WORD	$0x6e606894	// uadalp v20.4s, v4.8h
	ADD	$16, R9, R9
	ADD	$16, R10, R10
	SUB	$1, R8, R8
	CBNZ	R8, chunkLoop16Chunks

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, rowLoop16Chunks

	VADDV	V20.S4, V20
	FMOVS	F20, ret+48(FP)
	RET

// sadDot4DNEON ABI ($0-80): 4-reference chunked SAD
// (libvpx sad_4d_neon_dotprod.c shape). Requires FEAT_DotProd.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref0+16(FP)      *byte
//   ref1+24(FP)      *byte
//   ref2+32(FP)      *byte
//   ref3+40(FP)      *byte
//   refStride+48(FP) int
//   rows+56(FP)      int
//   chunks+64(FP)    int   (width / 16)
//   out+72(FP)       *[4]uint32
TEXT ·sadDot4DNEON(SB), NOSPLIT, $0-80
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref0+16(FP), R2
	MOVD	ref1+24(FP), R3
	MOVD	ref2+32(FP), R4
	MOVD	ref3+40(FP), R5
	MOVD	refStride+48(FP), R6
	MOVD	rows+56(FP), R7
	MOVD	chunks+64(FP), R8

	WORD	$0x6f00e414	// movi v20.2d, #0
	WORD	$0x6f00e415	// movi v21.2d, #0
	WORD	$0x6f00e416	// movi v22.2d, #0
	WORD	$0x6f00e417	// movi v23.2d, #0
	WORD	$0x4f00e43f	// movi v31.16b, #1

rowLoopDot4D:
	MOVD	R8, R9
	MOVD	R0, R10
	MOVD	R2, R11
	MOVD	R3, R12
	MOVD	R4, R13
	MOVD	R5, R14

chunkLoopDot4D:
	VLD1	(R10), [V0.B16]

	VLD1	(R11), [V1.B16]
	WORD	$0x6e217402	// uabd v2.16b, v0.16b, v1.16b
	WORD	$0x6e9f9454	// udot v20.4s, v2.16b, v31.16b

	VLD1	(R12), [V4.B16]
	WORD	$0x6e247405	// uabd v5.16b, v0.16b, v4.16b
	WORD	$0x6e9f94b5	// udot v21.4s, v5.16b, v31.16b

	VLD1	(R13), [V6.B16]
	WORD	$0x6e267407	// uabd v7.16b, v0.16b, v6.16b
	WORD	$0x6e9f94f6	// udot v22.4s, v7.16b, v31.16b

	VLD1	(R14), [V16.B16]
	WORD	$0x6e307411	// uabd v17.16b, v0.16b, v16.16b
	WORD	$0x6e9f9637	// udot v23.4s, v17.16b, v31.16b

	ADD	$16, R10, R10
	ADD	$16, R11, R11
	ADD	$16, R12, R12
	ADD	$16, R13, R13
	ADD	$16, R14, R14
	SUB	$1, R9, R9
	CBNZ	R9, chunkLoopDot4D

	ADD	R1, R0, R0
	ADD	R6, R2, R2
	ADD	R6, R3, R3
	ADD	R6, R4, R4
	ADD	R6, R5, R5
	SUB	$1, R7, R7
	CBNZ	R7, rowLoopDot4D

	MOVD	out+72(FP), R7
	VADDV	V20.S4, V20
	VADDV	V21.S4, V21
	VADDV	V22.S4, V22
	VADDV	V23.S4, V23
	VMOV	V20.S[0], R8
	VMOV	V21.S[0], R9
	VMOV	V22.S[0], R10
	VMOV	V23.S[0], R11
	MOVW	R8, 0(R7)
	MOVW	R9, 4(R7)
	MOVW	R10, 8(R7)
	MOVW	R11, 12(R7)
	RET

// sad16Chunksx4NEON ABI ($0-80): base (non-dotprod) 4-reference chunked
// SAD. Per-ref accumulation follows the sad32xh_neon scheme: UABD.16B
// abs-diffs are UADDLP-widened to u16 then UADALP-folded into a u32x4
// accumulator per reference, so any chunks*rows combination is safe.
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref0+16(FP)      *byte
//   ref1+24(FP)      *byte
//   ref2+32(FP)      *byte
//   ref3+40(FP)      *byte
//   refStride+48(FP) int
//   rows+56(FP)      int
//   chunks+64(FP)    int   (width / 16)
//   out+72(FP)       *[4]uint32

TEXT ·sad16Chunksx4NEON(SB), NOSPLIT, $0-80
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref0+16(FP), R2
	MOVD	ref1+24(FP), R3
	MOVD	ref2+32(FP), R4
	MOVD	ref3+40(FP), R5
	MOVD	refStride+48(FP), R6
	MOVD	rows+56(FP), R7
	MOVD	chunks+64(FP), R8

	WORD	$0x6f00e414	// movi v20.2d, #0
	WORD	$0x6f00e415	// movi v21.2d, #0
	WORD	$0x6f00e416	// movi v22.2d, #0
	WORD	$0x6f00e417	// movi v23.2d, #0

rowLoop16Chunksx4:
	MOVD	R8, R9
	MOVD	R0, R10
	MOVD	R2, R11
	MOVD	R3, R12
	MOVD	R4, R13
	MOVD	R5, R14

chunkLoop16Chunksx4:
	VLD1	(R10), [V0.B16]

	VLD1	(R11), [V1.B16]
	WORD	$0x6e217402	// uabd   v2.16b, v0.16b, v1.16b
	WORD	$0x6e202844	// uaddlp v4.8h, v2.16b
	WORD	$0x6e606894	// uadalp v20.4s, v4.8h

	VLD1	(R12), [V5.B16]
	WORD	$0x6e257406	// uabd   v6.16b, v0.16b, v5.16b
	WORD	$0x6e2028c7	// uaddlp v7.8h, v6.16b
	WORD	$0x6e6068f5	// uadalp v21.4s, v7.8h

	VLD1	(R13), [V16.B16]
	WORD	$0x6e307411	// uabd   v17.16b, v0.16b, v16.16b
	WORD	$0x6e202a32	// uaddlp v18.8h, v17.16b
	WORD	$0x6e606a56	// uadalp v22.4s, v18.8h

	VLD1	(R14), [V24.B16]
	WORD	$0x6e387419	// uabd   v25.16b, v0.16b, v24.16b
	WORD	$0x6e202b3a	// uaddlp v26.8h, v25.16b
	WORD	$0x6e606b57	// uadalp v23.4s, v26.8h

	ADD	$16, R10, R10
	ADD	$16, R11, R11
	ADD	$16, R12, R12
	ADD	$16, R13, R13
	ADD	$16, R14, R14
	SUB	$1, R9, R9
	CBNZ	R9, chunkLoop16Chunksx4

	ADD	R1, R0, R0
	ADD	R6, R2, R2
	ADD	R6, R3, R3
	ADD	R6, R4, R4
	ADD	R6, R5, R5
	SUB	$1, R7, R7
	CBNZ	R7, rowLoop16Chunksx4

	MOVD	out+72(FP), R7
	VADDV	V20.S4, V20
	VADDV	V21.S4, V21
	VADDV	V22.S4, V22
	VADDV	V23.S4, V23
	VMOV	V20.S[0], R8
	VMOV	V21.S[0], R9
	VMOV	V22.S[0], R10
	VMOV	V23.S[0], R11
	MOVW	R8, 0(R7)
	MOVW	R9, 4(R7)
	MOVW	R10, 8(R7)
	MOVW	R11, 12(R7)
	RET

// sad8xNNEON ABI ($0-44): 8 bytes per row (libvpx sad8xh_neon, UABAL).
TEXT ·sad8xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	WORD	$0x6f00e414	// movi v20.2d, #0

loop8xN:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x2e215014	// uabal v20.8h, v0.8b, v1.8b
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop8xN

	WORD	$0x6e703a94	// uaddlv s20, v20.8h
	FMOVS	F20, ret+40(FP)
	RET

// sad4xNNEON ABI ($0-44): 4 bytes per row, row count must be even
// (libvpx sad4xh_neon: two rows packed per 8-byte vector, UABAL).

TEXT ·sad4xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	WORD	$0x6f00e414	// movi v20.2d, #0

	// Rows are processed two at a time; R6 / 2 iterations.
	LSR	$1, R6, R6

loop4xN:
	// Row y src/ref into V0.S[0], V1.S[0].
	FMOVS	(R0), F0
	FMOVS	(R2), F1
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	// Row y+1 src/ref into V4.S[0], V5.S[0].
	FMOVS	(R0), F4
	FMOVS	(R2), F5
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	// V0.S[1] = V4.S[0]; V1.S[1] = V5.S[0].
	WORD	$0x6e0c0480	// mov v0.s[1], v4.s[0]
	WORD	$0x6e0c04a1	// mov v1.s[1], v5.s[0]

	WORD	$0x2e215014	// uabal v20.8h, v0.8b, v1.8b (rows y..y+1)

	SUB	$1, R6, R6
	CBNZ	R6, loop4xN

	WORD	$0x6e703a94	// uaddlv s20, v20.8h
	FMOVS	F20, ret+40(FP)
	RET
