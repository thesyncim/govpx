package analysis

// Stats is the caller-owned per-frame statistics buffer filled by an
// [Analyzer]. Callers should reuse the same Stats value across frames to
// avoid per-frame allocation; [Stats.Reset] returns the buffer to a clean
// state while preserving the underlying slice capacities.
//
// Layouts:
//
//   - MotionHintsX / MotionHintsY are per-macroblock candidate motion
//     vector components in quarter-pel units, indexed in raster order
//     (row-major, top-left first). They are populated only when the
//     analyzer was constructed with [Config.CollectMotionHints] true.
//   - SkipCandidate is a per-macroblock bitmap of "looks skippable"
//     flags, packed one byte per macroblock for simplicity. It is
//     populated only when [Config.CollectSkipMap] is true.
//   - Complexity holds whole-frame scalar counters; populated only when
//     [Config.CollectComplexity] is true.
type Stats struct {
	// FrameIndex echoes the [FrameInput.FrameIndex] of the most recent
	// Observe call so downstream consumers can correlate stats with
	// the source frame even when frames are dropped or reordered.
	FrameIndex uint64

	// KeyFrame echoes whether the observed frame was the encoder's
	// chosen key frame.
	KeyFrame bool

	// MBCount is the number of 16x16 macroblocks in the observed frame.
	MBCount int

	// Observed reports whether Observe has been called since Reset.
	Observed bool

	// MotionHintsX, MotionHintsY are the per-macroblock motion hint
	// candidates. len == MBCount when populated; nil otherwise.
	MotionHintsX []int16
	MotionHintsY []int16

	// SkipCandidate is the per-macroblock skip-candidate map.
	// len == MBCount when populated; nil otherwise.
	SkipCandidate []uint8

	// Complexity holds whole-frame scalar counters.
	Complexity ComplexityStats
}

// ComplexityStats holds whole-frame scalar counters produced by the
// observation analyzer. All values are computed in fixed-point integer
// arithmetic so the observation path stays allocation-free and
// deterministic across architectures.
type ComplexityStats struct {
	// LumaSum is the sum of all luma sample values in the visible
	// frame.
	LumaSum uint64

	// LumaAbsDiff8x8Sum approximates frame variance: it is the sum
	// over each 8x8 luma block of the sum of absolute differences
	// between each sample and the block mean. Higher == more
	// detail / texture.
	LumaAbsDiff8x8Sum uint64

	// EdgeScore is a coarse 3-tap horizontal-edge energy proxy on
	// the luma plane. It samples every 4th row to stay cheap.
	EdgeScore uint64
}

// Reset clears all populated fields but preserves slice capacities so
// repeated calls do not allocate.
func (s *Stats) Reset() {
	s.FrameIndex = 0
	s.KeyFrame = false
	s.MBCount = 0
	s.Observed = false
	s.MotionHintsX = s.MotionHintsX[:0]
	s.MotionHintsY = s.MotionHintsY[:0]
	s.SkipCandidate = s.SkipCandidate[:0]
	s.Complexity = ComplexityStats{}
}

// ensureMBCapacityInt16 returns dst sized to n entries, reusing the
// underlying array when possible. It is used by the observe analyzer
// to grow caller-provided buffers without churning the heap.
func ensureMBCapacityInt16(dst []int16, n int) []int16 {
	if cap(dst) >= n {
		return dst[:n]
	}
	return make([]int16, n)
}

func ensureMBCapacityByte(dst []byte, n int) []byte {
	if cap(dst) >= n {
		return dst[:n]
	}
	return make([]byte, n)
}
