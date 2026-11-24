//go:build govpx_oracle_trace

package govpx_test

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxdecOracleMatchesACInterFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	base := vp9test.NewYCbCr(width, height, 96, 128, 128)
	next := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	key, err := e.Encode(base)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(next)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesInterIntraFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 0, 0, 0)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := vp9test.NewYCbCr(width, height, 128, 128, 128)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesStaticSegmentation(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	const segID = 3
	opts := govpx.VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.AltQEnabled[segID] = true
	opts.Segmentation.AltQ[segID] = -16
	opts.Segmentation.AltLFEnabled[segID] = true
	opts.Segmentation.AltLF[segID] = -4
	e, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	key, err := e.Encode(vp9test.NewYCbCr(width, height, 72, 128, 128))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(vp9test.NewCheckerYCbCr(width, height, 16, 240, 96, 224))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesStaticForcedReferences(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	cases := []struct {
		name     string
		refFrame int8
	}{
		{"last", govpx.VP9RefFrameLast},
		{"golden", govpx.VP9RefFrameGolden},
		{"altref", govpx.VP9RefFrameAltRef},
		{"intra", govpx.VP9RefFrameIntra},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const segID = 5
			opts := govpx.VP9EncoderOptions{Width: width, Height: height}
			opts.Segmentation.Enabled = true
			opts.Segmentation.UpdateMap = true
			opts.Segmentation.SegmentID = segID
			opts.Segmentation.RefFrameEnabled[segID] = true
			opts.Segmentation.RefFrame[segID] = tc.refFrame
			e, err := govpx.NewVP9Encoder(opts)
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			key, err := e.Encode(vp9test.NewYCbCr(width, height, 72, 128, 128))
			if err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			inter, err := e.Encode(vp9test.NewCheckerYCbCr(width, height, 16, 240, 96, 224))
			if err != nil {
				t.Fatalf("Encode inter: %v", err)
			}

			vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
		})
	}
}

func TestVP9EncoderVpxdecOracleMatchesCompoundInterFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	low := vp9test.NewCompoundAverageYCbCr(width, height, -32)
	mid := vp9test.NewCompoundAverageYCbCr(width, height, 0)
	high := vp9test.NewCompoundAverageYCbCr(width, height, 32)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		govpx.EncodeForceAltRefFrame|govpx.EncodeNoUpdateLast|govpx.EncodeNoUpdateGolden|
			govpx.EncodeNoReferenceGolden|govpx.EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode compound inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, alt, inter)
}

func TestVP9EncoderVpxdecOracleMatchesNoUpdateLastInterFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	inter, err := e.EncodeWithFlags(interSrc, govpx.EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode no-update-LAST inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesForceGoldenAltRefRefresh(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 96, 224)
	inter, err := e.EncodeWithFlags(interSrc,
		govpx.EncodeForceGoldenFrame|govpx.EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("Encode force GF/ARF inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesGoldenOnlyInter(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	goldenSrc := vp9test.NewYCbCr(width, height, 188, 96, 224)
	goldenRefresh, err := e.EncodeWithFlags(goldenSrc,
		govpx.EncodeForceGoldenFrame|govpx.EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode force-GOLDEN: %v", err)
	}
	inter, err := e.EncodeWithFlags(goldenSrc,
		govpx.EncodeNoReferenceLast|govpx.EncodeNoReferenceAltRef|govpx.EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode GOLDEN-only inter: %v", err)
	}

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, goldenRefresh, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsPublicInterSkip runs the structural gate
// against the second frame produced by the encoder: a visible LAST/ZeroMv
// skipped inter frame emitted after the keyframe.
func TestVP9EncoderVpxdecOracleAcceptsPublicInterSkip(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	// Frame 0 = keyframe, frame 1 = visible inter skip.
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	vp9test.VpxdecAccepts(t, "the public inter skip frame", 64, 64, key, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsEdgeClippedPublicInterSkip keeps the
// public second-frame inter skip path covered on the same edge-clipped
// dimensions as keyframes.
func TestVP9EncoderVpxdecOracleAcceptsEdgeClippedPublicInterSkip(t *testing.T) {
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
			key, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			inter, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode inter: %v", err)
			}

			vp9test.VpxdecAccepts(t, "edge-clipped public inter skip",
				tc.width, tc.height, key, inter)
		})
	}
}
