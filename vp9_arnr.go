package govpx

import "image"

func (e *VP9Encoder) vp9AutoAltRefSourceImage(center *vp9LookaheadEntry) *image.YCbCr {
	if center == nil {
		return nil
	}
	if e.applyVP9ARNRFilter(center) {
		return &e.vp9ARNRScratch
	}
	return &center.img
}

func (e *VP9Encoder) applyVP9ARNRFilter(center *vp9LookaheadEntry) bool {
	maxFrames := min(e.opts.ARNRMaxFrames, maxARNRFrames)
	if maxFrames <= 1 || len(e.vp9ARNRScratch.Y) == 0 ||
		e.lookaheadCount == 0 {
		return false
	}
	distance := int(e.lookaheadCount) - 1
	backward, forward, ok := vp9ARNRFilterWindow(distance,
		int(e.lookaheadCount), maxFrames, e.opts.ARNRType)
	if !ok || backward+forward == 0 {
		return false
	}
	framesToBlur := backward + forward + 1
	if framesToBlur <= 0 || framesToBlur > maxARNRFrames {
		return false
	}

	copyVP9LookaheadImage(&e.vp9ARNRScratch, &center.img, e.opts.Width,
		e.opts.Height)
	refs := e.vp9ARNRRefs[:framesToBlur:framesToBlur]
	startFrame := distance + forward
	for frame := 0; frame < framesToBlur; frame++ {
		entry, ok := e.peekVP9Lookahead(startFrame - frame)
		if !ok {
			return false
		}
		refs[framesToBlur-1-frame] = arnrViewFromYCbCr(&entry.img)
	}
	e.iterateVP9TemporalFilter(e.opts.ARNRStrength, refs, backward, true)
	return true
}

func vp9ARNRFilterWindow(distance int, lookaheadCount int, maxFrames int, filterType int) (int, int, bool) {
	if distance < 0 || lookaheadCount <= 0 || maxFrames <= 1 {
		return 0, 0, false
	}
	numFramesBackward := distance
	numFramesForward := lookaheadCount - (numFramesBackward + 1)
	if numFramesForward < 0 {
		return 0, 0, false
	}
	framesBackward := 0
	framesForward := 0
	switch filterType {
	case 1:
		framesBackward = numFramesBackward
		if framesBackward >= maxFrames {
			framesBackward = maxFrames - 1
		}
	case 2:
		framesForward = numFramesForward
		if framesForward >= maxFrames {
			framesForward = maxFrames - 1
		}
	case 3:
		framesForward = numFramesForward
		framesBackward = numFramesBackward
		if framesForward > framesBackward {
			framesForward = framesBackward
		}
		if framesBackward > framesForward {
			framesBackward = framesForward
		}
		if framesForward > (maxFrames-1)/2 {
			framesForward = (maxFrames - 1) / 2
		}
		if framesBackward > maxFrames/2 {
			framesBackward = maxFrames / 2
		}
	default:
		return 0, 0, false
	}
	return framesBackward, framesForward, true
}

func (e *VP9Encoder) peekVP9Lookahead(offset int) (*vp9LookaheadEntry, bool) {
	if !e.vp9LookaheadEnabled() || offset < 0 || offset >= int(e.lookaheadCount) {
		return nil, false
	}
	idx := int(e.lookaheadRead) + offset
	if idx >= len(e.lookahead) {
		idx -= len(e.lookahead)
	}
	return &e.lookahead[idx], true
}

func (e *VP9Encoder) iterateVP9TemporalFilter(strength int, refs []arnrFrameView, centerIdx int, doChroma bool) {
	if uint(centerIdx) >= uint(len(refs)) {
		return
	}
	dst := arnrViewFromYCbCr(&e.vp9ARNRScratch)
	mbCols := (dst.width + 15) >> 4
	mbRows := (dst.height + 15) >> 4
	if mbCols|mbRows == 0 {
		return
	}

	var accumulator [384]uint32
	var count [384]uint32
	for mbRow := 0; mbRow < mbRows; mbRow++ {
		mbY := mbRow << 4
		for mbCol := 0; mbCol < mbCols; mbCol++ {
			mbX := mbCol << 4
			processARNRMacroblock(&dst, refs, centerIdx, mbRow, mbCol,
				mbRows, mbCols, mbX, mbY, strength, doChroma,
				accumulator[:], count[:])
		}
	}
}

func arnrViewFromYCbCr(img *image.YCbCr) arnrFrameView {
	return arnrFrameView{
		width:   img.Rect.Dx(),
		height:  img.Rect.Dy(),
		y:       img.Y,
		u:       img.Cb,
		v:       img.Cr,
		yStride: img.YStride,
		uStride: img.CStride,
		vStride: img.CStride,
	}
}
