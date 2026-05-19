package analysis

// FrameInput is the read-only view of a source frame handed to an
// [Analyzer]. It is intentionally a flat struct of slices and ints so
// the VP8 encoder can build one on the stack per frame without
// allocating.
//
// The Y / U / V slices alias caller-owned plane storage. Analyzers must
// not retain them past the [Analyzer.Observe] call.
type FrameInput struct {
	// Width and Height are the visible (luma) frame dimensions in
	// pixels.
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

	// KeyFrame indicates the encoder has already decided to encode
	// this frame as a key frame. Provided for observation only.
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

// FrameAnalysis is the caller-owned per-frame analysis record. Callers
// reuse the same value across frames to keep the observation path
// allocation-free for steady-state encoders.
//
// MB is indexed in raster order (row-major, top-left first); len ==
// MBRows*MBCols when the observer is active.
type FrameAnalysis struct {
	// Width / Height are the visible luma dimensions of the observed
	// frame.
	Width  int
	Height int

	// MBCols / MBRows are the macroblock dimensions of the observed
	// frame.
	MBCols int
	MBRows int

	// FrameIndex echoes [FrameInput.FrameIndex] for downstream
	// correlation.
	FrameIndex uint64

	// KeyFrame echoes whether the observed frame was the encoder's
	// chosen key frame.
	KeyFrame bool

	// Observed reports whether Observe has been called since Reset.
	Observed bool

	// MB is the per-macroblock analysis array.
	MB []MacroblockAnalysis

	// Stats holds whole-frame aggregates derived from MB and the
	// observation timing.
	Stats AnalysisStats
}

// MacroblockAnalysis is the per-macroblock analysis record. All scalar
// fields are observation-only; the encoder must never consult them in
// this revision.
type MacroblockAnalysis struct {
	// MBX / MBY are the macroblock coordinates (column / row,
	// 0-based). Stored as int16 because VP8 frames cap at 65535
	// pixels per axis.
	MBX int16
	MBY int16

	// ZeroSAD is the sum of absolute differences between the current
	// source luma block and the colocated block in the previous
	// source frame. Zero on the first observed frame and on key
	// frames.
	ZeroSAD uint32

	// BestSAD is the best SAD found within a low-radius integer
	// search around (0,0). Currently equals ZeroSAD: the low-radius
	// search is reserved for a follow-up patch so the observation
	// stays cheap.
	BestSAD uint32

	// BestMVX / BestMVY hold the integer motion vector corresponding
	// to BestSAD, in pixels relative to colocated. In this revision
	// the observer reports (0,0) for every macroblock.
	BestMVX int16
	BestMVY int16

	// Variance is the unbiased per-macroblock 8-bit luma variance
	// proxy (sum over the MB of |x - mean|, which is cheap to
	// compute and monotone with true variance for natural content).
	Variance uint32

	// Texture is a coarse 3-tap horizontal-edge energy score for the
	// macroblock, sampled every other row.
	Texture uint16

	// StaticScore is min(255, ZeroSAD / 4) — a 0..255 view of the
	// current SAD intended for downstream classification.
	StaticScore uint16

	// Flags is a bitmask of AnalysisFlags hints.
	Flags AnalysisFlags

	// SearchRadius is a suggested integer-pel search radius derived
	// from ZeroSAD and Variance. Observation only.
	SearchRadius uint8
}

// AnalysisFlags carries coarse per-macroblock classifications produced
// by the observer.
type AnalysisFlags uint16

const (
	// FlagStatic marks blocks whose ZeroSAD falls below a small
	// threshold. They are likely well predicted by zero-motion
	// inter.
	FlagStatic AnalysisFlags = 1 << iota
	// FlagFlat marks blocks whose Variance is below a small
	// threshold.
	FlagFlat
	// FlagSkipLikely marks blocks that are both static and flat.
	FlagSkipLikely
	// FlagHighMotion marks blocks whose ZeroSAD exceeds a high
	// threshold.
	FlagHighMotion
	// FlagHighTexture marks blocks with high texture energy.
	FlagHighTexture
)

// AnalysisStats holds whole-frame aggregates produced by the observer.
type AnalysisStats struct {
	// BlocksTotal is the number of macroblocks in the observed
	// frame.
	BlocksTotal int

	// BlocksStatic counts macroblocks with FlagStatic set.
	BlocksStatic int

	// BlocksFlat counts macroblocks with FlagFlat set.
	BlocksFlat int

	// BlocksSkipLikely counts macroblocks with FlagSkipLikely set.
	BlocksSkipLikely int

	// BlocksHighMotion counts macroblocks with FlagHighMotion set.
	BlocksHighMotion int

	// AnalysisTimeNS is the elapsed monotonic-clock cost of the
	// most recent Observe call, in nanoseconds. The observer reads
	// the clock only when [Config.CollectComplexity] is true so the
	// disabled path remains clock-read free.
	AnalysisTimeNS int64
}

// Reset clears all populated fields but preserves slice capacities so
// repeated calls do not allocate.
func (fa *FrameAnalysis) Reset() {
	fa.Width = 0
	fa.Height = 0
	fa.MBCols = 0
	fa.MBRows = 0
	fa.FrameIndex = 0
	fa.KeyFrame = false
	fa.Observed = false
	fa.MB = fa.MB[:0]
	fa.Stats = AnalysisStats{}
}

// ensureMBCapacity grows fa.MB to exactly n entries, reusing the
// underlying array when possible and zeroing the new tail.
func (fa *FrameAnalysis) ensureMBCapacity(n int) {
	fa.EnsureMBCapacity(n)
}

// EnsureMBCapacity is the exported variant of ensureMBCapacity. It
// exists so the GPU analyzer in the public gpuanalysis package can
// grow the caller-owned FrameAnalysis without re-implementing the
// allocation strategy.
func (fa *FrameAnalysis) EnsureMBCapacity(n int) {
	if cap(fa.MB) >= n {
		old := len(fa.MB)
		fa.MB = fa.MB[:n]
		if n > old {
			tail := fa.MB[old:]
			for i := range tail {
				tail[i] = MacroblockAnalysis{}
			}
		}
		return
	}
	fa.MB = make([]MacroblockAnalysis, n)
}

// Analyzer is implemented by per-frame source analyzers. Implementations
// must be safe to call sequentially from the encoder's frame loop. The
// encoder does not invoke Analyzer concurrently for a single Analyzer
// instance.
//
// Implementations must not mutate the [FrameInput] argument or its plane
// slices. They write results into the caller-owned [FrameAnalysis].
//
// Analyzers must not influence encode decisions. In particular they must
// not call back into the encoder, schedule goroutines that outlive the
// call, or allocate per frame after the first observation has warmed
// caches.
type Analyzer interface {
	// Observe inspects the source frame and updates analysis.
	Observe(in *FrameInput, analysis *FrameAnalysis)

	// Mode returns the configured analyzer mode.
	Mode() VP8AnalysisMode

	// Close releases any analyzer-held resources. Calling Observe
	// after Close is a programmer error.
	Close() error
}

// ReconstructedRefConsumer is an optional interface analyzers MAY
// implement to receive the encoder's reconstructed-LAST plane after
// each frame. Implementations that do (e.g. the GPU analyzer)
// upload the plane to GPU memory so the next frame's Observe can
// compute SAD against the encoder-relevant reference instead of
// source-vs-source. Implementations that do not (e.g. the CPU
// analyzer) compare source-vs-source.
//
// The plane is treated as a packed width*height byte slice with no
// stride; the caller is responsible for stride-folding.
//
// The encoder calls this once per frame, after reconstruction is
// complete and before the next EncodeInto call.
type ReconstructedRefConsumer interface {
	AcceptReconstructedRef(plane []byte, width, height int) error
}

// New constructs an [Analyzer] from a [Config]. The returned analyzer is
// nil when cfg.Mode is [VP8AnalysisOff]; callers should treat a nil
// analyzer as "do nothing" and skip the hook entirely so the off path
// remains zero-cost (one nil check, no allocations, no interface
// dispatch).
//
// For [VP8AnalysisObserveGPU] the GPU constructor must have been
// registered via [RegisterGPUConstructor] from the gpuanalysis
// package; without that registration New returns nil and the caller
// can detect the case via the companion [NewOrError].
func New(cfg Config) Analyzer {
	a, _ := NewOrError(cfg)
	return a
}

// NewOrError is the fallible variant of [New]. It is the entry point
// the encoder uses so that VP8AnalysisObserveGPU without a registered
// constructor surfaces a clear error at NewVP8Encoder time rather
// than silently falling back to off.
func NewOrError(cfg Config) (Analyzer, error) {
	cfg = cfg.Normalize()
	switch cfg.Mode {
	case VP8AnalysisOff:
		return nil, nil
	case VP8AnalysisObserveCPU:
		return newCPUObserveAnalyzer(cfg), nil
	case VP8AnalysisObserveGPU:
		ctor := gpuConstructorFor()
		if ctor == nil {
			return nil, ErrGPUAnalyzerNotRegistered
		}
		return ctor(cfg)
	default:
		return nil, nil
	}
}
