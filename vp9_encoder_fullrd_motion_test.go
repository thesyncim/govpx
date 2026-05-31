package govpx

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9FullRDRefPruningMirrorsLibvpx(t *testing.T) {
	refs := []int8{vp9dec.LastFrame, vp9dec.GoldenFrame, vp9dec.AltrefFrame}
	var states [vp9dec.MaxRefFrames]vp9FullRDRefState
	states[vp9dec.LastFrame].mvPredState = vp9InterMvPredState{
		valid:   true,
		predSad: 1200,
	}
	states[vp9dec.GoldenFrame].mvPredState = vp9InterMvPredState{
		valid:   true,
		predSad: 100,
	}
	states[vp9dec.AltrefFrame].mvPredState = vp9InterMvPredState{
		valid:   true,
		predSad: 200,
	}

	vp9PruneFullRDRefStates(&states, refs, 1, 1, true)

	if got := states[vp9dec.LastFrame].modeSkip; got != vp9InterNearestNearZeroMask {
		t.Fatalf("LAST mode skip mask = %#x, want %#x",
			got, vp9InterNearestNearZeroMask)
	}
	if !states[vp9dec.LastFrame].skipNewMv {
		t.Fatalf("LAST NEWMV was not pruned")
	}
	if got := states[vp9dec.GoldenFrame].modeSkip; got != 0 {
		t.Fatalf("GOLDEN mode skip mask = %#x, want 0", got)
	}
	if states[vp9dec.GoldenFrame].skipNewMv {
		t.Fatalf("GOLDEN NEWMV was pruned")
	}
}

func TestVP9FullRDRefPruningKeepsAdaptiveSearchBehindShowFrame(t *testing.T) {
	refs := []int8{vp9dec.LastFrame, vp9dec.GoldenFrame}
	var states [vp9dec.MaxRefFrames]vp9FullRDRefState
	states[vp9dec.LastFrame].mvPredState = vp9InterMvPredState{
		valid:   true,
		predSad: 1200,
	}
	states[vp9dec.GoldenFrame].mvPredState = vp9InterMvPredState{
		valid:   true,
		predSad: 100,
	}

	vp9PruneFullRDRefStates(&states, refs, 0, 1, false)

	if states[vp9dec.LastFrame].skipNewMv {
		t.Fatalf("LAST NEWMV was pruned when show_frame was false")
	}
	if got := states[vp9dec.LastFrame].modeSkip; got != 0 {
		t.Fatalf("LAST mode skip mask = %#x, want 0", got)
	}
}
