package encoder

const (
	// FirstPassIntraPenalty mirrors libvpx vp9_firstpass.c
	// INTRA_MODE_PENALTY (=1024 LL).
	FirstPassIntraPenalty = 1024
	// FirstPassNewMVModePenalty mirrors libvpx vp9_firstpass.c
	// NEW_MV_MODE_PENALTY (=32).
	FirstPassNewMVModePenalty = 32
	// FirstPassSearchRange is the integer-pel search radius used by the
	// current govpx first-pass source analyzer.
	FirstPassSearchRange = 4
	// FirstPassDarkThresh mirrors libvpx vp9_firstpass.c DARK_THRESH (=64).
	FirstPassDarkThresh = 64
)

type FirstPassMotionAccumulator struct {
	sumRow     int64
	sumCol     int64
	sumRowAbs  int64
	sumColAbs  int64
	sumRowSq   int64
	sumColSq   int64
	sumIn      int64
	count      int
	newCount   int
	lastPacked uint32
}

func (a *FirstPassMotionAccumulator) Add(rowQ3 int16, colQ3 int16,
	mbRow int, mbCol int, mbRows int, mbCols int,
) {
	if a == nil || rowQ3 == 0 && colQ3 == 0 {
		return
	}
	row := int32(rowQ3)
	col := int32(colQ3)
	a.sumRow += int64(row)
	a.sumCol += int64(col)
	a.sumRowAbs += int64(abs32(row))
	a.sumColAbs += int64(abs32(col))
	a.sumRowSq += int64(row) * int64(row)
	a.sumColSq += int64(col) * int64(col)
	a.count++

	packed := (uint32(uint16(rowQ3)) << 16) | uint32(uint16(colQ3))
	if packed != a.lastPacked {
		a.newCount++
	}
	a.lastPacked = packed

	if mbRow < mbRows/2 {
		if row > 0 {
			a.sumIn--
		} else if row < 0 {
			a.sumIn++
		}
	} else if mbRow > mbRows/2 {
		if row > 0 {
			a.sumIn++
		} else if row < 0 {
			a.sumIn--
		}
	}
	if mbCol < mbCols/2 {
		if col > 0 {
			a.sumIn--
		} else if col < 0 {
			a.sumIn++
		}
	} else if mbCol > mbCols/2 {
		if col > 0 {
			a.sumIn++
		} else if col < 0 {
			a.sumIn--
		}
	}
}

func (a *FirstPassMotionAccumulator) Finish(stats *FirstPassFrameStats, blocks int) {
	if a == nil || stats == nil || a.count == 0 || blocks <= 0 {
		return
	}
	count := float64(a.count)
	stats.MVr = float64(a.sumRow) / count
	stats.MVrAbs = float64(a.sumRowAbs) / count
	stats.MVc = float64(a.sumCol) / count
	stats.MVcAbs = float64(a.sumColAbs) / count
	sumRow := float64(a.sumRow)
	sumCol := float64(a.sumCol)
	stats.MVrv = (float64(a.sumRowSq) - ((sumRow * sumRow) / count)) / count
	stats.MVcv = (float64(a.sumColSq) - ((sumCol * sumCol) / count)) / count
	stats.MVInOutCount = float64(a.sumIn) / float64(a.count*2)
	stats.PcntMotion = float64(a.count) / float64(blocks)
	stats.NewMVCount = float64(a.newCount) / float64(blocks)
}

func FirstPassMotionSearch(src []byte, srcStride int, ref []byte, refStride int,
	x int, y int, w int, h int, width int, height int,
) (best uint64, bestRowQ3 int16, bestColQ3 int16) {
	best = ^uint64(0)
	for dy := -FirstPassSearchRange; dy <= FirstPassSearchRange; dy++ {
		refY := y + dy
		if refY < 0 || refY+h > height {
			continue
		}
		for dx := -FirstPassSearchRange; dx <= FirstPassSearchRange; dx++ {
			refX := x + dx
			if refX < 0 || refX+w > width {
				continue
			}
			err := BlockSSE(src, srcStride, ref, refStride, x, y, refX, refY, w, h)
			if err < best {
				best = err
				bestRowQ3 = int16(dy << 3)
				bestColQ3 = int16(dx << 3)
			}
		}
	}
	return best, bestRowQ3, bestColQ3
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
