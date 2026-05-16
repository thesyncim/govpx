package govpx

import (
	"errors"
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9ScreenContentConstants pins the numeric values to libvpx's
// vp9e_tune_content enum so a refactor cannot silently re-number the
// constants without breaking the test.
func TestVP9ScreenContentConstants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  VP9ScreenContent
		want int8
	}{
		{"default", VP9ScreenContentDefault, 0},
		{"screen", VP9ScreenContentScreen, 1},
		{"film", VP9ScreenContentFilm, 2},
	} {
		if int8(tc.got) != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, int8(tc.got), tc.want)
		}
	}
}

// TestVP9EncoderSetScreenContentModeFilm accepts the new film constant
// and rejects values past the supported FILM=2 ceiling.
func TestVP9EncoderSetScreenContentModeFilm(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	if err := e.SetScreenContentMode(int(VP9ScreenContentFilm)); err != nil {
		t.Fatalf("SetScreenContentMode(film): %v", err)
	}
	if e.opts.ScreenContentMode != int8(VP9ScreenContentFilm) {
		t.Fatalf("ScreenContentMode = %d, want %d",
			e.opts.ScreenContentMode, VP9ScreenContentFilm)
	}
	if err := e.SetScreenContentMode(3); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode(3) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetScreenContentMode(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode(-1) err = %v, want ErrInvalidConfig", err)
	}
	// Constructing a fresh encoder with FILM mode is also accepted.
	e2, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 64, Height: 64,
		ScreenContentMode: int8(VP9ScreenContentFilm),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(film): %v", err)
	}
	t.Cleanup(func() { _ = e2.Close() })
}

// TestVP9VarianceAQFilmHighVarianceHeldAtBaseQ confirms that the FILM
// variance-AQ rate-ratio table holds segment 4 at 1:1 so the
// high-variance Q delta is zero. Default-video tuning still pushes
// segment 4's Q up via the 3:4 ratio.
func TestVP9VarianceAQFilmHighVarianceHeldAtBaseQ(t *testing.T) {
	const baseQ = 96
	defaultSeg := vp9VarianceAQSegmentationParams(baseQ, int8(VP9ScreenContentDefault))
	filmSeg := vp9VarianceAQSegmentationParams(baseQ, int8(VP9ScreenContentFilm))

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
	// Low-variance segments keep their negative Q boost under both
	// content modes — film should not disturb the flat-area protection.
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

// TestVP9VarianceAQFilmRatiosTableShape pins the FILM rate-ratio
// table itself so an accidental regression to the default values is
// caught at build time.
func TestVP9VarianceAQFilmRatiosTableShape(t *testing.T) {
	if vp9VarianceAQRateRatiosFilm[4].num != 1 ||
		vp9VarianceAQRateRatiosFilm[4].den != 1 {
		t.Fatalf("FILM rate ratio seg 4 = %d/%d, want 1/1",
			vp9VarianceAQRateRatiosFilm[4].num,
			vp9VarianceAQRateRatiosFilm[4].den)
	}
	if vp9VarianceAQRateRatios[4].num >= vp9VarianceAQRateRatios[4].den {
		t.Fatalf("default rate ratio seg 4 = %d/%d, want num<den",
			vp9VarianceAQRateRatios[4].num,
			vp9VarianceAQRateRatios[4].den)
	}
	// Picker dispatches to the right table.
	got := vp9VarianceAQRateRatiosForContent(int8(VP9ScreenContentFilm))
	if got != vp9VarianceAQRateRatiosFilm {
		t.Fatal("picker did not return FILM table for FILM mode")
	}
	got = vp9VarianceAQRateRatiosForContent(int8(VP9ScreenContentDefault))
	if got != vp9VarianceAQRateRatios {
		t.Fatal("picker did not return default table for DEFAULT mode")
	}
	got = vp9VarianceAQRateRatiosForContent(int8(VP9ScreenContentScreen))
	if got != vp9VarianceAQRateRatios {
		t.Fatal("picker did not return default table for SCREEN mode")
	}
}
