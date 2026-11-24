package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestProbDiffUpdateSavingsSearchModelPicksUpdate: counts at the
// PivotNode heavily favor "zero" (low prob); the search should
// find a smaller prob that saves bits and report positive savings.
// The tail nodes carry no events so they don't bias the savings.
func TestProbDiffUpdateSavingsSearchModelPicksUpdate(t *testing.T) {
	var ct [EntropyNodes][2]uint32
	ct[PivotNode] = [2]uint32{1000, 100} // heavy zero bias at pivot

	oldp := uint8(128)
	bestp := GetBinaryProb(ct[PivotNode][0], ct[PivotNode][1])
	startBestp := bestp
	savings := ProbDiffUpdateSavingsSearchModel(&ct, oldp, &bestp, DiffUpdateProb, 4)
	if savings <= 0 {
		t.Fatalf("savings = %d, want > 0 for heavy-bias counts", savings)
	}
	if bestp == oldp {
		t.Errorf("bestp unchanged at oldp; expected savings-search to pick a different prob")
	}
	// Search walks toward oldp. The picked bestp lies between
	// startBestp (raw count prob) and oldp, in either direction
	// depending on which way around they are.
	lo, hi := int(oldp), int(startBestp)
	if lo > hi {
		lo, hi = hi, lo
	}
	if int(bestp) < lo || int(bestp) > hi {
		t.Errorf("bestp=%d outside expected [%d, %d]", bestp, lo, hi)
	}
}

// TestProbDiffUpdateSavingsSearchModelStepSizeBounds: stepsize > 1
// reduces the search granularity; picking a stepsize that overshoots
// the gap (oldp == bestp ± 1) leaves bestp unchanged from oldp since
// no candidate lies strictly between them.
func TestProbDiffUpdateSavingsSearchModelStepSizeBounds(t *testing.T) {
	var ct [EntropyNodes][2]uint32
	ct[PivotNode] = [2]uint32{1, 0} // tiny counts → bestp should land near 255

	oldp := uint8(254)
	bestp := uint8(255) // bestp very close to oldp
	savings := ProbDiffUpdateSavingsSearchModel(&ct, oldp, &bestp, DiffUpdateProb, 4)
	// With stepsize=4 and only 1 step between oldp and bestp, the
	// search loop body never runs; savings stays 0 and bestp stays
	// at oldp.
	if savings != 0 {
		t.Errorf("savings = %d, want 0 for too-large stepsize", savings)
	}
	if bestp != oldp {
		t.Errorf("bestp = %d, want oldp = %d", bestp, oldp)
	}
}

// TestProbDiffUpdateSavingsSearchModelRoundTrip: write the update
// fragment that the model picked, then parse it back via
// VpxDiffUpdateProb — both sides land on the same new prob.
func TestProbDiffUpdateSavingsSearchModelRoundTrip(t *testing.T) {
	var ct [EntropyNodes][2]uint32
	ct[PivotNode] = [2]uint32{800, 50}

	oldp := uint8(128)
	bestp := max(GetBinaryProb(ct[PivotNode][0], ct[PivotNode][1]), 1)
	savings := ProbDiffUpdateSavingsSearchModel(&ct, oldp, &bestp, DiffUpdateProb, 4)
	if savings <= 0 {
		t.Skipf("savings=%d, no update path to round-trip", savings)
	}

	buf := make([]byte, 16)
	var bw bitstream.Writer
	bw.Start(buf)
	bw.Write(1, DiffUpdateProb) // update bit
	WriteProbDiffUpdate(&bw, bestp, oldp)
	size, _ := bw.Stop()

	var r bitstream.Reader
	r.Init(buf[:size])
	decoded := oldp
	vp9dec.VpxDiffUpdateProb(&r, &decoded)
	if decoded != bestp {
		t.Errorf("decoded = %d, want bestp = %d (oldp=%d)", decoded, bestp, oldp)
	}
}
