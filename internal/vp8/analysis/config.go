// Package analysis defines the VP8 source-frame analysis framework used by
// the VP8 encoder.
//
// Scope contract: in this initial revision the framework is observation-only.
// Analyzers may inspect source planes and record statistics, but no analyzer
// output is permitted to influence VP8 encode decisions. The encoder must
// produce a byte-identical bitstream whether [AnalysisOff] or
// [AnalysisObserveCPU] is selected.
//
// Future revisions may introduce non-parity modes (for example GPU-assisted
// SAD or motion hints that participate in mode decision). Such modes must be
// added explicitly and must not be enabled when [Config.ByteParityRequired]
// is true.
package analysis

// AnalysisMode selects which analyzer the VP8 encoder runs per source frame.
type AnalysisMode int

const (
	// AnalysisOff disables analysis entirely. The encoder takes the
	// exact pre-analysis code path: no per-frame hook is invoked, no
	// frame-input descriptor is built, and no statistics are recorded.
	AnalysisOff AnalysisMode = iota

	// AnalysisObserveCPU runs the CPU observation analyzer. It computes
	// optional motion / complexity / skip statistics on the source
	// frame but must not influence any encode decision. The output
	// bitstream is byte-identical to [AnalysisOff].
	AnalysisObserveCPU
)

// String returns a stable human-readable label for the mode.
func (m AnalysisMode) String() string {
	switch m {
	case AnalysisOff:
		return "off"
	case AnalysisObserveCPU:
		return "observe-cpu"
	default:
		return "unknown"
	}
}

// Config selects the analyzer and the statistics it should collect.
//
// The zero value disables analysis. Use [DefaultConfig] to obtain a value
// with the safe defaults applied (off + byte-parity required).
type Config struct {
	// Mode selects the analyzer. Defaults to [AnalysisOff].
	Mode AnalysisMode

	// ByteParityRequired guards against any analyzer that could change
	// the encoded bitstream. In this revision the framework supports
	// only observation, so ByteParityRequired is always honored; the
	// field is plumbed through so future non-parity modes can be added
	// without an API break. Defaults to true after [Config.Normalize].
	ByteParityRequired bool

	// CollectMotionHints requests per-macroblock motion hint candidates
	// (best-effort, observation-only). When false the analyzer may skip
	// the search entirely.
	CollectMotionHints bool

	// CollectSkipMap requests a per-macroblock skip-candidate bitmap.
	CollectSkipMap bool

	// CollectComplexity requests scalar frame-complexity counters
	// (variance proxies, edge energy).
	CollectComplexity bool
}

// DefaultConfig returns the safe default configuration: analysis disabled,
// byte parity required.
func DefaultConfig() Config {
	return Config{
		Mode:               AnalysisOff,
		ByteParityRequired: true,
	}
}

// Normalize fills in defaults and enforces invariants. In this revision the
// only enforced invariant is that ByteParityRequired is forced to true,
// because no non-parity code path exists yet. Returning a copy keeps Config
// values shareable across goroutines without surprises.
func (c Config) Normalize() Config {
	c.ByteParityRequired = true
	return c
}

// AffectsEncodeDecisions reports whether the configured mode is permitted
// to influence encode decisions. In this revision it is always false; the
// VP8 encoder uses this to assert that no observation result is wired into
// any decision path.
func (c Config) AffectsEncodeDecisions() bool {
	return false
}
