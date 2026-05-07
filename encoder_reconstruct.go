package govpx

import (
	"math"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

var wholeBlockIntraYModeCandidates = [...]vp8common.MBPredictionMode{
	vp8common.DCPred,
	vp8common.VPred,
	vp8common.HPred,
	vp8common.TMPred,
}

var wholeBlockIntraUVModeCandidates = [...]vp8common.MBPredictionMode{
	vp8common.DCPred,
	vp8common.VPred,
	vp8common.HPred,
	vp8common.TMPred,
}

var bPredIntraModeCandidates = [...]vp8common.BPredictionMode{
	vp8common.BDCPred,
	vp8common.BTMPred,
	vp8common.BVEPred,
	vp8common.BHEPred,
	vp8common.BLDPred,
	vp8common.BRDPred,
	vp8common.BVRPred,
	vp8common.BVLPred,
	vp8common.BHDPred,
	vp8common.BHUPred,
}

func libvpxFrameQuantDeltas(qIndex int, screenContentMode int) vp8common.QuantDeltas {
	var deltas vp8common.QuantDeltas
	if qIndex < 4 {
		deltas.Y2DC = 4 - qIndex
	}
	if screenContentMode != 0 && qIndex > 40 {
		uvDelta := -(15 * qIndex / 100)
		if uvDelta < -15 {
			uvDelta = -15
		}
		deltas.UVDC = uvDelta
		deltas.UVAC = uvDelta
	}
	return deltas
}

func quantHeaderForFrame(qIndex int, deltas vp8common.QuantDeltas) vp8dec.QuantHeader {
	return vp8dec.QuantHeader{
		BaseQIndex: uint8(qIndex),
		Y1DCDelta:  int8(deltas.Y1DC),
		Y2DCDelta:  int8(deltas.Y2DC),
		Y2ACDelta:  int8(deltas.Y2AC),
		UVDCDelta:  int8(deltas.UVDC),
		UVACDelta:  int8(deltas.UVAC),
	}
}

var libvpxFastInterModeOrder = [...]vp8common.MBPredictionMode{
	vp8common.ZeroMV, vp8common.DCPred,
	vp8common.NearestMV, vp8common.NearMV,
	vp8common.ZeroMV, vp8common.NearestMV,
	vp8common.ZeroMV, vp8common.NearestMV,
	vp8common.NearMV, vp8common.NearMV,
	vp8common.VPred, vp8common.HPred, vp8common.TMPred,
	vp8common.NewMV, vp8common.NewMV, vp8common.NewMV,
	vp8common.SplitMV, vp8common.SplitMV, vp8common.SplitMV,
	vp8common.BPred,
}

var libvpxFastRefFrameOrder = [...]int{
	1, 0,
	1, 1,
	2, 2,
	3, 3,
	2, 3,
	0, 0, 0,
	1, 2, 3,
	1, 2, 3,
	0,
}

const (
	libvpxInterModeCount = len(libvpxFastInterModeOrder)

	libvpxInterModeThresholdDisabled = -1
	libvpxSpeedMapMax                = 1 << 30
	libvpxRDThreshMultStart          = 128
	libvpxMinThreshMult              = 32
	libvpxMaxThreshMult              = 512
)

const (
	libvpxThrZero1 = iota
	libvpxThrDC
	libvpxThrNearest1
	libvpxThrNear1
	libvpxThrZero2
	libvpxThrNearest2
	libvpxThrZero3
	libvpxThrNearest3
	libvpxThrNear2
	libvpxThrNear3
	libvpxThrVPred
	libvpxThrHPred
	libvpxThrTMPred
	libvpxThrNew1
	libvpxThrNew2
	libvpxThrNew3
	libvpxThrSplit1
	libvpxThrSplit2
	libvpxThrSplit3
	libvpxThrBPred
)

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) error {
	return e.buildReconstructingKeyFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols)
}

func (e *VP8Encoder) buildReconstructingKeyFrameCoefficientsWithSegmentation(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.KeyFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int) error {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return ErrInvalidConfig
	}
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return ErrInvalidConfig
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, segmentation, &quants); err != nil {
		return ErrInvalidConfig
	}
	decSegmentation := encoderSegmentationToDecoder(segmentation)
	vp8dec.InitSegmentDequants(quantHeaderForFrame(qIndex, quantDeltas), &decSegmentation, &e.dequantTables, &e.dequants)

	aboveTok := make([]vp8enc.TokenContextPlanes, cols)
	for row := 0; row < rows; row++ {
		var leftTok vp8enc.TokenContextPlanes
		for col := 0; col < cols; col++ {
			index := row*cols + col
			segmentID, ok := keyFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return ErrInvalidConfig
			}
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			var above *vp8enc.KeyFrameMacroblockMode
			var left *vp8enc.KeyFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			mode, ok := predictBestKeyFrameIntraMode(src, segmentQIndex, row, col, above, left, &aboveTok[col], &leftTok, &quants[segmentID], &e.analysis.Img, &e.reconstructScratch, e.libvpxUseFastQuant())
			if !ok {
				return ErrInvalidConfig
			}
			mode.SegmentID = segmentID
			modes[index] = mode
			convertKeyFrameMode(&modes[index], &e.reconstructModes[index])
			if modes[index].YMode == vp8common.BPred {
				if !buildReconstructingBPredMacroblockCoefficients(&vp8tables.DefaultCoefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, &quants[segmentID], segmentQIndex, 0, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
				convertMacroblockCoefficients(&coeffs[index], true, &e.reconstructTokens[index])
				vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, true, &coeffs[index])
				continue
			}
			if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
			is4x4 := modes[index].YMode == vp8common.BPred
			buildPredictedMacroblockCoefficients(&vp8tables.DefaultCoefProbs, src, row, col, &e.analysis.Img, &aboveTok[col], &leftTok, &quants[segmentID], segmentQIndex, 0, 0, is4x4, true, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index])
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
				return ErrInvalidConfig
			}
			vp8enc.UpdateTokenContextPlanesFromCoefficients(&aboveTok[col], &leftTok, is4x4, &coeffs[index])
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return nil
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficients(src vp8enc.SourceImage, qIndex int, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) error {
	return e.buildReconstructingInterFrameCoefficientsWithSegmentation(src, qIndex, vp8enc.SegmentationConfig{}, false, modes, coeffs, rows, cols, flags)
}

func (e *VP8Encoder) buildReconstructingInterFrameCoefficientsWithSegmentation(src vp8enc.SourceImage, qIndex int, segmentation vp8enc.SegmentationConfig, preserveSegmentID bool, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, rows int, cols int, flags EncodeFlags) error {
	if qIndex < vp8common.MinQ || qIndex > vp8common.MaxQ {
		return ErrInvalidConfig
	}
	// Reset oracle trace MB buffer at the start of each build pass so retried
	// (recoded) attempts overwrite earlier rows.
	e.resetOracleMBTraceBuffer()
	required := rows * cols
	if len(modes) < required || len(coeffs) < required || len(e.reconstructModes) < required || len(e.reconstructTokens) < required {
		return ErrInvalidConfig
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	quantDeltas := libvpxFrameQuantDeltas(qIndex, e.opts.ScreenContentMode)
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, segmentation, &quants); err != nil {
		return ErrInvalidConfig
	}
	decSegmentation := encoderSegmentationToDecoder(segmentation)
	vp8dec.InitSegmentDequants(quantHeaderForFrame(qIndex, quantDeltas), &decSegmentation, &e.dequantTables, &e.dequants)

	var refs [3]interAnalysisReference
	refCount := e.interAnalysisReferences(flags, &refs)
	if refCount == 0 {
		return ErrInvalidConfig
	}
	if e.interAnalysisUsesRDModeDecision() {
		e.beginInterRDModeDecisionFrame()
		defer e.endInterRDModeDecisionFrame()
	}
	aboveTok := make([]vp8enc.TokenContextPlanes, cols)
	activeMapEnabled := e.activeMapEnabled && len(e.activeMap) >= rows*cols
	var lastRefForActiveMap *interAnalysisReference
	if activeMapEnabled {
		for ri := 0; ri < refCount; ri++ {
			if refs[ri].Frame == vp8common.LastFrame {
				lastRefForActiveMap = &refs[ri]
				break
			}
		}
	}
	for row := 0; row < rows; row++ {
		var leftTok vp8enc.TokenContextPlanes
		for col := 0; col < cols; col++ {
			index := row*cols + col
			if activeMapEnabled && lastRefForActiveMap != nil && e.activeMap[index] == 0 {
				if !e.encodeInactiveInterMacroblock(row, col, index, lastRefForActiveMap.Img, modes, coeffs, &aboveTok[col], &leftTok) {
					return ErrInvalidConfig
				}
				continue
			}
			segmentID, ok := interFrameAnalysisSegmentID(&modes[index], segmentation, preserveSegmentID)
			if !ok {
				return ErrInvalidConfig
			}
			var above *vp8enc.InterFrameMacroblockMode
			var left *vp8enc.InterFrameMacroblockMode
			var aboveLeft *vp8enc.InterFrameMacroblockMode
			if row > 0 {
				above = &modes[index-cols]
			}
			if col > 0 {
				left = &modes[index-1]
			}
			if row > 0 && col > 0 {
				aboveLeft = &modes[index-cols-1]
			}
			if e.interAnalysisUsesRDModeDecision() {
				e.beginInterRDModeDecisionMacroblock()
			}
			decision, ok := e.selectInterFrameModeDecision(
				src, refs[:], refCount,
				row, col, rows, cols,
				qIndex, segmentation, segmentID,
				above, left, aboveLeft,
				&aboveTok[col], &leftTok,
				&quants[segmentID],
			)
			if !ok {
				return ErrInvalidConfig
			}
			if segmentID != 0 && !decision.cyclicRefreshEligible() {
				segmentID = 0
				decision, ok = e.selectInterFrameModeDecision(
					src, refs[:], refCount,
					row, col, rows, cols,
					qIndex, segmentation, segmentID,
					above, left, aboveLeft,
					&aboveTok[col], &leftTok,
					&quants[segmentID],
				)
				if !ok {
					return ErrInvalidConfig
				}
			}
			segmentQIndex := encoderSegmentQIndex(qIndex, segmentation, segmentID)
			quant := &quants[segmentID]

			if decision.useIntra {
				modes[index] = decision.intraMode
				modes[index].SegmentID = segmentID
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				if modes[index].Mode == vp8common.BPred {
					if !buildReconstructingBPredMacroblockCoefficients(&e.coefProbs, src, row, col, &e.analysis.Img, &e.reconstructModes[index], &aboveTok[col], &leftTok, quant, segmentQIndex, e.rc.currentZbinOverQuant, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index], &e.reconstructScratch) {
						return ErrInvalidConfig
					}
				} else if !predictAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else {
				modes[index] = decision.interMode
				convertInterFrameMode(&modes[index], &e.reconstructModes[index])
				predMode := e.reconstructModes[index]
				predMode.MBSkipCoeff = true
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &predMode, &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			}
			breakoutSkip := modes[index].RefFrame != vp8common.IntraFrame &&
				staticInterEncodeBreakout(src, &e.analysis.Img, row, col, quant, e.opts.StaticThreshold)
			if breakoutSkip {
				clearMacroblockCoefficients(&coeffs[index])
			} else if modes[index].RefFrame != vp8common.IntraFrame || modes[index].Mode != vp8common.BPred {
				is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
				buildPredictedMacroblockCoefficients(&e.coefProbs, src, row, col, &e.analysis.Img, &aboveTok[col], &leftTok, quant, segmentQIndex, e.rc.currentZbinOverQuant, interZbinModeBoost(&modes[index]), is4x4, modes[index].RefFrame == vp8common.IntraFrame, e.libvpxUseFastQuant(), e.libvpxOptimizeCoefficients(), &coeffs[index])
			}
			is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
			modes[index].MBSkipCoeff = breakoutSkip || macroblockCoefficientsEmpty(&coeffs[index], is4x4)
			convertInterFrameMode(&modes[index], &e.reconstructModes[index])
			convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
			if modes[index].RefFrame == vp8common.IntraFrame && modes[index].Mode == vp8common.BPred {
				updateInterAnalysisTokenContext(&aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index])
				continue
			}
			if modes[index].RefFrame == vp8common.IntraFrame {
				if !reconstructAnalysisMacroblock(&e.analysis.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			} else {
				if !reconstructInterAnalysisMacroblock(&e.analysis.Img, decision.ref.Img, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[segmentID], &e.reconstructScratch) {
					return ErrInvalidConfig
				}
			}
			updateInterAnalysisTokenContext(&aboveTok[col], &leftTok, is4x4, modes[index].MBSkipCoeff, &coeffs[index])
			e.emitOracleMBTrace(row, col, &modes[index], &coeffs[index])
		}
		vp8dec.ExtendIntraRightEdgeForRow(&e.analysis.Img, row)
	}
	e.analysis.ExtendBorders()
	return nil
}

// encodeInactiveInterMacroblock matches libvpx's active-map fast path: an
// inactive macroblock skips mode decision and codes as ZEROMV from LAST with
// MBSkipCoeff=1, no segment override, and no residual. See
// vp8/encoder/pickinter.c evaluate_inter_mode and rdopt.c rd_pick_inter_mode
// active_ptr branches.
func (e *VP8Encoder) encodeInactiveInterMacroblock(row int, col int, index int, lastRef *vp8common.Image, modes []vp8enc.InterFrameMacroblockMode, coeffs []vp8enc.MacroblockCoefficients, above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes) bool {
	modes[index] = vp8enc.InterFrameMacroblockMode{
		SegmentID:   0,
		MBSkipCoeff: true,
		RefFrame:    vp8common.LastFrame,
		Mode:        vp8common.ZeroMV,
		UVMode:      vp8common.DCPred,
	}
	clearMacroblockCoefficients(&coeffs[index])
	convertInterFrameMode(&modes[index], &e.reconstructModes[index])
	is4x4 := interFrameModeUses4x4Tokens(modes[index].Mode)
	convertMacroblockCoefficients(&coeffs[index], is4x4, &e.reconstructTokens[index])
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, lastRef, row, col, &e.reconstructModes[index], &e.reconstructTokens[index], &e.dequants[0], &e.reconstructScratch) {
		return false
	}
	updateInterAnalysisTokenContext(above, left, is4x4, true, &coeffs[index])
	return true
}

func updateInterAnalysisTokenContext(above *vp8enc.TokenContextPlanes, left *vp8enc.TokenContextPlanes, is4x4 bool, skipped bool, coeffs *vp8enc.MacroblockCoefficients) {
	if skipped {
		vp8enc.ResetTokenContextPlanes(above, left, is4x4)
		return
	}
	vp8enc.UpdateTokenContextPlanesFromCoefficients(above, left, is4x4, coeffs)
}

func keyFrameAnalysisSegmentID(mode *vp8enc.KeyFrameMacroblockMode, segmentation vp8enc.SegmentationConfig, preserve bool) (uint8, bool) {
	if !segmentation.Enabled || !preserve {
		return 0, true
	}
	if mode.SegmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	return mode.SegmentID, true
}

func interFrameAnalysisSegmentID(mode *vp8enc.InterFrameMacroblockMode, segmentation vp8enc.SegmentationConfig, preserve bool) (uint8, bool) {
	if !segmentation.Enabled || !preserve {
		return 0, true
	}
	if mode.SegmentID >= vp8common.MaxMBSegments {
		return 0, false
	}
	return mode.SegmentID, true
}

func encoderSegmentQIndex(baseQ int, segmentation vp8enc.SegmentationConfig, segmentID uint8) int {
	if !segmentation.Enabled || segmentID >= vp8common.MaxMBSegments || !segmentation.FeatureEnabled[vp8common.MBLvlAltQ][segmentID] {
		return vp8common.ClampQIndex(baseQ)
	}
	altQ := int(segmentation.FeatureData[vp8common.MBLvlAltQ][segmentID])
	if segmentation.AbsDelta {
		return vp8common.ClampQIndex(altQ)
	}
	return vp8common.ClampQIndex(baseQ + altQ)
}

func encoderSegmentationToDecoder(segmentation vp8enc.SegmentationConfig) vp8dec.SegmentationHeader {
	if !segmentation.Enabled {
		return vp8dec.SegmentationHeader{}
	}
	var out vp8dec.SegmentationHeader
	out.Enabled = true
	out.UpdateMap = segmentation.UpdateMap
	out.UpdateData = segmentation.UpdateData
	out.AbsDelta = segmentation.AbsDelta
	out.TreeProbs = segmentation.TreeProbs
	for feature := 0; feature < int(vp8common.MBLvlMax); feature++ {
		for segment := 0; segment < vp8common.MaxMBSegments; segment++ {
			if segmentation.FeatureEnabled[feature][segment] {
				out.FeatureData[feature][segment] = segmentation.FeatureData[feature][segment]
			}
		}
	}
	return out
}

type interAnalysisReference struct {
	Frame      vp8common.MVReferenceFrame
	Img        *vp8common.Image
	RefRate    int
	RefRateSet bool
}

type interAnalysisMotionCandidate struct {
	Ref interAnalysisReference
	MV  vp8enc.MotionVector
}

func (e *VP8Encoder) interAnalysisReferences(flags EncodeFlags, refs *[3]interAnalysisReference) int {
	count := 0
	lastRate, goldenRate, altRate := e.interReferenceFrameRatesForFlags(flags)
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	if lastEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.LastFrame, Img: &e.lastRef.Img, RefRate: lastRate, RefRateSet: true}
		count++
	}
	if goldenEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.GoldenFrame, Img: &e.goldenRef.Img, RefRate: goldenRate, RefRateSet: true}
		count++
	}
	if altEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.AltRefFrame, Img: &e.altRef.Img, RefRate: altRate, RefRateSet: true}
		count++
	}
	return count
}

func (e *VP8Encoder) closestInterAnalysisReference(refs []interAnalysisReference, refCount int) vp8common.MVReferenceFrame {
	if e == nil {
		return vp8common.LastFrame
	}
	closest := vp8common.IntraFrame
	limit := refCount
	if limit > len(refs) {
		limit = len(refs)
	}
	for i := 0; i < limit; i++ {
		refFrame := refs[i].Frame
		if refFrame < vp8common.LastFrame || refFrame >= vp8common.MaxRefFrames {
			continue
		}
		if closest == vp8common.IntraFrame || e.referenceFrameNumbers[refFrame] > e.referenceFrameNumbers[closest] {
			closest = refFrame
		}
	}
	if closest == vp8common.IntraFrame {
		return vp8common.LastFrame
	}
	return closest
}

func interAnalysisReferencesInclude(refs []interAnalysisReference, refCount int, frame vp8common.MVReferenceFrame) bool {
	limit := refCount
	if limit > len(refs) {
		limit = len(refs)
	}
	for i := 0; i < limit; i++ {
		if refs[i].Frame == frame {
			return true
		}
	}
	return false
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c motion search.
// vp8_hex_search finishes with an eight-step full-pixel diamond refinement.
const interFrameFullPixelSearchRadius = 16
const interFrameMVSearchRange = interFrameFullPixelSearchRadius * 8
const interFrameSplitMVSearchRange = 8 * 8
const interFrameMVFullPixelStep = 8
const interFrameSubpixelSearchMaxCandidates = 31
const interFrameMotionCandidateMax = 15
const interFrameMaxMVSearchSteps = 8
const interFrameMaxFullPelVal = (1 << interFrameMaxMVSearchSteps) - 1
const interFrameUMVBorderPixels = 32
const libvpxFastNewMVBitCostWeight = 128
const libvpxRDNewMVBitCostWeight = 96

type interAnalysisFullPixelSearchMethod uint8

const (
	interAnalysisFullPixelSearchExhaustive interAnalysisFullPixelSearchMethod = iota
	interAnalysisFullPixelSearchNstep
	interAnalysisFullPixelSearchHex
)

type interAnalysisFractionalSearchMethod uint8

const (
	interAnalysisFractionalSearchIterative interAnalysisFractionalSearchMethod = iota
	interAnalysisFractionalSearchStep
	interAnalysisFractionalSearchHalf
	interAnalysisFractionalSearchSkip
)

type interAnalysisSearchConfig struct {
	fullPixelSearch       interAnalysisFullPixelSearchMethod
	fullPixelSearchParam  int
	fullPixelFurtherSteps int
	fullPixelFinalRefine  bool
	fullPixelSpeed        int
	fullPixelSpeedAdjust  int
	improvedMVPrediction  bool
	fractionalSearch      interAnalysisFractionalSearchMethod
}

var (
	interAnalysisBestQualitySplitPartitionOrder = [vp8tables.NumMBSplits]int{0, 1, 2, 3}
	interAnalysisSpeedSplitPartitionOrder       = [vp8tables.NumMBSplits]int{2, 1, 0, 3}
)

func defaultInterAnalysisSearchConfig() interAnalysisSearchConfig {
	return interAnalysisSearchConfig{
		fullPixelSearch:  interAnalysisFullPixelSearchExhaustive,
		fractionalSearch: interAnalysisFractionalSearchIterative,
	}
}

// interAnalysisSearchConfig mirrors the VP8 speed-feature branch in
// onyx_if.c: realtime speed > 4 uses vp8_hex_search and disables the
// iterative sub-pixel function pointer.
func (e *VP8Encoder) interAnalysisSearchConfig() interAnalysisSearchConfig {
	cfg := defaultInterAnalysisSearchConfig()
	if e == nil {
		return cfg
	}
	cfg.fullPixelSearch = interAnalysisFullPixelSearchNstep
	cfg.fullPixelSearchParam = libvpxInterFrameSearchParam(e.opts.Deadline, e.opts.CpuUsed)
	cfg.fullPixelFinalRefine = e.interAnalysisUsesRDModeDecision()
	cfg.fullPixelSpeed = e.opts.CpuUsed
	cfg.fullPixelSpeedAdjust = libvpxInterFrameSpeedAdjust(e.opts.CpuUsed)
	furtherStepsSpeed := e.opts.CpuUsed
	if e.interAnalysisUsesRDModeDecision() {
		cfg.fullPixelSearchParam = libvpxInterFrameFirstStep(e.opts.Deadline, e.opts.CpuUsed)
		cfg.fullPixelSpeedAdjust = 0
		if e.opts.Deadline == DeadlineBestQuality {
			furtherStepsSpeed = 0
		}
	}
	cfg.fullPixelFurtherSteps = libvpxInterFrameFurtherSteps(furtherStepsSpeed, cfg.fullPixelSearchParam)
	cfg.improvedMVPrediction = libvpxInterFrameImprovedMVPrediction(e.opts.Deadline, e.opts.CpuUsed)
	if e.opts.Deadline != DeadlineRealtime {
		return cfg
	}
	speed := e.opts.CpuUsed
	if speed > 4 {
		cfg.fullPixelSearch = interAnalysisFullPixelSearchHex
		cfg.fractionalSearch = interAnalysisFractionalSearchStep
	}
	if speed > 8 {
		cfg.fractionalSearch = interAnalysisFractionalSearchHalf
	}
	if speed >= 15 {
		cfg.fractionalSearch = interAnalysisFractionalSearchSkip
	}
	return cfg
}

func (e *VP8Encoder) interAnalysisCompressorSpeed() int {
	if e == nil || e.opts.Deadline == DeadlineBestQuality {
		return 0
	}
	if e.opts.Deadline == DeadlineRealtime {
		return 2
	}
	return 1
}

func (e *VP8Encoder) interAnalysisUsesRDModeDecision() bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineBestQuality:
		return true
	case DeadlineGoodQuality, DeadlineRealtime:
		return e.opts.CpuUsed <= 3
	default:
		return true
	}
}

func (e *VP8Encoder) libvpxOptimizeCoefficients() bool {
	if e == nil {
		return true
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return false
	case DeadlineGoodQuality:
		return e.opts.CpuUsed <= 0
	default:
		return true
	}
}

func (e *VP8Encoder) libvpxUseFastQuant() bool {
	if e == nil {
		return false
	}
	switch e.opts.Deadline {
	case DeadlineRealtime:
		return e.opts.CpuUsed > 0
	case DeadlineGoodQuality:
		return e.opts.CpuUsed > 2
	default:
		return false
	}
}

func (e *VP8Encoder) interAnalysisSplitPartitionOrder() [vp8tables.NumMBSplits]int {
	if e.interAnalysisCompressorSpeed() == 0 {
		return interAnalysisBestQualitySplitPartitionOrder
	}
	return interAnalysisSpeedSplitPartitionOrder
}

func (e *VP8Encoder) interAnalysisNoSkipBlock4x4Search() bool {
	return e.interAnalysisCompressorSpeed() == 0 || e.opts.CpuUsed <= 0
}

func libvpxInterFrameSearchParam(deadline Deadline, speed int) int {
	firstStep := libvpxInterFrameFirstStep(deadline, speed)
	stepParam := firstStep + libvpxInterFrameSpeedAdjust(speed)
	if stepParam < 0 {
		return 0
	}
	if stepParam >= interFrameMaxMVSearchSteps {
		return interFrameMaxMVSearchSteps - 1
	}
	return stepParam
}

func libvpxInterFrameFirstStep(deadline Deadline, speed int) int {
	if deadline != DeadlineBestQuality && speed > 0 {
		return 1
	}
	return 0
}

func libvpxInterFrameSpeedAdjust(speed int) int {
	if speed > 5 {
		if speed >= 8 {
			return 3
		}
		return 2
	}
	return 1
}

func libvpxInterFrameFurtherSteps(speed int, stepParam int) int {
	if speed >= 8 {
		return 0
	}
	further := interFrameMaxMVSearchSteps - 1 - stepParam
	if further < 0 {
		return 0
	}
	return further
}

func libvpxInterFrameImprovedMVPrediction(deadline Deadline, speed int) bool {
	return deadline != DeadlineRealtime || speed <= 6
}

func (e *VP8Encoder) interModeRDThresholds(qIndex int) [libvpxInterModeCount]int {
	return e.interModeRDThresholdsForReferences(qIndex, nil, 0)
}

func (e *VP8Encoder) interModeRDThresholdsForReferences(qIndex int, refs []interAnalysisReference, refCount int) [libvpxInterModeCount]int {
	if e == nil {
		return libvpxInterModeRDThresholds(qIndex, 0, DeadlineBestQuality, 0)
	}
	context := libvpxInterModeThresholdContext{}
	if refCount > 0 {
		context.temporalLayers = e.libvpxTemporalLayerCount()
		context.lastEnabled = interAnalysisReferencesInclude(refs, refCount, vp8common.LastFrame)
		context.goldenEnabled = interAnalysisReferencesInclude(refs, refCount, vp8common.GoldenFrame)
		context.closestRef = e.closestInterAnalysisReference(refs, refCount)
	}
	baseline := libvpxInterModeRDThresholdsForContext(qIndex, e.rc.currentZbinOverQuant, e.opts.Deadline, e.opts.CpuUsed, context)
	if !e.interRDFrameActive {
		return baseline
	}
	var thresholds [libvpxInterModeCount]int
	for i, value := range baseline {
		if value == libvpxInterModeThresholdDisabled {
			thresholds[i] = libvpxInterModeThresholdDisabled
			continue
		}
		if e.interRDThreshTouched[i] {
			thresholds[i] = (value >> 7) * e.interRDThreshMult[i]
		} else {
			thresholds[i] = value
		}
	}
	return thresholds
}

func (e *VP8Encoder) libvpxTemporalLayerCount() int {
	if e == nil || !e.opts.TemporalScalability.Enabled {
		return 1
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	if !ok {
		return 1
	}
	return pattern.Layers
}

func (e *VP8Encoder) resetInterRDThresholdMultipliers() {
	if e == nil {
		return
	}
	for i := range e.interRDThreshMult {
		e.interRDThreshMult[i] = libvpxRDThreshMultStart
	}
	e.interRDThreshTouched = [libvpxInterModeCount]bool{}
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFar = 0
}

func (e *VP8Encoder) beginInterRDModeDecisionFrame() {
	if e == nil {
		return
	}
	for i, mult := range e.interRDThreshMult {
		if mult == 0 {
			e.interRDThreshMult[i] = libvpxRDThreshMultStart
		}
	}
	e.interRDThreshTouched = [libvpxInterModeCount]bool{}
	e.interModeCheckFreq = libvpxInterModeCheckFrequencies(e.opts.Deadline, e.opts.CpuUsed)
	e.interModeTestHitCounts = [libvpxInterModeCount]int{}
	e.interMBsTestedSoFar = 0
	e.interRDFrameActive = true
}

func (e *VP8Encoder) endInterRDModeDecisionFrame() {
	if e == nil {
		return
	}
	e.interRDFrameActive = false
}

func (e *VP8Encoder) beginInterRDModeDecisionMacroblock() {
	if e == nil {
		return
	}
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionFrame()
	}
	e.interMBsTestedSoFar++
}

func (e *VP8Encoder) interRDModeTestAllowed(modeIndex int) bool {
	if e == nil || !e.interRDFrameActive || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return true
	}
	if e.interModeTestHitCounts[modeIndex] == 0 || e.interModeCheckFreq[modeIndex] <= 1 {
		return true
	}
	if e.interMBsTestedSoFar > e.interModeCheckFreq[modeIndex]*e.interModeTestHitCounts[modeIndex] {
		return true
	}
	e.raiseInterRDThreshold(modeIndex)
	return false
}

func (e *VP8Encoder) recordInterRDModeTest(modeIndex int) {
	if e == nil || !e.interRDFrameActive || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	e.interModeTestHitCounts[modeIndex]++
}

func (e *VP8Encoder) lowerInterRDThresholdForImprovement(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+2 {
		e.interRDThreshMult[modeIndex] -= 2
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) raiseInterRDThreshold(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	e.interRDThreshMult[modeIndex] += 4
	if e.interRDThreshMult[modeIndex] > libvpxMaxThreshMult {
		e.interRDThreshMult[modeIndex] = libvpxMaxThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func (e *VP8Encoder) lowerBestInterRDThreshold(modeIndex int) {
	if e == nil || modeIndex < 0 || modeIndex >= libvpxInterModeCount {
		return
	}
	bestAdjustment := e.interRDThreshMult[modeIndex] >> 2
	if e.interRDThreshMult[modeIndex] >= libvpxMinThreshMult+bestAdjustment {
		e.interRDThreshMult[modeIndex] -= bestAdjustment
	} else {
		e.interRDThreshMult[modeIndex] = libvpxMinThreshMult
	}
	e.interRDThreshTouched[modeIndex] = true
}

func libvpxInterModeRDThresholds(qIndex int, zbinOverQuant int, deadline Deadline, speed int) [libvpxInterModeCount]int {
	return libvpxInterModeRDThresholdsForContext(qIndex, zbinOverQuant, deadline, speed, libvpxInterModeThresholdContext{})
}

func libvpxInterModeRDThresholdsForContext(qIndex int, zbinOverQuant int, deadline Deadline, speed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	multipliers := libvpxInterModeThresholdMultipliersForContext(deadline, speed, context)
	qValue := vp8common.DCQuant(qIndex, 0)
	if qValue > 160 {
		qValue = 160
	}
	q := int(math.Pow(float64(qValue), 1.25))
	if q < 8 {
		q = 8
	}
	_, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	var thresholds [libvpxInterModeCount]int
	for i, mult := range multipliers {
		if mult == libvpxInterModeThresholdDisabled {
			thresholds[i] = libvpxInterModeThresholdDisabled
			continue
		}
		if rdDiv == 1 {
			thresholds[i] = mult * q / 100
		} else {
			thresholds[i] = mult * q
		}
	}
	return thresholds
}

type libvpxInterModeThresholdContext struct {
	temporalLayers int
	lastEnabled    bool
	goldenEnabled  bool
	closestRef     vp8common.MVReferenceFrame
}

func libvpxInterModeThresholdMultipliers(deadline Deadline, speed int) [libvpxInterModeCount]int {
	return libvpxInterModeThresholdMultipliersForContext(deadline, speed, libvpxInterModeThresholdContext{})
}

func libvpxInterModeThresholdMultipliersForContext(deadline Deadline, speed int, context libvpxInterModeThresholdContext) [libvpxInterModeCount]int {
	continuousSpeed := libvpxInterFrameContinuousSpeed(deadline, speed)
	znn := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapZNN[:])
	vhPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapVHPred[:])
	bPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapBPred[:])
	tmPred := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapTM[:])
	new1 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapNew1[:])
	new2 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapNew2[:])
	split1 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapSplit1[:])
	split2 := libvpxSpeedMap(continuousSpeed, libvpxThreshMultMapSplit2[:])

	var mult [libvpxInterModeCount]int
	mult[libvpxThrZero1] = 0
	mult[libvpxThrNearest1] = 0
	mult[libvpxThrNear1] = 0
	mult[libvpxThrDC] = 0
	mult[libvpxThrZero2] = znn
	mult[libvpxThrZero3] = znn
	mult[libvpxThrNearest2] = znn
	mult[libvpxThrNearest3] = znn
	mult[libvpxThrNear2] = znn
	mult[libvpxThrNear3] = znn
	mult[libvpxThrVPred] = vhPred
	mult[libvpxThrHPred] = vhPred
	mult[libvpxThrBPred] = bPred
	mult[libvpxThrTMPred] = tmPred
	mult[libvpxThrNew1] = new1
	mult[libvpxThrNew2] = new2
	mult[libvpxThrNew3] = new2
	mult[libvpxThrSplit1] = split1
	mult[libvpxThrSplit2] = split2
	mult[libvpxThrSplit3] = split2
	if context.temporalLayers > 1 && speed <= 6 && context.lastEnabled && context.goldenEnabled {
		shift := 1
		if context.closestRef == vp8common.GoldenFrame {
			shift = 3
		}
		mult[libvpxThrZero2] >>= shift
		mult[libvpxThrNearest2] >>= shift
		mult[libvpxThrNear2] >>= shift
	}
	return mult
}

func libvpxInterModeCheckFrequencies(deadline Deadline, speed int) [libvpxInterModeCount]int {
	continuousSpeed := libvpxInterFrameContinuousSpeed(deadline, speed)
	new1Speed := continuousSpeed
	if deadline == DeadlineRealtime && speed == 10 {
		new1Speed = 16
	}
	zn2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapZN2[:])
	near2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapNear2[:])
	vhBPred := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapVHBPred[:])
	new1 := libvpxSpeedMap(new1Speed, libvpxModeCheckFreqMapNew1[:])
	new2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapNew2[:])
	split1 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapSplit1[:])
	split2 := libvpxSpeedMap(continuousSpeed, libvpxModeCheckFreqMapSplit2[:])

	var freq [libvpxInterModeCount]int
	freq[libvpxThrZero1] = 0
	freq[libvpxThrNearest1] = 0
	freq[libvpxThrNear1] = 0
	freq[libvpxThrDC] = 0
	freq[libvpxThrTMPred] = 0
	freq[libvpxThrZero2] = zn2
	freq[libvpxThrZero3] = zn2
	freq[libvpxThrNearest2] = zn2
	freq[libvpxThrNearest3] = zn2
	freq[libvpxThrNear2] = near2
	freq[libvpxThrNear3] = near2
	freq[libvpxThrVPred] = vhBPred
	freq[libvpxThrHPred] = vhBPred
	freq[libvpxThrBPred] = vhBPred
	freq[libvpxThrNew1] = new1
	freq[libvpxThrNew2] = new2
	freq[libvpxThrNew3] = new2
	freq[libvpxThrSplit1] = split1
	freq[libvpxThrSplit2] = split2
	freq[libvpxThrSplit3] = split2
	return freq
}

func libvpxInterFrameContinuousSpeed(deadline Deadline, speed int) int {
	switch deadline {
	case DeadlineBestQuality:
		return 0
	case DeadlineRealtime:
		return speed + 7
	default:
		if speed > 5 {
			speed = 5
		}
		return speed + 1
	}
}

func libvpxSpeedMap(speed int, entries []int) int {
	for i := 0; i+1 < len(entries); i += 2 {
		result := entries[i]
		limit := entries[i+1]
		if speed < limit {
			return result
		}
	}
	return 0
}

var libvpxThreshMultMapZNN = [...]int{
	0, 3, 1500, 4, 2000, 7, 1000, 9, 2000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapVHPred = [...]int{
	1000, 3, 1500, 4, 2000, 7, 1000, 8, 2000, 14, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapBPred = [...]int{
	2000, 1, 2500, 3, 5000, 4, 7500, 7, 2500, 8, 5000, 13, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapTM = [...]int{
	1000, 3, 1500, 4, 2000, 7, 0, 8, 1000, 9, 2000, 14, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapNew1 = [...]int{
	1000, 3, 2000, 7, 2000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapNew2 = [...]int{
	1000, 3, 2000, 4, 2500, 6, 4000, 7, 2000, 9, 2500, 12, 4000, libvpxSpeedMapMax,
}

var libvpxThreshMultMapSplit1 = [...]int{
	2500, 1, 1700, 3, 10000, 4, 25000, 5, libvpxInterModeThresholdDisabled, 7, 5000, 8, 10000, 9, 25000, 10, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxThreshMultMapSplit2 = [...]int{
	5000, 1, 4500, 3, 20000, 4, 50000, 5, libvpxInterModeThresholdDisabled, 7, 10000, 8, 20000, 9, 50000, 10, libvpxInterModeThresholdDisabled, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapZN2 = [...]int{
	0, 17, 2, 18, 4, 19, 8, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapVHBPred = [...]int{
	0, 6, 2, 7, 0, 10, 2, 12, 4, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNear2 = [...]int{
	0, 6, 2, 7, 0, 10, 2, 17, 4, 18, 8, 19, 16, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNew1 = [...]int{
	0, 17, 2, 18, 4, 19, 8, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapNew2 = [...]int{
	0, 6, 4, 7, 0, 10, 4, 17, 8, 18, 16, 19, 32, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapSplit1 = [...]int{
	0, 3, 2, 4, 7, 8, 2, 9, 7, libvpxSpeedMapMax,
}

var libvpxModeCheckFreqMapSplit2 = [...]int{
	0, 2, 2, 3, 4, 4, 15, 8, 4, 9, 15, libvpxSpeedMapMax,
}

func interFrameFullPixelSearchCandidateCount() int {
	axis := (2*interFrameMVSearchRange)/interFrameMVFullPixelStep + 1
	return axis * axis
}

func interFrameSubpixelSearchCandidateCount() int {
	return interFrameSubpixelSearchMaxCandidates
}

func defaultInterFrameSignBias() [vp8common.MaxRefFrames]bool {
	return [vp8common.MaxRefFrames]bool{}
}

func selectInterFrameReferenceMotionVector(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbRows int, mbCols int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (interAnalysisReference, vp8enc.MotionVector) {
	return selectInterFrameReferenceMotionVectorWithSearch(src, refs, refCount, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, qIndex, defaultInterAnalysisSearchConfig(), mvProbs)
}

func selectInterFrameReferenceMotionVectorWithSearch(src vp8enc.SourceImage, refs []interAnalysisReference, refCount int, mbRow int, mbCol int, mbRows int, mbCols int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (interAnalysisReference, vp8enc.MotionVector) {
	bestRef := refs[0]
	signBias := defaultInterFrameSignBias()
	bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, bestRef.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	best, bestCost := selectInterFrameMotionVectorWithSearch(src, bestRef.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, mvProbs)
	if bestCost == 0 {
		return bestRef, best
	}
	for refIndex := 1; refIndex < refCount; refIndex++ {
		ref := refs[refIndex]
		refMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
		mv, cost := selectInterFrameMotionVectorWithSearch(src, ref.Img, mbRow, mbCol, mbRows, mbCols, refMV, qIndex, search, mvProbs)
		if cost < bestCost {
			bestRef = ref
			best = mv
			bestCost = cost
			if bestCost == 0 {
				return bestRef, best
			}
		}
	}
	return bestRef, best
}

type interFrameModeDecision struct {
	ref       interAnalysisReference
	interMode vp8enc.InterFrameMacroblockMode
	useIntra  bool
	intraMode vp8enc.InterFrameMacroblockMode
}

func (d interFrameModeDecision) cyclicRefreshEligible() bool {
	return !d.useIntra && d.interMode.RefFrame == vp8common.LastFrame && d.interMode.Mode == vp8common.ZeroMV
}

func (e *VP8Encoder) selectInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	baseQIndex int, segmentation vp8enc.SegmentationConfig, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
) (interFrameModeDecision, bool) {
	segmentQIndex := encoderSegmentQIndex(baseQIndex, segmentation, segmentID)
	if !e.interAnalysisUsesRDModeDecision() {
		return e.selectFastInterFrameModeDecision(
			src, refs, refCount,
			mbRow, mbCol, mbRows, mbCols,
			segmentQIndex, segmentID,
			above, left, aboveLeft,
		)
	}
	return e.selectRDInterFrameModeDecision(
		src, refs, refCount,
		mbRow, mbCol, mbRows, mbCols,
		segmentQIndex, segmentID,
		above, left, aboveLeft,
		aboveTok, leftTok,
		quant,
	)
}

func (e *VP8Encoder) selectRDInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
) (interFrameModeDecision, bool) {
	if !e.interRDFrameActive {
		e.beginInterRDModeDecisionMacroblock()
	}
	thresholds := e.interModeRDThresholdsForReferences(qIndex, refs, refCount)
	bestSet := false
	bestScore := maxInt()
	bestYRD := maxInt()
	bestModeIndex := -1
	best := interFrameModeDecision{
		intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID},
	}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
	}
	refSearchOrder := libvpxInterReferenceSearchOrder(refs, refCount)

	for modeIndex, mbMode := range libvpxFastInterModeOrder {
		threshold := thresholds[modeIndex]
		if threshold == libvpxInterModeThresholdDisabled {
			continue
		}
		if bestSet && bestScore <= threshold {
			continue
		}

		refSlot := libvpxFastRefFrameOrder[modeIndex]
		if refSlot == 0 {
			if !e.interRDModeTestAllowed(modeIndex) {
				continue
			}
			e.recordInterRDModeTest(modeIndex)
			mode, score, ok := e.estimateInterIntraModeRDScore(src, qIndex, mbRow, mbCol, mbMode, bestYRD, aboveTok, leftTok, quant)
			if !ok {
				continue
			}
			mode.SegmentID = segmentID
			if !bestSet || score < bestScore {
				e.lowerInterRDThresholdForImprovement(modeIndex)
				bestSet = true
				bestScore = score
				bestYRD = score
				bestModeIndex = modeIndex
				best = interFrameModeDecision{useIntra: true, intraMode: mode}
			} else {
				e.raiseInterRDThreshold(modeIndex)
			}
			continue
		}

		ref, refIndex, ok := libvpxInterReferenceSearchAt(refs, refSearchOrder, refSlot)
		if !ok {
			continue
		}
		if !e.interRDModeTestAllowed(modeIndex) {
			continue
		}
		e.recordInterRDModeTest(modeIndex)
		var mode vp8enc.InterFrameMacroblockMode
		var score int
		var yrd int
		rdLoopSkip := false
		if mbMode == vp8common.SplitMV {
			mode, score, yrd, rdLoopSkip, ok = e.selectInterFrameSplitModeRDScore(src, ref, mbRow, mbCol, mbRows, mbCols, qIndex, segmentID, bestYRD, above, left, aboveLeft, aboveTok, leftTok, quant)
		} else {
			mode, ok = e.interModeForRDLoopEntry(src, ref, refIndex, mbMode, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, &newMVCandidates)
			if ok {
				mode.SegmentID = segmentID
				acct, acctOK := e.estimateInterResidualRDAccounting(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, e.interReferenceFrameRateForReference(ref))
				ok = acctOK
				score = acct.rd
				yrd = acct.yrd
				rdLoopSkip = acct.rdLoopSkip
			}
		}
		if !ok {
			continue
		}
		if rdLoopSkip || !bestSet || score < bestScore {
			e.lowerInterRDThresholdForImprovement(modeIndex)
			bestSet = true
			bestScore = score
			bestYRD = yrd
			bestModeIndex = modeIndex
			best = interFrameModeDecision{ref: ref, interMode: mode, intraMode: vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID}}
		} else {
			e.raiseInterRDThreshold(modeIndex)
		}
		if rdLoopSkip {
			break
		}
	}
	if !bestSet {
		return interFrameModeDecision{}, false
	}
	if bestModeIndex >= 0 {
		e.lowerBestInterRDThreshold(bestModeIndex)
	}
	return best, true
}

func (e *VP8Encoder) selectInterFrameSplitModeRDScore(
	src vp8enc.SourceImage, ref interAnalysisReference,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8, bestYRD int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes,
	quant *vp8enc.MacroblockQuant,
) (vp8enc.InterFrameMacroblockMode, int, int, bool, bool) {
	signBias := defaultInterFrameSignBias()
	bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, signBias)
	bestSet := false
	bestScore := maxInt()
	bestPartitionYRD := maxInt()
	var bestMode vp8enc.InterFrameMacroblockMode
	for _, partition := range e.interAnalysisSplitPartitionOrder() {
		mode, ok := selectInterFrameSplitMotionMode(src, ref.Img, ref.Frame, mbRow, mbCol, bestRefMV, qIndex, partition)
		if !ok {
			continue
		}
		mode.SegmentID = segmentID
		acct, ok := e.estimateInterResidualRDAccounting(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, e.interReferenceFrameRateForReference(ref))
		if !ok {
			continue
		}
		if !acct.rdLoopSkip && acct.yrd >= bestYRD {
			continue
		}
		score := acct.rd
		if acct.rdLoopSkip || !bestSet || score < bestScore {
			bestSet = true
			bestScore = score
			bestPartitionYRD = acct.yrd
			bestMode = mode
		}
		if acct.rdLoopSkip {
			return bestMode, bestScore, bestPartitionYRD, true, true
		}
	}
	return bestMode, bestScore, bestPartitionYRD, false, bestSet
}

func libvpxSplitMVSubsearchThreshold(thresholds [libvpxInterModeCount]int, refSlot int) int {
	switch refSlot {
	case 1:
		return thresholds[libvpxThrNew1]
	case 2:
		return thresholds[libvpxThrNew2]
	default:
		return thresholds[libvpxThrNew3]
	}
}

func (e *VP8Encoder) estimateInterIntraModeRDScore(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, mbMode vp8common.MBPredictionMode, bestRD int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant) (vp8enc.InterFrameMacroblockMode, int, bool) {
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	fastQuant := e.libvpxUseFastQuant()
	if mbMode == vp8common.BPred {
		bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, bestRD, &e.coefProbs, fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, false
		}
		uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, fastQuant)
		if !ok {
			return vp8enc.InterFrameMacroblockMode{}, 0, false
		}
		rate := bRate + uvRate + intraYModeRate(false, vp8common.BPred) + e.interIntraMacroblockModeRate()
		score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, bDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
		return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: uvMode, BModes: bModes}, score, true
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	yRate, yDist := wholeBlockYTransformRD(src, &e.analysis.Img, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, &e.coefProbs, fastQuant)
	uvMode, uvRate, uvDist, ok := predictBestIntraChromaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, &e.analysis.Img, &e.reconstructScratch, &e.coefProbs, fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	rate := yRate + uvRate + intraYModeRate(false, mbMode) + e.interIntraMacroblockModeRate()
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist+uvDist) + libvpxInterIntraRDPenalty(qIndex)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: uvMode}, score, true
}

func (e *VP8Encoder) interModeForRDLoopEntry(
	src vp8enc.SourceImage, ref interAnalysisReference, refIndex int, mbMode vp8common.MBPredictionMode,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	newMVCandidates *[3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
	},
) (vp8enc.InterFrameMacroblockMode, bool) {
	switch mbMode {
	case vp8common.ZeroMV:
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.ZeroMV}, true
	case vp8common.NearestMV, vp8common.NearMV:
		nearest, near := interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		if mv.IsZero() {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: mbMode, MV: mv}, true
	case vp8common.NewMV:
		if refIndex < 0 || refIndex >= len(newMVCandidates) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		candidate := &newMVCandidates[refIndex]
		if !candidate.searched {
			bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, defaultInterFrameSignBias())
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			mv, _ := selectRDInterFrameMotionVectorWithSearchStart(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &e.modeProbs.MV)
			candidate.searched = true
			candidate.ok = true
			candidate.mv = mv
		}
		if !candidate.ok {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.NewMV, MV: candidate.mv}, true
	default:
		return vp8enc.InterFrameMacroblockMode{}, false
	}
}

func (e *VP8Encoder) selectFastInterFrameModeDecision(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int,
	qIndex int, segmentID uint8,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
) (interFrameModeDecision, bool) {
	bestSet := false
	bestScore := maxInt()
	bestSSE := maxInt()
	best := interFrameModeDecision{}
	var newMVCandidates [3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
	}
	refSearchOrder := libvpxInterReferenceSearchOrder(refs, refCount)

	for modeIndex, mbMode := range libvpxFastInterModeOrder {
		refSlot := libvpxFastRefFrameOrder[modeIndex]
		if refSlot == 0 {
			mode, score, sse, ok := e.estimateFastIntraModeScore(src, mbRow, mbCol, qIndex, mbMode, bestSSE)
			if !ok {
				continue
			}
			mode.SegmentID = segmentID
			if !bestSet || score < bestScore {
				bestSet = true
				bestScore = score
				bestSSE = sse
				best = interFrameModeDecision{useIntra: true, intraMode: mode}
			}
			continue
		}

		ref, refIndex, ok := libvpxInterReferenceSearchAt(refs, refSearchOrder, refSlot)
		if !ok {
			continue
		}
		mode, ok := e.fastInterModeForLoopEntry(src, ref, refIndex, refSlot, mbMode, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, &newMVCandidates)
		if !ok {
			continue
		}
		mode.SegmentID = segmentID
		score, ok := e.estimateFastInterModeScoreWithReferenceRate(src, ref.Img, mbRow, mbCol, mbRows, mbCols, &mode, above, left, aboveLeft, qIndex, e.interReferenceFrameRateForReference(ref))
		if !ok {
			continue
		}
		if !bestSet || score < bestScore {
			bestSet = true
			bestScore = score
			_, bestSSE = macroblockLumaMotionVarianceSSE(src, ref.Img, mbRow, mbCol, mode.MV)
			best = interFrameModeDecision{ref: ref, interMode: mode}
		}
	}
	if !bestSet {
		return interFrameModeDecision{}, false
	}
	if best.useIntra {
		uvMode, ok := e.predictFastIntraChromaMode(src, mbRow, mbCol)
		if !ok {
			return interFrameModeDecision{}, false
		}
		best.intraMode.UVMode = uvMode
	}
	if !best.useIntra {
		best.intraMode = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.DCPred, UVMode: vp8common.DCPred, SegmentID: segmentID}
	}
	return best, true
}

func libvpxFastInterReferenceAt(refs []interAnalysisReference, refCount int, refSlot int) (interAnalysisReference, int, bool) {
	return libvpxInterReferenceSearchAt(refs, libvpxInterReferenceSearchOrder(refs, refCount), refSlot)
}

func libvpxInterReferenceSearchOrder(refs []interAnalysisReference, refCount int) [4]int {
	order := [4]int{-1, -1, -1, -1}
	searchSlot := 1
	for refIndex := 0; refIndex < refCount && refIndex < len(refs) && searchSlot < len(order); refIndex++ {
		if refs[refIndex].Img == nil {
			continue
		}
		switch refs[refIndex].Frame {
		case vp8common.LastFrame, vp8common.GoldenFrame, vp8common.AltRefFrame:
			order[searchSlot] = refIndex
			searchSlot++
		}
	}
	return order
}

func libvpxInterReferenceSearchAt(refs []interAnalysisReference, searchOrder [4]int, refSlot int) (interAnalysisReference, int, bool) {
	if refSlot <= 0 || refSlot >= len(searchOrder) {
		return interAnalysisReference{}, 0, false
	}
	refIndex := searchOrder[refSlot]
	if refIndex < 0 || refIndex >= len(refs) || refs[refIndex].Img == nil {
		return interAnalysisReference{}, 0, false
	}
	return refs[refIndex], refIndex, true
}

func (e *VP8Encoder) fastInterModeForLoopEntry(
	src vp8enc.SourceImage, ref interAnalysisReference, refIndex int, refSlot int, mbMode vp8common.MBPredictionMode,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	newMVCandidates *[3]struct {
		searched bool
		ok       bool
		mv       vp8enc.MotionVector
	},
) (vp8enc.InterFrameMacroblockMode, bool) {
	switch mbMode {
	case vp8common.ZeroMV:
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.ZeroMV}, true
	case vp8common.NearestMV, vp8common.NearMV:
		nearest, near := interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		mv := nearest
		if mbMode == vp8common.NearMV {
			mv = near
		}
		if mv.IsZero() {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: mbMode, MV: mv}, true
	case vp8common.NewMV:
		if refIndex < 0 || refIndex >= len(newMVCandidates) {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		candidate := &newMVCandidates[refIndex]
		if !candidate.searched {
			bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, defaultInterFrameSignBias())
			search := e.interAnalysisSearchConfig()
			start := e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
			mv, _ := selectInterFrameMotionVectorWithSearchStart(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &e.modeProbs.MV)
			candidate.searched = true
			candidate.ok = !mv.IsZero()
			candidate.mv = mv
		}
		if !candidate.ok {
			return vp8enc.InterFrameMacroblockMode{}, false
		}
		return vp8enc.InterFrameMacroblockMode{RefFrame: ref.Frame, Mode: vp8common.NewMV, MV: candidate.mv}, true
	default:
		// libvpx pickinter.c does not support SPLITMV in the non-RD picker.
		return vp8enc.InterFrameMacroblockMode{}, false
	}
}

func (e *VP8Encoder) estimateFastIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, mbMode vp8common.MBPredictionMode, bestSSE int) (vp8enc.InterFrameMacroblockMode, int, int, bool) {
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	if mbMode == vp8common.BPred {
		return e.estimateFastBPredIntraModeScore(src, mbRow, mbCol, qIndex, bestSSE)
	}
	if mbMode < vp8common.DCPred || mbMode > vp8common.TMPred {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, false
	}
	mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}
	if !predictAnalysisMacroblock(&e.analysis.Img, mbRow, mbCol, &mode, &e.reconstructScratch) {
		return vp8enc.InterFrameMacroblockMode{}, 0, 0, false
	}
	variance, sse := macroblockLumaVarianceSSE(src, &e.analysis.Img, mbRow, mbCol)
	rate := boolBitCost(e.refProbIntra, 0) + intraYModeRate(false, mbMode)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: mbMode, UVMode: vp8common.DCPred}, rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, variance), sse, true
}

func (e *VP8Encoder) estimateFastBPredIntraModeScore(src vp8enc.SourceImage, mbRow int, mbCol int, qIndex int, bestSSE int) (vp8enc.InterFrameMacroblockMode, int, int, bool) {
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	refs := vp8dec.BuildIntraPredictorRefs(&e.analysis.Img, mbRow, mbCol, &e.reconstructScratch.Refs)
	var pred [16 * 16]byte
	var modes [16]vp8common.BPredictionMode
	rate := boolBitCost(e.refProbIntra, 0) + intraYModeRate(false, vp8common.BPred)
	distortion := 0
	for block := 0; block < 16; block++ {
		bestMode := vp8common.BModeCount
		bestRate := 0
		bestDist := 0
		bestCost := maxInt()
		var bestBlock [16]byte
		for _, bMode := range bPredIntraModeCandidates {
			var blockPred [16]byte
			if !predictAnalysisBPredBlock(bMode, blockPred[:], 4, pred[:], 16, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return vp8enc.InterFrameMacroblockMode{}, 0, 0, false
			}
			modeRate := bPredModeRate(false, bMode, vp8common.BDCPred, vp8common.BDCPred)
			modeDist := bPredBlockSSE(src, mbRow, mbCol, block, blockPred[:], 4)
			modeCost := rdModeScoreWithZbin(qIndex, zbinOverQuant, modeRate, modeDist)
			if modeCost < bestCost {
				bestMode = bMode
				bestRate = modeRate
				bestDist = modeDist
				bestCost = modeCost
				copy(bestBlock[:], blockPred[:])
			}
		}
		modes[block] = bestMode
		copyBPredBlock(bestBlock[:], 4, pred[:], 16, block)
		rate += bestRate
		distortion += bestDist
		if distortion > bestSSE {
			return vp8enc.InterFrameMacroblockMode{}, 0, 0, false
		}
	}
	yOff := mbRow*16*e.analysis.Img.YStride + mbCol*16
	for row := 0; row < 16; row++ {
		copy(e.analysis.Img.Y[yOff+row*e.analysis.Img.YStride:], pred[row*16:row*16+16])
	}
	variance, sse := macroblockLumaVarianceSSE(src, &e.analysis.Img, mbRow, mbCol)
	return vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: vp8common.DCPred, BModes: modes}, rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, variance), sse, true
}

func (e *VP8Encoder) predictFastIntraChromaMode(src vp8enc.SourceImage, mbRow int, mbCol int) (vp8common.MBPredictionMode, bool) {
	bestMode := vp8common.DCPred
	bestSet := false
	bestDist := maxInt()
	for _, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(&e.analysis.Img, mbRow, mbCol, uvMode, &e.reconstructScratch) {
			return 0, false
		}
		dist := macroblockChromaSSE(src, &e.analysis.Img, mbRow, mbCol)
		if !bestSet || dist < bestDist {
			bestSet = true
			bestMode = uvMode
			bestDist = dist
		}
	}
	return bestMode, bestSet
}

func selectInterFrameSplitMotionMode(src vp8enc.SourceImage, ref *vp8common.Image, refFrame vp8common.MVReferenceFrame, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, partition int) (vp8enc.InterFrameMacroblockMode, bool) {
	if ref == nil || refFrame == vp8common.IntraFrame || partition < 0 || partition >= vp8tables.NumMBSplits {
		return vp8enc.InterFrameMacroblockMode{}, false
	}
	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  refFrame,
		Mode:      vp8common.SplitMV,
		Partition: uint8(partition),
	}
	first := vp8enc.MotionVector{}
	allSame := true
	width, height := splitMotionPartitionBlockSize(partition)
	for subset := 0; subset < int(vp8tables.MBSplitCount[mode.Partition]); subset++ {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		mv, _ := selectInterFrameSplitBlockFullPixelMotionVector(src, ref, mbRow, mbCol, block, width, height, bestRefMV, qIndex)
		if subset == 0 {
			first = mv
		} else if mv != first {
			allSame = false
		}
		fillInterFrameSplitSubset(&mode, subset, mv)
	}
	mode.MV = mode.BlockMV[15]
	if allSame {
		return vp8enc.InterFrameMacroblockMode{}, false
	}
	return mode, true
}

func splitMotionPartitionBlockSize(partition int) (int, int) {
	switch partition {
	case 0:
		return 16, 8
	case 1:
		return 8, 16
	case 2:
		return 8, 8
	default:
		return 4, 4
	}
}

func fillInterFrameSplitSubset(mode *vp8enc.InterFrameMacroblockMode, subset int, mv vp8enc.MotionVector) {
	fillCount := int(vp8tables.MBSplitFillCount[mode.Partition])
	fillStart := subset * fillCount
	for i := 0; i < fillCount; i++ {
		mode.BlockMV[vp8tables.MBSplitFillOffset[mode.Partition][fillStart+i]] = mv
	}
}

func collectInterFrameMotionCandidates(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	return collectInterFrameMotionCandidatesWithSearch(src, refs, refCount, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, defaultInterAnalysisSearchConfig(), mvProbs, candidates)
}

func collectInterFrameMotionCandidatesWithSearch(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	return collectInterFrameMotionCandidatesWithEncoder(nil, src, refs, refCount, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, search, mvProbs, candidates)
}

func (e *VP8Encoder) collectInterFrameMotionCandidatesWithSearch(
	src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	return collectInterFrameMotionCandidatesWithEncoder(e, src, refs, refCount, mbRow, mbCol, mbRows, mbCols, qIndex, above, left, aboveLeft, search, mvProbs, candidates)
}

func collectInterFrameMotionCandidatesWithEncoder(
	e *VP8Encoder, src vp8enc.SourceImage, refs []interAnalysisReference, refCount int,
	mbRow int, mbCol int, mbRows int, mbCols int, qIndex int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
	mvProbs *[2][vp8tables.MVPCount]uint8,
	candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate,
) int {
	if candidates == nil || mvProbs == nil {
		return 0
	}
	count := 0
	for refIndex := 0; refIndex < refCount && refIndex < len(refs); refIndex++ {
		ref := refs[refIndex]
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, vp8enc.MotionVector{})
		nearest, near := interAnalysisReferenceMotionPredictors(ref.Frame, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, nearest)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, near)
		bestRefMV := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, ref.Frame, mbRow, mbCol, mbRows, mbCols, defaultInterFrameSignBias())
		start := interFrameSearchStart{}
		if e != nil {
			start = e.improvedInterFrameSearchStart(src, ref.Frame, mbRow, mbCol, mbRows, mbCols, above, left, aboveLeft, search)
		}
		fullMV, fullCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref.Img, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs)
		count = appendInterAnalysisMotionCandidate(candidates, count, ref, fullMV)
		if fullCost == 0 {
			continue
		}
		refinedMV, _, ok := refineInterFrameSubpixelMotionVector(src, ref.Img, mbRow, mbCol, fullMV, bestRefMV, qIndex, search, mvProbs)
		if ok && refinedMV != fullMV {
			count = appendInterAnalysisMotionCandidate(candidates, count, ref, refinedMV)
		}
	}
	return count
}

func interAnalysisReferenceMotionPredictors(refFrame vp8common.MVReferenceFrame, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) (vp8enc.MotionVector, vp8enc.MotionVector) {
	return vp8enc.InterFrameNearMotionVectorsAt(above, left, aboveLeft, refFrame, mbRow, mbCol, mbRows, mbCols, defaultInterFrameSignBias())
}

func appendInterAnalysisMotionCandidate(candidates *[interFrameMotionCandidateMax]interAnalysisMotionCandidate, count int, ref interAnalysisReference, mv vp8enc.MotionVector) int {
	if candidates == nil || count >= len(candidates) {
		return count
	}
	for i := 0; i < count; i++ {
		if candidates[i].Ref.Frame == ref.Frame && candidates[i].Ref.Img == ref.Img && candidates[i].MV == mv {
			return count
		}
	}
	candidates[count] = interAnalysisMotionCandidate{Ref: ref, MV: mv}
	return count + 1
}

func predictBestKeyFrameIntraMode(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, fastQuant bool) (vp8enc.KeyFrameMacroblockMode, bool) {
	coefProbs := &vp8tables.DefaultCoefProbs
	wholeY, wholeUV, wholeYRate, wholeYDist, wholeUVRate, wholeUVDist, ok := predictBestWholeBlockIntraModeRD(src, qIndex, 0, true, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, fastQuant)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, false
	}
	wholeRate := wholeYRate + wholeUVRate
	wholeDist := wholeYDist + wholeUVDist
	wholeCost := rdModeScore(qIndex, wholeRate, wholeDist)
	best := vp8enc.KeyFrameMacroblockMode{YMode: wholeY, UVMode: wholeUV}
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, 0, true, mbRow, mbCol, above, left, aboveTok, leftTok, quant, pred, scratch, wholeCost, coefProbs, fastQuant)
	if !ok {
		return best, true
	}
	bUV, bUVRate, bUVDist, ok := predictBestIntraChromaModeRD(src, qIndex, 0, true, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, &vp8tables.DefaultCoefProbs, fastQuant)
	if !ok {
		return vp8enc.KeyFrameMacroblockMode{}, false
	}
	bPredRate := bRate + bUVRate + intraYModeRate(true, vp8common.BPred)
	bPredCost := rdModeScore(qIndex, bPredRate, bDist+bUVDist)
	if bPredCost < wholeCost {
		best = vp8enc.KeyFrameMacroblockMode{YMode: vp8common.BPred, UVMode: bUV, BModes: bModes}
	}
	return best, true
}

func (e *VP8Encoder) predictBestInterIntraModeCost(src vp8enc.SourceImage, qIndex int, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch) (vp8enc.InterFrameMacroblockMode, int, bool) {
	fastQuant := e.libvpxUseFastQuant()
	zbinOverQuant := e.rc.currentZbinOverQuant
	wholeY, wholeUV, wholeYRate, wholeYDist, wholeUVRate, wholeUVDist, ok := predictBestWholeBlockIntraModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, &e.coefProbs, fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	wholeRate := wholeYRate + wholeUVRate + e.interIntraMacroblockModeRate()
	wholeDist := wholeYDist + wholeUVDist
	wholeCost := rdModeScoreWithZbin(qIndex, zbinOverQuant, wholeRate, wholeDist) + libvpxInterIntraRDPenalty(qIndex)
	best := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: wholeY, UVMode: wholeUV}
	bestCost := wholeCost
	bModes, bRate, bDist, ok := predictBestBPredLumaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, nil, nil, aboveTok, leftTok, quant, pred, scratch, wholeCost, &e.coefProbs, fastQuant)
	if !ok {
		return best, bestCost, true
	}
	bUV, bUVRate, bUVDist, ok := predictBestIntraChromaModeRD(src, qIndex, zbinOverQuant, false, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, &e.coefProbs, fastQuant)
	if !ok {
		return vp8enc.InterFrameMacroblockMode{}, 0, false
	}
	bPredRate := bRate + bUVRate + intraYModeRate(false, vp8common.BPred) + e.interIntraMacroblockModeRate()
	bPredTotal := rdModeScoreWithZbin(qIndex, zbinOverQuant, bPredRate, bDist+bUVDist) + libvpxInterIntraRDPenalty(qIndex)
	if bPredTotal < bestCost {
		best = vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.IntraFrame, Mode: vp8common.BPred, UVMode: bUV, BModes: bModes}
		bestCost = bPredTotal
	}
	return best, bestCost, true
}

// predictBestWholeBlockIntraModeRD picks the best 16x16 intra Y mode using
// libvpx's transform-domain RD (rdopt.c macro_block_yrd) instead of pixel-SSE
// — the AC coefficients are quantized as Y_NO_DC and the 16 DC samples are
// lifted into the Y2 block, Walsh-transformed, and quantized; rate is the
// summed cost_coeffs and distortion is libvpx's
// (mbblock_error<<2 + y2_block_error) >> 4.
func predictBestWholeBlockIntraModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (vp8common.MBPredictionMode, vp8common.MBPredictionMode, int, int, int, int, bool) {
	if quant == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	if coefProbs == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	bestYMode := vp8common.DCPred
	bestYRate := 0
	bestYDist := 0
	bestYCost := 0
	for i, yMode := range wholeBlockIntraYModeCandidates {
		mode := vp8dec.MacroblockMode{RefFrame: vp8common.IntraFrame, Mode: yMode, UVMode: vp8common.DCPred}
		if !predictAnalysisMacroblock(pred, mbRow, mbCol, &mode, scratch) {
			return 0, 0, 0, 0, 0, 0, false
		}
		yRate, yDist := wholeBlockYTransformRD(src, pred, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, coefProbs, fastQuant)
		rate := intraYModeRate(keyFrame, yMode) + yRate
		cost := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, yDist)
		if i == 0 || cost < bestYCost {
			bestYMode = yMode
			bestYRate = rate
			bestYDist = yDist
			bestYCost = cost
		}
	}

	bestUVMode, bestUVRate, bestUVDist, ok := predictBestIntraChromaModeRD(src, qIndex, zbinOverQuant, keyFrame, mbRow, mbCol, aboveTok, leftTok, quant, pred, scratch, coefProbs, fastQuant)
	if !ok {
		return 0, 0, 0, 0, 0, 0, false
	}
	return bestYMode, bestUVMode, bestYRate, bestYDist, bestUVRate, bestUVDist, true
}

// wholeBlockYTransformRD ports libvpx rdopt.c macro_block_yrd. The selected
// yMode prediction is assumed to be present in pred at (mbRow, mbCol).
// aboveTok and leftTok seed the per-block token contexts; libvpx
// vp8_rdcost_mby reads them from `e_mbd.above_context` / `left_context`.
// Callers pass the coefficient probability base that the matching packet
// writer will use for token-rate costing.
func wholeBlockYTransformRD(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, qIndex int, zbinOverQuant int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int) {
	if coefProbs == nil {
		return 0, 0
	}
	var input [16]int16
	var dct [16]int16
	var qcoeff [16]int16
	var dqcoeff [16]int16
	var y2Input [16]int16
	var y2Coeff [16]int16
	var y2Q [16]int16
	var y2DQ [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		y2Left = leftTok.Y2
	}

	rate := 0
	mbblockError := 0
	for block := 0; block < 16; block++ {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		y2Input[block] = dct[0]
		dct[0] = 0
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		eob := quantizeDecisionBlock(fastQuant, &dct, &quant.Y1DC, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
		rate += coefficientBlockTokenRate(coefProbs, 0, ctx, 1, &qcoeff, eob)
		mbblockError += transformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 1 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}
	vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
	y2Ctx := int(y2Above + y2Left)
	y2EOB := quantizeDecisionBlock(fastQuant, &y2Coeff, &quant.Y2, qIndex, zbinOverQuant/2, 0, &y2Q, &y2DQ)
	rate += coefficientBlockTokenRate(coefProbs, 1, y2Ctx, 0, &y2Q, y2EOB)
	y2Error := transformBlockError(&y2Coeff, &y2DQ)
	distortion := ((mbblockError << 2) + y2Error) >> 4
	return rate, distortion
}

func predictBestIntraChromaModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (vp8common.MBPredictionMode, int, int, bool) {
	if quant == nil || coefProbs == nil {
		return 0, 0, 0, false
	}
	bestUVMode := vp8common.DCPred
	bestUVRate := 0
	bestUVDist := 0
	bestUVCost := 0
	for i, uvMode := range wholeBlockIntraUVModeCandidates {
		if !predictAnalysisChroma(pred, mbRow, mbCol, uvMode, scratch) {
			return 0, 0, 0, false
		}
		tokenRate, dist := wholeBlockChromaTransformRD(src, pred, mbRow, mbCol, qIndex, zbinOverQuant, aboveTok, leftTok, quant, coefProbs, fastQuant)
		rate := intraUVModeRate(keyFrame, uvMode) + tokenRate
		cost := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, dist)
		if i == 0 || cost < bestUVCost {
			bestUVMode = uvMode
			bestUVRate = rate
			bestUVDist = dist
			bestUVCost = cost
		}
	}
	return bestUVMode, bestUVRate, bestUVDist, true
}

// wholeBlockChromaTransformRD mirrors libvpx rdopt.c rd_pick_intra_mbuv_mode:
// the predicted U/V blocks are transformed, quantized, token-costed, and
// measured with transform-domain reconstruction error divided by four.
func wholeBlockChromaTransformRD(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, qIndex int, zbinOverQuant int, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) (int, int) {
	if pred == nil || quant == nil || coefProbs == nil {
		return maxInt() / 4, maxInt() / 4
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if aboveTok != nil {
		uvAbove = tokenUVContextArray(aboveTok)
	}
	if leftTok != nil {
		uvLeft = tokenUVContextArray(leftTok)
	}

	rate := 0
	distortion := 0
	for block := 16; block < 24; block++ {
		planeBlock := block - 16
		srcPlane := src.U
		srcStride := src.UStride
		predPlane := pred.U
		predStride := pred.UStride
		if block >= 20 {
			planeBlock = block - 20
			srcPlane = src.V
			srcStride = src.VStride
			predPlane = pred.V
			predStride = pred.VStride
		}
		x := mbCol*8 + (planeBlock&1)*4
		y := mbRow*8 + (planeBlock>>1)*4
		var input [16]int16
		var dct [16]int16
		var qcoeff [16]int16
		var dqcoeff [16]int16
		fillPredictedResidual4x4(srcPlane, srcStride, uvWidth, uvHeight, predPlane, predStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeDecisionBlock(fastQuant, &dct, &quant.UV, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
		rate += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &qcoeff, eob)
		distortion += transformBlockError(&dct, &dqcoeff)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate, distortion >> 2
}

// Ported from libvpx v1.16.0 vp8/encoder/rdopt.c rd_pick_intra4x4block (and
// the per-MB driver rd_pick_intra4x4mby_modes at lines 519-644). Audit notes
// (parity items confirmed against the reference):
//  1. Bmode cost source: keyframe path uses vp8tables.KeyFrameBModeProbs[A][L]
//     via bPredAnalysisAboveMode/LeftMode, matching mb->bmode_costs[A][L];
//     inter path uses vp8tables.DefaultBModeProbs (cf. mb->inter_bmode_costs).
//  2. ENTROPY_CONTEXT: tokenAbove/tokenLeft are seeded once from the caller
//     and only committed using bestEOB after the candidate loop, mirroring
//     libvpx's "*a = tempa; *l = templ;" inside the if-best block.
//  3. Reconstruction: dsp.IDCT4x4Add is invoked inside the winning branch
//     and the resulting bestRecon is written via copyBPredBlock at the end
//     of each block iteration, equivalent to libvpx's deferred
//     vp8_short_idct4x4llm(best_dqcoeff, best_predictor, ...) call.
//  4. Bailout: govpx returns ok=false when the running rate/dist already
//     exceeds bestRD; callers then fall back to the whole-block result, the
//     same role as libvpx's "return INT_MAX" when total_rd >= best_rd.
//  5. BPred container cost: callers (predictBestKeyFrameIntraMode and
//     predictBestInterIntraModeCost) add intraYModeRate(keyFrame, BPred)
//     before comparing with whole-block RD, matching libvpx's
//     "cost = mb->mbmode_cost[xd->frame_type][B_PRED];" seed.
//  6. intra_prediction_down_copy: predictAnalysisBPredBlock reads
//     refs.YAbove[16:20] for the bottom-right sub-block, replacing libvpx's
//     in-place predictor copy.
//
// libvpx applies RDCOST once at MB level (rdopt.c rd_pick_intra4x4mby_modes);
// applying it per-block compounds the +128 rounding bias 16x. bestRD lets
// the caller short-circuit when the running cost already exceeds the best
// macroblock RD found so far.
func predictBestBPredLumaModeRD(src vp8enc.SourceImage, qIndex int, zbinOverQuant int, keyFrame bool, mbRow int, mbCol int, above *vp8enc.KeyFrameMacroblockMode, left *vp8enc.KeyFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, pred *vp8common.Image, scratch *vp8dec.IntraReconstructionScratch, bestRD int, coefProbs *vp8tables.CoefficientProbs, fastQuant bool) ([16]vp8common.BPredictionMode, int, int, bool) {
	if quant == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	if coefProbs == nil {
		return [16]vp8common.BPredictionMode{}, 0, 0, false
	}
	refs := vp8dec.BuildIntraPredictorRefs(pred, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*pred.YStride + mbCol*16
	y := pred.Y[yOff:]
	var modes [16]vp8common.BPredictionMode
	var tokenAbove [4]uint8
	var tokenLeft [4]uint8
	if aboveTok != nil {
		tokenAbove = aboveTok.Y1
	}
	if leftTok != nil {
		tokenLeft = leftTok.Y1
	}
	totalRate := 0
	totalDist := 0
	for block := 0; block < 16; block++ {
		bestMode := vp8common.BDCPred
		bestEOB := 0
		var bestRecon [16]byte
		bestRate := 0
		bestDist := 0
		bestCost := 0
		for i, candidate := range bPredIntraModeCandidates {
			var candidatePred [16]byte
			if !predictAnalysisBPredBlock(candidate, candidatePred[:], 4, y, pred.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
				return [16]vp8common.BPredictionMode{}, 0, 0, false
			}
			var input [16]int16
			var dct [16]int16
			var qcoeff [16]int16
			var dqcoeff [16]int16
			fillBPredResidual4x4(src, mbRow, mbCol, block, candidatePred[:], 4, &input)
			vp8enc.ForwardDCT4x4(input[:], 4, &dct)
			tokenCtx := int(tokenAbove[block&3] + tokenLeft[(block&0x0c)>>2])
			eob := quantizeDecisionBlock(fastQuant, &dct, &quant.Y1, qIndex, zbinOverQuant, 0, &qcoeff, &dqcoeff)
			coefRate := coefficientBlockTokenRate(coefProbs, 3, tokenCtx, 0, &qcoeff, eob)
			aboveMode := bPredAnalysisAboveMode(keyFrame, above, modes, block)
			leftMode := bPredAnalysisLeftMode(keyFrame, left, modes, block)
			rate := bPredModeRate(keyFrame, candidate, aboveMode, leftMode) + coefRate
			dist := transformBlockError(&dct, &dqcoeff) >> 2
			cost := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate, dist)
			if i == 0 || cost < bestCost {
				var candidateRecon [16]byte
				bestMode = candidate
				dsp.IDCT4x4Add(&dqcoeff, candidatePred[:], 4, candidateRecon[:], 4)
				bestRecon = candidateRecon
				bestEOB = eob
				bestRate = rate
				bestDist = dist
				bestCost = cost
			}
		}
		modes[block] = bestMode
		copyBPredBlock(bestRecon[:], 4, y, pred.YStride, block)
		hasCoeffs := uint8(0)
		if bestEOB > 0 {
			hasCoeffs = 1
		}
		tokenAbove[block&3] = hasCoeffs
		tokenLeft[(block&0x0c)>>2] = hasCoeffs
		totalRate += bestRate
		totalDist += bestDist
		if bestRD > 0 && rdModeScoreWithZbin(qIndex, zbinOverQuant, totalRate, totalDist) >= bestRD {
			return [16]vp8common.BPredictionMode{}, 0, 0, false
		}
	}
	return modes, totalRate, totalDist, true
}

func bPredAnalysisAboveMode(keyFrame bool, above *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block >= 4 {
		return modes[block-4]
	}
	if above == nil {
		return vp8common.BDCPred
	}
	if above.YMode == vp8common.BPred {
		return above.BModes[block+12]
	}
	return blockModeFromKeyFrameMacroblockMode(above.YMode)
}

func bPredAnalysisLeftMode(keyFrame bool, left *vp8enc.KeyFrameMacroblockMode, modes [16]vp8common.BPredictionMode, block int) vp8common.BPredictionMode {
	if !keyFrame {
		return vp8common.BDCPred
	}
	if block&3 != 0 {
		return modes[block-1]
	}
	if left == nil {
		return vp8common.BDCPred
	}
	if left.YMode == vp8common.BPred {
		return left.BModes[block+3]
	}
	return blockModeFromKeyFrameMacroblockMode(left.YMode)
}

func blockModeFromKeyFrameMacroblockMode(mode vp8common.MBPredictionMode) vp8common.BPredictionMode {
	switch mode {
	case vp8common.VPred:
		return vp8common.BVEPred
	case vp8common.HPred:
		return vp8common.BHEPred
	case vp8common.TMPred:
		return vp8common.BTMPred
	default:
		return vp8common.BDCPred
	}
}

func intraYModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.KeyFrameYModeTree[:], vp8tables.KeyFrameYModeProbs[:], int(mode))
	}
	return treeTokenCost(vp8tables.YModeTree[:], vp8tables.DefaultYModeProbs[:], int(mode))
}

func intraUVModeRate(keyFrame bool, mode vp8common.MBPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.UVModeTree[:], vp8tables.KeyFrameUVModeProbs[:], int(mode))
	}
	return treeTokenCost(vp8tables.UVModeTree[:], vp8tables.DefaultUVModeProbs[:], int(mode))
}

func bPredModeRate(keyFrame bool, mode vp8common.BPredictionMode, above vp8common.BPredictionMode, left vp8common.BPredictionMode) int {
	if keyFrame {
		return treeTokenCost(vp8tables.BModeTree[:], vp8tables.KeyFrameBModeProbs[int(above)][int(left)][:], int(mode))
	}
	return treeTokenCost(vp8tables.BModeTree[:], vp8tables.DefaultBModeProbs[:], int(mode))
}

// coefficientBlockTokenRate ports libvpx's vp8/encoder/rdopt.c:cost_coeffs.
// It returns the entropy-coded token cost (in 1/256-bit units) of the given
// quantized coefficient block, including the implicit "skip_eob_node" elision
// libvpx applies when the previous token had prev_token_class == 0 (i.e. the
// previous coefficient was a ZERO_TOKEN) and the current coefficient is past
// the first band of the plane.
//
// Equivalent libvpx loop body:
//
//	for (; c < eob; ++c) {
//	    cost += token_costs[type][bands[c]][pt][token(qcoeff[zigzag[c]])];
//	    cost += dct_value_cost[v];
//	    pt = prev_token_class[token];
//	}
//	if (c < 16) cost += token_costs[type][bands[c]][pt][EOB];
//
// where token_costs[type][band][0][...] for band > (type == 0 ? 1 : 0) uses the
// non-EOB subtree only (matching skip_eob_node = (pt == 0) in tokenize.c). All
// other (type, band, pt) combinations include the EOB-vs-not bit.
func coefficientBlockTokenRate(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) int {
	if probs == nil || qcoeff == nil || blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return maxInt() / 4
	}
	if eob < skipDC {
		eob = skipDC
	}
	if eob > 16 {
		eob = 16
	}

	pt := ctx
	cost := 0
	pos := skipDC
	for pos < eob {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		var token int
		if coeff == 0 {
			token = vp8tables.ZeroToken
			cost += coefTokenCostElided(p, token, blockType, band, pt)
		} else {
			t, mag, ok := coefficientTokenMagnitude(coeff)
			if !ok {
				return maxInt() / 4
			}
			token = t
			cost += coefTokenCostElided(p, token, blockType, band, pt)
			if coeff < 0 {
				cost += boolBitCost(128, 1)
			} else {
				cost += boolBitCost(128, 0)
			}
			cost += coefficientExtraBitsRate(token, mag)
		}
		pt = int(vp8tables.PrevTokenClass[token])
		pos++
	}
	if pos < 16 {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		cost += treeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
	}
	return cost
}

// coefficientTokenTraceEntry is one row in a per-position rate breakdown
// produced by coefficientBlockTokenTrace. Trace entries cover the positions
// scanned by the token writer loop (skipDC..eob), including any zero
// coefficients between non-zero ones, and a trailing EOB transition when
// eob<16.
type coefficientTokenTraceEntry struct {
	Position       int
	Coefficient    int
	Token          int
	TokenRate      int
	SignRate       int
	ExtraBits      int
	BandIndex      int
	PrevTokenClass int
}

// coefficientBlockTokenTrace mirrors coefficientBlockTokenRate but records the
// per-position rate breakdown so callers (e.g., the oracle harness) can emit a
// JSON dump for libvpx parity comparison. The returned total exactly matches
// coefficientBlockTokenRate for the same arguments.
func coefficientBlockTokenTrace(probs *vp8tables.CoefficientProbs, blockType int, ctx int, skipDC int, qcoeff *[16]int16, eob int) ([]coefficientTokenTraceEntry, int) {
	if probs == nil || qcoeff == nil || blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return nil, maxInt() / 4
	}
	if eob < skipDC {
		eob = skipDC
	}
	if eob > 16 {
		eob = 16
	}

	pt := ctx
	cost := 0
	trace := make([]coefficientTokenTraceEntry, 0, eob-skipDC+1)
	for pos := skipDC; pos < eob; pos++ {
		band := int(vp8tables.CoefBandsTable[pos])
		p := (*probs)[blockType][band][pt]
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coeff := int(qcoeff[rc])
		entry := coefficientTokenTraceEntry{
			Position:       pos,
			Coefficient:    coeff,
			BandIndex:      band,
			PrevTokenClass: pt,
		}
		var token int
		if coeff == 0 {
			token = vp8tables.ZeroToken
			entry.Token = token
			entry.TokenRate = coefTokenCostElided(p, token, blockType, band, pt)
			cost += entry.TokenRate
		} else {
			t, mag, ok := coefficientTokenMagnitude(coeff)
			if !ok {
				return nil, maxInt() / 4
			}
			token = t
			entry.Token = token
			entry.TokenRate = coefTokenCostElided(p, token, blockType, band, pt)
			if coeff < 0 {
				entry.SignRate = boolBitCost(128, 1)
			} else {
				entry.SignRate = boolBitCost(128, 0)
			}
			entry.ExtraBits = coefficientExtraBitsRate(token, mag)
			cost += entry.TokenRate + entry.SignRate + entry.ExtraBits
		}
		trace = append(trace, entry)
		pt = int(vp8tables.PrevTokenClass[token])
	}
	if eob < 16 {
		band := int(vp8tables.CoefBandsTable[eob])
		p := (*probs)[blockType][band][pt]
		eobRate := treeTokenCost(vp8tables.CoefTree[:], p[:], vp8tables.DCTEOBToken)
		trace = append(trace, coefficientTokenTraceEntry{
			Position:       eob,
			Coefficient:    0,
			Token:          vp8tables.DCTEOBToken,
			TokenRate:      eobRate,
			BandIndex:      band,
			PrevTokenClass: pt,
		})
		cost += eobRate
	}
	return trace, cost
}

// coefTokenCostElided returns the token cost charged at one coefficient
// position. It mirrors libvpx's `token_costs` table: when the prior token's
// prev_token_class is 0 (a ZERO_TOKEN) and the current band is past the
// plane's first encoded band, the EOB-vs-not bit is elided and only the
// non-EOB subtree cost is charged. Otherwise the full tree cost is charged.
func coefTokenCostElided(probs [vp8tables.EntropyNodes]uint8, token int, blockType int, band int, pt int) int {
	threshold := 0
	if blockType == 0 {
		threshold = 1
	}
	full := treeTokenCost(vp8tables.CoefTree[:], probs[:], token)
	if pt == 0 && band > threshold {
		nonEOB := boolBitCost(probs[0], 1)
		if full <= nonEOB {
			return maxInt() / 4
		}
		return full - nonEOB
	}
	return full
}

func nonZeroCoeffTokenRate(probs [vp8tables.EntropyNodes]uint8, token int) int {
	cost := treeTokenCost(vp8tables.CoefTree[:], probs[:], token)
	nonEOBRate := boolBitCost(probs[0], 1)
	if cost <= nonEOBRate {
		return maxInt() / 4
	}
	return cost - nonEOBRate
}

func coefficientTokenMagnitude(coeff int) (int, int, bool) {
	if coeff < 0 {
		coeff = -coeff
	}
	switch {
	case coeff <= 0:
		return 0, 0, false
	case coeff == 1:
		return vp8tables.OneToken, coeff, true
	case coeff == 2:
		return vp8tables.TwoToken, coeff, true
	case coeff == 3:
		return vp8tables.ThreeToken, coeff, true
	case coeff == 4:
		return vp8tables.FourToken, coeff, true
	case coeff <= 6:
		return vp8tables.DCTValCategory1, coeff, true
	case coeff <= 10:
		return vp8tables.DCTValCategory2, coeff, true
	case coeff <= 18:
		return vp8tables.DCTValCategory3, coeff, true
	case coeff <= 34:
		return vp8tables.DCTValCategory4, coeff, true
	case coeff <= 66:
		return vp8tables.DCTValCategory5, coeff, true
	case coeff <= vp8tables.DCTMaxValue:
		return vp8tables.DCTValCategory6, coeff, true
	default:
		return 0, 0, false
	}
}

func coefficientExtraBitsRate(token int, mag int) int {
	extra := vp8tables.ExtraBitsTable[token]
	offset := mag - int(extra.BaseVal)
	cost := 0
	for i := 0; i < int(extra.Len); i++ {
		shift := int(extra.Len) - 1 - i
		bit := int((offset >> uint(shift)) & 1)
		cost += boolBitCost(extra.Prob[i], bit)
	}
	return cost
}

func treeTokenCost(tree []int16, probs []uint8, token int) int {
	var encoded vp8enc.TreeToken
	if !vp8enc.BuildTreeToken(tree, token, &encoded) {
		return maxInt() / 4
	}
	node := int16(0)
	cost := 0
	for bitIndex := int(encoded.Len) - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		if probIndex < 0 || probIndex >= len(probs) || int(node)+1 >= len(tree) {
			return maxInt() / 4
		}
		prob := probs[probIndex]
		bit := int((encoded.Value >> uint(bitIndex)) & 1)
		cost += boolBitCost(prob, bit)
		next := tree[int(node)+bit]
		if next <= 0 {
			if bitIndex == 0 {
				return cost
			}
			return maxInt() / 4
		}
		node = next
	}
	return maxInt() / 4
}

func boolBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return vp8tables.ProbCost[prob]
	}
	return vp8tables.ProbCost[255-int(prob)]
}

func rdModeScore(qIndex int, rate int, distortion int) int {
	return rdModeScoreWithZbin(qIndex, 0, rate, distortion)
}

func rdModeScoreWithZbin(qIndex int, zbinOverQuant int, rate int, distortion int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func libvpxInterIntraRDPenalty(qIndex int) int {
	return 10 * vp8common.DCQuant(qIndex, 0)
}

// libvpxErrorPerBit ports the encodeframe.c errorperbit derivation used by
// libvpx fractional motion searches.
func libvpxErrorPerBit(qIndex int) int {
	return libvpxErrorPerBitWithZbin(qIndex, 0)
}

func libvpxErrorPerBitWithZbin(qIndex int, zbinOverQuant int) int {
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	errorPerBit := rdMult * 100 / (110 * rdDiv)
	if errorPerBit == 0 {
		return 1
	}
	return errorPerBit
}

// libvpxSADPerBit16 ports sad_per_bit16lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts.
func libvpxSADPerBit16(qIndex int) int {
	return libvpxSADPerBit16LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxSADPerBit4 ports sad_per_bit4lut from
// vp8/encoder/rdopt.c vp8cx_initialize_me_consts for SPLITMV block search.
func libvpxSADPerBit4(qIndex int) int {
	return libvpxSADPerBit4LUT[vp8common.ClampQIndex(qIndex)]
}

// libvpxRDConstants ports vp8_initialize_rd_consts for the single-pass path.
func libvpxRDConstants(qIndex int) (int, int) {
	return libvpxRDConstantsWithZbin(qIndex, 0)
}

func libvpxRDConstantsWithZbin(qIndex int, zbinOverQuant int) (int, int) {
	qValue := vp8common.DCQuant(qIndex, 0)
	if qValue > 160 {
		qValue = 160
	}
	rdMult := int(2.80 * float64(qValue*qValue))
	if zbinOverQuant > 0 {
		oqFactor := 1.0 + 0.0015625*float64(zbinOverQuant)
		modq := int(float64(qValue) * oqFactor)
		rdMult = int(2.80 * float64(modq*modq))
	}
	rdDiv := 100
	if rdMult > 1000 {
		rdDiv = 1
		rdMult /= 100
	}
	return rdMult, rdDiv
}

func libvpxRDCost(rdMult int, rdDiv int, rate int, distortion int) int {
	return ((128 + rate*rdMult) >> 8) + rdDiv*distortion
}

var libvpxSADPerBit16LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 4, 4,
	4, 4, 4, 4, 4, 4, 4, 4,
	4, 4, 5, 5, 5, 5, 5, 5,
	5, 5, 5, 5, 5, 5, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 10, 10,
	10, 10, 10, 10, 10, 10, 11, 11,
	11, 11, 11, 11, 12, 12, 12, 12,
	12, 12, 13, 13, 13, 13, 14, 14,
}

var libvpxSADPerBit4LUT = [vp8common.QIndexRange]int{
	2, 2, 2, 2, 2, 2, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 4, 4, 4, 4,
	4, 4, 4, 4, 4, 4, 5, 5,
	5, 5, 5, 5, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 8, 8, 8,
	8, 8, 9, 9, 9, 9, 9, 9,
	10, 10, 10, 10, 10, 10, 10, 10,
	11, 11, 11, 11, 11, 11, 11, 11,
	12, 12, 12, 12, 12, 12, 12, 12,
	13, 13, 13, 13, 13, 13, 13, 14,
	14, 14, 14, 14, 15, 15, 15, 15,
	16, 16, 16, 16, 17, 17, 17, 18,
	18, 18, 19, 19, 19, 20, 20, 20,
}

func interMotionRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return rdModeScore(qIndex, interMotionVectorCost(mv, mvProbs), macroblockLumaSSE(src, ref, mbRow, mbCol, mv))
}

func (e *VP8Encoder) estimateInterResidualRDScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8) (int, bool) {
	refRate := 1 << 30
	if e != nil && mode != nil {
		refRate = e.interReferenceFrameRate(mode.RefFrame)
	}
	return e.estimateInterResidualRDScoreWithReferenceRate(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, refRate)
}

func (e *VP8Encoder) estimateInterResidualRDScoreWithReferenceRate(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (int, bool) {
	score, _, ok := e.estimateInterResidualRDScoreWithReferenceRateAndSkip(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, refRate)
	return score, ok
}

func (e *VP8Encoder) estimateInterResidualRDScoreWithReferenceRateAndSkip(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (int, bool, bool) {
	acct, ok := e.estimateInterResidualRDAccounting(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, aboveTok, leftTok, quant, qIndex, segmentID, refRate)
	return acct.rd, acct.rdLoopSkip, ok
}

type interResidualRDAccounting struct {
	rd           int
	yrd          int
	rate2        int
	rateY        int
	rateUV       int
	distortion2  int
	distortionUV int
	otherCost    int
	refCost      int
	rdLoopSkip   bool
	mbSkipCoeff  bool
}

func (e *VP8Encoder) estimateInterResidualRDAccounting(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, segmentID uint8, refRate int) (interResidualRDAccounting, bool) {
	if e == nil || ref == nil || mode == nil || quant == nil || segmentID >= vp8common.MaxMBSegments {
		return interResidualRDAccounting{}, false
	}
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	var decMode vp8dec.MacroblockMode
	convertInterFrameMode(mode, &decMode)
	predMode := decMode
	predMode.MBSkipCoeff = true
	var zeroTokens vp8dec.MacroblockTokens
	if !reconstructInterAnalysisMacroblock(&e.analysis.Img, ref, mbRow, mbCol, &predMode, &zeroTokens, &e.dequants[segmentID], &e.reconstructScratch) {
		return interResidualRDAccounting{}, false
	}

	modeRate := e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate)
	refCost := boolBitCost(e.refProbIntra, 1) + refRate
	otherCost := e.interMacroblockSkipRate(false)
	predictionDist := macroblockImageSSE(src, &e.analysis.Img, mbRow, mbCol)
	if staticInterEncodeBreakout(src, &e.analysis.Img, mbRow, mbCol, quant, e.opts.StaticThreshold) {
		rd := rdModeScoreWithZbin(qIndex, zbinOverQuant, 500, predictionDist)
		return interResidualRDAccounting{
			rd:          rd,
			yrd:         rd,
			rate2:       500,
			distortion2: predictionDist,
			otherCost:   otherCost,
			refCost:     refCost,
			rdLoopSkip:  true,
			mbSkipCoeff: true,
		}, true
	}

	var coeffs vp8enc.MacroblockCoefficients
	is4x4 := interFrameModeUses4x4Tokens(mode.Mode)
	stats := buildPredictedMacroblockCoefficientsRD(&e.coefProbs, src, mbRow, mbCol, &e.analysis.Img, aboveTok, leftTok, quant, qIndex, e.rc.currentZbinOverQuant, interZbinModeBoost(mode), is4x4, false, e.libvpxUseFastQuant(), false, &coeffs)
	rateUV := stats.rateUV
	rate2 := modeRate + otherCost + stats.rateY + rateUV
	distortion2 := stats.distortionY + stats.distortionUV
	mbSkipCoeff := stats.tteob == 0
	if mbSkipCoeff {
		rate2 -= stats.rateY + stats.rateUV
		rateUV = 0
		skipBackout := e.interMacroblockSkipRate(true) - e.interMacroblockSkipRate(false)
		rate2 += skipBackout
		otherCost += skipBackout
	}
	rd := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate2, distortion2)
	yrd := rdModeScoreWithZbin(qIndex, zbinOverQuant, rate2-rateUV-otherCost-refCost, distortion2-stats.distortionUV)
	return interResidualRDAccounting{
		rd:           rd,
		yrd:          yrd,
		rate2:        rate2,
		rateY:        stats.rateY,
		rateUV:       rateUV,
		distortion2:  distortion2,
		distortionUV: stats.distortionUV,
		otherCost:    otherCost,
		refCost:      refCost,
		mbSkipCoeff:  mbSkipCoeff,
	}, true
}

func (e *VP8Encoder) estimateFastInterModeScore(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int) (int, bool) {
	refRate := 1 << 30
	if e != nil && mode != nil {
		refRate = e.interReferenceFrameRate(mode.RefFrame)
	}
	return e.estimateFastInterModeScoreWithReferenceRate(src, ref, mbRow, mbCol, mbRows, mbCols, mode, above, left, aboveLeft, qIndex, refRate)
}

func (e *VP8Encoder) estimateFastInterModeScoreWithReferenceRate(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, qIndex int, refRate int) (int, bool) {
	if ref == nil || mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode == vp8common.SplitMV {
		return 0, false
	}
	modeRate := e.fastInterMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate)
	variance, _ := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mode.MV)
	zbinOverQuant := 0
	if e != nil {
		zbinOverQuant = e.rc.currentZbinOverQuant
	}
	score := rdModeScoreWithZbin(qIndex, zbinOverQuant, modeRate, variance)
	if mode.RefFrame == vp8common.LastFrame && mode.Mode == vp8common.ZeroMV {
		adj := 100
		if e.fastZeroMVLastAdjustmentEligible(mbRows, mbCols) {
			adj = fastZeroMVLastRDAdjustment(mbRow, mbCol, above, left, aboveLeft)
		}
		// Dot-artifact bias overrides the local-motion reduction with a 1.5x
		// penalty (libvpx pickinter.c). Skin macroblocks reset the multiplier
		// to 100 so face-coloured blocks aren't pushed off ZEROMV-LAST.
		if e.checkDotArtifactCandidateY(src, ref, mbRow, mbCol, mbRows, mbCols) {
			adj = 150
		}
		if e.macroblockIsSkin(mbRow, mbCol, mbCols) {
			adj = 100
		}
		// libvpx denoiser pickmode_mv_bias: aggressive denoise scales ZEROMV
		// down (multiplier=75) so ZEROMV-LAST is preferred for noisy areas.
		// Non-aggressive denoise leaves the multiplier at 100.
		score = (score * adj * e.denoiserPickmodeMVBias()) / 10000
	}
	return score, true
}

func (e *VP8Encoder) macroblockIsSkin(mbRow int, mbCol int, mbCols int) bool {
	if e == nil || len(e.skinMap) == 0 {
		return false
	}
	index := mbRow*mbCols + mbCol
	if index < 0 || index >= len(e.skinMap) {
		return false
	}
	return e.skinMap[index] != 0
}

func (e *VP8Encoder) fastZeroMVLastAdjustmentEligible(mbRows int, mbCols int) bool {
	if e == nil || e.opts.ScreenContentMode != 0 {
		return false
	}
	required := mbRows * mbCols
	return required > 0 && e.lastInterZeroMVCount*100 > 40*required
}

func fastZeroMVLastRDAdjustment(mbRow int, mbCol int, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode) int {
	localMotion := 0
	if interModeHasSmallMotion(left) {
		localMotion++
	}
	if interModeHasSmallMotion(aboveLeft) {
		localMotion++
	}
	if interModeHasSmallMotion(above) {
		localMotion++
	}
	if ((mbRow == 0 || mbCol == 0) && localMotion > 0) || localMotion > 2 {
		return 80
	}
	if localMotion > 0 {
		return 90
	}
	return 100
}

func interModeHasSmallMotion(mode *vp8enc.InterFrameMacroblockMode) bool {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return false
	}
	row := int(mode.MV.Row)
	if row < 0 {
		row = -row
	}
	col := int(mode.MV.Col)
	if col < 0 {
		col = -col
	}
	return row < 8 && col < 8
}

func selectInterFrameMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearch(src, ref, mbRow, mbCol, 0, 0, bestRefMV, qIndex, defaultInterAnalysisSearchConfig(), mvProbs)
}

func selectInterFrameMotionVectorWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	return selectInterFrameMotionVectorWithSearchStart(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, interFrameSearchStart{}, mvProbs)
}

func selectInterFrameMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	best, bestCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs)
	if bestCost == 0 {
		return best, bestCost
	}
	bestRD := interMotionRDScore(src, ref, mbRow, mbCol, best, qIndex, mvProbs)
	if refined, _, ok := refineInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, search, mvProbs); ok {
		refinedRD := interMotionRDScore(src, ref, mbRow, mbCol, refined, qIndex, mvProbs)
		if refinedRD < bestRD {
			best = refined
			bestRD = refinedRD
		}
	}
	return best, bestRD
}

func selectRDInterFrameMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	best, bestCost := selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, mvProbs)
	if refined, refinedCost, ok := refineInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, search, mvProbs); ok {
		return refined, refinedCost
	}
	return best, bestCost
}

// selectInterFrameFullPixelMotionVector centers the integer-pel search at
// bestRefMV (libvpx pickinter.c uses `mvp_full = bestRefMV >> 3`) and charges
// the candidate's MV-cost against bestRefMV instead of (0,0). Standalone
// callers keep the exhaustive sweep for existing coverage; encoder mode
// decision uses libvpx's NSTEP/hex speed-feature paths.
func selectInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearch(src, ref, mbRow, mbCol, 0, 0, bestRefMV, qIndex, defaultInterAnalysisSearchConfig())
}

func selectInterFrameFullPixelMotionVectorWithSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStart(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, interFrameSearchStart{})
}

func selectInterFrameFullPixelMotionVectorWithSearchStart(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart) (vp8enc.MotionVector, int) {
	return selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src, ref, mbRow, mbCol, mbRows, mbCols, bestRefMV, qIndex, search, start, &vp8tables.DefaultMVContext)
}

func selectInterFrameFullPixelMotionVectorWithSearchStartAndProbs(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mbRows int, mbCols int, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, start interFrameSearchStart, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	searchStart := bestRefMV
	if start.ok && search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		searchStart = start.mv
		search = search.adjustedForImprovedMVStart(start)
	}
	centerRow := int(searchStart.Row) & ^7
	centerCol := int(searchStart.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	bounds := interFrameFullPixelSearchBounds(bestRefMV, mbRow, mbCol, mbRows, mbCols)
	if search.fullPixelSearch != interAnalysisFullPixelSearchExhaustive {
		best = bounds.clampEighth(best)
	}
	bestWalkCost := interMotionSearchCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex)
	if bestWalkCost == 0 {
		return best, interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchNstep {
		return nstepInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, bounds, search, mvProbs)
	}
	if search.fullPixelSearch == interAnalysisFullPixelSearchHex {
		return hexInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, bounds)
	}
	return exhaustiveInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestWalkCost, bestRefMV, qIndex, mvProbs)
}

type interFrameSearchStart struct {
	mv vp8enc.MotionVector
	sr int
	ok bool
}

func (search interAnalysisSearchConfig) adjustedForImprovedMVStart(start interFrameSearchStart) interAnalysisSearchConfig {
	if !start.ok {
		return search
	}
	stepParam := start.sr + search.fullPixelSpeedAdjust
	if stepParam > search.fullPixelSearchParam {
		if stepParam >= interFrameMaxMVSearchSteps {
			stepParam = interFrameMaxMVSearchSteps - 1
		}
		search.fullPixelSearchParam = stepParam
		search.fullPixelFurtherSteps = libvpxInterFrameFurtherSteps(search.fullPixelSpeed, stepParam)
	}
	return search
}

type improvedInterFrameMVSlot struct {
	mv       vp8enc.MotionVector
	refFrame vp8common.MVReferenceFrame
	sad      int
}

func (e *VP8Encoder) improvedInterFrameSearchStart(
	src vp8enc.SourceImage, refFrame vp8common.MVReferenceFrame,
	mbRow int, mbCol int, mbRows int, mbCols int,
	above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode,
	search interAnalysisSearchConfig,
) interFrameSearchStart {
	if e == nil || !search.improvedMVPrediction || refFrame == vp8common.IntraFrame {
		return interFrameSearchStart{}
	}
	var slots [8]improvedInterFrameMVSlot
	slotCount := 3
	fillImprovedInterFrameCurrentMVSlot(&slots[0], src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol, above)
	fillImprovedInterFrameCurrentMVSlot(&slots[1], src, &e.analysis.Img, mbRow, mbCol, mbRow, mbCol-1, left)
	fillImprovedInterFrameCurrentMVSlot(&slots[2], src, &e.analysis.Img, mbRow, mbCol, mbRow-1, mbCol-1, aboveLeft)
	if e.lastFrameInterModesValid && len(e.lastFrameInterModes) >= mbRows*mbCols && mbRows > 0 && mbCols > 0 {
		slotCount = 8
		fillImprovedInterFrameLastMVSlot(&slots[3], src, &e.lastRef.Img, e.lastFrameInterModes, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol)
		fillImprovedInterFrameLastMVSlot(&slots[4], src, &e.lastRef.Img, e.lastFrameInterModes, mbRow, mbCol, mbRows, mbCols, mbRow-1, mbCol)
		fillImprovedInterFrameLastMVSlot(&slots[5], src, &e.lastRef.Img, e.lastFrameInterModes, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol-1)
		fillImprovedInterFrameLastMVSlot(&slots[6], src, &e.lastRef.Img, e.lastFrameInterModes, mbRow, mbCol, mbRows, mbCols, mbRow, mbCol+1)
		fillImprovedInterFrameLastMVSlot(&slots[7], src, &e.lastRef.Img, e.lastFrameInterModes, mbRow, mbCol, mbRows, mbCols, mbRow+1, mbCol)
	}
	order := improvedInterFrameMVSlotOrder(slots, slotCount)
	for rank := 0; rank < slotCount; rank++ {
		slot := slots[order[rank]]
		if slot.refFrame == refFrame {
			sr := 2
			if rank < 3 {
				sr = 3
			}
			return interFrameSearchStart{mv: slot.mv, sr: sr, ok: true}
		}
	}
	mv := improvedInterFrameMVMedian(slots, slotCount)
	return interFrameSearchStart{mv: mv, sr: 0, ok: true}
}

func fillImprovedInterFrameCurrentMVSlot(slot *improvedInterFrameMVSlot, src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int, mode *vp8enc.InterFrameMacroblockMode) {
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if mode == nil || refMbRow < 0 || refMbCol < 0 {
		return
	}
	slot.mv = mode.MV
	slot.refFrame = convertInterFrameReference(mode)
	if slot.refFrame != vp8common.IntraFrame {
		slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
	}
}

func fillImprovedInterFrameLastMVSlot(slot *improvedInterFrameMVSlot, src vp8enc.SourceImage, img *vp8common.Image, modes []vp8enc.InterFrameMacroblockMode, srcMbRow int, srcMbCol int, mbRows int, mbCols int, refMbRow int, refMbCol int) {
	*slot = improvedInterFrameMVSlot{sad: maxInt()}
	if refMbRow < 0 || refMbCol < 0 || refMbRow >= mbRows || refMbCol >= mbCols {
		return
	}
	index := refMbRow*mbCols + refMbCol
	if index < 0 || index >= len(modes) {
		return
	}
	mode := &modes[index]
	slot.mv = mode.MV
	slot.refFrame = convertInterFrameReference(mode)
	if slot.refFrame != vp8common.IntraFrame {
		slot.sad = macroblockImageBlockSAD(src, img, srcMbRow, srcMbCol, refMbRow, refMbCol)
	}
}

func improvedInterFrameMVSlotOrder(slots [8]improvedInterFrameMVSlot, count int) [8]int {
	var order [8]int
	for i := 0; i < count && i < len(order); i++ {
		order[i] = i
	}
	for i := 1; i < count && i < len(order); i++ {
		idx := order[i]
		sad := slots[idx].sad
		j := i - 1
		for ; j >= 0 && sad < slots[order[j]].sad; j-- {
			order[j+1] = order[j]
		}
		order[j+1] = idx
	}
	return order
}

func improvedInterFrameMVMedian(slots [8]improvedInterFrameMVSlot, count int) vp8enc.MotionVector {
	if count <= 0 {
		return vp8enc.MotionVector{}
	}
	var rows [8]int
	var cols [8]int
	for i := 0; i < count && i < len(slots); i++ {
		rows[i] = int(slots[i].mv.Row)
		cols[i] = int(slots[i].mv.Col)
	}
	insertionSortInts(rows[:count])
	insertionSortInts(cols[:count])
	return vp8enc.MotionVector{Row: int16(rows[count/2]), Col: int16(cols[count/2])}
}

func insertionSortInts(values []int) {
	for i := 1; i < len(values); i++ {
		v := values[i]
		j := i - 1
		for ; j >= 0 && v < values[j]; j-- {
			values[j+1] = values[j]
		}
		values[j+1] = v
	}
}

func selectInterFrameSplitBlockFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, bestRefMV vp8enc.MotionVector, qIndex int) (vp8enc.MotionVector, int) {
	centerRow := int(bestRefMV.Row) & ^7
	centerCol := int(bestRefMV.Col) & ^7
	best := vp8enc.MotionVector{Row: int16(centerRow), Col: int16(centerCol)}
	bestCost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, best, bestRefMV, qIndex)
	if bestCost == 0 {
		return best, bestCost
	}
	for row := centerRow - interFrameSplitMVSearchRange; row <= centerRow+interFrameSplitMVSearchRange; row += interFrameMVFullPixelStep {
		for col := centerCol - interFrameSplitMVSearchRange; col <= centerCol+interFrameSplitMVSearchRange; col += interFrameMVFullPixelStep {
			mv := vp8enc.MotionVector{Row: int16(row), Col: int16(col)}
			if mv == best {
				continue
			}
			cost := interMotionSplitBlockSearchCost(src, ref, mbRow, mbCol, block, width, height, mv, bestRefMV, qIndex)
			if cost < bestCost {
				best = mv
				bestCost = cost
				if bestCost == 0 {
					return best, bestCost
				}
			}
		}
	}
	return best, bestCost
}

func exhaustiveInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	centerRow := int(bestRefMV.Row) & ^7
	centerCol := int(bestRefMV.Col) & ^7
	for row := centerRow - interFrameMVSearchRange; row <= centerRow+interFrameMVSearchRange; row += interFrameMVFullPixelStep {
		for col := centerCol - interFrameMVSearchRange; col <= centerCol+interFrameMVSearchRange; col += interFrameMVFullPixelStep {
			mv := vp8enc.MotionVector{Row: int16(row), Col: int16(col)}
			if mv == best {
				continue
			}
			cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestWalkCost, bestRefMV, qIndex)
			if cost < bestWalkCost {
				best = mv
				bestWalkCost = cost
			}
		}
	}
	return best, interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
}

type interFrameFullPixelBounds struct {
	rowMin int
	rowMax int
	colMin int
	colMax int
}

func interFrameFullPixelSearchBounds(bestRefMV vp8enc.MotionVector, mbRow int, mbCol int, mbRows int, mbCols int) interFrameFullPixelBounds {
	bounds := interFrameFullPixelBounds{
		rowMin: ((int(bestRefMV.Row) + 7) >> 3) - interFrameMaxFullPelVal,
		rowMax: (int(bestRefMV.Row) >> 3) + interFrameMaxFullPelVal,
		colMin: ((int(bestRefMV.Col) + 7) >> 3) - interFrameMaxFullPelVal,
		colMax: (int(bestRefMV.Col) >> 3) + interFrameMaxFullPelVal,
	}
	if mbRows > 0 {
		umv := interFrameUMVBorderPixels - 16
		rowMin := -((mbRow * 16) + umv)
		rowMax := ((mbRows - 1 - mbRow) * 16) + umv
		if bounds.rowMin < rowMin {
			bounds.rowMin = rowMin
		}
		if bounds.rowMax > rowMax {
			bounds.rowMax = rowMax
		}
	}
	if mbCols > 0 {
		umv := interFrameUMVBorderPixels - 16
		colMin := -((mbCol * 16) + umv)
		colMax := ((mbCols - 1 - mbCol) * 16) + umv
		if bounds.colMin < colMin {
			bounds.colMin = colMin
		}
		if bounds.colMax > colMax {
			bounds.colMax = colMax
		}
	}
	return bounds
}

func (b interFrameFullPixelBounds) containsFullPel(row int, col int) bool {
	return row >= b.rowMin && row <= b.rowMax && col >= b.colMin && col <= b.colMax
}

func (b interFrameFullPixelBounds) containsFullPelStrict(row int, col int) bool {
	return row > b.rowMin && row < b.rowMax && col > b.colMin && col < b.colMax
}

func (b interFrameFullPixelBounds) clampEighth(mv vp8enc.MotionVector) vp8enc.MotionVector {
	row := int(mv.Row) >> 3
	col := int(mv.Col) >> 3
	if row < b.rowMin {
		row = b.rowMin
	} else if row > b.rowMax {
		row = b.rowMax
	}
	if col < b.colMin {
		col = b.colMin
	} else if col > b.colMax {
		col = b.colMax
	}
	return vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
}

type interFrameNstepSearchResult struct {
	mv    vp8enc.MotionVector
	cost  int
	num00 int
}

func nstepInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	stepParam := search.fullPixelSearchParam
	if stepParam < 0 {
		stepParam = 0
	} else if stepParam >= interFrameMaxMVSearchSteps {
		stepParam = interFrameMaxMVSearchSteps - 1
	}

	result := diamondNstepInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam, mvProbs)
	best := result.mv
	bestCost := result.cost
	n := result.num00
	num00 := 0
	doRefine := search.fullPixelFinalRefine
	if n > search.fullPixelFurtherSteps {
		doRefine = false
	}
	for n < search.fullPixelFurtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		candidate := diamondNstepInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, center, centerWalkCost, bestRefMV, qIndex, bounds, stepParam+n, mvProbs)
		num00 = candidate.num00
		if search.fullPixelFinalRefine && num00 > search.fullPixelFurtherSteps-n {
			doRefine = false
		}
		if candidate.cost < bestCost {
			best = candidate.mv
			bestCost = candidate.cost
		}
	}
	if doRefine {
		best, bestCost = refineInterFrameFullPixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, bounds, 8, mvProbs)
	}
	return best, bestCost
}

func diamondNstepInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, center vp8enc.MotionVector, centerWalkCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchParam int, mvProbs *[2][vp8tables.MVPCount]uint8) interFrameNstepSearchResult {
	sites := interFrameNstepSearchSites()
	if searchParam < 0 {
		searchParam = 0
	} else if searchParam >= interFrameMaxMVSearchSteps {
		searchParam = interFrameMaxMVSearchSteps - 1
	}
	best := center
	bestWalkCost := centerWalkCost
	start := center
	startIndex := searchParam * 8
	totalSteps := (len(sites) / 8) - searchParam
	i := 1
	bestSite := 0
	lastSite := 0
	num00 := 0
	for step := 0; step < totalSteps; step++ {
		for j := 0; j < 8; j++ {
			site := sites[startIndex+i]
			row := (int(best.Row) >> 3) + int(site.Row)
			col := (int(best.Col) >> 3) + int(site.Col)
			if bounds.containsFullPelStrict(row, col) {
				mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
				cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestWalkCost, bestRefMV, qIndex)
				if cost < bestWalkCost {
					bestWalkCost = cost
					bestSite = i
				}
			}
			i++
		}
		if bestSite != lastSite {
			site := sites[startIndex+bestSite]
			best = vp8enc.MotionVector{
				Row: int16(int(best.Row) + int(site.Row)*interFrameMVFullPixelStep),
				Col: int16(int(best.Col) + int(site.Col)*interFrameMVFullPixelStep),
			}
			lastSite = bestSite
		} else if best == start {
			num00++
		}
	}
	return interFrameNstepSearchResult{mv: best, cost: interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs), num00: num00}
}

func refineInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, start vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds, searchRange int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int) {
	neighbors := [...]vp8enc.MotionVector{
		{Row: -1},
		{Col: -1},
		{Col: 1},
		{Row: 1},
	}
	best := start
	bestWalkCost := interMotionSearchCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex)
	for i := 0; i < searchRange; i++ {
		bestSite := -1
		br := int(best.Row) >> 3
		bc := int(best.Col) >> 3
		for j, step := range neighbors {
			row := br + int(step.Row)
			col := bc + int(step.Col)
			if !bounds.containsFullPelStrict(row, col) {
				continue
			}
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestWalkCost, bestRefMV, qIndex)
			if cost < bestWalkCost {
				bestWalkCost = cost
				bestSite = j
			}
		}
		if bestSite < 0 {
			break
		}
		best = vp8enc.MotionVector{
			Row: int16(int(best.Row) + int(neighbors[bestSite].Row)*interFrameMVFullPixelStep),
			Col: int16(int(best.Col) + int(neighbors[bestSite].Col)*interFrameMVFullPixelStep),
		}
	}
	return best, interMotionFullPixelSearchReturnCost(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
}

func interFrameNstepSearchSites() [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector {
	var sites [1 + interFrameMaxMVSearchSteps*8]vp8enc.MotionVector
	count := 1
	for length := 1 << (interFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: 0, Col: int16(length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(-length), Col: int16(length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(-length)}
		count++
		sites[count] = vp8enc.MotionVector{Row: int16(length), Col: int16(length)}
		count++
	}
	return sites
}

func hexInterFrameFullPixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, bounds interFrameFullPixelBounds) (vp8enc.MotionVector, int) {
	hex := [...]vp8enc.MotionVector{
		{Row: -1, Col: -2},
		{Row: 1, Col: -2},
		{Row: 2, Col: 0},
		{Row: 1, Col: 2},
		{Row: -1, Col: 2},
		{Row: -2, Col: 0},
	}
	nextCheckpoints := [...][3]vp8enc.MotionVector{
		{{Row: -2, Col: 0}, {Row: -1, Col: -2}, {Row: 1, Col: -2}},
		{{Row: -1, Col: -2}, {Row: 1, Col: -2}, {Row: 2, Col: 0}},
		{{Row: 1, Col: -2}, {Row: 2, Col: 0}, {Row: 1, Col: 2}},
		{{Row: 2, Col: 0}, {Row: 1, Col: 2}, {Row: -1, Col: 2}},
		{{Row: 1, Col: 2}, {Row: -1, Col: 2}, {Row: -2, Col: 0}},
		{{Row: -1, Col: 2}, {Row: -2, Col: 0}, {Row: -1, Col: -2}},
	}
	neighbors := [...]vp8enc.MotionVector{
		{Row: 0, Col: -1},
		{Row: -1, Col: 0},
		{Row: 1, Col: 0},
		{Row: 0, Col: 1},
	}

	br := int(best.Row) >> 3
	bc := int(best.Col) >> 3
	bestSite := -1
	for i, step := range hex {
		row := br + int(step.Row)
		col := bc + int(step.Col)
		if !bounds.containsFullPel(row, col) {
			continue
		}
		mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
		cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestCost, bestRefMV, qIndex)
		if cost < bestCost {
			best = mv
			bestCost = cost
			bestSite = i
		}
	}
	if bestSite >= 0 {
		br = int(best.Row) >> 3
		bc = int(best.Col) >> 3
		k := bestSite
		for j := 1; j < 127; j++ {
			bestSite = -1
			for i, step := range nextCheckpoints[k] {
				row := br + int(step.Row)
				col := bc + int(step.Col)
				if !bounds.containsFullPel(row, col) {
					continue
				}
				mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
				cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestCost, bestRefMV, qIndex)
				if cost < bestCost {
					best = mv
					bestCost = cost
					bestSite = i
				}
			}
			if bestSite < 0 {
				break
			}
			br = int(best.Row) >> 3
			bc = int(best.Col) >> 3
			k += 5 + bestSite
			if k >= 12 {
				k -= 12
			} else if k >= 6 {
				k -= 6
			}
		}
	}

	br = int(best.Row) >> 3
	bc = int(best.Col) >> 3
	for j := 0; j < 8; j++ {
		bestSite = -1
		for i, step := range neighbors {
			row := br + int(step.Row)
			col := bc + int(step.Col)
			if !bounds.containsFullPel(row, col) {
				continue
			}
			mv := vp8enc.MotionVector{Row: int16(row * interFrameMVFullPixelStep), Col: int16(col * interFrameMVFullPixelStep)}
			cost := interMotionSearchCostLimited(src, ref, mbRow, mbCol, mv, bestCost, bestRefMV, qIndex)
			if cost < bestCost {
				best = mv
				bestCost = cost
				bestSite = i
			}
		}
		if bestSite < 0 {
			break
		}
		br = int(best.Row) >> 3
		bc = int(best.Col) >> 3
	}
	return best, bestCost
}

func refineInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, search interAnalysisSearchConfig, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	switch search.fractionalSearch {
	case interAnalysisFractionalSearchStep:
		return stepInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, true, mvProbs)
	case interAnalysisFractionalSearchHalf:
		return stepInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, false, mvProbs)
	case interAnalysisFractionalSearchSkip:
		return vp8enc.MotionVector{}, 0, false
	default:
		return iterativeInterFrameSubpixelMotionVector(src, ref, mbRow, mbCol, best, bestRefMV, qIndex, mvProbs)
	}
}

func stepInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, quarter bool, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	bestRow := (int(best.Row) >> 3) * 4
	bestCol := (int(best.Col) >> 3) * 4
	bestCost, ok := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, bestRow, bestCol, bestRefMV, qIndex, mvProbs)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost, bestRow, bestCol = stepInterFrameSubpixelDirectionalSearch(src, ref, mbRow, mbCol, bestRow, bestCol, 2, bestCost, bestRefMV, qIndex, mvProbs)
	if quarter {
		bestCost, bestRow, bestCol = stepInterFrameSubpixelDirectionalSearch(src, ref, mbRow, mbCol, bestRow, bestCol, 1, bestCost, bestRefMV, qIndex, mvProbs)
	}
	finalMV := vp8enc.MotionVector{Row: int16(bestRow * 2), Col: int16(bestCol * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func stepInterFrameSubpixelDirectionalSearch(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, startRow int, startCol int, step int, bestCost int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, int, int) {
	bestRow := startRow
	bestCol := startCol
	leftCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow, startCol-step, bestRefMV, qIndex, mvProbs)
	rightCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow, startCol+step, bestRefMV, qIndex, mvProbs)
	upCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow-step, startCol, bestRefMV, qIndex, mvProbs)
	downCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, startRow+step, startCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, leftCost, startRow, startCol-step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, rightCost, startRow, startCol+step)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, upCost, startRow-step, startCol)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, downCost, startRow+step, startCol)

	diagRow := startRow - step
	if upCost >= downCost {
		diagRow = startRow + step
	}
	diagCol := startCol - step
	if leftCost >= rightCost {
		diagCol = startCol + step
	}
	diagCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, diagRow, diagCol, bestRefMV, qIndex, mvProbs)
	bestCost, bestRow, bestCol = updateSubpixelSearchBest(bestCost, bestRow, bestCol, diagCost, diagRow, diagCol)
	return bestCost, bestRow, bestCol
}

func interFrameSubpixelMotionVectorInRange(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector) bool {
	maxFullPelEighths := interFrameMaxFullPelVal << 3
	rowDelta := int(mv.Row) - int(bestRefMV.Row)
	colDelta := int(mv.Col) - int(bestRefMV.Col)
	if rowDelta < 0 {
		rowDelta = -rowDelta
	}
	if colDelta < 0 {
		colDelta = -colDelta
	}
	return rowDelta <= maxFullPelEighths && colDelta <= maxFullPelEighths
}

// iterativeInterFrameSubpixelMotionVector performs the libvpx half- then
// quarter-pel refinement (vp8_find_best_sub_pixel_step_iteratively) anchored
// to bestRefMV: candidate MVs farther from bestRefMV than MAX_FULL_PEL_VAL
// (in 1/8-pel) get rejected with INT_MAX and the cost is charged against the
// ref-MV, not (0,0).
func iterativeInterFrameSubpixelMotionVector(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, best vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (vp8enc.MotionVector, int, bool) {
	if int(best.Row)&7 != 0 || int(best.Col)&7 != 0 {
		return vp8enc.MotionVector{}, 0, false
	}
	br := (int(best.Row) >> 3) * 4
	bc := (int(best.Col) >> 3) * 4
	tr := br
	tc := bc
	bestMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	bestDist, _, ok := macroblockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, br, bc)
	if !ok {
		return vp8enc.MotionVector{}, 0, false
	}
	bestCost := bestDist + interMotionSearchErrorVectorCost(bestMV, bestRefMV, qIndex, mvProbs)

	for halfiters := 0; halfiters < 3; halfiters++ {
		leftCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc-2, bestRefMV, qIndex, mvProbs)
		rightCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc+2, bestRefMV, qIndex, mvProbs)
		upCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr-2, tc, bestRefMV, qIndex, mvProbs)
		downCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr+2, tc, bestRefMV, qIndex, mvProbs)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+2)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-2, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+2, tc)

		diagRow := tr - 2
		if upCost >= downCost {
			diagRow = tr + 2
		}
		diagCol := tc - 2
		if leftCost >= rightCost {
			diagCol = tc + 2
		}
		diagCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, diagRow, diagCol, bestRefMV, qIndex, mvProbs)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	for quarteriters := 0; quarteriters < 3; quarteriters++ {
		leftCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc-1, bestRefMV, qIndex, mvProbs)
		rightCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr, tc+1, bestRefMV, qIndex, mvProbs)
		upCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr-1, tc, bestRefMV, qIndex, mvProbs)
		downCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, tr+1, tc, bestRefMV, qIndex, mvProbs)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, leftCost, tr, tc-1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, rightCost, tr, tc+1)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, upCost, tr-1, tc)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, downCost, tr+1, tc)

		diagRow := tr - 1
		if upCost >= downCost {
			diagRow = tr + 1
		}
		diagCol := tc - 1
		if leftCost >= rightCost {
			diagCol = tc + 1
		}
		diagCost, _ := subpixelMotionSearchCandidateCost(src, ref, mbRow, mbCol, diagRow, diagCol, bestRefMV, qIndex, mvProbs)
		bestCost, br, bc = updateSubpixelSearchBest(bestCost, br, bc, diagCost, diagRow, diagCol)

		if tr == br && tc == bc {
			break
		}
		tr = br
		tc = bc
	}

	finalMV := vp8enc.MotionVector{Row: int16(br * 2), Col: int16(bc * 2)}
	if !interFrameSubpixelMotionVectorInRange(finalMV, bestRefMV) {
		return vp8enc.MotionVector{}, 0, false
	}
	return finalMV, bestCost, true
}

func subpixelMotionSearchCandidateCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, row int, col int, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) (int, bool) {
	dist, _, ok := macroblockSubpixelVarianceForQuarterMV(src, ref, mbRow, mbCol, row, col)
	if !ok {
		return maxInt(), false
	}
	mv := vp8enc.MotionVector{Row: int16(row * 2), Col: int16(col * 2)}
	return dist + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, mvProbs), true
}

func updateSubpixelSearchBest(bestCost int, bestRow int, bestCol int, candidateCost int, candidateRow int, candidateCol int) (int, int, int) {
	if candidateCost < bestCost {
		return candidateCost, candidateRow, candidateCol
	}
	return bestCost, bestRow, bestCol
}

func macroblockSubpixelVarianceForQuarterMV(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, row int, col int) (int, int, bool) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY < 0 || baseX < 0 || baseY+16 > src.Height || baseX+16 > src.Width {
		return 0, 0, false
	}
	refBaseY := baseY + (row >> 2)
	refBaseX := baseX + (col >> 2)
	xOffset := (col & 3) << 1
	yOffset := (row & 3) << 1
	return macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset)
}

func interMotionSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return macroblockSAD(src, ref, mbRow, mbCol, mv) + interMotionSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSplitBlockSearchCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return splitBlockSAD(src, ref, mbRow, mbCol, block, width, height, mv) + interMotionSplitBlockSearchVectorCost(mv, bestRefMV, qIndex)
}

func interMotionSearchCostLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int, bestRefMV vp8enc.MotionVector, qIndex int) int {
	mvCost := interMotionSearchVectorCost(mv, bestRefMV, qIndex)
	sadLimit := limit - mvCost
	if sadLimit < 0 {
		return limit + 1
	}
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, sadLimit) + mvCost
}

func interMotionFullPixelSearchReturnCost(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	variance, _ := macroblockLumaMotionVarianceSSE(src, ref, mbRow, mbCol, mv)
	return variance + interMotionSearchErrorVectorCost(mv, bestRefMV, qIndex, mvProbs)
}

// interMotionSearchVectorCost charges full-pel MV bits against bestRefMV like
// libvpx mvsad_err_cost — picking against (0,0) inflates the cost of motion
// far from a strong predictor and biases NEWMV away from correct candidates.
func interMotionSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit16(qIndex))
}

func interMotionSplitBlockSearchVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int) int {
	return vp8enc.MotionVectorSADCost(mv, bestRefMV, libvpxSADPerBit4(qIndex))
}

// interMotionSearchErrorVectorCost charges sub-pel MV bits against bestRefMV
// (libvpx find_best_sub_pixel_step_iteratively in mcomp.c).
func interMotionSearchErrorVectorCost(mv vp8enc.MotionVector, bestRefMV vp8enc.MotionVector, qIndex int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return 0
	}
	return vp8enc.MotionVectorErrorCost(mv, bestRefMV, mvProbs, libvpxErrorPerBit(qIndex))
}

func interMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	return interMotionModeVectorCostWithNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, mvProbs, libvpxRDNewMVBitCostWeight)
}

func interMotionModeVectorCostWithNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, mvProbs *[2][vp8tables.MVPCount]uint8, newMVWeight int) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame {
		return 0
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	best := vp8enc.InterFrameBestMotionVectorAt(above, left, aboveLeft, mode.RefFrame, mbRow, mbCol, mbRows, mbCols, defaultInterFrameSignBias())
	if mode.Mode == vp8common.SplitMV {
		return splitMotionModeVectorCost(mode, left, above, best, mvProbs)
	}
	if mode.Mode != vp8common.NewMV {
		return 0
	}
	return interNewMVVectorCost(mode.MV, best, mvProbs, newMVWeight)
}

func interMacroblockSkipRate(skip bool) int {
	return interMacroblockSkipRateWithProb(128, skip)
}

func interMacroblockSkipRateWithProb(prob uint8, skip bool) int {
	if prob == 0 {
		prob = 128
	}
	if skip {
		return boolBitCost(prob, 1)
	}
	return boolBitCost(prob, 0)
}

func (e *VP8Encoder) interMacroblockSkipRate(skip bool) int {
	if e == nil {
		return interMacroblockSkipRate(skip)
	}
	return interMacroblockSkipRateWithProb(e.probSkipFalse, skip)
}

// interIntraMacroblockModeRate models libvpx vp8_calc_ref_frame_costs for the
// intra-coded ref-frame branch: skip-bit + intra/inter selector with the
// previous-frame prob_intra_coded.
func (e *VP8Encoder) interIntraMacroblockModeRate() int {
	return e.interMacroblockSkipRate(false) + boolBitCost(e.refProbIntra, 0)
}

func (e *VP8Encoder) interMotionModeRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	return e.interMotionModeRateWithReferenceRate(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, e.interReferenceFrameRate(mode.RefFrame))
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxRDNewMVBitCostWeight)
}

func (e *VP8Encoder) fastInterMotionModeRateWithReferenceRate(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int) int {
	return e.interMotionModeRateWithReferenceRateAndNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, refRate, libvpxFastNewMVBitCostWeight)
}

func (e *VP8Encoder) interMotionModeRateWithReferenceRateAndNewMVWeight(mode *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, aboveLeft *vp8enc.InterFrameMacroblockMode, mbRow int, mbCol int, mbRows int, mbCols int, refRate int, newMVWeight int) int {
	if mode == nil {
		return 1 << 30
	}
	if mode.RefFrame == vp8common.IntraFrame {
		return boolBitCost(e.refProbIntra, 0)
	}
	return boolBitCost(e.refProbIntra, 1) +
		refRate +
		interPredictionModeRate(mode.Mode, vp8enc.InterFrameModeCounts(above, left, aboveLeft, mode.RefFrame, defaultInterFrameSignBias())) +
		interMotionModeVectorCostWithNewMVWeight(mode, above, left, aboveLeft, mbRow, mbCol, mbRows, mbCols, &e.modeProbs.MV, newMVWeight)
}

// interReferenceFrameRate ports libvpx vp8_calc_ref_frame_costs (bitstream.c):
// the LAST/GOLDEN/ALTREF tree uses the previous-frame prob_last_coded and
// prob_gf_coded, NOT a per-frame static 128.
func (e *VP8Encoder) interReferenceFrameRate(refFrame vp8common.MVReferenceFrame) int {
	return interReferenceFrameRateWithProbs(refFrame, e.refProbLast, e.refProbGolden)
}

func (e *VP8Encoder) interReferenceFrameRateForReference(ref interAnalysisReference) int {
	if ref.RefRateSet {
		return ref.RefRate
	}
	return e.interReferenceFrameRate(ref.Frame)
}

func (e *VP8Encoder) interReferenceFrameRatesForFlags(flags EncodeFlags) (last int, golden int, alt int) {
	probLast := e.refProbLast
	probGolden := e.refProbGolden
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	temporalSingleRef := e.interReferenceFrameRatesUseTemporalSingleRefSpecialCase()
	switch {
	case lastEnabled && !goldenEnabled && !altEnabled:
		probLast = 255
		probGolden = 128
	case temporalSingleRef && !lastEnabled && goldenEnabled && !altEnabled:
		probLast = 1
		probGolden = 255
	case temporalSingleRef && !lastEnabled && !goldenEnabled && altEnabled:
		probLast = 1
		probGolden = 1
	}
	return interReferenceFrameRateWithProbs(vp8common.LastFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.GoldenFrame, probLast, probGolden),
		interReferenceFrameRateWithProbs(vp8common.AltRefFrame, probLast, probGolden)
}

func (e *VP8Encoder) interReferenceFrameRatesUseTemporalSingleRefSpecialCase() bool {
	if e == nil || !e.opts.TemporalScalability.Enabled {
		return false
	}
	pattern, ok := temporalLayeringPattern(e.opts.TemporalScalability.Mode)
	return ok && pattern.Layers > 1
}

func interReferenceFrameRateWithProbs(refFrame vp8common.MVReferenceFrame, probLast uint8, probGolden uint8) int {
	switch refFrame {
	case vp8common.LastFrame:
		return boolBitCost(probLast, 0)
	case vp8common.GoldenFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 0)
	case vp8common.AltRefFrame:
		return boolBitCost(probLast, 1) + boolBitCost(probGolden, 1)
	default:
		return 1 << 30
	}
}

func interPredictionModeRate(mode vp8common.MBPredictionMode, counts vp8enc.InterModeCounts) int {
	probs := vp8tables.InterModeContexts
	switch mode {
	case vp8common.ZeroMV:
		return boolBitCost(probs[counts.Intra][0], 0)
	case vp8common.NearestMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 0)
	case vp8common.NearMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 0)
	case vp8common.NewMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 0)
	case vp8common.SplitMV:
		return boolBitCost(probs[counts.Intra][0], 1) +
			boolBitCost(probs[counts.Nearest][1], 1) +
			boolBitCost(probs[counts.Near][2], 1) +
			boolBitCost(probs[counts.Split][3], 1)
	default:
		return 1 << 30
	}
}

func splitMotionModeVectorCost(mode *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mode.Partition >= vp8tables.NumMBSplits {
		return 1 << 30
	}
	if mvProbs == nil {
		return maxInt() / 4
	}
	cost := mbSplitPartitionRate(mode.Partition)
	partitions := int(vp8tables.MBSplitCount[mode.Partition])
	for subset := 0; subset < partitions; subset++ {
		block := int(vp8tables.MBSplitOffset[mode.Partition][subset])
		leftMV := analysisSplitLeftMV(mode, left, block)
		aboveMV := analysisSplitAboveMV(mode, above, block)
		target := mode.BlockMV[block]
		probs := analysisSubMVRefProbs(leftMV, aboveMV)
		if target == leftMV {
			cost += analysisBoolBitCost(probs[0], 0)
			continue
		}
		cost += analysisBoolBitCost(probs[0], 1)
		if target == aboveMV {
			cost += analysisBoolBitCost(probs[1], 0)
			continue
		}
		cost += analysisBoolBitCost(probs[1], 1)
		if target.IsZero() {
			cost += analysisBoolBitCost(probs[2], 0)
			continue
		}
		cost += analysisBoolBitCost(probs[2], 1)
		delta := vp8enc.MotionVector{Row: int16(int(target.Row) - int(best.Row)), Col: int16(int(target.Col) - int(best.Col))}
		cost += splitMotionVectorCost(delta, mvProbs)
	}
	return cost
}

func mbSplitPartitionRate(partition uint8) int {
	if partition >= vp8tables.NumMBSplits {
		return maxInt() / 4
	}
	return treeTokenCost(vp8tables.MBSplitTree[:], vp8tables.MBSplitProbs[:], int(partition))
}

func analysisSplitLeftMV(cur *vp8enc.InterFrameMacroblockMode, left *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block&3 == 0 {
		if left == nil {
			return vp8enc.MotionVector{}
		}
		if left.Mode == vp8common.SplitMV {
			return left.BlockMV[block+3]
		}
		return left.MV
	}
	return cur.BlockMV[block-1]
}

func analysisSplitAboveMV(cur *vp8enc.InterFrameMacroblockMode, above *vp8enc.InterFrameMacroblockMode, block int) vp8enc.MotionVector {
	if block>>2 == 0 {
		if above == nil {
			return vp8enc.MotionVector{}
		}
		if above.Mode == vp8common.SplitMV {
			return above.BlockMV[block+12]
		}
		return above.MV
	}
	return cur.BlockMV[block-4]
}

func analysisSubMVRefProbs(left vp8enc.MotionVector, above vp8enc.MotionVector) [3]uint8 {
	lez := 0
	if left.IsZero() {
		lez = 1
	}
	aez := 0
	if above.IsZero() {
		aez = 1
	}
	lea := 0
	if left == above {
		lea = 1
	}
	return vp8tables.SubMVRefProb3[(aez<<2)|(lez<<1)|lea]
}

func analysisBoolBitCost(prob uint8, bit int) int {
	if bit == 0 {
		return vp8tables.ProbCost[prob]
	}
	return vp8tables.ProbCost[255-int(prob)]
}

func interMotionVectorCost(mv vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, mvProbs, libvpxFastNewMVBitCostWeight)
}

func interNewMVVectorCost(mv vp8enc.MotionVector, best vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8, weight int) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, best, mvProbs, weight)
}

func splitMotionVectorCost(mv vp8enc.MotionVector, mvProbs *[2][vp8tables.MVPCount]uint8) int {
	if mvProbs == nil {
		return maxInt() / 4
	}
	return vp8enc.MotionVectorBitCost(mv, vp8enc.MotionVector{}, mvProbs, 102)
}

func macroblockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	return macroblockSADLimited(src, ref, mbRow, mbCol, mv, maxInt())
}

func macroblockLumaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if sse, ok := macroblockSubpixelSSE(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride)
	}

	sse := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sse += diff * diff
		}
	}
	return sse
}

func macroblockLumaMotionVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if variance, sse, ok := macroblockSubpixelVariance(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset); ok {
				return variance, sse
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		srcStart := src.Y[baseY*src.YStride+baseX:]
		refStart := ref.Y[refBaseY*ref.YStride+refBaseX:]
		return dsp.Variance16x16(srcStart, src.YStride, refStart, ref.YStride),
			dsp.SSE16x16(srcStart, src.YStride, refStart, ref.YStride)
	}

	sum := 0
	sse := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

func macroblockSADLimited(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, mv vp8enc.MotionVector, limit int) int {
	baseY := mbRow * 16
	baseX := mbCol * 16
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	xOffset := int(mv.Col) & 7
	yOffset := int(mv.Row) & 7
	if xOffset|yOffset != 0 {
		if baseY >= 0 && baseX >= 0 &&
			baseY+16 <= src.Height && baseX+16 <= src.Width {
			if sad, ok := macroblockSubpixelSAD(src, ref, baseY, baseX, refBaseY, refBaseX, xOffset, yOffset, limit); ok {
				return sad
			}
		}
	}
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= ref.CodedHeight && refBaseX+16 <= ref.CodedWidth {
		return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, ref.Y[refBaseY*ref.YStride+refBaseX:], ref.YStride, limit)
	}

	sad := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
		if sad > limit {
			return sad
		}
	}
	return sad
}

func splitBlockSAD(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int, block int, width int, height int, mv vp8enc.MotionVector) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	mvY := int(mv.Row >> 3)
	mvX := int(mv.Col >> 3)
	refBaseY := baseY + mvY
	refBaseX := baseX + mvX
	if baseY >= 0 && baseX >= 0 &&
		baseY+height <= src.Height && baseX+width <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+height <= ref.CodedHeight && refBaseX+width <= ref.CodedWidth {
		srcBlock := src.Y[baseY*src.YStride+baseX:]
		refBlock := ref.Y[refBaseY*ref.YStride+refBaseX:]
		switch {
		case width == 16 && height == 8:
			return dsp.SAD16x8(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 8 && height == 16:
			return dsp.SAD8x16(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 8 && height == 8:
			return dsp.SAD8x8(srcBlock, src.YStride, refBlock, ref.YStride)
		case width == 4 && height == 4:
			return dsp.SAD4x4(srcBlock, src.YStride, refBlock, ref.YStride)
		}
	}

	sad := 0
	for row := 0; row < height; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, ref.CodedHeight)
		for col := 0; col < width; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func macroblockSubpixelSSE(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SSE16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16), true
}

func macroblockSubpixelSAD(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int, limit int) (int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YBorder < 2 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, false
	}
	if refBaseY < -ref.YBorder+2 || refBaseX < -ref.YBorder+2 ||
		refBaseY+16+3 > ref.CodedHeight+ref.YBorder ||
		refBaseX+16+3 > ref.CodedWidth+ref.YBorder {
		return 0, false
	}
	start := ref.YOrigin + (refBaseY-2)*ref.YStride + refBaseX - 2
	if start < 0 || start+20*ref.YStride+21 > len(ref.YFull) {
		return 0, false
	}
	var pred [16 * 16]byte
	dsp.SixTapPredict16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, pred[:], 16)
	return dsp.SAD16x16Limit(src.Y[baseY*src.YStride+baseX:], src.YStride, pred[:], 16, limit), true
}

func macroblockSubpixelVariance(src vp8enc.SourceImage, ref *vp8common.Image, baseY int, baseX int, refBaseY int, refBaseX int, xOffset int, yOffset int) (int, int, bool) {
	if ref == nil || len(ref.YFull) == 0 || ref.YOrigin < 0 || ref.YStride < ref.CodedWidth+2*ref.YBorder {
		return 0, 0, false
	}
	if refBaseY < -ref.YBorder || refBaseX < -ref.YBorder ||
		refBaseY+17 > ref.CodedHeight+ref.YBorder ||
		refBaseX+17 > ref.CodedWidth+ref.YBorder {
		return 0, 0, false
	}
	start := ref.YOrigin + refBaseY*ref.YStride + refBaseX
	if start < 0 || start+16*ref.YStride+17 > len(ref.YFull) {
		return 0, 0, false
	}
	variance, sse := dsp.SubpelVariance16x16(ref.YFull[start:], ref.YStride, xOffset, yOffset, src.Y[baseY*src.YStride+baseX:], src.YStride)
	return variance, sse, true
}

func macroblockChromaSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) int {
	baseY := mbRow * 8
	baseX := mbCol * 8
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	refUVWidth := (ref.CodedWidth + 1) >> 1
	refUVHeight := (ref.CodedHeight + 1) >> 1
	if baseY >= 0 && baseX >= 0 &&
		baseY+8 <= uvHeight && baseX+8 <= uvWidth &&
		baseY+8 <= refUVHeight && baseX+8 <= refUVWidth {
		srcOffset := baseY*src.UStride + baseX
		refOffset := baseY*ref.UStride + baseX
		return dsp.SSE8x8(src.U[srcOffset:], src.UStride, ref.U[refOffset:], ref.UStride) +
			dsp.SSE8x8(src.V[baseY*src.VStride+baseX:], src.VStride, ref.V[baseY*ref.VStride+baseX:], ref.VStride)
	}

	sse := 0
	for row := 0; row < 8; row++ {
		srcY := clampEncodeCoord(baseY+row, uvHeight)
		refY := clampEncodeCoord(baseY+row, refUVHeight)
		for col := 0; col < 8; col++ {
			srcX := clampEncodeCoord(baseX+col, uvWidth)
			refX := clampEncodeCoord(baseX+col, refUVWidth)
			uDiff := int(src.U[srcY*src.UStride+srcX]) - int(ref.U[refY*ref.UStride+refX])
			vDiff := int(src.V[srcY*src.VStride+srcX]) - int(ref.V[refY*ref.VStride+refX])
			sse += uDiff*uDiff + vDiff*vDiff
		}
	}
	return sse
}

func macroblockLumaVarianceSSE(src vp8enc.SourceImage, ref *vp8common.Image, mbRow int, mbCol int) (int, int) {
	baseY := mbRow * 16
	baseX := mbCol * 16
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		baseY+16 <= ref.CodedHeight && baseX+16 <= ref.CodedWidth {
		srcStart := src.Y[baseY*src.YStride+baseX:]
		refStart := ref.Y[baseY*ref.YStride+baseX:]
		return dsp.Variance16x16(srcStart, src.YStride, refStart, ref.YStride),
			dsp.SSE16x16(srcStart, src.YStride, refStart, ref.YStride)
	}

	sum := 0
	sse := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(baseY+row, ref.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(baseX+col, ref.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(ref.Y[refY*ref.YStride+refX])
			sum += diff
			sse += diff * diff
		}
	}
	return sse - ((sum * sum) >> 8), sse
}

type predictedMacroblockRDStats struct {
	rateY        int
	rateUV       int
	distortionY  int
	distortionUV int
	tteob        int
}

func buildPredictedMacroblockCoefficients(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, is4x4 bool, intra bool, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients) {
	_ = buildPredictedMacroblockCoefficientsRD(coefProbs, src, mbRow, mbCol, pred, aboveTok, leftTok, quant, qIndex, zbinOverQuant, zbinModeBoost, is4x4, intra, fastQuant, optimize, coeffs)
}

func buildPredictedMacroblockCoefficientsRD(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, pred *vp8common.Image, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, is4x4 bool, intra bool, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients) predictedMacroblockRDStats {
	var stats predictedMacroblockRDStats
	if coefProbs == nil || pred == nil || quant == nil || coeffs == nil {
		return stats
	}
	var y2Input [16]int16
	var y2Coeff [16]int16
	var dq [16]int16
	var input [16]int16
	var dct [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}

	for block := 0; block < 16; block++ {
		x := mbCol*16 + (block&3)*4
		y := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, pred.Y, pred.YStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		if is4x4 {
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob := quantizeEncodedBlock(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
			coeffs.SetBlockEOB(block, eob)
			stats.rateY += coefficientBlockTokenRate(coefProbs, 3, ctx, 0, &coeffs.QCoeff[block], eob)
			stats.distortionY += transformBlockError(&dct, &dq)
			if eob > 0 {
				stats.tteob++
			}
			hasCoeffs := uint8(0)
			if eob > 0 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
		} else {
			y2Input[block] = dct[0]
			dct[0] = 0
			a := block & 3
			l := (block & 0x0c) >> 2
			ctx := int(yAbove[a] + yLeft[l])
			eob := quantizeEncodedBlock(coefProbs, qIndex, 0, ctx, 1, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, &dct, &quant.Y1DC, &coeffs.QCoeff[block], &dq)
			coeffs.SetBlockEOB(block, eob)
			stats.rateY += coefficientBlockTokenRate(coefProbs, 0, ctx, 1, &coeffs.QCoeff[block], eob)
			stats.distortionY += transformBlockError(&dct, &dq)
			if eob > 1 {
				stats.tteob++
			}
			hasCoeffs := uint8(0)
			if eob > 1 {
				hasCoeffs = 1
			}
			yAbove[a] = hasCoeffs
			yLeft[l] = hasCoeffs
		}
	}
	if !is4x4 {
		vp8enc.ForwardWalsh4x4(y2Input[:], 4, &y2Coeff)
		eob := quantizeEncodedBlock(coefProbs, qIndex, 1, int(y2Above+y2Left), 0, zbinOverQuant/2, zbinModeBoost, intra, fastQuant, optimize, &y2Coeff, &quant.Y2, &coeffs.QCoeff[24], &dq)
		coeffs.SetBlockEOB(24, eob)
		stats.rateY += coefficientBlockTokenRate(coefProbs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
		y2Error := transformBlockError(&y2Coeff, &dq)
		stats.distortionY = ((stats.distortionY << 2) + y2Error) >> 4
		stats.tteob += eob
	} else {
		coeffs.SetBlockEOB(24, 0)
		stats.distortionY >>= 2
	}

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		y := mbRow*8 + (block>>1)*4
		fillPredictedResidual4x4(src.U, src.UStride, uvWidth, uvHeight, pred.U, pred.UStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[16+block], eob)
		stats.distortionUV += transformBlockError(&dct, &dq)
		stats.tteob += eob
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, pred.V, pred.VStride, x, y, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, zbinModeBoost, intra, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		stats.rateUV += coefficientBlockTokenRate(coefProbs, 2, ctx, 0, &coeffs.QCoeff[20+block], eob)
		stats.distortionUV += transformBlockError(&dct, &dq)
		stats.tteob += eob
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	stats.distortionUV >>= 2
	return stats
}

func macroblockCoefficientsEmpty(coeffs *vp8enc.MacroblockCoefficients, is4x4 bool) bool {
	if coeffs.EOB[24] != 0 {
		return false
	}
	for i := 0; i < 16; i++ {
		if (is4x4 && coeffs.EOB[i] != 0) || (!is4x4 && coeffs.EOB[i] > 1) {
			return false
		}
	}
	for i := 16; i < 24; i++ {
		if coeffs.EOB[i] != 0 {
			return false
		}
	}
	return true
}

func clearMacroblockCoefficients(coeffs *vp8enc.MacroblockCoefficients) {
	*coeffs = vp8enc.MacroblockCoefficients{}
}

func staticInterEncodeBreakout(src vp8enc.SourceImage, pred *vp8common.Image, mbRow int, mbCol int, quant *vp8enc.MacroblockQuant, encodeBreakout int) bool {
	if encodeBreakout <= 0 || pred == nil || quant == nil {
		return false
	}
	yAC := int(quant.Y1.Dequant[1])
	threshold := (yAC * yAC) >> 4
	if threshold < encodeBreakout {
		threshold = encodeBreakout
	}
	lumaVar, lumaSSE := macroblockLumaVarianceSSE(src, pred, mbRow, mbCol)
	if lumaSSE >= threshold {
		return false
	}
	y2DC := int(quant.Y2.Dequant[0])
	dcError := lumaSSE - lumaVar
	if dcError >= (y2DC*y2DC)>>4 && (lumaSSE/2 <= lumaVar || dcError >= 64) {
		return false
	}
	return macroblockChromaSSE(src, pred, mbRow, mbCol)*2 < threshold
}

const (
	lastFrameZeroMVZbinBoost  = 6
	goldenAltZeroMVZbinBoost  = 12
	nonZeroInterModeZbinBoost = 4
	splitInterModeZbinBoost   = 0
	intraInterFrameZbinBoost  = 0
)

func interZbinModeBoost(mode *vp8enc.InterFrameMacroblockMode) int {
	if mode == nil || mode.RefFrame == vp8common.IntraFrame || mode.Mode >= vp8common.DCPred && mode.Mode <= vp8common.BPred {
		return intraInterFrameZbinBoost
	}
	switch mode.Mode {
	case vp8common.ZeroMV:
		if mode.RefFrame == vp8common.LastFrame {
			return lastFrameZeroMVZbinBoost
		}
		return goldenAltZeroMVZbinBoost
	case vp8common.SplitMV:
		return splitInterModeZbinBoost
	default:
		return nonZeroInterModeZbinBoost
	}
}

func quantizeBlockWithZbin(coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if coeff == nil || quant == nil || qcoeff == nil || dqcoeff == nil {
		return 0
	}
	eob := -1
	zeroRun := 0
	for pos := 0; pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		z := int(coeff[rc])
		if z == 0 {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x := z
		if x < 0 {
			x = -x
		}
		zbin := int(quant.Zbin[rc])
		zbin += int(quant.ZbinBoost[zeroRun])
		zbin += (int(quant.Dequant[1]) * (zbinOverQuant + zbinModeBoost)) >> 7
		if x < zbin {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
			if zeroRun < len(quant.ZbinBoost)-1 {
				zeroRun++
			}
			continue
		}

		x += int(quant.Round[rc])
		y := ((((x * int(quant.Quant[rc])) >> 16) + x) * int(quant.QuantShift[rc])) >> 16
		if z < 0 {
			y = -y
		}
		q := int16(y)
		qcoeff[rc] = q
		dqcoeff[rc] = q * quant.Dequant[rc]
		if y != 0 {
			eob = pos
			zeroRun = 0
		} else if zeroRun < len(quant.ZbinBoost)-1 {
			zeroRun++
		}
	}
	return eob + 1
}

func quantizeOptimizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	eob := quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
	eob = optimizeQuantizedBlock(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, intra, coeff, quant, qcoeff, eob)
	dequantizeQuantizedBlock(quant, qcoeff, dqcoeff)
	return eob
}

func quantizeEncodedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, zbinModeBoost int, intra bool, fastQuant bool, optimize bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	if optimize {
		eob := quantizeOptimizedBlock(coefProbs, qIndex, blockType, ctx, skipDC, zbinOverQuant, zbinModeBoost, intra, coeff, quant, qcoeff, dqcoeff)
		if blockType == 1 && skipDC == 0 {
			eob = resetLibvpxSmallSecondOrderCoefficients(quant, qcoeff, dqcoeff, eob)
		}
		return eob
	}
	return quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func quantizeDecisionBlock(fastQuant bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qIndex int, zbinOverQuant int, zbinModeBoost int, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	if fastQuant {
		return vp8enc.FastQuantizeBlock(coeff, quant, qcoeff, dqcoeff)
	}
	return quantizeBlockWithZbin(coeff, quant, qIndex, zbinOverQuant, zbinModeBoost, qcoeff, dqcoeff)
}

func dequantizeQuantizedBlock(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) {
	if quant == nil || qcoeff == nil || dqcoeff == nil {
		return
	}
	for i := 0; i < 16; i++ {
		dqcoeff[i] = qcoeff[i] * quant.Dequant[i]
	}
}

// optimizeQuantizedBlock ports libvpx v1.16.0 vp8/encoder/encodemb.c optimize_b.
// It walks the quantized block from eob-1 down to skipDC, builds a 2-state
// Viterbi trellis exploring (keep current value) vs (shift |x| toward 0 when
// the dequant boundary allows), and applies the path that minimizes the libvpx
// RDCOST. Tied RDCOSTs use the libvpx RDTRUNC tie-break.
func optimizeQuantizedBlock(coefProbs *vp8tables.CoefficientProbs, qIndex int, blockType int, ctx int, skipDC int, zbinOverQuant int, intra bool, coeff *[16]int16, quant *vp8enc.BlockQuant, qcoeff *[16]int16, eob int) int {
	if coeff == nil || quant == nil || qcoeff == nil || eob <= skipDC {
		return eob
	}
	if blockType < 0 || blockType >= vp8tables.BlockTypes || ctx < 0 || ctx >= vp8tables.PrevCoefContexts || skipDC < 0 || skipDC > 1 {
		return eob
	}
	if coefProbs == nil {
		return eob
	}
	if eob > 16 {
		eob = 16
	}

	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= blockPlaneRDMultiplier(blockType)
	if intra {
		rdMult = (rdMult * 9) >> 4
	}

	type tokenState struct {
		rate  int
		error int
		next  int8
		token int8
		qc    int16
	}
	var tokens [17][2]tokenState
	var bestMask [2]uint32

	tokens[eob][0] = tokenState{next: 16, token: int8(vp8tables.DCTEOBToken)}
	tokens[eob][1] = tokens[eob][0]
	next := eob

	for i := eob - 1; i >= skipDC; i-- {
		rc := int(vp8tables.DefaultZigZag1D[i])
		x := int(qcoeff[rc])
		if x != 0 {
			error0 := tokens[next][0].error
			error1 := tokens[next][1].error
			rate0 := tokens[next][0].rate
			rate1 := tokens[next][1].rate
			t0 := dctValueToken(x)

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				pt := int(vp8tables.PrevTokenClass[t0])
				p := (*coefProbs)[blockType][band][pt]
				rate0 += treeTokenCost(vp8tables.CoefTree[:], p[:], int(tokens[next][0].token))
				rate1 += treeTokenCost(vp8tables.CoefTree[:], p[:], int(tokens[next][1].token))
			}

			rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best := 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits := dctValueBaseCost(x)
			dq := int(quant.Dequant[rc])
			dx := x*dq - int(coeff[rc])
			d2 := dx * dx

			if best == 1 {
				tokens[i][0].rate = baseBits + rate1
				tokens[i][0].error = d2 + error1
			} else {
				tokens[i][0].rate = baseBits + rate0
				tokens[i][0].error = d2 + error0
			}
			tokens[i][0].next = int8(next)
			tokens[i][0].token = int8(t0)
			tokens[i][0].qc = int16(x)
			bestMask[0] |= uint32(best) << uint(i)

			rate0 = tokens[next][0].rate
			rate1 = tokens[next][1].rate

			absX := x
			if absX < 0 {
				absX = -absX
			}
			absC := int(coeff[rc])
			if absC < 0 {
				absC = -absC
			}
			shortcut := absX*dq > absC && absX*dq < absC+dq
			xs := x
			sz := 0
			if shortcut {
				if x < 0 {
					sz = -1
				}
				xs -= 2*sz + 1
			}

			var t1 int
			if xs == 0 {
				if int(tokens[next][0].token) == vp8tables.DCTEOBToken {
					t0 = vp8tables.DCTEOBToken
				} else {
					t0 = vp8tables.ZeroToken
				}
				if int(tokens[next][1].token) == vp8tables.DCTEOBToken {
					t1 = vp8tables.DCTEOBToken
				} else {
					t1 = vp8tables.ZeroToken
				}
			} else {
				t0 = dctValueToken(xs)
				t1 = t0
			}

			if next < 16 {
				band := int(vp8tables.CoefBandsTable[i+1])
				if t0 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t0])
					p := (*coefProbs)[blockType][band][pt]
					rate0 += treeTokenCost(vp8tables.CoefTree[:], p[:], int(tokens[next][0].token))
				}
				if t1 != vp8tables.DCTEOBToken {
					pt := int(vp8tables.PrevTokenClass[t1])
					p := (*coefProbs)[blockType][band][pt]
					rate1 += treeTokenCost(vp8tables.CoefTree[:], p[:], int(tokens[next][1].token))
				}
			}

			rdCost0 = libvpxRDCost(rdMult, rdDiv, rate0, error0)
			rdCost1 = libvpxRDCost(rdMult, rdDiv, rate1, error1)
			if rdCost0 == rdCost1 {
				rdCost0 = libvpxRDTrunc(rdMult, rate0)
				rdCost1 = libvpxRDTrunc(rdMult, rate1)
			}
			best = 0
			if rdCost1 < rdCost0 {
				best = 1
			}

			baseBits = dctValueBaseCost(xs)

			d2s := d2
			if shortcut {
				dxs := dx - ((dq + sz) ^ sz)
				d2s = dxs * dxs
			}

			if best == 1 {
				tokens[i][1].rate = baseBits + rate1
				tokens[i][1].error = d2s + error1
				tokens[i][1].token = int8(t1)
			} else {
				tokens[i][1].rate = baseBits + rate0
				tokens[i][1].error = d2s + error0
				tokens[i][1].token = int8(t0)
			}
			tokens[i][1].next = int8(next)
			tokens[i][1].qc = int16(xs)
			bestMask[1] |= uint32(best) << uint(i)
			next = i
		} else {
			band := int(vp8tables.CoefBandsTable[i+1])
			p := (*coefProbs)[blockType][band][0]
			t0Tok := int(tokens[next][0].token)
			t1Tok := int(tokens[next][1].token)
			if t0Tok != vp8tables.DCTEOBToken {
				tokens[next][0].rate += treeTokenCost(vp8tables.CoefTree[:], p[:], t0Tok)
				tokens[next][0].token = int8(vp8tables.ZeroToken)
			}
			if t1Tok != vp8tables.DCTEOBToken {
				tokens[next][1].rate += treeTokenCost(vp8tables.CoefTree[:], p[:], t1Tok)
				tokens[next][1].token = int8(vp8tables.ZeroToken)
			}
		}
	}

	band := int(vp8tables.CoefBandsTable[skipDC])
	rate0 := tokens[next][0].rate
	rate1 := tokens[next][1].rate
	error0 := tokens[next][0].error
	error1 := tokens[next][1].error
	p := (*coefProbs)[blockType][band][ctx]
	rate0 += treeTokenCost(vp8tables.CoefTree[:], p[:], int(tokens[next][0].token))
	rate1 += treeTokenCost(vp8tables.CoefTree[:], p[:], int(tokens[next][1].token))
	rdCost0 := libvpxRDCost(rdMult, rdDiv, rate0, error0)
	rdCost1 := libvpxRDCost(rdMult, rdDiv, rate1, error1)
	if rdCost0 == rdCost1 {
		rdCost0 = libvpxRDTrunc(rdMult, rate0)
		rdCost1 = libvpxRDTrunc(rdMult, rate1)
	}
	best := 0
	if rdCost1 < rdCost0 {
		best = 1
	}

	finalEOB := skipDC - 1
	for i := next; i < eob; {
		x := tokens[i][best].qc
		if x != 0 {
			finalEOB = i
		}
		rc := int(vp8tables.DefaultZigZag1D[i])
		qcoeff[rc] = x
		nextI := int(tokens[i][best].next)
		best = int((bestMask[best] >> uint(i)) & 1)
		i = nextI
	}
	return finalEOB + 1
}

// libvpxRDTrunc mirrors the encodemb.c RDTRUNC macro used to break ties when
// two trellis paths have equal RDCOST.
func libvpxRDTrunc(rdMult int, rate int) int {
	return (128 + rate*rdMult) & 0xFF
}

// dctValueToken returns the libvpx coefficient-token classification for value x
// (mirrors the dct_value_tokens table indexed by signed value).
func dctValueToken(x int) int {
	abs := x
	if abs < 0 {
		abs = -abs
	}
	if abs == 0 {
		return vp8tables.ZeroToken
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return vp8tables.ZeroToken
	}
	return token
}

// dctValueBaseCost mirrors libvpx's dct_value_cost table: extra bits cost plus
// sign bit cost for value x. The token-tree cost is added separately by the
// trellis using band/context-specific token costs.
func dctValueBaseCost(x int) int {
	if x == 0 {
		return 0
	}
	abs := x
	if abs < 0 {
		abs = -abs
	}
	token, _, ok := coefficientTokenMagnitude(abs)
	if !ok {
		return maxInt() / 4
	}
	cost := 0
	if x < 0 {
		cost += boolBitCost(128, 1)
	} else {
		cost += boolBitCost(128, 0)
	}
	cost += coefficientExtraBitsRate(token, abs)
	return cost
}

// Ported from libvpx v1.16.0 vp8/encoder/encodemb.c
// check_reset_2nd_coeffs. Very small Y2 residuals inverse-transform to a zero
// pixel delta, so libvpx drops the whole second-order block after optimization.
func resetLibvpxSmallSecondOrderCoefficients(quant *vp8enc.BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16, eob int) int {
	if quant == nil || qcoeff == nil || eob <= 0 {
		return eob
	}
	if quant.Dequant[0] >= 35 && quant.Dequant[1] >= 35 {
		return eob
	}
	sum := 0
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		coef := int(qcoeff[rc]) * int(quant.Dequant[rc])
		if coef < 0 {
			coef = -coef
		}
		sum += coef
		if sum >= 35 {
			return eob
		}
	}
	for pos := 0; pos < eob && pos < 16; pos++ {
		rc := int(vp8tables.DefaultZigZag1D[pos])
		qcoeff[rc] = 0
		if dqcoeff != nil {
			dqcoeff[rc] = 0
		}
	}
	return 0
}

func rdBlockScore(qIndex int, planeMultiplier int, intra bool, rate int, distortion int) int {
	return rdBlockScoreWithZbin(qIndex, 0, planeMultiplier, intra, rate, distortion)
}

func rdBlockScoreWithZbin(qIndex int, zbinOverQuant int, planeMultiplier int, intra bool, rate int, distortion int) int {
	if planeMultiplier <= 0 {
		planeMultiplier = 1
	}
	rdMult, rdDiv := libvpxRDConstantsWithZbin(qIndex, zbinOverQuant)
	rdMult *= planeMultiplier
	if intra {
		rdMult = (rdMult * 9) >> 4
	}
	return libvpxRDCost(rdMult, rdDiv, rate, distortion)
}

func blockPlaneRDMultiplier(blockType int) int {
	switch blockType {
	case 1:
		return 16
	case 2:
		return 2
	default:
		return 4
	}
}

func macroblockCoefficientTokenRate(probs *vp8tables.CoefficientProbs, is4x4 bool, coeffs *vp8enc.MacroblockCoefficients) int {
	return macroblockCoefficientTokenRateWithContext(probs, is4x4, nil, nil, coeffs)
}

func macroblockCoefficientTokenRateWithContext(probs *vp8tables.CoefficientProbs, is4x4 bool, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, coeffs *vp8enc.MacroblockCoefficients) int {
	if probs == nil || coeffs == nil {
		return maxInt() / 4
	}

	rate := 0
	blockType := 0
	skipDC := 0
	var yAbove [4]uint8
	var yLeft [4]uint8
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	var y2Above, y2Left uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
		uvAbove = tokenUVContextArray(aboveTok)
		y2Above = aboveTok.Y2
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
		uvLeft = tokenUVContextArray(leftTok)
		y2Left = leftTok.Y2
	}
	if !is4x4 {
		eob := coeffs.BlockEOB(24, 0)
		rate += coefficientBlockTokenRate(probs, 1, int(y2Above+y2Left), 0, &coeffs.QCoeff[24], eob)
		blockType = 0
		skipDC = 1
	} else {
		blockType = 3
	}

	for block := 0; block < 16; block++ {
		eob := coeffs.BlockEOB(block, skipDC)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		rate += coefficientBlockTokenRate(probs, blockType, ctx, skipDC, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > skipDC {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
	}

	for block := 16; block < 24; block++ {
		eob := coeffs.BlockEOB(block, 0)
		a, l := macroblockCoefficientUVContextIndex(block)
		ctx := int(uvAbove[a] + uvLeft[l])
		rate += coefficientBlockTokenRate(probs, 2, ctx, 0, &coeffs.QCoeff[block], eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
	}
	return rate
}

func tokenUVContextArray(ctx *vp8enc.TokenContextPlanes) [4]uint8 {
	if ctx == nil {
		return [4]uint8{}
	}
	return [4]uint8{ctx.U[0], ctx.U[1], ctx.V[0], ctx.V[1]}
}

func macroblockCoefficientUVContextIndex(block int) (int, int) {
	base := 0
	if block > 19 {
		base = 2
	}
	a := base + (block & 1)
	l := base
	if block&3 > 1 {
		l++
	}
	return a, l
}

func macroblockImageSSE(src vp8enc.SourceImage, img *vp8common.Image, mbRow int, mbCol int) int {
	return macroblockLumaSSE(src, img, mbRow, mbCol, vp8enc.MotionVector{}) +
		macroblockChromaSSE(src, img, mbRow, mbCol)
}

func macroblockImageBlockSAD(src vp8enc.SourceImage, img *vp8common.Image, srcMbRow int, srcMbCol int, refMbRow int, refMbCol int) int {
	if img == nil {
		return maxInt()
	}
	baseY := srcMbRow * 16
	baseX := srcMbCol * 16
	refBaseY := refMbRow * 16
	refBaseX := refMbCol * 16
	if baseY >= 0 && baseX >= 0 &&
		baseY+16 <= src.Height && baseX+16 <= src.Width &&
		refBaseY >= 0 && refBaseX >= 0 &&
		refBaseY+16 <= img.CodedHeight && refBaseX+16 <= img.CodedWidth {
		return dsp.SAD16x16(src.Y[baseY*src.YStride+baseX:], src.YStride, img.Y[refBaseY*img.YStride+refBaseX:], img.YStride)
	}

	sad := 0
	for row := 0; row < 16; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		refY := clampEncodeCoord(refBaseY+row, img.CodedHeight)
		for col := 0; col < 16; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			refX := clampEncodeCoord(refBaseX+col, img.CodedWidth)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(img.Y[refY*img.YStride+refX])
			if diff < 0 {
				diff = -diff
			}
			sad += diff
		}
	}
	return sad
}

func predictAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	yOK := false
	if mode.Is4x4 || mode.Mode == vp8common.BPred {
		yOK = vp8dec.PredictIntraY4x4(&mode.BModes, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft)
	} else {
		yOK = vp8dec.PredictIntraY16x16(mode.Mode, img.Y[yOff:], img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, refs.UpAvailable, refs.LeftAvailable)
	}
	return yOK &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(mode.UVMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
}

func predictAnalysisChroma(img *vp8common.Image, row int, col int, uvMode vp8common.MBPredictionMode, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.PredictIntraUV8x8(uvMode, img.U[uOff:], img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) &&
		vp8dec.PredictIntraUV8x8(uvMode, img.V[vOff:], img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable)
}

func predictAnalysisBPredBlock(mode vp8common.BPredictionMode, dst []byte, stride int, macroblock []byte, macroblockStride int, above []byte, left []byte, topLeft byte, block int) bool {
	blockRow := block >> 2
	blockCol := block & 3
	y := blockRow * 4
	x := blockCol * 4
	var blockAbove [8]byte
	var blockLeft [4]byte

	if blockRow == 0 {
		copy(blockAbove[:], above[x:x+8])
	} else {
		aboveOff := (y-1)*macroblockStride + x
		copy(blockAbove[:4], macroblock[aboveOff:aboveOff+4])
		if blockCol < 3 {
			copy(blockAbove[4:], macroblock[aboveOff+4:aboveOff+8])
		} else {
			copy(blockAbove[4:], above[16:20])
		}
	}

	if blockCol == 0 {
		copy(blockLeft[:], left[y:y+4])
	} else {
		for i := 0; i < 4; i++ {
			blockLeft[i] = macroblock[(y+i)*macroblockStride+x-1]
		}
	}

	blockTopLeft := topLeft
	switch {
	case blockRow == 0 && blockCol == 0:
	case blockRow == 0:
		blockTopLeft = above[x-1]
	case blockCol == 0:
		blockTopLeft = left[y-1]
	default:
		blockTopLeft = macroblock[(y-1)*macroblockStride+x-1]
	}

	return dsp.Intra4x4Predict(dst, stride, mode, blockAbove[:], blockLeft[:], blockTopLeft)
}

func copyBPredBlock(src []byte, srcStride int, dst []byte, dstStride int, block int) {
	y := (block >> 2) * 4
	x := (block & 3) * 4
	for row := 0; row < 4; row++ {
		copy(dst[(y+row)*dstStride+x:], src[row*srcStride:row*srcStride+4])
	}
}

func bPredBlockSSE(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int) int {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	sse := 0
	for row := 0; row < 4; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := 0; col < 4; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			diff := int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col])
			sse += diff * diff
		}
	}
	return sse
}

func fillBPredResidual4x4(src vp8enc.SourceImage, mbRow int, mbCol int, block int, pred []byte, predStride int, out *[16]int16) {
	baseY := mbRow*16 + (block>>2)*4
	baseX := mbCol*16 + (block&3)*4
	for row := 0; row < 4; row++ {
		srcY := clampEncodeCoord(baseY+row, src.Height)
		for col := 0; col < 4; col++ {
			srcX := clampEncodeCoord(baseX+col, src.Width)
			out[row*4+col] = int16(int(src.Y[srcY*src.YStride+srcX]) - int(pred[row*predStride+col]))
		}
	}
}

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	err := 0
	for i := 0; i < 16; i++ {
		diff := int(coeff[i]) - int(dqcoeff[i])
		err += diff * diff
	}
	return err
}

func buildReconstructingBPredMacroblockCoefficients(coefProbs *vp8tables.CoefficientProbs, src vp8enc.SourceImage, mbRow int, mbCol int, img *vp8common.Image, mode *vp8dec.MacroblockMode, aboveTok *vp8enc.TokenContextPlanes, leftTok *vp8enc.TokenContextPlanes, quant *vp8enc.MacroblockQuant, qIndex int, zbinOverQuant int, fastQuant bool, optimize bool, coeffs *vp8enc.MacroblockCoefficients, scratch *vp8dec.IntraReconstructionScratch) bool {
	if img == nil || mode == nil || quant == nil || coeffs == nil || scratch == nil || !mode.Is4x4 || mode.Mode != vp8common.BPred {
		return false
	}
	if coefProbs == nil {
		return false
	}

	refs := vp8dec.BuildIntraPredictorRefs(img, mbRow, mbCol, &scratch.Refs)
	yOff := mbRow*16*img.YStride + mbCol*16
	uOff := mbRow*8*img.UStride + mbCol*8
	vOff := mbRow*8*img.VStride + mbCol*8
	y := img.Y[yOff:]
	u := img.U[uOff:]
	v := img.V[vOff:]

	var input [16]int16
	var dct [16]int16
	var dq [16]int16
	var yAbove [4]uint8
	var yLeft [4]uint8
	if aboveTok != nil {
		yAbove = aboveTok.Y1
	}
	if leftTok != nil {
		yLeft = leftTok.Y1
	}
	for block := 0; block < 16; block++ {
		blockOffset := analysisYBlockOffset(block, img.YStride)
		if !predictAnalysisBPredBlock(mode.BModes[block], y[blockOffset:], img.YStride, y, img.YStride, refs.YAbove, refs.YLeft, refs.YTopLeft, block) {
			return false
		}
		x := mbCol*16 + (block&3)*4
		yCoord := mbRow*16 + (block>>2)*4
		fillPredictedResidual4x4(src.Y, src.YStride, src.Width, src.Height, img.Y, img.YStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a := block & 3
		l := (block & 0x0c) >> 2
		ctx := int(yAbove[a] + yLeft[l])
		eob := quantizeEncodedBlock(coefProbs, qIndex, 3, ctx, 0, zbinOverQuant, 0, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.Y1, &coeffs.QCoeff[block], &dq)
		coeffs.SetBlockEOB(block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		yAbove[a] = hasCoeffs
		yLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, y[blockOffset:], img.YStride)
	}
	coeffs.QCoeff[24] = [16]int16{}
	coeffs.SetBlockEOB(24, 0)

	if !vp8dec.PredictIntraUV8x8(mode.UVMode, u, img.UStride, refs.UAbove, refs.ULeft, refs.UTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}
	if !vp8dec.PredictIntraUV8x8(mode.UVMode, v, img.VStride, refs.VAbove, refs.VLeft, refs.VTopLeft, refs.UpAvailable, refs.LeftAvailable) {
		return false
	}

	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	var uvAbove [4]uint8
	var uvLeft [4]uint8
	if aboveTok != nil {
		uvAbove = tokenUVContextArray(aboveTok)
	}
	if leftTok != nil {
		uvLeft = tokenUVContextArray(leftTok)
	}
	for block := 0; block < 4; block++ {
		x := mbCol*8 + (block&1)*4
		yCoord := mbRow*8 + (block>>1)*4

		fillPredictedResidual4x4(src.U, src.UStride, uvWidth, uvHeight, img.U, img.UStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l := macroblockCoefficientUVContextIndex(16 + block)
		ctx := int(uvAbove[a] + uvLeft[l])
		eob := quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, 0, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[16+block], &dq)
		coeffs.SetBlockEOB(16+block, eob)
		hasCoeffs := uint8(0)
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, u[analysisUVBlockOffset(block, img.UStride):], img.UStride)

		fillPredictedResidual4x4(src.V, src.VStride, uvWidth, uvHeight, img.V, img.VStride, x, yCoord, &input)
		vp8enc.ForwardDCT4x4(input[:], 4, &dct)
		a, l = macroblockCoefficientUVContextIndex(20 + block)
		ctx = int(uvAbove[a] + uvLeft[l])
		eob = quantizeEncodedBlock(coefProbs, qIndex, 2, ctx, 0, zbinOverQuant, 0, mode.RefFrame == vp8common.IntraFrame, fastQuant, optimize, &dct, &quant.UV, &coeffs.QCoeff[20+block], &dq)
		coeffs.SetBlockEOB(20+block, eob)
		hasCoeffs = 0
		if eob > 0 {
			hasCoeffs = 1
		}
		uvAbove[a] = hasCoeffs
		uvLeft[l] = hasCoeffs
		addQuantizedBlockResidual(eob, &dq, v[analysisUVBlockOffset(block, img.VStride):], img.VStride)
	}
	return true
}

func addQuantizedBlockResidual(eob int, dq *[16]int16, dst []byte, stride int) {
	if eob == 0 {
		return
	}
	if eob == 1 {
		dsp.DCOnlyIDCT4x4Add(dq[0], dst, stride, dst, stride)
		return
	}
	dsp.IDCT4x4Add(dq, dst, stride, dst, stride)
}

func analysisYBlockOffset(block int, stride int) int {
	return (block>>2)*4*stride + (block&3)*4
}

func analysisUVBlockOffset(block int, stride int) int {
	return (block>>1)*4*stride + (block&1)*4
}

func reconstructInterAnalysisMacroblock(img *vp8common.Image, last *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	if mode.Mode == vp8common.SplitMV {
		return vp8dec.ReconstructSplitMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
	}
	return vp8dec.ReconstructWholeMVInterMacroblock(mode, tokens, dequant, last, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual, row, col, vp8dec.InterPredictionConfig{})
}

func reconstructAnalysisMacroblock(img *vp8common.Image, row int, col int, mode *vp8dec.MacroblockMode, tokens *vp8dec.MacroblockTokens, dequant *vp8common.MacroblockDequant, scratch *vp8dec.IntraReconstructionScratch) bool {
	refs := vp8dec.BuildIntraPredictorRefs(img, row, col, &scratch.Refs)
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	return vp8dec.ReconstructIntraMacroblock(mode, tokens, dequant, refs, img.Y[yOff:], img.YStride, img.U[uOff:], img.UStride, img.V[vOff:], img.VStride, &scratch.Residual)
}

func fillPredictedResidual4x4(src []byte, srcStride int, width int, height int, pred []byte, predStride int, x int, y int, out *[16]int16) {
	for row := 0; row < 4; row++ {
		sampleY := clampEncodeCoord(y+row, height)
		for col := 0; col < 4; col++ {
			sampleX := clampEncodeCoord(x+col, width)
			out[row*4+col] = int16(int(src[sampleY*srcStride+sampleX]) - int(pred[(y+row)*predStride+x+col]))
		}
	}
}

func clampEncodeCoord(v int, limit int) int {
	if v < 0 {
		return 0
	}
	if v >= limit {
		return limit - 1
	}
	return v
}
