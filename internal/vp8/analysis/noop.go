package analysis

// NoopAnalyzer is an [Analyzer] that records nothing. It exists for
// callers that want a non-nil Analyzer value (for benchmark setup,
// test fixtures, or interface plumbing) without paying any per-frame
// observation cost.
//
// The VP8 encoder hook treats a nil Analyzer as the disabled path;
// this type is not the disabled path. Use [VP8AnalysisOff] in [Config]
// to get the zero-cost path.
type NoopAnalyzer struct{}

// NewNoop returns a stateless no-op analyzer.
func NewNoop() *NoopAnalyzer { return &NoopAnalyzer{} }

// Observe does nothing. It explicitly does not touch the
// [FrameAnalysis] value so callers can detect the absence of
// observation downstream.
func (*NoopAnalyzer) Observe(_ *FrameInput, _ *FrameAnalysis) {}

// Mode reports [VP8AnalysisOff].
func (*NoopAnalyzer) Mode() VP8AnalysisMode { return VP8AnalysisOff }

// Close is a no-op.
func (*NoopAnalyzer) Close() error { return nil }
