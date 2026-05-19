package govpx

import (
	vp8analysis "github.com/thesyncim/govpx/internal/vp8/analysis"
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
