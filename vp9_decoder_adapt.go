package govpx

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

type vp9TxCounts struct {
	P32x32 [vp9dec.TxSizeContexts][common.TxSizes]uint32
	P16x16 [vp9dec.TxSizeContexts][common.TxSizes - 1]uint32
	P8x8   [vp9dec.TxSizeContexts][common.TxSizes - 2]uint32
}

type vp9NmvComponentCounts struct {
	Sign     [2]uint32
	Classes  [vp9dec.MvClasses]uint32
	Class0   [vp9dec.Class0Size]uint32
	Bits     [vp9dec.MvOffsetBits][2]uint32
	Class0Fp [vp9dec.Class0Size][vp9dec.MvFpSize]uint32
	Fp       [vp9dec.MvFpSize]uint32
	Class0Hp [2]uint32
	Hp       [2]uint32
}

type vp9NmvContextCounts struct {
	Joints [vp9dec.MvJoints]uint32
	Comps  [2]vp9NmvComponentCounts
}

type vp9FrameCounts struct {
	YMode            [vp9dec.BlockSizeGroups][common.IntraModes]uint32
	UvMode           [common.IntraModes][common.IntraModes]uint32
	Partition        [common.PartitionContexts][common.PartitionTypes]uint32
	Coef             vp9dec.CoefCounts
	SwitchableInterp [vp9dec.SwitchableFilterContexts][vp9dec.SwitchableFilters]uint32
	InterMode        [common.InterModeContexts][common.InterModes]uint32
	IntraInter       [common.IntraInterContexts][2]uint32
	CompInter        [common.CompInterContexts][2]uint32
	SingleRef        [common.RefContexts][2][2]uint32
	CompRef          [common.RefContexts][2]uint32
	Tx               vp9TxCounts
	Skip             [common.SkipContexts][2]uint32
	Mv               vp9NmvContextCounts
}

func (d *VP9Decoder) adaptVP9FrameContext(hdr *vp9dec.UncompressedHeader,
	comp vp9dec.CompressedHeader, idx int,
) {
	if idx < 0 || idx >= common.FrameContexts ||
		hdr.ErrorResilientMode || hdr.FrameParallelDecoding {
		return
	}
	pre := &d.frameContexts[idx]
	d.adaptVP9CoefProbs(pre, hdr)
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		return
	}
	d.adaptVP9ModeProbs(pre, hdr, comp)
	d.adaptVP9MvProbs(pre, hdr.AllowHighPrecisionMv)
}

func (d *VP9Decoder) adaptVP9CoefProbs(pre *vp9dec.FrameContext,
	hdr *vp9dec.UncompressedHeader,
) {
	const (
		coefCountSat                = 24
		coefMaxUpdateFactor         = 112
		coefCountSatKey             = 24
		coefMaxUpdateFactorKey      = 112
		coefCountSatAfterKey        = 24
		coefMaxUpdateFactorAfterKey = 128
	)
	countSat := uint32(coefCountSat)
	updateFactor := uint32(coefMaxUpdateFactor)
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		countSat = coefCountSatKey
		updateFactor = coefMaxUpdateFactorKey
	} else if d.lastHeaderValid && d.lastHeader.FrameType == common.KeyFrame {
		countSat = coefCountSatAfterKey
		updateFactor = coefMaxUpdateFactorAfterKey
	}

	for tx := common.Tx4x4; tx <= common.Tx32x32; tx++ {
		for plane := range vp9dec.CoefPlaneTypes {
			for ref := range vp9dec.CoefRefTypes {
				for band := range vp9dec.CoefBands {
					for ctx := range vp9dec.BandCoefContexts(band) {
						n0 := d.counts.Coef.Coef[tx][plane][ref][band][ctx][0]
						n1 := d.counts.Coef.Coef[tx][plane][ref][band][ctx][1]
						n2 := d.counts.Coef.Coef[tx][plane][ref][band][ctx][2]
						neob := d.counts.Coef.Coef[tx][plane][ref][band][ctx][3]
						eob := d.counts.Coef.EobBranch[tx][plane][ref][band][ctx]
						branch := [vp9dec.UnconstrainedNodes][2]uint32{
							{neob, eob - neob},
							{n0, n1 + n2},
							{n1, n2},
						}
						for node := range vp9dec.UnconstrainedNodes {
							d.fc.CoefProbs[tx][plane][ref][band][ctx][node] =
								vp9MergeProbs(pre.CoefProbs[tx][plane][ref][band][ctx][node],
									branch[node], countSat, updateFactor)
						}
					}
				}
			}
		}
	}
}

func (d *VP9Decoder) adaptVP9ModeProbs(pre *vp9dec.FrameContext,
	hdr *vp9dec.UncompressedHeader, comp vp9dec.CompressedHeader,
) {
	for i := range d.fc.IntraInterProb {
		d.fc.IntraInterProb[i] = vp9ModeMvMergeProbs(pre.IntraInterProb[i],
			d.counts.IntraInter[i])
	}
	for i := range d.fc.ReferenceModeProbs.CompInterProb {
		d.fc.ReferenceModeProbs.CompInterProb[i] =
			vp9ModeMvMergeProbs(pre.ReferenceModeProbs.CompInterProb[i],
				d.counts.CompInter[i])
	}
	for i := range d.fc.ReferenceModeProbs.CompRefProb {
		d.fc.ReferenceModeProbs.CompRefProb[i] =
			vp9ModeMvMergeProbs(pre.ReferenceModeProbs.CompRefProb[i],
				d.counts.CompRef[i])
	}
	for i := range d.fc.ReferenceModeProbs.SingleRefProb {
		for j := range d.fc.ReferenceModeProbs.SingleRefProb[i] {
			d.fc.ReferenceModeProbs.SingleRefProb[i][j] =
				vp9ModeMvMergeProbs(pre.ReferenceModeProbs.SingleRefProb[i][j],
					d.counts.SingleRef[i][j])
		}
	}
	for i := range d.fc.InterModeProbs {
		vp9TreeMergeProbs(common.InterModeTree[:], pre.InterModeProbs[i][:],
			d.counts.InterMode[i][:], d.fc.InterModeProbs[i][:])
	}
	for i := range d.fc.YModeProb {
		vp9TreeMergeProbs(common.IntraModeTree[:], pre.YModeProb[i][:],
			d.counts.YMode[i][:], d.fc.YModeProb[i][:])
	}
	for i := range d.fc.UvModeProb {
		vp9TreeMergeProbs(common.IntraModeTree[:], pre.UvModeProb[i][:],
			d.counts.UvMode[i][:], d.fc.UvModeProb[i][:])
	}
	for i := range d.fc.PartitionProb {
		vp9TreeMergeProbs(common.PartitionTree[:], pre.PartitionProb[i][:],
			d.counts.Partition[i][:], d.fc.PartitionProb[i][:])
	}
	if hdr.InterpFilter == vp9dec.InterpSwitchable {
		for i := range d.fc.SwitchableInterpProb {
			vp9TreeMergeProbs(common.SwitchableInterpTree[:],
				pre.SwitchableInterpProb[i][:], d.counts.SwitchableInterp[i][:],
				d.fc.SwitchableInterpProb[i][:])
		}
	}
	if comp.TxMode == common.TxModeSelect {
		for i := range vp9dec.TxSizeContexts {
			c8 := d.counts.Tx.P8x8[i]
			d.fc.TxProbs.P8x8[i][0] = vp9ModeMvMergeProbs(pre.TxProbs.P8x8[i][0],
				[2]uint32{c8[0], c8[1]})

			c16 := d.counts.Tx.P16x16[i]
			d.fc.TxProbs.P16x16[i][0] = vp9ModeMvMergeProbs(pre.TxProbs.P16x16[i][0],
				[2]uint32{c16[0], c16[1] + c16[2]})
			d.fc.TxProbs.P16x16[i][1] = vp9ModeMvMergeProbs(pre.TxProbs.P16x16[i][1],
				[2]uint32{c16[1], c16[2]})

			c32 := d.counts.Tx.P32x32[i]
			d.fc.TxProbs.P32x32[i][0] = vp9ModeMvMergeProbs(pre.TxProbs.P32x32[i][0],
				[2]uint32{c32[0], c32[1] + c32[2] + c32[3]})
			d.fc.TxProbs.P32x32[i][1] = vp9ModeMvMergeProbs(pre.TxProbs.P32x32[i][1],
				[2]uint32{c32[1], c32[2] + c32[3]})
			d.fc.TxProbs.P32x32[i][2] = vp9ModeMvMergeProbs(pre.TxProbs.P32x32[i][2],
				[2]uint32{c32[2], c32[3]})
		}
	}
	for i := range d.fc.SkipProbs {
		d.fc.SkipProbs[i] = vp9ModeMvMergeProbs(pre.SkipProbs[i],
			d.counts.Skip[i])
	}
}

func (d *VP9Decoder) adaptVP9MvProbs(pre *vp9dec.FrameContext, allowHP bool) {
	vp9TreeMergeProbs(tables.MvJointTree[:], pre.Nmvc.Joints[:],
		d.counts.Mv.Joints[:], d.fc.Nmvc.Joints[:])
	for i := range 2 {
		comp := &d.fc.Nmvc.Comps[i]
		preComp := &pre.Nmvc.Comps[i]
		counts := &d.counts.Mv.Comps[i]

		comp.Sign = vp9ModeMvMergeProbs(preComp.Sign, counts.Sign)
		vp9TreeMergeProbs(tables.MvClassTree[:], preComp.Classes[:],
			counts.Classes[:], comp.Classes[:])
		vp9TreeMergeProbs(tables.MvClass0Tree[:], preComp.Class0[:],
			counts.Class0[:], comp.Class0[:])
		for j := range vp9dec.MvOffsetBits {
			comp.Bits[j] = vp9ModeMvMergeProbs(preComp.Bits[j], counts.Bits[j])
		}
		for j := range vp9dec.Class0Size {
			vp9TreeMergeProbs(tables.MvFpTree[:], preComp.Class0Fp[j][:],
				counts.Class0Fp[j][:], comp.Class0Fp[j][:])
		}
		vp9TreeMergeProbs(tables.MvFpTree[:], preComp.Fp[:],
			counts.Fp[:], comp.Fp[:])
		if allowHP {
			comp.Class0Hp = vp9ModeMvMergeProbs(preComp.Class0Hp, counts.Class0Hp)
			comp.Hp = vp9ModeMvMergeProbs(preComp.Hp, counts.Hp)
		}
	}
}

func vp9TreeMergeProbs(tree []int8, preProbs []uint8, counts []uint32, probs []uint8) {
	var walk func(i int) uint32
	walk = func(i int) uint32 {
		var left, right uint32
		if tree[i] <= 0 {
			left = counts[-tree[i]]
		} else {
			left = walk(int(tree[i]))
		}
		if tree[i+1] <= 0 {
			right = counts[-tree[i+1]]
		} else {
			right = walk(int(tree[i+1]))
		}
		probs[i>>1] = vp9ModeMvMergeProbs(preProbs[i>>1], [2]uint32{left, right})
		return left + right
	}
	walk(0)
}

func vp9ModeMvMergeProbs(preProb uint8, ct [2]uint32) uint8 {
	den := ct[0] + ct[1]
	if den == 0 {
		return preProb
	}
	const modeMvCountSat = 20
	count := min(den, uint32(modeMvCountSat))
	factor := [...]uint32{
		0, 6, 12, 19, 25, 32, 38, 44, 51, 57, 64,
		70, 76, 83, 89, 96, 102, 108, 115, 121, 128,
	}[count]
	prob := vp9GetProb(ct[0], den)
	return uint8((uint32(preProb)*(256-factor) + uint32(prob)*factor + 128) >> 8)
}

func vp9MergeProbs(preProb uint8, ct [2]uint32, countSat, maxUpdateFactor uint32) uint8 {
	den := ct[0] + ct[1]
	prob := uint8(128)
	if den != 0 {
		prob = vp9GetProb(ct[0], den)
	}
	count := min(den, countSat)
	factor := maxUpdateFactor * count / countSat
	return uint8((uint32(preProb)*(256-factor) + uint32(prob)*factor + 128) >> 8)
}

func vp9GetProb(num, den uint32) uint8 {
	p := (uint64(num)*256 + uint64(den>>1)) / uint64(den)
	if p == 0 {
		return 1
	}
	if p > 255 {
		return 255
	}
	return uint8(p)
}

func (d *VP9Decoder) countVP9NewMv(mv, refMv vp9dec.MV) {
	diff := vp9dec.MV{
		Row: mv.Row - refMv.Row,
		Col: mv.Col - refMv.Col,
	}
	vp9IncMv(diff, &d.counts.Mv)
}

func vp9IncMv(mv vp9dec.MV, counts *vp9NmvContextCounts) {
	joint := vp9GetMvJoint(mv)
	counts.Joints[joint]++
	if joint == tables.MvJointHzVnz || joint == tables.MvJointHnzVnz {
		vp9IncMvComponent(mv.Row, &counts.Comps[0])
	}
	if joint == tables.MvJointHnzVz || joint == tables.MvJointHnzVnz {
		vp9IncMvComponent(mv.Col, &counts.Comps[1])
	}
}

func vp9IncMvComponent(v int16, counts *vp9NmvComponentCounts) {
	sign := 0
	zv := int(v)
	if zv < 0 {
		sign = 1
		zv = -zv
	}
	counts.Sign[sign]++
	z := zv - 1
	cls, offset := vp9GetMvClass(z)
	counts.Classes[cls]++
	d := offset >> 3
	f := (offset >> 1) & 3
	hp := offset & 1
	if cls == tables.MvClass0 {
		counts.Class0[d]++
		counts.Class0Fp[d][f]++
		counts.Class0Hp[hp]++
		return
	}
	nBits := cls + vp9dec.Class0Bits - 1
	for i := 0; i < nBits; i++ {
		counts.Bits[i][(d>>i)&1]++
	}
	counts.Fp[f]++
	counts.Hp[hp]++
}

func vp9GetMvJoint(mv vp9dec.MV) int {
	switch {
	case mv.Row == 0 && mv.Col == 0:
		return tables.MvJointZero
	case mv.Row == 0:
		return tables.MvJointHnzVz
	case mv.Col == 0:
		return tables.MvJointHzVnz
	default:
		return tables.MvJointHnzVnz
	}
}

func vp9GetMvClass(z int) (cls int, offset int) {
	if z >= vp9dec.Class0Size*4096 {
		cls = tables.MvClass10
	} else if z < vp9dec.Class0Size*8 {
		cls = tables.MvClass0
	} else {
		cls = bits.Len(uint(z>>3)) - 1
	}
	return cls, z - vp9MvClassBase(cls)
}

func vp9MvClassBase(cls int) int {
	if cls == 0 {
		return 0
	}
	return vp9dec.Class0Size << uint(cls+2)
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
	tx := vp9dec.ReadTxSize(r, &d.fc, txMode, bsize, above, left, allowSelect)
	maxTx := common.MaxTxsizeLookup[bsize]
	if !hdr.FrameParallelDecoding && allowSelect &&
		txMode == common.TxModeSelect && bsize >= common.Block8x8 {
		ctx := vp9dec.GetTxSizeContext(above, left, maxTx)
		switch maxTx {
		case common.Tx8x8:
			d.counts.Tx.P8x8[ctx][tx]++
		case common.Tx16x16:
			d.counts.Tx.P16x16[ctx][tx]++
		case common.Tx32x32:
			d.counts.Tx.P32x32[ctx][tx]++
		}
	}
	return tx
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
	filter := vp9dec.ReadSwitchableInterpFilter(r, &d.fc, above, left)
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
