package govpx

import "image"

type vp9SceneCutFrameStats struct {
	Macroblocks       int
	ReferenceError    int64
	IntraError        int64
	IntraBetterBlocks int
	HighErrorBlocks   int
}

func (e *VP9Encoder) shouldEncodeVP9SceneCutKeyFrame(src *image.YCbCr,
	flags EncodeFlags, temporalEnabled bool, rows int, cols int,
) bool {
	if !e.opts.AdaptiveKeyFrames ||
		e.frameIndex == 0 ||
		e.twoPass.enabled() ||
		temporalEnabled ||
		!e.vp9AdaptiveKeyFrameMinDistanceMet() ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef) != 0 {
		return false
	}
	stats, ok := e.vp9OnePassSceneCutStats(src, flags, rows, cols)
	if !ok {
		return false
	}
	if stats.Macroblocks <= 0 ||
		stats.ReferenceError < int64(sceneCutMinimumReferenceSSEPerMB)*int64(stats.Macroblocks) {
		return false
	}
	if stats.IntraBetterBlocks*100 < stats.Macroblocks*sceneCutIntraWinPct {
		return false
	}
	if stats.HighErrorBlocks*100 < stats.Macroblocks*sceneCutHighErrorPct {
		return false
	}
	return stats.ReferenceError > stats.IntraError*sceneCutIntraErrorRatio
}

func (e *VP9Encoder) vp9AdaptiveKeyFrameMinDistanceMet() bool {
	minFrames := e.opts.MinKeyframeInterval
	if minFrames <= 0 {
		return true
	}
	return int(e.framesSinceKey)+1 >= minFrames
}

func (e *VP9Encoder) vp9FinishKeyFrameDistance(isKey bool) {
	if isKey {
		e.framesSinceKey = 0
		return
	}
	if e.framesSinceKey < ^uint16(0) {
		e.framesSinceKey++
	}
}

func (e *VP9Encoder) vp9OnePassSceneCutStats(src *image.YCbCr,
	flags EncodeFlags, rows int, cols int,
) (vp9SceneCutFrameStats, bool) {
	stats := vp9SceneCutFrameStats{Macroblocks: rows * cols}
	if stats.Macroblocks <= 0 {
		return vp9SceneCutFrameStats{}, false
	}
	hasReference := false
	for row := range rows {
		for col := range cols {
			best, ok := e.bestVP9SceneCutReferenceSSE(src, flags, row, col)
			if !ok {
				return vp9SceneCutFrameStats{}, false
			}
			hasReference = true
			intra := vp9MacroblockMeanLumaSSE(src, row, col)
			stats.ReferenceError += int64(best)
			stats.IntraError += int64(intra)
			if int64(best) > int64(intra)*sceneCutIntraErrorRatio {
				stats.IntraBetterBlocks++
			}
			if best >= sceneCutHighReferenceSSEPerMB {
				stats.HighErrorBlocks++
			}
		}
	}
	return stats, hasReference
}

func (e *VP9Encoder) bestVP9SceneCutReferenceSSE(src *image.YCbCr,
	flags EncodeFlags, mbRow int, mbCol int,
) (int, bool) {
	best := maxInt()
	ok := false
	if flags&EncodeNoReferenceLast == 0 {
		best, ok = lowerVP9SceneCutReferenceSSE(src,
			&e.refFrames[vp9LastRefSlot], mbRow, mbCol, best, ok)
	}
	if flags&EncodeNoReferenceGolden == 0 {
		best, ok = lowerVP9SceneCutReferenceSSE(src,
			&e.refFrames[vp9GoldenRefSlot], mbRow, mbCol, best, ok)
	}
	if flags&EncodeNoReferenceAltRef == 0 {
		best, ok = lowerVP9SceneCutReferenceSSE(src,
			&e.refFrames[vp9AltRefSlot], mbRow, mbCol, best, ok)
	}
	return best, ok
}

func lowerVP9SceneCutReferenceSSE(src *image.YCbCr,
	ref *vp9ReferenceFrame, mbRow int, mbCol int, best int, ok bool,
) (int, bool) {
	if ref == nil || !ref.valid || ref.img.Width != src.Rect.Dx() ||
		ref.img.Height != src.Rect.Dy() {
		return best, ok
	}
	sse := vp9MacroblockLumaSSE(src, &ref.img, mbRow, mbCol)
	if !ok || sse < best {
		best = sse
	}
	return best, true
}

func vp9MacroblockLumaSSE(src *image.YCbCr, ref *Image, mbRow int, mbCol int) int {
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	baseY := mbRow * 16
	baseX := mbCol * 16
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, height)
		refY := clampEncodeCoord(baseY+row, ref.Height)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, width)
			refX := clampEncodeCoord(baseX+col, ref.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) -
				int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}

func vp9MacroblockMeanLumaSSE(src *image.YCbCr, mbRow int, mbCol int) int {
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	baseY := mbRow * 16
	baseX := mbCol * 16
	sum := 0
	sse := 0
	for row := range 16 {
		srcY := clampEncodeCoord(baseY+row, height)
		for col := range 16 {
			srcX := clampEncodeCoord(baseX+col, width)
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
