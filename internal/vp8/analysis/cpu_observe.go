package analysis

// cpuObserveAnalyzer is the CPU-only observation analyzer. It computes
// the statistics requested by its [Config] and writes them into the
// caller-owned [Stats]. By construction it must never feed any value
// back into the VP8 encoder; the encoder hook discards the observation
// result for any byte-parity sensitive decision.
type cpuObserveAnalyzer struct {
	cfg Config
}

func newCPUObserveAnalyzer(cfg Config) *cpuObserveAnalyzer {
	return &cpuObserveAnalyzer{cfg: cfg}
}

// Observe inspects the source frame and writes statistics to stats.
// The analyzer reads the luma plane only; chroma is ignored in this
// revision because no chroma-only statistic is currently emitted.
func (a *cpuObserveAnalyzer) Observe(in *FrameInput, stats *Stats) {
	if in == nil || stats == nil {
		return
	}
	mbCount := in.MBCount()
	stats.FrameIndex = in.FrameIndex
	stats.KeyFrame = in.KeyFrame
	stats.MBCount = mbCount
	stats.Observed = true

	if a.cfg.CollectMotionHints {
		stats.MotionHintsX = ensureMBCapacityInt16(stats.MotionHintsX, mbCount)
		stats.MotionHintsY = ensureMBCapacityInt16(stats.MotionHintsY, mbCount)
		// First-revision hint policy: report zero-motion candidates.
		// This is a deterministic placeholder; future revisions may
		// run integer-pel SAD across small windows. Zero hints are
		// safe and require no extra reads, so the observation cost
		// remains bounded.
		for i := range stats.MotionHintsX {
			stats.MotionHintsX[i] = 0
			stats.MotionHintsY[i] = 0
		}
	} else {
		stats.MotionHintsX = stats.MotionHintsX[:0]
		stats.MotionHintsY = stats.MotionHintsY[:0]
	}

	if a.cfg.CollectSkipMap {
		stats.SkipCandidate = ensureMBCapacityByte(stats.SkipCandidate, mbCount)
		for i := range stats.SkipCandidate {
			stats.SkipCandidate[i] = 0
		}
	} else {
		stats.SkipCandidate = stats.SkipCandidate[:0]
	}

	if a.cfg.CollectComplexity {
		stats.Complexity = computeLumaComplexity(in)
		if a.cfg.CollectSkipMap && len(stats.SkipCandidate) == mbCount {
			fillSkipCandidates(in, stats.SkipCandidate)
		}
	} else {
		stats.Complexity = ComplexityStats{}
	}
}

func (a *cpuObserveAnalyzer) Mode() AnalysisMode { return AnalysisObserveCPU }

func (a *cpuObserveAnalyzer) Close() error { return nil }

// computeLumaComplexity walks the luma plane producing the three scalar
// complexity counters defined on [ComplexityStats].
//
// Implementation notes:
//   - The traversal is allocation-free: no slices are constructed, only
//     scalar accumulators are touched.
//   - The 8x8 absolute-difference proxy reads each visible luma sample
//     at most twice (once for the block mean, once for the deviation
//     sum). It tolerates partial 8x8 blocks at the right/bottom frame
//     edges by clipping the inner loops.
//   - Edge energy is a horizontal 3-tap proxy sampled every 4th row to
//     keep the analyzer cheap; it is documented as such for future
//     tuning.
func computeLumaComplexity(in *FrameInput) ComplexityStats {
	var stats ComplexityStats
	if in == nil || in.Width <= 0 || in.Height <= 0 || len(in.Y) == 0 || in.YStride <= 0 {
		return stats
	}
	w := in.Width
	h := in.Height
	stride := in.YStride
	plane := in.Y

	// LumaSum.
	for y := range h {
		row := plane[y*stride : y*stride+w]
		var rowSum uint64
		for _, v := range row {
			rowSum += uint64(v)
		}
		stats.LumaSum += rowSum
	}

	// 8x8 absolute-difference proxy.
	for by := 0; by < h; by += 8 {
		blockH := 8
		if by+blockH > h {
			blockH = h - by
		}
		for bx := 0; bx < w; bx += 8 {
			blockW := 8
			if bx+blockW > w {
				blockW = w - bx
			}
			var sum uint32
			for j := 0; j < blockH; j++ {
				row := plane[(by+j)*stride+bx : (by+j)*stride+bx+blockW]
				for _, v := range row {
					sum += uint32(v)
				}
			}
			n := uint32(blockW * blockH)
			if n == 0 {
				continue
			}
			mean := sum / n
			var dev uint64
			for j := 0; j < blockH; j++ {
				row := plane[(by+j)*stride+bx : (by+j)*stride+bx+blockW]
				for _, v := range row {
					if uint32(v) >= mean {
						dev += uint64(uint32(v) - mean)
					} else {
						dev += uint64(mean - uint32(v))
					}
				}
			}
			stats.LumaAbsDiff8x8Sum += dev
		}
	}

	// Horizontal 3-tap edge energy sampled every 4th row.
	if w >= 3 {
		for y := 0; y < h; y += 4 {
			row := plane[y*stride : y*stride+w]
			var rowEnergy uint64
			for x := 1; x < w-1; x++ {
				left := int32(row[x-1])
				center := int32(row[x])
				right := int32(row[x+1])
				delta := left - 2*center + right
				if delta < 0 {
					delta = -delta
				}
				rowEnergy += uint64(uint32(delta))
			}
			stats.EdgeScore += rowEnergy
		}
	}

	return stats
}

// fillSkipCandidates marks macroblocks as "looks skippable" using the
// LumaAbsDiff8x8 proxy: a macroblock whose four 8x8 deviation sums fall
// below a small threshold is flagged. The threshold is intentionally
// tight so the map stays mostly zero on motion-heavy content. The map
// is observation-only and never consulted by the encoder.
func fillSkipCandidates(in *FrameInput, dst []uint8) {
	if in == nil || in.Width <= 0 || in.Height <= 0 || in.YStride <= 0 {
		return
	}
	mbCols := (in.Width + 15) >> 4
	mbRows := (in.Height + 15) >> 4
	stride := in.YStride
	plane := in.Y
	const threshold = 256 // sum of |x - mean| over a 16x16 block
	for mby := range mbRows {
		for mbx := range mbCols {
			x0 := mbx * 16
			y0 := mby * 16
			x1 := min(x0+16, in.Width)
			y1 := min(y0+16, in.Height)
			var sum uint32
			for y := y0; y < y1; y++ {
				row := plane[y*stride+x0 : y*stride+x1]
				for _, v := range row {
					sum += uint32(v)
				}
			}
			n := uint32((x1 - x0) * (y1 - y0))
			if n == 0 {
				dst[mby*mbCols+mbx] = 0
				continue
			}
			mean := sum / n
			var dev uint32
			for y := y0; y < y1; y++ {
				row := plane[y*stride+x0 : y*stride+x1]
				for _, v := range row {
					if uint32(v) >= mean {
						dev += uint32(v) - mean
					} else {
						dev += mean - uint32(v)
					}
				}
			}
			if dev < threshold {
				dst[mby*mbCols+mbx] = 1
			} else {
				dst[mby*mbCols+mbx] = 0
			}
		}
	}
}
