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
	probeWriteCompressedHeaderSection("after_txmode", bw.Pos())

	WriteCoefProbsFromCounts(&bw, &args.Probs.CoefProbs,
		&args.Counts.CoefBranchStats, &args.Counts.TxTotals,
		args.Lossless, args.TxMode, args.CoefStepsize,
		args.CoefUpdateMode, args.SkipTx16PlusCoefUpdates)
	probeWriteCompressedHeaderSection("after_coef", bw.Pos())

	WriteSkipProbsFromCounts(&bw, &args.Probs.SkipProbs, &args.Counts.Skip)
	probeWriteCompressedHeaderSection("after_skip", bw.Pos())

	frameIsIntraOnly := args.IntraOnly
	if !frameIsIntraOnly {
		var interModeScratch [common.InterModes - 1][2]uint32
		WriteInterModeProbsFromCounts(&bw, &args.Probs.InterModeProbs,
			&args.Counts.InterMode, interModeScratch[:])
		probeWriteCompressedHeaderSection("after_intermode", bw.Pos())

		if args.InterpFilter == vp9dec.InterpSwitchable {
			var interpScratch [vp9dec.SwitchableFilters - 1][2]uint32
			WriteSwitchableInterpProbsFromCounts(&bw,
				&args.Probs.SwitchableInterpProb,
				&args.Counts.SwitchableInterp,
				interpScratch[:])
		}
		probeWriteCompressedHeaderSection("after_interp", bw.Pos())

		WriteIntraInterProbsFromCounts(&bw, &args.Probs.IntraInterProb,
			&args.Counts.IntraInter)
		probeWriteCompressedHeaderSection("after_intrainter", bw.Pos())

		writeFrameReferenceMode(&bw, args.ReferenceMode, args.CompoundRefAllowed)
		WriteReferenceModeProbsFromCounts(&bw, &args.Probs.ReferenceModeProbs,
			&args.Counts.ReferenceMode, args.ReferenceMode,
			args.CompoundRefAllowed)
		probeWriteCompressedHeaderSection("after_refmode", bw.Pos())

		var yModeScratch [common.IntraModes - 1][2]uint32
		WriteYModeProbsFromCounts(&bw, &args.Probs.YModeProb,
			&args.Counts.YMode, yModeScratch[:])
		probeWriteCompressedHeaderSection("after_ymode", bw.Pos())

		var partitionScratch [common.PartitionTypes - 1][2]uint32
		WritePartitionProbsFromCounts(&bw, &args.Probs.PartitionProb,
			&args.Counts.Partition, partitionScratch[:])
		probeWriteCompressedHeaderSection("after_partition", bw.Pos())

		var mvScratch [vp9dec.MvClasses - 1][2]uint32
		WriteNmvProbsFromCounts(&bw, &args.Probs.Nmvc, &args.Counts.Mv,
			args.AllowHighPrecisionMv, mvScratch[:])
		probeWriteCompressedHeaderSection("after_nmv", bw.Pos())
	}

	return bw.Stop()
}

// CompressedHeaderProbe optionally observes per-section byte offsets of
// the compressed-header writer. Tests set this to attribute bitstream
// growth to a specific section. Production code leaves it nil and the
// probe is a no-op.
var CompressedHeaderProbe func(section string, pos int)

func probeWriteCompressedHeaderSection(section string, pos int) {
	if CompressedHeaderProbe != nil {
		CompressedHeaderProbe(section, pos)
	}
}
