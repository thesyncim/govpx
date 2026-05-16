package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderRejectsInvalidTargetLevel(t *testing.T) {
	for _, level := range []int{-1, 1, 5, 12, 100, 254} {
		opts := VP9EncoderOptions{
			Width:       64,
			Height:      64,
			FPS:         30,
			TargetLevel: level,
		}
		if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("TargetLevel=%d err = %v, want ErrInvalidConfig", level, err)
		}
	}
}

func TestVP9EncoderAcceptsCanonicalTargetLevels(t *testing.T) {
	for _, level := range []int{0, 10, 11, 20, 21, 30, 31, 40, 41, 50, 51, 52, 60, 61, 62, 255} {
		opts := VP9EncoderOptions{
			Width:       64,
			Height:      64,
			FPS:         30,
			TargetLevel: level,
		}
		e, err := NewVP9Encoder(opts)
		if err != nil {
			t.Fatalf("TargetLevel=%d unexpected err: %v", level, err)
		}
		if e.opts.TargetLevel != level {
			t.Fatalf("TargetLevel=%d not stored, got %d", level, e.opts.TargetLevel)
		}
		_ = e.Close()
	}
}

func TestVP9EncoderSetTargetLevelUpdatesOption(t *testing.T) {
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
	if err := e.SetTargetLevel(12); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTargetLevel(12) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTargetLevel(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTargetLevel(-1) err = %v, want ErrInvalidConfig", err)
	}
}
