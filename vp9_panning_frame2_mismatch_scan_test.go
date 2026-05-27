//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9PanningFrame2MismatchScan(t *testing.T) {
	vp9test.RequireOracle(t, "panning frame2 mismatch scan")
	const width, height, frames = 64, 64, 3
	sources := make([]*image.YCbCr, frames)
	for i := range sources {
		sources[i] = vp9test.NewPanningYCbCr(width, height, i)
	}
	opts := vp9OracleCyclicRefreshCBROptions(width, height, 700)
	extraArgs := vp9OracleCyclicRefreshCBRArgs(700, 600, 400, 500, 0)
	got := encodeVP9FramesWithGovpx(t, opts, sources, nil)
	want := vp9test.VpxencFrameFlagPackets(t, sources, vp9LibvpxFrameFlags(nil), extraArgs...)
	gGrid := decodeVP9SequenceMiGridsForOracleTest(t, got)[2]
	lGrid := decodeVP9SequenceMiGridsForOracleTest(t, want)[2]
	var segHistG [8]int
	for _, g := range gGrid {
		segHistG[g.SegmentID]++
	}
	t.Logf("govpx seg hist=%v", segHistG)
	t.Logf("pick trace mi[3] 8x8: predMvSad=%v refSkipMask=0x%x maxUsable=%d forceSkip=%v sseZeromvNorm=%d goldenSkip=%v seg=%d segQ=%d cyclicBoost=%v rdmult=%d scores lastNear=%d (rate=%d dist=%d sse=%d skip=%v) lastNew=%d goldenNew=%d (rate=%d dist=%d sse=%d skip=%v) lastNearestMv=%v lastNearMv=%v goldenNewMv=%v bestRef=%d bestMode=%d bestScore=%d",
		vp9NonrdPickTraceLast.PredMvSad[:4], vp9NonrdPickTraceLast.RefSkipMask,
		vp9NonrdPickTraceLast.MaxUsableRef, vp9NonrdPickTraceLast.ForceSkip,
		vp9NonrdPickTraceLast.SseZeromvNorm, vp9NonrdPickTraceLast.GoldenSkipFires,
		vp9NonrdPickTraceLast.PickSegID, vp9NonrdPickTraceLast.SegQIndex,
		vp9NonrdPickTraceLast.CyclicBoosted,
		vp9NonrdPickTraceLast.ActiveRDMult,
		vp9NonrdPickTraceLast.LastNearScore,
		vp9NonrdPickTraceLast.LastNearRate, vp9NonrdPickTraceLast.LastNearDist,
		vp9NonrdPickTraceLast.LastNearSSE, vp9NonrdPickTraceLast.LastNearSkip,
		vp9NonrdPickTraceLast.LastNewScore,
		vp9NonrdPickTraceLast.GoldenNewScore,
		vp9NonrdPickTraceLast.GoldenNewRate, vp9NonrdPickTraceLast.GoldenNewDist,
		vp9NonrdPickTraceLast.GoldenNewSSE, vp9NonrdPickTraceLast.GoldenNewSkip,
		vp9NonrdPickTraceLast.LastNearestMv, vp9NonrdPickTraceLast.LastNearMv,
		vp9NonrdPickTraceLast.GoldenNewMv,
		vp9NonrdPickTraceLast.BestRef, vp9NonrdPickTraceLast.BestMode,
		vp9NonrdPickTraceLast.BestScore)
	for _, idx := range []int{0, 1, 2, 3, 4, 5} {
		g, l := gGrid[idx], lGrid[idx]
		t.Logf("mi[%d] sbtype=%d/%d seg=%d/%d mode=%d/%d ref=%v/%v filter=%d/%d",
			idx, g.SbType, l.SbType, g.SegmentID, l.SegmentID,
			g.Mode, l.Mode, g.RefFrame, l.RefFrame,
			g.InterpFilter, l.InterpFilter)
	}
	var firstMode, firstRef int = -1, -1
	for i := range gGrid {
		if gGrid[i].Mode != lGrid[i].Mode || gGrid[i].RefFrame != lGrid[i].RefFrame {
			if firstMode < 0 {
				firstMode, firstRef = i, i
			}
		}
	}
	t.Logf("first mode/ref mismatch at mi[%d] (mode) mi[%d] (ref)", firstMode, firstRef)
	for i := range min(8, len(gGrid)) {
		if gGrid[i].Mode != lGrid[i].Mode || gGrid[i].RefFrame != lGrid[i].RefFrame {
			t.Logf("mi[%d] mode=%d/%d ref=%v/%v mv=%v/%v",
				i, gGrid[i].Mode, lGrid[i].Mode,
				gGrid[i].RefFrame, lGrid[i].RefFrame,
				gGrid[i].Mv, lGrid[i].Mv)
		}
	}
}
