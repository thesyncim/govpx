package encoder

// MacroblockMeanLumaSSE mirrors the libvpx v1.16.0 VP8 first-pass fallback
// intra_error estimate for a 16x16 macroblock.
func MacroblockMeanLumaSSE(src SourceImage, mbRow int, mbCol int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
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
