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
	return writeTokenForCoeff(bw, ctxTree, absCoeff, sign, nil)
}

func writeTokenForCoeff(
	bw *bitstream.Writer, ctxTree []uint8, absCoeff int, sign int,
	branchStats *[EntropyNodes][2]uint32,
) bool {
	if absCoeff == 0 {
		// Caller should have already written the zero bit.
		return false
	}
	token, extra := TokenForAbsCoeff(absCoeff)
	if token == OneToken {
		recordCoefBranch(branchStats, PivotNode, 0)
		bw.Write(0, uint32(ctxTree[2]))
		bw.WriteBit(uint32(sign))
		return true
	}
	// Non-ONE: emit the "go to pareto" bit at ctx[2].
	recordCoefBranch(branchStats, PivotNode, 1)
	bw.Write(1, uint32(ctxTree[2]))
	enc := CoefEncodings[token]
	pareto := tables.Pareto8Full[ctxTree[2]-1]
	// vp9_coef_encodings[].len carries the FULL token-tree depth
	// (including the 3 unconstrained nodes consumed before the
	// CoefConTree walk). pack_mb_tokens passes (n - UNCONSTRAINED_NODES)
	// so the tree walk consumes only the pareto-tail bits.
	walkLen := int(enc.Len) - UnconstrainedNodes
	writeTreeBitsWithCounts(bw, CoefConTree[:], pareto[:], int(enc.Value), walkLen,
		branchStats)
	if token >= Category1Tok {
		eb := VP9ExtraBits[token]
		for i := eb.Len - 1; i >= 0; i-- {
			bit := (extra >> uint(i)) & 1
			bw.Write(uint32(bit), uint32(eb.Prob[eb.Len-1-i]))
		}
	}
	bw.WriteBit(uint32(sign))
	return true
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
