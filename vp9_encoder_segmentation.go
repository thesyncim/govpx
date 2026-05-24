package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9PrepareCyclicRefreshFrame drives the libvpx
// vp9_cyclic_refresh_update_parameters() + vp9_cyclic_refresh_setup()
// pair (vp9/encoder/vp9_aq_cyclicrefresh.c:479-680). It is the
// encoder-facing entry that picks up the active rate-control state
// and emits the per-frame segmentation map cyclicAQ.SegmentID()
// consults. Called once per frame in EncodeInto.
func (e *VP9Encoder) vp9PrepareCyclicRefreshFrame(isKey, intraOnly, showFrame bool, miRows, miCols, macroblocks int, header *vp9dec.UncompressedHeader) {
	if e == nil || !e.cyclicAQ.Enabled {
		e.cyclicAQ.Apply = false
		return
	}
	if isKey || intraOnly || !showFrame {
		e.cyclicAQ.Apply = false
		// libvpx: vp9_aq_cyclicrefresh.c:614-621 — keyframe also resets
		// last_coded_q_map / sb_index / scene_change counter.
		if isKey && e.cyclicAQ.MIRows == miRows && e.cyclicAQ.MICols == miCols {
			for i := range e.cyclicAQ.LastCodedQMap {
				e.cyclicAQ.LastCodedQMap[i] = vp9dec.MaxQ
			}
			// libvpx: vp9_encoder.c:4103-4106 — intra_only zeros
			// consec_zero_mv too. Without this, post-key stale counters
			// would still drive the next frame's eligibility filter.
			for i := range e.cyclicAQ.ConsecZeroMV {
				e.cyclicAQ.ConsecZeroMV[i] = 0
			}
			e.cyclicAQ.SBIndex = 0
			e.cyclicAQ.ReduceRefresh = false
			e.cyclicAQ.CounterEncodeMaxqSceneChange = 0
		}
		return
	}
	// Re-alloc on mi-grid change.
	if e.cyclicAQ.MIRows != miRows || e.cyclicAQ.MICols != miCols ||
		len(e.cyclicAQ.SegMap) < miRows*miCols {
		e.cyclicAQ.Alloc(miRows, miCols)
	}
	screen := e.opts.ScreenContentMode > 0
	noiseMedium := e.opts.NoiseSensitivity >= 1
	// libvpx: vp9_aq_cyclicrefresh.c:479-593.
	e.cyclicAQ.UpdateParameters(encoder.CyclicRefreshUpdateParametersArgs{
		Macroblocks:          macroblocks,
		FrameIsIntraOnly:     false,
		TemporalLayerID:      0,
		NumberTemporalLayers: 1,
		NumberSpatialLayers:  1,
		SpatialLayerID:       0,
		Lossless:             header.Quant.Lossless,
		UseSVC:               false,
		ScreenContent:        screen,
		NoiseLevelMedium:     noiseMedium,
		RateControlIsVBR:     e.rc.mode == RateControlVBR,
		RefreshGoldenFrame:   false,
		AvgFrameQindexInter:  int(e.rc.avgFrameQIndexInter),
		AvgFrameLowMotion:    100, // libvpx default until measured.
		FramesSinceKey:       int(e.rc.framesSinceKey),
		BestQuality:          int(e.rc.bestQuality),
		AvgFrameBandwidth:    e.rc.bitsPerFrame,
		Width:                e.opts.Width,
		Height:               e.opts.Height,
	})
	// libvpx: vp9_aq_cyclicrefresh.c:596-680.
	e.cyclicAQ.Setup(encoder.CyclicRefreshSetupArgs{
		CurrentVideoFrame: e.frameIndex,
		FrameIsKey:        false,
		FrameIsIntraOnly:  false,
		TemporalLayerID:   0,
		ResizePending:     false,
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
		// CR runs on visible inter frames only (see early-returns above),
		// so is_src_frame_alt_ref is always false here.  The refresh
		// flags are not yet known at this point in govpx (RefreshFrame
		// is set later in EncodeInto), so we conservatively pass false
		// for both — matching libvpx's path because cyclic_refresh_setup
		// runs before refresh_golden/alt are finalised in many of its
		// realtime call paths.  The CR RDMult therefore lands in the
		// inter bucket which is what libvpx's realtime CR runs evaluate.
		IsSrcFrameAltRef:   false,
		RefreshGoldenFrame: false,
		RefreshAltRefFrame: false,
	})
	e.cyclicAQ.Apply = e.cyclicAQ.ApplyCyclicRefresh && e.cyclicAQ.TargetNumSegBlocks > 0
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
		// In fixed-Q / pure-Q mode the rate controller cannot
		// absorb variance-AQ's per-segment qindex shifts: the
		// low-variance bonus segments over-spend bits on flat
		// regions, the segment map and segment-aware partition
		// splits add overhead, and the user-chosen quality anchor
		// is left unanchored. Suppress map/data updates in that
		// mode — variance-AQ becomes a header-only no-op rather than
		// the +70%+ BD-rate regression that the buggy v1 implementation
		// produced on synthetic half-flat content. Rate-controlled
		// pipelines (CBR/VBR) still get the perceptual benefit
		// because the rate loop compensates for the qindex shift.
		if e.vp9VarianceAQRateControlFixedQ() {
			if e.activeMapEnabled && !intraFrame {
				seg := vp9dec.SegmentationParams{
					Enabled:   true,
					UpdateMap: true,
				}
				initVP9SegmentationProbDefaults(&seg)
				vp9EnableActiveMapSegmentation(&seg)
				return seg
			}
			return vp9dec.SegmentationParams{Enabled: true}
		}
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
		seg := vp9VarianceAQSegmentationParams(anchorQindex, e.opts.ScreenContentMode)
		if e.activeMapEnabled && !intraFrame {
			vp9EnableActiveMapSegmentation(&seg)
		}
		return seg
	}
	if e.opts.AQMode == VP9AQComplexity {
		if e.vp9ComplexityAQSB64TargetRate() < vp9ComplexityAQMinSB64TargetRate {
			return vp9dec.SegmentationParams{}
		}
		seg := vp9ComplexityAQSegmentationParams(baseQIndex)
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

func vp9VarianceAQSegmentationParams(baseQIndex int, screenContentMode int8) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initVP9SegmentationProbDefaults(&seg)
	ratios := vp9VarianceAQRateRatiosForContent(screenContentMode)
	for i, ratio := range ratios {
		if ratio.num == ratio.den {
			continue
		}
		delta := encoder.ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}

// vp9VarianceAQRateRatiosForContent returns the per-segment rate
// ratios used to derive variance-AQ Q deltas. Default video uses
// libvpx's table where the highest-variance segment (index 4) is
// pushed up in Q by a 3:4 ratio. VP9ScreenContentFilm clamps that
// segment back to 1:1, preserving film-grain texture by leaving the
// high-variance blocks at the base Q.
func vp9VarianceAQRateRatiosForContent(screenContentMode int8) [vp9dec.MaxSegments]struct {
	num int
	den int
} {
	if screenContentMode == int8(VP9ScreenContentFilm) {
		return vp9VarianceAQRateRatiosFilm
	}
	return vp9VarianceAQRateRatios
}

var vp9VarianceAQRateRatios = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{5, 2},
	{2, 1},
	{3, 2},
	{1, 1},
	{3, 4},
	{1, 1},
	{1, 1},
	{1, 1},
}

// vp9VarianceAQRateRatiosFilm is the FILM-content variant of
// vp9VarianceAQRateRatios. Segments 0..2 keep their low-variance Q
// boost so flat areas are still coded cleanly; segment 4 is held at
// 1:1 instead of 3:4 so the encoder leaves the high-variance grain
// blocks at the base Q and the grain texture survives quantization.
var vp9VarianceAQRateRatiosFilm = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{5, 2},
	{2, 1},
	{3, 2},
	{1, 1},
	{1, 1},
	{1, 1},
	{1, 1},
	{1, 1},
}

const (
	vp9ComplexityAQSegments          = 5
	vp9ComplexityAQDefaultSegment    = 3
	vp9ComplexityAQStrengths         = 3
	vp9ComplexityAQMinSB64TargetRate = 256
	vp9ComplexityAQLowVarThreshold   = 10.0
)

var vp9ComplexityAQRateRatios = [vp9ComplexityAQStrengths][vp9ComplexityAQSegments]struct {
	num int
	den int
}{
	{{7, 4}, {5, 4}, {21, 20}, {1, 1}, {9, 10}},
	{{2, 1}, {3, 2}, {23, 20}, {1, 1}, {17, 20}},
	{{5, 2}, {7, 4}, {5, 4}, {1, 1}, {4, 5}},
}

var vp9ComplexityAQTransitions = [vp9ComplexityAQStrengths][vp9ComplexityAQSegments]struct {
	num int
	den int
}{
	{{15, 100}, {30, 100}, {55, 100}, {2, 1}, {100, 1}},
	{{20, 100}, {40, 100}, {65, 100}, {2, 1}, {100, 1}},
	{{25, 100}, {50, 100}, {75, 100}, {2, 1}, {100, 1}},
}

var vp9ComplexityAQVarThresholds = [vp9ComplexityAQStrengths][vp9ComplexityAQSegments]float64{
	{-4.0, -3.0, -2.0, 100.0, 100.0},
	{-3.5, -2.5, -1.5, 100.0, 100.0},
	{-3.0, -2.0, -1.0, 100.0, 100.0},
}

func vp9ComplexityAQSegmentationParams(baseQIndex int) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initVP9SegmentationProbDefaults(&seg)
	strength := vp9ComplexityAQStrength(baseQIndex)
	for i, ratio := range vp9ComplexityAQRateRatios[strength] {
		if i == vp9ComplexityAQDefaultSegment || ratio.num == ratio.den {
			continue
		}
		delta := encoder.ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if baseQIndex+delta <= 0 {
			continue
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}

func vp9ComplexityAQStrength(baseQIndex int) int {
	baseQuant := int(vp9dec.VpxAcQuant(baseQIndex, 0, vp9dec.BitDepth8)) / 4
	strength := 0
	if baseQuant > 10 {
		strength++
	}
	if baseQuant > 25 {
		strength++
	}
	return strength
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
	if prev.Enabled && vp9SegmentationDataEqual(seg, &prev) {
		seg.UpdateData = false
	}
	if prev.Enabled && seg.UpdateMap &&
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
