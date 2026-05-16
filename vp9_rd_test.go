package govpx

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// libvpxRDCostOracle reproduces the libvpx RDCOST macro byte-for-byte
// using only the public formula from vp9_rd.h:29-30 so the test is a
// genuine cross-check rather than a self-reference.
//
//	RDCOST(RM, DM, R, D) =
//	    ROUND_POWER_OF_TWO((int64_t)R * RM, VP9_PROB_COST_SHIFT) +
//	    (D << DM)
//
// ROUND_POWER_OF_TWO is ((v + (1 << (n-1))) >> n) (libvpx:
// vpx_ports/mem.h:37).  VP9_PROB_COST_SHIFT is 9 (libvpx:
// vp9_cost.h:24).  RDDIV_BITS is 7 (libvpx: vp9_rd.h:26).
func libvpxRDCostOracle(rdmult, rddiv, rate int, distortion uint64) uint64 {
	const probCostShift = 9
	rateCost := (int64(rate)*int64(rdmult) + (1 << (probCostShift - 1))) >>
		probCostShift
	return uint64(rateCost) + (distortion << uint(rddiv))
}

// libvpxComputeRDMultBasedOnQindexOracle reproduces the libvpx
// vp9_compute_rd_mult_based_on_qindex formula from vp9_rd.c:241-275 for
// bit-depth 8 so we can validate vp9ComputeRDMultBasedOnQindex against
// it independently.
func libvpxComputeRDMultBasedOnQindexOracle(qindex int, ft vp9RDFrameType) int {
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
	case vp9RDFrameKey:
		coeff = 4.35 + 0.001*float64(qindex)
	case vp9RDFrameArfGolden:
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

// TestVP9RDCostMatchesLibvpxFormula validates that vp9RDCost matches
// the libvpx RDCOST macro at the qindex / rate / distortion tuples a
// realistic SB-level mode-decision search would exercise.  Failure
// here means the inter mode picker is no longer scoring in the libvpx
// shape and the BD-rate gates will regress.
func TestVP9RDCostMatchesLibvpxFormula(t *testing.T) {
	if encoder.VP9ProbCostShift != 9 {
		t.Fatalf("encoder.VP9ProbCostShift=%d, libvpx wants 9 — port drift",
			encoder.VP9ProbCostShift)
	}
	if vp9RDDivBits != 7 {
		t.Fatalf("vp9RDDivBits=%d, libvpx wants 7 — port drift", vp9RDDivBits)
	}
	type tc struct {
		qindex     int
		ft         vp9RDFrameType
		rate       int
		distortion uint64
	}
	cases := []tc{
		// Low-rate cases — RD bias dominated by distortion.
		{qindex: 0, ft: vp9RDFrameKey, rate: 0, distortion: 0},
		{qindex: 0, ft: vp9RDFrameInter, rate: 64, distortion: 0},
		{qindex: 8, ft: vp9RDFrameInter, rate: 64, distortion: 16},
		// Mid-range cases — RDCOST has comparable rate / distortion terms.
		{qindex: 32, ft: vp9RDFrameKey, rate: 4096, distortion: 1024},
		{qindex: 64, ft: vp9RDFrameInter, rate: 4096, distortion: 1024},
		{qindex: 64, ft: vp9RDFrameArfGolden, rate: 4096, distortion: 1024},
		// High-rate cases — rate term outpaces distortion.
		{qindex: 128, ft: vp9RDFrameInter, rate: 32768, distortion: 4096},
		{qindex: 196, ft: vp9RDFrameArfGolden, rate: 65536, distortion: 8192},
		{qindex: 255, ft: vp9RDFrameKey, rate: 131072, distortion: 16384},
		// Edge — distortion-only.
		{qindex: 100, ft: vp9RDFrameInter, rate: 0, distortion: 1 << 24},
		// Edge — rate-only with huge rate.
		{qindex: 100, ft: vp9RDFrameInter, rate: 1 << 30, distortion: 0},
	}
	for _, c := range cases {
		rdmult := vp9ComputeRDMultBasedOnQindex(c.qindex, c.ft)
		wantRdmult := libvpxComputeRDMultBasedOnQindexOracle(c.qindex, c.ft)
		if rdmult != wantRdmult {
			t.Fatalf("qindex=%d ft=%d: rdmult=%d want=%d",
				c.qindex, c.ft, rdmult, wantRdmult)
		}
		got := vp9RDCost(rdmult, vp9RDDivBits, c.rate, c.distortion)
		want := libvpxRDCostOracle(rdmult, vp9RDDivBits, c.rate, c.distortion)
		if got != want {
			t.Fatalf("qindex=%d ft=%d rate=%d dist=%d: vp9RDCost=%d want=%d",
				c.qindex, c.ft, c.rate, c.distortion, got, want)
		}
		// Cross-check the split helpers (rate-only + distortion-only) sum
		// to the joint cost.  libvpx's RDCOST macro is exactly that sum.
		rateOnly := vp9RDCostFromRate(rdmult, c.rate)
		distOnly := vp9RDCostFromDistortion(vp9RDDivBits, c.distortion)
		if rateOnly+distOnly != want {
			t.Fatalf("qindex=%d ft=%d: split helpers sum=%d want=%d",
				c.qindex, c.ft, rateOnly+distOnly, want)
		}
	}
}

// TestVP9RDFrameTypeBranchMatchesLibvpx validates the frame-type
// selector against the libvpx branching in
// vp9_compute_rd_mult_based_on_qindex (vp9_rd.c:256-266).
func TestVP9RDFrameTypeBranchMatchesLibvpx(t *testing.T) {
	cases := []struct {
		isKey, isAltRef, refreshGold, refreshAlt bool
		want                                     vp9RDFrameType
	}{
		// KF wins outright — even with golden refresh active.
		{isKey: true, refreshGold: true, want: vp9RDFrameKey},
		// Plain inter — no GF / ARF refresh.
		{want: vp9RDFrameInter},
		// GF refresh alone -> ARF/GF bucket.
		{refreshGold: true, want: vp9RDFrameArfGolden},
		// ARF refresh alone -> ARF/GF bucket.
		{refreshAlt: true, want: vp9RDFrameArfGolden},
		// is_src_frame_alt_ref + refresh -> inter (libvpx skips the
		// ARF bucket when the source frame IS the alt-ref because the
		// rate cost has already been paid by the hidden encode).
		{isAltRef: true, refreshGold: true, want: vp9RDFrameInter},
		{isAltRef: true, refreshAlt: true, want: vp9RDFrameInter},
	}
	for _, c := range cases {
		got := vp9RDFrameTypeFor(c.isKey, c.isAltRef, c.refreshGold, c.refreshAlt)
		if got != c.want {
			t.Fatalf("isKey=%v alt=%v gold=%v arf=%v: got %d want %d",
				c.isKey, c.isAltRef, c.refreshGold, c.refreshAlt,
				got, c.want)
		}
	}
}

// TestVP9ComputeRDMultMatchesKeyframeHelper guards the rewrite of the
// legacy vp9KeyframeRDMul integer helper against the new float-form
// vp9ComputeRDMult(KF) so both helpers stay producing the same per-
// qindex rdmult.  Drift here would shift the keyframe-only mode picker
// scores away from the inter picker which now also routes through
// vp9ComputeRDMult.
func TestVP9ComputeRDMultMatchesKeyframeHelper(t *testing.T) {
	for qindex := 0; qindex <= vp9dec.MaxQ; qindex++ {
		legacy := vp9KeyframeRDMul(qindex)
		ported := vp9ComputeRDMult(qindex, vp9RDFrameKey)
		// Allow a 1-ulp slack because the legacy integer formula
		// q*q*(4350+qindex)/1000 truncates differently from the float
		// form int(q*q*(4.35+0.001*qindex)) at the rare points where
		// 4350 + qindex overflows 32 bits times q*q.  In practice the
		// rdmult is identical for the entire qindex range.
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

// TestVP9EncoderInitializeRDConstsPopulatesPerFrameState validates that
// vp9_initialize_rd_consts populates rc.rdmult / rc.rddiv and clears
// cbRdmult.  Asserting the populated values lets the inter mode picker
// rely on the per-frame state instead of synthesising rdmult on every
// candidate score, which is the load-bearing wiring step.
func TestVP9EncoderInitializeRDConstsPopulatesPerFrameState(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	e.cbRdmult = 12345 // simulate stale per-SB cache from prior frame.
	e.vp9EncoderInitializeRDConsts(64, vp9RDFrameInter)
	if e.rc.rddiv != vp9RDDivBits {
		t.Fatalf("rc.rddiv = %d, want %d", e.rc.rddiv, vp9RDDivBits)
	}
	want := vp9ComputeRDMult(64, vp9RDFrameInter)
	if e.rc.rdmult != want {
		t.Fatalf("rc.rdmult = %d, want %d", e.rc.rdmult, want)
	}
	if e.cbRdmult != 0 {
		t.Fatalf("cbRdmult = %d, want 0 after frame init", e.cbRdmult)
	}
}

// TestVP9EncoderRDMultLookupPrecedence validates that the per-block
// scorer reads cb_rdmult > rc.rdmult > qindex-derived fallback, in that
// order — mirroring libvpx's MACROBLOCK::rdmult precedence.
func TestVP9EncoderRDMultLookupPrecedence(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 32, Height: 32})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	// Fallback path.
	want := vp9ComputeRDMultBasedOnQindex(80, vp9RDFrameInter)
	if got := e.activeRDMult(80); got != want {
		t.Fatalf("fallback activeRDMult = %d, want %d", got, want)
	}
	// rc.rdmult overrides the fallback.
	e.rc.rdmult = 9876
	if got := e.activeRDMult(80); got != 9876 {
		t.Fatalf("rc.rdmult override = %d, want 9876", got)
	}
	// cbRdmult overrides rc.rdmult.
	e.cbRdmult = 4321
	if got := e.activeRDMult(80); got != 4321 {
		t.Fatalf("cbRdmult override = %d, want 4321", got)
	}
	// Zero cbRdmult falls back to rc.rdmult.
	e.cbRdmult = 0
	if got := e.activeRDMult(80); got != 9876 {
		t.Fatalf("zero cbRdmult fallback = %d, want 9876", got)
	}
}
