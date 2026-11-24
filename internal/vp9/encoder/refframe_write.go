package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 per-block ref-frame / skip / is-inter writers. Ported from
// libvpx v1.16.0 vp9/encoder/vp9_bitstream.c — write_ref_frames,
// write_skip (inline at pack_inter_mode_mvs), and the inter-block
// flag emitted via the matching write_intra_inter_prob path.
// These compose with the mode + MV writers into a complete per-block
// inter-frame wire fragment.

// WriteSkipArgs bundles the inputs WriteSkip consults: the active
// segmentation params (the SEG_LVL_SKIP feature short-circuits the
// per-block bit), the segment id of the current block, the
// per-frame skip-prob table, and the neighbor MIs for context.
type WriteSkipArgs struct {
	Seg         *vp9dec.SegmentationParams
	SegID       int
	SkipProbs   [3]uint8 // matches FrameContext.SkipProbs
	Above, Left *vp9dec.NeighborMi
}

// WriteSkip mirrors libvpx's per-block skip emit. When
// SEG_LVL_SKIP is active for the segment, libvpx asserts skip==1
// and emits nothing — the decoder also short-circuits. Otherwise
// the skip bit is written against fc.SkipProbs[GetSkipContext()].
func WriteSkip(bw *bitstream.Writer, args WriteSkipArgs, skip int) {
	if vp9dec.SegFeatureActive(args.Seg, args.SegID, vp9dec.SegLvlSkip) {
		// segment forces skip; libvpx asserts skip==1.
		return
	}
	ctx := vp9dec.GetSkipContext(args.Above, args.Left)
	bw.Write(uint32(skip), uint32(args.SkipProbs[ctx]))
}

// WriteIsInterBlock mirrors libvpx's per-block intra/inter flag
// emit. SEG_LVL_REF_FRAME forces the value from the segment data;
// otherwise the bit is written against fc.IntraInterProb[ctx]
// where ctx = GetIntraInterContext(above, left).
func WriteIsInterBlock(bw *bitstream.Writer, seg *vp9dec.SegmentationParams, segID int,
	intraInterProb [4]uint8, above, left *vp9dec.NeighborMi, isInter int,
) {
	if vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		// segment override; no bit emitted.
		return
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	bw.Write(uint32(isInter), uint32(intraInterProb[ctx]))
}

// WriteRefFramesArgs bundles the inputs WriteRefFrames consults.
// Mirrors the libvpx VP9_COMMON / xd field set the C function
// reads from.
type WriteRefFramesArgs struct {
	Seg              *vp9dec.SegmentationParams
	SegID            int
	FrameMode        vp9dec.ReferenceMode
	CompFixedRef     int8
	CompVarRef       [2]int8
	RefFrameSignBias [4]uint8
	CompInterProb    [5]uint8 // FrameContext.ReferenceModeProbs.CompInterProb
	CompRefProb      [5]uint8
	SingleRefProb    [5][2]uint8
	Above, Left      *vp9dec.NeighborMi
	IsCompound       bool
	RefFrame         [2]int8 // (rf0, rf1)
}

// WriteRefFrames mirrors libvpx's write_ref_frames per-block emit.
// When SEG_LVL_REF_FRAME is active for the segment the writer
// emits nothing (decoder pulls the ref-frame value from segment
// data). Otherwise:
//   - ReferenceModeSelect: one bit picks Single vs Compound.
//   - Compound: one bit picks the var ref (CompVarRef[0/1]).
//   - Single: one or two bits pick LAST / GOLDEN / ALTREF.
func WriteRefFrames(bw *bitstream.Writer, args WriteRefFramesArgs) {
	if vp9dec.SegFeatureActive(args.Seg, args.SegID, vp9dec.SegLvlRefFrame) {
		// Segment forces ref_frame; libvpx asserts !is_compound and
		// ref_frame[0] == seg data.
		return
	}
	refs := vp9dec.CompoundFrameRefs{
		CompFixedRef: args.CompFixedRef,
		CompVarRef:   args.CompVarRef,
	}

	if args.FrameMode == vp9dec.ReferenceModeSelect {
		ctx := vp9dec.GetReferenceModeContext(args.Above, args.Left, refs)
		v := uint32(0)
		if args.IsCompound {
			v = 1
		}
		bw.Write(v, uint32(args.CompInterProb[ctx]))
	}

	if args.IsCompound {
		idx := int(args.RefFrameSignBias[args.CompFixedRef])
		ctx := vp9dec.GetPredContextCompRefP(args.Above, args.Left, refs, args.RefFrameSignBias)
		// Emit the bit selecting var ref: 0 → CompVarRef[0], 1 → CompVarRef[1].
		v := uint32(0)
		if args.RefFrame[1-idx] == args.CompVarRef[1] {
			v = 1
		}
		bw.Write(v, uint32(args.CompRefProb[ctx]))
		return
	}

	// Single ref: bit0 = "not LAST_FRAME"; if bit0, bit1 = "not GOLDEN".
	ctx0 := vp9dec.GetPredContextSingleRefP1(args.Above, args.Left)
	bit0 := uint32(0)
	if args.RefFrame[0] != vp9dec.LastFrame {
		bit0 = 1
	}
	bw.Write(bit0, uint32(args.SingleRefProb[ctx0][0]))
	if bit0 != 0 {
		ctx1 := vp9dec.GetPredContextSingleRefP2(args.Above, args.Left)
		bit1 := uint32(0)
		if args.RefFrame[0] != vp9dec.GoldenFrame {
			bit1 = 1
		}
		bw.Write(bit1, uint32(args.SingleRefProb[ctx1][1]))
	}
}
