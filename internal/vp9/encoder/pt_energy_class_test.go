package encoder

import "testing"

// TestPtEnergyClassPositionalAnchors enforces the structural invariants
// the libvpx comment block in vp9_entropy.h:27-38 promises: ZERO maps
// to class 0, ONE/TWO each get their own class, THREE and FOUR share
// class 3, CAT1/CAT2 share class 4, CAT3..CAT6 + EOB all live at the
// terminal class 5. These shape checks catch a drift that the
// source-text pin can't (e.g. permuted indices that still total the
// right number of distinct values).
func TestPtEnergyClassPositionalAnchors(t *testing.T) {
	type anchor struct {
		idx  int
		want uint8
		name string
	}
	cases := []anchor{
		{ZeroToken, 0, "ZERO_TOKEN"},
		{OneToken, 1, "ONE_TOKEN"},
		{TwoToken, 2, "TWO_TOKEN"},
		{ThreeToken, 3, "THREE_TOKEN"},
		{FourToken, 3, "FOUR_TOKEN"},
		{Category1Tok, 4, "CATEGORY1_TOKEN"},
		{Category2Tok, 4, "CATEGORY2_TOKEN"},
		{Category3Tok, 5, "CATEGORY3_TOKEN"},
		{Category4Tok, 5, "CATEGORY4_TOKEN"},
		{Category5Tok, 5, "CATEGORY5_TOKEN"},
		{Category6Tok, 5, "CATEGORY6_TOKEN"},
		{EobToken, 5, "EOB_TOKEN"},
	}
	for _, c := range cases {
		if got := PtEnergyClass[c.idx]; got != c.want {
			t.Errorf("PtEnergyClass[%s=%d] = %d, want %d", c.name, c.idx, got, c.want)
		}
	}
}
