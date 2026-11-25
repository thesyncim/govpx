package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 compressed header foundations. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c — the boolean-coder-driven
// probability updates that follow the uncompressed header.

// TxSizeContexts is libvpx's TX_SIZE_CONTEXTS — 2 contexts per
// transform-size probability bucket.
const TxSizeContexts = 2

// TxProbs mirrors libvpx's struct tx_probs. The dimensions follow the
// fixed `TX_SIZES - {1, 2, 3}` triangular layout in vp9_entropymode.h.
type TxProbs struct {
	P32x32 [TxSizeContexts][common.TxSizes - 1]uint8
	P16x16 [TxSizeContexts][common.TxSizes - 2]uint8
	P8x8   [TxSizeContexts][common.TxSizes - 3]uint8
}

// ReadTxMode mirrors read_tx_mode. Two bits select Only4x4 / Allow8x8
// / Allow16x16 / Allow32x32; a third bit is read only when the first
// two say "Allow32x32", promoting to TxModeSelect.
func ReadTxMode(r *bitstream.Reader) common.TxMode {
	tm := common.TxMode(r.ReadLiteral(2))
	if tm == common.Allow32x32 {
		tm += common.TxMode(r.ReadBit())
	}
	return tm
}

// ReadTxModeProbs mirrors read_tx_mode_probs. Three nested loops over
// the (TxSizeContexts × {TxSizes-3, TxSizes-2, TxSizes-1}) probability
// slots; each slot is conditionally updated via vp9_diff_update_prob.
func ReadTxModeProbs(r *bitstream.Reader, tp *TxProbs) {
	for i := range TxSizeContexts {
		for j := range int(common.TxSizes - 3) {
			VpxDiffUpdateProb(r, &tp.P8x8[i][j])
		}
	}
	for i := range TxSizeContexts {
		for j := range int(common.TxSizes - 2) {
			VpxDiffUpdateProb(r, &tp.P16x16[i][j])
		}
	}
	for i := range TxSizeContexts {
		for j := range int(common.TxSizes - 1) {
			VpxDiffUpdateProb(r, &tp.P32x32[i][j])
		}
	}
}

// SkipContexts mirrors libvpx's SKIP_CONTEXTS (= 3).
const SkipContexts = common.SkipContexts

// ReadSkipProbs runs vp9_diff_update_prob across the 3 skip-context
// probability slots. Mirrors the corresponding fragment of
// read_compressed_header.
func ReadSkipProbs(r *bitstream.Reader, probs *[SkipContexts]uint8) {
	for k := range SkipContexts {
		VpxDiffUpdateProb(r, &probs[k])
	}
}

// Coefficient probability updates from the VP9 compressed header.
// Ported from libvpx v1.16.0 vp9/decoder/vp9_decodeframe.c
// (read_coef_probs / read_coef_probs_common) plus the table sizing
// constants in vp9/common/vp9_entropy.h.

const (
	// CoefPlaneTypes — VP9 keeps separate coefficient probs for luma
	// and chroma (libvpx's PLANE_TYPES = 2).
	CoefPlaneTypes = 2
	// CoefRefTypes — intra vs inter reference frame (REF_TYPES = 2).
	CoefRefTypes = 2
	// CoefBands — six bands per (plane, ref, tx_size) entry.
	CoefBands = 6
	// CoefContexts — full per-band context count outside band 0.
	CoefContexts = 6
	// CoefBand0Contexts — band 0 uses a reduced context space.
	CoefBand0Contexts = 3
	// UnconstrainedNodes — three of the four token-tree nodes are
	// unconstrained and carry their own probabilities; the fourth is
	// implied from the others.
	UnconstrainedNodes = 3
)

// BandCoefContexts mirrors libvpx's BAND_COEFF_CONTEXTS(band) macro —
// band 0 uses 3 contexts, every other band uses 6.
func BandCoefContexts(band int) int {
	if band == 0 {
		return CoefBand0Contexts
	}
	return CoefContexts
}

// CoefProbsModel matches libvpx's vp9_coeff_probs_model layout. The
// per-tx-size table is a 4-D array indexed as
//
//	[PLANE_TYPES][REF_TYPES][COEF_BANDS][COEF_CONTEXTS][UNCONSTRAINED_NODES]
//
// Band 0 only actually uses 3 of the COEF_CONTEXTS slots; the tail
// slots are unused but the storage is square so the surrounding code
// can index uniformly.
type CoefProbsModel [CoefPlaneTypes][CoefRefTypes][CoefBands][CoefContexts][UnconstrainedNodes]uint8

// FrameCoefProbs aggregates the per-tx-size coefficient probability
// tables the compressed header may update — one model per TxSize
// (4x4 / 8x8 / 16x16 / 32x32).
type FrameCoefProbs [common.TxSizes]CoefProbsModel

// readCoefProbsCommon mirrors read_coef_probs_common — a single
// leading bit gates the full nested loop over (planes × refs × bands ×
// per-band contexts × unconstrained nodes).
func readCoefProbsCommon(r *bitstream.Reader, model *CoefProbsModel) {
	if r.ReadBit() == 0 {
		return
	}
	for i := range CoefPlaneTypes {
		for j := range CoefRefTypes {
			for k := range CoefBands {
				for l := range BandCoefContexts(k) {
					for m := range UnconstrainedNodes {
						VpxDiffUpdateProb(r, &model[i][j][k][l][m])
					}
				}
			}
		}
	}
}

// ReadCoefProbs mirrors read_coef_probs. It walks every TxSize up to
// the cap that tx_mode_to_biggest_tx_size selects from the frame
// header's TxMode and invokes the per-model update routine on each.
func ReadCoefProbs(r *bitstream.Reader, fc *FrameCoefProbs, txMode common.TxMode) {
	max := common.TxModeToBiggestTxSize[txMode]
	for tx := common.TxSize(0); tx <= max; tx++ {
		readCoefProbsCommon(r, &fc[tx])
	}
}

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
	UvModeProb           [common.IntraModes][common.IntraModes - 1]uint8
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

// VP9 motion-vector probability tables and the compressed-header walk
// that updates them. Ported from libvpx v1.16.0:
//   - vp9/common/vp9_entropymv.h (nmv_component, nmv_context layout +
//     MV_JOINTS / MV_CLASSES / CLASS0_SIZE / MV_OFFSET_BITS / MV_FP_SIZE)
//   - vp9/decoder/vp9_decodeframe.c (read_mv_probs walking the
//     nmv_context via update_mv_probs)

const (
	MvJoints     = 4
	MvClasses    = 11
	Class0Bits   = 1
	Class0Size   = 1 << Class0Bits
	MvOffsetBits = MvClasses + Class0Bits - 2 // = 10
	MvFpSize     = 4
)

// NmvComponent mirrors libvpx's struct nmv_component — the per-axis
// probability layout (horizontal then vertical) inside an nmv_context.
type NmvComponent struct {
	Sign     uint8
	Classes  [MvClasses - 1]uint8
	Class0   [Class0Size - 1]uint8
	Bits     [MvOffsetBits]uint8
	Class0Fp [Class0Size][MvFpSize - 1]uint8
	Fp       [MvFpSize - 1]uint8
	Class0Hp uint8
	Hp       uint8
}

// NmvContext mirrors libvpx's nmv_context — the joints + 2 components
// the compressed header walks per inter frame.
type NmvContext struct {
	Joints [MvJoints - 1]uint8
	Comps  [2]NmvComponent
}

// ReadMvProbs mirrors read_mv_probs. The walk order — joints, then for
// each axis (sign / classes / class0 / bits), then for each axis
// (class0_fp[*] / fp), then (gated by allow_hp) class0_hp / hp — is
// wire-stable; reordering even one slot would break byte parity.
func ReadMvProbs(r *bitstream.Reader, ctx *NmvContext, allowHp bool) {
	UpdateMvProbs(r, ctx.Joints[:])

	for i := range 2 {
		cc := &ctx.Comps[i]
		updateMvProb(r, &cc.Sign)
		UpdateMvProbs(r, cc.Classes[:])
		UpdateMvProbs(r, cc.Class0[:])
		UpdateMvProbs(r, cc.Bits[:])
	}

	for i := range 2 {
		cc := &ctx.Comps[i]
		for j := range Class0Size {
			UpdateMvProbs(r, cc.Class0Fp[j][:])
		}
		UpdateMvProbs(r, cc.Fp[:])
	}

	if allowHp {
		for i := range 2 {
			cc := &ctx.Comps[i]
			updateMvProb(r, &cc.Class0Hp)
			updateMvProb(r, &cc.Hp)
		}
	}
}

func updateMvProb(r *bitstream.Reader, p *uint8) {
	if r.Read(MvUpdateProb) != 0 {
		*p = uint8((r.ReadLiteral(7) << 1) | 1)
	}
}
