package decoder

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

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
		UpdateMvProbs(r, asSlice(&cc.Sign))
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
			UpdateMvProbs(r, asSlice(&cc.Class0Hp))
			UpdateMvProbs(r, asSlice(&cc.Hp))
		}
	}
}

// asSlice exposes a single uint8 as a 1-element slice so UpdateMvProbs
// — which works on []uint8 — can address individual scalar probability
// slots without an extra allocation. The slice aliases the caller's
// storage; nothing is copied.
func asSlice(p *uint8) []uint8 {
	return unsafe.Slice(p, 1)
}
