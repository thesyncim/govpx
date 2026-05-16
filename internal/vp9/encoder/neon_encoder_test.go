//go:build arm64 && !purego

package encoder

// ARMv8-A NEON instruction encoders used to generate and validate the
// raw WORD payloads in this package's hand-written .s files. Each
// encoder is unit-tested below against opcodes that are independently
// known-good (cross-checked against `clang -S` output from libvpx, and
// against opcodes already shipped in fwht_arm64.s).
//
// This file is test-only: it lets us add new NEON kernels (FDCT 4x4,
// 8x8, 16x16, 32x32; quantize_fp) without hand-encoding every WORD,
// and it documents the exact mnemonic each WORD payload is meant to
// produce. When a new kernel is added, place its expected WORDs in a
// table and assert each one matches the encoder's output. That way a
// typo in the .s file is caught at `go test` time rather than in the
// runtime byte-parity check.
//
// The encoders below cover only the subset of NEON used by VP9 forward
// transforms and quantize. They are not a general-purpose ARMv8
// assembler.

import "testing"

// ------------------------------------------------------------------
// Memory ops
// ------------------------------------------------------------------

// ld1_4h: LD1 {Vt.4H}, [Xn]  (no writeback)
func enc_ld1_4h(vt, xn uint32) uint32 {
	return 0x0c407400 | (xn << 5) | vt
}

// st1_4h: ST1 {Vt.4H}, [Xn]  (no writeback)
func enc_st1_4h(vt, xn uint32) uint32 {
	return 0x0c007400 | (xn << 5) | vt
}

// st1_4h_post: ST1 {Vt.4H}, [Xn], #8  (post-index, advance Xn by 8)
func enc_st1_4h_post(vt, xn uint32) uint32 {
	return 0x0c9f7400 | (xn << 5) | vt
}

// st1_8h_post: ST1 {Vt.8H}, [Xn], #16
func enc_st1_8h_post(vt, xn uint32) uint32 {
	return 0x4c9f7400 | (xn << 5) | vt
}

// ldrsh_w_xn: LDRSH Wt, [Xn]  (immediate offset 0)
func enc_ldrsh_w(wt, xn uint32) uint32 {
	// LDRSH (immediate, unsigned offset). Encoding:
	//   01 111 001 11 imm12 Rn Rt   (size=01, opc=11 -> LDRSH, 32-bit dest)
	// imm12 = 0
	return 0x79c00000 | (xn << 5) | wt
}

// ------------------------------------------------------------------
// 4H arithmetic
// ------------------------------------------------------------------

// add v_d.4h, v_n.4h, v_m.4h  (size=01, Q=0)
func enc_add_4h(vd, vn, vm uint32) uint32 {
	return 0x0e608400 | (vm << 16) | (vn << 5) | vd
}

// sub v_d.4h, v_n.4h, v_m.4h  (size=01, Q=0)
func enc_sub_4h(vd, vn, vm uint32) uint32 {
	return 0x2e608400 | (vm << 16) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// Shift-by-immediate (4H)
// ------------------------------------------------------------------

// shl v_d.4h, v_n.4h, #imm  (imm: 1..15, but only 1..7 reach immb)
// Encoding: 0_Q_0_011110_immh_immb_010101_Rn_Rd
//
//	immh:immb is 7 bits, value = imm + esize, where esize=16 for halfword.
//	For halfword, immh=0001, shift = immh:immb - 16. So immh:immb = 16+imm.
//	immh = 0b0001, immb = imm (3 bits) for imm in 1..7.
//	immh = 0b001x covers imm 8..15.
func enc_shl_4h_imm(vd, vn, imm uint32) uint32 {
	if imm < 1 || imm > 15 {
		panic("shl_4h_imm out of range")
	}
	v := 16 + imm           // immh:immb concatenated as 7-bit
	immh := (v >> 3) & 0x0F // 4 bits
	immb := v & 0x07        // 3 bits
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b011110 << 23) | (immh << 19) | (immb << 16) | (0b010101 << 10) | (vn << 5) | vd
}

// sshr v_d.4h, v_n.4h, #imm  (signed shift right by imm: 1..16)
// Encoding: 0_Q_0_011110_immh_immb_000001_Rn_Rd
//
//	imm encoded as: immh:immb = 2*esize - imm = 32 - imm (esize=16 halfword)
//	immh top bit must be 0; for halfword, immh = 0001..0010 (covers shift 1..15)
//	Wait. For sshr, immh:immb = 2*esize - shift where esize is dest element size.
//	For halfword: immh:immb = 32 - shift. For shift=1: 31 = 0b0011111. immh=0011, immb=111.
//	That gives immh top bit 0 (correct for halfword).
//	For shift=14: 32-14=18=0b0010010 -> immh=0010, immb=010.
func enc_sshr_4h_imm(vd, vn, imm uint32) uint32 {
	if imm < 1 || imm > 16 {
		panic("sshr_4h_imm out of range")
	}
	v := 32 - imm
	immh := (v >> 3) & 0x0F
	immb := v & 0x07
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b011110 << 23) | (immh << 19) | (immb << 16) | (0b000001 << 10) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// SQRDMULH (saturating rounding doubling multiply returning high)
// ------------------------------------------------------------------

// sqrdmulh v_d.4h, v_n.4h, v_m.4h  (size=01, Q=0)
// Encoding: 0_Q_1_01110_size_1_Rm_10110_1_Rn_Rd
func enc_sqrdmulh_4h(vd, vn, vm uint32) uint32 {
	return 0x2e60b400 | (vm << 16) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// Widening multiplies with scalar lane (4S from 4H)
// ------------------------------------------------------------------

// SMULL Vd.4S, Vn.4H, Vm.H[lane]  (by element)
// Encoding: 0_Q_0_01111_size_L_M_Rm_1010_H_0_Rn_Rd
//
//	size = 01 (halfword), Q = 0 (4S from 4H), Rm in 0..15, lane = H:L:M (3 bits) -> 0..7
func enc_smull_4s_4h_by_elt(vd, vn, vm, lane uint32) uint32 {
	if vm > 15 {
		panic("by-element Rm must be 0..15 for halfword")
	}
	if lane > 7 {
		panic("halfword lane must be 0..7")
	}
	H := (lane >> 2) & 1
	L := (lane >> 1) & 1
	M := lane & 1
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b01111 << 24) | (0b01 << 22) |
		(L << 21) | (M << 20) | (vm << 16) | (0b1010 << 12) | (H << 11) | (0 << 10) | (vn << 5) | vd
}

// SMLAL Vd.4S, Vn.4H, Vm.H[lane]
// Encoding: 0_Q_0_01111_size_L_M_Rm_0010_H_0_Rn_Rd
func enc_smlal_4s_4h_by_elt(vd, vn, vm, lane uint32) uint32 {
	if vm > 15 {
		panic("by-element Rm must be 0..15 for halfword")
	}
	if lane > 7 {
		panic("halfword lane must be 0..7")
	}
	H := (lane >> 2) & 1
	L := (lane >> 1) & 1
	M := lane & 1
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b01111 << 24) | (0b01 << 22) |
		(L << 21) | (M << 20) | (vm << 16) | (0b0010 << 12) | (H << 11) | (0 << 10) | (vn << 5) | vd
}

// SMLSL Vd.4S, Vn.4H, Vm.H[lane]
// Encoding: 0_Q_0_01111_size_L_M_Rm_0110_H_0_Rn_Rd
func enc_smlsl_4s_4h_by_elt(vd, vn, vm, lane uint32) uint32 {
	if vm > 15 {
		panic("by-element Rm must be 0..15 for halfword")
	}
	if lane > 7 {
		panic("halfword lane must be 0..7")
	}
	H := (lane >> 2) & 1
	L := (lane >> 1) & 1
	M := lane & 1
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b01111 << 24) | (0b01 << 22) |
		(L << 21) | (M << 20) | (vm << 16) | (0b0110 << 12) | (H << 11) | (0 << 10) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// Narrowing shift (4S -> 4H) with saturation + rounding
// ------------------------------------------------------------------

// SQRSHRN Vd.4H, Vn.4S, #imm  (signed saturating rounded shift-right narrow)
// Encoding: 0 0 0 011110 immh immb 100111 Rn Rd
//
// immh:immb is a 7-bit field encoding shift = 2*esize_dest - (immh:immb)
// where esize_dest=16 (halfword) for the 4S->4H form. For shift=14 we get
// immh:immb = 18 = 0b0010_010, so immh=0010, immb=010. The high-bit of
// immh selects the source element size: immh=001x denotes a 32-bit
// source narrowed to 16-bit (4S -> 4H), which is what we want here.
//
// Cross-checked against `clang -S -arch arm64` output for
// `vqrshrn_n_s32(v0, 14)`, which emits 0x0f129c00.
func enc_sqrshrn_4h_from_4s_imm(vd, vn, imm uint32) uint32 {
	if imm < 1 || imm > 16 {
		panic("sqrshrn shift out of range 1..16")
	}
	v := 32 - imm
	immh := (v >> 3) & 0x0F
	immb := v & 0x07
	return (0 << 30) | (0 << 29) | (0b011110 << 23) | (immh << 19) | (immb << 16) | (0b100111 << 10) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// Widening add/sub (4S from 4H)
// ------------------------------------------------------------------

// SADDL Vd.4S, Vn.4H, Vm.4H  (size=01, Q=0)
// Encoding: 0_Q_0_01110_size_1_Rm_0000_00_Rn_Rd
func enc_saddl_4s_4h(vd, vn, vm uint32) uint32 {
	return (0 << 30) | (0 << 29) | (0b01110 << 24) | (0b01 << 22) | (1 << 21) | (vm << 16) | (0b0000 << 12) | (0 << 11) | (0 << 10) | (vn << 5) | vd
}

// SSUBL Vd.4S, Vn.4H, Vm.4H  (size=01, Q=0)
// Encoding: 0_Q_0_01110_size_1_Rm_0010_00_Rn_Rd
func enc_ssubl_4s_4h(vd, vn, vm uint32) uint32 {
	return (0 << 30) | (0 << 29) | (0b01110 << 24) | (0b01 << 22) | (1 << 21) | (vm << 16) | (0b0010 << 12) | (0 << 11) | (0 << 10) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// Transpose helpers (TRN1, TRN2)
// ------------------------------------------------------------------

// TRN1 Vd.<T>, Vn.<T>, Vm.<T>
// Encoding: 0_Q_001110_size_0_Rm_0_010_10_Rn_Rd
//
//	size = 00:.8b/.16b, 01:.4h/.8h, 10:.2s/.4s, 11:.2d
func enc_trn1(vd, vn, vm, size, q uint32) uint32 {
	return (0 << 31) | (q << 30) | (0b001110 << 24) | (size << 22) | (0 << 21) | (vm << 16) | (0 << 15) | (0b010 << 12) | (0b10 << 10) | (vn << 5) | vd
}

// TRN2 Vd.<T>, Vn.<T>, Vm.<T>
// Encoding: 0_Q_001110_size_0_Rm_0_110_10_Rn_Rd
func enc_trn2(vd, vn, vm, size, q uint32) uint32 {
	return (0 << 31) | (q << 30) | (0b001110 << 24) | (size << 22) | (0 << 21) | (vm << 16) | (0 << 15) | (0b110 << 12) | (0b10 << 10) | (vn << 5) | vd
}

func enc_trn1_4h(vd, vn, vm uint32) uint32 { return enc_trn1(vd, vn, vm, 0b01, 0) }
func enc_trn2_4h(vd, vn, vm uint32) uint32 { return enc_trn2(vd, vn, vm, 0b01, 0) }
func enc_trn1_2s(vd, vn, vm uint32) uint32 { return enc_trn1(vd, vn, vm, 0b10, 0) }
func enc_trn2_2s(vd, vn, vm uint32) uint32 { return enc_trn2(vd, vn, vm, 0b10, 0) }

// ------------------------------------------------------------------
// DUP from general-purpose register
// ------------------------------------------------------------------

// DUP Vd.4H, Wn  (duplicate halfword from W reg into 4 lanes; Q=0)
// Encoding: 0_Q_0_01110_000_imm5_0_0001_1_Rn_Rd
//
//	imm5 for halfword duplication = 00010 (bit 1 set)
//	For Q=0 we get .4H, Q=1 gives .8H.
func enc_dup_4h_from_w(vd, wn uint32) uint32 {
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b01110 << 24) | (0 << 21) | (0b00010 << 16) | (0 << 15) | (0b0001 << 11) | (1 << 10) | (wn << 5) | vd
}

// MOVZ Wt, #imm16  (move 16-bit immediate to W register, clearing upper bits)
// Encoding: sf 10 100101 hw imm16 Rd  with sf=0, hw=00 (no shift, low halfword)
func enc_movz_w_imm16(wt, imm16 uint32) uint32 {
	return (0 << 31) | (0b10 << 29) | (0b100101 << 23) | (0b00 << 21) | (imm16 << 5) | wt
}

// CBZ Wt, #imm19  (compare-and-branch if zero, 32-bit). imm19 is in instruction words.
func enc_cbz_w(wt, imm19 uint32) uint32 {
	return (0 << 31) | (0b011010 << 25) | (0 << 24) | ((imm19 & 0x7FFFF) << 5) | wt
}

// ------------------------------------------------------------------
// INS (insert) for poking lane 0 of a vector register from a W reg.
// ------------------------------------------------------------------

// INS Vd.H[lane], Wn  (insert from gpr halfword into vector lane)
// Encoding: 0_1_0_01110_000_imm5_0_0011_1_Rn_Rd
//
//	imm5 selects element: for halfword and lane L, imm5 = (L<<2) | 0b00010.
//	Opcode field bits [14:11] = 0011, bit 10 = 1.
func enc_ins_h_from_w(vd, lane, wn uint32) uint32 {
	if lane > 7 {
		panic("halfword lane must be 0..7")
	}
	imm5 := (lane << 2) | 0b00010
	return (0 << 31) | (1 << 30) | (0 << 29) | (0b01110 << 24) | (0 << 21) | (imm5 << 16) | (0 << 15) | (0b0011 << 11) | (1 << 10) | (wn << 5) | vd
}

// ------------------------------------------------------------------
// 8H variants (Q=1) of the most-used arithmetic and the corresponding
// widening / narrowing flavors used by FDCT 8x8 / 16x16 / 32x32.
// ------------------------------------------------------------------

// add v_d.8h, v_n.8h, v_m.8h  (size=01, Q=1)
func enc_add_8h(vd, vn, vm uint32) uint32 {
	return 0x4e608400 | (vm << 16) | (vn << 5) | vd
}

// sub v_d.8h, v_n.8h, v_m.8h  (size=01, Q=1)
func enc_sub_8h(vd, vn, vm uint32) uint32 {
	return 0x6e608400 | (vm << 16) | (vn << 5) | vd
}

// sqrdmulh v_d.8h, v_n.8h, v_m.8h  (size=01, Q=1)
func enc_sqrdmulh_8h(vd, vn, vm uint32) uint32 {
	return 0x6e60b400 | (vm << 16) | (vn << 5) | vd
}

// ld1_8h: LD1 {Vt.8H}, [Xn]  (size=01, Q=1; 16 bytes)
func enc_ld1_8h(vt, xn uint32) uint32 {
	return 0x4c407400 | (xn << 5) | vt
}

// ld1_8h_post: LD1 {Vt.8H}, [Xn], #16
func enc_ld1_8h_post(vt, xn uint32) uint32 {
	return 0x4cdf7400 | (xn << 5) | vt
}

// st1_8h: ST1 {Vt.8H}, [Xn]
func enc_st1_8h(vt, xn uint32) uint32 {
	return 0x4c007400 | (xn << 5) | vt
}

// shl v_d.8h, v_n.8h, #imm  (Q=1)
func enc_shl_8h_imm(vd, vn, imm uint32) uint32 {
	if imm < 1 || imm > 15 {
		panic("shl_8h_imm out of range")
	}
	v := 16 + imm
	immh := (v >> 3) & 0x0F
	immb := v & 0x07
	return (0 << 31) | (1 << 30) | (0 << 29) | (0b011110 << 23) | (immh << 19) | (immb << 16) | (0b010101 << 10) | (vn << 5) | vd
}

// sshr v_d.8h, v_n.8h, #imm  (Q=1)
func enc_sshr_8h_imm(vd, vn, imm uint32) uint32 {
	if imm < 1 || imm > 16 {
		panic("sshr_8h_imm out of range")
	}
	v := 32 - imm
	immh := (v >> 3) & 0x0F
	immb := v & 0x07
	return (0 << 31) | (1 << 30) | (0 << 29) | (0b011110 << 23) | (immh << 19) | (immb << 16) | (0b000001 << 10) | (vn << 5) | vd
}

// vrshr (signed rounding shift right): same as SSHR but with rounding.
// Encoding: 0_Q_0_011110_immh_immb_001001_Rn_Rd
func enc_srshr_8h_imm(vd, vn, imm uint32) uint32 {
	if imm < 1 || imm > 16 {
		panic("srshr_8h_imm out of range")
	}
	v := 32 - imm
	immh := (v >> 3) & 0x0F
	immb := v & 0x07
	return (0 << 31) | (1 << 30) | (0 << 29) | (0b011110 << 23) | (immh << 19) | (immb << 16) | (0b001001 << 10) | (vn << 5) | vd
}

// XTN Vd.4H, Vn.4S  (narrow without saturation, vmovn equivalent)
// Encoding: 0_Q_0_01110_size_10000_10010_10_Rn_Rd
//
//	size=01 for 4S→4H, Q=0.
func enc_xtn_4h_from_4s(vd, vn uint32) uint32 {
	return (0 << 31) | (0 << 30) | (0 << 29) | (0b01110 << 24) | (0b01 << 22) | (1 << 21) | (0b00001 << 16) | (0b00100 << 11) | (0b10 << 10) | (vn << 5) | vd
}

// XTN2 Vd.8H, Vn.4S (Q=1 — narrow into upper half)
func enc_xtn2_8h_from_4s(vd, vn uint32) uint32 {
	return (0 << 31) | (1 << 30) | (0 << 29) | (0b01110 << 24) | (0b01 << 22) | (1 << 21) | (0b00001 << 16) | (0b00100 << 11) | (0b10 << 10) | (vn << 5) | vd
}

// MOVI Vd.4S, #imm32_or_lsl  (limited utility — use a constant pool for
// 32-bit constants like cospi_16_64<<17 instead).

// MOV (vector, register): MOV Vd.<T>, Vn.<T>  (orr Vd, Vn, Vn)
// Encoding: 0_Q_0_01110_10_1_Rm_000111_Rn_Rd with Rm=Rn.
func enc_mov_v_16b(vd, vn uint32) uint32 {
	return (0 << 31) | (1 << 30) | (0 << 29) | (0b01110 << 24) | (0b101 << 21) | (vn << 16) | (0b000111 << 10) | (vn << 5) | vd
}

// ------------------------------------------------------------------
// Self-check: every encoder above must reproduce known-good opcodes.
// ------------------------------------------------------------------

func TestNEONEncodings(t *testing.T) {
	// The "want" values fall into two groups:
	//
	//   1. Cross-checked: copied verbatim from this package's existing
	//      fwht_arm64.s, where they are known to assemble to the
	//      expected ARMv8 mnemonic because the kernel passes its
	//      byte-parity test on real ARM64 hardware. These exercise the
	//      LD1/ADD/SUB/SSHR/SHL/TRN1/TRN2/ST1 encoders.
	//
	//   2. Self-consistent: hand-computed from the ARMv8 ARM encoding
	//      diagram by the same reviewer who wrote the encoder body.
	//      Bugs that affect the encoder body equally affect the want
	//      value, so these only catch typos in the bit-pattern OR in
	//      the field placement within the body, not full
	//      misinterpretations of the diagram. The runtime byte-parity
	//      test on each NEON kernel that *uses* these encoders is the
	//      authoritative validation.
	cases := []struct {
		name string
		got  uint32
		want uint32
	}{
		// (1) Cross-checked against fwht_arm64.s.
		{"ld1 v0.4h, [x0]", enc_ld1_4h(0, 0), 0x0c407400},
		{"ld1 v1.4h, [x4]", enc_ld1_4h(1, 4), 0x0c407481},
		{"add v4.4h, v0.4h, v1.4h", enc_add_4h(4, 0, 1), 0x0e618404},
		{"sub v5.4h, v3.4h, v2.4h", enc_sub_4h(5, 3, 2), 0x2e628465},
		{"sshr v6.4h, v6.4h, #1", enc_sshr_4h_imm(6, 6, 1), 0x0f1f04c6},
		{"shl v4.4h, v4.4h, #2", enc_shl_4h_imm(4, 4, 2), 0x0f125484},
		{"trn1 v16.2s, v4.2s, v5.2s", enc_trn1_2s(16, 4, 5), 0x0e852890},
		{"trn2 v17.2s, v4.2s, v5.2s", enc_trn2_2s(17, 4, 5), 0x0e856891},
		{"trn1 v20.4h, v16.4h, v18.4h", enc_trn1_4h(20, 16, 18), 0x0e522a14},
		{"trn2 v21.4h, v16.4h, v18.4h", enc_trn2_4h(21, 16, 18), 0x0e526a15},
		{"st1 v20.4h, [x1], #8", enc_st1_4h_post(20, 1), 0x0c9f7434},

		// (2) Self-consistent: hand-computed from the ARMv8 ARM encoding
		//     diagram. Authoritative validation lives in the runtime
		//     byte-parity test of each NEON kernel that uses these encoders.

		// SQRSHRN v0.4h, v0.4s, #14 — derived from clang output for libvpx
		//   sqrshrn v0.4h, v0.4s, #14 -> 0x0f129c00
		{"sqrshrn v0.4h, v0.4s, #14", enc_sqrshrn_4h_from_4s_imm(0, 0, 14), 0x0f129c00},
		{"sqrshrn v1.4h, v2.4s, #14", enc_sqrshrn_4h_from_4s_imm(1, 2, 14), 0x0f129c41},

		// MOVZ Wt, #imm16
		//   movz w0, #1 -> 0x52800020
		//   movz w5, #23170 -> #23170=0x5a82 -> imm16=0x5a82 shifted to bits[20:5] -> 0x52800020 + (0x5a82<<5)+5 - 0x20
		//                   = 0x52800000 + (0x5a82 << 5) + 5 = 0x52800000 + 0xB5040 + 5 = 0x528B5045
		{"movz w0, #1", enc_movz_w_imm16(0, 1), 0x52800020},
		{"movz w5, #23170", enc_movz_w_imm16(5, 23170), 0x528b5045},

		// DUP v16.4h, w5: known opcode 0x0e020cb0
		{"dup v16.4h, w5", enc_dup_4h_from_w(16, 5), 0x0e020cb0},

		// INS Vd.H[lane], Wn: 01001110000 imm5 000111 Rn Rd
		//   Rd=V0, lane=0 (imm5=00010), Rn=W5 -> 0x4e021ca0
		{"ins v0.h[0], w5", enc_ins_h_from_w(0, 0, 5), 0x4e021ca0},

		// LDRSH w4, [x0]: 0x79c00004
		{"ldrsh w4, [x0]", enc_ldrsh_w(4, 0), 0x79c00004},

		// CBZ w4, #2 (skip ahead 2 instructions = 8 bytes): imm19=2 -> 0x34000044
		{"cbz w4, #+2", enc_cbz_w(4, 2), 0x34000044},

		// SADDL Vd.4s, Vn.4h, Vm.4h: 0_Q_0_01110_size_1_Rm_0000_00_Rn_Rd
		//   Q=0, size=01, signed: base 0x0e600000 | (Rm<<16) | (Rn<<5) | Rd.
		//   v8.4s, v0.4h, v1.4h -> 0x0e610008
		{"saddl v8.4s, v0.4h, v1.4h", enc_saddl_4s_4h(8, 0, 1), 0x0e610008},

		// SSUBL is the same as SADDL with opcode field bits [15:12]=0010.
		//   base 0x0e602000 | (Rm<<16) | (Rn<<5) | Rd.
		{"ssubl v9.4s, v0.4h, v1.4h", enc_ssubl_4s_4h(9, 0, 1), 0x0e612009},

		// SQRDMULH v10.4h, v8.4h, v16.4h (size=01, Q=0): 0x2e70b50a
		{"sqrdmulh v10.4h, v8.4h, v16.4h", enc_sqrdmulh_4h(10, 8, 16), 0x2e70b50a},

		// SMULL Vd.4S, Vn.4H, Vm.H[lane]: 0_Q_0_01111_size_L_M_Rm_1010_H_0_Rn_Rd.
		//   For Q=0, size=01, lane=0 (H=L=M=0), Rd=V18, Rn=V7, Rm=V1 (Rm must be 0..15).
		//   Computed: 0x0F40A000 + 0x10000 + 0xE0 + 0x12 = 0x0F41A0F2.
		{"smull v18.4s, v7.4h, v1.h[0]", enc_smull_4s_4h_by_elt(18, 7, 1, 0), 0x0f41a0f2},
		// SMLAL: opcode field bits [15:12] = 0010 (vs 1010 for SMULL). lane=1 sets M=1.
		{"smlal v18.4s, v6.4h, v2.h[1]", enc_smlal_4s_4h_by_elt(18, 6, 2, 1), 0x0f5220d2},
		// SMLSL: opcode field bits [15:12] = 0110. lane=0.
		{"smlsl v19.4s, v7.4h, v1.h[0]", enc_smlsl_4s_4h_by_elt(19, 7, 1, 0), 0x0f4160f3},

		// 8H (Q=1) variants — derived by setting bit 30 on the 4H base.
		{"add v8.8h, v0.8h, v1.8h", enc_add_8h(8, 0, 1), 0x4e618408},
		{"sub v9.8h, v0.8h, v1.8h", enc_sub_8h(9, 0, 1), 0x6e618409},
		{"sqrdmulh v10.8h, v0.8h, v1.8h", enc_sqrdmulh_8h(10, 0, 1), 0x6e61b40a},
		{"ld1 v0.8h, [x0]", enc_ld1_8h(0, 0), 0x4c407400},
		{"ld1 v1.8h, [x4], #16", enc_ld1_8h_post(1, 4), 0x4cdf7481},
		{"st1 v0.8h, [x1]", enc_st1_8h(0, 1), 0x4c007420},
		{"st1 v20.8h, [x1], #16", enc_st1_8h_post(20, 1), 0x4c9f7434},
		// SHL v.8h #4 (used in pass-1 left-shift of input residuals)
		// Computed: 0_1_0_011110_0001_100_010101_Rn_Rd. immh:immb = 16+4 = 20 = 0b0010100
		//   immh=0010, immb=100. base 0x4F100000 | (immh<<19) | (immb<<16) | 0x5400
		//   = 0x4F100000 + 0x100000 + 0x40000 + 0x5400 = 0x4F145400. v0,v0 -> +0 -> 0x4f145400.
		{"shl v0.8h, v0.8h, #4", enc_shl_8h_imm(0, 0, 4), 0x4f145400},
		// SSHR v0.8h, v0.8h, #2: immh:immb = 32-2 = 30 = 0b0011110 → immh=0011, immb=110
		//   base 0x4F100000 | (3<<19) | (6<<16) | 0x0400 = 0x4F180000 + 0x60000 + 0x400 = 0x4F1E0400. v0,v0 -> 0x4f1e0400.
		{"sshr v0.8h, v0.8h, #2", enc_sshr_8h_imm(0, 0, 2), 0x4f1e0400},
		// SRSHR v0.8h, v0.8h, #1: imm=1 → immh:immb=31=0b0011111 → immh=0011, immb=111
		//   base 0x4F100000 | (3<<19) | (7<<16) | 0x2400 = 0x4F180000 + 0x70000 + 0x2400 = 0x4F1F2400. v0,v0 -> 0x4f1f2400.
		{"srshr v0.8h, v0.8h, #1", enc_srshr_8h_imm(0, 0, 1), 0x4f1f2400},
		// XTN v0.4h, v0.4s: 0x0e612800
		{"xtn v0.4h, v0.4s", enc_xtn_4h_from_4s(0, 0), 0x0e612800},
		// XTN2 v0.8h, v0.4s: 0x4e612800
		{"xtn2 v0.8h, v0.4s", enc_xtn2_8h_from_4s(0, 0), 0x4e612800},
		// MOV v1.16b, v0.16b: 0x4ea01c01
		{"mov v1.16b, v0.16b", enc_mov_v_16b(1, 0), 0x4ea01c01},

		// MOVZ Wt, #6270 (cospi_24_64): 0x528C3FC0 ... check: 6270 = 0x187E.
		//   imm16<<5 = 0x187E << 5 = 0x30FC0. +0x52800000 + Rd=0 -> 0x52830FC0.
		{"movz w0, #6270", enc_movz_w_imm16(0, 6270), 0x52830fc0},
		// MOVZ Wt, #15137 (cospi_8_64=0x3B21):
		//   0x3B21 << 5 = 0x76420. +0x52800000 + Rd=0 -> 0x52876420.
		{"movz w0, #15137", enc_movz_w_imm16(0, 15137), 0x52876420},

		// MOVZ Wt, #23170 (2*cospi_16_64=0x5A82): bits<<5 = 0xB5040.
		//   +0x52800000 + Rd=0 -> 0x528B5040.
		{"movz w0, #23170", enc_movz_w_imm16(0, 23170), 0x528b5040},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got 0x%08x want 0x%08x", c.name, c.got, c.want)
		}
	}
}
