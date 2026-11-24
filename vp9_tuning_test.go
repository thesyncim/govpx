package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

func TestVP9EncoderSetTuningValidation(t *testing.T) {
	if _, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: 16, Height: 16, FPS: 30, Tuning: govpx.Tuning(2),
	}); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("NewVP9Encoder invalid tuning error = %v, want ErrInvalidConfig", err)
	}

	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 16, Height: 16, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder returned error: %v", err)
	}
	if err := e.SetTuning(govpx.Tuning(2)); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetTuning invalid error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTuning(govpx.TuneSSIM); err != nil {
		t.Fatalf("SetTuning(TuneSSIM) returned error: %v", err)
	}
	if err := e.SetTuning(govpx.TunePSNR); err != nil {
		t.Fatalf("SetTuning(TunePSNR) returned error: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := e.SetTuning(govpx.TuneSSIM); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetTuning error = %v, want ErrClosed", err)
	}
}
