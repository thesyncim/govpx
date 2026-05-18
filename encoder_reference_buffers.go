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
	e.clearLatestLookaheadReferenceSets()
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
		e.clearLatestLookaheadReferenceFrame(ReferenceLast)
	}
	if cfg.RefreshGolden && refFrame != vp8common.GoldenFrame {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceGolden)
	}
	if cfg.RefreshAltRef && refFrame != vp8common.AltRefFrame {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceAltRef)
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
		e.clearLatestLookaheadReferenceFrame(ReferenceLast)
	}
	if cfg.RefreshGolden {
		copyFrameImage(&e.goldenRef.Img, &e.current.Img)
		e.goldenRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceGolden)
	}
	if cfg.RefreshAltRef {
		copyFrameImage(&e.altRef.Img, &e.current.Img)
		e.altRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceAltRef)
	}
	e.updateInterReferenceAliases(cfg)
	e.updateInterReferenceFrameNumbers(cfg)
}

// updateInterReferenceAliases mirrors libvpx vp8/encoder/onyx_if.c
// update_reference_frames (lines 2860-2950) by tracking which alias-state
// transitions follow each combination of refresh_*_frame and
// copy_buffer_to_arf / copy_buffer_to_gf settings.
//
// libvpx threads cm->lst_fb_idx, cm->gld_fb_idx, cm->alt_fb_idx through the
// same update_reference_frames block; the aliases are simply identity tests
// on the post-update slot indices. When neither refresh nor copy touches
// a slot, the corresponding alias preserves its previous value because
// the underlying _fb_idx pointers do not move.
//
// The CopyBuffer paths (encoder-internal copy_buffer_to_arf=2 from
// shouldCopyOldGoldenToAltRefOnGoldenRefresh, and the wire-decodable
// copy_buffer_to_arf=1 / copy_buffer_to_gf=1/2 values the encoder never
// emits but the decoder accepts) cause the receiving slot to alias the
// donor slot post-update. The two-pass logic below is required because
// copy_buffer_to_gf=2 (gld = alt) in libvpx assigns the alt-already-
// updated slot, so the golden-vs-last alias must reference the
// freshly-derived altLast rather than the pre-update one.
func (e *VP8Encoder) updateInterReferenceAliases(cfg vp8enc.InterFrameStateConfig) {
	prevGoldenAliasesAlt := e.goldenRefAliasesAlt
	prevAltAliasesLast := e.altRefAliasesLast
	prevGoldenAliasesLast := e.goldenRefAliasesLast

	// libvpx alt_fb_idx update (lines 2879-2909): refresh wins over copy.
	altUpdatedToNew := cfg.RefreshAltRef
	altCopiedFromLast := !cfg.RefreshAltRef && cfg.CopyBufferToAltRef == 1
	altCopiedFromGolden := !cfg.RefreshAltRef && cfg.CopyBufferToAltRef == 2

	// libvpx gld_fb_idx update (lines 2911-2941): refresh wins over copy.
	goldenUpdatedToNew := cfg.RefreshGolden
	goldenCopiedFromLast := !cfg.RefreshGolden && cfg.CopyBufferToGolden == 1
	goldenCopiedFromAlt := !cfg.RefreshGolden && cfg.CopyBufferToGolden == 2

	// libvpx lst_fb_idx update (lines 2944-2950): only refresh_last touches it.
	lastUpdatedToNew := cfg.RefreshLast

	// Derive post-update alt-vs-last alias. When CopyBufferToAltRef=2
	// (alt = prev gold), the alt slot points at the gold_fb_idx slot AS
	// IT WAS BEFORE refresh_golden_frame moves it — that snapshot is
	// the prevGoldenAliasesLast value.
	var altLast bool
	switch {
	case altUpdatedToNew && lastUpdatedToNew:
		altLast = true
	case altUpdatedToNew || lastUpdatedToNew:
		altLast = false
	case altCopiedFromLast:
		altLast = true
	case altCopiedFromGolden:
		altLast = prevGoldenAliasesLast
	default:
		altLast = prevAltAliasesLast
	}

	// Derive post-update golden-vs-last alias. When CopyBufferToGolden=2,
	// libvpx assigns gld_fb_idx = alt_fb_idx AFTER the alt slot has been
	// re-pointed, so the golden-vs-last alias must reference the
	// freshly-computed altLast above.
	var goldenLast bool
	switch {
	case goldenUpdatedToNew && lastUpdatedToNew:
		goldenLast = true
	case goldenUpdatedToNew || lastUpdatedToNew:
		goldenLast = false
	case goldenCopiedFromLast:
		goldenLast = true
	case goldenCopiedFromAlt:
		goldenLast = altLast
	default:
		goldenLast = prevGoldenAliasesLast
	}

	// Derive post-update golden-vs-alt alias. The copy_buffer_to_gf==2
	// and copy_buffer_to_arf==2 paths each leave the receiving slot
	// pointing at the other slot, so golden and alt end up aliased in
	// both directions.
	var goldenAlt bool
	switch {
	case goldenUpdatedToNew && altUpdatedToNew:
		goldenAlt = true
	case goldenUpdatedToNew || altUpdatedToNew:
		goldenAlt = false
	case goldenCopiedFromAlt:
		goldenAlt = true
	case altCopiedFromGolden:
		goldenAlt = true
	case goldenCopiedFromLast && altCopiedFromLast:
		goldenAlt = true
	case goldenCopiedFromLast:
		goldenAlt = prevAltAliasesLast
	case altCopiedFromLast:
		goldenAlt = prevGoldenAliasesLast
	default:
		goldenAlt = prevGoldenAliasesAlt
	}

	e.altRefAliasesLast = altLast
	e.goldenRefAliasesLast = goldenLast
	e.goldenRefAliasesAlt = goldenAlt
}

func (e *VP8Encoder) copyInterFrameReferences(cfg vp8enc.InterFrameStateConfig) {
	switch cfg.CopyBufferToAltRef {
	case 1:
		copyFrameImage(&e.altRef.Img, &e.lastRef.Img)
		e.altRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceAltRef)
	case 2:
		copyFrameImage(&e.altRef.Img, &e.goldenRef.Img)
		e.altRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceAltRef)
	}
	switch cfg.CopyBufferToGolden {
	case 1:
		copyFrameImage(&e.goldenRef.Img, &e.lastRef.Img)
		e.goldenRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceGolden)
	case 2:
		copyFrameImage(&e.goldenRef.Img, &e.altRef.Img)
		e.goldenRef.ExtendBorders()
		e.clearLatestLookaheadReferenceFrame(ReferenceGolden)
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
