//go:build arm64 && !purego

// ARMv8 NEON port of libvpx v1.16.0 vp9/encoder/arm/neon/vp9_dct_neon.c
// vp9_fht8x8_neon hybrid-transform path. Instruction WORDs are
// generated from a local -fno-stack-protector build of the pinned libvpx source.

#include "textflag.h"

// forwardHT8x8NEON ABI ($0-32):
//   input+0(FP)   *int16
//   output+8(FP)  *int16
//   stride+16(FP) int (in int16 elements)
//   txType+24(FP) int (ADST_DCT, DCT_ADST, ADST_ADST only)
TEXT ·forwardHT8x8NEON(SB), NOSPLIT, $0-32
	MOVD	input+0(FP), R0
	MOVD	output+8(FP), R1
	MOVD	stride+16(FP), R2
	MOVD	txType+24(FP), R3
	WORD	$0xd10283ff	// 0464: sub sp, sp, #0xa0
	WORD	$0xa9084ff4	// 0468: stp x20, x19, [sp, #0x80]
	WORD	$0xa9097bfd	// 046c: stp x29, x30, [sp, #0x90]
	WORD	$0x910243fd	// 0470: add x29, sp, #0x90
	WORD	$0xaa0103f3	// 0474: mov x19, x1
	WORD	$0x7100047f	// 0478: cmp w3, #0x1
	WORD	$0x540015ec	// 047c: b.gt 0x738 <_vp9_fht8x8_neon+0x2d4>
	WORD	$0x340027c3	// 0480: cbz w3, 0x978 <_vp9_fht8x8_neon+0x514>
	WORD	$0x7100047f	// 0484: cmp w3, #0x1
	WORD	$0x54003041	// 0488: b.ne 0xa90 <_vp9_fht8x8_neon+0x62c>
	WORD	$0x3dc00000	// 048c: ldr q0, [x0]
	WORD	$0x4f125400	// 0490: shl.8h v0, v0, #0x2
	WORD	$0x937f7c48	// 0494: sbfiz x8, x2, #1, #32
	WORD	$0x3ce86801	// 0498: ldr q1, [x0, x8]
	WORD	$0x4f125421	// 049c: shl.8h v1, v1, #0x2
	WORD	$0xad0007e0	// 04a0: stp q0, q1, [sp]
	WORD	$0x937e7c48	// 04a4: sbfiz x8, x2, #2, #32
	WORD	$0x3ce86800	// 04a8: ldr q0, [x0, x8]
	WORD	$0x4f125400	// 04ac: shl.8h v0, v0, #0x2
	WORD	$0x528000c8	// 04b0: mov w8, #0x6                ; =6
	WORD	$0x9b287c48	// 04b4: smull x8, w2, w8
	WORD	$0x3ce86801	// 04b8: ldr q1, [x0, x8]
	WORD	$0x4f125421	// 04bc: shl.8h v1, v1, #0x2
	WORD	$0xad0107e0	// 04c0: stp q0, q1, [sp, #0x20]
	WORD	$0x937d7c48	// 04c4: sbfiz x8, x2, #3, #32
	WORD	$0x3ce86800	// 04c8: ldr q0, [x0, x8]
	WORD	$0x4f125400	// 04cc: shl.8h v0, v0, #0x2
	WORD	$0x52800148	// 04d0: mov w8, #0xa                ; =10
	WORD	$0x9b287c48	// 04d4: smull x8, w2, w8
	WORD	$0x3ce86801	// 04d8: ldr q1, [x0, x8]
	WORD	$0x4f125421	// 04dc: shl.8h v1, v1, #0x2
	WORD	$0xad0207e0	// 04e0: stp q0, q1, [sp, #0x40]
	WORD	$0x52800188	// 04e4: mov w8, #0xc                ; =12
	WORD	$0x9b287c48	// 04e8: smull x8, w2, w8
	WORD	$0x3ce86800	// 04ec: ldr q0, [x0, x8]
	WORD	$0x4f125400	// 04f0: shl.8h v0, v0, #0x2
	WORD	$0x528001c8	// 04f4: mov w8, #0xe                ; =14
	WORD	$0x9b287c48	// 04f8: smull x8, w2, w8
	WORD	$0x3ce86801	// 04fc: ldr q1, [x0, x8]
	WORD	$0x4f125421	// 0500: shl.8h v1, v1, #0x2
	WORD	$0xad0307e0	// 0504: stp q0, q1, [sp, #0x60]
	WORD	$0x910003e0	// 0508: mov x0, sp
	WORD	$0x94000162	// 050c: bl 0x50c <_vp9_fht8x8_neon+0xa8>
	WORD	$0xad4007e0	// 0510: ldp q0, q1, [sp]
	WORD	$0xad430be3	// 0514: ldp q3, q2, [sp, #0x60]
	WORD	$0x4e608445	// 0518: add.8h v5, v2, v0
	WORD	$0x4e618466	// 051c: add.8h v6, v3, v1
	WORD	$0xad411fe4	// 0520: ldp q4, q7, [sp, #0x20]
	WORD	$0xad4243f1	// 0524: ldp q17, q16, [sp, #0x40]
	WORD	$0x4e648612	// 0528: add.8h v18, v16, v4
	WORD	$0x4e678633	// 052c: add.8h v19, v17, v7
	WORD	$0x6e7184e7	// 0530: sub.8h v7, v7, v17
	WORD	$0x6e708490	// 0534: sub.8h v16, v4, v16
	WORD	$0x6e638431	// 0538: sub.8h v17, v1, v3
	WORD	$0x6e628404	// 053c: sub.8h v4, v0, v2
	WORD	$0x4e658660	// 0540: add.8h v0, v19, v5
	WORD	$0x4e668641	// 0544: add.8h v1, v18, v6
	WORD	$0x6e7284c2	// 0548: sub.8h v2, v6, v18
	WORD	$0x6e7384a3	// 054c: sub.8h v3, v5, v19
	WORD	$0x0e610005	// 0550: saddl.4s v5, v0, v1
	WORD	$0x52ab5048	// 0554: mov w8, #0x5a820000         ; =1518469120
	WORD	$0x4e040d06	// 0558: dup.4s v6, w8
	WORD	$0x6ea6b4a5	// 055c: sqrdmulh.4s v5, v5, v6
	WORD	$0x4e610012	// 0560: saddl2.4s v18, v0, v1
	WORD	$0x6ea6b652	// 0564: sqrdmulh.4s v18, v18, v6
	WORD	$0x0e612013	// 0568: ssubl.4s v19, v0, v1
	WORD	$0x6ea6b673	// 056c: sqrdmulh.4s v19, v19, v6
	WORD	$0x4e612000	// 0570: ssubl2.4s v0, v0, v1
	WORD	$0x6ea6b400	// 0574: sqrdmulh.4s v0, v0, v6
	WORD	$0x4e5218a1	// 0578: uzp1.8h v1, v5, v18
	WORD	$0x4e401a60	// 057c: uzp1.8h v0, v19, v0
	WORD	$0x52876428	// 0580: mov w8, #0x3b21             ; =15137
	WORD	$0x4e020d05	// 0584: dup.8h v5, w8
	WORD	$0x52830fc8	// 0588: mov w8, #0x187e             ; =6270
	WORD	$0x4e020d12	// 058c: dup.8h v18, w8
	WORD	$0x0e72c073	// 0590: smull.4s v19, v3, v18
	WORD	$0x4e72c074	// 0594: smull2.4s v20, v3, v18
	WORD	$0x0e72c055	// 0598: smull.4s v21, v2, v18
	WORD	$0x0e658075	// 059c: smlal.4s v21, v3, v5
	WORD	$0x4e72c052	// 05a0: smull2.4s v18, v2, v18
	WORD	$0x4e658072	// 05a4: smlal2.4s v18, v3, v5
	WORD	$0x0e65a053	// 05a8: smlsl.4s v19, v2, v5
	WORD	$0x4e65a054	// 05ac: smlsl2.4s v20, v2, v5
	WORD	$0x0f129ea3	// 05b0: sqrshrn.4h v3, v21, #0xe
	WORD	$0x0f129e62	// 05b4: sqrshrn.4h v2, v19, #0xe
	WORD	$0x4f129e43	// 05b8: sqrshrn2.8h v3, v18, #0xe
	WORD	$0x4f129e82	// 05bc: sqrshrn2.8h v2, v20, #0xe
	WORD	$0x0e710205	// 05c0: saddl.4s v5, v16, v17
	WORD	$0x6ea6b4a5	// 05c4: sqrdmulh.4s v5, v5, v6
	WORD	$0x4e710212	// 05c8: saddl2.4s v18, v16, v17
	WORD	$0x6ea6b652	// 05cc: sqrdmulh.4s v18, v18, v6
	WORD	$0x0e702233	// 05d0: ssubl.4s v19, v17, v16
	WORD	$0x6ea6b673	// 05d4: sqrdmulh.4s v19, v19, v6
	WORD	$0x4e702230	// 05d8: ssubl2.4s v16, v17, v16
	WORD	$0x6ea6b606	// 05dc: sqrdmulh.4s v6, v16, v6
	WORD	$0x4e5218a5	// 05e0: uzp1.8h v5, v5, v18
	WORD	$0x4e461a66	// 05e4: uzp1.8h v6, v19, v6
	WORD	$0x4e6684f0	// 05e8: add.8h v16, v7, v6
	WORD	$0x6e6684e6	// 05ec: sub.8h v6, v7, v6
	WORD	$0x6e658487	// 05f0: sub.8h v7, v4, v5
	WORD	$0x5287d8a8	// 05f4: mov w8, #0x3ec5             ; =16069
	WORD	$0x4e020d11	// 05f8: dup.8h v17, w8
	WORD	$0x4e658484	// 05fc: add.8h v4, v4, v5
	WORD	$0x52818f88	// 0600: mov w8, #0xc7c              ; =3196
	WORD	$0x4e020d05	// 0604: dup.8h v5, w8
	WORD	$0x0e65c092	// 0608: smull.4s v18, v4, v5
	WORD	$0x4e65c093	// 060c: smull2.4s v19, v4, v5
	WORD	$0x0e65c214	// 0610: smull.4s v20, v16, v5
	WORD	$0x0e718094	// 0614: smlal.4s v20, v4, v17
	WORD	$0x4e65c205	// 0618: smull2.4s v5, v16, v5
	WORD	$0x4e718085	// 061c: smlal2.4s v5, v4, v17
	WORD	$0x0e71a212	// 0620: smlsl.4s v18, v16, v17
	WORD	$0x4e71a213	// 0624: smlsl2.4s v19, v16, v17
	WORD	$0x0f129e84	// 0628: sqrshrn.4h v4, v20, #0xe
	WORD	$0x0f129e50	// 062c: sqrshrn.4h v16, v18, #0xe
	WORD	$0x4f129ca4	// 0630: sqrshrn2.8h v4, v5, #0xe
	WORD	$0x528471c8	// 0634: mov w8, #0x238e             ; =9102
	WORD	$0x4e020d05	// 0638: dup.8h v5, w8
	WORD	$0x4f129e70	// 063c: sqrshrn2.8h v16, v19, #0xe
	WORD	$0x5286a6e8	// 0640: mov w8, #0x3537             ; =13623
	WORD	$0x4e020d11	// 0644: dup.8h v17, w8
	WORD	$0x0e71c0f2	// 0648: smull.4s v18, v7, v17
	WORD	$0x4e71c0f3	// 064c: smull2.4s v19, v7, v17
	WORD	$0x0e71c0d4	// 0650: smull.4s v20, v6, v17
	WORD	$0x0e6580f4	// 0654: smlal.4s v20, v7, v5
	WORD	$0x4e71c0d1	// 0658: smull2.4s v17, v6, v17
	WORD	$0x4e6580f1	// 065c: smlal2.4s v17, v7, v5
	WORD	$0x0e65a0d2	// 0660: smlsl.4s v18, v6, v5
	WORD	$0x4e65a0d3	// 0664: smlsl2.4s v19, v6, v5
	WORD	$0x0f129e85	// 0668: sqrshrn.4h v5, v20, #0xe
	WORD	$0x0f129e46	// 066c: sqrshrn.4h v6, v18, #0xe
	WORD	$0x4f129e25	// 0670: sqrshrn2.8h v5, v17, #0xe
	WORD	$0x4f129e66	// 0674: sqrshrn2.8h v6, v19, #0xe
	WORD	$0x4e442827	// 0678: trn1.8h v7, v1, v4
	WORD	$0x4e446821	// 067c: trn2.8h v1, v1, v4
	WORD	$0x4e462864	// 0680: trn1.8h v4, v3, v6
	WORD	$0x4e466863	// 0684: trn2.8h v3, v3, v6
	WORD	$0x4e452806	// 0688: trn1.8h v6, v0, v5
	WORD	$0x4e456800	// 068c: trn2.8h v0, v0, v5
	WORD	$0x4e502845	// 0690: trn1.8h v5, v2, v16
	WORD	$0x4e506842	// 0694: trn2.8h v2, v2, v16
	WORD	$0x4e8428f0	// 0698: trn1.4s v16, v7, v4
	WORD	$0x4e8468e4	// 069c: trn2.4s v4, v7, v4
	WORD	$0x4e832827	// 06a0: trn1.4s v7, v1, v3
	WORD	$0x4e836821	// 06a4: trn2.4s v1, v1, v3
	WORD	$0x4e8528c3	// 06a8: trn1.4s v3, v6, v5
	WORD	$0x4e8568c5	// 06ac: trn2.4s v5, v6, v5
	WORD	$0x4e822806	// 06b0: trn1.4s v6, v0, v2
	WORD	$0x4e826800	// 06b4: trn2.4s v0, v0, v2
	WORD	$0x4ec37a02	// 06b8: zip2.2d v2, v16, v3
	WORD	$0x6e180470	// 06bc: mov.d v16[1], v3[0]
	WORD	$0x4ec678e3	// 06c0: zip2.2d v3, v7, v6
	WORD	$0x6e1804c7	// 06c4: mov.d v7[1], v6[0]
	WORD	$0x4ec57886	// 06c8: zip2.2d v6, v4, v5
	WORD	$0x6e1804a4	// 06cc: mov.d v4[1], v5[0]
	WORD	$0x4ec07825	// 06d0: zip2.2d v5, v1, v0
	WORD	$0x6e180401	// 06d4: mov.d v1[1], v0[0]
	WORD	$0x6f111610	// 06d8: usra.8h v16, v16, #0xf
	WORD	$0x6f1114e7	// 06dc: usra.8h v7, v7, #0xf
	WORD	$0x6f111484	// 06e0: usra.8h v4, v4, #0xf
	WORD	$0x6f111421	// 06e4: usra.8h v1, v1, #0xf
	WORD	$0x6f111442	// 06e8: usra.8h v2, v2, #0xf
	WORD	$0x6f111463	// 06ec: usra.8h v3, v3, #0xf
	WORD	$0x6f1114c6	// 06f0: usra.8h v6, v6, #0xf
	WORD	$0x4f1f0600	// 06f4: sshr.8h v0, v16, #0x1
	WORD	$0x4f1f04e7	// 06f8: sshr.8h v7, v7, #0x1
	WORD	$0xad001e60	// 06fc: stp q0, q7, [x19]
	WORD	$0x6f1114a5	// 0700: usra.8h v5, v5, #0xf
	WORD	$0x4f1f0480	// 0704: sshr.8h v0, v4, #0x1
	WORD	$0x4f1f0421	// 0708: sshr.8h v1, v1, #0x1
	WORD	$0x4f1f0442	// 070c: sshr.8h v2, v2, #0x1
	WORD	$0x4f1f0463	// 0710: sshr.8h v3, v3, #0x1
	WORD	$0x4f1f04c4	// 0714: sshr.8h v4, v6, #0x1
	WORD	$0xad010660	// 0718: stp q0, q1, [x19, #0x20]
	WORD	$0xad020e62	// 071c: stp q2, q3, [x19, #0x40]
	WORD	$0x4f1f04a0	// 0720: sshr.8h v0, v5, #0x1
	WORD	$0xad030264	// 0724: stp q4, q0, [x19, #0x60]
	WORD	$0xa9497bfd	// 0728: ldp x29, x30, [sp, #0x90]
	WORD	$0xa9484ff4	// 072c: ldp x20, x19, [sp, #0x80]
	WORD	$0x910283ff	// 0730: add sp, sp, #0xa0
	WORD	$0xd65f03c0	// 0734: ret
	WORD	$0x7100087f	// 0738: cmp w3, #0x2
	WORD	$0x54001281	// 073c: b.ne 0x98c <_vp9_fht8x8_neon+0x528>
	WORD	$0x3dc00000	// 0740: ldr q0, [x0]
	WORD	$0x4f125400	// 0744: shl.8h v0, v0, #0x2
	WORD	$0x937f7c49	// 0748: sbfiz x9, x2, #1, #32
	WORD	$0x3ce96801	// 074c: ldr q1, [x0, x9]
	WORD	$0x4f125421	// 0750: shl.8h v1, v1, #0x2
	WORD	$0x937e7c49	// 0754: sbfiz x9, x2, #2, #32
	WORD	$0x3ce96802	// 0758: ldr q2, [x0, x9]
	WORD	$0x4f125442	// 075c: shl.8h v2, v2, #0x2
	WORD	$0x528000c9	// 0760: mov w9, #0x6                ; =6
	WORD	$0x9b297c49	// 0764: smull x9, w2, w9
	WORD	$0x3ce96803	// 0768: ldr q3, [x0, x9]
	WORD	$0x937d7c48	// 076c: sbfiz x8, x2, #3, #32
	WORD	$0x3ce86804	// 0770: ldr q4, [x0, x8]
	WORD	$0x4f125463	// 0774: shl.8h v3, v3, #0x2
	WORD	$0x4f125484	// 0778: shl.8h v4, v4, #0x2
	WORD	$0x52800148	// 077c: mov w8, #0xa                ; =10
	WORD	$0x9b287c48	// 0780: smull x8, w2, w8
	WORD	$0x3ce86805	// 0784: ldr q5, [x0, x8]
	WORD	$0x4f1254a5	// 0788: shl.8h v5, v5, #0x2
	WORD	$0x52800188	// 078c: mov w8, #0xc                ; =12
	WORD	$0x9b287c48	// 0790: smull x8, w2, w8
	WORD	$0x3ce86806	// 0794: ldr q6, [x0, x8]
	WORD	$0x4f1254c6	// 0798: shl.8h v6, v6, #0x2
	WORD	$0x528001c8	// 079c: mov w8, #0xe                ; =14
	WORD	$0x9b287c48	// 07a0: smull x8, w2, w8
	WORD	$0x3ce86807	// 07a4: ldr q7, [x0, x8]
	WORD	$0x4f1254e7	// 07a8: shl.8h v7, v7, #0x2
	WORD	$0x4e6084f0	// 07ac: add.8h v16, v7, v0
	WORD	$0x4e6184d1	// 07b0: add.8h v17, v6, v1
	WORD	$0x4e6284b2	// 07b4: add.8h v18, v5, v2
	WORD	$0x4e638493	// 07b8: add.8h v19, v4, v3
	WORD	$0x6e648464	// 07bc: sub.8h v4, v3, v4
	WORD	$0x6e658445	// 07c0: sub.8h v5, v2, v5
	WORD	$0x6e668426	// 07c4: sub.8h v6, v1, v6
	WORD	$0x6e678407	// 07c8: sub.8h v7, v0, v7
	WORD	$0x4e738600	// 07cc: add.8h v0, v16, v19
	WORD	$0x4e728621	// 07d0: add.8h v1, v17, v18
	WORD	$0x6e728622	// 07d4: sub.8h v2, v17, v18
	WORD	$0x6e738603	// 07d8: sub.8h v3, v16, v19
	WORD	$0x0e610010	// 07dc: saddl.4s v16, v0, v1
	WORD	$0x52ab5048	// 07e0: mov w8, #0x5a820000         ; =1518469120
	WORD	$0x4e040d11	// 07e4: dup.4s v17, w8
	WORD	$0x6eb1b610	// 07e8: sqrdmulh.4s v16, v16, v17
	WORD	$0x4e610012	// 07ec: saddl2.4s v18, v0, v1
	WORD	$0x6eb1b652	// 07f0: sqrdmulh.4s v18, v18, v17
	WORD	$0x0e612013	// 07f4: ssubl.4s v19, v0, v1
	WORD	$0x6eb1b673	// 07f8: sqrdmulh.4s v19, v19, v17
	WORD	$0x4e612000	// 07fc: ssubl2.4s v0, v0, v1
	WORD	$0x6eb1b400	// 0800: sqrdmulh.4s v0, v0, v17
	WORD	$0x4e521a01	// 0804: uzp1.8h v1, v16, v18
	WORD	$0x4e401a60	// 0808: uzp1.8h v0, v19, v0
	WORD	$0x52876428	// 080c: mov w8, #0x3b21             ; =15137
	WORD	$0x4e020d10	// 0810: dup.8h v16, w8
	WORD	$0x52830fc8	// 0814: mov w8, #0x187e             ; =6270
	WORD	$0x4e020d12	// 0818: dup.8h v18, w8
	WORD	$0x0e72c073	// 081c: smull.4s v19, v3, v18
	WORD	$0x4e72c074	// 0820: smull2.4s v20, v3, v18
	WORD	$0x0e72c055	// 0824: smull.4s v21, v2, v18
	WORD	$0x0e708075	// 0828: smlal.4s v21, v3, v16
	WORD	$0x4e72c052	// 082c: smull2.4s v18, v2, v18
	WORD	$0x4e708072	// 0830: smlal2.4s v18, v3, v16
	WORD	$0x0e70a053	// 0834: smlsl.4s v19, v2, v16
	WORD	$0x4e70a054	// 0838: smlsl2.4s v20, v2, v16
	WORD	$0x0f129ea3	// 083c: sqrshrn.4h v3, v21, #0xe
	WORD	$0x0f129e62	// 0840: sqrshrn.4h v2, v19, #0xe
	WORD	$0x4f129e43	// 0844: sqrshrn2.8h v3, v18, #0xe
	WORD	$0x4f129e82	// 0848: sqrshrn2.8h v2, v20, #0xe
	WORD	$0x0e6500d0	// 084c: saddl.4s v16, v6, v5
	WORD	$0x6eb1b610	// 0850: sqrdmulh.4s v16, v16, v17
	WORD	$0x4e6500d2	// 0854: saddl2.4s v18, v6, v5
	WORD	$0x6eb1b652	// 0858: sqrdmulh.4s v18, v18, v17
	WORD	$0x0e6520d3	// 085c: ssubl.4s v19, v6, v5
	WORD	$0x6eb1b673	// 0860: sqrdmulh.4s v19, v19, v17
	WORD	$0x4e6520c5	// 0864: ssubl2.4s v5, v6, v5
	WORD	$0x6eb1b4a5	// 0868: sqrdmulh.4s v5, v5, v17
	WORD	$0x4e521a06	// 086c: uzp1.8h v6, v16, v18
	WORD	$0x4e451a65	// 0870: uzp1.8h v5, v19, v5
	WORD	$0x4e658490	// 0874: add.8h v16, v4, v5
	WORD	$0x6e658484	// 0878: sub.8h v4, v4, v5
	WORD	$0x6e6684e5	// 087c: sub.8h v5, v7, v6
	WORD	$0x4e6684e6	// 0880: add.8h v6, v7, v6
	WORD	$0x5287d8a8	// 0884: mov w8, #0x3ec5             ; =16069
	WORD	$0x4e020d07	// 0888: dup.8h v7, w8
	WORD	$0x52818f88	// 088c: mov w8, #0xc7c              ; =3196
	WORD	$0x4e020d11	// 0890: dup.8h v17, w8
	WORD	$0x0e71c0d2	// 0894: smull.4s v18, v6, v17
	WORD	$0x4e71c0d3	// 0898: smull2.4s v19, v6, v17
	WORD	$0x0e71c214	// 089c: smull.4s v20, v16, v17
	WORD	$0x0e6780d4	// 08a0: smlal.4s v20, v6, v7
	WORD	$0x4e71c211	// 08a4: smull2.4s v17, v16, v17
	WORD	$0x4e6780d1	// 08a8: smlal2.4s v17, v6, v7
	WORD	$0x0e67a212	// 08ac: smlsl.4s v18, v16, v7
	WORD	$0x4e67a213	// 08b0: smlsl2.4s v19, v16, v7
	WORD	$0x0f129e86	// 08b4: sqrshrn.4h v6, v20, #0xe
	WORD	$0x0f129e47	// 08b8: sqrshrn.4h v7, v18, #0xe
	WORD	$0x4f129e26	// 08bc: sqrshrn2.8h v6, v17, #0xe
	WORD	$0x4f129e67	// 08c0: sqrshrn2.8h v7, v19, #0xe
	WORD	$0x528471c8	// 08c4: mov w8, #0x238e             ; =9102
	WORD	$0x4e020d10	// 08c8: dup.8h v16, w8
	WORD	$0x5286a6e8	// 08cc: mov w8, #0x3537             ; =13623
	WORD	$0x4e020d11	// 08d0: dup.8h v17, w8
	WORD	$0x0e71c0b2	// 08d4: smull.4s v18, v5, v17
	WORD	$0x4e71c0b3	// 08d8: smull2.4s v19, v5, v17
	WORD	$0x0e71c094	// 08dc: smull.4s v20, v4, v17
	WORD	$0x0e7080b4	// 08e0: smlal.4s v20, v5, v16
	WORD	$0x4e71c091	// 08e4: smull2.4s v17, v4, v17
	WORD	$0x4e7080b1	// 08e8: smlal2.4s v17, v5, v16
	WORD	$0x0e70a092	// 08ec: smlsl.4s v18, v4, v16
	WORD	$0x4e70a093	// 08f0: smlsl2.4s v19, v4, v16
	WORD	$0x0f129e84	// 08f4: sqrshrn.4h v4, v20, #0xe
	WORD	$0x0f129e45	// 08f8: sqrshrn.4h v5, v18, #0xe
	WORD	$0x4f129e24	// 08fc: sqrshrn2.8h v4, v17, #0xe
	WORD	$0x4f129e65	// 0900: sqrshrn2.8h v5, v19, #0xe
	WORD	$0x4e462830	// 0904: trn1.8h v16, v1, v6
	WORD	$0x4e466821	// 0908: trn2.8h v1, v1, v6
	WORD	$0x4e452866	// 090c: trn1.8h v6, v3, v5
	WORD	$0x4e456863	// 0910: trn2.8h v3, v3, v5
	WORD	$0x4e442805	// 0914: trn1.8h v5, v0, v4
	WORD	$0x4e446800	// 0918: trn2.8h v0, v0, v4
	WORD	$0x4e472844	// 091c: trn1.8h v4, v2, v7
	WORD	$0x4e476842	// 0920: trn2.8h v2, v2, v7
	WORD	$0x4e862a07	// 0924: trn1.4s v7, v16, v6
	WORD	$0x4e866a06	// 0928: trn2.4s v6, v16, v6
	WORD	$0x4e832830	// 092c: trn1.4s v16, v1, v3
	WORD	$0x4e836821	// 0930: trn2.4s v1, v1, v3
	WORD	$0x4e8428a3	// 0934: trn1.4s v3, v5, v4
	WORD	$0x4e8468a4	// 0938: trn2.4s v4, v5, v4
	WORD	$0x4e822805	// 093c: trn1.4s v5, v0, v2
	WORD	$0x4e826800	// 0940: trn2.4s v0, v0, v2
	WORD	$0x4ec378e2	// 0944: zip2.2d v2, v7, v3
	WORD	$0x6e180467	// 0948: mov.d v7[1], v3[0]
	WORD	$0x4ec57a03	// 094c: zip2.2d v3, v16, v5
	WORD	$0x6e1804b0	// 0950: mov.d v16[1], v5[0]
	WORD	$0x4ec478c5	// 0954: zip2.2d v5, v6, v4
	WORD	$0x6e180486	// 0958: mov.d v6[1], v4[0]
	WORD	$0x4ec07824	// 095c: zip2.2d v4, v1, v0
	WORD	$0x6e180401	// 0960: mov.d v1[1], v0[0]
	WORD	$0xad0043e7	// 0964: stp q7, q16, [sp]
	WORD	$0xad0107e6	// 0968: stp q6, q1, [sp, #0x20]
	WORD	$0xad020fe2	// 096c: stp q2, q3, [sp, #0x40]
	WORD	$0xad0313e5	// 0970: stp q5, q4, [sp, #0x60]
	WORD	$0x14000029	// 0974: b 0xa18 <_vp9_fht8x8_neon+0x5b4>
	WORD	$0xaa1303e1	// 0978: mov x1, x19
	WORD	$0xa9497bfd	// 097c: ldp x29, x30, [sp, #0x90]
	WORD	$0xa9484ff4	// 0980: ldp x20, x19, [sp, #0x80]
	WORD	$0x910283ff	// 0984: add sp, sp, #0xa0
	WORD	$0x14000000	// 0988: b 0x988 <_vp9_fht8x8_neon+0x524>
	WORD	$0x71000c7f	// 098c: cmp w3, #0x3
	WORD	$0x54000801	// 0990: b.ne 0xa90 <_vp9_fht8x8_neon+0x62c>
	WORD	$0x3dc00000	// 0994: ldr q0, [x0]
	WORD	$0x4f125400	// 0998: shl.8h v0, v0, #0x2
	WORD	$0x937f7c49	// 099c: sbfiz x9, x2, #1, #32
	WORD	$0x3ce96801	// 09a0: ldr q1, [x0, x9]
	WORD	$0x4f125421	// 09a4: shl.8h v1, v1, #0x2
	WORD	$0xad0007e0	// 09a8: stp q0, q1, [sp]
	WORD	$0x937e7c49	// 09ac: sbfiz x9, x2, #2, #32
	WORD	$0x3ce96800	// 09b0: ldr q0, [x0, x9]
	WORD	$0x4f125400	// 09b4: shl.8h v0, v0, #0x2
	WORD	$0x528000c9	// 09b8: mov w9, #0x6                ; =6
	WORD	$0x9b297c49	// 09bc: smull x9, w2, w9
	WORD	$0x3ce96801	// 09c0: ldr q1, [x0, x9]
	WORD	$0x4f125421	// 09c4: shl.8h v1, v1, #0x2
	WORD	$0xad0107e0	// 09c8: stp q0, q1, [sp, #0x20]
	WORD	$0x937d7c48	// 09cc: sbfiz x8, x2, #3, #32
	WORD	$0x3ce86800	// 09d0: ldr q0, [x0, x8]
	WORD	$0x4f125400	// 09d4: shl.8h v0, v0, #0x2
	WORD	$0x52800148	// 09d8: mov w8, #0xa                ; =10
	WORD	$0x9b287c48	// 09dc: smull x8, w2, w8
	WORD	$0x3ce86801	// 09e0: ldr q1, [x0, x8]
	WORD	$0x4f125421	// 09e4: shl.8h v1, v1, #0x2
	WORD	$0xad0207e0	// 09e8: stp q0, q1, [sp, #0x40]
	WORD	$0x52800188	// 09ec: mov w8, #0xc                ; =12
	WORD	$0x9b287c48	// 09f0: smull x8, w2, w8
	WORD	$0x3ce86800	// 09f4: ldr q0, [x0, x8]
	WORD	$0x4f125400	// 09f8: shl.8h v0, v0, #0x2
	WORD	$0x528001c8	// 09fc: mov w8, #0xe                ; =14
	WORD	$0x9b287c48	// 0a00: smull x8, w2, w8
	WORD	$0x3ce86801	// 0a04: ldr q1, [x0, x8]
	WORD	$0x4f125421	// 0a08: shl.8h v1, v1, #0x2
	WORD	$0xad0307e0	// 0a0c: stp q0, q1, [sp, #0x60]
	WORD	$0x910003e0	// 0a10: mov x0, sp
	WORD	$0x94000020	// 0a14: bl 0xa14 <_vp9_fht8x8_neon+0x5b0>
	WORD	$0x910003e0	// 0a18: mov x0, sp
	WORD	$0x9400001e	// 0a1c: bl 0xa1c <_vp9_fht8x8_neon+0x5b8>
	WORD	$0xad4007e0	// 0a20: ldp q0, q1, [sp]
	WORD	$0xad410fe2	// 0a24: ldp q2, q3, [sp, #0x20]
	WORD	$0xad4217e4	// 0a28: ldp q4, q5, [sp, #0x40]
	WORD	$0xad431fe6	// 0a2c: ldp q6, q7, [sp, #0x60]
	WORD	$0x6f111400	// 0a30: usra.8h v0, v0, #0xf
	WORD	$0x6f111421	// 0a34: usra.8h v1, v1, #0xf
	WORD	$0x6f111442	// 0a38: usra.8h v2, v2, #0xf
	WORD	$0x6f111463	// 0a3c: usra.8h v3, v3, #0xf
	WORD	$0x6f111484	// 0a40: usra.8h v4, v4, #0xf
	WORD	$0x6f1114a5	// 0a44: usra.8h v5, v5, #0xf
	WORD	$0x6f1114c6	// 0a48: usra.8h v6, v6, #0xf
	WORD	$0x6f1114e7	// 0a4c: usra.8h v7, v7, #0xf
	WORD	$0x4f1f0400	// 0a50: sshr.8h v0, v0, #0x1
	WORD	$0x4f1f0421	// 0a54: sshr.8h v1, v1, #0x1
	WORD	$0x4f1f0442	// 0a58: sshr.8h v2, v2, #0x1
	WORD	$0x4f1f0463	// 0a5c: sshr.8h v3, v3, #0x1
	WORD	$0x4f1f0484	// 0a60: sshr.8h v4, v4, #0x1
	WORD	$0x4f1f04a5	// 0a64: sshr.8h v5, v5, #0x1
	WORD	$0x4f1f04c6	// 0a68: sshr.8h v6, v6, #0x1
	WORD	$0xad000660	// 0a6c: stp q0, q1, [x19]
	WORD	$0x4f1f04e0	// 0a70: sshr.8h v0, v7, #0x1
	WORD	$0xad010e62	// 0a74: stp q2, q3, [x19, #0x20]
	WORD	$0xad021664	// 0a78: stp q4, q5, [x19, #0x40]
	WORD	$0xad030266	// 0a7c: stp q6, q0, [x19, #0x60]
	WORD	$0xa9497bfd	// 0a80: ldp x29, x30, [sp, #0x90]
	WORD	$0xa9484ff4	// 0a84: ldp x20, x19, [sp, #0x80]
	WORD	$0x910283ff	// 0a88: add sp, sp, #0xa0
	WORD	$0xd65f03c0	// 0a8c: ret
	WORD	$0x94000000	// 0a90: bl 0xa90 <_vp9_fht8x8_neon+0x62c>
	WORD	$0x6dbe2beb	// 0a94: stp d11, d10, [sp, #-0x20]!
	WORD	$0x6d0123e9	// 0a98: stp d9, d8, [sp, #0x10]
	WORD	$0xad411003	// 0a9c: ldp q3, q4, [x0, #0x20]
	WORD	$0xad421401	// 0aa0: ldp q1, q5, [x0, #0x40]
	WORD	$0xad400006	// 0aa4: ldp q6, q0, [x0]
	WORD	$0x5287f628	// 0aa8: mov w8, #0x3fb1             ; =16305
	WORD	$0x4e020d07	// 0aac: dup.8h v7, w8
	WORD	$0x5280c8c8	// 0ab0: mov w8, #0x646              ; =1606
	WORD	$0x4e020d13	// 0ab4: dup.8h v19, w8
	WORD	$0xad435002	// 0ab8: ldp q2, q20, [x0, #0x60]
	WORD	$0x0e73c291	// 0abc: smull.4s v17, v20, v19
	WORD	$0x4e73c290	// 0ac0: smull2.4s v16, v20, v19
	WORD	$0x0e73c0d2	// 0ac4: smull.4s v18, v6, v19
	WORD	$0x0e678292	// 0ac8: smlal.4s v18, v20, v7
	WORD	$0x4e73c0d3	// 0acc: smull2.4s v19, v6, v19
	WORD	$0x4e678293	// 0ad0: smlal2.4s v19, v20, v7
	WORD	$0x0e67a0d1	// 0ad4: smlsl.4s v17, v6, v7
	WORD	$0x4e67a0d0	// 0ad8: smlsl2.4s v16, v6, v7
	WORD	$0x52870e28	// 0adc: mov w8, #0x3871             ; =14449
	WORD	$0x4e020d06	// 0ae0: dup.8h v6, w8
	WORD	$0x5283c568	// 0ae4: mov w8, #0x1e2b             ; =7723
	WORD	$0x4e020d07	// 0ae8: dup.8h v7, w8
	WORD	$0x0e67c0b5	// 0aec: smull.4s v21, v5, v7
	WORD	$0x4e67c0b4	// 0af0: smull2.4s v20, v5, v7
	WORD	$0x0e67c076	// 0af4: smull.4s v22, v3, v7
	WORD	$0x0e6680b6	// 0af8: smlal.4s v22, v5, v6
	WORD	$0x4e67c077	// 0afc: smull2.4s v23, v3, v7
	WORD	$0x4e6680b7	// 0b00: smlal2.4s v23, v5, v6
	WORD	$0x0e66a075	// 0b04: smlsl.4s v21, v3, v6
	WORD	$0x4e66a074	// 0b08: smlsl2.4s v20, v3, v6
	WORD	$0x52851348	// 0b0c: mov w8, #0x289a             ; =10394
	WORD	$0x4e020d03	// 0b10: dup.8h v3, w8
	WORD	$0x52862f28	// 0b14: mov w8, #0x3179             ; =12665
	WORD	$0x4e020d05	// 0b18: dup.8h v5, w8
	WORD	$0x0e65c099	// 0b1c: smull.4s v25, v4, v5
	WORD	$0x4e65c09a	// 0b20: smull2.4s v26, v4, v5
	WORD	$0x0e65c03b	// 0b24: smull.4s v27, v1, v5
	WORD	$0x0e63809b	// 0b28: smlal.4s v27, v4, v3
	WORD	$0x4e65c03c	// 0b2c: smull2.4s v28, v1, v5
	WORD	$0x4e63809c	// 0b30: smlal2.4s v28, v4, v3
	WORD	$0x0e63a039	// 0b34: smlsl.4s v25, v1, v3
	WORD	$0x52825288	// 0b38: mov w8, #0x1294             ; =4756
	WORD	$0x4e020d04	// 0b3c: dup.8h v4, w8
	WORD	$0x5287a7e8	// 0b40: mov w8, #0x3d3f             ; =15679
	WORD	$0x4e020d05	// 0b44: dup.8h v5, w8
	WORD	$0x4e63a03a	// 0b48: smlsl2.4s v26, v1, v3
	WORD	$0x0e65c01d	// 0b4c: smull.4s v29, v0, v5
	WORD	$0x4e65c01e	// 0b50: smull2.4s v30, v0, v5
	WORD	$0x0e65c05f	// 0b54: smull.4s v31, v2, v5
	WORD	$0x0e64801f	// 0b58: smlal.4s v31, v0, v4
	WORD	$0x4e65c048	// 0b5c: smull2.4s v8, v2, v5
	WORD	$0x4e648008	// 0b60: smlal2.4s v8, v0, v4
	WORD	$0x0e64a05d	// 0b64: smlsl.4s v29, v2, v4
	WORD	$0x4e64a05e	// 0b68: smlsl2.4s v30, v2, v4
	WORD	$0x4eb28764	// 0b6c: add.4s v4, v27, v18
	WORD	$0x4f322498	// 0b70: srshr.4s v24, v4, #0xe
	WORD	$0x4eb38785	// 0b74: add.4s v5, v28, v19
	WORD	$0x4f3224a9	// 0b78: srshr.4s v9, v5, #0xe
	WORD	$0x4eb18720	// 0b7c: add.4s v0, v25, v17
	WORD	$0x4f32240a	// 0b80: srshr.4s v10, v0, #0xe
	WORD	$0x4eb08741	// 0b84: add.4s v1, v26, v16
	WORD	$0x4f32242b	// 0b88: srshr.4s v11, v1, #0xe
	WORD	$0x4eb687e2	// 0b8c: add.4s v2, v31, v22
	WORD	$0x4f322446	// 0b90: srshr.4s v6, v2, #0xe
	WORD	$0x4eb78502	// 0b94: add.4s v2, v8, v23
	WORD	$0x4f322447	// 0b98: srshr.4s v7, v2, #0xe
	WORD	$0x4eb587a2	// 0b9c: add.4s v2, v29, v21
	WORD	$0x4f322442	// 0ba0: srshr.4s v2, v2, #0xe
	WORD	$0x4eb487c3	// 0ba4: add.4s v3, v30, v20
	WORD	$0x4f322463	// 0ba8: srshr.4s v3, v3, #0xe
	WORD	$0x6ebb8652	// 0bac: sub.4s v18, v18, v27
	WORD	$0x4f322652	// 0bb0: srshr.4s v18, v18, #0xe
	WORD	$0x6ebc8673	// 0bb4: sub.4s v19, v19, v28
	WORD	$0x4f322673	// 0bb8: srshr.4s v19, v19, #0xe
	WORD	$0x6eb98631	// 0bbc: sub.4s v17, v17, v25
	WORD	$0x4f322631	// 0bc0: srshr.4s v17, v17, #0xe
	WORD	$0x6eba8610	// 0bc4: sub.4s v16, v16, v26
	WORD	$0x4f322610	// 0bc8: srshr.4s v16, v16, #0xe
	WORD	$0x6ebf86d6	// 0bcc: sub.4s v22, v22, v31
	WORD	$0x4f3226d6	// 0bd0: srshr.4s v22, v22, #0xe
	WORD	$0x6ea886f7	// 0bd4: sub.4s v23, v23, v8
	WORD	$0x4f3226f7	// 0bd8: srshr.4s v23, v23, #0xe
	WORD	$0x6ebd86b5	// 0bdc: sub.4s v21, v21, v29
	WORD	$0x4f3226b5	// 0be0: srshr.4s v21, v21, #0xe
	WORD	$0x6ebe8694	// 0be4: sub.4s v20, v20, v30
	WORD	$0x4f322694	// 0be8: srshr.4s v20, v20, #0xe
	WORD	$0x52876428	// 0bec: mov w8, #0x3b21             ; =15137
	WORD	$0x4e040d19	// 0bf0: dup.4s v25, w8
	WORD	$0x4eb99e5a	// 0bf4: mul.4s v26, v18, v25
	WORD	$0x52830fc8	// 0bf8: mov w8, #0x187e             ; =6270
	WORD	$0x4e040d1b	// 0bfc: dup.4s v27, w8
	WORD	$0x4eb99e7c	// 0c00: mul.4s v28, v19, v25
	WORD	$0x4ebb9e52	// 0c04: mul.4s v18, v18, v27
	WORD	$0x4ebb9e73	// 0c08: mul.4s v19, v19, v27
	WORD	$0x4ebb963a	// 0c0c: mla.4s v26, v17, v27
	WORD	$0x4ebb961c	// 0c10: mla.4s v28, v16, v27
	WORD	$0x12876408	// 0c14: mov w8, #-0x3b21            ; =-15137
	WORD	$0x4e040d1d	// 0c18: dup.4s v29, w8
	WORD	$0x4ebd9632	// 0c1c: mla.4s v18, v17, v29
	WORD	$0x12830fa8	// 0c20: mov w8, #-0x187e            ; =-6270
	WORD	$0x4e040d11	// 0c24: dup.4s v17, w8
	WORD	$0x4ebd9613	// 0c28: mla.4s v19, v16, v29
	WORD	$0x4eb19ed0	// 0c2c: mul.4s v16, v22, v17
	WORD	$0x4eb19ef1	// 0c30: mul.4s v17, v23, v17
	WORD	$0x4eb99ed6	// 0c34: mul.4s v22, v22, v25
	WORD	$0x4eb99ef7	// 0c38: mul.4s v23, v23, v25
	WORD	$0x4eb996b0	// 0c3c: mla.4s v16, v21, v25
	WORD	$0x4eb99691	// 0c40: mla.4s v17, v20, v25
	WORD	$0x4ebb96b6	// 0c44: mla.4s v22, v21, v27
	WORD	$0x4ebb9697	// 0c48: mla.4s v23, v20, v27
	WORD	$0x6ea68714	// 0c4c: sub.4s v20, v24, v6
	WORD	$0x6ea78535	// 0c50: sub.4s v21, v9, v7
	WORD	$0x6ea28558	// 0c54: sub.4s v24, v10, v2
	WORD	$0x6ea38579	// 0c58: sub.4s v25, v11, v3
	WORD	$0x4eba861b	// 0c5c: add.4s v27, v16, v26
	WORD	$0x4f32277b	// 0c60: srshr.4s v27, v27, #0xe
	WORD	$0x4ebc863d	// 0c64: add.4s v29, v17, v28
	WORD	$0x4f3227bd	// 0c68: srshr.4s v29, v29, #0xe
	WORD	$0x4eb286de	// 0c6c: add.4s v30, v22, v18
	WORD	$0x4f3227de	// 0c70: srshr.4s v30, v30, #0xe
	WORD	$0x4eb386ff	// 0c74: add.4s v31, v23, v19
	WORD	$0x4f3227ff	// 0c78: srshr.4s v31, v31, #0xe
	WORD	$0x6eb08750	// 0c7c: sub.4s v16, v26, v16
	WORD	$0x4f322610	// 0c80: srshr.4s v16, v16, #0xe
	WORD	$0x6eb18791	// 0c84: sub.4s v17, v28, v17
	WORD	$0x4f322631	// 0c88: srshr.4s v17, v17, #0xe
	WORD	$0x6eb68652	// 0c8c: sub.4s v18, v18, v22
	WORD	$0x4f322652	// 0c90: srshr.4s v18, v18, #0xe
	WORD	$0x6eb78673	// 0c94: sub.4s v19, v19, v23
	WORD	$0x4f322673	// 0c98: srshr.4s v19, v19, #0xe
	WORD	$0x5285a828	// 0c9c: mov w8, #0x2d41             ; =11585
	WORD	$0x4e040d16	// 0ca0: dup.4s v22, w8
	WORD	$0x4eb69e94	// 0ca4: mul.4s v20, v20, v22
	WORD	$0x4eb69eb5	// 0ca8: mul.4s v21, v21, v22
	WORD	$0x4eb69f17	// 0cac: mul.4s v23, v24, v22
	WORD	$0x4eb486f8	// 0cb0: add.4s v24, v23, v20
	WORD	$0x4eb69f39	// 0cb4: mul.4s v25, v25, v22
	WORD	$0x4eb5873a	// 0cb8: add.4s v26, v25, v21
	WORD	$0x6eb78694	// 0cbc: sub.4s v20, v20, v23
	WORD	$0x6eb986b5	// 0cc0: sub.4s v21, v21, v25
	WORD	$0x4eb69e10	// 0cc4: mul.4s v16, v16, v22
	WORD	$0x4eb69e31	// 0cc8: mul.4s v17, v17, v22
	WORD	$0x4eb69e52	// 0ccc: mul.4s v18, v18, v22
	WORD	$0x4eb08657	// 0cd0: add.4s v23, v18, v16
	WORD	$0x4eb69e73	// 0cd4: mul.4s v19, v19, v22
	WORD	$0x4eb18676	// 0cd8: add.4s v22, v19, v17
	WORD	$0x6eb28610	// 0cdc: sub.4s v16, v16, v18
	WORD	$0x6eb38631	// 0ce0: sub.4s v17, v17, v19
	WORD	$0x0f128f12	// 0ce4: rshrn.4h v18, v24, #0xe
	WORD	$0x0f128e93	// 0ce8: rshrn.4h v19, v20, #0xe
	WORD	$0x0f128ef4	// 0cec: rshrn.4h v20, v23, #0xe
	WORD	$0x0f128e10	// 0cf0: rshrn.4h v16, v16, #0xe
	WORD	$0x4f323486	// 0cf4: srsra.4s v6, v4, #0xe
	WORD	$0x4f3234a7	// 0cf8: srsra.4s v7, v5, #0xe
	WORD	$0x4e4718c4	// 0cfc: uzp1.8h v4, v6, v7
	WORD	$0x4e5d1b65	// 0d00: uzp1.8h v5, v27, v29
	WORD	$0x6e60b8a5	// 0d04: neg.8h v5, v5
	WORD	$0x4f128ed4	// 0d08: rshrn2.8h v20, v22, #0xe
	WORD	$0x4f128f52	// 0d0c: rshrn2.8h v18, v26, #0xe
	WORD	$0x6e60ba46	// 0d10: neg.8h v6, v18
	WORD	$0x4f128eb3	// 0d14: rshrn2.8h v19, v21, #0xe
	WORD	$0x4f128e30	// 0d18: rshrn2.8h v16, v17, #0xe
	WORD	$0x6e60ba07	// 0d1c: neg.8h v7, v16
	WORD	$0x4e5f1bd0	// 0d20: uzp1.8h v16, v30, v31
	WORD	$0x4f323402	// 0d24: srsra.4s v2, v0, #0xe
	WORD	$0x4f323423	// 0d28: srsra.4s v3, v1, #0xe
	WORD	$0x4e431840	// 0d2c: uzp1.8h v0, v2, v3
	WORD	$0x6e60b800	// 0d30: neg.8h v0, v0
	WORD	$0x4e452881	// 0d34: trn1.8h v1, v4, v5
	WORD	$0x4e456882	// 0d38: trn2.8h v2, v4, v5
	WORD	$0x4e462a83	// 0d3c: trn1.8h v3, v20, v6
	WORD	$0x4e466a84	// 0d40: trn2.8h v4, v20, v6
	WORD	$0x4e472a65	// 0d44: trn1.8h v5, v19, v7
	WORD	$0x4e476a66	// 0d48: trn2.8h v6, v19, v7
	WORD	$0x4e402a07	// 0d4c: trn1.8h v7, v16, v0
	WORD	$0x4e406a00	// 0d50: trn2.8h v0, v16, v0
	WORD	$0x4e832830	// 0d54: trn1.4s v16, v1, v3
	WORD	$0x4e836821	// 0d58: trn2.4s v1, v1, v3
	WORD	$0x4e842843	// 0d5c: trn1.4s v3, v2, v4
	WORD	$0x4e846842	// 0d60: trn2.4s v2, v2, v4
	WORD	$0x4e8728a4	// 0d64: trn1.4s v4, v5, v7
	WORD	$0x4e8768a5	// 0d68: trn2.4s v5, v5, v7
	WORD	$0x4e8028c7	// 0d6c: trn1.4s v7, v6, v0
	WORD	$0x4e8068c0	// 0d70: trn2.4s v0, v6, v0
	WORD	$0x4ec47a06	// 0d74: zip2.2d v6, v16, v4
	WORD	$0x6e180490	// 0d78: mov.d v16[1], v4[0]
	WORD	$0x4ec77864	// 0d7c: zip2.2d v4, v3, v7
	WORD	$0x6e1804e3	// 0d80: mov.d v3[1], v7[0]
	WORD	$0x4ec57827	// 0d84: zip2.2d v7, v1, v5
	WORD	$0x6e1804a1	// 0d88: mov.d v1[1], v5[0]
	WORD	$0x4ec07845	// 0d8c: zip2.2d v5, v2, v0
	WORD	$0x6e180402	// 0d90: mov.d v2[1], v0[0]
	WORD	$0xad000c10	// 0d94: stp q16, q3, [x0]
	WORD	$0xad010801	// 0d98: stp q1, q2, [x0, #0x20]
	WORD	$0xad021006	// 0d9c: stp q6, q4, [x0, #0x40]
	WORD	$0xad031407	// 0da0: stp q7, q5, [x0, #0x60]
	WORD	$0x6d4123e9	// 0da4: ldp d9, d8, [sp, #0x10]
	WORD	$0x6cc22beb	// 0da8: ldp d11, d10, [sp], #0x20
	WORD	$0xd65f03c0	// 0dac: ret
