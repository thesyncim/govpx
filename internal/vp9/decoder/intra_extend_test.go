package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestIntraNeedHelpers covers the three boolean predicates.
func TestIntraNeedHelpers(t *testing.T) {
	cases := []struct {
		mode     common.PredictionMode
		l, a, ar bool
	}{
		{common.DcPred, true, true, false},
		{common.VPred, false, true, false},
		{common.HPred, true, false, false},
		{common.D45Pred, false, false, true},
		{common.D63Pred, false, false, true},
		{common.D207Pred, true, false, false},
		{common.TmPred, true, true, false},
	}
	for _, c := range cases {
		if got := IntraNeedsLeft(c.mode); got != c.l {
			t.Errorf("mode %d Left: got %v want %v", c.mode, got, c.l)
		}
		if got := IntraNeedsAbove(c.mode); got != c.a {
			t.Errorf("mode %d Above: got %v want %v", c.mode, got, c.a)
		}
		if got := IntraNeedsAboveRight(c.mode); got != c.ar {
			t.Errorf("mode %d AboveRight: got %v want %v", c.mode, got, c.ar)
		}
	}
}
