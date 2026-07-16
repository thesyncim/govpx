package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 coefficient token-encoding tables and per-coefficient writer.
// Ported from libvpx v1.16.0 vp9/common/vp9_entropy.c
// (vp9_coef_con_tree) and vp9/encoder/vp9_tokenize.c
// (vp9_coef_encodings) plus the per-token write fragment in
// pack_mb_tokens.
//
// The tables drive every non-zero coefficient: the token class +
// extra-bits split is determined by the magnitude (see
// TokenForAbsCoeff), and the wire fragment is the EOB / ZERO / ONE
// node trio plus optional pareto-tree walk + extra magnitude bits
// + sign.

// CoefConTree mirrors libvpx's vp9_coef_con_tree — the binary tree
// over TWO/THREE/FOUR/CAT1..CAT6. Internal nodes are positive
// indices into the array; leaves are -token_class.
var CoefConTree = [16]int8{
	2, 6,
	-TwoToken, 4,
	-ThreeToken, -FourToken,
	8, 10,
	-Category1Tok, -Category2Tok,
	12, 14,
	-Category3Tok, -Category4Tok,
	-Category5Tok, -Category6Tok,
}

// CoefTree mirrors libvpx's vp9_coef_tree (vp9/encoder/vp9_tokenize.c:75)
// — the full 11-internal-node tree over all 12 entropy tokens, including
// the three unconstrained nodes (EOB, ZERO, ONE) that the encoder writes
// directly before walking the pareto8-modeled tail. The table drives
// libvpx's fill_token_costs (vp9/encoder/vp9_rd.c:135-152) via
// vp9_cost_tokens; govpx exposes it so the Go-side pinning test can
// replay the same walk against the libvpx-oracle cost blob byte-for-byte.
//
// Internal nodes are positive offsets into the array (always even);
// leaves are -token_class, mapping to the indices in
// vp9_entropy.h:27-38 (ZERO_TOKEN..EOB_TOKEN).
var CoefTree = [22]int8{
	-EobToken, 2, //         0  = EOB
	-ZeroToken, 4, //        1  = ZERO
	-OneToken, 6, //         2  = ONE
	8, 12, //                3  = LOW_VAL
	-TwoToken, 10, //        4  = TWO
	-ThreeToken, -FourToken, //  5  = THREE
	14, 16, //                6  = HIGH_LOW
	-Category1Tok, -Category2Tok, // 7 = CAT_ONE
	18, 20, //                       8 = CAT_THREEFOUR
	-Category3Tok, -Category4Tok, // 9 = CAT_THREE
	-Category5Tok, -Category6Tok, // 10 = CAT_FIVE
}

// CoefEncoding mirrors libvpx's struct vp9_token — the (value, len)
// bit pattern that walks CoefConTree to the matching leaf. The
// encoder picks Encodings[token].Value and emits its top `Len` bits
// MSB-first against the active probability row.
type CoefEncoding struct {
	Value uint8
	Len   uint8
}

// CoefEncodings mirrors libvpx's vp9_coef_encodings[ENTROPY_TOKENS].
// The (value, len) pair for each token class is the bit pattern
// the encoder emits to reach that leaf in CoefConTree.
//
// Layout: indices 0..3 (ZERO/ONE/TWO/THREE) are unused by the
// tokenized fragment — the EOB / ZERO / ONE bits before the
// CoefConTree walk handle those classes directly. The CAT* and
// FOUR_TOKEN slots carry the meaningful patterns.
var CoefEncodings = [EntropyTokens]CoefEncoding{
	{2, 2},   // ZeroToken (unused)
	{6, 3},   // OneToken (unused; ONE handled before tree)
	{28, 5},  // TwoToken
	{58, 6},  // ThreeToken
	{59, 6},  // FourToken
	{60, 6},  // Category1Tok
	{61, 6},  // Category2Tok
	{124, 7}, // Category3Tok
	{125, 7}, // Category4Tok
	{126, 7}, // Category5Tok
	{127, 7}, // Category6Tok
	{0, 1},   // EobToken (unused)
}

// UnconstrainedNodes mirrors libvpx's UNCONSTRAINED_NODES — the
// three nodes (EOB, ZERO, ONE) before the pareto-modeled tail.
const UnconstrainedNodes = 3

// WriteTokenForCoeff emits the wire fragment for one coefficient in
// the post-zero-run position. The caller has already written the
// non-EOB and non-ZERO unconstrained-tree bits; this function
// handles everything from the ONE node onward:
//
//   - ONE token: write 0 against ctx[2] then sign.
//   - TWO..CAT6: write 1 against ctx[2], walk CoefConTree against
//     Pareto8[ctx[PIVOT]-1], emit Len extra bits for CAT* against
//     the matching Cat{n}Prob row, then sign.
//
// `ctxTree` is the active band/context probability slice with at
// least 3 entries (EOB, ZERO, ONE/PIVOT).
//
// Returns true if the coefficient was emittable. CAT classes that
// haven't been wired here (none yet) return false.
func WriteTokenForCoeff(bw *bitstream.Writer, ctxTree []uint8, absCoeff int, sign int) bool {
	if len(ctxTree) < UnconstrainedNodes {
		return false
	}
	return writeTokenForCoeff(bw, (*[UnconstrainedNodes]uint8)(ctxTree[:UnconstrainedNodes]),
		absCoeff, sign, nil)
}

func writeTokenForCoeff(
	bw *bitstream.Writer, ctxTree *[UnconstrainedNodes]uint8, absCoeff int, sign int,
	branchStats *[EntropyNodes][2]uint32,
) bool {
	if absCoeff == 0 {
		// Caller should have already written the zero bit.
		return false
	}
	token, extra := TokenForAbsCoeff(absCoeff)
	if token == OneToken {
		recordCoefBranchPivot0(branchStats)
		writePackedCoefTokenBody(bw, token, extra, sign, ctxTree[2])
		return true
	}
	// Non-ONE: emit the "go to pareto" bit at ctx[2].
	recordCoefBranchPivot1(branchStats)
	pivot := ctxTree[2]
	if pivot == 0 {
		panic("encoder: invalid VP9 coefficient probability")
	}
	recordCoefTokenTailBranches(token, branchStats)
	writePackedCoefTokenBody(bw, token, extra, sign, pivot)
	return true
}

func writePackedCoefTokenBody(
	bw *bitstream.Writer, token int, extra int, sign int, pivot uint8,
) {
	signBit := uint32(sign & 1)
	switch token {
	case OneToken:
		bw.WritePacked(signBit, uint32(pivot)<<8|128, 2)
		return
	case TwoToken:
		if pivot == 0 {
			panic("encoder: invalid VP9 coefficient probability")
		}
		pareto := &tables.Pareto8Full[pivot-1]
		bw.WritePacked(0b1000|signBit, uint32(pivot)<<24|
			uint32(pareto[0])<<16|uint32(pareto[1])<<8|128, 4)
		return
	case ThreeToken, FourToken, Category1Tok, Category2Tok:
		if pivot == 0 {
			panic("encoder: invalid VP9 coefficient probability")
		}
		pareto := &tables.Pareto8Full[pivot-1]
		switch token {
		case ThreeToken:
			bw.WritePacked(0b1010, uint32(pivot)<<24|
				uint32(pareto[0])<<16|uint32(pareto[1])<<8|
				uint32(pareto[2]), 4)
		case FourToken:
			bw.WritePacked(0b1011, uint32(pivot)<<24|
				uint32(pareto[0])<<16|uint32(pareto[1])<<8|
				uint32(pareto[2]), 4)
		case Category1Tok:
			bw.WritePacked(0b1100, uint32(pivot)<<24|
				uint32(pareto[0])<<16|uint32(pareto[3])<<8|
				uint32(pareto[4]), 4)
		case Category2Tok:
			bw.WritePacked(0b1101, uint32(pivot)<<24|
				uint32(pareto[0])<<16|uint32(pareto[3])<<8|
				uint32(pareto[4]), 4)
		}
	default:
		if pivot == 0 {
			panic("encoder: invalid VP9 coefficient probability")
		}
		bw.Write(1, uint32(pivot))
		writePackedCoefTokenTail(bw, token, &tables.Pareto8Full[pivot-1])
	}
	if token >= Category1Tok {
		writeCoefExtraBits(bw, token, extra)
	}
	bw.WriteBit(signBit)
}

func writePackedCoefTokenBodyAfterNotZero(
	bw *bitstream.Writer, token int, extra int, sign int, zeroProb, pivot uint8,
) {
	signBit := uint32(sign & 1)
	switch token {
	case OneToken:
		bw.WritePacked(0b100|signBit, uint32(zeroProb)<<16|uint32(pivot)<<8|128, 3)
		return
	}
	if pivot == 0 {
		panic("encoder: invalid VP9 coefficient probability")
	}
	pareto := &tables.Pareto8Full[pivot-1]
	switch token {
	case TwoToken:
		bw.WritePacked64(0b11000|signBit, uint64(zeroProb)<<32|
			uint64(pivot)<<24|uint64(pareto[0])<<16|
			uint64(pareto[1])<<8|128, 5)
	case ThreeToken:
		bw.WritePacked64(0b110100|signBit, uint64(zeroProb)<<40|
			uint64(pivot)<<32|uint64(pareto[0])<<24|
			uint64(pareto[1])<<16|uint64(pareto[2])<<8|128, 6)
	case FourToken:
		bw.WritePacked64(0b110110|signBit, uint64(zeroProb)<<40|
			uint64(pivot)<<32|uint64(pareto[0])<<24|
			uint64(pareto[1])<<16|uint64(pareto[2])<<8|128, 6)
	case Category1Tok:
		bw.WritePacked64(0b1110000|uint32((extra&1)<<1)|signBit,
			uint64(zeroProb)<<48|uint64(pivot)<<40|
				uint64(pareto[0])<<32|uint64(pareto[3])<<24|
				uint64(pareto[4])<<16|uint64(tables.Cat1Prob[0])<<8|128, 7)
	case Category2Tok:
		bw.WritePacked64(0b11101000|uint32((extra&0x3)<<1)|signBit,
			uint64(zeroProb)<<56|uint64(pivot)<<48|
				uint64(pareto[0])<<40|uint64(pareto[3])<<32|
				uint64(pareto[4])<<24|uint64(tables.Cat2Prob[0])<<16|
				uint64(tables.Cat2Prob[1])<<8|128, 8)
	case Category3Tok:
		bw.WritePacked64(0b111100, uint64(zeroProb)<<40|uint64(pivot)<<32|
			uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[6]), 6)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	case Category4Tok:
		bw.WritePacked64(0b111101, uint64(zeroProb)<<40|uint64(pivot)<<32|
			uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[6]), 6)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	case Category5Tok:
		bw.WritePacked64(0b111110, uint64(zeroProb)<<40|uint64(pivot)<<32|
			uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[7]), 6)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	case Category6Tok:
		bw.WritePacked64(0b111111, uint64(zeroProb)<<40|uint64(pivot)<<32|
			uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[7]), 6)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	default:
		panic("encoder: invalid staged VP9 coefficient token")
	}
}

// writePackedCoefTokenBodyWithNotEOB fuses the run-head not-EOB decision
// (a 1 bit against notEOB, the EOB_CONTEXT_NODE probability) into the same
// packed fragment as the coefficient token body. VP9 codes the EOB node only
// at zero-run heads (skip_eob_node elsewhere), so this is the companion of
// writePackedCoefTokenBodyAfterNotZero for the run-head shape. Category2 needs
// nine bits with the fused head, which exceeds the eight-byte packed
// probability payload, so it keeps the split Write + body form.
func writePackedCoefTokenBodyWithNotEOB(
	bw *bitstream.Writer, token int, extra int, sign int,
	notEOB, zeroProb, pivot uint8,
) {
	signBit := uint32(sign & 1)
	switch token {
	case OneToken:
		bw.WritePacked(0b1100|signBit, uint32(notEOB)<<24|uint32(zeroProb)<<16|
			uint32(pivot)<<8|128, 4)
		return
	case Category2Tok:
		bw.Write(1, uint32(notEOB))
		writePackedCoefTokenBodyAfterNotZero(bw, token, extra, sign,
			zeroProb, pivot)
		return
	}
	if pivot == 0 {
		panic("encoder: invalid VP9 coefficient probability")
	}
	pareto := &tables.Pareto8Full[pivot-1]
	switch token {
	case TwoToken:
		bw.WritePacked64(0b111000|signBit, uint64(notEOB)<<40|
			uint64(zeroProb)<<32|uint64(pivot)<<24|uint64(pareto[0])<<16|
			uint64(pareto[1])<<8|128, 6)
	case ThreeToken:
		bw.WritePacked64(0b1110100|signBit, uint64(notEOB)<<48|
			uint64(zeroProb)<<40|uint64(pivot)<<32|uint64(pareto[0])<<24|
			uint64(pareto[1])<<16|uint64(pareto[2])<<8|128, 7)
	case FourToken:
		bw.WritePacked64(0b1110110|signBit, uint64(notEOB)<<48|
			uint64(zeroProb)<<40|uint64(pivot)<<32|uint64(pareto[0])<<24|
			uint64(pareto[1])<<16|uint64(pareto[2])<<8|128, 7)
	case Category1Tok:
		bw.WritePacked64(0b11110000|uint32((extra&1)<<1)|signBit,
			uint64(notEOB)<<56|uint64(zeroProb)<<48|uint64(pivot)<<40|
				uint64(pareto[0])<<32|uint64(pareto[3])<<24|
				uint64(pareto[4])<<16|uint64(tables.Cat1Prob[0])<<8|128, 8)
	case Category3Tok:
		bw.WritePacked64(0b1111100, uint64(notEOB)<<48|uint64(zeroProb)<<40|
			uint64(pivot)<<32|uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[6]), 7)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	case Category4Tok:
		bw.WritePacked64(0b1111101, uint64(notEOB)<<48|uint64(zeroProb)<<40|
			uint64(pivot)<<32|uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[6]), 7)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	case Category5Tok:
		bw.WritePacked64(0b1111110, uint64(notEOB)<<48|uint64(zeroProb)<<40|
			uint64(pivot)<<32|uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[7]), 7)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	case Category6Tok:
		bw.WritePacked64(0b1111111, uint64(notEOB)<<48|uint64(zeroProb)<<40|
			uint64(pivot)<<32|uint64(pareto[0])<<24|uint64(pareto[3])<<16|
			uint64(pareto[5])<<8|uint64(pareto[7]), 7)
		writeCoefExtraBits(bw, token, extra)
		bw.WriteBit(signBit)
	default:
		panic("encoder: invalid staged VP9 coefficient token")
	}
}

func writeCoefExtraBits(bw *bitstream.Writer, token, extra int) {
	switch token {
	case Category1Tok:
		bw.Write(uint32(extra&1), uint32(tables.Cat1Prob[0]))
	case Category2Tok:
		bw.WritePacked(uint32(extra&0x3),
			uint32(tables.Cat2Prob[0])<<8|uint32(tables.Cat2Prob[1]), 2)
	case Category3Tok:
		bw.WritePacked(uint32(extra&0x7),
			uint32(tables.Cat3Prob[0])<<16|uint32(tables.Cat3Prob[1])<<8|
				uint32(tables.Cat3Prob[2]), 3)
	case Category4Tok:
		bw.WritePacked(uint32(extra&0xf),
			uint32(tables.Cat4Prob[0])<<24|uint32(tables.Cat4Prob[1])<<16|
				uint32(tables.Cat4Prob[2])<<8|uint32(tables.Cat4Prob[3]), 4)
	case Category5Tok:
		bw.WritePacked(uint32(extra>>1)&0xf,
			uint32(tables.Cat5Prob[0])<<24|uint32(tables.Cat5Prob[1])<<16|
				uint32(tables.Cat5Prob[2])<<8|uint32(tables.Cat5Prob[3]), 4)
		bw.Write(uint32(extra&1), uint32(tables.Cat5Prob[4]))
	case Category6Tok:
		bw.WritePacked(uint32(extra>>10)&0xf,
			uint32(tables.Cat6Prob[0])<<24|uint32(tables.Cat6Prob[1])<<16|
				uint32(tables.Cat6Prob[2])<<8|uint32(tables.Cat6Prob[3]), 4)
		bw.WritePacked(uint32(extra>>6)&0xf,
			uint32(tables.Cat6Prob[4])<<24|uint32(tables.Cat6Prob[5])<<16|
				uint32(tables.Cat6Prob[6])<<8|uint32(tables.Cat6Prob[7]), 4)
		bw.WritePacked(uint32(extra>>2)&0xf,
			uint32(tables.Cat6Prob[8])<<24|uint32(tables.Cat6Prob[9])<<16|
				uint32(tables.Cat6Prob[10])<<8|uint32(tables.Cat6Prob[11]), 4)
		bw.WritePacked(uint32(extra&0x3),
			uint32(tables.Cat6Prob[12])<<8|uint32(tables.Cat6Prob[13]), 2)
	}
}

// writeTreeBits mirrors libvpx's vp9_write_tree — walks the tree
// against the given probability row, emitting one bit per level.
func writeTreeBits(bw *bitstream.Writer, tree []int8, probs []uint8, bits, length int) {
	writeTreeBitsWithCounts(bw, tree, probs, bits, length, nil)
}

func writeTreeBitsWithCounts(
	bw *bitstream.Writer, tree []int8, probs []uint8, bits, length int,
	branchStats *[EntropyNodes][2]uint32,
) {
	i := int8(0)
	for length > 0 {
		length--
		bit := (bits >> uint(length)) & 1
		recordCoefBranch(branchStats, UnconstrainedNodes+int(i>>1), bit)
		bw.Write(uint32(bit), uint32(probs[i>>1]))
		i = tree[int(i)+bit]
	}
}

func recordCoefBranch(stats *[EntropyNodes][2]uint32, node int, bit int) {
	if stats == nil {
		return
	}
	stats[node][bit]++
}

func recordCoefBranch00(stats *[EntropyNodes][2]uint32) {
	if stats != nil {
		stats[0][0]++
	}
}

func recordCoefBranch01(stats *[EntropyNodes][2]uint32) {
	if stats != nil {
		stats[0][1]++
	}
}

func recordCoefBranch10(stats *[EntropyNodes][2]uint32) {
	if stats != nil {
		stats[1][0]++
	}
}

func recordCoefBranch11(stats *[EntropyNodes][2]uint32) {
	if stats != nil {
		stats[1][1]++
	}
}

func recordCoefBranchPivot0(stats *[EntropyNodes][2]uint32) {
	if stats != nil {
		stats[PivotNode][0]++
	}
}

func recordCoefBranchPivot1(stats *[EntropyNodes][2]uint32) {
	if stats != nil {
		stats[PivotNode][1]++
	}
}
