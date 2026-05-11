//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0 vp8/encoder/x86/fwalsh_sse2.asm
// (vp8_short_walsh4x4_sse2).
//
// Loads 4 rows of 4 int16 each (stride is in int16 elements). Pass 1 is
// done in int16 lanes after a 4x4 transpose via PUNPCKLWD/PUNPCKLDQ; pass 2
// is widened to int32 (PMADDWD against [1,1,1,1] / [1,-1,1,-1] multipliers)
// and the sign-correction is done with PCMPGTD against zero ANDed with 1.
// PSRAD by 3 produces the final 4-lane int32 columns; PACKSSDW packs back
// to int16 for the 16-element output.
//
// Output is byte-identical to forwardWalsh4x4Scalar for the encoder's
// residual range.

#include "textflag.h"

// 8 x int16 of 1, used as PMADDWD multiplier for "a + b" and as additive
// for the (a1 != 0) ? 1 : 0 correction.
DATA  fwalsh_c1<>+0x00(SB)/4, $0x00010001
DATA  fwalsh_c1<>+0x04(SB)/4, $0x00010001
DATA  fwalsh_c1<>+0x08(SB)/4, $0x00010001
DATA  fwalsh_c1<>+0x0c(SB)/4, $0x00010001
GLOBL fwalsh_c1<>(SB), RODATA|NOPTR, $16

// 8 x int16 alternating 1,-1 — PMADDWD multiplier for "a - b".
DATA  fwalsh_cn1<>+0x00(SB)/4, $0xffff0001
DATA  fwalsh_cn1<>+0x04(SB)/4, $0xffff0001
DATA  fwalsh_cn1<>+0x08(SB)/4, $0xffff0001
DATA  fwalsh_cn1<>+0x0c(SB)/4, $0xffff0001
GLOBL fwalsh_cn1<>(SB), RODATA|NOPTR, $16

// 4 x int32 of 1 — pass-2 sign-correction additive (after AND with mask).
DATA  fwalsh_cd1<>+0x00(SB)/4, $0x00000001
DATA  fwalsh_cd1<>+0x04(SB)/4, $0x00000001
DATA  fwalsh_cd1<>+0x08(SB)/4, $0x00000001
DATA  fwalsh_cd1<>+0x0c(SB)/4, $0x00000001
GLOBL fwalsh_cd1<>(SB), RODATA|NOPTR, $16

// 4 x int32 of 3 — column-pass +3 bias before PSRAD by 3.
DATA  fwalsh_cd3<>+0x00(SB)/4, $0x00000003
DATA  fwalsh_cd3<>+0x04(SB)/4, $0x00000003
DATA  fwalsh_cd3<>+0x08(SB)/4, $0x00000003
DATA  fwalsh_cd3<>+0x0c(SB)/4, $0x00000003
GLOBL fwalsh_cd3<>(SB), RODATA|NOPTR, $16

// forwardWalsh4x4SSE2 ABI ($0-24):
//   input+0(FP)   *int16
//   stride+8(FP)  int  (in int16 elements)
//   output+16(FP) *int16
TEXT ·forwardWalsh4x4SSE2(SB), NOSPLIT, $0-24
	MOVQ	input+0(FP), SI
	MOVQ	stride+8(FP), DX
	MOVQ	output+16(FP), DI
	SHLQ	$1, DX                  // bytes per row = stride * 2

	// Load 4 rows of 4 int16 (8 bytes each) into low halves of X0..X3.
	MOVQ	(SI), X0                // X0 lo64 = row 0  (4 int16)
	MOVQ	(SI)(DX*1), X1          // X1 lo64 = row 1
	LEAQ	(SI)(DX*2), SI          // SI = input + 2*stride
	MOVQ	(SI), X2                // X2 lo64 = row 2
	MOVQ	(SI)(DX*1), X3          // X3 lo64 = row 3

	// === 4x4 transpose into per-column 4-int16 vectors ===
	// X0 (8 lanes) = (r0c0, r1c0, r0c1, r1c1, r0c2, r1c2, r0c3, r1c3)
	// X2 (8 lanes) = (r2c0, r3c0, r2c1, r3c1, r2c2, r3c2, r2c3, r3c3)
	PUNPCKLWL	X1, X0          // (Plan9 PUNPCKLWL = Intel PUNPCKLWD)
	PUNPCKLWL	X3, X2

	MOVO	X0, X1
	PUNPCKLLQ	X2, X0          // X0 lanes (8 int16) = col0 (lanes 0..3, rows 0..3) || col1 (lanes 4..7, rows 0..3)
	PUNPCKHLQ	X2, X1          // X1 = col 2 || col 3

	// === Pass 1 ===
	// a = (col0 + col2) << 2 ; d = (col1 + col3) << 2
	// b = (col0 - col2) << 2 ; c = (col1 - col3) << 2
	MOVO	X0, X2
	PADDW	X1, X0                  // X0 = (col0+col2, col1+col3) = (a, d) pre-shift
	PSUBW	X1, X2                  // X2 = (col0-col2, col1-col3) = (b, c) pre-shift
	PSLLW	$2, X0                  // X0 = (a1, d1)
	PSLLW	$2, X2                  // X2 = (b1, c1)

	// Pack into [a1, b1] and [d1, c1] for the per-row butterfly.
	MOVO	X0, X1                  // X1 = (a1, d1)
	PUNPCKLQDQ	X2, X0          // X0 = (a1 lo, b1 lo) — i.e., [a1 (4 lanes), b1 (4 lanes)]
	PUNPCKHQDQ	X2, X1          // X1 = (d1 hi from X1, c1 hi from X2) = [d1, c1]

	// (a1 != 0) ? 1 : 0 sign-correction. PCMPEQW against zeroed reg
	// produces -1 mask in lanes where a1==0; PADDW with all-ones constant
	// shifts the mask to {0, 1}. The high 4 lanes (b1) compare against 0
	// in the high lanes of X6 (also 0) so they end up at {0..} → 0+1 = 1
	// in libvpx's flow but those lanes don't affect the final result
	// because we only add this correction to the (a1+d1) half.
	//
	// Specifically: build adj such that adj[low 4 lanes] = (a1!=0?1:0)
	// and adj[high 4 lanes] = arbitrary, but XMM7 ends up with its high
	// half ALSO 0 at this point (because we PXOR'd it). After PCMPEQW
	// against XMM6 which has ONLY a1 in low 4 and zeros in high 4, the
	// HIGH lanes are 0 == 0 = -1; PADDW with c1 = 1 gives 0 in high.
	// So adj.high = 0, perfect — PADDW into op[0]+op[4] won't disturb
	// op[4].
	PXOR	X6, X6
	MOVQ	X0, X6                  // X6 lo64 = a1 (4 int16); hi64 = 0
	PXOR	X7, X7
	PCMPEQW	X6, X7                  // X7 = -1 in lanes where a1 == 0 (low) or 0 == 0 (high).
	PADDW	fwalsh_c1<>(SB), X7     // X7 = adj : low 4 = (a1!=0?1:0), high 4 = 0

	MOVO	X0, X2                  // X2 = [a1, b1]
	PADDW	X1, X0                  // X0 = [a1+d1, b1+c1] = [op0, op1]   (lane k = row k)
	PSUBW	X1, X2                  // X2 = [a1-d1, b1-c1] = [op3, op2]
	PADDW	X7, X0                  // X0[low] += adj  → [op0+adj, op1]

	// === Pass 2 — interleave row pairs via PSHUF{LW,HW} so that the
	// PMADDWD with [1,1] / [1,-1] computes per-column a/d/b/c. ===
	// X0 lanes (low 4 = op0, high 4 = op1, each 4 rows).
	// PSHUFLW $0xd8 reorders lanes 0..3 from (0,1,2,3) -> (0,2,1,3).
	PSHUFLW	$0xd8, X0, X3
	PSHUFHW	$0xd8, X3, X0           // X0 = (op0[r0,r2,r1,r3], op1[r0,r2,r1,r3])
	PSHUFLW	$0xd8, X2, X3
	PSHUFHW	$0xd8, X3, X1           // X1 = (op3[r0,r2,r1,r3], op2[r0,r2,r1,r3])

	MOVO	X0, X2
	PMADDWL	fwalsh_c1<>(SB), X0     // X0 (4 int32) = (op0[0]+op0[2], op0[1]+op0[3], op1[0]+op1[2], op1[1]+op1[3])
	                                //              = (a1c0, d1c0, a1c1, d1c1)
	PMADDWL	fwalsh_cn1<>(SB), X2    // X2 = (b1c0, c1c0, b1c1, c1c1)
	MOVO	X1, X3
	PMADDWL	fwalsh_c1<>(SB), X1     // X1 = pmadd of [op3 shuffled, op2 shuffled] with [1,1] vector.
	                                //   For op3 in low half, op3[0]+op3[2] = a1 for col 3 (since op3 corresponds to col 3 in tmp).
	                                //   For op2 in high half, op2[0]+op2[2] = a1 for col 2.
	                                //   So X1 = (a1c3, d1c3, a1c2, d1c2)
	PMADDWL	fwalsh_cn1<>(SB), X3    // X3 = (b1c3, c1c3, b1c2, c1c2)

	// Re-order: pack a1's together, d1's together, etc.
	// PSHUFD $0xd8 on X0 = (a1c0, d1c0, a1c1, d1c1):
	//   0xd8 = 11_01_10_00 → dst[0]=src[0], dst[1]=src[2], dst[2]=src[1], dst[3]=src[3]
	//   X4 = (a1c0, a1c1, d1c0, d1c1)
	PSHUFD	$0xd8, X0, X4           // X4 = (a1c0, a1c1, d1c0, d1c1)
	PSHUFD	$0xd8, X2, X5           // X5 = (b1c0, b1c1, c1c0, c1c1)
	// PSHUFD $0x72 on X1 = (a1c3, d1c3, a1c2, d1c2):
	//   0x72 = 01_11_00_10 → dst[0]=src[2], dst[1]=src[0], dst[2]=src[3], dst[3]=src[1]
	//   X6 = (a1c2, a1c3, d1c2, d1c3)
	PSHUFD	$0x72, X1, X6           // X6 = (a1c2, a1c3, d1c2, d1c3)
	PSHUFD	$0x72, X3, X7           // X7 = (b1c2, b1c3, c1c2, c1c3)

	MOVO	X4, X0
	PUNPCKLQDQ	X5, X0          // X0 = [X4 lo64, X5 lo64] = (a1c0, a1c1, b1c0, b1c1)
	PUNPCKHQDQ	X5, X4          // X4 = (d1c0, d1c1, c1c0, c1c1)
	MOVO	X6, X1
	PUNPCKLQDQ	X7, X1          // X1 = (a1c2, a1c3, b1c2, b1c3)
	PUNPCKHQDQ	X7, X6          // X6 = (d1c2, d1c3, c1c2, c1c3)

	// Pass-2 butterfly: a2 = a1+d1, b2 = b1+c1, c2 = b1-c1, d2 = a1-d1.
	MOVO	X0, X2
	PADDL	X4, X0                  // X0 = [a2c0, a2c1, b2c0, b2c1]
	PSUBL	X4, X2                  // X2 = [d2c0, d2c1, c2c0, c2c1]
	MOVO	X1, X3
	PADDL	X6, X1                  // X1 = [a2c2, a2c3, b2c2, b2c3]
	PSUBL	X6, X3                  // X3 = [d2c2, d2c3, c2c2, c2c3]

	// Sign correction: x += (x < 0) ? 1 : 0.
	PXOR	X4, X4
	MOVO	X4, X5
	PCMPGTL	X0, X4                  // X4 = -1 mask where 0 > X0 (i.e., X0 < 0)
	PCMPGTL	X2, X5                  // X5 = mask where X2 < 0
	PAND	fwalsh_cd1<>(SB), X4    // X4 = 1 where X0 < 0, else 0
	PAND	fwalsh_cd1<>(SB), X5

	PXOR	X6, X6
	MOVO	X6, X7
	PCMPGTL	X1, X6
	PCMPGTL	X3, X7
	PAND	fwalsh_cd1<>(SB), X6
	PAND	fwalsh_cd1<>(SB), X7

	PADDL	X4, X0
	PADDL	X5, X2
	PADDL	fwalsh_cd3<>(SB), X0    // X0 += 3
	PADDL	fwalsh_cd3<>(SB), X2
	PADDL	X6, X1
	PADDL	X7, X3
	PADDL	fwalsh_cd3<>(SB), X1
	PADDL	fwalsh_cd3<>(SB), X3

	PSRAL	$3, X0                  // signed shift right by 3
	PSRAL	$3, X1
	PSRAL	$3, X2
	PSRAL	$3, X3

	// Repack so xmm0 = [a2c0..a2c3, b2c0..b2c3] and xmm2 = [c2c0..c2c3, d2c0..d2c3].
	MOVO	X0, X4
	PUNPCKLQDQ	X1, X0          // X0 = (a2c0, a2c1, a2c2, a2c3)         (4 int32 lanes)
	PUNPCKHQDQ	X1, X4          // X4 = (b2c0, b2c1, b2c2, b2c3)
	MOVO	X2, X5
	PUNPCKHQDQ	X3, X2          // X2 = (c2c0, c2c1, c2c2, c2c3)
	PUNPCKLQDQ	X3, X5          // X5 = (d2c0, d2c1, d2c2, d2c3)

	PACKSSLW	X4, X0          // X0 (8 int16) = [a2c0..3, b2c0..3]   = output[0..7]
	PACKSSLW	X5, X2          // X2 = [c2c0..3, d2c0..3]            = output[8..15]

	MOVOU	X0, 0(DI)
	MOVOU	X2, 16(DI)
	RET
