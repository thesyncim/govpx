package govpx

import (
	"encoding/binary"
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9DecoderRejectsNonProfile0AsNotImplemented(t *testing.T) {
	var pk vp9test.BitPacker
	pk.WriteLiteral(2, 2)    // frame_marker = 0b10
	pk.WriteLiteral(1, 2)    // profile = 1
	pk.WriteBit(0)           // show_existing_frame
	pk.WriteBit(0)           // frame_type = KEY
	pk.WriteBit(1)           // show_frame
	pk.WriteBit(0)           // error_resilient
	pk.WriteLiteral(0x49, 8) // sync code 0
	pk.WriteLiteral(0x83, 8) // sync code 1
	pk.WriteLiteral(0x42, 8) // sync code 2
	pk.WriteLiteral(2, 3)    // color_space = CSBT601
	pk.WriteBit(0)           // color_range = StudioRange
	pk.WriteBit(0)           // subsampling_x = 0
	pk.WriteBit(0)           // subsampling_y = 0
	pk.WriteBit(0)           // reserved bit
	pk.WriteLiteral(15, 16)  // width - 1
	pk.WriteLiteral(15, 16)  // height - 1
	pk.WriteBit(0)           // render_flag
	pk.WriteBit(1)           // refresh_frame_context
	pk.WriteBit(0)           // frame_parallel_decoding
	pk.WriteLiteral(0, 2)    // frame_context_idx
	pk.WriteLiteral(0, 6)    // loopfilter filter_level
	pk.WriteLiteral(0, 3)    // loopfilter sharpness
	pk.WriteBit(0)           // mode_ref_delta_enabled
	pk.WriteLiteral(1, 8)    // base_qindex
	pk.WriteBit(0)           // y_dc_delta_q
	pk.WriteBit(0)           // uv_dc_delta_q
	pk.WriteBit(0)           // uv_ac_delta_q
	pk.WriteBit(0)           // seg.enabled
	pk.WriteBit(0)           // log2_tile_rows
	pk.WriteLiteral(0, 16)   // first_partition_size
	pk.FlushByte()

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(pk.Bytes()); !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode profile 1 err = %v, want ErrVP9NotImplemented", err)
	}
}

// TestVP9DecoderRejectsTruncatedCompressedHeader: a well-formed
// profile-0 keyframe header whose first_partition_size points past
// the packet end is rejected before the reconstruct boundary.

func TestVP9DecoderRejectsTruncatedCompressedHeader(t *testing.T) {
	var pk vp9test.BitPacker
	pk.WriteLiteral(2, 2)    // frame_marker = 0b10
	pk.WriteLiteral(0, 2)    // profile = 0
	pk.WriteBit(0)           // show_existing_frame
	pk.WriteBit(0)           // frame_type = KEY
	pk.WriteBit(1)           // show_frame
	pk.WriteBit(0)           // error_resilient
	pk.WriteLiteral(0x49, 8) // sync code 0
	pk.WriteLiteral(0x83, 8) // sync code 1
	pk.WriteLiteral(0x42, 8) // sync code 2
	pk.WriteLiteral(2, 3)    // color_space = CSBT601 (0b010)
	pk.WriteBit(0)           // color_range = StudioRange
	pk.WriteLiteral(319, 16) // width - 1
	pk.WriteLiteral(239, 16) // height - 1
	pk.WriteBit(0)           // render_flag
	pk.WriteBit(1)           // refresh_frame_context
	pk.WriteBit(0)           // frame_parallel_decoding
	pk.WriteLiteral(1, 2)    // frame_context_idx
	pk.WriteLiteral(8, 6)    // loopfilter filter_level
	pk.WriteLiteral(2, 3)    // loopfilter sharpness
	pk.WriteBit(0)           // mode_ref_delta_enabled
	pk.WriteLiteral(64, 8)   // base_qindex
	pk.WriteBit(0)           // y_dc_delta_q
	pk.WriteBit(0)           // uv_dc_delta_q
	pk.WriteBit(0)           // uv_ac_delta_q
	pk.WriteBit(0)           // seg.enabled
	pk.WriteBit(0)           // log2_tile_rows
	pk.WriteLiteral(42, 16)  // first_partition_size
	// Tail bytes: the compressed header. We need at least 42 bytes
	// of payload after the uncompressed header for libvpx to accept,
	// but our parser returns once first_partition_size is read.
	pk.FlushByte()
	packet := pk.Bytes()

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
