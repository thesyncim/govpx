//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"image"
	"testing"
)

func TestVP9EncoderVpxdecOracleMatchesACInterFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesInterIntraFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
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

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesStaticSegmentation(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	const segID = 3
	opts := VP9EncoderOptions{Width: width, Height: height}
	opts.Segmentation.Enabled = true
	opts.Segmentation.UpdateMap = true
	opts.Segmentation.SegmentID = segID
	opts.Segmentation.AltQEnabled[segID] = true
	opts.Segmentation.AltQ[segID] = -16
	opts.Segmentation.AltLFEnabled[segID] = true
	opts.Segmentation.AltLF[segID] = -4
	e, err := NewVP9Encoder(opts)
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

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesStaticForcedReferences(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	cases := []struct {
		name     string
		refFrame int8
	}{
		{"last", VP9RefFrameLast},
		{"golden", VP9RefFrameGolden},
		{"altref", VP9RefFrameAltRef},
		{"intra", VP9RefFrameIntra},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const segID = 5
			opts := VP9EncoderOptions{Width: width, Height: height}
			opts.Segmentation.Enabled = true
			opts.Segmentation.UpdateMap = true
			opts.Segmentation.SegmentID = segID
			opts.Segmentation.RefFrameEnabled[segID] = true
			opts.Segmentation.RefFrame[segID] = tc.refFrame
			e, err := NewVP9Encoder(opts)
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

			assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
		})
	}
}

func TestVP9EncoderVpxdecOracleMatchesCompoundInterFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	low := vp9test.NewCompoundAverageYCbCr(width, height, -32)
	mid := vp9test.NewCompoundAverageYCbCr(width, height, 0)
	high := vp9test.NewCompoundAverageYCbCr(width, height, 32)
	key, err := e.Encode(low)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	alt, err := e.EncodeWithFlags(high,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden|
			EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode alt refresh: %v", err)
	}
	inter, err := e.Encode(mid)
	if err != nil {
		t.Fatalf("Encode compound inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, alt, inter)
}

func TestVP9EncoderVpxdecOracleMatchesNoUpdateLastInterFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)
	inter, err := e.EncodeWithFlags(interSrc, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode no-update-LAST inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesForceGoldenAltRefRefresh(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 96, 224)
	inter, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("Encode force GF/ARF inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesGoldenOnlyInter(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	goldenSrc := vp9test.NewYCbCr(width, height, 188, 96, 224)
	goldenRefresh, err := e.EncodeWithFlags(goldenSrc,
		EncodeForceGoldenFrame|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode force-GOLDEN: %v", err)
	}
	inter, err := e.EncodeWithFlags(goldenSrc,
		EncodeNoReferenceLast|EncodeNoReferenceAltRef|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode GOLDEN-only inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, goldenRefresh, inter)
}

func TestVP9EncoderVpxdecOracleMatchesOddIntegerMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 7, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatches16x8InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 32, 8
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert32x32InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 32, 32
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert16x16InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 16, 16
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 4, -4)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesHorz64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := splitYShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesSplit64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := quadrantShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img,
		image.Point{X: 8}, image.Point{X: -8},
		image.Point{Y: 8}, image.Point{Y: -8})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesQuarterPelMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := predictedVP9ReferenceYCbCrForTest(t,
		e.refFrames[0].img, vp9dec.MV{Col: 58})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesEighthPelMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := predictedVP9ReferenceYCbCrForTest(t,
		e.refFrames[0].img, vp9dec.MV{Col: 57})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsPublicInterSkip runs the structural gate
// against the second frame produced by the encoder: a visible LAST/ZeroMv
// skipped inter frame emitted after the keyframe.
func TestVP9EncoderVpxdecOracleAcceptsPublicInterSkip(t *testing.T) {
	vp9test.RequireVpxdec(t)

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
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

// TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame covers the first
// non-intra inter tile shape the public decoder now parses: one
// LAST/ZeroMv skipped block referencing the prior keyframe.
func TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	vp9test.VpxdecAccepts(t, "the inter skip frame", 64, 64, key, inter)
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
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: tc.width, Height: tc.height})
			img := image.NewYCbCr(image.Rect(0, 0, tc.width, tc.height), image.YCbCrSubsampleRatio420)
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
