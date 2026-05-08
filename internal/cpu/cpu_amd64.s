// CPUID and XGETBV trampolines for AVX2 detection.
//
// Calling conventions (ABI0):
//
//   func cpuid(eax, ecx uint32) (a, b, c, d uint32)
//     in:  eax+0(FP) uint32, ecx+4(FP) uint32
//     out: a+8(FP), b+12(FP), c+16(FP), d+20(FP)
//
//   func xgetbv(idx uint32) (lo, hi uint32)
//     in:  idx+0(FP) uint32 (XCR index, typically 0)
//     out: lo+8(FP) uint32, hi+12(FP) uint32

#include "textflag.h"

TEXT ·cpuid(SB), NOSPLIT, $0-24
	MOVL	eax+0(FP), AX
	MOVL	ecx+4(FP), CX
	CPUID
	MOVL	AX, a+8(FP)
	MOVL	BX, b+12(FP)
	MOVL	CX, c+16(FP)
	MOVL	DX, d+20(FP)
	RET

TEXT ·xgetbv(SB), NOSPLIT, $0-16
	MOVL	idx+0(FP), CX
	// XGETBV: read XCR[ECX] -> EDX:EAX. Encoded as the raw bytes
	// because the Go assembler doesn't accept the mnemonic on every
	// version we target.
	BYTE	$0x0f
	BYTE	$0x01
	BYTE	$0xd0
	MOVL	AX, lo+8(FP)
	MOVL	DX, hi+12(FP)
	RET
