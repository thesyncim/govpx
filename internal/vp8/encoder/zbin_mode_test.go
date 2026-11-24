package encoder

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestInterZbinModeBoostMatchesLibvpxClasses(t *testing.T) {
	tests := []struct {
		name string
		mode InterFrameMacroblockMode
		want int
	}{
		{name: "last zeromv", mode: InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}, want: LastFrameZeroMVZbinBoost},
		{name: "golden zeromv", mode: InterFrameMacroblockMode{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV}, want: GoldenAltZeroMVZbinBoost},
		{name: "alt zeromv", mode: InterFrameMacroblockMode{RefFrame: vp8common.AltRefFrame, Mode: vp8common.ZeroMV}, want: GoldenAltZeroMVZbinBoost},
		{name: "newmv", mode: InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV}, want: NonZeroInterModeZbinBoost},
		{name: "splitmv", mode: InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.SplitMV}, want: SplitInterModeZbinBoost},
		{name: "intra", mode: InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred}, want: IntraInterFrameZbinBoost},
	}
	for _, tt := range tests {
		if got := InterZbinModeBoost(&tt.mode); got != tt.want {
			t.Fatalf("%s boost = %d, want %d", tt.name, got, tt.want)
		}
	}
}
