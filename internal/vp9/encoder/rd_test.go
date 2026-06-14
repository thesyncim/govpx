package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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

func TestModulateRDMultMatchesLibvpxTables(t *testing.T) {
	base := 1000
	cases := []struct {
		name string
		mod  RDMultModulation
		want int
	}{
		{name: "one pass identity", want: base},
		{
			name: "keyframe identity",
			mod:  RDMultModulation{TwoPass: true, IsKeyFrame: true, UpdateType: LFUpdate, GFUBoost: 0},
			want: base,
		},
		{
			name: "lf boost zero",
			mod:  RDMultModulation{TwoPass: true, UpdateType: LFUpdate, GFUBoost: 0},
			want: 1687,
		},
		{
			name: "arf boost clamp",
			mod:  RDMultModulation{TwoPass: true, UpdateType: ARFUpdate, GFUBoost: 2000},
			want: 1000,
		},
		{
			name: "overlay mid boost",
			mod:  RDMultModulation{TwoPass: true, UpdateType: OverlayUpdate, GFUBoost: 800},
			want: 1195,
		},
		{
			name: "use buffer dummy",
			mod:  RDMultModulation{TwoPass: true, UpdateType: UseBufFrame, GFUBoost: 0},
			want: 0,
		},
	}
	for _, c := range cases {
		if got := ModulateRDMult(base, c.mod); got != c.want {
			t.Fatalf("%s: ModulateRDMult=%d want %d", c.name, got, c.want)
		}
	}
}

func absDiffInt(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
