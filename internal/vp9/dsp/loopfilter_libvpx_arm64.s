//go:build arm64 && !purego

// VP9 4-tap and 16-tap (wide flat2) loopfilter kernels. Generated from
// libvpx v1.16.0 vpx_dsp/arm/loopfilter_neon.c with
//   clang -O3 -fomit-frame-pointer -mllvm -inline-threshold=100000
//         -ffixed-x18 -ffixed-x28 -target arm64-apple-macos13
// transcribed instruction-for-instruction so internal branch offsets
// are preserved. Edits relative to the compiler output:
//   - the C ABI prologue is replaced by Go FP argument loads into the
//     same registers;
//   - for the 16-tap kernels, clang's sub/add sp pair is replaced by
//     NOPs and every [sp, #off] spill is re-encoded at off+16 so the
//     spill slots live inside the Go-declared frame (below the saved
//     LR at [sp]); Go allocates and releases the frame itself, so SP
//     never moves inside the body and tracebacks stay valid;
//   - every `ret` is re-encoded as a branch to the trailing Go RET.
// The d8-d15 saves inside the 16-tap kernels write into the Go frame
// and are harmless (Go asm treats all vector registers as
// caller-saved).
//
// All kernels take the C ABI argument shape: s points at the edge
// pixel, pitch is the row stride, and the threshold pointers each
// reference a single byte the kernel dup-loads.

#include "textflag.h"

TEXT ·lpfHorizontal4NEON(SB), NOSPLIT, $0-40
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	blimit+16(FP), R2
	MOVD	limit+24(FP), R3
	MOVD	thresh+32(FP), R4
	WORD	$0x531e7428	// lsl w8, w1, #2
	WORD	$0x0d40c040	// ld1r.8b { v0 }, [x2]
	WORD	$0x0d40c061	// ld1r.8b { v1 }, [x3]
	WORD	$0x0d40c082	// ld1r.8b { v2 }, [x4]
	WORD	$0xcb28c009	// sub x9, x0, w8, sxtw
	WORD	$0xfd400123	// ldr d3, [x9]
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400124	// ldr d4, [x9]
	WORD	$0xfd400145	// ldr d5, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400126	// ldr d6, [x9]
	WORD	$0xfd400147	// ldr d7, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400130	// ldr d16, [x9]
	WORD	$0xfd400151	// ldr d17, [x10]
	WORD	$0xfc686952	// ldr d18, [x10, x8]
	WORD	$0x2e2674b3	// uabd.8b v19, v5, v6
	WORD	$0x2e277614	// uabd.8b v20, v16, v7
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e223662	// cmhi.8b v2, v19, v2
	WORD	$0x2e247463	// uabd.8b v3, v3, v4
	WORD	$0x2e236663	// umax.8b v3, v19, v3
	WORD	$0x2e257484	// uabd.8b v4, v4, v5
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e307624	// uabd.8b v4, v17, v16
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e317644	// uabd.8b v4, v18, v17
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e2774c4	// uabd.8b v4, v6, v7
	WORD	$0x2e3074b1	// uabd.8b v17, v5, v16
	WORD	$0x2e240c84	// uqadd.8b v4, v4, v4
	WORD	$0x2f0f0631	// ushr.8b v17, v17, #0x1
	WORD	$0x2e310c84	// uqadd.8b v4, v4, v17
	WORD	$0x2e233c21	// cmhs.8b v1, v1, v3
	WORD	$0x2e243c00	// cmhs.8b v0, v0, v4
	WORD	$0x0f04e403	// movi.8b v3, #0x80
	WORD	$0x2e231ca4	// eor.8b v4, v5, v3
	WORD	$0x2e231cc5	// eor.8b v5, v6, v3
	WORD	$0x2e231ce6	// eor.8b v6, v7, v3
	WORD	$0x2e231e07	// eor.8b v7, v16, v3
	WORD	$0x0e272c90	// sqsub.8b v16, v4, v7
	WORD	$0x0e301c50	// and.8b v16, v2, v16
	WORD	$0x0e252cd1	// sqsub.8b v17, v6, v5
	WORD	$0x0e310e10	// sqadd.8b v16, v16, v17
	WORD	$0x0e310e10	// sqadd.8b v16, v16, v17
	WORD	$0x0e310e10	// sqadd.8b v16, v16, v17
	WORD	$0x0e301c00	// and.8b v0, v0, v16
	WORD	$0x0e201c20	// and.8b v0, v1, v0
	WORD	$0x0f00e481	// movi.8b v1, #0x4
	WORD	$0x0e210c01	// sqadd.8b v1, v0, v1
	WORD	$0x0f0d0421	// sshr.8b v1, v1, #0x3
	WORD	$0x0f00e470	// movi.8b v16, #0x3
	WORD	$0x0e300c00	// sqadd.8b v0, v0, v16
	WORD	$0x0f0d0400	// sshr.8b v0, v0, #0x3
	WORD	$0x0e212cc6	// sqsub.8b v6, v6, v1
	WORD	$0x0e200ca0	// sqadd.8b v0, v5, v0
	WORD	$0x2e231cc5	// eor.8b v5, v6, v3
	WORD	$0x2e231c00	// eor.8b v0, v0, v3
	WORD	$0x0f0f2421	// srshr.8b v1, v1, #0x1
	WORD	$0x0e621c21	// bic.8b v1, v1, v2
	WORD	$0x0e212ce2	// sqsub.8b v2, v7, v1
	WORD	$0x0e210c81	// sqadd.8b v1, v4, v1
	WORD	$0x2e231c21	// eor.8b v1, v1, v3
	WORD	$0x531f7829	// lsl w9, w1, #1
	WORD	$0xcb29c009	// sub x9, x0, w9, sxtw
	WORD	$0xfd000121	// str d1, [x9]
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd000120	// str d0, [x9]
	WORD	$0x2e231c40	// eor.8b v0, v2, v3
	WORD	$0xfd000145	// str d5, [x10]
	WORD	$0xfc286940	// str d0, [x10, x8]
	WORD	$0x14000001	// b end (was ret)
	RET

TEXT ·lpfVertical4NEON(SB), NOSPLIT, $0-40
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	blimit+16(FP), R2
	MOVD	limit+24(FP), R3
	MOVD	thresh+32(FP), R4
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x0d40c040	// ld1r.8b { v0 }, [x2]
	WORD	$0x0d40c061	// ld1r.8b { v1 }, [x3]
	WORD	$0x0d40c082	// ld1r.8b { v2 }, [x4]
	WORD	$0xfc5fcc03	// ldr d3, [x0, #-0x4]!
	WORD	$0x8b080009	// add x9, x0, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400124	// ldr d4, [x9]
	WORD	$0xfd400145	// ldr d5, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400126	// ldr d6, [x9]
	WORD	$0xfd400147	// ldr d7, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400130	// ldr d16, [x9]
	WORD	$0xfd400151	// ldr d17, [x10]
	WORD	$0xfc686952	// ldr d18, [x10, x8]
	WORD	$0x4e043863	// zip1.16b v3, v3, v4
	WORD	$0x4e0638a4	// zip1.16b v4, v5, v6
	WORD	$0x4e1038e5	// zip1.16b v5, v7, v16
	WORD	$0x4e123a26	// zip1.16b v6, v17, v18
	WORD	$0x4e443867	// zip1.8h v7, v3, v4
	WORD	$0x4e447863	// zip2.8h v3, v3, v4
	WORD	$0x4e4638a4	// zip1.8h v4, v5, v6
	WORD	$0x4e4678a5	// zip2.8h v5, v5, v6
	WORD	$0x4e8438e6	// zip1.4s v6, v7, v4
	WORD	$0x4e8478e4	// zip2.4s v4, v7, v4
	WORD	$0x4e853867	// zip1.4s v7, v3, v5
	WORD	$0x4e857863	// zip2.4s v3, v3, v5
	WORD	$0x6e0640c5	// ext.16b v5, v6, v6, #0x8
	WORD	$0x6e044090	// ext.16b v16, v4, v4, #0x8
	WORD	$0x6e0740f1	// ext.16b v17, v7, v7, #0x8
	WORD	$0x6e034072	// ext.16b v18, v3, v3, #0x8
	WORD	$0x2e307493	// uabd.8b v19, v4, v16
	WORD	$0x2e277634	// uabd.8b v20, v17, v7
	WORD	$0x2e346673	// umax.8b v19, v19, v20
	WORD	$0x2e223662	// cmhi.8b v2, v19, v2
	WORD	$0x2e2574c6	// uabd.8b v6, v6, v5
	WORD	$0x2e266666	// umax.8b v6, v19, v6
	WORD	$0x2e2474a5	// uabd.8b v5, v5, v4
	WORD	$0x2e2564c5	// umax.8b v5, v6, v5
	WORD	$0x2e317466	// uabd.8b v6, v3, v17
	WORD	$0x2e2664a5	// umax.8b v5, v5, v6
	WORD	$0x2e237643	// uabd.8b v3, v18, v3
	WORD	$0x2e2364a3	// umax.8b v3, v5, v3
	WORD	$0x2e277605	// uabd.8b v5, v16, v7
	WORD	$0x2e317486	// uabd.8b v6, v4, v17
	WORD	$0x2e250ca5	// uqadd.8b v5, v5, v5
	WORD	$0x2f0f04c6	// ushr.8b v6, v6, #0x1
	WORD	$0x2e260ca5	// uqadd.8b v5, v5, v6
	WORD	$0x2e233c21	// cmhs.8b v1, v1, v3
	WORD	$0x2e253c00	// cmhs.8b v0, v0, v5
	WORD	$0x0f04e403	// movi.8b v3, #0x80
	WORD	$0x2e231c84	// eor.8b v4, v4, v3
	WORD	$0x2e231e05	// eor.8b v5, v16, v3
	WORD	$0x2e231ce6	// eor.8b v6, v7, v3
	WORD	$0x2e231e27	// eor.8b v7, v17, v3
	WORD	$0x0e272c90	// sqsub.8b v16, v4, v7
	WORD	$0x0e301c50	// and.8b v16, v2, v16
	WORD	$0x0e252cd1	// sqsub.8b v17, v6, v5
	WORD	$0x0e310e10	// sqadd.8b v16, v16, v17
	WORD	$0x0e310e10	// sqadd.8b v16, v16, v17
	WORD	$0x0e310e10	// sqadd.8b v16, v16, v17
	WORD	$0x0e301c00	// and.8b v0, v0, v16
	WORD	$0x0e201c20	// and.8b v0, v1, v0
	WORD	$0x0f00e481	// movi.8b v1, #0x4
	WORD	$0x0e210c01	// sqadd.8b v1, v0, v1
	WORD	$0x0f0d0421	// sshr.8b v1, v1, #0x3
	WORD	$0x0f00e470	// movi.8b v16, #0x3
	WORD	$0x0e300c00	// sqadd.8b v0, v0, v16
	WORD	$0x0f0d0400	// sshr.8b v0, v0, #0x3
	WORD	$0x0e212cc6	// sqsub.8b v6, v6, v1
	WORD	$0x0e200ca0	// sqadd.8b v0, v5, v0
	WORD	$0x2e231cd2	// eor.8b v18, v6, v3
	WORD	$0x2e231c11	// eor.8b v17, v0, v3
	WORD	$0x0f0f2420	// srshr.8b v0, v1, #0x1
	WORD	$0x0e621c00	// bic.8b v0, v0, v2
	WORD	$0x0e202ce1	// sqsub.8b v1, v7, v0
	WORD	$0x0e200c80	// sqadd.8b v0, v4, v0
	WORD	$0x2e231c33	// eor.8b v19, v1, v3
	WORD	$0x2e231c10	// eor.8b v16, v0, v3
	WORD	$0x91000809	// add x9, x0, #0x2
	WORD	$0x0da82130	// st4.b { v16, v17, v18, v19 }[0], [x9], x8
	WORD	$0x0da82530	// st4.b { v16, v17, v18, v19 }[1], [x9], x8
	WORD	$0x0da82930	// st4.b { v16, v17, v18, v19 }[2], [x9], x8
	WORD	$0x0da82d30	// st4.b { v16, v17, v18, v19 }[3], [x9], x8
	WORD	$0x0da83130	// st4.b { v16, v17, v18, v19 }[4], [x9], x8
	WORD	$0x0da83530	// st4.b { v16, v17, v18, v19 }[5], [x9], x8
	WORD	$0x0da83930	// st4.b { v16, v17, v18, v19 }[6], [x9], x8
	WORD	$0x0d203d30	// st4.b { v16, v17, v18, v19 }[7], [x9]
	WORD	$0x14000001	// b end (was ret)
	RET

TEXT ·lpfHorizontal4DualNEON(SB), NOSPLIT, $0-64
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	blimit0+16(FP), R2
	MOVD	limit0+24(FP), R3
	MOVD	thresh0+32(FP), R4
	MOVD	blimit1+40(FP), R5
	MOVD	limit1+48(FP), R6
	MOVD	thresh1+56(FP), R7
	WORD	$0x0d40c0a0	// ld1r.8b { v0 }, [x5]
	WORD	$0x0d40c041	// ld1r.8b { v1 }, [x2]
	WORD	$0x6e180401	// mov.d v1[1], v0[0]
	WORD	$0x0d40c0c0	// ld1r.8b { v0 }, [x6]
	WORD	$0x0d40c062	// ld1r.8b { v2 }, [x3]
	WORD	$0x6e180402	// mov.d v2[1], v0[0]
	WORD	$0x0d40c0e0	// ld1r.8b { v0 }, [x7]
	WORD	$0x0d40c083	// ld1r.8b { v3 }, [x4]
	WORD	$0x6e180403	// mov.d v3[1], v0[0]
	WORD	$0x531e7428	// lsl w8, w1, #2
	WORD	$0xcb28c009	// sub x9, x0, w8, sxtw
	WORD	$0x3dc00120	// ldr q0, [x9]
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3dc00124	// ldr q4, [x9]
	WORD	$0x3dc00145	// ldr q5, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3dc00126	// ldr q6, [x9]
	WORD	$0x3dc00147	// ldr q7, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3dc00130	// ldr q16, [x9]
	WORD	$0x3dc00151	// ldr q17, [x10]
	WORD	$0x3ce86952	// ldr q18, [x10, x8]
	WORD	$0x6e2674b3	// uabd.16b v19, v5, v6
	WORD	$0x6e277614	// uabd.16b v20, v16, v7
	WORD	$0x6e346673	// umax.16b v19, v19, v20
	WORD	$0x6e233663	// cmhi.16b v3, v19, v3
	WORD	$0x6e247400	// uabd.16b v0, v0, v4
	WORD	$0x6e206660	// umax.16b v0, v19, v0
	WORD	$0x6e257484	// uabd.16b v4, v4, v5
	WORD	$0x6e246400	// umax.16b v0, v0, v4
	WORD	$0x6e307624	// uabd.16b v4, v17, v16
	WORD	$0x6e246400	// umax.16b v0, v0, v4
	WORD	$0x6e317644	// uabd.16b v4, v18, v17
	WORD	$0x6e246400	// umax.16b v0, v0, v4
	WORD	$0x6e2774c4	// uabd.16b v4, v6, v7
	WORD	$0x6e3074b1	// uabd.16b v17, v5, v16
	WORD	$0x6e240c84	// uqadd.16b v4, v4, v4
	WORD	$0x6f0f0631	// ushr.16b v17, v17, #0x1
	WORD	$0x6e310c84	// uqadd.16b v4, v4, v17
	WORD	$0x6e203c40	// cmhs.16b v0, v2, v0
	WORD	$0x6e243c21	// cmhs.16b v1, v1, v4
	WORD	$0x4f04e402	// movi.16b v2, #0x80
	WORD	$0x6e221ca4	// eor.16b v4, v5, v2
	WORD	$0x6e221cc5	// eor.16b v5, v6, v2
	WORD	$0x6e221ce6	// eor.16b v6, v7, v2
	WORD	$0x6e221e07	// eor.16b v7, v16, v2
	WORD	$0x4e272c90	// sqsub.16b v16, v4, v7
	WORD	$0x4e301c70	// and.16b v16, v3, v16
	WORD	$0x4e252cd1	// sqsub.16b v17, v6, v5
	WORD	$0x4e310e10	// sqadd.16b v16, v16, v17
	WORD	$0x4e310e10	// sqadd.16b v16, v16, v17
	WORD	$0x4e310e10	// sqadd.16b v16, v16, v17
	WORD	$0x4e301c21	// and.16b v1, v1, v16
	WORD	$0x4e211c00	// and.16b v0, v0, v1
	WORD	$0x4f00e481	// movi.16b v1, #0x4
	WORD	$0x4e210c01	// sqadd.16b v1, v0, v1
	WORD	$0x4f0d0421	// sshr.16b v1, v1, #0x3
	WORD	$0x4f00e470	// movi.16b v16, #0x3
	WORD	$0x4e300c00	// sqadd.16b v0, v0, v16
	WORD	$0x4f0d0400	// sshr.16b v0, v0, #0x3
	WORD	$0x4e212cc6	// sqsub.16b v6, v6, v1
	WORD	$0x4e200ca0	// sqadd.16b v0, v5, v0
	WORD	$0x6e221cc5	// eor.16b v5, v6, v2
	WORD	$0x6e221c00	// eor.16b v0, v0, v2
	WORD	$0x4f0f2421	// srshr.16b v1, v1, #0x1
	WORD	$0x4e631c21	// bic.16b v1, v1, v3
	WORD	$0x4e212ce3	// sqsub.16b v3, v7, v1
	WORD	$0x4e210c81	// sqadd.16b v1, v4, v1
	WORD	$0x6e221c21	// eor.16b v1, v1, v2
	WORD	$0x531f7829	// lsl w9, w1, #1
	WORD	$0xcb29c009	// sub x9, x0, w9, sxtw
	WORD	$0x3d800121	// str q1, [x9]
	WORD	$0x8b080129	// add x9, x9, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0x3d800120	// str q0, [x9]
	WORD	$0x6e221c60	// eor.16b v0, v3, v2
	WORD	$0x3d800145	// str q5, [x10]
	WORD	$0x3ca86940	// str q0, [x10, x8]
	WORD	$0x14000001	// b end (was ret)
	RET

TEXT ·lpfVertical4DualNEON(SB), NOSPLIT, $0-64
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	blimit0+16(FP), R2
	MOVD	limit0+24(FP), R3
	MOVD	thresh0+32(FP), R4
	MOVD	blimit1+40(FP), R5
	MOVD	limit1+48(FP), R6
	MOVD	thresh1+56(FP), R7
	WORD	$0x0d40c0a1	// ld1r.8b { v1 }, [x5]
	WORD	$0x0d40c040	// ld1r.8b { v0 }, [x2]
	WORD	$0x6e180420	// mov.d v0[1], v1[0]
	WORD	$0x0d40c0c2	// ld1r.8b { v2 }, [x6]
	WORD	$0x0d40c061	// ld1r.8b { v1 }, [x3]
	WORD	$0x6e180441	// mov.d v1[1], v2[0]
	WORD	$0x0d40c0e3	// ld1r.8b { v3 }, [x7]
	WORD	$0x0d40c082	// ld1r.8b { v2 }, [x4]
	WORD	$0x6e180462	// mov.d v2[1], v3[0]
	WORD	$0xfc5fcc03	// ldr d3, [x0, #-0x4]!
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b080009	// add x9, x0, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400125	// ldr d5, [x9]
	WORD	$0xfd400144	// ldr d4, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400127	// ldr d7, [x9]
	WORD	$0xfd400146	// ldr d6, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400130	// ldr d16, [x9]
	WORD	$0xfd400151	// ldr d17, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400132	// ldr d18, [x9]
	WORD	$0xfd400153	// ldr d19, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400134	// ldr d20, [x9]
	WORD	$0xfd400155	// ldr d21, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400136	// ldr d22, [x9]
	WORD	$0xfd400157	// ldr d23, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd400138	// ldr d24, [x9]
	WORD	$0xfc686959	// ldr d25, [x10, x8]
	WORD	$0x6e180663	// mov.d v3[1], v19[0]
	WORD	$0x6e180685	// mov.d v5[1], v20[0]
	WORD	$0x6e1806a4	// mov.d v4[1], v21[0]
	WORD	$0x6e1806c7	// mov.d v7[1], v22[0]
	WORD	$0x6e1806e6	// mov.d v6[1], v23[0]
	WORD	$0x6e180710	// mov.d v16[1], v24[0]
	WORD	$0xfd400153	// ldr d19, [x10]
	WORD	$0x6e180671	// mov.d v17[1], v19[0]
	WORD	$0x6e180732	// mov.d v18[1], v25[0]
	WORD	$0x4e052873	// trn1.16b v19, v3, v5
	WORD	$0x4e056863	// trn2.16b v3, v3, v5
	WORD	$0x4e072885	// trn1.16b v5, v4, v7
	WORD	$0x4e076884	// trn2.16b v4, v4, v7
	WORD	$0x4e1028c7	// trn1.16b v7, v6, v16
	WORD	$0x4e1068c6	// trn2.16b v6, v6, v16
	WORD	$0x4e122a30	// trn1.16b v16, v17, v18
	WORD	$0x4e126a31	// trn2.16b v17, v17, v18
	WORD	$0x4e452a72	// trn1.8h v18, v19, v5
	WORD	$0x4e456a65	// trn2.8h v5, v19, v5
	WORD	$0x4e442873	// trn1.8h v19, v3, v4
	WORD	$0x4e446863	// trn2.8h v3, v3, v4
	WORD	$0x4e5028e4	// trn1.8h v4, v7, v16
	WORD	$0x4e5068e7	// trn2.8h v7, v7, v16
	WORD	$0x4e5128d0	// trn1.8h v16, v6, v17
	WORD	$0x4e5168c6	// trn2.8h v6, v6, v17
	WORD	$0x4e842a51	// trn1.4s v17, v18, v4
	WORD	$0x4e846a44	// trn2.4s v4, v18, v4
	WORD	$0x4e8728b2	// trn1.4s v18, v5, v7
	WORD	$0x4e8768a5	// trn2.4s v5, v5, v7
	WORD	$0x4e902a67	// trn1.4s v7, v19, v16
	WORD	$0x4e906a70	// trn2.4s v16, v19, v16
	WORD	$0x4e862873	// trn1.4s v19, v3, v6
	WORD	$0x4e866863	// trn2.4s v3, v3, v6
	WORD	$0x6e337646	// uabd.16b v6, v18, v19
	WORD	$0x6e247614	// uabd.16b v20, v16, v4
	WORD	$0x6e3464c6	// umax.16b v6, v6, v20
	WORD	$0x6e2234c2	// cmhi.16b v2, v6, v2
	WORD	$0x6e277631	// uabd.16b v17, v17, v7
	WORD	$0x6e3164c6	// umax.16b v6, v6, v17
	WORD	$0x6e3274e7	// uabd.16b v7, v7, v18
	WORD	$0x6e2764c6	// umax.16b v6, v6, v7
	WORD	$0x6e3074a7	// uabd.16b v7, v5, v16
	WORD	$0x6e2764c6	// umax.16b v6, v6, v7
	WORD	$0x6e257463	// uabd.16b v3, v3, v5
	WORD	$0x6e2364c3	// umax.16b v3, v6, v3
	WORD	$0x6e247665	// uabd.16b v5, v19, v4
	WORD	$0x6e307646	// uabd.16b v6, v18, v16
	WORD	$0x6e250ca5	// uqadd.16b v5, v5, v5
	WORD	$0x6f0f04c6	// ushr.16b v6, v6, #0x1
	WORD	$0x6e260ca5	// uqadd.16b v5, v5, v6
	WORD	$0x6e233c21	// cmhs.16b v1, v1, v3
	WORD	$0x6e253c00	// cmhs.16b v0, v0, v5
	WORD	$0x4f04e403	// movi.16b v3, #0x80
	WORD	$0x6e231e45	// eor.16b v5, v18, v3
	WORD	$0x6e231e66	// eor.16b v6, v19, v3
	WORD	$0x6e231c84	// eor.16b v4, v4, v3
	WORD	$0x6e231e07	// eor.16b v7, v16, v3
	WORD	$0x4e272cb0	// sqsub.16b v16, v5, v7
	WORD	$0x4e301c50	// and.16b v16, v2, v16
	WORD	$0x4e262c91	// sqsub.16b v17, v4, v6
	WORD	$0x4e310e10	// sqadd.16b v16, v16, v17
	WORD	$0x4e310e10	// sqadd.16b v16, v16, v17
	WORD	$0x4e310e10	// sqadd.16b v16, v16, v17
	WORD	$0x4e301c00	// and.16b v0, v0, v16
	WORD	$0x4e201c20	// and.16b v0, v1, v0
	WORD	$0x4f00e481	// movi.16b v1, #0x4
	WORD	$0x4e210c01	// sqadd.16b v1, v0, v1
	WORD	$0x4f0d0421	// sshr.16b v1, v1, #0x3
	WORD	$0x4f00e470	// movi.16b v16, #0x3
	WORD	$0x4e300c00	// sqadd.16b v0, v0, v16
	WORD	$0x4f0d0400	// sshr.16b v0, v0, #0x3
	WORD	$0x4e212c84	// sqsub.16b v4, v4, v1
	WORD	$0x4e200cc0	// sqadd.16b v0, v6, v0
	WORD	$0x6e231c92	// eor.16b v18, v4, v3
	WORD	$0x6e231c11	// eor.16b v17, v0, v3
	WORD	$0x4f0f2420	// srshr.16b v0, v1, #0x1
	WORD	$0x4e621c00	// bic.16b v0, v0, v2
	WORD	$0x4e202ce1	// sqsub.16b v1, v7, v0
	WORD	$0x4e200ca0	// sqadd.16b v0, v5, v0
	WORD	$0x6e231c33	// eor.16b v19, v1, v3
	WORD	$0x6e231c10	// eor.16b v16, v0, v3
	WORD	$0x91000809	// add x9, x0, #0x2
	WORD	$0x937d7c2a	// sbfiz x10, x1, #3, #32
	WORD	$0x8b08012b	// add x11, x9, x8
	WORD	$0x0daa2130	// st4.b { v16, v17, v18, v19 }[0], [x9], x10
	WORD	$0x0da82570	// st4.b { v16, v17, v18, v19 }[1], [x11], x8
	WORD	$0x0da82970	// st4.b { v16, v17, v18, v19 }[2], [x11], x8
	WORD	$0x0da82d70	// st4.b { v16, v17, v18, v19 }[3], [x11], x8
	WORD	$0x0da83170	// st4.b { v16, v17, v18, v19 }[4], [x11], x8
	WORD	$0x0da83570	// st4.b { v16, v17, v18, v19 }[5], [x11], x8
	WORD	$0x0da83970	// st4.b { v16, v17, v18, v19 }[6], [x11], x8
	WORD	$0x0d203d70	// st4.b { v16, v17, v18, v19 }[7], [x11]
	WORD	$0x6e104200	// ext.16b v0, v16, v16, #0x8
	WORD	$0x6e114221	// ext.16b v1, v17, v17, #0x8
	WORD	$0x6e124242	// ext.16b v2, v18, v18, #0x8
	WORD	$0x6e134263	// ext.16b v3, v19, v19, #0x8
	WORD	$0x0da82120	// st4.b { v0, v1, v2, v3 }[0], [x9], x8
	WORD	$0x0da82520	// st4.b { v0, v1, v2, v3 }[1], [x9], x8
	WORD	$0x0da82920	// st4.b { v0, v1, v2, v3 }[2], [x9], x8
	WORD	$0x0da82d20	// st4.b { v0, v1, v2, v3 }[3], [x9], x8
	WORD	$0x0da83120	// st4.b { v0, v1, v2, v3 }[4], [x9], x8
	WORD	$0x0da83520	// st4.b { v0, v1, v2, v3 }[5], [x9], x8
	WORD	$0x0da83920	// st4.b { v0, v1, v2, v3 }[6], [x9], x8
	WORD	$0x0d203d20	// st4.b { v0, v1, v2, v3 }[7], [x9]
	WORD	$0x14000001	// b end (was ret)
	RET

TEXT ·lpfHorizontal16NEON(SB), NOSPLIT, $96-40
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	blimit+16(FP), R2
	MOVD	limit+24(FP), R3
	MOVD	thresh+32(FP), R4
	WORD	$0xd503201f	// nop (was: sub sp, sp, #0x50 — hosted in Go frame)
	WORD	$0x6d023bef	// stp d15, d14, [sp, #0x10] (sp offset +16)
	WORD	$0x6d0333ed	// stp d13, d12, [sp, #0x20] (sp offset +16)
	WORD	$0x6d042beb	// stp d11, d10, [sp, #0x30] (sp offset +16)
	WORD	$0x6d0523e9	// stp d9, d8, [sp, #0x40] (sp offset +16)
	WORD	$0x531d7029	// lsl w9, w1, #3
	WORD	$0xcb29c00a	// sub x10, x0, w9, sxtw
	WORD	$0xfd400156	// ldr d22, [x10]
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b08014a	// add x10, x10, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd400150	// ldr d16, [x10]
	WORD	$0xfd400167	// ldr d7, [x11]
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd400146	// ldr d6, [x10]
	WORD	$0xfd400171	// ldr d17, [x11]
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd400157	// ldr d23, [x10]
	WORD	$0xfd40017a	// ldr d26, [x11]
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd40015d	// ldr d29, [x10]
	WORD	$0xfd40017c	// ldr d28, [x11]
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd40015b	// ldr d27, [x10]
	WORD	$0xfd400160	// ldr d0, [x11]
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd400155	// ldr d21, [x10]
	WORD	$0xfd400172	// ldr d18, [x11]
	WORD	$0x8b08016a	// add x10, x11, x8
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0xfd40014f	// ldr d15, [x10]
	WORD	$0xfc686974	// ldr d20, [x11, x8]
	WORD	$0x0d40c041	// ld1r.8b { v1 }, [x2]
	WORD	$0xfd400173	// ldr d19, [x11]
	WORD	$0x0d40c062	// ld1r.8b { v2 }, [x3]
	WORD	$0x2e3d7743	// uabd.8b v3, v26, v29
	WORD	$0x2e3c7764	// uabd.8b v4, v27, v28
	WORD	$0x2e246479	// umax.8b v25, v3, v4
	WORD	$0x2e377623	// uabd.8b v3, v17, v23
	WORD	$0x2e236723	// umax.8b v3, v25, v3
	WORD	$0x2e3a76e4	// uabd.8b v4, v23, v26
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e3b7404	// uabd.8b v4, v0, v27
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e2076a4	// uabd.8b v4, v21, v0
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e3c77a4	// uabd.8b v4, v29, v28
	WORD	$0x2e3b7758	// uabd.8b v24, v26, v27
	WORD	$0x2e240c84	// uqadd.8b v4, v4, v4
	WORD	$0x2f0f0718	// ushr.8b v24, v24, #0x1
	WORD	$0x2e380c84	// uqadd.8b v4, v4, v24
	WORD	$0x2e233c42	// cmhs.8b v2, v2, v3
	WORD	$0x2e243c21	// cmhs.8b v1, v1, v4
	WORD	$0x0e211c5e	// and.8b v30, v2, v1
	WORD	$0x2e3d76e1	// uabd.8b v1, v23, v29
	WORD	$0x2e216721	// umax.8b v1, v25, v1
	WORD	$0x2e3c7402	// uabd.8b v2, v0, v28
	WORD	$0x2e226421	// umax.8b v1, v1, v2
	WORD	$0x2e3d7622	// uabd.8b v2, v17, v29
	WORD	$0x2e226421	// umax.8b v1, v1, v2
	WORD	$0x2e3c76a2	// uabd.8b v2, v21, v28
	WORD	$0x2e226421	// umax.8b v1, v1, v2
	WORD	$0x0f00e458	// movi.8b v24, #0x2
	WORD	$0x2e213701	// cmhi.8b v1, v24, v1
	WORD	$0x0e211fc8	// and.8b v8, v30, v1
	WORD	$0x2ea02901	// uaddlp.1d v1, v8
	WORD	$0x1e26002a	// fmov w10, s1
	WORD	$0x3100095f	// cmn w10, #0x2
	WORD	$0x54000420	// b.eq 0x1714 <_vpx_lpf_horizontal_16_neon+0x1a8>
	WORD	$0x0d40c081	// ld1r.8b { v1 }, [x4]
	WORD	$0x2e213723	// cmhi.8b v3, v25, v1
	WORD	$0x0f04e404	// movi.8b v4, #0x80
	WORD	$0x2e241f59	// eor.8b v25, v26, v4
	WORD	$0x2e241fa1	// eor.8b v1, v29, v4
	WORD	$0x2e241f82	// eor.8b v2, v28, v4
	WORD	$0x2e241f7f	// eor.8b v31, v27, v4
	WORD	$0x0e3f2f29	// sqsub.8b v9, v25, v31
	WORD	$0x0e291c69	// and.8b v9, v3, v9
	WORD	$0x0e212c4a	// sqsub.8b v10, v2, v1
	WORD	$0x0e2a0d29	// sqadd.8b v9, v9, v10
	WORD	$0x0e2a0d29	// sqadd.8b v9, v9, v10
	WORD	$0x0e2a0d29	// sqadd.8b v9, v9, v10
	WORD	$0x0e291fde	// and.8b v30, v30, v9
	WORD	$0x0f00e489	// movi.8b v9, #0x4
	WORD	$0x0e290fc9	// sqadd.8b v9, v30, v9
	WORD	$0x0f0d0529	// sshr.8b v9, v9, #0x3
	WORD	$0x0f00e46a	// movi.8b v10, #0x3
	WORD	$0x0e2a0fde	// sqadd.8b v30, v30, v10
	WORD	$0x0f0d07de	// sshr.8b v30, v30, #0x3
	WORD	$0x0e292c42	// sqsub.8b v2, v2, v9
	WORD	$0x0e3e0c3e	// sqadd.8b v30, v1, v30
	WORD	$0x2e241c42	// eor.8b v2, v2, v4
	WORD	$0x2e241fc1	// eor.8b v1, v30, v4
	WORD	$0xfd000fe1	// str d1, [sp, #0x8] (sp offset +16)
	WORD	$0x0f0f253e	// srshr.8b v30, v9, #0x1
	WORD	$0x0e631fc3	// bic.8b v3, v30, v3
	WORD	$0x0e232ffe	// sqsub.8b v30, v31, v3
	WORD	$0x0e230f39	// sqadd.8b v25, v25, v3
	WORD	$0x2e241fc3	// eor.8b v3, v30, v4
	WORD	$0x2e241f24	// eor.8b v4, v25, v4
	WORD	$0x34001a2a	// cbz w10, 0x1a54 <_vpx_lpf_horizontal_16_neon+0x4e8>
	WORD	$0x2e3d76d9	// uabd.8b v25, v22, v29
	WORD	$0x2e3d761e	// uabd.8b v30, v16, v29
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e3d74fe	// uabd.8b v30, v7, v29
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e3d74de	// uabd.8b v30, v6, v29
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e3c765e	// uabd.8b v30, v18, v28
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e3c75fe	// uabd.8b v30, v15, v28
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e3c767e	// uabd.8b v30, v19, v28
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e3c769e	// uabd.8b v30, v20, v28
	WORD	$0x2e3e6739	// umax.8b v25, v25, v30
	WORD	$0x2e393718	// cmhi.8b v24, v24, v25
	WORD	$0x0e281f18	// and.8b v24, v24, v8
	WORD	$0x2ea02b19	// uaddlp.1d v25, v24
	WORD	$0x1e26032b	// fmov w11, s25
	WORD	$0x2f08a639	// ushll.8h v25, v17, #0x0
	WORD	$0x3100097f	// cmn w11, #0x2
	WORD	$0x54000121	// b.ne 0x178c <_vpx_lpf_horizontal_16_neon+0x220>
	WORD	$0x2f08a6e8	// ushll.8h v8, v23, #0x0
	WORD	$0x2f08a74b	// ushll.8h v11, v26, #0x0
	WORD	$0x2f08a7aa	// ushll.8h v10, v29, #0x0
	WORD	$0x2f08a789	// ushll.8h v9, v28, #0x0
	WORD	$0x2f08a77f	// ushll.8h v31, v27, #0x0
	WORD	$0x2f08a41e	// ushll.8h v30, v0, #0x0
	WORD	$0x2f08a6bb	// ushll.8h v27, v21, #0x0
	WORD	$0x14000036	// b 0x1860 <_vpx_lpf_horizontal_16_neon+0x2f4>
	WORD	$0xfd000bef	// str d15, [sp] (sp offset +16)
	WORD	$0x0e212b3e	// xtn.8b v30, v25
	WORD	$0x0f00e47f	// movi.8b v31, #0x3
	WORD	$0x2f09a6e9	// ushll.8h v9, v23, #0x1
	WORD	$0x2e3f83c9	// umlal.8h v9, v30, v31
	WORD	$0x2f08a74b	// ushll.8h v11, v26, #0x0
	WORD	$0x2e3a113e	// uaddw.8h v30, v9, v26
	WORD	$0x2f08a7aa	// ushll.8h v10, v29, #0x0
	WORD	$0x2e3d13de	// uaddw.8h v30, v30, v29
	WORD	$0x2f08a789	// ushll.8h v9, v28, #0x0
	WORD	$0x2e3c13de	// uaddw.8h v30, v30, v28
	WORD	$0x0f0d8fcc	// rshrn.8b v12, v30, #0x3
	WORD	$0x2f08a77f	// ushll.8h v31, v27, #0x0
	WORD	$0x2e3102ed	// uaddl.8h v13, v23, v17
	WORD	$0x6e6d856d	// sub.8h v13, v11, v13
	WORD	$0x2e3b11ad	// uaddw.8h v13, v13, v27
	WORD	$0x4e7e85ad	// add.8h v13, v13, v30
	WORD	$0x0f0d8dae	// rshrn.8b v14, v13, #0x3
	WORD	$0x2f08a41e	// ushll.8h v30, v0, #0x0
	WORD	$0x2e31034f	// uaddl.8h v15, v26, v17
	WORD	$0x6e6f854f	// sub.8h v15, v10, v15
	WORD	$0x2e2011ef	// uaddw.8h v15, v15, v0
	WORD	$0x4e6d85ed	// add.8h v13, v15, v13
	WORD	$0x0f0d8daf	// rshrn.8b v15, v13, #0x3
	WORD	$0x2e3103bd	// uaddl.8h v29, v29, v17
	WORD	$0x6e7d853d	// sub.8h v29, v9, v29
	WORD	$0x2e3513bd	// uaddw.8h v29, v29, v21
	WORD	$0x4e6d87bd	// add.8h v29, v29, v13
	WORD	$0x0f0d8fad	// rshrn.8b v13, v29, #0x3
	WORD	$0x2e37039c	// uaddl.8h v28, v28, v23
	WORD	$0x6e7c87fc	// sub.8h v28, v31, v28
	WORD	$0x2e35139c	// uaddw.8h v28, v28, v21
	WORD	$0x4e7d879c	// add.8h v28, v28, v29
	WORD	$0x0f0d8f9d	// rshrn.8b v29, v28, #0x3
	WORD	$0x2e3a037a	// uaddl.8h v26, v27, v26
	WORD	$0x6e7a87da	// sub.8h v26, v30, v26
	WORD	$0x2e35135a	// uaddw.8h v26, v26, v21
	WORD	$0x4e7c875a	// add.8h v26, v26, v28
	WORD	$0x0f0d8f5b	// rshrn.8b v27, v26, #0x3
	WORD	$0x0ea81d1a	// mov.8b v26, v8
	WORD	$0x2e771d9a	// bsl.8b v26, v12, v23
	WORD	$0x2ea81dc4	// bit.8b v4, v14, v8
	WORD	$0xfd400fe1	// ldr d1, [sp, #0x8] (sp offset +16)
	WORD	$0x2ea81de1	// bit.8b v1, v15, v8
	WORD	$0xfd000fe1	// str d1, [sp, #0x8] (sp offset +16)
	WORD	$0x2ea81da2	// bit.8b v2, v13, v8
	WORD	$0x2ea81fa3	// bit.8b v3, v29, v8
	WORD	$0x2ea81f60	// bit.8b v0, v27, v8
	WORD	$0x34000f4b	// cbz w11, 0x1a34 <_vpx_lpf_horizontal_16_neon+0x4c8>
	WORD	$0x2f08a6e8	// ushll.8h v8, v23, #0x0
	WORD	$0x2f08a6bb	// ushll.8h v27, v21, #0x0
	WORD	$0x4eba1f57	// mov.16b v23, v26
	WORD	$0xfd400bef	// ldr d15, [sp] (sp offset +16)
	WORD	$0x0f00e4fa	// movi.8b v26, #0x7
	WORD	$0x2f09a61c	// ushll.8h v28, v16, #0x1
	WORD	$0x2e3a82dc	// umlal.8h v28, v22, v26
	WORD	$0x2f08a4fa	// ushll.8h v26, v7, #0x0
	WORD	$0x2e27139c	// uaddw.8h v28, v28, v7
	WORD	$0x2f08a4cd	// ushll.8h v13, v6, #0x0
	WORD	$0x2e26139c	// uaddw.8h v28, v28, v6
	WORD	$0x4e69873d	// add.8h v29, v25, v9
	WORD	$0x4e6a879c	// add.8h v28, v28, v10
	WORD	$0x4e6b87bd	// add.8h v29, v29, v11
	WORD	$0x4e68879c	// add.8h v28, v28, v8
	WORD	$0x4e7d879d	// add.8h v29, v28, v29
	WORD	$0x2e36021c	// uaddl.8h v28, v16, v22
	WORD	$0x6e7c875a	// sub.8h v26, v26, v28
	WORD	$0x4e7f875a	// add.8h v26, v26, v31
	WORD	$0x4e7d874c	// add.8h v12, v26, v29
	WORD	$0x2e3600fa	// uaddl.8h v26, v7, v22
	WORD	$0x6e7a85ba	// sub.8h v26, v13, v26
	WORD	$0x4e7e875a	// add.8h v26, v26, v30
	WORD	$0x4e6c874d	// add.8h v13, v26, v12
	WORD	$0x2e3600da	// uaddl.8h v26, v6, v22
	WORD	$0x6e7a873a	// sub.8h v26, v25, v26
	WORD	$0x4e7b875a	// add.8h v26, v26, v27
	WORD	$0x4e6d874e	// add.8h v14, v26, v13
	WORD	$0x2f08a65a	// ushll.8h v26, v18, #0x0
	WORD	$0x2e36023c	// uaddl.8h v28, v17, v22
	WORD	$0x6e7c875a	// sub.8h v26, v26, v28
	WORD	$0x4e68875a	// add.8h v26, v26, v8
	WORD	$0x4e6e875a	// add.8h v26, v26, v14
	WORD	$0x4eaf1de1	// mov.16b v1, v15
	WORD	$0x2e2f117c	// uaddw.8h v28, v11, v15
	WORD	$0x2e36110f	// uaddw.8h v15, v8, v22
	WORD	$0x6e6f878f	// sub.8h v15, v28, v15
	WORD	$0x4e7a85ef	// add.8h v15, v15, v26
	WORD	$0x0f0c8de5	// rshrn.8b v5, v15, #0x4
	WORD	$0x2eb81ca4	// bit.8b v4, v5, v24
	WORD	$0x2e331145	// uaddw.8h v5, v10, v19
	WORD	$0x2e36116b	// uaddw.8h v11, v11, v22
	WORD	$0x6e6b84a5	// sub.8h v5, v5, v11
	WORD	$0x4e6f84a5	// add.8h v5, v5, v15
	WORD	$0x0f0c8cab	// rshrn.8b v11, v5, #0x4
	WORD	$0xfd400fef	// ldr d15, [sp, #0x8] (sp offset +16)
	WORD	$0x2eb81d6f	// bit.8b v15, v11, v24
	WORD	$0xfd000fef	// str d15, [sp, #0x8] (sp offset +16)
	WORD	$0x2e34112b	// uaddw.8h v11, v9, v20
	WORD	$0x2e361156	// uaddw.8h v22, v10, v22
	WORD	$0x6e768576	// sub.8h v22, v11, v22
	WORD	$0x4e6586c5	// add.8h v5, v22, v5
	WORD	$0x0f0c8cb6	// rshrn.8b v22, v5, #0x4
	WORD	$0x2eb81ec2	// bit.8b v2, v22, v24
	WORD	$0x2e3413f6	// uaddw.8h v22, v31, v20
	WORD	$0x2e301129	// uaddw.8h v9, v9, v16
	WORD	$0x6e6986d6	// sub.8h v22, v22, v9
	WORD	$0x4e6586d6	// add.8h v22, v22, v5
	WORD	$0x0f0c8ec5	// rshrn.8b v5, v22, #0x4
	WORD	$0x2eb81ca3	// bit.8b v3, v5, v24
	WORD	$0x340008aa	// cbz w10, 0x1a54 <_vpx_lpf_horizontal_16_neon+0x4e8>
	WORD	$0x0f0c8f45	// rshrn.8b v5, v26, #0x4
	WORD	$0x0eb81f1a	// mov.8b v26, v24
	WORD	$0x2e771cba	// bsl.8b v26, v5, v23
	WORD	$0x2e3413c5	// uaddw.8h v5, v30, v20
	WORD	$0x2e2713f7	// uaddw.8h v23, v31, v7
	WORD	$0x6e7784a5	// sub.8h v5, v5, v23
	WORD	$0x4e7684b6	// add.8h v22, v5, v22
	WORD	$0x0f0c8ec5	// rshrn.8b v5, v22, #0x4
	WORD	$0x2eb81ca0	// bit.8b v0, v5, v24
	WORD	$0x3400068b	// cbz w11, 0x1a38 <_vpx_lpf_horizontal_16_neon+0x4cc>
	WORD	$0x0f0c8fa5	// rshrn.8b v5, v29, #0x4
	WORD	$0x2ef81e05	// bif.8b v5, v16, v24
	WORD	$0x0f0c8d90	// rshrn.8b v16, v12, #0x4
	WORD	$0x2eb81e07	// bit.8b v7, v16, v24
	WORD	$0x0f0c8db0	// rshrn.8b v16, v13, #0x4
	WORD	$0x2ef81cd0	// bif.8b v16, v6, v24
	WORD	$0x0f0c8dd7	// rshrn.8b v23, v14, #0x4
	WORD	$0x2eb81ef1	// bit.8b v17, v23, v24
	WORD	$0x2e341377	// uaddw.8h v23, v27, v20
	WORD	$0x2e2613c6	// uaddw.8h v6, v30, v6
	WORD	$0x6e6686e6	// sub.8h v6, v23, v6
	WORD	$0x4e7684c6	// add.8h v6, v6, v22
	WORD	$0x0f0c8cd6	// rshrn.8b v22, v6, #0x4
	WORD	$0x2eb81ed5	// bit.8b v21, v22, v24
	WORD	$0x2e340256	// uaddl.8h v22, v18, v20
	WORD	$0x4e798777	// add.8h v23, v27, v25
	WORD	$0x6e7786d6	// sub.8h v22, v22, v23
	WORD	$0x4e6686c6	// add.8h v6, v22, v6
	WORD	$0x0f0c8cd6	// rshrn.8b v22, v6, #0x4
	WORD	$0x2ef81e56	// bif.8b v22, v18, v24
	WORD	$0x2e340037	// uaddl.8h v23, v1, v20
	WORD	$0x2e321112	// uaddw.8h v18, v8, v18
	WORD	$0x6e7286f2	// sub.8h v18, v23, v18
	WORD	$0x4e668646	// add.8h v6, v18, v6
	WORD	$0x0f0c8cd2	// rshrn.8b v18, v6, #0x4
	WORD	$0x2ef81c32	// bif.8b v18, v1, v24
	WORD	$0x4eb31e77	// mov.16b v23, v19
	WORD	$0x2e330293	// uaddl.8h v19, v20, v19
	WORD	$0x6e7c8673	// sub.8h v19, v19, v28
	WORD	$0x4e668666	// add.8h v6, v19, v6
	WORD	$0x4b010129	// sub w9, w9, w1
	WORD	$0xcb29c009	// sub x9, x0, w9, sxtw
	WORD	$0xfd000125	// str d5, [x9]
	WORD	$0x8b080509	// add x9, x8, x8, lsl #1
	WORD	$0xd37ff92a	// lsl x10, x9, #1
	WORD	$0xcb0a000b	// sub x11, x0, x10
	WORD	$0xfd000167	// str d7, [x11]
	WORD	$0xd37ef50b	// lsl x11, x8, #2
	WORD	$0x8b08016c	// add x12, x11, x8
	WORD	$0xcb0c000d	// sub x13, x0, x12
	WORD	$0xfd0001b0	// str d16, [x13]
	WORD	$0xcb0b000d	// sub x13, x0, x11
	WORD	$0xfd0001b1	// str d17, [x13]
	WORD	$0xfc296815	// str d21, [x0, x9]
	WORD	$0xfc2b6816	// str d22, [x0, x11]
	WORD	$0x0f0c8cc5	// rshrn.8b v5, v6, #0x4
	WORD	$0x2ef81ee5	// bif.8b v5, v23, v24
	WORD	$0xfc2c6812	// str d18, [x0, x12]
	WORD	$0xfc2a6805	// str d5, [x0, x10]
	WORD	$0x14000004	// b 0x1a40 <_vpx_lpf_horizontal_16_neon+0x4d4>
	WORD	$0x3400010a	// cbz w10, 0x1a54 <_vpx_lpf_horizontal_16_neon+0x4e8>
	WORD	$0x0b010429	// add w9, w1, w1, lsl #1
	WORD	$0x93407d29	// sxtw x9, w9
	WORD	$0xcb090009	// sub x9, x0, x9
	WORD	$0xfd00013a	// str d26, [x9]
	WORD	$0xd37ff909	// lsl x9, x8, #1
	WORD	$0xfc296800	// str d0, [x0, x9]
	WORD	$0x14000003	// b 0x1a5c <_vpx_lpf_horizontal_16_neon+0x4f0>
	WORD	$0x531f7829	// lsl w9, w1, #1
	WORD	$0x93407d29	// sxtw x9, w9
	WORD	$0xfd400fe0	// ldr d0, [sp, #0x8] (sp offset +16)
	WORD	$0xcb090009	// sub x9, x0, x9
	WORD	$0xfd000124	// str d4, [x9]
	WORD	$0xcb080009	// sub x9, x0, x8
	WORD	$0xfd000120	// str d0, [x9]
	WORD	$0xfd000002	// str d2, [x0]
	WORD	$0xfc286803	// str d3, [x0, x8]
	WORD	$0x6d4523e9	// ldp d9, d8, [sp, #0x40] (sp offset +16)
	WORD	$0x6d442beb	// ldp d11, d10, [sp, #0x30] (sp offset +16)
	WORD	$0x6d4333ed	// ldp d13, d12, [sp, #0x20] (sp offset +16)
	WORD	$0x6d423bef	// ldp d15, d14, [sp, #0x10] (sp offset +16)
	WORD	$0xd503201f	// nop (was: add sp, sp, #0x50 — hosted in Go frame)
	WORD	$0x14000001	// b end (was ret)
	RET

TEXT ·lpfVertical16NEON(SB), NOSPLIT, $176-40
	MOVD	s+0(FP), R0
	MOVD	pitch+8(FP), R1
	MOVD	blimit+16(FP), R2
	MOVD	limit+24(FP), R3
	MOVD	thresh+32(FP), R4
	WORD	$0xd503201f	// nop (was: sub sp, sp, #0xa0 — hosted in Go frame)
	WORD	$0x6d073bef	// stp d15, d14, [sp, #0x60] (sp offset +16)
	WORD	$0x6d0833ed	// stp d13, d12, [sp, #0x70] (sp offset +16)
	WORD	$0x6d092beb	// stp d11, d10, [sp, #0x80] (sp offset +16)
	WORD	$0x6d0a23e9	// stp d9, d8, [sp, #0x90] (sp offset +16)
	WORD	$0xaa0003ea	// mov x10, x0
	WORD	$0x3cdf8d40	// ldr q0, [x10, #-0x8]!
	WORD	$0x93407c28	// sxtw x8, w1
	WORD	$0x8b08014b	// add x11, x10, x8
	WORD	$0x8b080169	// add x9, x11, x8
	WORD	$0x3dc00161	// ldr q1, [x11]
	WORD	$0x8b08012c	// add x12, x9, x8
	WORD	$0x8b08018b	// add x11, x12, x8
	WORD	$0x3dc00182	// ldr q2, [x12]
	WORD	$0x8b08016d	// add x13, x11, x8
	WORD	$0x8b0801ac	// add x12, x13, x8
	WORD	$0x3dc001a3	// ldr q3, [x13]
	WORD	$0x3dc00124	// ldr q4, [x9]
	WORD	$0x3dc00165	// ldr q5, [x11]
	WORD	$0x3ce86986	// ldr q6, [x12, x8]
	WORD	$0x3dc00187	// ldr q7, [x12]
	WORD	$0x4e012810	// trn1.16b v16, v0, v1
	WORD	$0x4e016800	// trn2.16b v0, v0, v1
	WORD	$0x4e022881	// trn1.16b v1, v4, v2
	WORD	$0x4e026882	// trn2.16b v2, v4, v2
	WORD	$0x4e0328a4	// trn1.16b v4, v5, v3
	WORD	$0x4e0368a3	// trn2.16b v3, v5, v3
	WORD	$0x4e0628e5	// trn1.16b v5, v7, v6
	WORD	$0x4e0668e6	// trn2.16b v6, v7, v6
	WORD	$0x4e412a07	// trn1.8h v7, v16, v1
	WORD	$0x4e416a01	// trn2.8h v1, v16, v1
	WORD	$0x4e422811	// trn1.8h v17, v0, v2
	WORD	$0x4e426802	// trn2.8h v2, v0, v2
	WORD	$0x4e452880	// trn1.8h v0, v4, v5
	WORD	$0x4e456884	// trn2.8h v4, v4, v5
	WORD	$0x4e462872	// trn1.8h v18, v3, v6
	WORD	$0x4e466863	// trn2.8h v3, v3, v6
	WORD	$0x4e8028e5	// trn1.4s v5, v7, v0
	WORD	$0x4e8068e0	// trn2.4s v0, v7, v0
	WORD	$0x4e842834	// trn1.4s v20, v1, v4
	WORD	$0x4e846830	// trn2.4s v16, v1, v4
	WORD	$0x4e922a35	// trn1.4s v21, v17, v18
	WORD	$0x4e926a33	// trn2.4s v19, v17, v18
	WORD	$0x4e832847	// trn1.4s v7, v2, v3
	WORD	$0x4e836851	// trn2.4s v17, v2, v3
	WORD	$0x6e0540ba	// ext.16b v26, v5, v5, #0x8
	WORD	$0x6e1542bc	// ext.16b v28, v21, v21, #0x8
	WORD	$0x6e144286	// ext.16b v6, v20, v20, #0x8
	WORD	$0x0d40c041	// ld1r.8b { v1 }, [x2]
	WORD	$0x6e0740f2	// ext.16b v18, v7, v7, #0x8
	WORD	$0x0d40c062	// ld1r.8b { v2 }, [x3]
	WORD	$0x2e317603	// uabd.8b v3, v16, v17
	WORD	$0x2e3a7784	// uabd.8b v4, v28, v26
	WORD	$0x2e246476	// umax.8b v22, v3, v4
	WORD	$0x2e337403	// uabd.8b v3, v0, v19
	WORD	$0x2e2366c3	// umax.8b v3, v22, v3
	WORD	$0x2e307664	// uabd.8b v4, v19, v16
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e3c74c4	// uabd.8b v4, v6, v28
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e267644	// uabd.8b v4, v18, v6
	WORD	$0x2e246463	// umax.8b v3, v3, v4
	WORD	$0x2e3a7624	// uabd.8b v4, v17, v26
	WORD	$0x2e3c7617	// uabd.8b v23, v16, v28
	WORD	$0x2e240c84	// uqadd.8b v4, v4, v4
	WORD	$0x2f0f06f7	// ushr.8b v23, v23, #0x1
	WORD	$0x2e370c84	// uqadd.8b v4, v4, v23
	WORD	$0x2e233c42	// cmhs.8b v2, v2, v3
	WORD	$0x2e243c21	// cmhs.8b v1, v1, v4
	WORD	$0x0e211c58	// and.8b v24, v2, v1
	WORD	$0x2e317661	// uabd.8b v1, v19, v17
	WORD	$0x2e2166c1	// umax.8b v1, v22, v1
	WORD	$0x2e3a74c2	// uabd.8b v2, v6, v26
	WORD	$0x2e226421	// umax.8b v1, v1, v2
	WORD	$0x2e317402	// uabd.8b v2, v0, v17
	WORD	$0x2e226421	// umax.8b v1, v1, v2
	WORD	$0x2e3a7642	// uabd.8b v2, v18, v26
	WORD	$0x2e226421	// umax.8b v1, v1, v2
	WORD	$0x0f00e457	// movi.8b v23, #0x2
	WORD	$0x2e2136e1	// cmhi.8b v1, v23, v1
	WORD	$0x0e211f1f	// and.8b v31, v24, v1
	WORD	$0x2ea02be1	// uaddlp.1d v1, v31
	WORD	$0x1e26002d	// fmov w13, s1
	WORD	$0x310009bf	// cmn w13, #0x2
	WORD	$0x54000400	// b.eq 0x2404 <_vpx_lpf_vertical_16_neon+0x1d0>
	WORD	$0x0d40c081	// ld1r.8b { v1 }, [x4]
	WORD	$0x2e2136d6	// cmhi.8b v22, v22, v1
	WORD	$0x0f04e419	// movi.8b v25, #0x80
	WORD	$0x2e391e1b	// eor.8b v27, v16, v25
	WORD	$0x2e391e21	// eor.8b v1, v17, v25
	WORD	$0x2e391f42	// eor.8b v2, v26, v25
	WORD	$0x2e391f9d	// eor.8b v29, v28, v25
	WORD	$0x0e3d2f63	// sqsub.8b v3, v27, v29
	WORD	$0x0e231ec3	// and.8b v3, v22, v3
	WORD	$0x0e212c44	// sqsub.8b v4, v2, v1
	WORD	$0x0e240c63	// sqadd.8b v3, v3, v4
	WORD	$0x0e240c63	// sqadd.8b v3, v3, v4
	WORD	$0x0e240c63	// sqadd.8b v3, v3, v4
	WORD	$0x0e231f03	// and.8b v3, v24, v3
	WORD	$0x0f00e484	// movi.8b v4, #0x4
	WORD	$0x0e240c64	// sqadd.8b v4, v3, v4
	WORD	$0x0f0d0498	// sshr.8b v24, v4, #0x3
	WORD	$0x0f00e464	// movi.8b v4, #0x3
	WORD	$0x0e240c63	// sqadd.8b v3, v3, v4
	WORD	$0x0f0d0463	// sshr.8b v3, v3, #0x3
	WORD	$0x0e382c42	// sqsub.8b v2, v2, v24
	WORD	$0x0e230c3e	// sqadd.8b v30, v1, v3
	WORD	$0x2e391c43	// eor.8b v3, v2, v25
	WORD	$0x2e391fc2	// eor.8b v2, v30, v25
	WORD	$0x0f0f2718	// srshr.8b v24, v24, #0x1
	WORD	$0x0e761f16	// bic.8b v22, v24, v22
	WORD	$0x0e362fb8	// sqsub.8b v24, v29, v22
	WORD	$0x0e360f76	// sqadd.8b v22, v27, v22
	WORD	$0x2e391f04	// eor.8b v4, v24, v25
	WORD	$0x2e391ec1	// eor.8b v1, v22, v25
	WORD	$0x3400246d	// cbz w13, 0x288c <_vpx_lpf_vertical_16_neon+0x658>
	WORD	$0x6e004016	// ext.16b v22, v0, v0, #0x8
	WORD	$0x6e13427b	// ext.16b v27, v19, v19, #0x8
	WORD	$0x6e10420f	// ext.16b v15, v16, v16, #0x8
	WORD	$0x6e11422e	// ext.16b v14, v17, v17, #0x8
	WORD	$0x2e3174b8	// uabd.8b v24, v5, v17
	WORD	$0x2e3176b9	// uabd.8b v25, v21, v17
	WORD	$0x2e396718	// umax.8b v24, v24, v25
	WORD	$0x2e317699	// uabd.8b v25, v20, v17
	WORD	$0x2e396718	// umax.8b v24, v24, v25
	WORD	$0x2e3174f9	// uabd.8b v25, v7, v17
	WORD	$0x2e396718	// umax.8b v24, v24, v25
	WORD	$0x2e3a76d6	// uabd.8b v22, v22, v26
	WORD	$0x2e366716	// umax.8b v22, v24, v22
	WORD	$0x3d8013fb	// str q27, [sp, #0x30] (sp offset +16)
	WORD	$0x2e3a7778	// uabd.8b v24, v27, v26
	WORD	$0x2e3866d6	// umax.8b v22, v22, v24
	WORD	$0x2e3a75f8	// uabd.8b v24, v15, v26
	WORD	$0x2e3866d6	// umax.8b v22, v22, v24
	WORD	$0x2e3a75d8	// uabd.8b v24, v14, v26
	WORD	$0x2e3866d6	// umax.8b v22, v22, v24
	WORD	$0x2e3636f6	// cmhi.8b v22, v23, v22
	WORD	$0x0e3f1ed7	// and.8b v23, v22, v31
	WORD	$0x2ea02af6	// uaddlp.1d v22, v23
	WORD	$0x1e2602ce	// fmov w14, s22
	WORD	$0x2f08a40b	// ushll.8h v11, v0, #0x0
	WORD	$0x310009df	// cmn w14, #0x2
	WORD	$0x3d801beb	// str q11, [sp, #0x50] (sp offset +16)
	WORD	$0x540001e1	// b.ne 0x24ac <_vpx_lpf_vertical_16_neon+0x278>
	WORD	$0xad013bef	// stp q15, q14, [sp, #0x10] (sp offset +16)
	WORD	$0x2f08a67f	// ushll.8h v31, v19, #0x0
	WORD	$0x2f08a60a	// ushll.8h v10, v16, #0x0
	WORD	$0x2f08a628	// ushll.8h v8, v17, #0x0
	WORD	$0x2f08a75e	// ushll.8h v30, v26, #0x0
	WORD	$0x4eb31e76	// mov.16b v22, v19
	WORD	$0x3d8017f6	// str q22, [sp, #0x40] (sp offset +16)
	WORD	$0x2f08a789	// ushll.8h v9, v28, #0x0
	WORD	$0x4ea21c4c	// mov.16b v12, v2
	WORD	$0x2f08a4dd	// ushll.8h v29, v6, #0x0
	WORD	$0x4ea31c7b	// mov.16b v27, v3
	WORD	$0x4ea41c8d	// mov.16b v13, v4
	WORD	$0x2f08a65c	// ushll.8h v28, v18, #0x0
	WORD	$0x14000036	// b 0x2580 <_vpx_lpf_vertical_16_neon+0x34c>
	WORD	$0x0e212976	// xtn.8b v22, v11
	WORD	$0x0f00e478	// movi.8b v24, #0x3
	WORD	$0x2f09a679	// ushll.8h v25, v19, #0x1
	WORD	$0x2f08a60a	// ushll.8h v10, v16, #0x0
	WORD	$0x2f08a628	// ushll.8h v8, v17, #0x0
	WORD	$0x2f08a75e	// ushll.8h v30, v26, #0x0
	WORD	$0x2e30023d	// uaddl.8h v29, v17, v16
	WORD	$0x2e3a13bd	// uaddw.8h v29, v29, v26
	WORD	$0x2e3882dd	// umlal.8h v29, v22, v24
	WORD	$0x4e7987b6	// add.8h v22, v29, v25
	WORD	$0x0f0d8ed8	// rshrn.8b v24, v22, #0x3
	WORD	$0x2f08a789	// ushll.8h v9, v28, #0x0
	WORD	$0x2e200279	// uaddl.8h v25, v19, v0
	WORD	$0x6e798559	// sub.8h v25, v10, v25
	WORD	$0x2e3c1339	// uaddw.8h v25, v25, v28
	WORD	$0x4e768736	// add.8h v22, v25, v22
	WORD	$0x0f0d8ed9	// rshrn.8b v25, v22, #0x3
	WORD	$0x2f08a4dd	// ushll.8h v29, v6, #0x0
	WORD	$0x2e20020b	// uaddl.8h v11, v16, v0
	WORD	$0x6e6b850b	// sub.8h v11, v8, v11
	WORD	$0x2e26116b	// uaddw.8h v11, v11, v6
	WORD	$0x4e768576	// add.8h v22, v11, v22
	WORD	$0x0f0d8ecb	// rshrn.8b v11, v22, #0x3
	WORD	$0x2e20022c	// uaddl.8h v12, v17, v0
	WORD	$0x6e6c87cc	// sub.8h v12, v30, v12
	WORD	$0x2e32118c	// uaddw.8h v12, v12, v18
	WORD	$0x4e768596	// add.8h v22, v12, v22
	WORD	$0x0f0d8ecd	// rshrn.8b v13, v22, #0x3
	WORD	$0x2e33035a	// uaddl.8h v26, v26, v19
	WORD	$0x6e7a853a	// sub.8h v26, v9, v26
	WORD	$0x2e32135a	// uaddw.8h v26, v26, v18
	WORD	$0x4e768756	// add.8h v22, v26, v22
	WORD	$0x0f0d8eda	// rshrn.8b v26, v22, #0x3
	WORD	$0x2e30039c	// uaddl.8h v28, v28, v16
	WORD	$0x6e7c87bc	// sub.8h v28, v29, v28
	WORD	$0x2e32139c	// uaddw.8h v28, v28, v18
	WORD	$0x4e768796	// add.8h v22, v28, v22
	WORD	$0x0f0d8ed6	// rshrn.8b v22, v22, #0x3
	WORD	$0x2eff1e78	// bif.8b v24, v19, v31
	WORD	$0x3d8017f8	// str q24, [sp, #0x40] (sp offset +16)
	WORD	$0x2ebf1f21	// bit.8b v1, v25, v31
	WORD	$0x0ebf1fec	// mov.8b v12, v31
	WORD	$0x2e621d6c	// bsl.8b v12, v11, v2
	WORD	$0x0ebf1ffb	// mov.8b v27, v31
	WORD	$0x2e631dbb	// bsl.8b v27, v13, v3
	WORD	$0x0ebf1fed	// mov.8b v13, v31
	WORD	$0x2e641f4d	// bsl.8b v13, v26, v4
	WORD	$0x2ebf1ec6	// bit.8b v6, v22, v31
	WORD	$0x340013ee	// cbz w14, 0x27e8 <_vpx_lpf_vertical_16_neon+0x5b4>
	WORD	$0xad013bef	// stp q15, q14, [sp, #0x10] (sp offset +16)
	WORD	$0x2f08a67f	// ushll.8h v31, v19, #0x0
	WORD	$0x2f08a65c	// ushll.8h v28, v18, #0x0
	WORD	$0x3dc01beb	// ldr q11, [sp, #0x50] (sp offset +16)
	WORD	$0x0f00e4f6	// movi.8b v22, #0x7
	WORD	$0x2f09a6b8	// ushll.8h v24, v21, #0x1
	WORD	$0x2f08a699	// ushll.8h v25, v20, #0x0
	WORD	$0x2f08a4fa	// ushll.8h v26, v7, #0x0
	WORD	$0x2e3400ee	// uaddl.8h v14, v7, v20
	WORD	$0x4e6b85ce	// add.8h v14, v14, v11
	WORD	$0x2e3680ae	// umlal.8h v14, v5, v22
	WORD	$0x4e7e8716	// add.8h v22, v24, v30
	WORD	$0x4e6886d6	// add.8h v22, v22, v8
	WORD	$0x4e7685d6	// add.8h v22, v14, v22
	WORD	$0x4e7f8558	// add.8h v24, v10, v31
	WORD	$0x4e7886cf	// add.8h v15, v22, v24
	WORD	$0x2e2502b6	// uaddl.8h v22, v21, v5
	WORD	$0x6e768736	// sub.8h v22, v25, v22
	WORD	$0x4e6986d6	// add.8h v22, v22, v9
	WORD	$0x4e6f86d6	// add.8h v22, v22, v15
	WORD	$0x2e250298	// uaddl.8h v24, v20, v5
	WORD	$0x6e788758	// sub.8h v24, v26, v24
	WORD	$0x4e7d8718	// add.8h v24, v24, v29
	WORD	$0x4e768718	// add.8h v24, v24, v22
	WORD	$0x2e2500f9	// uaddl.8h v25, v7, v5
	WORD	$0x6e798579	// sub.8h v25, v11, v25
	WORD	$0x3d8007fc	// str q28, [sp] (sp offset +16)
	WORD	$0x4e7c8739	// add.8h v25, v25, v28
	WORD	$0x4e788739	// add.8h v25, v25, v24
	WORD	$0x6f08a41a	// ushll2.8h v26, v0, #0x0
	WORD	$0x2e25000e	// uaddl.8h v14, v0, v5
	WORD	$0x6e6e875a	// sub.8h v26, v26, v14
	WORD	$0x4e7f875a	// add.8h v26, v26, v31
	WORD	$0x4e79875a	// add.8h v26, v26, v25
	WORD	$0x6e33114e	// uaddw2.8h v14, v10, v19
	WORD	$0x2e2513fc	// uaddw.8h v28, v31, v5
	WORD	$0x6e7c85dc	// sub.8h v28, v14, v28
	WORD	$0x4e7a879c	// add.8h v28, v28, v26
	WORD	$0x0f0c8f8b	// rshrn.8b v11, v28, #0x4
	WORD	$0x2eb71d61	// bit.8b v1, v11, v23
	WORD	$0x6e30110b	// uaddw2.8h v11, v8, v16
	WORD	$0x2e25114a	// uaddw.8h v10, v10, v5
	WORD	$0x6e6a856a	// sub.8h v10, v11, v10
	WORD	$0x4e7c855c	// add.8h v28, v10, v28
	WORD	$0x0f0c8f8a	// rshrn.8b v10, v28, #0x4
	WORD	$0x0eb71ee2	// mov.8b v2, v23
	WORD	$0x2e6c1d42	// bsl.8b v2, v10, v12
	WORD	$0x6e3113ca	// uaddw2.8h v10, v30, v17
	WORD	$0x2e251108	// uaddw.8h v8, v8, v5
	WORD	$0x6e688548	// sub.8h v8, v10, v8
	WORD	$0x4e7c851c	// add.8h v28, v8, v28
	WORD	$0x0f0c8f88	// rshrn.8b v8, v28, #0x4
	WORD	$0x0eb71ee3	// mov.8b v3, v23
	WORD	$0x2e7b1d03	// bsl.8b v3, v8, v27
	WORD	$0x6e311128	// uaddw2.8h v8, v9, v17
	WORD	$0x2e3513de	// uaddw.8h v30, v30, v21
	WORD	$0x6e7e851e	// sub.8h v30, v8, v30
	WORD	$0x4e7c87de	// add.8h v30, v30, v28
	WORD	$0x0f0c8fdc	// rshrn.8b v28, v30, #0x4
	WORD	$0x0eb71ee4	// mov.8b v4, v23
	WORD	$0x2e6d1f84	// bsl.8b v4, v28, v13
	WORD	$0x3400114d	// cbz w13, 0x288c <_vpx_lpf_vertical_16_neon+0x658>
	WORD	$0x0f0c8f5a	// rshrn.8b v26, v26, #0x4
	WORD	$0x3dc017e8	// ldr q8, [sp, #0x40] (sp offset +16)
	WORD	$0x2eb71f48	// bit.8b v8, v26, v23
	WORD	$0x6e3113ba	// uaddw2.8h v26, v29, v17
	WORD	$0x2e34113c	// uaddw.8h v28, v9, v20
	WORD	$0x6e7c875a	// sub.8h v26, v26, v28
	WORD	$0x4e7e875a	// add.8h v26, v26, v30
	WORD	$0x0f0c8f5c	// rshrn.8b v28, v26, #0x4
	WORD	$0x2eb71f86	// bit.8b v6, v28, v23
	WORD	$0x34000a6e	// cbz w14, 0x27d8 <_vpx_lpf_vertical_16_neon+0x5a4>
	WORD	$0x0f0c8df2	// rshrn.8b v18, v15, #0x4
	WORD	$0x2ef71eb2	// bif.8b v18, v21, v23
	WORD	$0x0f0c8ed5	// rshrn.8b v21, v22, #0x4
	WORD	$0x2eb71eb4	// bit.8b v20, v21, v23
	WORD	$0x0f0c8f15	// rshrn.8b v21, v24, #0x4
	WORD	$0x0f0c8f36	// rshrn.8b v22, v25, #0x4
	WORD	$0x3dc007fc	// ldr q28, [sp] (sp offset +16)
	WORD	$0x6e311398	// uaddw2.8h v24, v28, v17
	WORD	$0x2e2713b9	// uaddw.8h v25, v29, v7
	WORD	$0x6e798718	// sub.8h v24, v24, v25
	WORD	$0x4e7a8718	// add.8h v24, v24, v26
	WORD	$0x0f0c8f19	// rshrn.8b v25, v24, #0x4
	WORD	$0x6e31001a	// uaddl2.8h v26, v0, v17
	WORD	$0x3dc01bfb	// ldr q27, [sp, #0x50] (sp offset +16)
	WORD	$0x4e7b879b	// add.8h v27, v28, v27
	WORD	$0x6e7b875a	// sub.8h v26, v26, v27
	WORD	$0x4e788758	// add.8h v24, v26, v24
	WORD	$0x0f0c8f1a	// rshrn.8b v26, v24, #0x4
	WORD	$0x6e310273	// uaddl2.8h v19, v19, v17
	WORD	$0x6e2013fb	// uaddw2.8h v27, v31, v0
	WORD	$0x6e7b8673	// sub.8h v19, v19, v27
	WORD	$0x4e788673	// add.8h v19, v19, v24
	WORD	$0x0f0c8e78	// rshrn.8b v24, v19, #0x4
	WORD	$0x3dc013fb	// ldr q27, [sp, #0x30] (sp offset +16)
	WORD	$0x2ef71f78	// bif.8b v24, v27, v23
	WORD	$0x6e310210	// uaddl2.8h v16, v16, v17
	WORD	$0x6e6e8610	// sub.8h v16, v16, v14
	WORD	$0x4e738610	// add.8h v16, v16, v19
	WORD	$0x0f0c8e10	// rshrn.8b v16, v16, #0x4
	WORD	$0x3dc00bf1	// ldr q17, [sp, #0x10] (sp offset +16)
	WORD	$0x2ef71e30	// bif.8b v16, v17, v23
	WORD	$0x6e180465	// mov.d v5[1], v3[0]
	WORD	$0x6e180492	// mov.d v18[1], v4[0]
	WORD	$0x6e1804d4	// mov.d v20[1], v6[0]
	WORD	$0x6e1806f7	// mov.d v23[1], v23[0]
	WORD	$0x6e180735	// mov.d v21[1], v25[0]
	WORD	$0x4eb71ee6	// mov.16b v6, v23
	WORD	$0x6e671ea6	// bsl.16b v6, v21, v7
	WORD	$0x6e180756	// mov.d v22[1], v26[0]
	WORD	$0x6eb71ec0	// bit.16b v0, v22, v23
	WORD	$0x6e180708	// mov.d v8[1], v24[0]
	WORD	$0x6e180601	// mov.d v1[1], v16[0]
	WORD	$0x3dc00fe7	// ldr q7, [sp, #0x20] (sp offset +16)
	WORD	$0x6e1804e2	// mov.d v2[1], v7[0]
	WORD	$0x4e1228a7	// trn1.16b v7, v5, v18
	WORD	$0x4e1268a5	// trn2.16b v5, v5, v18
	WORD	$0x4e062a90	// trn1.16b v16, v20, v6
	WORD	$0x4e066a86	// trn2.16b v6, v20, v6
	WORD	$0x4e082811	// trn1.16b v17, v0, v8
	WORD	$0x4e086800	// trn2.16b v0, v0, v8
	WORD	$0x4e022832	// trn1.16b v18, v1, v2
	WORD	$0x4e026821	// trn2.16b v1, v1, v2
	WORD	$0x4e5028e2	// trn1.8h v2, v7, v16
	WORD	$0x4e5068e3	// trn2.8h v3, v7, v16
	WORD	$0x4e4628a4	// trn1.8h v4, v5, v6
	WORD	$0x4e4668a5	// trn2.8h v5, v5, v6
	WORD	$0x4e522a26	// trn1.8h v6, v17, v18
	WORD	$0x4e526a27	// trn2.8h v7, v17, v18
	WORD	$0x4e412810	// trn1.8h v16, v0, v1
	WORD	$0x4e862851	// trn1.4s v17, v2, v6
	WORD	$0x4e902892	// trn1.4s v18, v4, v16
	WORD	$0x3d800151	// str q17, [x10]
	WORD	$0x3ca86952	// str q18, [x10, x8]
	WORD	$0x4e416800	// trn2.8h v0, v0, v1
	WORD	$0x4e866841	// trn2.4s v1, v2, v6
	WORD	$0x4e872862	// trn1.4s v2, v3, v7
	WORD	$0x4e876863	// trn2.4s v3, v3, v7
	WORD	$0x4e906884	// trn2.4s v4, v4, v16
	WORD	$0x4e8028a6	// trn1.4s v6, v5, v0
	WORD	$0x3d800122	// str q2, [x9]
	WORD	$0x3ca86926	// str q6, [x9, x8]
	WORD	$0x3d800161	// str q1, [x11]
	WORD	$0x3ca86964	// str q4, [x11, x8]
	WORD	$0x4e8068a0	// trn2.4s v0, v5, v0
	WORD	$0x3d800183	// str q3, [x12]
	WORD	$0x3ca86980	// str q0, [x12, x8]
	WORD	$0x6d4a23e9	// ldp d9, d8, [sp, #0x90] (sp offset +16)
	WORD	$0x6d492beb	// ldp d11, d10, [sp, #0x80] (sp offset +16)
	WORD	$0x6d4833ed	// ldp d13, d12, [sp, #0x70] (sp offset +16)
	WORD	$0x6d473bef	// ldp d15, d14, [sp, #0x60] (sp offset +16)
	WORD	$0xd503201f	// nop (was: add sp, sp, #0xa0 — hosted in Go frame)
	WORD	$0x1400003d	// b end (was ret)
	WORD	$0x4ea21c4c	// mov.16b v12, v2
	WORD	$0x4ea31c7b	// mov.16b v27, v3
	WORD	$0x4ea41c8d	// mov.16b v13, v4
	WORD	$0x14000003	// b 0x27f0 <_vpx_lpf_vertical_16_neon+0x5bc>
	WORD	$0x340004cd	// cbz w13, 0x2880 <_vpx_lpf_vertical_16_neon+0x64c>
	WORD	$0x3dc017e8	// ldr q8, [sp, #0x40] (sp offset +16)
	WORD	$0x4e083800	// zip1.16b v0, v0, v8
	WORD	$0x4e0c3821	// zip1.16b v1, v1, v12
	WORD	$0x4e0d3b62	// zip1.16b v2, v27, v13
	WORD	$0x4e1238c3	// zip1.16b v3, v6, v18
	WORD	$0x4e413804	// zip1.8h v4, v0, v1
	WORD	$0x4e417800	// zip2.8h v0, v0, v1
	WORD	$0x4e433841	// zip1.8h v1, v2, v3
	WORD	$0x4e437842	// zip2.8h v2, v2, v3
	WORD	$0x4e813883	// zip1.4s v3, v4, v1
	WORD	$0x4e817881	// zip2.4s v1, v4, v1
	WORD	$0x4e823804	// zip1.4s v4, v0, v2
	WORD	$0x6e034065	// ext.16b v5, v3, v3, #0x8
	WORD	$0x6e014026	// ext.16b v6, v1, v1, #0x8
	WORD	$0x6e044087	// ext.16b v7, v4, v4, #0x8
	WORD	$0xfc1fcc03	// str d3, [x0, #-0x4]!
	WORD	$0x8b080009	// add x9, x0, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd000125	// str d5, [x9]
	WORD	$0xfd000141	// str d1, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd000126	// str d6, [x9]
	WORD	$0xfd000144	// str d4, [x10]
	WORD	$0x8b080149	// add x9, x10, x8
	WORD	$0x8b08012a	// add x10, x9, x8
	WORD	$0xfd000127	// str d7, [x9]
	WORD	$0x4e827800	// zip2.4s v0, v0, v2
	WORD	$0x6e004001	// ext.16b v1, v0, v0, #0x8
	WORD	$0xfd000140	// str d0, [x10]
	WORD	$0xfc286941	// str d1, [x10, x8]
	WORD	$0x6d4a23e9	// ldp d9, d8, [sp, #0x90] (sp offset +16)
	WORD	$0x6d492beb	// ldp d11, d10, [sp, #0x80] (sp offset +16)
	WORD	$0x6d4833ed	// ldp d13, d12, [sp, #0x70] (sp offset +16)
	WORD	$0x6d473bef	// ldp d15, d14, [sp, #0x60] (sp offset +16)
	WORD	$0xd503201f	// nop (was: add sp, sp, #0xa0 — hosted in Go frame)
	WORD	$0x14000013	// b end (was ret)
	WORD	$0x4ead1da4	// mov.16b v4, v13
	WORD	$0x4ebb1f63	// mov.16b v3, v27
	WORD	$0x4eac1d82	// mov.16b v2, v12
	WORD	$0xd1000809	// sub x9, x0, #0x2
	WORD	$0x0da82121	// st4.b { v1, v2, v3, v4 }[0], [x9], x8
	WORD	$0x0da82521	// st4.b { v1, v2, v3, v4 }[1], [x9], x8
	WORD	$0x0da82921	// st4.b { v1, v2, v3, v4 }[2], [x9], x8
	WORD	$0x0da82d21	// st4.b { v1, v2, v3, v4 }[3], [x9], x8
	WORD	$0x0da83121	// st4.b { v1, v2, v3, v4 }[4], [x9], x8
	WORD	$0x0da83521	// st4.b { v1, v2, v3, v4 }[5], [x9], x8
	WORD	$0x0da83921	// st4.b { v1, v2, v3, v4 }[6], [x9], x8
	WORD	$0x0d203d21	// st4.b { v1, v2, v3, v4 }[7], [x9]
	WORD	$0x6d4a23e9	// ldp d9, d8, [sp, #0x90] (sp offset +16)
	WORD	$0x6d492beb	// ldp d11, d10, [sp, #0x80] (sp offset +16)
	WORD	$0x6d4833ed	// ldp d13, d12, [sp, #0x70] (sp offset +16)
	WORD	$0x6d473bef	// ldp d15, d14, [sp, #0x60] (sp offset +16)
	WORD	$0xd503201f	// nop (was: add sp, sp, #0xa0 — hosted in Go frame)
	WORD	$0x14000001	// b end (was ret)
	RET

