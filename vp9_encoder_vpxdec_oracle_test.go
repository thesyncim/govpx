package govpx

import (
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
)

// TestVP9EncoderVpxdecOracleAcceptsKeyframe pipes a govpx-emitted
// VP9 keyframe through the libvpx vpxdec binary (built via
// internal/coracle/build_vpxdec_vp9.sh). The byte-parity gate is:
// vpxdec parses the frame without error, so the encoder's output
// is structurally valid VP9.
//
// Gated by ErrVpxdecVP9NotBuilt — the test skips on CI hosts that
// haven't run the build script yet, mirroring how the VP8 oracle
// tests stay green when the matching binary is missing.
func TestVP9EncoderVpxdecOracleAcceptsKeyframe(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header),
		testutil.WriteIVFFrame(payload, 0)...)

	out, err := coracle.VpxdecVP9Decode(stream)
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected the encoder output: %v\nvpxdec output:\n%s",
			err, out)
	}
}
