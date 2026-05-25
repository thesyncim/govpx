package govpx

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestVP9DecoderDecodesZeroResidueKeyframe(t *testing.T) {
	packet := vp9SkipZeroKeyframeForTest(t, 64, 64, true)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (64, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after zero-residue keyframe")
	}
	assertVP9NeutralFrame(t, frame, 64, 64)
}

// TestVP9DecoderDecodesVerticalIntraPredictionFrame proves output is
// reconstructed from parsed intra modes, not special-cased to the
// public encoder's DC mode. With no above row, VP9's V predictor uses
// 127 for the visible luma samples.

func TestVP9DecoderDecodesVerticalIntraPredictionFrame(t *testing.T) {
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.VPred)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (64, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after V-pred keyframe")
	}
	assertVP9FilledFrame(t, frame, 64, 64, 127, 128, 128)
}

// TestVP9DecoderDecodesNonZeroResidueKeyframe verifies the residual
// path is wired through inverse transform/add. The fixture gives the
// first luma transform block a DC coefficient; DC prediction then
// propagates the raised edge through the rest of the frame.

func TestVP9DecoderDecodesNonZeroResidueKeyframe(t *testing.T) {
	packet := vp9SkipResidueKeyframeForTest(t, 64, 64, true, 32)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (64, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after nonzero-residue keyframe")
	}
	if got := frame.Y[0]; got <= 128 {
		t.Fatalf("Y[0,0] = %d, want residual above predictor", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsSegmentedAltQKeyframe(t *testing.T) {
	packet := vp9SegmentedAltQKeyframeForTest(t)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode segmented alt-q keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("segmented alt-q keyframe did not publish output")
	}
	left := frame.Y[0]
	right := frame.Y[32]
	if right <= left {
		t.Fatalf("segmented alt-q keyframe right segment Y[0,32] = %d, want above left segment %d",
			right, left)
	}
	bottomLeft := frame.Y[32*frame.YStride]
	bottomRight := frame.Y[32*frame.YStride+32]
	if bottomRight <= bottomLeft {
		t.Fatalf("segmented alt-q bottom-right Y[32,32] = %d, want above bottom-left segment %d",
			bottomRight, bottomLeft)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderAppliesLoopFilterKeyframe(t *testing.T) {
	unfilteredPacket := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 0)
	filteredPacket := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)

	unfiltered := vp9DecodeLastVisibleFrameForTest(t, unfilteredPacket)
	filtered := vp9DecodeLastVisibleFrameForTest(t, filteredPacket)
	if !vp9YRectDiffers(unfiltered, filtered, 28, 0, 12, 64) {
		t.Fatal("loop-filtered keyframe luma matches unfiltered edge band")
	}
	if bytes.Equal(appendVP9YForTest(nil, unfiltered), appendVP9YForTest(nil, filtered)) {
		t.Fatal("loop-filtered keyframe luma matches unfiltered luma")
	}
	assertVP9PlaneFilled(t, "U", filtered.U, filtered.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", filtered.V, filtered.VStride, 32, 32, 128)
}

func TestVP9DecoderSkipLoopFilterMatchesUnfilteredReconstruction(t *testing.T) {
	unfilteredPacket := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 0)
	filteredPacket := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)

	unfiltered := vp9DecodeLastVisibleFrameForTest(t, unfilteredPacket)
	filtered := vp9DecodeLastVisibleFrameForTest(t, filteredPacket)
	skipped := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{SkipLoopFilter: true}, filteredPacket)

	if bytes.Equal(appendVP9YForTest(nil, filtered), appendVP9YForTest(nil, skipped)) {
		t.Fatal("SkipLoopFilter output still matches loop-filtered luma")
	}
	assertVP9ImagesEqual(t, unfiltered, skipped)
}

func TestVP9DecoderSetSkipLoopFilterTogglesRuntimeControl(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	filtered := vp9DecodeLastVisibleFrameForTest(t, packet)
	unfiltered := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
		VP9DecoderOptions{SkipLoopFilter: true}, packet)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.SetSkipLoopFilter(true); err != nil {
		t.Fatalf("SetSkipLoopFilter(true): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode with skip-loop-filter: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after skip-loop-filter returned !ok")
	}
	assertVP9ImagesEqual(t, unfiltered, got)

	if err := d.SetSkipLoopFilter(false); err != nil {
		t.Fatalf("SetSkipLoopFilter(false): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode after clearing skip-loop-filter: %v", err)
	}
	got, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after clearing skip-loop-filter returned !ok")
	}
	assertVP9ImagesEqual(t, filtered, got)

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SetSkipLoopFilter(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetSkipLoopFilter err = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderThreadedLoopFilterMatchesSerial(t *testing.T) {
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	inter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)

	cases := []struct {
		name    string
		packets [][]byte
	}{
		{
			name: "keyframe",
			packets: [][]byte{
				vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32),
			},
		},
		{
			name:    "inter-motion",
			packets: [][]byte{key, inter},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serial := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
				VP9DecoderOptions{}, tc.packets...)
			threaded := vp9DecodeLastVisibleFrameWithOptionsForTest(t,
				VP9DecoderOptions{Threads: 3}, tc.packets...)
			assertVP9ImagesEqual(t, serial, threaded)
		})
	}
}

// TestVP9DecoderRejectsMissingResidueTokens proves skip=0 blocks now
// reach the coefficient reader. The packet stops after mode-info,
// which was enough for the old mode-only parser but is not a complete
// VP9 tile.
