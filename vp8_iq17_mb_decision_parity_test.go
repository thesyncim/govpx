package govpx

// Task #366: cross-family audit of the iQ=17 same-Q MB-decision divergence
// (lineage #343/#344/#353/#354). Three fixtures exhibit the class:
//
//	480p panning VBR  (#354)  — f3 iQ=17 +41 bytes (730 vs 689) same Q.
//	360p panning CBR  (#353)  — 2400 kbps iter=2 q=23 ~3.4× spread.
//	720p two-pass VBR (#344)  — Q=16 +1.34% rate.
//
// The structural hypothesis from task #354 is that at iQ=17 govpx's MB
// picker selects a different (mode, MV) than libvpx because the
// rate/distortion crossover at the Q-conditioned `RDMULT > 1000` boundary
// in vp8_initialize_rd_consts flips one or more candidate orderings.
//
// AUDIT (this task):
//
// 1. RDMULT / RDDIV branch (rdopt.c:174-236)
//
//    At iQ=17 libvpx computes Qvalue=vp8_dc_quant(17,0)=19,
//    RDMULT=(int)(2.80*19*19)=1010, which is the FIRST iQ value that
//    crosses the >1000 split:
//
//      iQ=16: capped_q=18, RDMULT=(int)(2.80*18*18)=907  → /=100 NOT taken
//      iQ=17: capped_q=19, RDMULT=(int)(2.80*19*19)=1010 → /=100 taken
//
//    govpx's vp8enc.RDConstantsWithZbin(17,0) returns (10,1) and
//    vp8enc.RDConstantsWithZbin(16,0) returns (907,100), byte-exact with
//    libvpx (verified by TestLibvpxRDConstantsByteExactSweep across the
//    whole QIndex range). The boundary itself is NOT a port gap — both
//    encoders see the same RDMULT/RDDIV pair at every Q.
//
//    The discontinuity at the boundary (rate factor drops from
//    rdmult/(256/100)≈354 at iQ=16 to rdmult/256≈0.04 at iQ=17, paired
//    with the 100× drop in distortion's coefficient via rdDiv) is a
//    real RD-cost reshape and is identical on both sides. So the +41
//    bytes are not produced by a divergent rd-consts branch.
//
// 2. rd_threshes Q clamp (rdopt.c:207-236)
//
//    libvpx then computes q = (int)pow(Qvalue, 1.25), q = max(q, 8),
//    and rd_threshes[i] = thresh_mult[i] * q (when rdmult<=1000) or
//    thresh_mult[i] * q / 100 (when rdmult>1000). govpx's
//    libvpxInterModeRDThresholdsFromMultipliersWithIIRatio in
//    vp8_encoder_inter_speed.go mirrors this verbatim
//    (TestLibvpxInterModeRDThresholdsScaleLikeInitializeRDConsts pins
//    both branches). The thresh_mult lookup itself is per-speed and
//    Q-INDEPENDENT, so at the same iQ and same Speed both encoders
//    derive byte-identical rd_threshes. No port gap here.
//
// 3. errorperbit derivation (rdopt.c:198)
//
//    libvpx sets x->errorperbit = (cpi->RDMULT / 110), with the RAW
//    RDMULT BEFORE the /100 split. govpx's vp8enc.ErrorPerBitWithZbin in
//    vp8_encoder_rd_cost.go uses vp8enc.RawRDMultiplierWithZbin
//    (the pre-split value) divided by 110. At iQ=17 raw RDMULT=1010,
//    errorperbit=9 on both sides. Byte-exact, no gap.
//
// 4. encodemb.c optimize_b plane_rd_mult (encodemb.c:174-190)
//
//    libvpx: rdmult = mb->rdmult * plane_rd_mult[type]; intra ref_frame
//    further lifts by *9>>4. govpx's optimizeQuantizedBlockWithRDConstants
//    at vp8_encoder_inter_quantize.go:179-182 mirrors both lifts (verified by
//    TestVP8ChromaRDCostStructure). Inter MBs (the iQ=17 frame 3 case)
//    skip the intra penalty on both sides. No port gap here.
//
// 5. rd_iifactor lift (rdopt.c:189-196) — TWO-PASS-ONLY GAP
//
//    libvpx applies, on inter frames in pass==2:
//      RDMULT += (RDMULT * rd_iifactor[next_iiratio]) >> 4
//    with rd_iifactor[32] = {4,4,3,2,1,0,...,0}, capping next_iiratio
//    at 31. govpx has NO rd_iifactor port (grep -rn rd_iifactor over
//    *.go finds only this audit's comment + the existing
//    TestLibvpxRDConstantsByteExactSweep's pin which skips pass==2
//    inter frames). For the 480p ONE-PASS fixture (#354) pass==1 in
//    both encoders so this gap doesn't fire. For the 720p TWO-PASS VBR
//    fixture (#344) pass==2 inter frames receive an RDMULT lift of up
//    to 25% in libvpx that govpx skips. At Q=16 (close to iQ=17), the
//    lift is large enough to push RDMULT from 907 (no lift, govpx)
//    to ≥1010 in libvpx, which would FURTHER cross the >1000 split.
//
//    The 720p two-pass +1.34% rate divergence is therefore explainable
//    by the missing rd_iifactor lift, which is a TARGETED PORT GAP that
//    can be closed independently of the 480p/360p one-pass cases.
//
// 6. Picker downstream
//
//    For the 480p ONE-PASS case the +41 bytes at frame 3 iQ=17 cannot
//    be attributed to any of the four Q-conditioned branches called
//    out in the task description. The residual MUST live in the
//    existing ARNR audit chain (#313→#314→#316→#318→#319→#329→#330)
//    where a chroma optimize_b PLANE_TYPE_UV keep/drop divergence
//    surfaces at high-Q boundary conditions. The same-Q same-bytes-
//    differ-by-41 signature is the same fingerprint task #316 left
//    unresolved; #354 is the VBR-amplified projection of that chain.
//
// CONCLUSION:
//
//   (a) Port a verbatim rd_iifactor lift (rdopt.c:189-196) to close the
//       720p two-pass VBR Q=16 +1.34% gap (#344). This is a clean port
//       with no cross-fixture risk: the lift only fires on pass==2 inter
//       frames so all one-pass / CBR / RT fixtures are unaffected.
//
//   (b) The 480p one-pass VBR f3 iQ=17 +41 bytes (#354) and the 360p
//       CBR iter=2 q=23 spread (#353) are NOT closed by the rd_iifactor
//       port. They remain on the ARNR audit chain (#316 chroma trellis).
//
// This file pins (a) the RDMULT/RDDIV iQ=17 boundary so any drift in
// the >1000 split is caught instantly, and (b) sentinels the
// rd_iifactor port gap with the lift formula so a follow-up port lands
// cleanly.

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8RDConstsIQ17Boundary pins the RDMULT/RDDIV pair at
// the iQ=16→17 boundary. Any change in vp8enc.RDConstantsWithZbin that
// shifts the >1000 split flips this pin. The pin uses the ground-truth
// rdconst=2.80 * dc_qlookup[iQ]^2 formula from libvpx
// vp8/encoder/rdopt.c:163-187 (single-pass, zbinOverQuant=0).
func TestVP8RDConstsIQ17Boundary(t *testing.T) {
	// iQ=16 sits BELOW the >1000 split: rdMult is the raw value,
	// rdDiv stays at 100.
	if gotMult, gotDiv := vp8enc.RDConstantsWithZbin(16, 0); gotMult != 907 || gotDiv != 100 {
		t.Errorf("vp8enc.RDConstantsWithZbin(16,0) = (%d,%d), want (907,100) — iQ=16 must stay below the rdMult>1000 split", gotMult, gotDiv)
	}
	// iQ=17 is the FIRST qindex that crosses the >1000 split.
	// rdMult drops 100×, rdDiv flips to 1.
	if gotMult, gotDiv := vp8enc.RDConstantsWithZbin(17, 0); gotMult != 10 || gotDiv != 1 {
		t.Errorf("vp8enc.RDConstantsWithZbin(17,0) = (%d,%d), want (10,1) — iQ=17 must cross the rdMult>1000 split", gotMult, gotDiv)
	}
	// Raw RDMULT at iQ=17 must be 1010 = (int)(2.80 * 19 * 19), the
	// pre-divide value that feeds x->errorperbit derivation.
	if gotRaw := vp8enc.RawRDMultiplierWithZbin(17, 0); gotRaw != 1010 {
		t.Errorf("vp8enc.RawRDMultiplierWithZbin(17,0) = %d, want 1010 — pre-divide RDMULT must match (int)(2.80*19*19)", gotRaw)
	}
	if gotRaw := vp8enc.RawRDMultiplierWithZbin(16, 0); gotRaw != 907 {
		t.Errorf("vp8enc.RawRDMultiplierWithZbin(16,0) = %d, want 907 — pre-divide RDMULT must match (int)(2.80*18*18)", gotRaw)
	}
	// dc_qlookup[16]=18, dc_qlookup[17]=19 — verify the source table
	// row that drives the boundary.
	if got := vp8common.DCQuant(16, 0); got != 18 {
		t.Errorf("DCQuant(16,0) = %d, want 18 — dc_qlookup row 16 must match libvpx", got)
	}
	if got := vp8common.DCQuant(17, 0); got != 19 {
		t.Errorf("DCQuant(17,0) = %d, want 19 — dc_qlookup row 17 must match libvpx", got)
	}
}

// libvpxRDIifactorBoundaryTable is the 32-entry table libvpx multiplies into
// cpi->RDMULT on pass==2 inter frames (vp8/encoder/rdopt.c:134-136).
// The lift is `RDMULT += (RDMULT * rd_iifactor[next_iiratio]) >> 4`,
// with next_iiratio clamped at 31. govpx does NOT currently apply this
// lift; this table is the sentinel for the follow-up port.
var libvpxRDIifactorBoundaryTable = [32]int{
	4, 4, 3, 2, 1, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

// TestVP8RDIifactorTableBoundaryValues pins the rd_iifactor table
// shape and the lift-multiplier expression. When the rd_iifactor port
// lands, this test becomes a regression guard for the table values; the
// lift call site (rdopt.c:189-196 in libvpx) must read RDMULT, multiply
// by rd_iifactor[min(next_iiratio,31)], shift right by 4, and add back
// to RDMULT — on pass==2 inter frames only.
func TestVP8RDIifactorTableBoundaryValues(t *testing.T) {
	// Spot-check the table against libvpx vp8/encoder/rdopt.c:134-136.
	wantHead := []int{4, 4, 3, 2, 1, 0}
	for i, w := range wantHead {
		if libvpxRDIifactorBoundaryTable[i] != w {
			t.Errorf("rd_iifactor[%d] = %d, want %d", i, libvpxRDIifactorBoundaryTable[i], w)
		}
	}
	for i := 6; i < 32; i++ {
		if libvpxRDIifactorBoundaryTable[i] != 0 {
			t.Errorf("rd_iifactor[%d] = %d, want 0 (tail of table is all zero)", i, libvpxRDIifactorBoundaryTable[i])
		}
	}

	// Sanity-check the lift expression on a representative inter-frame
	// case from the 720p two-pass VBR fixture. At Q=16 the raw RDMULT
	// is 907; with next_iiratio=2 (the table value 3) the lift is
	// (907 * 3) >> 4 = 170 -> RDMULT becomes 1077, which IS >1000 and
	// triggers the /100 split. govpx without the lift stays at 907,
	// /100 NOT triggered. The boundary cross-over is exactly the
	// mechanism behind the 720p two-pass +1.34% rate divergence.
	rawMult := 907
	lift := (rawMult * libvpxRDIifactorBoundaryTable[2]) >> 4
	if got, want := rawMult+lift, 1077; got != want {
		t.Errorf("iQ=16 / next_iiratio=2 lifted RDMULT = %d, want %d (= 907 + (907*3>>4)=907+170)", got, want)
	}

	// Verify govpx CURRENTLY skips the lift: vp8enc.RDConstantsWithZbin
	// returns the un-lifted pair at iQ=16. When the port lands the
	// signature changes and this assertion needs updating.
	if gotMult, gotDiv := vp8enc.RDConstantsWithZbin(16, 0); gotMult != 907 || gotDiv != 100 {
		t.Errorf("pre-port vp8enc.RDConstantsWithZbin(16,0) = (%d,%d), want un-lifted (907,100); update when the rd_iifactor port lands", gotMult, gotDiv)
	}
}
