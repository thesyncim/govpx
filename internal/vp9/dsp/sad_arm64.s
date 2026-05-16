//go:build arm64 && !purego

// ARMv8 NEON SAD primitives for VP9. Mirrors libvpx v1.16.0
// vpx_dsp/arm/sad_neon.c sad{4,8,16,32,64}xh_neon. All kernels return
// uint32 matching the VP9 vpx_sad{W}x{H}_c signature.
//
// 16-wide kernels: per row, VLD1 16 src + 16 ref bytes, UABDL/UABDL2
// widen the byte abs-diffs to 8x int16 each, UADALP pairwise-accumulates
// into a 4-lane int32 accumulator. After h rows, VADDV reduces to a
// scalar.
//
// 32/64-wide kernels share sad16ChunksNEON: per row we walk `chunks`
// 16-byte sub-blocks using the same UABDL/UADALP chain.
//
// 8-wide kernels: per row, FMOVD 8 src + 8 ref bytes into V0.8B/V1.8B
// (low halves of 128-bit V0/V1). UABDL widens to 8x int16; UADALP folds
// into a 4-lane int32 accumulator.
//
// 4-wide kernel: per row pair, FMOVS 4 bytes from row y and row y+1 into
// V0.S[0]/V0.S[1] (via INS), giving 8 bytes per side. UABDL widens to
// 8x int16; UADALP folds into the int32 accumulator. Rows must be even.
//
// UABDL/UABDL2/UADALP aren't natively in Go's arm64 assembler so they're
// emitted as raw WORD directives; encodings come from clang.

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

	VEOR	V20.B16, V20.B16, V20.B16

loop16xN:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b,  v1.8b
	WORD	$0x6e217003	// uabdl2 v3.8h, v0.16b, v1.16b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	WORD	$0x6e606874	// uadalp v20.4s, v3.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop16xN

	VADDV	V20.S4, V20
	FMOVS	F20, ret+40(FP)
	RET

// sad16ChunksNEON ABI ($0-52):
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

	VEOR	V20.B16, V20.B16, V20.B16

rowLoop16Chunks:
	MOVD	R7, R8
	MOVD	R0, R9
	MOVD	R2, R10

chunkLoop16Chunks:
	VLD1	(R9), [V0.B16]
	VLD1	(R10), [V1.B16]
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b,  v1.8b
	WORD	$0x6e217003	// uabdl2 v3.8h, v0.16b, v1.16b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	WORD	$0x6e606874	// uadalp v20.4s, v3.8h
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

// sad8xNNEON ABI ($0-44): 8 bytes per row, row count supplied by caller.
TEXT ·sad8xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	VEOR	V20.B16, V20.B16, V20.B16

loop8xN:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop8xN

	VADDV	V20.S4, V20
	FMOVS	F20, ret+40(FP)
	RET

// sad4xNNEON ABI ($0-44): 4 bytes per row, row count must be even.
//
// Per row pair: FMOVS 4 bytes from row y and row y+1 into V0.S[0],
// V0.S[1] (8 bytes total per side). UABDL widens the 8-byte abs-diff
// vector to V2.8H; UADALP pairwise-accumulates into V20.4S.

TEXT ·sad4xNNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVD	rows+32(FP), R6

	VEOR	V20.B16, V20.B16, V20.B16

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

	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b  (rows y..y+1, 8 bytes)
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h

	SUB	$1, R6, R6
	CBNZ	R6, loop4xN

	VADDV	V20.S4, V20
	FMOVS	F20, ret+40(FP)
	RET
