//go:build amd64 && !purego

// SSE2 port of the libvpx v1.16.0 vp8 loop_filter and mb_loop_filter
// inner kernels (16-wide horizontal-edge form). Mirrors
// vp8/common/x86/loopfilter_sse2.asm — LFH_FILTER_AND_HEV_MASK plus
// B_FILTER for the inner LF; the same mask path plus
// MB_FILTER_AND_WRITEBACK for the MB LF. Each routine takes a pointer
// at the start of the p3 row (16 contiguous bytes), reads p3..q3 at
// +pitch increments, and writes the filtered samples back.
//
// loopFilterEdgeH16SSE2  : libvpx vp8_loop_filter_horizontal_edge_sse2
//                         (writes p1, p0, q0, q1)
// mbLoopFilterEdgeH16SSE2: libvpx vp8_mbloop_filter_horizontal_edge_sse2
//                         (writes p2, p1, p0, q0, q1, q2)
//
// Vertical-edge variants reuse the same kernels after the dispatch
// gathers each row's 8-byte window into the column lanes (see
// loopfilter_dispatch_amd64.go).

#include "textflag.h"

// 16x byte 0xFE
DATA  lfTfe<>+0x00(SB)/8, $0xFEFEFEFEFEFEFEFE
DATA  lfTfe<>+0x08(SB)/8, $0xFEFEFEFEFEFEFEFE
GLOBL lfTfe<>(SB), RODATA|NOPTR, $16

// 16x byte 0x80
DATA  lfT80<>+0x00(SB)/8, $0x8080808080808080
DATA  lfT80<>+0x08(SB)/8, $0x8080808080808080
GLOBL lfT80<>(SB), RODATA|NOPTR, $16

// 16x byte 0x03
DATA  lfT3<>+0x00(SB)/8, $0x0303030303030303
DATA  lfT3<>+0x08(SB)/8, $0x0303030303030303
GLOBL lfT3<>(SB), RODATA|NOPTR, $16

// 16x byte 0x04
DATA  lfT4<>+0x00(SB)/8, $0x0404040404040404
DATA  lfT4<>+0x08(SB)/8, $0x0404040404040404
GLOBL lfT4<>(SB), RODATA|NOPTR, $16

// 8x word 0x0001 (ones)
DATA  lfOnes<>+0x00(SB)/8, $0x0001000100010001
DATA  lfOnes<>+0x08(SB)/8, $0x0001000100010001
GLOBL lfOnes<>(SB), RODATA|NOPTR, $16

// 8x word 0x0900 (s9 — used so that PMULHW with (Filter2<<8)*0x0900
// yields Filter2*9 in 16-bit signed lanes).
DATA  lfS9<>+0x00(SB)/8, $0x0900090009000900
DATA  lfS9<>+0x08(SB)/8, $0x0900090009000900
GLOBL lfS9<>(SB), RODATA|NOPTR, $16

// 8x word 0x003F (s63)
DATA  lfS63<>+0x00(SB)/8, $0x003F003F003F003F
DATA  lfS63<>+0x08(SB)/8, $0x003F003F003F003F
GLOBL lfS63<>(SB), RODATA|NOPTR, $16

// 16x byte 0xE0 (te0) — upper-3-bit mask used in the simple-LF signed
// arith-shift-right-by-3 trick (libvpx vp8_loop_filter_simple_*_sse2).
DATA  lfTe0<>+0x00(SB)/8, $0xE0E0E0E0E0E0E0E0
DATA  lfTe0<>+0x08(SB)/8, $0xE0E0E0E0E0E0E0E0
GLOBL lfTe0<>(SB), RODATA|NOPTR, $16

// 16x byte 0x1F (t1f) — bottom-5-bit mask, paired with te0 above.
DATA  lfT1f<>+0x00(SB)/8, $0x1F1F1F1F1F1F1F1F
DATA  lfT1f<>+0x08(SB)/8, $0x1F1F1F1F1F1F1F1F
GLOBL lfT1f<>(SB), RODATA|NOPTR, $16

// loopFilterEdgeH16SSE2 ABI ($0-19, no stack):
//   src+0(FP)     *byte (points at p3 row, 16 contiguous bytes)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·loopFilterEdgeH16SSE2(SB), NOSPLIT, $0-19
	MOVQ	src+0(FP), AX
	MOVQ	pitch+8(FP), BX

	// Row pointers: AX=p3, R10=p2, R11=p1, R12=p0, R13=q0, SI=q1, R8=q2, R9=q3.
	// Avoid R14 (g register in Go ABIInternal) and R15 (PIE GOT base on
	// linux/amd64 -buildmode=pie). The wrapper restores R14 from TLS
	// after this call returns, but during async preemption windows
	// keeping g intact guards against runtime probes that read R14
	// optimistically.
	MOVQ	AX, R10
	ADDQ	BX, R10           // R10 = p2
	MOVQ	R10, R11
	ADDQ	BX, R11           // R11 = p1
	MOVQ	R11, R12
	ADDQ	BX, R12           // R12 = p0
	MOVQ	R12, R13
	ADDQ	BX, R13           // R13 = q0
	MOVQ	R13, SI
	ADDQ	BX, SI            // SI  = q1
	MOVQ	SI, R8
	ADDQ	BX, R8            // R8  = q2
	MOVQ	R8, R9
	ADDQ	BX, R9            // R9  = q3

	// Pre-load all eight rows into XMM registers up front so the rest
	// of the kernel never re-reads via the row pointers. p3, p2, q2,
	// q3 live in scratch slots X3, X4, X5, X6 (consumed during mask
	// build); p1, p0, q0, q1 stay in X9..X12 (mutated in place to
	// signed below). Pre-loading avoids any second memory access where
	// the ModR/M encoding for an SSE op with an extended-register base
	// might hit a stale TLB entry or be reordered after a partial
	// pointer update — both common ways the original layout could
	// SEGV intermittently on linux/amd64 even though the row pointers
	// are computed only once and never modified.
	MOVOU	(AX),  X3         // p3 (scratch — only used during mask)
	MOVOU	(R10), X4         // p2 (scratch)
	MOVOU	(R11), X9         // p1 (kept)
	MOVOU	(R12), X10        // p0 (kept)
	MOVOU	(R13), X11        // q0 (kept)
	MOVOU	(SI),  X12        // q1 (kept)
	MOVOU	(R8),  X5         // q2 (scratch)
	MOVOU	(R9),  X6         // q3 (scratch)

	// Mask compute: build X1 = max of |p3-p2|,|p2-p1|,|p1-p0|,|q1-q0|,
	// |q2-q1|,|q3-q2|. Save |p1-p0| (-> t1) and |q1-q0| (-> t0) for hev
	// later: keep t0 in X13, t1 in X14.

	// |p3-p2| using pre-loaded X3 (p3), X4 (p2)
	MOVOU	X3, X0
	MOVOU	X4, X2
	PSUBUSB	X2, X0            // X0 = sat(p3-p2)
	PSUBUSB	X3, X2            // X2 = sat(p2-p3)
	POR	X2, X0            // X0 = |p3-p2|
	MOVOU	X0, X1            // X1 = running max

	// |p2-p1|
	MOVOU	X4, X0
	MOVOU	X9, X2
	PSUBUSB	X9, X0            // X0 = sat(p2-p1)
	PSUBUSB	X4, X2            // X2 = sat(p1-p2)
	POR	X2, X0            // X0 = |p2-p1|
	PMAXUB	X0, X1

	// |p1-p0| -> X14 (t1)
	MOVOU	X9, X0
	MOVOU	X10, X2
	PSUBUSB	X10, X0
	PSUBUSB	X9, X2
	POR	X2, X0            // X0 = |p1-p0|
	MOVOU	X0, X14           // t1
	PMAXUB	X0, X1

	// |q1-q0| -> X13 (t0)
	MOVOU	X12, X0
	MOVOU	X11, X2
	PSUBUSB	X11, X0
	PSUBUSB	X12, X2
	POR	X2, X0            // X0 = |q1-q0|
	MOVOU	X0, X13           // t0
	PMAXUB	X0, X1

	// |q2-q1| using pre-loaded X5 (q2)
	MOVOU	X5, X0
	MOVOU	X12, X2
	PSUBUSB	X12, X0           // X0 = sat(q2-q1)
	PSUBUSB	X5, X2            // X2 = sat(q1-q2)
	POR	X2, X0
	PMAXUB	X0, X1

	// |q3-q2| using pre-loaded X6 (q3), X5 (q2)
	MOVOU	X6, X0
	MOVOU	X5, X2
	PSUBUSB	X5, X0            // X0 = sat(q3-q2)
	PSUBUSB	X6, X2            // X2 = sat(q2-q3)
	POR	X2, X0
	PMAXUB	X0, X1            // X1 = max-of-six

	// X1 -= limit (broadcast). Zero means OK.
	XORL	CX, CX
	MOVB	limit+17(FP), CL
	MOVQ	CX, X2
	PUNPCKLBW X2, X2
	PSHUFLW	$0, X2, X2
	PSHUFD	$0, X2, X2        // limit broadcast
	PSUBUSB	X2, X1

	// Composite: (|p0-q0| * 2 sat) + |p1-q1|/2.
	MOVOU	X9, X0
	MOVOU	X12, X2
	PSUBUSB	X12, X0
	PSUBUSB	X9, X2
	POR	X2, X0            // |p1-q1|
	PAND	lfTfe<>(SB), X0
	PSRLW	$1, X0            // |p1-q1|/2
	MOVOU	X0, X3            // X3 = |p1-q1|/2

	MOVOU	X10, X0
	MOVOU	X11, X2
	PSUBUSB	X11, X0
	PSUBUSB	X10, X2
	POR	X2, X0            // |p0-q0|
	PADDUSB	X0, X0            // *2 (sat)
	PADDUSB	X3, X0            // +|p1-q1|/2

	XORL	CX, CX
	MOVB	blimit+16(FP), CL
	MOVQ	CX, X2
	PUNPCKLBW X2, X2
	PSHUFLW	$0, X2, X2
	PSHUFD	$0, X2, X2
	PSUBUSB	X2, X0
	POR	X0, X1            // X1 = 0 iff both checks pass

	PXOR	X2, X2
	PCMPEQB	X2, X1            // X1 = filter_mask (0xFF = filter)

	// hev mask: (t0 -= thresh) | (t1 -= thresh) == 0 → ~hev; invert.
	XORL	CX, CX
	MOVB	thresh+18(FP), CL
	MOVQ	CX, X2
	PUNPCKLBW X2, X2
	PSHUFLW	$0, X2, X2
	PSHUFD	$0, X2, X2        // thresh broadcast
	MOVOU	X13, X0           // t0
	MOVOU	X14, X3           // t1
	PSUBUSB	X2, X0
	PSUBUSB	X2, X3
	PADDB	X3, X0            // 0 means both <= thresh (NOT-hev)
	PXOR	X2, X2
	PCMPEQB	X2, X0            // X0 = NOT-hev mask
	PCMPEQB	X3, X3            // all ones
	PXOR	X3, X0            // X0 = hev mask (0xFF when hev)

	// X0 will be reused below. We need to keep:
	//   X1 = filter_mask
	//   X0 = hev_mask
	// Sign-convert p1, p0, q0, q1 in place.
	MOVOU	lfT80<>(SB), X3
	PXOR	X3, X9            // sps1
	PXOR	X3, X10           // sps0
	PXOR	X3, X11           // sqs0
	PXOR	X3, X12           // sqs1

	// fv = sat(sps1 - sqs1); fv &= hev
	MOVOU	X9, X2
	PSUBSB	X12, X2           // sat(sps1 - sqs1)
	PAND	X0, X2            // & hev

	// fv += 3 * sat(sqs0 - sps0)
	MOVOU	X11, X3           // copy sqs0
	PSUBSB	X10, X3           // sat(sqs0 - sps0)
	PADDSB	X3, X2
	PADDSB	X3, X2
	PADDSB	X3, X2            // X2 = fv (pre-mask)
	PAND	X1, X2            // & filter_mask

	// f1 = (fv + 4) >> 3,   f2 = (fv + 3) >> 3   (signed arith shift via punpck/psraw/pack)
	MOVOU	X2, X4
	PADDSB	lfT3<>(SB), X4    // fv+3
	MOVOU	X2, X5
	PADDSB	lfT4<>(SB), X5    // fv+4

	// f2
	MOVOU	X4, X6
	PUNPCKLBW X4, X4
	PUNPCKHBW X6, X6
	PSRAW	$11, X4
	PSRAW	$11, X6
	PACKSSWB X6, X4           // X4 = f2

	// f1 (also keep post-shift halves in X7/X8 for the (f1+1)>>1 step)
	MOVOU	X5, X7
	PUNPCKLBW X5, X5
	PUNPCKHBW X7, X7
	PSRAW	$11, X5
	PSRAW	$11, X7           // X5 = f1_lo (16-bit), X7 = f1_hi (16-bit)
	MOVOU	X5, X8            // copy lo
	MOVOU	X7, X6            // copy hi  (X6 free)
	PACKSSWB X7, X5           // X5 = f1 (signed bytes)

	// sps0 += f2 ;  sqs0 -= f1
	PADDSB	X4, X10           // sps0 += f2
	PSUBSB	X5, X11           // sqs0 -= f1

	// (f1+1)>>1 from post-shift halves X8 (lo), X6 (hi).
	MOVOU	lfOnes<>(SB), X4
	PADDSW	X4, X8
	PADDSW	X4, X6
	PSRAW	$1, X8
	PSRAW	$1, X6
	PACKSSWB X6, X8           // X8 = (f1+1)>>1

	// X8 &= ~hev   (PANDN dst,src is dst = ~dst & src; want X8 = ~hev & X8)
	PANDN	X8, X0            // X0 := ~X0 & X8 = ~hev & X8

	// sps1 += X0 ;  sqs1 -= X0
	PADDSB	X0, X9            // sps1 += X0
	PSUBSB	X0, X12           // sqs1 -= X0

	// Convert back to unsigned and store p1, p0, q0, q1.
	MOVOU	lfT80<>(SB), X3
	PXOR	X3, X9
	PXOR	X3, X10
	PXOR	X3, X11
	PXOR	X3, X12

	MOVOU	X9,  (R11)
	MOVOU	X10, (R12)
	MOVOU	X11, (R13)
	MOVOU	X12, (SI)

	RET

// mbLoopFilterEdgeH16SSE2 ABI ($0-19, no stack):
//   src+0(FP)     *byte
//   pitch+8(FP)   int
//   blimit+16(FP) byte
//   limit+17(FP)  byte
//   thresh+18(FP) byte
TEXT ·mbLoopFilterEdgeH16SSE2(SB), NOSPLIT, $0-19
	MOVQ	src+0(FP), AX
	MOVQ	pitch+8(FP), BX

	// Row pointers: AX=p3, R10=p2, R11=p1, R12=p0, R13=q0, SI=q1, R8=q2, R9=q3.
	// Avoid R14 (g register in Go ABIInternal) and R15 (PIE GOT base on
	// linux/amd64 -buildmode=pie). Pre-load p3 and q3 into XMM scratch
	// so the kernel does not re-touch the row pointers after the
	// mask-compute prologue.
	MOVQ	AX, R10
	ADDQ	BX, R10           // R10 = p2
	MOVQ	R10, R11
	ADDQ	BX, R11           // R11 = p1
	MOVQ	R11, R12
	ADDQ	BX, R12           // R12 = p0
	MOVQ	R12, R13
	ADDQ	BX, R13           // R13 = q0
	MOVQ	R13, SI
	ADDQ	BX, SI            // SI  = q1
	MOVQ	SI, R8
	ADDQ	BX, R8            // R8  = q2
	MOVQ	R8, R9
	ADDQ	BX, R9            // R9  = q3

	// Live registers across the kernel:
	//   X9  = p1   (then sps1)
	//   X10 = p0   (then sps0)
	//   X11 = q0   (then sqs0)
	//   X12 = q1   (then sqs1)
	//   X13 = q2   (then sqs2)
	//   X14 = p2   (then sps2)
	//   X1  = filter_mask
	//   X0  = hev_mask
	MOVOU	(R10), X14        // p2
	MOVOU	(R11), X9         // p1
	MOVOU	(R12), X10        // p0
	MOVOU	(R13), X11        // q0
	MOVOU	(SI),  X12        // q1
	MOVOU	(R8),  X13        // q2

	// Pre-load p3 and q3 into scratch XMMs so the rest of the mask
	// compute never re-reads via the row pointers.
	MOVOU	(AX), X3          // p3 (scratch)
	MOVOU	(R9), X4          // q3 (scratch)

	// Mask compute.  Same recipe as inner LF; build X1 = max of six diffs.
	MOVOU	X3, X0
	MOVOU	X14, X2           // p2
	PSUBUSB	X2, X0            // X0 = sat(p3-p2)
	PSUBUSB	X3, X2            // X2 = sat(p2-p3)
	POR	X2, X0            // |p3-p2|
	MOVOU	X0, X1            // X1 = running max

	MOVOU	X14, X0
	MOVOU	X9, X2
	PSUBUSB	X9, X0
	PSUBUSB	X14, X2
	POR	X2, X0            // |p2-p1|
	PMAXUB	X0, X1

	MOVOU	X9, X0
	MOVOU	X10, X2
	PSUBUSB	X10, X0
	PSUBUSB	X9, X2
	POR	X2, X0            // |p1-p0| -> save in X8 = t1
	MOVOU	X0, X8
	PMAXUB	X0, X1

	MOVOU	X12, X0
	MOVOU	X11, X2
	PSUBUSB	X11, X0
	PSUBUSB	X12, X2
	POR	X2, X0            // |q1-q0| -> save in X7 = t0
	MOVOU	X0, X7
	PMAXUB	X0, X1

	MOVOU	X13, X0
	MOVOU	X12, X2
	PSUBUSB	X12, X0
	PSUBUSB	X13, X2
	POR	X2, X0            // |q2-q1|
	PMAXUB	X0, X1

	MOVOU	X4, X0            // q3
	MOVOU	X13, X2           // q2
	PSUBUSB	X13, X0           // X0 = sat(q3-q2)
	PSUBUSB	X4, X2            // X2 = sat(q2-q3)
	POR	X2, X0            // |q3-q2|
	PMAXUB	X0, X1

	XORL	CX, CX
	MOVB	limit+17(FP), CL
	MOVQ	CX, X2
	PUNPCKLBW X2, X2
	PSHUFLW	$0, X2, X2
	PSHUFD	$0, X2, X2        // limit broadcast
	PSUBUSB	X2, X1

	MOVOU	X9, X0
	MOVOU	X12, X2
	PSUBUSB	X12, X0
	PSUBUSB	X9, X2
	POR	X2, X0            // |p1-q1|
	PAND	lfTfe<>(SB), X0
	PSRLW	$1, X0
	MOVOU	X0, X3            // |p1-q1|/2

	MOVOU	X10, X0
	MOVOU	X11, X2
	PSUBUSB	X11, X0
	PSUBUSB	X10, X2
	POR	X2, X0            // |p0-q0|
	PADDUSB	X0, X0
	PADDUSB	X3, X0

	XORL	CX, CX
	MOVB	blimit+16(FP), CL
	MOVQ	CX, X2
	PUNPCKLBW X2, X2
	PSHUFLW	$0, X2, X2
	PSHUFD	$0, X2, X2
	PSUBUSB	X2, X0
	POR	X0, X1

	PXOR	X2, X2
	PCMPEQB	X2, X1            // X1 = filter_mask

	// hev mask in X0.
	XORL	CX, CX
	MOVB	thresh+18(FP), CL
	MOVQ	CX, X2
	PUNPCKLBW X2, X2
	PSHUFLW	$0, X2, X2
	PSHUFD	$0, X2, X2
	MOVOU	X7, X0            // t0 = |q1-q0|
	MOVOU	X8, X3            // t1 = |p1-p0|
	PSUBUSB	X2, X0
	PSUBUSB	X2, X3
	PADDB	X3, X0            // 0 if NOT-hev
	PXOR	X2, X2
	PCMPEQB	X2, X0            // 0xFF where NOT-hev
	PCMPEQB	X3, X3            // all ones
	PXOR	X3, X0            // X0 = hev (0xFF if hev)

	// ----- MB filter apply -----
	// Sign-convert p2, p1, p0, q0, q1, q2 in place.
	MOVOU	lfT80<>(SB), X3
	PXOR	X3, X14           // sps2
	PXOR	X3, X9            // sps1
	PXOR	X3, X10           // sps0
	PXOR	X3, X11           // sqs0
	PXOR	X3, X12           // sqs1
	PXOR	X3, X13           // sqs2

	// vp8_filter (pre-mask) = sat(sps1 - sqs1) + 3*sat(sqs0 - sps0)
	MOVOU	X9, X2
	PSUBSB	X12, X2           // sat(sps1 - sqs1)
	MOVOU	X11, X3
	PSUBSB	X10, X3           // sat(sqs0 - sps0)
	PADDSB	X3, X2
	PADDSB	X3, X2
	PADDSB	X3, X2            // X2 = pre-mask vp8_filter
	PAND	X1, X2            // X2 = vp8_filter (& filter_mask)

	// Filter2 (hev branch) = vp8_filter & hev → X3
	MOVOU	X2, X3
	PAND	X0, X3            // X3 = Filter2

	// f1 = sat((Filter2+4)>>3); f2 = sat((Filter2+3)>>3)
	MOVOU	X3, X5
	PADDSB	lfT4<>(SB), X3    // Filter2+4
	PADDSB	lfT3<>(SB), X5    // Filter2+3

	MOVOU	X5, X6
	PUNPCKLBW X5, X5
	PUNPCKHBW X6, X6
	PSRAW	$11, X5
	PSRAW	$11, X6
	PACKSSWB X6, X5           // X5 = f2

	MOVOU	X3, X6
	PUNPCKLBW X3, X3
	PUNPCKHBW X6, X6
	PSRAW	$11, X3
	PSRAW	$11, X6
	PACKSSWB X6, X3           // X3 = f1

	PADDSB	X5, X10           // sps0 += f2
	PSUBSB	X3, X11           // sqs0 -= f1

	// Filter for u-taps = vp8_filter & ~hev   (X2 -> X2)
	PCMPEQB	X3, X3
	PXOR	X0, X3            // X3 = ~hev
	PAND	X3, X2            // X2 = vp8_filter & ~hev (signed bytes)

	// Sign-extend X2 to 16-bit lanes shifted-left-8 (so PMULHW with
	// s9=0x0900 yields Filter2 * 9 as signed 16-bit).
	PXOR	X3, X3
	PXOR	X4, X4
	PUNPCKLBW X2, X3          // X3 = Filter2_lo as 8 int16 (byte<<8 form)
	PUNPCKHBW X2, X4          // X4 = Filter2_hi as 8 int16

	// Filter2 * 9
	MOVOU	lfS9<>(SB), X5
	PMULHW	X5, X3            // X3 = Filter2_lo * 9
	PMULHW	X5, X4            // X4 = Filter2_hi * 9

	// u9 applies to (sps2, sqs2)
	MOVOU	lfS63<>(SB), X5
	MOVOU	X3, X6
	MOVOU	X4, X7
	PADDW	X5, X6
	PADDW	X5, X7
	PSRAW	$7, X6
	PSRAW	$7, X7
	PACKSSWB X7, X6           // X6 = u9
	PADDSB	X6, X14           // sps2 += u9
	PSUBSB	X6, X13           // sqs2 -= u9

	// u18 = ((Filter2*9)*2 + 63) >> 7
	MOVOU	X3, X6
	MOVOU	X4, X7
	PADDW	X6, X6            // *2 → *18
	PADDW	X7, X7
	PADDW	X5, X6
	PADDW	X5, X7
	PSRAW	$7, X6
	PSRAW	$7, X7
	PACKSSWB X7, X6           // X6 = u18
	PADDSB	X6, X9            // sps1 += u18
	PSUBSB	X6, X12           // sqs1 -= u18

	// u27 = ((Filter2*9)*3 + 63) >> 7
	MOVOU	X3, X6
	MOVOU	X4, X7
	MOVOU	X6, X8
	MOVOU	X7, X15
	PADDW	X8, X8            // *2
	PADDW	X15, X15
	PADDW	X8, X6            // *3
	PADDW	X15, X7
	PADDW	X5, X6
	PADDW	X5, X7
	PSRAW	$7, X6
	PSRAW	$7, X7
	PACKSSWB X7, X6           // X6 = u27
	PADDSB	X6, X10           // sps0 += u27
	PSUBSB	X6, X11           // sqs0 -= u27

	// Convert back to unsigned and store six modified samples.
	MOVOU	lfT80<>(SB), X3
	PXOR	X3, X14
	PXOR	X3, X9
	PXOR	X3, X10
	PXOR	X3, X11
	PXOR	X3, X12
	PXOR	X3, X13

	MOVOU	X14, (R10)        // p2
	MOVOU	X9,  (R11)        // p1
	MOVOU	X10, (R12)        // p0
	MOVOU	X11, (R13)        // q0
	MOVOU	X12, (SI)         // q1
	MOVOU	X13, (R8)         // q2

	RET

// loopFilterSimpleEdgeH16SSE2 ABI ($0-17, no stack):
//   src+0(FP)     *byte (points at p1 row, 16 contiguous bytes)
//   pitch+8(FP)   int
//   blimit+16(FP) byte
TEXT ·loopFilterSimpleEdgeH16SSE2(SB), NOSPLIT, $0-17
	MOVQ	src+0(FP), AX
	MOVQ	pitch+8(FP), BX

	MOVQ	AX, R10           // p0 row
	ADDQ	BX, R10
	MOVQ	R10, R11          // q0 row
	ADDQ	BX, R11
	MOVQ	R11, R12          // q1 row
	ADDQ	BX, R12

	// Live registers across the kernel:
	//   X1 = p1, X2 = p0, X3 = q0, X4 = q1   (then sign-converted in place)
	//   X5 = filter_mask
	MOVOU	(AX),  X1         // p1
	MOVOU	(R10), X2         // p0
	MOVOU	(R11), X3         // q0
	MOVOU	(R12), X4         // q1

	// abs(p1-q1) → X0, then & 0xFE, >>1 → |p1-q1|/2
	MOVOU	X1, X0
	MOVOU	X4, X6
	PSUBUSB	X4, X0            // sat(p1-q1)
	PSUBUSB	X1, X6            // sat(q1-p1)
	POR	X6, X0            // |p1-q1|
	PAND	lfTfe<>(SB), X0
	PSRLW	$1, X0            // X0 = |p1-q1|/2

	// abs(p0-q0)*2 (saturating) + |p1-q1|/2 → X5
	MOVOU	X2, X5
	MOVOU	X3, X6
	PSUBUSB	X3, X5            // sat(p0-q0)
	PSUBUSB	X2, X6            // sat(q0-p0)
	POR	X6, X5            // |p0-q0|
	PADDUSB	X5, X5            // *2 (sat)
	PADDUSB	X0, X5            // + |p1-q1|/2

	// blimit broadcast → X6
	XORL	CX, CX
	MOVB	blimit+16(FP), CL
	MOVQ	CX, X6
	PUNPCKLBW X6, X6
	PSHUFLW	$0, X6, X6
	PSHUFD	$0, X6, X6
	PSUBUSB	X6, X5            // 0 iff composite <= blimit
	PXOR	X6, X6
	PCMPEQB	X6, X5            // X5 = filter_mask (0xFF where filtered)

	// Sign-convert p1, p0, q0, q1.
	MOVOU	lfT80<>(SB), X7
	PXOR	X7, X1            // sps1
	PXOR	X7, X2            // sps0
	PXOR	X7, X3            // sqs0
	PXOR	X7, X4            // sqs1

	// fv = sat(sps1 - sqs1)  [signed]
	MOVOU	X1, X0
	PSUBSB	X4, X0            // X0 = sat(sps1 - sqs1)

	// fv += 3 * (sqs0 - sps0)  via three signed-add-sat steps
	MOVOU	X3, X6
	PSUBSB	X2, X6            // X6 = sat(sqs0 - sps0)
	PADDSB	X6, X0
	PADDSB	X6, X0
	PADDSB	X6, X0            // X0 = pre-mask vp8_filter

	PAND	X5, X0            // X0 = fv & mask

	// f1 = sra((fv + 4), 3); f2 = sra((fv + 3), 3)
	// libvpx trick: pcmpgtb to extract sign, mask upper-3 bits via te0,
	// PSRLW by 3 then mask off bottom 5 bits via t1f, OR the sign bits
	// back. This implements the per-byte arithmetic-shift-right-by-3
	// without PSRAB.
	MOVOU	X0, X5            // copy fv
	PADDSB	lfT3<>(SB), X0    // fv + 3 (sat)
	PADDSB	lfT4<>(SB), X5    // fv + 4 (sat)

	MOVOU	lfTe0<>(SB), X8
	MOVOU	lfT1f<>(SB), X9

	// f2 in X0
	PXOR	X6, X6
	PCMPGTB	X0, X6            // X6 = 0xFF where (fv+3) < 0
	PAND	X8, X6            // keep upper 3 bits of sign extension
	PSRLW	$3, X0
	PAND	X9, X0
	POR	X6, X0            // X0 = sra(fv+3, 3) = f2

	// f1 in X5
	PXOR	X6, X6
	PCMPGTB	X5, X6
	PAND	X8, X6
	PSRLW	$3, X5
	PAND	X9, X5
	POR	X6, X5            // X5 = sra(fv+4, 3) = f1

	// sps0 += f2; sqs0 -= f1
	PADDSB	X0, X2            // sps0 += f2
	PSUBSB	X5, X3            // sqs0 -= f1

	// Convert back to unsigned and store p0, q0 only.
	MOVOU	lfT80<>(SB), X7
	PXOR	X7, X2
	PXOR	X7, X3

	MOVOU	X2, (R10)         // p0
	MOVOU	X3, (R11)         // q0

	RET
