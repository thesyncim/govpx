package encoder

import "github.com/thesyncim/govpx/internal/vp9/bitstream"

// VP9 probability sub-exponential update writer. Ported from libvpx
// v1.16.0 vp9/encoder/vp9_subexp.c — vp9_write_prob_diff_update and
// the supporting remap_prob / encode_term_subexp helpers.
//
// The compressed header walks each probability slot and decides
// whether to emit a fresh value or preserve the existing one. When
// updating, the new probability is remapped through a 254-entry
// permutation table and then encoded with a 3-category prefix code
// + uniform tail. This mirrors the decoder-side DecodeTermSubexp +
// InvRemapProb chain in internal/vp9/decoder/dsubexp.go.

// maxProbConst mirrors libvpx's MAX_PROB.
const maxProbConst = 255

// VP9ProbCostShift mirrors libvpx's VP9_PROB_COST_SHIFT — the
// fixed-point shift applied to update-bit costs before they're
// compared against the bit-cost estimate of the coefficient stream.
const VP9ProbCostShift = 9

// MinDelpBits mirrors libvpx's MIN_DELP_BITS — the minimum sub-exp
// encoding cost (in raw bits) for any non-zero delta. The cost
// search uses this as a floor when deciding whether an update is
// worth the bits.
const MinDelpBits = 5

// updateBits mirrors libvpx's static update_bits[255] — the
// bit-length of the sub-exp encoding for each remapped delta. The
// final entry (delp==254) is 0 because that remap index never
// occurs for legal (oldp, newp) pairs.
var updateBits = [maxProbConst]uint8{
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11,
	11, 11, 11, 11, 11, 11, 11, 0,
}

// ProbDiffUpdateCost mirrors libvpx's static prob_diff_update_cost.
// Returns the cost (in left-shifted "prob-cost" units) of encoding
// the (newp, oldp) sub-exp delta. The compressed-header walker
// compares this against the cost of leaving the slot unchanged
// before deciding which path to emit.
func ProbDiffUpdateCost(newp, oldp uint8) int {
	delp := remapProb(int(newp), int(oldp))
	return int(updateBits[delp]) << VP9ProbCostShift
}

// mapTable mirrors libvpx's static remap permutation. Mapping
// `(delta_index → encoded_value)` flattens the delta-encoded
// probabilities so the prefix code below can pick a tighter slot for
// the most-frequent values.
var mapTable = [maxProbConst - 1]uint8{
	20, 21, 22, 23, 24, 25, 0, 26, 27, 28, 29, 30, 31, 32, 33,
	34, 35, 36, 37, 1, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47,
	48, 49, 2, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61,
	3, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 4, 74,
	75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85, 5, 86, 87, 88,
	89, 90, 91, 92, 93, 94, 95, 96, 97, 6, 98, 99, 100, 101, 102,
	103, 104, 105, 106, 107, 108, 109, 7, 110, 111, 112, 113, 114, 115, 116,
	117, 118, 119, 120, 121, 8, 122, 123, 124, 125, 126, 127, 128, 129, 130,
	131, 132, 133, 9, 134, 135, 136, 137, 138, 139, 140, 141, 142, 143, 144,
	145, 10, 146, 147, 148, 149, 150, 151, 152, 153, 154, 155, 156, 157, 11,
	158, 159, 160, 161, 162, 163, 164, 165, 166, 167, 168, 169, 12, 170, 171,
	172, 173, 174, 175, 176, 177, 178, 179, 180, 181, 13, 182, 183, 184, 185,
	186, 187, 188, 189, 190, 191, 192, 193, 14, 194, 195, 196, 197, 198, 199,
	200, 201, 202, 203, 204, 205, 15, 206, 207, 208, 209, 210, 211, 212, 213,
	214, 215, 216, 217, 16, 218, 219, 220, 221, 222, 223, 224, 225, 226, 227,
	228, 229, 17, 230, 231, 232, 233, 234, 235, 236, 237, 238, 239, 240, 241,
	18, 242, 243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 19,
}

// WriteProbDiffUpdate mirrors vp9_write_prob_diff_update — emit the
// sub-exp-coded delta between `oldp` and `newp`. Caller is
// responsible for emitting the "update?" bit before invoking this.
func WriteProbDiffUpdate(bw *bitstream.Writer, newp, oldp uint8) {
	delp := remapProb(int(newp), int(oldp))
	encodeTermSubexp(bw, delp)
}

// CondProbDiffUpdate mirrors vp9_cond_prob_diff_update's wire shape:
// emits the "update?" bit (against DIFF_UPDATE_PROB) and, when set,
// the sub-exp delta. Caller decides whether to update — the encoder
// pipeline runs the cost analysis upstream; this helper just emits
// the wire fragment for the chosen path.
func CondProbDiffUpdate(bw *bitstream.Writer, oldp, newp uint8) {
	if newp == oldp {
		bw.Write(0, DiffUpdateProb)
		return
	}
	bw.Write(1, DiffUpdateProb)
	WriteProbDiffUpdate(bw, newp, oldp)
}

func recenterNonneg(v, m int) int {
	switch {
	case v > (m << 1):
		return v
	case v >= m:
		return (v - m) << 1
	default:
		return ((m - v) << 1) - 1
	}
}

// remapProb mirrors libvpx's static remap_prob — projects the
// (newp, oldp) pair onto the 254-entry mapTable through
// recenter_nonneg.
func remapProb(v, m int) int {
	v--
	m--
	var i int
	if (m << 1) <= maxProbConst {
		i = recenterNonneg(v, m) - 1
	} else {
		i = recenterNonneg(maxProbConst-1-v, maxProbConst-1-m) - 1
	}
	return int(mapTable[i])
}

// encodeTermSubexp mirrors libvpx's encode_term_subexp — three
// magnitude buckets (0..15, 16..31, 32..63) plus a uniform tail for
// values >= 64.
func encodeTermSubexp(bw *bitstream.Writer, word int) {
	switch {
	case word < 16:
		bw.WriteBit(0)
		bw.WriteLiteral(uint32(word), 4)
	case word < 32:
		bw.WriteBit(1)
		bw.WriteBit(0)
		bw.WriteLiteral(uint32(word-16), 4)
	case word < 64:
		bw.WriteBit(1)
		bw.WriteBit(1)
		bw.WriteBit(0)
		bw.WriteLiteral(uint32(word-32), 5)
	default:
		bw.WriteBit(1)
		bw.WriteBit(1)
		bw.WriteBit(1)
		encodeUniform(bw, word-64)
	}
}

// encodeUniform mirrors libvpx's encode_uniform — an 8-bit uniform
// coder with a 191-entry sub-table for the high half.
func encodeUniform(bw *bitstream.Writer, v int) {
	const l = 8
	const m = (1 << l) - 191 // 65
	if v < m {
		bw.WriteLiteral(uint32(v), l-1)
		return
	}
	bw.WriteLiteral(uint32(m+((v-m)>>1)), l-1)
	bw.WriteLiteral(uint32((v-m)&1), 1)
}
