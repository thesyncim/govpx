package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

// TestReadUncompressedHeaderKeyframe synthesizes a minimal profile-0
// keyframe header end-to-end and round-trips it through the driver.
// This validates the full state-machine ordering: frame marker →
// profile → show_existing_frame=0 → frame_type=KEY → show_frame=1 →
// error_resilient=0 → sync code → bitdepth/colorspace/sampling →
// frame size → render size → refresh_frame_context + frame_parallel →
// frame_context_idx → loopfilter → quant → segmentation → tile info →
// first_partition_size.
func TestReadUncompressedHeaderKeyframe(t *testing.T) {
	var pk bitPacker

	// Frame marker: 0b10
	pk.writeLiteral(common.VP9FrameMarker, 2)
	// Profile 0: bits 00
	pk.writeBit(0)
	pk.writeBit(0)
	// show_existing_frame = 0
	pk.writeBit(0)
	// frame_type = KEY (0)
	pk.writeBit(0)
	// show_frame = 1
	pk.writeBit(1)
	// error_resilient_mode = 0
	pk.writeBit(0)
	// sync code bytes — three 8-bit literals.
	pk.writeLiteral(uint32(common.VP9SyncCode0), 8)
	pk.writeLiteral(uint32(common.VP9SyncCode1), 8)
	pk.writeLiteral(uint32(common.VP9SyncCode2), 8)
	// bitdepth/colorspace/sampling — profile 0 just emits the
	// color_space and color_range bit.
	pk.writeLiteral(uint32(common.CSBT601), 3)
	pk.writeBit(uint32(common.CRStudioRange))
	// frame size — 320x240 → (319, 239).
	pk.writeLiteral(319, 16)
	pk.writeLiteral(239, 16)
	// render flag = 0 (inherit)
	pk.writeBit(0)
	// !error_resilient: refresh_frame_context + frame_parallel
	pk.writeBit(1) // refresh_frame_context
	pk.writeBit(0) // frame_parallel_decoding
	// frame_context_idx (2 bits)
	pk.writeLiteral(1, 2)
	// loopfilter: filter_level=8, sharpness=2, delta_enabled=0
	pk.writeLiteral(8, 6)
	pk.writeLiteral(2, 3)
	pk.writeBit(0)
	// quant: base_qindex=64, no deltas
	pk.writeLiteral(64, 8)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(0)
	// segmentation: enabled=0
	pk.writeBit(0)
	// tile info: 320px → 40 mi_cols, sb64=ceil(40/8)=5. tileNBits:
	// min_log2 = 0 (since 64<<0 = 64 >= 5), max_log2 = 1 (5>>1=2>=4? no
	// → max stays 0). So tile_cols stays at 0 (no expand bit). Then
	// log2_tile_rows=0 (1 bit).
	pk.writeBit(0) // log2_tile_rows = 0
	// first_partition_size = 42
	pk.writeLiteral(42, 16)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	h, err := ReadUncompressedHeader(&r, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if h.Profile != common.Profile0 {
		t.Errorf("Profile = %d, want 0", h.Profile)
	}
	if h.FrameType != common.KeyFrame || !h.ShowFrame {
		t.Errorf("frame flags wrong: %+v", h)
	}
	if h.Width != 320 || h.Height != 240 {
		t.Errorf("size = (%d, %d), want (320, 240)", h.Width, h.Height)
	}
	if h.BitDepthColor.BitDepth != Bits8 {
		t.Errorf("BitDepth = %d, want 8", h.BitDepthColor.BitDepth)
	}
	if h.RefreshFrameFlags != 0xff {
		t.Errorf("RefreshFrameFlags = %x, want 0xff", h.RefreshFrameFlags)
	}
	if !h.RefreshFrameContext || h.FrameParallelDecoding {
		t.Errorf("frame-context flags: %+v", h)
	}
	if h.FrameContextIdx != 1 {
		t.Errorf("FrameContextIdx = %d, want 1", h.FrameContextIdx)
	}
	if h.Loopfilter.FilterLevel != 8 || h.Loopfilter.SharpnessLevel != 2 {
		t.Errorf("loopfilter: %+v", h.Loopfilter)
	}
	if h.Quant.BaseQindex != 64 {
		t.Errorf("BaseQindex = %d, want 64", h.Quant.BaseQindex)
	}
	if h.Seg.Enabled {
		t.Error("Seg.Enabled should be false")
	}
	if h.FirstPartitionSize != 42 {
		t.Errorf("FirstPartitionSize = %d, want 42", h.FirstPartitionSize)
	}
}

func TestReadUncompressedHeaderShowExisting(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(common.VP9FrameMarker, 2)
	pk.writeBit(0)        // profile bit 0
	pk.writeBit(0)        // profile bit 1
	pk.writeBit(1)        // show_existing_frame
	pk.writeLiteral(5, 3) // slot 5
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	h, err := ReadUncompressedHeader(&r, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if !h.ShowExistingFrame || h.ExistingFrameSlot != 5 {
		t.Errorf("show-existing flow wrong: %+v", h)
	}
}

func TestReadUncompressedHeaderBadMarker(t *testing.T) {
	var r BitReader
	r.Init([]byte{0x00}) // 0b00 — bad marker
	if _, err := ReadUncompressedHeader(&r, nil, nil); err == nil {
		t.Fatal("expected ErrInvalidHeader, got nil")
	}
}
