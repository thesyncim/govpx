package encoder

import "github.com/thesyncim/govpx/internal/pixel"

// MacroblockMeanLumaSSE mirrors the libvpx v1.16.0 VP8 first-pass fallback
// intra_error estimate for a 16x16 macroblock.
func MacroblockMeanLumaSSE(src SourceImage, mbRow int, mbCol int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16

	if pixel.Enabled && src.Width >= 16 && src.Height >= 16 && uint(baseX) <= uint(src.Width-16) && uint(baseY) <= uint(src.Height-16) && src.YStride >= src.Width && len(src.Y) >= baseX+16 && baseY+15 <= (len(src.Y)-baseX-16)/src.YStride {
		var zero [16]byte
		sum, sse := pixel.Stats(src.Y[baseY*src.YStride+baseX:], src.YStride, zero[:], 0, 16, 16)
		v := int(sse) - int((int64(sum)*int64(sum)+128)>>8)
		return max(v, 0)
	}
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampSourceCoord(baseY+row, src.Height)
		for col := range 16 {
			srcX := clampSourceCoord(baseX+col, src.Width)
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

func clampSourceCoord(v int, limit int) int {
	return min(max(v, 0), limit-1)
}
