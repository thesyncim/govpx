package govpx

import (
	"image"
	"math"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// libvpx parity references for the VP9 first-pass collector:
//   - vp9/encoder/vp9_firstpass.c:1353 vp9_first_pass (the macro-block-level
//     analysis loop this file paraphrases without a Lagrangian RD path).
//   - vp9/encoder/vp9_firstpass_stats.h:20 FIRSTPASS_STATS (the on-disk
//     packet layout that VP9FirstPassFrameStats mirrors field-for-field).
//   - vp9/encoder/vp9_firstpass.c:759 first_pass_stat_calc (the
//     accumulator-to-FIRSTPASS_STATS finalization step).
//
// The Q3 motion-vector convention, intra penalty, new-MV penalty, and
// search range are libvpx-pinned constants below.
const (
	// libvpx: vp9/encoder/vp9_firstpass.c INTRA_MODE_PENALTY (=1024 LL)
	vp9FirstPassIntraPenalty = 1024
	// libvpx: vp9/encoder/vp9_firstpass.c NEW_MV_MODE_PENALTY (=32)
	vp9FirstPassNewMVModePenalty = 32
	// libvpx: vp9/encoder/vp9_firstpass.c FIRST_PASS_Q (search range
	// derives from speed features but caps at 4 in the lowest-resolution
	// fixture; libvpx defaults to a wider range via the motion search
	// driver — see TODO at vp9_first_pass_motion_search below).
	vp9FirstPassSearchRange = 4
	// libvpx: vp9/encoder/vp9_firstpass.c DARK_THRESH (=64)
	vp9FirstPassDarkThresh = 64
)

// VP9FirstPassFrameStats mirrors libvpx VP9 FIRSTPASS_STATS for one analyzed
// source frame or for the finalized sequence total.
//
// libvpx: vp9/encoder/vp9_firstpass_stats.h:20
type VP9FirstPassFrameStats = encoder.FirstPassFrameStats

// FinalizeVP9FirstPassStats appends the libvpx-style terminal total-stats
// record to per-frame VP9 first-pass stats. If stats is empty or already ends
// in a total row, the input slice is returned unchanged.
func FinalizeVP9FirstPassStats(stats []VP9FirstPassFrameStats) []VP9FirstPassFrameStats {
	return encoder.FinalizeFirstPassStats(stats)
}

// CollectFirstPassStats runs VP9 first-pass source analysis for future
// two-pass VOD planning. The returned row should be accumulated across input
// frames and passed through [FinalizeVP9FirstPassStats].
func (e *VP9Encoder) CollectFirstPassStats(img *image.YCbCr, pts uint64, duration uint64, flags EncodeFlags) (VP9FirstPassFrameStats, error) {
	if e == nil || e.closed {
		return VP9FirstPassFrameStats{}, ErrClosed
	}
	if err := e.validateVP9EncoderSource(img); err != nil {
		return VP9FirstPassFrameStats{}, err
	}
	if err := validateVP9EncodeFlags(flags); err != nil {
		return VP9FirstPassFrameStats{}, err
	}
	_ = pts

	stats := e.computeVP9FirstPassStats(img, duration)
	if e.vp9FirstPassCount > 0 && stats.PcntInter > 0.20 &&
		stats.CodedError > 0 && stats.IntraError/stats.CodedError > 2.0 &&
		vp9FirstPassImageMatches(&e.vp9FirstPassLast, e.opts.Width, e.opts.Height) {
		ensureVP9FirstPassImage(&e.vp9FirstPassGF, e.opts.Width, e.opts.Height)
		copyVP9LookaheadImage(&e.vp9FirstPassGF, &e.vp9FirstPassLast,
			e.opts.Width, e.opts.Height)
	}
	ensureVP9FirstPassImage(&e.vp9FirstPassLast, e.opts.Width, e.opts.Height)
	copyVP9LookaheadImage(&e.vp9FirstPassLast, img, e.opts.Width, e.opts.Height)
	if e.vp9FirstPassCount == 0 {
		ensureVP9FirstPassImage(&e.vp9FirstPassGF, e.opts.Width, e.opts.Height)
		copyVP9LookaheadImage(&e.vp9FirstPassGF, &e.vp9FirstPassLast,
			e.opts.Width, e.opts.Height)
	}
	e.vp9FirstPassCount++
	return stats, nil
}

func (e *VP9Encoder) computeVP9FirstPassStats(img *image.YCbCr, duration uint64) VP9FirstPassFrameStats {
	width := e.opts.Width
	height := e.opts.Height
	mbCols := (width + 15) >> 4
	mbRows := (height + 15) >> 4
	mbs := mbCols * mbRows
	if mbs <= 0 {
		return VP9FirstPassFrameStats{
			Frame:    e.vp9FirstPassCount,
			Duration: float64(duration),
			Count:    1,
		}
	}

	src, srcStride, _, _ := vp9EncoderSourcePlane(img, 0)
	hasLast := e.vp9FirstPassCount > 0 &&
		vp9FirstPassImageMatches(&e.vp9FirstPassLast, width, height)
	hasGF := e.vp9FirstPassCount > 1 &&
		vp9FirstPassImageMatches(&e.vp9FirstPassGF, width, height)
	last, lastStride, _, _ := vp9EncoderSourcePlane(&e.vp9FirstPassLast, 0)
	gf, gfStride, _, _ := vp9EncoderSourcePlane(&e.vp9FirstPassGF, 0)

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
	var motion vp9FirstPassMotionAccumulator

	for mbRow := range mbRows {
		for mbCol := range mbCols {
			x := mbCol << 4
			y := mbRow << 4
			w := min(16, width-x)
			h := min(16, height-y)
			if w <= 0 || h <= 0 {
				continue
			}
			intraRaw := encoder.BlockSourceVariance128(src, srcStride, x, y, w, h)
			logIntra := math.Log(float64(intraRaw) + 1.0)
			if logIntra < 10.0 {
				intraFactor += 1.0 + ((10.0 - logIntra) * 0.05)
			} else {
				intraFactor += 1.0
			}
			if src[y*srcStride+x] < vp9FirstPassDarkThresh && logIntra < 9.0 {
				brightnessFactor += 1.0 +
					0.01*float64(vp9FirstPassDarkThresh-src[y*srcStride+x])
			} else {
				brightnessFactor += 1.0
			}
			intra := intraRaw + vp9FirstPassIntraPenalty
			intraError += intra
			thisErr := intra
			bestRow := int16(0)
			bestCol := int16(0)
			lastErr := ^uint64(0)

			if hasLast {
				bestErr, rowQ3, colQ3 := vp9FirstPassMotionSearch(src, srcStride,
					last, lastStride, x, y, w, h, width, height)
				if rowQ3 != 0 || colQ3 != 0 {
					bestErr += vp9FirstPassNewMVModePenalty
				}
				lastErr = bestErr
				if bestErr <= thisErr {
					if ((intra-vp9FirstPassIntraPenalty)*9 <= bestErr*10) &&
						intra < 2*vp9FirstPassIntraPenalty {
						neutralCount++
					}
					thisErr = bestErr
					bestRow = rowQ3
					bestCol = colQ3
					interCount++
					motion.add(rowQ3, colQ3, mbRow, mbCol, mbRows, mbCols)
				}
			}
			if hasGF {
				gfErr, _, _ := vp9FirstPassMotionSearch(src, srcStride, gf,
					gfStride, x, y, w, h, width, height)
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
	stats := VP9FirstPassFrameStats{
		Frame:            e.vp9FirstPassCount,
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
		Duration:         float64(duration),
		Count:            1,
		SpatialLayerID:   0,
	}
	stats.Weight = (intraFactor / mbsF) * (brightnessFactor / mbsF)
	if stats.Weight < 0.1 {
		stats.Weight = 0.1
	}
	motion.finish(&stats, mbs)
	return stats
}

type vp9FirstPassMotionAccumulator struct {
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

func (a *vp9FirstPassMotionAccumulator) add(rowQ3 int16, colQ3 int16, mbRow int, mbCol int, mbRows int, mbCols int) {
	if rowQ3 == 0 && colQ3 == 0 {
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

func (a *vp9FirstPassMotionAccumulator) finish(stats *VP9FirstPassFrameStats, blocks int) {
	if stats == nil || a.count == 0 || blocks <= 0 {
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

func vp9FirstPassMotionSearch(src []byte, srcStride int, ref []byte, refStride int,
	x int, y int, w int, h int, width int, height int,
) (best uint64, bestRowQ3 int16, bestColQ3 int16) {
	best = ^uint64(0)
	for dy := -vp9FirstPassSearchRange; dy <= vp9FirstPassSearchRange; dy++ {
		refY := y + dy
		if refY < 0 || refY+h > height {
			continue
		}
		for dx := -vp9FirstPassSearchRange; dx <= vp9FirstPassSearchRange; dx++ {
			refX := x + dx
			if refX < 0 || refX+w > width {
				continue
			}
			err := encoder.BlockSSE(src, srcStride, ref, refStride,
				x, y, refX, refY, w, h)
			if err < best {
				best = err
				bestRowQ3 = int16(dy << 3)
				bestColQ3 = int16(dx << 3)
			}
		}
	}
	return best, bestRowQ3, bestColQ3
}

func ensureVP9FirstPassImage(img *image.YCbCr, width int, height int) {
	if vp9FirstPassImageMatches(img, width, height) {
		return
	}
	*img = *image.NewYCbCr(image.Rect(0, 0, width, height),
		image.YCbCrSubsampleRatio420)
}

func vp9FirstPassImageMatches(img *image.YCbCr, width int, height int) bool {
	if img == nil || img.Rect.Dx() != width || img.Rect.Dy() != height ||
		img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return false
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return img.YStride >= width && img.CStride >= uvWidth &&
		len(img.Y) >= ycbcrPlaneLen(img.YStride, width, height) &&
		len(img.Cb) >= ycbcrPlaneLen(img.CStride, uvWidth, uvHeight) &&
		len(img.Cr) >= ycbcrPlaneLen(img.CStride, uvWidth, uvHeight)
}
