package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderRejectsOversizedFrameForTargetLevel(t *testing.T) {
	// Level 3.1 caps the picture size at 983040 luma samples (1280x720)
	// and the sample rate at 36864000 (1280x720@30). A 4K source at 60
	// fps exceeds both limits.
	opts := VP9EncoderOptions{
		Width:       3840,
		Height:      2160,
		FPS:         60,
		TargetLevel: 31,
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("4K@60 with TargetLevel=31 err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderAcceptsFrameWithinTargetLevel(t *testing.T) {
	// 720p@30 fits inside level 3.1's 36864000 sample/sec, 983040 luma
	// picture-size budget.
	opts := VP9EncoderOptions{
		Width:       1280,
		Height:      720,
		FPS:         30,
		TargetLevel: 31,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("720p@30 with TargetLevel=31 err = %v, want nil", err)
	}
	_ = e.Close()
}

func TestVP9EncoderRejectsExcessiveBitrateForTargetLevel(t *testing.T) {
	// Level 3 caps target_bitrate at 7200 kbps; 30000 must be rejected.
	opts := VP9EncoderOptions{
		Width:              640,
		Height:             480,
		FPS:                30,
		TargetLevel:        30,
		TargetBitrateKbps:  30000,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("excessive bitrate err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderRejectsExcessiveFrameRateForTargetLevel(t *testing.T) {
	// Level 4 caps the luma sample-rate at 83558400 samples/sec (~1080p@40).
	// 1080p@60 = 124416000 > limit.
	opts := VP9EncoderOptions{
		Width:       1920,
		Height:      1080,
		FPS:         60,
		TargetLevel: 40,
	}
	if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("1080p@60 with TargetLevel=40 err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderTargetLevelAutoSkipsLimitCheck(t *testing.T) {
	opts := VP9EncoderOptions{
		Width:       3840,
		Height:      2160,
		FPS:         60,
		TargetLevel: 0, // auto
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("4K@60 with TargetLevel=auto err = %v, want nil", err)
	}
	_ = e.Close()
}

func TestVP9EncoderTargetLevelMaxSkipsLimitCheck(t *testing.T) {
	opts := VP9EncoderOptions{
		Width:       3840,
		Height:      2160,
		FPS:         60,
		TargetLevel: 255, // unconstrained
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("4K@60 with TargetLevel=255 err = %v, want nil", err)
	}
	_ = e.Close()
}

func TestVP9EncoderSetTargetLevelRejectsExceedingConfiguration(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  3840,
		Height: 2160,
		FPS:    60,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	// Level 4 caps both luma-sample-rate and picture-size; 4K@60 violates
	// both. SetTargetLevel must reject without mutating the option.
	if err := e.SetTargetLevel(40); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTargetLevel(40) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.TargetLevel != 0 {
		t.Fatalf("opts.TargetLevel = %d, want 0 after rejected setter",
			e.opts.TargetLevel)
	}
	// A level that accommodates the source must still be accepted.
	if err := e.SetTargetLevel(62); err != nil {
		t.Fatalf("SetTargetLevel(62) err = %v, want nil", err)
	}
	if e.opts.TargetLevel != 62 {
		t.Fatalf("opts.TargetLevel = %d, want 62", e.opts.TargetLevel)
	}
}


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
