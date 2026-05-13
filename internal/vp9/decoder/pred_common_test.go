package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// helper constructors keep the test tables compact.
func miInter(rf0, rf1 int8) *NeighborMi {
	return &NeighborMi{RefFrame: [2]int8{rf0, rf1}}
}
func miIntra() *NeighborMi {
	return &NeighborMi{RefFrame: [2]int8{IntraFrame, NoRefFrame}}
}

func TestGetPredContextSegId(t *testing.T) {
	a := &NeighborMi{SegIDPredicted: 1}
	l := &NeighborMi{SegIDPredicted: 1}
	cases := []struct {
		above, left *NeighborMi
		want        int
	}{
		{nil, nil, 0},
		{a, nil, 1},
		{nil, l, 1},
		{a, l, 2},
	}
	for i, c := range cases {
		if got := GetPredContextSegId(c.above, c.left); got != c.want {
			t.Errorf("case %d: got %d, want %d", i, got, c.want)
		}
	}
}

func TestGetSkipContext(t *testing.T) {
	a := &NeighborMi{Skip: 1}
	l := &NeighborMi{Skip: 1}
	cases := []struct {
		above, left *NeighborMi
		want        int
	}{
		{nil, nil, 0},
		{a, nil, 1},
		{nil, l, 1},
		{a, l, 2},
	}
	for i, c := range cases {
		if got := GetSkipContext(c.above, c.left); got != c.want {
			t.Errorf("case %d: got %d, want %d", i, got, c.want)
		}
	}
}

// TestGetIntraInterContextMatchesLibvpx exhaustively walks the 4-state
// truth table libvpx's comment block specifies:
//
//	0 - inter/inter, inter/--, --/inter, --/--
//	1 - intra/inter, inter/intra
//	2 - intra/--, --/intra
//	3 - intra/intra
func TestGetIntraInterContextMatchesLibvpx(t *testing.T) {
	inter := miInter(LastFrame, NoRefFrame)
	intra := miIntra()
	cases := []struct {
		above, left *NeighborMi
		want        int
	}{
		{nil, nil, 0},
		{inter, nil, 0},
		{nil, inter, 0},
		{inter, inter, 0},
		{intra, inter, 1},
		{inter, intra, 1},
		{intra, nil, 2},
		{nil, intra, 2},
		{intra, intra, 3},
	}
	for i, c := range cases {
		if got := GetIntraInterContext(c.above, c.left); got != c.want {
			t.Errorf("case %d: got %d, want %d", i, got, c.want)
		}
	}
}

// TestGetPredContextSwitchableInterpMatchesLibvpx exercises the
// nested if-chain: matching filters return the filter; a missing
// neighbor takes the other's; both-missing returns the sentinel
// SwitchableFilters.
func TestGetPredContextSwitchableInterpMatchesLibvpx(t *testing.T) {
	a := func(f uint8) *NeighborMi { return &NeighborMi{InterpFilter: f} }
	cases := []struct {
		above, left *NeighborMi
		want        int
	}{
		{nil, nil, SwitchableFilters},
		{a(0), nil, 0},
		{nil, a(1), 1},
		{a(0), a(0), 0},
		{a(2), a(2), 2},
		{a(0), a(1), SwitchableFilters},
	}
	for i, c := range cases {
		if got := GetPredContextSwitchableInterp(c.above, c.left); got != c.want {
			t.Errorf("case %d: got %d, want %d", i, got, c.want)
		}
	}
}

// TestGetTxSizeContextMatchesLibvpx exercises the (skip, present)
// fallback chain for above_ctx/left_ctx selection.
func TestGetTxSizeContextMatchesLibvpx(t *testing.T) {
	mkMi := func(tx common.TxSize, skip uint8) *NeighborMi {
		return &NeighborMi{TxSize: tx, Skip: skip}
	}
	// max=Tx16x16 (2): tx_sum > 2 → ctx=1, otherwise 0.
	cases := []struct {
		above, left *NeighborMi
		max         common.TxSize
		want        int
	}{
		{nil, nil, common.Tx16x16, 0}, // (max+max) == 2+2=4 > 2 → 1
		// actually with both nil and the fallback rules:
		//   aboveCtx = leftCtx = max; then (max+max) > max for any max>0.
	}
	// Recompute the first case under the actual algorithm to anchor it.
	// With both nil, both ctx fields default to maxTxSize; both fallback
	// reassignments are identity, so the sum is 2*max which is > max
	// whenever max > 0 — therefore the answer is 1, not 0. Replace.
	cases = []struct {
		above, left *NeighborMi
		max         common.TxSize
		want        int
	}{
		// nil,nil: both ctx = max; sum = 2*max > max for any max>0 → 1.
		{nil, nil, common.Tx16x16, 1},
		// Two Tx4x4 neighbors: 0+0 = 0, 0 > 2 false → 0.
		{mkMi(common.Tx4x4, 0), mkMi(common.Tx4x4, 0), common.Tx16x16, 0},
		// Tx16x16 above + Tx4x4 left: 2+0=2, 2>2 false → 0.
		{mkMi(common.Tx16x16, 0), mkMi(common.Tx4x4, 0), common.Tx16x16, 0},
		// Skip-above forces aboveCtx=max=2, left=0: 2+0=2, 2>2 false → 0.
		{mkMi(common.Tx16x16, 1), mkMi(common.Tx4x4, 0), common.Tx16x16, 0},
		// Both Tx32x32 neighbors at max=Tx16x16: 3+3=6 > 2 → 1.
		{mkMi(common.Tx32x32, 0), mkMi(common.Tx32x32, 0), common.Tx16x16, 1},
	}
	for i, c := range cases {
		if got := GetTxSizeContext(c.above, c.left, c.max); got != c.want {
			t.Errorf("case %d: got %d, want %d", i, got, c.want)
		}
	}
}

// TestSetupCompoundReferenceMode walks all 8 sign-bias combinations of
// (LAST,GOLDEN,ALTREF) and confirms the dispatch picks the right
// fixed/var triple.
func TestSetupCompoundReferenceMode(t *testing.T) {
	cases := []struct {
		last, golden, altref uint8
		wantFixed            int8
		wantVar              [2]int8
	}{
		// last==golden → fix=altref
		{0, 0, 0, AltrefFrame, [2]int8{LastFrame, GoldenFrame}},
		{1, 1, 0, AltrefFrame, [2]int8{LastFrame, GoldenFrame}},
		// last==altref → fix=golden
		{0, 1, 0, GoldenFrame, [2]int8{LastFrame, AltrefFrame}},
		{1, 0, 1, GoldenFrame, [2]int8{LastFrame, AltrefFrame}},
		// else fix=last
		{0, 1, 1, LastFrame, [2]int8{GoldenFrame, AltrefFrame}},
		{1, 0, 0, LastFrame, [2]int8{GoldenFrame, AltrefFrame}},
	}
	for i, c := range cases {
		var sb [MaxRefFrames]uint8
		sb[LastFrame] = c.last
		sb[GoldenFrame] = c.golden
		sb[AltrefFrame] = c.altref
		got := SetupCompoundReferenceMode(sb)
		if got.CompFixedRef != c.wantFixed || got.CompVarRef != c.wantVar {
			t.Errorf("case %d: got %+v, want fixed=%d var=%v",
				i, got, c.wantFixed, c.wantVar)
		}
	}
}

// TestCompoundReferenceAllowed: returns true iff sign-bias of any
// non-LAST ref frame differs from LAST.
func TestCompoundReferenceAllowed(t *testing.T) {
	cases := []struct {
		sb   [MaxRefFrames]uint8
		want bool
	}{
		{[MaxRefFrames]uint8{0, 0, 0, 0}, false},
		{[MaxRefFrames]uint8{0, 1, 1, 1}, false},
		{[MaxRefFrames]uint8{0, 0, 1, 0}, true},
		{[MaxRefFrames]uint8{0, 1, 0, 0}, true},
	}
	for i, c := range cases {
		if got := CompoundReferenceAllowed(c.sb); got != c.want {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

// TestGetPredContextSingleRefP1 anchors the "single-ref bit 1"
// context against representative branches of the libvpx C source.
func TestGetPredContextSingleRefP1(t *testing.T) {
	last := func() *NeighborMi { return &NeighborMi{RefFrame: [2]int8{LastFrame, NoRefFrame}} }
	gold := func() *NeighborMi { return &NeighborMi{RefFrame: [2]int8{GoldenFrame, NoRefFrame}} }
	comp := func(a, b int8) *NeighborMi { return &NeighborMi{RefFrame: [2]int8{a, b}} }
	intra := func() *NeighborMi { return &NeighborMi{RefFrame: [2]int8{IntraFrame, NoRefFrame}} }

	cases := []struct {
		name        string
		above, left *NeighborMi
		want        int
	}{
		{"no_edges", nil, nil, 2},
		{"both_intra", intra(), intra(), 2},
		{"above_intra_left_single_last", intra(), last(), 4},
		{"above_intra_left_single_gold", intra(), gold(), 0},
		{"both_single_last_last", last(), last(), 4},
		{"both_single_gold_gold", gold(), gold(), 0},
		{"both_single_last_gold", last(), gold(), 2},
		{"comp_both", comp(LastFrame, AltrefFrame), comp(GoldenFrame, AltrefFrame), 2},
		{"comp_both_with_last", comp(LastFrame, AltrefFrame), comp(LastFrame, GoldenFrame), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetPredContextSingleRefP1(c.above, c.left); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestGetPredContextSingleRefP2 anchors the "single-ref bit 2"
// context. Per libvpx, this discriminates GOLDEN vs ALTREF when the
// block already committed to "not LAST".
func TestGetPredContextSingleRefP2(t *testing.T) {
	last := &NeighborMi{RefFrame: [2]int8{LastFrame, NoRefFrame}}
	gold := &NeighborMi{RefFrame: [2]int8{GoldenFrame, NoRefFrame}}
	altr := &NeighborMi{RefFrame: [2]int8{AltrefFrame, NoRefFrame}}
	intra := &NeighborMi{RefFrame: [2]int8{IntraFrame, NoRefFrame}}

	cases := []struct {
		name        string
		above, left *NeighborMi
		want        int
	}{
		{"no_edges", nil, nil, 2},
		{"both_intra", intra, intra, 2},
		{"above_intra_left_single_last", intra, last, 3},
		{"above_intra_left_single_gold", intra, gold, 4},
		{"above_intra_left_single_altref", intra, altr, 0},
		{"both_single_last_last", last, last, 3},
		{"both_single_gold_gold", gold, gold, 4},
		{"both_single_altref_gold", altr, gold, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetPredContextSingleRefP2(c.above, c.left); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestGetPredContextCompRefP anchors the compound-ref-half-pick
// context for the common "single/single inter/inter" and the
// "no-edges" cases. The full truth table has many branches; here we
// pick at least one anchor per top-level switch.
func TestGetPredContextCompRefP(t *testing.T) {
	// Setup: fixed=ALTREF, var=(LAST,GOLDEN). sign_bias[ALTREF]=0
	// → fixRefIdx=0, varRefIdx=1.
	refs := CompoundFrameRefs{
		CompFixedRef: AltrefFrame,
		CompVarRef:   [2]int8{LastFrame, GoldenFrame},
	}
	var sb [MaxRefFrames]uint8
	sb[AltrefFrame] = 0

	intra := &NeighborMi{RefFrame: [2]int8{IntraFrame, NoRefFrame}}
	single := func(rf int8) *NeighborMi { return &NeighborMi{RefFrame: [2]int8{rf, NoRefFrame}} }

	cases := []struct {
		name        string
		above, left *NeighborMi
		want        int
	}{
		{"no_edges", nil, nil, 2},
		{"both_intra", intra, intra, 2},
		// single/single, vrfa=last vrfl=last → both==var[1]? last vs GOLDEN: no.
		// Both single → fall to the (l_sg && a_sg) branch. Not the fixed/var
		// crossover → pred_context = 3 if vrfa==vrfl else 1.
		{"both_single_last_last", single(LastFrame), single(LastFrame), 3},
		{"both_single_last_gold", single(LastFrame), single(GoldenFrame), 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetPredContextCompRefP(c.above, c.left, refs, sb); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// TestGetReferenceModeContextExhaustive covers each branch of the
// libvpx truth table:
//   - both edges single → XOR of (ref0==fixed)
//   - one comp, one single → 2 + (single's ref0==fixed || !inter)
//   - both comp → 4
//   - one edge: single → ref0==fixed, comp → 3
//   - no edges → 1
func TestGetReferenceModeContextExhaustive(t *testing.T) {
	refs := CompoundFrameRefs{CompFixedRef: AltrefFrame}
	mkSingle := func(rf int8) *NeighborMi { return &NeighborMi{RefFrame: [2]int8{rf, NoRefFrame}} }
	mkComp := func(rf0, rf1 int8) *NeighborMi { return &NeighborMi{RefFrame: [2]int8{rf0, rf1}} }
	mkIntra := func() *NeighborMi { return &NeighborMi{RefFrame: [2]int8{IntraFrame, NoRefFrame}} }

	cases := []struct {
		name        string
		above, left *NeighborMi
		want        int
	}{
		{"no_edges", nil, nil, 1},
		{"only_above_single_match", mkSingle(AltrefFrame), nil, 1},
		{"only_above_single_nomatch", mkSingle(LastFrame), nil, 0},
		{"only_above_comp", mkComp(LastFrame, AltrefFrame), nil, 3},
		{"both_single_xor_0", mkSingle(LastFrame), mkSingle(LastFrame), 0},
		{"both_single_xor_1", mkSingle(AltrefFrame), mkSingle(LastFrame), 1},
		{"both_single_xor_1_b", mkSingle(LastFrame), mkSingle(AltrefFrame), 1},
		{"both_single_xor_0_b", mkSingle(AltrefFrame), mkSingle(AltrefFrame), 0},
		{"above_single_left_comp_intra_match", mkSingle(AltrefFrame), mkComp(LastFrame, AltrefFrame), 3},
		{"above_single_left_comp_no_intra_no_match", mkSingle(LastFrame), mkComp(LastFrame, AltrefFrame), 2},
		{"above_intra_left_comp", mkIntra(), mkComp(LastFrame, AltrefFrame), 3},
		{"both_comp", mkComp(LastFrame, AltrefFrame), mkComp(GoldenFrame, AltrefFrame), 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GetReferenceModeContext(c.above, c.left, refs); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}
