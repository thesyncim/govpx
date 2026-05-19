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
// any value with Mode==AnalysisOff) is byte-identical to a build without
// the analysis hook at all.
//
// The type is a transparent alias for the internal analysis package's
// Config so callers can construct it with named fields and govpx
// internals can manipulate it without import cycles.
type VP8AnalysisConfig = analysis.Config

// AnalysisMode selects the analyzer that the VP8 encoder runs per frame.
type AnalysisMode = analysis.AnalysisMode

// VP8AnalysisStats is the per-frame statistics record produced by the
// analyzer. Use [VP8Encoder.LastAnalysisStats] to read the most recent
// snapshot. The slice fields alias internal storage and are overwritten
// by the next encode call; copy them if they must outlive the call.
type VP8AnalysisStats = analysis.Stats

// VP8AnalysisComplexityStats holds the scalar complexity counters that
// the observation analyzer can produce.
type VP8AnalysisComplexityStats = analysis.ComplexityStats

const (
	// AnalysisOff disables the analyzer entirely. The encoder takes
	// the exact pre-analysis code path: no per-frame hook is invoked,
	// no frame-input descriptor is built, and no statistics are
	// recorded. This is the default.
	AnalysisOff = analysis.AnalysisOff

	// AnalysisObserveCPU runs the CPU observation analyzer. It
	// computes optional motion / complexity / skip statistics on the
	// source frame but does not influence encode decisions. The
	// output bitstream is byte-identical to AnalysisOff.
	AnalysisObserveCPU = analysis.AnalysisObserveCPU
)

// DefaultVP8AnalysisConfig returns the safe default analyzer
// configuration: disabled, byte-parity required.
func DefaultVP8AnalysisConfig() VP8AnalysisConfig {
	return analysis.DefaultConfig()
}

// LastAnalysisStats returns the most recently recorded analysis statistics
// for this encoder, or nil if no analyzer is configured.
//
// The returned pointer is stable across calls; its contents are overwritten
// by the next EncodeInto or FlushInto call. Callers must not retain the
// embedded slice references across encode calls.
func (e *VP8Encoder) LastAnalysisStats() *VP8AnalysisStats {
	if e == nil || e.analyzer == nil {
		return nil
	}
	return &e.analysisStats
}

// AnalysisMode reports the configured analyzer mode for this encoder.
// Returns [AnalysisOff] when no analyzer is configured.
func (e *VP8Encoder) AnalysisMode() AnalysisMode {
	if e == nil || e.analyzer == nil {
		return AnalysisOff
	}
	return e.analyzer.Mode()
}
