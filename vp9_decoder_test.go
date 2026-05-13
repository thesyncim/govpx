package govpx

import (
	"encoding/binary"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

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

// TestVP9DecoderParsesEncoderKeyframeModeTile feeds the current
// encoder stub into the public decoder. Decode parses uncompressed /
// compressed headers plus the keyframe tile mode-info stream, updates
// frame dimensions, then stops at the reconstruct boundary.
func TestVP9DecoderParsesEncoderKeyframeModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := image.NewYCbCr(image.Rect(0, 0, 96, 96), image.YCbCrSubsampleRatio420)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode err = %v, want ErrVP9NotImplemented", err)
	}
	w, h := d.LastFrameSize()
	if w != 96 || h != 96 {
		t.Errorf("LastFrameSize() = (%d, %d), want (96, 96)", w, h)
	}
}

// TestVP9DecoderParsesEncoderIntraOnlyModeTile covers the second-frame
// fallback path. It depends on the first keyframe parse to seed
// preserved header state before the intra-only inter header,
// compressed header, and tile mode-info stream are read.
func TestVP9DecoderParsesEncoderIntraOnlyModeTile(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := image.NewYCbCr(image.Rect(0, 0, 96, 96), image.YCbCrSubsampleRatio420)
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
	if err := d.Decode(key); !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode keyframe err = %v, want ErrVP9NotImplemented", err)
	}
	if err := d.Decode(inter); !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode intra-only err = %v, want ErrVP9NotImplemented", err)
	}
	w, h := d.LastFrameSize()
	if w != 96 || h != 96 {
		t.Errorf("LastFrameSize() = (%d, %d), want (96, 96)", w, h)
	}
}

// TestVP9DecoderParsesEncoderEdgeClippedModeTiles covers the same
// partial-SB shapes as the vpxdec oracle, but through the public
// decoder's tile-mode parser for both keyframe and intra-only frames.
func TestVP9DecoderParsesEncoderEdgeClippedModeTiles(t *testing.T) {
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
			img := image.NewYCbCr(image.Rect(0, 0, tc.w, tc.h), image.YCbCrSubsampleRatio420)
			key, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			inter, err := e.Encode(img)
			if err != nil {
				t.Fatalf("Encode intra-only: %v", err)
			}

			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			if err := d.Decode(key); !errors.Is(err, ErrVP9NotImplemented) {
				t.Fatalf("Decode keyframe err = %v, want ErrVP9NotImplemented", err)
			}
			if err := d.Decode(inter); !errors.Is(err, ErrVP9NotImplemented) {
				t.Fatalf("Decode intra-only err = %v, want ErrVP9NotImplemented", err)
			}
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
	img := image.NewYCbCr(image.Rect(0, 0, 96, 96), image.YCbCrSubsampleRatio420)
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

// TestVP9DecoderParsesMultiTileModeFrame drives the public decoder
// through the 4-byte size-prefixed tile layout. The public encoder
// still emits one tile, so this test packs a two-column keyframe via
// the internal packer and the same stub mode writer.
func TestVP9DecoderParsesMultiTileModeFrame(t *testing.T) {
	packet := vp9MultiTileStubPacketForTest(t, 1024, 64, 1)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode err = %v, want ErrVP9NotImplemented", err)
	}
	w, h := d.LastFrameSize()
	if w != 1024 || h != 64 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (1024, 64)", w, h)
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
// tile-mode parse path allocation-free after construction.
func TestVP9DecoderDecodeSteadyStateAlloc(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 96, Height: 96})
	img := image.NewYCbCr(image.Rect(0, 0, 96, 96), image.YCbCrSubsampleRatio420)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("warm Decode err = %v, want ErrVP9NotImplemented", err)
	}

	allocs := testing.AllocsPerRun(100, func() {
		err = d.Decode(packet)
	})
	if !errors.Is(err, ErrVP9NotImplemented) {
		t.Fatalf("Decode err = %v, want ErrVP9NotImplemented", err)
	}
	if allocs != 0 {
		t.Fatalf("Decode steady state: got %v allocs/op, want 0", allocs)
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
		FrameParallelDecoding: true,
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
		Mode:   common.DcPred,
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

// TestVP9DecoderMaxWidthRejectsLargerKeyframe: a header whose width
// exceeds the configured MaxWidth gets rejected before
// ErrVP9NotImplemented surfaces.
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
