//go:build arm64 && !purego

// VP9 dual (16-lane) 8-tap loopfilter kernels. Generated from libvpx
// v1.16.0 vpx_dsp/arm/loopfilter_neon.c (vpx_lpf_horizontal_8_dual_neon
// and vpx_lpf_vertical_8_dual_neon) with
//   clang -O3 -fomit-frame-pointer -mllvm -inline-threshold=100000
//        -target arm64-apple-macos13
// transcribed instruction-for-instruction so all internal branch
// offsets are preserved. The only edits: the C ABI prologue is replaced
// by Go FP argument loads into the same registers, and the d8/d9
// callee-save push/pop pair is replaced by NOPs (Go asm treats all
// vector registers as caller-saved). The kernels are leaf functions
// with no other stack or callee-saved register use.
//
// Both take the C ABI argument shape: s points at the edge pixel
// (q0 row / column), pitch is the row stride, and the six threshold
// pointers each reference a single byte that the kernel dup-loads.
// The dual kernels filter two adjacent 8-pixel edge segments in one
// 16-lane pass: horizontal covers columns s..s+15, vertical covers
// rows s..s+15*pitch.

#include "textflag.h"

TEXT ·lpfHorizontal8DualNEON(SB), NOSPLIT, $0-32
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	thr0+16(FP), R2
	MOVD	thr1+24(FP), R5
	ADD	$1, R2, R3
	ADD	$2, R2, R4
	ADD	$1, R5, R6
	ADD	$2, R5, R7
	WORD	$0x0d40c0a0	// ld1r.8b { v0 }, [x5]
	WORD	$0x0d40c051	// ld1r.8b { v17 }, [x2]
	WORD	$0x6e180411	// mov.d v17[1], v0[0]
	WORD	$0x0d40c0c0	// ld1r.8b { v0 }, [x6]
	WORD	$0x0d40c072	// ld1r.8b { v18 }, [x3]
	WORD	$0x6e180412	// mov.d v18[1], v0[0]
	WORD	$0x531e7428	// lsl w8, w1, #2
	WORD	$0xcb28c009	// sub x9, x0, w8, sxtw
	WORD	$0x3dc00123	// ldr q3, [x9]
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3dc00122	// ldr q2, [x9]
	WORD	$0x3dc00146	// ldr q6, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3dc00124	// ldr q4, [x9]
	WORD	$0x3dc00145	// ldr q5, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3dc00127	// ldr q7, [x9]
	WORD	$0x3dc00140	// ldr q0, [x10]
	WORD	$0x3ce86941	// ldr q1, [x10, x8]
	WORD	$0x6e2474d0	// uabd.16b v16, v6, v4
	WORD	$0x6e2574f3	// uabd.16b v19, v7, v5
	WORD	$0x6e336610	// umax.16b v16, v16, v19
	WORD	$0x6e227473	// uabd.16b v19, v3, v2
	WORD	$0x6e336613	// umax.16b v19, v16, v19
	WORD	$0x6e267454	// uabd.16b v20, v2, v6
	WORD	$0x6e346673	// umax.16b v19, v19, v20
	WORD	$0x6e277414	// uabd.16b v20, v0, v7
	WORD	$0x6e346673	// umax.16b v19, v19, v20
	WORD	$0x6e207434	// uabd.16b v20, v1, v0
	WORD	$0x6e346673	// umax.16b v19, v19, v20
	WORD	$0x6e257494	// uabd.16b v20, v4, v5
	WORD	$0x6e2774d5	// uabd.16b v21, v6, v7
	WORD	$0x6e340e94	// uqadd.16b v20, v20, v20
	WORD	$0x6f0f06b5	// ushr.16b v21, v21, #0x1
	WORD	$0x6e350e94	// uqadd.16b v20, v20, v21
	WORD	$0x6e333e52	// cmhs.16b v18, v18, v19
	WORD	$0x6e343e31	// cmhs.16b v17, v17, v20
	WORD	$0x4e311e51	// and.16b v17, v18, v17
	WORD	$0x6e247452	// uabd.16b v18, v2, v4
	WORD	$0x6e326612	// umax.16b v18, v16, v18
	WORD	$0x6e257413	// uabd.16b v19, v0, v5
	WORD	$0x6e336652	// umax.16b v18, v18, v19
	WORD	$0x6e247473	// uabd.16b v19, v3, v4
	WORD	$0x6e336652	// umax.16b v18, v18, v19
	WORD	$0x6e257433	// uabd.16b v19, v1, v5
	WORD	$0x6e336652	// umax.16b v18, v18, v19
	WORD	$0x4f00e453	// movi.16b v19, #0x2
	WORD	$0x6e323672	// cmhi.16b v18, v19, v18
	WORD	$0x4e321e34	// and.16b v20, v17, v18
	WORD	$0x0f0c8692	// shrn.8b v18, v20, #0x4
	WORD	$0x2ea02a52	// uaddlp.1d v18, v18
	WORD	$0x1e260249	// fmov w9, s18
	WORD	$0x3100093f	// cmn w9, #0x2
	WORD	$0x54000921	// b.ne 0xb00 <_vpx_lpf_horizontal_8_dual_neon+0x208>
	WORD	$0x4f00e470	// movi.16b v16, #0x3
	WORD	$0x0f00e471	// movi.8b v17, #0x3
	WORD	$0x2f09a452	// ushll.8h v18, v2, #0x1
	WORD	$0x2e318072	// umlal.8h v18, v3, v17
	WORD	$0x6f09a451	// ushll2.8h v17, v2, #0x1
	WORD	$0x6e308071	// umlal2.8h v17, v3, v16
	WORD	$0x2e261250	// uaddw.8h v16, v18, v6
	WORD	$0x6e261232	// uaddw2.8h v18, v17, v6
	WORD	$0x2f08a494	// ushll.8h v20, v4, #0x0
	WORD	$0x2e241210	// uaddw.8h v16, v16, v4
	WORD	$0x6f08a491	// ushll2.8h v17, v4, #0x0
	WORD	$0x6e241255	// uaddw2.8h v21, v18, v4
	WORD	$0x2f08a4b3	// ushll.8h v19, v5, #0x0
	WORD	$0x2e251210	// uaddw.8h v16, v16, v5
	WORD	$0x6f08a4b2	// ushll2.8h v18, v5, #0x0
	WORD	$0x6e2512b5	// uaddw2.8h v21, v21, v5
	WORD	$0x2e230056	// uaddl.8h v22, v2, v3
	WORD	$0x6e230057	// uaddl2.8h v23, v2, v3
	WORD	$0x2e2200b8	// uaddl.8h v24, v5, v2
	WORD	$0x6e2200a2	// uaddl2.8h v2, v5, v2
	WORD	$0x0f0d8e05	// rshrn.8b v5, v16, #0x3
	WORD	$0x4f0d8ea5	// rshrn2.16b v5, v21, #0x3
	WORD	$0x2f08a4f9	// ushll.8h v25, v7, #0x0
	WORD	$0x2e2600fa	// uaddl.8h v26, v7, v6
	WORD	$0x6e768756	// sub.8h v22, v26, v22
	WORD	$0x4e7086d6	// add.8h v22, v22, v16
	WORD	$0x6f08a4fb	// ushll2.8h v27, v7, #0x0
	WORD	$0x6e2600e7	// uaddl2.8h v7, v7, v6
	WORD	$0x6e7784f0	// sub.8h v16, v7, v23
	WORD	$0x4e758615	// add.8h v21, v16, v21
	WORD	$0x0f0d8ed0	// rshrn.8b v16, v22, #0x3
	WORD	$0x4f0d8eb0	// rshrn2.16b v16, v21, #0x3
	WORD	$0x2f08a417	// ushll.8h v23, v0, #0x0
	WORD	$0x2e2300dc	// uaddl.8h v28, v6, v3
	WORD	$0x6e7c8694	// sub.8h v20, v20, v28
	WORD	$0x2e201294	// uaddw.8h v20, v20, v0
	WORD	$0x4e768694	// add.8h v20, v20, v22
	WORD	$0x6f08a416	// ushll2.8h v22, v0, #0x0
	WORD	$0x6e2300c6	// uaddl2.8h v6, v6, v3
	WORD	$0x6e668626	// sub.8h v6, v17, v6
	WORD	$0x6e2010c0	// uaddw2.8h v0, v6, v0
	WORD	$0x4e758400	// add.8h v0, v0, v21
	WORD	$0x0f0d8e91	// rshrn.8b v17, v20, #0x3
	WORD	$0x4f0d8c11	// rshrn2.16b v17, v0, #0x3
	WORD	$0x2e230086	// uaddl.8h v6, v4, v3
	WORD	$0x6e668666	// sub.8h v6, v19, v6
	WORD	$0x2e2110c6	// uaddw.8h v6, v6, v1
	WORD	$0x4e7484c6	// add.8h v6, v6, v20
	WORD	$0x6e230083	// uaddl2.8h v3, v4, v3
	WORD	$0x6e638643	// sub.8h v3, v18, v3
	WORD	$0x6e211063	// uaddw2.8h v3, v3, v1
	WORD	$0x4e608460	// add.8h v0, v3, v0
	WORD	$0x0f0d8cd2	// rshrn.8b v18, v6, #0x3
	WORD	$0x4f0d8c12	// rshrn2.16b v18, v0, #0x3
	WORD	$0x6e788723	// sub.8h v3, v25, v24
	WORD	$0x2e211063	// uaddw.8h v3, v3, v1
	WORD	$0x4e668463	// add.8h v3, v3, v6
	WORD	$0x6e628762	// sub.8h v2, v27, v2
	WORD	$0x6e211042	// uaddw2.8h v2, v2, v1
	WORD	$0x4e608440	// add.8h v0, v2, v0
	WORD	$0x0f0d8c73	// rshrn.8b v19, v3, #0x3
	WORD	$0x4f0d8c13	// rshrn2.16b v19, v0, #0x3
	WORD	$0x6e7a86e2	// sub.8h v2, v23, v26
	WORD	$0x2e211042	// uaddw.8h v2, v2, v1
	WORD	$0x4e638442	// add.8h v2, v2, v3
	WORD	$0x6e6786c3	// sub.8h v3, v22, v7
	WORD	$0x6e211061	// uaddw2.8h v1, v3, v1
	WORD	$0x4e608421	// add.8h v1, v1, v0
	WORD	$0x0f0d8c40	// rshrn.8b v0, v2, #0x3
	WORD	$0x4f0d8c20	// rshrn2.16b v0, v1, #0x3
	WORD	$0x4ea51ca2	// mov.16b v2, v5
	WORD	$0x1400006f	// b 0xcb8 <_vpx_lpf_horizontal_8_dual_neon+0x3c0>
	WORD	$0x0d40c0f2	// ld1r.8b { v18 }, [x7]
	WORD	$0x0d40c093	// ld1r.8b { v19 }, [x4]
	WORD	$0x6e180653	// mov.d v19[1], v18[0]
	WORD	$0x6e333610	// cmhi.16b v16, v16, v19
	WORD	$0x4f04e416	// movi.16b v22, #0x80
	WORD	$0x6e361cd3	// eor.16b v19, v6, v22
	WORD	$0x6e361c92	// eor.16b v18, v4, v22
	WORD	$0x6e361cb7	// eor.16b v23, v5, v22
	WORD	$0x6e361cf8	// eor.16b v24, v7, v22
	WORD	$0x4e382e75	// sqsub.16b v21, v19, v24
	WORD	$0x4e351e15	// and.16b v21, v16, v21
	WORD	$0x4e322ef9	// sqsub.16b v25, v23, v18
	WORD	$0x4e390eb5	// sqadd.16b v21, v21, v25
	WORD	$0x4e390eb5	// sqadd.16b v21, v21, v25
	WORD	$0x4e390eb5	// sqadd.16b v21, v21, v25
	WORD	$0x4e351e31	// and.16b v17, v17, v21
	WORD	$0x4f00e495	// movi.16b v21, #0x4
	WORD	$0x4e350e35	// sqadd.16b v21, v17, v21
	WORD	$0x4f0d06b9	// sshr.16b v25, v21, #0x3
	WORD	$0x4f00e475	// movi.16b v21, #0x3
	WORD	$0x4e350e31	// sqadd.16b v17, v17, v21
	WORD	$0x4f0d0631	// sshr.16b v17, v17, #0x3
	WORD	$0x4e392ef7	// sqsub.16b v23, v23, v25
	WORD	$0x4e310e51	// sqadd.16b v17, v18, v17
	WORD	$0x6e361ef2	// eor.16b v18, v23, v22
	WORD	$0x6e361e31	// eor.16b v17, v17, v22
	WORD	$0x4f0f2737	// srshr.16b v23, v25, #0x1
	WORD	$0x4e701ef0	// bic.16b v16, v23, v16
	WORD	$0x4e302f17	// sqsub.16b v23, v24, v16
	WORD	$0x4e300e70	// sqadd.16b v16, v19, v16
	WORD	$0x6e361ef3	// eor.16b v19, v23, v22
	WORD	$0x6e361e10	// eor.16b v16, v16, v22
	WORD	$0x340009c9	// cbz w9, 0xcb8 <_vpx_lpf_horizontal_8_dual_neon+0x3c0>
	WORD	$0xd503201f	// nop (was: stp d9, d8, [sp, #-0x10]! — v8/v9 caller-saved in Go asm)
	WORD	$0x0f00e476	// movi.8b v22, #0x3
	WORD	$0x2f09a457	// ushll.8h v23, v2, #0x1
	WORD	$0x2e368077	// umlal.8h v23, v3, v22
	WORD	$0x6f09a456	// ushll2.8h v22, v2, #0x1
	WORD	$0x6e358076	// umlal2.8h v22, v3, v21
	WORD	$0x2e2612f5	// uaddw.8h v21, v23, v6
	WORD	$0x6e2612d6	// uaddw2.8h v22, v22, v6
	WORD	$0x2f08a497	// ushll.8h v23, v4, #0x0
	WORD	$0x2e2412b5	// uaddw.8h v21, v21, v4
	WORD	$0x6f08a498	// ushll2.8h v24, v4, #0x0
	WORD	$0x6e2412d6	// uaddw2.8h v22, v22, v4
	WORD	$0x2f08a4b9	// ushll.8h v25, v5, #0x0
	WORD	$0x2e2512ba	// uaddw.8h v26, v21, v5
	WORD	$0x6f08a4bb	// ushll2.8h v27, v5, #0x0
	WORD	$0x6e2512d6	// uaddw2.8h v22, v22, v5
	WORD	$0x0f0d8f55	// rshrn.8b v21, v26, #0x3
	WORD	$0x4f0d8ed5	// rshrn2.16b v21, v22, #0x3
	WORD	$0x2f08a4fc	// ushll.8h v28, v7, #0x0
	WORD	$0x2e2600fd	// uaddl.8h v29, v7, v6
	WORD	$0x2e23005e	// uaddl.8h v30, v2, v3
	WORD	$0x6e7e87be	// sub.8h v30, v29, v30
	WORD	$0x4e7a87da	// add.8h v26, v30, v26
	WORD	$0x6f08a4fe	// ushll2.8h v30, v7, #0x0
	WORD	$0x6e2600e7	// uaddl2.8h v7, v7, v6
	WORD	$0x6e23005f	// uaddl2.8h v31, v2, v3
	WORD	$0x6e7f84ff	// sub.8h v31, v7, v31
	WORD	$0x4e7687f6	// add.8h v22, v31, v22
	WORD	$0x0f0d8f5f	// rshrn.8b v31, v26, #0x3
	WORD	$0x4f0d8edf	// rshrn2.16b v31, v22, #0x3
	WORD	$0x2f08a408	// ushll.8h v8, v0, #0x0
	WORD	$0x2e2300c9	// uaddl.8h v9, v6, v3
	WORD	$0x6e6986f7	// sub.8h v23, v23, v9
	WORD	$0x2e2012f7	// uaddw.8h v23, v23, v0
	WORD	$0x4e7a86f7	// add.8h v23, v23, v26
	WORD	$0x6f08a41a	// ushll2.8h v26, v0, #0x0
	WORD	$0x6e2300c6	// uaddl2.8h v6, v6, v3
	WORD	$0x6e668706	// sub.8h v6, v24, v6
	WORD	$0x6e2010c6	// uaddw2.8h v6, v6, v0
	WORD	$0x4e7684c6	// add.8h v6, v6, v22
	WORD	$0x0f0d8ef6	// rshrn.8b v22, v23, #0x3
	WORD	$0x4f0d8cd6	// rshrn2.16b v22, v6, #0x3
	WORD	$0x2e230098	// uaddl.8h v24, v4, v3
	WORD	$0x6e788738	// sub.8h v24, v25, v24
	WORD	$0x2e211318	// uaddw.8h v24, v24, v1
	WORD	$0x4e778717	// add.8h v23, v24, v23
	WORD	$0x6e230083	// uaddl2.8h v3, v4, v3
	WORD	$0x6e638763	// sub.8h v3, v27, v3
	WORD	$0x6e211063	// uaddw2.8h v3, v3, v1
	WORD	$0x4e668463	// add.8h v3, v3, v6
	WORD	$0x0f0d8ee4	// rshrn.8b v4, v23, #0x3
	WORD	$0x4f0d8c64	// rshrn2.16b v4, v3, #0x3
	WORD	$0x2e2200a6	// uaddl.8h v6, v5, v2
	WORD	$0x6e668786	// sub.8h v6, v28, v6
	WORD	$0x2e2110c6	// uaddw.8h v6, v6, v1
	WORD	$0x4e7784c6	// add.8h v6, v6, v23
	WORD	$0x6e2200a5	// uaddl2.8h v5, v5, v2
	WORD	$0x6e6587c5	// sub.8h v5, v30, v5
	WORD	$0x6e2110a5	// uaddw2.8h v5, v5, v1
	WORD	$0x4e6384a3	// add.8h v3, v5, v3
	WORD	$0x0f0d8cc5	// rshrn.8b v5, v6, #0x3
	WORD	$0x4f0d8c65	// rshrn2.16b v5, v3, #0x3
	WORD	$0x6e7d8517	// sub.8h v23, v8, v29
	WORD	$0x2e2112f7	// uaddw.8h v23, v23, v1
	WORD	$0x4e6686e6	// add.8h v6, v23, v6
	WORD	$0x6e678747	// sub.8h v7, v26, v7
	WORD	$0x6e2110e1	// uaddw2.8h v1, v7, v1
	WORD	$0x4e638421	// add.8h v1, v1, v3
	WORD	$0x0f0d8cc3	// rshrn.8b v3, v6, #0x3
	WORD	$0x4f0d8c23	// rshrn2.16b v3, v1, #0x3
	WORD	$0x6eb41ea2	// bit.16b v2, v21, v20
	WORD	$0x6eb41ff0	// bit.16b v16, v31, v20
	WORD	$0x6eb41ed1	// bit.16b v17, v22, v20
	WORD	$0x6eb41c92	// bit.16b v18, v4, v20
	WORD	$0x6eb41cb3	// bit.16b v19, v5, v20
	WORD	$0x6eb41c60	// bit.16b v0, v3, v20
	WORD	$0xd503201f	// nop (was: ldp d9, d8, [sp], #0x10 — v8/v9 caller-saved in Go asm)
	WORD	$0x0b010429	// add w9, w1, w1, lsl #1
	WORD	$0xcb29c009	// sub x9, x0, w9, sxtw
	WORD	$0x3d800122	// str q2, [x9]
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3d800130	// str q16, [x9]
	WORD	$0x3d800151	// str q17, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3d800132	// str q18, [x9]
	WORD	$0x3d800153	// str q19, [x10]
	WORD	$0x3ca86940	// str q0, [x10, x8]
	WORD	$0xd65f03c0	// ret

TEXT ·lpfVertical8DualNEON(SB), NOSPLIT, $0-32
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	thr0+16(FP), R2
	MOVD	thr1+24(FP), R5
	ADD	$1, R2, R3
	ADD	$2, R2, R4
	ADD	$1, R5, R6
	ADD	$2, R5, R7
	WORD	$0x0d40c0a0	// ld1r.8b { v0 }, [x5]
	WORD	$0x0d40c054	// ld1r.8b { v20 }, [x2]
	WORD	$0x6e180414	// mov.d v20[1], v0[0]
	WORD	$0x0d40c0c0	// ld1r.8b { v0 }, [x6]
	WORD	$0x0d40c075	// ld1r.8b { v21 }, [x3]
	WORD	$0x6e180415	// mov.d v21[1], v0[0]
	WORD	$0xaa0003e9	// mov x9, x0
	WORD	$0xfc5fcd20	// ldr d0, [x9, #-0x4]!
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400122	// ldr d2, [x9]
	WORD	$0xfd400141	// ldr d1, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400123	// ldr d3, [x9]
	WORD	$0xfd400144	// ldr d4, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400125	// ldr d5, [x9]
	WORD	$0xfd400146	// ldr d6, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400127	// ldr d7, [x9]
	WORD	$0xfd400150	// ldr d16, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400131	// ldr d17, [x9]
	WORD	$0xfd400152	// ldr d18, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400133	// ldr d19, [x9]
	WORD	$0xfd400156	// ldr d22, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400137	// ldr d23, [x9]
	WORD	$0xfd400158	// ldr d24, [x10]
	WORD	$0xfc686959	// ldr d25, [x10, x8]
	WORD	$0x6e180600	// mov.d v0[1], v16[0]
	WORD	$0x6e180622	// mov.d v2[1], v17[0]
	WORD	$0x6e180641	// mov.d v1[1], v18[0]
	WORD	$0x6e180663	// mov.d v3[1], v19[0]
	WORD	$0x6e1806c4	// mov.d v4[1], v22[0]
	WORD	$0x6e1806e5	// mov.d v5[1], v23[0]
	WORD	$0x6e180706	// mov.d v6[1], v24[0]
	WORD	$0x6e180727	// mov.d v7[1], v25[0]
	WORD	$0x4e022810	// trn1.16b v16, v0, v2
	WORD	$0x4e026800	// trn2.16b v0, v0, v2
	WORD	$0x4e032822	// trn1.16b v2, v1, v3
	WORD	$0x4e036821	// trn2.16b v1, v1, v3
	WORD	$0x4e052883	// trn1.16b v3, v4, v5
	WORD	$0x4e056884	// trn2.16b v4, v4, v5
	WORD	$0x4e0728c5	// trn1.16b v5, v6, v7
	WORD	$0x4e0768c6	// trn2.16b v6, v6, v7
	WORD	$0x4e422a07	// trn1.8h v7, v16, v2
	WORD	$0x4e426a02	// trn2.8h v2, v16, v2
	WORD	$0x4e412811	// trn1.8h v17, v0, v1
	WORD	$0x4e416816	// trn2.8h v22, v0, v1
	WORD	$0x4e452860	// trn1.8h v0, v3, v5
	WORD	$0x4e456861	// trn2.8h v1, v3, v5
	WORD	$0x4e462893	// trn1.8h v19, v4, v6
	WORD	$0x4e466886	// trn2.8h v6, v4, v6
	WORD	$0x4e8028f0	// trn1.4s v16, v7, v0
	WORD	$0x4e8068e7	// trn2.4s v7, v7, v0
	WORD	$0x4e812852	// trn1.4s v18, v2, v1
	WORD	$0x4e816842	// trn2.4s v2, v2, v1
	WORD	$0x4e932a23	// trn1.4s v3, v17, v19
	WORD	$0x4e936a33	// trn2.4s v19, v17, v19
	WORD	$0x4e862ad1	// trn1.4s v17, v22, v6
	WORD	$0x4e866ac6	// trn2.4s v6, v22, v6
	WORD	$0x6e317656	// uabd.16b v22, v18, v17
	WORD	$0x6e277677	// uabd.16b v23, v19, v7
	WORD	$0x6e3766d6	// umax.16b v22, v22, v23
	WORD	$0x6e237617	// uabd.16b v23, v16, v3
	WORD	$0x6e3766d7	// umax.16b v23, v22, v23
	WORD	$0x6e327478	// uabd.16b v24, v3, v18
	WORD	$0x6e3866f7	// umax.16b v23, v23, v24
	WORD	$0x6e337458	// uabd.16b v24, v2, v19
	WORD	$0x6e3866f7	// umax.16b v23, v23, v24
	WORD	$0x6e2274d8	// uabd.16b v24, v6, v2
	WORD	$0x6e3866f7	// umax.16b v23, v23, v24
	WORD	$0x6e277638	// uabd.16b v24, v17, v7
	WORD	$0x6e337659	// uabd.16b v25, v18, v19
	WORD	$0x6e380f18	// uqadd.16b v24, v24, v24
	WORD	$0x6f0f0739	// ushr.16b v25, v25, #0x1
	WORD	$0x6e390f18	// uqadd.16b v24, v24, v25
	WORD	$0x6e373eb5	// cmhs.16b v21, v21, v23
	WORD	$0x6e383e94	// cmhs.16b v20, v20, v24
	WORD	$0x4e341eb5	// and.16b v21, v21, v20
	WORD	$0x6e317474	// uabd.16b v20, v3, v17
	WORD	$0x6e3466d4	// umax.16b v20, v22, v20
	WORD	$0x6e277457	// uabd.16b v23, v2, v7
	WORD	$0x6e376694	// umax.16b v20, v20, v23
	WORD	$0x6e317617	// uabd.16b v23, v16, v17
	WORD	$0x6e376694	// umax.16b v20, v20, v23
	WORD	$0x6e2774d7	// uabd.16b v23, v6, v7
	WORD	$0x6e376694	// umax.16b v20, v20, v23
	WORD	$0x4f00e457	// movi.16b v23, #0x2
	WORD	$0x6e3436f4	// cmhi.16b v20, v23, v20
	WORD	$0x4e341eb4	// and.16b v20, v21, v20
	WORD	$0x0f0c8697	// shrn.8b v23, v20, #0x4
	WORD	$0x2ea02af7	// uaddlp.1d v23, v23
	WORD	$0x1e2602e9	// fmov w9, s23
	WORD	$0x3100093f	// cmn w9, #0x2
	WORD	$0x540008e1	// b.ne 0x12d8 <_vpx_lpf_vertical_8_dual_neon+0x2bc>
	WORD	$0x4f00e474	// movi.16b v20, #0x3
	WORD	$0x0f00e475	// movi.8b v21, #0x3
	WORD	$0x2f09a476	// ushll.8h v22, v3, #0x1
	WORD	$0x6f09a477	// ushll2.8h v23, v3, #0x1
	WORD	$0x2f08a638	// ushll.8h v24, v17, #0x0
	WORD	$0x6f08a639	// ushll2.8h v25, v17, #0x0
	WORD	$0x2f08a4fa	// ushll.8h v26, v7, #0x0
	WORD	$0x2e32023b	// uaddl.8h v27, v17, v18
	WORD	$0x2e27137b	// uaddw.8h v27, v27, v7
	WORD	$0x2e35821b	// umlal.8h v27, v16, v21
	WORD	$0x4e768775	// add.8h v21, v27, v22
	WORD	$0x6f08a4f6	// ushll2.8h v22, v7, #0x0
	WORD	$0x6e32023b	// uaddl2.8h v27, v17, v18
	WORD	$0x6e27137b	// uaddw2.8h v27, v27, v7
	WORD	$0x6e34821b	// umlal2.8h v27, v16, v20
	WORD	$0x4e778777	// add.8h v23, v27, v23
	WORD	$0x0f0d8eb4	// rshrn.8b v20, v21, #0x3
	WORD	$0x4f0d8ef4	// rshrn2.16b v20, v23, #0x3
	WORD	$0x2f08a67b	// ushll.8h v27, v19, #0x0
	WORD	$0x2e32027c	// uaddl.8h v28, v19, v18
	WORD	$0x2e30007d	// uaddl.8h v29, v3, v16
	WORD	$0x6e7d879d	// sub.8h v29, v28, v29
	WORD	$0x4e7587b5	// add.8h v21, v29, v21
	WORD	$0x6f08a67d	// ushll2.8h v29, v19, #0x0
	WORD	$0x6e320273	// uaddl2.8h v19, v19, v18
	WORD	$0x6e30007e	// uaddl2.8h v30, v3, v16
	WORD	$0x6e7e867e	// sub.8h v30, v19, v30
	WORD	$0x4e7787d7	// add.8h v23, v30, v23
	WORD	$0x0f0d8ea4	// rshrn.8b v4, v21, #0x3
	WORD	$0x4f0d8ee4	// rshrn2.16b v4, v23, #0x3
	WORD	$0x2e30025e	// uaddl.8h v30, v18, v16
	WORD	$0x6e7e8718	// sub.8h v24, v24, v30
	WORD	$0x2e221318	// uaddw.8h v24, v24, v2
	WORD	$0x4e758715	// add.8h v21, v24, v21
	WORD	$0x6e300252	// uaddl2.8h v18, v18, v16
	WORD	$0x6e728732	// sub.8h v18, v25, v18
	WORD	$0x6e221252	// uaddw2.8h v18, v18, v2
	WORD	$0x4e778652	// add.8h v18, v18, v23
	WORD	$0x0f0d8ea5	// rshrn.8b v5, v21, #0x3
	WORD	$0x4f0d8e45	// rshrn2.16b v5, v18, #0x3
	WORD	$0x2e300237	// uaddl.8h v23, v17, v16
	WORD	$0x6e778757	// sub.8h v23, v26, v23
	WORD	$0x2e2612f7	// uaddw.8h v23, v23, v6
	WORD	$0x4e7586f5	// add.8h v21, v23, v21
	WORD	$0x6e300230	// uaddl2.8h v16, v17, v16
	WORD	$0x6e7086d0	// sub.8h v16, v22, v16
	WORD	$0x6e261210	// uaddw2.8h v16, v16, v6
	WORD	$0x4e728610	// add.8h v16, v16, v18
	WORD	$0x0f0d8ea0	// rshrn.8b v0, v21, #0x3
	WORD	$0x4f0d8e00	// rshrn2.16b v0, v16, #0x3
	WORD	$0x2e2300f1	// uaddl.8h v17, v7, v3
	WORD	$0x6e718771	// sub.8h v17, v27, v17
	WORD	$0x2e261231	// uaddw.8h v17, v17, v6
	WORD	$0x4e758631	// add.8h v17, v17, v21
	WORD	$0x6e2300e7	// uaddl2.8h v7, v7, v3
	WORD	$0x6e6787a7	// sub.8h v7, v29, v7
	WORD	$0x6e2610e7	// uaddw2.8h v7, v7, v6
	WORD	$0x4e7084e7	// add.8h v7, v7, v16
	WORD	$0x0f0d8e21	// rshrn.8b v1, v17, #0x3
	WORD	$0x4f0d8ce1	// rshrn2.16b v1, v7, #0x3
	WORD	$0x2e2200d0	// uaddl.8h v16, v6, v2
	WORD	$0x6e7c8610	// sub.8h v16, v16, v28
	WORD	$0x4e718610	// add.8h v16, v16, v17
	WORD	$0x6e2200c6	// uaddl2.8h v6, v6, v2
	WORD	$0x6e7384c6	// sub.8h v6, v6, v19
	WORD	$0x4e6784c6	// add.8h v6, v6, v7
	WORD	$0x0f0d8e02	// rshrn.8b v2, v16, #0x3
	WORD	$0x4f0d8cc2	// rshrn2.16b v2, v6, #0x3
	WORD	$0x4eb41e83	// mov.16b v3, v20
	WORD	$0x1400006d	// b 0x1488 <_vpx_lpf_vertical_8_dual_neon+0x46c>
	WORD	$0x0d40c0f7	// ld1r.8b { v23 }, [x7]
	WORD	$0x0d40c098	// ld1r.8b { v24 }, [x4]
	WORD	$0x6e1806f8	// mov.d v24[1], v23[0]
	WORD	$0x6e3836d6	// cmhi.16b v22, v22, v24
	WORD	$0x4f04e417	// movi.16b v23, #0x80
	WORD	$0x6e371e58	// eor.16b v24, v18, v23
	WORD	$0x6e371e39	// eor.16b v25, v17, v23
	WORD	$0x6e371cfa	// eor.16b v26, v7, v23
	WORD	$0x6e371e7b	// eor.16b v27, v19, v23
	WORD	$0x4e3b2f1c	// sqsub.16b v28, v24, v27
	WORD	$0x4e3c1edc	// and.16b v28, v22, v28
	WORD	$0x4e392f5d	// sqsub.16b v29, v26, v25
	WORD	$0x4e3d0f9c	// sqadd.16b v28, v28, v29
	WORD	$0x4e3d0f9c	// sqadd.16b v28, v28, v29
	WORD	$0x4e3d0f9c	// sqadd.16b v28, v28, v29
	WORD	$0x4e3c1ebc	// and.16b v28, v21, v28
	WORD	$0x4f00e495	// movi.16b v21, #0x4
	WORD	$0x4e350f95	// sqadd.16b v21, v28, v21
	WORD	$0x4f0d06bd	// sshr.16b v29, v21, #0x3
	WORD	$0x4f00e475	// movi.16b v21, #0x3
	WORD	$0x4e350f9c	// sqadd.16b v28, v28, v21
	WORD	$0x4f0d079c	// sshr.16b v28, v28, #0x3
	WORD	$0x4e3d2f5a	// sqsub.16b v26, v26, v29
	WORD	$0x4e3c0f39	// sqadd.16b v25, v25, v28
	WORD	$0x6e371f40	// eor.16b v0, v26, v23
	WORD	$0x6e371f25	// eor.16b v5, v25, v23
	WORD	$0x4f0f27b9	// srshr.16b v25, v29, #0x1
	WORD	$0x4e761f36	// bic.16b v22, v25, v22
	WORD	$0x4e362f79	// sqsub.16b v25, v27, v22
	WORD	$0x4e360f16	// sqadd.16b v22, v24, v22
	WORD	$0x6e371f21	// eor.16b v1, v25, v23
	WORD	$0x6e371ec4	// eor.16b v4, v22, v23
	WORD	$0x34000989	// cbz w9, 0x1488 <_vpx_lpf_vertical_8_dual_neon+0x46c>
	WORD	$0xd503201f	// nop (was: stp d9, d8, [sp, #-0x10]! — v8/v9 caller-saved in Go asm)
	WORD	$0x0f00e476	// movi.8b v22, #0x3
	WORD	$0x2f09a477	// ushll.8h v23, v3, #0x1
	WORD	$0x6f09a478	// ushll2.8h v24, v3, #0x1
	WORD	$0x2f08a639	// ushll.8h v25, v17, #0x0
	WORD	$0x6f08a63a	// ushll2.8h v26, v17, #0x0
	WORD	$0x2f08a4fb	// ushll.8h v27, v7, #0x0
	WORD	$0x2e32023c	// uaddl.8h v28, v17, v18
	WORD	$0x2e27139c	// uaddw.8h v28, v28, v7
	WORD	$0x2e36821c	// umlal.8h v28, v16, v22
	WORD	$0x4e778796	// add.8h v22, v28, v23
	WORD	$0x6f08a4f7	// ushll2.8h v23, v7, #0x0
	WORD	$0x6e32023c	// uaddl2.8h v28, v17, v18
	WORD	$0x6e27139c	// uaddw2.8h v28, v28, v7
	WORD	$0x6e35821c	// umlal2.8h v28, v16, v21
	WORD	$0x4e788798	// add.8h v24, v28, v24
	WORD	$0x0f0d8ed5	// rshrn.8b v21, v22, #0x3
	WORD	$0x4f0d8f15	// rshrn2.16b v21, v24, #0x3
	WORD	$0x2f08a67c	// ushll.8h v28, v19, #0x0
	WORD	$0x2e32027d	// uaddl.8h v29, v19, v18
	WORD	$0x2e30007e	// uaddl.8h v30, v3, v16
	WORD	$0x6e7e87be	// sub.8h v30, v29, v30
	WORD	$0x4e7687d6	// add.8h v22, v30, v22
	WORD	$0x6f08a67e	// ushll2.8h v30, v19, #0x0
	WORD	$0x6e320273	// uaddl2.8h v19, v19, v18
	WORD	$0x6e30007f	// uaddl2.8h v31, v3, v16
	WORD	$0x6e7f867f	// sub.8h v31, v19, v31
	WORD	$0x4e7887f8	// add.8h v24, v31, v24
	WORD	$0x0f0d8edf	// rshrn.8b v31, v22, #0x3
	WORD	$0x4f0d8f1f	// rshrn2.16b v31, v24, #0x3
	WORD	$0x2e300248	// uaddl.8h v8, v18, v16
	WORD	$0x6e688739	// sub.8h v25, v25, v8
	WORD	$0x2e221339	// uaddw.8h v25, v25, v2
	WORD	$0x4e768736	// add.8h v22, v25, v22
	WORD	$0x6e300252	// uaddl2.8h v18, v18, v16
	WORD	$0x6e728752	// sub.8h v18, v26, v18
	WORD	$0x6e221252	// uaddw2.8h v18, v18, v2
	WORD	$0x4e788652	// add.8h v18, v18, v24
	WORD	$0x0f0d8ed8	// rshrn.8b v24, v22, #0x3
	WORD	$0x4f0d8e58	// rshrn2.16b v24, v18, #0x3
	WORD	$0x2e300239	// uaddl.8h v25, v17, v16
	WORD	$0x6e798779	// sub.8h v25, v27, v25
	WORD	$0x2e261339	// uaddw.8h v25, v25, v6
	WORD	$0x4e768736	// add.8h v22, v25, v22
	WORD	$0x6e300230	// uaddl2.8h v16, v17, v16
	WORD	$0x6e7086f0	// sub.8h v16, v23, v16
	WORD	$0x6e261210	// uaddw2.8h v16, v16, v6
	WORD	$0x4e728610	// add.8h v16, v16, v18
	WORD	$0x0f0d8ed1	// rshrn.8b v17, v22, #0x3
	WORD	$0x4f0d8e11	// rshrn2.16b v17, v16, #0x3
	WORD	$0x2e2300f2	// uaddl.8h v18, v7, v3
	WORD	$0x6e728792	// sub.8h v18, v28, v18
	WORD	$0x2e261252	// uaddw.8h v18, v18, v6
	WORD	$0x4e768652	// add.8h v18, v18, v22
	WORD	$0x6e2300e7	// uaddl2.8h v7, v7, v3
	WORD	$0x6e6787c7	// sub.8h v7, v30, v7
	WORD	$0x6e2610e7	// uaddw2.8h v7, v7, v6
	WORD	$0x4e7084e7	// add.8h v7, v7, v16
	WORD	$0x0f0d8e50	// rshrn.8b v16, v18, #0x3
	WORD	$0x4f0d8cf0	// rshrn2.16b v16, v7, #0x3
	WORD	$0x2e2200d6	// uaddl.8h v22, v6, v2
	WORD	$0x6e7d86d6	// sub.8h v22, v22, v29
	WORD	$0x4e7286d2	// add.8h v18, v22, v18
	WORD	$0x6e2200c6	// uaddl2.8h v6, v6, v2
	WORD	$0x6e7384c6	// sub.8h v6, v6, v19
	WORD	$0x4e6784c6	// add.8h v6, v6, v7
	WORD	$0x0f0d8e47	// rshrn.8b v7, v18, #0x3
	WORD	$0x4f0d8cc7	// rshrn2.16b v7, v6, #0x3
	WORD	$0x6eb41ea3	// bit.16b v3, v21, v20
	WORD	$0x6eb41fe4	// bit.16b v4, v31, v20
	WORD	$0x6eb41f05	// bit.16b v5, v24, v20
	WORD	$0x6eb41e20	// bit.16b v0, v17, v20
	WORD	$0x6eb41e01	// bit.16b v1, v16, v20
	WORD	$0x6eb41ce2	// bit.16b v2, v7, v20
	WORD	$0xd503201f	// nop (was: ldp d9, d8, [sp], #0x10 — v8/v9 caller-saved in Go asm)
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d002123	// st3.b { v3, v4, v5 }[0], [x9]
	WORD	$0xd37df109	// lsl x9, x8, #3
	WORD	$0x8b08000a	// add x10, x0, x8
	WORD	$0x0d892000	// st3.b { v0, v1, v2 }[0], [x0], x9
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d002523	// st3.b { v3, v4, v5 }[1], [x9]
	WORD	$0x0d882540	// st3.b { v0, v1, v2 }[1], [x10], x8
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d002923	// st3.b { v3, v4, v5 }[2], [x9]
	WORD	$0x0d882940	// st3.b { v0, v1, v2 }[2], [x10], x8
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d002d23	// st3.b { v3, v4, v5 }[3], [x9]
	WORD	$0x0d882d40	// st3.b { v0, v1, v2 }[3], [x10], x8
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d003123	// st3.b { v3, v4, v5 }[4], [x9]
	WORD	$0x0d883140	// st3.b { v0, v1, v2 }[4], [x10], x8
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d003523	// st3.b { v3, v4, v5 }[5], [x9]
	WORD	$0x0d883540	// st3.b { v0, v1, v2 }[5], [x10], x8
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d003923	// st3.b { v3, v4, v5 }[6], [x9]
	WORD	$0x0d883940	// st3.b { v0, v1, v2 }[6], [x10], x8
	WORD	$0xd1000d49	// sub x9, x10, #0x3
	WORD	$0x0d003d23	// st3.b { v3, v4, v5 }[7], [x9]
	WORD	$0x0d003d40	// st3.b { v0, v1, v2 }[7], [x10]
	WORD	$0x6e034070	// ext.16b v16, v3, v3, #0x8
	WORD	$0x6e044091	// ext.16b v17, v4, v4, #0x8
	WORD	$0x6e0540b2	// ext.16b v18, v5, v5, #0x8
	WORD	$0x6e004003	// ext.16b v3, v0, v0, #0x8
	WORD	$0x6e014024	// ext.16b v4, v1, v1, #0x8
	WORD	$0x6e024045	// ext.16b v5, v2, v2, #0x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d002130	// st3.b { v16, v17, v18 }[0], [x9]
	WORD	$0x0d882003	// st3.b { v3, v4, v5 }[0], [x0], x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d002530	// st3.b { v16, v17, v18 }[1], [x9]
	WORD	$0x0d882403	// st3.b { v3, v4, v5 }[1], [x0], x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d002930	// st3.b { v16, v17, v18 }[2], [x9]
	WORD	$0x0d882803	// st3.b { v3, v4, v5 }[2], [x0], x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d002d30	// st3.b { v16, v17, v18 }[3], [x9]
	WORD	$0x0d882c03	// st3.b { v3, v4, v5 }[3], [x0], x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d003130	// st3.b { v16, v17, v18 }[4], [x9]
	WORD	$0x0d883003	// st3.b { v3, v4, v5 }[4], [x0], x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d003530	// st3.b { v16, v17, v18 }[5], [x9]
	WORD	$0x0d883403	// st3.b { v3, v4, v5 }[5], [x0], x8
	WORD	$0xd1000c09	// sub x9, x0, #0x3
	WORD	$0x0d003930	// st3.b { v16, v17, v18 }[6], [x9]
	WORD	$0x0d883803	// st3.b { v3, v4, v5 }[6], [x0], x8
	WORD	$0xd1000c08	// sub x8, x0, #0x3
	WORD	$0x0d003d10	// st3.b { v16, v17, v18 }[7], [x8]
	WORD	$0x0d003c03	// st3.b { v3, v4, v5 }[7], [x0]
	WORD	$0xd65f03c0	// ret

