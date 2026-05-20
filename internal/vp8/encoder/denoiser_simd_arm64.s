//go:build arm64 && !purego

// ARMv8 NEON first pass for the VP8 denoiser. Mirrors libvpx v1.16.0
// vp8/encoder/arm/neon/denoising_neon.c vp8_denoiser_filter_neon up to
// the first sum-diff threshold check.

#include "textflag.h"

// func denoiserFilterYFirstPassNEON(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)
TEXT ·denoiserFilterYFirstPassNEON(SB), NOSPLIT, $0-72
	MOVD	mc+0(FP), R0
	MOVD	mcStride+8(FP), R1
	MOVD	avg+16(FP), R2
	MOVD	avgStride+24(FP), R3
	MOVD	sig+32(FP), R4
	MOVD	sigStride+40(FP), R5
	MOVD	level1Adjustment+48(FP), R6
	MOVD	level1Threshold+56(FP), R7
	MOVD	sumOut+64(FP), R8

	MOVD	$1, R9
	MOVD	$2, R10
	MOVD	$8, R11
	MOVD	$16, R12
	VDUP	R6, V29.B16	// level 1 absolute adjustment
	VDUP	R7, V30.B16	// level 1 threshold
	VDUP	R9, V26.B16	// delta between levels 1 and 2
	VDUP	R10, V25.B16	// delta between levels 2 and 3
	VDUP	R11, V28.B16	// level 2 threshold
	VDUP	R12, V27.B16	// level 3 threshold

	VEOR	V20.B16, V20.B16, V20.B16	// signed per-column adjustment accumulator
	MOVD	$16, R9

denoise_y_loop:
	VLD1	(R4), [V0.B16]	// sig
	VLD1	(R0), [V1.B16]	// mc_running_avg

	WORD	$0x6e217402	// uabd  v2.16b, v0.16b, v1.16b
	WORD	$0x6e203423	// cmhi  v3.16b, v1.16b, v0.16b (mc > sig)
	WORD	$0x6e213404	// cmhi  v4.16b, v0.16b, v1.16b (sig > mc)
	WORD	$0x6e3e3c45	// cmhs  v5.16b, v2.16b, v30.16b (abs >= level1 threshold)
	WORD	$0x6e3c3c46	// cmhs  v6.16b, v2.16b, v28.16b (abs >= 8)
	WORD	$0x6e3b3c47	// cmhs  v7.16b, v2.16b, v27.16b (abs >= 16)
	WORD	$0x4e3a1cc8	// and   v8.16b, v6.16b, v26.16b
	WORD	$0x4e391ce9	// and   v9.16b, v7.16b, v25.16b
	WORD	$0x4e2887aa	// add   v10.16b, v29.16b, v8.16b
	WORD	$0x4e29854a	// add   v10.16b, v10.16b, v9.16b
	WORD	$0x6e621d45	// bsl   v5.16b, v10.16b, v2.16b (level? adjusted : absdiff)
	WORD	$0x4e251c68	// and   v8.16b, v3.16b, v5.16b (positive adjustment)
	WORD	$0x4e251c89	// and   v9.16b, v4.16b, v5.16b (negative adjustment)
	WORD	$0x6e280c0b	// uqadd v11.16b, v0.16b, v8.16b
	WORD	$0x6e292d6b	// uqsub v11.16b, v11.16b, v9.16b
	VST1	[V11.B16], (R2)

	WORD	$0x4e280e94	// sqadd v20.16b, v20.16b, v8.16b
	WORD	$0x4e292e94	// sqsub v20.16b, v20.16b, v9.16b

	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	R5, R4, R4
	SUB	$1, R9, R9
	CBNZ	R9, denoise_y_loop

	WORD	$0x4e202a94	// saddlp v20.8h, v20.16b
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	FMOVS	F20, (R8)
	RET

// func denoiserFilterUVFirstPassNEON(mc *byte, mcStride int, avg *byte, avgStride int, sig *byte, sigStride int, level1Adjustment uint64, level1Threshold uint64, sumOut *int32)
TEXT ·denoiserFilterUVFirstPassNEON(SB), NOSPLIT, $0-72
	MOVD	mc+0(FP), R0
	MOVD	mcStride+8(FP), R1
	MOVD	avg+16(FP), R2
	MOVD	avgStride+24(FP), R3
	MOVD	sig+32(FP), R4
	MOVD	sigStride+40(FP), R5
	MOVD	level1Adjustment+48(FP), R6
	MOVD	level1Threshold+56(FP), R7
	MOVD	sumOut+64(FP), R8

	MOVD	$1, R9
	MOVD	$2, R10
	MOVD	$8, R11
	MOVD	$16, R12
	VDUP	R6, V29.B16
	VDUP	R7, V30.B16
	VDUP	R9, V26.B16
	VDUP	R10, V25.B16
	VDUP	R11, V28.B16
	VDUP	R12, V27.B16

	VEOR	V20.B16, V20.B16, V20.B16
	MOVD	$4, R9

denoise_uv_loop:
	FMOVD	(R4), F0
	FMOVD	(R0), F1
	ADD	R5, R4, R10
	ADD	R1, R0, R11
	FMOVD	(R10), F4
	FMOVD	(R11), F5
	WORD	$0x6e180480	// mov v0.d[1], v4.d[0]
	WORD	$0x6e1804a1	// mov v1.d[1], v5.d[0]

	WORD	$0x6e217402	// uabd  v2.16b, v0.16b, v1.16b
	WORD	$0x6e203423	// cmhi  v3.16b, v1.16b, v0.16b
	WORD	$0x6e213404	// cmhi  v4.16b, v0.16b, v1.16b
	WORD	$0x6e3e3c45	// cmhs  v5.16b, v2.16b, v30.16b
	WORD	$0x6e3c3c46	// cmhs  v6.16b, v2.16b, v28.16b
	WORD	$0x6e3b3c47	// cmhs  v7.16b, v2.16b, v27.16b
	WORD	$0x4e3a1cc8	// and   v8.16b, v6.16b, v26.16b
	WORD	$0x4e391ce9	// and   v9.16b, v7.16b, v25.16b
	WORD	$0x4e2887aa	// add   v10.16b, v29.16b, v8.16b
	WORD	$0x4e29854a	// add   v10.16b, v10.16b, v9.16b
	WORD	$0x6e621d45	// bsl   v5.16b, v10.16b, v2.16b
	WORD	$0x4e251c68	// and   v8.16b, v3.16b, v5.16b
	WORD	$0x4e251c89	// and   v9.16b, v4.16b, v5.16b
	WORD	$0x6e280c0b	// uqadd v11.16b, v0.16b, v8.16b
	WORD	$0x6e292d6b	// uqsub v11.16b, v11.16b, v9.16b

	FMOVD	F11, (R2)
	ADD	R3, R2, R12
	WORD	$0x6e08456c	// mov v12.d[0], v11.d[1]
	FMOVD	F12, (R12)

	WORD	$0x4e280e94	// sqadd v20.16b, v20.16b, v8.16b
	WORD	$0x4e292e94	// sqsub v20.16b, v20.16b, v9.16b

	ADD	R1, R0, R0
	ADD	R1, R0, R0
	ADD	R3, R2, R2
	ADD	R3, R2, R2
	ADD	R5, R4, R4
	ADD	R5, R4, R4
	SUB	$1, R9, R9
	CBNZ	R9, denoise_uv_loop

	WORD	$0x4e202a94	// saddlp v20.8h, v20.16b
	WORD	$0x4e703a94	// saddlv s20, v20.8h
	FMOVS	F20, (R8)
	RET
