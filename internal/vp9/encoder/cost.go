package encoder

// VP9 cost-of-bits tables and helpers. Ported from libvpx v1.16.0
// vp9/encoder/vp9_cost.{h,c}. The encoder uses these to compute the
// rate cost of encoding a stream against a given probability table,
// which it then compares against alternative probabilities to pick
// the per-frame slot updates that minimize total bits.

// VP9ProbCost mirrors libvpx's vp9_prob_cost[256] — round(-log2(i/256)
// * (1 << VP9ProbCostShift)). The first entry (i=0) is a sentinel
// (4096) so the table is addressable with prob=0 without overflow.
var VP9ProbCost = [256]uint16{
	4096, 4096, 3584, 3284, 3072, 2907, 2772, 2659, 2560, 2473, 2395, 2325, 2260,
	2201, 2147, 2096, 2048, 2003, 1961, 1921, 1883, 1847, 1813, 1780, 1748, 1718,
	1689, 1661, 1635, 1609, 1584, 1559, 1536, 1513, 1491, 1470, 1449, 1429, 1409,
	1390, 1371, 1353, 1335, 1318, 1301, 1284, 1268, 1252, 1236, 1221, 1206, 1192,
	1177, 1163, 1149, 1136, 1123, 1110, 1097, 1084, 1072, 1059, 1047, 1036, 1024,
	1013, 1001, 990, 979, 968, 958, 947, 937, 927, 917, 907, 897, 887,
	878, 868, 859, 850, 841, 832, 823, 814, 806, 797, 789, 780, 772,
	764, 756, 748, 740, 732, 724, 717, 709, 702, 694, 687, 680, 673,
	665, 658, 651, 644, 637, 631, 624, 617, 611, 604, 598, 591, 585,
	578, 572, 566, 560, 554, 547, 541, 535, 530, 524, 518, 512, 506,
	501, 495, 489, 484, 478, 473, 467, 462, 456, 451, 446, 441, 435,
	430, 425, 420, 415, 410, 405, 400, 395, 390, 385, 380, 375, 371,
	366, 361, 356, 352, 347, 343, 338, 333, 329, 324, 320, 316, 311,
	307, 302, 298, 294, 289, 285, 281, 277, 273, 268, 264, 260, 256,
	252, 248, 244, 240, 236, 232, 228, 224, 220, 216, 212, 209, 205,
	201, 197, 194, 190, 186, 182, 179, 175, 171, 168, 164, 161, 157,
	153, 150, 146, 143, 139, 136, 132, 129, 125, 122, 119, 115, 112,
	109, 105, 102, 99, 95, 92, 89, 86, 82, 79, 76, 73, 70,
	66, 63, 60, 57, 54, 51, 48, 45, 42, 38, 35, 32, 29,
	26, 23, 20, 18, 15, 12, 9, 6, 3,
}

// VP9CostZero mirrors libvpx's vp9_cost_zero macro: cost of writing
// a 0 bit against probability `p`.
func VP9CostZero(p uint8) int { return int(VP9ProbCost[p]) }

// VP9CostOne mirrors vp9_cost_one: cost of writing a 1 bit against
// probability `p` (== cost of a 0 bit at probability 256-p).
func VP9CostOne(p uint8) int { return int(VP9ProbCost[256-int(p)]) }

// VP9CostBit dispatches to VP9CostZero / VP9CostOne based on bit
// value. Mirrors vp9_cost_bit.
func VP9CostBit(p uint8, bit int) int {
	if bit != 0 {
		return VP9CostOne(p)
	}
	return VP9CostZero(p)
}

// CostBranch256 mirrors libvpx's cost_branch256 — the bit-cost of
// emitting `ct[0]` zero-bits and `ct[1]` one-bits against
// probability `p`. Returned in cost units (bits << VP9ProbCostShift).
func CostBranch256(ct [2]uint32, p uint8) uint64 {
	return uint64(ct[0])*uint64(VP9CostZero(p)) +
		uint64(ct[1])*uint64(VP9CostOne(p))
}

// ProbDiffUpdateSavingsSearch mirrors libvpx's
// vp9_prob_diff_update_savings_search — the binary-prob RD search
// that picks the best new probability for a slot given the observed
// count pair `ct` and the previous probability `oldp`.
//
// `bestp` is the seed candidate (typically computed from the counts
// via get_binary_prob). The search walks from `*bestp` toward
// `oldp`, scoring each candidate by `old_b - new_b - update_b` and
// keeping the maximum-savings pick. Returns the total savings in
// cost units; `*bestp` is set to the winning probability (oldp when
// no candidate beats the no-update baseline).
//
// `upd` is the update-decision prob (typically DIFF_UPDATE_PROB).
func ProbDiffUpdateSavingsSearch(ct [2]uint32, oldp uint8, bestp *uint8, upd uint8) int64 {
	oldB := int64(CostBranch256(ct, oldp))
	bestSavings := int64(0)
	bestNewP := oldp
	step := int(1)
	if int(*bestp) > int(oldp) {
		step = -1
	}
	updCost := VP9CostOne(upd) - VP9CostZero(upd)

	if oldB > int64(updCost+(MinDelpBits<<VP9ProbCostShift)) {
		for newp := int(*bestp); newp != int(oldp); newp += step {
			newB := int64(CostBranch256(ct, uint8(newp)))
			updateB := int64(ProbDiffUpdateCost(uint8(newp), oldp) + updCost)
			savings := oldB - newB - updateB
			if savings > bestSavings {
				bestSavings = savings
				bestNewP = uint8(newp)
			}
		}
	}
	*bestp = bestNewP
	return bestSavings
}

// TreedCost mirrors libvpx's treed_cost inline helper. Walks a
// token tree against the supplied probability row, accumulating
// the bit cost for the encoded leaf bit-pattern (`bits` consumed
// MSB-first, `length` bits total).
func TreedCost(tree []int8, probs []uint8, bits, length int) int {
	cost := 0
	i := int8(0)
	for length > 0 {
		length--
		bit := (bits >> uint(length)) & 1
		cost += VP9CostBit(probs[i>>1], bit)
		i = tree[int(i)+bit]
	}
	return cost
}

// VP9CostTokens mirrors libvpx's vp9_cost_tokens — populates the
// `costs` slice with the bit cost of every leaf in `tree` under the
// supplied probability row.
//
// The leaf labels in libvpx's token trees are encoded as negative
// values, so `costs[-leaf]` is the slot for a given leaf value. The
// caller sizes `costs` to one entry per possible leaf.
func VP9CostTokens(costs []int, probs []uint8, tree []int8) {
	costTokensRec(costs, tree, probs, 0, 0)
}

// VP9CostTokensSkip mirrors vp9_cost_tokens_skip — the variant
// libvpx uses when the first tree decision is fixed to "go right".
// The first leaf gets vp9_cost_bit(probs[0], 0) and the rest are
// computed starting from tree[2].
func VP9CostTokensSkip(costs []int, probs []uint8, tree []int8) {
	// libvpx asserts tree[0] <= 0 && tree[1] > 0.
	if tree[0] > 0 || tree[1] <= 0 {
		return
	}
	costs[-int(tree[0])] = VP9CostBit(probs[0], 0)
	costTokensRec(costs, tree, probs, 2, 0)
}

func costTokensRec(costs []int, tree []int8, probs []uint8, i, c int) {
	prob := probs[i>>1]
	for b := 0; b <= 1; b++ {
		cc := c + VP9CostBit(prob, b)
		next := tree[i+b]
		if next <= 0 {
			costs[-int(next)] = cc
			continue
		}
		costTokensRec(costs, tree, probs, int(next), cc)
	}
}

// GetBinaryProb mirrors libvpx's get_binary_prob — the canonical
// "what probability best fits these counts?" projection. The MV
// and prob update paths both hand this through to the savings
// search as the seed candidate.
func GetBinaryProb(n0, n1 uint32) uint8 {
	const probMax = 255
	total := n0 + n1
	if total == 0 {
		return 128
	}
	// libvpx: ((n0 << 8) + (total >> 1)) / total, then clamp to [1, 255].
	p := (int(n0)<<8 + int(total)>>1) / int(total)
	if p < 1 {
		return 1
	}
	if p > probMax {
		return probMax
	}
	return uint8(p)
}
