package analysis

import (
	_ "unsafe" // for go:linkname
)

// nanotime is linked to runtime.nanotime so the observer can report
// AnalysisTimeNS without paying time.Now()'s per-call wall-clock cost.
// It is only called when Config.CollectComplexity is true, keeping the
// minimal observation path clock-read free.
//
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// cpuObserveAnalyzer is the CPU-only observation analyzer. It computes
// the statistics requested by its [Config] and writes them into the
// caller-owned [FrameAnalysis]. By construction it must never feed any
// value back into the VP8 encoder; the encoder hook discards the
// observation result for any byte-parity sensitive decision.
//
// The analyzer caches the previous source luma plane in prevY so it
// can compute zero-MV SAD without consulting encoder reconstruction
// buffers. The cache is grown on demand and never shrunk, so
// steady-state encoding does not allocate.
type cpuObserveAnalyzer struct {
	cfg Config

	prevY      []byte
	prevW      int
	prevH      int
	prevStride int
	prevValid  bool
}

func newCPUObserveAnalyzer(cfg Config) *cpuObserveAnalyzer {
	return &cpuObserveAnalyzer{cfg: cfg}
}

// Observe inspects the source frame and writes per-macroblock results
// into out. The function operates only on the luma plane; chroma is
// reserved for a future revision.
func (a *cpuObserveAnalyzer) Observe(in *FrameInput, out *FrameAnalysis) {
	if in == nil || out == nil {
		return
	}
	var start int64
	if a.cfg.CollectComplexity {
		start = nanotime()
	}

	mbCols := (in.Width + 15) >> 4
	mbRows := (in.Height + 15) >> 4
	mbCount := mbCols * mbRows
	out.Width = in.Width
	out.Height = in.Height
	out.MBCols = mbCols
	out.MBRows = mbRows
	out.FrameIndex = in.FrameIndex
	out.KeyFrame = in.KeyFrame
	out.Observed = true
	out.ensureMBCapacity(mbCount)
	out.Stats = AnalysisStats{BlocksTotal: mbCount}

	// Decide whether motion features are usable for this frame:
	// keyframes skip ZeroSAD (no comparable previous frame), and the
	// very first observation also skips it.
	canCompareToPrev := a.cfg.CollectMotionHints &&
		a.prevValid && !in.KeyFrame &&
		a.prevW == in.Width && a.prevH == in.Height &&
		a.prevStride == in.YStride

	for r := range mbRows {
		for c := range mbCols {
			mb := &out.MB[r*mbCols+c]
			mb.MBX = int16(c)
			mb.MBY = int16(r)
			mb.BestMVX = 0
			mb.BestMVY = 0
			mb.Flags = 0
			mb.SearchRadius = 0

			x0 := c * 16
			y0 := r * 16
			x1 := min(x0+16, in.Width)
			y1 := min(y0+16, in.Height)

			if a.cfg.CollectComplexity {
				v, t := computeMBVarianceAndTexture(in.Y, in.YStride, x0, y0, x1, y1)
				mb.Variance = v
				mb.Texture = t
				if v < flatVarianceThreshold {
					mb.Flags |= FlagFlat
					out.Stats.BlocksFlat++
				}
				if t > highTextureThreshold {
					mb.Flags |= FlagHighTexture
				}
			}

			if canCompareToPrev {
				sad := computeMBSAD(in.Y, a.prevY, in.YStride, a.prevStride, x0, y0, x1, y1)
				mb.ZeroSAD = sad
				mb.BestSAD = sad
				if sad <= staticSADThreshold {
					mb.Flags |= FlagStatic
					out.Stats.BlocksStatic++
					mb.SearchRadius = 1
				} else if sad >= highMotionSADThreshold {
					mb.Flags |= FlagHighMotion
					out.Stats.BlocksHighMotion++
					mb.SearchRadius = 8
				} else {
					mb.SearchRadius = 4
				}
				// StaticScore is min(255, sad/4).
				score := min(sad>>2, 255)
				mb.StaticScore = uint16(score)
			} else {
				mb.ZeroSAD = 0
				mb.BestSAD = 0
				mb.StaticScore = 0
				mb.SearchRadius = 0
			}

			if a.cfg.CollectSkipMap &&
				mb.Flags&FlagStatic != 0 && mb.Flags&FlagFlat != 0 {
				mb.Flags |= FlagSkipLikely
				out.Stats.BlocksSkipLikely++
			}
		}
	}

	// Cache the current luma plane so the next frame can compute
	// ZeroSAD. We copy because the caller-owned source memory may be
	// recycled before the next Observe call.
	if a.cfg.CollectMotionHints {
		a.cachePrevLuma(in)
	} else {
		a.prevValid = false
	}

	if a.cfg.CollectComplexity {
		out.Stats.AnalysisTimeNS = nanotime() - start
	}
}

func (a *cpuObserveAnalyzer) Mode() VP8AnalysisMode { return VP8AnalysisObserveCPU }

func (a *cpuObserveAnalyzer) Close() error {
	a.prevY = nil
	a.prevValid = false
	return nil
}

func (a *cpuObserveAnalyzer) cachePrevLuma(in *FrameInput) {
	needed := in.Height * in.YStride
	if cap(a.prevY) < needed {
		a.prevY = make([]byte, needed)
	} else {
		a.prevY = a.prevY[:needed]
	}
	copy(a.prevY, in.Y[:needed])
	a.prevW = in.Width
	a.prevH = in.Height
	a.prevStride = in.YStride
	a.prevValid = true
}

// computeMBSAD returns the sum of absolute differences between the
// luma rectangle [x0,y0)-[x1,y1) of cur and the same rectangle of
// prev. The two planes may have different strides but must cover the
// same pixel rectangle.
func computeMBSAD(cur, prev []byte, curStride, prevStride, x0, y0, x1, y1 int) uint32 {
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	var sad uint32
	w := x1 - x0
	for y := y0; y < y1; y++ {
		curRow := cur[y*curStride+x0 : y*curStride+x0+w]
		prevRow := prev[y*prevStride+x0 : y*prevStride+x0+w]
		for i := range w {
			a := int(curRow[i])
			b := int(prevRow[i])
			d := a - b
			if d < 0 {
				d = -d
			}
			sad += uint32(d)
		}
	}
	return sad
}

// computeMBVarianceAndTexture returns a 16x16 macroblock's sum of
// absolute deviations from the block mean (variance proxy) and a
// horizontal 3-tap edge energy proxy. Both are read-only over the
// caller's Y buffer.
func computeMBVarianceAndTexture(plane []byte, stride, x0, y0, x1, y1 int) (uint32, uint16) {
	if x1 <= x0 || y1 <= y0 {
		return 0, 0
	}
	w := x1 - x0
	h := y1 - y0

	var sum uint32
	for y := y0; y < y1; y++ {
		row := plane[y*stride+x0 : y*stride+x0+w]
		for _, v := range row {
			sum += uint32(v)
		}
	}
	n := uint32(w * h)
	mean := sum / n
	var dev uint32
	for y := y0; y < y1; y++ {
		row := plane[y*stride+x0 : y*stride+x0+w]
		for _, v := range row {
			if uint32(v) >= mean {
				dev += uint32(v) - mean
			} else {
				dev += mean - uint32(v)
			}
		}
	}

	var texture uint32
	if w >= 3 {
		for y := y0; y < y1; y += 2 {
			row := plane[y*stride+x0 : y*stride+x0+w]
			for x := 1; x < w-1; x++ {
				left := int32(row[x-1])
				center := int32(row[x])
				right := int32(row[x+1])
				delta := left - 2*center + right
				if delta < 0 {
					delta = -delta
				}
				texture += uint32(delta)
			}
		}
	}
	if texture > 0xFFFF {
		texture = 0xFFFF
	}
	return dev, uint16(texture)
}
