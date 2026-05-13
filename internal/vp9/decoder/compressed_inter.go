package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 compressed-header probability updates for the inter-frame
// branch. Ported from libvpx v1.16.0 vp9/decoder/vp9_decodeframe.c:
// read_switchable_interp_probs, read_inter_mode_probs,
// read_frame_reference_mode, read_frame_reference_mode_probs,
// update_mv_probs.

// Compressed-header sizing constants from libvpx.
const (
	SwitchableFilters        = 3
	SwitchableFilterContexts = SwitchableFilters + 1
	BlockSizeGroups          = 4
	MvUpdateProb             = 252
)

// ReferenceMode mirrors libvpx's REFERENCE_MODE enum.
type ReferenceMode uint8

const (
	SingleReference     ReferenceMode = 0
	CompoundReference   ReferenceMode = 1
	ReferenceModeSelect ReferenceMode = 2
)

// ReadSwitchableInterpProbs mirrors read_switchable_interp_probs. The
// caller passes the (contexts × filters-1) probability table.
func ReadSwitchableInterpProbs(r *bitstream.Reader,
	probs *[SwitchableFilterContexts][SwitchableFilters - 1]uint8,
) {
	for j := range SwitchableFilterContexts {
		for i := range SwitchableFilters - 1 {
			VpxDiffUpdateProb(r, &probs[j][i])
		}
	}
}

// ReadInterModeProbs mirrors read_inter_mode_probs. Walks the
// (INTER_MODE_CONTEXTS × INTER_MODES-1) probability table.
func ReadInterModeProbs(r *bitstream.Reader,
	probs *[common.InterModeContexts][common.InterModes - 1]uint8,
) {
	for i := range common.InterModeContexts {
		for j := range common.InterModes - 1 {
			VpxDiffUpdateProb(r, &probs[i][j])
		}
	}
}

// ReadFrameReferenceMode mirrors read_frame_reference_mode. When the
// compound-reference path is allowed the wire form is a 1-or-2-bit
// prefix selecting SingleReference / CompoundReference /
// ReferenceModeSelect; otherwise the value is SingleReference.
func ReadFrameReferenceMode(r *bitstream.Reader, compoundAllowed bool) ReferenceMode {
	if !compoundAllowed {
		return SingleReference
	}
	if r.ReadBit() == 0 {
		return SingleReference
	}
	if r.ReadBit() != 0 {
		return ReferenceModeSelect
	}
	return CompoundReference
}

// FrameReferenceModeProbs carries the three probability tables the
// reference-mode update fragment touches. Each is conditionally
// updated based on the active ReferenceMode.
type FrameReferenceModeProbs struct {
	CompInterProb [common.CompInterContexts]uint8
	SingleRefProb [common.RefContexts][2]uint8
	CompRefProb   [common.RefContexts]uint8
}

// ReadFrameReferenceModeProbs mirrors read_frame_reference_mode_probs.
// The active reference mode gates which subtables receive updates.
func ReadFrameReferenceModeProbs(r *bitstream.Reader, mode ReferenceMode,
	probs *FrameReferenceModeProbs,
) {
	if mode == ReferenceModeSelect {
		for i := range common.CompInterContexts {
			VpxDiffUpdateProb(r, &probs.CompInterProb[i])
		}
	}
	if mode != CompoundReference {
		for i := range common.RefContexts {
			VpxDiffUpdateProb(r, &probs.SingleRefProb[i][0])
			VpxDiffUpdateProb(r, &probs.SingleRefProb[i][1])
		}
	}
	if mode != SingleReference {
		for i := range common.RefContexts {
			VpxDiffUpdateProb(r, &probs.CompRefProb[i])
		}
	}
}

// UpdateMvProbs mirrors update_mv_probs: for each slot, read a single
// "update?" bit against MV_UPDATE_PROB; on update read 7 bits and
// store as (literal << 1) | 1 so the resulting probability is always
// odd in [1, 255].
func UpdateMvProbs(r *bitstream.Reader, p []uint8) {
	for i := range p {
		if r.Read(MvUpdateProb) != 0 {
			p[i] = uint8((r.ReadLiteral(7) << 1) | 1)
		}
	}
}

// ReadIntraInterProbs runs vp9_diff_update_prob across the
// INTRA_INTER_CONTEXTS probability slots. Mirrors the intra/inter
// probability update fragment of read_compressed_header.
func ReadIntraInterProbs(r *bitstream.Reader, probs *[common.IntraInterContexts]uint8) {
	for i := range common.IntraInterContexts {
		VpxDiffUpdateProb(r, &probs[i])
	}
}

// ReadYModeProbs runs vp9_diff_update_prob across the
// BLOCK_SIZE_GROUPS × (INTRA_MODES - 1) intra-mode probability table.
// Mirrors the y_mode_prob update fragment of read_compressed_header.
func ReadYModeProbs(r *bitstream.Reader, probs *[BlockSizeGroups][common.IntraModes - 1]uint8) {
	for j := range BlockSizeGroups {
		for i := range common.IntraModes - 1 {
			VpxDiffUpdateProb(r, &probs[j][i])
		}
	}
}

// ReadPartitionProbs runs vp9_diff_update_prob across the
// PARTITION_CONTEXTS × (PARTITION_TYPES - 1) partition probability
// table. Mirrors the partition_prob update fragment.
func ReadPartitionProbs(r *bitstream.Reader, probs *[common.PartitionContexts][common.PartitionTypes - 1]uint8) {
	for j := range common.PartitionContexts {
		for i := range common.PartitionTypes - 1 {
			VpxDiffUpdateProb(r, &probs[j][i])
		}
	}
}
