//go:build govpx_oracle_trace

package vp8test

import (
	"io"

	"github.com/thesyncim/govpx/internal/coracle"
)

// CompareOptions configures oracle trace comparison.
type CompareOptions = coracle.CompareOptions

// Divergence describes one oracle trace divergence.
type Divergence = coracle.Divergence

// CompareOracleTraces compares projected govpx and libvpx JSONL traces.
func CompareOracleTraces(govpxJSONL io.Reader, libvpxJSONL io.Reader,
	opts CompareOptions,
) ([]Divergence, error) {
	divergences, err := coracle.CompareOracleTraces(govpxJSONL, libvpxJSONL,
		opts)
	return divergences, err
}

// FormatDivergences formats trace divergences for failure output.
func FormatDivergences(divergences []Divergence) string {
	out := coracle.FormatDivergences(divergences)
	return out
}

// FirstTraceRows formats the first non-empty trace rows.
func FirstTraceRows(trace []byte, limit int) string {
	rows := coracle.FirstTraceRows(trace, limit)
	return rows
}

// ProjectVP8EncoderDecisionTrace keeps the VP8 encoder-decision trace fields
// tested by root oracle comparisons.
func ProjectVP8EncoderDecisionTrace(trace []byte) ([]byte, error) {
	projected, err := coracle.ProjectVP8EncoderDecisionTrace(trace)
	return projected, err
}

// ProjectVP8InterCandidateTrace keeps VP8 inter-candidate trace fields.
func ProjectVP8InterCandidateTrace(trace []byte) ([]byte, error) {
	projected, err := coracle.ProjectVP8InterCandidateTrace(trace)
	return projected, err
}

// ProjectVP8InterCandidateThresholdTrace keeps VP8 inter-candidate threshold
// trace fields.
func ProjectVP8InterCandidateThresholdTrace(trace []byte) ([]byte, error) {
	projected, err := coracle.ProjectVP8InterCandidateThresholdTrace(trace)
	return projected, err
}

// TraceRows parses oracle JSONL trace rows.
func TraceRows(trace []byte) ([]map[string]any, error) {
	rows, err := coracle.TraceRows(trace)
	return rows, err
}

// TraceRowsOfType returns rows whose type matches rowType.
func TraceRowsOfType(trace []byte, rowType string) ([]map[string]any, error) {
	rows, err := coracle.TraceRowsOfType(trace, rowType)
	return rows, err
}

// TraceFrameRows returns frame rows from an oracle trace.
func TraceFrameRows(trace []byte) ([]map[string]any, error) {
	rows, err := coracle.TraceFrameRows(trace)
	return rows, err
}

// TraceRowsByFrame indexes typed oracle trace rows by frame number.
func TraceRowsByFrame(trace []byte, rowType string) (map[int64]map[string]any, error) {
	rows, err := coracle.TraceRowsByFrame(trace, rowType)
	return rows, err
}

// TraceFloat extracts numeric fields from oracle trace rows.
func TraceFloat(value any) float64 {
	v := coracle.TraceFloat(value)
	return v
}
