package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

// BPredBlockSSE returns the source-vs-predictor SSE for one VP8 B_PRED 4x4
// luma block, clamping source reads at visible edges like libvpx v1.16.0.
func BPredBlockSSE(src SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	if src.Width >= 4 && src.Height >= 4 && uint(baseX) <= uint(src.Width-4) && uint(baseY) <= uint(src.Height-4) && src.YStride >= src.Width && predStride >= 4 && len(src.Y) >= baseX+4 && len(pred) >= 4 && baseY+3 <= (len(src.Y)-baseX-4)/src.YStride && predStride <= (len(pred)-4)/3 {
		return dsp.SSE4x4(src.Y[baseY*src.YStride+baseX:], src.YStride, pred, predStride)
	}
	sse := 0
	for row := range 4 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col])
			sse += diff * diff
		}
	}
	return sse
}

// FillBPredResidual4x4 writes source-predictor residuals for one VP8 B_PRED
// luma block into row-major 4x4 coefficient input order.
func FillBPredResidual4x4(src SourceImage, mbRow int, mbCol int, block int, pred []byte, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	if src.Width >= 4 && src.Height >= 4 && uint(baseX) <= uint(src.Width-4) && uint(baseY) <= uint(src.Height-4) && src.YStride >= src.Width && len(src.Y) >= baseX+4 && len(pred) >= 16 && baseY+3 <= (len(src.Y)-baseX-4)/src.YStride {
		pixel.Subtract(src.Y[baseY*src.YStride+baseX:], src.YStride, pred, 4, out[:], 4, 4, 4)
		return
	}
	for row := range 4 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*4+col]))
		}
	}
}

// CopyBPredBlock copies one row-major 4x4 predictor/reconstruction block into
// its scan-order position within a 16x16 luma macroblock.
func CopyBPredBlock(src []byte, dst []byte, dstStride int, block int) {
	y := (block >> 2) * 4
	x := (block & 3) * 4
	for row := range 4 {
		copy(dst[(y+row)*dstStride+x:], src[row*4:row*4+4])
	}
}

// BPredBlockStats preserves the selected predictor statistics for macroblock variance.
func BPredBlockStats(src SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int) (int, int) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	if src.Width >= 4 && src.Height >= 4 && uint(baseX) <= uint(src.Width-4) && uint(baseY) <= uint(src.Height-4) && src.YStride >= src.Width && predStride >= 4 && len(src.Y) >= baseX+4 && len(pred) >= 4 && baseY+3 <= (len(src.Y)-baseX-4)/src.YStride && predStride <= (len(pred)-4)/3 {
		return dsp.PixelStats4x4(src.Y[baseY*src.YStride+baseX:], src.YStride, pred, predStride)
	}
	sum, sse := 0, 0
	for row := range 4 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 4 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}
