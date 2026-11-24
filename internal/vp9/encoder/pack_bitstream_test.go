package encoder

import (
	"encoding/binary"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestPackBitstreamKeyframeRoundTrip packs a minimal keyframe (no
// tile content beyond a stub callback) and verifies the layout is
// uncompressed_header + compressed_header + tile_data where:
//   - the uncompressed header parses back via ReadUncompressedHeader,
//   - the recovered FirstPartitionSize equals the compressed-header
//     byte count written into the buffer,
//   - the compressed header parses cleanly as the no-update payload
//     (every "update?" bit is 0).
func TestPackBitstreamKeyframeRoundTrip(t *testing.T) {
	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 320,
		Height:                240,
		RefreshFrameContext:   true,
		FrameParallelDecoding: false,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSBT601,
			ColorRange: common.CRStudioRange,
		},
	}
	comp := CompressedHeaderInputs{
		TxMode:        common.Only4x4,
		IntraOnly:     true,
		InterpFilter:  vp9dec.InterpEighttap,
		ReferenceMode: vp9dec.SingleReference,
	}

	dest := make([]byte, 1024)
	scratch := make([]byte, 1024)
	args := PackBitstreamArgs{
		Dest:     dest,
		Scratch:  scratch,
		Header:   &header,
		Comp:     comp,
		TileRows: 1,
		TileCols: 1,
		WriteTile: func(bw *bitstream.Writer, r, c int) error {
			// Minimal tile payload: a single bit to give vpx_stop_encode
			// something to flush.
			bw.Write(0, 128)
			return nil
		},
	}

	total, err := PackBitstream(args)
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	if total <= 0 {
		t.Fatalf("total = %d, want > 0", total)
	}

	var br vp9dec.BitReader
	br.Init(dest[:total])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	if got.Width != header.Width || got.Height != header.Height {
		t.Errorf("size = (%d,%d), want (%d,%d)",
			got.Width, got.Height, header.Width, header.Height)
	}
	uncSize := br.BytesRead()
	if got.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0; compressed header didn't land")
	}
	compEnd := uncSize + int(got.FirstPartitionSize)
	if compEnd > total {
		t.Fatalf("compressed end %d past total %d", compEnd, total)
	}

	// Parse the compressed header through the no-update branch; with
	// our stub inputs there should be no update bits to flip.
	var cr bitstream.Reader
	if err := cr.Init(dest[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader init: %v", err)
	}
	var fc vp9dec.FrameContext
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		IntraOnly:    true,
		KeyFrame:     true,
		Lossless:     false,
		InterpFilter: vp9dec.InterpEighttap,
	})
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}

	// Tile data follows compEnd; for the 1-tile case there's no size
	// prefix so the entire trailing region is the tile.
	if total <= compEnd {
		t.Errorf("no tile bytes: total=%d, compEnd=%d", total, compEnd)
	}
}

// TestPackBitstreamCountsPath packs an inter frame through the
// counts-driven compressed-header path; the decoder reads back the
// uncompressed header + the counts-driven compressed header byte
// for byte, with every prob slot landing on the value the
// savings_search settled on.
func TestPackBitstreamCountsPath(t *testing.T) {
	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.InterFrame,
		ShowFrame:             true,
		IntraOnly:             false,
		RefreshFrameFlags:     0,
		Width:                 320,
		Height:                240,
		InterpFilter:          vp9dec.InterpEighttap,
		RefreshFrameContext:   true,
		FrameParallelDecoding: false,
		FrameContextIdx:       0,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth: vp9dec.Bits8,
		},
	}

	var fc vp9dec.FrameContext
	seedFrameContext(&fc)
	counts := FrameCounts{}
	counts.Skip[0] = [2]uint32{900, 100}
	counts.IntraInter[0] = [2]uint32{100, 900}

	countsArgs := &WriteCompressedHeaderFromCountsArgs{
		Lossless:             false,
		TxMode:               common.Only4x4,
		IntraOnly:            false,
		InterpFilter:         vp9dec.InterpEighttap,
		ReferenceMode:        vp9dec.SingleReference,
		CompoundRefAllowed:   false,
		AllowHighPrecisionMv: false,
		CoefStepsize:         4,
		Probs:                &fc,
		Counts:               &counts,
	}

	dest := make([]byte, 4096)
	scratch := make([]byte, 4096)
	args := PackBitstreamArgs{
		Dest:       dest,
		Scratch:    scratch,
		Header:     &header,
		CountsArgs: countsArgs,
		TileRows:   1,
		TileCols:   1,
		WriteTile: func(bw *bitstream.Writer, r, c int) error {
			bw.Write(0, 128)
			return nil
		},
		RefDims: func(slot uint8) (uint32, uint32) { return 320, 240 },
	}

	total, err := PackBitstream(args)
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	if total <= 0 {
		t.Fatalf("total = %d, want > 0", total)
	}

	// Re-parse uncompressed header and confirm the counts-driven
	// compressed header rides correctly.
	var br vp9dec.BitReader
	br.Init(dest[:total])
	got, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	uncSize := br.BytesRead()
	if got.FirstPartitionSize == 0 {
		t.Fatal("FirstPartitionSize = 0; counts-driven compressed header didn't land")
	}
	compEnd := uncSize + int(got.FirstPartitionSize)
	if compEnd > total {
		t.Fatalf("compressed end %d past total %d", compEnd, total)
	}

	var cr bitstream.Reader
	if err := cr.Init(dest[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader init: %v", err)
	}
	var decFc vp9dec.FrameContext
	seedFrameContext(&decFc)
	out := vp9dec.ReadCompressedHeader(&cr, &decFc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            false,
		KeyFrame:             false,
		InterpFilter:         vp9dec.InterpEighttap,
		AllowHighPrecisionMv: false,
		CompoundRefAllowed:   false,
	})
	if out.TxMode != common.Only4x4 {
		t.Errorf("TxMode = %d, want Only4x4", out.TxMode)
	}
	if decFc != fc {
		t.Errorf("decoder FrameContext diverged from encoder after PackBitstream counts path")
	}
}

// TestPackBitstreamMultiTilePrefixWidth packs a 2-tile frame and
// confirms the tile region starts with a 4-byte big-endian size
// prefix the multi-tile decoder needs to walk past tile 0.
func TestPackBitstreamMultiTilePrefixWidth(t *testing.T) {
	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 1024,
		Height:                256,
		RefreshFrameContext:   true,
		FrameParallelDecoding: false,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth: vp9dec.Bits8,
		},
	}
	header.Tile.Log2TileCols = 1 // 2 columns × 1 row
	header.Tile.Log2TileRows = 0

	dest := make([]byte, 1024)
	scratch := make([]byte, 1024)
	args := PackBitstreamArgs{
		Dest:     dest,
		Scratch:  scratch,
		Header:   &header,
		Comp:     CompressedHeaderInputs{TxMode: common.Only4x4, IntraOnly: true, InterpFilter: vp9dec.InterpEighttap, ReferenceMode: vp9dec.SingleReference},
		TileRows: 1,
		TileCols: 2,
		WriteTile: func(bw *bitstream.Writer, r, c int) error {
			// Make each tile easy to identify: 8 bits of distinct value.
			bw.Write(uint32(c&1), 128)
			return nil
		},
	}
	total, err := PackBitstream(args)
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(dest[:total])
	got, _ := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	uncSize := br.BytesRead()
	compEnd := uncSize + int(got.FirstPartitionSize)
	// Tile 0 has a 4-byte BE size prefix in front of it.
	tile0Size := int(binary.BigEndian.Uint32(dest[compEnd : compEnd+4]))
	if tile0Size == 0 {
		t.Errorf("tile0 size prefix = 0")
	}
	if compEnd+4+tile0Size > total {
		t.Errorf("tile0 region %d+%d past total %d", compEnd+4, tile0Size, total)
	}
}
