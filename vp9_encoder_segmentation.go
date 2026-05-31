package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9UpdateCyclicRefreshParameters runs libvpx's
// vp9_cyclic_refresh_update_parameters() before vp9_rc_regulate_q so
// weight_segment and apply_cyclic_refresh match the regulate-q model.
//
// libvpx: vp9/encoder/vp9_encoder.c (encode path before pick_q).
func (e *VP9Encoder) vp9UpdateCyclicRefreshParameters(isKey, intraOnly, showFrame bool, miRows, miCols, macroblocks int, refreshFlags uint8, lossless bool) {
	if e == nil || !e.cyclicAQ.Enabled {
		e.cyclicAQ.ApplyCyclicRefresh = false
		return
	}
	if isKey || intraOnly || !showFrame {
		e.cyclicAQ.ApplyCyclicRefresh = false
		if isKey && e.cyclicAQ.MIRows == miRows && e.cyclicAQ.MICols == miCols {
			for i := range e.cyclicAQ.LastCodedQMap {
				e.cyclicAQ.LastCodedQMap[i] = vp9dec.MaxQ
			}
			for i := range e.cyclicAQ.ConsecZeroMV {
				e.cyclicAQ.ConsecZeroMV[i] = 0
			}
			e.cyclicAQ.SBIndex = 0
			e.cyclicAQ.ReduceRefresh = false
			e.cyclicAQ.CounterEncodeMaxqSceneChange = 0
		}
		return
	}
	if e.cyclicAQ.MIRows != miRows || e.cyclicAQ.MICols != miCols ||
		len(e.cyclicAQ.SegMap) < miRows*miCols {
		e.cyclicAQ.Alloc(miRows, miCols)
	}
	screen := e.opts.ScreenContentMode > 0
	noiseMedium := e.opts.NoiseSensitivity >= 1
	e.cyclicAQ.UpdateParameters(encoder.CyclicRefreshUpdateParametersArgs{
		Macroblocks:          macroblocks,
		FrameIsIntraOnly:     false,
		TemporalLayerID:      0,
		NumberTemporalLayers: 1,
		NumberSpatialLayers:  1,
		SpatialLayerID:       0,
		Lossless:             lossless,
		UseSVC:               false,
		ScreenContent:        screen,
		NoiseLevelMedium:     noiseMedium,
		RateControlIsVBR:     e.rc.mode == RateControlVBR,
		RefreshGoldenFrame:   refreshFlags&(1<<vp9GoldenRefSlot) != 0,
		AvgFrameQindexInter:  int(e.rc.avgFrameQIndexInter),
		AvgFrameLowMotion:    e.rc.avgFrameLowMotion,
		FramesSinceKey:       int(e.rc.framesSinceKey),
		BestQuality:          int(e.rc.bestQuality),
		AvgFrameBandwidth:    e.rc.bitsPerFrame,
		Width:                e.opts.Width,
		Height:               e.opts.Height,
	})
}

// vp9PrepareCyclicRefreshFrame drives vp9_cyclic_refresh_setup()
// (vp9/encoder/vp9_aq_cyclicrefresh.c:596-680) after the base qindex
// is known. UpdateParameters runs earlier via
// vp9UpdateCyclicRefreshParameters.
// vp9SegEnabledForLoopfilter reports whether cm->seg.enabled is true for
// vp9_picklpf's cyclic-refresh 5/8 scale at the pre-tile loopfilter
// placeholder. header.Seg is not wired yet at that site; cyclic refresh's
// Apply latch is the earliest faithful signal.
func (e *VP9Encoder) vp9SegEnabledForLoopfilter(isKey, intraOnly bool) bool {
	if e == nil || isKey || intraOnly {
		return false
	}
	if e.opts.Segmentation.Enabled || e.roi.enabled || e.activeMapEnabled {
		return true
	}
	switch e.opts.AQMode {
	case VP9AQVariance:
		return true
	case VP9AQComplexity:
		return e.vp9ComplexityAQSB64TargetRate() >= encoder.ComplexityAQMinSB64TargetRate
	case VP9AQEquator360:
		return encoder.Equator360AQApplies(e.opts.Width, e.opts.Height)
	case VP9AQPerceptual:
		return true
	case VP9AQCyclicRefresh:
		// Apply is latched in vp9PrepareCyclicRefreshFrame; ApplyCyclicRefresh
		// from vp9UpdateCyclicRefreshParameters is available earlier for the
		// loopfilter placeholder (libvpx vp9_picklpf.c:113 uses cm->seg.enabled
		// after cyclic setup has decided the frame will carry a segment map).
		return e.cyclicAQ.Enabled && e.cyclicAQ.ApplyCyclicRefresh &&
			e.cyclicAQ.ContentMode
	}
	return false
}

func (e *VP9Encoder) vp9PrepareCyclicRefreshFrame(isKey, intraOnly, showFrame bool, miRows, miCols, macroblocks int, header *vp9dec.UncompressedHeader, srcFrameAltRef bool, refreshFlags uint8) {
	if e == nil || !e.cyclicAQ.Enabled {
		e.cyclicAQ.Apply = false
		return
	}
	if isKey || intraOnly || !showFrame {
		e.cyclicAQ.Apply = false
		return
	}
	screen := e.opts.ScreenContentMode > 0
	noiseMedium := e.opts.NoiseSensitivity >= 1
	// libvpx: vp9_aq_cyclicrefresh.c:596-680.
	e.cyclicAQ.Setup(encoder.CyclicRefreshSetupArgs{
		CurrentVideoFrame: e.frameIndex,
		FrameIsKey:        false,
		FrameIsIntraOnly:  false,
		TemporalLayerID:   0,
		ResizePending:     e.cyclicResizeFramePending,
		HighSourceSad:     e.rc.highSourceSAD,
		ScreenContent:     screen,
		NoiseLevelMedium:  noiseMedium,
		BaseQindex:        int(header.Quant.BaseQindex),
		YDcDeltaQ:         int(header.Quant.YDcDeltaQ),
		Sb64TargetRate:    e.rc.frameTargetBits >> 6,
		// libvpx: vp9_aq_cyclicrefresh.c:439 — consec_zero_mv feeds the
		// update_map eligibility filter. The slice is maintained per
		// encoded SB by vp9CyclicRefreshUpdateEncodedSb so this frame
		// sees the previous frame's stationarity history.
		ConsecZeroMv: e.cyclicAQ.ConsecZeroMV,
		// Feed the resolved refresh mask into setup so RDMult tracks
		// the same frame-type bucket as libvpx's cyclic_refresh_setup.
		IsSrcFrameAltRef:   srcFrameAltRef,
		RefreshGoldenFrame: refreshFlags&(1<<vp9GoldenRefSlot) != 0,
		RefreshAltRefFrame: refreshFlags&(1<<vp9AltRefSlot) != 0,
	})
	e.cyclicAQ.Apply = e.cyclicAQ.ApplyCyclicRefresh && e.cyclicAQ.TargetNumSegBlocks > 0
	e.vp9ApplyCyclicRefreshRDMult(isKey, intraOnly)
}

// vp9ApplyCyclicRefreshRDMult is a no-op on rc.rdmult. libvpx stores
// cr->rdmult in CYCLIC_REFRESH but applies it only per block when
// cyclic_refresh_segment_id_boosted() is true (vp9_encodeframe.c:1958-1962,
// 4417-4419). The frame-level cpi->rd.RDMULT from vp9_initialize_rd_consts
// remains in effect for base-segment mode scoring; govpx mirrors that via
// pickVP9InterReferenceModeNonRD's cbRdmult override on boosted segments.
func (e *VP9Encoder) vp9ApplyCyclicRefreshRDMult(isKey, intraOnly bool) {
	_ = isKey
	_ = intraOnly
}

func (e *VP9Encoder) vp9UpdateCyclicRefreshInterSegment(inter *vp9InterEncodeState,
	seg *vp9dec.SegmentationParams, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, mi *vp9dec.NeighborMi, decision vp9InterModeDecision,
) {
	if e == nil || inter == nil || mi == nil ||
		e.opts.AQMode != VP9AQCyclicRefresh || !e.vp9InterUsesNonrdPickmode() ||
		!e.cyclicAQ.Enabled || !e.cyclicAQ.Apply || !e.cyclicAQ.ContentMode ||
		e.cyclicAQ.MIRows != miRows || e.cyclicAQ.MICols != miCols {
		return
	}
	isInter := !decision.intra && decision.refFrame > vp9dec.IntraFrame
	args := encoder.CyclicRefreshUpdateSegmentArgs{
		MIRow:            miRow,
		MICol:            miCol,
		BSize:            bsize,
		SegmentID:        mi.SegmentID,
		RefFrame:         decision.refFrame,
		MvRow:            decision.mv[0].Row,
		MvCol:            decision.mv[0].Col,
		Rate:             decision.rate,
		Dist:             decision.distortion,
		IsInter:          isInter,
		Skip:             decision.skip,
		UseNonrdPickMode: true,
		RateControlIsVBR: e.rc.mode == RateControlVBR,
	}
	segID := e.cyclicAQ.UpdateSegment(args)
	if segID >= vp9dec.MaxSegments {
		segID = 0
	}
	mi.SegmentID = segID
	if seg != nil && seg.Enabled && seg.UpdateMap {
		if seg.TemporalUpdate {
			mi.SegIDPredicted = e.vp9EncoderSegmentMapPredicted(miRows, miCols,
				miRow, miCol, bsize, segID)
		} else {
			mi.SegIDPredicted = 0
		}
	}
}

func (e *VP9Encoder) vp9EncoderSegmentationParams(intraFrame bool, baseQIndex int) vp9dec.SegmentationParams {
	if e.roi.enabled && !intraFrame {
		seg := e.roi.segmentationParams()
		if e.activeMapEnabled {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQVariance {
		// libvpx's vp9_aq_variance.c only recomputes the per-segment
		// AltQ deltas on intra / alt-ref / golden frames; the deltas
		// persist on the shared cm->seg between frames so inter
		// frames re-use the keyframe-anchored values. Mirroring that
		// behaviour matters because recomputing deltas at the live
		// (potentially higher) inter qindex would scale the swings
		// linearly with frame Q and blow up rate on flat regions.
		anchorQindex := baseQIndex
		if intraFrame || !e.varianceAQDeltaQindexSet {
			e.varianceAQDeltaQindex = baseQIndex
			e.varianceAQDeltaQindexSet = true
		} else {
			anchorQindex = e.varianceAQDeltaQindex
		}
		filmContent := e.opts.ScreenContentMode == int8(VP9ScreenContentFilm)
		seg := encoder.VarianceAQSegmentationParams(anchorQindex, filmContent)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQComplexity {
		if e.vp9ComplexityAQSB64TargetRate() < encoder.ComplexityAQMinSB64TargetRate {
			return vp9dec.SegmentationParams{}
		}
		seg := encoder.ComplexityAQSegmentationParams(baseQIndex)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQEquator360 && encoder.Equator360AQApplies(e.opts.Width, e.opts.Height) {
		seg := encoder.Equator360AQSegmentationParams(baseQIndex, intraFrame)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQPerceptual {
		seg := e.perceptualAQ.SegmentationParams(intraFrame)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.cyclicAQ.Enabled && e.cyclicAQ.Apply && !intraFrame {
		seg := e.cyclicAQ.SegmentationParams(baseQIndex)
		if e.activeMapEnabled {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	cfg := e.opts.Segmentation
	if !cfg.Enabled {
		if e.activeMapEnabled && !intraFrame {
			seg := vp9dec.SegmentationParams{
				Enabled:   true,
				UpdateMap: true,
			}
			initVP9SegmentationProbDefaults(&seg)
			vp9EnableActiveMapSegmentation(&seg)
			return seg
		}
		return vp9dec.SegmentationParams{}
	}
	seg := vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: cfg.UpdateMap,
		AbsDelta:  cfg.AbsDelta,
	}
	initVP9SegmentationProbDefaults(&seg)
	for i := range vp9dec.MaxSegments {
		if cfg.AltQEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
			seg.FeatureData[i][vp9dec.SegLvlAltQ] = cfg.AltQ[i]
			seg.UpdateData = true
		}
		if cfg.AltLFEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltLf)
			seg.FeatureData[i][vp9dec.SegLvlAltLf] = cfg.AltLF[i]
			seg.UpdateData = true
		}
		if cfg.SkipEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlSkip)
			seg.UpdateData = true
		}
		if cfg.RefFrameEnabled[i] {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlRefFrame)
			seg.FeatureData[i][vp9dec.SegLvlRefFrame] = int16(cfg.RefFrame[i])
			seg.UpdateData = true
		}
	}
	if e.activeMapEnabled && !intraFrame {
		vp9EnableActiveMapSegmentation(&seg)
	}
	return seg
}

func initVP9SegmentationProbDefaults(seg *vp9dec.SegmentationParams) {
	if seg == nil {
		return
	}
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = vp9dec.MaxProb
	}
	for i := range vp9dec.PredictionProbs {
		seg.PredProbs[i] = vp9dec.MaxProb
	}
}

func vp9EnableActiveMapSegmentation(seg *vp9dec.SegmentationParams) {
	if seg == nil {
		return
	}
	seg.Enabled = true
	seg.UpdateMap = true
	seg.UpdateData = true
	seg.TemporalUpdate = true
	for i := range vp9dec.SegTreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.PredProbs[0] = 1
	for i := 1; i < vp9dec.PredictionProbs; i++ {
		seg.PredProbs[i] = 128
	}
	seg.FeatureMask[vp9ActiveMapSegmentInactive] |=
		1 << uint(vp9dec.SegLvlSkip)
	seg.FeatureMask[vp9ActiveMapSegmentInactive] |=
		1 << uint(vp9dec.SegLvlAltLf)
	seg.FeatureData[vp9ActiveMapSegmentInactive][vp9dec.SegLvlAltLf] =
		-vp9dec.MaxLoopFilter
}

func (e *VP9Encoder) vp9CarryActiveMapDisableSegmentation(
	seg *vp9dec.SegmentationParams, intraFrame bool,
) {
	if e == nil || seg == nil || seg.Enabled || intraFrame ||
		e.activeMapEnabled || !e.prevSegmentationValid ||
		!vp9SegmentationIsActiveMapOnly(&e.prevSegmentation) {
		return
	}
	*seg = e.prevSegmentation
	seg.Enabled = true
	seg.UpdateMap = e.prevFrameActiveMapEnabled
	seg.UpdateData = false
}

func vp9SegmentationIsActiveMapOnly(seg *vp9dec.SegmentationParams) bool {
	if seg == nil || !seg.Enabled {
		return false
	}
	for i := range vp9dec.MaxSegments {
		mask := seg.FeatureMask[i]
		if i != int(vp9ActiveMapSegmentInactive) {
			if mask != 0 {
				return false
			}
			continue
		}
		want := uint32((1 << uint(vp9dec.SegLvlSkip)) |
			(1 << uint(vp9dec.SegLvlAltLf)))
		if mask != want ||
			seg.FeatureData[i][vp9dec.SegLvlAltLf] != -vp9dec.MaxLoopFilter {
			return false
		}
		for j := range vp9dec.SegLvlMax {
			if j == vp9dec.SegLvlAltLf {
				continue
			}
			if seg.FeatureData[i][j] != 0 {
				return false
			}
		}
	}
	return true
}

func (e *VP9Encoder) vp9ReuseStableSegmentationState(seg *vp9dec.SegmentationParams,
	intraFrame bool, miRows, miCols int, inter *vp9InterEncodeState,
) {
	if e == nil || seg == nil || !seg.Enabled || intraFrame ||
		!e.prevSegmentationValid || !e.vp9DynamicSegmentMapActive() {
		return
	}
	prev := e.prevSegmentation
	if prev.Enabled && e.opts.AQMode != VP9AQCyclicRefresh &&
		vp9SegmentationDataEqual(seg, &prev) {
		seg.UpdateData = false
	}
	if prev.Enabled && seg.UpdateMap &&
		e.opts.AQMode != VP9AQCyclicRefresh &&
		e.vp9SegmentMapMatchesPrevious(miRows, miCols, inter) {
		seg.UpdateMap = false
		seg.TemporalUpdate = prev.TemporalUpdate
		seg.TreeProbs = prev.TreeProbs
		seg.PredProbs = prev.PredProbs
	}
}

func vp9SegmentationDataEqual(a, b *vp9dec.SegmentationParams) bool {
	if a == nil || b == nil {
		return false
	}
	if a.AbsDelta != b.AbsDelta {
		return false
	}
	for i := range vp9dec.MaxSegments {
		if a.FeatureMask[i] != b.FeatureMask[i] {
			return false
		}
		for j := range vp9dec.SegLvlMax {
			if a.FeatureData[i][j] != b.FeatureData[i][j] {
				return false
			}
		}
	}
	return true
}

func (e *VP9Encoder) vp9SegmentMapMatchesPrevious(miRows, miCols int,
	inter *vp9InterEncodeState,
) bool {
	if e == nil || !e.useVP9EncoderPrevSegmentMap(miRows, miCols) {
		return false
	}
	staticSegID := e.vp9StaticSegmentIDForMap()
	for miRow := range miRows {
		row := e.prevSegmentMap[miRow*miCols:]
		for miCol := range miCols {
			if row[miCol] != e.vp9PartitionSegmentID(miRow, miCol,
				staticSegID, nil, inter) {
				return false
			}
		}
	}
	return true
}
