package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

func TestVP9EncoderRejectsNegativeMaxInterBitratePct(t *testing.T) {
	if _, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		MaxInterBitratePct: -1,
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderAcceptsMaxInterBitratePct(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		MaxInterBitratePct: 200,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  700,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestVP9SetMaxInterBitratePctValidation(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxInterBitratePct(-1); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetMaxInterBitratePct(-1) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetMaxInterBitratePct(150); err != nil {
		t.Fatalf("SetMaxInterBitratePct(150): %v", err)
	}
}

func TestVP9SetMaxIntraBitratePctValidation(t *testing.T) {
	var nilEnc *govpx.VP9Encoder
	if err := nilEnc.SetMaxIntraBitratePct(100); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil SetMaxIntraBitratePct err = %v, want ErrClosed", err)
	}

	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(-1); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetMaxIntraBitratePct(-1) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetMaxIntraBitratePct(175); err != nil {
		t.Fatalf("SetMaxIntraBitratePct(175): %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := e.SetMaxIntraBitratePct(100); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetMaxIntraBitratePct err = %v, want ErrClosed", err)
	}
}

func TestVP9SetGFCBRBoostPctValidation(t *testing.T) {
	var nilEnc *govpx.VP9Encoder
	if err := nilEnc.SetGFCBRBoostPct(50); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil SetGFCBRBoostPct err = %v, want ErrClosed", err)
	}

	noRC, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder(no RC): %v", err)
	}
	if err := noRC.SetGFCBRBoostPct(-1); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetGFCBRBoostPct(-1) err = %v, want ErrInvalidConfig", err)
	}
	if err := noRC.SetGFCBRBoostPct(25); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetGFCBRBoostPct without CBR err = %v, want ErrInvalidConfig", err)
	}
	if err := noRC.SetGFCBRBoostPct(0); err != nil {
		t.Fatalf("SetGFCBRBoostPct(0) without CBR: %v", err)
	}

	cbr, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    govpx.RateControlCBR,
		TargetBitrateKbps:  700,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(CBR): %v", err)
	}
	if err := cbr.SetGFCBRBoostPct(40); err != nil {
		t.Fatalf("SetGFCBRBoostPct(40): %v", err)
	}
	if err := cbr.SetGFCBRBoostPct(0); err != nil {
		t.Fatalf("SetGFCBRBoostPct(0): %v", err)
	}
	if err := cbr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cbr.SetGFCBRBoostPct(50); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetGFCBRBoostPct err = %v, want ErrClosed", err)
	}
}
