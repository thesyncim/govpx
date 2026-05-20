package govpx

import vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"

func macroblockMeanLumaSSE(src vp8enc.SourceImage, mbRow int, mbCol int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			v := int(src.Y[srcY*src.YStride+srcX])
			sum += v
			sse += v * v
		}
	}
	variance := sse - int((int64(sum)*int64(sum)+128)>>8)
	if variance < 0 {
		return 0
	}
	return variance
}
