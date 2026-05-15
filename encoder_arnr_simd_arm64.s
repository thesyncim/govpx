//go:build arm64 && !purego

// NEON temporal-filter apply kernel for VP8 ARNR. libvpx has only an x86 SSE2
// specialization for this primitive; this is a govpx arm64 counterpart that
// preserves the scalar math exactly for strengths 0..6.

#include "textflag.h"

#define TEMPORAL_FILTER_NEON_LOW \
	WORD	$0x6e6d348f; /* cmhi  v15.8h, v4.8h, v13.8h */ \
	WORD	$0x4e649c84; /* mul   v4.8h,  v4.8h, v4.8h */ \
	WORD	$0x4e699c84; /* mul   v4.8h,  v4.8h, v9.8h */ \
	WORD	$0x4e6b8484; /* add   v4.8h,  v4.8h, v11.8h */ \
	WORD	$0x6e6e4484; /* ushl  v4.8h,  v4.8h, v14.8h */ \
	WORD	$0x6e641d4f; /* bsl   v15.16b, v10.16b, v4.16b */ \
	WORD	$0x6e6f854f; /* sub   v15.8h, v10.8h, v15.8h */ \
	WORD	$0x4e6c9def; /* mul   v15.8h, v15.8h, v12.8h */ \
	VLD1	(R12), [V20.B16]; \
	WORD	$0x2e6f1294; /* uaddw  v20.4s, v20.4s, v15.4h */ \
	VST1	[V20.B16], (R12); \
	ADD	$16, R12, R13; \
	VLD1	(R13), [V21.B16]; \
	WORD	$0x6e6f12b5; /* uaddw2 v21.4s, v21.4s, v15.8h */ \
	VST1	[V21.B16], (R13); \
	VLD1	(R11), [V22.B16]; \
	WORD	$0x2e6681f6; /* umlal  v22.4s, v15.4h, v6.4h */ \
	VST1	[V22.B16], (R11); \
	ADD	$16, R11, R13; \
	VLD1	(R13), [V23.B16]; \
	WORD	$0x6e6681f7; /* umlal2 v23.4s, v15.8h, v6.8h */ \
	VST1	[V23.B16], (R13)

#define TEMPORAL_FILTER_NEON_HIGH \
	WORD	$0x6e6d34af; /* cmhi  v15.8h, v5.8h, v13.8h */ \
	WORD	$0x4e659ca5; /* mul   v5.8h,  v5.8h, v5.8h */ \
	WORD	$0x4e699ca5; /* mul   v5.8h,  v5.8h, v9.8h */ \
	WORD	$0x4e6b84a5; /* add   v5.8h,  v5.8h, v11.8h */ \
	WORD	$0x6e6e44a5; /* ushl  v5.8h,  v5.8h, v14.8h */ \
	WORD	$0x6e651d4f; /* bsl   v15.16b, v10.16b, v5.16b */ \
	WORD	$0x6e6f854f; /* sub   v15.8h, v10.8h, v15.8h */ \
	WORD	$0x4e6c9def; /* mul   v15.8h, v15.8h, v12.8h */ \
	VLD1	(R12), [V20.B16]; \
	WORD	$0x2e6f1294; /* uaddw  v20.4s, v20.4s, v15.4h */ \
	VST1	[V20.B16], (R12); \
	ADD	$16, R12, R13; \
	VLD1	(R13), [V21.B16]; \
	WORD	$0x6e6f12b5; /* uaddw2 v21.4s, v21.4s, v15.8h */ \
	VST1	[V21.B16], (R13); \
	VLD1	(R11), [V22.B16]; \
	WORD	$0x2e6781f6; /* umlal  v22.4s, v15.4h, v7.4h */ \
	VST1	[V22.B16], (R11); \
	ADD	$16, R11, R13; \
	VLD1	(R13), [V23.B16]; \
	WORD	$0x6e6781f7; /* umlal2 v23.4s, v15.8h, v7.8h */ \
	VST1	[V23.B16], (R13)

// func applyTemporalFilterNEON(src *byte, srcStride int, pred *byte, predStride int, blockSize int, negStrength uint64, rounding uint64, filterWeight uint64, maxDiff uint64, accumulator *uint32, count *uint32)
TEXT ·applyTemporalFilterNEON(SB), NOSPLIT, $0-88
	MOVD	src+0(FP), R0
	MOVD	srcStride+8(FP), R1
	MOVD	pred+16(FP), R2
	MOVD	predStride+24(FP), R3
	MOVD	blockSize+32(FP), R4
	MOVD	accumulator+72(FP), R11
	MOVD	count+80(FP), R12

	MOVD	$3, R5
	VDUP	R5, V9.H8
	MOVD	$16, R5
	VDUP	R5, V10.H8
	MOVD	rounding+48(FP), R5
	VDUP	R5, V11.H8
	MOVD	filterWeight+56(FP), R5
	VDUP	R5, V12.H8
	MOVD	maxDiff+64(FP), R5
	VDUP	R5, V13.H8
	MOVD	negStrength+40(FP), R5
	VDUP	R5, V14.H8
	VEOR	V8.B16, V8.B16, V8.B16

	CMP	$8, R4
	BEQ	temporal_filter_neon_8

	MOVD	$16, R9
temporal_filter_neon_16_loop:
	VLD1	(R0), [V0.B16]
	VLD1	(R2), [V2.B16]
	WORD	$0x2e227004	// uabdl  v4.8h, v0.8b, v2.8b
	WORD	$0x6e227005	// uabdl2 v5.8h, v0.16b, v2.16b
	WORD	$0x2f08a446	// ushll  v6.8h, v2.8b, #0
	WORD	$0x6f08a447	// ushll2 v7.8h, v2.16b, #0
	TEMPORAL_FILTER_NEON_LOW
	ADD	$32, R11, R11
	ADD	$32, R12, R12
	TEMPORAL_FILTER_NEON_HIGH
	ADD	$32, R11, R11
	ADD	$32, R12, R12
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	SUB	$1, R9, R9
	CBNZ	R9, temporal_filter_neon_16_loop
	RET

temporal_filter_neon_8:
	MOVD	$4, R9
temporal_filter_neon_8_loop:
	FMOVD	(R0), F0
	ADD	R1, R0, R13
	FMOVD	(R13), F16
	WORD	$0x6e180600	// mov v0.d[1], v16.d[0]
	FMOVD	(R2), F2
	ADD	R3, R2, R13
	FMOVD	(R13), F17
	WORD	$0x6e180622	// mov v2.d[1], v17.d[0]
	WORD	$0x2e227004	// uabdl  v4.8h, v0.8b, v2.8b
	WORD	$0x6e227005	// uabdl2 v5.8h, v0.16b, v2.16b
	WORD	$0x2f08a446	// ushll  v6.8h, v2.8b, #0
	WORD	$0x6f08a447	// ushll2 v7.8h, v2.16b, #0
	TEMPORAL_FILTER_NEON_LOW
	ADD	$32, R11, R11
	ADD	$32, R12, R12
	TEMPORAL_FILTER_NEON_HIGH
	ADD	$32, R11, R11
	ADD	$32, R12, R12
	ADD	R1, R0, R0
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	R3, R2, R2
	SUB	$1, R9, R9
	CBNZ	R9, temporal_filter_neon_8_loop
	RET
