package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// FrameContext carries the full set of per-frame probability tables
// the VP9 compressed header may update. Mirrors the parser-visible
// subset of libvpx's FRAME_CONTEXT — the tables that
// read_compressed_header actually walks. Tables not touched by the
// compressed header (token tree positions, mv tree positions, etc.)
// stay in internal/vp9/tables / internal/vp9/common.
type FrameContext struct {
	TxProbs              TxProbs
	CoefProbs            FrameCoefProbs
	SkipProbs            [SkipContexts]uint8
	InterModeProbs       [common.InterModeContexts][common.InterModes - 1]uint8
	SwitchableInterpProb [SwitchableFilterContexts][SwitchableFilters - 1]uint8
	IntraInterProb       [common.IntraInterContexts]uint8
	ReferenceModeProbs   FrameReferenceModeProbs
	YModeProb            [BlockSizeGroups][common.IntraModes - 1]uint8
	PartitionProb        [common.PartitionContexts][common.PartitionTypes - 1]uint8
	Nmvc                 NmvContext
}

// CompressedHeader carries the outputs of ReadCompressedHeader the
// decoder needs to drive the tile pass. Most of the table updates
// land in *FrameContext directly; this struct carries the few
// frame-scalar outputs (TxMode, ReferenceMode).
type CompressedHeader struct {
	TxMode        common.TxMode
	ReferenceMode ReferenceMode
}

// ReadCompressedHeaderArgs bundles the frame-state inputs to the
// driver: the uncompressed-header fields that gate the compressed
// header's optional branches, plus a flag indicating whether the
// compound-reference path is allowed for this frame.
type ReadCompressedHeaderArgs struct {
	Lossless             bool
	IntraOnly            bool
	KeyFrame             bool
	InterpFilter         InterpFilter
	AllowHighPrecisionMv bool
	CompoundRefAllowed   bool
}

// ReadCompressedHeader drives the VP9 compressed-header parse end to
// end, mirroring read_compressed_header in libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c. The frame_context pointer holds the
// caller-provided probability state — typically the entry in
// cm->frame_contexts[frame_context_idx] for this frame — and is
// updated in place. The bitstream.Reader must already be Init'd
// against the first_partition_size bytes of the compressed payload.
func ReadCompressedHeader(r *bitstream.Reader, fc *FrameContext,
	args ReadCompressedHeaderArgs,
) CompressedHeader {
	var out CompressedHeader

	if args.Lossless {
		out.TxMode = common.Only4x4
	} else {
		out.TxMode = ReadTxMode(r)
		if out.TxMode == common.TxModeSelect {
			ReadTxModeProbs(r, &fc.TxProbs)
		}
	}
	ReadCoefProbs(r, &fc.CoefProbs, out.TxMode)
	ReadSkipProbs(r, &fc.SkipProbs)

	frameIsIntraOnly := args.KeyFrame || args.IntraOnly
	if !frameIsIntraOnly {
		ReadInterModeProbs(r, &fc.InterModeProbs)
		if args.InterpFilter == InterpSwitchable {
			ReadSwitchableInterpProbs(r, &fc.SwitchableInterpProb)
		}
		ReadIntraInterProbs(r, &fc.IntraInterProb)
		out.ReferenceMode = ReadFrameReferenceMode(r, args.CompoundRefAllowed)
		ReadFrameReferenceModeProbs(r, out.ReferenceMode, &fc.ReferenceModeProbs)
		ReadYModeProbs(r, &fc.YModeProb)
		ReadPartitionProbs(r, &fc.PartitionProb)
		ReadMvProbs(r, &fc.Nmvc, args.AllowHighPrecisionMv)
	}

	return out
}
