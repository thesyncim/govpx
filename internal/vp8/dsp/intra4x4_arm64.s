//go:build arm64 && !purego

// ARMv8 NEON 4x4 B_PRED intra-prediction kernels. Mirrors libvpx
// v1.16.0 vp8/common/reconintra4x4.c per-mode formulas (exact AVG3
// = (a + 2*b + c + 2) >> 2 and AVG2 = (a + b + 1) >> 1) with
// byte-identical output to the scalar reference in intra4x4.go.
//
// libvpx's NEON predictors for VP9 use the cheaper rounding-halving
// idiom vrhadd(vhadd(a, c), b) which is *not* exact. VP8 requires the
// exact AVG3 formula, so these kernels widen to int16 lanes and use
// signed-saturate-narrow (SQXTUN) to drop back to bytes.
//
// Output is laid out as four 4-byte rows at arbitrary stride. We
// extract each row's four bytes via VMOV V.S[i], Rtmp; MOVW Rtmp, (Rdst).
//
// SQXTUN is encoded as a raw WORD because the Go arm64 assembler does
// not recognize the mnemonic.

#include "textflag.h"

// avg3 helper expectation: build int16 lanes V_a, V_b, V_c (lanes
// holding a, b, c respectively), compute (V_a + V_c + V_b + V_b + 2)
// >> 2 -> narrow back to bytes.
//
// The 4x4 kernels load up to 8 bytes of `above` and 4 bytes of `left`
// once into 64-bit V registers; widen to int16 with VUXTL B8 -> H8;
// shift via VEXT on the byte (16-byte) vector to align the windows.

// intra4x4DCPredictNEON ABI ($0-32):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   above+16(FP) *byte
//   left+24(FP)  *byte
//
// dc = (sum(above[0..3]) + sum(left[0..3]) + 4) >> 3, broadcast to 4x4.
TEXT ·intra4x4DCPredictNEON(SB), NOSPLIT, $0-32
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3

	// Load 4 bytes each via FMOVS so the upper bytes are zeroed.
	FMOVS	(R2), F0
	FMOVS	(R3), F1
	VUXTL	V0.B8, V0.H8
	VUXTL	V1.B8, V1.H8
	VADD	V1.H8, V0.H8, V0.H8
	// Reduce 4 lanes (lanes 4..7 are zero) to a single value with VADDV.
	VADDV	V0.H8, V0
	VMOV	V0.H[0], R4
	ADD	$4, R4, R4
	LSR	$3, R4, R4
	// Broadcast R4 byte across 4 bytes.
	AND	$0xff, R4, R4
	MOVD	R4, R5
	LSL	$8, R4, R4
	ORR	R5, R4, R4
	MOVD	R4, R5
	LSL	$16, R4, R4
	ORR	R5, R4, R4
	// Store row by row.
	MOVW	R4, (R0)
	ADD	R1, R0, R0
	MOVW	R4, (R0)
	ADD	R1, R0, R0
	MOVW	R4, (R0)
	ADD	R1, R0, R0
	MOVW	R4, (R0)
	RET

// intra4x4TMPredictNEON ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// dst[y, x] = clip255(left[y] + above[x] - topLeft).
TEXT ·intra4x4TMPredictNEON(SB), NOSPLIT, $0-33
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3
	MOVBU	topLeft+32(FP), R4

	// Load 4 above bytes -> int16x4 (low 4 lanes of V10.H8).
	FMOVS	(R2), F0
	VUXTL	V0.B8, V10.H8
	// Broadcast topLeft and subtract.
	VDUP	R4, V20.H8
	VSUB	V20.H8, V10.H8, V10.H8

	// Load 4 left bytes; broadcast each lane via VDUP V.H[i].
	FMOVS	(R3), F1
	VUXTL	V1.B8, V12.H8

	// Row 0
	VDUP	V12.H[0], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce             // sqxtun v14.8b, v14.8h
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	// Row 1
	VDUP	V12.H[1], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	// Row 2
	VDUP	V12.H[2], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	// Row 3
	VDUP	V12.H[3], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	RET

// intra4x4VEPredictNEON ABI ($0-25):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   topLeft+24(FP) byte
//
// row = [ avg3(tl, A0, A1), avg3(A0, A1, A2), avg3(A1, A2, A3), avg3(A2, A3, A4) ]
// Replicated to all 4 rows.
//
// Strategy: build a 6-byte sequence [tl, A0, A1, A2, A3, A4] in V0.B8
// lanes 0..5; extract three int16x8 windows shifted by 0/1/2; compute
// (a + 2b + c + 2) >> 2; narrow lanes 0..3 (the valid ones).
TEXT ·intra4x4VEPredictNEON(SB), NOSPLIT, $0-25
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVBU	topLeft+24(FP), R4

	// Load A0..A4 (5 bytes). Use FMOVS for 4 bytes then read A4
	// separately and insert.
	FMOVS	(R2), F0                // V0.B8 = [A0 A1 A2 A3 0 0 0 0]
	MOVBU	4(R2), R5               // R5 = A4
	// Insert tl into lane 0 of V_tl, A0..A4 follow at lanes 1..5
	// But VEXT needs aligned vectors. We'll use a different layout:
	//   V_a holds tl, A0, A1, A2 at lanes 0..3 (a-window)
	//   V_b holds A0, A1, A2, A3 at lanes 0..3 (b-window)
	//   V_c holds A1, A2, A3, A4 at lanes 0..3 (c-window)
	//
	// Build via VMOV inserts on a fresh register V8.B16:
	//   V8.B[0]=tl V8.B[1]=A0 V8.B[2]=A1 V8.B[3]=A2 V8.B[4]=A3 V8.B[5]=A4
	VMOV	R4, V8.B[0]
	VMOV	V0.S[0], R6             // R6 has A0..A3 packed
	MOVD	R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[1]             // A0
	LSR	$8, R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[2]             // A1
	LSR	$16, R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[3]             // A2
	LSR	$24, R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[4]             // A3
	VMOV	R5, V8.B[5]             // A4

	// Three windows by EXT on the BYTE vector V8 by 0/1/2 bytes,
	// then widen each to int16x8.
	VEXT	$1, V8.B16, V8.B16, V1.B16
	VEXT	$2, V8.B16, V8.B16, V2.B16
	VUXTL	V8.B8, V10.H8           // a-window: lanes 0..3 = tl, A0, A1, A2
	VUXTL	V1.B8, V11.H8           // b-window: A0, A1, A2, A3
	VUXTL	V2.B8, V12.H8           // c-window: A1, A2, A3, A4
	// (a + 2b + c + 2) >> 2
	VADD	V11.H8, V11.H8, V13.H8  // 2*b
	VADD	V13.H8, V10.H8, V13.H8  // a + 2b
	VADD	V13.H8, V12.H8, V13.H8  // a + 2b + c
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8  // + 2
	VUSHR	$2, V13.H8, V13.H8      // >> 2
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h

	// Lanes 0..3 of V13.B8 are the 4-byte row.
	VMOV	V13.S[0], R6
	MOVW	R6, (R0)
	ADD	R1, R0, R0
	MOVW	R6, (R0)
	ADD	R1, R0, R0
	MOVW	R6, (R0)
	ADD	R1, R0, R0
	MOVW	R6, (R0)
	RET

// intra4x4HEPredictNEON ABI ($0-25):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   left+16(FP)  *byte
//   topLeft+24(FP) byte
//
// row[0] = avg3(tl, L0, L1) replicated x4
// row[1] = avg3(L0, L1, L2) replicated x4
// row[2] = avg3(L1, L2, L3) replicated x4
// row[3] = avg3(L2, L3, L3) replicated x4
TEXT ·intra4x4HEPredictNEON(SB), NOSPLIT, $0-25
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	left+16(FP), R2
	MOVBU	topLeft+24(FP), R4

	// Build sequence: V8.B[0]=tl, [1]=L0, [2]=L1, [3]=L2, [4]=L3, [5]=L3
	// (last entry duplicated for the boundary case avg3(L2, L3, L3)).
	FMOVS	(R2), F0                // V0.B8 = [L0 L1 L2 L3 ...]
	VMOV	R4, V8.B[0]
	VMOV	V0.S[0], R6
	MOVD	R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[1]
	LSR	$8, R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[2]
	LSR	$16, R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[3]
	LSR	$24, R6, R7
	AND	$0xff, R7, R7
	VMOV	R7, V8.B[4]
	VMOV	R7, V8.B[5]             // duplicate L3

	VEXT	$1, V8.B16, V8.B16, V1.B16
	VEXT	$2, V8.B16, V8.B16, V2.B16
	VUXTL	V8.B8, V10.H8           // tl, L0, L1, L2
	VUXTL	V1.B8, V11.H8           // L0, L1, L2, L3
	VUXTL	V2.B8, V12.H8           // L1, L2, L3, L3
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h

	// V13.B[0..3] = row0..row3 fill values; broadcast each to 4 bytes.
	VMOV	V13.B[0], R5
	AND	$0xff, R5, R5
	MOVD	R5, R6
	LSL	$8, R5, R5
	ORR	R6, R5, R5
	MOVD	R5, R6
	LSL	$16, R5, R5
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	VMOV	V13.B[1], R5
	AND	$0xff, R5, R5
	MOVD	R5, R6
	LSL	$8, R5, R5
	ORR	R6, R5, R5
	MOVD	R5, R6
	LSL	$16, R5, R5
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	VMOV	V13.B[2], R5
	AND	$0xff, R5, R5
	MOVD	R5, R6
	LSL	$8, R5, R5
	ORR	R6, R5, R5
	MOVD	R5, R6
	LSL	$16, R5, R5
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	VMOV	V13.B[3], R5
	AND	$0xff, R5, R5
	MOVD	R5, R6
	LSL	$8, R5, R5
	ORR	R6, R5, R5
	MOVD	R5, R6
	LSL	$16, R5, R5
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	RET

// intra4x4LDPredictNEON ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   above+16(FP) *byte
//
// d[k] = avg3(A[k], A[k+1], A[k+2]) for k = 0..5 (last d[5] uses A[5..7]
// where the scalar formula is avg3(F, G, H)). Then dst[r][c] = d[r+c],
// except dst[3][3] = avg3(g, h, h).
//
// Layout:
//   row 0 = d0 d1 d2 d3
//   row 1 = d1 d2 d3 d4
//   row 2 = d2 d3 d4 d5
//   row 3 = d3 d4 d5 avg3(g,h,h)
TEXT ·intra4x4LDPredictNEON(SB), NOSPLIT, $0-24
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2

	// Load 8 bytes of above; duplicate A7 into lane 6 of the b- and
	// c-windows so the k=6 avg3 lane resolves to avg3(A6, A7, A7) which
	// is exactly the dst[3,3] = avg3(g, h, h) corner from the scalar
	// reference. FMOVD also zeroes V0's upper 8 bytes, which keeps the
	// VEXT wraparound bytes deterministic (we only consume lanes 0..6).
	FMOVD	(R2), F0
	VEXT	$1, V0.B16, V0.B16, V20.B16
	VEXT	$2, V0.B16, V0.B16, V21.B16
	VMOV	V0.B[7], V20.B[6]
	VMOV	V0.B[7], V21.B[6]

	VUXTL	V0.B8, V10.H8           // a window: A0..A7 as int16
	VUXTL	V20.B8, V11.H8          // b window
	VUXTL	V21.B8, V12.H8          // c window
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h
	// V13.B[0..6] = d0..d6.

	// Build the four output rows:
	// row0: d0 d1 d2 d3 -> bytes 0..3 of V13
	// row1: d1 d2 d3 d4
	// row2: d2 d3 d4 d5
	// row3: d3 d4 d5 d6
	VEXT	$1, V13.B16, V13.B16, V14.B16
	VEXT	$2, V13.B16, V13.B16, V15.B16
	VEXT	$3, V13.B16, V13.B16, V16.B16
	VMOV	V13.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	VMOV	V15.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	VMOV	V16.S[0], R5
	MOVW	R5, (R0)
	RET

// intra4x4RDPredictNEON ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// Build sequence S = [l, k, j, i, x, a, b, c, d] (9 bytes); compute
// d[k] = avg3(S[k], S[k+1], S[k+2]) for k = 0..6. Then:
//   row0 = d6 d5 d4 d3
//   row1 = d5 d4 d3 d2
//   row2 = d4 d3 d2 d1
//   row3 = d3 d2 d1 d0
//
// (Verified by expanding the scalar formula in intra4x4.go.)
TEXT ·intra4x4RDPredictNEON(SB), NOSPLIT, $0-33
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3
	MOVBU	topLeft+32(FP), R4

	// V8 = [l k j i x a b c d 0 0 0 0 0 0 0]
	MOVBU	3(R3), R5
	VMOV	R5, V8.B[0]             // l
	MOVBU	2(R3), R5
	VMOV	R5, V8.B[1]             // k
	MOVBU	1(R3), R5
	VMOV	R5, V8.B[2]             // j
	MOVBU	0(R3), R5
	VMOV	R5, V8.B[3]             // i
	VMOV	R4, V8.B[4]             // x
	MOVBU	0(R2), R5
	VMOV	R5, V8.B[5]             // a
	MOVBU	1(R2), R5
	VMOV	R5, V8.B[6]             // b
	MOVBU	2(R2), R5
	VMOV	R5, V8.B[7]             // c
	MOVBU	3(R2), R5
	VMOV	R5, V8.B[8]             // d (in upper half - but V8 only used to .B[7])

	// We need d at lane 8 to compute window k=6 with c=d. VEXT works on
	// 16-byte vectors so upper 8 bytes matter. The above VMOV writes go
	// across lanes 0..8 of V8.B16 which is fine.

	VEXT	$1, V8.B16, V8.B16, V20.B16
	VEXT	$2, V8.B16, V8.B16, V21.B16
	VUXTL	V8.B8, V10.H8           // a: lanes 0..7 (l k j i x a b c)
	VUXTL	V20.B8, V11.H8          // b: lanes 0..7 (k j i x a b c d)
	VUXTL	V21.B8, V12.H8          // c: lanes 0..7 (j i x a b c d 0)
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h
	// V13.B[0..6] = d0..d6 with:
	//   d0 = avg3(l, k, j)
	//   d1 = avg3(k, j, i)
	//   d2 = avg3(j, i, x)
	//   d3 = avg3(i, x, a)
	//   d4 = avg3(x, a, b)
	//   d5 = avg3(a, b, c)
	//   d6 = avg3(b, c, d)

	// Cross-check with intra4x4.go scalar:
	//   dst[3,0] = avg3(j,k,l) = d0 (since avg3 is symmetric in a,c)
	//   dst[3,1] = avg3(i,j,k) = d1
	//   dst[3,2] = avg3(x,i,j) = d2
	//   dst[3,3] = avg3(a,x,i) = d3
	//   dst[2,3] = avg3(b,a,x) = d4
	//   dst[1,3] = avg3(c,b,a) = d5
	//   dst[0,3] = avg3(d,c,b) = d6
	// And the diagonal duplications give:
	//   row3: d0 d1 d2 d3
	//   row2: d1 d2 d3 d4
	//   row1: d2 d3 d4 d5
	//   row0: d3 d4 d5 d6
	//
	// So the row writes are: row0 starts at offset 3 in d-vector,
	// row1 at offset 2, row2 at offset 1, row3 at offset 0.
	VEXT	$1, V13.B16, V13.B16, V14.B16
	VEXT	$2, V13.B16, V13.B16, V15.B16
	VEXT	$3, V13.B16, V13.B16, V16.B16
	// row0 = V16.S[0] (bytes d3 d4 d5 d6)
	VMOV	V16.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	// row1 = V15.S[0] (d2 d3 d4 d5)
	VMOV	V15.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	// row2 = V14.S[0] (d1 d2 d3 d4)
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0
	// row3 = V13.S[0] (d0 d1 d2 d3)
	VMOV	V13.S[0], R5
	MOVW	R5, (R0)
	RET

// intra4x4VRPredictNEON ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// VR is a mix of avg2 and avg3:
//   row0 (even row): dst[0,0..3] = avg2(x,a) avg2(a,b) avg2(b,c) avg2(c,d)
//   row1 (odd row):  dst[1,0..3] = avg3(i,x,a) avg3(x,a,b) avg3(a,b,c) avg3(b,c,d)
//   row2 (even row): dst[2,0..3] = avg3(j,i,x) avg2(x,a) avg2(a,b) avg2(b,c)
//   row3 (odd row):  dst[3,0..3] = avg3(k,j,i) avg3(i,x,a) avg3(x,a,b) avg3(a,b,c)
//
// Using:
//   row2[0] = avg3(j, i, x)
//   row3[0] = avg3(k, j, i)
//   row3[1] = row1[0] = avg3(i, x, a)
//   row3[2] = row1[1] = avg3(x, a, b)
//   row3[3] = row1[2] = avg3(a, b, c)
//   row1[3] = avg3(b, c, d)
//   row2[1] = row0[0] = avg2(x, a)
//   row2[2] = row0[1] = avg2(a, b)
//   row2[3] = row0[2] = avg2(b, c)
//   row0[3] = avg2(c, d)
//
// Build base sequence S = [k, j, i, x, a, b, c, d] for the avg3 work.
// d_k = avg3(S[k], S[k+1], S[k+2]) for k = 0..5:
//   d0 = avg3(k, j, i)
//   d1 = avg3(j, i, x)
//   d2 = avg3(i, x, a)
//   d3 = avg3(x, a, b)
//   d4 = avg3(a, b, c)
//   d5 = avg3(b, c, d)
// Then for avg2 build T = [x, a, b, c, d] and e_k = avg2(T[k], T[k+1]) for k=0..3:
//   e0 = avg2(x, a)
//   e1 = avg2(a, b)
//   e2 = avg2(b, c)
//   e3 = avg2(c, d)
TEXT ·intra4x4VRPredictNEON(SB), NOSPLIT, $0-33
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3
	MOVBU	topLeft+32(FP), R4

	// V8 = [k j i x a b c d ...]
	MOVBU	2(R3), R5
	VMOV	R5, V8.B[0]
	MOVBU	1(R3), R5
	VMOV	R5, V8.B[1]
	MOVBU	0(R3), R5
	VMOV	R5, V8.B[2]
	VMOV	R4, V8.B[3]
	MOVBU	0(R2), R5
	VMOV	R5, V8.B[4]
	MOVBU	1(R2), R5
	VMOV	R5, V8.B[5]
	MOVBU	2(R2), R5
	VMOV	R5, V8.B[6]
	MOVBU	3(R2), R5
	VMOV	R5, V8.B[7]

	// Compute d_k (avg3 windows over V8 lanes 0..5).
	VEXT	$1, V8.B16, V8.B16, V20.B16
	VEXT	$2, V8.B16, V8.B16, V21.B16
	VUXTL	V8.B8, V10.H8
	VUXTL	V20.B8, V11.H8
	VUXTL	V21.B8, V12.H8
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h
	// V13.B[0..5] = d0..d5

	// Compute e_k (avg2 windows over T = [x a b c d] = V8.B[3..7])
	// via int16 lanes: e = ((T[k] + T[k+1] + 1) >> 1).
	VEXT	$3, V8.B16, V8.B16, V24.B16     // V24.B[0..4] = x a b c d
	VEXT	$1, V24.B16, V24.B16, V25.B16   // shifted by 1
	VUXTL	V24.B8, V24.H8
	VUXTL	V25.B8, V25.H8
	VADD	V24.H8, V25.H8, V22.H8
	MOVD	$1, R9
	VDUP	R9, V23.H8
	VADD	V22.H8, V23.H8, V22.H8
	VUSHR	$1, V22.H8, V22.H8
	WORD	$0x2e212ad6             // sqxtun v22.8b, v22.8h
	// V22.B[0..3] = e0 e1 e2 e3

	// Now assemble rows:
	// row0 = e0 e1 e2 e3                        -> V22.S[0]
	// row1 = d2 d3 d4 d5                        -> shift d by 2
	// row2 = d1 e0 e1 e2                        -> shift d by 1 (lane 0=d1), then patch lanes 1..3 with e0..e2
	// row3 = d0 d2 d3 d4                        -> lane 0=d0, lanes 1..3=d2..d4
	//
	// Easier: extract scalars and pack into R registers.

	VMOV	V22.S[0], R5            // row0 = e0..e3
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row1 = d2 d3 d4 d5: VEXT V13 by 2.
	VEXT	$2, V13.B16, V13.B16, V14.B16
	VMOV	V14.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row2 = d1, e0, e1, e2.
	VMOV	V13.B[1], R5            // d1
	AND	$0xff, R5, R5
	VMOV	V22.B[0], R6            // e0
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[1], R6            // e1
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[2], R6            // e2
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row3 = d0, d2, d3, d4.
	VMOV	V13.B[0], R5            // d0
	AND	$0xff, R5, R5
	VMOV	V13.B[2], R6            // d2
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[3], R6            // d3
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[4], R6            // d4
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	RET

// intra4x4VLPredictNEON ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   above+16(FP) *byte
//
// VL uses only `above` (8 bytes a..h):
//   row0 (even): avg2(a,b) avg2(b,c) avg2(c,d) avg2(d,e)
//   row1 (odd):  avg3(a,b,c) avg3(b,c,d) avg3(c,d,e) avg3(d,e,f)
//   row2 (even): avg2(b,c) avg2(c,d) avg2(d,e) avg3(e,f,g)   <-- last is avg3
//   row3 (odd):  avg3(b,c,d) avg3(c,d,e) avg3(d,e,f) avg3(f,g,h)
//
// Compute d_k = avg3(A[k], A[k+1], A[k+2]) for k = 0..5; e_k =
// avg2(A[k], A[k+1]) for k = 0..3.
//
// Per the scalar reference, row2[3] = avg3(e,f,g) = d4 and row3[3] =
// avg3(f,g,h) = d5; row1[3] = avg3(d,e,f) = d3. So only d0..d5 are
// needed, no special boundary lane.
//
// row0 = e0 e1 e2 e3
// row1 = d0 d1 d2 d3
// row2 = e1 e2 e3 d4
// row3 = d1 d2 d3 d5
TEXT ·intra4x4VLPredictNEON(SB), NOSPLIT, $0-24
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2

	FMOVD	(R2), F8                      // V8.B8 = above[0..7]
	VEXT	$1, V8.B16, V8.B16, V20.B16
	VEXT	$2, V8.B16, V8.B16, V21.B16

	VUXTL	V8.B8, V10.H8
	VUXTL	V20.B8, V11.H8
	VUXTL	V21.B8, V12.H8
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h
	// V13.B[0..5] = d0..d5.

	// Compute e_k = avg2(A[k], A[k+1]) for k = 0..3 via int16 lanes
	// reusing the already-widened windows V10 (a) and V11 (b).
	VADD	V10.H8, V11.H8, V22.H8
	MOVD	$1, R9
	VDUP	R9, V23.H8
	VADD	V22.H8, V23.H8, V22.H8
	VUSHR	$1, V22.H8, V22.H8
	WORD	$0x2e212ad6             // sqxtun v22.8b, v22.8h
	// V22.B[0..3] = e0..e3

	// row0 = e0 e1 e2 e3
	VMOV	V22.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row1 = d0 d1 d2 d3
	VMOV	V13.S[0], R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row2 = e1 e2 e3 d4
	VMOV	V22.B[1], R5
	AND	$0xff, R5, R5
	VMOV	V22.B[2], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[3], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[4], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row3 = d1 d2 d3 d5
	VMOV	V13.B[1], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[2], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[3], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[5], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	RET

// intra4x4HDPredictNEON ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// HD layout (from scalar, dst[r,c]):
//   row0 = avg2(i,x), avg3(i,x,a),  avg3(x,a,b),  avg3(a,b,c)
//   row1 = avg2(j,i), avg3(j,i,x),  avg2(i,x),    avg3(i,x,a)
//   row2 = avg2(k,j), avg3(k,j,i),  avg2(j,i),    avg3(j,i,x)
//   row3 = avg2(l,k), avg3(l,k,j),  avg2(k,j),    avg3(k,j,i)
//
// Build S = [l, k, j, i, x, a, b, c] (length 8).
// d_k = avg3(S[k], S[k+1], S[k+2]) for k = 0..5:
//   d0 = avg3(l, k, j)
//   d1 = avg3(k, j, i)
//   d2 = avg3(j, i, x)
//   d3 = avg3(i, x, a)
//   d4 = avg3(x, a, b)
//   d5 = avg3(a, b, c)
// e_k = avg2(S[k], S[k+1]) for k = 0..3 (using S[0..4] = l,k,j,i,x):
//   e0 = avg2(l, k)
//   e1 = avg2(k, j)
//   e2 = avg2(j, i)
//   e3 = avg2(i, x)
//
// row0 = e3 d3 d4 d5
// row1 = e2 d2 e3 d3
// row2 = e1 d1 e2 d2
// row3 = e0 d0 e1 d1
TEXT ·intra4x4HDPredictNEON(SB), NOSPLIT, $0-33
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3
	MOVBU	topLeft+32(FP), R4

	// V8 = [l k j i x a b c]
	MOVBU	3(R3), R5
	VMOV	R5, V8.B[0]
	MOVBU	2(R3), R5
	VMOV	R5, V8.B[1]
	MOVBU	1(R3), R5
	VMOV	R5, V8.B[2]
	MOVBU	0(R3), R5
	VMOV	R5, V8.B[3]
	VMOV	R4, V8.B[4]
	MOVBU	0(R2), R5
	VMOV	R5, V8.B[5]
	MOVBU	1(R2), R5
	VMOV	R5, V8.B[6]
	MOVBU	2(R2), R5
	VMOV	R5, V8.B[7]

	VEXT	$1, V8.B16, V8.B16, V20.B16
	VEXT	$2, V8.B16, V8.B16, V21.B16
	VUXTL	V8.B8, V10.H8
	VUXTL	V20.B8, V11.H8
	VUXTL	V21.B8, V12.H8
	// d windows
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h
	// V13.B[0..5] = d0..d5

	// e windows: avg2 over V8 lanes 0..3 with V20 lanes 0..3.
	VADD	V10.H8, V11.H8, V22.H8
	MOVD	$1, R9
	VDUP	R9, V23.H8
	VADD	V22.H8, V23.H8, V22.H8
	VUSHR	$1, V22.H8, V22.H8
	WORD	$0x2e212ad6             // sqxtun v22.8b, v22.8h
	// V22.B[0..3] = e0..e3

	// Pack rows:
	// row0 = e3 d3 d4 d5
	VMOV	V22.B[3], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[3], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[4], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[5], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row1 = e2 d2 e3 d3
	VMOV	V22.B[2], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[2], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[3], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[3], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row2 = e1 d1 e2 d2
	VMOV	V22.B[1], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[1], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[2], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[2], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row3 = e0 d0 e1 d1
	VMOV	V22.B[0], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[0], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[1], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[1], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	RET

// intra4x4HUPredictNEON ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   left+16(FP)  *byte
//
// HU uses only `left` (4 bytes i,j,k,l):
//   row0 = avg2(i,j), avg3(i,j,k), avg2(j,k), avg3(j,k,l)
//   row1 = avg2(j,k), avg3(j,k,l), avg2(k,l), avg3(k,l,l)
//   row2 = avg2(k,l), avg3(k,l,l), l, l
//   row3 = l, l, l, l
//
// Build S = [i, j, k, l, l, l] (length 6).
// d_k = avg3(S[k], S[k+1], S[k+2]) for k = 0..3:
//   d0 = avg3(i, j, k)
//   d1 = avg3(j, k, l)
//   d2 = avg3(k, l, l)
//   d3 = avg3(l, l, l) = l   (unused)
// e_k = avg2(S[k], S[k+1]) for k = 0..3:
//   e0 = avg2(i, j)
//   e1 = avg2(j, k)
//   e2 = avg2(k, l)
//   e3 = avg2(l, l) = l      (unused)
//
// row0 = e0 d0 e1 d1
// row1 = e1 d1 e2 d2
// row2 = e2 d2 l  l
// row3 = l  l  l  l
TEXT ·intra4x4HUPredictNEON(SB), NOSPLIT, $0-24
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	left+16(FP), R2

	FMOVS	(R2), F0                // V0.B8 = [i j k l 0 0 0 0]
	// Build V8.B[0..5] = [i j k l l l]
	VMOV	V0.S[0], R5
	AND	$0xff, R5, R6
	VMOV	R6, V8.B[0]
	LSR	$8, R5, R6
	AND	$0xff, R6, R6
	VMOV	R6, V8.B[1]
	LSR	$16, R5, R6
	AND	$0xff, R6, R6
	VMOV	R6, V8.B[2]
	LSR	$24, R5, R6
	AND	$0xff, R6, R6
	VMOV	R6, V8.B[3]
	VMOV	R6, V8.B[4]             // duplicate l
	VMOV	R6, V8.B[5]             // duplicate l

	VEXT	$1, V8.B16, V8.B16, V20.B16
	VEXT	$2, V8.B16, V8.B16, V21.B16
	VUXTL	V8.B8, V10.H8
	VUXTL	V20.B8, V11.H8
	VUXTL	V21.B8, V12.H8
	// d windows
	VADD	V11.H8, V11.H8, V13.H8
	VADD	V13.H8, V10.H8, V13.H8
	VADD	V13.H8, V12.H8, V13.H8
	MOVD	$2, R9
	VDUP	R9, V14.H8
	VADD	V13.H8, V14.H8, V13.H8
	VUSHR	$2, V13.H8, V13.H8
	WORD	$0x2e2129ad             // sqxtun v13.8b, v13.8h
	// V13.B[0..3] = d0..d3 (d3 = l)

	// e windows
	VADD	V10.H8, V11.H8, V22.H8
	MOVD	$1, R9
	VDUP	R9, V23.H8
	VADD	V22.H8, V23.H8, V22.H8
	VUSHR	$1, V22.H8, V22.H8
	WORD	$0x2e212ad6             // sqxtun v22.8b, v22.8h
	// V22.B[0..3] = e0..e3 (e3 = l)

	// Extract l for row2/row3.
	VMOV	V8.B[3], R7             // l
	AND	$0xff, R7, R7

	// row0 = e0 d0 e1 d1
	VMOV	V22.B[0], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[0], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[1], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[1], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row1 = e1 d1 e2 d2
	VMOV	V22.B[1], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[1], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	VMOV	V22.B[2], R6
	AND	$0xff, R6, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	VMOV	V13.B[2], R6
	AND	$0xff, R6, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row2 = e2 d2 l l
	VMOV	V22.B[2], R5
	AND	$0xff, R5, R5
	VMOV	V13.B[2], R6
	AND	$0xff, R6, R6
	LSL	$8, R6, R6
	ORR	R6, R5, R5
	MOVD	R7, R6
	LSL	$16, R6, R6
	ORR	R6, R5, R5
	MOVD	R7, R6
	LSL	$24, R6, R6
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	ADD	R1, R0, R0

	// row3 = l l l l
	MOVD	R7, R5
	MOVD	R5, R6
	LSL	$8, R5, R5
	ORR	R6, R5, R5
	MOVD	R5, R6
	LSL	$16, R5, R5
	ORR	R6, R5, R5
	MOVW	R5, (R0)
	RET
