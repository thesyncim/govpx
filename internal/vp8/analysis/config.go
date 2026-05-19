// Package analysis defines the VP8 source-frame analysis framework used by
// the VP8 encoder.
//
// Scope contract: in this initial revision the framework is observation-only.
// Analyzers may inspect source planes and record statistics, but no analyzer
// output is permitted to influence VP8 encode decisions. The encoder must
// produce a byte-identical bitstream whether [VP8AnalysisOff] or
// [VP8AnalysisObserveCPU] is selected.
//
// Future revisions may introduce non-parity modes (for example GPU-assisted
// SAD or motion hints that participate in mode decision). Such modes must be
// added explicitly and must not be enabled when [Config.ByteParityRequired]
// is true.
package analysis

// VP8AnalysisMode selects which analyzer the VP8 encoder runs per source
// frame.
type VP8AnalysisMode int

const (
	// VP8AnalysisOff disables analysis entirely. The encoder takes the
	// exact pre-analysis code path: no per-frame hook is invoked, no
	// frame-input descriptor is built, and no statistics are recorded.
	VP8AnalysisOff VP8AnalysisMode = iota

	// VP8AnalysisObserveCPU runs the CPU observation analyzer. It
	// computes optional motion / complexity / skip statistics on the
	// source frame but must not influence any encode decision. The
	// output bitstream is byte-identical to [VP8AnalysisOff].
	VP8AnalysisObserveCPU

	// VP8AnalysisObserveGPU runs the GPU observation analyzer. The
	// implementation lives in a separate package
	// (github.com/thesyncim/govpx/gpuanalysis) which the caller must
	// blank-import to register the constructor. Without the import,
	// requesting this mode returns an error at encoder construction
	// time. Output bitstream is still byte-identical to
	// [VP8AnalysisOff] because the analyzer is observation-only.
	VP8AnalysisObserveGPU
)

// String returns a stable human-readable label for the mode.
func (m VP8AnalysisMode) String() string {
	switch m {
	case VP8AnalysisOff:
		return "off"
	case VP8AnalysisObserveCPU:
		return "observe-cpu"
	case VP8AnalysisObserveGPU:
		return "observe-gpu"
	default:
		return "unknown"
	}
}

// Config selects the analyzer and the statistics it should collect.
//
// The zero value disables analysis. Use [DefaultConfig] to obtain a value
// with the safe defaults applied (off + byte-parity required).
type Config struct {
	// Mode selects the analyzer. Defaults to [VP8AnalysisOff].
	Mode VP8AnalysisMode

	// ByteParityRequired guards against any analyzer or encoder hint
	// consumer that could change the encoded bitstream. When true (the
	// default), no analyzer output is permitted to influence encode
	// decisions; the bitstream is byte-identical to a build without
	// the hook at all. [Config.Normalize] forces it true unless
	// [UseEncodeHints] is explicitly opted in.
	ByteParityRequired bool

	// CollectMotionHints requests per-macroblock zero-MV SAD and a
	// rough low-radius best-MV estimate. The CPU observer caches the
	// previous source luma plane to compute these without consulting
	// encoder reconstruction buffers.
	CollectMotionHints bool

	// CollectSkipMap requests a per-macroblock skip-likely flag in the
	// FrameAnalysis MB array.
	CollectSkipMap bool

	// CollectComplexity requests per-macroblock variance / texture
	// counters and the whole-frame AnalysisStats aggregates.
	CollectComplexity bool

	// UseEncodeHints opts the encoder into consuming analyzer output as
	// a decision input. Setting it to true:
	//
	//   - implies [ByteParityRequired] = false (the encoded bitstream
	//     WILL differ from the no-analysis baseline);
	//   - causes the encoder to apply hint-driven optimizations such
	//     as motion-search early-exit on macroblocks flagged
	//     [FlagSkipLikely] / [FlagStatic];
	//   - is documented in docs/vp8_gpu_hint_consumption.md.
	//
	// Quality impact is documented per-optimization in that file. The
	// design intent is "non-noticeable quality loss for measurable
	// encode speedup"; consumers SHOULD validate quality on their
	// target corpus before flipping this on for production.
	UseEncodeHints bool
}

// DefaultConfig returns the safe default configuration: analysis disabled,
// byte parity required.
func DefaultConfig() Config {
	return Config{
		Mode:               VP8AnalysisOff,
		ByteParityRequired: true,
	}
}

// Normalize fills in defaults and enforces invariants. The rule is:
//
//   - If UseEncodeHints is false, ByteParityRequired is forced true.
//   - If UseEncodeHints is true, ByteParityRequired is forced false
//     (parity cannot hold once hints feed decisions).
//
// Returning a copy keeps Config values shareable across goroutines without
// surprises.
func (c Config) Normalize() Config {
	if c.UseEncodeHints {
		c.ByteParityRequired = false
	} else {
		c.ByteParityRequired = true
	}
	return c
}

// AffectsEncodeDecisions reports whether the configured mode is permitted
// to influence encode decisions. True only when [UseEncodeHints] is set.
func (c Config) AffectsEncodeDecisions() bool {
	return c.UseEncodeHints
}
