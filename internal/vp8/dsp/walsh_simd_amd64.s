//go:build amd64 && !purego

// SSE2 port of libvpx v1.16.0 vp8/common/x86/iwalsh_sse2.asm
// (vp8_short_inv_walsh4x4_sse2).
//
// Layout: rows 0..1 packed in X0 (8 int16), rows 2..3 in X1.
// Pass 1 is "rows-paired butterfly" — PSHUFD swaps row 2 and row 3 in X1
// so PADDW gives lane[0..3]=a1 (row0+row3), lane[4..7]=b1 (row1+row2),
// and PSUBW gives lane[0..3]=d1 (row0-row3), lane[4..7]=c1 (row1-row2).
// PUNPCKLQDQ / PUNPCKHQDQ then re-pair into [a1,d1] and [b1,c1] which add
// and subtract to produce intermediate output rows 0..3 packed in two xmm
// regs.
//
// The 16-bit transpose is done with PUNPCKLWL/HWL (= PUNPCKLWD/HWD) in
// two stages, after which X4 holds (col 0 in low 4 lanes, col 1 in high 4
// lanes) and X1 holds (col 2 in low, col 3 in high). Pass 2 re-uses pass-1
// structure on those columns. Final +3 / >>3 (signed) and stride-16 stores
// match the libvpx scalar reference exactly.
//
// Output is byte-identical to inverseWalsh4x4Scalar for the decoder's
// coefficient range.

#include "textflag.h"

// 8 x int16 of 3 (column-pass +3 bias before >>3).
DATA  iwalsh_threes<>+0x00(SB)/4, $0x00030003
DATA  iwalsh_threes<>+0x04(SB)/4, $0x00030003
DATA  iwalsh_threes<>+0x08(SB)/4, $0x00030003
DATA  iwalsh_threes<>+0x0c(SB)/4, $0x00030003
GLOBL iwalsh_threes<>(SB), RODATA|NOPTR, $16

// inverseWalsh4x4SSE2 ABI ($0-16):
//   input+0(FP)      *int16  (16 lanes packed row-major)
//   mbDQCoeff+8(FP)  *int16  (destination, written at stride 16 elements)
TEXT ·inverseWalsh4x4SSE2(SB), NOSPLIT, $0-16
	MOVQ	input+0(FP), AX
	MOVQ	mbDQCoeff+8(FP), DI

	// Load rows 0..1 in X0, rows 2..3 in X1 (8 int16 each, low->high).
	MOVOU	0(AX), X0       // X0 = [row0, row1]   (8 int16 lanes)
	MOVOU	16(AX), X1      // X1 = [row2, row3]

	// === Pass 1 ===
	// Swap halves of X1 so it becomes [row3, row2].
	PSHUFD	$0x4e, X1, X2   // X2 = [row3, row2]
	MOVO	X0, X3          // X3 = [row0, row1]

	PADDW	X2, X0          // X0 = [row0+row3, row1+row2] = [a1 (4 lanes), b1 (4 lanes)]
	PSUBW	X2, X3          // X3 = [row0-row3, row1-row2] = [d1 (4 lanes), c1 (4 lanes)]

	MOVO	X0, X4          // X4 = [a1, b1]
	PUNPCKLQDQ	X3, X0  // X0 = [a1, d1]   (a1 in low 64, d1 in high 64)
	PUNPCKHQDQ	X3, X4  // X4 = [b1, c1]
	MOVO	X4, X1          // X1 = [b1, c1]
	PADDW	X0, X4          // X4 = [a1+b1, d1+c1] = [out_row0(0..3), out_row1(0..3)]
	PSUBW	X1, X0          // X0 = [a1-b1, d1-c1] = [out_row2(0..3), out_row3(0..3)]

	// === Transpose (16-bit lanes) so each xmm register holds (col c in
	// low half, col c+1 in high half), with lanes 0..3 = rows 0..3. ===
	// X4 = (lane 0..7) = (out_row0[0..3], out_row1[0..3])
	// X0 = (out_row2[0..3], out_row3[0..3])
	MOVO	X4, X3          // X3 = [out_row0, out_row1]
	PUNPCKLWL	X0, X4  // X4 = interleave low 4 lanes of X4 (out_row0) with X0 (out_row2):
	                        //   = [r0c0, r2c0, r0c1, r2c1, r0c2, r2c2, r0c3, r2c3]
	PUNPCKHWL	X0, X3  // X3 = interleave high 4 lanes of X4 (out_row1) with X0 (out_row3):
	                        //   = [r1c0, r3c0, r1c1, r3c1, r1c2, r3c2, r1c3, r3c3]
	MOVO	X4, X1          // X1 = X4 (saved)
	PUNPCKLWL	X3, X4  // X4 = interleave low 4 lanes of [r0c0,r2c0,r0c1,r2c1] with [r1c0,r3c0,r1c1,r3c1]
	                        //   = [r0c0, r1c0, r2c0, r3c0, r0c1, r1c1, r2c1, r3c1]
	                        //   = [col 0 (lanes 0..3 = rows 0..3), col 1 (lanes 4..7 = rows 0..3)]
	PUNPCKHWL	X3, X1  // X1 = [col 2, col 3]

	// === Pass 2 (rows-paired butterfly on cols, lanes = rows) ===
	PSHUFD	$0x4e, X1, X2   // X2 = [col 3, col 2]
	MOVO	X4, X3          // X3 = [col 0, col 1]

	PADDW	X2, X4          // X4 = [col0+col3, col1+col2] = [a1, b1]  (lane k = row k)
	PSUBW	X2, X3          // X3 = [col0-col3, col1-col2] = [d1, c1]

	MOVO	X4, X5          // X5 = [a1, b1]
	PUNPCKLQDQ	X3, X4  // X4 = [a1, d1]
	PUNPCKHQDQ	X3, X5  // X5 = [b1, c1]
	MOVO	X5, X1          // X1 = [b1, c1]
	PADDW	X4, X5          // X5 = [a1+b1, d1+c1] = [a2 (4 rows), b2 (4 rows)]
	PSUBW	X1, X4          // X4 = [a1-b1, d1-c1] = [c2 (4 rows), d2 (4 rows)]

	// Add 3 and signed-shift right 3.
	MOVO	iwalsh_threes<>(SB), X0
	PADDW	X0, X5
	PADDW	X0, X4
	PSRAW	$3, X5          // X5 = [out_row[k][col 0], out_row[k][col 1]]
	PSRAW	$3, X4          // X4 = [out_row[k][col 2], out_row[k][col 3]]

	// === Stores ===
	// Lane mapping (each xmm has 8 int16 lanes):
	//   X5 lane k       = a2[k] = output[k*4 + 0]   (k = 0..3 = row 0..3)
	//   X5 lane (4+k)   = b2[k] = output[k*4 + 1]
	//   X4 lane k       = c2[k] = output[k*4 + 2]
	//   X4 lane (4+k)   = d2[k] = output[k*4 + 3]
	// Target: mbDQCoeff[i*16] = output[i] for i = 0..15. Byte stride = 32.
	//
	// libvpx interleaves: take MOVD (low 32 bits → ax) which gets lanes 0,1
	// at once, then PSRLDQ by 4 bytes shifts lanes; we follow the same pattern.

	MOVD	X5, AX          // AX = a2[0..1] (low 32: lo16 = a2[0], hi16 = a2[1])
	MOVD	X4, CX          // CX = c2[0..1]
	PSRLO	$4, X5          // X5 lanes 0..5 = original lanes 2..7 (a2[2..3], b2[0..3])
	PSRLO	$4, X4          // X4 lanes 0..5 = original lanes 2..7 (c2[2..3], d2[0..3])
	MOVW	AX, 0(DI)       // mbDQCoeff[0]   = a2[0] = output[0]   (row 0 col 0)
	MOVW	CX, 64(DI)      // mbDQCoeff[32]  = c2[0] = output[2]   (row 0 col 2)
	SHRL	$16, AX
	SHRL	$16, CX
	MOVW	AX, 128(DI)     // mbDQCoeff[64]  = a2[1] = output[4]   (row 1 col 0)
	MOVW	CX, 192(DI)     // mbDQCoeff[96]  = c2[1] = output[6]   (row 1 col 2)

	MOVD	X5, AX          // AX = a2[2..3]
	MOVD	X4, CX          // CX = c2[2..3]
	PSRLO	$4, X5          // X5 lanes 0..3 = b2[0..3]
	PSRLO	$4, X4          // X4 lanes 0..3 = d2[0..3]
	MOVW	AX, 256(DI)     // mbDQCoeff[128] = a2[2] = output[8]   (row 2 col 0)
	MOVW	CX, 320(DI)     // mbDQCoeff[160] = c2[2] = output[10]  (row 2 col 2)
	SHRL	$16, AX
	SHRL	$16, CX
	MOVW	AX, 384(DI)     // mbDQCoeff[192] = a2[3] = output[12]  (row 3 col 0)
	MOVW	CX, 448(DI)     // mbDQCoeff[224] = c2[3] = output[14]  (row 3 col 2)

	MOVD	X5, AX          // AX = b2[0..1]
	MOVD	X4, CX          // CX = d2[0..1]
	PSRLO	$4, X5
	PSRLO	$4, X4
	MOVW	AX, 32(DI)      // mbDQCoeff[16]  = b2[0] = output[1]   (row 0 col 1)
	MOVW	CX, 96(DI)      // mbDQCoeff[48]  = d2[0] = output[3]   (row 0 col 3)
	SHRL	$16, AX
	SHRL	$16, CX
	MOVW	AX, 160(DI)     // mbDQCoeff[80]  = b2[1] = output[5]   (row 1 col 1)
	MOVW	CX, 224(DI)     // mbDQCoeff[112] = d2[1] = output[7]   (row 1 col 3)

	MOVD	X5, AX          // AX = b2[2..3]
	MOVD	X4, CX          // CX = d2[2..3]
	MOVW	AX, 288(DI)     // mbDQCoeff[144] = b2[2] = output[9]   (row 2 col 1)
	MOVW	CX, 352(DI)     // mbDQCoeff[176] = d2[2] = output[11]  (row 2 col 3)
	SHRL	$16, AX
	SHRL	$16, CX
	MOVW	AX, 416(DI)     // mbDQCoeff[208] = b2[3] = output[13]  (row 3 col 1)
	MOVW	CX, 480(DI)     // mbDQCoeff[240] = d2[3] = output[15]  (row 3 col 3)
	RET
