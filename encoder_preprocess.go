package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

const maxLookaheadFrames = 25

type lookaheadEntry struct {
	frame    vp8common.FrameBuffer
	pts      uint64
	duration uint64
	flags    EncodeFlags
}

type encodeSourceMetadata struct {
	lookaheadDepth int
	arnrFiltered   bool
	denoised       bool
}

func (e *VP8Encoder) initPreprocessFrames(width int, height int) error {
	if err := e.preprocess.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.arnrScratch.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.arnrLastSource.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.denoiseRunningAvg.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassLastRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	if err := e.firstPassGoldenRef.Resize(width, height, 32, 32); err != nil {
		return ErrInvalidConfig
	}
	return nil
}

func (e *VP8Encoder) initLookahead(width int, height int, depth int) error {
	if depth <= 0 {
		return nil
	}
	e.lookahead = make([]lookaheadEntry, depth+1)
	for i := range e.lookahead {
		if err := e.lookahead[i].frame.Resize(width, height, 32, 32); err != nil {
			return ErrInvalidConfig
		}
	}
	return nil
}

func (e *VP8Encoder) lookaheadEnabled() bool {
	return e != nil && e.opts.LookaheadFrames > 0 && len(e.lookahead) > 0
}

func (e *VP8Encoder) lookaheadSize() int {
	if !e.lookaheadEnabled() {
		return 0
	}
	return e.lookaheadCount
}

func (e *VP8Encoder) encodeLookaheadInto(dst []byte, src Image, pts uint64, duration uint64, flags EncodeFlags) (EncodeResult, error) {
	if err := validateEncodeFlags(flags); err != nil {
		return EncodeResult{}, err
	}
	if err := e.pushLookahead(sourceImageFromImage(src), pts, duration, flags); err != nil {
		return EncodeResult{}, err
	}
	if e.lookaheadSize() < e.opts.LookaheadFrames {
		return EncodeResult{}, ErrFrameNotReady
	}
	entry, ok := e.popLookahead(false)
	if !ok {
		return EncodeResult{}, ErrFrameNotReady
	}
	meta := encodeSourceMetadata{lookaheadDepth: e.lookaheadSize()}
	result, err := e.encodeSourceInto(dst, sourceImageFromVP8(&entry.frame.Img), entry.pts, entry.duration, entry.flags, meta)
	e.clearPoppedLookahead(entry)
	return result, err
}

func (e *VP8Encoder) pushLookahead(src vp8enc.SourceImage, pts uint64, duration uint64, flags EncodeFlags) error {
	if !e.lookaheadEnabled() {
		return ErrInvalidConfig
	}
	if e.lookaheadCount >= len(e.lookahead)-1 {
		return ErrFrameNotReady
	}
	entry := &e.lookahead[e.lookaheadWrite]
	copySourceToFrameBuffer(&entry.frame, src)
	entry.pts = pts
	entry.duration = duration
	if entry.duration == 0 {
		entry.duration = 1
	}
	entry.flags = flags
	e.lookaheadWrite++
	if e.lookaheadWrite >= len(e.lookahead) {
		e.lookaheadWrite = 0
	}
	e.lookaheadCount++
	return nil
}

func (e *VP8Encoder) popLookahead(drain bool) (*lookaheadEntry, bool) {
	if !e.lookaheadEnabled() {
		return nil, false
	}
	if e.lookaheadCount == 0 || (!drain && e.lookaheadCount < e.opts.LookaheadFrames) {
		return nil, false
	}
	entry := &e.lookahead[e.lookaheadRead]
	e.lookaheadRead++
	if e.lookaheadRead >= len(e.lookahead) {
		e.lookaheadRead = 0
	}
	e.lookaheadCount--
	return entry, true
}

func (e *VP8Encoder) clearPoppedLookahead(entry *lookaheadEntry) {
	if entry == nil {
		return
	}
	entry.pts, entry.duration, entry.flags = 0, 0, 0
}

func (e *VP8Encoder) preprocessSource(source vp8enc.SourceImage, flags EncodeFlags, meta encodeSourceMetadata) (vp8enc.SourceImage, encodeSourceMetadata) {
	src := source
	if e.opts.ARNRMaxFrames > 1 && e.lookaheadEnabled() {
		if e.applyARNRFilter(src, flags) {
			src = sourceImageFromVP8(&e.arnrScratch.Img)
			meta.arnrFiltered = true
		}
	}
	if e.opts.NoiseSensitivity > 0 {
		e.applySpatialDenoiser(src)
		src = sourceImageFromVP8(&e.preprocess.Img)
		meta.denoised = true
	}
	copySourceToFrameBuffer(&e.arnrLastSource, source)
	e.arnrLastReady = true
	return src, meta
}

func (e *VP8Encoder) applyARNRFilter(center vp8enc.SourceImage, flags EncodeFlags) bool {
	maxFrames := e.opts.ARNRMaxFrames
	if maxFrames > maxLookaheadFrames {
		maxFrames = maxLookaheadFrames
	}
	if maxFrames <= 1 {
		return false
	}
	arnrType := e.opts.ARNRType
	if arnrType == 0 {
		arnrType = 3
	}
	backward := 0
	forward := 0
	switch arnrType {
	case 1:
		if e.arnrLastReady {
			backward = 1
		}
	case 2:
		forward = min(e.lookaheadSize(), maxFrames-1)
	default:
		if e.arnrLastReady {
			backward = 1
		}
		forward = min(e.lookaheadSize(), maxFrames-1-backward)
	}
	if backward+forward == 0 {
		return false
	}
	strength := e.opts.ARNRStrength
	if strength <= 0 {
		strength = 3
	}
	copySourceToFrameBuffer(&e.arnrScratch, center)
	temporalFilterPlane(e.arnrScratch.Img.Y, e.arnrScratch.Img.YStride, center.Y, center.YStride, center.Width, center.Height, strength, e.arnrLastSource.Img.Y, e.arnrLastSource.Img.YStride, backward > 0, e.lookaheadFutureEntry, forward, 0)
	if flags&EncodeInvisibleFrame != 0 || e.opts.ARNRStrength > 4 {
		temporalFilterPlane(e.arnrScratch.Img.U, e.arnrScratch.Img.UStride, center.U, center.UStride, (center.Width+1)>>1, (center.Height+1)>>1, strength, e.arnrLastSource.Img.U, e.arnrLastSource.Img.UStride, backward > 0, e.lookaheadFutureEntry, forward, 1)
		temporalFilterPlane(e.arnrScratch.Img.V, e.arnrScratch.Img.VStride, center.V, center.VStride, (center.Width+1)>>1, (center.Height+1)>>1, strength, e.arnrLastSource.Img.V, e.arnrLastSource.Img.VStride, backward > 0, e.lookaheadFutureEntry, forward, 2)
	}
	e.arnrScratch.ExtendBorders()
	return true
}

func (e *VP8Encoder) lookaheadFutureEntry(index int) *lookaheadEntry {
	if index < 0 || index >= e.lookaheadCount || len(e.lookahead) == 0 {
		return nil
	}
	pos := e.lookaheadRead + index
	for pos >= len(e.lookahead) {
		pos -= len(e.lookahead)
	}
	return &e.lookahead[pos]
}

func temporalFilterPlane(dst []byte, dstStride int, center []byte, centerStride int, width int, height int, strength int, back []byte, backStride int, useBack bool, future func(int) *lookaheadEntry, forward int, planeID int) {
	threshold := 8 + strength*8
	if planeID != 0 {
		threshold += 8
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := int(center[y*centerStride+x])
			sum := c * threshold
			weightSum := threshold
			if useBack {
				v := int(back[y*backStride+x])
				w := temporalFilterWeight(c, v, threshold)
				sum += v * w
				weightSum += w
			}
			for i := 0; i < forward; i++ {
				entry := future(i)
				if entry == nil {
					continue
				}
				var plane []byte
				stride := 0
				switch planeID {
				case 1:
					plane = entry.frame.Img.U
					stride = entry.frame.Img.UStride
				case 2:
					plane = entry.frame.Img.V
					stride = entry.frame.Img.VStride
				default:
					plane = entry.frame.Img.Y
					stride = entry.frame.Img.YStride
				}
				v := int(plane[y*stride+x])
				w := temporalFilterWeight(c, v, threshold)
				sum += v * w
				weightSum += w
			}
			dst[y*dstStride+x] = byte((sum + weightSum/2) / weightSum)
		}
	}
}

func temporalFilterWeight(center int, sample int, threshold int) int {
	diff := center - sample
	if diff < 0 {
		diff = -diff
	}
	if diff >= threshold {
		return 0
	}
	return threshold - diff
}

func (e *VP8Encoder) applySpatialDenoiser(src vp8enc.SourceImage) {
	copySourceToFrameBuffer(&e.preprocess, src)
	strength := e.opts.NoiseSensitivity
	if strength < 1 {
		return
	}
	spatialDenoisePlane(e.preprocess.Img.Y, e.preprocess.Img.YStride, src.Y, src.YStride, src.Width, src.Height, strength)
	if strength >= 2 {
		uvWidth := (src.Width + 1) >> 1
		uvHeight := (src.Height + 1) >> 1
		spatialDenoisePlane(e.preprocess.Img.U, e.preprocess.Img.UStride, src.U, src.UStride, uvWidth, uvHeight, strength-1)
		spatialDenoisePlane(e.preprocess.Img.V, e.preprocess.Img.VStride, src.V, src.VStride, uvWidth, uvHeight, strength-1)
	}
	if e.denoiseReady {
		temporalDenoisePlane(e.preprocess.Img.Y, e.preprocess.Img.YStride, e.denoiseRunningAvg.Img.Y, e.denoiseRunningAvg.Img.YStride, src.Width, src.Height, strength)
		if strength >= 2 {
			uvWidth := (src.Width + 1) >> 1
			uvHeight := (src.Height + 1) >> 1
			temporalDenoisePlane(e.preprocess.Img.U, e.preprocess.Img.UStride, e.denoiseRunningAvg.Img.U, e.denoiseRunningAvg.Img.UStride, uvWidth, uvHeight, strength)
			temporalDenoisePlane(e.preprocess.Img.V, e.preprocess.Img.VStride, e.denoiseRunningAvg.Img.V, e.denoiseRunningAvg.Img.VStride, uvWidth, uvHeight, strength)
		}
	}
	copyFrameImage(&e.denoiseRunningAvg.Img, &e.preprocess.Img)
	e.denoiseRunningAvg.ExtendBorders()
	e.denoiseReady = true
	e.preprocess.ExtendBorders()
}

func spatialDenoisePlane(dst []byte, dstStride int, src []byte, srcStride int, width int, height int, strength int) {
	if width <= 2 || height <= 2 {
		return
	}
	threshold := 6 + strength*4
	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			center := int(src[y*srcStride+x])
			sum := center * 4
			count := 4
			for yy := -1; yy <= 1; yy++ {
				for xx := -1; xx <= 1; xx++ {
					if yy == 0 && xx == 0 {
						continue
					}
					v := int(src[(y+yy)*srcStride+x+xx])
					diff := center - v
					if diff < 0 {
						diff = -diff
					}
					if diff <= threshold {
						sum += v
						count++
					}
				}
			}
			dst[y*dstStride+x] = byte((sum + count/2) / count)
		}
	}
}

func temporalDenoisePlane(dst []byte, dstStride int, avg []byte, avgStride int, width int, height int, strength int) {
	threshold := 8 + strength*6
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			offset := y*dstStride + x
			v := int(dst[offset])
			a := int(avg[y*avgStride+x])
			diff := v - a
			if diff < 0 {
				diff = -diff
			}
			if diff <= threshold {
				dst[offset] = byte((3*a + v + 2) >> 2)
			}
		}
	}
}

func copySourceToFrameBuffer(dst *vp8common.FrameBuffer, src vp8enc.SourceImage) {
	copyPlane(dst.Img.Y, dst.Img.YStride, src.Y, src.YStride, src.Width, src.Height)
	copyPlane(dst.Img.U, dst.Img.UStride, src.U, src.UStride, (src.Width+1)>>1, (src.Height+1)>>1)
	copyPlane(dst.Img.V, dst.Img.VStride, src.V, src.VStride, (src.Width+1)>>1, (src.Height+1)>>1)
	padFrameVisibleToCoded(&dst.Img)
	dst.ExtendBorders()
}

func padFrameVisibleToCoded(img *vp8common.Image) {
	padPlaneVisibleToCoded(img.Y, img.YStride, img.Width, img.Height, img.CodedWidth, img.CodedHeight)
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	codedUVWidth := (img.CodedWidth + 1) >> 1
	codedUVHeight := (img.CodedHeight + 1) >> 1
	padPlaneVisibleToCoded(img.U, img.UStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight)
	padPlaneVisibleToCoded(img.V, img.VStride, uvWidth, uvHeight, codedUVWidth, codedUVHeight)
}

func padPlaneVisibleToCoded(plane []byte, stride int, width int, height int, codedWidth int, codedHeight int) {
	if width <= 0 || height <= 0 {
		return
	}
	for y := 0; y < height; y++ {
		row := plane[y*stride:]
		last := row[width-1]
		for x := width; x < codedWidth; x++ {
			row[x] = last
		}
	}
	lastRow := plane[(height-1)*stride:]
	for y := height; y < codedHeight; y++ {
		copy(plane[y*stride:y*stride+codedWidth], lastRow[:codedWidth])
	}
}

func sourceImageFromVP8(src *vp8common.Image) vp8enc.SourceImage {
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
