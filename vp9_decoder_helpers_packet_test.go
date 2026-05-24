package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func vp9SVCStyleSuperframeForTest(t *testing.T) []byte {
	t.Helper()
	return vp9test.SuperframePacket(t,
		vp9EncodedKeyframeForTest(t, 32, 32, 80),
		vp9EncodedKeyframeForTest(t, 64, 64, 160),
	)
}

func vp9EncodedKeyframeForTest(t *testing.T, width, height int, y byte) []byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:     width,
		Height:    height,
		Quantizer: 37,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder %dx%d: %v", width, height, err)
	}
	dstSize, err := vp9AllocatingEncodeBufferSize(width, height)
	if err != nil {
		t.Fatalf("vp9AllocatingEncodeBufferSize %dx%d: %v", width, height, err)
	}
	dst := make([]byte, dstSize)
	result, err := e.EncodeIntoWithResult(vp9test.NewYCbCr(width, height, y, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult %dx%d: %v", width, height, err)
	}
	if !result.KeyFrame || !result.ShowFrame || len(result.Data) == 0 {
		t.Fatalf("encoded test frame result = %+v, want visible keyframe", result)
	}
	return append([]byte(nil), result.Data...)
}

// TestVP9DecoderClose marks the decoder as closed; subsequent Decode
// returns ErrClosed.
