// ARMv8 NEON 16x16 SAD primitives. Mirrors libvpx v1.16.0
// vpx_dsp/arm/sad_neon.c vpx_sad16x16_neon and vpx_sad16x16_limit:
//
//   sad = SUM_{y in [0,16), x in [0,16)} |src[y][x] - ref[y][x]|
//
// Per row: VLD1 16 src + 16 ref bytes, UABDL/UABDL2 to widen the
// byte abs-diffs to int16, UADALP to pairwise-accumulate into a
// 4-lane int32 accumulator. After 16 rows, VADDV reduces to scalar.
// The limit variant reduces and checks every row so the caller's
// best-so-far pruning still wins on bad candidates.
//
// UABDL/UADALP aren't natively in Go's arm64 assembler so they're
// emitted as raw WORD directives; encodings come from clang.
//
// sadBlock16x16NEON ABI ($0-32):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   ret+32(FP)       int32 (return value)

#include "textflag.h"

TEXT ·sadBlock16x16NEON(SB), NOSPLIT, $0-40
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	ref+16(FP), R2
	MOVD	refStride+24(FP), R3

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$16, R6

loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V1.B16]
	WORD	$0x2e217002	// uabdl  v2.8h, v0.8b, v1.8b
	WORD	$0x6e217003	// uabdl2 v3.8h, v0.16b, v1.16b
	WORD	$0x6e606854	// uadalp v20.4s, v2.8h
	WORD	$0x6e606874	// uadalp v20.4s, v3.8h
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R6, R6
	CBNZ	R6, loop

	VADDV	V20.S4, V20
	FMOVS	F20, ret+32(FP)
	RET

// sadBlock16x16LimitNEON ABI ($0-40):
//   src+0(FP)        *byte
//   srcStride+8(FP)  int
//   ref+16(FP)       *byte
//   refStride+24(FP) int
//   limit+32(FP)     int32
//   ret+40(FP)       int32

TEXT ·sadBlock16x16LimitNEON(SB), NOSPLIT, $0-48
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
