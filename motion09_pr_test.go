// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func motion09ParentDiamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(search *splitFullPelSearchCtx, center vp8enc.MotionVector, centerWalkCost int, centerReturnCost int, bestRefMV vp8enc.MotionVector, refRowFull int, refColFull int, qIndex int, errorPerBit int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, bounds vp8enc.InterFrameFullPixelBounds, searchParam int) splitFullPixelSearchResult {
	sites := vp8enc.InterFrameNstepSearchSites[:]
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
	best := center
	bestWalkCost := centerWalkCost
	start := center
	startIndex := searchParam * 8
	totalSteps := (len(sites) / 8) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	for range totalSteps {
		for range 8 {
			site := sites[startIndex+i]
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.ContainsFullPelStrict(row, col) {
				cost := search.fullPelCostFull(row, col, refRowFull, refColFull, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = i
				}
			}
			i++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			best = vp8enc.MotionVector{
				Row: int16(int(best.Row) + int(site.Row)*interFrameMVFullPixelStep),
				Col: int16(int(best.Col) + int(site.Col)*interFrameMVFullPixelStep),
			}
			lastSite = bestSite
		} else if best == start {
			num00++
		}
	}
	returnCost := centerReturnCost
	if best != center {
		var ok bool
		returnCost, ok = interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(search.src, search.ref, search.mbRow, search.mbCol, search.block, search.width, search.height, best, bestRefMV, errorPerBit, mvProbs, mvCosts)
		if !ok {
			returnCost = maxInt()
		}
	}
	return splitFullPixelSearchResult{mv: best, walkCost: bestWalkCost, returnCost: returnCost, num00: uint8(num00)}
}
func motion09ParentFullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(search *splitFullPelSearchCtx, center vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, refRowFull int, refColFull int, qIndex int, errorPerBit int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables, bounds vp8enc.InterFrameFullPixelBounds, distance int) splitFullPixelSearchResult {
	refRow := int(center.Row) >> 3
	refCol := int(center.Col) >> 3
	rowMin := refRow - distance
	rowMax := refRow + distance
	colMin := refCol - distance
	colMax := refCol + distance
	if rowMin < bounds.RowMin {
		rowMin = bounds.RowMin
	}
	if rowMax > bounds.RowMax {
		rowMax = bounds.RowMax
	}
	if colMin < bounds.ColMin {
		colMin = bounds.ColMin
	}
	if colMax > bounds.ColMax {
		colMax = bounds.ColMax
	}
	best := center
	bestWalkCost := search.fullPelCostFull(int(best.Row)>>3, int(best.Col)>>3, refRowFull, refColFull, qIndex)
	for row := rowMin; row < rowMax; row++ {
		for col := colMin; col < colMax; col++ {
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := search.fullPelCostFull(row, col, refRowFull, refColFull, qIndex)
			if cost < bestWalkCost {
				best = mv
				bestWalkCost = cost
			}
		}
	}
	returnCost, ok := interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(search.src, search.ref, search.mbRow, search.mbCol, search.block, search.width, search.height, best, bestRefMV, errorPerBit, mvProbs, mvCosts)
	if !ok {
		returnCost = maxInt()
	}
	return splitFullPixelSearchResult{mv: best, walkCost: bestWalkCost, returnCost: returnCost}
}
func motion09PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
	src := testImage(w, h)
	for i := range src.Y {
		src.Y[i] = byte(i*17 + i/11)
	}
	ref := testVP8Frame(tb, w, h, 10, 90, 170)
	for i := range ref.Img.Y {
		ref.Img.Y[i] = byte(i*23 + i/7)
	}
	ref.ExtendBorders()
	return sourceImageFromPublic(src), &ref.Img
}
func TestMotion09PRSplitRateBound(t *testing.T) {
	for _, sz := range [][2]int{{64, 64}, {65, 49}} {
		src, ref := motion09PixelSetup(t, sz[0], sz[1])
		var costs vp8enc.MotionVectorCostTables
		costs.Build(&vp8tables.DefaultMVContext)
		for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
			for _, q := range []int{0, 44, 127} {
				y, x := (src.Height-1)/16, (src.Width-1)/16
				c1 := newSplitFullPelSearchCtx(src, ref, y, x, 0, shape[0], shape[1])
				c2 := c1
				bounds := vp8enc.InterFrameFullPixelSearchBounds(vp8enc.MotionVector{}, y, x, (src.Height+15)/16, (src.Width+15)/16)
				center := vp8enc.MotionVector{}
				a := fullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c1, center, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, 4)
				b := motion09ParentFullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c2, center, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, 4)
				if a != b {
					t.Fatal("full", sz, shape, q, a, b)
				}
				wc := c1.fullPelCostFull(0, 0, 0, 0, q)
				rc, ok := interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(src, ref, y, x, 0, shape[0], shape[1], center, center, 34, &vp8tables.DefaultMVContext, &costs)
				if !ok {
					t.Fatal("returncost")
				}
				for param := 0; param < 8; param++ {
					a := diamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c1, center, wc, rc, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, param)
					b := motion09ParentDiamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&c2, center, wc, rc, center, 0, 0, q, 34, &vp8tables.DefaultMVContext, &costs, bounds, param)
					if a != b {
						t.Fatal("diamond", sz, shape, q, param, a, b)
					}
				}
			}
		}
	}
}

var motion09PRInt int
var motion09PRMV vp8enc.MotionVector

func BenchmarkMotion09PRSplitRateBound(b *testing.B) {
	for _, shape := range [][2]int{{8, 8}, {4, 4}} {
		for _, which := range []string{"full", "nstep"} {
			b.Run(fmt.Sprintf("%s_%dx%d", which, shape[0], shape[1]), func(b *testing.B) {
				src, ref := motion09PixelSetup(b, 128, 96)
				for y := 0; y < src.Height; y++ {
					for x := 0; x < src.Width; x++ {
						src.Y[y*src.YStride+x] = ref.Y[y*ref.YStride+min(x+1, ref.CodedWidth-1)]
					}
				}
				ctx := newSplitFullPelSearchCtx(src, ref, 2, 2, 0, shape[0], shape[1])
				var costs vp8enc.MotionVectorCostTables
				costs.Build(&vp8tables.DefaultMVContext)
				bounds := vp8enc.InterFrameFullPixelSearchBounds(vp8enc.MotionVector{}, 2, 2, 6, 8)
				center := vp8enc.MotionVector{}
				wc := ctx.fullPelCostFull(0, 0, 0, 0, 44)
				rc, _ := interMotionSplitBlockFullPixelReturnCostWithErrorPerBitAndCostTables(src, ref, 2, 2, 0, shape[0], shape[1], center, center, 34, &vp8tables.DefaultMVContext, &costs)
				b.ReportAllocs()
				for b.Loop() {
					var r splitFullPixelSearchResult
					if which == "full" {
						r = fullSearchInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&ctx, center, center, 0, 0, 44, 34, &vp8tables.DefaultMVContext, &costs, bounds, 6)
					} else {
						r = diamondNstepInterFrameSplitBlockFullPixelMotionVectorWithErrorPerBitAndCostTables(&ctx, center, wc, rc, center, 0, 0, 44, 34, &vp8tables.DefaultMVContext, &costs, bounds, 4)
					}
					motion09PRMV = r.mv
					motion09PRInt = r.returnCost + r.walkCost
				}
			})
		}
	}
}
