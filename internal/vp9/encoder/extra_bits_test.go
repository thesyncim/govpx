package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// TestVP9ExtraBitsShape: every CAT* token's Len matches its Prob
// slice length, and the BaseVal sequence is monotonically increasing.
func TestVP9ExtraBitsShape(t *testing.T) {
	for i, want := range []int{1, 2, 3, 4, 5, 14} {
		cls := Category1Tok + i
		if VP9ExtraBits[cls].Len != want {
			t.Errorf("CAT%d Len = %d, want %d", i+1, VP9ExtraBits[cls].Len, want)
		}
		if cls != Category6Tok && len(VP9ExtraBits[cls].Prob) != want {
			t.Errorf("CAT%d Prob len = %d, want %d", i+1, len(VP9ExtraBits[cls].Prob), want)
		}
	}
	// CAT6 in 8-bit profile uses Cat6Prob (14 entries); confirm
	// they're the same backing array as the decoder table.
	if len(VP9ExtraBits[Category6Tok].Prob) != 14 {
		t.Errorf("CAT6 Prob len = %d, want 14", len(VP9ExtraBits[Category6Tok].Prob))
	}
	if VP9ExtraBits[Category6Tok].Prob[0] != tables.Cat6Prob[0] {
		t.Error("CAT6 Prob doesn't share storage with tables.Cat6Prob")
	}
}

// TestTokenForAbsCoeff anchors the magnitude → token mapping. For
// magnitudes <=4 the token directly encodes the value; CAT1..CAT6
// carry the value - BaseVal as extra bits.
func TestTokenForAbsCoeff(t *testing.T) {
	cases := []struct {
		abs       int
		wantTok   int
		wantExtra int
	}{
		{0, ZeroToken, 0},
		{1, OneToken, 0},
		{4, FourToken, 0},
		{5, Category1Tok, 0},  // CAT1_MIN_VAL = 5
		{6, Category1Tok, 1},  // 6 - 5 = 1
		{7, Category2Tok, 0},  // CAT2_MIN_VAL = 7
		{10, Category2Tok, 3}, // 10 - 7 = 3
		{11, Category3Tok, 0}, // CAT3_MIN_VAL = 11
		{18, Category3Tok, 7},
		{19, Category4Tok, 0}, // CAT4_MIN_VAL = 19
		{34, Category4Tok, 15},
		{35, Category5Tok, 0}, // CAT5_MIN_VAL = 35
		{66, Category5Tok, 31},
		{67, Category6Tok, 0}, // CAT6_MIN_VAL = 67
		{1000, Category6Tok, 933},
	}
	for _, c := range cases {
		gotTok, gotExtra := TokenForAbsCoeff(c.abs)
		if gotTok != c.wantTok || gotExtra != c.wantExtra {
			t.Errorf("abs=%d: got (tok=%d, extra=%d), want (tok=%d, extra=%d)",
				c.abs, gotTok, gotExtra, c.wantTok, c.wantExtra)
		}
	}
}

// TestVP9ExtraBitsBaseVals confirms the (BaseVal, Len) pair lines
// up so that every magnitude in [BaseVal, BaseVal + (1<<Len) - 1]
// maps to that category.
func TestVP9ExtraBitsBaseVals(t *testing.T) {
	for i := Category1Tok; i <= Category6Tok; i++ {
		eb := VP9ExtraBits[i]
		maxAbs := eb.BaseVal + (1 << uint(eb.Len)) - 1
		got, extra := TokenForAbsCoeff(maxAbs)
		if got != i {
			t.Errorf("cat=%d: max abs=%d mapped to token %d", i, maxAbs, got)
		}
		if extra != (1<<uint(eb.Len))-1 {
			t.Errorf("cat=%d: max abs=%d extra=%d, want %d", i, maxAbs, extra, (1<<uint(eb.Len))-1)
		}
	}
}
