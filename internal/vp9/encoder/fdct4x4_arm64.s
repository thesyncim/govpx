//go:build arm64 && !purego

// ARMv8 NEON port of libvpx v1.16.0 vpx_dsp/arm/fdct4x4_neon.c
// vpx_fdct4x4_neon. Instruction WORDs are generated from the local
// libvpx oracle object, with the input[0] != 0 lane-zero increment
// rewritten to avoid clang's literal-pool load.

#include "textflag.h"

// forwardDCT4x4NEON ABI ($0-24):
//   input+0(FP)   *int16
//   output+8(FP)  *int16
//   stride+16(FP) int (in int16 elements)
TEXT ·forwardDCT4x4NEON(SB), NOSPLIT, $0-24
	MOVD	input+0(FP), R0
	MOVD	output+8(FP), R1
	MOVD	stride+16(FP), R2
	WORD	$0xfd400001	// ldr d1, [x0]
	WORD	$0x937f7c48	// sbfiz x8, x2, #1, #32
	WORD	$0xfc686802	// ldr d2, [x0, x8]
	WORD	$0x0f145423	// shl v3.4h, v1.4h, #4
	WORD	$0x0f145442	// shl v2.4h, v2.4h, #4
	WORD	$0x937e7c48	// sbfiz x8, x2, #2, #32
	WORD	$0xfc686804	// ldr d4, [x0, x8]
	WORD	$0x528000c8	// mov w8, #6
	WORD	$0x9b287c48	// smull x8, w2, w8
	WORD	$0xfc686805	// ldr d5, [x0, x8]

	// vpx_fdct4x4_neon adds one to lane 0 iff input[0] != 0.
	WORD	$0x4ea31c60	// mov v0.16b, v3.16b
	WORD	$0x79c00008	// ldrsh w8, [x0]
	WORD	$0x340000a8	// cbz w8, .+20
	WORD	$0x6f00e401	// movi v1.2d, #0
	WORD	$0x52800028	// mov w8, #1
	WORD	$0x4e021d01	// mov v1.h[0], w8
	WORD	$0x0e618400	// add v0.4h, v0.4h, v1.4h

	WORD	$0x6e180440	// mov v0.d[1], v2.d[0]
	WORD	$0x6e180485	// mov v5.d[1], v4.d[0]
	WORD	$0x4f1454a1	// shl v1.8h, v5.8h, #4
	WORD	$0x4e618402	// add v2.8h, v0.8h, v1.8h
	WORD	$0x6e618400	// sub v0.8h, v0.8h, v1.8h
	WORD	$0x6e024041	// ext v1.16b, v2.16b, v2.16b, #8
	WORD	$0x0e628423	// add v3.4h, v1.4h, v2.4h
	WORD	$0x528b5048	// mov w8, #23170
	WORD	$0x0e020d04	// dup v4.4h, w8
	WORD	$0x2e64b463	// sqrdmulh v3.4h, v3.4h, v4.4h
	WORD	$0x2e618441	// sub v1.4h, v2.4h, v1.4h
	WORD	$0x2e64b421	// sqrdmulh v1.4h, v1.4h, v4.4h
	WORD	$0x52876428	// mov w8, #15137
	WORD	$0x4e020d02	// dup v2.8h, w8
	WORD	$0x0e62c004	// smull v4.4s, v0.4h, v2.h[0]
	WORD	$0x52830fc8	// mov w8, #6270
	WORD	$0x4e020d05	// dup v5.8h, w8
	WORD	$0x0e65c006	// smull v6.4s, v0.4h, v5.h[0]
	WORD	$0x4e658004	// smlal2 v4.4s, v0.8h, v5.h[0]
	WORD	$0x4e62a006	// smlsl2 v6.4s, v0.8h, v2.h[0]
	WORD	$0x0f129c80	// sqrshrn v0.4h, v4.4s, #14
	WORD	$0x0f129cc4	// sqrshrn v4.4h, v6.4s, #14
	WORD	$0x0e402866	// trn1 v6.4h, v3.4h, v0.4h
	WORD	$0x0e406860	// trn2 v0.4h, v3.4h, v0.4h
	WORD	$0x0e442823	// trn1 v3.4h, v1.4h, v4.4h
	WORD	$0x0e446821	// trn2 v1.4h, v1.4h, v4.4h
	WORD	$0x0e8338c4	// zip1 v4.2s, v6.2s, v3.2s
	WORD	$0x0e8378c3	// zip2 v3.2s, v6.2s, v3.2s
	WORD	$0x0e813806	// zip1 v6.2s, v0.2s, v1.2s
	WORD	$0x0e817800	// zip2 v0.2s, v0.2s, v1.2s
	WORD	$0x0e648401	// add v1.4h, v0.4h, v4.4h
	WORD	$0x0e668467	// add v7.4h, v3.4h, v6.4h
	WORD	$0x2e6384c3	// sub v3.4h, v6.4h, v3.4h
	WORD	$0x2e608480	// sub v0.4h, v4.4h, v0.4h
	WORD	$0x0e6100e4	// saddl v4.4s, v7.4h, v1.4h
	WORD	$0x52ab5048	// mov w8, #1518469120
	WORD	$0x4e040d06	// dup v6.4s, w8
	WORD	$0x6ea6b484	// sqrdmulh v4.4s, v4.4s, v6.4s
	WORD	$0x0e672021	// ssubl v1.4s, v1.4h, v7.4h
	WORD	$0x6ea6b421	// sqrdmulh v1.4s, v1.4s, v6.4s
	WORD	$0x0e612884	// xtn v4.4h, v4.4s
	WORD	$0x0e612821	// xtn v1.4h, v1.4s
	WORD	$0x0e65c006	// smull v6.4s, v0.4h, v5.h[0]
	WORD	$0x0e65c065	// smull v5.4s, v3.4h, v5.h[0]
	WORD	$0x0e628005	// smlal v5.4s, v0.4h, v2.h[0]
	WORD	$0x0e62a066	// smlsl v6.4s, v3.4h, v2.h[0]
	WORD	$0x0f129ca0	// sqrshrn v0.4h, v5.4s, #14
	WORD	$0x0f129cc2	// sqrshrn v2.4h, v6.4s, #14
	WORD	$0x0e402883	// trn1 v3.4h, v4.4h, v0.4h
	WORD	$0x0e406880	// trn2 v0.4h, v4.4h, v0.4h
	WORD	$0x0e422824	// trn1 v4.4h, v1.4h, v2.4h
	WORD	$0x0e426821	// trn2 v1.4h, v1.4h, v2.4h
	WORD	$0x0e843862	// zip1 v2.2s, v3.2s, v4.2s
	WORD	$0x0e847863	// zip2 v3.2s, v3.2s, v4.2s
	WORD	$0x0e813804	// zip1 v4.2s, v0.2s, v1.2s
	WORD	$0x0e817800	// zip2 v0.2s, v0.2s, v1.2s
	WORD	$0x6e180482	// mov v2.d[1], v4.d[0]
	WORD	$0x6e180403	// mov v3.d[1], v0.d[0]
	WORD	$0x4f008420	// movi v0.8h, #1
	WORD	$0x4e608441	// add v1.8h, v2.8h, v0.8h
	WORD	$0x4f1e0421	// sshr v1.8h, v1.8h, #2
	WORD	$0x4e608460	// add v0.8h, v3.8h, v0.8h
	WORD	$0x4f1e0400	// sshr v0.8h, v0.8h, #2
	WORD	$0xad000021	// stp q1, q0, [x1]
	RET
