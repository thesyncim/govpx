package govpx_test

import (
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func FuzzVP8DecoderMalformedPackets(f *testing.F) {
	// Handcrafted seeds cover protocol edges: partition0 size greater
	// than the remaining payload, oversized first_part_size, key frames
	// with version > 3, width/height boundaries, and targeted bit flips
	// on the frame tag and sync code.
	seeds := [][]byte{
		{},
		{0},
		{0, 0, 0},
		{0xff, 0xff, 0xff},
		vp8test.InterFramePacket(0, 0, true),
		vp8test.InterFramePacket(0, 0, false),
		vp8test.InterFramePacket(1, 0, true),
		vp8test.InterFramePacket(0xfffff, 0, true), // first_part overflow

		vp8test.KeyFramePacket(0, 16, 0, 0, true),
		vp8test.KeyFramePacket(16, 0, 0, 0, true),
		vp8test.KeyFramePacket(0, 0, 0, 0, true),
		vp8test.KeyFramePacket(16, 16, 0, 0, false), // hidden frame
		vp8test.KeyFramePacket(16, 16, 200, 0, true),
		vp8test.KeyFramePacket(16, 16, 200, 4, true),     // profile=4 (invalid)
		vp8test.KeyFramePacket(16, 16, 200, 7, true),     // profile=7 (invalid)
		vp8test.KeyFramePacket(65535, 65535, 1, 0, true), // dimensions at uint16 max
		vp8test.KeyFramePacket(8192, 4320, 1, 0, true),   // 8K width
		vp8test.KeyFramePacket(16, 16, 0xffff, 0, true),  // huge first_part_size

		vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(16, 16, 200, 4, true),
		vp8test.KeyFramePacketWithPayload(16, 16, 1, 0, true),
		vp8test.KeyFramePacketWithPayload(16, 16, 0, 0, true),
		vp8test.KeyFramePacketWithPayload(32, 16, 50, 0, true),
		vp8test.KeyFramePacketWithPayload(31, 17, 50, 0, true), // odd dims
		vp8test.KeyFramePacketWithPayload(65, 33, 50, 0, true), // larger odd dims

		// Truncated forms: drop trailing bytes so the parser hits EOF
		// at different points (frame header, partition0, tokens).
		truncatedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 1),
		truncatedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 9),
		truncatedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 209),
		truncatedFuzzPacket(vp8test.KeyFramePacket(16, 16, 200, 0, true), 6),

		// Frame-tag bit flips.
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 0, 0x01),
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 0, 0x80),
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 1, 0xff),
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 2, 0xff),

		// Sync-code (start_code_prefix) corruption on a keyframe.
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 3, 0xff),
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 4, 0x00),
		taintedFuzzPacket(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), 5, 0x00),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{MaxWidth: 64, MaxHeight: 64})
		if err != nil {
			t.Fatalf("NewVP8Decoder returned error: %v", err)
		}

		_, _ = govpx.PeekVP8StreamInfo(packet)
		_ = d.Decode(packet)

		dst := fuzzDecodeTarget(packet)
		_, _ = d.DecodeInto(packet, &dst)
		d.Reset()
		_, _ = d.NextFrame()
	})
}

// truncatedFuzzPacket returns p[:len(p)-n] when feasible. Used to
// seed parser-EOF cases at specific offsets.
func truncatedFuzzPacket(p []byte, n int) []byte {
	if n >= len(p) {
		return nil
	}
	return append([]byte(nil), p[:len(p)-n]...)
}

// taintedFuzzPacket returns p with p[off] XOR'd by mask. Used to
// seed targeted bit-flip cases on the frame tag and sync code.
func taintedFuzzPacket(p []byte, off int, mask byte) []byte {
	if off >= len(p) {
		return append([]byte(nil), p...)
	}
	out := append([]byte(nil), p...)
	out[off] ^= mask
	return out
}

func fuzzDecodeTarget(packet []byte) govpx.Image {
	info, err := govpx.PeekVP8StreamInfo(packet)
	if err == nil && info.KeyFrame && info.Width > 0 && info.Width <= 64 && info.Height > 0 && info.Height <= 64 {
		return newVP8FacadeImage(info.Width, info.Height)
	}
	return newVP8FacadeImage(64, 64)
}

func TestDecoderResetKeepsDecodeHotPathAllocationFree(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8test.KeyFramePacketWithPayload(64, 64, 200, 0, true)
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}
	d.Reset()

	allocs := testing.AllocsPerRun(1000, func() {
		d.Reset()
		_ = d.Decode(packet)
	})
	if allocs != 0 {
		t.Fatalf("Decode after reset allocs = %v, want 0", allocs)
	}
}
