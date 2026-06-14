package encoder

import "testing"

func TestAnalyzeFirstPassFrameNoReferenceUsesIntraErrors(t *testing.T) {
	const (
		width    = 32
		height   = 32
		stride   = 32
		frame    = 7
		duration = 3
	)
	src := makeFirstPassPlane(width, height, stride, 80)

	stats := AnalyzeFirstPassFrame(FirstPassFrameAnalysis{
		Width:        width,
		Height:       height,
		Frame:        frame,
		Duration:     duration,
		SourceY:      src,
		SourceStride: stride,
	})
	if stats.Frame != frame || stats.Duration != duration || stats.Count != 1 {
		t.Fatalf("shape = frame %d duration %.0f count %.0f, want %d/%d/1",
			stats.Frame, stats.Duration, stats.Count, frame, duration)
	}
	if stats.PcntInter != 0 || stats.PcntMotion != 0 || stats.PcntSecondRef != 0 {
		t.Fatalf("inter/motion/second-ref = %.3f/%.3f/%.3f, want 0",
			stats.PcntInter, stats.PcntMotion, stats.PcntSecondRef)
	}
	if stats.IntraError == 0 || stats.CodedError != stats.IntraError ||
		stats.SRCodedError != stats.CodedError {
		t.Fatalf("errors = intra %.3f coded %.3f sr %.3f, want same non-zero intra",
			stats.IntraError, stats.CodedError, stats.SRCodedError)
	}
	if stats.PcntIntraLow != 1 || stats.IntraSmoothPct != 1 || stats.PcntIntraHigh != 0 {
		t.Fatalf("intra buckets low/smooth/high = %.3f/%.3f/%.3f, want 1/1/0",
			stats.PcntIntraLow, stats.IntraSmoothPct, stats.PcntIntraHigh)
	}
	if stats.Weight < 0.1 {
		t.Fatalf("weight = %.3f, want at least 0.1", stats.Weight)
	}
}

func TestAnalyzeFirstPassFrameUsesLastReference(t *testing.T) {
	const (
		width  = 32
		height = 32
		stride = 32
	)
	src := makePatternFirstPassPlane(width, height, stride)
	last := makePatternFirstPassPlane(width, height, stride)

	stats := AnalyzeFirstPassFrame(FirstPassFrameAnalysis{
		Width:        width,
		Height:       height,
		SourceY:      src,
		SourceStride: stride,
		HasLast:      true,
		LastY:        last,
		LastStride:   stride,
	})
	if stats.PcntInter != 1 {
		t.Fatalf("PcntInter = %.3f, want 1", stats.PcntInter)
	}
	if stats.PcntMotion != 0 || stats.NewMVCount != 0 {
		t.Fatalf("PcntMotion/NewMVCount = %.3f/%.3f, want 0/0",
			stats.PcntMotion, stats.NewMVCount)
	}
	if stats.CodedError >= stats.IntraError {
		t.Fatalf("coded error %.3f >= intra error %.3f", stats.CodedError, stats.IntraError)
	}
	if stats.SRCodedError != stats.CodedError {
		t.Fatalf("SR coded error %.3f, want coded %.3f",
			stats.SRCodedError, stats.CodedError)
	}
}

func TestAnalyzeFirstPassFrameSkipsNonZeroSearchForFlatZeroMotion(t *testing.T) {
	const (
		width  = 64
		height = 64
		stride = 64
	)
	src := makeFirstPassPlane(width, height, stride, 128)
	last := makeFirstPassPlane(width, height, stride, 128)

	stats := AnalyzeFirstPassFrame(FirstPassFrameAnalysis{
		Width:        width,
		Height:       height,
		SourceY:      src,
		SourceStride: stride,
		HasLast:      true,
		LastY:        last,
		LastStride:   stride,
	})
	if stats.PcntInter != 1 {
		t.Fatalf("PcntInter = %.3f, want 1", stats.PcntInter)
	}
	if stats.PcntMotion != 0 || stats.NewMVCount != 0 {
		t.Fatalf("PcntMotion/NewMVCount = %.3f/%.3f, want 0/0",
			stats.PcntMotion, stats.NewMVCount)
	}
}

func TestAnalyzeFirstPassFrameCountsGoldenReferenceWins(t *testing.T) {
	const (
		width  = 32
		height = 32
		stride = 32
	)
	src := makeFirstPassPlane(width, height, stride, 80)
	last := makeFirstPassPlane(width, height, stride, 200)
	golden := makeFirstPassPlane(width, height, stride, 80)

	stats := AnalyzeFirstPassFrame(FirstPassFrameAnalysis{
		Width:        width,
		Height:       height,
		SourceY:      src,
		SourceStride: stride,
		HasLast:      true,
		LastY:        last,
		LastStride:   stride,
		HasGolden:    true,
		GoldenY:      golden,
		GoldenStride: stride,
	})
	if stats.PcntInter != 0 {
		t.Fatalf("PcntInter = %.3f, want 0", stats.PcntInter)
	}
	if stats.PcntSecondRef != 1 {
		t.Fatalf("PcntSecondRef = %.3f, want 1", stats.PcntSecondRef)
	}
	if stats.SRCodedError >= stats.CodedError {
		t.Fatalf("SR coded error %.3f >= coded error %.3f",
			stats.SRCodedError, stats.CodedError)
	}
}

func TestAnalyzeFirstPassFrameEmptyImageKeepsStatsShape(t *testing.T) {
	stats := AnalyzeFirstPassFrame(FirstPassFrameAnalysis{
		Frame:    11,
		Duration: 13,
	})
	if stats.Frame != 11 || stats.Duration != 13 || stats.Count != 1 {
		t.Fatalf("shape = frame %d duration %.0f count %.0f, want 11/13/1",
			stats.Frame, stats.Duration, stats.Count)
	}
}

func TestFirstPassMotionSearchFindsBestIntegerOffset(t *testing.T) {
	const (
		width  = 32
		height = 32
		stride = 32
		x      = 8
		y      = 8
		w      = 8
		h      = 8
	)
	src := make([]byte, stride*height)
	ref := make([]byte, stride*height)
	for i := range ref {
		ref[i] = 200
	}
	for yy := range h {
		for xx := range w {
			v := byte(20 + xx*3 + yy*5)
			src[(y+yy)*stride+x+xx] = v
			ref[(y-1+yy)*stride+x+2+xx] = v
		}
	}

	best, rowQ3, colQ3 := FirstPassMotionSearch(src, stride, ref, stride,
		x, y, w, h, width, height)
	if best != 0 {
		t.Fatalf("best SSE = %d, want 0", best)
	}
	if rowQ3 != -8 || colQ3 != 16 {
		t.Fatalf("motion = (%d,%d), want (-8,16)", rowQ3, colQ3)
	}
}

func makeFirstPassPlane(width int, height int, stride int, value byte) []byte {
	plane := make([]byte, stride*height)
	for y := range height {
		row := plane[y*stride:]
		for x := range width {
			row[x] = value
		}
	}
	return plane
}

func makePatternFirstPassPlane(width int, height int, stride int) []byte {
	plane := make([]byte, stride*height)
	for y := range height {
		row := plane[y*stride:]
		for x := range width {
			row[x] = byte((x*17 + y*29 + x*y*3 + 41) & 0xff)
		}
	}
	return plane
}

func TestFirstPassMotionAccumulatorFinishesStats(t *testing.T) {
	var acc FirstPassMotionAccumulator
	acc.Add(8, -16, 0, 0, 4, 4)
	acc.Add(0, 0, 1, 1, 4, 4)
	acc.Add(24, 8, 3, 3, 4, 4)

	var stats FirstPassFrameStats
	acc.Finish(&stats, 16)
	if stats.MVr != 16 || stats.MVc != -4 {
		t.Fatalf("mean motion row/col = %.2f/%.2f, want 16/-4",
			stats.MVr, stats.MVc)
	}
	if stats.MVrAbs != 16 || stats.MVcAbs != 12 {
		t.Fatalf("mean abs motion row/col = %.2f/%.2f, want 16/12",
			stats.MVrAbs, stats.MVcAbs)
	}
	if stats.MVInOutCount != 0.5 {
		t.Fatalf("MVInOutCount = %.3f, want 0.5", stats.MVInOutCount)
	}
	if stats.PcntMotion != 0.125 || stats.NewMVCount != 0.125 {
		t.Fatalf("PcntMotion/NewMVCount = %.3f/%.3f, want 0.125/0.125",
			stats.PcntMotion, stats.NewMVCount)
	}
}
