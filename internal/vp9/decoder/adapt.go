package decoder

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const (
	// libvpx: vpx_dsp/prob.h:37 — MODE_MV_COUNT_SAT.
	modeMvCountSat uint32 = 20
	// libvpx: vpx_dsp/prob.h:78-82 — last entry of count_to_update_factor[].
	modeMvMaxUpdateFactor uint32 = 128
	// libvpx: vp9/common/vp9_entropy.c:1048 — COEF_COUNT_SAT.
	coefCountSatInterAfterInter uint32 = 24
	// libvpx: vp9/common/vp9_entropy.c:1049 — COEF_MAX_UPDATE_FACTOR.
	coefMaxUpdateFactorInterAfterInter uint32 = 112
	// libvpx: vp9/common/vp9_entropy.c:1050 — COEF_COUNT_SAT_KEY.
	coefCountSatIntraOnly uint32 = 24
	// libvpx: vp9/common/vp9_entropy.c:1051 — COEF_MAX_UPDATE_FACTOR_KEY.
	coefMaxUpdateFactorIntraOnly uint32 = 112
	// libvpx: vp9/common/vp9_entropy.c:1052 — COEF_COUNT_SAT_AFTER_KEY.
	coefCountSatInterAfterKey uint32 = 24
	// libvpx: vp9/common/vp9_entropy.c:1053 — COEF_MAX_UPDATE_FACTOR_AFTER_KEY.
	coefMaxUpdateFactorInterAfterKey uint32 = 128
)

var modeMvUpdateFactors = [...]uint32{
	0, 6, 12, 19, 25, 32, 38, 44, 51, 57, 64,
	70, 76, 83, 89, 96, 102, 108, 115, 121, 128,
}

// TxCounts carries VP9 transform-size adaptation counts.
type TxCounts struct {
	P32x32 [TxSizeContexts][common.TxSizes]uint32
	P16x16 [TxSizeContexts][common.TxSizes - 1]uint32
	P8x8   [TxSizeContexts][common.TxSizes - 2]uint32
}

// NmvComponentCounts carries VP9 motion-vector component adaptation counts.
type NmvComponentCounts struct {
	Sign     [2]uint32
	Classes  [MvClasses]uint32
	Class0   [Class0Size]uint32
	Bits     [MvOffsetBits][2]uint32
	Class0Fp [Class0Size][MvFpSize]uint32
	Fp       [MvFpSize]uint32
	Class0Hp [2]uint32
	Hp       [2]uint32
}

// NmvContextCounts carries VP9 motion-vector adaptation counts.
type NmvContextCounts struct {
	Joints [MvJoints]uint32
	Comps  [2]NmvComponentCounts
}

// FrameCounts carries the VP9 frame adaptation counts accumulated while
// parsing a non-frame-parallel frame.
type FrameCounts struct {
	YMode            [BlockSizeGroups][common.IntraModes]uint32
	UvMode           [common.IntraModes][common.IntraModes]uint32
	Partition        [common.PartitionContexts][common.PartitionTypes]uint32
	Coef             CoefCounts
	SwitchableInterp [SwitchableFilterContexts][SwitchableFilters]uint32
	InterMode        [common.InterModeContexts][common.InterModes]uint32
	IntraInter       [common.IntraInterContexts][2]uint32
	CompInter        [common.CompInterContexts][2]uint32
	SingleRef        [common.RefContexts][2][2]uint32
	CompRef          [common.RefContexts][2]uint32
	Tx               TxCounts
	Skip             [common.SkipContexts][2]uint32
	Mv               NmvContextCounts
}

// AdaptFrameContextWithCounts applies libvpx-style VP9 probability adaptation
// from the accumulated frame counts.
func AdaptFrameContextWithCounts(fc *FrameContext,
	pre *FrameContext, counts *FrameCounts,
	hdr *UncompressedHeader, txMode common.TxMode, afterKey bool,
) {
	if fc == nil || pre == nil || counts == nil || hdr == nil {
		return
	}
	adaptCoefProbsWithCounts(fc, pre, counts, hdr, afterKey)
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		return
	}
	adaptModeProbsWithCounts(fc, pre, counts, hdr, txMode)
	adaptMvProbsWithCounts(fc, pre, counts, hdr.AllowHighPrecisionMv)
}

func adaptCoefProbsWithCounts(fc *FrameContext,
	pre *FrameContext, counts *FrameCounts,
	hdr *UncompressedHeader, afterKey bool,
) {
	countSat := coefCountSatInterAfterInter
	updateFactor := coefMaxUpdateFactorInterAfterInter
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		countSat = coefCountSatIntraOnly
		updateFactor = coefMaxUpdateFactorIntraOnly
	} else if afterKey {
		countSat = coefCountSatInterAfterKey
		updateFactor = coefMaxUpdateFactorInterAfterKey
	}

	for tx := common.Tx4x4; tx <= common.Tx32x32; tx++ {
		for plane := range CoefPlaneTypes {
			for ref := range CoefRefTypes {
				for band := range CoefBands {
					for ctx := range BandCoefContexts(band) {
						n0 := counts.Coef.Coef[tx][plane][ref][band][ctx][0]
						n1 := counts.Coef.Coef[tx][plane][ref][band][ctx][1]
						n2 := counts.Coef.Coef[tx][plane][ref][band][ctx][2]
						neob := counts.Coef.Coef[tx][plane][ref][band][ctx][3]
						eob := counts.Coef.EobBranch[tx][plane][ref][band][ctx]
						branch := [UnconstrainedNodes][2]uint32{
							{neob, eob - neob},
							{n0, n1 + n2},
							{n1, n2},
						}
						for node := range UnconstrainedNodes {
							fc.CoefProbs[tx][plane][ref][band][ctx][node] =
								mergeProbs(pre.CoefProbs[tx][plane][ref][band][ctx][node],
									branch[node], countSat, updateFactor)
						}
					}
				}
			}
		}
	}
}

func adaptModeProbsWithCounts(fc *FrameContext,
	pre *FrameContext, counts *FrameCounts,
	hdr *UncompressedHeader, txMode common.TxMode,
) {
	for i := range fc.IntraInterProb {
		fc.IntraInterProb[i] = modeMvMergeProbs(pre.IntraInterProb[i],
			counts.IntraInter[i])
	}
	for i := range fc.ReferenceModeProbs.CompInterProb {
		fc.ReferenceModeProbs.CompInterProb[i] =
			modeMvMergeProbs(pre.ReferenceModeProbs.CompInterProb[i],
				counts.CompInter[i])
	}
	for i := range fc.ReferenceModeProbs.CompRefProb {
		fc.ReferenceModeProbs.CompRefProb[i] =
			modeMvMergeProbs(pre.ReferenceModeProbs.CompRefProb[i],
				counts.CompRef[i])
	}
	for i := range fc.ReferenceModeProbs.SingleRefProb {
		for j := range fc.ReferenceModeProbs.SingleRefProb[i] {
			fc.ReferenceModeProbs.SingleRefProb[i][j] =
				modeMvMergeProbs(pre.ReferenceModeProbs.SingleRefProb[i][j],
					counts.SingleRef[i][j])
		}
	}
	for i := range fc.InterModeProbs {
		treeMergeProbs(common.InterModeTree[:], pre.InterModeProbs[i][:],
			counts.InterMode[i][:], fc.InterModeProbs[i][:])
	}
	for i := range fc.YModeProb {
		treeMergeProbs(common.IntraModeTree[:], pre.YModeProb[i][:],
			counts.YMode[i][:], fc.YModeProb[i][:])
	}
	for i := range fc.UvModeProb {
		treeMergeProbs(common.IntraModeTree[:], pre.UvModeProb[i][:],
			counts.UvMode[i][:], fc.UvModeProb[i][:])
	}
	for i := range fc.PartitionProb {
		treeMergeProbs(common.PartitionTree[:], pre.PartitionProb[i][:],
			counts.Partition[i][:], fc.PartitionProb[i][:])
	}
	if hdr.InterpFilter == InterpSwitchable {
		for i := range fc.SwitchableInterpProb {
			treeMergeProbs(common.SwitchableInterpTree[:],
				pre.SwitchableInterpProb[i][:], counts.SwitchableInterp[i][:],
				fc.SwitchableInterpProb[i][:])
		}
	}
	if txMode == common.TxModeSelect {
		for i := range TxSizeContexts {
			c8 := counts.Tx.P8x8[i]
			fc.TxProbs.P8x8[i][0] = modeMvMergeProbs(pre.TxProbs.P8x8[i][0],
				[2]uint32{c8[0], c8[1]})

			c16 := counts.Tx.P16x16[i]
			fc.TxProbs.P16x16[i][0] = modeMvMergeProbs(pre.TxProbs.P16x16[i][0],
				[2]uint32{c16[0], c16[1] + c16[2]})
			fc.TxProbs.P16x16[i][1] = modeMvMergeProbs(pre.TxProbs.P16x16[i][1],
				[2]uint32{c16[1], c16[2]})

			c32 := counts.Tx.P32x32[i]
			fc.TxProbs.P32x32[i][0] = modeMvMergeProbs(pre.TxProbs.P32x32[i][0],
				[2]uint32{c32[0], c32[1] + c32[2] + c32[3]})
			fc.TxProbs.P32x32[i][1] = modeMvMergeProbs(pre.TxProbs.P32x32[i][1],
				[2]uint32{c32[1], c32[2] + c32[3]})
			fc.TxProbs.P32x32[i][2] = modeMvMergeProbs(pre.TxProbs.P32x32[i][2],
				[2]uint32{c32[2], c32[3]})
		}
	}
	for i := range fc.SkipProbs {
		fc.SkipProbs[i] = modeMvMergeProbs(pre.SkipProbs[i], counts.Skip[i])
	}
}

func adaptMvProbsWithCounts(fc *FrameContext,
	pre *FrameContext, counts *FrameCounts, allowHP bool,
) {
	treeMergeProbs(tables.MvJointTree[:], pre.Nmvc.Joints[:],
		counts.Mv.Joints[:], fc.Nmvc.Joints[:])
	for i := range 2 {
		comp := &fc.Nmvc.Comps[i]
		preComp := &pre.Nmvc.Comps[i]
		compCounts := &counts.Mv.Comps[i]

		comp.Sign = modeMvMergeProbs(preComp.Sign, compCounts.Sign)
		treeMergeProbs(tables.MvClassTree[:], preComp.Classes[:],
			compCounts.Classes[:], comp.Classes[:])
		treeMergeProbs(tables.MvClass0Tree[:], preComp.Class0[:],
			compCounts.Class0[:], comp.Class0[:])
		for j := range MvOffsetBits {
			comp.Bits[j] = modeMvMergeProbs(preComp.Bits[j], compCounts.Bits[j])
		}
		for j := range Class0Size {
			treeMergeProbs(tables.MvFpTree[:], preComp.Class0Fp[j][:],
				compCounts.Class0Fp[j][:], comp.Class0Fp[j][:])
		}
		treeMergeProbs(tables.MvFpTree[:], preComp.Fp[:],
			compCounts.Fp[:], comp.Fp[:])
		if allowHP {
			comp.Class0Hp = modeMvMergeProbs(preComp.Class0Hp, compCounts.Class0Hp)
			comp.Hp = modeMvMergeProbs(preComp.Hp, compCounts.Hp)
		}
	}
}

func treeMergeProbs(tree []int8, preProbs []uint8, counts []uint32, probs []uint8) {
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
		probs[i>>1] = modeMvMergeProbs(preProbs[i>>1], [2]uint32{left, right})
		return left + right
	}
	walk(0)
}

func modeMvMergeProbs(preProb uint8, ct [2]uint32) uint8 {
	den := ct[0] + ct[1]
	if den == 0 {
		return preProb
	}
	count := min(den, modeMvCountSat)
	factor := modeMvUpdateFactors[count]
	prob := getProb(ct[0], den)
	return uint8((uint32(preProb)*(256-factor) + uint32(prob)*factor + 128) >> 8)
}

func mergeProbs(preProb uint8, ct [2]uint32, countSat, maxUpdateFactor uint32) uint8 {
	den := ct[0] + ct[1]
	prob := uint8(128)
	if den != 0 {
		prob = getProb(ct[0], den)
	}
	count := min(den, countSat)
	factor := maxUpdateFactor * count / countSat
	return uint8((uint32(preProb)*(256-factor) + uint32(prob)*factor + 128) >> 8)
}

func getProb(num, den uint32) uint8 {
	p := (uint64(num)*256 + uint64(den>>1)) / uint64(den)
	if p == 0 {
		return 1
	}
	if p > 255 {
		return 255
	}
	return uint8(p)
}

// IncMv records one decoded NEWMV delta into the VP9 motion-vector count
// tables.
func IncMv(mv MV, counts *NmvContextCounts) {
	joint := GetMvJoint(mv)
	counts.Joints[joint]++
	if joint == tables.MvJointHzVnz || joint == tables.MvJointHnzVnz {
		incMvComponent(mv.Row, &counts.Comps[0])
	}
	if joint == tables.MvJointHnzVz || joint == tables.MvJointHnzVnz {
		incMvComponent(mv.Col, &counts.Comps[1])
	}
}

func incMvComponent(v int16, counts *NmvComponentCounts) {
	sign := 0
	zv := int(v)
	if zv < 0 {
		sign = 1
		zv = -zv
	}
	counts.Sign[sign]++
	z := zv - 1
	cls, offset := GetMvClass(z)
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
	nBits := cls + Class0Bits - 1
	for i := range nBits {
		counts.Bits[i][(d>>i)&1]++
	}
	counts.Fp[f]++
	counts.Hp[hp]++
}

// GetMvJoint returns the libvpx VP9 motion-vector joint class.
func GetMvJoint(mv MV) int {
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

// GetMvClass returns the libvpx VP9 motion-vector class and class offset for
// a positive magnitude minus one.
func GetMvClass(z int) (cls int, offset int) {
	if z >= Class0Size*4096 {
		cls = tables.MvClass10
	} else if z < Class0Size*8 {
		cls = tables.MvClass0
	} else {
		cls = bits.Len(uint(z>>3)) - 1
	}
	return cls, z - mvClassBase(cls)
}

func mvClassBase(cls int) int {
	if cls == 0 {
		return 0
	}
	return Class0Size << uint(cls+2)
}

// MergeFrameCounts adds src into dst.
func MergeFrameCounts(dst, src *FrameCounts) {
	for i := range dst.YMode {
		for j := range dst.YMode[i] {
			dst.YMode[i][j] += src.YMode[i][j]
		}
	}
	for i := range dst.UvMode {
		for j := range dst.UvMode[i] {
			dst.UvMode[i][j] += src.UvMode[i][j]
		}
	}
	for i := range dst.Partition {
		for j := range dst.Partition[i] {
			dst.Partition[i][j] += src.Partition[i][j]
		}
	}
	mergeCoefCounts(&dst.Coef, &src.Coef)
	for i := range dst.SwitchableInterp {
		for j := range dst.SwitchableInterp[i] {
			dst.SwitchableInterp[i][j] += src.SwitchableInterp[i][j]
		}
	}
	for i := range dst.InterMode {
		for j := range dst.InterMode[i] {
			dst.InterMode[i][j] += src.InterMode[i][j]
		}
	}
	for i := range dst.IntraInter {
		for j := range dst.IntraInter[i] {
			dst.IntraInter[i][j] += src.IntraInter[i][j]
		}
	}
	for i := range dst.CompInter {
		for j := range dst.CompInter[i] {
			dst.CompInter[i][j] += src.CompInter[i][j]
		}
	}
	for i := range dst.SingleRef {
		for j := range dst.SingleRef[i] {
			for k := range dst.SingleRef[i][j] {
				dst.SingleRef[i][j][k] += src.SingleRef[i][j][k]
			}
		}
	}
	for i := range dst.CompRef {
		for j := range dst.CompRef[i] {
			dst.CompRef[i][j] += src.CompRef[i][j]
		}
	}
	mergeTxCounts(&dst.Tx, &src.Tx)
	for i := range dst.Skip {
		for j := range dst.Skip[i] {
			dst.Skip[i][j] += src.Skip[i][j]
		}
	}
	mergeMvCounts(&dst.Mv, &src.Mv)
}

func mergeCoefCounts(dst, src *CoefCounts) {
	for tx := range dst.Coef {
		for plane := range dst.Coef[tx] {
			for ref := range dst.Coef[tx][plane] {
				for band := range dst.Coef[tx][plane][ref] {
					for ctx := range dst.Coef[tx][plane][ref][band] {
						for node := range dst.Coef[tx][plane][ref][band][ctx] {
							dst.Coef[tx][plane][ref][band][ctx][node] +=
								src.Coef[tx][plane][ref][band][ctx][node]
						}
					}
				}
			}
		}
	}
	for tx := range dst.EobBranch {
		for plane := range dst.EobBranch[tx] {
			for ref := range dst.EobBranch[tx][plane] {
				for band := range dst.EobBranch[tx][plane][ref] {
					for ctx := range dst.EobBranch[tx][plane][ref][band] {
						dst.EobBranch[tx][plane][ref][band][ctx] +=
							src.EobBranch[tx][plane][ref][band][ctx]
					}
				}
			}
		}
	}
}

func mergeTxCounts(dst, src *TxCounts) {
	for i := range dst.P32x32 {
		for j := range dst.P32x32[i] {
			dst.P32x32[i][j] += src.P32x32[i][j]
		}
	}
	for i := range dst.P16x16 {
		for j := range dst.P16x16[i] {
			dst.P16x16[i][j] += src.P16x16[i][j]
		}
	}
	for i := range dst.P8x8 {
		for j := range dst.P8x8[i] {
			dst.P8x8[i][j] += src.P8x8[i][j]
		}
	}
}

func mergeMvCounts(dst, src *NmvContextCounts) {
	for i := range dst.Joints {
		dst.Joints[i] += src.Joints[i]
	}
	for i := range dst.Comps {
		mergeMvComponentCounts(&dst.Comps[i], &src.Comps[i])
	}
}

func mergeMvComponentCounts(dst, src *NmvComponentCounts) {
	for i := range dst.Sign {
		dst.Sign[i] += src.Sign[i]
	}
	for i := range dst.Classes {
		dst.Classes[i] += src.Classes[i]
	}
	for i := range dst.Class0 {
		dst.Class0[i] += src.Class0[i]
	}
	for i := range dst.Bits {
		for j := range dst.Bits[i] {
			dst.Bits[i][j] += src.Bits[i][j]
		}
	}
	for i := range dst.Class0Fp {
		for j := range dst.Class0Fp[i] {
			dst.Class0Fp[i][j] += src.Class0Fp[i][j]
		}
	}
	for i := range dst.Fp {
		dst.Fp[i] += src.Fp[i]
	}
	for i := range dst.Class0Hp {
		dst.Class0Hp[i] += src.Class0Hp[i]
	}
	for i := range dst.Hp {
		dst.Hp[i] += src.Hp[i]
	}
}
