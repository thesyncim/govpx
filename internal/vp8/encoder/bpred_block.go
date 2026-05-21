package encoder

// BPredBlockSSE returns the source-vs-predictor SSE for one VP8 B_PRED 4x4
// luma block, clamping source reads at visible edges like libvpx v1.16.0.
func BPredBlockSSE(src SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
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
