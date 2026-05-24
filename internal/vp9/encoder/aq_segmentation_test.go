package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVarianceAQFilmHighVarianceHeldAtBaseQ(t *testing.T) {
	const baseQ = 96
	defaultSeg := VarianceAQSegmentationParams(baseQ, false)
	filmSeg := VarianceAQSegmentationParams(baseQ, true)

	const hiVarSeg = 4
	defaultDelta := defaultSeg.FeatureData[hiVarSeg][vp9dec.SegLvlAltQ]
	defaultMask := defaultSeg.FeatureMask[hiVarSeg] & (1 << uint(vp9dec.SegLvlAltQ))
	if defaultMask == 0 || defaultDelta <= 0 {
		t.Fatalf("default-content seg %d altQ delta = %d (mask=%d), want positive",
			hiVarSeg, defaultDelta, defaultMask)
	}
	filmDelta := filmSeg.FeatureData[hiVarSeg][vp9dec.SegLvlAltQ]
	filmMask := filmSeg.FeatureMask[hiVarSeg] & (1 << uint(vp9dec.SegLvlAltQ))
	if filmMask != 0 || filmDelta != 0 {
		t.Fatalf("film-content seg %d altQ delta = %d (mask=%d), want zero",
			hiVarSeg, filmDelta, filmMask)
	}
	for _, seg := range []int{0, 1, 2} {
		if defaultSeg.FeatureData[seg][vp9dec.SegLvlAltQ] !=
			filmSeg.FeatureData[seg][vp9dec.SegLvlAltQ] {
			t.Errorf("seg %d altQ delta diverged: default=%d film=%d",
				seg,
				defaultSeg.FeatureData[seg][vp9dec.SegLvlAltQ],
				filmSeg.FeatureData[seg][vp9dec.SegLvlAltQ])
		}
	}
}

func TestVarianceAQFilmRatiosTableShape(t *testing.T) {
	if varianceAQRateRatiosFilm[4].num != 1 ||
		varianceAQRateRatiosFilm[4].den != 1 {
		t.Fatalf("FILM rate ratio seg 4 = %d/%d, want 1/1",
			varianceAQRateRatiosFilm[4].num,
			varianceAQRateRatiosFilm[4].den)
	}
	if varianceAQRateRatios[4].num >= varianceAQRateRatios[4].den {
		t.Fatalf("default rate ratio seg 4 = %d/%d, want num<den",
			varianceAQRateRatios[4].num,
			varianceAQRateRatios[4].den)
	}
	if got := varianceAQRateRatiosForContent(true); got != varianceAQRateRatiosFilm {
		t.Fatal("picker did not return FILM table for FILM mode")
	}
	if got := varianceAQRateRatiosForContent(false); got != varianceAQRateRatios {
		t.Fatal("picker did not return default table for non-FILM mode")
	}
}

func TestComplexityAQSegmentIDRejectsEmptyTarget(t *testing.T) {
	if _, ok := ComplexityAQSegmentID(0, 0, 0, 96); ok {
		t.Fatal("ComplexityAQSegmentID accepted an empty target rate")
	}
}
