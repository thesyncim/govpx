package decoder

import "testing"

// TestLowerMvPrecisionRoundsOddBits drops the 1/8-pel bit when hp is
// not allowed. Rounding is toward zero — positive odd values lose 1,
// negative odd values gain 1.
func TestLowerMvPrecisionRoundsOddBits(t *testing.T) {
	cases := []struct {
		row, col     int16
		allowHp      bool
		wantR, wantC int16
	}{
		{3, -5, false, 2, -4},
		{3, -5, true, 3, -5}, // ref small ⇒ use_mv_hp true; bits kept
		{-1, 1, false, 0, 0},
		{0, 0, false, 0, 0},
	}
	for i, c := range cases {
		mv := MV{Row: c.row, Col: c.col}
		LowerMvPrecision(&mv, c.allowHp)
		if mv.Row != c.wantR || mv.Col != c.wantC {
			t.Errorf("case %d: got (%d,%d) want (%d,%d)", i, mv.Row, mv.Col, c.wantR, c.wantC)
		}
	}
}

// TestLowerMvPrecisionHpGate confirms that when the magnitude is at
// or above kMvRefThresh=64, the high-precision bit is dropped
// regardless of `allowHp`.
func TestLowerMvPrecisionHpGate(t *testing.T) {
	// ref row=64 -> useMvHp returns false (|64| < 64 is false).
	mv := MV{Row: 65, Col: 67}
	LowerMvPrecision(&mv, true)
	if mv.Row != 64 || mv.Col != 66 {
		t.Errorf("got (%d,%d) want (64,66)", mv.Row, mv.Col)
	}
}

// TestClampMv saturates row/col to the supplied bounding box.
func TestClampMv(t *testing.T) {
	mv := MV{Row: 200, Col: -50}
	ClampMv(&mv, -10, 100, -10, 50)
	if mv.Row != 50 || mv.Col != -10 {
		t.Errorf("got (%d,%d) want (50,-10)", mv.Row, mv.Col)
	}
}
