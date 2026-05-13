package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// Loopfilter / quantization / tile-info parsers from the VP9
// uncompressed header. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c — setup_loopfilter, setup_quantization,
// setup_tile_info.

// MaxRefLfDeltas / MaxModeLfDeltas mirror libvpx's MAX_REF_LF_DELTAS
// and MAX_MODE_LF_DELTAS — four ref deltas and two mode deltas per
// frame.
const (
	MaxRefLfDeltas  = 4
	MaxModeLfDeltas = 2
)

// LoopfilterParams holds the loop filter knobs the uncompressed header
// emits. Mirrors libvpx's struct loopfilter (just the parser-visible
// fields).
type LoopfilterParams struct {
	FilterLevel         uint8
	SharpnessLevel      uint8
	ModeRefDeltaEnabled bool
	ModeRefDeltaUpdate  bool
	RefDeltas           [MaxRefLfDeltas]int8
	ModeDeltas          [MaxModeLfDeltas]int8
}

// ReadLoopfilter parses the 6-bit filter level + 3-bit sharpness +
// optional mode/ref delta block. Mirrors setup_loopfilter exactly:
// when the delta-update bit is 0 the existing deltas are preserved, so
// the caller passes the previous frame's state in `lf` and this
// function only overwrites entries with their per-slot enable bit set.
func ReadLoopfilter(r *BitReader, lf *LoopfilterParams) {
	lf.FilterLevel = uint8(r.ReadLiteral(6))
	lf.SharpnessLevel = uint8(r.ReadLiteral(3))
	lf.ModeRefDeltaUpdate = false
	lf.ModeRefDeltaEnabled = r.ReadBit() != 0
	if !lf.ModeRefDeltaEnabled {
		return
	}
	lf.ModeRefDeltaUpdate = r.ReadBit() != 0
	if !lf.ModeRefDeltaUpdate {
		return
	}
	for i := range MaxRefLfDeltas {
		if r.ReadBit() != 0 {
			lf.RefDeltas[i] = int8(r.ReadSignedLiteral(6))
		}
	}
	for i := range MaxModeLfDeltas {
		if r.ReadBit() != 0 {
			lf.ModeDeltas[i] = int8(r.ReadSignedLiteral(6))
		}
	}
}

// QindexBits is the wire-format width of the base qindex.
const QindexBits = 8

// QuantizationParams holds the frame-level quant knobs.
type QuantizationParams struct {
	BaseQindex int16
	YDcDeltaQ  int8
	UvDcDeltaQ int8
	UvAcDeltaQ int8
	Lossless   bool
}

func readDeltaQ(r *BitReader) int8 {
	if r.ReadBit() != 0 {
		return int8(r.ReadSignedLiteral(4))
	}
	return 0
}

// ReadQuantization mirrors setup_quantization — reads the 8-bit
// base_qindex plus the three optional 4-bit signed deltas and derives
// the lossless flag.
func ReadQuantization(r *BitReader, q *QuantizationParams) {
	q.BaseQindex = int16(r.ReadLiteral(QindexBits))
	q.YDcDeltaQ = readDeltaQ(r)
	q.UvDcDeltaQ = readDeltaQ(r)
	q.UvAcDeltaQ = readDeltaQ(r)
	q.Lossless = q.BaseQindex == 0 && q.YDcDeltaQ == 0 && q.UvDcDeltaQ == 0 && q.UvAcDeltaQ == 0
}

// TileInfo holds the tile-grid sizing derived from the frame's mi
// dimensions and the trailing tile_cols / tile_rows bits.
type TileInfo struct {
	Log2TileCols int
	Log2TileRows int
}

// ReadTileInfo mirrors setup_tile_info. It needs the frame's mi_cols
// to pick the valid (min, max) tile column range. Returns ErrInvalid
// Header if tile_cols exceeds 6 (the VP9 spec limit).
func ReadTileInfo(r *BitReader, miCols int, tile *TileInfo) error {
	minLog2, maxLog2 := TileNBits(miCols)

	tile.Log2TileCols = minLog2
	for n := maxLog2 - minLog2; n > 0; n-- {
		if r.ReadBit() == 0 {
			break
		}
		tile.Log2TileCols++
	}
	if tile.Log2TileCols > 6 {
		return ErrInvalidHeader
	}

	tile.Log2TileRows = int(r.ReadBit())
	if tile.Log2TileRows != 0 {
		tile.Log2TileRows += int(r.ReadBit())
	}
	return nil
}

// TileNBits mirrors vp9_get_tile_n_bits. Returns (min, max) for the
// log2_tile_cols range based on the frame's mode-info column count.
func TileNBits(miCols int) (minLog2, maxLog2 int) {
	const (
		miBlockSizeLog2 = common.MiBlockSizeLog2
		minTileWidthB64 = 4
		maxTileWidthB64 = 64
	)
	sb64Cols := alignPowerOfTwo(miCols, miBlockSizeLog2) >> miBlockSizeLog2

	for (maxTileWidthB64 << minLog2) < sb64Cols {
		minLog2++
	}
	maxLog2 = 1
	for (sb64Cols >> maxLog2) >= minTileWidthB64 {
		maxLog2++
	}
	maxLog2--
	return
}

func alignPowerOfTwo(value, n int) int {
	return (value + (1 << uint(n)) - 1) &^ ((1 << uint(n)) - 1)
}
