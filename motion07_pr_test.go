// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func motion07ParentIterativeInterFrameSplitBlockSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, errorPerBit int, mvProbs *[2][vp8tables.MVPCount]uint8, mvCosts *vp8enc.MotionVectorCostTables) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	bestDist, ok := splitBlockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, block, width, height, br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCostWithErrorPerBitAndCostTables(bestMV, bestRefMV, errorPerBit, mvProbs, mvCosts)
	mbRows := (src.Height + 15) >> 4
	mbCols := (src.Width + 15) >> 4
	bounds := vp8enc.InterFrameSubpelSearchBoundsFor(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	_ = qIndex
	var cache subpelEvalCache
	cand := func(r, c int) int {
		return cachedBoundedSplitBlockSubpixelMotionSearchCandidateCost(&cache, bounds, src, ref, mbRow, mbCol, block, width, height, r, c, bestRefMV, errorPerBit, mvProbs, mvCosts)
	}

	for range 3 {
		leftCost := cand(tr, tc-2)
		rightCost := cand(tr, tc+2)
		upCost := cand(tr-2, tc)
		downCost := cand(tr+2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for range 3 {
		leftCost := cand(tr, tc-1)
		rightCost := cand(tr, tc+1)
		upCost := cand(tr-1, tc)
		downCost := cand(tr+1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost := cand(diagRow, diagCol)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !vp8enc.InterFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}
func motion07PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
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
func TestMotion07PRSubpelSource(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {64, 64}} {
		s, r := motion07PixelSetup(t, sz[0], sz[1])
		var costs vp8enc.MotionVectorCostTables
		costs.Build(&vp8tables.DefaultMVContext)
		for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
			for _, block := range []int{0, 5, 15} {
				for _, center := range []vp8enc.MotionVector{{}, {Row: 8, Col: -8}, {Row: -32, Col: 16}} {
					y, x := (s.Height-1)/16, (s.Width-1)/16
					a, b, c := iterativeInterFrameSplitBlockSubpixelMotionVector(s, r, y, x, block, shape[0], shape[1], center, vp8enc.MotionVector{}, 44, 34, &vp8tables.DefaultMVContext, &costs)
					d, e, f := motion07ParentIterativeInterFrameSplitBlockSubpixelMotionVector(s, r, y, x, block, shape[0], shape[1], center, vp8enc.MotionVector{}, 44, 34, &vp8tables.DefaultMVContext, &costs)
					if a != d || b != e || c != f {
						t.Fatal(sz, shape, block, center, a, b, c, d, e, f)
					}
				}
			}
		}
	}
}

var motion07PRInt int
var motion07PRMV vp8enc.MotionVector

func BenchmarkMotion07PRSubpelSource(b *testing.B) {
	for _, shape := range [][2]int{{8, 8}, {4, 4}} {
		b.Run(fmt.Sprintf("partial_%dx%d", shape[0], shape[1]), func(b *testing.B) {
			s, r := motion07PixelSetup(b, 65, 49)
			var costs vp8enc.MotionVectorCostTables
			costs.Build(&vp8tables.DefaultMVContext)
			b.ReportAllocs()
			for b.Loop() {
				mv, c, ok := iterativeInterFrameSplitBlockSubpixelMotionVector(s, r, 3, 4, 0, shape[0], shape[1], vp8enc.MotionVector{}, vp8enc.MotionVector{}, 44, 34, &vp8tables.DefaultMVContext, &costs)
				motion07PRInt = c
				if !ok {
					b.Fatal("invalid")
				}
				motion07PRMV = mv
			}
		})
	}
}
