package encoder

import (
	"errors"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// VP9 frame-level pack driver. Ported from libvpx v1.16.0
// vp9/encoder/vp9_bitstream.c — vp9_pack_bitstream. Composes the
// uncompressed header, the compressed header, and the tile data
// into a single frame payload, threading the
// first_partition_size literal at the tail of the uncompressed
// header with the compressed-header byte count.
//
// libvpx writes the uncompressed header with a placeholder for
// first_partition_size, then patches it after the compressed
// header lands (saved_wb captures the placeholder position). Our
// model is simpler because the existing uncompressed-header
// writers emit FirstPartitionSize from the header struct: we
// stage the compressed header in a scratch buffer first, set
// FirstPartitionSize, then emit the uncompressed header and copy
// the compressed bytes into place. The byte layout is identical.

// ErrCompressedHeaderTooLarge is returned when the compressed
// header exceeds 16 bits — first_partition_size on the wire is a
// 16-bit literal, mirroring libvpx's "compressed_hdr_size > 16
// bits" error.
var ErrCompressedHeaderTooLarge = errors.New(
	"encoder: compressed_hdr_size exceeds 16 bits")

// ErrPackBufferFull is returned when the output buffer doesn't
// have room for the assembled frame.
var ErrPackBufferFull = errors.New(
	"encoder: pack output buffer full")

// PackBitstreamArgs bundles the inputs PackBitstream consults.
type PackBitstreamArgs struct {
	// Dest is the per-frame output buffer. The caller sizes it to
	// hold the uncompressed header + compressed header + tile data.
	Dest []byte

	// Scratch stages the compressed header. Must be at least large
	// enough to hold one compressed-header payload (libvpx caps this
	// at 16 bits = 65535 bytes; typical values are well under 1 KB).
	// Reused across frames by the caller for zero-alloc operation.
	Scratch []byte

	// Header is the uncompressed-header struct to emit. PackBitstream
	// sets Header.FirstPartitionSize to the computed compressed-header
	// size before invoking the matching uncompressed-header writer.
	Header *vp9dec.UncompressedHeader

	// Comp threads the per-frame inputs to the no-update
	// compressed-header writer (TxMode, lossless, interp filter,
	// reference mode, etc.). Consulted when CountsArgs is nil.
	Comp CompressedHeaderInputs

	// CountsArgs, when non-nil, routes the compressed-header pack
	// through the counts-driven driver (mirroring libvpx's
	// write_compressed_header). Comp is ignored on this path because
	// CountsArgs carries the same per-frame gating plus the
	// FrameContext + FrameCounts payloads. This is the byte-parity
	// path; callers without per-frame counters fall back to the
	// no-update fields on Comp.
	CountsArgs *WriteCompressedHeaderFromCountsArgs

	// TileRows / TileCols set the tile grid (derived from the header's
	// Tile.Log2TileRows / Log2TileCols by the caller).
	TileRows  int
	TileCols  int
	WriteTile WriteTileFn

	// RefDims is consulted only for inter (non-intra-only, non-key)
	// frames. May be nil for keyframe / intra-only paths.
	RefDims func(slot uint8) (uint32, uint32)
}

// PackBitstream mirrors libvpx's vp9_pack_bitstream. Walks the
// per-frame pack sequence:
//
//  1. Stage the compressed header into args.Scratch.
//  2. Set header.FirstPartitionSize = compressed-header byte count.
//  3. Emit the uncompressed header at args.Dest[0:].
//  4. Copy the compressed header to follow the uncompressed header.
//  5. Append tile data via WriteTiles.
//
// Returns the total byte count written into args.Dest.
//
// Zero-alloc when the caller reuses Scratch across frames; the
// uncompressed-header writer + WriteTiles both reach for the
// caller-owned buffers without intermediate heap allocation.
func PackBitstream(a PackBitstreamArgs) (int, error) {
	var compSize int
	var err error
	if a.CountsArgs != nil {
		compSize, err = WriteCompressedHeaderFromCounts(a.Scratch, *a.CountsArgs)
	} else {
		compSize, err = WriteCompressedHeaderNoUpdate(a.Scratch, a.Comp)
	}
	if err != nil {
		return 0, err
	}
	if compSize > 0xFFFF {
		return 0, ErrCompressedHeaderTooLarge
	}
	a.Header.FirstPartitionSize = uint16(compSize)

	w := NewBitWriter(a.Dest)
	var uncSize int
	switch {
	case a.Header.FrameType == common.KeyFrame:
		uncSize = WriteKeyframeUncompressedHeader(w, a.Header)
	case a.Header.IntraOnly:
		uncSize = WriteIntraOnlyUncompressedHeader(w, a.Header)
	default:
		uncSize = WriteInterUncompressedHeader(w, a.Header, a.RefDims)
	}

	if uncSize+compSize > len(a.Dest) {
		return uncSize, ErrPackBufferFull
	}
	copy(a.Dest[uncSize:uncSize+compSize], a.Scratch[:compSize])

	tileTotal, err := WriteTiles(WriteTilesArgs{
		TileRows:  a.TileRows,
		TileCols:  a.TileCols,
		Output:    a.Dest[uncSize+compSize:],
		WriteTile: a.WriteTile,
	})
	if err != nil {
		return uncSize + compSize, err
	}
	return uncSize + compSize + tileTotal, nil
}
