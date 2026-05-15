package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 per-block inter-frame mode composer (encoder side). Ported
// from libvpx v1.16.0 vp9/encoder/vp9_bitstream.c —
// pack_inter_mode_mvs. Composes the per-block writers already in
// place (WriteSegmentId, WriteSkip, WriteIsInterBlock,
// WriteSelectedTxSize, WriteRefFrames, WriteIntraMode,
// WriteInterMode, WriteSwitchableInterpFilter, WriteMv) into the
// full inter-frame wire fragment.

// WriteInterBlockArgs bundles the inputs WriteInterBlock consults.
//
// `InterModeCtx` and `SwitchableInterpCtx` are pre-computed by the
// caller: in libvpx they come from mbmi_ext->mode_context[ref] and
// get_pred_context_switchable_interp(xd) respectively, both of
// which need the MV-ref search state the writer doesn't carry.
//
// `Mv` carries the >=8x8 block-level motion vector for each ref.
// Sub-8x8 per-subblock MVs live in `Mi.Bmi[j].AsMv`.
// `BestRefMv` is mbmi_ext->ref_mvs[ref_frame[ref]][0].as_mv per ref —
// the predictor the encoded delta is computed against.
type WriteInterBlockArgs struct {
	Seg     *vp9dec.SegmentationParams
	Mi      *vp9dec.NeighborMi
	AboveMi *vp9dec.NeighborMi
	LeftMi  *vp9dec.NeighborMi
	Fc      *vp9dec.FrameContext

	TxMode    common.TxMode
	MaxTxSize common.TxSize
	TxProbs   []uint8

	FrameRefMode vp9dec.ReferenceMode
	InterpFilter vp9dec.InterpFilter
	AllowHP      bool

	CompFixedRef     int8
	CompVarRef       [2]int8
	RefFrameSignBias [vp9dec.MaxRefFrames]uint8

	InterModeCtx        int
	SwitchableInterpCtx int

	IsCompound bool

	UvMode common.PredictionMode

	Mv        [2]vp9dec.MV
	BestRefMv [2]vp9dec.MV
}

// WriteInterBlock mirrors libvpx's pack_inter_mode_mvs. Emits the
// per-block inter-frame wire fragment in order:
//
//  1. segment id (when seg.UpdateMap).
//  2. skip bit via WriteSkip.
//  3. is_inter via WriteIsInterBlock (gates on SEG_LVL_REF_FRAME).
//  4. selected tx size if bsize >=8x8 && TxMode==Select && !(is_inter && skip).
//  5. intra path: Y mode(s) against fc.YModeProb (size-group indexed
//     for >=8x8; per-subblock for sub-8x8 with size_group=0) + UV
//     mode against fc.UvModeProb[Y mode].
//  6. inter path:
//     - ref frames via WriteRefFrames.
//     - inter mode for bsize >=8x8 (skipped if SEG_LVL_SKIP).
//     - switchable interp filter when frame InterpFilter==Switchable.
//     - per-sub-block inter mode + MV for sub-8x8 NEWMV;
//     single MV at block level for >=8x8 NEWMV.
func WriteInterBlock(bw *bitstream.Writer, a WriteInterBlockArgs) {
	bsize := a.Mi.SbType
	segID := int(a.Mi.SegmentID)
	skip := int(a.Mi.Skip)
	isInter := 0
	if a.Mi.RefFrame[0] > vp9dec.IntraFrame {
		isInter = 1
	}

	WriteInterSegmentId(bw, a.Seg, segID, a.Mi.SegIDPredicted,
		a.AboveMi, a.LeftMi)
	WriteSkip(bw, WriteSkipArgs{
		Seg:       a.Seg,
		SegID:     segID,
		SkipProbs: a.Fc.SkipProbs,
		Above:     a.AboveMi,
		Left:      a.LeftMi,
	}, skip)
	WriteIsInterBlock(bw, a.Seg, segID, a.Fc.IntraInterProb,
		a.AboveMi, a.LeftMi, isInter)

	if bsize >= common.Block8x8 && a.TxMode == common.TxModeSelect &&
		!(isInter == 1 && skip == 1) {
		WriteSelectedTxSize(bw, a.Mi.TxSize, a.MaxTxSize, a.TxProbs)
	}

	if isInter == 0 {
		if bsize >= common.Block8x8 {
			sg := int(common.SizeGroupLookup[bsize])
			WriteIntraMode(bw, a.Mi.Mode, a.Fc.YModeProb[sg][:])
		} else {
			num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
			num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
			for idy := 0; idy < 2; idy += num4x4H {
				for idx := 0; idx < 2; idx += num4x4W {
					j := idy*2 + idx
					WriteIntraMode(bw, a.Mi.Bmi[j].AsMode, a.Fc.YModeProb[0][:])
				}
			}
		}
		WriteIntraMode(bw, a.UvMode, a.Fc.UvModeProb[a.Mi.Mode][:])
		return
	}

	interProbs := a.Fc.InterModeProbs[a.InterModeCtx][:]
	WriteRefFrames(bw, WriteRefFramesArgs{
		Seg:              a.Seg,
		SegID:            segID,
		FrameMode:        a.FrameRefMode,
		CompFixedRef:     a.CompFixedRef,
		CompVarRef:       a.CompVarRef,
		RefFrameSignBias: a.RefFrameSignBias,
		CompInterProb:    a.Fc.ReferenceModeProbs.CompInterProb,
		CompRefProb:      a.Fc.ReferenceModeProbs.CompRefProb,
		SingleRefProb:    a.Fc.ReferenceModeProbs.SingleRefProb,
		Above:            a.AboveMi,
		Left:             a.LeftMi,
		IsCompound:       a.IsCompound,
		RefFrame:         a.Mi.RefFrame,
	})

	if !vp9dec.SegFeatureActive(a.Seg, segID, vp9dec.SegLvlSkip) {
		if bsize >= common.Block8x8 {
			WriteInterMode(bw, a.Mi.Mode, interProbs)
		}
	}

	if a.InterpFilter == vp9dec.InterpSwitchable {
		probs := a.Fc.SwitchableInterpProb[a.SwitchableInterpCtx][:]
		WriteSwitchableInterpFilter(bw, int(a.Mi.InterpFilter), probs)
	}

	nrefs := 1
	if a.IsCompound {
		nrefs = 2
	}

	if bsize < common.Block8x8 {
		num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
		num4x4H := int(common.Num4x4BlocksHighLookup[bsize])
		for idy := 0; idy < 2; idy += num4x4H {
			for idx := 0; idx < 2; idx += num4x4W {
				j := idy*2 + idx
				bMode := a.Mi.Bmi[j].AsMode
				WriteInterMode(bw, bMode, interProbs)
				if bMode == common.NewMv {
					for ref := 0; ref < nrefs; ref++ {
						WriteMv(bw, a.Mi.Bmi[j].AsMv[ref], a.BestRefMv[ref], &a.Fc.Nmvc, a.AllowHP)
					}
				}
			}
		}
		return
	}

	if a.Mi.Mode == common.NewMv {
		for ref := 0; ref < nrefs; ref++ {
			WriteMv(bw, a.Mv[ref], a.BestRefMv[ref], &a.Fc.Nmvc, a.AllowHP)
		}
	}
}
