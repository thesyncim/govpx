package govpx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderRejectsNonProfile0AsNotImplemented(t *testing.T) {
	var pk vp9BitPacker
	pk.writeLiteral(2, 2)    // frame_marker = 0b10
	pk.writeLiteral(1, 2)    // profile = 1
	pk.writeBit(0)           // show_existing_frame
	pk.writeBit(0)           // frame_type = KEY
	pk.writeBit(1)           // show_frame
	pk.writeBit(0)           // error_resilient
	pk.writeLiteral(0x49, 8) // sync code 0
	pk.writeLiteral(0x83, 8) // sync code 1
	pk.writeLiteral(0x42, 8) // sync code 2
	pk.writeLiteral(2, 3)    // color_space = CSBT601
	pk.writeBit(0)           // color_range = StudioRange
	pk.writeBit(0)           // subsampling_x = 0
	pk.writeBit(0)           // subsampling_y = 0
	pk.writeBit(0)           // reserved bit
	pk.writeLiteral(15, 16)  // width - 1
	pk.writeLiteral(15, 16)  // height - 1
	pk.writeBit(0)           // render_flag
	pk.writeBit(1)           // refresh_frame_context
	pk.writeBit(0)           // frame_parallel_decoding
	pk.writeLiteral(0, 2)    // frame_context_idx
	pk.writeLiteral(0, 6)    // loopfilter filter_level
	pk.writeLiteral(0, 3)    // loopfilter sharpness
	pk.writeBit(0)           // mode_ref_delta_enabled
	pk.writeLiteral(1, 8)    // base_qindex
	pk.writeBit(0)           // y_dc_delta_q
	pk.writeBit(0)           // uv_dc_delta_q
	pk.writeBit(0)           // uv_ac_delta_q
	pk.writeBit(0)           // seg.enabled
	pk.writeBit(0)           // log2_tile_rows
	pk.writeLiteral(0, 16)   // first_partition_size
	pk.flushByte()

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(pk.buf); !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode profile 1 err = %v, want ErrVP9NotImplemented", err)
	}
}

// TestVP9DecoderRejectsTruncatedCompressedHeader: a well-formed
// profile-0 keyframe header whose first_partition_size points past
// the packet end is rejected before the reconstruct boundary.
func TestVP9DecoderRejectsTruncatedCompressedHeader(t *testing.T) {
	var pk vp9BitPacker
	pk.writeLiteral(2, 2)    // frame_marker = 0b10
	pk.writeLiteral(0, 2)    // profile = 0
	pk.writeBit(0)           // show_existing_frame
	pk.writeBit(0)           // frame_type = KEY
	pk.writeBit(1)           // show_frame
	pk.writeBit(0)           // error_resilient
	pk.writeLiteral(0x49, 8) // sync code 0
	pk.writeLiteral(0x83, 8) // sync code 1
	pk.writeLiteral(0x42, 8) // sync code 2
	pk.writeLiteral(2, 3)    // color_space = CSBT601 (0b010)
	pk.writeBit(0)           // color_range = StudioRange
	pk.writeLiteral(319, 16) // width - 1
	pk.writeLiteral(239, 16) // height - 1
	pk.writeBit(0)           // render_flag
	pk.writeBit(1)           // refresh_frame_context
	pk.writeBit(0)           // frame_parallel_decoding
	pk.writeLiteral(1, 2)    // frame_context_idx
	pk.writeLiteral(8, 6)    // loopfilter filter_level
	pk.writeLiteral(2, 3)    // loopfilter sharpness
	pk.writeBit(0)           // mode_ref_delta_enabled
	pk.writeLiteral(64, 8)   // base_qindex
	pk.writeBit(0)           // y_dc_delta_q
	pk.writeBit(0)           // uv_dc_delta_q
	pk.writeBit(0)           // uv_ac_delta_q
	pk.writeBit(0)           // seg.enabled
	pk.writeBit(0)           // log2_tile_rows
	pk.writeLiteral(42, 16)  // first_partition_size
	// Tail bytes: the compressed header. We need at least 42 bytes
	// of payload after the uncompressed header for libvpx to accept,
	// but our parser returns once first_partition_size is read.
	pk.flushByte()
	packet := pk.buf

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Errorf("LastFrameSize() = (%d, %d), want (0, 0) after rejection", w, h)
	}
}

// TestVP9DecoderDecodesEncoderKeyframeModeTile feeds the current
// encoder stub into the public decoder. The stub is a DC-predicted,
// zero-residue keyframe, so Decode publishes the expected neutral
// I420 frame.
func TestVP9DecoderDecodesEncoderKeyframeModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 96 || h != 96 {
		t.Errorf("LastFrameSize() = (%d, %d), want (96, 96)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible keyframe")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned a second frame without another Decode")
	}
}

// TestVP9DecoderDecodesEncoderInterSkipModeTile covers the second-frame
// public encoder path. It depends on the first keyframe parse to seed
// reference state before the visible LAST/ZeroMv skip inter header,
// compressed header, and tile mode-info stream are read.
func TestVP9DecoderDecodesEncoderInterSkipModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe err = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible keyframe")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	w, h := d.LastFrameSize()
	if w != 96 || h != 96 {
		t.Errorf("LastFrameSize() = (%d, %d), want (96, 96)", w, h)
	}
}

// TestVP9DecoderShowExistingFrameUsesReferenceSlot covers the first
// reference-frame-manager behavior: keyframes refresh the VP9 ring, a
// show-existing packet displays a stored slot, and that packet must not
// disturb the preserved header state needed by the following inter header.
func TestVP9DecoderShowExistingFrameUsesReferenceSlot(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe err = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after visible keyframe")
	}

	if err := d.Decode(vp9ShowExistingFramePacketForTest(5)); err != nil {
		t.Fatalf("Decode show-existing err = %v, want nil", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after show-existing frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter after show-existing err = %v, want nil", err)
	}
	frame, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
}

// TestVP9DecoderRejectsShowExistingMissingReference rejects a show-
// existing packet before any frame has refreshed the requested slot.
func TestVP9DecoderRejectsShowExistingMissingReference(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(vp9ShowExistingFramePacketForTest(0))
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame published output for invalid show-existing frame")
	}
}

// TestVP9DecoderDecodeIntoCopiesVisibleFrame mirrors the VP8
// caller-owned-output path for the VP9 reconstruction slice.
func TestVP9DecoderDecodeIntoCopiesVisibleFrame(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
	info, err := d.DecodeIntoWithPTS(packet, &dst, 42)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 ||
		!info.KeyFrame || !info.ShowFrame || info.ShowExistingFrame ||
		info.Quantizer != vp9DefaultBaseQIndex || info.RefreshFrameFlags != 0xff || info.PTS != 42 {
		t.Fatalf("DecodeIntoWithPTS info = %+v, want visible keyframe metadata", info)
	}
	assertVP9NeutralFrame(t, dst, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output for NextFrame")
	}
}

// TestVP9DecoderDecodeIntoInterFrameCopiesDestination covers visible public
// encoder inter packets copied directly into caller-owned output.
func TestVP9DecoderDecodeIntoInterFrameCopiesDestination(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seed := newTestImage(96, 96)
	if _, err := d.DecodeInto(key, &seed); err != nil {
		t.Fatalf("DecodeInto keyframe err = %v, want nil", err)
	}

	dst := newTestImage(96, 96)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 ||
		info.KeyFrame || !info.ShowFrame || info.ShowExistingFrame ||
		info.Quantizer != vp9DefaultInterBaseQIndex || info.RefreshFrameFlags != 1 {
		t.Fatalf("DecodeInto inter info = %+v, want visible inter metadata", info)
	}
	assertVP9NeutralFrame(t, dst, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output for visible inter frame")
	}
}

// TestVP9DecoderDecodeIntoShowExistingCopiesReference verifies that
// DecodeInto consumes a show-existing packet through the reference
// manager and returns the shown slot metadata.
func TestVP9DecoderDecodeIntoShowExistingCopiesReference(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seed := newTestImage(96, 96)
	if _, err := d.DecodeInto(key, &seed); err != nil {
		t.Fatalf("DecodeInto keyframe err = %v, want nil", err)
	}

	dst := newTestImage(96, 96)
	info, err := d.DecodeIntoWithPTS(vp9ShowExistingFramePacketForTest(5), &dst, 7)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS show-existing err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 ||
		info.KeyFrame || !info.ShowFrame || !info.ShowExistingFrame ||
		info.ExistingFrameSlot != 5 || info.PTS != 7 {
		t.Fatalf("DecodeIntoWithPTS show-existing info = %+v, want slot 5 metadata", info)
	}
	assertVP9NeutralFrame(t, dst, 96, 96)
	if _, ok := d.NextFrame(); ok {
		t.Fatal("DecodeInto queued output for show-existing frame")
	}
}

// TestVP9DecoderDecodeIntoRejectsInvalidDestinationBeforeDecode keeps
// invalid caller buffers from mutating decoder stream state.
func TestVP9DecoderDecodeIntoRejectsInvalidDestinationBeforeDecode(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(64, 64)
	_, err = d.DecodeInto(packet, &dst)
	if !errors.Is(err, ErrInvalidConfig) {
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
func TestVP9DecoderLastFrameInfoTracksDecodedPackets(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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
		info.Quantizer != vp9DefaultBaseQIndex || info.RefreshFrameFlags != 0xff || info.PTS != 100 {
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
		info.Quantizer != vp9DefaultInterBaseQIndex || info.RefreshFrameFlags != 1 || info.PTS != 200 {
		t.Fatalf("inter LastFrameInfo = %+v, want visible inter metadata", info)
	}

	if err := d.DecodeWithPTS(vp9ShowExistingFramePacketForTest(5), 300); err != nil {
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
	e, err := NewVP9Encoder(VP9EncoderOptions{
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

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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

	if err := d.DecodeWithPTS(vp9ShowExistingFramePacketForTest(5), 456); err != nil {
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
	var nilDec *VP9Decoder
	if _, ok := nilDec.LastFrameCorrupted(); ok {
		t.Fatal("nil decoder LastFrameCorrupted ok = true, want false")
	}
	if _, ok := nilDec.LastReferenceUpdates(); ok {
		t.Fatal("nil decoder LastReferenceUpdates ok = true, want false")
	}
	if _, ok := nilDec.LastBitDepth(); ok {
		t.Fatal("nil decoder LastBitDepth ok = true, want false")
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	key, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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
func TestVP9DecoderDecodeIntoUpdatesLastFrameInfoWithPTS(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
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
func TestVP9DecoderRejectsConfiguredResolutionChange(t *testing.T) {
	e64, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	e96, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 64})
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

	d, err := NewVP9Decoder(VP9DecoderOptions{RejectResolutionChange: true})
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
	if !errors.Is(err, ErrFrameRejected) {
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
	e64, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	e96, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 64})
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

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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
	assertVP9NeutralFrame(t, frame, 96, 64)
}

// TestVP9DecoderResetClearsFrameState keeps VP9 reset semantics aligned
// with the VP8 decoder API.
func TestVP9DecoderResetClearsFrameState(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
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
	if err := d.Decode(vp9ShowExistingFramePacketForTest(0)); !errors.Is(err, ErrInvalidVP9Data) {
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
func TestVP9DecoderDecodesEncoderEdgeClippedModeTiles(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"right-edge", 96, 64},
		{"bottom-edge", 64, 96},
		{"corner-edge", 96, 96},
		{"sub-sb", 32, 32},
		{"odd-visible", 70, 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: tc.w, Height: tc.h})
			img := vp9test.NewYCbCr(tc.w, tc.h, 128, 128, 128)
			key, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			inter, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode inter: %v", err)
			}

			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			if err := d.Decode(key); err != nil {
				t.Fatalf("Decode keyframe err = %v, want nil", err)
			}
			frame, ok := d.NextFrame()
			if !ok {
				t.Fatal("NextFrame returned !ok after visible keyframe")
			}
			assertVP9NeutralFrame(t, frame, tc.w, tc.h)
			if err := d.Decode(inter); err != nil {
				t.Fatalf("Decode inter err = %v, want nil", err)
			}
			frame, ok = d.NextFrame()
			if !ok {
				t.Fatal("NextFrame returned !ok after visible inter frame")
			}
			assertVP9NeutralFrame(t, frame, tc.w, tc.h)
			w, h := d.LastFrameSize()
			if w != tc.w || h != tc.h {
				t.Fatalf("LastFrameSize() = (%d, %d), want (%d, %d)",
					w, h, tc.w, tc.h)
			}
		})
	}
}

// TestVP9DecoderRejectsMissingModeTile ensures a packet with valid
// headers but no tile body fails in the mode-info pass before the
// decoder publishes the new frame size.
func TestVP9DecoderRejectsMissingModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := vp9test.NewYCbCr(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	tileStart, err := vp9TileStartForTest(packet)
	if err != nil {
		t.Fatalf("vp9TileStartForTest: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet[:tileStart])
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
}

// TestVP9DecoderDecodesMultiTileModeFrame drives the public decoder
// through the 4-byte size-prefixed tile layout. The public encoder
// still emits one tile, so this test packs a two-column keyframe via
// the internal packer and the same stub mode writer.
func TestVP9DecoderDecodesMultiTileModeFrame(t *testing.T) {
	packet := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	w, h := d.LastFrameSize()
	if w != 1024 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (1024, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible multi-tile keyframe")
	}
	assertVP9NeutralFrame(t, frame, 1024, 64)
}

func TestVP9DecoderInvertTileDecodeOrderMatchesForwardOrder(t *testing.T) {
	key := vp9MultiTileModePacketForTest(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})
	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)

	for _, tc := range []struct {
		name    string
		opts    VP9DecoderOptions
		packets [][]byte
	}{
		{
			name:    "keyframe",
			opts:    VP9DecoderOptions{InvertTileDecodeOrder: true},
			packets: [][]byte{key},
		},
		{
			name: "threaded inter fallback",
			opts: VP9DecoderOptions{
				Threads:               4,
				InvertTileDecodeOrder: true,
			},
			packets: [][]byte{key, inter},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseOpts := VP9DecoderOptions{Threads: tc.opts.Threads}
			want := vp9DecodeLastVisibleFrameWithOptionsForTest(t, baseOpts,
				tc.packets...)
			got := vp9DecodeLastVisibleFrameWithOptionsForTest(t, tc.opts,
				tc.packets...)
			assertVP9ImagesEqual(t, want, got)
		})
	}
}

func TestVP9DecoderSetInvertTileDecodeOrderTogglesRuntimeControl(t *testing.T) {
	packet := vp9MultiTileModePacketForTest(t, 1024, 64, 1,
		[]common.PredictionMode{common.DcPred, common.VPred})
	want := vp9DecodeLastVisibleFrameForTest(t, packet)

	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 4})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.SetInvertTileDecodeOrder(true); err != nil {
		t.Fatalf("SetInvertTileDecodeOrder(true): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode inverted: %v", err)
	}
	got, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after inverted decode returned !ok")
	}
	assertVP9ImagesEqual(t, want, got)
	if d.vp9TilePool == nil {
		t.Fatal("threaded decoder did not initialise tile pool")
	}
	if got := d.vp9TilePool.lastTileJobs; got != 0 {
		t.Fatalf("inverted decode used %d tile-worker jobs, want serial fallback", got)
	}

	if err := d.SetInvertTileDecodeOrder(false); err != nil {
		t.Fatalf("SetInvertTileDecodeOrder(false): %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode forward: %v", err)
	}
	got, ok = d.NextFrame()
	if !ok {
		t.Fatal("NextFrame after forward decode returned !ok")
	}
	assertVP9ImagesEqual(t, want, got)
	if got := d.vp9TilePool.lastTileJobs; got != 2 {
		t.Fatalf("forward decode used %d tile-worker jobs, want 2", got)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SetInvertTileDecodeOrder(true); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetInvertTileDecodeOrder err = %v, want ErrClosed", err)
	}
}

// TestVP9DecoderDecodesZeroResidueKeyframe drives a skip=0 keyframe
// through the public decoder. The tile body carries all-zero
// coefficient streams, so Decode must consume residual tokens before
// publishing reconstructed I420 output.
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
func TestVP9DecoderRejectsMissingResidueTokens(t *testing.T) {
	packet := vp9SkipZeroKeyframeForTest(t, 64, 64, false)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
}

// TestVP9DecoderRejectsInvalidMultiTilePrefix covers malformed
// size-prefix framing before the tile mode reader starts.
func TestVP9DecoderRejectsInvalidMultiTilePrefix(t *testing.T) {
	packet := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)
	tileStart, err := vp9TileStartForTest(packet)
	if err != nil {
		t.Fatalf("vp9TileStartForTest: %v", err)
	}

	cases := []struct {
		name   string
		packet []byte
	}{
		{"truncated-prefix", packet[:tileStart+2]},
		{"oversized-prefix", func() []byte {
			corrupt := make([]byte, len(packet))
			copy(corrupt, packet)
			binary.BigEndian.PutUint32(corrupt[tileStart:tileStart+4], uint32(len(packet)))
			return corrupt
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			err = d.Decode(tc.packet)
			if !errors.Is(err, ErrInvalidVP9Data) {
				t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
			}
			w, h := d.LastFrameSize()
			if w != 0 || h != 0 {
				t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
			}
		})
	}
}

// TestVP9DecoderDecodeSteadyStateAlloc keeps the public header +
// tile/residual parse and intra reconstruct output path allocation-free after
// construction.
