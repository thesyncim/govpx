package govpx

import (
	vp8analysis "github.com/thesyncim/govpx/internal/vp8/analysis"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// runSourceAnalysis is the implementation of the VP8 source-frame
// analysis hook. The caller (encodeSourceInto) guards entry on a nil
// e.analyzer check so this function is only reached when an analyzer
// is active; the byte-parity rule for VP8AnalysisOff is enforced at
// the call site, not here.
//
// The function reuses e.analysisInput and e.analysisOutput; it does
// not allocate per frame once the analysis MB array has reached the
// frame's macroblock count.
//
// Observation-only contract: this function and any analyzer reachable
// from it may not call back into the encoder, mutate encoder state
// other than e.analysisInput / e.analysisOutput, or perform work that
// could leak into encode decisions. Reviewers should treat any change
// that lifts these constraints as a behavior change requiring the
// byte-parity tests in vp8_analysis_parity_test.go to be re-run on a
// representative corpus.
func (e *VP8Encoder) runSourceAnalysis(source vp8enc.SourceImage, keyFrame bool) {
	in := &e.analysisInput
	in.Width = source.Width
	in.Height = source.Height
	in.YStride = source.YStride
	in.UStride = source.UStride
	in.VStride = source.VStride
	in.Y = source.Y
	in.U = source.U
	in.V = source.V
	in.FrameIndex = e.frameCount
	in.KeyFrame = keyFrame
	e.analyzer.Observe(in, &e.analysisOutput)
}

// hintSkipsRemainingInterModes reports whether the per-MB analysis
// hint for (mbRow, mbCol) authorises the encoder to commit to the
// first inter mode (ZEROMV-LAST) and skip evaluating the rest. It
// returns true only when ALL of the following hold:
//
//   - VP8AnalysisConfig.UseEncodeHints is set (explicit opt-in to
//     parity-breaking optimizations);
//   - an analyzer is configured and has produced output for the
//     current frame;
//   - the analyzer's per-MB record has the FlagStatic bit set.
//
// The cost of this check on the canonical path (UseEncodeHints == false)
// is one cached-load + branch — no per-MB allocation, no lookups.
func (e *VP8Encoder) hintSkipsRemainingInterModes(mbRow, mbCol, mbCols int) bool {
	if !e.opts.Analysis.UseEncodeHints || e.analyzer == nil {
		return false
	}
	fa := &e.analysisOutput
	if !fa.Observed {
		return false
	}
	if fa.MBCols != mbCols {
		return false
	}
	idx := mbRow*mbCols + mbCol
	if idx < 0 || idx >= len(fa.MB) {
		return false
	}
	hit := fa.MB[idx].Flags&vp8analysis.FlagStatic != 0
	if hit {
		e.hintEarlyExitCount++
	} else {
		e.hintMissCount++
	}
	return hit
}

// HintEarlyExitCount returns the cumulative number of macroblocks
// where hintSkipsRemainingInterModes returned true since the last
// Reset. Used by hint-driven bench tests to verify the wire-up is
// actually firing.
func (e *VP8Encoder) HintEarlyExitCount() uint64 {
	if e == nil {
		return 0
	}
	return e.hintEarlyExitCount
}

// HintMissCount returns the cumulative number of macroblocks where
// hintSkipsRemainingInterModes was consulted but returned false.
// Together with HintEarlyExitCount this gives the hit-rate for the
// hint-driven optimization.
func (e *VP8Encoder) HintMissCount() uint64 {
	if e == nil {
		return 0
	}
	return e.hintMissCount
}

// HintForceSkipCount returns the cumulative number of macroblocks
// routed into the encoder's static-encode-breakout path via
// interStaticThresholdForSegmentMB. Zero on the canonical path.
func (e *VP8Encoder) HintForceSkipCount() uint64 {
	if e == nil {
		return 0
	}
	return e.hintForceSkipCount
}

// HintPickerBypassCount returns the cumulative number of macroblocks
// where selectInterFrameModeDecision returned a synthesized
// ZEROMV-LAST decision without invoking the picker.
func (e *VP8Encoder) HintPickerBypassCount() uint64 {
	if e == nil {
		return 0
	}
	return e.hintPickerBypassCount
}

// hintBypassPickerDecision synthesises a ZEROMV-LAST decision for
// hint-flagged macroblocks, computing the real reconstruction-domain
// SSE so the rate controller sees a value consistent with what the
// picker would have produced. This is the fix for the earlier
// bypass experiment that used the GPU's source-domain ZeroSAD and
// triggered rate-control recodes (60-72% regression at 1080p / 4K).
//
// The cost is one macroblockLumaMotionVarianceSSE call (~1us per MB
// at 4K) in exchange for skipping the picker's ~50 lines of setup +
// 19 mode evaluations.
func (e *VP8Encoder) hintBypassPickerDecision(
	src vp8enc.SourceImage,
	refs []interAnalysisReference, refCount int,
	segmentID uint8, mbRow, mbCol, mbCols int,
) (interFrameModeDecision, bool) {
	if !e.opts.Analysis.UseEncodeHints || e.analyzer == nil {
		return interFrameModeDecision{}, false
	}
	fa := &e.analysisOutput
	if !fa.Observed || fa.MBCols != mbCols {
		return interFrameModeDecision{}, false
	}
	idx := mbRow*mbCols + mbCol
	if idx < 0 || idx >= len(fa.MB) {
		return interFrameModeDecision{}, false
	}
	if fa.MB[idx].Flags&vp8analysis.FlagSkipLikely == 0 {
		return interFrameModeDecision{}, false
	}
	// Find LAST. The bypass only emits ZEROMV-LAST, so a missing
	// LAST reference disqualifies the MB and the picker runs
	// normally.
	var lastRef interAnalysisReference
	found := false
	for i := 0; i < refCount && i < len(refs); i++ {
		if refs[i].Frame == vp8common.LastFrame && refs[i].Img != nil {
			lastRef = refs[i]
			found = true
			break
		}
	}
	if !found {
		return interFrameModeDecision{}, false
	}
	// Compute the true reconstruction-domain SSE at MV=(0,0) so the
	// decision's predictionError matches what the picker would
	// have populated.
	_, sse := macroblockLumaMotionVarianceSSE(src, lastRef.Img, mbRow, mbCol, vp8enc.MotionVector{})
	e.hintPickerBypassCount++
	return interFrameModeDecision{
		useIntra: false,
		interMode: vp8enc.InterFrameMacroblockMode{
			RefFrame:  vp8common.LastFrame,
			Mode:      vp8common.ZeroMV,
			SegmentID: segmentID,
		},
		ref:             lastRef,
		projectedRate:   500,
		predictionError: int32(sse),
	}, true
}

// pushReconstructedRefToAnalyzer hands e.lastRef's luma plane to the
// analyzer's optional ReconstructedRefConsumer interface, if it
// implements it. CPU analyzer does not; GPU analyzer does — it
// uploads the plane into a dedicated GPU buffer so the next Observe
// can compute SAD in the reconstruction domain (what the encoder's
// motion search uses) instead of source-vs-source.
//
// Default path (no analyzer / no consumer) is a single type
// assertion that resolves to nil; no per-frame allocation.
func (e *VP8Encoder) pushReconstructedRefToAnalyzer() {
	if e == nil || e.analyzer == nil {
		return
	}
	consumer, ok := e.analyzer.(vp8analysis.ReconstructedRefConsumer)
	if !ok {
		return
	}
	plane := e.lastRef.Img.Y
	w := e.lastRef.Img.Width
	h := e.lastRef.Img.Height
	stride := e.lastRef.Img.YStride
	if w <= 0 || h <= 0 || len(plane) == 0 {
		return
	}
	// Stride-fold if necessary so the backend sees a packed
	// width*height buffer.
	if stride == w {
		_ = consumer.AcceptReconstructedRef(plane[:w*h], w, h)
		return
	}
	// Allocate per-call scratch — this is the rare case; common
	// reference layouts have stride == width.
	scratch := make([]byte, w*h)
	for y := range h {
		copy(scratch[y*w:(y+1)*w], plane[y*stride:y*stride+w])
	}
	_ = consumer.AcceptReconstructedRef(scratch, w, h)
}

// closeAnalysis releases analyzer-held resources, if any. Called by
// the encoder Close path so a non-nil analyzer can clean up. Safe to
// call when no analyzer is configured.
func (e *VP8Encoder) closeAnalysis() error {
	if e == nil || e.analyzer == nil {
		return nil
	}
	err := e.analyzer.Close()
	e.analyzer = nil
	return err
}

// compile-time assertion that the analyzer is exactly the interface
// the public alias type identifies. The unused declaration keeps the
// internal import alive for tools that prune unused imports during
// future refactors.
var _ vp8analysis.Analyzer = (*vp8analysis.NoopAnalyzer)(nil)
