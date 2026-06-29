//go:build arm64 && !purego

// ARMv8 NEON port of libvpx v1.16.0 vpx_dsp/arm/fdct16x16_neon.c
// vpx_fdct16x16_neon. Instruction WORDs are generated from the
// pinned libvpx object rebuilt with -fno-stack-protector; the four
// internal calls to vpx_fdct8x16_body are encoded as local BL immediates.

#include "textflag.h"

// forwardDCT16x16NEON ABI ($0-24):
//   input+0(FP)   *int16
//   output+8(FP)  *int16
//   stride+16(FP) int (in int16 elements)
// The copied libvpx prologue owns the stack frame and preserves Go's
// g register in x28, so this must stay NOFRAME.
TEXT ·forwardDCT16x16NEON(SB), NOSPLIT|NOFRAME, $0-24
	MOVD	input+0(FP), R0
	MOVD	output+8(FP), R1
	MOVD	stride+16(FP), R2
	WORD	$0x6db82beb	// 0000: stp	d11, d10, [sp, #-0x80]!
	WORD	$0x6d0123e9	// 0004: stp	d9, d8, [sp, #0x10]
	WORD	$0xa9026ffc	// 0008: stp	x28, x27, [sp, #0x20]
	WORD	$0xa90367fa	// 000c: stp	x26, x25, [sp, #0x30]
	WORD	$0xa9045ff8	// 0010: stp	x24, x23, [sp, #0x40]
	WORD	$0xa90557f6	// 0014: stp	x22, x21, [sp, #0x50]
	WORD	$0xa9064ff4	// 0018: stp	x20, x19, [sp, #0x60]
	WORD	$0xa9077bfd	// 001c: stp	x29, x30, [sp, #0x70]
	WORD	$0x9101c3fd	// 0020: add	x29, sp, #0x70
	WORD	$0xd11143ff	// 0024: sub	sp, sp, #0x450
	WORD	$0xaa0103f3	// 0028: mov	x19, x1
	WORD	$0xaa0003f4	// 002c: mov	x20, x0
	WORD	$0x937b7c48	// 0030: sbfiz	x8, x2, #5, #32
	WORD	$0x937f7c57	// 0034: sbfiz	x23, x2, #1, #32
	WORD	$0xcb170108	// 0038: sub	x8, x8, x23
	WORD	$0xf9001fe8	// 003c: str	x8, [sp, #0x38]
	WORD	$0x3ce86800	// 0040: ldr	q0, [x0, x8]
	WORD	$0x3cf76801	// 0044: ldr	q1, [x0, x23]
	WORD	$0x937c7c55	// 0048: sbfiz	x21, x2, #4, #32
	WORD	$0xcb1702b6	// 004c: sub	x22, x21, x23
	WORD	$0xd37ffac8	// 0050: lsl	x8, x22, #1
	WORD	$0xf9001be8	// 0054: str	x8, [sp, #0x30]
	WORD	$0x3ce86802	// 0058: ldr	q2, [x0, x8]
	WORD	$0x937e7c5a	// 005c: sbfiz	x26, x2, #2, #32
	WORD	$0x3cfa6804	// 0060: ldr	q4, [x0, x26]
	WORD	$0x52800348	// 0064: mov	w8, #0x1a               ; =26
	WORD	$0x9b287c48	// 0068: smull	x8, w2, w8
	WORD	$0xf90017e8	// 006c: str	x8, [sp, #0x28]
	WORD	$0x3ce86805	// 0070: ldr	q5, [x0, x8]
	WORD	$0x3dc00003	// 0074: ldr	q3, [x0]
	WORD	$0x93407c48	// 0078: sxtw	x8, w2
	WORD	$0x8b0802e9	// 007c: add	x9, x23, x8
	WORD	$0xd37ff92a	// 0080: lsl	x10, x9, #1
	WORD	$0xf90013ea	// 0084: str	x10, [sp, #0x20]
	WORD	$0x3cea6806	// 0088: ldr	q6, [x0, x10]
	WORD	$0xd37df12a	// 008c: lsl	x10, x9, #3
	WORD	$0xf9000fea	// 0090: str	x10, [sp, #0x18]
	WORD	$0x3cea6807	// 0094: ldr	q7, [x0, x10]
	WORD	$0x4e638410	// 0098: add.8h	v16, v0, v3
	WORD	$0x4e618451	// 009c: add.8h	v17, v2, v1
	WORD	$0xad1a47f0	// 00a0: stp	q16, q17, [sp, #0x340]
	WORD	$0x937d7c59	// 00a4: sbfiz	x25, x2, #3, #32
	WORD	$0x3cf96812	// 00a8: ldr	q18, [x0, x25]
	WORD	$0x528002ca	// 00ac: mov	w10, #0x16              ; =22
	WORD	$0x9b2a7c4a	// 00b0: smull	x10, w2, w10
	WORD	$0xf9000bea	// 00b4: str	x10, [sp, #0x10]
	WORD	$0x3cea6813	// 00b8: ldr	q19, [x0, x10]
	WORD	$0x8b08034a	// 00bc: add	x10, x26, x8
	WORD	$0xd37ff94b	// 00c0: lsl	x11, x10, #1
	WORD	$0xf90007eb	// 00c4: str	x11, [sp, #0x8]
	WORD	$0x3ceb6814	// 00c8: ldr	q20, [x0, x11]
	WORD	$0xd37ef558	// 00cc: lsl	x24, x10, #2
	WORD	$0x3cf86815	// 00d0: ldr	q21, [x0, x24]
	WORD	$0x4e6484b6	// 00d4: add.8h	v22, v5, v4
	WORD	$0x4e6684f7	// 00d8: add.8h	v23, v7, v6
	WORD	$0xad1b5ff6	// 00dc: stp	q22, q23, [sp, #0x360]
	WORD	$0xd37ef53b	// 00e0: lsl	x27, x9, #2
	WORD	$0x4e728678	// 00e4: add.8h	v24, v19, v18
	WORD	$0x4e7486b9	// 00e8: add.8h	v25, v21, v20
	WORD	$0xad1c67f8	// 00ec: stp	q24, q25, [sp, #0x380]
	WORD	$0x3cfb681a	// 00f0: ldr	q26, [x0, x27]
	WORD	$0x8b080328	// 00f4: add	x8, x25, x8
	WORD	$0xd37ff91c	// 00f8: lsl	x28, x8, #1
	WORD	$0x3cfc681b	// 00fc: ldr	q27, [x0, x28]
	WORD	$0x3cf6681c	// 0100: ldr	q28, [x0, x22]
	WORD	$0x3cf5681d	// 0104: ldr	q29, [x0, x21]
	WORD	$0x4e7a877e	// 0108: add.8h	v30, v27, v26
	WORD	$0x4e7c87bf	// 010c: add.8h	v31, v29, v28
	WORD	$0xad1d7ffe	// 0110: stp	q30, q31, [sp, #0x3a0]
	WORD	$0x4f125610	// 0114: shl.8h	v16, v16, #0x2
	WORD	$0x4f125631	// 0118: shl.8h	v17, v17, #0x2
	WORD	$0xad1247f0	// 011c: stp	q16, q17, [sp, #0x240]
	WORD	$0x6e7d8790	// 0120: sub.8h	v16, v28, v29
	WORD	$0x4f1256d1	// 0124: shl.8h	v17, v22, #0x2
	WORD	$0x4f1256f6	// 0128: shl.8h	v22, v23, #0x2
	WORD	$0xad135bf1	// 012c: stp	q17, q22, [sp, #0x260]
	WORD	$0x6e7b8751	// 0130: sub.8h	v17, v26, v27
	WORD	$0x6e758694	// 0134: sub.8h	v20, v20, v21
	WORD	$0x6e738652	// 0138: sub.8h	v18, v18, v19
	WORD	$0x6e6784c6	// 013c: sub.8h	v6, v6, v7
	WORD	$0x4f125707	// 0140: shl.8h	v7, v24, #0x2
	WORD	$0x4f125733	// 0144: shl.8h	v19, v25, #0x2
	WORD	$0xad144fe7	// 0148: stp	q7, q19, [sp, #0x280]
	WORD	$0x6e658484	// 014c: sub.8h	v4, v4, v5
	WORD	$0x6e628421	// 0150: sub.8h	v1, v1, v2
	WORD	$0x4f1257c2	// 0154: shl.8h	v2, v30, #0x2
	WORD	$0x4f1257e5	// 0158: shl.8h	v5, v31, #0x2
	WORD	$0xad1517e2	// 015c: stp	q2, q5, [sp, #0x2a0]
	WORD	$0x6e608460	// 0160: sub.8h	v0, v3, v0
	WORD	$0x4f125602	// 0164: shl.8h	v2, v16, #0x2
	WORD	$0x4f125623	// 0168: shl.8h	v3, v17, #0x2
	WORD	$0xad160fe2	// 016c: stp	q2, q3, [sp, #0x2c0]
	WORD	$0x4f125682	// 0170: shl.8h	v2, v20, #0x2
	WORD	$0x4f125643	// 0174: shl.8h	v3, v18, #0x2
	WORD	$0xad170fe2	// 0178: stp	q2, q3, [sp, #0x2e0]
	WORD	$0x4f1254c2	// 017c: shl.8h	v2, v6, #0x2
	WORD	$0x4f125483	// 0180: shl.8h	v3, v4, #0x2
	WORD	$0xad180fe2	// 0184: stp	q2, q3, [sp, #0x300]
	WORD	$0x4f125421	// 0188: shl.8h	v1, v1, #0x2
	WORD	$0x4f125400	// 018c: shl.8h	v0, v0, #0x2
	WORD	$0xad1903e1	// 0190: stp	q1, q0, [sp, #0x320]
	WORD	$0x910903e0	// 0194: add	x0, sp, #0x240
	WORD	$0x910d03e1	// 0198: add	x1, sp, #0x340
	WORD	$0x940001d4	// 019c: bl local fdct8x16 body (+0x8ec)
	WORD	$0x3cc10e80	// 01a0: ldr	q0, [x20, #0x10]!
	WORD	$0xa94327e8	// 01a4: ldp	x8, x9, [sp, #0x30]
	WORD	$0x3ce96a81	// 01a8: ldr	q1, [x20, x9]
	WORD	$0x4e608422	// 01ac: add.8h	v2, v1, v0
	WORD	$0x3cf76a83	// 01b0: ldr	q3, [x20, x23]
	WORD	$0x3ce86a84	// 01b4: ldr	q4, [x20, x8]
	WORD	$0x4e638485	// 01b8: add.8h	v5, v4, v3
	WORD	$0xad1217e2	// 01bc: stp	q2, q5, [sp, #0x240]
	WORD	$0x3cfa6a86	// 01c0: ldr	q6, [x20, x26]
	WORD	$0xa94227e8	// 01c4: ldp	x8, x9, [sp, #0x20]
	WORD	$0x3ce96a87	// 01c8: ldr	q7, [x20, x9]
	WORD	$0x4e6684f0	// 01cc: add.8h	v16, v7, v6
	WORD	$0x3ce86a91	// 01d0: ldr	q17, [x20, x8]
	WORD	$0xa94127e8	// 01d4: ldp	x8, x9, [sp, #0x10]
	WORD	$0x3ce96a92	// 01d8: ldr	q18, [x20, x9]
	WORD	$0x4e718653	// 01dc: add.8h	v19, v18, v17
	WORD	$0xad134ff0	// 01e0: stp	q16, q19, [sp, #0x260]
	WORD	$0x3cf96a94	// 01e4: ldr	q20, [x20, x25]
	WORD	$0x3ce86a95	// 01e8: ldr	q21, [x20, x8]
	WORD	$0x4e7486b6	// 01ec: add.8h	v22, v21, v20
	WORD	$0xf94007e8	// 01f0: ldr	x8, [sp, #0x8]
	WORD	$0x3ce86a97	// 01f4: ldr	q23, [x20, x8]
	WORD	$0x3cf86a98	// 01f8: ldr	q24, [x20, x24]
	WORD	$0x4e778719	// 01fc: add.8h	v25, v24, v23
	WORD	$0xad1467f6	// 0200: stp	q22, q25, [sp, #0x280]
	WORD	$0x3cfb6a9a	// 0204: ldr	q26, [x20, x27]
	WORD	$0x3cfc6a9b	// 0208: ldr	q27, [x20, x28]
	WORD	$0x4e7a877c	// 020c: add.8h	v28, v27, v26
	WORD	$0x3cf66a9d	// 0210: ldr	q29, [x20, x22]
	WORD	$0x3cf56a9e	// 0214: ldr	q30, [x20, x21]
	WORD	$0x4e7d87df	// 0218: add.8h	v31, v30, v29
	WORD	$0xad157ffc	// 021c: stp	q28, q31, [sp, #0x2a0]
	WORD	$0x6e7e87bd	// 0220: sub.8h	v29, v29, v30
	WORD	$0x6e7b875a	// 0224: sub.8h	v26, v26, v27
	WORD	$0x6e7886f7	// 0228: sub.8h	v23, v23, v24
	WORD	$0x6e758694	// 022c: sub.8h	v20, v20, v21
	WORD	$0x6e728631	// 0230: sub.8h	v17, v17, v18
	WORD	$0x6e6784c6	// 0234: sub.8h	v6, v6, v7
	WORD	$0x6e648463	// 0238: sub.8h	v3, v3, v4
	WORD	$0x6e618400	// 023c: sub.8h	v0, v0, v1
	WORD	$0x4f125441	// 0240: shl.8h	v1, v2, #0x2
	WORD	$0x4f1254a2	// 0244: shl.8h	v2, v5, #0x2
	WORD	$0xad0a0be1	// 0248: stp	q1, q2, [sp, #0x140]
	WORD	$0x4f125601	// 024c: shl.8h	v1, v16, #0x2
	WORD	$0x4f125662	// 0250: shl.8h	v2, v19, #0x2
	WORD	$0xad0b0be1	// 0254: stp	q1, q2, [sp, #0x160]
	WORD	$0x4f1256c1	// 0258: shl.8h	v1, v22, #0x2
	WORD	$0x4f125722	// 025c: shl.8h	v2, v25, #0x2
	WORD	$0xad0c0be1	// 0260: stp	q1, q2, [sp, #0x180]
	WORD	$0x4f125781	// 0264: shl.8h	v1, v28, #0x2
	WORD	$0x4f1257e2	// 0268: shl.8h	v2, v31, #0x2
	WORD	$0xad0d0be1	// 026c: stp	q1, q2, [sp, #0x1a0]
	WORD	$0x4f1257a1	// 0270: shl.8h	v1, v29, #0x2
	WORD	$0x4f125742	// 0274: shl.8h	v2, v26, #0x2
	WORD	$0xad0e0be1	// 0278: stp	q1, q2, [sp, #0x1c0]
	WORD	$0x4f1256e1	// 027c: shl.8h	v1, v23, #0x2
	WORD	$0x4f125682	// 0280: shl.8h	v2, v20, #0x2
	WORD	$0xad0f0be1	// 0284: stp	q1, q2, [sp, #0x1e0]
	WORD	$0x4f125621	// 0288: shl.8h	v1, v17, #0x2
	WORD	$0x4f1254c2	// 028c: shl.8h	v2, v6, #0x2
	WORD	$0xad100be1	// 0290: stp	q1, q2, [sp, #0x200]
	WORD	$0x4f125461	// 0294: shl.8h	v1, v3, #0x2
	WORD	$0x4f125400	// 0298: shl.8h	v0, v0, #0x2
	WORD	$0xad1103e1	// 029c: stp	q1, q0, [sp, #0x220]
	WORD	$0x910503e0	// 02a0: add	x0, sp, #0x140
	WORD	$0x910903e1	// 02a4: add	x1, sp, #0x240
	WORD	$0x94000191	// 02a8: bl local fdct8x16 body (+0x8ec)
	WORD	$0xad5a07e0	// 02ac: ldp	q0, q1, [sp, #0x340]
	WORD	$0x4e412802	// 02b0: trn1.8h	v2, v0, v1
	WORD	$0x4e416800	// 02b4: trn2.8h	v0, v0, v1
	WORD	$0xad5b0fe1	// 02b8: ldp	q1, q3, [sp, #0x360]
	WORD	$0x4e432824	// 02bc: trn1.8h	v4, v1, v3
	WORD	$0x4e436821	// 02c0: trn2.8h	v1, v1, v3
	WORD	$0xad5c17e3	// 02c4: ldp	q3, q5, [sp, #0x380]
	WORD	$0x4e452866	// 02c8: trn1.8h	v6, v3, v5
	WORD	$0x4e456863	// 02cc: trn2.8h	v3, v3, v5
	WORD	$0xad5d1fe5	// 02d0: ldp	q5, q7, [sp, #0x3a0]
	WORD	$0x4e4728b0	// 02d4: trn1.8h	v16, v5, v7
	WORD	$0x4e4768a5	// 02d8: trn2.8h	v5, v5, v7
	WORD	$0x4e842854	// 02dc: trn1.4s	v20, v2, v4
	WORD	$0x4e846857	// 02e0: trn2.4s	v23, v2, v4
	WORD	$0x4e812816	// 02e4: trn1.4s	v22, v0, v1
	WORD	$0x4e816818	// 02e8: trn2.4s	v24, v0, v1
	WORD	$0x4e9028c1	// 02ec: trn1.4s	v1, v6, v16
	WORD	$0x4e9068c2	// 02f0: trn2.4s	v2, v6, v16
	WORD	$0x4e852864	// 02f4: trn1.4s	v4, v3, v5
	WORD	$0x4e856863	// 02f8: trn2.4s	v3, v3, v5
	WORD	$0x4ec17a93	// 02fc: zip2.2d	v19, v20, v1
	WORD	$0x6e180434	// 0300: mov.d	v20[1], v1[0]
	WORD	$0x4ec47ad5	// 0304: zip2.2d	v21, v22, v4
	WORD	$0x6e180496	// 0308: mov.d	v22[1], v4[0]
	WORD	$0x4ec27ae8	// 030c: zip2.2d	v8, v23, v2
	WORD	$0x6e180457	// 0310: mov.d	v23[1], v2[0]
	WORD	$0x4ec37b04	// 0314: zip2.2d	v4, v24, v3
	WORD	$0x6e180478	// 0318: mov.d	v24[1], v3[0]
	WORD	$0xad5207e0	// 031c: ldp	q0, q1, [sp, #0x240]
	WORD	$0x4e412802	// 0320: trn1.8h	v2, v0, v1
	WORD	$0x4e416800	// 0324: trn2.8h	v0, v0, v1
	WORD	$0xad530fe1	// 0328: ldp	q1, q3, [sp, #0x260]
	WORD	$0x4e432825	// 032c: trn1.8h	v5, v1, v3
	WORD	$0x4e436821	// 0330: trn2.8h	v1, v1, v3
	WORD	$0xad541be3	// 0334: ldp	q3, q6, [sp, #0x280]
	WORD	$0x4e462867	// 0338: trn1.8h	v7, v3, v6
	WORD	$0x4e466863	// 033c: trn2.8h	v3, v3, v6
	WORD	$0xad5543e6	// 0340: ldp	q6, q16, [sp, #0x2a0]
	WORD	$0x4e5028d1	// 0344: trn1.8h	v17, v6, v16
	WORD	$0x4e5068d0	// 0348: trn2.8h	v16, v6, v16
	WORD	$0x4e852852	// 034c: trn1.4s	v18, v2, v5
	WORD	$0x4e856859	// 0350: trn2.4s	v25, v2, v5
	WORD	$0x4e81281a	// 0354: trn1.4s	v26, v0, v1
	WORD	$0x4e81681b	// 0358: trn2.4s	v27, v0, v1
	WORD	$0x4e9128e0	// 035c: trn1.4s	v0, v7, v17
	WORD	$0x4e9168e6	// 0360: trn2.4s	v6, v7, v17
	WORD	$0x4e902861	// 0364: trn1.4s	v1, v3, v16
	WORD	$0x4e906865	// 0368: trn2.4s	v5, v3, v16
	WORD	$0x4ec07a50	// 036c: zip2.2d	v16, v18, v0
	WORD	$0x4eb21e42	// 0370: mov.16b	v2, v18
	WORD	$0x4ec17b52	// 0374: zip2.2d	v18, v26, v1
	WORD	$0x4eba1f43	// 0378: mov.16b	v3, v26
	WORD	$0x4ec67b3a	// 037c: zip2.2d	v26, v25, v6
	WORD	$0x4eb91f27	// 0380: mov.16b	v7, v25
	WORD	$0x4ec57b6a	// 0384: zip2.2d	v10, v27, v5
	WORD	$0x4ebb1f71	// 0388: mov.16b	v17, v27
	WORD	$0x4f00842b	// 038c: movi.8h	v11, #0x1
	WORD	$0x4e6b869f	// 0390: add.8h	v31, v20, v11
	WORD	$0x4f1e07e9	// 0394: sshr.8h	v9, v31, #0x2
	WORD	$0x4e6b86dd	// 0398: add.8h	v29, v22, v11
	WORD	$0x4f1e07be	// 039c: sshr.8h	v30, v29, #0x2
	WORD	$0xad0a7be9	// 03a0: stp	q9, q30, [sp, #0x140]
	WORD	$0x4e6b86f7	// 03a4: add.8h	v23, v23, v11
	WORD	$0x4f1e06fb	// 03a8: sshr.8h	v27, v23, #0x2
	WORD	$0x4e6b8719	// 03ac: add.8h	v25, v24, v11
	WORD	$0x4f1e073c	// 03b0: sshr.8h	v28, v25, #0x2
	WORD	$0xad0b73fb	// 03b4: stp	q27, q28, [sp, #0x160]
	WORD	$0x4e6b8674	// 03b8: add.8h	v20, v19, v11
	WORD	$0x4f1e0698	// 03bc: sshr.8h	v24, v20, #0x2
	WORD	$0x4e6b86b5	// 03c0: add.8h	v21, v21, v11
	WORD	$0x4f1e06b6	// 03c4: sshr.8h	v22, v21, #0x2
	WORD	$0xad0c5bf8	// 03c8: stp	q24, q22, [sp, #0x180]
	WORD	$0x4e6b8513	// 03cc: add.8h	v19, v8, v11
	WORD	$0x4e6b8548	// 03d0: add.8h	v8, v10, v11
	WORD	$0x4f1e0508	// 03d4: sshr.8h	v8, v8, #0x2
	WORD	$0x6e688529	// 03d8: sub.8h	v9, v9, v8
	WORD	$0x4f1e17e8	// 03dc: ssra.8h	v8, v31, #0x2
	WORD	$0x4f1e067f	// 03e0: sshr.8h	v31, v19, #0x2
	WORD	$0x4e6b8484	// 03e4: add.8h	v4, v4, v11
	WORD	$0x4e6b875a	// 03e8: add.8h	v26, v26, v11
	WORD	$0x4f1e075a	// 03ec: sshr.8h	v26, v26, #0x2
	WORD	$0x6e7a87de	// 03f0: sub.8h	v30, v30, v26
	WORD	$0x4f1e17ba	// 03f4: ssra.8h	v26, v29, #0x2
	WORD	$0x4f1e049d	// 03f8: sshr.8h	v29, v4, #0x2
	WORD	$0xad0d77ff	// 03fc: stp	q31, q29, [sp, #0x1a0]
	WORD	$0x4e6b8610	// 0400: add.8h	v16, v16, v11
	WORD	$0x4f1e0610	// 0404: sshr.8h	v16, v16, #0x2
	WORD	$0x4e6b8652	// 0408: add.8h	v18, v18, v11
	WORD	$0x4f1e0652	// 040c: sshr.8h	v18, v18, #0x2
	WORD	$0xad026be8	// 0410: stp	q8, q26, [sp, #0x40]
	WORD	$0x6e72877a	// 0414: sub.8h	v26, v27, v18
	WORD	$0x6e70879b	// 0418: sub.8h	v27, v28, v16
	WORD	$0x6e1804c7	// 041c: mov.d	v7[1], v6[0]
	WORD	$0x4f1e16f2	// 0420: ssra.8h	v18, v23, #0x2
	WORD	$0x4f1e1730	// 0424: ssra.8h	v16, v25, #0x2
	WORD	$0xad0343f2	// 0428: stp	q18, q16, [sp, #0x60]
	WORD	$0x6e1804b1	// 042c: mov.d	v17[1], v5[0]
	WORD	$0x4e6b8625	// 0430: add.8h	v5, v17, v11
	WORD	$0x4f1e04a5	// 0434: sshr.8h	v5, v5, #0x2
	WORD	$0x6e658706	// 0438: sub.8h	v6, v24, v5
	WORD	$0x4e6b84e7	// 043c: add.8h	v7, v7, v11
	WORD	$0x4f1e04e7	// 0440: sshr.8h	v7, v7, #0x2
	WORD	$0x4f1e1685	// 0444: ssra.8h	v5, v20, #0x2
	WORD	$0x6e6786d0	// 0448: sub.8h	v16, v22, v7
	WORD	$0x4f1e16a7	// 044c: ssra.8h	v7, v21, #0x2
	WORD	$0xad041fe5	// 0450: stp	q5, q7, [sp, #0x80]
	WORD	$0x6e180402	// 0454: mov.d	v2[1], v0[0]
	WORD	$0x6e180423	// 0458: mov.d	v3[1], v1[0]
	WORD	$0x4e6b8440	// 045c: add.8h	v0, v2, v11
	WORD	$0x4f1e0400	// 0460: sshr.8h	v0, v0, #0x2
	WORD	$0x4e6b8461	// 0464: add.8h	v1, v3, v11
	WORD	$0x4f1e0421	// 0468: sshr.8h	v1, v1, #0x2
	WORD	$0x6e6187e2	// 046c: sub.8h	v2, v31, v1
	WORD	$0x4f1e1661	// 0470: ssra.8h	v1, v19, #0x2
	WORD	$0x6e6087a3	// 0474: sub.8h	v3, v29, v0
	WORD	$0x4f1e1480	// 0478: ssra.8h	v0, v4, #0x2
	WORD	$0xad0503e1	// 047c: stp	q1, q0, [sp, #0xa0]
	WORD	$0xad060be3	// 0480: stp	q3, q2, [sp, #0xc0]
	WORD	$0xad071bf0	// 0484: stp	q16, q6, [sp, #0xe0]
	WORD	$0xad086bfb	// 0488: stp	q27, q26, [sp, #0x100]
	WORD	$0xad0927fe	// 048c: stp	q30, q9, [sp, #0x120]
	WORD	$0x910103e0	// 0490: add	x0, sp, #0x40
	WORD	$0x910503e1	// 0494: add	x1, sp, #0x140
	WORD	$0x94000115	// 0498: bl local fdct8x16 body (+0x8ec)
	WORD	$0xad4a07e0	// 049c: ldp	q0, q1, [sp, #0x140]
	WORD	$0x4e412802	// 04a0: trn1.8h	v2, v0, v1
	WORD	$0x4e416800	// 04a4: trn2.8h	v0, v0, v1
	WORD	$0xad4b0fe1	// 04a8: ldp	q1, q3, [sp, #0x160]
	WORD	$0x4e432824	// 04ac: trn1.8h	v4, v1, v3
	WORD	$0x4e436821	// 04b0: trn2.8h	v1, v1, v3
	WORD	$0xad4c17e3	// 04b4: ldp	q3, q5, [sp, #0x180]
	WORD	$0x4e452866	// 04b8: trn1.8h	v6, v3, v5
	WORD	$0x4e456863	// 04bc: trn2.8h	v3, v3, v5
	WORD	$0xad4d1fe5	// 04c0: ldp	q5, q7, [sp, #0x1a0]
	WORD	$0x4e4728b0	// 04c4: trn1.8h	v16, v5, v7
	WORD	$0x4e4768a7	// 04c8: trn2.8h	v7, v5, v7
	WORD	$0x4e842845	// 04cc: trn1.4s	v5, v2, v4
	WORD	$0x4e846844	// 04d0: trn2.4s	v4, v2, v4
	WORD	$0x4e812811	// 04d4: trn1.4s	v17, v0, v1
	WORD	$0x4e816812	// 04d8: trn2.4s	v18, v0, v1
	WORD	$0x4e9028c0	// 04dc: trn1.4s	v0, v6, v16
	WORD	$0x4e9068d0	// 04e0: trn2.4s	v16, v6, v16
	WORD	$0x4e872873	// 04e4: trn1.4s	v19, v3, v7
	WORD	$0x4ec078a1	// 04e8: zip2.2d	v1, v5, v0
	WORD	$0x4ea51ca6	// 04ec: mov.16b	v6, v5
	WORD	$0x6e180406	// 04f0: mov.d	v6[1], v0[0]
	WORD	$0x4ed37a22	// 04f4: zip2.2d	v2, v17, v19
	WORD	$0x4eb11e25	// 04f8: mov.16b	v5, v17
	WORD	$0x6e180665	// 04fc: mov.d	v5[1], v19[0]
	WORD	$0x4e876871	// 0500: trn2.4s	v17, v3, v7
	WORD	$0x4ed07883	// 0504: zip2.2d	v3, v4, v16
	WORD	$0x6e180604	// 0508: mov.d	v4[1], v16[0]
	WORD	$0x4ed17a40	// 050c: zip2.2d	v0, v18, v17
	WORD	$0x4eb21e47	// 0510: mov.16b	v7, v18
	WORD	$0x6e180627	// 0514: mov.d	v7[1], v17[0]
	WORD	$0xad4e47f0	// 0518: ldp	q16, q17, [sp, #0x1c0]
	WORD	$0x4e512a12	// 051c: trn1.8h	v18, v16, v17
	WORD	$0x4e516a10	// 0520: trn2.8h	v16, v16, v17
	WORD	$0xad4f4ff1	// 0524: ldp	q17, q19, [sp, #0x1e0]
	WORD	$0x4e532a34	// 0528: trn1.8h	v20, v17, v19
	WORD	$0x4e536a31	// 052c: trn2.8h	v17, v17, v19
	WORD	$0xad5057f3	// 0530: ldp	q19, q21, [sp, #0x200]
	WORD	$0x4e552a76	// 0534: trn1.8h	v22, v19, v21
	WORD	$0x4e556a73	// 0538: trn2.8h	v19, v19, v21
	WORD	$0xad515ff5	// 053c: ldp	q21, q23, [sp, #0x220]
	WORD	$0x4e572ab8	// 0540: trn1.8h	v24, v21, v23
	WORD	$0x4e576ab5	// 0544: trn2.8h	v21, v21, v23
	WORD	$0x4e942a57	// 0548: trn1.4s	v23, v18, v20
	WORD	$0x4e946a54	// 054c: trn2.4s	v20, v18, v20
	WORD	$0x4e912a12	// 0550: trn1.4s	v18, v16, v17
	WORD	$0x4e916a19	// 0554: trn2.4s	v25, v16, v17
	WORD	$0x4e982ad1	// 0558: trn1.4s	v17, v22, v24
	WORD	$0x4e986ad6	// 055c: trn2.4s	v22, v22, v24
	WORD	$0x4e952a78	// 0560: trn1.4s	v24, v19, v21
	WORD	$0x4ed17af0	// 0564: zip2.2d	v16, v23, v17
	WORD	$0x6e180637	// 0568: mov.d	v23[1], v17[0]
	WORD	$0x4e956a73	// 056c: trn2.4s	v19, v19, v21
	WORD	$0x4ed87a51	// 0570: zip2.2d	v17, v18, v24
	WORD	$0x4eb21e55	// 0574: mov.16b	v21, v18
	WORD	$0x6e180715	// 0578: mov.d	v21[1], v24[0]
	WORD	$0x4ed67a92	// 057c: zip2.2d	v18, v20, v22
	WORD	$0x4eb41e98	// 0580: mov.16b	v24, v20
	WORD	$0x6e1806d8	// 0584: mov.d	v24[1], v22[0]
	WORD	$0x4eb91f34	// 0588: mov.16b	v20, v25
	WORD	$0x6e180674	// 058c: mov.d	v20[1], v19[0]
	WORD	$0x4ed37b33	// 0590: zip2.2d	v19, v25, v19
	WORD	$0xad005e66	// 0594: stp	q6, q23, [x19]
	WORD	$0xad015665	// 0598: stp	q5, q21, [x19, #0x20]
	WORD	$0xad5e1be5	// 059c: ldp	q5, q6, [sp, #0x3c0]
	WORD	$0x4e4628b5	// 05a0: trn1.8h	v21, v5, v6
	WORD	$0x4e4668a5	// 05a4: trn2.8h	v5, v5, v6
	WORD	$0xad5f5be6	// 05a8: ldp	q6, q22, [sp, #0x3e0]
	WORD	$0x4e5628d7	// 05ac: trn1.8h	v23, v6, v22
	WORD	$0x4e5668c6	// 05b0: trn2.8h	v6, v6, v22
	WORD	$0x3dc103f6	// 05b4: ldr	q22, [sp, #0x400]
	WORD	$0x3dc107f9	// 05b8: ldr	q25, [sp, #0x410]
	WORD	$0x4e592ada	// 05bc: trn1.8h	v26, v22, v25
	WORD	$0x4e596ad9	// 05c0: trn2.8h	v25, v22, v25
	WORD	$0x3dc10bf6	// 05c4: ldr	q22, [sp, #0x420]
	WORD	$0x3dc10ffb	// 05c8: ldr	q27, [sp, #0x430]
	WORD	$0x4e5b2adc	// 05cc: trn1.8h	v28, v22, v27
	WORD	$0x4e5b6adb	// 05d0: trn2.8h	v27, v22, v27
	WORD	$0x4e972ab6	// 05d4: trn1.4s	v22, v21, v23
	WORD	$0xad026264	// 05d8: stp	q4, q24, [x19, #0x40]
	WORD	$0x4e976ab8	// 05dc: trn2.4s	v24, v21, v23
	WORD	$0x4e8628b7	// 05e0: trn1.4s	v23, v5, v6
	WORD	$0x4e8668be	// 05e4: trn2.4s	v30, v5, v6
	WORD	$0x4e9c2b44	// 05e8: trn1.4s	v4, v26, v28
	WORD	$0x4e9c6b46	// 05ec: trn2.4s	v6, v26, v28
	WORD	$0x4e9b2b25	// 05f0: trn1.4s	v5, v25, v27
	WORD	$0x4ec47ad5	// 05f4: zip2.2d	v21, v22, v4
	WORD	$0x6e180496	// 05f8: mov.d	v22[1], v4[0]
	WORD	$0x4ec57ae4	// 05fc: zip2.2d	v4, v23, v5
	WORD	$0x6e1804b7	// 0600: mov.d	v23[1], v5[0]
	WORD	$0x4ec67b05	// 0604: zip2.2d	v5, v24, v6
	WORD	$0x6e1804d8	// 0608: mov.d	v24[1], v6[0]
	WORD	$0x4e9b6b39	// 060c: trn2.4s	v25, v25, v27
	WORD	$0x4ed97bc6	// 0610: zip2.2d	v6, v30, v25
	WORD	$0x6e18073e	// 0614: mov.d	v30[1], v25[0]
	WORD	$0xad035267	// 0618: stp	q7, q20, [x19, #0x60]
	WORD	$0xad5653e7	// 061c: ldp	q7, q20, [sp, #0x2c0]
	WORD	$0x4e5428f9	// 0620: trn1.8h	v25, v7, v20
	WORD	$0x4e5468e7	// 0624: trn2.8h	v7, v7, v20
	WORD	$0xad576bf4	// 0628: ldp	q20, q26, [sp, #0x2e0]
	WORD	$0x4e5a2a9b	// 062c: trn1.8h	v27, v20, v26
	WORD	$0x4e5a6a94	// 0630: trn2.8h	v20, v20, v26
	WORD	$0xad5873fa	// 0634: ldp	q26, q28, [sp, #0x300]
	WORD	$0x4e5c2b5d	// 0638: trn1.8h	v29, v26, v28
	WORD	$0x4e5c6b5a	// 063c: trn2.8h	v26, v26, v28
	WORD	$0xad597ffc	// 0640: ldp	q28, q31, [sp, #0x320]
	WORD	$0x4e5f2b88	// 0644: trn1.8h	v8, v28, v31
	WORD	$0x4e5f6b9c	// 0648: trn2.8h	v28, v28, v31
	WORD	$0x4e9b2b3f	// 064c: trn1.4s	v31, v25, v27
	WORD	$0xad044261	// 0650: stp	q1, q16, [x19, #0x80]
	WORD	$0x4e9b6b39	// 0654: trn2.4s	v25, v25, v27
	WORD	$0x4e9428fb	// 0658: trn1.4s	v27, v7, v20
	WORD	$0x4e9468e9	// 065c: trn2.4s	v9, v7, v20
	WORD	$0xad054662	// 0660: stp	q2, q17, [x19, #0xa0]
	WORD	$0x4e882ba1	// 0664: trn1.4s	v1, v29, v8
	WORD	$0x4e886bb1	// 0668: trn2.4s	v17, v29, v8
	WORD	$0x4e9c2b42	// 066c: trn1.4s	v2, v26, v28
	WORD	$0xad064a63	// 0670: stp	q3, q18, [x19, #0xc0]
	WORD	$0x4e9c6b50	// 0674: trn2.4s	v16, v26, v28
	WORD	$0x4ec17bf2	// 0678: zip2.2d	v18, v31, v1
	WORD	$0x4ebf1fe3	// 067c: mov.16b	v3, v31
	WORD	$0x4ec27b74	// 0680: zip2.2d	v20, v27, v2
	WORD	$0x4ebb1f67	// 0684: mov.16b	v7, v27
	WORD	$0xad074e60	// 0688: stp	q0, q19, [x19, #0xe0]
	WORD	$0x4ed17b3f	// 068c: zip2.2d	v31, v25, v17
	WORD	$0x4eb91f20	// 0690: mov.16b	v0, v25
	WORD	$0x4ed07928	// 0694: zip2.2d	v8, v9, v16
	WORD	$0x4ea91d33	// 0698: mov.16b	v19, v9
	WORD	$0x4f008429	// 069c: movi.8h	v9, #0x1
	WORD	$0x4e6986da	// 06a0: add.8h	v26, v22, v9
	WORD	$0x4f1e075c	// 06a4: sshr.8h	v28, v26, #0x2
	WORD	$0x4e6986fb	// 06a8: add.8h	v27, v23, v9
	WORD	$0x4f1e077d	// 06ac: sshr.8h	v29, v27, #0x2
	WORD	$0xad1277fc	// 06b0: stp	q28, q29, [sp, #0x240]
	WORD	$0x4e698716	// 06b4: add.8h	v22, v24, v9
	WORD	$0x4f1e06d9	// 06b8: sshr.8h	v25, v22, #0x2
	WORD	$0x4e6987d7	// 06bc: add.8h	v23, v30, v9
	WORD	$0x4f1e06f8	// 06c0: sshr.8h	v24, v23, #0x2
	WORD	$0xad1363f9	// 06c4: stp	q25, q24, [sp, #0x260]
	WORD	$0x4e6986b5	// 06c8: add.8h	v21, v21, v9
	WORD	$0x4e6987fe	// 06cc: add.8h	v30, v31, v9
	WORD	$0x4f1e07de	// 06d0: sshr.8h	v30, v30, #0x2
	WORD	$0x4e69851f	// 06d4: add.8h	v31, v8, v9
	WORD	$0x4f1e07ff	// 06d8: sshr.8h	v31, v31, #0x2
	WORD	$0x6e7f879c	// 06dc: sub.8h	v28, v28, v31
	WORD	$0x6e7e87bd	// 06e0: sub.8h	v29, v29, v30
	WORD	$0x4f1e175f	// 06e4: ssra.8h	v31, v26, #0x2
	WORD	$0x4f1e06ba	// 06e8: sshr.8h	v26, v21, #0x2
	WORD	$0x4e698484	// 06ec: add.8h	v4, v4, v9
	WORD	$0x4f1e177e	// 06f0: ssra.8h	v30, v27, #0x2
	WORD	$0x4f1e049b	// 06f4: sshr.8h	v27, v4, #0x2
	WORD	$0xad146ffa	// 06f8: stp	q26, q27, [sp, #0x280]
	WORD	$0x4e6984a5	// 06fc: add.8h	v5, v5, v9
	WORD	$0xad1a7bff	// 0700: stp	q31, q30, [sp, #0x340]
	WORD	$0x4f1e04be	// 0704: sshr.8h	v30, v5, #0x2
	WORD	$0x4e6984c6	// 0708: add.8h	v6, v6, v9
	WORD	$0x4f1e04df	// 070c: sshr.8h	v31, v6, #0x2
	WORD	$0xad157ffe	// 0710: stp	q30, q31, [sp, #0x2a0]
	WORD	$0x4e698694	// 0714: add.8h	v20, v20, v9
	WORD	$0x4f1e0694	// 0718: sshr.8h	v20, v20, #0x2
	WORD	$0x6e748739	// 071c: sub.8h	v25, v25, v20
	WORD	$0x4e698652	// 0720: add.8h	v18, v18, v9
	WORD	$0x4f1e0652	// 0724: sshr.8h	v18, v18, #0x2
	WORD	$0x6e728718	// 0728: sub.8h	v24, v24, v18
	WORD	$0x6e180620	// 072c: mov.d	v0[1], v17[0]
	WORD	$0x4f1e16d4	// 0730: ssra.8h	v20, v22, #0x2
	WORD	$0x4f1e16f2	// 0734: ssra.8h	v18, v23, #0x2
	WORD	$0xad1b4bf4	// 0738: stp	q20, q18, [sp, #0x360]
	WORD	$0x6e180613	// 073c: mov.d	v19[1], v16[0]
	WORD	$0x4e698670	// 0740: add.8h	v16, v19, v9
	WORD	$0x4f1e0610	// 0744: sshr.8h	v16, v16, #0x2
	WORD	$0x6e708751	// 0748: sub.8h	v17, v26, v16
	WORD	$0x4e698400	// 074c: add.8h	v0, v0, v9
	WORD	$0x4f1e0400	// 0750: sshr.8h	v0, v0, #0x2
	WORD	$0x4f1e16b0	// 0754: ssra.8h	v16, v21, #0x2
	WORD	$0x6e608772	// 0758: sub.8h	v18, v27, v0
	WORD	$0x4f1e1480	// 075c: ssra.8h	v0, v4, #0x2
	WORD	$0xad1c03f0	// 0760: stp	q16, q0, [sp, #0x380]
	WORD	$0x6e180423	// 0764: mov.d	v3[1], v1[0]
	WORD	$0x6e180447	// 0768: mov.d	v7[1], v2[0]
	WORD	$0x4e698460	// 076c: add.8h	v0, v3, v9
	WORD	$0x4f1e0400	// 0770: sshr.8h	v0, v0, #0x2
	WORD	$0x4e6984e1	// 0774: add.8h	v1, v7, v9
	WORD	$0x4f1e0421	// 0778: sshr.8h	v1, v1, #0x2
	WORD	$0x6e6187c2	// 077c: sub.8h	v2, v30, v1
	WORD	$0x4f1e14a1	// 0780: ssra.8h	v1, v5, #0x2
	WORD	$0x6e6087e3	// 0784: sub.8h	v3, v31, v0
	WORD	$0x4f1e14c0	// 0788: ssra.8h	v0, v6, #0x2
	WORD	$0xad1d03e1	// 078c: stp	q1, q0, [sp, #0x3a0]
	WORD	$0xad1e0be3	// 0790: stp	q3, q2, [sp, #0x3c0]
	WORD	$0xad1f47f2	// 0794: stp	q18, q17, [sp, #0x3e0]
	WORD	$0x3d8103f8	// 0798: str	q24, [sp, #0x400]
	WORD	$0x3d8107f9	// 079c: str	q25, [sp, #0x410]
	WORD	$0x3d810bfd	// 07a0: str	q29, [sp, #0x420]
	WORD	$0x3d810ffc	// 07a4: str	q28, [sp, #0x430]
	WORD	$0x910d03e0	// 07a8: add	x0, sp, #0x340
	WORD	$0x910903e1	// 07ac: add	x1, sp, #0x240
	WORD	$0x9400004f	// 07b0: bl local fdct8x16 body (+0x8ec)
	WORD	$0xad5207e0	// 07b4: ldp	q0, q1, [sp, #0x240]
	WORD	$0x4e412802	// 07b8: trn1.8h	v2, v0, v1
	WORD	$0x4e416800	// 07bc: trn2.8h	v0, v0, v1
	WORD	$0xad530fe1	// 07c0: ldp	q1, q3, [sp, #0x260]
	WORD	$0x4e432824	// 07c4: trn1.8h	v4, v1, v3
	WORD	$0x4e436821	// 07c8: trn2.8h	v1, v1, v3
	WORD	$0xad5417e3	// 07cc: ldp	q3, q5, [sp, #0x280]
	WORD	$0x4e452866	// 07d0: trn1.8h	v6, v3, v5
	WORD	$0x4e456863	// 07d4: trn2.8h	v3, v3, v5
	WORD	$0xad551fe5	// 07d8: ldp	q5, q7, [sp, #0x2a0]
	WORD	$0x4e4728b0	// 07dc: trn1.8h	v16, v5, v7
	WORD	$0x4e4768a5	// 07e0: trn2.8h	v5, v5, v7
	WORD	$0x4e842847	// 07e4: trn1.4s	v7, v2, v4
	WORD	$0x4e846844	// 07e8: trn2.4s	v4, v2, v4
	WORD	$0x4e812802	// 07ec: trn1.4s	v2, v0, v1
	WORD	$0x4e816811	// 07f0: trn2.4s	v17, v0, v1
	WORD	$0x4e9028c1	// 07f4: trn1.4s	v1, v6, v16
	WORD	$0x4e9068d0	// 07f8: trn2.4s	v16, v6, v16
	WORD	$0x4e852866	// 07fc: trn1.4s	v6, v3, v5
	WORD	$0x4e856872	// 0800: trn2.4s	v18, v3, v5
	WORD	$0x4ec178e0	// 0804: zip2.2d	v0, v7, v1
	WORD	$0x4ea71ce5	// 0808: mov.16b	v5, v7
	WORD	$0x6e180425	// 080c: mov.d	v5[1], v1[0]
	WORD	$0x4ec67841	// 0810: zip2.2d	v1, v2, v6
	WORD	$0x4ea21c43	// 0814: mov.16b	v3, v2
	WORD	$0x6e1804c3	// 0818: mov.d	v3[1], v6[0]
	WORD	$0x4ed07882	// 081c: zip2.2d	v2, v4, v16
	WORD	$0x4ea41c86	// 0820: mov.16b	v6, v4
	WORD	$0x6e180606	// 0824: mov.d	v6[1], v16[0]
	WORD	$0x4ed27a24	// 0828: zip2.2d	v4, v17, v18
	WORD	$0x4eb11e27	// 082c: mov.16b	v7, v17
	WORD	$0x6e180647	// 0830: mov.d	v7[1], v18[0]
	WORD	$0xad5647f0	// 0834: ldp	q16, q17, [sp, #0x2c0]
	WORD	$0x4e512a12	// 0838: trn1.8h	v18, v16, v17
	WORD	$0x4e516a10	// 083c: trn2.8h	v16, v16, v17
	WORD	$0xad574ff1	// 0840: ldp	q17, q19, [sp, #0x2e0]
	WORD	$0x4e532a34	// 0844: trn1.8h	v20, v17, v19
	WORD	$0x4e536a31	// 0848: trn2.8h	v17, v17, v19
	WORD	$0xad5857f3	// 084c: ldp	q19, q21, [sp, #0x300]
	WORD	$0x4e552a76	// 0850: trn1.8h	v22, v19, v21
	WORD	$0x4e556a73	// 0854: trn2.8h	v19, v19, v21
	WORD	$0xad595ff5	// 0858: ldp	q21, q23, [sp, #0x320]
	WORD	$0x4e572ab8	// 085c: trn1.8h	v24, v21, v23
	WORD	$0x4e576ab5	// 0860: trn2.8h	v21, v21, v23
	WORD	$0x4e942a57	// 0864: trn1.4s	v23, v18, v20
	WORD	$0x4e946a52	// 0868: trn2.4s	v18, v18, v20
	WORD	$0x4e912a14	// 086c: trn1.4s	v20, v16, v17
	WORD	$0x4e916a10	// 0870: trn2.4s	v16, v16, v17
	WORD	$0x4e982ad1	// 0874: trn1.4s	v17, v22, v24
	WORD	$0x4e986ad6	// 0878: trn2.4s	v22, v22, v24
	WORD	$0x4ed17af8	// 087c: zip2.2d	v24, v23, v17
	WORD	$0x6e180637	// 0880: mov.d	v23[1], v17[0]
	WORD	$0x4e952a71	// 0884: trn1.4s	v17, v19, v21
	WORD	$0x4ed17a99	// 0888: zip2.2d	v25, v20, v17
	WORD	$0x6e180634	// 088c: mov.d	v20[1], v17[0]
	WORD	$0x4e956a71	// 0890: trn2.4s	v17, v19, v21
	WORD	$0x4ed67a53	// 0894: zip2.2d	v19, v18, v22
	WORD	$0x6e1806d2	// 0898: mov.d	v18[1], v22[0]
	WORD	$0x4ed17a15	// 089c: zip2.2d	v21, v16, v17
	WORD	$0x6e180630	// 08a0: mov.d	v16[1], v17[0]
	WORD	$0xad085e65	// 08a4: stp	q5, q23, [x19, #0x100]
	WORD	$0xad095263	// 08a8: stp	q3, q20, [x19, #0x120]
	WORD	$0xad0a4a66	// 08ac: stp	q6, q18, [x19, #0x140]
	WORD	$0xad0b4267	// 08b0: stp	q7, q16, [x19, #0x160]
	WORD	$0xad0c6260	// 08b4: stp	q0, q24, [x19, #0x180]
	WORD	$0xad0d6661	// 08b8: stp	q1, q25, [x19, #0x1a0]
	WORD	$0xad0e4e62	// 08bc: stp	q2, q19, [x19, #0x1c0]
	WORD	$0xad0f5664	// 08c0: stp	q4, q21, [x19, #0x1e0]
	WORD	$0x911143ff	// 08c4: add	sp, sp, #0x450
	WORD	$0xa9477bfd	// 08c8: ldp	x29, x30, [sp, #0x70]
	WORD	$0xa9464ff4	// 08cc: ldp	x20, x19, [sp, #0x60]
	WORD	$0xa94557f6	// 08d0: ldp	x22, x21, [sp, #0x50]
	WORD	$0xa9445ff8	// 08d4: ldp	x24, x23, [sp, #0x40]
	WORD	$0xa94367fa	// 08d8: ldp	x26, x25, [sp, #0x30]
	WORD	$0xa9426ffc	// 08dc: ldp	x28, x27, [sp, #0x20]
	WORD	$0x6d4123e9	// 08e0: ldp	d9, d8, [sp, #0x10]
	WORD	$0x6cc82beb	// 08e4: ldp	d11, d10, [sp], #0x80
	WORD	$0xd65f03c0	// 08e8: ret

// vpx_fdct8x16_body inlined as a local raw helper target.
	WORD	$0xad400400	// 08ec: ldp	q0, q1, [x0]
	WORD	$0xad430803	// 08f0: ldp	q3, q2, [x0, #0x60]
	WORD	$0x4e608444	// 08f4: add.8h	v4, v2, v0
	WORD	$0x4e618465	// 08f8: add.8h	v5, v3, v1
	WORD	$0xad411c06	// 08fc: ldp	q6, q7, [x0, #0x20]
	WORD	$0xad424011	// 0900: ldp	q17, q16, [x0, #0x40]
	WORD	$0x4e668612	// 0904: add.8h	v18, v16, v6
	WORD	$0x4e678633	// 0908: add.8h	v19, v17, v7
	WORD	$0x6e7184e7	// 090c: sub.8h	v7, v7, v17
	WORD	$0x6e7084c6	// 0910: sub.8h	v6, v6, v16
	WORD	$0x6e638423	// 0914: sub.8h	v3, v1, v3
	WORD	$0x6e628410	// 0918: sub.8h	v16, v0, v2
	WORD	$0x4e648660	// 091c: add.8h	v0, v19, v4
	WORD	$0x4e658641	// 0920: add.8h	v1, v18, v5
	WORD	$0x6e7284a2	// 0924: sub.8h	v2, v5, v18
	WORD	$0x6e738484	// 0928: sub.8h	v4, v4, v19
	WORD	$0x0e610005	// 092c: saddl.4s	v5, v0, v1
	WORD	$0x52ab5048	// 0930: mov	w8, #0x5a820000         ; =1518469120
	WORD	$0x4e040d11	// 0934: dup.4s	v17, w8
	WORD	$0x6eb1b4a5	// 0938: sqrdmulh.4s	v5, v5, v17
	WORD	$0x4e610012	// 093c: saddl2.4s	v18, v0, v1
	WORD	$0x6eb1b652	// 0940: sqrdmulh.4s	v18, v18, v17
	WORD	$0x0e612013	// 0944: ssubl.4s	v19, v0, v1
	WORD	$0x6eb1b673	// 0948: sqrdmulh.4s	v19, v19, v17
	WORD	$0x4e612000	// 094c: ssubl2.4s	v0, v0, v1
	WORD	$0x6eb1b400	// 0950: sqrdmulh.4s	v0, v0, v17
	WORD	$0x4e5218a1	// 0954: uzp1.8h	v1, v5, v18
	WORD	$0x3d800021	// 0958: str	q1, [x1]
	WORD	$0x4e401a60	// 095c: uzp1.8h	v0, v19, v0
	WORD	$0x3d802020	// 0960: str	q0, [x1, #0x80]
	WORD	$0x52876428	// 0964: mov	w8, #0x3b21             ; =15137
	WORD	$0x4e020d01	// 0968: dup.8h	v1, w8
	WORD	$0x52830fc8	// 096c: mov	w8, #0x187e             ; =6270
	WORD	$0x4e020d00	// 0970: dup.8h	v0, w8
	WORD	$0x0e60c085	// 0974: smull.4s	v5, v4, v0
	WORD	$0x4e60c091	// 0978: smull2.4s	v17, v4, v0
	WORD	$0x0e60c052	// 097c: smull.4s	v18, v2, v0
	WORD	$0x0e618092	// 0980: smlal.4s	v18, v4, v1
	WORD	$0x4e60c053	// 0984: smull2.4s	v19, v2, v0
	WORD	$0x4e618093	// 0988: smlal2.4s	v19, v4, v1
	WORD	$0x0e61a045	// 098c: smlsl.4s	v5, v2, v1
	WORD	$0x4e61a051	// 0990: smlsl2.4s	v17, v2, v1
	WORD	$0x0f129e42	// 0994: sqrshrn.4h	v2, v18, #0xe
	WORD	$0x0f129ca4	// 0998: sqrshrn.4h	v4, v5, #0xe
	WORD	$0x4f129e62	// 099c: sqrshrn2.8h	v2, v19, #0xe
	WORD	$0x3d801022	// 09a0: str	q2, [x1, #0x40]
	WORD	$0x4f129e24	// 09a4: sqrshrn2.8h	v4, v17, #0xe
	WORD	$0x3d803024	// 09a8: str	q4, [x1, #0xc0]
	WORD	$0x4e6384c4	// 09ac: add.8h	v4, v6, v3
	WORD	$0x528b5048	// 09b0: mov	w8, #0x5a82             ; =23170
	WORD	$0x4e020d02	// 09b4: dup.8h	v2, w8
	WORD	$0x6e62b484	// 09b8: sqrdmulh.8h	v4, v4, v2
	WORD	$0x6e668463	// 09bc: sub.8h	v3, v3, v6
	WORD	$0x6e62b463	// 09c0: sqrdmulh.8h	v3, v3, v2
	WORD	$0x4e678465	// 09c4: add.8h	v5, v3, v7
	WORD	$0x6e6384e3	// 09c8: sub.8h	v3, v7, v3
	WORD	$0x6e648606	// 09cc: sub.8h	v6, v16, v4
	WORD	$0x5287d8a8	// 09d0: mov	w8, #0x3ec5             ; =16069
	WORD	$0x4e020d07	// 09d4: dup.8h	v7, w8
	WORD	$0x4e708484	// 09d8: add.8h	v4, v4, v16
	WORD	$0x52818f88	// 09dc: mov	w8, #0xc7c              ; =3196
	WORD	$0x4e020d10	// 09e0: dup.8h	v16, w8
	WORD	$0x0e70c091	// 09e4: smull.4s	v17, v4, v16
	WORD	$0x4e70c092	// 09e8: smull2.4s	v18, v4, v16
	WORD	$0x0e70c0b3	// 09ec: smull.4s	v19, v5, v16
	WORD	$0x0e678093	// 09f0: smlal.4s	v19, v4, v7
	WORD	$0x4e70c0b0	// 09f4: smull2.4s	v16, v5, v16
	WORD	$0x4e678090	// 09f8: smlal2.4s	v16, v4, v7
	WORD	$0x0e67a0b1	// 09fc: smlsl.4s	v17, v5, v7
	WORD	$0x4e67a0b2	// 0a00: smlsl2.4s	v18, v5, v7
	WORD	$0x0f129e64	// 0a04: sqrshrn.4h	v4, v19, #0xe
	WORD	$0x0f129e25	// 0a08: sqrshrn.4h	v5, v17, #0xe
	WORD	$0x4f129e04	// 0a0c: sqrshrn2.8h	v4, v16, #0xe
	WORD	$0x3d800824	// 0a10: str	q4, [x1, #0x20]
	WORD	$0x4f129e45	// 0a14: sqrshrn2.8h	v5, v18, #0xe
	WORD	$0x3d803825	// 0a18: str	q5, [x1, #0xe0]
	WORD	$0x528471c8	// 0a1c: mov	w8, #0x238e             ; =9102
	WORD	$0x4e020d04	// 0a20: dup.8h	v4, w8
	WORD	$0x5286a6e8	// 0a24: mov	w8, #0x3537             ; =13623
	WORD	$0x4e020d05	// 0a28: dup.8h	v5, w8
	WORD	$0x0e65c0c7	// 0a2c: smull.4s	v7, v6, v5
	WORD	$0x4e65c0d0	// 0a30: smull2.4s	v16, v6, v5
	WORD	$0x0e65c071	// 0a34: smull.4s	v17, v3, v5
	WORD	$0x0e6480d1	// 0a38: smlal.4s	v17, v6, v4
	WORD	$0x4e65c065	// 0a3c: smull2.4s	v5, v3, v5
	WORD	$0x4e6480c5	// 0a40: smlal2.4s	v5, v6, v4
	WORD	$0x0e64a067	// 0a44: smlsl.4s	v7, v3, v4
	WORD	$0x4e64a070	// 0a48: smlsl2.4s	v16, v3, v4
	WORD	$0x0f129e23	// 0a4c: sqrshrn.4h	v3, v17, #0xe
	WORD	$0x0f129ce4	// 0a50: sqrshrn.4h	v4, v7, #0xe
	WORD	$0x4f129ca3	// 0a54: sqrshrn2.8h	v3, v5, #0xe
	WORD	$0x3d802823	// 0a58: str	q3, [x1, #0xa0]
	WORD	$0x4f129e04	// 0a5c: sqrshrn2.8h	v4, v16, #0xe
	WORD	$0x3d801824	// 0a60: str	q4, [x1, #0x60]
	WORD	$0xad460c04	// 0a64: ldp	q4, q3, [x0, #0xc0]
	WORD	$0xad451805	// 0a68: ldp	q5, q6, [x0, #0xa0]
	WORD	$0x4e6384a7	// 0a6c: add.8h	v7, v5, v3
	WORD	$0x6e62b4e7	// 0a70: sqrdmulh.8h	v7, v7, v2
	WORD	$0x6e658463	// 0a74: sub.8h	v3, v3, v5
	WORD	$0x6e62b463	// 0a78: sqrdmulh.8h	v3, v3, v2
	WORD	$0x4e6484c5	// 0a7c: add.8h	v5, v6, v4
	WORD	$0x6e62b4a5	// 0a80: sqrdmulh.8h	v5, v5, v2
	WORD	$0x6e668484	// 0a84: sub.8h	v4, v4, v6
	WORD	$0x6e62b482	// 0a88: sqrdmulh.8h	v2, v4, v2
	WORD	$0xad441804	// 0a8c: ldp	q4, q6, [x0, #0x80]
	WORD	$0x4e628490	// 0a90: add.8h	v16, v4, v2
	WORD	$0x4e6384d1	// 0a94: add.8h	v17, v6, v3
	WORD	$0x6e6384c3	// 0a98: sub.8h	v3, v6, v3
	WORD	$0x6e628482	// 0a9c: sub.8h	v2, v4, v2
	WORD	$0xad471006	// 0aa0: ldp	q6, q4, [x0, #0xe0]
	WORD	$0x6e658492	// 0aa4: sub.8h	v18, v4, v5
	WORD	$0x6e6784d3	// 0aa8: sub.8h	v19, v6, v7
	WORD	$0x4e6784c6	// 0aac: add.8h	v6, v6, v7
	WORD	$0x4e658484	// 0ab0: add.8h	v4, v4, v5
	WORD	$0x0e60c0c5	// 0ab4: smull.4s	v5, v6, v0
	WORD	$0x4e60c0c7	// 0ab8: smull2.4s	v7, v6, v0
	WORD	$0x0e60c234	// 0abc: smull.4s	v20, v17, v0
	WORD	$0x0e6180d4	// 0ac0: smlal.4s	v20, v6, v1
	WORD	$0x4e60c235	// 0ac4: smull2.4s	v21, v17, v0
	WORD	$0x4e6180d5	// 0ac8: smlal2.4s	v21, v6, v1
	WORD	$0x0e61a225	// 0acc: smlsl.4s	v5, v17, v1
	WORD	$0x4e61a227	// 0ad0: smlsl2.4s	v7, v17, v1
	WORD	$0x0f129e86	// 0ad4: sqrshrn.4h	v6, v20, #0xe
	WORD	$0x0f129ca5	// 0ad8: sqrshrn.4h	v5, v5, #0xe
	WORD	$0x4f129ea6	// 0adc: sqrshrn2.8h	v6, v21, #0xe
	WORD	$0x4f129ce5	// 0ae0: sqrshrn2.8h	v5, v7, #0xe
	WORD	$0x0e61c067	// 0ae4: smull.4s	v7, v3, v1
	WORD	$0x4e61c071	// 0ae8: smull2.4s	v17, v3, v1
	WORD	$0x0e61c274	// 0aec: smull.4s	v20, v19, v1
	WORD	$0x0e608074	// 0af0: smlal.4s	v20, v3, v0
	WORD	$0x4e61c261	// 0af4: smull2.4s	v1, v19, v1
	WORD	$0x4e608061	// 0af8: smlal2.4s	v1, v3, v0
	WORD	$0x0e60a267	// 0afc: smlsl.4s	v7, v19, v0
	WORD	$0x4e60a271	// 0b00: smlsl2.4s	v17, v19, v0
	WORD	$0x0f129e83	// 0b04: sqrshrn.4h	v3, v20, #0xe
	WORD	$0x0f129ce7	// 0b08: sqrshrn.4h	v7, v7, #0xe
	WORD	$0x4f129c23	// 0b0c: sqrshrn2.8h	v3, v1, #0xe
	WORD	$0x4f129e27	// 0b10: sqrshrn2.8h	v7, v17, #0xe
	WORD	$0x4e7084b1	// 0b14: add.8h	v17, v5, v16
	WORD	$0x6e658605	// 0b18: sub.8h	v5, v16, v5
	WORD	$0x4e628460	// 0b1c: add.8h	v0, v3, v2
	WORD	$0x6e638442	// 0b20: sub.8h	v2, v2, v3
	WORD	$0x6e678643	// 0b24: sub.8h	v3, v18, v7
	WORD	$0x4e7284e1	// 0b28: add.8h	v1, v7, v18
	WORD	$0x6e668487	// 0b2c: sub.8h	v7, v4, v6
	WORD	$0x52851348	// 0b30: mov	w8, #0x289a             ; =10394
	WORD	$0x4e020d10	// 0b34: dup.8h	v16, w8
	WORD	$0x4e6484c4	// 0b38: add.8h	v4, v6, v4
	WORD	$0x52862f28	// 0b3c: mov	w8, #0x3179             ; =12665
	WORD	$0x4e020d06	// 0b40: dup.8h	v6, w8
	WORD	$0x0e66c0f2	// 0b44: smull.4s	v18, v7, v6
	WORD	$0x4e66c0f3	// 0b48: smull2.4s	v19, v7, v6
	WORD	$0x0e66c0b4	// 0b4c: smull.4s	v20, v5, v6
	WORD	$0x0e7080f4	// 0b50: smlal.4s	v20, v7, v16
	WORD	$0x4e66c0a6	// 0b54: smull2.4s	v6, v5, v6
	WORD	$0x4e7080e6	// 0b58: smlal2.4s	v6, v7, v16
	WORD	$0x0e70a0b2	// 0b5c: smlsl.4s	v18, v5, v16
	WORD	$0x4e70a0b3	// 0b60: smlsl2.4s	v19, v5, v16
	WORD	$0x0f129e85	// 0b64: sqrshrn.4h	v5, v20, #0xe
	WORD	$0x0f129e47	// 0b68: sqrshrn.4h	v7, v18, #0xe
	WORD	$0x4f129cc5	// 0b6c: sqrshrn2.8h	v5, v6, #0xe
	WORD	$0x3d802425	// 0b70: str	q5, [x1, #0x90]
	WORD	$0x4f129e67	// 0b74: sqrshrn2.8h	v7, v19, #0xe
	WORD	$0x3d801c27	// 0b78: str	q7, [x1, #0x70]
	WORD	$0x5287f628	// 0b7c: mov	w8, #0x3fb1             ; =16305
	WORD	$0x4e020d05	// 0b80: dup.8h	v5, w8
	WORD	$0x5280c8c8	// 0b84: mov	w8, #0x646              ; =1606
	WORD	$0x4e020d06	// 0b88: dup.8h	v6, w8
	WORD	$0x0e66c087	// 0b8c: smull.4s	v7, v4, v6
	WORD	$0x4e66c090	// 0b90: smull2.4s	v16, v4, v6
	WORD	$0x0e66c232	// 0b94: smull.4s	v18, v17, v6
	WORD	$0x0e658092	// 0b98: smlal.4s	v18, v4, v5
	WORD	$0x4e66c226	// 0b9c: smull2.4s	v6, v17, v6
	WORD	$0x4e658086	// 0ba0: smlal2.4s	v6, v4, v5
	WORD	$0x0e65a227	// 0ba4: smlsl.4s	v7, v17, v5
	WORD	$0x4e65a230	// 0ba8: smlsl2.4s	v16, v17, v5
	WORD	$0x0f129e44	// 0bac: sqrshrn.4h	v4, v18, #0xe
	WORD	$0x0f129ce5	// 0bb0: sqrshrn.4h	v5, v7, #0xe
	WORD	$0x4f129cc4	// 0bb4: sqrshrn2.8h	v4, v6, #0xe
	WORD	$0x3d800424	// 0bb8: str	q4, [x1, #0x10]
	WORD	$0x4f129e05	// 0bbc: sqrshrn2.8h	v5, v16, #0xe
	WORD	$0x3d803c25	// 0bc0: str	q5, [x1, #0xf0]
	WORD	$0x52825288	// 0bc4: mov	w8, #0x1294             ; =4756
	WORD	$0x4e020d04	// 0bc8: dup.8h	v4, w8
	WORD	$0x5287a7e8	// 0bcc: mov	w8, #0x3d3f             ; =15679
	WORD	$0x4e020d05	// 0bd0: dup.8h	v5, w8
	WORD	$0x0e65c066	// 0bd4: smull.4s	v6, v3, v5
	WORD	$0x4e65c067	// 0bd8: smull2.4s	v7, v3, v5
	WORD	$0x0e65c050	// 0bdc: smull.4s	v16, v2, v5
	WORD	$0x0e648070	// 0be0: smlal.4s	v16, v3, v4
	WORD	$0x4e65c045	// 0be4: smull2.4s	v5, v2, v5
	WORD	$0x4e648065	// 0be8: smlal2.4s	v5, v3, v4
	WORD	$0x0e64a046	// 0bec: smlsl.4s	v6, v2, v4
	WORD	$0x4e64a047	// 0bf0: smlsl2.4s	v7, v2, v4
	WORD	$0x0f129e02	// 0bf4: sqrshrn.4h	v2, v16, #0xe
	WORD	$0x0f129cc3	// 0bf8: sqrshrn.4h	v3, v6, #0xe
	WORD	$0x4f129ca2	// 0bfc: sqrshrn2.8h	v2, v5, #0xe
	WORD	$0x3d803422	// 0c00: str	q2, [x1, #0xd0]
	WORD	$0x4f129ce3	// 0c04: sqrshrn2.8h	v3, v7, #0xe
	WORD	$0x52870e28	// 0c08: mov	w8, #0x3871             ; =14449
	WORD	$0x4e020d02	// 0c0c: dup.8h	v2, w8
	WORD	$0x3d800c23	// 0c10: str	q3, [x1, #0x30]
	WORD	$0x5283c568	// 0c14: mov	w8, #0x1e2b             ; =7723
	WORD	$0x4e020d03	// 0c18: dup.8h	v3, w8
	WORD	$0x0e63c024	// 0c1c: smull.4s	v4, v1, v3
	WORD	$0x4e63c025	// 0c20: smull2.4s	v5, v1, v3
	WORD	$0x0e63c006	// 0c24: smull.4s	v6, v0, v3
	WORD	$0x0e628026	// 0c28: smlal.4s	v6, v1, v2
	WORD	$0x4e63c003	// 0c2c: smull2.4s	v3, v0, v3
	WORD	$0x4e628023	// 0c30: smlal2.4s	v3, v1, v2
	WORD	$0x0e62a004	// 0c34: smlsl.4s	v4, v0, v2
	WORD	$0x4e62a005	// 0c38: smlsl2.4s	v5, v0, v2
	WORD	$0x0f129cc0	// 0c3c: sqrshrn.4h	v0, v6, #0xe
	WORD	$0x0f129c81	// 0c40: sqrshrn.4h	v1, v4, #0xe
	WORD	$0x4f129c60	// 0c44: sqrshrn2.8h	v0, v3, #0xe
	WORD	$0x3d801420	// 0c48: str	q0, [x1, #0x50]
	WORD	$0x4f129ca1	// 0c4c: sqrshrn2.8h	v1, v5, #0xe
	WORD	$0x3d802c21	// 0c50: str	q1, [x1, #0xb0]
	WORD	$0xd65f03c0	// 0c54: ret
