package encoder

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

func VarianceAQSegmentationParams(baseQIndex int, filmContent bool) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initSegmentationProbDefaults(&seg)
	ratios := varianceAQRateRatiosForContent(filmContent)
	for i, ratio := range ratios {
		if ratio.num == ratio.den {
			continue
		}
		delta := ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}

// varianceAQRateRatiosForContent returns the per-segment rate ratios used to
// derive variance-AQ Q deltas. Default video uses libvpx's table where the
// highest-variance segment (index 4) is pushed up in Q by a 3:4 ratio. Film
// content clamps that segment back to 1:1, preserving grain texture.
func varianceAQRateRatiosForContent(filmContent bool) [vp9dec.MaxSegments]struct {
	num int
	den int
} {
	if filmContent {
		return varianceAQRateRatiosFilm
	}
	return varianceAQRateRatios
}

var varianceAQRateRatios = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{5, 2},
	{2, 1},
	{3, 2},
	{1, 1},
	{3, 4},
	{1, 1},
	{1, 1},
	{1, 1},
}

// varianceAQRateRatiosFilm is the FILM-content variant of
// varianceAQRateRatios. Segments 0..2 keep their low-variance Q boost so flat
// areas are still coded cleanly; segment 4 is held at 1:1 instead of 3:4 so
// the encoder leaves high-variance grain blocks at the base Q.
var varianceAQRateRatiosFilm = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{5, 2},
	{2, 1},
	{3, 2},
	{1, 1},
	{1, 1},
	{1, 1},
	{1, 1},
	{1, 1},
}

const (
	complexityAQSegments          = 5
	ComplexityAQDefaultSegment    = 3
	complexityAQStrengths         = 3
	ComplexityAQMinSB64TargetRate = 256
	complexityAQLowVarThreshold   = 10.0
)

var complexityAQRateRatios = [complexityAQStrengths][complexityAQSegments]struct {
	num int
	den int
}{
	{{7, 4}, {5, 4}, {21, 20}, {1, 1}, {9, 10}},
	{{2, 1}, {3, 2}, {23, 20}, {1, 1}, {17, 20}},
	{{5, 2}, {7, 4}, {5, 4}, {1, 1}, {4, 5}},
}

var complexityAQTransitions = [complexityAQStrengths][complexityAQSegments]struct {
	num int
	den int
}{
	{{15, 100}, {30, 100}, {55, 100}, {2, 1}, {100, 1}},
	{{20, 100}, {40, 100}, {65, 100}, {2, 1}, {100, 1}},
	{{25, 100}, {50, 100}, {75, 100}, {2, 1}, {100, 1}},
}

var complexityAQVarThresholds = [complexityAQStrengths][complexityAQSegments]float64{
	{-4.0, -3.0, -2.0, 100.0, 100.0},
	{-3.5, -2.5, -1.5, 100.0, 100.0},
	{-3.0, -2.0, -1.0, 100.0, 100.0},
}

func ComplexityAQSegmentationParams(baseQIndex int) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initSegmentationProbDefaults(&seg)
	strength := ComplexityAQStrength(baseQIndex)
	for i, ratio := range complexityAQRateRatios[strength] {
		if i == ComplexityAQDefaultSegment || ratio.num == ratio.den {
			continue
		}
		delta := ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if baseQIndex+delta <= 0 {
			continue
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}

func ComplexityAQStrength(baseQIndex int) int {
	baseQuant := int(vp9dec.VpxAcQuant(baseQIndex, 0, vp9dec.BitDepth8)) / 4
	strength := 0
	if baseQuant > 10 {
		strength++
	}
	if baseQuant > 25 {
		strength++
	}
	return strength
}

func ComplexityAQSegmentID(logVar float64, projectedRate, targetRate, qindex int) (uint8, bool) {
	if targetRate <= 0 {
		return 0, false
	}
	if projectedRate < 0 {
		projectedRate = 0
	}
	strength := ComplexityAQStrength(qindex)
	for i, transition := range complexityAQTransitions[strength] {
		if int64(projectedRate)*int64(transition.den) <
			int64(targetRate)*int64(transition.num) &&
			logVar < complexityAQLowVarThreshold+
				complexityAQVarThresholds[strength][i] {
			return uint8(i), true
		}
	}
	return complexityAQSegments - 1, true
}

func VarianceAQSegmentIDFromEnergy(energy int) uint8 {
	switch {
	case energy <= -4:
		return 0
	case energy <= -3:
		return 1
	case energy <= -2:
		return 1
	case energy <= -1:
		return 2
	case energy <= 0:
		return 3
	default:
		return 4
	}
}
