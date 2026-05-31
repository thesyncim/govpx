package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9DecoderDecodeIntoCopiesVisibleFrame(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newVP9TestImageForTest(96, 96)
	info, err := d.DecodeIntoWithPTS(packet, &dst, 42)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 ||
		!info.KeyFrame || !info.ShowFrame || info.ShowExistingFrame ||
		info.Quantizer != vp9DefaultBaseQIndexForTest || info.RefreshFrameFlags != 0xff || info.PTS != 42 {
		t.Fatalf("DecodeIntoWithPTS info = %+v, want visible keyframe metadata", info)
	}
	assertVP9NeutralFrameForTest(t, dst, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output for NextFrame")
	}
}

// TestVP9DecoderDecodeIntoInterFrameCopiesDestination covers visible public
// encoder inter packets copied directly into caller-owned output.

func TestVP9DecoderDecodeIntoInterFrameCopiesDestination(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seed := newVP9TestImageForTest(96, 96)
	if _, err := d.DecodeInto(key, &seed); err != nil {
		t.Fatalf("DecodeInto keyframe err = %v, want nil", err)
	}

	dst := newVP9TestImageForTest(96, 96)
	fillVP9PublicImageForTest(&dst, 77)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 ||
		info.KeyFrame || !info.ShowFrame || info.ShowExistingFrame ||
		info.Quantizer != vp9DefaultInterBaseQIndexForTest || info.RefreshFrameFlags != 1 {
		t.Fatalf("DecodeInto inter info = %+v, want visible inter metadata", info)
	}
	assertVP9NeutralFrameForTest(t, dst, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output for visible inter frame")
	}
}

// TestVP9DecoderDecodeIntoShowExistingCopiesReference verifies that
// DecodeInto consumes a show-existing packet through the reference
// manager and returns the shown slot metadata.

func TestVP9DecoderDecodeIntoShowExistingCopiesReference(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seed := newVP9TestImageForTest(96, 96)
	if _, err := d.DecodeInto(key, &seed); err != nil {
		t.Fatalf("DecodeInto keyframe err = %v, want nil", err)
	}

	dst := newVP9TestImageForTest(96, 96)
	info, err := d.DecodeIntoWithPTS(vp9test.ShowExistingFramePacket(5), &dst, 7)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS show-existing err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 ||
		info.KeyFrame || !info.ShowFrame || !info.ShowExistingFrame ||
		info.ExistingFrameSlot != 5 || info.PTS != 7 {
		t.Fatalf("DecodeIntoWithPTS show-existing info = %+v, want slot 5 metadata", info)
	}
	assertVP9NeutralFrameForTest(t, dst, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output for show-existing frame")
	}
}

// TestVP9DecoderDecodeIntoRejectsInvalidDestinationBeforeDecode keeps
// invalid caller buffers from mutating decoder stream state.

func TestVP9DecoderDecodeIntoRejectsInvalidDestinationBeforeDecode(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newVP9TestImageForTest(64, 64)
	_, err = d.DecodeInto(packet, &dst)
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("DecodeInto err = %v, want ErrInvalidConfig", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output after invalid destination")
	}
}

// TestVP9DecoderLastFrameInfoTracksDecodedPackets covers the Decode metadata
// path across key, inter, and show-existing packets.

func TestVP9DecoderDecodeIntoUpdatesLastFrameInfoWithPTS(t *testing.T) {
	e, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newVP9TestImageForTest(96, 96)
	if _, err := d.DecodeIntoWithPTS(packet, &dst, 77); err != nil {
		t.Fatalf("DecodeIntoWithPTS err = %v, want nil", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after DecodeIntoWithPTS returned !ok")
	}
	if info.PTS != 77 || !info.KeyFrame || !info.ShowFrame {
		t.Fatalf("LastFrameInfo = %+v, want DecodeIntoWithPTS metadata", info)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeIntoWithPTS queued output for NextFrame")
	}
}

// TestVP9DecoderRejectsConfiguredResolutionChange wires the VP9
// RejectResolutionChange option through header validation.
