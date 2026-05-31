package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
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

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(pk.Bytes()); !errors.Is(err, govpx.ErrVP9NotImplemented) {
		t.Fatalf("Decode profile 1 err = %v, want ErrVP9NotImplemented", err)
	}
}

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
	pk.WriteLiteral(2, 3)    // color_space = CSBT601
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
	pk.FlushByte()

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(pk.Bytes())
	if !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Errorf("LastFrameSize() = (%d, %d), want (0, 0) after rejection", w, h)
	}
}

func TestVP9DecoderRejectsMissingModeTile(t *testing.T) {
	packet := vp9EncodedKeyframeForTest(t, 96, 96, 128)
	_, tileStart := vp9test.ParseHeader(t, packet)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet[:tileStart])
	if !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
}
