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

// TestVP9EncoderVpxdecOracleAcceptsMultiSbKeyframe runs the byte-
// parity oracle gate against a 128x64 frame — two side-by-side
// 64x64 SBs. The encoder's WriteModesTile dispatches per SB; the
// gate confirms libvpx accepts the resulting multi-SB tile body
// just as cleanly as the single-SB case.
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

// TestVP9EncoderVpxdecOracleAcceptsVerticalSBStack runs the gate
// against a 64x128 frame — two stacked 64x64 SBs. The encoder's
// SB row loop in WriteModesTile steps mi_row by MiBlockSize across
// the two rows; this gate confirms the per-row left_seg_context
// reset is happening at the right boundary against libvpx.
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

// TestVP9EncoderVpxdecOracleAcceptsLargeFrame runs the gate against
// a 256x192 keyframe — 4×3 SB grid. The gate exercises the SB
// walker against a fuller mi grid + entropy-context propagation
// across both axes.
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

// TestVP9EncoderVpxdecOracleAcceptsIntraOnlyInter runs the gate
// against the second frame produced by the encoder — an
// intra-only inter frame (FrameType=InterFrame + IntraOnly=true)
// emitted after the keyframe. The encoder takes the intra-only
// fallback path because the full inter encode pipeline isn't wired
// yet; the gate confirms that fallback frame is also valid VP9.
func TestVP9EncoderVpxdecOracleAcceptsIntraOnlyInter(t *testing.T) {
	if _, err := coracle.VpxdecVP9Path(); err != nil {
		if errors.Is(err, coracle.ErrVpxdecVP9NotBuilt) {
			t.Skip("vpxdec-vp9 not built; run internal/coracle/build_vpxdec_vp9.sh")
		}
		t.Fatalf("VpxdecVP9Path: %v", err)
	}

	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	// Frame 0 = keyframe, frame 1 = intra-only inter.
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
		t.Fatalf("vpxdec-vp9 rejected the intra-only inter frame: %v\nvpxdec:\n%s",
			err, out)
	}
}
