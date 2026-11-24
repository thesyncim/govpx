// Frozen reference: 870699ce83f3eb6de44f507ef29f6d6a429e916d.
package govpx

import (
	"fmt"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func motion04ParentSplitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
			if sad, ok := splitBlockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, width, height, xOffset, yOffset); ok {
				return sad
			}
		} else {
			var srcScratch [16 * 16]byte
			vp8enc.GatherClampedLumaBlock(src, baseY, baseX, width, height, srcScratch[:], 16)
			if sad, ok := splitBlockSubpixelSADBlock(ref, refBaseY, refBaseX, width, height, xOffset, yOffset, srcScratch[:], 16); ok {
				return sad
			}
		}
	}
	if uint(baseY) <= uint(src.Height-height) && uint(baseX) <= uint(src.Width-width) {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock, ok := refFullPelYSlice(ref, refBaseY, refBaseX, width, height)
		if ok {
			switch {
			case width == 16 && height == 8:
				return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 16:
				return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 8 && height == 8:
				return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
			case width == 4 && height == 4:
				return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
			}
		}
	}

	// libvpx allocates every reference (cm->yv12_fb[]) and lookahead source
	// buffer with 16-aligned width/height via vp8_yv12_alloc_frame_buffer
	// (vp8/common/alloccommon.c:56-65 rounds (width|height) up to a
	// multiple of 16 before calling vp8_yv12_alloc_frame_buffer, and
	// vp8/encoder/lookahead.c:68-70 does the same), so for an odd-axis
	// frame y_crop_height == y_height == coded height (e.g. 368 for a
	// visible 360 input). The post-loop-filter publication path
	// (vp8/encoder/onyx_if.c:3212 vp8_yv12_extend_frame_borders applied to
	// cm->frame_to_show) therefore extends from coded-height-1 (not from
	// visible-height-1), and the coded-but-invisible MB-padded rows/cols
	// retain the LF-modified reconstruction. The SPLITMV picker's NEW4X4
	// SAD walk on padded-edge MBs must read from that region the same way
	// libvpx does — i.e. clamp reference coords to CodedHeight/CodedWidth,
	// not to the visible extent. The source side, by contrast, comes from
	// vp8_copy_and_extend_frame (vp8/common/extend.c:14-72, called from
	// lookahead.c:139) which is a visible-edge extend, so source coords
	// remain clamped to visible Width/Height.
	refClampHeight := ref.CodedHeight
	refClampWidth := ref.CodedWidth
	sad := 0
	for row := range height {
		srcY := vp8enc.ClampEncodeCoord(baseY+row, src.Height)
		refY := vp8enc.ClampEncodeCoord(refBaseY+row, refClampHeight)
		for col := range width {
			srcX := vp8enc.ClampEncodeCoord(baseX+col, src.Width)
			refX := vp8enc.ClampEncodeCoord(refBaseX+col, refClampWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			mask := diff >> mvKernelSignShift
			sad += (diff ^ mask) - mask
		}
	}
	return sad
}
func motion04PixelSetup(tb testing.TB, w, h int) (vp8enc.SourceImage, *vp8common.Image) {
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
func TestMotion04PRSplitSAD(t *testing.T) {
	for _, sz := range [][2]int{{65, 49}, {17, 17}, {33, 48}, {64, 64}} {
		s, r := motion04PixelSetup(t, sz[0], sz[1])
		for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
			for _, block := range []int{0, 5, 15} {
				for _, mv := range []vp8enc.MotionVector{{}, {Row: 8, Col: -8}, {Row: -32, Col: 16}, {Row: 2, Col: 4}} {
					y, x := (s.Height-1)/16, (s.Width-1)/16
					got := splitBlockSAD(s, r, y, x, block, shape[0], shape[1], mv)
					want := motion04ParentSplitBlockSAD(s, r, y, x, block, shape[0], shape[1], mv)
					if got != want {
						t.Fatalf("size %v shape %v block %d mv %v got %d want %d", sz, shape, block, mv, got, want)
					}
				}
			}
		}
	}
}

var motion04PRInt int

func BenchmarkMotion04PRSplitSAD(b *testing.B) {
	for _, shape := range [][2]int{{16, 8}, {8, 16}, {8, 8}, {4, 4}} {
		b.Run(fmt.Sprintf("partial_%dx%d", shape[0], shape[1]), func(b *testing.B) {
			s, r := motion04PixelSetup(b, 65, 49)
			b.ReportAllocs()
			for b.Loop() {
				motion04PRInt = splitBlockSAD(s, r, 3, 4, 0, shape[0], shape[1], vp8enc.MotionVector{})
			}
		})
	}
}
