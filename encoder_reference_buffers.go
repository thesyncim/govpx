package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func (e *VP8Encoder) refreshKeyFrameReferencesFromAnalysis() {
	e.resetGoldenFrameStats()
	copyFrameImage(&e.current.Img, &e.analysis.Img)
	e.current.ExtendBorders()
	copyFrameImage(&e.lastRef.Img, &e.current.Img)
	e.lastRef.ExtendBorders()
	copyFrameImage(&e.goldenRef.Img, &e.current.Img)
	e.goldenRef.ExtendBorders()
	copyFrameImage(&e.altRef.Img, &e.current.Img)
	e.altRef.ExtendBorders()
	e.lastFrameInterModesValid = false
	e.goldenRefAliasesLast = true
	e.altRefAliasesLast = true
	e.goldenRefAliasesAlt = true
	e.updateKeyFrameReferenceFrameNumbers()
}

func (e *VP8Encoder) rememberLastFrameInterModes(signBias [vp8common.MaxRefFrames]bool) {
	if len(e.interFrameModes) == 0 {
		return
	}
	if len(e.lastFrameInterModes) != len(e.interFrameModes) {
		e.lastFrameInterModes = make([]vp8enc.InterFrameMacroblockMode, len(e.interFrameModes))
	}
	if len(e.lastFrameInterModeBias) != len(e.interFrameModes) {
		e.lastFrameInterModeBias = make([]bool, len(e.interFrameModes))
	}
	copy(e.lastFrameInterModes, e.interFrameModes)
	for i := range e.interFrameModes {
		ref := e.interFrameModes[i].RefFrame
		e.lastFrameInterModeBias[i] = ref > vp8common.IntraFrame && ref < vp8common.MaxRefFrames && signBias[ref]
	}
	e.lastFrameInterModesValid = true
}

func (e *VP8Encoder) refreshZeroInterFrameReferences(cfg vp8enc.InterFrameStateConfig, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame) {
	copyFrameImage(&e.current.Img, ref)
	e.current.ExtendBorders()
	e.copyInterFrameReferences(cfg)
	if cfg.RefreshLast && refFrame != vp8common.LastFrame {
		copyFrameImage(&e.lastRef.Img, &e.current.Img)
		e.lastRef.ExtendBorders()
	}
	if cfg.RefreshGolden && refFrame != vp8common.GoldenFrame {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
	}
	if cfg.RefreshAltRef && refFrame != vp8common.AltRefFrame {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
	}
	e.updateInterReferenceAliases(cfg)
	e.updateInterReferenceFrameNumbers(cfg)
}

func (e *VP8Encoder) refreshInterFrameReferencesFromAnalysis(cfg vp8enc.InterFrameStateConfig) {
	copyFrameImage(&e.current.Img, &e.analysis.Img)
	e.current.ExtendBorders()
	e.copyInterFrameReferences(cfg)
	if cfg.RefreshLast {
		copyFrameImage(&e.lastRef.Img, &e.current.Img)
		e.lastRef.ExtendBorders()
	}
	if cfg.RefreshGolden {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
	}
	if cfg.RefreshAltRef {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
	}
	e.updateInterReferenceAliases(cfg)
	e.updateInterReferenceFrameNumbers(cfg)
}

func (e *VP8Encoder) updateInterReferenceAliases(cfg vp8enc.InterFrameStateConfig) {
	if cfg.RefreshLast && cfg.RefreshGolden {
		e.goldenRefAliasesLast = true
	} else if cfg.RefreshLast != cfg.RefreshGolden {
		e.goldenRefAliasesLast = false
	}
	if cfg.RefreshLast && cfg.RefreshAltRef {
		e.altRefAliasesLast = true
	} else if cfg.RefreshLast != cfg.RefreshAltRef {
		e.altRefAliasesLast = false
	}
	if cfg.RefreshAltRef && cfg.RefreshGolden {
		e.goldenRefAliasesAlt = true
	} else if cfg.RefreshAltRef != cfg.RefreshGolden {
		e.goldenRefAliasesAlt = false
	}
}

func (e *VP8Encoder) copyInterFrameReferences(cfg vp8enc.InterFrameStateConfig) {
	switch cfg.CopyBufferToAltRef {
	case 1:
		copyFrameImage(&e.altRef.Img, &e.lastRef.Img)
		e.altRef.ExtendBorders()
	case 2:
		copyFrameImage(&e.altRef.Img, &e.goldenRef.Img)
		e.altRef.ExtendBorders()
	}
	switch cfg.CopyBufferToGolden {
	case 1:
		copyFrameImage(&e.goldenRef.Img, &e.lastRef.Img)
		e.goldenRef.ExtendBorders()
	case 2:
		copyFrameImage(&e.goldenRef.Img, &e.altRef.Img)
		e.goldenRef.ExtendBorders()
	}
}

func (e *VP8Encoder) updateKeyFrameReferenceFrameNumbers() {
	frameNumber := e.frameCount
	e.referenceFrameNumbers[vp8common.LastFrame] = frameNumber
	e.referenceFrameNumbers[vp8common.GoldenFrame] = frameNumber
	e.referenceFrameNumbers[vp8common.AltRefFrame] = frameNumber
}

func (e *VP8Encoder) updateInterReferenceFrameNumbers(cfg vp8enc.InterFrameStateConfig) {
	frameNumber := e.frameCount

	if cfg.RefreshAltRef {
		e.referenceFrameNumbers[vp8common.AltRefFrame] = frameNumber
	} else {
		switch cfg.CopyBufferToAltRef {
		case 1:
			e.referenceFrameNumbers[vp8common.AltRefFrame] = e.referenceFrameNumbers[vp8common.LastFrame]
		case 2:
			e.referenceFrameNumbers[vp8common.AltRefFrame] = e.referenceFrameNumbers[vp8common.GoldenFrame]
		}
	}

	if cfg.RefreshGolden {
		e.referenceFrameNumbers[vp8common.GoldenFrame] = frameNumber
	} else {
		switch cfg.CopyBufferToGolden {
		case 1:
			e.referenceFrameNumbers[vp8common.GoldenFrame] = e.referenceFrameNumbers[vp8common.LastFrame]
		case 2:
			e.referenceFrameNumbers[vp8common.GoldenFrame] = e.referenceFrameNumbers[vp8common.AltRefFrame]
		}
	}

	if cfg.RefreshLast {
		e.referenceFrameNumbers[vp8common.LastFrame] = frameNumber
	}
}
