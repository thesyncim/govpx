//go:build arm64 && !purego

#include "textflag.h"

// packTokenWindowNEON packs one staged TokenExtra window with the boolean
// range-coder state held in registers. Bit order and arithmetic mirror
// bitstream.Writer.Write exactly (vpx_dsp/bitwriter.h vpx_write), including
// the carry back-propagation over previously emitted 0xff bytes. The caller
// prechecks buffer capacity for the batch worst case, so the store path has
// no per-byte bounds check.
//
// Register plan (main body):
//   R0  args pointer
//   R1  lowValue (W)   R2 range (W)   R3 count (W, signed)
//   R4  buf base       R5 pos (X, zero-extended)
//   R6  token cursor   R7 token end
//   R8  fc base        R9 pareto base   R10 packed cat probs base
//   R11 hasResidue     R12 in-zero-run flag
//   R13 wb bit in      R14 wb prob in
//   R15-R17, R19, R20  wb scratch
//   R21 probs ptr      R22 token   R23 raw extra   R24 pareto row / cat ptr
//   R25 extra-bit counter          R26 pivot / scratch
//
// wb<> emits one boolean decision; inputs R13 (bit 0/1) and R14 (prob).

// func packTokenWindowNEON(args *packTokenKernelArgs)
TEXT ·packTokenWindowNEON(SB), NOSPLIT, $16-8
	MOVD args+0(FP), R0

	MOVWU 0(R0), R1   // lo
	MOVWU 4(R0), R2   // rng
	MOVW  8(R0), R3   // count (sign-extended)
	MOVWU 12(R0), R5  // pos
	MOVD  16(R0), R4  // buf
	MOVD  24(R0), R6  // toks
	MOVD  32(R0), R7  // nTok
	MOVD  40(R0), R8  // fc
	MOVD  48(R0), R9  // pareto
	MOVD  56(R0), R10 // cats

	MOVD R6, 8(RSP) // token base for consumed computation

	// end = toks + nTok*6
	MOVD R7, R15
	LSL  $1, R7, R16
	LSL  $2, R7, R7
	ADD  R16, R7, R7
	ADD  R6, R7, R7

	MOVD ZR, R11 // hasResidue
	MOVD ZR, R12 // in-run flag

tok_loop:
	CMP  R6, R7
	BEQ  done_window
	MOVH  0(R6), R22 // Token
	MOVH  2(R6), R23 // Extra
	MOVHU 4(R6), R21 // ProbOff
	ADD  $6, R6, R6
	ADD  R8, R21, R21 // probs = fc + ProbOff

	CMPW $0, R22
	BEQ  tok_zero
	CMPW $11, R22
	BEQ  tok_eob
	CMPW $127, R22
	BEQ  bad_stream

	// Non-zero coefficient token (ONE..CAT6).
	MOVD $1, R11
	CBNZ R12, nz_body
	// Run head: not-EOB bit against probs[0].
	MOVBU 0(R21), R14
	MOVW  $1, R13
	BL    wb<>(SB)

nz_body:
	MOVD ZR, R12
	// Not-zero bit against probs[1].
	MOVBU 1(R21), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 2(R21), R26 // pivot

	CMPW $1, R22
	BEQ  tok_one

	// pareto row = pareto + (pivot-1)*8
	SUBW $1, R26, R24
	LSL  $3, R24, R24
	ADD  R9, R24, R24

	// More-than-one bit against the pivot probability.
	MOVW R26, R14
	MOVW $1, R13
	BL   wb<>(SB)

	CMPW $2, R22
	BEQ  tok_two
	CMPW $3, R22
	BEQ  tok_three
	CMPW $4, R22
	BEQ  tok_four
	CMPW $5, R22
	BEQ  tok_cat1
	CMPW $6, R22
	BEQ  tok_cat2
	CMPW $7, R22
	BEQ  tok_cat3
	CMPW $8, R22
	BEQ  tok_cat4
	CMPW $9, R22
	BEQ  tok_cat5
	B    tok_cat6

tok_one:
	MOVW R26, R14
	MOVW $0, R13
	BL   wb<>(SB)
	B    emit_sign

tok_two:
	MOVBU 0(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 1(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	B     emit_sign

tok_three:
	MOVBU 0(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 1(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 2(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	B     emit_sign

tok_four:
	MOVBU 0(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 1(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 2(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	B     emit_sign

tok_cat1:
	MOVBU 0(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 3(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 4(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	ADD   $0, R10, R24 // cat1 probs
	MOVW  $1, R25
	B     cat_extras

tok_cat2:
	MOVBU 0(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 3(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 4(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	ADD   $1, R10, R24 // cat2 probs
	MOVW  $2, R25
	B     cat_extras

tok_cat3:
	MOVBU 0(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 3(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 5(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 6(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	ADD   $3, R10, R24 // cat3 probs
	MOVW  $3, R25
	B     cat_extras

tok_cat4:
	MOVBU 0(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 3(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 5(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVBU 6(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	ADD   $6, R10, R24 // cat4 probs
	MOVW  $4, R25
	B     cat_extras

tok_cat5:
	MOVBU 0(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 3(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 5(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 7(R24), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	ADD   $10, R10, R24 // cat5 probs
	MOVW  $5, R25
	B     cat_extras

tok_cat6:
	MOVBU 0(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 3(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 5(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	MOVBU 7(R24), R14
	MOVW  $1, R13
	BL    wb<>(SB)
	ADD   $15, R10, R24 // cat6 probs
	MOVW  $14, R25
	B     cat_extras

	// Category extra bits, MSB first. Bit b of the magnitude is bit b+1 of
	// the raw staged extra (bit 0 is the sign).
cat_extras:
	SUBW  $1, R25, R25
	ADDW  $1, R25, R13
	LSRW  R13, R23, R13
	ANDW  $1, R13, R13
	MOVBU (R24), R14
	ADD   $1, R24, R24
	BL    wb<>(SB)
	CBNZW R25, cat_extras

emit_sign:
	ANDW $1, R23, R13
	MOVW $128, R14
	BL   wb<>(SB)
	B    tok_loop

tok_zero:
	CBNZ R12, z_inrun
	// Run head: not-EOB bit against probs[0].
	MOVBU 0(R21), R14
	MOVW  $1, R13
	BL    wb<>(SB)

z_inrun:
	MOVBU 1(R21), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	MOVD  $1, R12
	B     tok_loop

tok_eob:
	// EOB inside a zero run is an invalid staged stream.
	CBNZ R12, bad_stream
	MOVBU 0(R21), R14
	MOVW  $0, R13
	BL    wb<>(SB)
	// consumed = (cursor - base) / 6
	MOVD 8(RSP), R15
	SUB  R15, R6, R15
	MOVD $6, R16
	UDIV R16, R15, R15
	MOVD R15, 72(R0) // consumed
	MOVD ZR, R16
	MOVD R16, 80(R0) // status = 0
	B    store_state

done_window:
	MOVD 32(R0), R15
	MOVD R15, 72(R0) // consumed = nTok
	MOVD ZR, R15
	MOVD R15, 80(R0) // status = 0
	B    store_state

bad_stream:
	MOVD ZR, R15
	MOVD R15, 72(R0)
	MOVD $1, R15
	MOVD R15, 80(R0)

store_state:
	MOVD R11, 64(R0) // hasResidue
	MOVW R1, 0(R0)   // lo
	MOVW R2, 4(R0)   // rng
	MOVW R3, 8(R0)   // count
	MOVW R5, 12(R0)  // pos
	RET

// wb<> encodes one boolean decision (bit in R13, prob in R14) against the
// register-resident coder state. Mirrors bitstream.Writer.Write bit-exactly.
// Clobbers R15-R17, R19, R20.
TEXT wb<>(SB), NOSPLIT, $0
	SUBW $1, R2, R15
	MULW R14, R15, R15
	LSRW $8, R15, R15
	ADDW $1, R15, R15 // split
	CBZW R13, wb_zero
	ADDW R15, R1, R1  // lo += split
	SUBW R15, R2, R2  // rng -= split
	B    wb_norm

wb_zero:
	MOVW R15, R2 // rng = split

wb_norm:
	CLZW R2, R16
	SUBW $24, R16, R16 // shift
	LSLW R16, R2, R2   // rng <<= shift
	ADDW R16, R3, R3   // count += shift
	TBZ  $31, R3, wb_emit
	LSLW R16, R1, R1 // lo <<= shift
	RET

wb_emit:
	SUBW R3, R16, R17 // offset = shift - count
	// Carry: (lo << (offset-1)) & 0x80000000
	SUBW $1, R17, R19
	LSLW R19, R1, R19
	TBZ  $31, R19, wb_store
	// Propagate the carry back through emitted 0xff bytes. The leading
	// marker bit written by Writer.Start guarantees the walk terminates
	// before the buffer start.
	SUB $1, R5, R19

wb_carry:
	MOVBU (R4)(R19), R20
	CMPW  $0xff, R20
	BNE   wb_carry_add
	MOVB  ZR, (R4)(R19)
	SUB   $1, R19, R19
	B     wb_carry

wb_carry_add:
	ADDW $1, R20, R20
	MOVB R20, (R4)(R19)

wb_store:
	// buf[pos] = byte(lo >> (24-offset)); pos++
	MOVW $24, R20
	SUBW R17, R20, R20
	LSRW R20, R1, R20
	MOVB R20, (R4)(R5)
	ADD  $1, R5, R5
	LSLW R17, R1, R1
	ANDW $0xffffff, R1, R1
	MOVW R3, R16     // shift = count
	SUBW $8, R3, R3  // count -= 8
	LSLW R16, R1, R1 // lo <<= shift
	RET
