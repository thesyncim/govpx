package govpx

import "image"

const (
	sceneCutMinimumReferenceSSEPerMB = 16 * 16 * 64 * 64
	sceneCutHighReferenceSSEPerMB    = 16 * 16 * 48 * 48
	sceneCutIntraWinPct              = 75
	sceneCutHighErrorPct             = 75
	sceneCutIntraErrorRatio          = 4
)

type vp9SceneCutFrameStats struct {
	Macroblocks       int
	ReferenceError    int64
	IntraError        int64
	IntraBetterBlocks int
	HighErrorBlocks   int
}

func (e *VP9Encoder) vp9SceneDetectionOnePass(src *image.YCbCr,
	showFrame bool, miRows, miCols int,
) {
	if e == nil {
		return
	}
	e.rc.highSourceSAD = false
	e.rc.highNumBlocksWithMotion = false
	if src == nil || !showFrame ||
		vp9ResolveDeadlineMode(e.opts.Deadline) != vp9ModeRealtime ||
		!(e.rc.mode == RateControlVBR ||
			e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) ||
			e.vp9SpeedFeatureCPUUsed() >= 5) ||
		e.opts.LookaheadFrames > 0 ||
		!e.lastSourceValid ||
		src.Rect.Dx() != e.lastSource.Rect.Dx() ||
		src.Rect.Dy() != e.lastSource.Rect.Dy() {
		return
	}
	avgSAD, zeroTemp, samples, ok := vp9SourceSADSceneSamples(src,
		&e.lastSource, miRows, miCols)
	if !ok {
		return
	}

	minThresh := uint64(65000)
	if e.opts.ScreenContentMode == int8(VP9ScreenContentScreen) {
		minThresh = 20000
	}
	thresh := 8.0
	if e.rc.mode == RateControlVBR {
		thresh = 2.1
	}
	refThresh := max(uint64(float64(e.rc.avgSourceSAD[0])*thresh), minThresh)
	if avgSAD > refThresh &&
		e.rc.framesSinceKey > 2 &&
		zeroTemp < 3*(samples>>2) {
		e.rc.highSourceSAD = true
	}
	if avgSAD > 0 || e.rc.mode == RateControlCBR {
		e.rc.avgSourceSAD[0] = (3*e.rc.avgSourceSAD[0] + avgSAD) >> 2
	}
	if zeroTemp < (3*samples)>>2 {
		e.rc.highNumBlocksWithMotion = true
	}
}

func vp9SourceSADSceneSamples(src, last *image.YCbCr,
	miRows, miCols int,
) (avgSAD uint64, zeroTemp int, samples int, ok bool) {
	if src == nil || last == nil || miRows <= 0 || miCols <= 0 {
		return 0, 0, 0, false
	}
	sbCols := (miCols + 7) >> 3
	sbRows := (miRows + 7) >> 3
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	if width != last.Rect.Dx() || height != last.Rect.Dy() {
		return 0, 0, 0, false
	}
	for sbiRow := range sbRows {
		for sbiCol := range sbCols {
			if !((sbiRow > 0 && sbiCol > 0) &&
				(sbiRow < sbRows-1 && sbiCol < sbCols-1) &&
				((sbiRow%2 == 0 && sbiCol%2 == 0) ||
					(sbiRow%2 != 0 && sbiCol%2 != 0))) {
				continue
			}
			x := sbiCol * 64
			y := sbiRow * 64
			if x+64 > width || y+64 > height {
				continue
			}
			sad := vp9BlockSAD(src.Y, src.YStride, last.Y, last.YStride,
				x, y, x, y, 64, 64, ^uint64(0))
			avgSAD += sad
			samples++
			if sad == 0 {
				zeroTemp++
			}
		}
	}
	if samples <= 0 {
		return 0, 0, 0, false
	}
	return avgSAD / uint64(samples), zeroTemp, samples, true
}

func (e *VP9Encoder) shouldEncodeVP9SceneCutKeyFrame(src *image.YCbCr,
	flags EncodeFlags, temporalEnabled bool, rows int, cols int,
) bool {
	if !e.opts.AdaptiveKeyFrames ||
		e.opts.RTCExternalRateControl ||
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
