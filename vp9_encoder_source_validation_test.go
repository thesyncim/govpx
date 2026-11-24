package govpx_test

import (
	"errors"
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

func TestVP9EncoderRejectsInvalidSourceShape(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	dst := make([]byte, 1024)

	if _, err := e.EncodeInto(nil, dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("nil source err = %v, want govpx.ErrInvalidConfig", err)
	}

	wrongSize := image.NewYCbCr(image.Rect(0, 0, 32, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(wrongSize, dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("wrong-size source err = %v, want govpx.ErrInvalidConfig", err)
	}

	wrongChroma := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio444)
	if _, err := e.EncodeInto(wrongChroma, dst); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("wrong-chroma source err = %v, want govpx.ErrInvalidConfig", err)
	}

	valid := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	if _, err := e.EncodeInto(valid, nil); !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("empty dst err = %v, want govpx.ErrBufferTooSmall", err)
	}
}
