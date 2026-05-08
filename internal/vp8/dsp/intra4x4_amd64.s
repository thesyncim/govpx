// SSE2 4x4 B_PRED intra-prediction kernels. Mirrors libvpx v1.16.0
// vp8/common/reconintra4x4.c per-mode formulas (exact AVG3 = (a + 2*b
// + c + 2) >> 2 and AVG2 = (a + b + 1) >> 1) with byte-identical
// output to the scalar reference in intra4x4.go.
//
// SSE2 is part of the x86-64 baseline so dispatch is unconditional.
// All kernels follow the same shape: build the source sequence in the
// low half of an XMM, generate shifted views via PSRLDQ, widen each
// to int16 with PUNPCKLBW against a zero register, do the arithmetic,
// PACKUSWB back to bytes, and store 4 bytes per row with MOVD.
//
// PAVGB gives exact AVG2 and is used where applicable. AVG3 uses the
// int16-lane path because the cheaper PAVGB(PAVGB(a, c), b) idiom is
// not bit-exact for the AVG3 definition VP8 reconintra4x4.c uses.

#include "textflag.h"

// Constants (built once per function via the canonical PINSRW idiom):
//   X_zero    : zero register for PUNPCKLBW widening.
//   X_two     : 0x0002 broadcast across 8 word lanes (for AVG3 +2).
//   X_one     : 0x0001 broadcast across 8 word lanes (for AVG2 +1).
//
// Each kernel rebuilds the constants it needs.

// intra4x4DCPredictSSE2 ABI ($0-32):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   above+16(FP) *byte
//   left+24(FP)  *byte
//
// dc = (sum(above[0..3]) + sum(left[0..3]) + 4) >> 3, broadcast to 4x4.
TEXT ·intra4x4DCPredictSSE2(SB), NOSPLIT, $0-32
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9

	// Sum 4 above + 4 left bytes via PSADBW against zero. PSADBW only
	// reads the low 8 bytes per 64-bit lane and writes the low 16-bit
	// field, but we have only 4 valid bytes; load via 32-bit MOVD.
	PXOR	X1, X1
	MOVL	(R8), AX
	MOVD	AX, X0
	PSADBW	X1, X0          // X0 low word = sum(above[0..3])
	MOVL	(R9), AX
	MOVD	AX, X2
	PSADBW	X1, X2          // X2 low word = sum(left[0..3])
	PADDD	X2, X0          // PADDD merges the 32-bit lane sums
	MOVD	X0, AX
	ADDQ	$4, AX
	SHRQ	$3, AX
	// Broadcast AX byte across 4 bytes.
	ANDQ	$0xff, AX
	MOVQ	AX, CX
	SHLQ	$8, CX
	ORQ	CX, AX
	MOVQ	AX, CX
	SHLQ	$16, CX
	ORQ	CX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI
	MOVL	AX, (DI)
	ADDQ	BX, DI
	MOVL	AX, (DI)
	ADDQ	BX, DI
	MOVL	AX, (DI)
	RET

// intra4x4TMPredictSSE2 ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// dst[y, x] = clip255(left[y] + above[x] - topLeft).
TEXT ·intra4x4TMPredictSSE2(SB), NOSPLIT, $0-33
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9
	MOVBQZX	topLeft+32(FP), AX

	PXOR	X4, X4

	// Broadcast topLeft to 8 word lanes in X3.
	MOVQ	AX, CX
	SHLQ	$16, CX
	ORQ	CX, AX
	MOVD	AX, X3
	PSHUFLW	$0, X3, X3
	PSHUFD	$0, X3, X3

	// Widen 4 above bytes to 4 word lanes in X1; subtract topLeft.
	MOVL	(R8), AX
	MOVD	AX, X1
	PUNPCKLBW X4, X1
	PSUBW	X3, X1

	// Widen 4 left bytes to 4 word lanes in X2.
	MOVL	(R9), AX
	MOVD	AX, X2
	PUNPCKLBW X4, X2

	// For each of 4 rows, broadcast left[y] in word lanes and add to X1.
	// PSHUFLW with index = y replicates lane y into lanes 0..3; PSHUFD
	// $0 broadcasts the low 64 bits to the upper 64 bits (we don't
	// care about upper; 4 lanes are plenty).
	PSHUFLW	$0x00, X2, X5   // broadcast lane 0
	PADDW	X1, X5
	PACKUSWB X5, X5
	MOVL	X5, (DI)
	ADDQ	BX, DI

	PSHUFLW	$0x55, X2, X5   // broadcast lane 1
	PADDW	X1, X5
	PACKUSWB X5, X5
	MOVL	X5, (DI)
	ADDQ	BX, DI

	PSHUFLW	$0xAA, X2, X5   // broadcast lane 2
	PADDW	X1, X5
	PACKUSWB X5, X5
	MOVL	X5, (DI)
	ADDQ	BX, DI

	PSHUFLW	$0xFF, X2, X5   // broadcast lane 3
	PADDW	X1, X5
	PACKUSWB X5, X5
	MOVL	X5, (DI)
	RET

// avg3 helper macro (in comments): given X_a, X_b, X_c (int16 lanes),
// X_two preset to 0x0002 per lane, X_zero zero, compute
// X_d = ((X_a + X_b + X_b + X_c + 2) >> 2) packed back to bytes.
// Implemented inline per kernel because Go amd64 assembler does not
// support GAS-style macros.

// intra4x4VEPredictSSE2 ABI ($0-25):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   topLeft+24(FP) byte
//
// row = [ avg3(tl, A0, A1), avg3(A0, A1, A2), avg3(A1, A2, A3), avg3(A2, A3, A4) ]
// replicated across 4 rows.
TEXT ·intra4x4VEPredictSSE2(SB), NOSPLIT, $0-25
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVBQZX	topLeft+24(FP), AX

	PXOR	X7, X7

	// Build constant X_two = 0x0002 across 8 word lanes.
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Build sequence S = [tl, A0, A1, A2, A3, A4] in low 6 bytes of X0.
	MOVL	(R8), CX                // CX = A0..A3
	SHLQ	$8, CX
	ORQ	AX, CX                  // CX = [tl A0 A1 A2 A3 0 0 0]
	MOVQ	CX, R10
	MOVBQZX	4(R8), DX               // DX = A4
	SHLQ	$40, DX                 // place A4 at byte index 5
	ORQ	DX, R10
	MOVQ	R10, X0
	// X0 low 6 bytes = [tl A0 A1 A2 A3 A4]; rest 0.

	// X1 = X0 shifted right by 1 byte (a-window vs b-window).
	MOVO	X0, X1
	PSRLDQ	$1, X1
	// X2 = X0 shifted right by 2 bytes (c-window).
	MOVO	X0, X2
	PSRLDQ	$2, X2

	// Widen to int16 lanes via PUNPCKLBW.
	PUNPCKLBW X7, X0   // a window: tl A0 A1 A2 A3 A4 0 0
	PUNPCKLBW X7, X1   // b window: A0 A1 A2 A3 A4 0 0 0
	PUNPCKLBW X7, X2   // c window: A1 A2 A3 A4 0 0 0 0

	PADDW	X1, X1     // 2*b
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..3] = the 4 row values.
	MOVD	X1, AX

	MOVL	AX, (DI)
	ADDQ	BX, DI
	MOVL	AX, (DI)
	ADDQ	BX, DI
	MOVL	AX, (DI)
	ADDQ	BX, DI
	MOVL	AX, (DI)
	RET

// intra4x4HEPredictSSE2 ABI ($0-25):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   left+16(FP)  *byte
//   topLeft+24(FP) byte
//
// rows: each filled with avg3(prev, mid, next) - with prev=tl/L0/L1/L2,
// mid=L0/L1/L2/L3, next=L1/L2/L3/L3.
TEXT ·intra4x4HEPredictSSE2(SB), NOSPLIT, $0-25
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	left+16(FP), R8
	MOVBQZX	topLeft+24(FP), AX

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Build S = [tl, L0, L1, L2, L3, L3] in low 6 bytes of X0.
	MOVL	(R8), CX                // CX = L0 L1 L2 L3
	MOVQ	CX, R10
	SHLQ	$8, R10
	ORQ	AX, R10                  // R10 = [tl L0 L1 L2 L3 0 0 0]
	MOVQ	CX, R11
	SHRQ	$24, R11                 // R11 low byte = L3
	ANDQ	$0xff, R11
	SHLQ	$40, R11                  // L3 at byte index 5
	ORQ	R11, R10
	MOVQ	R10, X0

	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..3] = row0..row3 fill values.
	MOVD	X1, AX
	MOVQ	AX, R10

	// row0: broadcast byte 0 of AX.
	MOVQ	AX, R11
	ANDQ	$0xff, R11
	MOVQ	R11, R12
	SHLQ	$8, R12
	ORQ	R12, R11
	MOVQ	R11, R12
	SHLQ	$16, R12
	ORQ	R12, R11
	MOVL	R11, (DI)
	ADDQ	BX, DI

	// row1: byte 1.
	MOVQ	R10, R11
	SHRQ	$8, R11
	ANDQ	$0xff, R11
	MOVQ	R11, R12
	SHLQ	$8, R12
	ORQ	R12, R11
	MOVQ	R11, R12
	SHLQ	$16, R12
	ORQ	R12, R11
	MOVL	R11, (DI)
	ADDQ	BX, DI

	// row2: byte 2.
	MOVQ	R10, R11
	SHRQ	$16, R11
	ANDQ	$0xff, R11
	MOVQ	R11, R12
	SHLQ	$8, R12
	ORQ	R12, R11
	MOVQ	R11, R12
	SHLQ	$16, R12
	ORQ	R12, R11
	MOVL	R11, (DI)
	ADDQ	BX, DI

	// row3: byte 3.
	MOVQ	R10, R11
	SHRQ	$24, R11
	ANDQ	$0xff, R11
	MOVQ	R11, R12
	SHLQ	$8, R12
	ORQ	R12, R11
	MOVQ	R11, R12
	SHLQ	$16, R12
	ORQ	R12, R11
	MOVL	R11, (DI)
	RET

// intra4x4LDPredictSSE2 ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   above+16(FP) *byte
//
// d[k] = avg3(A[k], A[k+1], A[k+2]) for k=0..5; row r col c = d[r+c]
// except dst[3,3] = avg3(g, h, h).
TEXT ·intra4x4LDPredictSSE2(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Load 8 above bytes into low 8 of X0; ensure byte 8 = A7 (so the
	// k=6 window uses (a,b,c) = (A6, A7, A7), giving d6 = avg3(g,h,h)).
	MOVQ	(R8), AX
	MOVQ	AX, X0          // X0 low 8 bytes = above[0..7]
	// Read A7 as byte and place at byte index 8 of X0.
	MOVBQZX	7(R8), CX
	PINSRW	$4, CX, X0      // word lane 4 (= bytes 8..9) low byte = A7. high byte garbage.
	// We only need byte 8 to be A7; byte 9 is the c-window extension at
	// offset 9 which is past d6, so we don't care.

	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..6] = d0..d6.

	// Row 0: d0 d1 d2 d3 -> X1 lower 4 bytes.
	MOVD	X1, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// Row 1: d1 d2 d3 d4 -> shift X1 right 1 byte.
	MOVO	X1, X2
	PSRLDQ	$1, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// Row 2: d2 d3 d4 d5 -> shift X1 right 2 bytes.
	MOVO	X1, X2
	PSRLDQ	$2, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// Row 3: d3 d4 d5 d6 -> shift X1 right 3 bytes.
	MOVO	X1, X2
	PSRLDQ	$3, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	RET

// intra4x4RDPredictSSE2 ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// Build S = [l, k, j, i, x, a, b, c, d] (9 bytes); compute d[k]
// = avg3(S[k], S[k+1], S[k+2]) for k=0..6. Then:
//   row0 = d3 d4 d5 d6
//   row1 = d2 d3 d4 d5
//   row2 = d1 d2 d3 d4
//   row3 = d0 d1 d2 d3
TEXT ·intra4x4RDPredictSSE2(SB), NOSPLIT, $0-33
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9
	MOVBQZX	topLeft+32(FP), R10

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Build S in CX (bytes 0..7) and DX (byte 8).
	// byte0 = l = left[3]
	MOVBQZX	3(R9), AX
	MOVQ	AX, CX
	MOVBQZX	2(R9), AX        // k
	SHLQ	$8, AX
	ORQ	AX, CX
	MOVBQZX	1(R9), AX        // j
	SHLQ	$16, AX
	ORQ	AX, CX
	MOVBQZX	(R9), AX         // i
	SHLQ	$24, AX
	ORQ	AX, CX
	MOVQ	R10, AX           // x
	SHLQ	$32, AX
	ORQ	AX, CX
	MOVBQZX	(R8), AX         // a
	SHLQ	$40, AX
	ORQ	AX, CX
	MOVBQZX	1(R8), AX        // b
	SHLQ	$48, AX
	ORQ	AX, CX
	MOVBQZX	2(R8), AX        // c
	SHLQ	$56, AX
	ORQ	AX, CX
	// d (byte 8) goes via PINSRW into word lane 4.
	MOVQ	CX, X0
	MOVBQZX	3(R8), AX        // d
	PINSRW	$4, AX, X0        // word lane 4 low byte = d; high byte garbage (unused).

	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..6] = d0..d6.

	// row0 = d3 d4 d5 d6 (shift right 3)
	MOVO	X1, X2
	PSRLDQ	$3, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row1 = d2 d3 d4 d5 (shift right 2)
	MOVO	X1, X2
	PSRLDQ	$2, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row2 = d1 d2 d3 d4 (shift right 1)
	MOVO	X1, X2
	PSRLDQ	$1, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row3 = d0 d1 d2 d3 (no shift)
	MOVD	X1, AX
	MOVL	AX, (DI)
	RET

// intra4x4VRPredictSSE2 ABI ($0-33):
//   See intra4x4_arm64.s VR comments for the full layout discussion.
//
// Build S = [k, j, i, x, a, b, c, d] (8 bytes); the d-window at k=0..5
// then gives:
//   d0 = avg3(k,j,i)  d1 = avg3(j,i,x)  d2 = avg3(i,x,a)
//   d3 = avg3(x,a,b)  d4 = avg3(a,b,c)  d5 = avg3(b,c,d)
// Build T = [x, a, b, c, d] -> e_k = avg2(T[k], T[k+1]) for k=0..3.
//
// row0 = e0 e1 e2 e3
// row1 = d2 d3 d4 d5
// row2 = d1 e0 e1 e2
// row3 = d0 d2 d3 d4
TEXT ·intra4x4VRPredictSSE2(SB), NOSPLIT, $0-33
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9
	MOVBQZX	topLeft+32(FP), R10

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Build S in CX (8 bytes). Layout: S[0]=k, S[1]=j, S[2]=i, S[3]=x,
	// S[4]=a, S[5]=b, S[6]=c, S[7]=d.
	MOVBQZX	2(R9), AX         // k
	MOVQ	AX, CX
	MOVBQZX	1(R9), AX         // j
	SHLQ	$8, AX
	ORQ	AX, CX
	MOVBQZX	(R9), AX          // i
	SHLQ	$16, AX
	ORQ	AX, CX
	MOVQ	R10, AX            // x
	SHLQ	$24, AX
	ORQ	AX, CX
	MOVBQZX	(R8), AX          // a
	SHLQ	$32, AX
	ORQ	AX, CX
	MOVBQZX	1(R8), AX         // b
	SHLQ	$40, AX
	ORQ	AX, CX
	MOVBQZX	2(R8), AX         // c
	SHLQ	$48, AX
	ORQ	AX, CX
	MOVBQZX	3(R8), AX         // d
	SHLQ	$56, AX
	ORQ	AX, CX
	MOVQ	CX, X0

	// d-window via PSRLDQ + PUNPCKLBW.
	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..5] = d0..d5.

	// e-window: T = S>>3. Use PAVGB on T-shifted-by-1 against T.
	MOVQ	CX, R11
	SHRQ	$24, R11               // R11 low 5 bytes = [x a b c d]
	MOVQ	R11, X3                 // X3 low byte 0..4 = T
	MOVO	X3, X4
	PSRLDQ	$1, X4
	PAVGB	X4, X3
	// X3.byte[0..3] = e0..e3 (PAVGB is exact AVG2).

	// row0 = e0 e1 e2 e3
	MOVD	X3, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row1 = d2 d3 d4 d5  (X1 shifted right by 2 bytes)
	MOVO	X1, X2
	PSRLDQ	$2, X2
	MOVD	X2, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row2 = d1, e0, e1, e2.
	// Build via byte-level extract through GP regs.
	MOVD	X1, AX
	MOVD	X3, R11
	SHRQ	$8, AX                  // AX low byte = d1
	ANDQ	$0xff, AX
	MOVQ	R11, R12
	SHLQ	$8, R12
	ANDQ	$0xffffff00, R12        // R12 = (e0 e1 e2 e3) << 8 -> bytes 1..3 = e0 e1 e2
	ORQ	R12, AX
	ANDQ	$0xffffffff, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row3 = d0, d2, d3, d4.
	MOVD	X1, AX
	MOVQ	AX, R11
	ANDQ	$0xff, R11               // R11 = d0
	SHRQ	$16, AX                  // AX low 3 bytes = d2 d3 d4
	ANDQ	$0xffffff, AX
	SHLQ	$8, AX                   // shift to bytes 1..3
	ORQ	R11, AX
	MOVL	AX, (DI)
	RET

// intra4x4VLPredictSSE2 ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   above+16(FP) *byte
//
// VL: only above (8 bytes a..h).
//   d_k = avg3(A[k], A[k+1], A[k+2]) for k=0..5
//   e_k = avg2(A[k], A[k+1]) for k=0..3
//   row0 = e0 e1 e2 e3
//   row1 = d0 d1 d2 d3
//   row2 = e1 e2 e3 d4
//   row3 = d1 d2 d3 d5
TEXT ·intra4x4VLPredictSSE2(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Load 8 above bytes into low 8 of X0.
	MOVQ	(R8), AX
	MOVQ	AX, X0

	// d-window
	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..5] = d0..d5.

	// e-window via PAVGB (exact AVG2). Rebuild from raw `above` bytes.
	MOVQ	(R8), AX
	MOVQ	AX, X3
	MOVO	X3, X4
	PSRLDQ	$1, X4
	PAVGB	X4, X3
	// X3.byte[0..3] = e0..e3 (e3 = avg2(A3, A4)).

	// row0 = e0..e3
	MOVD	X3, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row1 = d0..d3
	MOVD	X1, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row2 = e1 e2 e3 d4.
	MOVD	X3, AX
	SHRQ	$8, AX                  // AX low 3 bytes = e1 e2 e3
	ANDQ	$0xffffff, AX
	MOVD	X1, R11
	SHRQ	$32, R11                 // R11 low byte = d4
	ANDQ	$0xff, R11
	SHLQ	$24, R11
	ORQ	R11, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row3 = d1 d2 d3 d5.
	MOVD	X1, AX
	SHRQ	$8, AX                  // AX low 3 bytes = d1 d2 d3
	ANDQ	$0xffffff, AX
	MOVD	X1, R11
	SHRQ	$40, R11                 // R11 low byte = d5
	ANDQ	$0xff, R11
	SHLQ	$24, R11
	ORQ	R11, AX
	MOVL	AX, (DI)
	RET

// intra4x4HDPredictSSE2 ABI ($0-33):
//   See intra4x4_arm64.s HD comments for the layout proof.
//
// Build S = [l, k, j, i, x, a, b, c] (8 bytes).
//   d0..d5 from avg3 windows over S[0..7].
//   e0..e3 from avg2(S[k], S[k+1]) for k=0..3 (so e3 = avg2(i, x)).
//
// row0 = e3 d3 d4 d5
// row1 = e2 d2 e3 d3
// row2 = e1 d1 e2 d2
// row3 = e0 d0 e1 d1
TEXT ·intra4x4HDPredictSSE2(SB), NOSPLIT, $0-33
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	above+16(FP), R8
	MOVQ	left+24(FP), R9
	MOVBQZX	topLeft+32(FP), R10

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Build S in CX (8 bytes).
	MOVBQZX	3(R9), AX         // l
	MOVQ	AX, CX
	MOVBQZX	2(R9), AX         // k
	SHLQ	$8, AX
	ORQ	AX, CX
	MOVBQZX	1(R9), AX         // j
	SHLQ	$16, AX
	ORQ	AX, CX
	MOVBQZX	(R9), AX          // i
	SHLQ	$24, AX
	ORQ	AX, CX
	MOVQ	R10, AX            // x
	SHLQ	$32, AX
	ORQ	AX, CX
	MOVBQZX	(R8), AX          // a
	SHLQ	$40, AX
	ORQ	AX, CX
	MOVBQZX	1(R8), AX         // b
	SHLQ	$48, AX
	ORQ	AX, CX
	MOVBQZX	2(R8), AX         // c
	SHLQ	$56, AX
	ORQ	AX, CX
	MOVQ	CX, X0

	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..5] = d0..d5.

	// e via PAVGB on S vs S>>1.
	MOVQ	CX, X3
	MOVO	X3, X4
	PSRLDQ	$1, X4
	PAVGB	X4, X3
	// X3.byte[0..3] = e0..e3 (e3 uses S[3]=i and S[4]=x => avg2(i,x)).

	// Pack rows. Use GP registers because we need interleaved
	// (e_k, d_j, e_k', d_j') sequences.
	MOVD	X1, R11        // R11 = d0 d1 d2 d3 (low 4 bytes)
	MOVD	X3, R12        // R12 = e0 e1 e2 e3
	// d4 d5 are in upper bytes of X1 (.byte[4], [5]).
	MOVD	X1, R13
	SHRQ	$32, R13        // R13 byte[0] = d4, byte[1] = d5

	// row0 = e3, d3, d4, d5.
	MOVQ	R12, AX
	SHRQ	$24, AX         // AX byte 0 = e3
	ANDQ	$0xff, AX
	MOVQ	R11, DX
	SHRQ	$24, DX         // DX byte 0 = d3
	ANDQ	$0xff, DX
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R13, DX
	ANDQ	$0xff, DX       // d4
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R13, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX       // d5
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row1 = e2 d2 e3 d3.
	MOVQ	R12, AX
	SHRQ	$16, AX
	ANDQ	$0xff, AX        // e2
	MOVQ	R11, DX
	SHRQ	$16, DX
	ANDQ	$0xff, DX        // d2
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R12, DX
	SHRQ	$24, DX
	ANDQ	$0xff, DX        // e3
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R11, DX
	SHRQ	$24, DX
	ANDQ	$0xff, DX        // d3
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row2 = e1 d1 e2 d2.
	MOVQ	R12, AX
	SHRQ	$8, AX
	ANDQ	$0xff, AX        // e1
	MOVQ	R11, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX        // d1
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R12, DX
	SHRQ	$16, DX
	ANDQ	$0xff, DX        // e2
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R11, DX
	SHRQ	$16, DX
	ANDQ	$0xff, DX        // d2
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row3 = e0 d0 e1 d1.
	MOVQ	R12, AX
	ANDQ	$0xff, AX        // e0
	MOVQ	R11, DX
	ANDQ	$0xff, DX        // d0
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R12, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX        // e1
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R11, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX        // d1
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	RET

// intra4x4HUPredictSSE2 ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   left+16(FP)  *byte
//
// Build S = [i, j, k, l, l, l] (6 bytes, l duplicated for the boundary
// avg3(k,l,l) = d2 and avg2(l,l) = e3 = l).
//   d0 = avg3(i, j, k)
//   d1 = avg3(j, k, l)
//   d2 = avg3(k, l, l)
//   d3 = avg3(l, l, l) = l       (unused)
//   e0 = avg2(i, j)
//   e1 = avg2(j, k)
//   e2 = avg2(k, l)
//   e3 = avg2(l, l) = l          (unused)
//
// row0 = e0 d0 e1 d1
// row1 = e1 d1 e2 d2
// row2 = e2 d2 l  l
// row3 = l  l  l  l
TEXT ·intra4x4HUPredictSSE2(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	stride+8(FP), BX
	MOVQ	left+16(FP), R8

	PXOR	X7, X7
	MOVL	$0x00020002, CX
	MOVD	CX, X6
	PSHUFD	$0, X6, X6

	// Build S = [i j k l l l 0 0] in CX.
	MOVL	(R8), AX                 // AX low 4 bytes = i j k l
	MOVQ	AX, CX
	MOVQ	AX, DX
	SHRQ	$24, DX                  // DX low byte = l
	ANDQ	$0xff, DX
	MOVQ	DX, R10
	SHLQ	$32, R10                 // l at byte 4
	ORQ	R10, CX
	SHLQ	$8, R10                   // l at byte 5
	ORQ	R10, CX
	MOVQ	CX, X0

	MOVO	X0, X1
	PSRLDQ	$1, X1
	MOVO	X0, X2
	PSRLDQ	$2, X2
	PUNPCKLBW X7, X0
	PUNPCKLBW X7, X1
	PUNPCKLBW X7, X2
	PADDW	X1, X1
	PADDW	X0, X1
	PADDW	X2, X1
	PADDW	X6, X1
	PSRLW	$2, X1
	PACKUSWB X1, X1
	// X1.byte[0..3] = d0..d3 (d3 = l).

	// e via PAVGB on S vs S>>1.
	MOVQ	CX, X3
	MOVO	X3, X4
	PSRLDQ	$1, X4
	PAVGB	X4, X3
	// X3.byte[0..3] = e0..e3 (e3 = l).

	// Pack via GP registers.
	MOVD	X1, R11        // R11 = d0..d3 (low 4 bytes)
	MOVD	X3, R12        // R12 = e0..e3
	MOVQ	DX, R13        // R13 = l (already in low byte)

	// row0 = e0 d0 e1 d1
	MOVQ	R12, AX
	ANDQ	$0xff, AX        // e0
	MOVQ	R11, DX
	ANDQ	$0xff, DX        // d0
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R12, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX        // e1
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R11, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX        // d1
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row1 = e1 d1 e2 d2
	MOVQ	R12, AX
	SHRQ	$8, AX
	ANDQ	$0xff, AX        // e1
	MOVQ	R11, DX
	SHRQ	$8, DX
	ANDQ	$0xff, DX        // d1
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R12, DX
	SHRQ	$16, DX
	ANDQ	$0xff, DX        // e2
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R11, DX
	SHRQ	$16, DX
	ANDQ	$0xff, DX        // d2
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row2 = e2 d2 l l
	MOVQ	R12, AX
	SHRQ	$16, AX
	ANDQ	$0xff, AX        // e2
	MOVQ	R11, DX
	SHRQ	$16, DX
	ANDQ	$0xff, DX        // d2
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	R13, DX
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVQ	R13, DX
	SHLQ	$24, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	ADDQ	BX, DI

	// row3 = l l l l
	MOVQ	R13, AX
	MOVQ	AX, DX
	SHLQ	$8, DX
	ORQ	DX, AX
	MOVQ	AX, DX
	SHLQ	$16, DX
	ORQ	DX, AX
	MOVL	AX, (DI)
	RET
