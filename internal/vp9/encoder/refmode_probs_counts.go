package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 reference-mode probability counts-driven updates. Ported from
// libvpx v1.16.0 vp9/encoder/vp9_bitstream.c — the comp_inter /
// single_ref / comp_ref vp9_cond_prob_diff_update calls inside
// write_uncompressed_header's inter-frame branch.
//
// Gating mirrors libvpx exactly:
//   - comp_inter updates only when reference_mode == REFERENCE_MODE_SELECT
//   - single_ref updates only when reference_mode != COMPOUND_REFERENCE
//   - comp_ref updates only when reference_mode != SINGLE_REFERENCE
//
// The frame-level reference_mode bits + the compound-pred flag bits
// already land via writeFrameReferenceMode in compressed_writer.go.

// ReferenceModeCounts mirrors libvpx's FRAME_COUNTS.comp_inter /
// single_ref / comp_ref slabs.
type ReferenceModeCounts struct {
	// CompInter is (intra/inter) → counts of (not-compound, compound)
	// per CompInterContexts context.
	CompInter [common.CompInterContexts][2]uint32
	// SingleRef is (counts of bit0=0, bit0=1) and (bit1=0, bit1=1) per
	// RefContexts context.
	SingleRef [common.RefContexts][2][2]uint32
	// CompRef is counts of (var_ref==CompVarRef[0], var_ref==CompVarRef[1])
	// per RefContexts context.
	CompRef [common.RefContexts][2]uint32
}

// CollapseReferenceModeFromCounts mirrors libvpx's post-encode
// reference-mode demotion. If REFERENCE_MODE_SELECT observed only single-ref
// or only compound-ref blocks, the frame-level mode is reduced and comp_inter
// counts are cleared so the compressed header omits impossible updates.
func CollapseReferenceModeFromCounts(mode vp9dec.ReferenceMode,
	counts *ReferenceModeCounts,
) vp9dec.ReferenceMode {
	if mode != vp9dec.ReferenceModeSelect || counts == nil {
		return mode
	}
	var singleCount, compoundCount uint32
	for i := range counts.CompInter {
		singleCount += counts.CompInter[i][0]
		compoundCount += counts.CompInter[i][1]
	}
	switch {
	case compoundCount == 0:
		counts.CompInter = [common.CompInterContexts][2]uint32{}
		return vp9dec.SingleReference
	case singleCount == 0:
		counts.CompInter = [common.CompInterContexts][2]uint32{}
		return vp9dec.CompoundReference
	default:
		return mode
	}
}

// WriteReferenceModeProbsFromCounts mirrors the comp_inter /
// single_ref / comp_ref update block of write_uncompressed_header.
// Caller has already emitted the per-frame reference_mode bits.
func WriteReferenceModeProbsFromCounts(bw *bitstream.Writer,
	probs *vp9dec.FrameReferenceModeProbs,
	counts *ReferenceModeCounts,
	mode vp9dec.ReferenceMode, compoundAllowed bool,
) {
	if compoundAllowed && mode == vp9dec.ReferenceModeSelect {
		for i := range common.CompInterContexts {
			CondProbDiffUpdateFromCounts(bw, &probs.CompInterProb[i],
				counts.CompInter[i])
		}
	}
	if mode != vp9dec.CompoundReference {
		for i := range common.RefContexts {
			CondProbDiffUpdateFromCounts(bw, &probs.SingleRefProb[i][0],
				counts.SingleRef[i][0])
			CondProbDiffUpdateFromCounts(bw, &probs.SingleRefProb[i][1],
				counts.SingleRef[i][1])
		}
	}
	if mode != vp9dec.SingleReference {
		for i := range common.RefContexts {
			CondProbDiffUpdateFromCounts(bw, &probs.CompRefProb[i],
				counts.CompRef[i])
		}
	}
}
