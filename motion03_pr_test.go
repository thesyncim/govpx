// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func motion03ParentMacroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	// Uint range collapses (base >= 0) and (base+16 <= dim) into one
	// compare per dimension (works when dim >= 16; smaller dims fall
	// through to the per-pixel clamped path).
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if sse, ok := macroblockSubpixelSSEBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return sse
			}
		}
	}
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refSlice, ok := refFullPelYSlice(ref, refBaseY, refBaseX, 16, 16); ok {
			return dsp.SSE16x16Fast(src.Y[baseY*src.YStride+baseX:], src.YStride, refSlice, ref.YStride)
		}
	}

	sse := 0
	for row := range 16 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := range 16 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}
func motion03ParentMacroblockLumaMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, 16, 16, srcScratch[:], 16)
			if variance, sse, ok := macroblockSubpixelVarianceBlock(ref, refBaseY, refBaseX, xOffset, yOffset, srcScratch[:], 16); ok {
				return variance, sse
			}
		}
	}
	// Uint range collapses (baseY/X >= 0) and (baseY/X+16 <= dim) into
	// one compare each, matching the pattern in macroblockLumaSSE.
	if uint(baseY) <= uint(src.Height-16) && uint(baseX) <= uint(src.Width-16) {
		if refSlice, ok := refFullPelYSlice(ref, refBaseY, refBaseX, 16, 16); ok {
			sum, sse := dsp.VarianceBlock16x16Fast(src.Y[baseY*src.YStride+baseX:], src.YStride, refSlice, ref.YStride)
			return sse - ((sum * sum) >> 8), sse
		}
	}

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
func motion03PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
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
func TestMotion03PRMoments(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {33, 48}, {64, 64}} {
		s, r := motion03PixelSetup(t, sz[0], sz[1])
		for y := 0; y < (s.Height+15)/16; y++ {
			for x := 0; x < (s.Width+15)/16; x++ {
				for _, mv := range []vp8enc.MotionVector{{}, {Row: 8, Col: -8}, {Row: -32, Col: 16}, {Row: 2, Col: 4}} {
					got := macroblockLumaSSE(s, r, y, x, mv)
					want := motion03ParentMacroblockLumaSSE(s, r, y, x, mv)
					a, b := macroblockLumaMotionVarianceSSE(s, r, y, x, mv)
					c, d := motion03ParentMacroblockLumaMotionVarianceSSE(s, r, y, x, mv)
					if got != want || a != c || b != d {
						t.Fatalf("size %v yx %d %d mv %v got %d %d %d want %d %d %d", sz, y, x, mv, got, a, b, want, c, d)
					}
				}
			}
		}
	}
}

var motion03PRInt int

func BenchmarkMotion03PRMoments(b *testing.B) {
	for _, which := range []string{"SSE", "variance"} {
		b.Run("partial_"+which, func(b *testing.B) {
			s, r := motion03PixelSetup(b, 65, 49)
			b.ReportAllocs()
			for b.Loop() {
				if which == "SSE" {
					motion03PRInt = macroblockLumaSSE(s, r, 3, 4, vp8enc.MotionVector{})
				} else {
					a, v := macroblockLumaMotionVarianceSSE(s, r, 3, 4, vp8enc.MotionVector{})
					motion03PRInt = a + v
				}
			}
		})
	}
}
