package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderRejectsNegativeMaxInterBitratePct(t *testing.T) {
	if _, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		MaxInterBitratePct: -1,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderMaxInterBitratePctStored(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		MaxInterBitratePct: 200,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.maxInterBitratePct != 200 {
		t.Fatalf("rc.maxInterBitratePct = %d, want 200", e.rc.maxInterBitratePct)
	}
}

func TestVP9SetMaxInterBitratePctRejectsNegative(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxInterBitratePct(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetMaxInterBitratePct(-1) err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9SetMaxInterBitratePctAppliesValue(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxInterBitratePct(150); err != nil {
		t.Fatalf("SetMaxInterBitratePct(150): %v", err)
	}
	if e.opts.MaxInterBitratePct != 150 || e.rc.maxInterBitratePct != 150 {
		t.Fatalf("opts=%d rc=%d, want both 150",
			e.opts.MaxInterBitratePct, e.rc.maxInterBitratePct)
	}
}

func TestVP9MaxInterBoundCapsInterTarget(t *testing.T) {
	rc := &vp9RateControlState{
		bitsPerFrame:       1000,
		maxInterBitratePct: 200,
	}
	if got := rc.applyVP9MaxInterBound(5000); got != 2000 {
		t.Fatalf("max-inter bound = %d, want 2000", got)
	}
	if got := rc.applyVP9MaxInterBound(1500); got != 1500 {
		t.Fatalf("under-cap target = %d, want 1500", got)
	}
	rc.maxInterBitratePct = 0
	if got := rc.applyVP9MaxInterBound(99999); got != 99999 {
		t.Fatalf("zero cap disabled, got %d", got)
	}
}
