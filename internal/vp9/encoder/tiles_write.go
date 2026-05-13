package encoder

import (
	"encoding/binary"
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// VP9 multi-tile output driver. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — the single-threaded encode_tiles
// loop. Iterates the tile grid in row-major order, allocates a
// fresh boolean coder per tile against the caller-owned output
// buffer, and frames each non-last tile with a 4-byte big-endian
// size prefix matching libvpx's mem_put_be32 layout.
//
// The multi-threaded encode_tiles_mt fork in libvpx is not yet
// ported; the single-threaded path is wire-equivalent and the
// canonical reference for byte parity.

// WriteTileFn is the per-tile body callback. It receives a fresh
// bitstream.Writer already pointed at the next available output
// region; the caller emits the tile contents (typically via
// WriteModesTile) and returns once done. WriteTiles handles the
// per-tile size-prefix framing around the call.
type WriteTileFn func(bw *bitstream.Writer, tileRow, tileCol int) error

// WriteTilesArgs bundles the inputs WriteTiles consults.
type WriteTilesArgs struct {
	// Tile grid dimensions. Derived in libvpx from
	// 1<<log2_tile_cols / log2_tile_rows.
	TileRows int
	TileCols int

	// Output buffer. WriteTiles writes tile data sequentially with a
	// 4-byte big-endian length prefix in front of every tile except
	// the last. Caller sizes the buffer.
	Output []byte

	// WriteTile is invoked per (tile_row, tile_col) to emit one tile's
	// contents into the supplied boolean coder.
	WriteTile WriteTileFn
}

// ErrTileBufferFull is returned when the supplied output buffer
// doesn't have room for the tile being written.
var ErrTileBufferFull = errors.New("encoder: tile output buffer full")

// WriteTiles mirrors libvpx's encode_tiles single-threaded path.
// Loops over tile_rows × tile_cols, allocates a fresh boolean coder
// per tile against the next available output region, invokes the
// caller's WriteTile callback to emit that tile's contents, and
// frames each non-last tile with its 4-byte big-endian size.
//
// Returns the number of bytes written into Output. The caller is
// responsible for resetting above_seg_context before this call;
// per-row left_seg_context resets land inside WriteModesTile.
func WriteTiles(a WriteTilesArgs) (int, error) {
	totalSize := 0
	nTiles := a.TileRows * a.TileCols
	for tileRow := 0; tileRow < a.TileRows; tileRow++ {
		for tileCol := 0; tileCol < a.TileCols; tileCol++ {
			idx := tileRow*a.TileCols + tileCol
			isLast := idx == nTiles-1
			offset := totalSize
			if !isLast {
				offset += 4
			}
			if offset >= len(a.Output) {
				return totalSize, ErrTileBufferFull
			}
			var bw bitstream.Writer
			bw.Start(a.Output[offset:])
			if err := a.WriteTile(&bw, tileRow, tileCol); err != nil {
				return totalSize, err
			}
			size, err := bw.Stop()
			if err != nil {
				return totalSize, err
			}
			if !isLast {
				binary.BigEndian.PutUint32(
					a.Output[totalSize:totalSize+4], uint32(size))
				totalSize += 4
			}
			totalSize += size
		}
	}
	return totalSize, nil
}
