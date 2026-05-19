package analysis

// NoopAnalyzer is an [Analyzer] that records nothing. It exists for
// callers that want a non-nil Analyzer value (for benchmark setup,
// test fixtures, or interface plumbing) without paying any per-frame
// observation cost.
//
// The VP8 encoder hook treats a nil Analyzer as the disabled path; this
// type is not the disabled path. Use [AnalysisOff] in [Config] to get the
// zero-cost path.
type NoopAnalyzer struct{}

// NewNoop returns a stateless no-op analyzer.
func NewNoop() *NoopAnalyzer { return &NoopAnalyzer{} }

// Observe does nothing. It explicitly does not touch the [Stats] value so
// callers can detect the absence of observation downstream.
func (*NoopAnalyzer) Observe(_ *FrameInput, _ *Stats) {}

// Mode reports [AnalysisOff].
func (*NoopAnalyzer) Mode() AnalysisMode { return AnalysisOff }

// Close is a no-op.
func (*NoopAnalyzer) Close() error { return nil }
