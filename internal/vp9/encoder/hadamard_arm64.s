//go:build arm64 && !purego

// ARMv8 NEON ports of the libvpx v1.16.0 block_yrd scoring primitives:
//
//   hadamard8x8NEON       vpx_dsp/arm/hadamard_neon.c::vpx_hadamard_8x8_neon
//   hadamardCombine16NEON vpx_dsp/arm/hadamard_neon.c::vpx_hadamard_16x16_neon
//                         (the cross-slab halving butterfly after the four
//                         8x8 Hadamards)
//   satdNEON              vpx_dsp/arm/avg_neon.c::vpx_satd_neon
//
// Deviation from upstream, documented: libvpx's NEON hadamard skips the
// second transpose ("the order of the output coeff of the hadamard is not
// important" -- vpx_dsp/avg.c:229) so its NEON output is a permutation of
// the C output. govpx requires SIMD kernels to match the scalar reference
// bit-exactly, so this port performs the second transpose and emits the
// exact vpx_hadamard_8x8_c coefficient order. All arithmetic is add/sub on
// 16-bit lanes; wrapping per-op is congruent (mod 2^16) with the scalar
// port's widen-then-truncate, so results are identical for all int16 inputs.
//
// satdNEON uses sabd-against-zero + uadalp instead of libvpx's vabsq +
// vpadalq so that the |INT16_MIN| = 32768 case stays exact (vabsq wraps
// -32768 to itself, while sabd emits the unsigned magnitude 0x8000 that
// the unsigned pairwise accumulate reads back as 32768); for all other
// inputs the arithmetic is identical.

#include "textflag.h"

// hadamard8x8NEON ABI ($0-24):
//   src+0(FP)    *int16
//   stride+8(FP) int (in int16 elements, >= 8)
//   coeff+16(FP) *int16 (64 contiguous outputs)
TEXT ·hadamard8x8NEON(SB), NOSPLIT, $0-24
	MOVD	src+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	coeff+16(FP), R2
	LSL	$1, R1, R1  // byte stride

	// Load the 8 rows.
	WORD	$0x3dc00000 // ldr q0, [x0]
	ADD	R1, R0
	WORD	$0x3dc00001 // ldr q1, [x0]
	ADD	R1, R0
	WORD	$0x3dc00002 // ldr q2, [x0]
	ADD	R1, R0
	WORD	$0x3dc00003 // ldr q3, [x0]
	ADD	R1, R0
	WORD	$0x3dc00004 // ldr q4, [x0]
	ADD	R1, R0
	WORD	$0x3dc00005 // ldr q5, [x0]
	ADD	R1, R0
	WORD	$0x3dc00006 // ldr q6, [x0]
	ADD	R1, R0
	WORD	$0x3dc00007 // ldr q7, [x0]

	// First hadamard8x8_one_pass (per-column butterflies).
	WORD	$0x4e618410 // add v16.8h, v0.8h, v1.8h   (b0)
	WORD	$0x6e618411 // sub v17.8h, v0.8h, v1.8h   (b1)
	WORD	$0x4e638452 // add v18.8h, v2.8h, v3.8h   (b2)
	WORD	$0x6e638453 // sub v19.8h, v2.8h, v3.8h   (b3)
	WORD	$0x4e658494 // add v20.8h, v4.8h, v5.8h   (b4)
	WORD	$0x6e658495 // sub v21.8h, v4.8h, v5.8h   (b5)
	WORD	$0x4e6784d6 // add v22.8h, v6.8h, v7.8h   (b6)
	WORD	$0x6e6784d7 // sub v23.8h, v6.8h, v7.8h   (b7)
	WORD	$0x4e728618 // add v24.8h, v16.8h, v18.8h (c0)
	WORD	$0x4e738639 // add v25.8h, v17.8h, v19.8h (c1)
	WORD	$0x6e72861a // sub v26.8h, v16.8h, v18.8h (c2)
	WORD	$0x6e73863b // sub v27.8h, v17.8h, v19.8h (c3)
	WORD	$0x4e76869c // add v28.8h, v20.8h, v22.8h (c4)
	WORD	$0x4e7786bd // add v29.8h, v21.8h, v23.8h (c5)
	WORD	$0x6e76869e // sub v30.8h, v20.8h, v22.8h (c6)
	WORD	$0x6e7786bf // sub v31.8h, v21.8h, v23.8h (c7)
	WORD	$0x4e7c8700 // add v0.8h, v24.8h, v28.8h  (a0 = c0+c4)
	WORD	$0x6e7e8741 // sub v1.8h, v26.8h, v30.8h  (a1 = c2-c6)
	WORD	$0x6e7c8702 // sub v2.8h, v24.8h, v28.8h  (a2 = c0-c4)
	WORD	$0x4e7e8743 // add v3.8h, v26.8h, v30.8h  (a3 = c2+c6)
	WORD	$0x4e7f8764 // add v4.8h, v27.8h, v31.8h  (a4 = c3+c7)
	WORD	$0x6e7f8765 // sub v5.8h, v27.8h, v31.8h  (a5 = c3-c7)
	WORD	$0x6e7d8726 // sub v6.8h, v25.8h, v29.8h  (a6 = c1-c5)
	WORD	$0x4e7d8727 // add v7.8h, v25.8h, v29.8h  (a7 = c1+c5)

	// transpose_s16_8x8 (vpx_dsp/arm/transpose_neon.h).
	WORD	$0x4e412810 // trn1 v16.8h, v0.8h, v1.8h
	WORD	$0x4e416811 // trn2 v17.8h, v0.8h, v1.8h
	WORD	$0x4e432852 // trn1 v18.8h, v2.8h, v3.8h
	WORD	$0x4e436853 // trn2 v19.8h, v2.8h, v3.8h
	WORD	$0x4e452894 // trn1 v20.8h, v4.8h, v5.8h
	WORD	$0x4e456895 // trn2 v21.8h, v4.8h, v5.8h
	WORD	$0x4e4728d6 // trn1 v22.8h, v6.8h, v7.8h
	WORD	$0x4e4768d7 // trn2 v23.8h, v6.8h, v7.8h
	WORD	$0x4e922a18 // trn1 v24.4s, v16.4s, v18.4s
	WORD	$0x4e926a19 // trn2 v25.4s, v16.4s, v18.4s
	WORD	$0x4e932a3a // trn1 v26.4s, v17.4s, v19.4s
	WORD	$0x4e936a3b // trn2 v27.4s, v17.4s, v19.4s
	WORD	$0x4e962a9c // trn1 v28.4s, v20.4s, v22.4s
	WORD	$0x4e966a9d // trn2 v29.4s, v20.4s, v22.4s
	WORD	$0x4e972abe // trn1 v30.4s, v21.4s, v23.4s
	WORD	$0x4e976abf // trn2 v31.4s, v21.4s, v23.4s
	WORD	$0x4edc2b00 // trn1 v0.2d, v24.2d, v28.2d
	WORD	$0x4ede2b41 // trn1 v1.2d, v26.2d, v30.2d
	WORD	$0x4edd2b22 // trn1 v2.2d, v25.2d, v29.2d
	WORD	$0x4edf2b63 // trn1 v3.2d, v27.2d, v31.2d
	WORD	$0x4edc6b04 // trn2 v4.2d, v24.2d, v28.2d
	WORD	$0x4ede6b45 // trn2 v5.2d, v26.2d, v30.2d
	WORD	$0x4edd6b26 // trn2 v6.2d, v25.2d, v29.2d
	WORD	$0x4edf6b67 // trn2 v7.2d, v27.2d, v31.2d

	// Second hadamard8x8_one_pass (per-row butterflies).
	WORD	$0x4e618410 // add v16.8h, v0.8h, v1.8h   (b0)
	WORD	$0x6e618411 // sub v17.8h, v0.8h, v1.8h   (b1)
	WORD	$0x4e638452 // add v18.8h, v2.8h, v3.8h   (b2)
	WORD	$0x6e638453 // sub v19.8h, v2.8h, v3.8h   (b3)
	WORD	$0x4e658494 // add v20.8h, v4.8h, v5.8h   (b4)
	WORD	$0x6e658495 // sub v21.8h, v4.8h, v5.8h   (b5)
	WORD	$0x4e6784d6 // add v22.8h, v6.8h, v7.8h   (b6)
	WORD	$0x6e6784d7 // sub v23.8h, v6.8h, v7.8h   (b7)
	WORD	$0x4e728618 // add v24.8h, v16.8h, v18.8h (c0)
	WORD	$0x4e738639 // add v25.8h, v17.8h, v19.8h (c1)
	WORD	$0x6e72861a // sub v26.8h, v16.8h, v18.8h (c2)
	WORD	$0x6e73863b // sub v27.8h, v17.8h, v19.8h (c3)
	WORD	$0x4e76869c // add v28.8h, v20.8h, v22.8h (c4)
	WORD	$0x4e7786bd // add v29.8h, v21.8h, v23.8h (c5)
	WORD	$0x6e76869e // sub v30.8h, v20.8h, v22.8h (c6)
	WORD	$0x6e7786bf // sub v31.8h, v21.8h, v23.8h (c7)
	WORD	$0x4e7c8700 // add v0.8h, v24.8h, v28.8h  (a0)
	WORD	$0x6e7e8741 // sub v1.8h, v26.8h, v30.8h  (a1)
	WORD	$0x6e7c8702 // sub v2.8h, v24.8h, v28.8h  (a2)
	WORD	$0x4e7e8743 // add v3.8h, v26.8h, v30.8h  (a3)
	WORD	$0x4e7f8764 // add v4.8h, v27.8h, v31.8h  (a4)
	WORD	$0x6e7f8765 // sub v5.8h, v27.8h, v31.8h  (a5)
	WORD	$0x6e7d8726 // sub v6.8h, v25.8h, v29.8h  (a6)
	WORD	$0x4e7d8727 // add v7.8h, v25.8h, v29.8h  (a7)

	// Second transpose: emit the exact scalar (vpx_hadamard_8x8_c) order.
	WORD	$0x4e412810 // trn1 v16.8h, v0.8h, v1.8h
	WORD	$0x4e416811 // trn2 v17.8h, v0.8h, v1.8h
	WORD	$0x4e432852 // trn1 v18.8h, v2.8h, v3.8h
	WORD	$0x4e436853 // trn2 v19.8h, v2.8h, v3.8h
	WORD	$0x4e452894 // trn1 v20.8h, v4.8h, v5.8h
	WORD	$0x4e456895 // trn2 v21.8h, v4.8h, v5.8h
	WORD	$0x4e4728d6 // trn1 v22.8h, v6.8h, v7.8h
	WORD	$0x4e4768d7 // trn2 v23.8h, v6.8h, v7.8h
	WORD	$0x4e922a18 // trn1 v24.4s, v16.4s, v18.4s
	WORD	$0x4e926a19 // trn2 v25.4s, v16.4s, v18.4s
	WORD	$0x4e932a3a // trn1 v26.4s, v17.4s, v19.4s
	WORD	$0x4e936a3b // trn2 v27.4s, v17.4s, v19.4s
	WORD	$0x4e962a9c // trn1 v28.4s, v20.4s, v22.4s
	WORD	$0x4e966a9d // trn2 v29.4s, v20.4s, v22.4s
	WORD	$0x4e972abe // trn1 v30.4s, v21.4s, v23.4s
	WORD	$0x4e976abf // trn2 v31.4s, v21.4s, v23.4s
	WORD	$0x4edc2b00 // trn1 v0.2d, v24.2d, v28.2d
	WORD	$0x4ede2b41 // trn1 v1.2d, v26.2d, v30.2d
	WORD	$0x4edd2b22 // trn1 v2.2d, v25.2d, v29.2d
	WORD	$0x4edf2b63 // trn1 v3.2d, v27.2d, v31.2d
	WORD	$0x4edc6b04 // trn2 v4.2d, v24.2d, v28.2d
	WORD	$0x4ede6b45 // trn2 v5.2d, v26.2d, v30.2d
	WORD	$0x4edd6b26 // trn2 v6.2d, v25.2d, v29.2d
	WORD	$0x4edf6b67 // trn2 v7.2d, v27.2d, v31.2d

	// Store the 64 coefficients.
	WORD	$0x3d800040 // str q0, [x2]
	WORD	$0x3d800441 // str q1, [x2, #16]
	WORD	$0x3d800842 // str q2, [x2, #32]
	WORD	$0x3d800c43 // str q3, [x2, #48]
	WORD	$0x3d801044 // str q4, [x2, #64]
	WORD	$0x3d801445 // str q5, [x2, #80]
	WORD	$0x3d801846 // str q6, [x2, #96]
	WORD	$0x3d801c47 // str q7, [x2, #112]
	RET

// hadamardCombine16NEON ABI ($0-8):
//   coeff+0(FP) *int16 (256 entries: four 8x8 Hadamard slabs)
//
// libvpx vpx_hadamard_16x16_neon second stage: for each of the 8 lanes x 8
// rows, a halving butterfly across the four 64-coefficient slabs.
TEXT ·hadamardCombine16NEON(SB), NOSPLIT, $0-8
	MOVD	coeff+0(FP), R0
	ADD	$128, R0, R1 // coeff + 64  (int16s)
	ADD	$256, R0, R2 // coeff + 128
	ADD	$384, R0, R3 // coeff + 192
	MOVD	$8, R4

combine_loop:
	WORD	$0x3dc00000 // ldr q0, [x0]  (a0)
	WORD	$0x3dc00021 // ldr q1, [x1]  (a1)
	WORD	$0x3dc00042 // ldr q2, [x2]  (a2)
	WORD	$0x3dc00063 // ldr q3, [x3]  (a3)
	WORD	$0x4e610404 // shadd v4.8h, v0.8h, v1.8h  (b0 = (a0+a1)>>1)
	WORD	$0x4e612405 // shsub v5.8h, v0.8h, v1.8h  (b1 = (a0-a1)>>1)
	WORD	$0x4e630446 // shadd v6.8h, v2.8h, v3.8h  (b2 = (a2+a3)>>1)
	WORD	$0x4e632447 // shsub v7.8h, v2.8h, v3.8h  (b3 = (a2-a3)>>1)
	WORD	$0x4e668490 // add v16.8h, v4.8h, v6.8h   (b0+b2)
	WORD	$0x4e6784b1 // add v17.8h, v5.8h, v7.8h   (b1+b3)
	WORD	$0x6e668492 // sub v18.8h, v4.8h, v6.8h   (b0-b2)
	WORD	$0x6e6784b3 // sub v19.8h, v5.8h, v7.8h   (b1-b3)
	WORD	$0x3c810410 // str q16, [x0], #16
	WORD	$0x3c810431 // str q17, [x1], #16
	WORD	$0x3c810452 // str q18, [x2], #16
	WORD	$0x3c810473 // str q19, [x3], #16
	SUB	$1, R4
	CBNZ	R4, combine_loop
	RET

// satdNEON ABI ($0-24):
//   coeff+0(FP) *int16
//   n+8(FP)     int, positive multiple of 16
//   ret+16(FP)  int64 (sum of |coeff[i]|, always >= 0)
TEXT ·satdNEON(SB), NOSPLIT, $0-24
	MOVD	coeff+0(FP), R0
	MOVD	n+8(FP), R1

	// movi zeroing (not eor v,v,v: eor adds a false input dependency
	// on the previous register contents).
	WORD	$0x6f00e400 // movi v0.2d, #0 (acc lo)
	WORD	$0x6f00e401 // movi v1.2d, #0 (acc hi)
	WORD	$0x6f00e404 // movi v4.2d, #0 (zero)

satd_loop:
	WORD	$0x3cc10402 // ldr q2, [x0], #16
	WORD	$0x3cc10403 // ldr q3, [x0], #16
	WORD	$0x4e647442 // sabd v2.8h, v2.8h, v4.8h   (|x|, exact for -32768)
	WORD	$0x4e647463 // sabd v3.8h, v3.8h, v4.8h
	WORD	$0x6e606840 // uadalp v0.4s, v2.8h
	WORD	$0x6e606861 // uadalp v1.4s, v3.8h
	SUB	$16, R1
	CBNZ	R1, satd_loop

	WORD	$0x4ea18400 // add v0.4s, v0.4s, v1.4s
	WORD	$0x4eb1b800 // addv s0, v0.4s
	WORD	$0x1e260000 // fmov w0, s0
	MOVD	R0, ret+16(FP)
	RET
