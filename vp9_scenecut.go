package govpx

import (
	"image"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

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
	samples, ok := encoder.SourceSADSceneSamples(encoder.SourceSADSceneSamplesArgs{
		SourceY:           src.Y,
		SourceYStride:     src.YStride,
		LastSourceY:       e.lastSource.Y,
		LastSourceYStride: e.lastSource.YStride,
		Width:             src.Rect.Dx(),
		Height:            src.Rect.Dy(),
		MIRows:            miRows,
		MICols:            miCols,
	})
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
	if samples.AverageSAD > refThresh &&
		e.rc.framesSinceKey > 2 &&
		samples.ZeroTemp < 3*(samples.Samples>>2) {
		e.rc.highSourceSAD = true
	}
	if samples.AverageSAD > 0 || e.rc.mode == RateControlCBR {
		e.rc.avgSourceSAD[0] = (3*e.rc.avgSourceSAD[0] + samples.AverageSAD) >> 2
	}
	if samples.ZeroTemp < (3*samples.Samples)>>2 {
		e.rc.highNumBlocksWithMotion = true
	}
}

func (e *VP9Encoder) vp9CarryPostEncodeDroppedSceneChange() {
	if e == nil || !e.rc.lastPostEncodeDroppedSceneChange {
		return
	}
	e.rc.highSourceSAD = true
	e.sf.UseSourceSad = 0
	e.rc.lastPostEncodeDroppedSceneChange = false
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
	return stats.PromotesKeyFrame()
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
) (encoder.SceneCutFrameStats, bool) {
	stats := encoder.SceneCutFrameStats{Macroblocks: rows * cols}
	if stats.Macroblocks <= 0 {
		return encoder.SceneCutFrameStats{}, false
	}
	hasReference := false
	source := encoder.LumaPlane{
		Pixels: src.Y,
		Stride: src.YStride,
		Width:  src.Rect.Dx(),
		Height: src.Rect.Dy(),
	}
	for row := range rows {
		for col := range cols {
			best, ok := e.bestVP9SceneCutReferenceSSE(src, flags, row, col)
			if !ok {
				return encoder.SceneCutFrameStats{}, false
			}
			hasReference = true
			intra := encoder.MacroblockMeanLumaSSE(source, row, col)
			stats.AddMacroblock(best, intra)
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
		if slot, slotOK := e.vp9ReferenceSlotForFrame(vp9dec.LastFrame); slotOK {
			best, ok = lowerVP9SceneCutReferenceSSE(src,
				&e.refFrames[slot], mbRow, mbCol, best, ok)
		}
	}
	if flags&EncodeNoReferenceGolden == 0 {
		if slot, slotOK := e.vp9ReferenceSlotForFrame(vp9dec.GoldenFrame); slotOK {
			best, ok = lowerVP9SceneCutReferenceSSE(src,
				&e.refFrames[slot], mbRow, mbCol, best, ok)
		}
	}
	if flags&EncodeNoReferenceAltRef == 0 {
		if slot, slotOK := e.vp9ReferenceSlotForFrame(vp9dec.AltrefFrame); slotOK {
			best, ok = lowerVP9SceneCutReferenceSSE(src,
				&e.refFrames[slot], mbRow, mbCol, best, ok)
		}
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
	source := encoder.LumaPlane{
		Pixels: src.Y,
		Stride: src.YStride,
		Width:  src.Rect.Dx(),
		Height: src.Rect.Dy(),
	}
	reference := encoder.LumaPlane{
		Pixels: ref.img.Y,
		Stride: ref.img.YStride,
		Width:  ref.img.Width,
		Height: ref.img.Height,
	}
	sse := encoder.MacroblockLumaSSE(source, reference, mbRow, mbCol)
	if !ok || sse < best {
		best = sse
	}
	return best, true
}
