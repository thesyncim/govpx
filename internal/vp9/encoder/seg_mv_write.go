package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 per-block segment + MV writers. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c (write_segment_id) and
// vp9/encoder/vp9_encodemv.c (encode_mv_component / vp9_encode_mv).

// WriteSegmentId emits the 3-bit segment-id literal via the
// canonical SegmentTree against the per-frame TreeProbs. Mirrors
// libvpx's write_segment_id — only emits when (enabled,
// update_map) both hold; otherwise no-op.
func WriteSegmentId(bw *bitstream.Writer, seg *vp9dec.SegmentationParams, segID int) {
	if !seg.Enabled || !seg.UpdateMap {
		return
	}
	writeTreeBits(bw, common.SegmentTree[:], seg.TreeProbs[:], segID, 3)
}

// WriteMv mirrors libvpx's vp9_encode_mv — emits the joint plus
// per-axis component deltas needed to reconstruct (mv - ref) at the
// decoder. `allowHp` mirrors the frame-level allow-high-precision
// gate; libvpx also gates on use_mv_hp(ref).
func WriteMv(bw *bitstream.Writer, mv, ref vp9dec.MV, ctx *vp9dec.NmvContext, allowHp bool) {
	dRow := int(mv.Row - ref.Row)
	dCol := int(mv.Col - ref.Col)
	useHp := allowHp && useMvHpRef(ref)

	joint := mvJoint(dRow, dCol)
	jenc := mvJointEncoding(joint)
	writeTreeBits(bw, tables.MvJointTree[:], ctx.Joints[:], jenc.value, jenc.length)

	if mvJointVertical(joint) {
		WriteMvComponent(bw, dRow, &ctx.Comps[0], useHp)
	}
	if mvJointHorizontal(joint) {
		WriteMvComponent(bw, dCol, &ctx.Comps[1], useHp)
	}
}

// MvCost returns the entropy cost of WriteMv in VP9 cost units
// (bits << VP9ProbCostShift) without touching a bitstream. The mode
// picker uses this to compare NEWMV against predictor modes using the
// same tree walks as the writer.
func MvCost(mv, ref vp9dec.MV, ctx *vp9dec.NmvContext, allowHp bool) int {
	dRow := int(mv.Row - ref.Row)
	dCol := int(mv.Col - ref.Col)
	useHp := allowHp && useMvHpRef(ref)

	joint := mvJoint(dRow, dCol)
	jenc := mvJointEncoding(joint)
	cost := TreedCost(tables.MvJointTree[:], ctx.Joints[:], jenc.value, jenc.length)
	if mvJointVertical(joint) {
		cost += MvComponentCost(dRow, &ctx.Comps[0], useHp)
	}
	if mvJointHorizontal(joint) {
		cost += MvComponentCost(dCol, &ctx.Comps[1], useHp)
	}
	return cost
}

// WriteMvComponent mirrors libvpx's encode_mv_component. Emits a
// single axis of an MV delta: sign + class + integer + fractional
// + optional eighth-pel bit.
func WriteMvComponent(bw *bitstream.Writer, comp int, c *vp9dec.NmvComponent, usehp bool) {
	if comp == 0 {
		// libvpx asserts comp != 0; callers shouldn't hit this path.
		return
	}
	sign := uint32(0)
	mag := comp
	if comp < 0 {
		sign = 1
		mag = -comp
	}
	mvClass, _ := classifyMvForEnc(mag)
	offset := mag - 1 - mvClassBase(mvClass)
	d := offset >> 3
	fr := (offset >> 1) & 3
	hp := offset & 1

	bw.Write(sign, uint32(c.Sign))

	// Class via MvClassTree walk.
	writeTreeBits(bw, tables.MvClassTree[:], c.Classes[:],
		mvClassEncoding(mvClass).value, mvClassEncoding(mvClass).length)

	if mvClass == tables.MvClass0 {
		bw.Write(uint32(d), uint32(c.Class0[0]))
	} else {
		n := mvClass + 1 - 1 // CLASS0_BITS = 1 → n = mvClass + 0 = mvClass
		for i := range n {
			bw.Write(uint32((d>>uint(i))&1), uint32(c.Bits[i]))
		}
	}

	// Fractional via MvFpTree walk.
	var fpProbs []uint8
	if mvClass == tables.MvClass0 {
		fpProbs = c.Class0Fp[d][:]
	} else {
		fpProbs = c.Fp[:]
	}
	writeTreeBits(bw, tables.MvFpTree[:], fpProbs,
		mvFpEncoding(fr).value, mvFpEncoding(fr).length)

	if usehp {
		var hpProb uint8
		if mvClass == tables.MvClass0 {
			hpProb = c.Class0Hp
		} else {
			hpProb = c.Hp
		}
		bw.Write(uint32(hp), uint32(hpProb))
	}
}

// MvComponentCost returns the entropy cost of WriteMvComponent in VP9
// cost units. comp must be non-zero, matching WriteMvComponent's
// caller contract.
func MvComponentCost(comp int, c *vp9dec.NmvComponent, usehp bool) int {
	if comp == 0 {
		return 0
	}
	sign := 0
	mag := comp
	if comp < 0 {
		sign = 1
		mag = -comp
	}
	mvClass, _ := classifyMvForEnc(mag)
	offset := mag - 1 - mvClassBase(mvClass)
	d := offset >> 3
	fr := (offset >> 1) & 3
	hp := offset & 1

	cost := VP9CostBit(c.Sign, sign)
	cenc := mvClassEncoding(mvClass)
	cost += TreedCost(tables.MvClassTree[:], c.Classes[:], cenc.value, cenc.length)

	if mvClass == tables.MvClass0 {
		cost += VP9CostBit(c.Class0[0], d)
		fpEnc := mvFpEncoding(fr)
		cost += TreedCost(tables.MvFpTree[:], c.Class0Fp[d][:], fpEnc.value, fpEnc.length)
		if usehp {
			cost += VP9CostBit(c.Class0Hp, hp)
		}
		return cost
	}

	n := mvClass + 1 - 1
	for i := range n {
		cost += VP9CostBit(c.Bits[i], (d>>uint(i))&1)
	}
	fpEnc := mvFpEncoding(fr)
	cost += TreedCost(tables.MvFpTree[:], c.Fp[:], fpEnc.value, fpEnc.length)
	if usehp {
		cost += VP9CostBit(c.Hp, hp)
	}
	return cost
}

// classifyMvForEnc mirrors decoder's classifyMv. Returns the
// magnitude class for a 1-indexed positive value. Boundaries are
// (CLASS0_SIZE * {8, 16, 32, ..., 4096}) with CLASS0_SIZE=2.
func classifyMvForEnc(value int) (mvClass, _ int) {
	const class0Size = 2
	z := value - 1
	switch {
	case z < class0Size*8: // 16
		return tables.MvClass0, 0
	case z < class0Size*16: // 32
		return tables.MvClass1, 0
	case z < class0Size*32: // 64
		return 2, 0
	case z < class0Size*64: // 128
		return 3, 0
	case z < class0Size*128: // 256
		return 4, 0
	case z < class0Size*256: // 512
		return 5, 0
	case z < class0Size*512: // 1024
		return 6, 0
	case z < class0Size*1024: // 2048
		return 7, 0
	case z < class0Size*2048: // 4096
		return 8, 0
	case z < class0Size*4096: // 8192
		return 9, 0
	default:
		return tables.MvClass10, 0
	}
}

// mvClassBase mirrors libvpx's mv_class_base. CLASS_0 starts at 0;
// CLASS_k for k>=1 starts at CLASS0_SIZE << (k+2) = 2 << (k+2).
func mvClassBase(mvClass int) int {
	if mvClass == 0 {
		return 0
	}
	return 2 << uint(mvClass+2)
}

// mvJoint returns the joint type from a (drow, dcol) pair. Note
// libvpx's "Hnz/Hz" / "Vnz/Vz" naming refers to *horizontal* and
// *vertical* presence (Vnz = vertical present = non-zero row;
// Hnz = horizontal present = non-zero col).
func mvJoint(dRow, dCol int) int {
	switch {
	case dRow != 0 && dCol != 0:
		return tables.MvJointHnzVnz
	case dCol != 0:
		return tables.MvJointHnzVz // horizontal-only-nonzero
	case dRow != 0:
		return tables.MvJointHzVnz // vertical-only-nonzero
	default:
		return tables.MvJointZero
	}
}

func mvJointVertical(j int) bool   { return j == tables.MvJointHzVnz || j == tables.MvJointHnzVnz }
func mvJointHorizontal(j int) bool { return j == tables.MvJointHnzVz || j == tables.MvJointHnzVnz }

// useMvHpRef mirrors decoder.useMvHp: high-precision MVs require
// |ref.{row,col}| < COMPANDED_MVREF_THRESH (=8).
func useMvHpRef(ref vp9dec.MV) bool {
	abs := func(v int16) int16 {
		if v < 0 {
			return -v
		}
		return v
	}
	return abs(ref.Row) < 8 && abs(ref.Col) < 8
}

// mvJointEncoding returns the (value, length) bit-pattern walking
// MvJointTree to the matching leaf. Tree = {-0, 2, -1, 4, -2, -3}.
func mvJointEncoding(joint int) valLen {
	switch joint {
	case tables.MvJointZero:
		return valLen{0, 1} // "0"
	case tables.MvJointHnzVz:
		return valLen{2, 2} // "10"
	case tables.MvJointHzVnz:
		return valLen{6, 3} // "110"
	default:
		return valLen{7, 3} // "111" (HnzVnz)
	}
}

// mvClassEncoding returns the (value, length) bit-pattern that
// walks MvClassTree to the matching class leaf. Computed from the
// tree shape `{-0, 2, -1, 4, 6, 8, -2, -3, -4, -5, 10, 12, -6, -7, -8, -9, 14, 16, 18, -10}`.
type valLen struct {
	value, length int
}

func mvClassEncoding(c int) valLen {
	// Hand-derived from libvpx's MvClassTree walk. The tree is
	// right-skewed below MV_CLASS_6, so classes 7..10 land at
	// depth 7 while classes 4..6 are at depth 5.
	switch c {
	case 0:
		return valLen{0, 1} // "0"
	case 1:
		return valLen{2, 2} // "10"
	case 2:
		return valLen{12, 4} // "1100"
	case 3:
		return valLen{13, 4} // "1101"
	case 4:
		return valLen{28, 5} // "11100"
	case 5:
		return valLen{29, 5} // "11101"
	case 6:
		return valLen{30, 5} // "11110"
	case 7:
		return valLen{124, 7} // "1111100"
	case 8:
		return valLen{125, 7} // "1111101"
	case 9:
		return valLen{126, 7} // "1111110"
	default:
		return valLen{127, 7} // "1111111" (MV_CLASS_10)
	}
}

// mvFpEncoding mirrors the MvFpTree walk.
//
//	Tree: {-0, 2, -1, 4, -2, -3}.
//
// Leaves: 0 → 0/1, 1 → 1 0/2, 2 → 1 1 0/3, 3 → 1 1 1/3.
func mvFpEncoding(fr int) valLen {
	switch fr {
	case 0:
		return valLen{0, 1}
	case 1:
		return valLen{2, 2}
	case 2:
		return valLen{6, 3}
	default:
		return valLen{7, 3}
	}
}
