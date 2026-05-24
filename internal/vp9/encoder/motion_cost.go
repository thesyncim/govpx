package encoder

import (
	"math"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
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
