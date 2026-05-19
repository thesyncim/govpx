package govpx

import (
	"github.com/thesyncim/govpx/internal/vp8/analysis"
)

// VP8AnalysisConfig configures the optional VP8 source-frame analyzer.
//
// The analyzer runs once per encoded source frame, before mode decision,
// and records the statistics selected by its [VP8AnalysisConfig] fields.
// In this revision the analyzer is observation-only: no analyzer output
// may influence the VP8 bitstream. Default behavior (the zero value, or
// any value with Mode == VP8AnalysisOff) is byte-identical to a build
// without the analysis hook at all.
//
// The type is a transparent alias for the internal analysis package's
// Config so callers can construct it with named fields and govpx
// internals can manipulate it without import cycles.
type VP8AnalysisConfig = analysis.Config

// VP8AnalysisMode selects the analyzer that the VP8 encoder runs per
// frame.
type VP8AnalysisMode = analysis.VP8AnalysisMode

// VP8FrameAnalysis is the per-frame analysis record. Use
// [VP8Encoder.LastFrameAnalysis] to read the most recent snapshot.
// The MB slice aliases internal storage and is overwritten by the next
// encode call; copy it if it must outlive the call.
type VP8FrameAnalysis = analysis.FrameAnalysis

// VP8MacroblockAnalysis is the per-macroblock analysis record produced
// by the observer.
type VP8MacroblockAnalysis = analysis.MacroblockAnalysis

// VP8AnalysisStats is the whole-frame aggregate produced by the
// observer.
type VP8AnalysisStats = analysis.AnalysisStats

// VP8AnalysisFlags carries coarse per-macroblock classification hints.
type VP8AnalysisFlags = analysis.AnalysisFlags

const (
	// VP8AnalysisOff disables the analyzer entirely. The encoder
	// takes the exact pre-analysis code path: no per-frame hook is
	// invoked, no frame-input descriptor is built, and no statistics
	// are recorded. This is the default.
	VP8AnalysisOff = analysis.VP8AnalysisOff

	// VP8AnalysisObserveCPU runs the CPU observation analyzer. It
	// computes optional motion / complexity / skip statistics on the
	// source frame but does not influence encode decisions. The
	// output bitstream is byte-identical to VP8AnalysisOff.
	VP8AnalysisObserveCPU = analysis.VP8AnalysisObserveCPU
)

// Per-macroblock analysis flags exported at the public package level.
const (
	VP8FlagStatic      = analysis.FlagStatic
	VP8FlagFlat        = analysis.FlagFlat
	VP8FlagSkipLikely  = analysis.FlagSkipLikely
	VP8FlagHighMotion  = analysis.FlagHighMotion
	VP8FlagHighTexture = analysis.FlagHighTexture
)

// DefaultVP8AnalysisConfig returns the safe default analyzer
// configuration: disabled, byte-parity required.
func DefaultVP8AnalysisConfig() VP8AnalysisConfig {
	return analysis.DefaultConfig()
}

// LastFrameAnalysis returns the most recently recorded analysis for
// this encoder, or nil if no analyzer is configured.
//
// The returned pointer is stable across calls; its contents are
// overwritten by the next EncodeInto or FlushInto call. Callers must
// not retain the embedded slice references across encode calls.
func (e *VP8Encoder) LastFrameAnalysis() *VP8FrameAnalysis {
	if e == nil || e.analyzer == nil {
		return nil
	}
	return &e.analysisOutput
}

// LastAnalysisStats is a convenience accessor that returns a pointer
// to the most recent [VP8AnalysisStats] aggregate, or nil if no
// analyzer is configured.
func (e *VP8Encoder) LastAnalysisStats() *VP8AnalysisStats {
	if e == nil || e.analyzer == nil {
		return nil
	}
	return &e.analysisOutput.Stats
}

// AnalysisMode reports the configured analyzer mode for this encoder.
// Returns [VP8AnalysisOff] when no analyzer is configured.
func (e *VP8Encoder) AnalysisMode() VP8AnalysisMode {
	if e == nil || e.analyzer == nil {
		return VP8AnalysisOff
	}
	return e.analyzer.Mode()
}
