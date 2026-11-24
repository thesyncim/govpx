package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
)

func TestChromaRDCostStructure(t *testing.T) {
	for qIndex := 4; qIndex <= 56; qIndex++ {
		qValue := min(common.DCQuant(qIndex, 0), 160)
		wantRDMult := int(2.80 * float64(qValue*qValue))
		wantRDDiv := 100
		if wantRDMult > 1000 {
			wantRDDiv = 1
			wantRDMult /= 100
		}

		gotRDMult, gotRDDiv := RDConstantsWithZbin(qIndex, 0)
		if gotRDMult != wantRDMult {
			t.Errorf("qIndex=%d RDConstantsWithZbin rdMult=%d, want %d",
				qIndex, gotRDMult, wantRDMult)
		}
		if gotRDDiv != wantRDDiv {
			t.Errorf("qIndex=%d RDConstantsWithZbin rdDiv=%d, want %d",
				qIndex, gotRDDiv, wantRDDiv)
		}

		if got := BlockPlaneRDMultiplier(2); got != 2 {
			t.Errorf("BlockPlaneRDMultiplier(2) = %d, want 2", got)
		}
		if got, want := gotRDMult*BlockPlaneRDMultiplier(2), wantRDMult*2; got != want {
			t.Errorf("qIndex=%d chroma trellis rdmult=%d, want %d", qIndex, got, want)
		}
	}
}

func TestActivityMaskedRDMultiplierFormula(t *testing.T) {
	libvpxActivityMaskingFormula := func(rdMult int, act, avg int64) int {
		a := act + 2*avg
		if a <= 0 {
			return rdMult
		}
		b := 2*act + avg
		adjusted := int((int64(rdMult)*b + (a >> 1)) / a)
		if adjusted < 1 {
			return 1
		}
		return adjusted
	}

	const libvpxAltActivityAvg = 100000
	cases := []struct {
		name   string
		rdMult int
		act    int64
		avg    int64
	}{
		{name: "saturated_act_equals_avg", rdMult: 1000, act: 1 << 16, avg: 1 << 16},
		{name: "textured_act_gt_avg", rdMult: 1000, act: 1 << 17, avg: 1 << 16},
		{name: "flat_act_lt_avg", rdMult: 1000, act: 1 << 14, avg: 1 << 16},
		{name: "rdmult_1_saturated", rdMult: 1, act: 1 << 16, avg: 1 << 16},
		{name: "arnr_activity_avg_cohort", rdMult: 1000, act: libvpxAltActivityAvg, avg: libvpxAltActivityAvg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := libvpxActivityMaskingFormula(tc.rdMult, tc.act, tc.avg)
			a := tc.act + 2*tc.avg
			got := tc.rdMult
			if a > 0 {
				b := 2*tc.act + tc.avg
				got = max(int((int64(tc.rdMult)*b+(a>>1))/a), 1)
			}
			if got != want {
				t.Errorf("activity-masking formula drift (rdMult=%d act=%d avg=%d): got=%d want=%d",
					tc.rdMult, tc.act, tc.avg, got, want)
			}
		})
	}
}
