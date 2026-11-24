package encoder

import (
	"encoding/binary"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// TestWriteTilesSingleTile: 1 tile case — no size prefix is
// emitted; the output is just the tile bytes.
func TestWriteTilesSingleTile(t *testing.T) {
	out := make([]byte, 64)
	calls := 0
	args := WriteTilesArgs{
		TileRows: 1,
		TileCols: 1,
		Output:   out,
		WriteTile: func(bw *bitstream.Writer, r, c int) error {
			calls++
			if r != 0 || c != 0 {
				t.Errorf("tile coord = (%d,%d), want (0,0)", r, c)
			}
			bw.Write(0, 128)
			bw.Write(1, 128)
			return nil
		},
	}
	total, err := WriteTiles(args)
	if err != nil {
		t.Fatalf("WriteTiles: %v", err)
	}
	if calls != 1 {
		t.Errorf("WriteTile called %d times, want 1", calls)
	}
	if total < 2 {
		t.Errorf("total size = %d, want >= 2", total)
	}
}

// TestWriteTilesTwoTilesFrame: 2-tile case — the first tile gets a
// 4-byte big-endian size prefix; the second tile follows
// immediately with no prefix. Decode parses the prefix and walks the
// matching tile bytes through a fresh bitstream.Reader.
func TestWriteTilesTwoTilesFrame(t *testing.T) {
	out := make([]byte, 128)
	want := [2][]byte{
		{0x80, 0x40},
		{0x20, 0x10, 0x08},
	}
	args := WriteTilesArgs{
		TileRows: 1,
		TileCols: 2,
		Output:   out,
		WriteTile: func(bw *bitstream.Writer, r, c int) error {
			// Encode 4 bits per tile, distinct values per call.
			payload := want[c]
			for _, b := range payload {
				for i := 7; i >= 0; i-- {
					bit := uint32((b >> uint(i)) & 1)
					bw.Write(bit, 128)
				}
			}
			return nil
		},
	}
	total, err := WriteTiles(args)
	if err != nil {
		t.Fatalf("WriteTiles: %v", err)
	}

	if total < 4 {
		t.Fatalf("total = %d, want >= 4", total)
	}
	tile0Size := int(binary.BigEndian.Uint32(out[0:4]))
	if tile0Size == 0 {
		t.Fatal("tile0 size prefix is zero")
	}
	// Tile 0 contents follow the 4-byte prefix.
	t0 := out[4 : 4+tile0Size]
	t1 := out[4+tile0Size : total]
	if len(t1) == 0 {
		t.Fatal("tile1 has no bytes")
	}

	// Decode tile 0 against its size; should recover the 16 bits we
	// wrote.
	var r0 bitstream.Reader
	if err := r0.Init(t0); err != nil {
		t.Fatalf("Init tile0: %v", err)
	}
	for _, b := range want[0] {
		for i := 7; i >= 0; i-- {
			got := r0.Read(128)
			expected := uint32((b >> uint(i)) & 1)
			if got != expected {
				t.Errorf("tile0 bit = %d, want %d", got, expected)
			}
		}
	}
	// Decode tile 1.
	var r1 bitstream.Reader
	if err := r1.Init(t1); err != nil {
		t.Fatalf("Init tile1: %v", err)
	}
	for _, b := range want[1] {
		for i := 7; i >= 0; i-- {
			got := r1.Read(128)
			expected := uint32((b >> uint(i)) & 1)
			if got != expected {
				t.Errorf("tile1 bit = %d, want %d", got, expected)
			}
		}
	}
}

// TestWriteTilesBufferFull: an output buffer too small to fit even
// the size prefix returns ErrTileBufferFull.
func TestWriteTilesBufferFull(t *testing.T) {
	out := make([]byte, 3)
	args := WriteTilesArgs{
		TileRows: 1,
		TileCols: 2,
		Output:   out,
		WriteTile: func(bw *bitstream.Writer, r, c int) error {
			bw.Write(0, 128)
			return nil
		},
	}
	if _, err := WriteTiles(args); err != ErrTileBufferFull {
		t.Errorf("err = %v, want ErrTileBufferFull", err)
	}
}
