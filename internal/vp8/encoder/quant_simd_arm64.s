// ARMv8 NEON port of the libvpx v1.16.0
// vp8/encoder/arm/neon/fastquantizeb_neon.c vp8_fast_quantize_b_neon kernel.
//
// Computes for each of 16 lanes:
//   sz = z >> 15                     // sign bits (0 or -1)
//   x  = abs(z) = (z ^ sz) - sz
//   x  = x + round
//   y  = (x * quantFast) >> 16       // via vqdmulhq_s16 then >>1
//   x  = (y ^ sz) - sz               // restore sign
//   qcoeff  = x
//   dqcoeff = x * dequant
//   eob_lane = (x != 0) ? invZigZag[rc] : 0
// Returns max(eob_lane) over 16 lanes (== eob+1 from scalar).
//
// Most NEON ops aren't supported by Go's arm64 assembler natively, so the
// arithmetic forms are emitted as raw WORD encodings. Encodings come from
// clang with the equivalent native ARMv8 mnemonics.

#include "textflag.h"

// Inverse zig-zag mapping (1-indexed scan position per natural-order
// coefficient slot). Matches tables.DefaultInvZigZag.
DATA  invZigZagFastQuant<>+0x00(SB)/2, $1
DATA  invZigZagFastQuant<>+0x02(SB)/2, $2
DATA  invZigZagFastQuant<>+0x04(SB)/2, $6
DATA  invZigZagFastQuant<>+0x06(SB)/2, $7
DATA  invZigZagFastQuant<>+0x08(SB)/2, $3
DATA  invZigZagFastQuant<>+0x0a(SB)/2, $5
DATA  invZigZagFastQuant<>+0x0c(SB)/2, $8
DATA  invZigZagFastQuant<>+0x0e(SB)/2, $13
DATA  invZigZagFastQuant<>+0x10(SB)/2, $4
DATA  invZigZagFastQuant<>+0x12(SB)/2, $9
DATA  invZigZagFastQuant<>+0x14(SB)/2, $12
DATA  invZigZagFastQuant<>+0x16(SB)/2, $14
DATA  invZigZagFastQuant<>+0x18(SB)/2, $10
DATA  invZigZagFastQuant<>+0x1a(SB)/2, $11
DATA  invZigZagFastQuant<>+0x1c(SB)/2, $15
DATA  invZigZagFastQuant<>+0x1e(SB)/2, $16
GLOBL invZigZagFastQuant<>(SB), RODATA|NOPTR, $32

// fastQuantizeBlockNEON ABI ($0-56):
//   coeff+0(FP)      *int16
//   round+8(FP)      *int16
//   quantFast+16(FP) *int16
//   dequant+24(FP)   *int16
//   qcoeff+32(FP)    *int16
//   dqcoeff+40(FP)   *int16
//   ret+48(FP)       int32
TEXT ·fastQuantizeBlockNEON(SB), NOSPLIT, $0-56
	MOVD	coeff+0(FP), R0
	MOVD	round+8(FP), R1
	MOVD	quantFast+16(FP), R2
	MOVD	dequant+24(FP), R3
	MOVD	qcoeff+32(FP), R4
	MOVD	dqcoeff+40(FP), R5
	MOVD	$invZigZagFastQuant<>(SB), R6

	VLD1	(R0), [V0.H8, V1.H8]	// V0/V1: z
	VLD1	(R1), [V2.H8, V3.H8]	// V2/V3: round
	VLD1	(R2), [V4.H8, V5.H8]	// V4/V5: quantFast
	VLD1	(R3), [V6.H8, V7.H8]	// V6/V7: dequant
	VLD1	(R6), [V8.H8, V9.H8]	// V8/V9: invZigZag

	WORD	$0x4e60a80a	// cmlt v10.8h, v0.8h, #0
	WORD	$0x4e60a82b	// cmlt v11.8h, v1.8h, #0

	WORD	$0x4e60b80c	// abs v12.8h, v0.8h
	WORD	$0x4e60b82d	// abs v13.8h, v1.8h

	WORD	$0x4e62858e	// add v14.8h, v12.8h, v2.8h
	WORD	$0x4e6385af	// add v15.8h, v13.8h, v3.8h

	WORD	$0x4e64b5d0	// sqdmulh v16.8h, v14.8h, v4.8h
	WORD	$0x4e65b5f1	// sqdmulh v17.8h, v15.8h, v5.8h

	WORD	$0x4f1f0610	// sshr v16.8h, v16.8h, #1
	WORD	$0x4f1f0631	// sshr v17.8h, v17.8h, #1

	WORD	$0x6e2a1e12	// eor v18.16b, v16.16b, v10.16b
	WORD	$0x6e2b1e33	// eor v19.16b, v17.16b, v11.16b

	WORD	$0x6e6a8652	// sub v18.8h, v18.8h, v10.8h
	WORD	$0x6e6b8673	// sub v19.8h, v19.8h, v11.8h

	// V18/V19 = qcoeff
	VST1	[V18.H8, V19.H8], (R4)

	WORD	$0x4e669e54	// mul v20.8h, v18.8h, v6.8h
	WORD	$0x4e679e75	// mul v21.8h, v19.8h, v7.8h
	VST1	[V20.H8, V21.H8], (R5)

	// EOB: cmtst x with itself -> -1 mask in lanes where x != 0.
	WORD	$0x4e728e56	// cmtst v22.8h, v18.8h, v18.8h
	WORD	$0x4e738e77	// cmtst v23.8h, v19.8h, v19.8h
	WORD	$0x4e281ed6	// and v22.16b, v22.16b, v8.16b
	WORD	$0x4e291ef7	// and v23.16b, v23.16b, v9.16b
	WORD	$0x6e7766d8	// umax v24.8h, v22.8h, v23.8h
	WORD	$0x6e70ab19	// umaxv h25, v24.8h
	WORD	$0x1e260320	// fmov w0, s25

	MOVW	R0, ret+48(FP)
	RET
