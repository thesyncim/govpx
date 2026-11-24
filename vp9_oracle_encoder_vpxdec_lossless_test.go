//go:build govpx_oracle_trace

package govpx_test

import (
	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9EncoderVpxdecOracleMatchesLosslessKeyAndInter(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 70, 70
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:    width,
		Height:   height,
		Lossless: true,
	})
	keySrc := vp9test.NewCheckerYCbCr(width, height, 16, 240, 32, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode lossless keyframe: %v", err)
	}
	ref := vp9oracle.DecodeLastVisibleFrame(t, key)
	interSrc := vp9test.ShiftedI420(ref.Width, ref.Height,
		ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 4, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode lossless inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode lossless keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless keyframe")
	}
	vp9oracle.AssertImageMatchesYCbCr(t, "lossless keyframe", keyFrame, keySrc)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode lossless inter: %v", err)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after lossless inter")
	}
	vp9oracle.AssertImageMatchesYCbCr(t, "lossless inter", interFrame, interSrc)
}
