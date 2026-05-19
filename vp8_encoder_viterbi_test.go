package govpx

import (
	"testing"

	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// viterbiTestRegularBlockQuant mirrors testRegularBlockQuant in
// vp8_encoder_reconstruct_test.go but is duplicated locally so this file does not
// share symbols with the parallel tests.
func viterbiTestRegularBlockQuant(qIndex int, dequantValue int16) vp8enc.BlockQuant {
	var dequant [16]int16
	for i := range dequant {
		dequant[i] = dequantValue
	}
	var quant vp8enc.BlockQuant
	vp8enc.InitRegularBlockQuant(qIndex, &dequant, &quant)
	return quant
}

// TestViterbiY2PlaneDropsOvershootDC exercises the blockType=1 (Y2) path with
// skipDC=0. The Y2 plane multiplier is 16, so even modest rate cost dominates
// distortion at high qIndex. With a tiny overshoot at DC the trellis should
// drive qcoeff[0] to zero.
func TestViterbiY2PlaneDropsOvershootDC(t *testing.T) {
	quant := viterbiTestRegularBlockQuant(127, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	// Position 0 in zigzag is rc=0. Coeff slightly above one quant step so
	// dequant=100*1=100 overshoots coeff=11. |x|*dq=100 lies in (|c|=11, |c|+dq=111).
	coeff[0] = 11
	qcoeff[0] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 1, 0, 0, 0, false, &coeff, &quant, &qcoeff, 1)

	if eob != 0 || qcoeff[0] != 0 {
		t.Fatalf("Y2 plane optimized eob/qcoeff[0] = %d/%d, want overshoot DC dropped", eob, qcoeff[0])
	}
}

func TestY2OptimizedQuantUsesFullZbinOverQuantForTrellis(t *testing.T) {
	const (
		qIndex         = 20
		zbinOverQuant  = 32
		dequantValue   = int16(35)
		coefficientVal = int16(28)
	)
	quant := viterbiTestRegularBlockQuant(qIndex, dequantValue)
	var coeff [16]int16
	var halfQ, fullQ [16]int16
	var halfDQ, fullDQ [16]int16
	coeff[0] = coefficientVal

	halfEOB := quantizeEncodedBlockWithRDZbin(&vp8tables.DefaultCoefProbs, qIndex, 1, 0, 0, zbinOverQuant/2, 0, zbinOverQuant/2, false, false, true, &coeff, &quant, &halfQ, &halfDQ)
	fullEOB := quantizeEncodedBlockWithRDZbin(&vp8tables.DefaultCoefProbs, qIndex, 1, 0, 0, zbinOverQuant/2, 0, zbinOverQuant, false, false, true, &coeff, &quant, &fullQ, &fullDQ)

	if halfEOB != 1 || halfQ[0] == 0 {
		t.Fatalf("halved-zbin trellis eob/qcoeff[0] = %d/%d, want coefficient kept to prove fixture sensitivity", halfEOB, halfQ[0])
	}
	if fullEOB != 0 || fullQ[0] != 0 {
		t.Fatalf("full-zbin trellis eob/qcoeff[0] = %d/%d, want libvpx full-rdmult path to drop coefficient", fullEOB, fullQ[0])
	}
}

// TestViterbiUVPlaneDropsOvershootDC exercises blockType=2 (UV plane,
// UV_RD_MULT=2). UV's plane multiplier is smaller than Y2's so we still need
// a high qIndex to make the rate cost dominate.
func TestViterbiUVPlaneDropsOvershootDC(t *testing.T) {
	quant := viterbiTestRegularBlockQuant(127, 100)
	var coeff [16]int16
	var qcoeff [16]int16
	coeff[0] = 11
	qcoeff[0] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 2, 0, 0, 0, false, &coeff, &quant, &qcoeff, 1)

	if eob != 0 || qcoeff[0] != 0 {
		t.Fatalf("UV plane optimized eob/qcoeff[0] = %d/%d, want overshoot DC dropped", eob, qcoeff[0])
	}
}

// TestViterbiIntraScalingChangesDecision exercises the (rdMult*9)>>4 scaling
// applied for intra blocks. With intra=true the rate side of the RD cost is
// reduced so distortion-heavy choices are favored. We pick an input where
// inter (intra=false) drops the trailing coefficient but intra retains it.
func TestViterbiIntraScalingChangesDecision(t *testing.T) {
	// Search for a (qIndex, dequantValue, coeffValue) triple that makes the
	// inter path drop the coefficient while the intra path keeps it. Doing
	// this dynamically keeps the test robust to small RD constant changes.
	var found bool
	var foundQ, foundDQ int
	var foundCoeff int16

outer:
	for q := 30; q <= 110; q += 5 {
		for dq := int16(20); dq <= 200; dq += 10 {
			for c := dq / 2; c <= dq*3/2; c++ {
				quant := viterbiTestRegularBlockQuant(q, dq)

				rc := int(vp8tables.DefaultZigZag1D[1])
				var inter, intra [16]int16
				var coeff [16]int16
				coeff[rc] = c
				inter[rc] = 1
				intra[rc] = 1

				interEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, q, 0, 0, 1, 0, false, &coeff, &quant, &inter, 2)
				intraEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, q, 0, 0, 1, 0, true, &coeff, &quant, &intra, 2)

				if interEOB == 1 && inter[rc] == 0 && intraEOB == 2 && intra[rc] == 1 {
					found = true
					foundQ, foundDQ, foundCoeff = q, int(dq), c
					break outer
				}
			}
		}
	}
	if !found {
		t.Fatalf("could not locate input where intra/inter RD scaling diverged")
	}
	t.Logf("intra/inter divergence at qIndex=%d dequant=%d coeff=%d", foundQ, foundDQ, foundCoeff)
}

// TestViterbiAllZeroQCoeffRollsBackEOB confirms the trellis correctly rolls
// EOB back to skipDC when every quantized coefficient is zero — libvpx's
// optimize_b reaches no states in the trellis, so the backtrace records no
// final-EOB candidates and returns skipDC.
func TestViterbiAllZeroQCoeffRollsBackEOB(t *testing.T) {
	quant := viterbiTestRegularBlockQuant(60, 50)
	var coeff [16]int16
	var qcoeff [16]int16
	for i := range coeff {
		coeff[i] = int16(3 * (i + 1))
	}
	startQ := qcoeff
	startEOB := 8
	const skipDC = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 60, 0, 0, skipDC, 0, false, &coeff, &quant, &qcoeff, startEOB)

	if eob != skipDC {
		t.Fatalf("all-zero optimized eob = %d, want skipDC=%d", eob, skipDC)
	}
	if qcoeff != startQ {
		t.Fatalf("all-zero qcoeff mutated: %v vs %v", qcoeff, startQ)
	}
}

// TestViterbiSingleCoefficientBlock exercises an eob=2 input with a single
// non-zero AC coefficient at zigzag position 1. With distortion dominant the
// coefficient is preserved; with rate dominant it is dropped. We verify both
// branches to confirm the single-coefficient sweep picks the better RD score.
func TestViterbiSingleCoefficientBlock(t *testing.T) {
	rc := int(vp8tables.DefaultZigZag1D[1])

	// Distortion-dominant: very low qIndex (small rdMult), large coeff
	// magnitude relative to dequant means dropping costs a lot of distortion.
	keepQuant := viterbiTestRegularBlockQuant(4, 100)
	var keepCoeff [16]int16
	var keepQ [16]int16
	keepCoeff[rc] = 100
	keepQ[rc] = 1
	keepEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 4, 0, 0, 1, 0, false, &keepCoeff, &keepQuant, &keepQ, 2)
	if keepEOB != 2 || keepQ[rc] != 1 {
		t.Fatalf("single-coef keep: eob/q = %d/%d, want preserved", keepEOB, keepQ[rc])
	}

	// Rate-dominant: high qIndex (large rdMult), tiny overshoot makes
	// dropping cheap in distortion.
	dropQuant := viterbiTestRegularBlockQuant(127, 10)
	var dropCoeff [16]int16
	var dropQ [16]int16
	dropCoeff[rc] = 9
	dropQ[rc] = 1
	dropEOB := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &dropCoeff, &dropQuant, &dropQ, 2)
	if dropEOB != 1 || dropQ[rc] != 0 {
		t.Fatalf("single-coef drop: eob/q = %d/%d, want dropped", dropEOB, dropQ[rc])
	}
}

// TestViterbiFullBlockHandlesAllPositions exercises an eob=16 block where
// every zigzag position carries a non-zero quantized coefficient. The
// trellis must walk all 16 positions without out-of-bounds indexing and
// must not introduce sign flips.
func TestViterbiFullBlockHandlesAllPositions(t *testing.T) {
	quant := viterbiTestRegularBlockQuant(80, 50)
	var coeff [16]int16
	var qcoeff [16]int16
	for pos := range 16 {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		// Choose magnitudes that survive the quantizer (close-to-exact
		// match with dequant=50) so most positions stay non-zero.
		coeff[rc] = int16(50 + 3*pos)
		qcoeff[rc] = 1
	}

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 80, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 16)

	if eob < 0 || eob > 16 {
		t.Fatalf("full-block eob out of range: %d", eob)
	}
	for pos := range 16 {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		// The trellis only decrements magnitudes by one, so signs must
		// match the original (positive here) or be zero.
		if qcoeff[rc] < 0 {
			t.Fatalf("full-block qcoeff[%d]=%d, want non-negative after trellis", rc, qcoeff[rc])
		}
		if qcoeff[rc] > 1 {
			t.Fatalf("full-block qcoeff[%d]=%d, want at most original magnitude 1", rc, qcoeff[rc])
		}
	}

	// Walking positions beyond eob in scan order must all be zero so the
	// returned EOB describes the last non-zero coefficient correctly.
	for pos := eob; pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		if qcoeff[rc] != 0 {
			t.Fatalf("full-block tail qcoeff[%d]=%d at pos %d after eob=%d, want zero", rc, qcoeff[rc], pos, eob)
		}
	}
}

// TestViterbiBacktraceMixesKeepAndDropDecisions targets the backward sweep's
// path-dependence: a trailing rate-cheap coefficient should be dropped while
// a distortion-heavy interior coefficient is retained. This exercises mixed
// keep/drop decisions across positions in a single call.
func TestViterbiBacktraceMixesKeepAndDropDecisions(t *testing.T) {
	quant := viterbiTestRegularBlockQuant(127, 10)
	var coeff [16]int16
	var qcoeff [16]int16

	// Position 1 (interior, scan order): large coefficient with a very
	// close quantized match -> dropping it costs a lot of distortion.
	rc1 := int(vp8tables.DefaultZigZag1D[1])
	coeff[rc1] = 200
	qcoeff[rc1] = 20

	// Position 3 (later in scan order): tiny overshoot -> dropping it
	// costs almost no distortion but saves rate, so it should be dropped.
	rc3 := int(vp8tables.DefaultZigZag1D[3])
	coeff[rc3] = 9
	qcoeff[rc3] = 1

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 4)

	if qcoeff[rc1] == 0 {
		t.Fatalf("backtrace dropped distortion-heavy interior qcoeff[%d]=0, want retained", rc1)
	}
	if qcoeff[rc3] != 0 {
		t.Fatalf("backtrace kept rate-cheap trailing qcoeff[%d]=%d, want dropped", rc3, qcoeff[rc3])
	}
	// EOB must reflect the last non-zero scan position. With pos=3 dropped
	// and pos=1 retained, the next non-zero position in scan order is 1,
	// so eob should be 2 (1-indexed end-of-block marker).
	if eob != 2 {
		t.Fatalf("backtrace eob = %d, want 2 (after dropping trailing coefficient)", eob)
	}
}

// TestViterbiDoesNotUpdateDqcoeff documents the contract that
// optimizeQuantizedBlock mutates only qcoeff. Callers (e.g. the
// quantizeOptimizedBlock wrapper) must invoke dequantizeQuantizedBlock to
// resync dqcoeff after the trellis pass.
func TestViterbiDoesNotUpdateDqcoeff(t *testing.T) {
	quant := viterbiTestRegularBlockQuant(127, 10)
	rc := int(vp8tables.DefaultZigZag1D[1])
	var coeff [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	coeff[rc] = 9
	qcoeff[rc] = 1
	// Pre-populate dqcoeff with a sentinel; the trellis must leave it alone.
	for i := range dqcoeff {
		dqcoeff[i] = 1234
	}
	startDQ := dqcoeff

	eob := optimizeQuantizedBlock(&vp8tables.DefaultCoefProbs, 127, 0, 0, 1, 0, false, &coeff, &quant, &qcoeff, 2)

	if eob != 1 || qcoeff[rc] != 0 {
		t.Fatalf("trellis eob/q = %d/%d, want trailing coefficient dropped", eob, qcoeff[rc])
	}
	if dqcoeff != startDQ {
		t.Fatalf("trellis mutated dqcoeff: %v vs %v", dqcoeff, startDQ)
	}
}
