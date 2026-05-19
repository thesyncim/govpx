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

func TestVP9SetMaxIntraBitratePctValidationAndAppliesValue(t *testing.T) {
	var nilEnc *VP9Encoder
	if err := nilEnc.SetMaxIntraBitratePct(100); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil SetMaxIntraBitratePct err = %v, want ErrClosed", err)
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetMaxIntraBitratePct(-1) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.MaxIntraBitratePct != 0 || e.rc.maxIntraBitratePct != 0 {
		t.Fatalf("invalid max-intra mutation opts=%d rc=%d, want both 0",
			e.opts.MaxIntraBitratePct, e.rc.maxIntraBitratePct)
	}
	if err := e.SetMaxIntraBitratePct(175); err != nil {
		t.Fatalf("SetMaxIntraBitratePct(175): %v", err)
	}
	if e.opts.MaxIntraBitratePct != 175 || e.rc.maxIntraBitratePct != 175 {
		t.Fatalf("opts=%d rc=%d, want both 175",
			e.opts.MaxIntraBitratePct, e.rc.maxIntraBitratePct)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(100); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetMaxIntraBitratePct err = %v, want ErrClosed", err)
	}
}

func TestVP9SetGFCBRBoostPctValidationAndAppliesValue(t *testing.T) {
	var nilEnc *VP9Encoder
	if err := nilEnc.SetGFCBRBoostPct(50); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil SetGFCBRBoostPct err = %v, want ErrClosed", err)
	}

	noRC, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder(no RC): %v", err)
	}
	if err := noRC.SetGFCBRBoostPct(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetGFCBRBoostPct(-1) err = %v, want ErrInvalidConfig", err)
	}
	if err := noRC.SetGFCBRBoostPct(25); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetGFCBRBoostPct without CBR err = %v, want ErrInvalidConfig", err)
	}
	if noRC.opts.GFCBRBoostPct != 0 || noRC.rc.gfCBRBoostPct != 0 {
		t.Fatalf("invalid gf-boost mutation opts=%d rc=%d, want both 0",
			noRC.opts.GFCBRBoostPct, noRC.rc.gfCBRBoostPct)
	}
	if err := noRC.SetGFCBRBoostPct(0); err != nil {
		t.Fatalf("SetGFCBRBoostPct(0) without CBR: %v", err)
	}

	cbr, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  700,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(CBR): %v", err)
	}
	if err := cbr.SetGFCBRBoostPct(40); err != nil {
		t.Fatalf("SetGFCBRBoostPct(40): %v", err)
	}
	if cbr.opts.GFCBRBoostPct != 40 || cbr.rc.gfCBRBoostPct != 40 {
		t.Fatalf("opts=%d rc=%d, want both 40",
			cbr.opts.GFCBRBoostPct, cbr.rc.gfCBRBoostPct)
	}
	if err := cbr.SetGFCBRBoostPct(0); err != nil {
		t.Fatalf("SetGFCBRBoostPct(0): %v", err)
	}
	if cbr.opts.GFCBRBoostPct != 0 || cbr.rc.gfCBRBoostPct != 0 {
		t.Fatalf("cleared opts=%d rc=%d, want both 0",
			cbr.opts.GFCBRBoostPct, cbr.rc.gfCBRBoostPct)
	}
	if err := cbr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cbr.SetGFCBRBoostPct(50); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetGFCBRBoostPct err = %v, want ErrClosed", err)
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
