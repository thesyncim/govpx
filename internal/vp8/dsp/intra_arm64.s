// ARMv8 NEON intra-prediction kernels. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/vp8_intrapred_neon.c per-mode primitives for
// the Y16x16 and UV8x8 whole-block predictors:
//
//   intraSum16NEON         : sum of 16 bytes -> int32 (used by DC).
//   intraSum8NEON          : sum of 8  bytes -> int32 (used by DC).
//   intraFill16x16NEON     : broadcast a byte to a 16x16 stride-aware block.
//   intraFill8x8NEON       : broadcast a byte to an  8x8  stride-aware block.
//   intraHPredict16x16NEON : per-row broadcast of left[y] across 16 cols.
//   intraHPredict8x8NEON   : per-row broadcast of left[y] across  8 cols.
//   intraTMPredict16x16NEON: per-row clip255(left[y] - topLeft + above[x]).
//   intraTMPredict8x8NEON  : same, 8x8 form.
//
// V-prediction (copy of `above` to each output row) is intentionally
// kept on the pure-Go `copy()` path — the compiler already lowers the
// 16/8-byte row copy to a single MOVOU/STR, so there is no SIMD win to
// be had over a memcpy at this scale.
//
// Sum routines widen the loaded bytes via VUXTL/VUXTL2 to int16 lanes and
// horizontal-reduce via VADDV (H lane form) to a single int16 in lane 0.
// FMOVS returns the low 32 bits as int32 (upper 16 bits are zero per
// VADDV's documented zero-extension of unwritten lanes).
//
// TM uses VUXTL/VUXTL2 to widen the above row to two int16x8 vectors,
// subtracts the broadcasted topLeft once, then per-row adds the broadcast
// left[y] and saturates with SQXTUN/SQXTUN2 back to bytes.
//
// SQXTUN/SQXTUN2 aren't recognized by Go's arm64 assembler so they are
// emitted as raw WORD directives (encodings from clang).

#include "textflag.h"

// intraSum16NEON ABI ($0-12):
//   src+0(FP) *byte
//   ret+8(FP) int32
TEXT ·intraSum16NEON(SB), NOSPLIT, $0-12
	MOVD	src+0(FP), R0
	VLD1	(R0), [V0.B16]
	VUXTL	V0.B8, V1.H8
	VUXTL2	V0.B16, V2.H8
	VADD	V2.H8, V1.H8, V1.H8
	VADDV	V1.H8, V1
	FMOVS	F1, ret+8(FP)
	RET

// intraSum8NEON ABI ($0-12):
//   src+0(FP) *byte
//   ret+8(FP) int32
TEXT ·intraSum8NEON(SB), NOSPLIT, $0-12
	MOVD	src+0(FP), R0
	FMOVD	(R0), F0
	VUXTL	V0.B8, V1.H8
	VADDV	V1.H8, V1
	FMOVS	F1, ret+8(FP)
	RET

// intraFill16x16NEON ABI ($0-17):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   val+16(FP)   byte
TEXT ·intraFill16x16NEON(SB), NOSPLIT, $0-17
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVBU	val+16(FP), R2
	VDUP	R2, V0.B16
	MOVD	$16, R3
fill16_loop:
	VST1	[V0.B16], (R0)
	ADD	R1, R0, R0
	SUB	$1, R3, R3
	CBNZ	R3, fill16_loop
	RET

// intraFill8x8NEON ABI ($0-17):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   val+16(FP)   byte
TEXT ·intraFill8x8NEON(SB), NOSPLIT, $0-17
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVBU	val+16(FP), R2
	VDUP	R2, V0.B8
	MOVD	$8, R3
fill8_loop:
	VST1	[V0.B8], (R0)
	ADD	R1, R0, R0
	SUB	$1, R3, R3
	CBNZ	R3, fill8_loop
	RET

// intraHPredict16x16NEON ABI ($0-24):
//   dst+0(FP)    *byte
//   stride+8(FP) int
//   left+16(FP)  *byte
TEXT ·intraHPredict16x16NEON(SB), NOSPLIT, $0-24
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	left+16(FP), R2
	// Load all 16 left bytes once; broadcast each lane per row.
	VLD1	(R2), [V0.B16]
	VDUP	V0.B[0],  V1.B16
	VDUP	V0.B[1],  V2.B16
	VDUP	V0.B[2],  V3.B16
	VDUP	V0.B[3],  V4.B16
	VDUP	V0.B[4],  V5.B16
	VDUP	V0.B[5],  V6.B16
	VDUP	V0.B[6],  V7.B16
	VDUP	V0.B[7],  V8.B16
	VST1	[V1.B16], (R0); ADD R1, R0, R0
	VST1	[V2.B16], (R0); ADD R1, R0, R0
	VST1	[V3.B16], (R0); ADD R1, R0, R0
	VST1	[V4.B16], (R0); ADD R1, R0, R0
	VST1	[V5.B16], (R0); ADD R1, R0, R0
	VST1	[V6.B16], (R0); ADD R1, R0, R0
	VST1	[V7.B16], (R0); ADD R1, R0, R0
	VST1	[V8.B16], (R0); ADD R1, R0, R0
	VDUP	V0.B[8],  V1.B16
	VDUP	V0.B[9],  V2.B16
	VDUP	V0.B[10], V3.B16
	VDUP	V0.B[11], V4.B16
	VDUP	V0.B[12], V5.B16
	VDUP	V0.B[13], V6.B16
	VDUP	V0.B[14], V7.B16
	VDUP	V0.B[15], V8.B16
	VST1	[V1.B16], (R0); ADD R1, R0, R0
	VST1	[V2.B16], (R0); ADD R1, R0, R0
	VST1	[V3.B16], (R0); ADD R1, R0, R0
	VST1	[V4.B16], (R0); ADD R1, R0, R0
	VST1	[V5.B16], (R0); ADD R1, R0, R0
	VST1	[V6.B16], (R0); ADD R1, R0, R0
	VST1	[V7.B16], (R0); ADD R1, R0, R0
	VST1	[V8.B16], (R0)
	RET

// intraHPredict8x8NEON ABI ($0-24):
TEXT ·intraHPredict8x8NEON(SB), NOSPLIT, $0-24
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	left+16(FP), R2
	FMOVD	(R2), F0
	VDUP	V0.B[0], V1.B8
	VDUP	V0.B[1], V2.B8
	VDUP	V0.B[2], V3.B8
	VDUP	V0.B[3], V4.B8
	VDUP	V0.B[4], V5.B8
	VDUP	V0.B[5], V6.B8
	VDUP	V0.B[6], V7.B8
	VDUP	V0.B[7], V8.B8
	VST1	[V1.B8], (R0); ADD R1, R0, R0
	VST1	[V2.B8], (R0); ADD R1, R0, R0
	VST1	[V3.B8], (R0); ADD R1, R0, R0
	VST1	[V4.B8], (R0); ADD R1, R0, R0
	VST1	[V5.B8], (R0); ADD R1, R0, R0
	VST1	[V6.B8], (R0); ADD R1, R0, R0
	VST1	[V7.B8], (R0); ADD R1, R0, R0
	VST1	[V8.B8], (R0)
	RET

// intraTMPredict16x16NEON ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
//
// Plan: widen above to two int16x8 (V10/V11), subtract broadcast
// topLeft (V20.H8). Then for each row y: broadcast left[y] (V21.H8),
// add into V12/V13 = V10+V21, V11+V21 (signed int16), saturate-to-uint8
// with SQXTUN -> low 8 bytes of V14, SQXTUN2 -> high 8 bytes of V14, store.
TEXT ·intraTMPredict16x16NEON(SB), NOSPLIT, $0-33
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3
	MOVBU	topLeft+32(FP), R4

	VLD1	(R2), [V0.B16]      // above
	VLD1	(R3), [V1.B16]      // left
	VUXTL	V0.B8,  V10.H8      // above[0..7]  -> int16 (V10)
	VUXTL2	V0.B16, V11.H8      // above[8..15] -> int16 (V11)
	VUXTL	V1.B8,  V12.H8      // left[0..7]   -> int16 (V12)
	VUXTL2	V1.B16, V13.H8      // left[8..15]  -> int16 (V13)

	VDUP	R4, V20.H8          // topLeft replicated as int16
	VSUB	V20.H8, V10.H8, V10.H8
	VSUB	V20.H8, V11.H8, V11.H8

	// Row 0: broadcast left[0] from V12.H[0]; sum + saturate.
	VDUP	V12.H[0], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce             // sqxtun  v14.8b, v14.8h
	WORD	$0x6e2129ee             // sqxtun2 v14.16b, v15.8h
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[1], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[2], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[3], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[4], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[5], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[6], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V12.H[7], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0

	VDUP	V13.H[0], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[1], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[2], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[3], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[4], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[5], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[6], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0); ADD R1, R0, R0
	VDUP	V13.H[7], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	VADD	V21.H8, V11.H8, V15.H8
	WORD	$0x2e2129ce
	WORD	$0x6e2129ee
	VST1	[V14.B16], (R0)
	RET

// intraTMPredict8x8NEON ABI ($0-33):
//   dst+0(FP)     *byte
//   stride+8(FP)  int
//   above+16(FP)  *byte
//   left+24(FP)   *byte
//   topLeft+32(FP) byte
TEXT ·intraTMPredict8x8NEON(SB), NOSPLIT, $0-33
	MOVD	dst+0(FP), R0
	MOVD	stride+8(FP), R1
	MOVD	above+16(FP), R2
	MOVD	left+24(FP), R3
	MOVBU	topLeft+32(FP), R4

	FMOVD	(R2), F0
	FMOVD	(R3), F1
	VUXTL	V0.B8, V10.H8       // above as int16x8
	VUXTL	V1.B8, V12.H8       // left  as int16x8

	VDUP	R4, V20.H8
	VSUB	V20.H8, V10.H8, V10.H8

	VDUP	V12.H[0], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce             // sqxtun v14.8b, v14.8h
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[1], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[2], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[3], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[4], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[5], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[6], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0); ADD R1, R0, R0
	VDUP	V12.H[7], V21.H8
	VADD	V21.H8, V10.H8, V14.H8
	WORD	$0x2e2129ce
	VST1	[V14.B8], (R0)
	RET
