package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 compressed-header writer (no-update path). Ported from libvpx
// v1.16.0 vp9/encoder/vp9_bitstream.c — write_compressed_header. The
// full encoder emits per-slot vp9_cond_prob_diff_update calls that
// run a cost analysis against the per-frame counters; for an MVP
// encoder we instead emit the "no update" bit (a 0 bit against
// DIFF_UPDATE_PROB) for every probability slot, signalling the
// decoder to preserve the seed/previous probabilities.
//
// Coverage: this writer is the smallest legal compressed header.
// Future commits add the cost-driven update path that matches what
// libvpx's encoder emits when run with non-zero per-frame counters.

const (
	// DiffUpdateProb mirrors libvpx's DIFF_UPDATE_PROB — the
	// probability the "update this slot?" bit is encoded against.
	DiffUpdateProb = 252
	// MvUpdateProbConst mirrors libvpx's MV_UPDATE_PROB. The decoder
	// already exposes this; we duplicate the constant here for the
	// writer's call site to stay self-contained.
	MvUpdateProbConst = 252
)

// CompressedHeaderInputs bundles the per-frame inputs the compressed
// header writer consults.
type CompressedHeaderInputs struct {
	// Lossless indicates the frame is in lossless mode; libvpx forces
	// TxMode to Only4x4 (no transform-size probs emitted) and skips
	// the encode_txfm_probs call.
	Lossless bool
	// TxMode is the frame-level transform-mode pick (Only4x4, Allow8x8,
	// Allow16x16, Allow32x32, or TxModeSelect).
	TxMode common.TxMode
	// IntraOnly toggles emission of the inter-only probability
	// blocks (inter_mode_probs, switchable_interp_probs,
	// intra_inter_probs, reference_mode + ref-mode probs,
	// y_mode_probs, partition_probs, mv_probs).
	IntraOnly bool
	// InterpFilter is the per-frame interp_filter pick. When equal
	// to InterpSwitchable, the writer emits the switchable interp
	// prob update block (also no-update in this writer).
	InterpFilter vp9dec.InterpFilter
	// ReferenceMode is the per-frame reference-mode pick. Mirrors
	// vp9_decoder.ReferenceMode (SingleReference / CompoundReference
	// / ReferenceModeSelect). The writer gates the comp_inter /
	// single_ref / comp_ref prob blocks the same way the parser does.
	ReferenceMode vp9dec.ReferenceMode
	// CompoundRefAllowed comes from the per-frame compound-reference
	// gate.
	CompoundRefAllowed bool
	// AllowHighPrecisionMv mirrors the inter-frame hp gate; threads
	// into the MV prob block.
	AllowHighPrecisionMv bool
}

// WriteCompressedHeaderNoUpdate emits the minimum-legal compressed
// header — every "update?" bit is 0 so the decoder preserves all
// probability tables across the frame boundary. Returns the byte
// length of the written payload.
//
// `dst` is sized by the caller; libvpx caps the compressed header
// at the uncompressed header's first_partition_size literal.
func WriteCompressedHeaderNoUpdate(dst []byte, in CompressedHeaderInputs) (int, error) {
	var bw bitstream.Writer
	bw.Start(dst)

	// 1) TxMode header. Only emitted when !lossless.
	if !in.Lossless {
		writeTxMode(&bw, in.TxMode)
		if in.TxMode == common.TxModeSelect {
			writeTxProbsNoUpdate(&bw)
		}
	}

	// 2) Coef probs — for every active TX size up to TxMode, walk
	// 2 (plane) × 2 (ref) × 6 (band) × 6 (ctx) entries emitting a
	// single "update?" bit each. Cap TxMode for the iteration is
	// determined by libvpx's `for (tx_size = TX_4X4; tx_size <=
	// tx_mode_max_txsize[tx_mode]; ++tx_size)`. The no-update path
	// emits a single 0 bit per slot.
	writeCoefProbsNoUpdate(&bw, in.Lossless, in.TxMode)

	// 3) Skip probs (3 slots).
	for range skipContexts {
		bw.Write(0, DiffUpdateProb)
	}

	frameIsIntraOnly := in.IntraOnly
	if !frameIsIntraOnly {
		// 4) Inter-mode probs (7 contexts × 3 nodes each).
		for range common.InterModeContexts {
			writeProbTreeNoUpdate(&bw, common.InterModes-1)
		}

		// 5) Switchable-interp probs (4 contexts × 2 nodes).
		if in.InterpFilter == vp9dec.InterpSwitchable {
			for range switchableFilterContexts {
				writeProbTreeNoUpdate(&bw, switchableFilters-1)
			}
		}

		// 6) Intra/inter probs (4 contexts).
		for range intraInterContexts {
			bw.Write(0, DiffUpdateProb)
		}

		// 7) ReferenceMode select + per-mode prob blocks.
		writeFrameReferenceMode(&bw, in.ReferenceMode, in.CompoundRefAllowed)
		writeFrameReferenceModeProbs(&bw, in.ReferenceMode)

		// 8) Y-mode probs (4 size groups × 9 nodes).
		for range blockSizeGroups {
			writeProbTreeNoUpdate(&bw, common.IntraModes-1)
		}

		// 9) Partition probs (16 contexts × 3 nodes).
		for range common.PartitionContexts {
			writeProbTreeNoUpdate(&bw, int(common.PartitionTypes)-1)
		}

		// 10) MV probs — joints + per-axis + (allow_hp) high-precision.
		writeMvProbsNoUpdate(&bw, in.AllowHighPrecisionMv)
	}

	return bw.Stop()
}

// writeTxMode mirrors write_tx_mode. Two-bit cap + an optional 1-bit
// extension when the selected mode is TxModeSelect.
func writeTxMode(bw *bitstream.Writer, m common.TxMode) {
	if m == common.TxModeSelect {
		bw.WriteLiteral(uint32(common.Allow32x32), 2)
		bw.WriteBit(1)
		return
	}
	bw.WriteLiteral(uint32(m), 2)
}

// writeTxProbsNoUpdate emits one 0 bit per slot of the 8x8, 16x16,
// and 32x32 TxProbs sub-tables, matching the encode_txfm_probs
// per-slot vp9_cond_prob_diff_update calls.
func writeTxProbsNoUpdate(bw *bitstream.Writer) {
	// p8x8: 2 ctx × 1 node, p16x16: 2 ctx × 2 nodes, p32x32: 2 ctx × 3 nodes.
	for range 2 * 1 {
		bw.Write(0, DiffUpdateProb)
	}
	for range 2 * 2 {
		bw.Write(0, DiffUpdateProb)
	}
	for range 2 * 3 {
		bw.Write(0, DiffUpdateProb)
	}
}

// writeCoefProbsNoUpdate walks every active TxSize and emits one
// 0-bit "update?" per probability slot. Mirrors the per-slot update
// emission in update_coef_probs_common's no-update path.
func writeCoefProbsNoUpdate(bw *bitstream.Writer, lossless bool, m common.TxMode) {
	var max common.TxSize
	switch {
	case lossless:
		max = common.Tx4x4
	case m == common.TxModeSelect:
		max = common.Tx32x32
	default:
		max = common.TxModeToBiggestTxSize[m]
	}
	for tx := common.Tx4x4; tx <= max; tx++ {
		// libvpx emits a single "update_probs" frame-level bit per
		// tx size (1 = full update, 0 = no update). We emit 0.
		bw.WriteBit(0)
	}
}

// writeProbTreeNoUpdate emits `n` 0-bit "update?" bits — one per
// node in a tree-shaped probability table.
func writeProbTreeNoUpdate(bw *bitstream.Writer, n int) {
	for range n {
		bw.Write(0, DiffUpdateProb)
	}
}

// writeFrameReferenceMode mirrors the reference_mode header bit
// shape: 1-bit "compound prediction allowed", then if allowed a
// 1-bit "select", then if not select a 1-bit "is compound".
func writeFrameReferenceMode(bw *bitstream.Writer, m vp9dec.ReferenceMode, compAllowed bool) {
	useCompound := compAllowed
	if useCompound {
		bw.WriteBit(1)
		if m == vp9dec.ReferenceModeSelect {
			bw.WriteBit(1)
			return
		}
		bw.WriteBit(0)
		if m == vp9dec.CompoundReference {
			bw.WriteBit(1)
		} else {
			bw.WriteBit(0)
		}
		return
	}
	bw.WriteBit(0)
}

// writeFrameReferenceModeProbs mirrors the per-mode prob update
// fragment. Each active sub-table (comp_inter / single_ref /
// comp_ref) emits one 0 bit per slot.
func writeFrameReferenceModeProbs(bw *bitstream.Writer, m vp9dec.ReferenceMode) {
	if m == vp9dec.ReferenceModeSelect {
		for range common.CompInterContexts {
			bw.Write(0, DiffUpdateProb)
		}
	}
	if m != vp9dec.CompoundReference {
		for range common.RefContexts {
			bw.Write(0, DiffUpdateProb)
			bw.Write(0, DiffUpdateProb)
		}
	}
	if m != vp9dec.SingleReference {
		for range common.RefContexts {
			bw.Write(0, DiffUpdateProb)
		}
	}
}

// writeMvProbsNoUpdate walks the NMV probability table in the same
// order ReadMvProbs reads it: joints / per-axis (sign, classes,
// class0, bits) / per-axis (class0_fp, fp), then if allow_hp also
// per-axis (class0_hp, hp).
func writeMvProbsNoUpdate(bw *bitstream.Writer, allowHp bool) {
	// joints: 3 slots.
	for range vp9dec.MvJoints - 1 {
		bw.Write(0, MvUpdateProbConst)
	}
	// Per axis: sign (1), classes (10), class0 (1), bits (10).
	for range 2 {
		bw.Write(0, MvUpdateProbConst)
		for range vp9dec.MvClasses - 1 {
			bw.Write(0, MvUpdateProbConst)
		}
		for range vp9dec.Class0Size - 1 {
			bw.Write(0, MvUpdateProbConst)
		}
		for range vp9dec.MvOffsetBits {
			bw.Write(0, MvUpdateProbConst)
		}
	}
	// Per axis: class0_fp (Class0Size × MvFpSize-1), fp (MvFpSize-1).
	for range 2 {
		for range vp9dec.Class0Size {
			for range vp9dec.MvFpSize - 1 {
				bw.Write(0, MvUpdateProbConst)
			}
		}
		for range vp9dec.MvFpSize - 1 {
			bw.Write(0, MvUpdateProbConst)
		}
	}
	if allowHp {
		for range 2 {
			bw.Write(0, MvUpdateProbConst)
			bw.Write(0, MvUpdateProbConst)
		}
	}
}

// Constants mirroring the decoder-side names — duplicated here so the
// writer file stays self-contained.
const (
	skipContexts             = 3
	intraInterContexts       = 4
	switchableFilterContexts = 4
	switchableFilters        = 3
	blockSizeGroups          = 4
)
