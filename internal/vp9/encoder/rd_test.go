package encoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestConditionalSkipIntraDirectionalPruning(t *testing.T) {
	if !ConditionalSkipIntra(common.D117Pred, common.D45Pred) {
		t.Fatal("D117 should be skipped after an unrelated best mode")
	}
	if ConditionalSkipIntra(common.D117Pred, common.VPred) {
		t.Fatal("D117 should remain after V_PRED")
	}
	if ConditionalSkipIntra(common.DcPred, common.VPred) {
		t.Fatal("non-directional modes should not be pruned")
	}
}

func TestRestorePlaneRectCopiesPackedRows(t *testing.T) {
	data := []byte{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
	}
	saved := []byte{
		50, 51,
		52, 53,
	}
	RestorePlaneRect(data, 4, 1, 1, 2, 2, saved)
	want := []byte{
		1, 2, 3, 4,
		5, 50, 51, 8,
		9, 52, 53, 12,
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("RestorePlaneRect = %v, want %v", data, want)
	}
}

func TestTransformBlockMetricsShiftForSub32x32(t *testing.T) {
	coeffs := []int16{8, -4, 3}
	dqcoeffs := []int16{2, -6, -1}
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx4x4); got != 14 {
		t.Fatalf("TransformBlockError 4x4 = %d, want 14", got)
	}
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32); got != 56 {
		t.Fatalf("TransformBlockError 32x32 = %d, want 56", got)
	}
	if got := TransformBlockErrorShifted(coeffs, dqcoeffs); got != 14 {
		t.Fatalf("TransformBlockErrorShifted = %d, want 14", got)
	}
	if got := TransformBlockEnergy(coeffs, common.Tx8x8); got != 22 {
		t.Fatalf("TransformBlockEnergy 8x8 = %d, want 22", got)
	}
	if got := ResidualSSE([]int16{3, -4, 12}); got != 169 {
		t.Fatalf("ResidualSSE = %d, want 169", got)
	}
}

func TestTransformBlockMetricsExactInt16Extremes(t *testing.T) {
	coeffs := make([]int16, 64)
	dqcoeffs := make([]int16, 64)
	residue := make([]int16, 64)
	for i := range coeffs {
		if i&1 == 0 {
			coeffs[i] = -32768
			dqcoeffs[i] = 32767
			residue[i] = -32768
		} else {
			coeffs[i] = 32767
			dqcoeffs[i] = -32768
			residue[i] = 32767
		}
	}

	wantErr := transformBlockErrorScalar(coeffs, dqcoeffs, len(coeffs))
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32); got != wantErr {
		t.Fatalf("TransformBlockError extremes = %d, want %d", got, wantErr)
	}
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx8x8); got != wantErr>>2 {
		t.Fatalf("TransformBlockError shifted extremes = %d, want %d", got, wantErr>>2)
	}

	wantEnergy := transformBlockEnergyScalar(coeffs)
	if got := TransformBlockEnergy(coeffs, common.Tx32x32); got != wantEnergy {
		t.Fatalf("TransformBlockEnergy extremes = %d, want %d", got, wantEnergy)
	}
	if got := TransformBlockEnergy(coeffs, common.Tx4x4); got != wantEnergy>>2 {
		t.Fatalf("TransformBlockEnergy shifted extremes = %d, want %d", got, wantEnergy>>2)
	}

	wantSSE := transformBlockEnergyScalar(residue)
	if got := ResidualSSE(residue); got != wantSSE {
		t.Fatalf("ResidualSSE extremes = %d, want %d", got, wantSSE)
	}
}

func TestTransformBlockMetricsOddLengthsUseScalarSemantics(t *testing.T) {
	coeffs := []int16{9, -17, 23, -31, 47, -55, 63, -71, 89}
	dqcoeffs := []int16{-3, 5, -7, 11, -13, 17, -19, 23, -29, 31}
	wantErr := transformBlockErrorScalar(coeffs, dqcoeffs, len(coeffs))
	if got := TransformBlockError(coeffs, dqcoeffs, common.Tx32x32); got != wantErr {
		t.Fatalf("TransformBlockError odd length = %d, want %d", got, wantErr)
	}
	wantEnergy := transformBlockEnergyScalar(coeffs)
	if got := TransformBlockEnergy(coeffs, common.Tx32x32); got != wantEnergy {
		t.Fatalf("TransformBlockEnergy odd length = %d, want %d", got, wantEnergy)
	}
}

var rdBlockMetricSink uint64

func BenchmarkTransformBlockMetrics(b *testing.B) {
	for _, n := range []int{16, 64, 256, 1024} {
		coeffs := make([]int16, n)
		dqcoeffs := make([]int16, n)
		for i := range coeffs {
			coeffs[i] = int16((i*37)%8192 - 4096)
			dqcoeffs[i] = int16((i*19)%8192 - 4096)
		}

		b.Run(fmt.Sprintf("error_n%d", n), func(b *testing.B) {
			var sum uint64
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sum += TransformBlockError(coeffs, dqcoeffs, common.Tx32x32)
			}
			rdBlockMetricSink = sum
		})
		b.Run(fmt.Sprintf("energy_n%d", n), func(b *testing.B) {
			var sum uint64
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sum += TransformBlockEnergy(coeffs, common.Tx32x32)
			}
			rdBlockMetricSink = sum
		})
	}
}

// libvpxRDCostOracle reproduces the libvpx RDCOST macro byte-for-byte using
// only the public formula from vp9_rd.h:29-30 so the test is a genuine
// cross-check rather than a self-reference.
//
//	RDCOST(RM, DM, R, D) =
//	    ROUND_POWER_OF_TWO((int64_t)R * RM, VP9_PROB_COST_SHIFT) +
//	    (D << DM)
//
// ROUND_POWER_OF_TWO is ((v + (1 << (n-1))) >> n) (libvpx:
// vpx_ports/mem.h:37). VP9_PROB_COST_SHIFT is 9 (libvpx:
// vp9_cost.h:24). RDDIV_BITS is 7 (libvpx: vp9_rd.h:26).
func libvpxRDCostOracle(rdmult, rddiv, rate int, distortion uint64) uint64 {
	const probCostShift = 9
	rateCost := (int64(rate)*int64(rdmult) + (1 << (probCostShift - 1))) >>
		probCostShift
	return uint64(rateCost) + (distortion << uint(rddiv))
}

// libvpxComputeRDMultBasedOnQindexOracle reproduces the libvpx
// vp9_compute_rd_mult_based_on_qindex formula from vp9_rd.c:241-275 for
// bit-depth 8 so we can validate ComputeRDMultBasedOnQindex independently.
func libvpxComputeRDMultBasedOnQindexOracle(qindex int, ft RDFrameType) int {
	if qindex < 0 {
		qindex = 0
	}
	if qindex > vp9dec.MaxQ {
		qindex = vp9dec.MaxQ
	}
	q := int(vp9dec.VpxDcQuant(qindex, 0, vp9dec.BitDepth8))
	rdmult := q * q
	var coeff float64
	switch ft {
	case RDFrameKey:
		coeff = 4.35 + 0.001*float64(qindex)
	case RDFrameArfGolden:
		coeff = 4.25 + 0.001*float64(qindex)
	default:
		coeff = 4.15 + 0.001*float64(qindex)
	}
	rdmult = int(float64(rdmult) * coeff)
	if rdmult <= 0 {
		return 1
	}
	return rdmult
}

func TestRDCostMatchesLibvpxFormula(t *testing.T) {
	if VP9ProbCostShift != 9 {
		t.Fatalf("VP9ProbCostShift=%d, libvpx wants 9", VP9ProbCostShift)
	}
	if RDDivBits != 7 {
		t.Fatalf("RDDivBits=%d, libvpx wants 7", RDDivBits)
	}
	type tc struct {
		qindex     int
		ft         RDFrameType
		rate       int
		distortion uint64
	}
	cases := []tc{
		// Low-rate cases: RD bias dominated by distortion.
		{qindex: 0, ft: RDFrameKey, rate: 0, distortion: 0},
		{qindex: 0, ft: RDFrameInter, rate: 64, distortion: 0},
		{qindex: 8, ft: RDFrameInter, rate: 64, distortion: 16},
		// Mid-range cases: comparable rate and distortion terms.
		{qindex: 32, ft: RDFrameKey, rate: 4096, distortion: 1024},
		{qindex: 64, ft: RDFrameInter, rate: 4096, distortion: 1024},
		{qindex: 64, ft: RDFrameArfGolden, rate: 4096, distortion: 1024},
		// High-rate cases: rate term outpaces distortion.
		{qindex: 128, ft: RDFrameInter, rate: 32768, distortion: 4096},
		{qindex: 196, ft: RDFrameArfGolden, rate: 65536, distortion: 8192},
		{qindex: 255, ft: RDFrameKey, rate: 131072, distortion: 16384},
		// Edge cases: pure distortion and pure rate.
		{qindex: 100, ft: RDFrameInter, rate: 0, distortion: 1 << 24},
		{qindex: 100, ft: RDFrameInter, rate: 1 << 30, distortion: 0},
	}
	for _, c := range cases {
		rdmult := ComputeRDMultBasedOnQindex(c.qindex, c.ft)
		wantRdmult := libvpxComputeRDMultBasedOnQindexOracle(c.qindex, c.ft)
		if rdmult != wantRdmult {
			t.Fatalf("qindex=%d ft=%d: rdmult=%d want=%d",
				c.qindex, c.ft, rdmult, wantRdmult)
		}
		got := RDCost(rdmult, RDDivBits, c.rate, c.distortion)
		want := libvpxRDCostOracle(rdmult, RDDivBits, c.rate, c.distortion)
		if got != want {
			t.Fatalf("qindex=%d ft=%d rate=%d dist=%d: RDCost=%d want=%d",
				c.qindex, c.ft, c.rate, c.distortion, got, want)
		}
		rateOnly := RDCostFromRate(rdmult, c.rate)
		distOnly := RDCostFromDistortion(RDDivBits, c.distortion)
		if rateOnly+distOnly != want {
			t.Fatalf("qindex=%d ft=%d: split helpers sum=%d want=%d",
				c.qindex, c.ft, rateOnly+distOnly, want)
		}
	}
}

func TestRDFrameTypeBranchMatchesLibvpx(t *testing.T) {
	cases := []struct {
		isKey, isAltRef, refreshGold, refreshAlt bool
		want                                     RDFrameType
	}{
		{isKey: true, refreshGold: true, want: RDFrameKey},
		{want: RDFrameInter},
		{refreshGold: true, want: RDFrameArfGolden},
		{refreshAlt: true, want: RDFrameArfGolden},
		{isAltRef: true, refreshGold: true, want: RDFrameInter},
		{isAltRef: true, refreshAlt: true, want: RDFrameInter},
	}
	for _, c := range cases {
		got := RDFrameTypeFor(c.isKey, c.isAltRef, c.refreshGold, c.refreshAlt)
		if got != c.want {
			t.Fatalf("isKey=%v alt=%v gold=%v arf=%v: got %d want %d",
				c.isKey, c.isAltRef, c.refreshGold, c.refreshAlt,
				got, c.want)
		}
	}
}

func TestComputeRDMultMatchesKeyframeHelper(t *testing.T) {
	for qindex := 0; qindex <= vp9dec.MaxQ; qindex++ {
		legacy := KeyframeRDMul(qindex)
		ported := ComputeRDMult(qindex, RDFrameKey)
		if absDiffInt(legacy, ported) > 1 {
			t.Fatalf("qindex=%d: legacy=%d ported=%d diff>1",
				qindex, legacy, ported)
		}
	}
}

func absDiffInt(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
