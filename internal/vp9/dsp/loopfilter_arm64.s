//go:build arm64 && !purego

// Generated from libvpx v1.16.0 vpx_dsp/arm/loopfilter_neon.c with
// clang -target arm64-apple-macos13 -O3. The C ABI threshold pointer
// loads are replaced with byte-argument NEON dup instructions.

#include "textflag.h"

// lpfHorizontal8NEON ABI ($0-19):
//   s+0(FP)       *byte, points at q0
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·lpfHorizontal8NEON(SB), NOSPLIT, $0-19
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2
	MOVBU	limit+17(FP), R3
	MOVBU	thresh+18(FP), R4
	WORD	$0x531e7428	// lsl w8, w1, #2
	WORD	$0x0e010c51	// ld1r.8b { v17 }, [x2] -> byte dup
	WORD	$0x0e010c72	// ld1r.8b { v18 }, [x3] -> byte dup
	WORD	$0xcb28c009	// sub x9, x0, w8, sxtw
	WORD	$0xfd400125	// ldr d5, [x9]
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400121	// ldr d1, [x9]
	WORD	$0xfd400142	// ldr d2, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400126	// ldr d6, [x9]
	WORD	$0xfd400147	// ldr d7, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400124	// ldr d4, [x9]
	WORD	$0xfd400140	// ldr d0, [x10]
	WORD	$0xfc686943	// ldr d3, [x10, x8]
	WORD	$0x2e267450	// uabd.8b v16, v2, v6
	WORD	$0x2e277493	// uabd.8b v19, v4, v7
	WORD	$0x2e336610	// umax.8b v16, v16, v19
	WORD	$0x2e2174b3	// uabd.8b v19, v5, v1
	WORD	$0x2e336613	// umax.8b v19, v16, v19
	WORD	$0x2e227434	// uabd.8b v20, v1, v2
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e247414	// uabd.8b v20, v0, v4
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e207474	// uabd.8b v20, v3, v0
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e2774d4	// uabd.8b v20, v6, v7
	WORD	$0x2e247455	// uabd.8b v21, v2, v4
	WORD	$0x2e340e94	// uqadd.8b v20, v20, v20
	WORD	$0x2f0f06b5	// ushr.8b v21, v21, #0x1
	WORD	$0x2e350e94	// uqadd.8b v20, v20, v21
	WORD	$0x2e333e52	// cmhs.8b v18, v18, v19
	WORD	$0x2e343e31	// cmhs.8b v17, v17, v20
	WORD	$0x0e311e51	// and.8b v17, v18, v17
	WORD	$0x2e267432	// uabd.8b v18, v1, v6
	WORD	$0x2e326612	// umax.8b v18, v16, v18
	WORD	$0x2e277413	// uabd.8b v19, v0, v7
	WORD	$0x2e336652	// umax.8b v18, v18, v19
	WORD	$0x2e2674b3	// uabd.8b v19, v5, v6
	WORD	$0x2e336652	// umax.8b v18, v18, v19
	WORD	$0x2e277473	// uabd.8b v19, v3, v7
	WORD	$0x2e336652	// umax.8b v18, v18, v19
	WORD	$0x0f00e453	// movi.8b v19, #0x2
	WORD	$0x2e323672	// cmhi.8b v18, v19, v18
	WORD	$0x0e321e34	// and.8b v20, v17, v18
	WORD	$0x2ea02a92	// uaddlp.1d v18, v20
	WORD	$0x1e260249	// fmov w9, s18
	WORD	$0x3100093f	// cmn w9, #0x2
	WORD	$0x54000501	// b.ne 0x7a0
	WORD	$0x0f00e470	// movi.8b v16, #0x3
	WORD	$0x2f09a431	// ushll.8h v17, v1, #0x1
	WORD	$0x2e3080b1	// umlal.8h v17, v5, v16
	WORD	$0x2f08a450	// ushll.8h v16, v2, #0x0
	WORD	$0x2e221231	// uaddw.8h v17, v17, v2
	WORD	$0x2f08a4d3	// ushll.8h v19, v6, #0x0
	WORD	$0x2e261231	// uaddw.8h v17, v17, v6
	WORD	$0x2f08a4f4	// ushll.8h v20, v7, #0x0
	WORD	$0x2e271231	// uaddw.8h v17, v17, v7
	WORD	$0x2e250032	// uaddl.8h v18, v1, v5
	WORD	$0x2e2100e1	// uaddl.8h v1, v7, v1
	WORD	$0x0f0d8e27	// rshrn.8b v7, v17, #0x3
	WORD	$0x2f08a495	// ushll.8h v21, v4, #0x0
	WORD	$0x6e728610	// sub.8h v16, v16, v18
	WORD	$0x2e241210	// uaddw.8h v16, v16, v4
	WORD	$0x4e718610	// add.8h v16, v16, v17
	WORD	$0x0f0d8e12	// rshrn.8b v18, v16, #0x3
	WORD	$0x2f08a416	// ushll.8h v22, v0, #0x0
	WORD	$0x2e250051	// uaddl.8h v17, v2, v5
	WORD	$0x6e718671	// sub.8h v17, v19, v17
	WORD	$0x2e201220	// uaddw.8h v0, v17, v0
	WORD	$0x4e708400	// add.8h v0, v0, v16
	WORD	$0x0f0d8c11	// rshrn.8b v17, v0, #0x3
	WORD	$0x2e2500c5	// uaddl.8h v5, v6, v5
	WORD	$0x6e658685	// sub.8h v5, v20, v5
	WORD	$0x2e2310a5	// uaddw.8h v5, v5, v3
	WORD	$0x4e6084a0	// add.8h v0, v5, v0
	WORD	$0x0f0d8c10	// rshrn.8b v16, v0, #0x3
	WORD	$0x6e6186a1	// sub.8h v1, v21, v1
	WORD	$0x2e231021	// uaddw.8h v1, v1, v3
	WORD	$0x4e608420	// add.8h v0, v1, v0
	WORD	$0x0f0d8c13	// rshrn.8b v19, v0, #0x3
	WORD	$0x2e220081	// uaddl.8h v1, v4, v2
	WORD	$0x6e6186c1	// sub.8h v1, v22, v1
	WORD	$0x2e231021	// uaddw.8h v1, v1, v3
	WORD	$0x4e608420	// add.8h v0, v1, v0
	WORD	$0x0f0d8c00	// rshrn.8b v0, v0, #0x3
	WORD	$0x4ea71ce1	// mov.16b v1, v7
	WORD	$0x1400004a	// b 0x8c4
	WORD	$0x0e010c92	// ld1r.8b { v18 }, [x4] -> byte dup
	WORD	$0x2e323612	// cmhi.8b v18, v16, v18
	WORD	$0x0f04e416	// movi.8b v22, #0x80
	WORD	$0x2e361c53	// eor.8b v19, v2, v22
	WORD	$0x2e361cd0	// eor.8b v16, v6, v22
	WORD	$0x2e361cf7	// eor.8b v23, v7, v22
	WORD	$0x2e361c98	// eor.8b v24, v4, v22
	WORD	$0x0e382e75	// sqsub.8b v21, v19, v24
	WORD	$0x0e351e55	// and.8b v21, v18, v21
	WORD	$0x0e302ef9	// sqsub.8b v25, v23, v16
	WORD	$0x0e390eb5	// sqadd.8b v21, v21, v25
	WORD	$0x0e390eb5	// sqadd.8b v21, v21, v25
	WORD	$0x0e390eb5	// sqadd.8b v21, v21, v25
	WORD	$0x0e351e31	// and.8b v17, v17, v21
	WORD	$0x0f00e495	// movi.8b v21, #0x4
	WORD	$0x0e350e35	// sqadd.8b v21, v17, v21
	WORD	$0x0f0d06b9	// sshr.8b v25, v21, #0x3
	WORD	$0x0f00e475	// movi.8b v21, #0x3
	WORD	$0x0e350e31	// sqadd.8b v17, v17, v21
	WORD	$0x0f0d0631	// sshr.8b v17, v17, #0x3
	WORD	$0x0e392ef7	// sqsub.8b v23, v23, v25
	WORD	$0x0e310e11	// sqadd.8b v17, v16, v17
	WORD	$0x2e361ef0	// eor.8b v16, v23, v22
	WORD	$0x2e361e31	// eor.8b v17, v17, v22
	WORD	$0x0f0f2737	// srshr.8b v23, v25, #0x1
	WORD	$0x0e721ef2	// bic.8b v18, v23, v18
	WORD	$0x0e322f17	// sqsub.8b v23, v24, v18
	WORD	$0x0e320e72	// sqadd.8b v18, v19, v18
	WORD	$0x2e361ef3	// eor.8b v19, v23, v22
	WORD	$0x2e361e52	// eor.8b v18, v18, v22
	WORD	$0x34000569	// cbz w9, 0x8c4
	WORD	$0x2f09a436	// ushll.8h v22, v1, #0x1
	WORD	$0x2e3580b6	// umlal.8h v22, v5, v21
	WORD	$0x2f08a455	// ushll.8h v21, v2, #0x0
	WORD	$0x2e2212d6	// uaddw.8h v22, v22, v2
	WORD	$0x2f08a4d7	// ushll.8h v23, v6, #0x0
	WORD	$0x2e2612d6	// uaddw.8h v22, v22, v6
	WORD	$0x2f08a4f8	// ushll.8h v24, v7, #0x0
	WORD	$0x2e2712d6	// uaddw.8h v22, v22, v7
	WORD	$0x0f0d8ed9	// rshrn.8b v25, v22, #0x3
	WORD	$0x2f08a49a	// ushll.8h v26, v4, #0x0
	WORD	$0x2e25003b	// uaddl.8h v27, v1, v5
	WORD	$0x6e7b86b5	// sub.8h v21, v21, v27
	WORD	$0x2e2412b5	// uaddw.8h v21, v21, v4
	WORD	$0x4e7686b5	// add.8h v21, v21, v22
	WORD	$0x0f0d8eb6	// rshrn.8b v22, v21, #0x3
	WORD	$0x2f08a41b	// ushll.8h v27, v0, #0x0
	WORD	$0x2e25005c	// uaddl.8h v28, v2, v5
	WORD	$0x6e7c86f7	// sub.8h v23, v23, v28
	WORD	$0x2e2012f7	// uaddw.8h v23, v23, v0
	WORD	$0x4e7586f5	// add.8h v21, v23, v21
	WORD	$0x0f0d8eb7	// rshrn.8b v23, v21, #0x3
	WORD	$0x2e2500c5	// uaddl.8h v5, v6, v5
	WORD	$0x6e658705	// sub.8h v5, v24, v5
	WORD	$0x2e2310a5	// uaddw.8h v5, v5, v3
	WORD	$0x4e7584a5	// add.8h v5, v5, v21
	WORD	$0x0f0d8ca6	// rshrn.8b v6, v5, #0x3
	WORD	$0x2e2100e7	// uaddl.8h v7, v7, v1
	WORD	$0x6e678747	// sub.8h v7, v26, v7
	WORD	$0x2e2310e7	// uaddw.8h v7, v7, v3
	WORD	$0x4e6584e5	// add.8h v5, v7, v5
	WORD	$0x0f0d8ca7	// rshrn.8b v7, v5, #0x3
	WORD	$0x2e220082	// uaddl.8h v2, v4, v2
	WORD	$0x6e628762	// sub.8h v2, v27, v2
	WORD	$0x2e231042	// uaddw.8h v2, v2, v3
	WORD	$0x4e658442	// add.8h v2, v2, v5
	WORD	$0x0f0d8c42	// rshrn.8b v2, v2, #0x3
	WORD	$0x2eb41f21	// bit.8b v1, v25, v20
	WORD	$0x2eb41ed2	// bit.8b v18, v22, v20
	WORD	$0x2eb41ef1	// bit.8b v17, v23, v20
	WORD	$0x2eb41cd0	// bit.8b v16, v6, v20
	WORD	$0x2eb41cf3	// bit.8b v19, v7, v20
	WORD	$0x2eb41c40	// bit.8b v0, v2, v20
	WORD	$0x0b010429	// add w9, w1, w1, lsl #1
	WORD	$0xcb29c009	// sub x9, x0, w9, sxtw
	WORD	$0xfd000121	// str d1, [x9]
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd000132	// str d18, [x9]
	WORD	$0xfd000151	// str d17, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd000130	// str d16, [x9]
	WORD	$0xfd000153	// str d19, [x10]
	WORD	$0xfc286940	// str d0, [x10, x8]
	WORD	$0xd65f03c0	// ret

// lpfVertical8NEON ABI matches lpfHorizontal8NEON.
TEXT ·lpfVertical8NEON(SB), NOSPLIT, $0-19
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVBU	blimit+16(FP), R2
	MOVBU	limit+17(FP), R3
	MOVBU	thresh+18(FP), R4
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x0e010c50	// ld1r.8b { v16 }, [x2] -> byte dup
	WORD	$0x0e010c71	// ld1r.8b { v17 }, [x3] -> byte dup
	WORD	$0xfc5fcc00	// ldr d0, [x0, #-0x4]!
	WORD	$0x8b08000a	// add x10, x0, x8
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0xfd400141	// ldr d1, [x10]
	WORD	$0xfd400122	// ldr d2, [x9]
	WORD	$0x8b08012b	// add x11, x9, x8
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0xfd400163	// ldr d3, [x11]
	WORD	$0xfd400144	// ldr d4, [x10]
	WORD	$0x8b08014c	// add x12, x10, x8
	WORD	$0x8b08018b	// add x11, x12, x8
	WORD	$0xfd400185	// ldr d5, [x12]
	WORD	$0xfd400166	// ldr d6, [x11]
	WORD	$0xfc686967	// ldr d7, [x11, x8]
	WORD	$0x4e013800	// zip1.16b v0, v0, v1
	WORD	$0x4e033841	// zip1.16b v1, v2, v3
	WORD	$0x4e053882	// zip1.16b v2, v4, v5
	WORD	$0x4e0738c3	// zip1.16b v3, v6, v7
	WORD	$0x4e413804	// zip1.8h v4, v0, v1
	WORD	$0x4e417801	// zip2.8h v1, v0, v1
	WORD	$0x4e433845	// zip1.8h v5, v2, v3
	WORD	$0x4e437842	// zip2.8h v2, v2, v3
	WORD	$0x4e853880	// zip1.4s v0, v4, v5
	WORD	$0x4e857884	// zip2.4s v4, v4, v5
	WORD	$0x4e823827	// zip1.4s v7, v1, v2
	WORD	$0x4e827822	// zip2.4s v2, v1, v2
	WORD	$0x6e004003	// ext.16b v3, v0, v0, #0x8
	WORD	$0x6e044086	// ext.16b v6, v4, v4, #0x8
	WORD	$0x6e0740e5	// ext.16b v5, v7, v7, #0x8
	WORD	$0x6e024041	// ext.16b v1, v2, v2, #0x8
	WORD	$0x2e267492	// uabd.8b v18, v4, v6
	WORD	$0x2e2774b3	// uabd.8b v19, v5, v7
	WORD	$0x2e336652	// umax.8b v18, v18, v19
	WORD	$0x2e237413	// uabd.8b v19, v0, v3
	WORD	$0x2e336653	// umax.8b v19, v18, v19
	WORD	$0x2e247474	// uabd.8b v20, v3, v4
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e257454	// uabd.8b v20, v2, v5
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e227434	// uabd.8b v20, v1, v2
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e2774d4	// uabd.8b v20, v6, v7
	WORD	$0x2e257495	// uabd.8b v21, v4, v5
	WORD	$0x2e340e94	// uqadd.8b v20, v20, v20
	WORD	$0x2f0f06b5	// ushr.8b v21, v21, #0x1
	WORD	$0x2e350e94	// uqadd.8b v20, v20, v21
	WORD	$0x2e333e31	// cmhs.8b v17, v17, v19
	WORD	$0x2e343e10	// cmhs.8b v16, v16, v20
	WORD	$0x0e301e30	// and.8b v16, v17, v16
	WORD	$0x2e267471	// uabd.8b v17, v3, v6
	WORD	$0x2e316651	// umax.8b v17, v18, v17
	WORD	$0x2e277453	// uabd.8b v19, v2, v7
	WORD	$0x2e336631	// umax.8b v17, v17, v19
	WORD	$0x2e267413	// uabd.8b v19, v0, v6
	WORD	$0x2e336631	// umax.8b v17, v17, v19
	WORD	$0x2e277433	// uabd.8b v19, v1, v7
	WORD	$0x2e336631	// umax.8b v17, v17, v19
	WORD	$0x0f00e453	// movi.8b v19, #0x2
	WORD	$0x2e313671	// cmhi.8b v17, v19, v17
	WORD	$0x0e311e14	// and.8b v20, v16, v17
	WORD	$0x2ea02a91	// uaddlp.1d v17, v20
	WORD	$0x1e26022c	// fmov w12, s17
	WORD	$0x3100099f	// cmn w12, #0x2
	WORD	$0x54000501	// b.ne 0xf58
	WORD	$0x0f00e470	// movi.8b v16, #0x3
	WORD	$0x2f09a471	// ushll.8h v17, v3, #0x1
	WORD	$0x2f08a492	// ushll.8h v18, v4, #0x0
	WORD	$0x2f08a4d3	// ushll.8h v19, v6, #0x0
	WORD	$0x2f08a4f4	// ushll.8h v20, v7, #0x0
	WORD	$0x2e2400d5	// uaddl.8h v21, v6, v4
	WORD	$0x2e2712b5	// uaddw.8h v21, v21, v7
	WORD	$0x2e308015	// umlal.8h v21, v0, v16
	WORD	$0x4e7186b0	// add.8h v16, v21, v17
	WORD	$0x2e200071	// uaddl.8h v17, v3, v0
	WORD	$0x2e2300e3	// uaddl.8h v3, v7, v3
	WORD	$0x0f0d8e07	// rshrn.8b v7, v16, #0x3
	WORD	$0x2f08a4b5	// ushll.8h v21, v5, #0x0
	WORD	$0x6e718651	// sub.8h v17, v18, v17
	WORD	$0x2e251231	// uaddw.8h v17, v17, v5
	WORD	$0x4e708630	// add.8h v16, v17, v16
	WORD	$0x0f0d8e11	// rshrn.8b v17, v16, #0x3
	WORD	$0x2f08a456	// ushll.8h v22, v2, #0x0
	WORD	$0x2e200092	// uaddl.8h v18, v4, v0
	WORD	$0x6e728672	// sub.8h v18, v19, v18
	WORD	$0x2e221242	// uaddw.8h v2, v18, v2
	WORD	$0x4e708442	// add.8h v2, v2, v16
	WORD	$0x0f0d8c50	// rshrn.8b v16, v2, #0x3
	WORD	$0x2e2000c6	// uaddl.8h v6, v6, v0
	WORD	$0x6e668686	// sub.8h v6, v20, v6
	WORD	$0x2e2110c6	// uaddw.8h v6, v6, v1
	WORD	$0x4e6284c2	// add.8h v2, v6, v2
	WORD	$0x0f0d8c52	// rshrn.8b v18, v2, #0x3
	WORD	$0x6e6386a3	// sub.8h v3, v21, v3
	WORD	$0x2e211063	// uaddw.8h v3, v3, v1
	WORD	$0x4e628462	// add.8h v2, v3, v2
	WORD	$0x0f0d8c53	// rshrn.8b v19, v2, #0x3
	WORD	$0x2e2400a3	// uaddl.8h v3, v5, v4
	WORD	$0x6e6386c3	// sub.8h v3, v22, v3
	WORD	$0x2e211063	// uaddw.8h v3, v3, v1
	WORD	$0x4e628462	// add.8h v2, v3, v2
	WORD	$0x0f0d8c42	// rshrn.8b v2, v2, #0x3
	WORD	$0x4ea71ce3	// mov.16b v3, v7
	WORD	$0x1400004a	// b 0x107c
	WORD	$0x0e010c91	// ld1r.8b { v17 }, [x4] -> byte dup
	WORD	$0x2e313651	// cmhi.8b v17, v18, v17
	WORD	$0x0f04e416	// movi.8b v22, #0x80
	WORD	$0x2e361c93	// eor.8b v19, v4, v22
	WORD	$0x2e361cd2	// eor.8b v18, v6, v22
	WORD	$0x2e361cf7	// eor.8b v23, v7, v22
	WORD	$0x2e361cb8	// eor.8b v24, v5, v22
	WORD	$0x0e382e75	// sqsub.8b v21, v19, v24
	WORD	$0x0e351e35	// and.8b v21, v17, v21
	WORD	$0x0e322ef9	// sqsub.8b v25, v23, v18
	WORD	$0x0e390eb5	// sqadd.8b v21, v21, v25
	WORD	$0x0e390eb5	// sqadd.8b v21, v21, v25
	WORD	$0x0e390eb5	// sqadd.8b v21, v21, v25
	WORD	$0x0e351e10	// and.8b v16, v16, v21
	WORD	$0x0f00e495	// movi.8b v21, #0x4
	WORD	$0x0e350e15	// sqadd.8b v21, v16, v21
	WORD	$0x0f0d06b9	// sshr.8b v25, v21, #0x3
	WORD	$0x0f00e475	// movi.8b v21, #0x3
	WORD	$0x0e350e10	// sqadd.8b v16, v16, v21
	WORD	$0x0f0d0610	// sshr.8b v16, v16, #0x3
	WORD	$0x0e392ef7	// sqsub.8b v23, v23, v25
	WORD	$0x0e300e50	// sqadd.8b v16, v18, v16
	WORD	$0x2e361ef2	// eor.8b v18, v23, v22
	WORD	$0x2e361e10	// eor.8b v16, v16, v22
	WORD	$0x0f0f2737	// srshr.8b v23, v25, #0x1
	WORD	$0x0e711ef1	// bic.8b v17, v23, v17
	WORD	$0x0e312f17	// sqsub.8b v23, v24, v17
	WORD	$0x0e310e71	// sqadd.8b v17, v19, v17
	WORD	$0x2e361ef3	// eor.8b v19, v23, v22
	WORD	$0x2e361e31	// eor.8b v17, v17, v22
	WORD	$0x3400056c	// cbz w12, 0x107c
	WORD	$0x2f09a476	// ushll.8h v22, v3, #0x1
	WORD	$0x2f08a497	// ushll.8h v23, v4, #0x0
	WORD	$0x2f08a4d8	// ushll.8h v24, v6, #0x0
	WORD	$0x2f08a4f9	// ushll.8h v25, v7, #0x0
	WORD	$0x2e2400da	// uaddl.8h v26, v6, v4
	WORD	$0x2e27135a	// uaddw.8h v26, v26, v7
	WORD	$0x2e35801a	// umlal.8h v26, v0, v21
	WORD	$0x4e768755	// add.8h v21, v26, v22
	WORD	$0x0f0d8eb6	// rshrn.8b v22, v21, #0x3
	WORD	$0x2f08a4ba	// ushll.8h v26, v5, #0x0
	WORD	$0x2e20007b	// uaddl.8h v27, v3, v0
	WORD	$0x6e7b86f7	// sub.8h v23, v23, v27
	WORD	$0x2e2512f7	// uaddw.8h v23, v23, v5
	WORD	$0x4e7586f5	// add.8h v21, v23, v21
	WORD	$0x0f0d8eb7	// rshrn.8b v23, v21, #0x3
	WORD	$0x2f08a45b	// ushll.8h v27, v2, #0x0
	WORD	$0x2e20009c	// uaddl.8h v28, v4, v0
	WORD	$0x6e7c8718	// sub.8h v24, v24, v28
	WORD	$0x2e221318	// uaddw.8h v24, v24, v2
	WORD	$0x4e758715	// add.8h v21, v24, v21
	WORD	$0x0f0d8eb8	// rshrn.8b v24, v21, #0x3
	WORD	$0x2e2000c6	// uaddl.8h v6, v6, v0
	WORD	$0x6e668726	// sub.8h v6, v25, v6
	WORD	$0x2e2110c6	// uaddw.8h v6, v6, v1
	WORD	$0x4e7584c6	// add.8h v6, v6, v21
	WORD	$0x0f0d8cd5	// rshrn.8b v21, v6, #0x3
	WORD	$0x2e2300e7	// uaddl.8h v7, v7, v3
	WORD	$0x6e678747	// sub.8h v7, v26, v7
	WORD	$0x2e2110e7	// uaddw.8h v7, v7, v1
	WORD	$0x4e6684e6	// add.8h v6, v7, v6
	WORD	$0x0f0d8cc7	// rshrn.8b v7, v6, #0x3
	WORD	$0x2e2400a4	// uaddl.8h v4, v5, v4
	WORD	$0x6e648764	// sub.8h v4, v27, v4
	WORD	$0x2e211084	// uaddw.8h v4, v4, v1
	WORD	$0x4e668484	// add.8h v4, v4, v6
	WORD	$0x0f0d8c84	// rshrn.8b v4, v4, #0x3
	WORD	$0x2eb41ec3	// bit.8b v3, v22, v20
	WORD	$0x2eb41ef1	// bit.8b v17, v23, v20
	WORD	$0x2eb41f10	// bit.8b v16, v24, v20
	WORD	$0x2eb41eb2	// bit.8b v18, v21, v20
	WORD	$0x2eb41cf3	// bit.8b v19, v7, v20
	WORD	$0x2eb41c82	// bit.8b v2, v4, v20
	WORD	$0x4e033800	// zip1.16b v0, v0, v3
	WORD	$0x4e103a23	// zip1.16b v3, v17, v16
	WORD	$0x4e133a44	// zip1.16b v4, v18, v19
	WORD	$0x4e013841	// zip1.16b v1, v2, v1
	WORD	$0x4e433802	// zip1.8h v2, v0, v3
	WORD	$0x4e437800	// zip2.8h v0, v0, v3
	WORD	$0x4e413883	// zip1.8h v3, v4, v1
	WORD	$0x4e417881	// zip2.8h v1, v4, v1
	WORD	$0x4e833844	// zip1.4s v4, v2, v3
	WORD	$0x4e837842	// zip2.4s v2, v2, v3
	WORD	$0x4e813803	// zip1.4s v3, v0, v1
	WORD	$0x6e044085	// ext.16b v5, v4, v4, #0x8
	WORD	$0xfd000004	// str d4, [x0]
	WORD	$0xfc286805	// str d5, [x0, x8]
	WORD	$0x4e817800	// zip2.4s v0, v0, v1
	WORD	$0x6e024041	// ext.16b v1, v2, v2, #0x8
	WORD	$0x6e034064	// ext.16b v4, v3, v3, #0x8
	WORD	$0xfd000122	// str d2, [x9]
	WORD	$0xfc286921	// str d1, [x9, x8]
	WORD	$0xfd000143	// str d3, [x10]
	WORD	$0xfc286944	// str d4, [x10, x8]
	WORD	$0x6e004001	// ext.16b v1, v0, v0, #0x8
	WORD	$0xfd000160	// str d0, [x11]
	WORD	$0xfc286961	// str d1, [x11, x8]
	WORD	$0xd65f03c0	// ret

