package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestInterModeForRDLoopEntryAllowsZeroNewMVOnFlatMatch(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	e.modeProbs.MV = vp8tables.DefaultMVContext
	fillBenchmarkVP8Image(&e.analysis.Img, 72, 90, 170)
	e.analysis.ExtendBorders()

	src := testImage(16, 16)
	fillImage(src, 72, 90, 170)
	last := testVP8Frame(t, 16, 16, 72, 90, 170)
	ref := interAnalysisReference{Frame: vp8common.LastFrame, Img: &last.Img}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
		start    interFrameSearchStart
	}

	mode, ok := e.interModeForRDLoopEntry(sourceImageFromPublic(src), ref, 0, vp8common.NewMV, 0, 0, 1, 1, testInterSearchQIndex, nil, nil, nil, &newMVCandidates, nil)
	if !ok {
		t.Fatalf("RD NEWMV loop entry rejected zero MV on flat matching frame")
	}
	if mode.Mode != vp8common.NewMV || mode.RefFrame != vp8common.LastFrame || !mode.MV.IsZero() {
		t.Fatalf("RD NEWMV loop entry mode = %+v, want LAST/NEWMV with zero MV", mode)
	}
}

func TestSelectRDInterFrameMotionVectorAllowsSubpixelRefinementWithBestRefMVCost(t *testing.T) {
	src := testImage(48, 48)
	fillImage(src, 0, 90, 170)
	ref := testVP8Frame(t, 48, 48, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = byte((23 + row*11 + col*7 + row*col*5) & 0xff)
		}
	}
	ref.ExtendBorders()
	refStart := ref.Img.YOrigin + 16*ref.Img.YStride + 16
	dsp.BilinearPredict16x16(ref.Img.YFull[refStart:], ref.Img.YStride, 2, 2, src.Y[16*src.YStride+16:], src.YStride)
	bestRefMV := vp8enc.MotionVector{Row: 2, Col: 2}

	mv, cost := selectRDInterFrameMotionVectorWithSearchStart(sourceImageFromPublic(src), &ref.Img, 1, 1, 3, 3, bestRefMV, testInterSearchQIndex, defaultInterAnalysisSearchConfig(), interFrameSearchStart{}, &vp8tables.DefaultMVContext)

	if mv != bestRefMV {
		t.Fatalf("RD NEWMV search MV = %+v, want accepted subpel refinement %+v", mv, bestRefMV)
	}
	want := interMotionSearchErrorVectorCost(mv, bestRefMV, testInterSearchQIndex, &vp8tables.DefaultMVContext)
	if cost != want {
		t.Fatalf("RD NEWMV search cost = %d, want best_ref_mv anchored subpel cost %d", cost, want)
	}
	if zeroAnchor := interMotionSearchErrorVectorCost(mv, vp8enc.MotionVector{}, testInterSearchQIndex, &vp8tables.DefaultMVContext); cost == zeroAnchor {
		t.Fatalf("RD NEWMV search cost = zero-anchor cost %d, want best_ref_mv anchor", zeroAnchor)
	}
}
