package govpx

import (
	"testing"

	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestCoefficientBlockTokenTraceMatchesAggregateRate(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	// Use a representative block: a couple of non-zero coefficients
	// at varying scan positions, plus an interior zero, with eob<16.
	var qcoeff [16]int16
	qcoeff[vp8tables.DefaultZigZag1D[0]] = 3
	qcoeff[vp8tables.DefaultZigZag1D[1]] = -1
	qcoeff[vp8tables.DefaultZigZag1D[3]] = 5
	const eob = 4

	wantTotal := coefficientBlockTokenRate(&probs, 3, 0, 0, &qcoeff, eob)
	trace, gotTotal := coefficientBlockTokenTrace(&probs, 3, 0, 0, &qcoeff, eob)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) == 0 {
		t.Fatalf("trace empty, want entries for positions 0..%d", eob)
	}

	sum := 0
	for _, e := range trace {
		sum += e.TokenRate + e.SignRate + e.ExtraBits
	}
	if sum != wantTotal {
		t.Fatalf("sum of per-position rates = %d, want %d", sum, wantTotal)
	}
	// EOB transition recorded as the trailing entry since eob<16.
	last := trace[len(trace)-1]
	if last.Token != vp8tables.DCTEOBToken {
		t.Fatalf("trailing trace token = %d, want EOB %d", last.Token, vp8tables.DCTEOBToken)
	}
	if last.Position != eob {
		t.Fatalf("trailing trace position = %d, want %d", last.Position, eob)
	}
}

func TestCoefficientBlockTokenTraceAllZerosRecordsSingleEOB(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var qcoeff [16]int16

	wantTotal := coefficientBlockTokenRate(&probs, 3, 0, 0, &qcoeff, 0)
	trace, gotTotal := coefficientBlockTokenTrace(&probs, 3, 0, 0, &qcoeff, 0)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) != 1 {
		t.Fatalf("trace length = %d, want 1 EOB entry", len(trace))
	}
	entry := trace[0]
	if entry.Position != 0 {
		t.Fatalf("eob entry position = %d, want 0", entry.Position)
	}
	if entry.Token != vp8tables.DCTEOBToken {
		t.Fatalf("eob entry token = %d, want EOB %d", entry.Token, vp8tables.DCTEOBToken)
	}
	if entry.Coefficient != 0 {
		t.Fatalf("eob entry coefficient = %d, want 0", entry.Coefficient)
	}
	if entry.SignRate != 0 || entry.ExtraBits != 0 {
		t.Fatalf("eob entry sign/extra = (%d,%d), want (0,0)", entry.SignRate, entry.ExtraBits)
	}
	if entry.TokenRate != wantTotal {
		t.Fatalf("eob entry rate = %d, want total %d", entry.TokenRate, wantTotal)
	}
}

func TestCoefficientBlockTokenTraceSingleNonZeroAtSkipDC(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	// skipDC=1 with a single non-zero at scan position 1 (eob=2): the trace
	// should contain the non-zero entry at position 1 followed by the EOB
	// entry at position 2.
	var qcoeff [16]int16
	qcoeff[vp8tables.DefaultZigZag1D[1]] = 1
	const skipDC = 1
	const eob = 2

	wantTotal := coefficientBlockTokenRate(&probs, 0, 0, skipDC, &qcoeff, eob)
	trace, gotTotal := coefficientBlockTokenTrace(&probs, 0, 0, skipDC, &qcoeff, eob)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) != 2 {
		t.Fatalf("trace length = %d, want 2 (non-zero + EOB)", len(trace))
	}

	first := trace[0]
	if first.Position != skipDC {
		t.Fatalf("first entry position = %d, want %d", first.Position, skipDC)
	}
	if first.Coefficient != 1 {
		t.Fatalf("first entry coefficient = %d, want 1", first.Coefficient)
	}
	if first.Token != vp8tables.OneToken {
		t.Fatalf("first entry token = %d, want OneToken %d", first.Token, vp8tables.OneToken)
	}
	if first.SignRate != boolBitCost(128, 0) {
		t.Fatalf("first entry sign rate = %d, want %d", first.SignRate, boolBitCost(128, 0))
	}

	second := trace[1]
	if second.Position != skipDC+1 {
		t.Fatalf("second entry position = %d, want %d", second.Position, skipDC+1)
	}
	if second.Token != vp8tables.DCTEOBToken {
		t.Fatalf("second entry token = %d, want EOB %d", second.Token, vp8tables.DCTEOBToken)
	}
	if second.SignRate != 0 || second.ExtraBits != 0 {
		t.Fatalf("second entry sign/extra = (%d,%d), want (0,0)", second.SignRate, second.ExtraBits)
	}

	sum := 0
	for _, e := range trace {
		sum += e.TokenRate + e.SignRate + e.ExtraBits
	}
	if sum != wantTotal {
		t.Fatalf("sum of per-position rates = %d, want %d", sum, wantTotal)
	}
}

func TestCoefficientBlockTokenTracePostZeroElidesEOBNode(t *testing.T) {
	probs := vp8tables.DefaultCoefProbs
	var qcoeff [16]int16
	qcoeff[vp8tables.DefaultZigZag1D[2]] = 2

	trace, gotTotal := coefficientBlockTokenTrace(&probs, 0, 0, 1, &qcoeff, 3)
	wantTotal := coefficientBlockTokenRate(&probs, 0, 0, 1, &qcoeff, 3)
	if gotTotal != wantTotal {
		t.Fatalf("trace total = %d, want %d", gotTotal, wantTotal)
	}
	if len(trace) != 3 {
		t.Fatalf("trace length = %d, want zero, nonzero, EOB entries", len(trace))
	}

	entry := trace[1]
	if entry.Position != 2 || entry.Token != vp8tables.TwoToken || entry.PrevTokenClass != 0 {
		t.Fatalf("post-zero entry = %+v, want position 2 TwoToken with prev-token class 0", entry)
	}
	band := int(vp8tables.CoefBandsTable[2])
	p := probs[0][band][0]
	full := treeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.TwoToken)
	want := full - boolBitCost(p[0], 1)
	if entry.TokenRate != want {
		t.Fatalf("post-zero token rate = %d, want elided subtree cost %d (full %d)", entry.TokenRate, want, full)
	}
}
