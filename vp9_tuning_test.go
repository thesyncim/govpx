package govpx

import (
	"errors"
	"testing"
)

func TestVP9EncoderSetTuningValidation(t *testing.T) {
	if _, err := NewVP9Encoder(VP9EncoderOptions{
		Width: 16, Height: 16, FPS: 30, Tuning: Tuning(2),
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewVP9Encoder invalid tuning error = %v, want ErrInvalidConfig", err)
	}

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
	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := e.SetTuning(TuneSSIM); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetTuning error = %v, want ErrClosed", err)
	}
}
