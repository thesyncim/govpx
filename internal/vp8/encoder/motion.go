package encoder

import (
	"math"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0:
// - vp8/encoder/encodemv.c motion-vector component packing and costing
// - vp8/encoder/mcomp.c vp8_mv_bit_cost

const (
	mvProbIsShort  = 0
	mvProbSign     = 1
	mvProbShort    = 2
	mvNumShort     = 8
	mvProbBits     = mvProbShort + mvNumShort - 1
	mvLongWidth    = 10
	mvComponentMax = 1023
	mvFullPixelMax = 255
)

type MotionVector struct {
	Row int16
	Col int16
}

var smallMVTokens = initSmallMVTokens()
var motionVectorSADCosts = initMotionVectorSADCosts()

type MotionVectorCostTables struct {
	Component [2][2*mvComponentMax + 1]int32
}

func WriteMotionVector(w *BoolWriter, probs *[2][tables.MVPCount]uint8, mv MotionVector) error {
	if w == nil || probs == nil || mv.Row&1 != 0 || mv.Col&1 != 0 {
		return ErrInvalidPacketConfig
	}
	if !writeMVComponent(w, probs[0][:], int(mv.Row/2)) {
		return ErrInvalidPacketConfig
	}
	if !writeMVComponent(w, probs[1][:], int(mv.Col/2)) {
		return ErrInvalidPacketConfig
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func MotionVectorCost(mv MotionVector) int {
	probs := tables.DefaultMVContext
	return MotionVectorBitCost(mv, MotionVector{}, &probs, 128)
}

func MotionVectorBitCost(mv MotionVector, ref MotionVector, probs *[2][tables.MVPCount]uint8, weight int) int {
	if probs == nil {
		return 1 << 30
	}
	row := clampMVMCompCostInput((int(mv.Row) - int(ref.Row)) >> 1)
	col := clampMVMCompCostInput((int(mv.Col) - int(ref.Col)) >> 1)
	cost := motionVectorComponentCost(row, probs[0][:]) + motionVectorComponentCost(col, probs[1][:])
	return (cost * weight) >> 7
}

func MotionVectorErrorCost(mv MotionVector, ref MotionVector, probs *[2][tables.MVPCount]uint8, errorPerBit int) int {
	if probs == nil {
		return 0
	}
	row := clampMVMCompCostInput((int(mv.Row) - int(ref.Row)) >> 1)
	col := clampMVMCompCostInput((int(mv.Col) - int(ref.Col)) >> 1)
	cost := motionVectorComponentCost(row, probs[0][:]) + motionVectorComponentCost(col, probs[1][:])
	return (cost*errorPerBit + 128) >> 8
}

// MotionVectorSubpelSearchCost mirrors libvpx's MVC macro inside
// vp8_find_best_sub_pixel_step_iteratively / vp8_find_best_sub_pixel_step in
// mcomp.c. CHECK_BETTER and the half/quarter-pel candidate cost calculation
// look up mvcost with a signed 1/4-pel index computed as
//
//	idx = r - (ref->row >> 1)
//
// where r is the candidate row in 1/4-pel and ref is in 1/8-pel. That index
// shape differs from mv_err_cost / vp8_mv_bit_cost (see MotionVectorErrorCost
// above): mv_err_cost computes (mv-ref)>>1 once and CLAMPS the result to
// [0, MVvals], so negative deltas all collapse to mvcost[0][0]. The MVC
// macro uses the bidirectional mvcost table (the pointer is offset by mv_max
// + 1 in onyx_if.c so negative indices are valid) and never clamps. The two
// formulas only agree when ref is an exact full-pel multiple of 1/8 (i.e.
// ref&1 == 0); otherwise they differ by one 1/4-pel index, which biases the
// subpel candidate ranking and shows up as block_mv match-rate deficits on
// the SPLITMV scoreboard whenever bestRefMV is fractional.
//
// mv is in 1/8-pel; ref is in 1/8-pel. The returned cost is the
// MVC-formatted RD-shaped error cost (i.e. ((mvcost[row]+mvcost[col])*error_per_bit+128)>>8).
func MotionVectorSubpelSearchCost(mv MotionVector, ref MotionVector, probs *[2][tables.MVPCount]uint8, errorPerBit int) int {
	if probs == nil {
		return 0
	}
	row := clampMVSignedComponent((int(mv.Row) >> 1) - (int(ref.Row) >> 1))
	col := clampMVSignedComponent((int(mv.Col) >> 1) - (int(ref.Col) >> 1))
	cost := motionVectorComponentCost(row, probs[0][:]) + motionVectorComponentCost(col, probs[1][:])
	return (cost*errorPerBit + 128) >> 8
}

func (t *MotionVectorCostTables) Build(probs *[2][tables.MVPCount]uint8) {
	if t == nil || probs == nil {
		return
	}
	for component := range 2 {
		table := &t.Component[component]
		prob := probs[component][:]
		for delta := -mvComponentMax; delta <= mvComponentMax; delta++ {
			table[delta+mvComponentMax] = int32(motionVectorComponentCost(delta, prob))
		}
	}
}

func (t *MotionVectorCostTables) SubpelSearchCostFromQuarterDeltas(mvRow4 int, mvCol4 int, refRow4 int, refCol4 int, errorPerBit int) int {
	if t == nil {
		return 0
	}
	row := clampMVSignedComponent(mvRow4 - refRow4)
	col := clampMVSignedComponent(mvCol4 - refCol4)
	cost := int(t.Component[0][row+mvComponentMax]) + int(t.Component[1][col+mvComponentMax])
	return (cost*errorPerBit + 128) >> 8
}

// MotionVectorSADCost mirrors libvpx mvsad_err_cost (mcomp.c). libvpx
// pre-shifts both operands to full-pel before subtracting:
//
//	mvp_full.row = bsi->mvp.row >> 3;  // diamond search ref
//	fcenter.row  = center_mv.row >> 3; // search-cost anchor
//	delta = mvp_full.row - fcenter.row
//
// so the index into mvsadcost is the difference of arithmetic-shifted
// full-pel values, NOT the arithmetic-shifted difference. The two only
// agree when ref is already a multiple of eighth-pel-per-pel (i.e. an
// exact full-pel value); whenever ref has a fractional eighth-pel part
// (which is the common case — bestRefMV is a neighbor's chosen MV, in
// eighth-pel units), `(mv-ref)>>3` is off by one from `(mv>>3)-(ref>>3)`
// for the unfavourable rounding direction. That off-by-one leaks into the
// SPLITMV per-label diamond search and biases it toward the wrong full-pel
// site whenever bestRefMV is fractional, which shows up as block_mv and
// partition deficits on the SPLITMV scoreboard.
func MotionVectorSADCost(mv MotionVector, ref MotionVector, sadPerBit int) int {
	row := clampMVFullPixelComponent((int(mv.Row) >> 3) - (int(ref.Row) >> 3))
	col := clampMVFullPixelComponent((int(mv.Col) >> 3) - (int(ref.Col) >> 3))
	cost := motionVectorSADComponentCost(row) + motionVectorSADComponentCost(col)
	return (cost*sadPerBit + 128) >> 8
}

// MotionVectorSADCostFromDeltas is the picker-hot-path variant of
// MotionVectorSADCost that takes the already-shifted-to-full-pel MV
// components as ints. Picker call sites have these values pre-extracted
// (`int(mv.Row) >> 3`, `int(bestRefMV.Row) >> 3`); calling through this
// entry skips the four int16/int conversions in the public wrapper, which
// also shrinks the inline cost enough for the function to inline cleanly
// inside the diamond/n-step inner loop.
//
// Behaviour is byte-identical to MotionVectorSADCost — both arithmetic
// expressions use the libvpx mv_sad_cost shape (full-pel-shifted diff,
// clamped, looked-up via the precomputed log2-cost table).
func MotionVectorSADCostFromDeltas(mvRow8 int, mvCol8 int, refRow8 int, refCol8 int, sadPerBit int) int {
	rowDelta := mvRow8 - refRow8
	if rowDelta > mvFullPixelMax {
		rowDelta = mvFullPixelMax
	} else if rowDelta < -mvFullPixelMax {
		rowDelta = -mvFullPixelMax
	}
	if rowDelta < 0 {
		rowDelta = -rowDelta
	}
	colDelta := mvCol8 - refCol8
	if colDelta > mvFullPixelMax {
		colDelta = mvFullPixelMax
	} else if colDelta < -mvFullPixelMax {
		colDelta = -mvFullPixelMax
	}
	if colDelta < 0 {
		colDelta = -colDelta
	}
	cost := motionVectorSADCosts[rowDelta] + motionVectorSADCosts[colDelta]
	return (cost*sadPerBit + 128) >> 8
}

func motionVectorComponentCost(component int, probs []uint8) int {
	if len(probs) < tables.MVPCount {
		return 1 << 30
	}
	negative := component < 0
	x := component
	if negative {
		x = -x
	}

	cost := 0
	if x < mvNumShort {
		cost += mvBoolCost(probs[mvProbIsShort], 0)
		cost += mvTreeTokenCost(tables.SmallMVTree[:], probs[mvProbShort:], smallMVTokens[x])
		if x == 0 {
			return cost
		}
	} else {
		cost += mvBoolCost(probs[mvProbIsShort], 1)
		for i := range 3 {
			cost += mvBoolCost(probs[mvProbBits+i], (x>>i)&1)
		}
		for i := mvLongWidth - 1; i > 3; i-- {
			cost += mvBoolCost(probs[mvProbBits+i], (x>>i)&1)
		}
		if x&0xfff0 != 0 {
			cost += mvBoolCost(probs[mvProbBits+3], (x>>3)&1)
		}
	}

	if negative {
		cost += mvBoolCost(probs[mvProbSign], 1)
	} else {
		cost += mvBoolCost(probs[mvProbSign], 0)
	}
	return cost
}

func clampMVMCompCostInput(component int) int {
	if component < 0 {
		return 0
	}
	if component > mvComponentMax {
		return mvComponentMax
	}
	return component
}

// clampMVSignedComponent clamps a signed 1/4-pel component delta to the
// libvpx mvcost table extents (-mv_max..mv_max), preserving sign so the
// MVC-style signed lookup remains valid.
func clampMVSignedComponent(component int) int {
	if component < -mvComponentMax {
		return -mvComponentMax
	}
	if component > mvComponentMax {
		return mvComponentMax
	}
	return component
}

func motionVectorSADComponentCost(component int) int {
	if component < 0 {
		component = -component
	}
	return motionVectorSADCosts[component]
}

func clampMVFullPixelComponent(component int) int {
	if component > mvFullPixelMax {
		return mvFullPixelMax
	}
	if component < -mvFullPixelMax {
		return -mvFullPixelMax
	}
	return component
}

func mvBoolCost(prob uint8, bit int) int {
	if bit == 0 {
		return tables.ProbCost[prob]
	}
	return tables.ProbCost[255-int(prob)]
}

func mvTreeTokenCost(tree []int16, probs []uint8, token TreeToken) int {
	node := int16(0)
	cost := 0
	value := token.Value
	probsLen := len(probs)
	treeLen := len(tree)
	for bitIndex := int(token.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		nodeIdx := int(node)
		if probIndex < 0 || probIndex >= probsLen || nodeIdx+1 >= treeLen {
			return 1 << 30
		}
		bit := int((value >> uint(bitIndex)) & 1)
		cost += mvBoolCost(probs[probIndex], bit)
		next := tree[nodeIdx+bit]
		if next <= 0 {
			if bitIndex == 0 {
				return cost
			}
			return 1 << 30
		}
		node = next
	}
	return 1 << 30
}

func writeMVComponent(w *BoolWriter, probs []uint8, component int) bool {
	negative := component < 0
	if negative {
		component = -component
	}
	if component >= 8 {
		return writeLargeMVComponent(w, probs, component, negative)
	}
	if len(probs) < tables.MVPCount {
		return false
	}
	w.WriteBool(0, probs[mvProbIsShort])
	if !WriteTreeToken(w, tables.SmallMVTree[:], probs[mvProbShort:], smallMVTokens[component]) {
		return false
	}
	if component != 0 {
		writeBoolProb(w, negative, probs[mvProbSign])
	}
	return w.Err() == nil
}

func writeLargeMVComponent(w *BoolWriter, probs []uint8, component int, negative bool) bool {
	if len(probs) < tables.MVPCount || component < 8 || component > 0x7ff {
		return false
	}
	w.WriteBool(1, probs[mvProbIsShort])

	coded := component
	if component < 16 {
		coded = component - 8
	}
	for i := range 3 {
		w.WriteBool(uint8((coded>>i)&1), probs[mvProbBits+i])
	}
	for i := mvLongWidth - 1; i > 3; i-- {
		w.WriteBool(uint8((coded>>i)&1), probs[mvProbBits+i])
	}
	if coded&0xfff0 != 0 {
		w.WriteBool(uint8((component>>3)&1), probs[mvProbBits+3])
	}
	if component != 0 {
		writeBoolProb(w, negative, probs[mvProbSign])
	}
	return w.Err() == nil
}

func writeBoolProb(w *BoolWriter, value bool, prob uint8) {
	if value {
		w.WriteBool(1, prob)
		return
	}
	w.WriteBool(0, prob)
}

func initSmallMVTokens() [8]TreeToken {
	var out [8]TreeToken
	for i := range out {
		BuildTreeToken(tables.SmallMVTree[:], i, &out[i])
	}
	return out
}

func initMotionVectorSADCosts() [mvFullPixelMax + 1]int {
	var out [mvFullPixelMax + 1]int
	out[0] = 300
	for i := 1; i <= mvFullPixelMax; i++ {
		z := 256 * (2 * (math.Log2(float64(8*i)) + 0.6))
		out[i] = int(z)
	}
	return out
}
