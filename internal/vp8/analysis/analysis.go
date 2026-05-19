package analysis

// FrameInput is the read-only view of a source frame handed to an
// [Analyzer]. It is intentionally a flat struct of slices and ints so the
// VP8 encoder can build one on the stack per frame without allocating.
//
// The Y / U / V slices alias caller-owned plane storage. Analyzers must
// not retain them past the [Analyzer.Observe] call.
type FrameInput struct {
	// Width and Height are the visible (luma) frame dimensions in pixels.
	Width  int
	Height int

	// YStride / UStride / VStride are the per-row byte strides of the
	// corresponding plane.
	YStride int
	UStride int
	VStride int

	// Y, U, V alias the source planes. Read-only.
	Y []byte
	U []byte
	V []byte

	// FrameIndex is the encoder-relative frame counter (monotonically
	// increasing, includes hidden alt-ref frames).
	FrameIndex uint64

	// KeyFrame indicates the encoder has already decided to encode this
	// frame as a key frame. Provided for observation only.
	KeyFrame bool
}

// MBCount returns the number of 16x16 macroblocks in the frame.
func (in *FrameInput) MBCount() int {
	if in.Width <= 0 || in.Height <= 0 {
		return 0
	}
	cols := (in.Width + 15) >> 4
	rows := (in.Height + 15) >> 4
	return cols * rows
}

// Analyzer is implemented by per-frame source analyzers. Implementations
// must be safe to call sequentially from the encoder's frame loop. The
// encoder does not invoke Analyzer concurrently for a single Analyzer
// instance.
//
// Implementations must not mutate the [FrameInput] argument or its plane
// slices. They write results into the caller-owned [Stats] argument.
//
// Analyzers must not influence encode decisions. In particular they must
// not call back into the encoder, schedule goroutines that outlive the
// call, read wall-clock time in default builds, or allocate per frame.
type Analyzer interface {
	// Observe inspects the source frame and updates stats.
	Observe(in *FrameInput, stats *Stats)

	// Mode returns the configured analyzer mode.
	Mode() AnalysisMode

	// Close releases any analyzer-held resources. Calling Observe after
	// Close is a programmer error.
	Close() error
}

// New constructs an [Analyzer] from a [Config]. The returned analyzer is
// nil when cfg.Mode is [AnalysisOff]; callers should treat a nil analyzer
// as "do nothing" and skip the hook entirely so the off path remains
// zero-cost (one nil check, no allocations, no interface dispatch).
//
// For non-off modes the returned analyzer is preallocated; subsequent
// [Analyzer.Observe] calls allocate only when the requested statistics
// require growing the caller-supplied [Stats].
func New(cfg Config) Analyzer {
	cfg = cfg.Normalize()
	switch cfg.Mode {
	case AnalysisOff:
		return nil
	case AnalysisObserveCPU:
		return newCPUObserveAnalyzer(cfg)
	default:
		return nil
	}
}
