package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 NMV probability counts-driven writer. Ported from libvpx
// v1.16.0 vp9/encoder/vp9_encodemv.c — update_mv,
// write_mv_update, vp9_write_nmv_probs.
//
// The MV update path uses a different wire shape than the rest of
// the compressed header: the decision is based on a direct cost
// comparison (oldp vs newp under the observed counts, plus the
// 1-bit update gate and the 7-bit literal cost), and the new prob
// is encoded as a 7-bit literal (stored back as (lit << 1) | 1 on
// the decoder side, forcing odd values in [1, 255]).
//
// This is intentionally NOT routed through CondProbDiffUpdateFromCounts
// or WriteProbDiffUpdate — those encode the sub-exponential delta
// the non-MV slots use. The MV path is its own wire-format dialect.

// UpdateMv mirrors libvpx's update_mv inline (vp9_encodemv.c).
// Computes newp from counts as (get_binary_prob(...) | 1) so the
// stored-back prob is always odd, then compares the cost of emitting
// the per-block bits against oldp + the no-update bit, versus newp +
// the 1-bit update gate + 7-bit literal. Emits the update fragment
// when it wins. Returns whether the update was emitted (matches
// libvpx's return value, useful for callers tracking total updates).
func UpdateMv(bw *bitstream.Writer, ct [2]uint32, curP *uint8) bool {
	newP := GetBinaryProb(ct[0], ct[1]) | 1
	oldCost := int64(CostBranch256(ct, *curP)) +
		int64(VP9CostZero(vp9dec.MvUpdateProb))
	newCost := int64(CostBranch256(ct, newP)) +
		int64(VP9CostOne(vp9dec.MvUpdateProb)) +
		int64(7<<VP9ProbCostShift)
	if oldCost > newCost {
		bw.Write(1, vp9dec.MvUpdateProb)
		bw.WriteLiteral(uint32(newP>>1), 7)
		*curP = newP
		return true
	}
	bw.Write(0, vp9dec.MvUpdateProb)
	return false
}

// writeMvUpdate mirrors libvpx's static write_mv_update — for a
// tree-shaped per-leaf counts row, derives the (N-1) branch (left,
// right) pairs via TreeProbsFromDistribution and runs UpdateMv per
// pair. `scratch` is a caller-owned scratch slice of at least len(probs).
func writeMvUpdate(bw *bitstream.Writer, tree []int8, probs []uint8,
	counts []uint32, scratch [][2]uint32,
) {
	TreeProbsFromDistribution(tree, scratch[:len(probs)], counts)
	for i := range probs {
		UpdateMv(bw, scratch[i], &probs[i])
	}
}

// NmvComponentCounts mirrors libvpx's nmv_component_counts — the per-
// axis MV count payload. Sign / classes / class0 / bits / class0_fp /
// fp / class0_hp / hp slabs each in the count-per-leaf shape.
type NmvComponentCounts struct {
	Sign     [2]uint32
	Classes  [vp9dec.MvClasses]uint32
	Class0   [vp9dec.Class0Size]uint32
	Bits     [vp9dec.MvOffsetBits][2]uint32
	Class0Fp [vp9dec.Class0Size][vp9dec.MvFpSize]uint32
	Fp       [vp9dec.MvFpSize]uint32
	Class0Hp [2]uint32
	Hp       [2]uint32
}

// NmvContextCounts mirrors libvpx's nmv_context_counts — the
// joints histogram + 2 per-axis component count slabs.
type NmvContextCounts struct {
	Joints [vp9dec.MvJoints]uint32
	Comps  [2]NmvComponentCounts
}

// WriteNmvProbsFromCounts mirrors libvpx's vp9_write_nmv_probs.
// Walks the joints tree, then per-axis: sign, classes tree, class0
// tree, bits slabs, class0_fp[CLASS0_SIZE] trees, fp tree, and
// (gated by useHp) class0_hp / hp slabs. The wire shape matches
// UpdateMvProbs on the decoder side exactly — every slot is either
// a single 0 bit or a 1 bit plus a 7-bit literal.
//
// `scratch` is caller-owned; the writer needs at most
// max(MvClasses-1, MvFpSize-1) = 10 entries.
func WriteNmvProbsFromCounts(bw *bitstream.Writer,
	probs *vp9dec.NmvContext, counts *NmvContextCounts,
	useHp bool, scratch [][2]uint32,
) {
	// Joints tree.
	writeMvUpdate(bw, tables.MvJointTree[:], probs.Joints[:],
		counts.Joints[:], scratch)

	for i := 0; i < 2; i++ {
		comp := &probs.Comps[i]
		ccnt := &counts.Comps[i]
		UpdateMv(bw, ccnt.Sign, &comp.Sign)
		writeMvUpdate(bw, tables.MvClassTree[:], comp.Classes[:],
			ccnt.Classes[:], scratch)
		writeMvUpdate(bw, tables.MvClass0Tree[:], comp.Class0[:],
			ccnt.Class0[:], scratch)
		for j := 0; j < vp9dec.MvOffsetBits; j++ {
			UpdateMv(bw, ccnt.Bits[j], &comp.Bits[j])
		}
	}

	for i := 0; i < 2; i++ {
		comp := &probs.Comps[i]
		ccnt := &counts.Comps[i]
		for j := 0; j < vp9dec.Class0Size; j++ {
			writeMvUpdate(bw, tables.MvFpTree[:], comp.Class0Fp[j][:],
				ccnt.Class0Fp[j][:], scratch)
		}
		writeMvUpdate(bw, tables.MvFpTree[:], comp.Fp[:],
			ccnt.Fp[:], scratch)
	}

	if useHp {
		for i := 0; i < 2; i++ {
			comp := &probs.Comps[i]
			ccnt := &counts.Comps[i]
			UpdateMv(bw, ccnt.Class0Hp, &comp.Class0Hp)
			UpdateMv(bw, ccnt.Hp, &comp.Hp)
		}
	}
}
