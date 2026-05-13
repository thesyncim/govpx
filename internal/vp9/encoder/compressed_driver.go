package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
		&args.Counts.CoefBranchStats, args.Lossless, args.TxMode,
		args.CoefStepsize)

	WriteSkipProbsFromCounts(&bw, &args.Probs.SkipProbs, &args.Counts.Skip)

	frameIsIntraOnly := args.IntraOnly
	if !frameIsIntraOnly {
		WriteInterModeProbsFromCounts(&bw, &args.Probs.InterModeProbs,
			&args.Counts.InterMode, scratchPair(common.InterModes-1))

		if args.InterpFilter == vp9dec.InterpSwitchable {
			WriteSwitchableInterpProbsFromCounts(&bw,
				&args.Probs.SwitchableInterpProb,
				&args.Counts.SwitchableInterp,
				scratchPair(vp9dec.SwitchableFilters-1))
		}

		WriteIntraInterProbsFromCounts(&bw, &args.Probs.IntraInterProb,
			&args.Counts.IntraInter)

		writeFrameReferenceMode(&bw, args.ReferenceMode, args.CompoundRefAllowed)
		WriteReferenceModeProbsFromCounts(&bw, &args.Probs.ReferenceModeProbs,
			&args.Counts.ReferenceMode, args.ReferenceMode,
			args.CompoundRefAllowed)

		WriteYModeProbsFromCounts(&bw, &args.Probs.YModeProb,
			&args.Counts.YMode, scratchPair(common.IntraModes-1))

		WritePartitionProbsFromCounts(&bw, &args.Probs.PartitionProb,
			&args.Counts.Partition, scratchPair(int(common.PartitionTypes)-1))

		WriteNmvProbsFromCounts(&bw, &args.Probs.Nmvc, &args.Counts.Mv,
			args.AllowHighPrecisionMv, scratchPair(32))
	}

	return bw.Stop()
}

// scratchPair allocates a [N][2]uint32 scratch slice for one of the
// tree-shaped writers. Caller chooses N from the tree's branch count.
// Today this allocates; subsequent commits add a pooled scratch
// owned by the encoder. For the current standalone tests of the
// driver, the per-call allocations don't matter.
func scratchPair(n int) [][2]uint32 {
	return make([][2]uint32, n)
}
