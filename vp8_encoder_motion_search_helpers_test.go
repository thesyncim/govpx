package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func newTestInterFrameSubpixelStepSearch(tb testing.TB, fractional interAnalysisFractionalSearchMethod) interFrameSubpixelSearch {
	tb.Helper()
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[(row+16)*src.YStride+col+16] = byte((19 + row*73 + col*151 + row*col*37) & 255)
		}
	}

	last := testVP8Frame(tb, 64, 64, 127, 90, 170)
	for row := range 16 {
		for col := range 16 {
			v := src.Y[(row+16)*src.YStride+col+16]
			last.Img.Y[(row+20)*last.Img.YStride+col+16] = v
			last.Img.Y[(row+20)*last.Img.YStride+col+17] = v ^ 1
		}
	}
	last.ExtendBorders()

	return interFrameSubpixelSearch{
		ref:       &last.Img,
		mvProbs:   &vp8tables.DefaultMVContext,
		src:       sourceImageFromPublic(src),
		mbRow:     1,
		mbCol:     1,
		qIndex:    testInterSearchQIndex,
		best:      vp8enc.MotionVector{Row: 32},
		bestRefMV: vp8enc.MotionVector{},
		search:    interAnalysisSearchConfig{fractionalSearch: fractional},
	}
}

func scalarFullPelClampedMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int) (int, int) {
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}
