package encoder

import "testing"

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
