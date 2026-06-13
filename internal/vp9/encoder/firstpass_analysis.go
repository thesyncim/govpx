package encoder

import "math"

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
	// FirstPassNZMotionPenalty mirrors libvpx vp9_firstpass.c
	// NZ_MOTION_PENALTY (=128). libvpx skips the nonzero-MV search when
	// the raw zero-motion prediction error is already this small.
	FirstPassNZMotionPenalty = 128
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

// FirstPassFrameAnalysis carries the source and optional reference planes for
// one VP9 first-pass frame-analysis row.
type FirstPassFrameAnalysis struct {
	Width  int
	Height int

	Frame    uint64
	Duration uint64

	SourceY      []byte
	SourceStride int

	HasLast    bool
	LastY      []byte
	LastStride int

	HasGolden    bool
	GoldenY      []byte
	GoldenStride int
}

// AnalyzeFirstPassFrame computes a libvpx-shaped VP9 FIRSTPASS_STATS row for
// one source frame.
func AnalyzeFirstPassFrame(a FirstPassFrameAnalysis) FirstPassFrameStats {
	mbCols := (a.Width + 15) >> 4
	mbRows := (a.Height + 15) >> 4
	mbs := mbCols * mbRows
	if mbs <= 0 {
		return FirstPassFrameStats{
			Frame:    a.Frame,
			Duration: float64(a.Duration),
			Count:    1,
		}
	}

	intraError := uint64(0)
	codedError := uint64(0)
	srCodedError := uint64(0)
	interCount := 0
	secondRefCount := 0
	neutralCount := 0
	intraLowCount := 0
	intraHighCount := 0
	intraSmoothCount := 0
	intraFactor := 0.0
	brightnessFactor := 0.0
	var motion FirstPassMotionAccumulator

	for mbRow := range mbRows {
		for mbCol := range mbCols {
			x := mbCol << 4
			y := mbRow << 4
			w := min(16, a.Width-x)
			h := min(16, a.Height-y)
			if w <= 0 || h <= 0 {
				continue
			}
			intraRaw := BlockSourceVariance128(a.SourceY, a.SourceStride, x, y, w, h)
			logIntra := math.Log(float64(intraRaw) + 1.0)
			if logIntra < 10.0 {
				intraFactor += 1.0 + ((10.0 - logIntra) * 0.05)
			} else {
				intraFactor += 1.0
			}
			if a.SourceY[y*a.SourceStride+x] < FirstPassDarkThresh && logIntra < 9.0 {
				brightnessFactor += 1.0 +
					0.01*float64(FirstPassDarkThresh-a.SourceY[y*a.SourceStride+x])
			} else {
				brightnessFactor += 1.0
			}
			intra := intraRaw + FirstPassIntraPenalty
			intraError += intra
			thisErr := intra
			bestRow := int16(0)
			bestCol := int16(0)
			lastErr := ^uint64(0)

			if a.HasLast {
				bestErr := BlockSSE(a.SourceY, a.SourceStride,
					a.LastY, a.LastStride, x, y, x, y, w, h)
				rowQ3, colQ3 := int16(0), int16(0)
				if bestErr > FirstPassNZMotionPenalty {
					bestErr, rowQ3, colQ3 = FirstPassMotionSearch(a.SourceY, a.SourceStride,
						a.LastY, a.LastStride, x, y, w, h, a.Width, a.Height)
					if rowQ3 != 0 || colQ3 != 0 {
						bestErr += FirstPassNewMVModePenalty
					}
				}
				lastErr = bestErr
				if bestErr <= thisErr {
					if ((intra-FirstPassIntraPenalty)*9 <= bestErr*10) &&
						intra < 2*FirstPassIntraPenalty {
						neutralCount++
					}
					thisErr = bestErr
					bestRow = rowQ3
					bestCol = colQ3
					interCount++
					motion.Add(rowQ3, colQ3, mbRow, mbCol, mbRows, mbCols)
				}
			}
			if a.HasGolden {
				gfErr, _, _ := FirstPassMotionSearch(a.SourceY, a.SourceStride,
					a.GoldenY, a.GoldenStride, x, y, w, h, a.Width, a.Height)
				srCodedError += gfErr
				if gfErr < lastErr && gfErr < intra {
					secondRefCount++
				}
			} else {
				srCodedError += thisErr
			}
			if bestRow == 0 && bestCol == 0 && thisErr == intra {
				if intraRaw < 16 {
					intraSmoothCount++
				}
				if intraRaw < 512 {
					intraLowCount++
				} else {
					intraHighCount++
				}
			}
			codedError += thisErr
		}
	}

	mbsF := float64(mbs)
	minErr := 200 * math.Sqrt(mbsF)
	stats := FirstPassFrameStats{
		Frame:            a.Frame,
		IntraError:       (float64(intraError>>8) + minErr) / mbsF,
		CodedError:       (float64(codedError>>8) + minErr) / mbsF,
		SRCodedError:     (float64(srCodedError>>8) + minErr) / mbsF,
		PcntInter:        float64(interCount) / mbsF,
		PcntSecondRef:    float64(secondRefCount) / mbsF,
		PcntNeutral:      float64(neutralCount) / mbsF,
		PcntIntraLow:     float64(intraLowCount) / mbsF,
		PcntIntraHigh:    float64(intraHighCount) / mbsF,
		IntraSmoothPct:   float64(intraSmoothCount) / mbsF,
		InactiveZoneRows: 0,
		InactiveZoneCols: 0,
		Duration:         float64(a.Duration),
		Count:            1,
		SpatialLayerID:   0,
	}
	stats.Weight = (intraFactor / mbsF) * (brightnessFactor / mbsF)
	if stats.Weight < 0.1 {
		stats.Weight = 0.1
	}
	motion.Finish(&stats, mbs)
	return stats
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
