//go:build amd64 && !purego

// AVX2 (VEX-encoded) port of the libvpx v1.16.0 VP8 loop_filter and
// mb_loop_filter inner kernels (16-wide horizontal-edge form).
// Mirrors the SSE2 kernel in loopfilter_amd64.s but uses VEX-encoded
// non-destructive 3-operand instructions, which eliminates many of
// the per-instruction MOVOU copy slots in the SSE2 schedule and frees
// up scheduler bandwidth.
//
// VP8's horizontal-edge LF window is 16 columns wide so AVX2 does not
// give us 2x throughput out of the box (we'd need to process two
// edges in parallel, which would require larger ABI surgery). The win
// here is from reduced µop count: each PSUBUSB / POR pair in the
// SSE2 mask compute becomes a single VPSUBUSB / VPOR with a fresh
// destination register, no MOVOU copy.
//
// loopFilterEdgeH16AVX2  : libvpx vp8_loop_filter_horizontal_edge_sse2
//                         (writes p1, p0, q0, q1)
// mbLoopFilterEdgeH16AVX2: libvpx vp8_mbloop_filter_horizontal_edge_sse2
//                         (writes p2, p1, p0, q0, q1, q2)

#include "textflag.h"

// 16x byte 0xFE
DATA  lfAVX2Tfe<>+0x00(SB)/8, $0xFEFEFEFEFEFEFEFE
DATA  lfAVX2Tfe<>+0x08(SB)/8, $0xFEFEFEFEFEFEFEFE
GLOBL lfAVX2Tfe<>(SB), RODATA|NOPTR, $16

// 16x byte 0x80
DATA  lfAVX2T80<>+0x00(SB)/8, $0x8080808080808080
DATA  lfAVX2T80<>+0x08(SB)/8, $0x8080808080808080
GLOBL lfAVX2T80<>(SB), RODATA|NOPTR, $16

// 16x byte 0x03
DATA  lfAVX2T3<>+0x00(SB)/8, $0x0303030303030303
DATA  lfAVX2T3<>+0x08(SB)/8, $0x0303030303030303
GLOBL lfAVX2T3<>(SB), RODATA|NOPTR, $16

// 16x byte 0x04
DATA  lfAVX2T4<>+0x00(SB)/8, $0x0404040404040404
DATA  lfAVX2T4<>+0x08(SB)/8, $0x0404040404040404
GLOBL lfAVX2T4<>(SB), RODATA|NOPTR, $16

// 8x word 0x0001 (ones)
DATA  lfAVX2Ones<>+0x00(SB)/8, $0x0001000100010001
DATA  lfAVX2Ones<>+0x08(SB)/8, $0x0001000100010001
GLOBL lfAVX2Ones<>(SB), RODATA|NOPTR, $16

// 8x word 0x0900 (s9 — used so PMULHW with s9 yields Filter2*9 in
// 16-bit signed lanes when Filter2 is shifted left by 8).
DATA  lfAVX2S9<>+0x00(SB)/8, $0x0900090009000900
DATA  lfAVX2S9<>+0x08(SB)/8, $0x0900090009000900
GLOBL lfAVX2S9<>(SB), RODATA|NOPTR, $16

// 8x word 0x003F (s63)
DATA  lfAVX2S63<>+0x00(SB)/8, $0x003F003F003F003F
DATA  lfAVX2S63<>+0x08(SB)/8, $0x003F003F003F003F
GLOBL lfAVX2S63<>(SB), RODATA|NOPTR, $16

// loopFilterEdgeH16AVX2 ABI ($0-19, no stack):
//   src+0(FP)     *byte (points at p3 row, 16 contiguous bytes)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·loopFilterEdgeH16AVX2(SB), NOSPLIT, $0-19
	MOVQ	src+0(FP), AX
	MOVQ	pitch+8(FP), BX

	// Row pointers: AX=p3, R10=p2, R11=p1, R12=p0, R13=q0, SI=q1, R8=q2, R9=q3.
	MOVQ	AX, R10
	ADDQ	BX, R10
	MOVQ	R10, R11
	ADDQ	BX, R11
	MOVQ	R11, R12
	ADDQ	BX, R12
	MOVQ	R12, R13
	ADDQ	BX, R13
	MOVQ	R13, SI
	ADDQ	BX, SI
	MOVQ	SI, R8
	ADDQ	BX, R8
	MOVQ	R8, R9
	ADDQ	BX, R9

	// Pre-load all 8 rows.
	VMOVDQU	(AX),  X3                  // p3 (scratch — only used during mask)
	VMOVDQU	(R10), X4                  // p2 (scratch)
	VMOVDQU	(R11), X9                  // p1 (kept)
	VMOVDQU	(R12), X10                 // p0 (kept)
	VMOVDQU	(R13), X11                 // q0 (kept)
	VMOVDQU	(SI),  X12                 // q1 (kept)
	VMOVDQU	(R8),  X5                  // q2 (scratch)
	VMOVDQU	(R9),  X6                  // q3 (scratch)

	// Mask compute: build X1 = max of |p3-p2|,|p2-p1|,|p1-p0|,|q1-q0|,
	// |q2-q1|,|q3-q2|. Save |p1-p0| (-> X14 = t1) and |q1-q0|
	// (-> X13 = t0) for hev later.

	// |p3-p2|
	VPSUBUSB	X4, X3, X0
	VPSUBUSB	X3, X4, X2
	VPOR	X2, X0, X1                  // X1 = |p3-p2| = running max

	// |p2-p1|
	VPSUBUSB	X9, X4, X0
	VPSUBUSB	X4, X9, X2
	VPOR	X2, X0, X0                  // X0 = |p2-p1|
	VPMAXUB	X0, X1, X1

	// |p1-p0| -> X14 (t1)
	VPSUBUSB	X10, X9, X0
	VPSUBUSB	X9, X10, X2
	VPOR	X2, X0, X14                 // X14 = t1
	VPMAXUB	X14, X1, X1

	// |q1-q0| -> X13 (t0)
	VPSUBUSB	X11, X12, X0
	VPSUBUSB	X12, X11, X2
	VPOR	X2, X0, X13                 // X13 = t0
	VPMAXUB	X13, X1, X1

	// |q2-q1|
	VPSUBUSB	X12, X5, X0
	VPSUBUSB	X5, X12, X2
	VPOR	X2, X0, X0                  // |q2-q1|
	VPMAXUB	X0, X1, X1

	// |q3-q2|
	VPSUBUSB	X5, X6, X0
	VPSUBUSB	X6, X5, X2
	VPOR	X2, X0, X0                  // |q3-q2|
	VPMAXUB	X0, X1, X1                  // X1 = max-of-six

	// X1 -= limit (broadcast). Zero means OK.
	XORL	CX, CX
	MOVB	limit+17(FP), CL
	MOVQ	CX, X2
	VPUNPCKLBW X2, X2, X2
	VPSHUFLW $0, X2, X2
	VPSHUFD	$0, X2, X2                 // limit broadcast
	VPSUBUSB X2, X1, X1

	// Composite: (|p0-q0| * 2 sat) + |p1-q1|/2.
	VPSUBUSB	X12, X9, X0
	VPSUBUSB	X9, X12, X2
	VPOR	X2, X0, X0                  // |p1-q1|
	VPAND	lfAVX2Tfe<>(SB), X0, X0
	VPSRLW	$1, X0, X3                  // X3 = |p1-q1|/2

	VPSUBUSB	X11, X10, X0
	VPSUBUSB	X10, X11, X2
	VPOR	X2, X0, X0                  // |p0-q0|
	VPADDUSB	X0, X0, X0                  // *2 sat
	VPADDUSB	X3, X0, X0                  // + |p1-q1|/2

	XORL	CX, CX
	MOVB	blimit+16(FP), CL
	MOVQ	CX, X2
	VPUNPCKLBW X2, X2, X2
	VPSHUFLW $0, X2, X2
	VPSHUFD	$0, X2, X2                 // blimit broadcast
	VPSUBUSB X2, X0, X0
	VPOR	X0, X1, X1                  // X1 = 0 iff both checks pass

	VPXOR	X2, X2, X2
	VPCMPEQB X2, X1, X1                // X1 = filter_mask (0xFF = filter)

	// hev mask: (t0 -= thresh) | (t1 -= thresh) == 0 → ~hev; invert.
	XORL	CX, CX
	MOVB	thresh+18(FP), CL
	MOVQ	CX, X2
	VPUNPCKLBW X2, X2, X2
	VPSHUFLW $0, X2, X2
	VPSHUFD	$0, X2, X2                 // thresh broadcast
	VPSUBUSB X2, X13, X0                // t0 - thresh
	VPSUBUSB X2, X14, X3                // t1 - thresh
	VPADDB	X3, X0, X0                  // 0 iff both <= thresh (NOT-hev)
	VPXOR	X2, X2, X2
	VPCMPEQB X2, X0, X0                // 0xFF where NOT-hev
	VPCMPEQB X3, X3, X3                // all ones
	VPXOR	X3, X0, X0                  // X0 = hev mask (0xFF when hev)

	// Sign-convert p1, p0, q0, q1 in place.
	VMOVDQU	lfAVX2T80<>(SB), X3
	VPXOR	X3, X9,  X9                 // sps1
	VPXOR	X3, X10, X10                // sps0
	VPXOR	X3, X11, X11                // sqs0
	VPXOR	X3, X12, X12                // sqs1

	// fv = sat(sps1 - sqs1); fv &= hev
	VPSUBSB	X12, X9, X2                 // sat(sps1 - sqs1)
	VPAND	X0, X2, X2                  // & hev

	// fv += 3 * sat(sqs0 - sps0)
	VPSUBSB	X10, X11, X3                // sat(sqs0 - sps0)
	VPADDSB	X3, X2, X2
	VPADDSB	X3, X2, X2
	VPADDSB	X3, X2, X2                  // X2 = pre-mask vp8_filter
	VPAND	X1, X2, X2                  // & filter_mask

	// f1 = (fv + 4) >> 3, f2 = (fv + 3) >> 3 via punpck/psraw/pack
	VPADDSB	lfAVX2T3<>(SB), X2, X4      // fv+3
	VPADDSB	lfAVX2T4<>(SB), X2, X5      // fv+4

	// f2 (signed-saturating arithmetic shift right by 3)
	VPUNPCKLBW X4, X4, X6
	VPUNPCKHBW X4, X4, X4
	VPSRAW	$11, X6, X6
	VPSRAW	$11, X4, X4
	VPACKSSWB X4, X6, X4                // X4 = f2

	// f1 (also keep post-shift int16 halves in X8/X6 for (f1+1)>>1)
	VPUNPCKLBW X5, X5, X8
	VPUNPCKHBW X5, X5, X6
	VPSRAW	$11, X8, X8                 // X8 = f1_lo (16-bit)
	VPSRAW	$11, X6, X6                 // X6 = f1_hi (16-bit)
	VPACKSSWB X6, X8, X5                // X5 = f1 (signed bytes)

	// sps0 += f2 ;  sqs0 -= f1
	VPADDSB	X4, X10, X10                // sps0 += f2
	VPSUBSB	X5, X11, X11                // sqs0 -= f1

	// (f1+1)>>1 from post-shift halves X8 (lo), X6 (hi).
	VMOVDQU	lfAVX2Ones<>(SB), X4
	VPADDSW	X4, X8, X8
	VPADDSW	X4, X6, X6
	VPSRAW	$1, X8, X8
	VPSRAW	$1, X6, X6
	VPACKSSWB X6, X8, X8                // X8 = (f1+1)>>1

	// X8 &= ~hev   (PANDN-equivalent: X0 := ~X0 & X8)
	VPANDN	X8, X0, X0                  // X0 = ~X0 & X8 = ~hev & X8

	// sps1 += X0 ;  sqs1 -= X0
	VPADDSB	X0, X9,  X9                 // sps1 += X0
	VPSUBSB	X0, X12, X12                // sqs1 -= X0

	// Convert back to unsigned and store p1, p0, q0, q1.
	VMOVDQU	lfAVX2T80<>(SB), X3
	VPXOR	X3, X9,  X9
	VPXOR	X3, X10, X10
	VPXOR	X3, X11, X11
	VPXOR	X3, X12, X12

	VMOVDQU	X9,  (R11)
	VMOVDQU	X10, (R12)
	VMOVDQU	X11, (R13)
	VMOVDQU	X12, (SI)

	VZEROUPPER
	RET

// mbLoopFilterEdgeH16AVX2 ABI ($0-19, no stack):
//   src+0(FP)     *byte
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·mbLoopFilterEdgeH16AVX2(SB), NOSPLIT, $0-19
	MOVQ	src+0(FP), AX
	MOVQ	pitch+8(FP), BX

	MOVQ	AX, R10
	ADDQ	BX, R10
	MOVQ	R10, R11
	ADDQ	BX, R11
	MOVQ	R11, R12
	ADDQ	BX, R12
	MOVQ	R12, R13
	ADDQ	BX, R13
	MOVQ	R13, SI
	ADDQ	BX, SI
	MOVQ	SI, R8
	ADDQ	BX, R8
	MOVQ	R8, R9
	ADDQ	BX, R9

	// Live registers across the kernel:
	//   X9  = p1   (then sps1)
	//   X10 = p0   (then sps0)
	//   X11 = q0   (then sqs0)
	//   X12 = q1   (then sqs1)
	//   X13 = q2   (then sqs2)
	//   X14 = p2   (then sps2)
	//   X1  = filter_mask
	//   X0  = hev_mask
	VMOVDQU	(R10), X14                 // p2
	VMOVDQU	(R11), X9                  // p1
	VMOVDQU	(R12), X10                 // p0
	VMOVDQU	(R13), X11                 // q0
	VMOVDQU	(SI),  X12                 // q1
	VMOVDQU	(R8),  X13                 // q2

	// Pre-load p3 and q3 into scratch.
	VMOVDQU	(AX), X3                   // p3 (scratch)
	VMOVDQU	(R9), X4                   // q3 (scratch)

	// Mask compute: same recipe as inner LF.
	VPSUBUSB	X14, X3, X0
	VPSUBUSB	X3, X14, X2
	VPOR	X2, X0, X1                  // |p3-p2|

	VPSUBUSB	X9, X14, X0
	VPSUBUSB	X14, X9, X2
	VPOR	X2, X0, X0                  // |p2-p1|
	VPMAXUB	X0, X1, X1

	VPSUBUSB	X10, X9, X0
	VPSUBUSB	X9, X10, X2
	VPOR	X2, X0, X8                  // X8 = t1 = |p1-p0|
	VPMAXUB	X8, X1, X1

	VPSUBUSB	X11, X12, X0
	VPSUBUSB	X12, X11, X2
	VPOR	X2, X0, X7                  // X7 = t0 = |q1-q0|
	VPMAXUB	X7, X1, X1

	VPSUBUSB	X12, X13, X0
	VPSUBUSB	X13, X12, X2
	VPOR	X2, X0, X0                  // |q2-q1|
	VPMAXUB	X0, X1, X1

	VPSUBUSB	X13, X4, X0                // q3 - q2
	VPSUBUSB	X4, X13, X2                // q2 - q3
	VPOR	X2, X0, X0                  // |q3-q2|
	VPMAXUB	X0, X1, X1

	XORL	CX, CX
	MOVB	limit+17(FP), CL
	MOVQ	CX, X2
	VPUNPCKLBW X2, X2, X2
	VPSHUFLW $0, X2, X2
	VPSHUFD	$0, X2, X2
	VPSUBUSB X2, X1, X1

	VPSUBUSB	X12, X9, X0
	VPSUBUSB	X9, X12, X2
	VPOR	X2, X0, X0                  // |p1-q1|
	VPAND	lfAVX2Tfe<>(SB), X0, X0
	VPSRLW	$1, X0, X3                  // X3 = |p1-q1|/2

	VPSUBUSB	X11, X10, X0
	VPSUBUSB	X10, X11, X2
	VPOR	X2, X0, X0                  // |p0-q0|
	VPADDUSB	X0, X0, X0
	VPADDUSB	X3, X0, X0

	XORL	CX, CX
	MOVB	blimit+16(FP), CL
	MOVQ	CX, X2
	VPUNPCKLBW X2, X2, X2
	VPSHUFLW $0, X2, X2
	VPSHUFD	$0, X2, X2
	VPSUBUSB X2, X0, X0
	VPOR	X0, X1, X1

	VPXOR	X2, X2, X2
	VPCMPEQB X2, X1, X1                // X1 = filter_mask

	// hev mask in X0.
	XORL	CX, CX
	MOVB	thresh+18(FP), CL
	MOVQ	CX, X2
	VPUNPCKLBW X2, X2, X2
	VPSHUFLW $0, X2, X2
	VPSHUFD	$0, X2, X2
	VPSUBUSB X2, X7, X0                 // t0 - thresh
	VPSUBUSB X2, X8, X3                 // t1 - thresh
	VPADDB	X3, X0, X0                  // 0 iff NOT-hev
	VPXOR	X2, X2, X2
	VPCMPEQB X2, X0, X0                // 0xFF where NOT-hev
	VPCMPEQB X3, X3, X3                // all ones
	VPXOR	X3, X0, X0                  // X0 = hev (0xFF if hev)

	// ----- MB filter apply -----
	// Sign-convert p2, p1, p0, q0, q1, q2 in place.
	VMOVDQU	lfAVX2T80<>(SB), X3
	VPXOR	X3, X14, X14                // sps2
	VPXOR	X3, X9,  X9                 // sps1
	VPXOR	X3, X10, X10                // sps0
	VPXOR	X3, X11, X11                // sqs0
	VPXOR	X3, X12, X12                // sqs1
	VPXOR	X3, X13, X13                // sqs2

	// vp8_filter (pre-mask) = sat(sps1 - sqs1) + 3*sat(sqs0 - sps0)
	VPSUBSB	X12, X9, X2                 // sat(sps1 - sqs1)
	VPSUBSB	X10, X11, X3                // sat(sqs0 - sps0)
	VPADDSB	X3, X2, X2
	VPADDSB	X3, X2, X2
	VPADDSB	X3, X2, X2                  // pre-mask vp8_filter
	VPAND	X1, X2, X2                  // & filter_mask

	// Filter2 (hev branch) = vp8_filter & hev → X3
	VPAND	X0, X2, X3                  // X3 = Filter2

	// f1 = sat((Filter2+4)>>3); f2 = sat((Filter2+3)>>3)
	VPADDSB	lfAVX2T4<>(SB), X3, X6      // Filter2+4
	VPADDSB	lfAVX2T3<>(SB), X3, X5      // Filter2+3

	VPUNPCKLBW X5, X5, X4
	VPUNPCKHBW X5, X5, X5
	VPSRAW	$11, X4, X4
	VPSRAW	$11, X5, X5
	VPACKSSWB X5, X4, X5                // X5 = f2

	VPUNPCKLBW X6, X6, X4
	VPUNPCKHBW X6, X6, X6
	VPSRAW	$11, X4, X4
	VPSRAW	$11, X6, X6
	VPACKSSWB X6, X4, X3                // X3 = f1

	VPADDSB	X5, X10, X10                // sps0 += f2
	VPSUBSB	X3, X11, X11                // sqs0 -= f1

	// Filter for u-taps = vp8_filter & ~hev
	VPCMPEQB X3, X3, X3                // all ones
	VPXOR	X0, X3, X3                  // X3 = ~hev
	VPAND	X3, X2, X2                  // X2 = vp8_filter & ~hev

	// Sign-extend X2 to 16-bit (byte<<8 form) so PMULHW with s9=0x0900
	// yields Filter2*9.
	VPXOR	X3, X3, X3
	VPXOR	X4, X4, X4
	VPUNPCKLBW X2, X3, X3               // lo half int16 (byte<<8 form)
	VPUNPCKHBW X2, X4, X4               // hi half int16

	// Filter2 * 9
	VMOVDQU	lfAVX2S9<>(SB), X5
	VPMULHW	X5, X3, X3                  // X3 = Filter2_lo * 9
	VPMULHW	X5, X4, X4                  // X4 = Filter2_hi * 9

	// u9 applies to (sps2, sqs2)
	VMOVDQU	lfAVX2S63<>(SB), X5
	VPADDW	X5, X3, X6
	VPADDW	X5, X4, X7
	VPSRAW	$7, X6, X6
	VPSRAW	$7, X7, X7
	VPACKSSWB X7, X6, X6                // X6 = u9
	VPADDSB	X6, X14, X14                // sps2 += u9
	VPSUBSB	X6, X13, X13                // sqs2 -= u9

	// u18 = ((Filter2*9)*2 + 63) >> 7
	VPADDW	X3, X3, X6                  // 2x lo
	VPADDW	X4, X4, X7                  // 2x hi
	VPADDW	X5, X6, X6
	VPADDW	X5, X7, X7
	VPSRAW	$7, X6, X6
	VPSRAW	$7, X7, X7
	VPACKSSWB X7, X6, X6                // X6 = u18
	VPADDSB	X6, X9,  X9                 // sps1 += u18
	VPSUBSB	X6, X12, X12                // sqs1 -= u18

	// u27 = ((Filter2*9)*3 + 63) >> 7
	VPADDW	X3, X3, X6                  // 2x lo
	VPADDW	X4, X4, X7                  // 2x hi
	VPADDW	X3, X6, X6                  // 3x lo
	VPADDW	X4, X7, X7                  // 3x hi
	VPADDW	X5, X6, X6
	VPADDW	X5, X7, X7
	VPSRAW	$7, X6, X6
	VPSRAW	$7, X7, X7
	VPACKSSWB X7, X6, X6                // X6 = u27
	VPADDSB	X6, X10, X10                // sps0 += u27
	VPSUBSB	X6, X11, X11                // sqs0 -= u27

	// Convert back to unsigned and store six modified samples.
	VMOVDQU	lfAVX2T80<>(SB), X3
	VPXOR	X3, X14, X14
	VPXOR	X3, X9,  X9
	VPXOR	X3, X10, X10
	VPXOR	X3, X11, X11
	VPXOR	X3, X12, X12
	VPXOR	X3, X13, X13

	VMOVDQU	X14, (R10)                 // p2
	VMOVDQU	X9,  (R11)                 // p1
	VMOVDQU	X10, (R12)                 // p0
	VMOVDQU	X11, (R13)                 // q0
	VMOVDQU	X12, (SI)                  // q1
	VMOVDQU	X13, (R8)                  // q2

	VZEROUPPER
	RET
