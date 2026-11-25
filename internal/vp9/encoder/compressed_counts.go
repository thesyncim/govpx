package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 counts-driven compressed-header prob updates. Ported from
// libvpx v1.16.0 vp9/encoder/vp9_bitstream.c — the per-section
// update walkers (update_skip_probs, update_inter_mode_probs,
// update_switchable_interp_probs, write_partition_probs, etc.) that
// feed frame_counts.* through prob_diff_update / vp9_cond_prob_diff_update.
//
// Each helper here mirrors the matching libvpx walker exactly: same
// iteration order, same prob slots, same counts shape. The existing
// WriteCompressedHeaderNoUpdate writer falls back to all-zero "update?" bits
// per slot; these helpers replace that fallback when the caller has real
// per-frame counters.

// WriteSkipProbsFromCounts mirrors libvpx's update_skip_probs.
// Walks the 3 skip contexts and runs the savings-search cond-update
// for each against the (skip=0, skip=1) counter pair.
func WriteSkipProbsFromCounts(bw *bitstream.Writer,
	probs *[3]uint8, counts *[3][2]uint32,
) {
	for i := range counts {
		CondProbDiffUpdateFromCounts(bw, &probs[i], counts[i])
	}
}

// WriteIntraInterProbsFromCounts mirrors libvpx's
// update_intra_inter_probs callsite. Walks the 4 intra/inter
// contexts and runs the cond-update against the (intra, inter)
// counter pair.
func WriteIntraInterProbsFromCounts(bw *bitstream.Writer,
	probs *[common.IntraInterContexts]uint8,
	counts *[common.IntraInterContexts][2]uint32,
) {
	for i := range counts {
		CondProbDiffUpdateFromCounts(bw, &probs[i], counts[i])
	}
}

// WriteInterModeProbsFromCounts mirrors libvpx's
// update_inter_mode_probs. Walks the 7 inter-mode contexts and
// runs ProbDiffUpdateForTree against InterModeTree for each
// context's per-mode event counter. The 3 per-context branch
// slots scratch lives in `scratch[:]`.
func WriteInterModeProbsFromCounts(bw *bitstream.Writer,
	probs *[common.InterModeContexts][common.InterModes - 1]uint8,
	counts *[common.InterModeContexts][common.InterModes]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.InterModeTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// WriteSwitchableInterpProbsFromCounts mirrors libvpx's
// update_switchable_interp_probs. One ProbDiffUpdateForTree per
// switchable-filter context, walking SwitchableInterpTree.
func WriteSwitchableInterpProbsFromCounts(bw *bitstream.Writer,
	probs *[vp9dec.SwitchableFilterContexts][vp9dec.SwitchableFilters - 1]uint8,
	counts *[vp9dec.SwitchableFilterContexts][vp9dec.SwitchableFilters]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.SwitchableInterpTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// WritePartitionProbsFromCounts mirrors libvpx's
// write_partition_probs. One ProbDiffUpdateForTree per partition
// context, walking PartitionTree.
func WritePartitionProbsFromCounts(bw *bitstream.Writer,
	probs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	counts *[common.PartitionContexts][common.PartitionTypes]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.PartitionTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// WriteYModeProbsFromCounts mirrors libvpx's update_y_mode_probs
// callsite — one ProbDiffUpdateForTree per BlockSizeGroups row,
// walking IntraModeTree (the 9-node Y-mode tree).
func WriteYModeProbsFromCounts(bw *bitstream.Writer,
	probs *[vp9dec.BlockSizeGroups][common.IntraModes - 1]uint8,
	counts *[vp9dec.BlockSizeGroups][common.IntraModes]uint32,
	scratch [][2]uint32,
) {
	for i := range probs {
		ProbDiffUpdateForTree(bw, common.IntraModeTree[:],
			probs[i][:], counts[i][:], scratch)
	}
}

// VP9 compressed-header counts-driven driver. Ported from libvpx
// v1.16.0 vp9/encoder/vp9_bitstream.c — write_compressed_header.
// Composes the per-section update writers landed in earlier commits
// (WriteTxModeProbsFromCounts, WriteCoefProbsFromCounts,
// WriteSkipProbsFromCounts, WriteInterModeProbsFromCounts,
// WriteSwitchableInterpProbsFromCounts, WriteIntraInterProbsFromCounts,
// WriteReferenceModeProbsFromCounts, WriteYModeProbsFromCounts,
// WritePartitionProbsFromCounts, WriteNmvProbsFromCounts) into the
// single compressed-header byte stream the decoder's
// ReadCompressedHeader reads back.
//
// Section order mirrors libvpx's write_compressed_header line by
// line; the existing no-update driver in compressed_writer.go
// stays as the floor when callers don't have counters and want
// the minimum legal compressed header.

// FrameCounts mirrors libvpx's FRAME_COUNTS — the per-frame
// statistical counters every counts-driven update consults.
// Field shapes match the matching FrameContext probabilities so
// callers can fill the slab directly from their tokenize / encode
// loop without re-shuffling.
type FrameCounts struct {
	// CoefBranchStats is the per-tx-size coefficient branch counts —
	// shape [PlaneTypes][RefTypes][CoefBands][CoefContexts][EntropyNodes][2].
	CoefBranchStats FrameCoefBranchStats

	// TxTotals is libvpx counts->tx.tx_totals. It gates coefficient
	// probability updates for tx sizes with too little frame evidence.
	TxTotals [common.TxSizes]uint32

	// TxMode is the per-context tx-size selection histogram for the
	// 8x8 / 16x16 / 32x32 max-tx sub-tables.
	TxMode TxModeCounts

	// Skip is the per-context (skip=0, skip=1) counter pair.
	Skip [3][2]uint32

	// InterMode is the per-inter-mode-context (NearestMv, NearMv,
	// ZeroMv, NewMv) histogram. Shape mirrors fc.InterModeProbs.
	InterMode [common.InterModeContexts][common.InterModes]uint32

	// SwitchableInterp is the per-switchable-filter-context histogram
	// over the 3 switchable filters.
	SwitchableInterp [vp9dec.SwitchableFilterContexts][vp9dec.SwitchableFilters]uint32

	// IntraInter is the per-intra-inter-context (intra, inter) counter pair.
	IntraInter [common.IntraInterContexts][2]uint32

	// ReferenceMode threads into the comp_inter / single_ref / comp_ref
	// sub-tables.
	ReferenceMode ReferenceModeCounts

	// YMode is the per-size-group histogram over 10 intra modes.
	YMode [vp9dec.BlockSizeGroups][common.IntraModes]uint32

	// Partition is the per-partition-context histogram over 4 partition
	// types.
	Partition [common.PartitionContexts][common.PartitionTypes]uint32

	// Mv is the full NMV context counts (joints + per-axis component
	// counts + HP slabs).
	Mv NmvContextCounts
}

// WriteCompressedHeaderFromCountsArgs bundles the per-frame inputs
// the counts-driven compressed-header writer consults.
type WriteCompressedHeaderFromCountsArgs struct {
	Lossless             bool
	TxMode               common.TxMode
	IntraOnly            bool
	InterpFilter         vp9dec.InterpFilter
	ReferenceMode        vp9dec.ReferenceMode
	CompoundRefAllowed   bool
	AllowHighPrecisionMv bool

	// CoefStepsize selects the coeff_prob_appx_step speed-feature
	// (typical values 1..4). Bigger = coarser search.
	CoefStepsize int

	// CoefUpdateMode selects libvpx's coefficient-probability update
	// emitter. Realtime non-key frames use ONE_LOOP_REDUCED.
	CoefUpdateMode CoefUpdateMode

	// SkipTx16PlusCoefUpdates mirrors the USE_TX_8X8 speed feature:
	// TX_16X16 and larger coefficient updates are explicitly gated off.
	SkipTx16PlusCoefUpdates bool

	Probs  *vp9dec.FrameContext
	Counts *FrameCounts
}

// WriteCompressedHeaderFromCounts mirrors libvpx's
// write_compressed_header end to end. Walks the per-section update
// helpers in the same order libvpx emits, mutating
// args.Probs in place when the savings_search picks updates. The
// returned byte count goes into the uncompressed header's
// FirstPartitionSize literal.
//
// The wire shape matches vp9dec.ReadCompressedHeader exactly — both
// sides walk the same sections in the same order with the same
// gating predicates. Caller-supplied dst is sized to hold the
// resulting payload (first_partition_size is a 16-bit literal, so
// <= 65535 bytes).
func WriteCompressedHeaderFromCounts(dst []byte,
	args WriteCompressedHeaderFromCountsArgs,
) (int, error) {
	var bw bitstream.Writer
	bw.Start(dst)

	if !args.Lossless {
		writeTxMode(&bw, args.TxMode)
		if args.TxMode == common.TxModeSelect {
			WriteTxModeProbsFromCounts(&bw, &args.Probs.TxProbs, &args.Counts.TxMode)
		}
	}

	WriteCoefProbsFromCounts(&bw, &args.Probs.CoefProbs,
		&args.Counts.CoefBranchStats, &args.Counts.TxTotals,
		args.Lossless, args.TxMode, args.CoefStepsize,
		args.CoefUpdateMode, args.SkipTx16PlusCoefUpdates)

	WriteSkipProbsFromCounts(&bw, &args.Probs.SkipProbs, &args.Counts.Skip)

	frameIsIntraOnly := args.IntraOnly
	if !frameIsIntraOnly {
		var interModeScratch [common.InterModes - 1][2]uint32
		WriteInterModeProbsFromCounts(&bw, &args.Probs.InterModeProbs,
			&args.Counts.InterMode, interModeScratch[:])

		if args.InterpFilter == vp9dec.InterpSwitchable {
			var interpScratch [vp9dec.SwitchableFilters - 1][2]uint32
			WriteSwitchableInterpProbsFromCounts(&bw,
				&args.Probs.SwitchableInterpProb,
				&args.Counts.SwitchableInterp,
				interpScratch[:])
		}

		WriteIntraInterProbsFromCounts(&bw, &args.Probs.IntraInterProb,
			&args.Counts.IntraInter)

		writeFrameReferenceMode(&bw, args.ReferenceMode, args.CompoundRefAllowed)
		WriteReferenceModeProbsFromCounts(&bw, &args.Probs.ReferenceModeProbs,
			&args.Counts.ReferenceMode, args.ReferenceMode,
			args.CompoundRefAllowed)

		var yModeScratch [common.IntraModes - 1][2]uint32
		WriteYModeProbsFromCounts(&bw, &args.Probs.YModeProb,
			&args.Counts.YMode, yModeScratch[:])

		var partitionScratch [common.PartitionTypes - 1][2]uint32
		WritePartitionProbsFromCounts(&bw, &args.Probs.PartitionProb,
			&args.Counts.Partition, partitionScratch[:])

		var mvScratch [vp9dec.MvClasses - 1][2]uint32
		WriteNmvProbsFromCounts(&bw, &args.Probs.Nmvc, &args.Counts.Mv,
			args.AllowHighPrecisionMv, mvScratch[:])
	}

	return bw.Stop()
}
