package govpx

import (
	"image"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const (
	vp9DenoiserCopyBlock uint8 = iota
	vp9DenoiserFilterBlock
)

const (
	vp9DenoiserLowLow int8 = iota
	vp9DenoiserLow
	vp9DenoiserMedium
	vp9DenoiserHigh
)

const (
	vp9DenoiserAvgIntra = iota
	vp9DenoiserAvgLast
	vp9DenoiserAvgGolden
	vp9DenoiserAvgAltRef
)

type vp9DenoiserState struct {
	source       image.YCbCr
	sourceBak    image.YCbCr
	runningAvg   [4]image.YCbCr
	intraAvgBak  image.YCbCr
	mcRunningAvg image.YCbCr

	width       int
	height      int
	sensitivity int8
	level       int8
	allocated   bool
	reset       bool
}

func (d *vp9DenoiserState) setSensitivity(level int8) {
	if level <= 0 {
		d.sensitivity = 0
		d.level = vp9DenoiserLowLow
		return
	}
	if d.sensitivity == 0 {
		d.reset = true
	}
	d.sensitivity = level
	d.level = vp9DenoiserLevelForSensitivity(level)
}

func (d *vp9DenoiserState) disable() {
	d.sensitivity = 0
	d.level = vp9DenoiserLowLow
	d.reset = true
}

func vp9DenoiserLevelForSensitivity(level int8) int8 {
	switch {
	case level <= 0:
		return vp9DenoiserLowLow
	case level == 1:
		return vp9DenoiserLow
	case level == 2:
		return vp9DenoiserMedium
	default:
		return vp9DenoiserHigh
	}
}

func (d *vp9DenoiserState) ensure(width, height int) {
	d.setSensitivity(d.sensitivity)
	if d.allocated && d.width == width && d.height == height {
		return
	}
	rect := image.Rect(0, 0, width, height)
	d.source = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	d.sourceBak = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	for i := range d.runningAvg {
		d.runningAvg[i] = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	}
	d.intraAvgBak = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	d.mcRunningAvg = *image.NewYCbCr(rect, image.YCbCrSubsampleRatio420)
	d.width = width
	d.height = height
	d.allocated = true
	d.reset = true
}

func (d *vp9DenoiserState) active() bool {
	return d != nil && d.allocated && d.sensitivity > 0 &&
		d.level > vp9DenoiserLowLow
}

func (e *VP9Encoder) prepareVP9DenoiserSource(img *image.YCbCr) *image.YCbCr {
	if e == nil || e.opts.NoiseSensitivity <= 0 {
		return img
	}
	e.denoiser.sensitivity = e.opts.NoiseSensitivity
	e.denoiser.ensure(e.opts.Width, e.opts.Height)
	if e.noiseEstimate.enabled {
		e.denoiser.level = vp9DenoiserLevelForNoiseEstimate(
			vp9NoiseEstimateExtractLevel(&e.noiseEstimate))
	}
	if e.denoiser.level <= vp9DenoiserLowLow {
		return img
	}
	copyVP9LookaheadImage(&e.denoiser.source, img, e.opts.Width, e.opts.Height)
	copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgIntra],
		img, e.opts.Width, e.opts.Height)
	return &e.denoiser.source
}

func vp9DenoiserLevelForNoiseEstimate(level vp9NoiseLevel) int8 {
	switch level {
	case vp9NoiseLevelLow:
		return vp9DenoiserLow
	case vp9NoiseLevelMedium:
		return vp9DenoiserMedium
	case vp9NoiseLevelHigh:
		return vp9DenoiserHigh
	default:
		return vp9DenoiserLowLow
	}
}

func (e *VP9Encoder) saveVP9DenoiserForCounts(inter *vp9InterEncodeState) bool {
	if e == nil || inter == nil || !e.denoiser.active() {
		return false
	}
	copyVP9LookaheadImage(&e.denoiser.sourceBak, &e.denoiser.source,
		e.opts.Width, e.opts.Height)
	copyVP9LookaheadImage(&e.denoiser.intraAvgBak,
		&e.denoiser.runningAvg[vp9DenoiserAvgIntra],
		e.opts.Width, e.opts.Height)
	return true
}

func (e *VP9Encoder) restoreVP9DenoiserAfterCounts(saved bool) {
	if e == nil || !saved {
		return
	}
	copyVP9LookaheadImage(&e.denoiser.source, &e.denoiser.sourceBak,
		e.opts.Width, e.opts.Height)
	copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgIntra],
		&e.denoiser.intraAvgBak, e.opts.Width, e.opts.Height)
}

func (e *VP9Encoder) finishVP9DenoiserFrame(header *vp9dec.UncompressedHeader, src *image.YCbCr) {
	if e == nil || header == nil || src == nil || !e.denoiser.active() {
		return
	}
	if header.FrameType == common.KeyFrame || header.IntraOnly || e.denoiser.reset {
		for idx := vp9DenoiserAvgLast; idx <= vp9DenoiserAvgAltRef; idx++ {
			copyVP9LookaheadImage(&e.denoiser.runningAvg[idx], src,
				e.opts.Width, e.opts.Height)
		}
		e.denoiser.reset = false
		return
	}
	intra := &e.denoiser.runningAvg[vp9DenoiserAvgIntra]
	if header.RefreshFrameFlags&(1<<uint(vp9LastRefSlot)) != 0 {
		copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgLast],
			intra, e.opts.Width, e.opts.Height)
	}
	if header.RefreshFrameFlags&(1<<uint(vp9GoldenRefSlot)) != 0 {
		copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgGolden],
			intra, e.opts.Width, e.opts.Height)
	}
	if header.RefreshFrameFlags&(1<<uint(vp9AltRefSlot)) != 0 {
		copyVP9LookaheadImage(&e.denoiser.runningAvg[vp9DenoiserAvgAltRef],
			intra, e.opts.Width, e.opts.Height)
	}
}

func (e *VP9Encoder) applyVP9DenoiserToInterBlock(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	decision vp9InterModeDecision,
) {
	if e == nil || inter == nil || !e.denoiser.active() ||
		e.denoiser.level <= vp9DenoiserLowLow ||
		e.denoiser.reset ||
		decision.isCompound || decision.refFrame <= vp9dec.IntraFrame ||
		bsize >= common.BlockSizes {
		return
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) << 2
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) << 2
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if blockW <= 0 || blockH <= 0 || x0 < 0 || y0 < 0 ||
		x0+blockW > e.opts.Width || y0+blockH > e.opts.Height {
		return
	}
	if bsize == common.Block8x8 || bsize == common.Block8x16 ||
		bsize == common.Block16x8 ||
		(bsize == common.Block16x16 && e.opts.Width > 480 &&
			e.denoiser.level <= vp9DenoiserLow) {
		return
	}

	mv := decision.mv[0]
	motionMagnitude := int(mv.Row)*int(mv.Row) + int(mv.Col)*int(mv.Col)
	increase := e.denoiser.level >= vp9DenoiserHigh
	bestSSE := decision.distortion
	refFrame := decision.refFrame
	refSlot := decision.refSlot
	mode := decision.mode
	filter := decision.interpFilter
	if decision.refFrame == vp9dec.LastFrame {
		zeroSSE, ok := e.vp9DenoiserZeroLastSSE(x0, y0, blockW, blockH)
		if ok {
			sseDiff := int64(zeroSSE) - int64(bestSSE)
			if sseDiff <= int64(vp9DenoiserSSEDiffThresh(bsize, increase, motionMagnitude)) {
				refFrame = vp9dec.LastFrame
				refSlot = vp9LastRefSlot
				mode = common.ZeroMv
				filter = vp9dec.InterpEighttap
				mv = vp9dec.MV{}
				bestSSE = zeroSSE
				motionMagnitude = 0
			}
		}
	} else {
		refFrame = vp9dec.LastFrame
		refSlot = vp9LastRefSlot
		mode = common.ZeroMv
		filter = vp9dec.InterpEighttap
		mv = vp9dec.MV{}
		motionMagnitude = 0
		if zeroSSE, ok := e.vp9DenoiserZeroLastSSE(x0, y0, blockW, blockH); ok {
			bestSSE = zeroSSE
		}
	}
	if bestSSE > uint64(vp9DenoiserSSEThresh(bsize, increase)) ||
		motionMagnitude > vp9DenoiserNoiseMotionThresh(bsize, increase)<<3 {
		return
	}
	if !e.vp9DenoiserMCPredict(inter, miRows, miCols, miRow, miCol, bsize,
		refFrame, refSlot, mode, mv, filter) {
		return
	}

	src := &e.denoiser.source
	avg := &e.denoiser.runningAvg[vp9DenoiserAvgIntra]
	mc := &e.denoiser.mcRunningAvg
	srcOff := y0*src.YStride + x0
	avgOff := y0*avg.YStride + x0
	mcOff := y0*mc.YStride + x0
	if vp9DenoiserFilter(src.Y[srcOff:], src.YStride,
		mc.Y[mcOff:], mc.YStride,
		avg.Y[avgOff:], avg.YStride,
		increase, bsize, motionMagnitude) == vp9DenoiserFilterBlock {
		copyVP9LumaBlock(src.Y[srcOff:], src.YStride,
			avg.Y[avgOff:], avg.YStride, blockW, blockH)
		applyVP9DenoiserToInterChromaBlock(src, avg, mc,
			x0, y0, blockW, blockH, bsize, increase, motionMagnitude)
		return
	}
	copyVP9LumaBlock(avg.Y[avgOff:], avg.YStride,
		src.Y[srcOff:], src.YStride, blockW, blockH)
}

func applyVP9DenoiserToInterChromaBlock(
	src *image.YCbCr, avg *image.YCbCr, mc *image.YCbCr,
	x0, y0, blockW, blockH int, bsize common.BlockSize,
	increase bool, motionMagnitude int,
) {
	uvX := x0 >> 1
	uvY := y0 >> 1
	uvW := blockW >> 1
	uvH := blockH >> 1
	if uvW <= 0 || uvH <= 0 {
		return
	}
	uvNumPelsLog2 := vp9DenoiserNumPelsLog2(bsize) - 2
	srcOff := uvY*src.CStride + uvX
	avgOff := uvY*avg.CStride + uvX
	mcOff := uvY*mc.CStride + uvX
	uFilter := vp9DenoiserFilterPlane(src.Cb[srcOff:], src.CStride,
		mc.Cb[mcOff:], mc.CStride,
		avg.Cb[avgOff:], avg.CStride,
		uvW, uvH, uvNumPelsLog2, increase, motionMagnitude)
	vFilter := vp9DenoiserFilterPlane(src.Cr[srcOff:], src.CStride,
		mc.Cr[mcOff:], mc.CStride,
		avg.Cr[avgOff:], avg.CStride,
		uvW, uvH, uvNumPelsLog2, increase, motionMagnitude)
	if uFilter == vp9DenoiserFilterBlock &&
		vFilter == vp9DenoiserFilterBlock {
		copyVP9PlaneBlock(src.Cb[srcOff:], src.CStride,
			avg.Cb[avgOff:], avg.CStride, uvW, uvH)
		copyVP9PlaneBlock(src.Cr[srcOff:], src.CStride,
			avg.Cr[avgOff:], avg.CStride, uvW, uvH)
		return
	}
	copyVP9PlaneBlock(avg.Cb[avgOff:], avg.CStride,
		src.Cb[srcOff:], src.CStride, uvW, uvH)
	copyVP9PlaneBlock(avg.Cr[avgOff:], avg.CStride,
		src.Cr[srcOff:], src.CStride, uvW, uvH)
}

func (e *VP9Encoder) vp9DenoiserZeroLastSSE(x0, y0, blockW, blockH int) (uint64, bool) {
	if e == nil || !e.denoiser.active() {
		return 0, false
	}
	src := &e.denoiser.source
	last := &e.denoiser.runningAvg[vp9DenoiserAvgLast]
	if len(src.Y) == 0 || len(last.Y) == 0 {
		return 0, false
	}
	return vp9BlockSSE(src.Y, src.YStride, last.Y, last.YStride,
		x0, y0, x0, y0, blockW, blockH), true
}

func (e *VP9Encoder) vp9DenoiserMCPredict(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize,
	refFrame int8, refSlot int, mode common.PredictionMode, mv vp9dec.MV,
	filter vp9dec.InterpFilter,
) bool {
	avgIdx, ok := vp9DenoiserAvgIndexForRef(refFrame)
	if !ok || refSlot < 0 || refSlot >= len(e.refFrames) {
		return false
	}
	savedReconFrame := e.reconFrame
	savedReconY, savedReconU, savedReconV := e.reconY, e.reconU, e.reconV
	savedRef := e.refFrames[refSlot]
	savedInterRef := inter.ref

	e.reconFrame = vp9ImageFromYCbCr(&e.denoiser.mcRunningAvg)
	e.reconY = e.reconFrame.Y
	e.reconU = e.reconFrame.U
	e.reconV = e.reconFrame.V
	e.refFrames[refSlot] = vp9ReferenceFrameFromYCbCr(
		&e.denoiser.runningAvg[avgIdx])
	inter.ref = &e.refFrames[refSlot]

	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		Mode:         mode,
		InterpFilter: uint8(filter),
		RefFrame:     [2]int8{refFrame, vp9dec.NoRefFrame},
		Mv:           [2]vp9dec.MV{mv},
	}
	ok = e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol, bsize, &mi)

	inter.ref = savedInterRef
	e.refFrames[refSlot] = savedRef
	e.reconFrame = savedReconFrame
	e.reconY, e.reconU, e.reconV = savedReconY, savedReconU, savedReconV
	return ok
}

func vp9DenoiserAvgIndexForRef(refFrame int8) (int, bool) {
	switch refFrame {
	case vp9dec.LastFrame:
		return vp9DenoiserAvgLast, true
	case vp9dec.GoldenFrame:
		return vp9DenoiserAvgGolden, true
	case vp9dec.AltrefFrame:
		return vp9DenoiserAvgAltRef, true
	default:
		return 0, false
	}
}

func vp9ReferenceFrameFromYCbCr(img *image.YCbCr) vp9ReferenceFrame {
	return vp9ReferenceFrame{
		img:   vp9ImageFromYCbCr(img),
		y:     img.Y,
		u:     img.Cb,
		v:     img.Cr,
		valid: true,
	}
}

func vp9ImageFromYCbCr(img *image.YCbCr) Image {
	return Image{
		Width:   img.Rect.Dx(),
		Height:  img.Rect.Dy(),
		Y:       img.Y,
		U:       img.Cb,
		V:       img.Cr,
		YStride: img.YStride,
		UStride: img.CStride,
		VStride: img.CStride,
	}
}

func vp9DenoiserFilter(sig []byte, sigStride int, mcAvg []byte, mcAvgStride int,
	avg []byte, avgStride int, increase bool, bsize common.BlockSize,
	motionMagnitude int,
) uint8 {
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) << 2
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) << 2
	return vp9DenoiserFilterPlane(sig, sigStride, mcAvg, mcAvgStride,
		avg, avgStride, blockW, blockH, vp9DenoiserNumPelsLog2(bsize),
		increase, motionMagnitude)
}

func vp9DenoiserFilterPlane(sig []byte, sigStride int, mcAvg []byte, mcAvgStride int,
	avg []byte, avgStride int, blockW, blockH, numPelsLog2 int,
	increase bool, motionMagnitude int,
) uint8 {
	adj := [3]int{3, 4, 6}
	shiftInc := 1
	if motionMagnitude <= vp9DenoiserMotionMagnitudeThreshold {
		if increase {
			shiftInc = 2
		}
		adj[0] += shiftInc
		adj[1] += shiftInc
		adj[2] += shiftInc
	}

	totalAdj := 0
	for y := range blockH {
		sigRow := sig[y*sigStride : y*sigStride+blockW : y*sigStride+blockW]
		mcRow := mcAvg[y*mcAvgStride : y*mcAvgStride+blockW : y*mcAvgStride+blockW]
		avgRow := avg[y*avgStride : y*avgStride+blockW : y*avgStride+blockW]
		for x := range blockW {
			diff := int(mcRow[x]) - int(sigRow[x])
			absdiff := vp9AbsInt(diff)
			if absdiff <= vp9DenoiserAbsdiffThresh(increase) {
				avgRow[x] = mcRow[x]
				totalAdj += diff
				continue
			}
			var adjustment int
			switch {
			case absdiff >= 4 && absdiff <= 7:
				adjustment = adj[0]
			case absdiff >= 8 && absdiff <= 15:
				adjustment = adj[1]
			default:
				adjustment = adj[2]
			}
			if diff > 0 {
				avgRow[x] = byte(min(255, int(sigRow[x])+adjustment))
				totalAdj += adjustment
			} else {
				avgRow[x] = byte(max(0, int(sigRow[x])-adjustment))
				totalAdj -= adjustment
			}
		}
	}
	strongThresh := vp9DenoiserTotalAdjStrongThreshForPixels(numPelsLog2, increase)
	if vp9AbsInt(totalAdj) <= strongThresh {
		return vp9DenoiserFilterBlock
	}

	delta := ((vp9AbsInt(totalAdj) - strongThresh) >>
		uint(numPelsLog2)) + 1
	if delta >= 4 {
		return vp9DenoiserCopyBlock
	}

	for y := range blockH {
		sigRow := sig[y*sigStride : y*sigStride+blockW : y*sigStride+blockW]
		mcRow := mcAvg[y*mcAvgStride : y*mcAvgStride+blockW : y*mcAvgStride+blockW]
		avgRow := avg[y*avgStride : y*avgStride+blockW : y*avgStride+blockW]
		for x := range blockW {
			diff := int(mcRow[x]) - int(sigRow[x])
			adjustment := min(vp9AbsInt(diff), delta)
			if diff > 0 {
				avgRow[x] = byte(max(0, int(avgRow[x])-adjustment))
				totalAdj -= adjustment
			} else {
				avgRow[x] = byte(min(255, int(avgRow[x])+adjustment))
				totalAdj += adjustment
			}
		}
	}
	if vp9AbsInt(totalAdj) <= vp9DenoiserTotalAdjWeakThreshForPixels(numPelsLog2, increase) {
		return vp9DenoiserFilterBlock
	}
	return vp9DenoiserCopyBlock
}

const vp9DenoiserMotionMagnitudeThreshold = 8 * 3

func vp9DenoiserAbsdiffThresh(increase bool) int {
	if increase {
		return 4
	}
	return 3
}

func vp9DenoiserNoiseMotionThresh(_ common.BlockSize, _ bool) int {
	return 625
}

func vp9DenoiserSSEThresh(bsize common.BlockSize, increase bool) int {
	if increase {
		return (1 << uint(vp9DenoiserNumPelsLog2(bsize))) * 80
	}
	return (1 << uint(vp9DenoiserNumPelsLog2(bsize))) * 40
}

func vp9DenoiserSSEDiffThresh(bsize common.BlockSize, increase bool, motionMagnitude int) int {
	if motionMagnitude > vp9DenoiserNoiseMotionThresh(bsize, increase) {
		if increase {
			return (1 << uint(vp9DenoiserNumPelsLog2(bsize))) << 2
		}
		return 0
	}
	return (1 << uint(vp9DenoiserNumPelsLog2(bsize))) << 4
}

func vp9DenoiserTotalAdjStrongThreshForPixels(numPelsLog2 int, increase bool) int {
	if increase {
		return (1 << uint(numPelsLog2)) * 3
	}
	return (1 << uint(numPelsLog2)) * 2
}

func vp9DenoiserTotalAdjWeakThreshForPixels(numPelsLog2 int, increase bool) int {
	return vp9DenoiserTotalAdjStrongThreshForPixels(numPelsLog2, increase)
}

func vp9DenoiserNumPelsLog2(bsize common.BlockSize) int {
	return int(common.BWidthLog2Lookup[bsize]+common.BHeightLog2Lookup[bsize]) + 4
}

func copyVP9LumaBlock(dst []byte, dstStride int, src []byte, srcStride int, width int, height int) {
	copyVP9PlaneBlock(dst, dstStride, src, srcStride, width, height)
}

func copyVP9PlaneBlock(dst []byte, dstStride int, src []byte, srcStride int, width int, height int) {
	for y := range height {
		copy(dst[y*dstStride:][:width], src[y*srcStride:][:width])
	}
}
