package encoder

import (
	"math"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const mvSADMax = (1 << (10 + 1 + 2)) - 1

var mvSADComponentCosts = func() [mvSADMax + 1]int {
	var costs [mvSADMax + 1]int
	for i := 1; i <= mvSADMax; i++ {
		// libvpx: vp9_encoder.c cal_nmvsadcosts uses
		//   (int)(256 * (2 * (log2f(8 * i) + .6))).
		logv := float64(float32(math.Log2(float64(8 * i))))
		costs[i] = int(256 * (2 * (logv + .6)))
	}
	return costs
}()

// MVSADComponentCost mirrors libvpx's cal_nmvsadcosts lookup for one MV axis.
func MVSADComponentCost(v int) int {
	if v < 0 {
		v = -v
	}
	if v > mvSADMax {
		v = mvSADMax
	}
	return mvSADComponentCosts[v]
}

func SADPerBit16(qindex int) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	q := ConvertQIndexToQ(qindex)
	return int(0.0418*q + 2.4107)
}

// SADPerBit4 mirrors libvpx's x->sadperbit4 (init_me_luts_bd,
// vp9/encoder/vp9_rd.c:171: bit4lut[i] = (int)(0.063 * q + 2.742)). It is the
// SAD-per-bit weight the sub-8x8 NEWMV full-pixel search uses (x->sadperbit4 at
// vp9_rdopt.c:2174), distinct from sadperbit16.
func SADPerBit4(qindex int) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	q := ConvertQIndexToQ(qindex)
	return int(0.063*q + 2.742)
}

func FullPelMVSADCost(mvRow, mvCol, refRow, refCol, sadPerBit int) int {
	row := mvRow - refRow
	col := mvCol - refCol
	jointCost := 300
	if row == 0 && col == 0 {
		jointCost = 600
	}
	cost := jointCost + MVSADComponentCost(row) + MVSADComponentCost(col)
	// libvpx: mvsad_err_cost rounds by VP9_PROB_COST_SHIFT (9).
	return (cost*sadPerBit + 256) >> 9
}

// UseMvHP mirrors libvpx's use_mv_hp reference-MV threshold.
func UseMvHP(ref vp9dec.MV) bool {
	const mvRefThresh = 64
	row := int(ref.Row)
	if row < 0 {
		row = -row
	}
	col := int(ref.Col)
	if col < 0 {
		col = -col
	}
	return row < mvRefThresh && col < mvRefThresh
}

// SubpelMVErrorCost scales the HP-aware MV entropy cost by error_per_bit.
func SubpelMVErrorCost(fc *vp9dec.FrameContext, mv, ref vp9dec.MV,
	allowHP bool, errorPerBit int,
) uint64 {
	if fc == nil || errorPerBit <= 0 {
		return 0
	}
	cost := MvCostWithHP(mv, ref, &fc.Nmvc, allowHP)
	return uint64((int64(cost)*int64(errorPerBit) + (1 << 13)) >> 14)
}

const (
	nmvCostTableMax  = (1 << (vp9dec.MvClasses + vp9dec.Class0Bits + 2)) - 1
	nmvCostTableVals = (nmvCostTableMax << 1) + 1
)

// NmvCostTable mirrors libvpx's vp9_build_nmv_cost_table output: a joint-cost
// row plus two signed component cost slabs addressed by mv_diff + MV_MAX.
type NmvCostTable struct {
	Joint     [vp9dec.MvJoints]int
	Component [2][nmvCostTableVals]int
}

// Build fills the cost table from the supplied NMV probabilities.
func (t *NmvCostTable) Build(ctx *vp9dec.NmvContext, useHP bool) bool {
	if t == nil || ctx == nil {
		return false
	}
	VP9CostTokens(t.Joint[:], ctx.Joints[:], tables.MvJointTree[:])
	for axis := range t.Component {
		buildNmvComponentCostTable(t.Component[axis][:], &ctx.Comps[axis], useHP)
	}
	return true
}

// MvCost returns the raw libvpx mv_cost table score for mv-ref.
func (t *NmvCostTable) MvCost(mv, ref vp9dec.MV) (int, bool) {
	if t == nil {
		return 0, false
	}
	dRow := int(mv.Row) - int(ref.Row)
	dCol := int(mv.Col) - int(ref.Col)
	if dRow < -nmvCostTableMax || dRow > nmvCostTableMax ||
		dCol < -nmvCostTableMax || dCol > nmvCostTableMax {
		return 0, false
	}
	joint := mvJoint(dRow, dCol)
	return t.Joint[joint] +
		t.Component[0][dRow+nmvCostTableMax] +
		t.Component[1][dCol+nmvCostTableMax], true
}

// SubpelMVErrorCost returns the scaled mv_err_cost score from a built table.
func (t *NmvCostTable) SubpelMVErrorCost(mv, ref vp9dec.MV,
	errorPerBit int,
) (uint64, bool) {
	if errorPerBit <= 0 {
		return 0, true
	}
	cost, ok := t.MvCost(mv, ref)
	if !ok {
		return 0, false
	}
	return uint64((int64(cost)*int64(errorPerBit) + (1 << 13)) >> 14), true
}

func buildNmvComponentCostTable(mvcost []int, c *vp9dec.NmvComponent, useHP bool) {
	if len(mvcost) < nmvCostTableVals || c == nil {
		return
	}
	offset := nmvCostTableMax
	signCost := [2]int{VP9CostBit(c.Sign, 0), VP9CostBit(c.Sign, 1)}
	var classCost [vp9dec.MvClasses]int
	VP9CostTokens(classCost[:], c.Classes[:], tables.MvClassTree[:])
	var class0Cost [vp9dec.Class0Size]int
	VP9CostTokens(class0Cost[:], c.Class0[:], tables.MvClass0Tree[:])
	var bitsCost [vp9dec.MvOffsetBits][2]int
	for i := range bitsCost {
		bitsCost[i][0] = VP9CostBit(c.Bits[i], 0)
		bitsCost[i][1] = VP9CostBit(c.Bits[i], 1)
	}
	var class0FPCost [vp9dec.Class0Size][vp9dec.MvFpSize]int
	for i := range class0FPCost {
		VP9CostTokens(class0FPCost[i][:], c.Class0Fp[i][:], tables.MvFpTree[:])
	}
	var fpCost [vp9dec.MvFpSize]int
	VP9CostTokens(fpCost[:], c.Fp[:], tables.MvFpTree[:])
	class0HPCost := [2]int{VP9CostBit(c.Class0Hp, 0), VP9CostBit(c.Class0Hp, 1)}
	hpCost := [2]int{VP9CostBit(c.Hp, 0), VP9CostBit(c.Hp, 1)}

	mvcost[offset] = 0
	for o := range vp9dec.Class0Size << 3 {
		d := o >> 3
		f := (o >> 1) & 3
		cost := classCost[tables.MvClass0] + class0Cost[d] + class0FPCost[d][f]
		if useHP {
			cost += class0HPCost[o&1]
		}
		v := o + 1
		mvcost[offset+v] = cost + signCost[0]
		mvcost[offset-v] = cost + signCost[1]
	}
	for cls := tables.MvClass1; cls < vp9dec.MvClasses; cls++ {
		for d := 0; d < (1 << uint(cls)); d++ {
			wholeCost := classCost[cls]
			n := cls + vp9dec.Class0Bits - 1
			for i := range n {
				wholeCost += bitsCost[i][(d>>uint(i))&1]
			}
			for f := range vp9dec.MvFpSize {
				cost := wholeCost + fpCost[f]
				v := (vp9dec.Class0Size << uint(cls+2)) + d*8 + f*2 + 1
				if useHP {
					mvcost[offset+v] = cost + hpCost[0] + signCost[0]
					mvcost[offset-v] = cost + hpCost[0] + signCost[1]
					if v+1 > nmvCostTableMax {
						break
					}
					mvcost[offset+v+1] = cost + hpCost[1] + signCost[0]
					mvcost[offset-v-1] = cost + hpCost[1] + signCost[1]
					continue
				}
				mvcost[offset+v] = cost + signCost[0]
				mvcost[offset-v] = cost + signCost[1]
				if v+1 > nmvCostTableMax {
					break
				}
				mvcost[offset+v+1] = cost + signCost[0]
				mvcost[offset-v-1] = cost + signCost[1]
			}
		}
	}
}
