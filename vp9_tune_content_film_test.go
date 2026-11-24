package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

// TestVP9ScreenContentConstants pins the numeric values to libvpx's
// vp9e_tune_content enum so a refactor cannot silently re-number the
// constants without breaking the test.
func TestVP9ScreenContentConstants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  govpx.VP9ScreenContent
		want int8
	}{
		{"default", govpx.VP9ScreenContentDefault, 0},
		{"screen", govpx.VP9ScreenContentScreen, 1},
		{"film", govpx.VP9ScreenContentFilm, 2},
	} {
		if int8(tc.got) != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, int8(tc.got), tc.want)
		}
	}
}

// TestVP9EncoderSetScreenContentModeFilm accepts the new film constant
// and rejects values past the supported FILM=2 ceiling.
func TestVP9EncoderSetScreenContentModeFilm(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	if err := e.SetScreenContentMode(int(govpx.VP9ScreenContentFilm)); err != nil {
		t.Fatalf("SetScreenContentMode(film): %v", err)
	}
	if err := e.SetScreenContentMode(3); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode(3) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetScreenContentMode(-1); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode(-1) err = %v, want ErrInvalidConfig", err)
	}
	// Constructing a fresh encoder with FILM mode is also accepted.
	e2, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: 64, Height: 64,
		ScreenContentMode: int8(govpx.VP9ScreenContentFilm),
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(film): %v", err)
	}
	t.Cleanup(func() { _ = e2.Close() })
}
