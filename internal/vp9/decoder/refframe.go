package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// VP9 reference-frame + segmentation-feature decoders. Ported from
// libvpx v1.16.0 vp9/decoder/vp9_decodemv.c — the small composition
// layer that turns the predictor-context helpers in pred_common.go
// and the FrameContext probability tables into per-block decoded
// signals.

// SegFeatureActive mirrors libvpx's segfeature_active in
// vp9/common/vp9_seg_common.h. Returns true iff segmentation is
// enabled AND the given feature is masked on for that segment.
func SegFeatureActive(seg *SegmentationParams, segID int, feature int) bool {
	if !seg.Enabled {
		return false
	}
	return seg.FeatureMask[segID]&(1<<uint(feature)) != 0
}

// GetSegData mirrors get_segdata in vp9_seg_common.h — the per-segment
// feature value (qindex delta, lf delta, ref frame, etc.).
func GetSegData(seg *SegmentationParams, segID int, feature int) int16 {
	return seg.FeatureData[segID][feature]
}

// ReadSkipWithSeg mirrors read_skip. If SEG_LVL_SKIP is active for
// this segment the per-block bit isn't sent; libvpx returns 1
// unconditionally in that case.
func ReadSkipWithSeg(r *bitstream.Reader, seg *SegmentationParams, segID int,
	fc *FrameContext, above, left *NeighborMi,
) int {
	if SegFeatureActive(seg, segID, SegLvlSkip) {
		return 1
	}
	ctx := GetSkipContext(above, left)
	return int(r.Read(uint32(fc.SkipProbs[ctx])))
}

// ReadIntraInterFlag mirrors libvpx's per-block intra/inter bit. The
// context is GetIntraInterContext(above,left); the probability slot
// is fc.IntraInterProb[ctx].
func ReadIntraInterFlag(r *bitstream.Reader, fc *FrameContext,
	above, left *NeighborMi,
) int {
	ctx := GetIntraInterContext(above, left)
	return int(r.Read(uint32(fc.IntraInterProb[ctx])))
}

// ReadBlockReferenceMode mirrors read_block_reference_mode. When the
// frame's ReferenceMode is ReferenceModeSelect each block picks
// between SingleReference and CompoundReference; otherwise the
// frame-level choice is returned verbatim.
func ReadBlockReferenceMode(r *bitstream.Reader, frameMode ReferenceMode,
	fc *FrameContext, refs CompoundFrameRefs, above, left *NeighborMi,
) ReferenceMode {
	if frameMode != ReferenceModeSelect {
		return frameMode
	}
	ctx := GetReferenceModeContext(above, left, refs)
	if r.Read(uint32(fc.ReferenceModeProbs.CompInterProb[ctx])) != 0 {
		return CompoundReference
	}
	return SingleReference
}

// ReadRefFrames mirrors libvpx's read_ref_frames. Decodes the
// reference-frame pair for one inter block. The output is written
// into `out` as a (rf0, rf1) pair where rf1 == NoRefFrame for
// single-reference predictions.
//
// Inputs:
//   - frameMode    : per-frame REFERENCE_MODE (Single, Compound, or Select).
//   - signBias     : per-frame ref_frame_sign_bias array.
//   - refs         : compound-reference triple (CompFixedRef + CompVarRef[2]).
//   - seg          : segmentation parameters (may carry SEG_LVL_REF_FRAME).
//   - segID        : current block's segment id.
//   - fc           : entropy context (CompRef / SingleRef prob slots).
//   - above, left  : neighbor MIs for predictor contexts.
//   - out          : 2-element ref_frame array written in place.
func ReadRefFrames(r *bitstream.Reader,
	frameMode ReferenceMode, signBias [MaxRefFrames]uint8,
	refs CompoundFrameRefs,
	seg *SegmentationParams, segID int,
	fc *FrameContext, above, left *NeighborMi,
	out *[2]int8,
) {
	// Segment-feature ref-frame override takes precedence.
	if SegFeatureActive(seg, segID, SegLvlRefFrame) {
		out[0] = int8(GetSegData(seg, segID, SegLvlRefFrame))
		out[1] = NoRefFrame
		return
	}

	mode := ReadBlockReferenceMode(r, frameMode, fc, refs, above, left)
	switch mode {
	case CompoundReference:
		idx := int(signBias[refs.CompFixedRef])
		ctx := GetPredContextCompRefP(above, left, refs, signBias)
		bit := r.Read(uint32(fc.ReferenceModeProbs.CompRefProb[ctx]))
		out[idx] = refs.CompFixedRef
		out[1-idx] = refs.CompVarRef[bit]
	case SingleReference:
		ctx0 := GetPredContextSingleRefP1(above, left)
		bit0 := r.Read(uint32(fc.ReferenceModeProbs.SingleRefProb[ctx0][0]))
		if bit0 != 0 {
			ctx1 := GetPredContextSingleRefP2(above, left)
			bit1 := r.Read(uint32(fc.ReferenceModeProbs.SingleRefProb[ctx1][1]))
			if bit1 != 0 {
				out[0] = AltrefFrame
			} else {
				out[0] = GoldenFrame
			}
		} else {
			out[0] = LastFrame
		}
		out[1] = NoRefFrame
	}
}

// DecGetSegmentId mirrors libvpx's dec_get_segment_id — the
// per-block-row minimum over a [yMis × xMis] window of the input
// segment-id map. Returns MaxSegments if the window is empty; libvpx
// asserts < MaxSegments in caller scope.
func DecGetSegmentId(segmentIDs []uint8, miCols, miOffset, xMis, yMis int) int {
	segID := MaxSegments
	for y := range yMis {
		for x := range xMis {
			v := int(segmentIDs[miOffset+y*miCols+x])
			if v < segID {
				segID = v
			}
		}
	}
	return segID
}

// ReadSegmentIDProb mirrors read_segment_id. Same wire format as the
// existing ReadSegmentId, but reads directly out of the seg params
// rather than asking the caller to extract TreeProbs first. Kept as
// a thin wrapper so call sites mirror the C source.
func ReadSegmentIDProb(r *bitstream.Reader, seg *SegmentationParams) int {
	return r.ReadTree(common.SegmentTree[:], seg.TreeProbs[:])
}
