//go:build arm64 && !purego

// ARMv8 NEON port of libvpx v1.16.0 vpx_dsp/arm/fdct8x8_neon.c
// vpx_fdct8x8_neon. Instruction WORDs are generated from the
// local libvpx oracle object so the arithmetic stays source-identical.

#include "textflag.h"

// forwardDCT8x8NEON ABI ($0-24):
//   input+0(FP)   *int16
//   output+8(FP)  *int16
//   stride+16(FP) int (in int16 elements)
TEXT ·forwardDCT8x8NEON(SB), NOSPLIT, $0-24
	MOVD	input+0(FP), R0
	MOVD	output+8(FP), R1
	MOVD	stride+16(FP), R2
	WORD	$0x3dc00000	// ldr	q0, [x0]
	WORD	$0x4f125400	// shl.8h	v0, v0, #0x2
	WORD	$0x937f7c48	// sbfiz	x8, x2, #1, #32
	WORD	$0x3ce86801	// ldr	q1, [x0, x8]
	WORD	$0x937e7c48	// sbfiz	x8, x2, #2, #32
	WORD	$0x3ce86802	// ldr	q2, [x0, x8]
	WORD	$0x4f125421	// shl.8h	v1, v1, #0x2
	WORD	$0x4f125442	// shl.8h	v2, v2, #0x2
	WORD	$0x528000c8	// mov	w8, #0x6                ; =6
	WORD	$0x9b287c48	// smull	x8, w2, w8
	WORD	$0x3ce86803	// ldr	q3, [x0, x8]
	WORD	$0x4f125463	// shl.8h	v3, v3, #0x2
	WORD	$0x937d7c48	// sbfiz	x8, x2, #3, #32
	WORD	$0x3ce86804	// ldr	q4, [x0, x8]
	WORD	$0x4f125484	// shl.8h	v4, v4, #0x2
	WORD	$0x52800148	// mov	w8, #0xa                ; =10
	WORD	$0x9b287c48	// smull	x8, w2, w8
	WORD	$0x3ce86805	// ldr	q5, [x0, x8]
	WORD	$0x4f1254a5	// shl.8h	v5, v5, #0x2
	WORD	$0x52800188	// mov	w8, #0xc                ; =12
	WORD	$0x9b287c48	// smull	x8, w2, w8
	WORD	$0x3ce86806	// ldr	q6, [x0, x8]
	WORD	$0x528001c8	// mov	w8, #0xe                ; =14
	WORD	$0x9b287c48	// smull	x8, w2, w8
	WORD	$0x3ce86807	// ldr	q7, [x0, x8]
	WORD	$0x4f1254c6	// shl.8h	v6, v6, #0x2
	WORD	$0x4f1254e7	// shl.8h	v7, v7, #0x2
	WORD	$0x4e6084f0	// add.8h	v16, v7, v0
	WORD	$0x4e6184d1	// add.8h	v17, v6, v1
	WORD	$0x4e6284b2	// add.8h	v18, v5, v2
	WORD	$0x4e638493	// add.8h	v19, v4, v3
	WORD	$0x6e648463	// sub.8h	v3, v3, v4
	WORD	$0x6e658442	// sub.8h	v2, v2, v5
	WORD	$0x6e668425	// sub.8h	v5, v1, v6
	WORD	$0x6e678400	// sub.8h	v0, v0, v7
	WORD	$0x4e738601	// add.8h	v1, v16, v19
	WORD	$0x4e728624	// add.8h	v4, v17, v18
	WORD	$0x6e728631	// sub.8h	v17, v17, v18
	WORD	$0x6e738610	// sub.8h	v16, v16, v19
	WORD	$0x4e648426	// add.8h	v6, v1, v4
	WORD	$0x528b5048	// mov	w8, #0x5a82             ; =23170
	WORD	$0x4e020d12	// dup.8h	v18, w8
	WORD	$0x6e72b4c7	// sqrdmulh.8h	v7, v6, v18
	WORD	$0x6e648421	// sub.8h	v1, v1, v4
	WORD	$0x6e72b426	// sqrdmulh.8h	v6, v1, v18
	WORD	$0x52876428	// mov	w8, #0x3b21             ; =15137
	WORD	$0x4e020d01	// dup.8h	v1, w8
	WORD	$0x52830fc8	// mov	w8, #0x187e             ; =6270
	WORD	$0x4e020d04	// dup.8h	v4, w8
	WORD	$0x0e64c213	// smull.4s	v19, v16, v4
	WORD	$0x4e64c214	// smull2.4s	v20, v16, v4
	WORD	$0x0e64c235	// smull.4s	v21, v17, v4
	WORD	$0x0e618215	// smlal.4s	v21, v16, v1
	WORD	$0x4e64c236	// smull2.4s	v22, v17, v4
	WORD	$0x4e618216	// smlal2.4s	v22, v16, v1
	WORD	$0x0e61a233	// smlsl.4s	v19, v17, v1
	WORD	$0x4e61a234	// smlsl2.4s	v20, v17, v1
	WORD	$0x0f129eb1	// sqrshrn.4h	v17, v21, #0xe
	WORD	$0x0f129e70	// sqrshrn.4h	v16, v19, #0xe
	WORD	$0x4f129ed1	// sqrshrn2.8h	v17, v22, #0xe
	WORD	$0x4f129e90	// sqrshrn2.8h	v16, v20, #0xe
	WORD	$0x4e6284b3	// add.8h	v19, v5, v2
	WORD	$0x6e72b673	// sqrdmulh.8h	v19, v19, v18
	WORD	$0x6e6284a2	// sub.8h	v2, v5, v2
	WORD	$0x6e72b442	// sqrdmulh.8h	v2, v2, v18
	WORD	$0x4e638445	// add.8h	v5, v2, v3
	WORD	$0x6e628472	// sub.8h	v18, v3, v2
	WORD	$0x6e738414	// sub.8h	v20, v0, v19
	WORD	$0x4e608662	// add.8h	v2, v19, v0
	WORD	$0x5287d8a8	// mov	w8, #0x3ec5             ; =16069
	WORD	$0x4e020d00	// dup.8h	v0, w8
	WORD	$0x52818f88	// mov	w8, #0xc7c              ; =3196
	WORD	$0x4e020d03	// dup.8h	v3, w8
	WORD	$0x0e63c053	// smull.4s	v19, v2, v3
	WORD	$0x4e63c055	// smull2.4s	v21, v2, v3
	WORD	$0x0e63c0b6	// smull.4s	v22, v5, v3
	WORD	$0x0e608056	// smlal.4s	v22, v2, v0
	WORD	$0x4e63c0b7	// smull2.4s	v23, v5, v3
	WORD	$0x4e608057	// smlal2.4s	v23, v2, v0
	WORD	$0x0e60a0b3	// smlsl.4s	v19, v5, v0
	WORD	$0x4e60a0b5	// smlsl2.4s	v21, v5, v0
	WORD	$0x0f129ed6	// sqrshrn.4h	v22, v22, #0xe
	WORD	$0x0f129e73	// sqrshrn.4h	v19, v19, #0xe
	WORD	$0x4f129ef6	// sqrshrn2.8h	v22, v23, #0xe
	WORD	$0x4f129eb3	// sqrshrn2.8h	v19, v21, #0xe
	WORD	$0x528471c8	// mov	w8, #0x238e             ; =9102
	WORD	$0x4e020d02	// dup.8h	v2, w8
	WORD	$0x5286a6e8	// mov	w8, #0x3537             ; =13623
	WORD	$0x4e020d05	// dup.8h	v5, w8
	WORD	$0x0e65c295	// smull.4s	v21, v20, v5
	WORD	$0x4e65c297	// smull2.4s	v23, v20, v5
	WORD	$0x0e65c258	// smull.4s	v24, v18, v5
	WORD	$0x0e628298	// smlal.4s	v24, v20, v2
	WORD	$0x4e65c259	// smull2.4s	v25, v18, v5
	WORD	$0x4e628299	// smlal2.4s	v25, v20, v2
	WORD	$0x0e62a255	// smlsl.4s	v21, v18, v2
	WORD	$0x4e62a257	// smlsl2.4s	v23, v18, v2
	WORD	$0x0f129f12	// sqrshrn.4h	v18, v24, #0xe
	WORD	$0x0f129eb4	// sqrshrn.4h	v20, v21, #0xe
	WORD	$0x4f129f32	// sqrshrn2.8h	v18, v25, #0xe
	WORD	$0x4f129ef4	// sqrshrn2.8h	v20, v23, #0xe
	WORD	$0x4e5628f5	// trn1.8h	v21, v7, v22
	WORD	$0x4e5668e7	// trn2.8h	v7, v7, v22
	WORD	$0x4e542a36	// trn1.8h	v22, v17, v20
	WORD	$0x4e546a31	// trn2.8h	v17, v17, v20
	WORD	$0x4e5228d4	// trn1.8h	v20, v6, v18
	WORD	$0x4e5268c6	// trn2.8h	v6, v6, v18
	WORD	$0x4e532a12	// trn1.8h	v18, v16, v19
	WORD	$0x4e536a10	// trn2.8h	v16, v16, v19
	WORD	$0x4e962ab3	// trn1.4s	v19, v21, v22
	WORD	$0x4e966ab5	// trn2.4s	v21, v21, v22
	WORD	$0x4e9128f6	// trn1.4s	v22, v7, v17
	WORD	$0x4e9168e7	// trn2.4s	v7, v7, v17
	WORD	$0x4e922a91	// trn1.4s	v17, v20, v18
	WORD	$0x4e926a92	// trn2.4s	v18, v20, v18
	WORD	$0x4e9028d4	// trn1.4s	v20, v6, v16
	WORD	$0x4ed17a77	// zip2.2d	v23, v19, v17
	WORD	$0x6e180633	// mov.d	v19[1], v17[0]
	WORD	$0x4e9068c6	// trn2.4s	v6, v6, v16
	WORD	$0x4ed47ad0	// zip2.2d	v16, v22, v20
	WORD	$0x6e180696	// mov.d	v22[1], v20[0]
	WORD	$0x4ed27ab1	// zip2.2d	v17, v21, v18
	WORD	$0x6e180655	// mov.d	v21[1], v18[0]
	WORD	$0x4ea71cf2	// mov.16b	v18, v7
	WORD	$0x6e1804d2	// mov.d	v18[1], v6[0]
	WORD	$0x4ec678e6	// zip2.2d	v6, v7, v6
	WORD	$0x4e7384c7	// add.8h	v7, v6, v19
	WORD	$0x4e768634	// add.8h	v20, v17, v22
	WORD	$0x4e758618	// add.8h	v24, v16, v21
	WORD	$0x4e7286f9	// add.8h	v25, v23, v18
	WORD	$0x6e778652	// sub.8h	v18, v18, v23
	WORD	$0x6e7086b0	// sub.8h	v16, v21, v16
	WORD	$0x6e7186d1	// sub.8h	v17, v22, v17
	WORD	$0x6e668673	// sub.8h	v19, v19, v6
	WORD	$0x4e678726	// add.8h	v6, v25, v7
	WORD	$0x4e748715	// add.8h	v21, v24, v20
	WORD	$0x6e788694	// sub.8h	v20, v20, v24
	WORD	$0x6e7984f6	// sub.8h	v22, v7, v25
	WORD	$0x0e7500c7	// saddl.4s	v7, v6, v21
	WORD	$0x52ab5048	// mov	w8, #0x5a820000         ; =1518469120
	WORD	$0x4e040d17	// dup.4s	v23, w8
	WORD	$0x6eb7b4e7	// sqrdmulh.4s	v7, v7, v23
	WORD	$0x4e7500d8	// saddl2.4s	v24, v6, v21
	WORD	$0x6eb7b718	// sqrdmulh.4s	v24, v24, v23
	WORD	$0x0e7520d9	// ssubl.4s	v25, v6, v21
	WORD	$0x6eb7b739	// sqrdmulh.4s	v25, v25, v23
	WORD	$0x4e7520c6	// ssubl2.4s	v6, v6, v21
	WORD	$0x6eb7b4c6	// sqrdmulh.4s	v6, v6, v23
	WORD	$0x4e5818e7	// uzp1.8h	v7, v7, v24
	WORD	$0x4e461b26	// uzp1.8h	v6, v25, v6
	WORD	$0x0e64c2d5	// smull.4s	v21, v22, v4
	WORD	$0x4e64c2d8	// smull2.4s	v24, v22, v4
	WORD	$0x0e64c299	// smull.4s	v25, v20, v4
	WORD	$0x0e6182d9	// smlal.4s	v25, v22, v1
	WORD	$0x4e64c29a	// smull2.4s	v26, v20, v4
	WORD	$0x4e6182da	// smlal2.4s	v26, v22, v1
	WORD	$0x0e61a295	// smlsl.4s	v21, v20, v1
	WORD	$0x4e61a298	// smlsl2.4s	v24, v20, v1
	WORD	$0x0f129f24	// sqrshrn.4h	v4, v25, #0xe
	WORD	$0x0f129ea1	// sqrshrn.4h	v1, v21, #0xe
	WORD	$0x4f129f44	// sqrshrn2.8h	v4, v26, #0xe
	WORD	$0x4f129f01	// sqrshrn2.8h	v1, v24, #0xe
	WORD	$0x0e710214	// saddl.4s	v20, v16, v17
	WORD	$0x6eb7b694	// sqrdmulh.4s	v20, v20, v23
	WORD	$0x4e710215	// saddl2.4s	v21, v16, v17
	WORD	$0x6eb7b6b5	// sqrdmulh.4s	v21, v21, v23
	WORD	$0x0e702236	// ssubl.4s	v22, v17, v16
	WORD	$0x6eb7b6d6	// sqrdmulh.4s	v22, v22, v23
	WORD	$0x4e702230	// ssubl2.4s	v16, v17, v16
	WORD	$0x6eb7b610	// sqrdmulh.4s	v16, v16, v23
	WORD	$0x4e551a91	// uzp1.8h	v17, v20, v21
	WORD	$0x4e501ad0	// uzp1.8h	v16, v22, v16
	WORD	$0x4e708654	// add.8h	v20, v18, v16
	WORD	$0x6e708650	// sub.8h	v16, v18, v16
	WORD	$0x6e718672	// sub.8h	v18, v19, v17
	WORD	$0x4e718671	// add.8h	v17, v19, v17
	WORD	$0x0e63c233	// smull.4s	v19, v17, v3
	WORD	$0x4e63c235	// smull2.4s	v21, v17, v3
	WORD	$0x0e63c296	// smull.4s	v22, v20, v3
	WORD	$0x0e608236	// smlal.4s	v22, v17, v0
	WORD	$0x4e63c283	// smull2.4s	v3, v20, v3
	WORD	$0x4e608223	// smlal2.4s	v3, v17, v0
	WORD	$0x0e60a293	// smlsl.4s	v19, v20, v0
	WORD	$0x4e60a295	// smlsl2.4s	v21, v20, v0
	WORD	$0x0f129ec0	// sqrshrn.4h	v0, v22, #0xe
	WORD	$0x0f129e71	// sqrshrn.4h	v17, v19, #0xe
	WORD	$0x4f129c60	// sqrshrn2.8h	v0, v3, #0xe
	WORD	$0x4f129eb1	// sqrshrn2.8h	v17, v21, #0xe
	WORD	$0x0e65c243	// smull.4s	v3, v18, v5
	WORD	$0x4e65c253	// smull2.4s	v19, v18, v5
	WORD	$0x0e65c214	// smull.4s	v20, v16, v5
	WORD	$0x0e628254	// smlal.4s	v20, v18, v2
	WORD	$0x4e65c205	// smull2.4s	v5, v16, v5
	WORD	$0x4e628245	// smlal2.4s	v5, v18, v2
	WORD	$0x0e62a203	// smlsl.4s	v3, v16, v2
	WORD	$0x4e62a213	// smlsl2.4s	v19, v16, v2
	WORD	$0x0f129e82	// sqrshrn.4h	v2, v20, #0xe
	WORD	$0x0f129c63	// sqrshrn.4h	v3, v3, #0xe
	WORD	$0x4f129ca2	// sqrshrn2.8h	v2, v5, #0xe
	WORD	$0x4f129e63	// sqrshrn2.8h	v3, v19, #0xe
	WORD	$0x4e4028e5	// trn1.8h	v5, v7, v0
	WORD	$0x4e4068e0	// trn2.8h	v0, v7, v0
	WORD	$0x4e432887	// trn1.8h	v7, v4, v3
	WORD	$0x4e436883	// trn2.8h	v3, v4, v3
	WORD	$0x4e4228c4	// trn1.8h	v4, v6, v2
	WORD	$0x4e4268c2	// trn2.8h	v2, v6, v2
	WORD	$0x4e512826	// trn1.8h	v6, v1, v17
	WORD	$0x4e516821	// trn2.8h	v1, v1, v17
	WORD	$0x4e8728b0	// trn1.4s	v16, v5, v7
	WORD	$0x4e8768a5	// trn2.4s	v5, v5, v7
	WORD	$0x4e832807	// trn1.4s	v7, v0, v3
	WORD	$0x4e836800	// trn2.4s	v0, v0, v3
	WORD	$0x4e862883	// trn1.4s	v3, v4, v6
	WORD	$0x4e866884	// trn2.4s	v4, v4, v6
	WORD	$0x4e812846	// trn1.4s	v6, v2, v1
	WORD	$0x4e816841	// trn2.4s	v1, v2, v1
	WORD	$0x4ec37a02	// zip2.2d	v2, v16, v3
	WORD	$0x6e180470	// mov.d	v16[1], v3[0]
	WORD	$0x4ec678e3	// zip2.2d	v3, v7, v6
	WORD	$0x6e1804c7	// mov.d	v7[1], v6[0]
	WORD	$0x4ec478a6	// zip2.2d	v6, v5, v4
	WORD	$0x6e180485	// mov.d	v5[1], v4[0]
	WORD	$0x4ec17804	// zip2.2d	v4, v0, v1
	WORD	$0x6e180420	// mov.d	v0[1], v1[0]
	WORD	$0x4e60aa01	// cmlt.8h	v1, v16, #0
	WORD	$0x4e60a8f1	// cmlt.8h	v17, v7, #0
	WORD	$0x4e60a8b2	// cmlt.8h	v18, v5, #0
	WORD	$0x4e60a813	// cmlt.8h	v19, v0, #0
	WORD	$0x4e60a854	// cmlt.8h	v20, v2, #0
	WORD	$0x4e60a875	// cmlt.8h	v21, v3, #0
	WORD	$0x4e60a8d6	// cmlt.8h	v22, v6, #0
	WORD	$0x4e612601	// shsub.8h	v1, v16, v1
	WORD	$0x4e7124e7	// shsub.8h	v7, v7, v17
	WORD	$0xad001c21	// stp	q1, q7, [x1]
	WORD	$0x4e60a881	// cmlt.8h	v1, v4, #0
	WORD	$0x4e7224a5	// shsub.8h	v5, v5, v18
	WORD	$0x4e732400	// shsub.8h	v0, v0, v19
	WORD	$0x4e742442	// shsub.8h	v2, v2, v20
	WORD	$0x4e752463	// shsub.8h	v3, v3, v21
	WORD	$0x4e7624c6	// shsub.8h	v6, v6, v22
	WORD	$0xad010025	// stp	q5, q0, [x1, #0x20]
	WORD	$0xad020c22	// stp	q2, q3, [x1, #0x40]
	WORD	$0x4e612480	// shsub.8h	v0, v4, v1
	WORD	$0xad030026	// stp	q6, q0, [x1, #0x60]
	RET
