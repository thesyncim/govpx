package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9DecoderLastFrameInfoTracksDecodedPackets(t *testing.T) {
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
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatal("LastFrameInfo before decode returned ok")
	}
	if w, h := d.LastDisplaySize(); w != 0 || h != 0 {
		t.Fatalf("LastDisplaySize before decode = (%d, %d), want (0, 0)", w, h)
	}
	if _, ok := d.LastBitDepth(); ok {
		t.Fatal("LastBitDepth before decode returned ok")
	}

	if err := d.DecodeWithPTS(key, 100); err != nil {
		t.Fatalf("DecodeWithPTS keyframe err = %v, want nil", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after keyframe returned !ok")
	}
	if info.Width != 96 || info.Height != 96 ||
		info.RenderWidth != 96 || info.RenderHeight != 96 ||
		info.BitDepth != 8 ||
		!info.KeyFrame || !info.ShowFrame || info.ShowExistingFrame ||
		info.Quantizer != vp9DefaultBaseQIndexForTest || info.RefreshFrameFlags != 0xff || info.PTS != 100 {
		t.Fatalf("key LastFrameInfo = %+v, want visible keyframe metadata", info)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}

	if err := d.DecodeWithPTS(inter, 200); err != nil {
		t.Fatalf("DecodeWithPTS inter err = %v, want nil", err)
	}
	info, ok = d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after inter returned !ok")
	}
	if info.Width != 96 || info.Height != 96 ||
		info.RenderWidth != 96 || info.RenderHeight != 96 ||
		info.BitDepth != 8 ||
		info.KeyFrame || !info.ShowFrame || info.ShowExistingFrame ||
		info.Quantizer != vp9DefaultInterBaseQIndexForTest || info.RefreshFrameFlags != 1 || info.PTS != 200 {
		t.Fatalf("inter LastFrameInfo = %+v, want visible inter metadata", info)
	}

	if err := d.DecodeWithPTS(vp9test.ShowExistingFramePacket(5), 300); err != nil {
		t.Fatalf("DecodeWithPTS show-existing err = %v, want nil", err)
	}
	info, ok = d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after show-existing returned !ok")
	}
	if info.Width != 96 || info.Height != 96 ||
		info.RenderWidth != 96 || info.RenderHeight != 96 ||
		info.BitDepth != 8 ||
		info.KeyFrame || !info.ShowFrame || !info.ShowExistingFrame ||
		info.ExistingFrameSlot != 5 || info.PTS != 300 {
		t.Fatalf("show-existing LastFrameInfo = %+v, want slot 5 metadata", info)
	}
}

func TestVP9DecoderLastDisplaySizeTracksRenderSize(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width: 96, Height: 64,
		RenderWidth: 80, RenderHeight: 48,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	img := vp9test.NewYCbCr(96, 64, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.DecodeWithPTS(key, 123); err != nil {
		t.Fatalf("DecodeWithPTS keyframe: %v", err)
	}
	if w, h := d.LastFrameSize(); w != 96 || h != 64 {
		t.Fatalf("LastFrameSize = (%d, %d), want coded (96, 64)", w, h)
	}
	if w, h := d.LastDisplaySize(); w != 80 || h != 48 {
		t.Fatalf("LastDisplaySize = (%d, %d), want render (80, 48)", w, h)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after keyframe returned !ok")
	}
	if info.Width != 96 || info.Height != 64 ||
		info.RenderWidth != 80 || info.RenderHeight != 48 ||
		info.BitDepth != 8 ||
		!info.KeyFrame || info.PTS != 123 {
		t.Fatalf("key LastFrameInfo = %+v, want coded + render metadata", info)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after keyframe returned !ok")
	}
	if frame.Width != 96 || frame.Height != 64 {
		t.Fatalf("NextFrame dimensions = %dx%d, want coded storage 96x64",
			frame.Width, frame.Height)
	}

	if err := d.DecodeWithPTS(vp9test.ShowExistingFramePacket(5), 456); err != nil {
		t.Fatalf("DecodeWithPTS show-existing: %v", err)
	}
	if w, h := d.LastDisplaySize(); w != 80 || h != 48 {
		t.Fatalf("show-existing LastDisplaySize = (%d, %d), want render (80, 48)", w, h)
	}
	info, ok = d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after show-existing returned !ok")
	}
	if info.Width != 96 || info.Height != 64 ||
		info.RenderWidth != 80 || info.RenderHeight != 48 ||
		info.BitDepth != 8 ||
		!info.ShowExistingFrame || info.ExistingFrameSlot != 5 || info.PTS != 456 {
		t.Fatalf("show-existing LastFrameInfo = %+v, want stored render metadata", info)
	}
}

func TestVP9DecoderLastControlsBeforeDecode(t *testing.T) {
	var nilDec *govpx.VP9Decoder
	if _, ok := nilDec.LastFrameCorrupted(); ok {
		t.Fatal("nil decoder LastFrameCorrupted ok = true, want false")
	}
	if _, ok := nilDec.LastReferenceUpdates(); ok {
		t.Fatal("nil decoder LastReferenceUpdates ok = true, want false")
	}
	if _, ok := nilDec.LastBitDepth(); ok {
		t.Fatal("nil decoder LastBitDepth ok = true, want false")
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if _, ok := d.LastFrameCorrupted(); ok {
		t.Fatal("pre-decode LastFrameCorrupted ok = true, want false")
	}
	if _, ok := d.LastReferenceUpdates(); ok {
		t.Fatal("pre-decode LastReferenceUpdates ok = true, want false")
	}
	if _, ok := d.LastBitDepth(); ok {
		t.Fatal("pre-decode LastBitDepth ok = true, want false")
	}
}

func TestVP9DecoderLastControlsTrackDecodedPackets(t *testing.T) {
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
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	corrupted, ok := d.LastFrameCorrupted()
	if !ok || corrupted {
		t.Fatalf("key LastFrameCorrupted = (%v, %v), want (false, true)",
			corrupted, ok)
	}
	updates, ok := d.LastReferenceUpdates()
	if !ok || updates != 0xff {
		t.Fatalf("key LastReferenceUpdates = (%#x, %v), want (0xff, true)",
			updates, ok)
	}
	bitDepth, ok := d.LastBitDepth()
	if !ok || bitDepth != 8 {
		t.Fatalf("key LastBitDepth = (%d, %v), want (8, true)", bitDepth, ok)
	}

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after inter")
	}
	corrupted, ok = d.LastFrameCorrupted()
	if !ok || corrupted {
		t.Fatalf("inter LastFrameCorrupted = (%v, %v), want (false, true)",
			corrupted, ok)
	}
	updates, ok = d.LastReferenceUpdates()
	if !ok || updates != 1 {
		t.Fatalf("inter LastReferenceUpdates = (%#x, %v), want (0x1, true)",
			updates, ok)
	}
	bitDepth, ok = d.LastBitDepth()
	if !ok || bitDepth != 8 {
		t.Fatalf("inter LastBitDepth = (%d, %v), want (8, true)", bitDepth, ok)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := d.LastFrameCorrupted(); ok {
		t.Fatal("closed LastFrameCorrupted ok = true, want false")
	}
	if _, ok := d.LastReferenceUpdates(); ok {
		t.Fatal("closed LastReferenceUpdates ok = true, want false")
	}
	if _, ok := d.LastBitDepth(); ok {
		t.Fatal("closed LastBitDepth ok = true, want false")
	}
}

// TestVP9DecoderDecodeIntoUpdatesLastFrameInfoWithPTS keeps DecodeInto
// and Decode on the same metadata path.

func TestVP9DecoderRejectsConfiguredResolutionChange(t *testing.T) {
	e64, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	e96, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 64})
	img64 := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	img96 := vp9test.NewYCbCr(96, 64, 128, 128, 128)
	key64, err := e64.Encode(img64)
	if err != nil {
		t.Fatalf("Encode 64x64 keyframe: %v", err)
	}
	key96, err := e96.Encode(img96)
	if err != nil {
		t.Fatalf("Encode 96x64 keyframe: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{RejectResolutionChange: true})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key64); err != nil {
		t.Fatalf("Decode initial keyframe err = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after initial keyframe")
	}
	err = d.Decode(key96)
	if !errors.Is(err, govpx.ErrFrameRejected) {
		t.Fatalf("resolution-change Decode err = %v, want ErrFrameRejected", err)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want initial 64x64", w, h)
	}
}

// TestVP9DecoderAcceptsResolutionChangeByDefault preserves the default
// libvpx-style reallocating behavior for VP9 keyframe size changes.

func TestVP9DecoderAcceptsResolutionChangeByDefault(t *testing.T) {
	e64, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64})
	e96, _ := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 96, Height: 64})
	img64 := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	img96 := vp9test.NewYCbCr(96, 64, 128, 128, 128)
	key64, err := e64.Encode(img64)
	if err != nil {
		t.Fatalf("Encode 64x64 keyframe: %v", err)
	}
	key96, err := e96.Encode(img96)
	if err != nil {
		t.Fatalf("Encode 96x64 keyframe: %v", err)
	}

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key64); err != nil {
		t.Fatalf("Decode initial keyframe err = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after initial keyframe")
	}
	if err := d.Decode(key96); err != nil {
		t.Fatalf("Decode resolution-change keyframe err = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after resolution-change keyframe")
	}
	assertVP9NeutralFrameForTest(t, frame, 96, 64)
}

// TestVP9DecoderResetClearsFrameState keeps VP9 reset semantics aligned
// with the VP8 decoder API.

func TestVP9DecoderResetClearsFrameState(t *testing.T) {
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
	if err := d.DecodeWithPTS(packet, 33); err != nil {
		t.Fatalf("DecodeWithPTS err = %v, want nil", err)
	}
	if _, ok := d.LastFrameInfo(); !ok {
		t.Fatal("LastFrameInfo after decode returned !ok")
	}
	d.Reset()
	if w, h := d.LastFrameSize(); w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() after Reset = (%d, %d), want (0, 0)", w, h)
	}
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatal("LastFrameInfo after Reset returned ok")
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame after Reset returned ok")
	}
	if err := d.Decode(vp9test.ShowExistingFramePacket(0)); !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Fatalf("show-existing after Reset err = %v, want ErrInvalidVP9Data", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode after Reset err = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame after Decode following Reset returned !ok")
	}
}

// TestVP9DecoderDecodesEncoderEdgeClippedModeTiles covers the same
// partial-SB shapes as the vpxdec oracle, but through the public
// decoder's tile-mode/residual parser and prediction-only output path for
// both keyframe and visible inter frames.
