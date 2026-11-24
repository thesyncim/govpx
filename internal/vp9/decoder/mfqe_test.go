package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestMFQEDecisionMatchesLibvpx(t *testing.T) {
	cases := []struct {
		name string
		mi   NeighborMi
		bs   common.BlockSize
		want bool
	}{
		{
			name: "intra-rejected",
			mi: NeighborMi{
				Mode: common.DcPred,
			},
			bs:   common.Block16x16,
			want: false,
		},
		{
			name: "sub-16x16-rejected",
			mi: NeighborMi{
				Mode: common.NearestMv,
			},
			bs:   common.Block8x8,
			want: false,
		},
		{
			name: "inter-zero-mv-admitted",
			mi: NeighborMi{
				Mode: common.NearestMv,
			},
			bs:   common.Block16x16,
			want: true,
		},
		{
			name: "inter-mv-on-threshold",
			mi: NeighborMi{
				Mode: common.NewMv,
				Mv:   [2]MV{{Row: 10, Col: 0}},
			},
			bs:   common.Block32x32,
			want: true,
		},
		{
			name: "inter-mv-just-over",
			mi: NeighborMi{
				Mode: common.NewMv,
				Mv:   [2]MV{{Row: 10, Col: 1}},
			},
			bs:   common.Block32x32,
			want: false,
		},
		{
			name: "inter-mv-diagonal",
			mi: NeighborMi{
				Mode: common.NewMv,
				Mv:   [2]MV{{Row: 7, Col: 7}},
			},
			bs:   common.Block64x64,
			want: true,
		},
	}
	for _, tc := range cases {
		mi := tc.mi
		if got := MFQEDecision(&mi, tc.bs); got != tc.want {
			t.Errorf("%s: MFQEDecision = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestMFQEThresholdsMatchLibvpx(t *testing.T) {
	cases := []struct {
		bs           common.BlockSize
		qdiff        int
		wantSadThr   int
		wantVdiffThr int
	}{
		{common.Block16x16, 0, 7, 125},
		{common.Block32x32, 0, 6, 125},
		{common.Block64x64, 0, 5, 125},
		{common.Block16x16, 16, 8, 141},
		{common.Block32x32, 16, 7, 141},
		{common.Block64x64, 16, 6, 141},
		{common.Block16x16, 64, 11, 189},
		{common.Block32x32, 64, 10, 189},
		{common.Block64x64, 64, 9, 189},
	}
	for _, tc := range cases {
		sadThr, vdiffThr := MFQEThresholds(tc.bs, tc.qdiff)
		if sadThr != tc.wantSadThr {
			t.Errorf("MFQEThresholds(%v, %d) sadThr = %d, want %d",
				tc.bs, tc.qdiff, sadThr, tc.wantSadThr)
		}
		if vdiffThr != tc.wantVdiffThr {
			t.Errorf("MFQEThresholds(%v, %d) vdiffThr = %d, want %d",
				tc.bs, tc.qdiff, vdiffThr, tc.wantVdiffThr)
		}
	}
}

func TestMFQEConstantsMatchLibvpx(t *testing.T) {
	if MFQEQDiffThreshold != 20 {
		t.Errorf("MFQEQDiffThreshold = %d, want 20", MFQEQDiffThreshold)
	}
	if MFQELastQThreshold != 170 {
		t.Errorf("MFQELastQThreshold = %d, want 170", MFQELastQThreshold)
	}
	if MFQEMvLenSquareThreshold != 100 {
		t.Errorf("MFQEMvLenSquareThreshold = %d, want 100",
			MFQEMvLenSquareThreshold)
	}
	if MFQEPrecision != 4 {
		t.Errorf("MFQEPrecision = %d, want 4", MFQEPrecision)
	}
}

func TestMFQEBlockMetricsRoundingMatchesLibvpx(t *testing.T) {
	for _, side := range []int{16, 32, 64} {
		a := make([]byte, side*side)
		b := make([]byte, side*side)
		for i := range a {
			a[i] = byte(i & 0xff)
			b[i] = byte(i & 0xff)
		}
		vdiff, sad := MFQEBlockMetrics(side, a, side, b, side)
		if vdiff != 0 || sad != 0 {
			t.Errorf("side=%d identical buffers: vdiff=%d sad=%d, want both 0",
				side, vdiff, sad)
		}
	}
}

func TestMFQEBlockMetricsSide16(t *testing.T) {
	const side = 16
	a := make([]byte, side*side)
	b := make([]byte, side*side)
	for r := range side {
		for c := range side {
			a[r*side+c] = 0
			if r < 8 && c < 8 {
				b[r*side+c] = 16
			}
		}
	}
	vdiff, sad := MFQEBlockMetrics(side, a, side, b, side)
	if vdiff != 48 {
		t.Errorf("vdiff = %d, want 48", vdiff)
	}
	if sad != 4 {
		t.Errorf("sad = %d, want 4", sad)
	}
}
