package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const (
	sceneCutMinimumReferenceSSEPerMB = 16 * 16 * 64 * 64
	sceneCutHighReferenceSSEPerMB    = 16 * 16 * 48 * 48
	sceneCutIntraWinPct              = 75
	sceneCutHighErrorPct             = 75
	sceneCutIntraErrorRatio          = 4
)

type sceneCutFrameStats struct {
	Macroblocks       int
	ReferenceError    int64
	IntraError        int64
	IntraBetterBlocks int
	HighErrorBlocks   int
}

func (e *VP8Encoder) shouldEncodeSceneCutKeyFrame(src vp8enc.SourceImage, flags EncodeFlags, temporalEnabled bool, rows int, cols int) bool {
	if !e.opts.AdaptiveKeyFrames ||
		e.frameCount == 0 ||
		temporalEnabled ||
		flags&(EncodeInvisibleFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef) != 0 {
		return false
	}
	stats, ok := e.onePassSceneCutStats(src, flags, rows, cols)
	if !ok {
		return false
	}
	if stats.Macroblocks <= 0 || stats.ReferenceError < int64(sceneCutMinimumReferenceSSEPerMB)*int64(stats.Macroblocks) {
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

func (e *VP8Encoder) onePassSceneCutStats(src vp8enc.SourceImage, flags EncodeFlags, rows int, cols int) (sceneCutFrameStats, bool) {
	stats := sceneCutFrameStats{Macroblocks: rows * cols}
	if stats.Macroblocks <= 0 {
		return sceneCutFrameStats{}, false
	}
	hasReference := false
	for row := range rows {
		for col := range cols {
			best, ok := e.bestSceneCutReferenceSSE(src, flags, row, col)
			if !ok {
				return sceneCutFrameStats{}, false
			}
			hasReference = true
			intra := macroblockMeanLumaSSE(src, row, col)
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

func (e *VP8Encoder) bestSceneCutReferenceSSE(src vp8enc.SourceImage, flags EncodeFlags, mbRow int, mbCol int) (int, bool) {
	best := maxInt()
	ok := false
	if flags&EncodeNoReferenceLast == 0 {
		best, ok = lowerSceneCutReferenceSSE(src, &e.lastRef.Img, mbRow, mbCol, best, ok)
	}
	if flags&EncodeNoReferenceGolden == 0 {
		best, ok = lowerSceneCutReferenceSSE(src, &e.goldenRef.Img, mbRow, mbCol, best, ok)
	}
	if flags&EncodeNoReferenceAltRef == 0 {
		best, ok = lowerSceneCutReferenceSSE(src, &e.altRef.Img, mbRow, mbCol, best, ok)
	}
	return best, ok
}

func lowerSceneCutReferenceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best int, ok bool) (int, bool) {
	if ref == nil || ref.Width != src.Width || ref.Height != src.Height {
		return best, ok
	}
	sse := macroblockLumaSSE(src, ref, mbRow, mbCol, vp8enc.MotionVector{})
	if !ok || sse < best {
		best = sse
	}
	return best, true
}

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

func sourceImageFromImage(src Image) vp8enc.SourceImage {
	return vp8enc.SourceImage{
		Width:   src.Width,
		Height:  src.Height,
		Y:       src.Y,
		U:       src.U,
		V:       src.V,
		YStride: src.YStride,
		UStride: src.UStride,
		VStride: src.VStride,
	}
}
