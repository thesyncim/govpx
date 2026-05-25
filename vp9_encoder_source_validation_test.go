package govpx

import (
	"errors"
	"image"
	"testing"
)

func TestVP9EncoderRejectsInvalidSourceShape(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1024)

	if _, err := e.EncodeInto(nil, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil source err = %v, want ErrInvalidConfig", err)
	}

	wrongSize := image.NewYCbCr(image.Rect(0, 0, 32, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(wrongSize, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-size source err = %v, want ErrInvalidConfig", err)
	}

	wrongChroma := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio444)
	if _, err := e.EncodeInto(wrongChroma, dst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("wrong-chroma source err = %v, want ErrInvalidConfig", err)
	}

	valid := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(valid, nil); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("empty dst err = %v, want ErrBufferTooSmall", err)
	}
}

// TestVP9EncoderFrameTxModeFromCountsBypassesNonSelect pins libvpx
// vp9_encodeframe.c:5911 — the post-encode tx_mode demotion is gated
// on `cm->tx_mode == TX_MODE_SELECT`, so any fixed tx_mode emitted by
// select_tx_mode is written verbatim to the bitstream regardless of
// counts. The libvpx-faithful TX_MODE_SELECT partition-context ladder
// lives in TestVP9EncoderFrameTxModeFromCountsLibvpxSelectLadder.
