package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9EncoderVpxdecOracleAcceptsKeyframe pipes a govpx-emitted
// VP9 keyframe through the libvpx vpxdec binary (built via
// internal/coracle/build_vpxdec_vp9.sh). This is a structural acceptance
// gate: vpxdec parses the frame without error.
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

func TestVP9EncoderVpxdecOracleMatchesACKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesChromaDirectionalKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9ChromaHorizontalBandsForTest(width, height)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesTx16DirectionalKeyframe(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 128, 16
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	img := newVP9HorizontalBandsForTest(width, height, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, packet)
}

func TestVP9EncoderVpxdecOracleMatchesACInterFrame(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	base := newVP9YCbCrForTest(width, height, 96, 128, 128)
	next := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
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
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 0, 0, 0)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9YCbCrForTest(width, height, 128, 128, 128)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesCompoundInterFrame(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	low := newVP9CompoundAverageYCbCrForTest(width, height, -32)
	mid := newVP9CompoundAverageYCbCrForTest(width, height, 0)
	high := newVP9CompoundAverageYCbCrForTest(width, height, 32)
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
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9CheckerYCbCrForTest(width, height, 48, 208, 128, 128)
	inter, err := e.EncodeWithFlags(interSrc, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode no-update-LAST inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesForceGoldenAltRefRefresh(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := newVP9CheckerYCbCrForTest(width, height, 48, 208, 96, 224)
	inter, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("Encode force GF/ARF inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesGoldenOnlyInter(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	goldenSrc := newVP9YCbCrForTest(width, height, 188, 96, 224)
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

func TestVP9EncoderVpxdecOracleMatchesInvisibleKeyFrame(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 91, 143, 37)
	hidden, err := e.EncodeWithFlags(src, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("Encode hidden keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode visible inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, hidden, inter)
}

func TestVP9EncoderVpxdecOracleMatchesInvisibleAltRefRefresh(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9YCbCrForTest(width, height, 64, 128, 128)
	altSrc := newVP9YCbCrForTest(width, height, 188, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeWithFlags(altSrc,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode hidden ALTREF refresh: %v", err)
	}
	inter, err := e.EncodeWithFlags(altSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode visible ALTREF-only inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, hidden, inter)
}

func TestVP9EncoderVpxdecOracleMatchesShowExistingFrame(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := newVP9YCbCrForTest(width, height, 91, 143, 37)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	show, err := e.EncodeShowExistingFrame(5)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, show)
}

func TestVP9EncoderVpxdecOracleMatchesOddIntegerMotion(t *testing.T) {
	requireVP9VpxdecOracle(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 32, 8
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 32, 32
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 16, 16
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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
	requireVP9VpxdecOracle(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := newVP9MotionYCbCrForTest(width, height)
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

// TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe runs the structural
// oracle gate against a 128x64 frame: two side-by-side 64x64 SBs. The
// encoder's WriteModesTile dispatches per SB; libvpx must accept the
// resulting multi-SB tile body.
func TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 128, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 128, 64), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               128,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header),
		testutil.WriteIVFFrame(payload, 0)...)

	out, err := coracle.VpxdecVP9Decode(stream)
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected the multi-SB keyframe: %v\nvpxdec:\n%s",
			err, out)
	}
}

func assertVP9EncoderVpxdecI420Match(t *testing.T, width, height int, packets ...[]byte) {
	t.Helper()
	ivf := vp9IVFForTest(width, height, packets...)
	want, diag, err := coracle.VpxdecVP9DecodeI420(ivf)
	if err != nil {
		t.Fatalf("vpxdec-vp9 decode failed: %v\n%s", err, diag)
	}
	got := vp9DecodeVisibleI420ForTest(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for encoder stream\nlibvpx=%s\ngovpx=%s",
			testutil.MD5Hex(md5.Sum(want)),
			testutil.MD5Hex(md5.Sum(got)))
	}
}

// TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack runs the structural
// gate against a 64x128 frame: two stacked 64x64 SBs. The encoder's SB row
// loop in WriteModesTile steps mi_row by MiBlockSize across the two rows;
// libvpx must accept the per-row left_seg_context reset.
func TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 128})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 128), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              128,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header),
		testutil.WriteIVFFrame(payload, 0)...)

	out, err := coracle.VpxdecVP9Decode(stream)
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected the vertical-SB stack: %v\nvpxdec:\n%s",
			err, out)
	}
}

// TestVP9EncoderVpxdecOracleAcceptsLargeFrame runs the structural gate
// against a 256x192 keyframe: a 4x3 SB grid. This exercises the SB walker
// against a fuller mi grid and entropy-context propagation across both axes.
func TestVP9EncoderVpxdecOracleAcceptsLargeFrame(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 256, Height: 192})
	img := image.NewYCbCr(image.Rect(0, 0, 256, 192), image.YCbCrSubsampleRatio420)
	payload, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               256,
		Height:              192,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          1,
	}
	stream := append(testutil.WriteIVFHeader(header),
		testutil.WriteIVFFrame(payload, 0)...)

	out, err := coracle.VpxdecVP9Decode(stream)
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected the large keyframe: %v\nvpxdec:\n%s",
			err, out)
	}
}

// TestVP9EncoderVpxdecOracleAcceptsEdgeClippedKeyframes expands structural
// coverage beyond complete 64x64 SBs. These sizes force the
// partition writer into libvpx's frame-edge branches where the
// decoder may force SPLIT/HORZ/VERT decisions from has_rows /
// has_cols instead of reading the full tree.
func TestVP9EncoderVpxdecOracleAcceptsEdgeClippedKeyframes(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

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
			payload, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			header := testutil.IVFHeader{
				FourCC:              [4]byte{'V', 'P', '9', '0'},
				Width:               tc.width,
				Height:              tc.height,
				TimebaseDenominator: 30,
				TimebaseNumerator:   1,
				FrameCount:          1,
			}
			stream := append(testutil.WriteIVFHeader(header),
				testutil.WriteIVFFrame(payload, 0)...)

			out, err := coracle.VpxdecVP9Decode(stream)
			if err != nil {
				t.Fatalf("vpxdec-vp9 rejected %dx%d keyframe: %v\nvpxdec:\n%s",
					tc.width, tc.height, err, out)
			}
		})
	}
}

// TestVP9EncoderVpxdecOracleAcceptsPublicInterSkip runs the structural gate
// against the second frame produced by the encoder: a visible LAST/ZeroMv
// skipped inter frame emitted after the keyframe.
func TestVP9EncoderVpxdecOracleAcceptsPublicInterSkip(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

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

	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          2,
	}
	stream := append(testutil.WriteIVFHeader(header),
		testutil.WriteIVFFrame(key, 0)...)
	stream = append(stream, testutil.WriteIVFFrame(inter, 1)...)

	out, err := coracle.VpxdecVP9Decode(stream)
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected the public inter skip frame: %v\nvpxdec:\n%s",
			err, out)
	}
}

// TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame covers the first
// non-intra inter tile shape the public decoder now parses: one
// LAST/ZeroMv skipped block referencing the prior keyframe.
func TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	header := testutil.IVFHeader{
		FourCC:              [4]byte{'V', 'P', '9', '0'},
		Width:               64,
		Height:              64,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
		FrameCount:          2,
	}
	stream := append(testutil.WriteIVFHeader(header),
		testutil.WriteIVFFrame(key, 0)...)
	stream = append(stream, testutil.WriteIVFFrame(inter, 1)...)

	out, err := coracle.VpxdecVP9Decode(stream)
	if err != nil {
		t.Fatalf("vpxdec-vp9 rejected the inter skip frame: %v\nvpxdec:\n%s",
			err, out)
	}
}

// TestVP9EncoderVpxdecOracleAcceptsEdgeClippedPublicInterSkip keeps the
// public second-frame inter skip path covered on the same edge-clipped
// dimensions as keyframes.
func TestVP9EncoderVpxdecOracleAcceptsEdgeClippedPublicInterSkip(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

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

			header := testutil.IVFHeader{
				FourCC:              [4]byte{'V', 'P', '9', '0'},
				Width:               tc.width,
				Height:              tc.height,
				TimebaseDenominator: 30,
				TimebaseNumerator:   1,
				FrameCount:          2,
			}
			stream := append(testutil.WriteIVFHeader(header),
				testutil.WriteIVFFrame(key, 0)...)
			stream = append(stream, testutil.WriteIVFFrame(inter, 1)...)

			out, err := coracle.VpxdecVP9Decode(stream)
			if err != nil {
				t.Fatalf("vpxdec-vp9 rejected %dx%d public inter skip: %v\nvpxdec:\n%s",
					tc.width, tc.height, err, out)
			}
		})
	}
}
