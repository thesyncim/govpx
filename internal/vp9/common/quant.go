package common

import "github.com/thesyncim/govpx/internal/vp9/tables"

// Ported from libvpx v1.16.0 vp9/common/vp9_quant_common.{c,h}.
//
// DcQuant / AcQuant look up the dequant multiplier for a given qindex,
// optional per-segment delta, and bit depth. Public VP9 support is profile 0;
// the wider bit-depth table dispatch is retained to keep the libvpx-derived
// helper complete and to classify unsupported profile-family streams.

// BitDepth selects which of the (8,10,12)-bit dequant tables vp9_dc_quant
// and vp9_ac_quant should pull from. Matches vpx_bit_depth_t in libvpx.
type BitDepth uint8

const (
	Bits8  BitDepth = 8
	Bits10 BitDepth = 10
	Bits12 BitDepth = 12
)

// DcQuant returns the DC dequant scaler at qindex+delta, clamped to the
// valid [0, MaxQ] range. Matches vp9_dc_quant.
func DcQuant(qindex, delta int, bd BitDepth) int16 {
	idx := clampQindex(qindex + delta)
	switch bd {
	case Bits10:
		return tables.DcQLookup10[idx]
	case Bits12:
		return tables.DcQLookup12[idx]
	default:
		return tables.DcQLookup8[idx]
	}
}

// AcQuant returns the AC dequant scaler at qindex+delta, clamped to the
// valid [0, MaxQ] range. Matches vp9_ac_quant.
func AcQuant(qindex, delta int, bd BitDepth) int16 {
	idx := clampQindex(qindex + delta)
	switch bd {
	case Bits10:
		return tables.AcQLookup10[idx]
	case Bits12:
		return tables.AcQLookup12[idx]
	default:
		return tables.AcQLookup8[idx]
	}
}

func clampQindex(q int) int {
	if q < tables.MinQ {
		return tables.MinQ
	}
	if q > tables.MaxQ {
		return tables.MaxQ
	}
	return q
}
