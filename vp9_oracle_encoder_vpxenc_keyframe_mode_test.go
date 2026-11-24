//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxencOracleFlat64KeyframeModeParity(t *testing.T) {
	vp9test.RequireVpxenc(t)

	const width, height = 64, 64
	src := vp9test.NewYCbCr(width, height, 80, 128, 128)
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	libvpxPacket := vp9test.VpxencPackets(t, []*image.YCbCr{src})[0]

	govpxGrid := decodeVP9MiGridForOracleTest(t, govpxPacket)
	libvpxGrid := decodeVP9MiGridForOracleTest(t, libvpxPacket)
	if len(govpxGrid) != len(libvpxGrid) {
		t.Fatalf("mi grid length: govpx=%d libvpx=%d", len(govpxGrid), len(libvpxGrid))
	}
	modeMatches := 0
	blockMatches := 0
	skipMatches := 0
	for i := range govpxGrid {
		if govpxGrid[i].Mode == libvpxGrid[i].Mode {
			modeMatches++
		}
		if govpxGrid[i].SbType == libvpxGrid[i].SbType {
			blockMatches++
		}
		if govpxGrid[i].Skip == libvpxGrid[i].Skip {
			skipMatches++
		}
	}
	t.Logf("VP9 flat 64x64 keyframe mode trace: modes=%d/%d blocks=%d/%d skips=%d/%d govpx_bytes=%d libvpx_bytes=%d",
		modeMatches, len(govpxGrid), blockMatches, len(govpxGrid),
		skipMatches, len(govpxGrid), len(govpxPacket), len(libvpxPacket))
	vp9test.AssertPacketByteParity(t, "flat 64x64 keyframe", govpxPacket, libvpxPacket)
	if blockMatches != len(govpxGrid) || skipMatches != len(govpxGrid) {
		t.Fatalf("flat keyframe block/skip regression: block_matches=%d/%d skip_matches=%d/%d",
			blockMatches, len(govpxGrid), skipMatches, len(govpxGrid))
	}
	if vp9test.StrictEnv("GOVPX_VP9_KEYFRAME_MODE_STRICT") &&
		modeMatches != len(govpxGrid) {
		t.Fatalf("strict VP9 keyframe mode parity matched %d/%d modes",
			modeMatches, len(govpxGrid))
	}
}
