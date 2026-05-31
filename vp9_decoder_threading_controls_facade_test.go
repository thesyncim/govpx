package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP9DecoderSetRowMTValidation(t *testing.T) {
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.SetRowMT(true); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Errorf("single-threaded SetRowMT(true) err = %v, want ErrInvalidConfig", err)
	}
	if err := d.SetLoopFilterOpt(true); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Errorf("single-threaded SetLoopFilterOpt(true) err = %v, want ErrInvalidConfig",
			err)
	}
	if err := d.SetRowMT(false); err != nil {
		t.Errorf("SetRowMT(false) on single-threaded decoder err = %v, want nil",
			err)
	}
	if err := d.SetLoopFilterOpt(false); err != nil {
		t.Errorf("SetLoopFilterOpt(false) on single-threaded decoder err = %v, want nil",
			err)
	}

	threaded, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{Threads: 2})
	if err != nil {
		t.Fatalf("threaded NewVP9Decoder: %v", err)
	}
	if err := threaded.SetRowMT(true); err != nil {
		t.Errorf("threaded SetRowMT(true) err = %v, want nil", err)
	}
	if err := threaded.SetLoopFilterOpt(true); err != nil {
		t.Errorf("threaded SetLoopFilterOpt(true) err = %v, want nil", err)
	}
	if err := threaded.SetRowMT(false); err != nil {
		t.Errorf("threaded SetRowMT(false) err = %v, want nil", err)
	}
	if err := threaded.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := threaded.SetRowMT(true); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("closed SetRowMT err = %v, want ErrClosed", err)
	}
	if err := threaded.SetLoopFilterOpt(true); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("closed SetLoopFilterOpt err = %v, want ErrClosed", err)
	}
	var nilDecoder *govpx.VP9Decoder
	if err := nilDecoder.SetRowMT(false); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("nil SetRowMT err = %v, want ErrClosed", err)
	}
	if err := nilDecoder.SetLoopFilterOpt(false); !errors.Is(err, govpx.ErrClosed) {
		t.Errorf("nil SetLoopFilterOpt err = %v, want ErrClosed", err)
	}
}
