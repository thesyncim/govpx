//go:build arm64 && !purego

// ARMv8 NEON exact int16 sum-of-squares helpers for VP9 RD metrics.
// The shape mirrors libvpx v1.16.0 vp9/encoder/arm/neon/vp9_error_neon.c,
// but widens signed 16-bit values/differences to 32-bit before squaring so
// the Go helpers keep their exact arbitrary-int16 semantics.

#include "textflag.h"

// blockErrorFPNEON ABI ($0-32):
//   coeff+0(FP)   *int16
//   dqcoeff+8(FP) *int16
//   n+16(FP)      int, positive multiple of 8
//   ret+24(FP)    uint64
TEXT ·blockErrorFPNEON(SB), NOSPLIT, $0-32
	MOVD	coeff+0(FP), R0
	MOVD	dqcoeff+8(FP), R1
	MOVD	n+16(FP), R2

	WORD	$0x6f00e400 // movi.2d v0, #0
	WORD	$0x340001a2 // cbz w2, done
	WORD	$0x6f00e401 // movi.2d v1, #0
	WORD	$0x3cc10402 // ldr q2, [x0], #16
	WORD	$0x3cc10423 // ldr q3, [x1], #16
	WORD	$0x0e637044 // sabdl.4s v4, v2, v3
	WORD	$0x4e637042 // sabdl2.4s v2, v2, v3
	WORD	$0x2ea48080 // umlal.2d v0, v4, v4
	WORD	$0x6ea48081 // umlal2.2d v1, v4, v4
	WORD	$0x2ea28040 // umlal.2d v0, v2, v2
	WORD	$0x6ea28041 // umlal2.2d v1, v2, v2
	WORD	$0x71002042 // subs w2, w2, #8
	WORD	$0x54fffee1 // b.ne loop
	WORD	$0x4ee18400 // add.2d v0, v0, v1
	WORD	$0x5ef1b800 // addp.2d d0, v0
	WORD	$0x9e660000 // fmov x0, d0
	MOVD	R0, ret+24(FP)
	RET

// sumSquaresI16NEON ABI ($0-24):
//   src+0(FP) *int16
//   n+8(FP)   int, positive multiple of 8
//   ret+16(FP) uint64
TEXT ·sumSquaresI16NEON(SB), NOSPLIT, $0-24
	MOVD	src+0(FP), R0
	MOVD	n+8(FP), R1

	WORD	$0x6f00e400 // movi.2d v0, #0
	WORD	$0x340001c1 // cbz w1, done
	WORD	$0x6f00e401 // movi.2d v1, #0
	WORD	$0x3cc10402 // ldr q2, [x0], #16
	WORD	$0x0f10a443 // sshll.4s v3, v2, #0
	WORD	$0x4f10a442 // sshll2.4s v2, v2, #0
	WORD	$0x4ea0b863 // abs.4s v3, v3
	WORD	$0x4ea0b842 // abs.4s v2, v2
	WORD	$0x2ea38060 // umlal.2d v0, v3, v3
	WORD	$0x6ea38061 // umlal2.2d v1, v3, v3
	WORD	$0x2ea28040 // umlal.2d v0, v2, v2
	WORD	$0x6ea28041 // umlal2.2d v1, v2, v2
	WORD	$0x71002021 // subs w1, w1, #8
	WORD	$0x54fffec1 // b.ne loop
	WORD	$0x4ee18400 // add.2d v0, v0, v1
	WORD	$0x5ef1b800 // addp.2d d0, v0
	WORD	$0x9e660000 // fmov x0, d0
	MOVD	R0, ret+16(FP)
	RET
