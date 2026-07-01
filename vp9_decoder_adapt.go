package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func (d *VP9Decoder) adaptVP9FrameContext(hdr *vp9dec.UncompressedHeader,
	comp vp9dec.CompressedHeader, idx int,
) {
	if idx < 0 || idx >= common.FrameContexts ||
		hdr.ErrorResilientMode || hdr.FrameParallelDecoding {
		return
	}
	pre := &d.frameContexts[idx]
	vp9dec.AdaptFrameContextWithCounts(&d.fc, pre, &d.counts, hdr, comp.TxMode,
		d.lastHeaderValid && d.lastHeader.FrameType == common.KeyFrame)
}

func (d *VP9Decoder) countVP9NewMv(mv, refMv vp9dec.MV) {
	diff := vp9dec.MV{
		Row: mv.Row - refMv.Row,
		Col: mv.Col - refMv.Col,
	}
	vp9dec.IncMv(diff, &d.counts.Mv)
}

func (d *VP9Decoder) readVP9SkipWithCounts(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, segID int, above, left *vp9dec.NeighborMi,
) int {
	if vp9dec.SegFeatureActive(&hdr.Seg, segID, vp9dec.SegLvlSkip) {
		return 1
	}
	ctx := vp9dec.GetSkipContext(above, left)
	skip := int(r.Read(uint32(d.fc.SkipProbs[ctx])))
	if !hdr.FrameParallelDecoding {
		d.counts.Skip[ctx][skip]++
	}
	return skip
}

func (d *VP9Decoder) readVP9IsInterBlockWithCounts(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, segID int, above, left *vp9dec.NeighborMi,
) int {
	if vp9dec.SegFeatureActive(&hdr.Seg, segID, vp9dec.SegLvlRefFrame) {
		if int(vp9dec.GetSegData(&hdr.Seg, segID, vp9dec.SegLvlRefFrame)) != vp9dec.IntraFrame {
			return 1
		}
		return 0
	}
	ctx := vp9dec.GetIntraInterContext(above, left)
	isInter := int(r.Read(uint32(d.fc.IntraInterProb[ctx])))
	if !hdr.FrameParallelDecoding {
		d.counts.IntraInter[ctx][isInter]++
	}
	return isInter
}

func (d *VP9Decoder) readVP9TxSizeWithCounts(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, txMode common.TxMode, bsize common.BlockSize,
	above, left *vp9dec.NeighborMi, allowSelect bool,
) common.TxSize {
	maxTx := common.MaxTxsizeLookup[bsize]
	if allowSelect && txMode == common.TxModeSelect && bsize >= common.Block8x8 {
		ctx := vp9dec.GetTxSizeContext(above, left, maxTx)
		switch maxTx {
		case common.Tx8x8:
			probs := &d.fc.TxProbs.P8x8[ctx]
			tx := common.TxSize(r.Read(uint32(probs[0])))
			if !hdr.FrameParallelDecoding {
				d.counts.Tx.P8x8[ctx][tx]++
			}
			return tx
		case common.Tx16x16:
			probs := &d.fc.TxProbs.P16x16[ctx]
			tx := common.TxSize(r.Read(uint32(probs[0])))
			if tx != common.Tx4x4 {
				tx += common.TxSize(r.Read(uint32(probs[1])))
			}
			if !hdr.FrameParallelDecoding {
				d.counts.Tx.P16x16[ctx][tx]++
			}
			return tx
		case common.Tx32x32:
			probs := &d.fc.TxProbs.P32x32[ctx]
			tx := common.TxSize(r.Read(uint32(probs[0])))
			if tx != common.Tx4x4 {
				tx += common.TxSize(r.Read(uint32(probs[1])))
				if tx != common.Tx8x8 {
					tx += common.TxSize(r.Read(uint32(probs[2])))
				}
			}
			if !hdr.FrameParallelDecoding {
				d.counts.Tx.P32x32[ctx][tx]++
			}
			return tx
		}
	}
	cap := common.TxModeToBiggestTxSize[txMode]
	if maxTx < cap {
		return maxTx
	}
	return cap
}

func (d *VP9Decoder) readVP9InterModeWithCounts(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, ctx int,
) common.PredictionMode {
	mode := vp9dec.ReadInterMode(r, d.fc.InterModeProbs[ctx])
	if !hdr.FrameParallelDecoding {
		d.counts.InterMode[ctx][mode-common.NearestMv]++
	}
	return mode
}

func (d *VP9Decoder) readVP9SwitchableInterpFilterWithCounts(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, above, left *vp9dec.NeighborMi,
) vp9dec.InterpFilter {
	ctx := vp9dec.GetPredContextSwitchableInterp(above, left)
	probs := &d.fc.SwitchableInterpProb[ctx]
	filter := vp9dec.InterpEighttap
	if r.Read(uint32(probs[0])) != 0 {
		filter = vp9dec.InterpEighttapSmooth
		if r.Read(uint32(probs[1])) != 0 {
			filter = vp9dec.InterpEighttapSharp
		}
	}
	if !hdr.FrameParallelDecoding {
		d.counts.SwitchableInterp[ctx][filter]++
	}
	return filter
}

func (d *VP9Decoder) readVP9RefFramesWithCounts(r *bitstream.Reader,
	hdr *vp9dec.UncompressedHeader, frameMode vp9dec.ReferenceMode,
	signBias [vp9dec.MaxRefFrames]uint8, refs vp9dec.CompoundFrameRefs,
	segID int, above, left *vp9dec.NeighborMi, out *[2]int8,
) {
	if vp9dec.SegFeatureActive(&hdr.Seg, segID, vp9dec.SegLvlRefFrame) {
		out[0] = int8(vp9dec.GetSegData(&hdr.Seg, segID, vp9dec.SegLvlRefFrame))
		out[1] = vp9dec.NoRefFrame
		return
	}

	mode := frameMode
	if frameMode == vp9dec.ReferenceModeSelect {
		ctx := vp9dec.GetReferenceModeContext(above, left, refs)
		bit := int(r.Read(uint32(d.fc.ReferenceModeProbs.CompInterProb[ctx])))
		if !hdr.FrameParallelDecoding {
			d.counts.CompInter[ctx][bit]++
		}
		if bit != 0 {
			mode = vp9dec.CompoundReference
		} else {
			mode = vp9dec.SingleReference
		}
	}

	switch mode {
	case vp9dec.CompoundReference:
		idx := int(signBias[refs.CompFixedRef])
		ctx := vp9dec.GetPredContextCompRefP(above, left, refs, signBias)
		bit := int(r.Read(uint32(d.fc.ReferenceModeProbs.CompRefProb[ctx])))
		if !hdr.FrameParallelDecoding {
			d.counts.CompRef[ctx][bit]++
		}
		out[idx] = refs.CompFixedRef
		out[1-idx] = refs.CompVarRef[bit]
	case vp9dec.SingleReference:
		ctx0 := vp9dec.GetPredContextSingleRefP1(above, left)
		bit0 := int(r.Read(uint32(d.fc.ReferenceModeProbs.SingleRefProb[ctx0][0])))
		if !hdr.FrameParallelDecoding {
			d.counts.SingleRef[ctx0][0][bit0]++
		}
		if bit0 != 0 {
			ctx1 := vp9dec.GetPredContextSingleRefP2(above, left)
			bit1 := int(r.Read(uint32(d.fc.ReferenceModeProbs.SingleRefProb[ctx1][1])))
			if !hdr.FrameParallelDecoding {
				d.counts.SingleRef[ctx1][1][bit1]++
			}
			if bit1 != 0 {
				out[0] = vp9dec.AltrefFrame
			} else {
				out[0] = vp9dec.GoldenFrame
			}
		} else {
			out[0] = vp9dec.LastFrame
		}
		out[1] = vp9dec.NoRefFrame
	}
}

func (d *VP9Decoder) readVP9IntraBlockModeInfoInterWithCounts(
	r *bitstream.Reader, hdr *vp9dec.UncompressedHeader, out *vp9dec.NeighborMi,
) common.PredictionMode {
	readY := func(sizeGroup int) common.PredictionMode {
		mode := vp9dec.ReadIntraModeYInter(r, &d.fc, sizeGroup)
		if !hdr.FrameParallelDecoding {
			d.counts.YMode[sizeGroup][mode]++
		}
		return mode
	}
	switch out.SbType {
	case common.Block4x4:
		for i := range 4 {
			out.Bmi[i].AsMode = readY(0)
		}
		out.Mode = out.Bmi[3].AsMode
	case common.Block4x8:
		out.Bmi[0].AsMode = readY(0)
		out.Bmi[2].AsMode = out.Bmi[0].AsMode
		out.Bmi[1].AsMode = readY(0)
		out.Bmi[3].AsMode = out.Bmi[1].AsMode
		out.Mode = out.Bmi[1].AsMode
	case common.Block8x4:
		out.Bmi[0].AsMode = readY(0)
		out.Bmi[1].AsMode = out.Bmi[0].AsMode
		out.Bmi[2].AsMode = readY(0)
		out.Bmi[3].AsMode = out.Bmi[2].AsMode
		out.Mode = out.Bmi[2].AsMode
	default:
		out.Mode = readY(int(common.SizeGroupLookup[out.SbType]))
	}
	uvMode := vp9dec.ReadIntraModeUvInter(r, &d.fc, out.Mode)
	if !hdr.FrameParallelDecoding {
		d.counts.UvMode[out.Mode][uvMode]++
	}
	out.InterpFilter = uint8(vp9dec.SwitchableFilters)
	out.RefFrame[0] = vp9dec.IntraFrame
	out.RefFrame[1] = vp9dec.NoRefFrame
	return uvMode
}
