package govpx_test

import (
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// TestVP8DecoderLastControlsBeforeDecode asserts the libvpx-style getters
// report ok=false on a nil/closed decoder and before the first successful
// Decode call. Mirrors libvpx's VPX_CODEC_INVALID_PARAM / no-frame guard
// in vp8_get_frame_corrupted / vp8_get_last_ref_updates /
// vp8_get_last_ref_frame (vp8/vp8_dx_iface.c).
func TestVP8DecoderLastControlsBeforeDecode(t *testing.T) {
	var nilDec *govpx.VP8Decoder
	if _, ok := nilDec.LastFrameCorrupted(); ok {
		t.Fatalf("nil decoder LastFrameCorrupted ok = true, want false")
	}
	if _, _, ok := nilDec.LastQuantizer(); ok {
		t.Fatalf("nil decoder LastQuantizer ok = true, want false")
	}
	if _, ok := nilDec.LastReferenceUpdates(); ok {
		t.Fatalf("nil decoder LastReferenceUpdates ok = true, want false")
	}
	if _, ok := nilDec.LastReferencesUsed(); ok {
		t.Fatalf("nil decoder LastReferencesUsed ok = true, want false")
	}

	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder error = %v", err)
	}
	if _, ok := d.LastFrameCorrupted(); ok {
		t.Fatalf("pre-decode LastFrameCorrupted ok = true, want false")
	}
	if _, _, ok := d.LastQuantizer(); ok {
		t.Fatalf("pre-decode LastQuantizer ok = true, want false")
	}
	if _, ok := d.LastReferenceUpdates(); ok {
		t.Fatalf("pre-decode LastReferenceUpdates ok = true, want false")
	}
	if _, ok := d.LastReferencesUsed(); ok {
		t.Fatalf("pre-decode LastReferencesUsed ok = true, want false")
	}
}

// TestVP8DecoderLastControlsAfterKeyFrame asserts the libvpx-style getters
// after a clean key-frame decode: not corrupted, all three reference
// buffers refreshed, no references used (libvpx vp8_get_last_ref_frame
// returns 0 for key frames per vp8dx_references_buffer scanning intra
// modes).
func TestVP8DecoderLastControlsAfterKeyFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder error = %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}

	corrupted, ok := d.LastFrameCorrupted()
	if !ok {
		t.Fatalf("LastFrameCorrupted ok = false, want true")
	}
	if corrupted {
		t.Fatalf("LastFrameCorrupted = true on clean key frame, want false")
	}
	publicQ, internalQ, ok := d.LastQuantizer()
	if !ok {
		t.Fatalf("LastQuantizer ok = false, want true")
	}
	info, ok := d.LastFrameInfo()
	if !ok {
		t.Fatalf("LastFrameInfo ok = false, want true")
	}
	if publicQ != info.Quantizer || internalQ != info.InternalQuantizer {
		t.Fatalf("LastQuantizer = public:%d internal:%d, want LastFrameInfo public:%d internal:%d",
			publicQ, internalQ, info.Quantizer, info.InternalQuantizer)
	}

	updates, ok := d.LastReferenceUpdates()
	if !ok {
		t.Fatalf("LastReferenceUpdates ok = false, want true")
	}
	wantUpdates := govpx.ReferenceFlagLast | govpx.ReferenceFlagGolden | govpx.ReferenceFlagAltRef
	if updates != wantUpdates {
		t.Fatalf("LastReferenceUpdates = %#x, want %#x (all three)", updates, wantUpdates)
	}

	used, ok := d.LastReferencesUsed()
	if !ok {
		t.Fatalf("LastReferencesUsed ok = false, want true")
	}
	if used != 0 {
		t.Fatalf("LastReferencesUsed = %#x on key frame, want 0", used)
	}
}

// TestVP8DecoderLastControlsAfterInterFrame asserts the LAST_REF_USED
// getter surfaces the LAST reference flag when an inter frame's MBs
// reference it. Mirrors vp8dx_references_buffer's per-MB scan.
func TestVP8DecoderLastControlsAfterInterFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder error = %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}
	if err := d.Decode(vp8test.InterFramePacketWithFirstPartition(vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.OnePartition, false, 0))); err != nil {
		t.Fatalf("inter Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}

	corrupted, ok := d.LastFrameCorrupted()
	if !ok || corrupted {
		t.Fatalf("LastFrameCorrupted = (%v, %v), want (false, true)", corrupted, ok)
	}
	used, ok := d.LastReferencesUsed()
	if !ok {
		t.Fatalf("LastReferencesUsed ok = false, want true")
	}
	if used&govpx.ReferenceFlagLast == 0 {
		t.Fatalf("LastReferencesUsed = %#x, want at least ReferenceFlagLast set", used)
	}
}

// TestVP8DecoderLastControlsAfterClose asserts the getters fail closed
// after Close, mirroring libvpx's invalid-context rejection.
func TestVP8DecoderLastControlsAfterClose(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder error = %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}
	if _, ok := d.LastFrameCorrupted(); !ok {
		t.Fatalf("LastFrameCorrupted ok = false before Close")
	}

	d.Close()

	if _, ok := d.LastFrameCorrupted(); ok {
		t.Fatalf("LastFrameCorrupted after Close ok = true, want false")
	}
	if _, _, ok := d.LastQuantizer(); ok {
		t.Fatalf("LastQuantizer after Close ok = true, want false")
	}
	if _, ok := d.LastReferenceUpdates(); ok {
		t.Fatalf("LastReferenceUpdates after Close ok = true, want false")
	}
	if _, ok := d.LastReferencesUsed(); ok {
		t.Fatalf("LastReferencesUsed after Close ok = true, want false")
	}
}
