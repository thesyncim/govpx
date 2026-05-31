package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
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

func TestVP9FullRDInterModeOrderMirrorsLibvpxSubsets(t *testing.T) {
	wantSingle := [...]common.PredictionMode{
		common.NearestMv,
		common.NewMv,
		common.NearMv,
		common.ZeroMv,
	}
	if vp9SingleRefInterModeOrder != wantSingle {
		t.Fatalf("single-ref full-RD order = %v, want %v",
			vp9SingleRefInterModeOrder, wantSingle)
	}
	wantCompound := [...]common.PredictionMode{
		common.ZeroMv,
		common.NearestMv,
		common.NearMv,
		common.NewMv,
	}
	if vp9CompoundInterModeOrder != wantCompound {
		t.Fatalf("compound fallback order = %v, want %v",
			vp9CompoundInterModeOrder, wantCompound)
	}
}

func TestVP9FullRDModeSkipStartRefMaskMirrorsLibvpx(t *testing.T) {
	refSkipMask := [2]uint8{0, 1}
	vp9FullRDApplyBestRefSkipMask(&refSkipMask, vp9dec.LastFrame)

	if vp9FullRDRefSkipped(refSkipMask, vp9dec.LastFrame, vp9dec.NoRefFrame) {
		t.Fatalf("LAST single-ref mode was skipped after LAST best mode")
	}
	if !vp9FullRDRefSkipped(refSkipMask, vp9dec.GoldenFrame, vp9dec.NoRefFrame) {
		t.Fatalf("GOLDEN single-ref mode was not skipped after LAST best mode")
	}
	if !vp9FullRDRefSkipped(refSkipMask, vp9dec.AltrefFrame, vp9dec.NoRefFrame) {
		t.Fatalf("ALTREF single-ref mode was not skipped after LAST best mode")
	}
	if vp9FullRDRefSkipped(refSkipMask, vp9dec.LastFrame, vp9dec.AltrefFrame) {
		t.Fatalf("LAST/ALTREF compound mode was skipped by single-ref mask")
	}
	if vp9FullRDRefSkipped(refSkipMask, vp9dec.GoldenFrame, vp9dec.AltrefFrame) {
		t.Fatalf("GOLDEN/ALTREF compound mode was skipped by single-ref mask")
	}

	refSkipMask = [2]uint8{0, 1}
	vp9FullRDApplyBestRefSkipMask(&refSkipMask, vp9dec.AltrefFrame)
	if !vp9FullRDRefSkipped(refSkipMask, vp9dec.LastFrame, vp9dec.NoRefFrame) {
		t.Fatalf("LAST single-ref mode was not skipped after ALTREF best mode")
	}
	if vp9FullRDRefSkipped(refSkipMask, vp9dec.AltrefFrame, vp9dec.NoRefFrame) {
		t.Fatalf("ALTREF single-ref mode was skipped after ALTREF best mode")
	}
}

func TestVP9FullRDCheckBestZeroMVGateMirrorsLibvpx(t *testing.T) {
	const nearCost = 30
	const nearestCost = 20
	const zeroCost = 10

	if vp9FullRDZeroMVModeAllowed(common.NearMv, true, false, true,
		nearCost, nearestCost, zeroCost) {
		t.Fatalf("zero NEARMV with higher rate than ZEROMV was allowed")
	}
	if vp9FullRDZeroMVModeAllowed(common.NearestMv, true, true, false,
		nearCost, nearestCost, zeroCost) {
		t.Fatalf("zero NEARESTMV with higher rate than ZEROMV was allowed")
	}
	if !vp9FullRDZeroMVModeAllowed(common.NearMv, false, false, false,
		nearCost, nearestCost, zeroCost) {
		t.Fatalf("non-zero NEARMV candidate was rejected")
	}
	if !vp9FullRDZeroMVModeAllowed(common.NearMv, true, false, true,
		5, nearestCost, zeroCost) {
		t.Fatalf("cheap zero NEARMV candidate was rejected")
	}
	if !vp9FullRDZeroMVModeAllowed(common.ZeroMv, true, false, false,
		nearCost, nearestCost, zeroCost) {
		t.Fatalf("ZEROMV candidate was rejected without zero NEAR/NEAREST alternatives")
	}
	if vp9FullRDZeroMVModeAllowed(common.ZeroMv, true, true, false,
		nearCost, zeroCost, zeroCost) {
		t.Fatalf("ZEROMV candidate was allowed when zero NEARESTMV was no more expensive")
	}
	if vp9FullRDZeroMVModeAllowed(common.ZeroMv, true, false, true,
		zeroCost, nearestCost, zeroCost) {
		t.Fatalf("ZEROMV candidate was allowed when zero NEARMV was no more expensive")
	}
}
