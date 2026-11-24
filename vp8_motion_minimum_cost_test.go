package govpx

import (
	"fmt"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// The zero-delta MV-SAD entry is the global minimum, following libvpx's
// cal_mvsadcosts (300 at zero; logarithmic entries at nonzero deltas).
func TestVP8Algorithm2MinimumMotionCostBound(t *testing.T) {
	for q, costs := range vp8enc.FullPelMVSADComponentCost16 {
		floor := vp8enc.FullPelMVSADCost16FromDeltas(0, 0, 0, 0, q)
		if floor != (costs[0]+costs[0]+128)>>8 {
			t.Fatalf("q%d: floor %d", q, floor)
		}
		for delta, cost := range costs {
			if cost < costs[0] {
				t.Fatalf("q%d delta%d: %d < zero cost %d", q, delta, cost, costs[0])
			}
			if got := vp8enc.FullPelMVSADCost16FromDeltas(-delta, delta, 0, 0, q); got < floor {
				t.Fatalf("q%d delta%d: MV cost %d < floor %d", q, delta, got, floor)
			}
		}
	}
}

func astraMinimumMotionFixture(tb testing.TB, pattern string) interFrameMotionVectorSearch {
	tb.Helper()
	w, h, mbRow, mbCol := 64, 64, 1, 1
	if pattern == "partial" {
		w, h, mbRow, mbCol = 49, 35, 2, 3
	}
	src := testImage(w, h)
	fillImage(src, 113, 90, 170)
	ref := testVP8Frame(tb, w, h, 113, 90, 170)
	bestRef := vp8enc.MotionVector{}
	if pattern == "shifted" {
		bestRef = vp8enc.MotionVector{Row: 16, Col: -24}
	} else if pattern == "fractional" {
		bestRef = vp8enc.MotionVector{Row: 7, Col: -1}
	}
	if pattern != "flat" && pattern != "partial" {
		for y := range h {
			for x := range w {
				ref.Img.Y[y*ref.Img.YStride+x] = byte((y*y*7 + x*x*13 + x*y*19 + y*29 + x*31) & 255)
			}
		}
		ref.ExtendBorders()
		for y := range h {
			for x := range w {
				ry := max(0, min(h-1, y+(int(bestRef.Row)>>3)))
				rx := max(0, min(w-1, x+(int(bestRef.Col)>>3)))
				v := ref.Img.Y[ry*ref.Img.YStride+rx]
				if pattern == "dense" {
					v ^= byte((y*41 + x*53 + 71) & 255)
				}
				src.Y[y*src.YStride+x] = v
			}
		}
	}
	var mvCosts vp8enc.MotionVectorCostTables
	mvCosts.Build(&vp8tables.DefaultMVContext)
	s := interFrameMotionVectorSearch{
		src: sourceImageFromPublic(src), ref: &ref.Img,
		mbRow: mbRow, mbCol: mbCol, mbRows: (h + 15) / 16, mbCols: (w + 15) / 16,
		bestRefMV: bestRef, qIndex: 63, mvProbs: &vp8tables.DefaultMVContext, mvCosts: &mvCosts,
		search: interAnalysisSearchConfig{fullPixelSearch: interAnalysisFullPixelSearchHex,
			fullPixelSearchParam: 6, fullPixelFurtherSteps: 1, fractionalSearch: interAnalysisFractionalSearchStep},
	}
	if pattern == "improved" {
		s.start = newInterFrameSearchStart(vp8enc.MotionVector{Row: 8, Col: -8}, 7, 1)
	}
	return s
}

func astraFrozenMinimumMotionFast(s interFrameMotionVectorSearch) interFrameMotionVectorSearchResult {
	best, bestCost := astraFrozenMinCostSearch(s.src, s.ref, s.mbRow, s.mbCol, s.mbRows, s.mbCols,
		s.bestRefMV, s.qIndex, s.errorPerBit, s.search, s.start, s.mvProbs, s.mvCosts)
	if bestCost == 0 {
		return interFrameMotionVectorSearchResult{mv: best, cost: bestCost, haveError: true}
	}
	subpel := s.subpixel(best)
	if refined, refinedCost, variance, sse, ok := subpel.refine(); ok {
		return interFrameMotionVectorSearchResult{mv: refined, cost: refinedCost, variance: variance, sse: sse, haveError: true}
	}
	return interFrameMotionVectorSearchResult{mv: best, cost: bestCost}
}

func TestVP8Algorithm2MinimumMotionCostFrozen(t *testing.T) {
	for _, pattern := range []string{"flat", "exact", "shifted", "fractional", "partial", "dense", "improved"} {
		s := astraMinimumMotionFixture(t, pattern)
		for _, method := range []interAnalysisFullPixelSearchMethod{interAnalysisFullPixelSearchNstep, interAnalysisFullPixelSearchDiamond, interAnalysisFullPixelSearchHex} {
			s.search.fullPixelSearch = method
			for q := -1; q <= 128; q++ {
				s.qIndex = q
				s.search.fullPixelFinalRefine = q&1 != 0
				s.search.fullPixelSearchParam = int8(2 + q%6)
				s.search.fullPixelFurtherSteps = int8(q % 4)
				gotMV, gotCost := s.fullPixel()
				wantMV, wantCost := astraFrozenMinCostSearch(s.src, s.ref, s.mbRow, s.mbCol, s.mbRows, s.mbCols,
					s.bestRefMV, s.qIndex, s.errorPerBit, s.search, s.start, s.mvProbs, s.mvCosts)
				if gotMV != wantMV || gotCost != wantCost {
					t.Fatalf("%s method%d q%d: got %v/%d want %v/%d", pattern, method, q, gotMV, gotCost, wantMV, wantCost)
				}
				if got, want := s.selectFast(), astraFrozenMinimumMotionFast(s); got != want {
					t.Fatalf("%s method%d q%d fast: got %+v want %+v", pattern, method, q, got, want)
				}
			}
		}
		// Nil/custom MV-error costing must preserve each method's final cost;
		// the independent MV-SAD floor remains unchanged.
		s.mvProbs, s.mvCosts, s.errorPerBit = nil, nil, 37
		for _, method := range []interAnalysisFullPixelSearchMethod{interAnalysisFullPixelSearchExhaustive, interAnalysisFullPixelSearchNstep, interAnalysisFullPixelSearchDiamond, interAnalysisFullPixelSearchHex, 255} {
			s.search.fullPixelSearch, s.qIndex = method, 63
			gotMV, gotCost := s.fullPixel()
			wantMV, wantCost := astraFrozenMinCostSearch(s.src, s.ref, s.mbRow, s.mbCol, s.mbRows, s.mbCols,
				s.bestRefMV, s.qIndex, s.errorPerBit, s.search, s.start, s.mvProbs, s.mvCosts)
			if gotMV != wantMV || gotCost != wantCost {
				t.Fatalf("%s custom cost method%d: got %v/%d want %v/%d", pattern, method, gotMV, gotCost, wantMV, wantCost)
			}
		}
	}
}

var astraMinimumMotionSink interFrameMotionVectorSearchResult

func TestVP8Algorithm2MinimumMotionCostAllocs(t *testing.T) {
	for _, pattern := range []string{"exact", "dense", "partial"} {
		s := astraMinimumMotionFixture(t, pattern)
		if n := testing.AllocsPerRun(50, func() { astraMinimumMotionSink = s.selectFast() }); n != 0 {
			t.Fatalf("%s: %v allocations", pattern, n)
		}
	}
}

func BenchmarkAstraVP8MinimumMotionCost(b *testing.B) {
	for _, method := range []interAnalysisFullPixelSearchMethod{interAnalysisFullPixelSearchNstep, interAnalysisFullPixelSearchDiamond, interAnalysisFullPixelSearchHex} {
		for _, pattern := range []string{"flat", "exact", "shifted", "partial", "dense", "improved"} {
			b.Run(fmt.Sprintf("fullpel/method%d/%s", method, pattern), func(b *testing.B) {
				s := astraMinimumMotionFixture(b, pattern)
				s.search.fullPixelSearch = method
				var mv vp8enc.MotionVector
				var cost int
				b.ReportAllocs()
				for b.Loop() {
					mv, cost = s.fullPixel()
				}
				astraMinimumMotionSink = interFrameMotionVectorSearchResult{mv: mv, cost: cost}
			})
		}
	}
	for _, pattern := range []string{"flat", "exact", "dense", "improved"} {
		b.Run("fast/hex/"+pattern, func(b *testing.B) {
			s := astraMinimumMotionFixture(b, pattern)
			var result interFrameMotionVectorSearchResult
			b.ReportAllocs()
			for b.Loop() {
				result = s.selectFast()
			}
			astraMinimumMotionSink = result
		})
	}
}

func astraFrozenMinCostSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, errorPerBit int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables) (vp8enc.MotionVector, int) {
	searchStart := bestRefMV
	if start.ok() && search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		searchStart = start.mv
		search = search.adjustedForImprovedMVStart(start)
	}
	centerRow := int(searchStart.Row) & ^7
	centerCol := int(searchStart.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	bounds := vp8enc.InterFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	if search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		best = bounds.ClampEighth(best)
	}
	searcher := newFullPelMotionSearch(src, ref, mbRow, mbCol, bestRefMV, qIndex, bounds, mvProbs, mvCosts, errorPerBit, nil)
	bestWalkCost := searcher.walkCostNoStats(best, maxInt())
	if bestWalkCost == 0 {
		return best, searcher.costNoStats(best)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchNstep {
		return searcher.nstepNoStats(best, bestWalkCost, search)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchDiamond {
		return searcher.diamondNoStats(best, bestWalkCost, search)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchHex {
		return searcher.hexNoStats(best, bestWalkCost)
	}
	return searcher.exhaustiveNoStats(best, bestWalkCost)
}
