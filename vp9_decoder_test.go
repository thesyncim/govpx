package govpx

import (
	"errors"
	"testing"
)

// TestNewVP9DecoderZeroValueOptions: the zero value of options
// produces a usable decoder.
func TestNewVP9DecoderZeroValueOptions(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if d == nil {
		t.Fatal("NewVP9Decoder returned nil")
	}
	if got := d.Codec(); got != CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
}

// TestNewVP9DecoderRejectsBadOptions covers the negative-value checks.
func TestNewVP9DecoderRejectsBadOptions(t *testing.T) {
	cases := []VP9DecoderOptions{
		{Threads: -1},
		{MaxWidth: -1},
		{MaxHeight: -1},
	}
	for i, opts := range cases {
		_, err := NewVP9Decoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

// TestVP9DecoderDecodeReturnsNotImplemented: until the reconstruct
// pipeline lands, Decode must surface the well-known sentinel so
// callers can detect feature-gating cleanly.
func TestVP9DecoderDecodeReturnsNotImplemented(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	err = d.Decode([]byte{0x82, 0x49}) // arbitrary input
	if !errors.Is(err, ErrVP9NotImplemented) {
		t.Errorf("Decode err = %v, want ErrVP9NotImplemented", err)
	}
}

// TestVP9DecoderClose marks the decoder as closed; subsequent Decode
// returns ErrClosed.
func TestVP9DecoderClose(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = d.Decode([]byte{0x82})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("after Close, Decode err = %v, want ErrClosed", err)
	}
	// Double-close returns ErrClosed too.
	if err := d.Close(); !errors.Is(err, nil) {
		// Allow either nil or ErrClosed for idempotent close — the
		// VP8 decoder returns nil; mirror that.
		if !errors.Is(err, ErrClosed) {
			t.Errorf("second Close err = %v", err)
		}
	}
}
