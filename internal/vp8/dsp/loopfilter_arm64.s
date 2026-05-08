// ARMv8 NEON port of the libvpx v1.16.0 vp8 loop_filter and mb_loop_filter
// inner kernels (16-wide horizontal-edge form). Each routine consumes a
// pointer at the start of the p3 row, reads p3..q3 at +pitch increments,
// and writes the filtered samples back. Encodings produced by clang
// -mcpu=apple-m4 from a hand-written intrinsics translation; many of the
// signed/saturating NEON ops aren't recognized by Go's arm64 assembler so
// they're emitted as raw WORD directives.
//
// loopFilterEdgeH16NEON  : libvpx vp8_loop_filter_neon (writes p1,p0,q0,q1)
// mbLoopFilterEdgeH16NEON: libvpx vp8_mbloop_filter_neon (writes p2,p1,p0,q0,q1,q2)
//
// Vertical-edge variants reuse the same kernels after the dispatch
// gathers each row's 8-byte window into the column lanes.

#include "textflag.h"

// loopFilterEdgeH16NEON ABI ($0-19):
//   src+0(FP)     *byte (points at p3 row, 16 contiguous bytes)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·loopFilterEdgeH16NEON(SB), NOSPLIT, $0-19
	MOVD	src+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2
	MOVBU	limit+17(FP), R3
	MOVBU	thresh+18(FP), R4

	// Broadcast blimit/limit/thresh into V0/V1/V2.16B.
	WORD	$0x4e010c40                 // dup v0.16b, w2  (blimit)
	WORD	$0x4e010c61                 // dup v1.16b, w3  (limit)
	WORD	$0x4e010c82                 // dup v2.16b, w4  (thresh)

	// Row pointers.
	ADD	R1, R0, R5                  // p2
	ADD	R1, R5, R6                  // p1
	ADD	R1, R6, R7                  // p0
	ADD	R1, R7, R8                  // q0
	ADD	R1, R8, R9                  // q1
	ADD	R1, R9, R10                 // q2
	ADD	R1, R10, R11                // q3

	// Loads (mirror clang reg alloc: v17=p3, v3=p2, v4=p1, v5=p0,
	// v6=q0, v7=q1, v16=q2, v18=q3).
	VLD1	(R0), [V17.B16]
	VLD1	(R5), [V3.B16]
	VLD1	(R6), [V4.B16]
	VLD1	(R7), [V5.B16]
	VLD1	(R8), [V6.B16]
	VLD1	(R9), [V7.B16]
	VLD1	(R10), [V16.B16]
	VLD1	(R11), [V18.B16]

	// |p3-p2|, |p2-p1|, |p1-p0|, |q1-q0|, |q2-q1|, |q3-q2|, |p0-q0|, |p1-q1|.
	WORD	$0x6e237631                 // uabd v17, v17, v3
	WORD	$0x6e247463                 // uabd v3,  v3,  v4
	WORD	$0x6e257493                 // uabd v19, v4,  v5
	WORD	$0x6e2674f4                 // uabd v20, v7,  v6
	WORD	$0x6e277615                 // uabd v21, v16, v7
	WORD	$0x6e307650                 // uabd v16, v18, v16
	WORD	$0x6e2674b2                 // uabd v18, v5,  v6  (|p0-q0|)
	WORD	$0x6e277496                 // uabd v22, v4,  v7  (|p1-q1|)

	// Max chain: v3 = max(|p3-p2|,|p2-p1|,|p1-p0|,|q1-q0|,|q2-q1|,|q3-q2|).
	WORD	$0x6e236623                 // umax v3,  v17, v3
	WORD	$0x6e346671                 // umax v17, v19, v20
	WORD	$0x6e236623                 // umax v3,  v17, v3
	WORD	$0x6e356463                 // umax v3,  v3,  v21
	WORD	$0x6e306463                 // umax v3,  v3,  v16

	// v2 = (thresh >= max(|p1-p0|,|q1-q0|))  - i.e., NOT hev. v17 still
	// holds max(|p1-p0|,|q1-q0|) from the previous chain.
	WORD	$0x6e313c42                 // cmhs v2, v2, v17

	// Composite for blimit check: |p0-q0|*2 saturating + |p1-q1|/2.
	WORD	$0x6e320e50                 // uqadd v16, v18, v18
	WORD	$0x6f0f06d1                 // ushr  v17, v22, #1
	WORD	$0x6e310e10                 // uqadd v16, v16, v17

	WORD	$0x6e233c21                 // cmhs v1, v1, v3   ; v1 = (limit >= max-internals)
	WORD	$0x6e303c00                 // cmhs v0, v0, v16  ; v0 = (blimit >= composite)
	WORD	$0x4e201c20                 // and  v0, v1, v0   ; filterMask in v0

	// Convert p1,p0,q0,q1 to signed.
	WORD	$0x4f04e401                 // movi v1.16b, #0x80
	WORD	$0x6e211c83                 // eor v3, v4, v1   ; sps1
	WORD	$0x6e211ca4                 // eor v4, v5, v1   ; sps0
	WORD	$0x6e211cc5                 // eor v5, v6, v1   ; sqs0
	WORD	$0x6e211ce6                 // eor v6, v7, v1   ; sqs1

	// fv = SQSUB(sps1, sqs1)
	WORD	$0x4e262c67                 // sqsub v7, v3, v6
	// fv &= hev  via BIC with the not-hev mask v2.
	WORD	$0x4e621ce7                 // bic v7, v7, v2

	// 3*(sqs0-sps0) widened, in v17/v18.
	WORD	$0x0f00e470                 // movi v16.8b, #3
	WORD	$0x0e30c0b1                 // smull  v17, v5_low, v16   ; sqs0_low * 3
	WORD	$0x0e30a091                 // smlsl  v17, v4_low, v16   ; -= sps0_low * 3
	WORD	$0x4f00e470                 // movi v16.16b, #3
	WORD	$0x4e30c0b2                 // smull2 v18, v5, v16
	WORD	$0x4e30a092                 // smlsl2 v18, v4, v16

	WORD	$0x0e271231                 // saddw  v17, v17, v7
	WORD	$0x4e271247                 // saddw2 v7,  v18, v7

	WORD	$0x0e214a31                 // sqxtn  v17, v17
	WORD	$0x4e2148f1                 // sqxtn2 v17, v7

	WORD	$0x4e311c00                 // and v0, v0, v17     ; v0 = filterMask & fv

	// f1 = (fv+4)>>3, f2 = (fv+3)>>3.
	WORD	$0x4f00e487                 // movi v7.16b, #4
	WORD	$0x4e270c07                 // sqadd v7, v0, v7
	WORD	$0x4f0d04e7                 // sshr  v7, v7, #3
	WORD	$0x4e300c00                 // sqadd v0, v0, v16   ; v16 still = 3
	WORD	$0x4f0d0400                 // sshr  v0, v0, #3

	WORD	$0x4e272ca5                 // sqsub v5, v5, v7    ; sqs0 -= f1
	WORD	$0x4e200c80                 // sqadd v0, v4, v0    ; v0 = sps0 + f2

	WORD	$0x4f0f24e4                 // srshr v4, v7, #1
	WORD	$0x4e241c42                 // and   v2, v2, v4    ; v2 = (~hev) & rshr(f1,1)
	WORD	$0x4e222cc4                 // sqsub v4, v6, v2    ; sqs1 - masked
	WORD	$0x4e220c62                 // sqadd v2, v3, v2    ; sps1 + masked

	// Unsigned conversion + stores.
	WORD	$0x6e211c42                 // eor v2, v2, v1   ; p1
	WORD	$0x6e211c00                 // eor v0, v0, v1   ; p0
	VST1	[V2.B16], (R6)              // store p1
	VST1	[V0.B16], (R7)              // store p0
	WORD	$0x6e211ca0                 // eor v0, v5, v1   ; q0
	WORD	$0x6e211c81                 // eor v1, v4, v1   ; q1
	VST1	[V0.B16], (R8)              // store q0
	VST1	[V1.B16], (R9)              // store q1

	RET

// mbLoopFilterEdgeH16NEON ABI ($0-19):
//   src+0(FP)     *byte (points at p3 row, 16 contiguous bytes)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·mbLoopFilterEdgeH16NEON(SB), NOSPLIT, $0-19
	MOVD	src+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2
	MOVBU	limit+17(FP), R3
	MOVBU	thresh+18(FP), R4

	WORD	$0x4e010c40                 // dup v0.16b, w2  (blimit)
	WORD	$0x4e010c61                 // dup v1.16b, w3  (limit)
	WORD	$0x4e010c82                 // dup v2.16b, w4  (thresh)

	ADD	R1, R0, R5                  // p2
	ADD	R1, R5, R6                  // p1
	ADD	R1, R6, R7                  // p0
	ADD	R1, R7, R8                  // q0
	ADD	R1, R8, R9                  // q1
	ADD	R1, R9, R10                 // q2
	ADD	R1, R10, R11                // q3

	// Loads: v7=p3, v3=p2, v4=p1, v5=p0, v6=q0, v16=q1, v17=q2, v18=q3.
	VLD1	(R0), [V7.B16]
	VLD1	(R5), [V3.B16]
	VLD1	(R6), [V4.B16]
	VLD1	(R7), [V5.B16]
	VLD1	(R8), [V6.B16]
	VLD1	(R9), [V16.B16]
	VLD1	(R10), [V17.B16]
	VLD1	(R11), [V18.B16]

	WORD	$0x6e2374e7                 // uabd v7, v7, v3   (|p3-p2|)
	WORD	$0x6e247473                 // uabd v19, v3, v4  (|p2-p1|)
	WORD	$0x6e257494                 // uabd v20, v4, v5  (|p1-p0|)
	WORD	$0x6e267615                 // uabd v21, v16, v6 (|q1-q0|)
	WORD	$0x6e307636                 // uabd v22, v17, v16 (|q2-q1|)
	WORD	$0x6e317652                 // uabd v18, v18, v17 (|q3-q2|)
	WORD	$0x6e2674b7                 // uabd v23, v5, v6  (|p0-q0|)
	WORD	$0x6e307498                 // uabd v24, v4, v16 (|p1-q1|)

	WORD	$0x6e3364e7                 // umax v7, v7, v19
	WORD	$0x6e356693                 // umax v19, v20, v21
	WORD	$0x6e276667                 // umax v7, v19, v7
	WORD	$0x6e3664e7                 // umax v7, v7, v22
	WORD	$0x6e3264e7                 // umax v7, v7, v18

	WORD	$0x6e370ef2                 // uqadd v18, v23, v23
	WORD	$0x6f0f0714                 // ushr  v20, v24, #1
	WORD	$0x6e340e52                 // uqadd v18, v18, v20

	WORD	$0x6e273c21                 // cmhs v1, v1, v7    ; (limit >= max)
	WORD	$0x6e323c00                 // cmhs v0, v0, v18   ; (blimit >= composite)
	WORD	$0x4e201c27                 // and  v7, v1, v0    ; filterMask

	WORD	$0x6e333c52                 // cmhs v18, v2, v19  ; (thresh >= max(|p1-p0|,|q1-q0|)) = NOT-hev

	WORD	$0x4f04e400                 // movi v0.16b, #0x80
	WORD	$0x6e201c61                 // eor v1, v3,  v0    ; sps2
	WORD	$0x6e201c83                 // eor v3, v4,  v0    ; sps1
	WORD	$0x6e201ca4                 // eor v4, v5,  v0    ; sps0
	WORD	$0x6e201cc5                 // eor v5, v6,  v0    ; sqs0
	WORD	$0x6e201e06                 // eor v6, v16, v0    ; sqs1
	WORD	$0x6e201e22                 // eor v2, v17, v0    ; sqs2

	WORD	$0x4e262c70                 // sqsub v16, v3, v6
	WORD	$0x0f00e471                 // movi v17.8b, #3
	WORD	$0x0e31c0b3                 // smull v19, v5, v17
	WORD	$0x0e31a093                 // smlsl v19, v4, v17
	WORD	$0x4f00e471                 // movi v17.16b, #3
	WORD	$0x4e31c0b4                 // smull2 v20, v5, v17
	WORD	$0x4e31a094                 // smlsl2 v20, v4, v17
	WORD	$0x0e301273                 // saddw  v19, v19, v16
	WORD	$0x4e301290                 // saddw2 v16, v20, v16
	WORD	$0x0e214a73                 // sqxtn  v19, v19
	WORD	$0x4e214a13                 // sqxtn2 v19, v16
	WORD	$0x4e331ce7                 // and v7, v7, v19    ; v7 = filterMask & fv

	WORD	$0x4e721cf0                 // bic v16, v7, v18   ; fv2 = fv & hev (BIC with not-hev)
	WORD	$0x4f00e493                 // movi v19.16b, #4
	WORD	$0x4e330e13                 // sqadd v19, v16, v19
	WORD	$0x4f0d0673                 // sshr  v19, v19, #3 ; f1
	WORD	$0x4e310e10                 // sqadd v16, v16, v17 ; v17 still = 3
	WORD	$0x4f0d0610                 // sshr  v16, v16, #3 ; f2
	WORD	$0x4e332ca5                 // sqsub v5, v5, v19  ; sqs0 -= f1
	WORD	$0x4e300c84                 // sqadd v4, v4, v16  ; sps0 += f2

	WORD	$0x4e271e47                 // and v7, v18, v7    ; v7 = (~hev) & fv

	// u27 = sat((63 + v7*27) >> 7) - need int16 mlal, sqshrn narrow.
	WORD	$0x4f00e770                 // movi v16.16b, #0x1b (27)
	WORD	$0x0f00e771                 // movi v17.8b,  #0x1b
	WORD	$0x4f0187f2                 // movi v18.8h,  #0x3f (63)  -- ! note: this clobbers v18 which is the not-hev mask. clang reused it; we no longer need not-hev after this point.
	WORD	$0x4f0187f3                 // movi v19.8h,  #0x3f
	WORD	$0x0e3180f3                 // smlal v19, v7, v17  ; lo: 63 + fv_lo * 27
	WORD	$0x4f0187f1                 // movi v17.8h, #0x3f
	WORD	$0x4e3080f1                 // smlal2 v17, v7, v16 ; hi: 63 + fv_hi * 27
	WORD	$0x0f099670                 // sqshrn  v16, v19, #7
	WORD	$0x4f099630                 // sqshrn2 v16, v17, #7
	WORD	$0x4e302ca5                 // sqsub v5, v5, v16
	WORD	$0x4e300c84                 // sqadd v4, v4, v16

	WORD	$0x4f00e650                 // movi v16.16b, #0x12 (18)
	WORD	$0x0f00e651                 // movi v17.8b, #0x12
	WORD	$0x4f0187f3                 // movi v19.8h, #0x3f
	WORD	$0x0e3180f3                 // smlal v19, v7, v17
	WORD	$0x4f0187f1                 // movi v17.8h, #0x3f
	WORD	$0x4e3080f1                 // smlal2 v17, v7, v16
	WORD	$0x0f099670                 // sqshrn v16, v19, #7
	WORD	$0x4f099630                 // sqshrn2 v16, v17, #7
	WORD	$0x4e302cc6                 // sqsub v6, v6, v16
	WORD	$0x4e300c63                 // sqadd v3, v3, v16

	WORD	$0x4f00e530                 // movi v16.16b, #0x9
	WORD	$0x0f00e531                 // movi v17.8b, #0x9
	WORD	$0x4f0187f3                 // movi v19.8h, #0x3f
	WORD	$0x0e3180f3                 // smlal v19, v7, v17
	WORD	$0x4e3080f2                 // smlal2 v18, v7, v16  ; v18 still has 63 broadcast from earlier
	WORD	$0x0f099667                 // sqshrn  v7, v19, #7
	WORD	$0x4f099647                 // sqshrn2 v7, v18, #7
	WORD	$0x4e272c42                 // sqsub v2, v2, v7
	WORD	$0x4e270c21                 // sqadd v1, v1, v7

	// Reverse the signed offset on output and store p2,p1,p0,q0,q1,q2.
	WORD	$0x6e201c21                 // eor v1, v1, v0  -- p2 (sps2)
	WORD	$0x6e201c63                 // eor v3, v3, v0  -- p1 (sps1)
	WORD	$0x6e201c84                 // eor v4, v4, v0  -- p0 (sps0)
	WORD	$0x6e201ca5                 // eor v5, v5, v0  -- q0 (sqs0)
	VST1	[V1.B16], (R5)              // p2
	VST1	[V3.B16], (R6)              // p1
	VST1	[V4.B16], (R7)              // p0
	VST1	[V5.B16], (R8)              // q0
	WORD	$0x6e201cc1                 // eor v1, v6, v0  -- q1 (sqs1)
	WORD	$0x6e201c40                 // eor v0, v2, v0  -- q2 (sqs2)
	VST1	[V1.B16], (R9)              // q1
	VST1	[V0.B16], (R10)             // q2

	RET
