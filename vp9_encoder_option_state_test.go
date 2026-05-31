package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderSetTuningMutatesOptionState(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 16, Height: 16, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}
	if err := e.SetTuning(Tuning(2)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTuning invalid error = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Tuning != TunePSNR {
		t.Fatalf("invalid SetTuning changed tuning to %d", e.opts.Tuning)
	}
	if err := e.SetTuning(TuneSSIM); err != nil {
		t.Fatalf("SetTuning(TuneSSIM) returned error: %v", err)
	}
	if e.opts.Tuning != TuneSSIM {
		t.Fatalf("Tuning = %d, want TuneSSIM", e.opts.Tuning)
	}
	if err := e.SetTuning(TunePSNR); err != nil {
		t.Fatalf("SetTuning(TunePSNR) returned error: %v", err)
	}
	if e.opts.Tuning != TunePSNR {
		t.Fatalf("Tuning = %d, want TunePSNR", e.opts.Tuning)
	}
}

func TestVP9EncoderSetScreenContentModeMutatesOptionState(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetScreenContentMode(int(VP9ScreenContentFilm)); err != nil {
		t.Fatalf("SetScreenContentMode(film): %v", err)
	}
	if e.opts.ScreenContentMode != int8(VP9ScreenContentFilm) {
		t.Fatalf("ScreenContentMode = %d, want %d",
			e.opts.ScreenContentMode, VP9ScreenContentFilm)
	}
}

func TestVP9EncoderTargetLevelStoredByConstructorAndSetter(t *testing.T) {
	for _, level := range []int{0, 10, 11, 20, 21, 30, 31, 40, 41, 50, 51, 52, 60, 61, 62, 255} {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:       64,
			Height:      64,
			FPS:         30,
			TargetLevel: level,
		})
		if err != nil {
			t.Fatalf("TargetLevel=%d unexpected err: %v", level, err)
		}
		if e.opts.TargetLevel != level {
			t.Fatalf("TargetLevel=%d not stored, got %d", level,
				e.opts.TargetLevel)
		}
		_ = e.Close()
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetTargetLevel(31); err != nil {
		t.Fatalf("SetTargetLevel(31): %v", err)
	}
	if e.opts.TargetLevel != 31 {
		t.Fatalf("opts.TargetLevel = %d, want 31", e.opts.TargetLevel)
	}
	if err := e.SetTargetLevel(255); err != nil {
		t.Fatalf("SetTargetLevel(255): %v", err)
	}
	if e.opts.TargetLevel != 255 {
		t.Fatalf("opts.TargetLevel = %d, want 255", e.opts.TargetLevel)
	}
}

func TestVP9EncoderRejectedTargetLevelDoesNotMutateOptionState(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  3840,
		Height: 2160,
		FPS:    60,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetTargetLevel(40); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTargetLevel(40) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.TargetLevel != 0 {
		t.Fatalf("opts.TargetLevel = %d, want 0 after rejected setter",
			e.opts.TargetLevel)
	}
	if err := e.SetTargetLevel(62); err != nil {
		t.Fatalf("SetTargetLevel(62) err = %v, want nil", err)
	}
	if e.opts.TargetLevel != 62 {
		t.Fatalf("opts.TargetLevel = %d, want 62", e.opts.TargetLevel)
	}
}
