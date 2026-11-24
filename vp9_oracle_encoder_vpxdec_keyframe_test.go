//go:build govpx_oracle_trace

package govpx_test

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

// TestVP9EncoderVpxdecOracleAcceptsKeyframe pipes a govpx-emitted
// VP9 keyframe through the libvpx vpxdec binary (built via
// internal/coracle/build_vpxdec_vp9.sh). This is a structural acceptance
// gate: vpxdec parses the frame without error.
//
// The vp9test oracle resolver skips on CI hosts that have not built the pinned
// libvpx VP9 decoder yet.
func TestVP9EncoderVpxdecOracleAcceptsKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the encoder output", 64, 64, payload)
}

func TestVP9EncoderVpxdecOracleMatchesACKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesChromaDirectionalKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewChromaHorizontalBandsYCbCr(width, height)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesTx16DirectionalKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 16
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	img := vp9test.NewHorizontalBandsYCbCr(width, height, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesInvisibleKeyFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 91, 143, 37)
	hidden, err := e.EncodeWithFlags(src, govpx.EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("Encode hidden keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode visible inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, hidden, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe runs the structural
// oracle gate against a 128x64 frame: two side-by-side 64x64 SBs. The
// encoder's WriteModesTile dispatches per SB; libvpx must accept the
// resulting multi-SB tile body.
func TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 128, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 128, 64), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the multi-SB keyframe", 128, 64, payload)
}

// TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack runs the structural
// gate against a 64x128 frame: two stacked 64x64 SBs. The encoder's SB row
// loop in WriteModesTile steps mi_row by MiBlockSize across the two rows;
// libvpx must accept the per-row left_seg_context reset.
func TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 128})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 128), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the vertical-SB stack", 64, 128, payload)
}

// TestVP9EncoderVpxdecOracleAcceptsLargeFrame runs the structural gate
// against a 256x192 keyframe: a 4x3 SB grid. This exercises the SB walker
// against a fuller mi grid and entropy-context propagation across both axes.
func TestVP9EncoderVpxdecOracleAcceptsLargeFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 256, Height: 192})
	img := image.NewYCbCr(image.Rect(0, 0, 256, 192), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the large keyframe", 256, 192, payload)
}

// TestVP9EncoderVpxdecOracleAcceptsEdgeClippedKeyframes expands structural
// coverage beyond complete 64x64 SBs. These sizes force the
// partition writer into libvpx's frame-edge branches where the
// decoder may force SPLIT/HORZ/VERT decisions from has_rows /
// has_cols instead of reading the full tree.
func TestVP9EncoderVpxdecOracleAcceptsEdgeClippedKeyframes(t *testing.T) {
	vp9test.RequireVpxdec(t)

	cases := []struct {
		name          string
		width, height int
	}{
		{"right-edge", 96, 64},
		{"bottom-edge", 64, 96},
		{"corner-edge", 96, 96},
		{"sub-sb", 32, 32},
		{"odd-visible", 70, 70},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
				Width:  tc.width,
				Height: tc.height,
			})
			img := image.NewYCbCr(image.Rect(0, 0, tc.width, tc.height),
				image.YCbCrSubsampleRatio420)
			payload, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			vp9test.VpxdecAccepts(t, "edge-clipped keyframe", tc.width,
				tc.height, payload)
		})
	}
}
