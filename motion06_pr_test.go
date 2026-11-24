// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func motion06ParentFillSplitMotionResidual4x4(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred *[16]byte, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	for row := range 4 {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*4+col]))
		}
	}
}
func motion06PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
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
func TestMotion06PRResidual(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {64, 64}} {
		s, _ := motion06PixelSetup(t, sz[0], sz[1])
		var p [16]byte
		for i := range p {
			p[i] = byte(i * 13)
		}
		for y := 0; y < (s.Height+15)/16; y++ {
			for x := 0; x < (s.Width+15)/16; x++ {
				for block := 0; block < 16; block++ {
					var a, b [16]int16
					fillSplitMotionResidual4x4(s, y, x, block, &p, &a)
					motion06ParentFillSplitMotionResidual4x4(s, y, x, block, &p, &b)
					if a != b {
						t.Fatal(sz, y, x, block)
					}
				}
			}
		}
	}
}

var motion06PRResidual [16]int16

func BenchmarkMotion06PRResidual(b *testing.B) {
	s, _ := motion06PixelSetup(b, 64, 64)
	var p [16]byte
	for i := range p {
		p[i] = byte(i * 13)
	}
	b.ReportAllocs()
	for b.Loop() {
		fillSplitMotionResidual4x4(s, 1, 1, 5, &p, &motion06PRResidual)
	}
}
