package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// VP9 neighbor-context helpers. Ported from libvpx v1.16.0
// vp9/common/vp9_pred_common.{h,c}. These small functions sample the
// already-decoded above / left blocks to pick a context index into the
// frame's probability tables for every per-block boolean-coded signal:
// intra/inter, skip, switchable interp filter, tx-size, segment id,
// and the reference-frame selectors.
//
// NeighborMi is a lightweight view of the fields these helpers touch
// inside libvpx's struct MODE_INFO. The full struct carries the
// motion-vector cache, BMI quartet, and prediction-state flags; here we
// expose only what the context-context helpers read.
//
// A nil NeighborMi means the neighbor is outside the picture or not
// yet decoded — libvpx's "one-element border" of zero-initialized
// MODE_INFO. Helpers branch on (above != nil, left != nil) accordingly.

// VP9 reference-frame codes, mirroring vp9_blockd.h. NoRefFrame is the
// sentinel libvpx writes into ref_frame[1] for single-ref blocks.
const (
	NoRefFrame   = -1
	IntraFrame   = 0
	LastFrame    = 1
	GoldenFrame  = 2
	AltrefFrame  = 3
	MaxRefFrames = 4
)

// NeighborMi mirrors the subset of libvpx's MODE_INFO that the
// neighbor-context helpers read. Field types match libvpx widths so a
// future full ModeInfo type can embed or alias this view.
//
// Bmi[i].AsMode mirrors libvpx's b_mode_info.as_mode quartet —
// populated for sub-8x8 intra partitions (BLOCK_4X4 / 4X8 / 8X4) and
// referenced by [[GetYMode]] / [[LeftBlockMode]] / [[AboveBlockMode]].
type NeighborMi struct {
	SbType         common.BlockSize
	TxSize         common.TxSize
	InterpFilter   uint8
	Skip           uint8
	SegIDPredicted uint8
	Mode           common.PredictionMode
	RefFrame       [2]int8 // ref_frame[0..1] — NoRefFrame when unused
	Bmi            [4]Bmi
}

// Bmi mirrors libvpx's b_mode_info — the per-4x4-subblock side of a
// MODE_INFO. Carries the per-subblock intra mode (as_mode) and the
// per-ref motion vector pair (as_mv) for sub-8x8 inter partitions.
type Bmi struct {
	AsMode common.PredictionMode
	AsMv   [2]MV
}

// isInterBlock mirrors libvpx's is_inter_block(MODE_INFO*).
func isInterBlock(mi *NeighborMi) bool { return mi != nil && mi.RefFrame[0] > IntraFrame }

// hasSecondRef mirrors libvpx's has_second_ref(MODE_INFO*).
func hasSecondRef(mi *NeighborMi) bool { return mi != nil && mi.RefFrame[1] > IntraFrame }

// GetPredContextSegId mirrors vp9_get_pred_context_seg_id — adds the
// "segment id was predicted" flag from above and left.
func GetPredContextSegId(above, left *NeighborMi) int {
	a := 0
	if above != nil {
		a = int(above.SegIDPredicted)
	}
	l := 0
	if left != nil {
		l = int(left.SegIDPredicted)
	}
	return a + l
}

// GetSkipContext mirrors vp9_get_skip_context — sums the skip flags
// of above and left.
func GetSkipContext(above, left *NeighborMi) int {
	a := 0
	if above != nil {
		a = int(above.Skip)
	}
	l := 0
	if left != nil {
		l = int(left.Skip)
	}
	return a + l
}

// GetPredContextSwitchableInterp mirrors
// get_pred_context_switchable_interp from vp9_pred_common.h. The
// returned context indexes cm->fc->switchable_interp_prob.
func GetPredContextSwitchableInterp(above, left *NeighborMi) int {
	leftType := int(SwitchableFilters)
	if left != nil {
		leftType = int(left.InterpFilter)
	}
	aboveType := int(SwitchableFilters)
	if above != nil {
		aboveType = int(above.InterpFilter)
	}
	switch {
	case leftType == aboveType:
		return leftType
	case leftType == SwitchableFilters:
		return aboveType
	case aboveType == SwitchableFilters:
		return leftType
	default:
		return SwitchableFilters
	}
}

// GetIntraInterContext mirrors get_intra_inter_context. The 4-state
// context encodes the intra/inter mix of the above/left pair:
//
//	0 - inter/inter, inter/--, --/inter, --/--
//	1 - intra/inter, inter/intra
//	2 - intra/--, --/intra
//	3 - intra/intra
func GetIntraInterContext(above, left *NeighborMi) int {
	hasAbove := above != nil
	hasLeft := left != nil
	switch {
	case hasAbove && hasLeft:
		aboveIntra := !isInterBlock(above)
		leftIntra := !isInterBlock(left)
		if leftIntra && aboveIntra {
			return 3
		}
		if leftIntra || aboveIntra {
			return 1
		}
		return 0
	case hasAbove || hasLeft:
		edge := above
		if !hasAbove {
			edge = left
		}
		if !isInterBlock(edge) {
			return 2
		}
		return 0
	default:
		return 0
	}
}

// GetTxSizeContext mirrors get_tx_size_context. Returns 0 or 1
// depending on whether the average tx-size of above+left exceeds
// max_tx_size. Caller supplies max_tx_size for the current block
// (libvpx computes it from sb_type via max_txsize_lookup).
func GetTxSizeContext(above, left *NeighborMi, maxTxSize common.TxSize) int {
	hasAbove := above != nil
	hasLeft := left != nil
	aboveCtx := int(maxTxSize)
	if hasAbove && above.Skip == 0 {
		aboveCtx = int(above.TxSize)
	}
	leftCtx := int(maxTxSize)
	if hasLeft && left.Skip == 0 {
		leftCtx = int(left.TxSize)
	}
	if !hasLeft {
		leftCtx = aboveCtx
	}
	if !hasAbove {
		aboveCtx = leftCtx
	}
	if (aboveCtx + leftCtx) > int(maxTxSize) {
		return 1
	}
	return 0
}

// CompoundFrameRefs carries the two compound-reference assignments
// libvpx maintains on VP9_COMMON. CompFixedRef is the "fixed" half,
// CompVarRef the two alternatives the variable half ranges over.
type CompoundFrameRefs struct {
	CompFixedRef int8
	CompVarRef   [2]int8
}

// CompoundReferenceAllowed mirrors vp9_compound_reference_allowed.
// Returns true iff the three forward/backward ref frames have at
// least one direction-flip in their sign-bias table.
func CompoundReferenceAllowed(signBias [MaxRefFrames]uint8) bool {
	for i := 1; i < 3; i++ {
		if signBias[i+1] != signBias[1] {
			return true
		}
	}
	return false
}

// SetupCompoundReferenceMode mirrors vp9_setup_compound_reference_mode.
// Picks which of the three ref frames is the "fixed" anchor for compound
// prediction and which two are the variable alternatives. Driven by the
// per-frame ref_frame_sign_bias array libvpx wires through VP9_COMMON.
func SetupCompoundReferenceMode(signBias [MaxRefFrames]uint8) CompoundFrameRefs {
	switch {
	case signBias[LastFrame] == signBias[GoldenFrame]:
		return CompoundFrameRefs{
			CompFixedRef: AltrefFrame,
			CompVarRef:   [2]int8{LastFrame, GoldenFrame},
		}
	case signBias[LastFrame] == signBias[AltrefFrame]:
		return CompoundFrameRefs{
			CompFixedRef: GoldenFrame,
			CompVarRef:   [2]int8{LastFrame, AltrefFrame},
		}
	default:
		return CompoundFrameRefs{
			CompFixedRef: LastFrame,
			CompVarRef:   [2]int8{GoldenFrame, AltrefFrame},
		}
	}
}

// b2i returns 1 if cond is true else 0. Mirrors libvpx's C-style
// implicit bool→int coercion in the predictor-context helpers.
func b2i(cond bool) int {
	if cond {
		return 1
	}
	return 0
}

// GetPredContextCompRefP mirrors vp9_get_pred_context_comp_ref_p — the
// 5-state compound-ref-half-pick context used when the frame is in
// REFERENCE_MODE_SELECT and the block already chose COMPOUND_REFERENCE.
func GetPredContextCompRefP(above, left *NeighborMi, refs CompoundFrameRefs, signBias [MaxRefFrames]uint8) int {
	fixRefIdx := int(signBias[refs.CompFixedRef])
	varRefIdx := 1 - fixRefIdx
	hasAbove := above != nil
	hasLeft := left != nil

	switch {
	case hasAbove && hasLeft:
		aboveIntra := !isInterBlock(above)
		leftIntra := !isInterBlock(left)
		switch {
		case aboveIntra && leftIntra:
			return 2
		case aboveIntra || leftIntra:
			edge := above
			if aboveIntra {
				edge = left
			}
			if !hasSecondRef(edge) {
				return 1 + 2*b2i(edge.RefFrame[0] != refs.CompVarRef[1])
			}
			return 1 + 2*b2i(edge.RefFrame[varRefIdx] != refs.CompVarRef[1])
		default:
			lSg := !hasSecondRef(left)
			aSg := !hasSecondRef(above)
			vrfa := above.RefFrame[varRefIdx]
			if aSg {
				vrfa = above.RefFrame[0]
			}
			vrfl := left.RefFrame[varRefIdx]
			if lSg {
				vrfl = left.RefFrame[0]
			}
			switch {
			case vrfa == vrfl && refs.CompVarRef[1] == vrfa:
				return 0
			case lSg && aSg:
				if (vrfa == refs.CompFixedRef && vrfl == refs.CompVarRef[0]) ||
					(vrfl == refs.CompFixedRef && vrfa == refs.CompVarRef[0]) {
					return 4
				}
				if vrfa == vrfl {
					return 3
				}
				return 1
			case lSg || aSg:
				vrfc := vrfa
				if !lSg {
					vrfc = vrfl
				}
				rfs := vrfa
				if !aSg {
					rfs = vrfl
				}
				if vrfc == refs.CompVarRef[1] && rfs != refs.CompVarRef[1] {
					return 1
				}
				if rfs == refs.CompVarRef[1] && vrfc != refs.CompVarRef[1] {
					return 2
				}
				return 4
			case vrfa == vrfl:
				return 4
			default:
				return 2
			}
		}
	case hasAbove || hasLeft:
		edge := above
		if !hasAbove {
			edge = left
		}
		if !isInterBlock(edge) {
			return 2
		}
		if hasSecondRef(edge) {
			return 4 * b2i(edge.RefFrame[varRefIdx] != refs.CompVarRef[1])
		}
		return 3 * b2i(edge.RefFrame[0] != refs.CompVarRef[1])
	default:
		return 2
	}
}

// GetPredContextSingleRefP1 mirrors vp9_get_pred_context_single_ref_p1
// — the 5-state context for the first of two single-ref bits.
func GetPredContextSingleRefP1(above, left *NeighborMi) int {
	hasAbove := above != nil
	hasLeft := left != nil
	switch {
	case hasAbove && hasLeft:
		aboveIntra := !isInterBlock(above)
		leftIntra := !isInterBlock(left)
		switch {
		case aboveIntra && leftIntra:
			return 2
		case aboveIntra || leftIntra:
			edge := above
			if aboveIntra {
				edge = left
			}
			if !hasSecondRef(edge) {
				return 4 * b2i(edge.RefFrame[0] == LastFrame)
			}
			return 1 + b2i(edge.RefFrame[0] == LastFrame || edge.RefFrame[1] == LastFrame)
		default:
			aHasSecond := hasSecondRef(above)
			lHasSecond := hasSecondRef(left)
			a0, a1 := above.RefFrame[0], above.RefFrame[1]
			l0, l1 := left.RefFrame[0], left.RefFrame[1]
			switch {
			case aHasSecond && lHasSecond:
				return 1 + b2i(a0 == LastFrame || a1 == LastFrame ||
					l0 == LastFrame || l1 == LastFrame)
			case aHasSecond || lHasSecond:
				rfs := a0
				if aHasSecond {
					rfs = l0
				}
				crf1 := a0
				crf2 := a1
				if !aHasSecond {
					crf1 = l0
					crf2 = l1
				}
				if rfs == LastFrame {
					return 3 + b2i(crf1 == LastFrame || crf2 == LastFrame)
				}
				return b2i(crf1 == LastFrame || crf2 == LastFrame)
			default:
				return 2*b2i(a0 == LastFrame) + 2*b2i(l0 == LastFrame)
			}
		}
	case hasAbove || hasLeft:
		edge := above
		if !hasAbove {
			edge = left
		}
		if !isInterBlock(edge) {
			return 2
		}
		if !hasSecondRef(edge) {
			return 4 * b2i(edge.RefFrame[0] == LastFrame)
		}
		return 1 + b2i(edge.RefFrame[0] == LastFrame || edge.RefFrame[1] == LastFrame)
	default:
		return 2
	}
}

// GetPredContextSingleRefP2 mirrors vp9_get_pred_context_single_ref_p2
// — the second single-ref bit context.
func GetPredContextSingleRefP2(above, left *NeighborMi) int {
	hasAbove := above != nil
	hasLeft := left != nil
	switch {
	case hasAbove && hasLeft:
		aboveIntra := !isInterBlock(above)
		leftIntra := !isInterBlock(left)
		switch {
		case aboveIntra && leftIntra:
			return 2
		case aboveIntra || leftIntra:
			edge := above
			if aboveIntra {
				edge = left
			}
			if !hasSecondRef(edge) {
				if edge.RefFrame[0] == LastFrame {
					return 3
				}
				return 4 * b2i(edge.RefFrame[0] == GoldenFrame)
			}
			return 1 + 2*b2i(edge.RefFrame[0] == GoldenFrame ||
				edge.RefFrame[1] == GoldenFrame)
		default:
			aHasSecond := hasSecondRef(above)
			lHasSecond := hasSecondRef(left)
			a0, a1 := above.RefFrame[0], above.RefFrame[1]
			l0, l1 := left.RefFrame[0], left.RefFrame[1]
			switch {
			case aHasSecond && lHasSecond:
				if a0 == l0 && a1 == l1 {
					return 3 * b2i(a0 == GoldenFrame || a1 == GoldenFrame ||
						l0 == GoldenFrame || l1 == GoldenFrame)
				}
				return 2
			case aHasSecond || lHasSecond:
				rfs := a0
				if aHasSecond {
					rfs = l0
				}
				crf1 := a0
				crf2 := a1
				if !aHasSecond {
					crf1 = l0
					crf2 = l1
				}
				switch rfs {
				case GoldenFrame:
					return 3 + b2i(crf1 == GoldenFrame || crf2 == GoldenFrame)
				case AltrefFrame:
					return b2i(crf1 == GoldenFrame || crf2 == GoldenFrame)
				default:
					return 1 + 2*b2i(crf1 == GoldenFrame || crf2 == GoldenFrame)
				}
			default:
				if a0 == LastFrame && l0 == LastFrame {
					return 3
				}
				if a0 == LastFrame || l0 == LastFrame {
					edge0 := a0
					if a0 == LastFrame {
						edge0 = l0
					}
					return 4 * b2i(edge0 == GoldenFrame)
				}
				return 2*b2i(a0 == GoldenFrame) + 2*b2i(l0 == GoldenFrame)
			}
		}
	case hasAbove || hasLeft:
		edge := above
		if !hasAbove {
			edge = left
		}
		if !isInterBlock(edge) ||
			(edge.RefFrame[0] == LastFrame && !hasSecondRef(edge)) {
			return 2
		}
		if !hasSecondRef(edge) {
			return 4 * b2i(edge.RefFrame[0] == GoldenFrame)
		}
		return 3 * b2i(edge.RefFrame[0] == GoldenFrame || edge.RefFrame[1] == GoldenFrame)
	default:
		return 2
	}
}

// GetReferenceModeContext mirrors vp9_get_reference_mode_context. The
// 5-state context distinguishes (intra,single,compound) mixes of the
// above/left pair and selects an entry in fc->comp_inter_prob.
func GetReferenceModeContext(above, left *NeighborMi, refs CompoundFrameRefs) int {
	hasAbove := above != nil
	hasLeft := left != nil
	switch {
	case hasAbove && hasLeft:
		aboveSingle := !hasSecondRef(above)
		leftSingle := !hasSecondRef(left)
		switch {
		case aboveSingle && leftSingle:
			// neither edge uses comp pred (0/1)
			a := int8(0)
			if above.RefFrame[0] == refs.CompFixedRef {
				a = 1
			}
			l := int8(0)
			if left.RefFrame[0] == refs.CompFixedRef {
				l = 1
			}
			return int(a ^ l)
		case aboveSingle:
			// one of two edges uses comp pred (2/3)
			extra := 0
			if above.RefFrame[0] == refs.CompFixedRef || !isInterBlock(above) {
				extra = 1
			}
			return 2 + extra
		case leftSingle:
			extra := 0
			if left.RefFrame[0] == refs.CompFixedRef || !isInterBlock(left) {
				extra = 1
			}
			return 2 + extra
		default:
			// both edges use comp pred
			return 4
		}
	case hasAbove || hasLeft:
		edge := above
		if !hasAbove {
			edge = left
		}
		if !hasSecondRef(edge) {
			if edge.RefFrame[0] == refs.CompFixedRef {
				return 1
			}
			return 0
		}
		return 3
	default:
		return 1
	}
}
