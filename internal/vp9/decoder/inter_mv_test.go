package decoder

import "testing"

// TestRoundMvComp anchors the half-away-from-zero rounding helpers
// against a hand-built truth table.
func TestRoundMvComp(t *testing.T) {
	cases := []struct {
		v  int
		q2 int
		q4 int
	}{
		{0, 0, 0},
		{1, 1, 0},
		{2, 1, 1},
		{4, 2, 1},
		{-1, -1, 0},
		{-2, -1, -1},
		{8, 4, 2},
		{-8, -4, -2},
	}
	for _, c := range cases {
		if got := roundMvCompQ2(c.v); got != c.q2 {
			t.Errorf("roundMvCompQ2(%d)=%d want %d", c.v, got, c.q2)
		}
		if got := roundMvCompQ4(c.v); got != c.q4 {
			t.Errorf("roundMvCompQ4(%d)=%d want %d", c.v, got, c.q4)
		}
	}
}

// TestAverageSplitMvsByChroma covers the four subsampling shapes:
// 4:4:4 (identity), 4:4:2 (vertical pair), 4:2:2 (horizontal pair),
// and 4:2:0 (4-way average).
func TestAverageSplitMvsByChroma(t *testing.T) {
	var bmi [4]Bmi
	bmi[0].AsMv[0] = MV{Row: 8, Col: 4}
	bmi[1].AsMv[0] = MV{Row: 4, Col: 4}
	bmi[2].AsMv[0] = MV{Row: 4, Col: -2}
	bmi[3].AsMv[0] = MV{Row: -4, Col: 6}
	// 4:4:4 → identity, returns bmi[block].
	if got := AverageSplitMvs(&bmi, 0, 1, 0, 0); got != bmi[1].AsMv[0] {
		t.Errorf("ss=0,0: got %+v want %+v", got, bmi[1].AsMv[0])
	}
	// 4:4:2 → vertical pair (block, block+2). block=0 → avg(0, 2):
	// row=(8+4)/2=6, col=(4+-2)/2 = round_q2(2)=1.
	got := AverageSplitMvs(&bmi, 0, 0, 0, 1)
	if got != (MV{Row: 6, Col: 1}) {
		t.Errorf("ss=0,1 block=0: got %+v want (6,1)", got)
	}
	// 4:2:2 → horizontal pair (block, block+1). block=0 → avg(0, 1):
	// row=(8+4)/2=6, col=(4+4)/2=4.
	got = AverageSplitMvs(&bmi, 0, 0, 1, 0)
	if got != (MV{Row: 6, Col: 4}) {
		t.Errorf("ss=1,0 block=0: got %+v want (6,4)", got)
	}
	// 4:2:0 → 4-way average. row=(8+4+4-4)/4=q4(12)=3, col=(4+4-2+6)/4=q4(12)=3.
	got = AverageSplitMvs(&bmi, 0, 0, 1, 1)
	if got != (MV{Row: 3, Col: 3}) {
		t.Errorf("ss=1,1: got %+v want (3,3)", got)
	}
}

// TestClampMvToUmvBorderSb on a small block confirms the saturation
// math: a far-out-of-frame MV gets clamped to the per-edge limits.
func TestClampMvToUmvBorderSb(t *testing.T) {
	// 16x16 block; ssX=ssY=0 → shift=2 → MV*2 is clamped against
	// edges*2 ± (spelLeft / spelRight / spelTop / spelBottom).
	// Pick edges so we can predict the saturation cleanly.
	edges := BlockBoundsEdges{
		MbToLeftEdge:   -100,
		MbToRightEdge:  100,
		MbToTopEdge:    -200,
		MbToBottomEdge: 200,
	}
	// Inside the box → MV scaled by 2.
	in := MV{Row: 10, Col: -5}
	got := ClampMvToUmvBorderSb(edges, in, 16, 16, 0, 0)
	if got.Row != 20 || got.Col != -10 {
		t.Errorf("inside: got %+v want (20,-10)", got)
	}
	// Force a Col under-clamp with a value small enough to stay
	// inside int16 after the *shiftX scale-up: -500 * 2 = -1000,
	// well below the lower edge -520.
	in = MV{Row: 0, Col: -500}
	got = ClampMvToUmvBorderSb(edges, in, 16, 16, 0, 0)
	wantMinCol := edges.MbToLeftEdge*2 - ((VP9InterpExtend + 16) << SubpelBitsConst)
	if int(got.Col) != wantMinCol {
		t.Errorf("under col: got %d want %d", got.Col, wantMinCol)
	}
}
