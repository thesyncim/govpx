package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9EncoderRejectsNegativeMinGFInterval(t *testing.T) {
	if _, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:         64,
		Height:        64,
		FPS:           30,
		MinGFInterval: -1,
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRejectsNegativeMaxGFInterval(t *testing.T) {
	if _, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:         64,
		Height:        64,
		FPS:           30,
		MaxGFInterval: -1,
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRejectsOversizedGFInterval(t *testing.T) {
	if _, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:         64,
		Height:        64,
		FPS:           30,
		MaxGFInterval: 17,
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRejectsInvertedGFIntervalBounds(t *testing.T) {
	if _, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:         64,
		Height:        64,
		FPS:           30,
		MinGFInterval: 12,
		MaxGFInterval: 8,
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9SetMinGFIntervalRejectsInvertedBounds(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:         64,
		Height:        64,
		FPS:           30,
		MaxGFInterval: 6,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMinGFInterval(10); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetMinGFInterval(10) err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9SetMaxGFIntervalRejectsOversized(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxGFInterval(17); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetMaxGFInterval(>max) err = %v, want ErrInvalidConfig", err)
	}
}
