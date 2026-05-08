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


// loopFilterEdgeV16NEON ABI ($0-19):
//   src+0(FP)     *byte (points at q0 column of row 0)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
//
// Direct vertical-edge variant. Reads 16 rows of 8 bytes each at src-4,
// transposes 16x8 -> 8x16 in registers via TRN1/TRN2 cascade, runs the
// same loop_filter inner kernel, and writes 4 modified columns
// (p1,p0,q0,q1) at offset src-2 via ST4.B by-lane stores. Encodings
// produced by clang -O3 -mcpu=apple-m4 from libvpx v1.16.0
// vp8_loop_filter_vertical_edge_y_neon.
TEXT ·loopFilterEdgeV16NEON(SB), NOSPLIT, $0-19
	MOVD	src+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2
	MOVBU	limit+17(FP), R3
	MOVBU	thresh+18(FP), R4

	WORD	$0x93407c28                 // sxtw	x8, w1
	WORD	$0x4e010c40                 // dup.16b	v0, w2
	WORD	$0x4e010c62                 // dup.16b	v2, w3
	WORD	$0x4e010c81                 // dup.16b	v1, w4
	WORD	$0xfc5fcc03                 // ldr	d3, [x0, #-0x4]!
	WORD	$0x8b080009                 // add	x9, x0, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400125                 // ldr	d5, [x9]
	WORD	$0xfd400144                 // ldr	d4, [x10]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400126                 // ldr	d6, [x9]
	WORD	$0xfd400147                 // ldr	d7, [x10]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400130                 // ldr	d16, [x9]
	WORD	$0xfd400151                 // ldr	d17, [x10]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400132                 // ldr	d18, [x9]
	WORD	$0xfd400153                 // ldr	d19, [x10]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400134                 // ldr	d20, [x9]
	WORD	$0xfd400155                 // ldr	d21, [x10]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400136                 // ldr	d22, [x9]
	WORD	$0xfd400157                 // ldr	d23, [x10]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400138                 // ldr	d24, [x9]
	WORD	$0xfd400159                 // ldr	d25, [x10]
	WORD	$0xfc68695a                 // ldr	d26, [x10, x8]
	WORD	$0x6e180663                 // mov.d	v3[1], v19[0]
	WORD	$0x6e180685                 // mov.d	v5[1], v20[0]
	WORD	$0x6e1806a4                 // mov.d	v4[1], v21[0]
	WORD	$0x6e1806c6                 // mov.d	v6[1], v22[0]
	WORD	$0x6e1806e7                 // mov.d	v7[1], v23[0]
	WORD	$0x6e180710                 // mov.d	v16[1], v24[0]
	WORD	$0x6e180731                 // mov.d	v17[1], v25[0]
	WORD	$0x6e180752                 // mov.d	v18[1], v26[0]
	WORD	$0x4e872873                 // trn1.4s	v19, v3, v7
	WORD	$0x4e876863                 // trn2.4s	v3, v3, v7
	WORD	$0x4e9028a7                 // trn1.4s	v7, v5, v16
	WORD	$0x4e9068a5                 // trn2.4s	v5, v5, v16
	WORD	$0x4e912890                 // trn1.4s	v16, v4, v17
	WORD	$0x4e916884                 // trn2.4s	v4, v4, v17
	WORD	$0x4e9228d1                 // trn1.4s	v17, v6, v18
	WORD	$0x4e9268c6                 // trn2.4s	v6, v6, v18
	WORD	$0x4e502a72                 // trn1.8h	v18, v19, v16
	WORD	$0x4e506a70                 // trn2.8h	v16, v19, v16
	WORD	$0x4e5128f3                 // trn1.8h	v19, v7, v17
	WORD	$0x4e5168e7                 // trn2.8h	v7, v7, v17
	WORD	$0x4e442871                 // trn1.8h	v17, v3, v4
	WORD	$0x4e446863                 // trn2.8h	v3, v3, v4
	WORD	$0x4e4628a4                 // trn1.8h	v4, v5, v6
	WORD	$0x4e4668a5                 // trn2.8h	v5, v5, v6
	WORD	$0x4e132a46                 // trn1.16b	v6, v18, v19
	WORD	$0x4e136a52                 // trn2.16b	v18, v18, v19
	WORD	$0x4e072a13                 // trn1.16b	v19, v16, v7
	WORD	$0x4e076a07                 // trn2.16b	v7, v16, v7
	WORD	$0x4e042a30                 // trn1.16b	v16, v17, v4
	WORD	$0x4e046a24                 // trn2.16b	v4, v17, v4
	WORD	$0x4e052871                 // trn1.16b	v17, v3, v5
	WORD	$0x4e056863                 // trn2.16b	v3, v3, v5
	WORD	$0x6e3274c5                 // uabd.16b	v5, v6, v18
	WORD	$0x6e337646                 // uabd.16b	v6, v18, v19
	WORD	$0x6e277672                 // uabd.16b	v18, v19, v7
	WORD	$0x6e307494                 // uabd.16b	v20, v4, v16
	WORD	$0x6e247635                 // uabd.16b	v21, v17, v4
	WORD	$0x6e317463                 // uabd.16b	v3, v3, v17
	WORD	$0x6e2664a5                 // umax.16b	v5, v5, v6
	WORD	$0x6e2366a3                 // umax.16b	v3, v21, v3
	WORD	$0x6e346646                 // umax.16b	v6, v18, v20
	WORD	$0x6e2664a5                 // umax.16b	v5, v5, v6
	WORD	$0x6e3074f1                 // uabd.16b	v17, v7, v16
	WORD	$0x6e2364a3                 // umax.16b	v3, v5, v3
	WORD	$0x6e247665                 // uabd.16b	v5, v19, v4
	WORD	$0x6e310e31                 // uqadd.16b	v17, v17, v17
	WORD	$0x6e233c42                 // cmhs.16b	v2, v2, v3
	WORD	$0x4f04e403                 // movi.16b	v3, #0x80
	WORD	$0x6e231c84                 // eor.16b	v4, v4, v3
	WORD	$0x6e231e10                 // eor.16b	v16, v16, v3
	WORD	$0x6e231ce7                 // eor.16b	v7, v7, v3
	WORD	$0x6e231e72                 // eor.16b	v18, v19, v3
	WORD	$0x6f0f04a5                 // ushr.16b	v5, v5, #0x1
	WORD	$0x6e250e25                 // uqadd.16b	v5, v17, v5
	WORD	$0x6e253c00                 // cmhs.16b	v0, v0, v5
	WORD	$0x4e242e45                 // sqsub.16b	v5, v18, v4
	WORD	$0x6e263c21                 // cmhs.16b	v1, v1, v6
	WORD	$0x0f00e466                 // movi.8b	v6, #0x3
	WORD	$0x0e26c211                 // smull.8h	v17, v16, v6
	WORD	$0x0e26a0f1                 // smlsl.8h	v17, v7, v6
	WORD	$0x4f00e466                 // movi.16b	v6, #0x3
	WORD	$0x4e26c213                 // smull2.8h	v19, v16, v6
	WORD	$0x4e26a0f3                 // smlsl2.8h	v19, v7, v6
	WORD	$0x4e611ca5                 // bic.16b	v5, v5, v1
	WORD	$0x4e201c40                 // and.16b	v0, v2, v0
	WORD	$0x0e251222                 // saddw.8h	v2, v17, v5
	WORD	$0x4e251265                 // saddw2.8h	v5, v19, v5
	WORD	$0x0e214842                 // sqxtn.8b	v2, v2
	WORD	$0x4e2148a2                 // sqxtn2.16b	v2, v5
	WORD	$0x4e221c00                 // and.16b	v0, v0, v2
	WORD	$0x4e260c02                 // sqadd.16b	v2, v0, v6
	WORD	$0x4f00e485                 // movi.16b	v5, #0x4
	WORD	$0x4e250c00                 // sqadd.16b	v0, v0, v5
	WORD	$0x4f0d0442                 // sshr.16b	v2, v2, #0x3
	WORD	$0x4f0d0400                 // sshr.16b	v0, v0, #0x3
	WORD	$0x4e220ce2                 // sqadd.16b	v2, v7, v2
	WORD	$0x4e202e05                 // sqsub.16b	v5, v16, v0
	WORD	$0x4f0f2400                 // srshr.16b	v0, v0, #0x1
	WORD	$0x4e201c20                 // and.16b	v0, v1, v0
	WORD	$0x4e200e41                 // sqadd.16b	v1, v18, v0
	WORD	$0x4e202c80                 // sqsub.16b	v0, v4, v0
	WORD	$0x6e231c13                 // eor.16b	v19, v0, v3
	WORD	$0x6e231cb2                 // eor.16b	v18, v5, v3
	WORD	$0x6e231c51                 // eor.16b	v17, v2, v3
	WORD	$0x6e231c30                 // eor.16b	v16, v1, v3
	WORD	$0x91000809                 // add	x9, x0, #0x2
	WORD	$0x937d7c2a                 // sbfiz	x10, x1, #3, #32
	WORD	$0x8b08012b                 // add	x11, x9, x8
	WORD	$0x0daa2130                 // st4.b	{ v16, v17, v18, v19 }[0], [x9], x10
	WORD	$0x0da82570                 // st4.b	{ v16, v17, v18, v19 }[1], [x11], x8
	WORD	$0x0da82970                 // st4.b	{ v16, v17, v18, v19 }[2], [x11], x8
	WORD	$0x0da82d70                 // st4.b	{ v16, v17, v18, v19 }[3], [x11], x8
	WORD	$0x0da83170                 // st4.b	{ v16, v17, v18, v19 }[4], [x11], x8
	WORD	$0x0da83570                 // st4.b	{ v16, v17, v18, v19 }[5], [x11], x8
	WORD	$0x0da83970                 // st4.b	{ v16, v17, v18, v19 }[6], [x11], x8
	WORD	$0x6e104200                 // ext.16b	v0, v16, v16, #0x8
	WORD	$0x6e114221                 // ext.16b	v1, v17, v17, #0x8
	WORD	$0x0d203d70                 // st4.b	{ v16, v17, v18, v19 }[7], [x11]
	WORD	$0x6e124242                 // ext.16b	v2, v18, v18, #0x8
	WORD	$0x6e134263                 // ext.16b	v3, v19, v19, #0x8
	WORD	$0x0da82120                 // st4.b	{ v0, v1, v2, v3 }[0], [x9], x8
	WORD	$0x0da82520                 // st4.b	{ v0, v1, v2, v3 }[1], [x9], x8
	WORD	$0x0da82920                 // st4.b	{ v0, v1, v2, v3 }[2], [x9], x8
	WORD	$0x0da82d20                 // st4.b	{ v0, v1, v2, v3 }[3], [x9], x8
	WORD	$0x0da83120                 // st4.b	{ v0, v1, v2, v3 }[4], [x9], x8
	WORD	$0x0da83520                 // st4.b	{ v0, v1, v2, v3 }[5], [x9], x8
	WORD	$0x0da83920                 // st4.b	{ v0, v1, v2, v3 }[6], [x9], x8
	WORD	$0x0d203d20                 // st4.b	{ v0, v1, v2, v3 }[7], [x9]

	RET

// mbLoopFilterEdgeV16NEON ABI ($0-19):
//   src+0(FP)     *byte (points at q0 column of row 0)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
//
// Direct vertical-edge mb-filter variant. Same load+transpose front-end
// as loopFilterEdgeV16NEON; after the inner mb kernel writes p2..q2,
// the code re-transposes the full 8 columns and stores 8 bytes per row
// at offset src-4 for all 16 rows. Encodings produced by clang -O3
// -mcpu=apple-m4 from libvpx v1.16.0
// vp8_mbloop_filter_vertical_edge_y_neon.
TEXT ·mbLoopFilterEdgeV16NEON(SB), NOSPLIT, $0-19
	MOVD	src+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2
	MOVBU	limit+17(FP), R3
	MOVBU	thresh+18(FP), R4

	WORD	$0x93407c28                 // sxtw	x8, w1
	WORD	$0x4e010c40                 // dup.16b	v0, w2
	WORD	$0x4e010c62                 // dup.16b	v2, w3
	WORD	$0x4e010c81                 // dup.16b	v1, w4
	WORD	$0xfc5fcc03                 // ldr	d3, [x0, #-0x4]!
	WORD	$0x8b21cc09                 // add	x9, x0, w1, sxtw #3
	WORD	$0xfd400126                 // ldr	d6, [x9]
	WORD	$0x8b08000a                 // add	x10, x0, x8
	WORD	$0x8b08014b                 // add	x11, x10, x8
	WORD	$0xfd400144                 // ldr	d4, [x10]
	WORD	$0x8b080129                 // add	x9, x9, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400130                 // ldr	d16, [x9]
	WORD	$0xfd400165                 // ldr	d5, [x11]
	WORD	$0xfd400151                 // ldr	d17, [x10]
	WORD	$0x8b080169                 // add	x9, x11, x8
	WORD	$0x8b08012b                 // add	x11, x9, x8
	WORD	$0xfd400127                 // ldr	d7, [x9]
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd400133                 // ldr	d19, [x9]
	WORD	$0xfd400172                 // ldr	d18, [x11]
	WORD	$0xfd400155                 // ldr	d21, [x10]
	WORD	$0x8b08016b                 // add	x11, x11, x8
	WORD	$0x8b080169                 // add	x9, x11, x8
	WORD	$0xfd400174                 // ldr	d20, [x11]
	WORD	$0x8b08014b                 // add	x11, x10, x8
	WORD	$0x8b08016a                 // add	x10, x11, x8
	WORD	$0xfd400177                 // ldr	d23, [x11]
	WORD	$0xfd400136                 // ldr	d22, [x9]
	WORD	$0xfd400158                 // ldr	d24, [x10]
	WORD	$0x531d702b                 // lsl	w11, w1, #3
	WORD	$0x4b01016b                 // sub	w11, w11, w1
	WORD	$0x93407d6c                 // sxtw	x12, w11
	WORD	$0x8b080129                 // add	x9, x9, x8
	WORD	$0xcb0c012b                 // sub	x11, x9, x12
	WORD	$0xfd400139                 // ldr	d25, [x9]
	WORD	$0x8b08014a                 // add	x10, x10, x8
	WORD	$0xcb0c0149                 // sub	x9, x10, x12
	WORD	$0xfd40015a                 // ldr	d26, [x10]
	WORD	$0x6e1804c3                 // mov.d	v3[1], v6[0]
	WORD	$0x6e180604                 // mov.d	v4[1], v16[0]
	WORD	$0x6e180625                 // mov.d	v5[1], v17[0]
	WORD	$0x6e180667                 // mov.d	v7[1], v19[0]
	WORD	$0x6e1806b2                 // mov.d	v18[1], v21[0]
	WORD	$0x6e1806f4                 // mov.d	v20[1], v23[0]
	WORD	$0x6e180716                 // mov.d	v22[1], v24[0]
	WORD	$0x6e180759                 // mov.d	v25[1], v26[0]
	WORD	$0x4e922866                 // trn1.4s	v6, v3, v18
	WORD	$0x4e926863                 // trn2.4s	v3, v3, v18
	WORD	$0x4e942890                 // trn1.4s	v16, v4, v20
	WORD	$0x4e946884                 // trn2.4s	v4, v4, v20
	WORD	$0x4e9628b1                 // trn1.4s	v17, v5, v22
	WORD	$0x4e9668a5                 // trn2.4s	v5, v5, v22
	WORD	$0x4e9928f2                 // trn1.4s	v18, v7, v25
	WORD	$0x4e9968e7                 // trn2.4s	v7, v7, v25
	WORD	$0x4e5128d3                 // trn1.8h	v19, v6, v17
	WORD	$0x4e5168c6                 // trn2.8h	v6, v6, v17
	WORD	$0x4e522a11                 // trn1.8h	v17, v16, v18
	WORD	$0x4e526a10                 // trn2.8h	v16, v16, v18
	WORD	$0x4e452872                 // trn1.8h	v18, v3, v5
	WORD	$0x4e456863                 // trn2.8h	v3, v3, v5
	WORD	$0x4e472885                 // trn1.8h	v5, v4, v7
	WORD	$0x4e476887                 // trn2.8h	v7, v4, v7
	WORD	$0x4e112a64                 // trn1.16b	v4, v19, v17
	WORD	$0x4e116a71                 // trn2.16b	v17, v19, v17
	WORD	$0x4e1028d3                 // trn1.16b	v19, v6, v16
	WORD	$0x4e1068c6                 // trn2.16b	v6, v6, v16
	WORD	$0x4e052a50                 // trn1.16b	v16, v18, v5
	WORD	$0x4e056a45                 // trn2.16b	v5, v18, v5
	WORD	$0x4e072872                 // trn1.16b	v18, v3, v7
	WORD	$0x4e076863                 // trn2.16b	v3, v3, v7
	WORD	$0x6e317487                 // uabd.16b	v7, v4, v17
	WORD	$0x6e337634                 // uabd.16b	v20, v17, v19
	WORD	$0x6e267675                 // uabd.16b	v21, v19, v6
	WORD	$0x6e3074b6                 // uabd.16b	v22, v5, v16
	WORD	$0x6e257657                 // uabd.16b	v23, v18, v5
	WORD	$0x6e327478                 // uabd.16b	v24, v3, v18
	WORD	$0x6e3464e7                 // umax.16b	v7, v7, v20
	WORD	$0x6e3866f4                 // umax.16b	v20, v23, v24
	WORD	$0x6e3666b5                 // umax.16b	v21, v21, v22
	WORD	$0x6e3564e7                 // umax.16b	v7, v7, v21
	WORD	$0x6e3074d6                 // uabd.16b	v22, v6, v16
	WORD	$0x6e3464e7                 // umax.16b	v7, v7, v20
	WORD	$0x6e273c47                 // cmhs.16b	v7, v2, v7
	WORD	$0x6e257674                 // uabd.16b	v20, v19, v5
	WORD	$0x6e360ed6                 // uqadd.16b	v22, v22, v22
	WORD	$0x4f04e402                 // movi.16b	v2, #0x80
	WORD	$0x6e221e52                 // eor.16b	v18, v18, v2
	WORD	$0x6e221ca5                 // eor.16b	v5, v5, v2
	WORD	$0x6e221e10                 // eor.16b	v16, v16, v2
	WORD	$0x6e221cc6                 // eor.16b	v6, v6, v2
	WORD	$0x6e221e73                 // eor.16b	v19, v19, v2
	WORD	$0x6e221e31                 // eor.16b	v17, v17, v2
	WORD	$0x6f0f0694                 // ushr.16b	v20, v20, #0x1
	WORD	$0x6e340ed4                 // uqadd.16b	v20, v22, v20
	WORD	$0x6e353c21                 // cmhs.16b	v1, v1, v21
	WORD	$0x6e343c00                 // cmhs.16b	v0, v0, v20
	WORD	$0x4e252e74                 // sqsub.16b	v20, v19, v5
	WORD	$0x0f00e475                 // movi.8b	v21, #0x3
	WORD	$0x0e35c216                 // smull.8h	v22, v16, v21
	WORD	$0x0e35a0d6                 // smlsl.8h	v22, v6, v21
	WORD	$0x4f00e475                 // movi.16b	v21, #0x3
	WORD	$0x4e35c217                 // smull2.8h	v23, v16, v21
	WORD	$0x4e35a0d7                 // smlsl2.8h	v23, v6, v21
	WORD	$0x4e201ce0                 // and.16b	v0, v7, v0
	WORD	$0x0e3412c7                 // saddw.8h	v7, v22, v20
	WORD	$0x4e3412f4                 // saddw2.8h	v20, v23, v20
	WORD	$0x0e2148e7                 // sqxtn.8b	v7, v7
	WORD	$0x4e214a87                 // sqxtn2.16b	v7, v20
	WORD	$0x4e271c00                 // and.16b	v0, v0, v7
	WORD	$0x4e611c07                 // bic.16b	v7, v0, v1
	WORD	$0x4f00e494                 // movi.16b	v20, #0x4
	WORD	$0x4e340cf4                 // sqadd.16b	v20, v7, v20
	WORD	$0x4e350ce7                 // sqadd.16b	v7, v7, v21
	WORD	$0x4f0d0694                 // sshr.16b	v20, v20, #0x3
	WORD	$0x4f0d04e7                 // sshr.16b	v7, v7, #0x3
	WORD	$0x4e342e10                 // sqsub.16b	v16, v16, v20
	WORD	$0x4e270cc6                 // sqadd.16b	v6, v6, v7
	WORD	$0x4e201c20                 // and.16b	v0, v1, v0
	WORD	$0x4f00e521                 // movi.16b	v1, #0x9
	WORD	$0x0f00e527                 // movi.8b	v7, #0x9
	WORD	$0x4f0187f4                 // movi.8h	v20, #0x3f
	WORD	$0x4f0187f5                 // movi.8h	v21, #0x3f
	WORD	$0x0e278015                 // smlal.8h	v21, v0, v7
	WORD	$0x4f0187e7                 // movi.8h	v7, #0x3f
	WORD	$0x4e218007                 // smlal2.8h	v7, v0, v1
	WORD	$0x4f00e641                 // movi.16b	v1, #0x12
	WORD	$0x0f00e656                 // movi.8b	v22, #0x12
	WORD	$0x4f0187f7                 // movi.8h	v23, #0x3f
	WORD	$0x0e368017                 // smlal.8h	v23, v0, v22
	WORD	$0x4f0187f6                 // movi.8h	v22, #0x3f
	WORD	$0x4e218016                 // smlal2.8h	v22, v0, v1
	WORD	$0x4f00e761                 // movi.16b	v1, #0x1b
	WORD	$0x0f00e778                 // movi.8b	v24, #0x1b
	WORD	$0x4f0187f9                 // movi.8h	v25, #0x3f
	WORD	$0x0e388019                 // smlal.8h	v25, v0, v24
	WORD	$0x4e218014                 // smlal2.8h	v20, v0, v1
	WORD	$0x0f0996a0                 // sqshrn.8b	v0, v21, #0x7
	WORD	$0x0f0996e1                 // sqshrn.8b	v1, v23, #0x7
	WORD	$0x0f099735                 // sqshrn.8b	v21, v25, #0x7
	WORD	$0x4f0994e0                 // sqshrn2.16b	v0, v7, #0x7
	WORD	$0x4f0996c1                 // sqshrn2.16b	v1, v22, #0x7
	WORD	$0x4f099695                 // sqshrn2.16b	v21, v20, #0x7
	WORD	$0x4e202e47                 // sqsub.16b	v7, v18, v0
	WORD	$0x4e200e20                 // sqadd.16b	v0, v17, v0
	WORD	$0x4e212ca5                 // sqsub.16b	v5, v5, v1
	WORD	$0x4e210e61                 // sqadd.16b	v1, v19, v1
	WORD	$0x4e352e10                 // sqsub.16b	v16, v16, v21
	WORD	$0x4e350cc6                 // sqadd.16b	v6, v6, v21
	WORD	$0x6e221e10                 // eor.16b	v16, v16, v2
	WORD	$0x4e902891                 // trn1.4s	v17, v4, v16
	WORD	$0x4e906884                 // trn2.4s	v4, v4, v16
	WORD	$0x6e221c00                 // eor.16b	v0, v0, v2
	WORD	$0x6e221ca5                 // eor.16b	v5, v5, v2
	WORD	$0x4e852810                 // trn1.4s	v16, v0, v5
	WORD	$0x4e856800                 // trn2.4s	v0, v0, v5
	WORD	$0x6e221c21                 // eor.16b	v1, v1, v2
	WORD	$0x6e221ce5                 // eor.16b	v5, v7, v2
	WORD	$0x4e852827                 // trn1.4s	v7, v1, v5
	WORD	$0x4e856821                 // trn2.4s	v1, v1, v5
	WORD	$0x6e221cc2                 // eor.16b	v2, v6, v2
	WORD	$0x4e832845                 // trn1.4s	v5, v2, v3
	WORD	$0x4e836842                 // trn2.4s	v2, v2, v3
	WORD	$0x4e472a23                 // trn1.8h	v3, v17, v7
	WORD	$0x4e476a26                 // trn2.8h	v6, v17, v7
	WORD	$0x4e452a07                 // trn1.8h	v7, v16, v5
	WORD	$0x4e456a05                 // trn2.8h	v5, v16, v5
	WORD	$0x4e412890                 // trn1.8h	v16, v4, v1
	WORD	$0x4e416881                 // trn2.8h	v1, v4, v1
	WORD	$0x4e422804                 // trn1.8h	v4, v0, v2
	WORD	$0x4e072871                 // trn1.16b	v17, v3, v7
	WORD	$0x4e076863                 // trn2.16b	v3, v3, v7
	WORD	$0xfd000171                 // str	d17, [x11]
	WORD	$0x6e114227                 // ext.16b	v7, v17, v17, #0x8
	WORD	$0xfd000127                 // str	d7, [x9]
	WORD	$0x8b08016a                 // add	x10, x11, x8
	WORD	$0x8b08014b                 // add	x11, x10, x8
	WORD	$0xfd000143                 // str	d3, [x10]
	WORD	$0x6e034063                 // ext.16b	v3, v3, v3, #0x8
	WORD	$0x8b080129                 // add	x9, x9, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd000123                 // str	d3, [x9]
	WORD	$0x4e426800                 // trn2.8h	v0, v0, v2
	WORD	$0x4e0528c2                 // trn1.16b	v2, v6, v5
	WORD	$0xfd000162                 // str	d2, [x11]
	WORD	$0x6e024042                 // ext.16b	v2, v2, v2, #0x8
	WORD	$0xfd000142                 // str	d2, [x10]
	WORD	$0x4e0568c2                 // trn2.16b	v2, v6, v5
	WORD	$0x4e042a03                 // trn1.16b	v3, v16, v4
	WORD	$0x4e046a04                 // trn2.16b	v4, v16, v4
	WORD	$0x4e002825                 // trn1.16b	v5, v1, v0
	WORD	$0x8b080169                 // add	x9, x11, x8
	WORD	$0x8b08012b                 // add	x11, x9, x8
	WORD	$0xfd000122                 // str	d2, [x9]
	WORD	$0x6e024042                 // ext.16b	v2, v2, v2, #0x8
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd000122                 // str	d2, [x9]
	WORD	$0xfd000163                 // str	d3, [x11]
	WORD	$0x6e034062                 // ext.16b	v2, v3, v3, #0x8
	WORD	$0xfd000142                 // str	d2, [x10]
	WORD	$0x4e006820                 // trn2.16b	v0, v1, v0
	WORD	$0x8b080169                 // add	x9, x11, x8
	WORD	$0x8b08012b                 // add	x11, x9, x8
	WORD	$0xfd000124                 // str	d4, [x9]
	WORD	$0x6e044081                 // ext.16b	v1, v4, v4, #0x8
	WORD	$0x8b080149                 // add	x9, x10, x8
	WORD	$0x8b08012a                 // add	x10, x9, x8
	WORD	$0xfd000121                 // str	d1, [x9]
	WORD	$0xfd000165                 // str	d5, [x11]
	WORD	$0x6e0540a1                 // ext.16b	v1, v5, v5, #0x8
	WORD	$0xfd000141                 // str	d1, [x10]
	WORD	$0xfc286960                 // str	d0, [x11, x8]
	WORD	$0x6e004000                 // ext.16b	v0, v0, v0, #0x8
	WORD	$0xfc286940                 // str	d0, [x10, x8]

	RET

// loopFilterSimpleEdgeH16NEON ABI ($0-17, no stack):
//   src+0(FP)     *byte (points at p1 row, 16 contiguous bytes)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//
// Mirror of libvpx vp8_loop_filter_simple_horizontal_edge_neon. Reads
// p1 (row0), p0 (row1), q0 (row2), q1 (row3) at +pitch increments and
// writes p0 and q0 back. Encodings produced by clang -O3 -mcpu=apple-m4
// from a hand-written intrinsics translation.
TEXT ·loopFilterSimpleEdgeH16NEON(SB), NOSPLIT, $0-17
	MOVD	src+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2

	WORD	$0x4e010c40                 // dup.16b v0, w2
	WORD	$0x3dc00001                 // ldr q1, [x0]
	WORD	$0x93407c28                 // sxtw x8, w1
	WORD	$0x3ce86802                 // ldr q2, [x0, x8]
	WORD	$0x937f7c29                 // sbfiz x9, x1, #1, #32
	WORD	$0x3ce96803                 // ldr q3, [x0, x9]
	WORD	$0x8b08012a                 // add x10, x9, x8
	WORD	$0x3cea6804                 // ldr q4, [x0, x10]
	WORD	$0x6e237445                 // uabd.16b v5, v2, v3
	WORD	$0x6e247426                 // uabd.16b v6, v1, v4
	WORD	$0x6e250ca5                 // uqadd.16b v5, v5, v5
	WORD	$0x6f0f04c6                 // ushr.16b v6, v6, #0x1
	WORD	$0x6e260ca5                 // uqadd.16b v5, v5, v6
	WORD	$0x4f04e406                 // movi.16b v6, #0x80
	WORD	$0x6e261c21                 // eor.16b v1, v1, v6
	WORD	$0x6e261c42                 // eor.16b v2, v2, v6
	WORD	$0x6e261c63                 // eor.16b v3, v3, v6
	WORD	$0x6e261c84                 // eor.16b v4, v4, v6
	WORD	$0x0f00e467                 // movi.8b v7, #0x3
	WORD	$0x0e27c070                 // smull.8h v16, v3, v7
	WORD	$0x0e27a050                 // smlsl.8h v16, v2, v7
	WORD	$0x4f00e467                 // movi.16b v7, #0x3
	WORD	$0x4e27c071                 // smull2.8h v17, v3, v7
	WORD	$0x4e27a051                 // smlsl2.8h v17, v2, v7
	WORD	$0x4e242c21                 // sqsub.16b v1, v1, v4
	WORD	$0x0e211204                 // saddw.8h v4, v16, v1
	WORD	$0x4e211221                 // saddw2.8h v1, v17, v1
	WORD	$0x0e214884                 // sqxtn.8b v4, v4
	WORD	$0x4e214824                 // sqxtn2.16b v4, v1
	WORD	$0x6e253c00                 // cmhs.16b v0, v0, v5
	WORD	$0x4e241c00                 // and.16b v0, v0, v4
	WORD	$0x4f00e481                 // movi.16b v1, #0x4
	WORD	$0x4e210c01                 // sqadd.16b v1, v0, v1
	WORD	$0x4f0d0421                 // sshr.16b v1, v1, #0x3
	WORD	$0x4e270c00                 // sqadd.16b v0, v0, v7
	WORD	$0x4f0d0400                 // sshr.16b v0, v0, #0x3
	WORD	$0x4e200c40                 // sqadd.16b v0, v2, v0
	WORD	$0x4e212c61                 // sqsub.16b v1, v3, v1
	WORD	$0x6e261c00                 // eor.16b v0, v0, v6
	WORD	$0x6e261c21                 // eor.16b v1, v1, v6
	WORD	$0x3ca86800                 // str q0, [x0, x8]   ; p0
	WORD	$0x3ca96801                 // str q1, [x0, x9]   ; q0

	RET

// loopFilterSimpleEdgeV16NEON ABI ($0-17, no stack):
//   src+0(FP)     *byte (points at q0 column of row 0)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//
// Mirror of libvpx vp8_loop_filter_simple_vertical_edge_neon. Loads 4
// bytes (p1,p0,q0,q1) per row at src-2 across 16 rows via VLD4, applies
// the same kernel as the horizontal variant, and writes 2 modified
// bytes (p0,q0) per row at src-1 across 16 rows via VST2. Encodings
// produced by clang -O3 -mcpu=apple-m4 from a hand-written intrinsics
// translation.
TEXT ·loopFilterSimpleEdgeV16NEON(SB), NOSPLIT, $0-17
	MOVD	src+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2

	WORD	$0xd1000809                 // sub x9, x0, #0x2
	WORD	$0x6f00e400                 // movi.2d v0, #0
	WORD	$0x4ea01c01                 // mov.16b v1, v0
	WORD	$0x4ea01c02                 // mov.16b v2, v0
	WORD	$0x4ea01c03                 // mov.16b v3, v0
	WORD	$0x93407c28                 // sxtw x8, w1
	WORD	$0x4ea01c04                 // mov.16b v4, v0
	WORD	$0x4ea11c25                 // mov.16b v5, v1
	WORD	$0x4ea21c46                 // mov.16b v6, v2
	WORD	$0x4ea31c67                 // mov.16b v7, v3
	WORD	$0x0de82124                 // ld4.b { v4-v7 }[0], [x9], x8
	WORD	$0x0de82524                 // ld4.b { v4-v7 }[1], [x9], x8
	WORD	$0x0de82924                 // ld4.b { v4-v7 }[2], [x9], x8
	WORD	$0x0de82d24                 // ld4.b { v4-v7 }[3], [x9], x8
	WORD	$0x0de83124                 // ld4.b { v4-v7 }[4], [x9], x8
	WORD	$0x0de83524                 // ld4.b { v4-v7 }[5], [x9], x8
	WORD	$0x0de83924                 // ld4.b { v4-v7 }[6], [x9], x8
	WORD	$0x0de83d24                 // ld4.b { v4-v7 }[7], [x9], x8
	WORD	$0x0de82120                 // ld4.b { v0-v3 }[0], [x9], x8
	WORD	$0x0de82520                 // ld4.b { v0-v3 }[1], [x9], x8
	WORD	$0x0de82920                 // ld4.b { v0-v3 }[2], [x9], x8
	WORD	$0x0de82d20                 // ld4.b { v0-v3 }[3], [x9], x8
	WORD	$0x0de83120                 // ld4.b { v0-v3 }[4], [x9], x8
	WORD	$0x0de83520                 // ld4.b { v0-v3 }[5], [x9], x8
	WORD	$0x0de83920                 // ld4.b { v0-v3 }[6], [x9], x8
	WORD	$0x0d603d20                 // ld4.b { v0-v3 }[7], [x9]
	WORD	$0x4e010c50                 // dup.16b v16, w2
	WORD	$0x6e180404                 // mov.d v4[1], v0[0]
	WORD	$0x4ea51cb1                 // mov.16b v17, v5
	WORD	$0x6e180431                 // mov.d v17[1], v1[0]
	WORD	$0x4ea61cd2                 // mov.16b v18, v6
	WORD	$0x6e180452                 // mov.d v18[1], v2[0]
	WORD	$0x6e180467                 // mov.d v7[1], v3[0]
	WORD	$0x6e327633                 // uabd.16b v19, v17, v18
	WORD	$0x6e277494                 // uabd.16b v20, v4, v7
	WORD	$0x6e330e73                 // uqadd.16b v19, v19, v19
	WORD	$0x6f0f0694                 // ushr.16b v20, v20, #0x1
	WORD	$0x6e340e73                 // uqadd.16b v19, v19, v20
	WORD	$0x4f04e414                 // movi.16b v20, #0x80
	WORD	$0x6e341c95                 // eor.16b v21, v4, v20
	WORD	$0x6e341e31                 // eor.16b v17, v17, v20
	WORD	$0x6e341e52                 // eor.16b v18, v18, v20
	WORD	$0x6e341cf6                 // eor.16b v22, v7, v20
	WORD	$0x0f04e417                 // movi.8b v23, #0x80
	WORD	$0x2e371cd8                 // eor.8b v24, v6, v23
	WORD	$0x2e371ca4                 // eor.8b v4, v5, v23
	WORD	$0x2e371c45                 // eor.8b v5, v2, v23
	WORD	$0x2e371c20                 // eor.8b v0, v1, v23
	WORD	$0x0f00e461                 // movi.8b v1, #0x3
	WORD	$0x0e21c302                 // smull.8h v2, v24, v1
	WORD	$0x0e21a082                 // smlsl.8h v2, v4, v1
	WORD	$0x0e21c0a3                 // smull.8h v3, v5, v1
	WORD	$0x0e21a003                 // smlsl.8h v3, v0, v1
	WORD	$0x4e362ea0                 // sqsub.16b v0, v21, v22
	WORD	$0x0e201041                 // saddw.8h v1, v2, v0
	WORD	$0x4e201060                 // saddw2.8h v0, v3, v0
	WORD	$0x0e214821                 // sqxtn.8b v1, v1
	WORD	$0x4e214801                 // sqxtn2.16b v1, v0
	WORD	$0x6e333e00                 // cmhs.16b v0, v16, v19
	WORD	$0x4e211c00                 // and.16b v0, v0, v1
	WORD	$0x4f00e481                 // movi.16b v1, #0x4
	WORD	$0x4e210c01                 // sqadd.16b v1, v0, v1
	WORD	$0x4f0d0421                 // sshr.16b v1, v1, #0x3
	WORD	$0x4f00e462                 // movi.16b v2, #0x3
	WORD	$0x4e220c00                 // sqadd.16b v0, v0, v2
	WORD	$0x4f0d0400                 // sshr.16b v0, v0, #0x3
	WORD	$0x4e200e20                 // sqadd.16b v0, v17, v0
	WORD	$0x4e212e41                 // sqsub.16b v1, v18, v1
	WORD	$0x6e341c02                 // eor.16b v2, v0, v20
	WORD	$0x6e341c23                 // eor.16b v3, v1, v20
	WORD	$0x6e024040                 // ext.16b v0, v2, v2, #0x8
	WORD	$0x6e034061                 // ext.16b v1, v3, v3, #0x8
	WORD	$0xd1000409                 // sub x9, x0, #0x1
	WORD	$0x0da80122                 // st2.b { v2, v3 }[0], [x9], x8
	WORD	$0x0da80522                 // st2.b { v2, v3 }[1], [x9], x8
	WORD	$0x0da80922                 // st2.b { v2, v3 }[2], [x9], x8
	WORD	$0x0da80d22                 // st2.b { v2, v3 }[3], [x9], x8
	WORD	$0x0da81122                 // st2.b { v2, v3 }[4], [x9], x8
	WORD	$0x0da81522                 // st2.b { v2, v3 }[5], [x9], x8
	WORD	$0x0da81922                 // st2.b { v2, v3 }[6], [x9], x8
	WORD	$0x0da81d22                 // st2.b { v2, v3 }[7], [x9], x8
	WORD	$0x0da80120                 // st2.b { v0, v1 }[0], [x9], x8
	WORD	$0x0da80520                 // st2.b { v0, v1 }[1], [x9], x8
	WORD	$0x0da80920                 // st2.b { v0, v1 }[2], [x9], x8
	WORD	$0x0da80d20                 // st2.b { v0, v1 }[3], [x9], x8
	WORD	$0x0da81120                 // st2.b { v0, v1 }[4], [x9], x8
	WORD	$0x0da81520                 // st2.b { v0, v1 }[5], [x9], x8
	WORD	$0x0da81920                 // st2.b { v0, v1 }[6], [x9], x8
	WORD	$0x0d201d20                 // st2.b { v0, v1 }[7], [x9]

	RET
