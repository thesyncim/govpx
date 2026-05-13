package encoder

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestWriteTileInfoRoundTrip walks (mi_cols, log2_tile_cols,
// log2_tile_rows) combinations and verifies each (encoder, decoder)
// pair recovers the same TileInfo from the wire fragment. Both
// directions share vp9dec.TileNBits, so this is a tight check on
// the encoder's bit pattern matching libvpx's write_tile_info.
func TestWriteTileInfoRoundTrip(t *testing.T) {
	// Cover a handful of mi_cols values across the (min, max) regime
	// shifts. The 8x8 mi-unit projection: width / 8 rounded up.
	miColSet := []int{8, 16, 32, 64, 128, 256, 512, 1024}
	for _, miCols := range miColSet {
		minLog2, maxLog2 := vp9dec.TileNBits(miCols)
		for log2Cols := minLog2; log2Cols <= maxLog2 && log2Cols <= 6; log2Cols++ {
			for _, log2Rows := range []int{0, 1, 2} {
				tile := &vp9dec.TileInfo{
					Log2TileCols: log2Cols,
					Log2TileRows: log2Rows,
				}

				buf := make([]byte, 8)
				w := NewBitWriter(buf)
				WriteTileInfo(w, tile, miCols)
				size := w.BytesWritten()

				var r vp9dec.BitReader
				r.Init(buf[:size])
				var got vp9dec.TileInfo
				if err := vp9dec.ReadTileInfo(&r, miCols, &got); err != nil {
					t.Fatalf("miCols=%d log2(c,r)=(%d,%d): ReadTileInfo: %v",
						miCols, log2Cols, log2Rows, err)
				}
				if got.Log2TileCols != log2Cols {
					t.Errorf("miCols=%d log2(c,r)=(%d,%d): got Log2TileCols=%d",
						miCols, log2Cols, log2Rows, got.Log2TileCols)
				}
				if got.Log2TileRows != log2Rows {
					t.Errorf("miCols=%d log2(c,r)=(%d,%d): got Log2TileRows=%d",
						miCols, log2Cols, log2Rows, got.Log2TileRows)
				}
			}
		}
	}
}

// TestWriteTileInfoBitCount confirms the bit-count formula: the
// columns side emits (log2_tile_cols - min) ones + (1 if cols <
// max), and the rows side emits 1 bit (if rows == 0) or 2 bits
// (rows > 0). This is the libvpx invariant the parser depends on
// when bounding header reads.
func TestWriteTileInfoBitCount(t *testing.T) {
	miCols := 128 // sb64_cols = 16 → min=0, max=2
	minLog2, maxLog2 := vp9dec.TileNBits(miCols)
	if minLog2 != 0 || maxLog2 != 2 {
		t.Fatalf("miCols=128 unexpected (min,max)=(%d,%d)", minLog2, maxLog2)
	}

	cases := []struct {
		log2Cols, log2Rows int
		// Expected bit count: ones for cols + zero terminator (if cols
		// < max) + 1 or 2 row bits.
		wantBits int
	}{
		{0, 0, 0 + 1 + 1},
		{0, 1, 0 + 1 + 2},
		{1, 0, 1 + 1 + 1},
		{2, 0, 2 + 0 + 1}, // at max, no terminator
		{2, 2, 2 + 0 + 2},
	}
	for _, c := range cases {
		tile := &vp9dec.TileInfo{Log2TileCols: c.log2Cols, Log2TileRows: c.log2Rows}
		buf := make([]byte, 4)
		w := NewBitWriter(buf)
		startBits := w.BitsWritten()
		WriteTileInfo(w, tile, miCols)
		got := w.BitsWritten() - startBits
		if got != c.wantBits {
			t.Errorf("log2(c,r)=(%d,%d): bits=%d, want %d",
				c.log2Cols, c.log2Rows, got, c.wantBits)
		}
	}
}
