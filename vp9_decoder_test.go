package govpx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

const vp9SteadyStateAllocRuns = 25

// TestNewVP9DecoderZeroValueOptions: the zero value of options
// produces a usable decoder.
func TestNewVP9DecoderZeroValueOptions(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	if d == nil {
		t.Fatal("NewVP9Decoder returned nil")
	}
	if got := d.Codec(); got != CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
}

// TestNewVP9DecoderRejectsBadOptions covers the negative-value checks.
func TestNewVP9DecoderRejectsBadOptions(t *testing.T) {
	cases := []VP9DecoderOptions{
		{Threads: -1},
		{MaxWidth: -1},
		{MaxHeight: -1},
	}
	for i, opts := range cases {
		_, err := NewVP9Decoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

func TestVP9DecoderPrepareIntraOnlyFrameContextResetSemantics(t *testing.T) {
	d, _ := NewVP9Decoder(VP9DecoderOptions{})
	d.frameContexts[0].SkipProbs[0] = 77
	hdr := vp9dec.UncompressedHeader{
		FrameType:         common.InterFrame,
		IntraOnly:         true,
		ResetFrameContext: 0,
		FrameContextIdx:   2,
	}
	if idx := d.prepareVP9FrameContext(&hdr); idx != 0 {
		t.Fatalf("prepareVP9FrameContext reset=0 idx = %d, want 0", idx)
	}
	if got := d.fc.SkipProbs[0]; got != 77 {
		t.Fatalf("prepareVP9FrameContext reset=0 SkipProbs[0] = %d, want preserved context 0", got)
	}

	d.frameContexts[0].SkipProbs[0] = 77
	hdr.ResetFrameContext = 2
	hdr.FrameContextIdx = 0
	if idx := d.prepareVP9FrameContext(&hdr); idx != 0 {
		t.Fatalf("prepareVP9FrameContext reset=2 idx = %d, want 0", idx)
	}
	var want vp9dec.FrameContext
	vp9dec.ResetFrameContext(&want)
	if d.fc != want || d.frameContexts[0] != want {
		t.Fatal("prepareVP9FrameContext reset=2 did not reset selected intra-only context")
	}
}

// TestVP9DecoderDecodeMalformedHeader: a too-short payload trips
// the uncompressed-header parser's sync-code check and surfaces
// ErrInvalidVP9Data.
func TestVP9DecoderDecodeMalformedHeader(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder returned error: %v", err)
	}
	// 0x82 packs frame_marker=10, profile=0, show_existing_frame=0,
	// frame_type=KEY, show_frame=1, error_resilient=0. The sync
	// code (49 83 42) is then truncated to one byte → invalid.
	err = d.Decode([]byte{0x82, 0x49})
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Errorf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
}

// TestVP9DecoderDecodeEmptyPacket: zero-length input is rejected.
func TestVP9DecoderDecodeEmptyPacket(t *testing.T) {
	d, _ := NewVP9Decoder(VP9DecoderOptions{})
	if err := d.Decode(nil); !errors.Is(err, ErrInvalidVP9Data) {
		t.Errorf("nil packet err = %v, want ErrInvalidVP9Data", err)
	}
	if err := d.Decode([]byte{}); !errors.Is(err, ErrInvalidVP9Data) {
		t.Errorf("empty packet err = %v, want ErrInvalidVP9Data", err)
	}
}

func TestVP9SuperframeIndexSplitsFrames(t *testing.T) {
	wantFrames := [][]byte{
		{0x82, 0x49, 0x83},
		{0x04, 0x05, 0x06, 0x07},
		{0x08},
	}
	packet := vp9SuperframePacketForTest(wantFrames...)
	sf, err := vp9ParseSuperframe(packet)
	if err != nil {
		t.Fatalf("vp9ParseSuperframe returned error: %v", err)
	}
	if sf.count != len(wantFrames) {
		t.Fatalf("superframe count = %d, want %d", sf.count, len(wantFrames))
	}
	for i := range wantFrames {
		if !bytes.Equal(sf.frames[i], wantFrames[i]) {
			t.Fatalf("frame %d = %v, want %v", i, sf.frames[i], wantFrames[i])
		}
	}
}

func TestVP9SuperframeIndexRejectsInvalidMarker(t *testing.T) {
	if _, err := vp9ParseSuperframe([]byte{0x01, 0xc0}); !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("vp9ParseSuperframe err = %v, want ErrInvalidVP9Data", err)
	}
}

func TestVP9SuperframeIndexRejectsSizeMismatch(t *testing.T) {
	packet := vp9SuperframePacketForTest([]byte{0x01}, []byte{0x02})
	marker := packet[len(packet)-1]
	indexSize := 2 + (int(marker&0x7)+1)*(int((marker>>3)&0x3)+1)
	indexStart := len(packet) - indexSize
	bad := append([]byte{}, packet[:indexStart]...)
	bad = append(bad, 0xff)
	bad = append(bad, packet[indexStart:]...)

	if _, err := vp9ParseSuperframe(bad); !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("vp9ParseSuperframe err = %v, want ErrInvalidVP9Data", err)
	}
}

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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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

	if err := d.DecodeWithPTS(key, 100); err != nil {
		t.Fatalf("DecodeWithPTS keyframe err = %v, want nil", err)
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatal("LastFrameInfo after keyframe returned !ok")
	}
	if info.Width != 96 || info.Height != 96 ||
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
		info.KeyFrame || !info.ShowFrame || !info.ShowExistingFrame ||
		info.ExistingFrameSlot != 5 || info.PTS != 300 {
		t.Fatalf("show-existing LastFrameInfo = %+v, want slot 5 metadata", info)
	}
}

// TestVP9DecoderDecodeIntoUpdatesLastFrameInfoWithPTS keeps DecodeInto
// and Decode on the same metadata path.
func TestVP9DecoderDecodeIntoUpdatesLastFrameInfoWithPTS(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
	img64 := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	img96 := newVP9YCbCrForTest(96, 64, 128, 128, 128)
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
	img64 := newVP9YCbCrForTest(64, 64, 128, 128, 128)
	img96 := newVP9YCbCrForTest(96, 64, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
			img := newVP9YCbCrForTest(tc.w, tc.h, 128, 128, 128)
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
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
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
func TestVP9DecoderDecodeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("Decode steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode loop-filtered keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderThreadedLoopFilteredKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, 64, 64, 32)
	d, err := NewVP9Decoder(VP9DecoderOptions{Threads: 3})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if d.vp9LoopFilterPool == nil {
		t.Fatal("threaded VP9 decoder did not initialize loop-filter pool")
	}
	if got, want := d.vp9LoopFilterPool.helperCount, int8(2); got != want {
		t.Fatalf("VP9 loop-filter helper count = %d, want %d", got, want)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode threaded loop-filtered keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode threaded loop-filtered keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("threaded loop-filtered keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderSegmentedAltQKeyframeSteadyStateAlloc(t *testing.T) {
	packet := vp9SegmentedAltQKeyframeForTest(t)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("warm Decode segmented alt-q keyframe err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(packet)
	})
	if err != nil {
		t.Fatalf("Decode segmented alt-q keyframe err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented alt-q keyframe steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9DecoderDecodeIntoSteadyStateAlloc keeps caller-owned VP9 output
// allocation-free after the decoder and reference slots are warm.
func TestVP9DecoderDecodeIntoSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := newVP9YCbCrForTest(96, 96, 128, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
	if _, err := d.DecodeInto(packet, &dst); err != nil {
		t.Fatalf("warm DecodeInto err = %v, want nil", err)
	}

	var info VP9FrameInfo
	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		info, err = d.DecodeInto(packet, &dst)
	})
	if err != nil {
		t.Fatalf("DecodeInto err = %v, want nil", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame {
		t.Fatalf("DecodeInto info = %+v, want visible 96x96 frame", info)
	}
	if allocs != 0 {
		t.Fatalf("DecodeInto steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterTileParseSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter tile parse steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledZeroMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled zero-mv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled zero-mv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled zero-mv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledNewMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	inter := vp9ScaledNewMvInterFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled newmv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled newmv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled newmv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledNearestMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled nearest seed keyframe: %v", err)
	}
	inter := vp9ScaledInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled nearestmv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled nearestmv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled nearestmv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledNearMvInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled near seed keyframe: %v", err)
	}
	inter := vp9ScaledInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled nearmv inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled nearmv inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled nearmv inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterIntraSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter-intra err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter-intra err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter-intra steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9CompoundInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundGoldenAltrefNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	inter := vp9CompoundInterGoldenAltrefNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound golden/altref newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound golden/altref newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound golden/altref newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundSignBiasLayoutsSteadyStateAlloc(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame func(*testing.T) []byte
	}{
		{"fixed-golden", vp9CompoundFixedGoldenSignBiasNewMvFrameForTest},
		{"fixed-last", vp9CompoundFixedLastSignBiasNewMvFrameForTest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
			inter := tc.frame(t)
			if err := d.Decode(inter); err != nil {
				t.Fatalf("warm Decode compound %s sign-bias err = %v, want nil",
					tc.name, err)
			}

			allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
				err = d.Decode(inter)
			})
			if err != nil {
				t.Fatalf("Decode compound %s sign-bias err = %v, want nil",
					tc.name, err)
			}
			if allocs != 0 {
				t.Fatalf("compound %s sign-bias steady state: got %v allocs/op, want 0",
					tc.name, allocs)
			}
		})
	}
}

func TestVP9DecoderSegmentedAltrefInterSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9SegmentedAltrefInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode segmented altref inter err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode segmented altref inter err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented altref inter steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderSegmentedAltrefInterMapReuseSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	if err := d.Decode(vp9SegmentedAltrefInterSkipFrameForTest(t)); err != nil {
		t.Fatalf("Decode segmented altref inter map seed err = %v, want nil", err)
	}
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode segmented altref inter map-reuse err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode segmented altref inter map-reuse err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("segmented altref inter map-reuse steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterSubpelNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 96, 96)
	inter := vp9CompoundInterSubpelNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledCompoundInterNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9ScaledCompoundInterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledCompoundInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)
	inter := vp9ScaledCompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter nearestmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledCompoundInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)
	inter := vp9ScaledCompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9CompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}

// TestVP9DecoderFrameContextSlotsTrackInterHeaderUpdates keeps VP9's
// four entropy-context slots separate. A valid inter frame may update
// the compressed-header probabilities while reconstructing through the
// skipped zero-MV path; that update belongs only to the selected
// frame_context_idx.
func TestVP9DecoderFrameContextSlotsTrackInterHeaderUpdates(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	packet, wantSkipProb := vp9InterFrameContextUpdatePacketForTest(t, 64, 64, 1, true)
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if got := d.frameContexts[1].SkipProbs[0]; got != wantSkipProb {
		t.Fatalf("context 1 skip prob = %d, want %d", got, wantSkipProb)
	}
	if got := d.frameContexts[0].SkipProbs[0]; got != tables.DefaultSkipProbs[0] {
		t.Fatalf("context 0 skip prob = %d, want default %d",
			got, tables.DefaultSkipProbs[0])
	}
}

// TestVP9DecoderFrameContextNoRefreshDoesNotPersistUpdates covers the
// refresh_frame_context gate: compressed-header updates are still used
// for the current frame parse, but they must not become the stored slot
// state when the header clears the refresh bit.
func TestVP9DecoderFrameContextNoRefreshDoesNotPersistUpdates(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	packet, wantSkipProb := vp9InterFrameContextUpdatePacketForTest(t, 64, 64, 2, false)
	if wantSkipProb == tables.DefaultSkipProbs[0] {
		t.Fatalf("test packet did not update skip prob away from default %d", wantSkipProb)
	}
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if got := d.frameContexts[2].SkipProbs[0]; got != tables.DefaultSkipProbs[0] {
		t.Fatalf("context 2 skip prob = %d, want default %d",
			got, tables.DefaultSkipProbs[0])
	}
}

func TestVP9DecoderResetClearsFrameContextSlots(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	packet, wantSkipProb := vp9InterFrameContextUpdatePacketForTest(t, 64, 64, 3, true)
	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if got := d.frameContexts[3].SkipProbs[0]; got != wantSkipProb {
		t.Fatalf("context 3 skip prob = %d, want %d", got, wantSkipProb)
	}

	d.Reset()
	for i := range d.frameContexts {
		if got := d.frameContexts[i].SkipProbs[0]; got != tables.DefaultSkipProbs[0] {
			t.Fatalf("context %d skip prob after Reset = %d, want default %d",
				i, got, tables.DefaultSkipProbs[0])
		}
	}
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatal("LastFrameInfo after Reset returned ok")
	}
}

func TestVP9DecoderParsesInterSkipModeTile(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	err = d.Decode(inter)
	if err != nil {
		t.Fatalf("Decode inter err = %v, want nil", err)
	}
	if len(d.miGrid) == 0 {
		t.Fatal("inter tile parse left miGrid empty")
	}
	mi := d.miGrid[0]
	if mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
		t.Fatalf("inter ref frames = %v, want Last/NoRef", mi.RefFrame)
	}
	if mi.Mode != common.ZeroMv || mi.Skip != 1 {
		t.Fatalf("inter MI = mode %d skip %d, want ZeroMv/skip", mi.Mode, mi.Skip)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (64, 64)", w, h)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter skip frame did not publish reconstructed output")
	}
	assertVP9NeutralFrame(t, frame, 64, 64)
}

func TestVP9DecoderReconstructsInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	want := appendVP9I420(nil, keyFrame)

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter skip frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter skip frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("inter skip frame did not copy the LAST reference pixels")
	}
}

func TestVP9DecoderReconstructsScaledZeroMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9SegmentedAltQKeyframeForTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}

	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled zero-mv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled zero-mv inter frame did not publish output")
	}
	if frame.Width != 32 || frame.Height != 32 {
		t.Fatalf("scaled zero-mv inter frame = %dx%d, want 32x32",
			frame.Width, frame.Height)
	}
	left := frame.Y[8*frame.YStride+8]
	right := frame.Y[8*frame.YStride+24]
	if right <= left {
		t.Fatalf("scaled zero-mv inter right sample = %d, want above left sample %d",
			right, left)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 16, 16, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 16, 16, 128)
}

func TestVP9DecoderReconstructsScaledNewMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9SegmentedAltQKeyframeForTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}

	zero := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	if err := d.Decode(zero); err != nil {
		t.Fatalf("Decode scaled zero-mv inter frame: %v", err)
	}
	zeroFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled zero-mv inter frame did not publish output")
	}
	zeroI420 := appendVP9I420(nil, zeroFrame)

	inter := vp9ScaledNewMvInterFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled newmv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled newmv inter frame did not publish output")
	}
	if frame.Width != 32 || frame.Height != 32 {
		t.Fatalf("scaled newmv inter frame = %dx%d, want 32x32",
			frame.Width, frame.Height)
	}
	if bytes.Equal(appendVP9I420(nil, frame), zeroI420) {
		t.Fatal("scaled newmv inter frame matched the zero-mv scaled predictor")
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 16, 16, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 16, 16, 128)
}

func TestVP9DecoderReconstructsScaledNearestMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled nearest seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled nearest seed keyframe did not publish output")
	}

	zero := vp9ScaledZeroMvInterFrameForTest(t, 64, 64, 128, 128)
	if err := d.Decode(zero); err != nil {
		t.Fatalf("Decode scaled zero-mv inter frame: %v", err)
	}
	zeroFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled zero-mv inter frame did not publish output")
	}
	zeroI420 := appendVP9I420(nil, zeroFrame)

	inter := vp9ScaledInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled nearestmv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled nearestmv inter frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled nearestmv inter frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	if bytes.Equal(appendVP9I420(nil, frame), zeroI420) {
		t.Fatal("scaled nearestmv inter frame matched the zero-mv scaled predictor")
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols].Mode; got != common.NearestMv {
		t.Fatalf("bottom-left inter mode = %v, want NEARESTMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsScaledNearMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9ColumnResidueKeyframeForMotionTest(t, 128, 128)); err != nil {
		t.Fatalf("Decode scaled nearmv seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled nearmv seed keyframe did not publish output")
	}

	inter := vp9ScaledInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled nearmv inter frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled nearmv inter frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled nearmv inter frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearMv {
		t.Fatalf("bottom-right inter mode = %v, want NEARMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderDecodeIntoScaledZeroMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}
	dst := newTestImage(32, 32)
	inter := vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto scaled zero-mv inter frame: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || !info.ShowFrame {
		t.Fatalf("DecodeInto scaled zero-mv info = %+v, want visible 32x32 frame", info)
	}
	left := dst.Y[8*dst.YStride+8]
	right := dst.Y[8*dst.YStride+24]
	if right <= left {
		t.Fatalf("DecodeInto scaled zero-mv right sample = %d, want above left sample %d",
			right, left)
	}
}

func TestVP9DecoderDecodeIntoScaledNewMvInterFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(vp9SegmentedAltQKeyframeForTest(t)); err != nil {
		t.Fatalf("Decode scaled-ref seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("scaled-ref seed keyframe did not publish output")
	}
	dst := newTestImage(32, 32)
	if _, err := d.DecodeInto(vp9ScaledZeroMvInterFrameForTest(t, 32, 32, 64, 64), &dst); err != nil {
		t.Fatalf("DecodeInto scaled zero-mv inter frame: %v", err)
	}
	zeroI420 := appendVP9I420(nil, dst)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(vp9ScaledNewMvInterFrameForTest(t), &dst)
	if err != nil {
		t.Fatalf("DecodeInto scaled newmv inter frame: %v", err)
	}
	if info.Width != 32 || info.Height != 32 || !info.ShowFrame {
		t.Fatalf("DecodeInto scaled newmv info = %+v, want visible 32x32 frame", info)
	}
	if bytes.Equal(appendVP9I420(nil, dst), zeroI420) {
		t.Fatal("DecodeInto scaled newmv frame matched the zero-mv scaled predictor")
	}
}

func TestVP9DecoderReconstructsSegmentedAltrefInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	want := appendVP9I420(nil, altRef)
	if bytes.Equal(appendVP9I420(nil, lastRef), want) {
		t.Fatal("segmented ref-frame test setup left LAST and ALTREF identical")
	}

	inter := vp9SegmentedAltrefInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode segmented altref inter skip frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("segmented altref inter skip frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("segmented altref inter skip frame did not copy the segment-forced ALTREF pixels")
	}
}

func TestVP9DecoderReconstructsSegmentedAltrefInterMapReuseFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	want := appendVP9I420(nil, altRef)

	if err := d.Decode(vp9SegmentedAltrefInterSkipFrameForTest(t)); err != nil {
		t.Fatalf("Decode segmented altref inter map seed frame: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("segmented altref inter map seed frame did not publish output")
	}
	inter := vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode segmented altref inter map-reuse frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("segmented altref inter map-reuse frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("segmented altref inter map-reuse frame did not preserve the forced ALTREF segment map")
	}
}

func TestVP9DecoderReconstructsCompoundInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	want := appendVP9I420(nil, keyFrame)

	inter := vp9CompoundInterSkipFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter skip frame: %v", err)
	}
	gotFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter skip frame did not publish output")
	}
	got := appendVP9I420(nil, gotFrame)
	if !bytes.Equal(got, want) {
		t.Fatal("compound inter skip frame did not average matching references back to the source pixels")
	}
}

func TestVP9DecoderReconstructsCompoundInterNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32]
	altPix := altRef.Y[32]
	if lastPix == altPix {
		t.Fatalf("compound reference test pattern missing: LAST=%d ALTREF=%d", lastPix, altPix)
	}
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound newmv Y[0,0] = %d, want average of LAST %d and ALTREF %d -> %d",
			got, lastPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundGoldenAltrefNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	goldenRef := d.refFrames[vp9CompoundGoldenSlotForTest].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	goldenPix := goldenRef.Y[32]
	altPix := altRef.Y[32]
	if goldenPix == altPix {
		t.Fatalf("compound reference test pattern missing: GOLDEN=%d ALTREF=%d",
			goldenPix, altPix)
	}
	want := byte((int(goldenPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterGoldenAltrefNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound golden/altref newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound golden/altref newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound golden/altref newmv Y[0,0] = %d, want average of GOLDEN %d and ALTREF %d -> %d",
			got, goldenPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundFixedGoldenSignBiasNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	goldenRef := d.refFrames[vp9CompoundGoldenSlotForTest].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	goldenPix := goldenRef.Y[32]
	altPix := altRef.Y[32]
	if goldenPix == altPix {
		t.Fatalf("compound fixed-GOLDEN pattern missing: GOLDEN=%d ALTREF=%d",
			goldenPix, altPix)
	}
	want := byte((int(goldenPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundFixedGoldenSignBiasNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound fixed-GOLDEN sign-bias frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound fixed-GOLDEN sign-bias frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound fixed-GOLDEN Y[0,0] = %d, want average of GOLDEN %d and ALTREF %d -> %d",
			got, goldenPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundFixedLastSignBiasNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32]
	altPix := altRef.Y[32]
	if lastPix == altPix {
		t.Fatalf("compound fixed-LAST pattern missing: LAST=%d ALTREF=%d",
			lastPix, altPix)
	}
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundFixedLastSignBiasNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound fixed-LAST sign-bias frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound fixed-LAST sign-bias frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left compound fixed-LAST Y[0,0] = %d, want average of LAST %d and ALTREF %d -> %d",
			got, lastPix, altPix, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterReferenceModeSelectNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32]
	altPix := altRef.Y[32]
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterReferenceModeSelectNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode reference-mode-select compound inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("reference-mode-select compound inter newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != want {
		t.Fatalf("top-left reference-mode-select compound newmv Y[0,0] = %d, want %d",
			got, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[16*lastRef.YStride+32]
	altPix := altRef.Y[16*altRef.YStride+32]
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter nearestmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride+32]; got != want {
		t.Fatalf("bottom-right compound nearestmv Y[32,32] = %d, want %d", got, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterNearMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	lastPix := lastRef.Y[32*lastRef.YStride+32]
	altPix := altRef.Y[32*altRef.YStride+32]
	want := byte((int(lastPix) + int(altPix) + 1) >> 1)

	inter := vp9CompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter nearmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter nearmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride+32]; got != want {
		t.Fatalf("bottom-right compound nearmv Y[32,32] = %d, want %d", got, want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsCompoundInterSubpelNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t,
		vp9CompoundInterSubpelNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpEighttap])
}

func TestVP9DecoderReconstructsInterIntraSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterIntraFrameForTest(t, common.VPred, common.DcPred, true, 0)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter-intra skip frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter-intra skip frame did not publish output")
	}
	assertVP9FilledFrame(t, frame, 64, 64, 127, 128, 128)
}

func TestVP9DecoderReconstructsInterIntraResidueFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterIntraFrameForTest(t, common.DcPred, common.DcPred, false, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter-intra residue frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter-intra residue frame did not publish output")
	}
	if got := frame.Y[0]; got <= 128 {
		t.Fatalf("inter-intra residue Y[0,0] = %d, want residual above predictor", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderDecodeIntoCopiesInterSkipFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	dst := newTestImage(64, 64)
	info, err := d.DecodeInto(key, &dst)
	if err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	if !info.ShowFrame {
		t.Fatalf("DecodeInto keyframe info = %+v, want visible frame", info)
	}
	want := appendVP9I420(nil, dst)

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	fillVP9PublicImage(&dst, 77)
	info, err = d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter skip frame: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter info = %+v, want visible non-key 64x64 frame", info)
	}
	got := appendVP9I420(nil, dst)
	if !bytes.Equal(got, want) {
		t.Fatal("DecodeInto inter skip frame did not copy the LAST reference pixels")
	}
}

func TestVP9DecoderInterResidueSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterResidueFrameForTest(t, 64, 64, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter residue err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter residue err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter residue steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredInterResidueSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterResidueFrameLoopFilterForTest(t, 64, 64, 32, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode loop-filtered inter residue err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered inter residue err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered inter residue steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderLoopFilteredInterMotionSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode loop-filtered inter motion err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode loop-filtered inter motion err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("loop-filtered inter motion steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderReconstructsInterResidueFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	if got := keyFrame.Y[0]; got != 128 {
		t.Fatalf("keyframe Y[0,0] = %d, want neutral predictor", got)
	}
	refY0 := keyFrame.Y[0]

	inter := vp9InterResidueFrameForTest(t, 64, 64, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter residue frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter residue frame did not publish output")
	}
	if got := frame.Y[0]; got <= refY0 {
		t.Fatalf("inter residue Y[0,0] = %d, want above copied reference %d",
			got, refY0)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterResidueEdgeFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 96, 96, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode edge keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	refY0 := keyFrame.Y[0]

	inter := vp9InterResidueFrameForTest(t, 96, 96, 32)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode edge inter residue frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("edge inter residue frame did not publish output")
	}
	if got := frame.Y[0]; got <= refY0 {
		t.Fatalf("edge inter residue Y[0,0] = %d, want above copied reference %d",
			got, refY0)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderAppliesLoopFilterInterMotionFrame(t *testing.T) {
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	unfilteredInter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 0)
	filteredInter := vp9InterMotionMvFrameLoopFilterForTest(t, common.ZeroMv, 32)

	unfiltered := vp9DecodeLastVisibleFrameForTest(t, key, unfilteredInter)
	filtered := vp9DecodeLastVisibleFrameForTest(t, key, filteredInter)
	if !vp9YRectDiffers(unfiltered, filtered, 28, 32, 12, 32) {
		t.Fatal("loop-filtered inter motion luma matches unfiltered prediction edge")
	}
	if bytes.Equal(appendVP9YForTest(nil, unfiltered), appendVP9YForTest(nil, filtered)) {
		t.Fatal("loop-filtered inter motion luma matches unfiltered luma")
	}
	assertVP9PlaneFilled(t, "U", filtered.U, filtered.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", filtered.V, filtered.VStride, 32, 32, 128)
}

func TestVP9DecoderInterNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter nearestmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderReconstructsInterNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	refY32 := keyFrame.Y[32]
	if refY32 <= keyFrame.Y[0] {
		t.Fatalf("keyframe test pattern missing: Y[32]=%d Y[0]=%d",
			refY32, keyFrame.Y[0])
	}

	inter := vp9InterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter newmv frame did not publish output")
	}
	if got := frame.Y[0]; got != refY32 {
		t.Fatalf("top-left newmv Y[0,0] = %d, want copied reference Y[0,32] %d",
			got, refY32)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	topRight := keyFrame.Y[32]
	bottomRight := keyFrame.Y[32*keyFrame.YStride+32]
	if topRight <= keyFrame.Y[0] || bottomRight <= keyFrame.Y[32*keyFrame.YStride] {
		t.Fatalf("keyframe motion pattern missing: topRight=%d bottomRight=%d",
			topRight, bottomRight)
	}

	inter := vp9InterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter nearestmv frame did not publish output")
	}
	if got := frame.Y[0]; got != topRight {
		t.Fatalf("top-left newmv Y[0,0] = %d, want copied reference Y[0,32] %d",
			got, topRight)
	}
	if got := frame.Y[32*frame.YStride]; got != bottomRight {
		t.Fatalf("bottom-left nearestmv Y[32,0] = %d, want copied reference Y[32,32] %d",
			got, bottomRight)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterNearMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter nearmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter nearmv frame did not publish output")
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearMv {
		t.Fatalf("bottom-right inter mode = %v, want NEARMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderInterSubpelNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSubpelNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterSubpelBorderSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSubpelTopRightBorderNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter border subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter border subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter border subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderInterSubpelSwitchableSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	inter := vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode inter switchable subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode inter switchable subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("inter switchable subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderDecodeIntoInterSubpelNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	dst := newTestImage(96, 96)
	if _, err := d.DecodeInto(key, &dst); err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	inter := vp9InterSubpelNewMvFrameForTest(t)
	if _, err := d.DecodeInto(inter, &dst); err != nil {
		t.Fatalf("warm DecodeInto inter subpel newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		_, err = d.DecodeInto(inter, &dst)
	})
	if err != nil {
		t.Fatalf("DecodeInto inter subpel newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("DecodeInto inter subpel newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderReconstructsInterSubpelNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var want [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, want[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	inter := vp9InterSubpelNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel newmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride]; got != want[0] {
		t.Fatalf("middle-left subpel newmv Y[32,0] = %d, want filtered reference %d",
			got, want[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsInterSubpelNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var topWant, middleWant [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, topWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0, 32)
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, middleWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	inter := vp9InterSubpelNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel nearestmv frame did not publish output")
	}
	if got := frame.Y[0]; got != topWant[0] {
		t.Fatalf("top-left subpel newmv Y[0,0] = %d, want filtered reference %d",
			got, topWant[0])
	}
	if got := frame.Y[32*frame.YStride]; got != middleWant[0] {
		t.Fatalf("middle-left subpel nearestmv Y[32,0] = %d, want filtered reference %d",
			got, middleWant[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsInterSubpelBilinearNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsInterSubpelNewMvFilter(t,
		vp9InterSubpelBilinearNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpBilinear])
}

func TestVP9DecoderReconstructsInterSubpelSwitchableSmoothNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsInterSubpelNewMvFilter(t,
		vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpEighttapSmooth])
}

func TestVP9DecoderReconstructsCompoundInterSubpelBilinearNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t,
		vp9CompoundInterSubpelBilinearNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpBilinear])
}

func TestVP9DecoderReconstructsCompoundInterSubpelSwitchableSmoothNewMvFrame(t *testing.T) {
	assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t,
		vp9CompoundInterSubpelSwitchableSmoothNewMvFrameForTest(t),
		tables.FilterKernels[vp9dec.InterpEighttapSmooth])
}

func TestVP9DecoderReconstructsInterSubpelSwitchableSharpNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var topWant, middleWant [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, topWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttapSharp],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0, 32)
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, middleWant[:], 32,
		8, 0, tables.FilterKernels[vp9dec.InterpEighttapSharp],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	inter := vp9InterSubpelSwitchableSharpNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel switchable sharp nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel switchable sharp nearestmv frame did not publish output")
	}
	if got := frame.Y[0]; got != topWant[0] {
		t.Fatalf("top-left switchable sharp newmv Y[0,0] = %d, want %d",
			got, topWant[0])
	}
	if got := frame.Y[32*frame.YStride]; got != middleWant[0] {
		t.Fatalf("middle-left switchable sharp nearestmv Y[32,0] = %d, want %d",
			got, middleWant[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsInterSubpelTopRightBorderNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var want [32 * 32]byte
	vp9InterPredictorWithBorderForTest(keyFrame.Y, keyFrame.YStride,
		keyFrame.Width, keyFrame.Height, want[:], 32,
		0, 4, common.Block32x32, vp9dec.MV{Row: -4, Col: 260},
		tables.FilterKernels[vp9dec.InterpEighttap])

	inter := vp9InterSubpelTopRightBorderNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter top-right border subpel newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter top-right border subpel newmv frame did not publish output")
	}
	if got := frame.Y[32]; got != want[0] {
		t.Fatalf("top-right border subpel newmv Y[0,32] = %d, want %d",
			got, want[0])
	}
	if got := frame.Y[32]; got <= 128 {
		t.Fatalf("top-right border subpel newmv Y[0,32] = %d, want residue-driven edge prediction", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsInterIntegerTopRightBorderNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var want [32 * 32]byte
	vp9InterPredictorWithBorderForTest(keyFrame.Y, keyFrame.YStride,
		keyFrame.Width, keyFrame.Height, want[:], 32,
		0, 4, common.Block32x32, vp9dec.MV{Col: 256},
		tables.FilterKernels[vp9dec.InterpEighttap])

	inter := vp9InterIntegerTopRightBorderNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter top-right border integer newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter top-right border integer newmv frame did not publish output")
	}
	if got := frame.Y[32]; got != want[0] {
		t.Fatalf("top-right border integer newmv Y[0,32] = %d, want %d",
			got, want[0])
	}
	if got := frame.Y[32]; got <= 128 {
		t.Fatalf("top-right border integer newmv Y[0,32] = %d, want residue-driven edge prediction", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func assertVP9DecoderReconstructsInterSubpelNewMvFilter(t *testing.T,
	inter []byte,
	kernel *[tables.SubpelShifts][tables.SubpelTaps]int16,
) {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("keyframe did not publish output")
	}
	var want [32 * 32]byte
	vp9dec.InterPredictor(keyFrame.Y, keyFrame.YStride, want[:], 32,
		8, 8, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*keyFrame.YStride+32)

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter subpel filtered newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("inter subpel filtered newmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride]; got != want[0] {
		t.Fatalf("middle-left filtered subpel newmv Y[32,0] = %d, want %d",
			got, want[0])
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func assertVP9DecoderReconstructsCompoundInterSubpelNewMvFilter(t *testing.T,
	inter []byte,
	kernel *[tables.SubpelShifts][tables.SubpelTaps]int16,
) {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 96, 96)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	var lastWant, altWant [32 * 32]byte
	vp9dec.InterPredictor(lastRef.Y, lastRef.YStride, lastWant[:], 32,
		8, 8, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*lastRef.YStride+32)
	vp9dec.InterPredictor(altRef.Y, altRef.YStride, altWant[:], 32,
		8, 8, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*altRef.YStride+32)
	if lastWant[0] == altWant[0] {
		t.Fatalf("compound subpel reference test pattern missing: LAST=%d ALTREF=%d",
			lastWant[0], altWant[0])
	}
	want := byte((int(lastWant[0]) + int(altWant[0]) + 1) >> 1)

	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode compound inter subpel filtered newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("compound inter subpel filtered newmv frame did not publish output")
	}
	if got := frame.Y[32*frame.YStride]; got != want {
		t.Fatalf("middle-left compound filtered subpel newmv Y[32,0] = %d, want average of LAST %d and ALTREF %d -> %d",
			got, lastWant[0], altWant[0], want)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 48, 48, 128)
}

func TestVP9DecoderReconstructsScaledCompoundInterNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)

	zero := vp9ScaledCompoundInterZeroMvFrameForTest(t)
	if err := d.Decode(zero); err != nil {
		t.Fatalf("Decode scaled compound zero-mv frame: %v", err)
	}
	zeroFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound zero-mv frame did not publish output")
	}
	zeroI420 := appendVP9I420(nil, zeroFrame)

	inter := vp9ScaledCompoundInterNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled compound inter newmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound inter newmv frame did not publish output")
	}
	if frame.Width != 32 || frame.Height != 32 {
		t.Fatalf("scaled compound newmv frame = %dx%d, want 32x32",
			frame.Width, frame.Height)
	}
	if bytes.Equal(appendVP9I420(nil, frame), zeroI420) {
		t.Fatal("scaled compound newmv frame matched the zero-mv compound predictor")
	}
}

func TestVP9DecoderReconstructsScaledCompoundInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)

	inter := vp9ScaledCompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled compound inter nearestmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound inter nearestmv frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled compound nearestmv frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearestMv {
		t.Fatalf("bottom-right compound inter mode = %v, want NEARESTMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderReconstructsScaledCompoundInterNearMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)

	inter := vp9ScaledCompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode scaled compound inter nearmv frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("scaled compound inter nearmv frame did not publish output")
	}
	if frame.Width != 64 || frame.Height != 64 {
		t.Fatalf("scaled compound nearmv frame = %dx%d, want 64x64",
			frame.Width, frame.Height)
	}
	miCols := miColsForSize(64)
	if got := d.miGrid[4*miCols+4].Mode; got != common.NearMv {
		t.Fatalf("bottom-right compound inter mode = %v, want NEARMV", got)
	}
	assertVP9PlaneFilled(t, "U", frame.U, frame.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", frame.V, frame.VStride, 32, 32, 128)
}

func TestVP9DecoderDecodeIntoInterSubpelNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(96, 96)
	key := vp9InteriorResidueKeyframeForSubpelTest(t)
	if _, err := d.DecodeInto(key, &dst); err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	var want [32 * 32]byte
	vp9dec.InterPredictor(dst.Y, dst.YStride, want[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*dst.YStride+32)

	inter := vp9InterSubpelNewMvFrameForTest(t)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter subpel newmv frame: %v", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter subpel newmv info = %+v, want visible non-key 96x96 frame", info)
	}
	if got := dst.Y[32*dst.YStride]; got != want[0] {
		t.Fatalf("DecodeInto middle-left subpel newmv Y[32,0] = %d, want %d",
			got, want[0])
	}
	assertVP9PlaneFilled(t, "U", dst.U, dst.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", dst.V, dst.VStride, 48, 48, 128)
}

func TestVP9DecoderDecodeIntoCompoundInterSubpelNewMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 96, 96)
	lastRef := d.refFrames[0].img
	altRef := d.refFrames[vp9CompoundAltrefSlotForTest].img
	var lastWant, altWant [32 * 32]byte
	vp9dec.InterPredictor(lastRef.Y, lastRef.YStride, lastWant[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*lastRef.YStride+32)
	vp9dec.InterPredictor(altRef.Y, altRef.YStride, altWant[:], 32,
		8, 8, tables.FilterKernels[vp9dec.InterpEighttap],
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, 32, 32, 0,
		32*altRef.YStride+32)
	want := byte((int(lastWant[0]) + int(altWant[0]) + 1) >> 1)

	dst := newTestImage(96, 96)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(vp9CompoundInterSubpelNewMvFrameForTest(t), &dst)
	if err != nil {
		t.Fatalf("DecodeInto compound inter subpel newmv frame: %v", err)
	}
	if info.Width != 96 || info.Height != 96 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto compound inter subpel newmv info = %+v, want visible non-key 96x96 frame", info)
	}
	if got := dst.Y[32*dst.YStride]; got != want {
		t.Fatalf("DecodeInto middle-left compound subpel newmv Y[32,0] = %d, want %d",
			got, want)
	}
	assertVP9PlaneFilled(t, "U", dst.U, dst.UStride, 48, 48, 128)
	assertVP9PlaneFilled(t, "V", dst.V, dst.VStride, 48, 48, 128)
}

func TestVP9DecoderDecodeIntoInterNearestMvFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	dst := newTestImage(64, 64)
	key := vp9TopRightResidueKeyframeForNewMvTest(t)
	if _, err := d.DecodeInto(key, &dst); err != nil {
		t.Fatalf("DecodeInto keyframe: %v", err)
	}
	topRight := dst.Y[32]
	bottomRight := dst.Y[32*dst.YStride+32]
	if topRight <= dst.Y[0] || bottomRight <= dst.Y[32*dst.YStride] {
		t.Fatalf("keyframe motion pattern missing: topRight=%d bottomRight=%d",
			topRight, bottomRight)
	}

	inter := vp9InterNearestMvFrameForTest(t)
	fillVP9PublicImage(&dst, 77)
	info, err := d.DecodeInto(inter, &dst)
	if err != nil {
		t.Fatalf("DecodeInto inter nearestmv frame: %v", err)
	}
	if info.Width != 64 || info.Height != 64 || !info.ShowFrame || info.KeyFrame {
		t.Fatalf("DecodeInto inter nearestmv info = %+v, want visible non-key 64x64 frame", info)
	}
	if got := dst.Y[0]; got != topRight {
		t.Fatalf("DecodeInto top-left newmv Y[0,0] = %d, want %d", got, topRight)
	}
	if got := dst.Y[32*dst.YStride]; got != bottomRight {
		t.Fatalf("DecodeInto bottom-left nearestmv Y[32,0] = %d, want %d", got, bottomRight)
	}
	assertVP9PlaneFilled(t, "U", dst.U, dst.UStride, 32, 32, 128)
	assertVP9PlaneFilled(t, "V", dst.V, dst.VStride, 32, 32, 128)
}

func TestVP9DecoderFindsInterMvRefs(t *testing.T) {
	d := &VP9Decoder{}
	const miRows = 8
	const miCols = 8
	d.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	topRight := &d.miGrid[3*miCols+5]
	*topRight = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
		Mv:       [2]vp9dec.MV{{Col: 64}},
	}
	bottomLeft := &d.miGrid[5*miCols+3]
	*bottomLeft = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
		Mv:       [2]vp9dec.MV{{Col: -128}},
	}

	refs, count := d.vp9FindInterMvRefs(tile, miRows, miCols,
		4, 4, common.Block32x32, common.NearMv, vp9dec.LastFrame,
		[vp9dec.MaxRefFrames]uint8{})
	if count != 2 {
		t.Fatalf("mv ref count = %d, want 2", count)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearestMv); got != (vp9dec.MV{Col: 64}) {
		t.Fatalf("nearest candidate = %+v, want col 64", got)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearMv); got != (vp9dec.MV{Col: -128}) {
		t.Fatalf("near candidate = %+v, want col -128", got)
	}
}

func TestVP9DecoderFindsDiffRefMvRefsWithSignBias(t *testing.T) {
	d := &VP9Decoder{}
	const miRows = 8
	const miCols = 8
	d.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	d.miGrid[3*miCols+5] = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame},
		Mv:       [2]vp9dec.MV{{Row: 16, Col: -32}},
	}
	var signBias [vp9dec.MaxRefFrames]uint8
	signBias[vp9dec.GoldenFrame] = 1

	refs, count := d.vp9FindInterMvRefs(tile, miRows, miCols,
		4, 4, common.Block32x32, common.NearestMv, vp9dec.LastFrame,
		signBias)
	if count != 1 {
		t.Fatalf("diff-ref mv ref count = %d, want 1", count)
	}
	if got := refs[0]; got != (vp9dec.MV{Row: -16, Col: 32}) {
		t.Fatalf("diff-ref candidate = %+v, want sign-bias inverted", got)
	}
}

func TestVP9DecoderFindsCompoundInterMvRefs(t *testing.T) {
	d := &VP9Decoder{}
	const miRows = 8
	const miCols = 8
	d.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	topLeft := &d.miGrid[3*miCols+3]
	*topLeft = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		Mv:       [2]vp9dec.MV{{Col: 64}, {Col: 96}},
	}
	topRight := &d.miGrid[3*miCols+5]
	*topRight = vp9dec.NeighborMi{
		Mode:     common.NewMv,
		RefFrame: [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		Mv:       [2]vp9dec.MV{{Col: 128}, {Col: 160}},
	}

	refs, count := d.vp9FindInterMvRefs(tile, miRows, miCols,
		4, 4, common.Block32x32, common.NearMv, vp9dec.AltrefFrame,
		[vp9dec.MaxRefFrames]uint8{})
	if count != 2 {
		t.Fatalf("compound mv ref count = %d, want 2", count)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearestMv); got != (vp9dec.MV{Col: 128}) {
		t.Fatalf("compound nearest candidate = %+v, want clamped ALTREF col 128", got)
	}
	if got := vp9InterModeMvCandidate(refs, count, common.NearMv); got != (vp9dec.MV{Col: 96}) {
		t.Fatalf("compound near candidate = %+v, want ALTREF col 96", got)
	}
}

func TestVP9DecoderInterPredictSourceInBounds(t *testing.T) {
	if !vp9InterPredictSourceInBounds(32, 32, 32, 32, 96, 96, 8, 8) {
		t.Fatal("interior two-axis subpel window rejected")
	}
	if vp9InterPredictSourceInBounds(32, 32, 32, 32, 64, 64, 8, 0) {
		t.Fatal("right-edge horizontal subpel window accepted without border")
	}
	if vp9InterPredictSourceInBounds(0, 32, 32, 32, 96, 96, 8, 0) {
		t.Fatal("left-edge horizontal subpel window accepted without border")
	}
	if vp9InterPredictSourceInBounds(32, 0, 32, 32, 96, 96, 0, 8) {
		t.Fatal("top-edge vertical subpel window accepted without border")
	}
	if !vp9InterPredictSourceInBounds(0, 0, 32, 32, 32, 32, 0, 0) {
		t.Fatal("integer-pel exact window rejected")
	}
}

func vp9InterPredictorWithBorderForTest(src []byte, srcStride, srcWidth, srcHeight int,
	dst []byte, dstStride int,
	miRow, miCol int,
	bsize common.BlockSize,
	mv vp9dec.MV,
	kernel *[tables.SubpelShifts][tables.SubpelTaps]int16,
) {
	bw := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	bh := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	miRows := (srcHeight + 7) >> 3
	miCols := (srcWidth + 7) >> 3
	edges := vp9dec.BlockBoundsEdges{
		MbToLeftEdge:   -((miCol * common.MiSize) * 8),
		MbToRightEdge:  ((miCols - int(common.Num8x8BlocksWideLookup[bsize]) - miCol) * common.MiSize) * 8,
		MbToTopEdge:    -((miRow * common.MiSize) * 8),
		MbToBottomEdge: ((miRows - int(common.Num8x8BlocksHighLookup[bsize]) - miRow) * common.MiSize) * 8,
	}
	mvQ4 := vp9dec.ClampMvToUmvBorderSb(edges, mv, bw, bh, 0, 0)
	subpelX := int(mvQ4.Col) & (vp9dec.SubpelShifts - 1)
	subpelY := int(mvQ4.Row) & (vp9dec.SubpelShifts - 1)
	srcX := miCol*common.MiSize + (int(mvQ4.Col) >> vp9dec.SubpelBitsConst)
	srcY := miRow*common.MiSize + (int(mvQ4.Row) >> vp9dec.SubpelBitsConst)
	predictSrc := src
	predictStride := srcStride
	predictOffset := srcY*srcStride + srcX
	if !vp9InterPredictSourceInBounds(srcX, srcY, bw, bh,
		srcWidth, srcHeight, subpelX, subpelY) {
		left, right, top, bottom := vp9InterPredictSourceMargins(subpelX, subpelY)
		extStride := bw + left + right
		extRows := bh + top + bottom
		var scratch [80 * 80]byte
		startX := srcX - left
		startY := srcY - top
		for y := range extRows {
			sy := vp9ClampInt(startY+y, 0, srcHeight-1)
			srcRow := src[sy*srcStride:]
			dstRow := scratch[y*extStride:]
			for x := range extStride {
				sx := vp9ClampInt(startX+x, 0, srcWidth-1)
				dstRow[x] = srcRow[sx]
			}
		}
		predictSrc = scratch[:extStride*extRows]
		predictStride = extStride
		predictOffset = top*extStride + left
	}
	vp9dec.InterPredictor(predictSrc, predictStride, dst, dstStride,
		subpelX, subpelY, kernel,
		vp9dec.SubpelShifts, vp9dec.SubpelShifts, bw, bh, 0,
		predictOffset)
}

func TestVP9DecoderReconstructsInterSkipEdgeFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 96, 96, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode edge keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterSkipFrameForTest(t, 96, 96)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode edge inter skip frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("edge inter skip frame did not publish output")
	}
	assertVP9NeutralFrame(t, frame, 96, 96)
	if len(d.miGrid) != miColsForSize(96)*miColsForSize(96) {
		t.Fatalf("miGrid len = %d, want full edge grid", len(d.miGrid))
	}
}

func TestVP9DecoderReconstructsInterSkipTiledFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode tiled keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("keyframe did not publish output")
	}

	inter := vp9InterSkipFrameTilesForTest(t, 1024, 64, 1)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode tiled inter skip frame: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("tiled inter skip frame did not publish output")
	}
	assertVP9NeutralFrame(t, frame, 1024, 64)
	if len(d.miGrid) != miColsForSize(1024)*miColsForSize(64) {
		t.Fatalf("miGrid len = %d, want full tiled grid", len(d.miGrid))
	}
}

func TestVP9DecoderRejectsTruncatedInterTile(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}

	inter := vp9InterSkipFrameForTest(t, 64, 64)
	tileStart, err := vp9TileStartForTest(inter)
	if err != nil {
		t.Fatalf("vp9TileStartForTest: %v", err)
	}
	err = d.Decode(inter[:tileStart])
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode truncated inter tile err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 64 || h != 64 {
		t.Fatalf("LastFrameSize() after rejection = (%d, %d), want prior keyframe size", w, h)
	}
}

func assertVP9NeutralFrame(t *testing.T, got Image, width, height int) {
	t.Helper()
	assertVP9FilledFrame(t, got, width, height, 128, 128, 128)
}

func vp9DecodeLastVisibleFrameForTest(t *testing.T, packets ...[]byte) Image {
	t.Helper()
	return vp9DecodeLastVisibleFrameWithOptionsForTest(t, VP9DecoderOptions{},
		packets...)
}

func vp9DecodeLastVisibleFrameWithOptionsForTest(t *testing.T,
	opts VP9DecoderOptions, packets ...[]byte,
) Image {
	t.Helper()
	d, err := NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	var last Image
	ok := false
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, frameOK := d.NextFrame(); frameOK {
			last = frame
			ok = true
		}
	}
	if !ok {
		t.Fatal("packet sequence did not publish a visible frame")
	}
	return last
}

func assertVP9ImagesEqual(t *testing.T, want, got Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, want.Width, want.Height)
	}
	if !vp9VisiblePlanesEqual(want.Y, want.YStride, got.Y, got.YStride,
		want.Width, want.Height) {
		t.Fatal("Y plane differs")
	}
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	if !vp9VisiblePlanesEqual(want.U, want.UStride, got.U, got.UStride,
		uvWidth, uvHeight) {
		t.Fatal("U plane differs")
	}
	if !vp9VisiblePlanesEqual(want.V, want.VStride, got.V, got.VStride,
		uvWidth, uvHeight) {
		t.Fatal("V plane differs")
	}
}

func vp9VisiblePlanesEqual(a []byte, aStride int, b []byte, bStride int,
	width, height int,
) bool {
	for row := range height {
		aStart := row * aStride
		bStart := row * bStride
		if !bytes.Equal(a[aStart:aStart+width], b[bStart:bStart+width]) {
			return false
		}
	}
	return true
}

func appendVP9YForTest(out []byte, img Image) []byte {
	for row := range img.Height {
		start := row * img.YStride
		out = append(out, img.Y[start:start+img.Width]...)
	}
	return out
}

func vp9YRectDiffers(a, b Image, x, y, width, height int) bool {
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			if a.Y[row*a.YStride+col] != b.Y[row*b.YStride+col] {
				return true
			}
		}
	}
	return false
}

func fillVP9PublicImage(img *Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func assertVP9FilledFrame(t *testing.T, got Image, width, height int,
	yValue, uValue, vValue byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	assertVP9PlaneFilled(t, "Y", got.Y, got.YStride, width, height, yValue)
	assertVP9PlaneFilled(t, "U", got.U, got.UStride, uvWidth, uvHeight, uValue)
	assertVP9PlaneFilled(t, "V", got.V, got.VStride, uvWidth, uvHeight, vValue)
}

func assertVP9FilledFrameWithin(t *testing.T, got Image, width, height int,
	yValue, uValue, vValue, tolerance byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	assertVP9PlaneFilledWithin(t, "Y", got.Y, got.YStride, width, height,
		yValue, tolerance)
	assertVP9PlaneFilledWithin(t, "U", got.U, got.UStride, uvWidth, uvHeight,
		uValue, tolerance)
	assertVP9PlaneFilledWithin(t, "V", got.V, got.VStride, uvWidth, uvHeight,
		vValue, tolerance)
}

func assertVP9PlaneFilled(t *testing.T, name string, plane []byte,
	stride, width, height int, want byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	if len(plane) < planeLen(stride, height, width) {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), planeLen(stride, height, width))
	}
	for row := range height {
		for col := range width {
			if got := plane[row*stride+col]; got != want {
				t.Fatalf("%s[%d,%d] = %d, want %d",
					name, row, col, got, want)
			}
		}
	}
}

func assertVP9PlaneFilledWithin(t *testing.T, name string, plane []byte,
	stride, width, height int, want, tolerance byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	if len(plane) < planeLen(stride, height, width) {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), planeLen(stride, height, width))
	}
	for row := range height {
		for col := range width {
			got := plane[row*stride+col]
			if vp9AbsInt(int(got)-int(want)) > int(tolerance) {
				t.Fatalf("%s[%d,%d] = %d, want %d +/- %d",
					name, row, col, got, want, tolerance)
			}
		}
	}
}

func vp9TileStartForTest(packet []byte) (int, error) {
	var br vp9dec.BitReader
	br.Init(packet)
	hdr, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		return 0, err
	}
	return br.BytesRead() + int(hdr.FirstPartitionSize), nil
}

func vp9MultiTileStubPacketForTest(t *testing.T, width, height, log2TileCols int) []byte {
	t.Helper()
	return vp9StubPacketForTest(t, width, height, log2TileCols, common.DcPred)
}

func vp9MultiTileStubPacketWithFrameParallelForTest(t *testing.T,
	width, height, log2TileCols int, frameParallel bool,
) []byte {
	t.Helper()
	return vp9StubPacketWithFrameParallelForTest(t, width, height,
		log2TileCols, common.DcPred, frameParallel)
}

func vp9StubPacketForTest(t *testing.T, width, height, log2TileCols int,
	yMode common.PredictionMode,
) []byte {
	t.Helper()
	return vp9StubPacketWithFrameParallelForTest(t, width, height,
		log2TileCols, yMode, true)
}

func vp9StubPacketWithFrameParallelForTest(t *testing.T, width, height,
	log2TileCols int, yMode common.PredictionMode, frameParallel bool,
) []byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)
	vp9dec.ResetFrameContext(&e.fc)
	e.aboveSegCtx = make([]int8, alignToSb(miCols))
	e.leftSegCtx = make([]int8, common.MiBlockSize)
	e.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: frameParallel,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	header.Tile.Log2TileCols = log2TileCols
	header.Tile.Log2TileRows = 0

	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   yMode,
		TxSize: common.Tx4x4,
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	var seg vp9dec.SegmentationParams
	partitionProbs := tables.KfPartitionProbs
	tileCols := 1 << uint(log2TileCols)
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: tileCols,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: vp9DecoderTileOffset(tileRow, miRows, header.Tile.Log2TileRows),
				MiRowEnd:   vp9DecoderTileOffset(tileRow+1, miRows, header.Tile.Log2TileRows),
				MiColStart: vp9DecoderTileOffset(tileCol, miCols, header.Tile.Log2TileCols),
				MiColEnd:   vp9DecoderTileOffset(tileCol+1, miCols, header.Tile.Log2TileCols),
			}
			e.writeVP9StubModesTileBounds(bw, miRows, miCols, tile,
				&partitionProbs, &seg, baseMi)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SkipZeroKeyframeForTest(t *testing.T, width, height int, writeResidue bool) []byte {
	t.Helper()
	return vp9SkipResidueKeyframeForTest(t, width, height, writeResidue, 0)
}

func vp9SkipResidueKeyframeForTest(t *testing.T, width, height int,
	writeResidue bool, dcCoeff int16,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(header.Quant.BaseQindex),
		BitDepth:   vp9dec.Bits8,
	}, &dq)

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, 16)
	planes[0].LeftContext = make([]uint8, 16)
	planes[1].AboveContext = make([]uint8, 8)
	planes[1].LeftContext = make([]uint8, 8)
	planes[2].AboveContext = make([]uint8, 8)
	planes[2].LeftContext = make([]uint8, 8)

	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   0,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	zeroCoeffs := make([]int16, 1024)
	coeffs := make([]int16, 1024)
	coeffs[0] = dcCoeff
	partitionProbs := tables.KfPartitionProbs
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)

	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			mi := baseMi
			vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
				Seg:       &seg,
				Mi:        &mi,
				TxMode:    common.Only4x4,
				SkipProbs: fc.SkipProbs,
			})
			vp9enc.WriteKeyframeUvMode(bw, common.DcPred, mi.Mode)
			if !writeResidue {
				return nil
			}
			return vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
				BSize:    common.Block64x64,
				MiTxSize: common.Tx4x4,
				IsInter:  0,
				Lossless: false,
				Mi:       &mi,
				Planes:   &planes,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					dq.Y[0],
					dq.Uv[0],
					dq.Uv[0],
				},
				Fc: &fc.CoefProbs,
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					if dcCoeff != 0 && plane == 0 && r == 0 && c == 0 {
						return coeffs[:vp9dec.MaxEobForTxSize(tx)]
					}
					if dcCoeff == 0 {
						return coeffs[:vp9dec.MaxEobForTxSize(tx)]
					}
					return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
				},
			})
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterFrameContextUpdatePacketForTest(t *testing.T, width, height int,
	contextIdx uint8, refreshFrameContext bool,
) ([]byte, uint8) {
	t.Helper()
	w := uint32(width)
	h := uint32(height)

	var probs vp9dec.FrameContext
	vp9dec.ResetFrameContext(&probs)
	var counts vp9enc.FrameCounts
	counts.Skip[0] = [2]uint32{1, 4096}
	var seg vp9dec.SegmentationParams
	aboveSegCtx := make([]int8, alignToSb(miColsForSize(w)))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miColsForSize(w)*miColsForSize(h))
	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   refreshFrameContext,
		FrameParallelDecoding: true,
		FrameContextIdx:       contextIdx,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		CountsArgs: &vp9enc.WriteCompressedHeaderFromCountsArgs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
			CoefStepsize:         1,
			Probs:                &probs,
			Counts:               &counts,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			miCols := miColsForSize(w)
			miRows := miColsForSize(h)
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &probs.PartitionProb,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:          &seg,
				Mi:           &mi,
				Fc:           &probs,
				TxMode:       common.Only4x4,
				FrameRefMode: vp9dec.SingleReference,
				InterpFilter: vp9dec.InterpEighttap,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
			})
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	if probs.SkipProbs[0] == tables.DefaultSkipProbs[0] {
		t.Fatalf("compressed-header counts left skip prob at default %d", probs.SkipProbs[0])
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet, probs.SkipProbs[0]
}

func miColsForSize(v uint32) int {
	return int((v + 7) >> 3)
}

func vp9TopRightResidueKeyframeForNewMvTest(t *testing.T) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionTest(t, 64, 64)
}

func vp9InteriorResidueKeyframeForSubpelTest(t *testing.T) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionTest(t, 96, 96)
}

func vp9ColumnResidueKeyframeForMotionTest(t *testing.T, width, height int) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionLoopFilterTest(t, width, height, 0)
}

func vp9ColumnResidueKeyframeForMotionLoopFilterTest(t *testing.T,
	width, height int, filterLevel uint8,
) []byte {
	t.Helper()
	return vp9ColumnResidueIntraFrameForMotionTest(t, vp9ColumnResidueIntraFrameArgs{
		Width:             width,
		Height:            height,
		KeyFrame:          true,
		ShowFrame:         true,
		RefreshFrameFlags: 0xff,
		FilterLevel:       filterLevel,
		DCCoeff:           32,
	})
}

func vp9ColumnResidueHiddenIntraOnlyFrameForTest(t *testing.T,
	width, height int, refreshFrameFlags uint8, dcCoeff int16,
) []byte {
	t.Helper()
	return vp9ColumnResidueIntraFrameForMotionTest(t, vp9ColumnResidueIntraFrameArgs{
		Width:             width,
		Height:            height,
		KeyFrame:          false,
		ShowFrame:         false,
		RefreshFrameFlags: refreshFrameFlags,
		FilterLevel:       0,
		DCCoeff:           dcCoeff,
	})
}

type vp9ColumnResidueIntraFrameArgs struct {
	Width             int
	Height            int
	KeyFrame          bool
	ShowFrame         bool
	RefreshFrameFlags uint8
	FilterLevel       uint8
	DCCoeff           int16
}

func vp9ColumnResidueIntraFrameForMotionTest(t *testing.T,
	args vp9ColumnResidueIntraFrameArgs,
) []byte {
	t.Helper()
	width := args.Width
	height := args.Height
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 1,
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 0))
	planes[0].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 0))
	planes[1].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[1].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))
	planes[2].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[2].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))

	partitionProbs := tables.KfPartitionProbs
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}
	coeffs := make([]int16, 1024)
	coeffs[0] = args.DCCoeff
	zeroCoeffs := make([]int16, 1024)

	frameType := common.InterFrame
	if args.KeyFrame {
		frameType = common.KeyFrame
	}
	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             frameType,
		IntraOnly:             !args.KeyFrame,
		ShowFrame:             args.ShowFrame,
		RefreshFrameFlags:     args.RefreshFrameFlags,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	header.Loopfilter.FilterLevel = args.FilterLevel

	baseMi := vp9dec.NeighborMi{
		SbType: common.Block32x32,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			var writeErr error
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
					tile := vp9dec.TileBounds{
						MiRowStart: 0,
						MiRowEnd:   miRows,
						MiColStart: 0,
						MiColEnd:   miCols,
					}
					vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
						AboveSegCtx:    aboveSegCtx,
						LeftSegCtx:     leftSegCtx,
						MiRows:         miRows,
						MiCols:         miCols,
						PartitionProbs: &partitionProbs,
						GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
							return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
						},
						WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
							if writeErr != nil {
								return
							}
							cur := baseMi
							cur.SbType = bsize
							if miCol == 4 {
								cur.Skip = 0
							}
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
								Seg:       &seg,
								Mi:        &cur,
								AboveMi:   vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
								LeftMi:    left,
								TxMode:    common.Only4x4,
								SkipProbs: fc.SkipProbs,
							})
							vp9enc.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
							aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, miRow, miCol)
							if cur.Skip != 0 {
								vp9dec.ResetSkipContext(planes[:], bsize, aboveOffsets[:], leftOffsets[:])
							} else {
								writeErr = vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
									BSize:        bsize,
									MiTxSize:     common.Tx4x4,
									IsInter:      0,
									Lossless:     false,
									Mi:           &cur,
									Planes:       &planes,
									AboveOffsets: aboveOffsets,
									LeftOffsets:  leftOffsets,
									PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
										dq.Y[0],
										dq.Uv[0],
										dq.Uv[0],
									},
									Fc: &fc.CoefProbs,
									GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
										if plane == 0 && r == 0 && c == 0 {
											return coeffs[:vp9dec.MaxEobForTxSize(tx)]
										}
										return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
									},
								})
							}
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return writeErr
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentedAltQKeyframeForTest(t *testing.T) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	seg := vp9SegmentationAltQForTest()
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 1,
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 0))
	planes[0].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 0))
	planes[1].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[1].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))
	planes[2].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[2].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))

	partitionProbs := tables.KfPartitionProbs
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}
	coeffsBySeg := [2][]int16{
		make([]int16, 1024),
		make([]int16, 1024),
	}
	for i := range coeffsBySeg {
		coeffsBySeg[i][0] = dq.Y[i][0]
	}
	zeroCoeffs := make([]int16, 1024)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		Seg:                   seg,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	baseMi := vp9dec.NeighborMi{
		SbType: common.Block32x32,
		Mode:   common.DcPred,
		TxSize: common.Tx4x4,
		Skip:   0,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			var writeErr error
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
					tile := vp9dec.TileBounds{
						MiRowStart: 0,
						MiRowEnd:   miRows,
						MiColStart: 0,
						MiColEnd:   miCols,
					}
					vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
						AboveSegCtx:    aboveSegCtx,
						LeftSegCtx:     leftSegCtx,
						MiRows:         miRows,
						MiCols:         miCols,
						PartitionProbs: &partitionProbs,
						GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
							return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
						},
						WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
							if writeErr != nil {
								return
							}
							segID := 0
							if miCol >= 4 {
								segID = 1
							}
							cur := baseMi
							cur.SbType = bsize
							cur.SegmentID = uint8(segID)
							cur.SegIDPredicted = uint8(segID)
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							vp9enc.WriteKeyframeBlock(bw, vp9enc.WriteKeyframeBlockArgs{
								Seg:       &seg,
								Mi:        &cur,
								AboveMi:   vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
								LeftMi:    left,
								TxMode:    common.Only4x4,
								SkipProbs: fc.SkipProbs,
							})
							vp9enc.WriteKeyframeUvMode(bw, common.DcPred, cur.Mode)
							aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, miRow, miCol)
							writeErr = vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
								BSize:        bsize,
								MiTxSize:     common.Tx4x4,
								IsInter:      0,
								Lossless:     false,
								Mi:           &cur,
								Planes:       &planes,
								AboveOffsets: aboveOffsets,
								LeftOffsets:  leftOffsets,
								PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
									dq.Y[segID],
									dq.Uv[segID],
									dq.Uv[segID],
								},
								Fc: &fc.CoefProbs,
								GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
									if plane == 0 && r == 0 && c == 0 {
										return coeffsBySeg[segID][:vp9dec.MaxEobForTxSize(tx)]
									}
									return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
								},
							})
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return writeErr
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentationAltQForTest() vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   true,
	}
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.FeatureMask[1] = 1 << uint(vp9dec.SegLvlAltQ)
	seg.FeatureData[1][vp9dec.SegLvlAltQ] = 96
	return seg
}

func vp9InterIntraFrameForTest(t *testing.T,
	yMode, uvMode common.PredictionMode, skip bool, dcCoeff int16,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 1,
		BitDepth:   vp9dec.Bits8,
	}, &dq)
	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 0))
	planes[0].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 0))
	planes[1].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[1].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))
	planes[2].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[2].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))

	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	zeroCoeffs := make([]int16, 1024)
	coeffs := make([]int16, 1024)
	coeffs[0] = dcCoeff

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	skipFlag := uint8(0)
	if skip {
		skipFlag = 1
	}
	mi := vp9dec.NeighborMi{
		SbType:   common.Block64x64,
		Mode:     yMode,
		TxSize:   common.Tx4x4,
		Skip:     skipFlag,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:          &seg,
				Mi:           &mi,
				Fc:           &fc,
				TxMode:       common.Only4x4,
				FrameRefMode: vp9dec.SingleReference,
				InterpFilter: vp9dec.InterpEighttap,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
				UvMode: uvMode,
			})
			if skip {
				fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
				return nil
			}
			aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(&planes, 0, 0)
			if err := vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
				BSize:        common.Block64x64,
				MiTxSize:     common.Tx4x4,
				IsInter:      0,
				Lossless:     false,
				Mi:           &mi,
				Planes:       &planes,
				AboveOffsets: aboveOffsets,
				LeftOffsets:  leftOffsets,
				PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
					dq.Y[0],
					dq.Uv[0],
					dq.Uv[0],
				},
				Fc: &fc.CoefProbs,
				GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
					if dcCoeff != 0 && plane == 0 && r == 0 && c == 0 {
						return coeffs[:vp9dec.MaxEobForTxSize(tx)]
					}
					if dcCoeff == 0 {
						return coeffs[:vp9dec.MaxEobForTxSize(tx)]
					}
					return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
				},
			}); err != nil {
				return err
			}
			fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterResidueFrameForTest(t *testing.T, width, height int, dcCoeff int16) []byte {
	t.Helper()
	return vp9InterResidueFrameLoopFilterForTest(t, width, height, dcCoeff, 0)
}

func vp9InterResidueFrameLoopFilterForTest(t *testing.T,
	width, height int, dcCoeff int16, filterLevel uint8,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	var dq vp9dec.DequantTables
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: 1,
		BitDepth:   vp9dec.Bits8,
	}, &dq)

	var planes [vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane
	vp9dec.SetupBlockPlanes(&planes, 1, 1)
	planes[0].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 0))
	planes[0].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 0))
	planes[1].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[1].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))
	planes[2].AboveContext = make([]uint8, vp9PlaneEntropyLen(alignToSb(miCols), 1))
	planes[2].LeftContext = make([]uint8, vp9PlaneEntropyLen(common.MiBlockSize, 1))

	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	zeroCoeffs := make([]int16, 1024)
	coeffs := make([]int16, 1024)
	coeffs[0] = dcCoeff

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	header.Loopfilter.FilterLevel = filterLevel

	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         0,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			return writeVP9InterResidueTileForTest(bw, miRows, miCols, tile,
				aboveSegCtx, leftSegCtx, miGrid, &partitionProbs, &seg, &fc,
				&planes, &dq, mi, dcCoeff, coeffs, zeroCoeffs)
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMotionMvFrameForTest(t, common.ZeroMv)
}

func vp9InterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMotionMvFrameForTest(t, common.NearestMv)
}

func vp9InterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMvReuseFrameRefDimsForTest(t, common.NearMv, 64, 64)
}

func vp9InterSubpelNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, false,
		vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9InterSubpelNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, true,
		vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9InterSubpelBilinearNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, false,
		vp9dec.InterpBilinear, vp9dec.InterpBilinear)
}

func vp9InterSubpelSwitchableSmoothNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, false,
		vp9dec.InterpSwitchable, vp9dec.InterpEighttapSmooth)
}

func vp9InterSubpelSwitchableSharpNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSubpelMotionFrameForTest(t, true,
		vp9dec.InterpSwitchable, vp9dec.InterpEighttapSharp)
}

func vp9InterSubpelTopRightBorderNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameForTest(t, 64, 64, 0, 4,
		vp9dec.MV{Row: -4, Col: 260}, vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9InterIntegerTopRightBorderNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameForTest(t, 64, 64, 0, 4,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap)
}

func vp9ScaledNewMvInterFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameRefDimsForTest(t, 32, 32, 0, 0,
		vp9dec.MV{Col: 32}, vp9dec.InterpEighttap, vp9dec.InterpEighttap, 64, 64)
}

func vp9ScaledInterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMvReuseFrameRefDimsForTest(t, common.NearestMv, 128, 128)
}

func vp9ScaledInterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9InterMvReuseFrameRefDimsForTest(t, common.NearMv, 128, 128)
}

const (
	vp9CompoundGoldenSlotForTest = 4
	vp9CompoundAltrefSlotForTest = 5
)

func seedVP9CompoundMotionRefsForTest(t *testing.T, d *VP9Decoder, width, height int) {
	t.Helper()
	key := vp9ColumnResidueKeyframeForMotionTest(t, width, height)
	hidden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, width, height,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode compound LAST seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("compound LAST seed keyframe did not publish output")
	}
	if err := d.Decode(hidden); err != nil {
		t.Fatalf("Decode compound ALTREF seed intra-only frame: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("compound ALTREF seed intra-only frame published output")
	}
	if !d.refFrames[0].valid || !d.refFrames[vp9CompoundAltrefSlotForTest].valid {
		t.Fatal("compound motion reference setup did not populate LAST and ALTREF slots")
	}
}

func seedVP9CompoundTripleRefsForTest(t *testing.T, d *VP9Decoder, width, height int) {
	t.Helper()
	key := vp9ColumnResidueKeyframeForMotionTest(t, width, height)
	golden := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, width, height,
		1<<uint(vp9CompoundGoldenSlotForTest), 32)
	altref := vp9ColumnResidueHiddenIntraOnlyFrameForTest(t, width, height,
		1<<uint(vp9CompoundAltrefSlotForTest), 96)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode compound LAST seed keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("compound LAST seed keyframe did not publish output")
	}
	if err := d.Decode(golden); err != nil {
		t.Fatalf("Decode compound GOLDEN seed intra-only frame: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("compound GOLDEN seed intra-only frame published output")
	}
	if err := d.Decode(altref); err != nil {
		t.Fatalf("Decode compound ALTREF seed intra-only frame: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("compound ALTREF seed intra-only frame published output")
	}
	if !d.refFrames[0].valid ||
		!d.refFrames[vp9CompoundGoldenSlotForTest].valid ||
		!d.refFrames[vp9CompoundAltrefSlotForTest].valid {
		t.Fatal("compound reference setup did not populate LAST/GOLDEN/ALTREF slots")
	}
}

func vp9CompoundInterNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 64, 64, 0, 0,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9CompoundInterGoldenAltrefNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeRefDimsForTest(t, 64, 64, 0, 0,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, vp9CompoundGoldenSlotForTest, vp9CompoundAltrefSlotForTest},
		vp9dec.CompoundReference,
		[2]int8{vp9dec.GoldenFrame, vp9dec.AltrefFrame}, 64, 64)
}

func vp9CompoundFixedGoldenSignBiasNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t,
		64, 64, 0, 0, vp9dec.MV{Col: 256},
		vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, vp9CompoundGoldenSlotForTest, vp9CompoundAltrefSlotForTest},
		vp9dec.CompoundReference, [2]int8{vp9dec.AltrefFrame, vp9dec.GoldenFrame},
		[3]uint8{0, 1, 0}, 64, 64)
}

func vp9CompoundFixedLastSignBiasNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t,
		64, 64, 0, 0, vp9dec.MV{Col: 256},
		vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, vp9CompoundGoldenSlotForTest, vp9CompoundAltrefSlotForTest},
		vp9dec.CompoundReference, [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
		[3]uint8{0, 1, 1}, 64, 64)
}

func vp9CompoundInterReferenceModeSelectNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeForTest(t, 64, 64, 0, 0,
		vp9dec.MV{Col: 256}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest}, vp9dec.ReferenceModeSelect)
}

func vp9CompoundInterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameForTest(t, common.NearestMv)
}

func vp9CompoundInterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameForTest(t, common.NearMv)
}

func vp9ScaledCompoundInterNearestMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameRefDimsForTest(t, common.NearestMv, 128, 128)
}

func vp9ScaledCompoundInterNearMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameRefDimsForTest(t, common.NearMv, 128, 128)
}

func vp9CompoundInterSubpelNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 96, 96, 4, 0,
		vp9dec.MV{Row: 4, Col: 260}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9CompoundInterSubpelBilinearNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 96, 96, 4, 0,
		vp9dec.MV{Row: 4, Col: 260}, vp9dec.InterpBilinear, vp9dec.InterpBilinear,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9CompoundInterSubpelSwitchableSmoothNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameForTest(t, 96, 96, 4, 0,
		vp9dec.MV{Row: 4, Col: 260}, vp9dec.InterpSwitchable, vp9dec.InterpEighttapSmooth,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})
}

func vp9ScaledCompoundInterZeroMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeRefDimsForTest(t, 32, 32, -1, -1,
		vp9dec.MV{}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest}, vp9dec.CompoundReference, 64, 64)
}

func vp9ScaledCompoundInterNewMvFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeRefDimsForTest(t, 32, 32, 0, 0,
		vp9dec.MV{Col: 32}, vp9dec.InterpEighttap, vp9dec.InterpEighttap,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest}, vp9dec.CompoundReference, 64, 64)
}

func vp9InterSingleNewMvFrameForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
) []byte {
	t.Helper()
	return vp9InterSingleNewMvFrameRefDimsForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		uint32(width), uint32(height))
}

func vp9InterSingleNewMvFrameRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          frameFilter,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(blockFilter),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         frameFilter,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv [2]vp9dec.MV
					if miRow == targetMiRow && miCol == targetMiCol {
						cur.Mode = common.NewMv
						mv[0] = targetMV
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:          &seg,
						Mi:           &cur,
						AboveMi:      above,
						LeftMi:       left,
						Fc:           &fc,
						TxMode:       common.Only4x4,
						FrameRefMode: vp9dec.SingleReference,
						InterpFilter: frameFilter,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(above, left),
						AllowHP:             false,
						Mv:                  mv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterMotionMvFrameForTest(t *testing.T, bottomLeftMode common.PredictionMode) []byte {
	t.Helper()
	return vp9InterMotionMvFrameLoopFilterRefDimsForTest(t, bottomLeftMode, 0, 64, 64)
}

func vp9InterMotionMvFrameLoopFilterForTest(t *testing.T,
	bottomLeftMode common.PredictionMode, filterLevel uint8,
) []byte {
	t.Helper()
	return vp9InterMotionMvFrameLoopFilterRefDimsForTest(t, bottomLeftMode,
		filterLevel, 64, 64)
}

func vp9InterMotionMvFrameLoopFilterRefDimsForTest(t *testing.T,
	bottomLeftMode common.PredictionMode, filterLevel uint8,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	header.Loopfilter.FilterLevel = filterLevel

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv [2]vp9dec.MV
					if miRow == 0 && miCol == 0 {
						cur.Mode = common.NewMv
						mv[0] = vp9dec.MV{Col: 256}
					} else if miRow == 4 && miCol == 0 && bottomLeftMode != common.ZeroMv {
						cur.Mode = bottomLeftMode
						mv[0] = vp9dec.MV{Col: 256}
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:          &seg,
						Mi:           &cur,
						AboveMi:      vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol),
						LeftMi:       left,
						Fc:           &fc,
						TxMode:       common.Only4x4,
						FrameRefMode: vp9dec.SingleReference,
						InterpFilter: vp9dec.InterpEighttap,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						AllowHP: false,
						Mv:      mv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterMvReuseFrameRefDimsForTest(t *testing.T,
	reuseMode common.PredictionMode,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	targetMV := vp9dec.MV{Col: 256}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv [2]vp9dec.MV
					switch {
					case reuseMode == common.NearestMv && miRow == 0 && miCol == 0:
						cur.Mode = common.NewMv
						mv[0] = targetMV
					case reuseMode == common.NearestMv && miRow == 4 && miCol == 0:
						cur.Mode = common.NearestMv
						mv[0] = targetMV
					case reuseMode == common.NearMv && miRow == 0 && miCol == 4:
						cur.Mode = common.NewMv
						mv[0] = targetMV
					case reuseMode == common.NearMv && miRow == 4 && miCol == 4:
						cur.Mode = common.NearMv
						mv[0] = targetMV
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:          &seg,
						Mi:           &cur,
						AboveMi:      above,
						LeftMi:       left,
						Fc:           &fc,
						TxMode:       common.Only4x4,
						FrameRefMode: vp9dec.SingleReference,
						InterpFilter: vp9dec.InterpEighttap,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						AllowHP: false,
						Mv:      mv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9InterSubpelMotionFrameForTest(t *testing.T, nearestReuse bool,
	frameFilter, blockFilter vp9dec.InterpFilter,
) []byte {
	t.Helper()
	const width = 96
	const height = 96
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          frameFilter,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(blockFilter),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 131072)
	scratch := make([]byte, 131072)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         frameFilter,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
					vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
						AboveSegCtx:    aboveSegCtx,
						LeftSegCtx:     leftSegCtx,
						MiRows:         miRows,
						MiCols:         miCols,
						PartitionProbs: &partitionProbs,
						GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
							return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
						},
						WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
							cur := baseMi
							cur.SbType = bsize
							var mv [2]vp9dec.MV
							if nearestReuse {
								if miRow == 0 && miCol == 0 {
									cur.Mode = common.NewMv
									mv[0] = vp9dec.MV{Col: 260}
								} else if miRow == 4 && miCol == 0 {
									cur.Mode = common.NearestMv
									mv[0] = vp9dec.MV{Col: 260}
								}
							} else if miRow == 4 && miCol == 0 {
								cur.Mode = common.NewMv
								mv[0] = vp9dec.MV{Row: 4, Col: 260}
							}
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
							vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
								Seg:          &seg,
								Mi:           &cur,
								AboveMi:      above,
								LeftMi:       left,
								Fc:           &fc,
								TxMode:       common.Only4x4,
								FrameRefMode: vp9dec.SingleReference,
								InterpFilter: frameFilter,
								InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
									miRows, miRow, miCol, bsize),
								SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(above, left),
								AllowHP:             false,
								Mv:                  mv,
							})
							cur.Mv = mv
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SetupCompoundHeaderRefsForTest(header *vp9dec.UncompressedHeader,
	refIndex [3]uint8,
) ([vp9dec.MaxRefFrames]uint8, vp9dec.CompoundFrameRefs) {
	return vp9SetupCompoundHeaderRefsSignBiasForTest(header, refIndex, [3]uint8{0, 0, 1})
}

func vp9SetupCompoundHeaderRefsSignBiasForTest(header *vp9dec.UncompressedHeader,
	refIndex [3]uint8, headerSignBias [3]uint8,
) ([vp9dec.MaxRefFrames]uint8, vp9dec.CompoundFrameRefs) {
	header.InterRef.RefIndex = refIndex
	header.InterRef.SignBias = headerSignBias
	signBias := vp9FrameRefSignBias(header)
	return signBias, vp9dec.SetupCompoundReferenceMode(signBias)
}

func vp9CompoundInterMotionFrameForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		refIndex, vp9dec.CompoundReference)
}

func vp9CompoundInterMotionFrameModeForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionFrameModeRefDimsForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		refIndex, referenceMode, uint32(width), uint32(height))
}

func vp9CompoundInterMotionFrameModeRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeRefDimsForTest(t, width, height,
		targetMiRow, targetMiCol, targetMV, frameFilter, blockFilter,
		refIndex, referenceMode,
		[2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}, refWidth, refHeight)
}

func vp9CompoundInterMotionRefsFrameModeRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
	refFrames [2]int8,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	return vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t,
		width, height, targetMiRow, targetMiCol, targetMV,
		frameFilter, blockFilter, refIndex, referenceMode, refFrames,
		[3]uint8{0, 0, 1}, refWidth, refHeight)
}

func vp9CompoundInterMotionRefsFrameModeSignBiasRefDimsForTest(t *testing.T,
	width, height int,
	targetMiRow, targetMiCol int,
	targetMV vp9dec.MV,
	frameFilter, blockFilter vp9dec.InterpFilter,
	refIndex [3]uint8,
	referenceMode vp9dec.ReferenceMode,
	refFrames [2]int8,
	headerSignBias [3]uint8,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          frameFilter,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	signBias, refs := vp9SetupCompoundHeaderRefsSignBiasForTest(&header,
		refIndex, headerSignBias)

	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(blockFilter),
		Skip:         1,
		RefFrame:     refFrames,
	}
	dest := make([]byte, 131072)
	scratch := make([]byte, 131072)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         frameFilter,
			ReferenceMode:        referenceMode,
			CompoundRefAllowed:   true,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
				for i := range leftSegCtx {
					leftSegCtx[i] = 0
				}
				for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
					tile := vp9dec.TileBounds{
						MiRowStart: 0,
						MiRowEnd:   miRows,
						MiColStart: 0,
						MiColEnd:   miCols,
					}
					vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
						AboveSegCtx:    aboveSegCtx,
						LeftSegCtx:     leftSegCtx,
						MiRows:         miRows,
						MiCols:         miCols,
						PartitionProbs: &partitionProbs,
						GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
							return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
						},
						WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
							cur := baseMi
							cur.SbType = bsize
							var mv [2]vp9dec.MV
							if miRow == targetMiRow && miCol == targetMiCol {
								cur.Mode = common.NewMv
								mv[0] = targetMV
								mv[1] = targetMV
							}
							var left *vp9dec.NeighborMi
							if miCol > tile.MiColStart {
								left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
							}
							above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
							vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
								Seg:              &seg,
								Mi:               &cur,
								AboveMi:          above,
								LeftMi:           left,
								Fc:               &fc,
								TxMode:           common.Only4x4,
								FrameRefMode:     referenceMode,
								InterpFilter:     frameFilter,
								CompFixedRef:     refs.CompFixedRef,
								CompVarRef:       refs.CompVarRef,
								RefFrameSignBias: signBias,
								InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
									miRows, miRow, miCol, bsize),
								SwitchableInterpCtx: vp9dec.GetPredContextSwitchableInterp(above, left),
								AllowHP:             false,
								IsCompound:          true,
								Mv:                  mv,
							})
							cur.Mv = mv
							fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
						},
					}, miRow, miCol, common.Block64x64)
				}
			}
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9CompoundInterMvReuseFrameForTest(t *testing.T,
	reuseMode common.PredictionMode,
) []byte {
	t.Helper()
	return vp9CompoundInterMvReuseFrameRefDimsForTest(t, reuseMode, 64, 64)
}

func vp9CompoundInterMvReuseFrameRefDimsForTest(t *testing.T,
	reuseMode common.PredictionMode,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	decodedGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	planGrid := make([]vp9dec.NeighborMi, miRows*miCols)
	for miRow := 0; miRow < miRows; miRow += 4 {
		for miCol := 0; miCol < miCols; miCol += 4 {
			fillVP9MiGridForTest(planGrid, miRows, miCols, miRow, miCol,
				common.Block32x32, vp9dec.NeighborMi{SbType: common.Block32x32})
		}
	}

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	signBias, refs := vp9SetupCompoundHeaderRefsForTest(&header,
		[3]uint8{0, 0, vp9CompoundAltrefSlotForTest})

	firstMV := vp9dec.MV{}
	secondMV := vp9dec.MV{Row: -128}
	baseMi := vp9dec.NeighborMi{
		SbType:       common.Block32x32,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.CompoundReference,
			CompoundRefAllowed:   true,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			vp9enc.WriteModesSb(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
				GetMi: func(miRow, miCol int) *vp9dec.NeighborMi {
					return vp9MiGridAtForTest(planGrid, miRows, miCols, miRow, miCol)
				},
				WriteB: func(bw *bitstream.Writer, miRow, miCol int, bsize common.BlockSize) {
					cur := baseMi
					cur.SbType = bsize
					var mv, bestRefMv [2]vp9dec.MV
					switch {
					case miRow == 0 && miCol == 0:
						cur.Mode = common.NewMv
						mv = [2]vp9dec.MV{firstMV, firstMV}
					case miRow == 0 && miCol == 4:
						cur.Mode = common.NewMv
						mv = [2]vp9dec.MV{secondMV, secondMV}
						bestRefMv = [2]vp9dec.MV{firstMV, firstMV}
					case miRow == 4 && miCol == 4:
						cur.Mode = reuseMode
						if reuseMode == common.NearMv {
							mv = [2]vp9dec.MV{firstMV, firstMV}
						} else {
							mv = [2]vp9dec.MV{secondMV, secondMV}
						}
					}
					var left *vp9dec.NeighborMi
					if miCol > tile.MiColStart {
						left = vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow, miCol-1)
					}
					above := vp9MiGridAtForTest(decodedGrid, miRows, miCols, miRow-1, miCol)
					vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
						Seg:              &seg,
						Mi:               &cur,
						AboveMi:          above,
						LeftMi:           left,
						Fc:               &fc,
						TxMode:           common.Only4x4,
						FrameRefMode:     vp9dec.CompoundReference,
						InterpFilter:     vp9dec.InterpEighttap,
						CompFixedRef:     refs.CompFixedRef,
						CompVarRef:       refs.CompVarRef,
						RefFrameSignBias: signBias,
						InterModeCtx: vp9dec.InterModeContext(decodedGrid, miCols, tile,
							miRows, miRow, miCol, bsize),
						AllowHP:    false,
						IsCompound: true,
						Mv:         mv,
						BestRefMv:  bestRefMv,
					})
					cur.Mv = mv
					fillVP9MiGridForTest(decodedGrid, miRows, miCols, miRow, miCol, bsize, cur)
				},
			}, 0, 0, common.Block64x64)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func writeVP9InterResidueTileForTest(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range leftSegCtx {
			leftSegCtx[i] = 0
		}
		for plane := range vp9dec.MaxMbPlane {
			left := planes[plane].LeftContext
			for i := range left {
				left[i] = 0
			}
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeVP9InterResidueSbForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	if miRow >= miRows || miCol >= miCols {
		return nil
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, bsize)
	partition := common.PartitionLookup[bsl][target]
	vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
		AboveSegCtx:    aboveSegCtx,
		LeftSegCtx:     leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
			subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
			dcCoeff, coeffs, zeroCoeffs); err != nil {
			return err
		}
	} else {
		switch partition {
		case common.PartitionNone:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		case common.PartitionHorz:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if miRow+bs < miRows {
				if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
					dcCoeff, coeffs, zeroCoeffs); err != nil {
					return err
				}
			}
		case common.PartitionVert:
			if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if miCol+bs < miCols {
				if err := writeVP9InterResidueBlockForTest(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, miGrid, seg, fc, planes, dq, baseMi,
					dcCoeff, coeffs, zeroCoeffs); err != nil {
					return err
				}
			}
		default:
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
			if err := writeVP9InterResidueSbForTest(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, planes, dq, baseMi,
				dcCoeff, coeffs, zeroCoeffs); err != nil {
				return err
			}
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(aboveSegCtx, leftSegCtx,
			miRow, miCol, subsize, vp9PartitionContextUpdateWidth(bs))
	}
	return nil
}

func writeVP9InterResidueBlockForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	miGrid []vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	dq *vp9dec.DequantTables,
	baseMi vp9dec.NeighborMi,
	dcCoeff int16,
	coeffs, zeroCoeffs []int16,
) error {
	cur := baseMi
	cur.SbType = bsize
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = vp9MiGridAtForTest(miGrid, miRows, miCols, miRow, miCol-1)
	}
	vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
		Seg:          seg,
		Mi:           &cur,
		AboveMi:      vp9MiGridAtForTest(miGrid, miRows, miCols, miRow-1, miCol),
		LeftMi:       left,
		Fc:           fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
		InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
			miRows, miRow, miCol, bsize),
	})
	aboveOffsets, leftOffsets := vp9PlaneContextOffsetsForTest(planes, miRow, miCol)
	if err := vp9enc.WriteCoefSb(bw, vp9enc.WriteCoefSbArgs{
		BSize:        bsize,
		MiTxSize:     common.Tx4x4,
		IsInter:      1,
		Lossless:     false,
		Mi:           &cur,
		Planes:       planes,
		AboveOffsets: aboveOffsets,
		LeftOffsets:  leftOffsets,
		PlaneDequant: [vp9dec.MaxMbPlane][2]int16{
			dq.Y[0],
			dq.Uv[0],
			dq.Uv[0],
		},
		Fc: &fc.CoefProbs,
		GetCoeffs: func(plane, r, c int, tx common.TxSize) []int16 {
			if dcCoeff != 0 && plane == 0 && r == 0 && c == 0 {
				return coeffs[:vp9dec.MaxEobForTxSize(tx)]
			}
			if dcCoeff == 0 {
				return coeffs[:vp9dec.MaxEobForTxSize(tx)]
			}
			return zeroCoeffs[:vp9dec.MaxEobForTxSize(tx)]
		},
	}); err != nil {
		return err
	}
	fillVP9MiGridForTest(miGrid, miRows, miCols, miRow, miCol, bsize, cur)
	return nil
}

func vp9PlaneContextOffsetsForTest(planes *[vp9dec.MaxMbPlane]vp9dec.MacroblockdPlane,
	miRow, miCol int,
) (above [vp9dec.MaxMbPlane]int, left [vp9dec.MaxMbPlane]int) {
	for plane := range vp9dec.MaxMbPlane {
		pd := &planes[plane]
		above[plane] = (miCol * 2) >> pd.SubsamplingX
		left[plane] = ((miRow * 2) >> pd.SubsamplingY) % len(pd.LeftContext)
	}
	return above, left
}

func vp9CompoundInterSkipFrameForTest(t *testing.T) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	signBias, refs := vp9SetupCompoundHeaderRefsForTest(&header, [3]uint8{0, 0, 0})

	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.CompoundReference,
			CompoundRefAllowed:   true,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:              &seg,
				Mi:               &mi,
				Fc:               &fc,
				TxMode:           common.Only4x4,
				FrameRefMode:     vp9dec.CompoundReference,
				InterpFilter:     vp9dec.InterpEighttap,
				CompFixedRef:     refs.CompFixedRef,
				CompVarRef:       refs.CompVarRef,
				RefFrameSignBias: signBias,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
				IsCompound: true,
			})
			fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentedAltrefInterSkipFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9SegmentedAltrefInterSkipFrameUpdateForTest(t, true)
}

func vp9SegmentedAltrefInterSkipMapReuseFrameForTest(t *testing.T) []byte {
	t.Helper()
	return vp9SegmentedAltrefInterSkipFrameUpdateForTest(t, false)
}

func vp9SegmentedAltrefInterSkipFrameUpdateForTest(t *testing.T, updateMap bool) []byte {
	t.Helper()
	const width = 64
	const height = 64
	w := uint32(width)
	h := uint32(height)
	miCols := miColsForSize(w)
	miRows := miColsForSize(h)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	seg := vp9SegmentationAltrefSkipForTest()
	if !updateMap {
		seg.UpdateMap = false
		seg.UpdateData = false
	}
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		FrameContextIdx:       0,
		Seg:                   seg,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	header.InterRef.RefIndex = [3]uint8{0, 0, vp9CompoundAltrefSlotForTest}

	mi := vp9dec.NeighborMi{
		SbType:         common.Block64x64,
		Mode:           common.ZeroMv,
		TxSize:         common.Tx4x4,
		InterpFilter:   uint8(vp9dec.InterpEighttap),
		Skip:           1,
		SegmentID:      1,
		SegIDPredicted: 1,
		RefFrame:       [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: 0,
				MiRowEnd:   miRows,
				MiColStart: 0,
				MiColEnd:   miCols,
			}
			bsl := int(common.BWidthLog2Lookup[common.Block64x64])
			bs := (1 << uint(bsl)) / 4
			vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
				AboveSegCtx:    aboveSegCtx,
				LeftSegCtx:     leftSegCtx,
				MiRows:         miRows,
				MiCols:         miCols,
				PartitionProbs: &partitionProbs,
			}, 0, 0, common.PartitionNone, common.Block64x64, bs)
			vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
				Seg:          &seg,
				Mi:           &mi,
				Fc:           &fc,
				TxMode:       common.Only4x4,
				FrameRefMode: vp9dec.SingleReference,
				InterpFilter: vp9dec.InterpEighttap,
				InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
					miRows, 0, 0, common.Block64x64),
			})
			fillVP9MiGridForTest(miGrid, miRows, miCols, 0, 0, common.Block64x64, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return w, h
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9SegmentationAltrefSkipForTest() vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
	}
	for i := range seg.TreeProbs {
		seg.TreeProbs[i] = 128
	}
	seg.FeatureMask[1] = (1 << uint(vp9dec.SegLvlRefFrame)) |
		(1 << uint(vp9dec.SegLvlSkip))
	seg.FeatureData[1][vp9dec.SegLvlRefFrame] = int16(vp9dec.AltrefFrame)
	return seg
}

func vp9InterSkipFrameForTest(t *testing.T, width, height int) []byte {
	t.Helper()
	return vp9InterSkipFrameTilesForTest(t, width, height, 0)
}

func vp9InterSkipFrameTilesForTest(t *testing.T, width, height, log2TileCols int) []byte {
	t.Helper()
	return vp9InterSkipFrameTilesWithFrameParallelForTest(t, width, height,
		log2TileCols, true)
}

func vp9InterSkipFrameTilesWithFrameParallelForTest(t *testing.T,
	width, height, log2TileCols int, frameParallel bool,
) []byte {
	t.Helper()
	return vp9InterSkipFrameRefDimsWithFrameParallelForTest(t, width, height,
		log2TileCols, uint32(width), uint32(height), frameParallel)
}

func vp9ScaledZeroMvInterFrameForTest(t *testing.T, width, height, refWidth, refHeight int) []byte {
	t.Helper()
	return vp9InterSkipFrameRefDimsForTest(t, width, height, 0,
		uint32(refWidth), uint32(refHeight))
}

func vp9InterSkipFrameRefDimsForTest(t *testing.T, width, height, log2TileCols int,
	refWidth, refHeight uint32,
) []byte {
	t.Helper()
	return vp9InterSkipFrameRefDimsWithFrameParallelForTest(t, width, height,
		log2TileCols, refWidth, refHeight, true)
}

func vp9InterSkipFrameRefDimsWithFrameParallelForTest(t *testing.T,
	width, height, log2TileCols int, refWidth, refHeight uint32,
	frameParallel bool,
) []byte {
	t.Helper()
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)

	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var seg vp9dec.SegmentationParams
	partitionProbs := fc.PartitionProb
	aboveSegCtx := make([]int8, alignToSb(miCols))
	leftSegCtx := make([]int8, common.MiBlockSize)
	miGrid := make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0,
		Width:                 w,
		Height:                h,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: frameParallel,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:     vp9dec.Bits8,
			ColorSpace:   common.CSUnknown,
			ColorRange:   common.CRStudioRange,
			SubsamplingX: 1,
			SubsamplingY: 1,
		},
	}
	header.Quant.BaseQindex = 1
	header.Tile.Log2TileCols = log2TileCols

	mi := vp9dec.NeighborMi{
		SbType:       common.Block64x64,
		Mode:         common.ZeroMv,
		TxSize:       common.Tx4x4,
		InterpFilter: uint8(vp9dec.InterpEighttap),
		Skip:         1,
		RefFrame:     [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame},
	}
	dest := make([]byte, 65536)
	scratch := make([]byte, 65536)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:             false,
			TxMode:               common.Only4x4,
			IntraOnly:            false,
			InterpFilter:         vp9dec.InterpEighttap,
			ReferenceMode:        vp9dec.SingleReference,
			CompoundRefAllowed:   false,
			AllowHighPrecisionMv: false,
		},
		TileRows: 1,
		TileCols: 1 << uint(log2TileCols),
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: vp9DecoderTileOffset(tileRow, miRows, header.Tile.Log2TileRows),
				MiRowEnd:   vp9DecoderTileOffset(tileRow+1, miRows, header.Tile.Log2TileRows),
				MiColStart: vp9DecoderTileOffset(tileCol, miCols, header.Tile.Log2TileCols),
				MiColEnd:   vp9DecoderTileOffset(tileCol+1, miCols, header.Tile.Log2TileCols),
			}
			writeVP9InterSkipTileForTest(bw, miRows, miCols, tile,
				aboveSegCtx, leftSegCtx, miGrid, &partitionProbs, &seg, &fc, mi)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) {
			return refWidth, refHeight
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func writeVP9InterSkipTileForTest(bw *bitstream.Writer, miRows, miCols int,
	tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	baseMi vp9dec.NeighborMi,
) {
	for miRow := tile.MiRowStart; miRow < tile.MiRowEnd; miRow += common.MiBlockSize {
		for i := range leftSegCtx {
			leftSegCtx[i] = 0
		}
		for miCol := tile.MiColStart; miCol < tile.MiColEnd; miCol += common.MiBlockSize {
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow, miCol,
				common.Block64x64, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
		}
	}
}

func writeVP9InterSkipSbForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	aboveSegCtx, leftSegCtx []int8,
	miGrid []vp9dec.NeighborMi,
	partitionProbs *[common.PartitionContexts][common.PartitionTypes - 1]uint8,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	baseMi vp9dec.NeighborMi,
) {
	if miRow >= miRows || miCol >= miCols {
		return
	}
	bsl := int(common.BWidthLog2Lookup[bsize])
	bs := (1 << uint(bsl)) / 4
	target := vp9StubBlockSizeForRegion(miRows, miCols, miRow, miCol, bsize)
	partition := common.PartitionLookup[bsl][target]
	vp9enc.WritePartitionForBlock(bw, vp9enc.WriteModesSbArgs{
		AboveSegCtx:    aboveSegCtx,
		LeftSegCtx:     leftSegCtx,
		MiRows:         miRows,
		MiCols:         miCols,
		PartitionProbs: partitionProbs,
	}, miRow, miCol, partition, bsize, bs)

	subsize := common.SubsizeLookup[partition][bsize]
	if subsize < common.Block8x8 {
		writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
			subsize, tile, miGrid, seg, fc, baseMi)
	} else {
		switch partition {
		case common.PartitionNone:
			writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, baseMi)
		case common.PartitionHorz:
			writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, baseMi)
			if miRow+bs < miRows {
				writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow+bs, miCol,
					subsize, tile, miGrid, seg, fc, baseMi)
			}
		case common.PartitionVert:
			writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, miGrid, seg, fc, baseMi)
			if miCol+bs < miCols {
				writeVP9InterSkipBlockForTest(bw, miRows, miCols, miRow, miCol+bs,
					subsize, tile, miGrid, seg, fc, baseMi)
			}
		default:
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow+bs, miCol,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
			writeVP9InterSkipSbForTest(bw, miRows, miCols, miRow+bs, miCol+bs,
				subsize, tile, aboveSegCtx, leftSegCtx, miGrid,
				partitionProbs, seg, fc, baseMi)
		}
	}

	if bsize >= common.Block8x8 &&
		(bsize == common.Block8x8 || partition != common.PartitionSplit) {
		vp9dec.UpdatePartitionContext(aboveSegCtx, leftSegCtx,
			miRow, miCol, subsize, vp9PartitionContextUpdateWidth(bs))
	}
}

func writeVP9InterSkipBlockForTest(bw *bitstream.Writer, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, tile vp9dec.TileBounds,
	miGrid []vp9dec.NeighborMi,
	seg *vp9dec.SegmentationParams,
	fc *vp9dec.FrameContext,
	baseMi vp9dec.NeighborMi,
) {
	cur := baseMi
	cur.SbType = bsize
	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = vp9MiGridAtForTest(miGrid, miRows, miCols, miRow, miCol-1)
	}
	vp9enc.WriteInterBlock(bw, vp9enc.WriteInterBlockArgs{
		Seg:          seg,
		Mi:           &cur,
		AboveMi:      vp9MiGridAtForTest(miGrid, miRows, miCols, miRow-1, miCol),
		LeftMi:       left,
		Fc:           fc,
		TxMode:       common.Only4x4,
		FrameRefMode: vp9dec.SingleReference,
		InterpFilter: vp9dec.InterpEighttap,
		InterModeCtx: vp9dec.InterModeContext(miGrid, miCols, tile,
			miRows, miRow, miCol, bsize),
	})
	fillVP9MiGridForTest(miGrid, miRows, miCols, miRow, miCol, bsize, cur)
}

func vp9MiGridAtForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int) *vp9dec.NeighborMi {
	if r < 0 || c < 0 || r >= miRows || c >= miCols {
		return nil
	}
	return &miGrid[r*miCols+c]
}

func fillVP9MiGridForTest(miGrid []vp9dec.NeighborMi, miRows, miCols, r, c int,
	bsize common.BlockSize, mi vp9dec.NeighborMi,
) {
	rows := int(common.Num8x8BlocksHighLookup[bsize])
	cols := int(common.Num8x8BlocksWideLookup[bsize])
	for rr := 0; rr < rows && r+rr < miRows; rr++ {
		row := miGrid[(r+rr)*miCols:]
		for cc := 0; cc < cols && c+cc < miCols; cc++ {
			row[c+cc] = mi
		}
	}
}

// TestVP9DecoderMaxWidthRejectsLargerKeyframe: a header whose width
// exceeds the configured MaxWidth gets rejected before tile parsing or
// output publication.
func TestVP9DecoderMaxWidthRejectsLargerKeyframe(t *testing.T) {
	var pk vp9BitPacker
	pk.writeLiteral(2, 2)
	pk.writeLiteral(0, 2)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(1)
	pk.writeBit(0)
	pk.writeLiteral(0x49, 8)
	pk.writeLiteral(0x83, 8)
	pk.writeLiteral(0x42, 8)
	pk.writeLiteral(2, 3)
	pk.writeBit(0)
	pk.writeLiteral(319, 16) // width-1 → 320
	pk.writeLiteral(239, 16)
	pk.writeBit(0)
	pk.writeBit(1)
	pk.writeBit(0)
	pk.writeLiteral(1, 2)
	pk.writeLiteral(8, 6)
	pk.writeLiteral(2, 3)
	pk.writeBit(0)
	pk.writeLiteral(64, 8)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeLiteral(42, 16)
	pk.flushByte()

	d, _ := NewVP9Decoder(VP9DecoderOptions{MaxWidth: 160})
	err := d.Decode(pk.buf)
	if !errors.Is(err, ErrFrameRejected) {
		t.Errorf("Decode err = %v, want ErrFrameRejected", err)
	}
}

// vp9BitPacker is a tiny MSB-first bit packer for test inputs.
// Packs writes left-to-right within each byte. flushByte tops up
// the current byte with zeros to align on a byte boundary.
type vp9BitPacker struct {
	buf []byte
	pos int // bit position from MSB of current byte
}

func (p *vp9BitPacker) writeBit(b uint32) {
	if p.pos == 0 {
		p.buf = append(p.buf, 0)
	}
	if b != 0 {
		p.buf[len(p.buf)-1] |= 1 << (7 - p.pos)
	}
	p.pos = (p.pos + 1) & 7
}

func (p *vp9BitPacker) writeLiteral(v uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		p.writeBit((v >> uint(i)) & 1)
	}
}

func (p *vp9BitPacker) flushByte() {
	if p.pos != 0 {
		p.pos = 0
	}
}

func vp9ShowExistingFramePacketForTest(slot uint8) []byte {
	var pk vp9BitPacker
	pk.writeLiteral(2, 2)              // frame_marker = 0b10
	pk.writeLiteral(0, 2)              // profile = 0
	pk.writeBit(1)                     // show_existing_frame
	pk.writeLiteral(uint32(slot&7), 3) // frame_to_show_map_idx
	pk.flushByte()
	return pk.buf
}

func vp9SuperframePacketForTest(frames ...[]byte) []byte {
	packet, err := PackVP9Superframe(frames...)
	if err != nil {
		panic(err)
	}
	return packet
}

// TestVP9DecoderClose marks the decoder as closed; subsequent Decode
// returns ErrClosed.
func TestVP9DecoderClose(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = d.Decode([]byte{0x82})
	if !errors.Is(err, ErrClosed) {
		t.Errorf("after Close, Decode err = %v, want ErrClosed", err)
	}
	// Double-close returns ErrClosed too.
	if err := d.Close(); !errors.Is(err, nil) {
		// Allow either nil or ErrClosed for idempotent close — the
		// VP8 decoder returns nil; mirror that.
		if !errors.Is(err, ErrClosed) {
			t.Errorf("second Close err = %v", err)
		}
	}
}
