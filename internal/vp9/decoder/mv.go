package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// VP9 per-block motion-vector decode. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodemv.c — read_mv_component / read_mv. Together
// with [[ReadIntraMode]] / [[ReadInterMode]] in mode.go these form the
// leaf functions [[read_mb_modes_mv]] composes against per block.

// MV is the int16 row/col pair libvpx carries through reconstruction.
// Matches the union MV layout in vpx_dsp/vpx_filter.h (row then col).
type MV struct {
	Row int16
	Col int16
}

// InvalidMV mirrors libvpx's INVALID_MV (0x80008000) read as an int_mv.
// Its as_int layout is {row, col} = {(int16)0x8000, (int16)0x8000}, i.e.
// {-32768, -32768}. libvpx parks intra-coded blocks' mv[0]/mv[1] at this
// sentinel (vp9/encoder/vp9_pickmode.c:2644-2645) so neighbour-MV consumers
// such as vp9_NEWMV_diff_bias can reject it via the
// `mv[0].as_int != INVALID_MV` check (vp9_pickmode.c:1327,1332).
var InvalidMV = MV{Row: int16(-0x8000), Col: int16(-0x8000)}

// ReadMvComponent mirrors libvpx's read_mv_component.
//
// Decodes one axis of an MV delta. The encoded value is sign-magnitude
// with:
//   - sign bit
//   - class (1..10 via mv_class_tree, plus class-0 fast path)
//   - integer-part magnitude
//   - fractional pel (mv_fp_tree)
//   - optional eighth-pel bit
//
// `usehp` gates the eighth-pel bit; when false the high-precision
// component defaults to 1, exactly as libvpx does.
func ReadMvComponent(r *bitstream.Reader, c *NmvComponent, usehp bool) int {
	sign := r.Read(uint32(c.Sign))
	mvClass := r.ReadTree(tables.MvClassTree[:], c.Classes[:])
	class0 := mvClass == tables.MvClass0

	var d, mag int
	if class0 {
		d = int(r.Read(uint32(c.Class0[0])))
		mag = 0
	} else {
		n := mvClass + Class0Bits - 1
		for i := range n {
			d |= int(r.Read(uint32(c.Bits[i]))) << uint(i)
		}
		mag = Class0Size << uint(mvClass+2)
	}

	var fr int
	if class0 {
		fr = r.ReadTree(tables.MvFpTree[:], c.Class0Fp[d][:])
	} else {
		fr = r.ReadTree(tables.MvFpTree[:], c.Fp[:])
	}

	hp := 1
	if usehp {
		if class0 {
			hp = int(r.Read(uint32(c.Class0Hp)))
		} else {
			hp = int(r.Read(uint32(c.Hp)))
		}
	}

	mag += ((d << 3) | (fr << 1) | hp) + 1
	if sign != 0 {
		return -mag
	}
	return mag
}

// ReadMv mirrors libvpx's read_mv. Decodes a joint, then up to two
// axis components, applies the delta against `ref`, and writes the
// result to `mv`. `allowHp` is the frame-level allow-high-precision
// flag; libvpx then ANDs it with use_mv_hp(ref) — we follow the same
// gate (a magnitude check on ref against COMPANDED_MVREF_THRESH).
func ReadMv(r *bitstream.Reader, mv, ref *MV, ctx *NmvContext, allowHp bool) {
	joint := r.ReadTree(tables.MvJointTree[:], ctx.Joints[:])
	useHp := allowHp && useMvHp(ref)

	var diff MV
	if mvJointVertical(joint) {
		diff.Row = int16(ReadMvComponent(r, &ctx.Comps[0], useHp))
	}
	if mvJointHorizontal(joint) {
		diff.Col = int16(ReadMvComponent(r, &ctx.Comps[1], useHp))
	}

	mv.Row = ref.Row + diff.Row
	mv.Col = ref.Col + diff.Col
}

// mvJointVertical: joint has a non-zero vertical component.
// Mirrors mv_joint_vertical (joint == MV_JOINT_HZVNZ || HNZVNZ).
func mvJointVertical(joint int) bool {
	return joint == tables.MvJointHzVnz || joint == tables.MvJointHnzVnz
}

// mvJointHorizontal: joint has a non-zero horizontal component.
// Mirrors mv_joint_horizontal (joint == MV_JOINT_HNZVZ || HNZVNZ).
func mvJointHorizontal(joint int) bool {
	return joint == tables.MvJointHnzVz || joint == tables.MvJointHnzVnz
}

// kMvRefThresh from vp9_entropymv.h. MV units are eighth-pel, so libvpx
// keeps the high-precision bit for references within eight pixels.
const compandedMvrefThresh = 64

// useMvHp mirrors use_mv_hp from vp9_entropymv.h: high-precision MVs
// are only enabled when the reference is close enough to the origin.
func useMvHp(ref *MV) bool {
	return absInt16(ref.Row) < compandedMvrefThresh &&
		absInt16(ref.Col) < compandedMvrefThresh
}

func absInt16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}
