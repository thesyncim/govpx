// ARMv8 NEON SAD primitives. Mirrors libvpx v1.16.0 vpx_dsp/arm/sad_neon.c
// (sad{4,8,16}xh_neon) plus a govpx-specific 16x16 limit-aware variant
// matching internal/vp8/dsp/sad.go's sadBlockLimit semantics.
//
//   sad = SUM_{y in [0,h), x in [0,w)} |src[y][x] - ref[y][x]|
//
// 16-wide kernels: per row, VLD1 16 src + 16 ref bytes, UABDL/UABDL2 widen
// the byte abs-diffs to int16, UADALP pairwise-accumulate into a 4-lane
// int32 accumulator. After h rows, VADDV reduces to a scalar.
//
// 8-wide kernels: per row, FMOVD 8 src + 8 ref bytes, UABDL widens to int16,
// SADALP into a 4-lane int32 accumulator. After h rows, VADDV reduces.
// (Could use UABAL into int16x8 but fold-into-i32 keeps headroom uniform.)
//
// 4-wide kernels: per row pair, FMOVS 4 bytes each into low halves; merge
// two rows into a single 8-byte src/ref via INS, then UABDL into int16x8.
// After h/2 row-pairs, VADDV.H8 reduces.
//
// The limit variant reduces and checks every row so the caller's
// best-so-far pruning still wins on bad candidates.
//
// UABDL/UABDL2/UADALP aren't natively in Go's arm64 assembler so they're
// emitted as raw WORD directives; encodings come from clang.

#include "textflag.h"

// sadBlock16x16NEON ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32

TEXT ·sadBlock16x16NEON(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$16, R6

loop16x16:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e217003	// uabdl2 v3.8h, v0.16b, v1.16b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	WORD	$0x6e606874	// uadalp v20.4s, v3.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop16x16

	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// sadBlock16x16LimitNEON ABI ($0-44):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   limit+32(FP)     int32
//   ret+40(FP)       int32

TEXT ·sadBlock16x16LimitNEON(SB), NOSPLIT, $0-44
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3
	MOVW	limit+32(FP), R4	// 32-bit signed limit

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$16, R6

limit_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e217003	// uabdl2 v3.8h, v0.16b, v1.16b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	WORD	$0x6e606874	// uadalp v20.4s, v3.8h

	// Reduce V20 to a scalar in V21 (preserve V20 for further accumulation).
	VADDV	V20.S4, V21
	VMOV	V21.S[0], R7
	CMPW	R4, R7
	BHI	limit_break

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, limit_loop

	// Done; return the final reduced value (in R7 from the last VMOV).
	MOVW	R7, ret+40(FP)
	RET

limit_break:
	MOVW	R7, ret+40(FP)
	RET

// sadBlock16x8NEON ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32

TEXT ·sadBlock16x8NEON(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$8, R6

loop16x8:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e217003	// uabdl2 v3.8h, v0.16b, v1.16b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	WORD	$0x6e606874	// uadalp v20.4s, v3.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop16x8

	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// sadBlock8x16NEON ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32
//
// Per row: FMOVD 8 src + 8 ref bytes into V0.8B/V1.8B (low halves of
// 128-bit V0/V1; high halves are don't-care). UABDL widens the 8 byte
// abs-diffs to V2.8H (int16). UADALP pairwise-accumulates into V20.4S.

TEXT ·sadBlock8x16NEON(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$16, R6

loop8x16:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop8x16

	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// sadBlock8x8NEON ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32

TEXT ·sadBlock8x8NEON(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$8, R6

loop8x8:
	FMOVD	(R0), F0
	FMOVD	(R2), F1
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop8x8

	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// sadBlock4x4NEON ABI ($0-36):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32
//
// Per row pair: FMOVS 4 bytes each from row y and row y+1 into V0.S[0],
// V0.S[1] (8 bytes total per side). After two row-pairs we have all 16
// bytes; UABDL widens 8-byte chunks to V2.8H, UADALP pairwise accumulates
// into V20.4S, and VADDV reduces.

TEXT ·sadBlock4x4NEON(SB), NOSPLIT, $0-36
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	VEOR	V20.B16, V20.B16, V20.B16

	// Row 0 src/ref into V0.S[0], V1.S[0].
	FMOVS	(R0), F0
	FMOVS	(R2), F1
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	// Row 1 src/ref into V0.S[1], V1.S[1] via INS (mov v0.s[1], v_tmp.s[0]).
	FMOVS	(R0), F4
	FMOVS	(R2), F5
	WORD	$0x6e0c0480	// mov v0.s[1], v4.s[0]
	WORD	$0x6e0c04a1	// mov v1.s[1], v5.s[0]

	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b (rows 0+1, 8 bytes)
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h

	ADD	R1, R0, R0
	ADD	R3, R2, R2

	// Row 2 src/ref into V0.S[0], V1.S[0].
	FMOVS	(R0), F0
	FMOVS	(R2), F1
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	// Row 3 src/ref into V0.S[1], V1.S[1].
	FMOVS	(R0), F4
	FMOVS	(R2), F5
	WORD	$0x6e0c0480	// mov v0.s[1], v4.s[0]
	WORD	$0x6e0c04a1	// mov v1.s[1], v5.s[0]

	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b (rows 2+3, 8 bytes)
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h

	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET
