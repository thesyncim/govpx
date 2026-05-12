package common

// Ported byte-for-byte from libvpx v1.16.0 vp9/common/vp9_common_data.{c,h}.
//
// These lookup tables map BlockSize to derived geometric quantities
// (log2 of width/height in pixels and in 8x8 / 4x4 units), describe the
// partition tree (partition_lookup, subsize_lookup), constrain the
// transform-size search (max_txsize_lookup, txsize_to_bsize,
// tx_mode_to_biggest_tx_size), and project luma block sizes onto chroma
// under each subsampling configuration (ss_size_lookup, uv_txsize_lookup).
// They are wire-stable and shared between the decoder and encoder.

// BWidthLog2Lookup is log2 of the block width in pixels.
var BWidthLog2Lookup = [BlockSizes]uint8{
	0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4,
}

// BHeightLog2Lookup is log2 of the block height in pixels.
var BHeightLog2Lookup = [BlockSizes]uint8{
	0, 1, 0, 1, 2, 1, 2, 3, 2, 3, 4, 3, 4,
}

// Num4x4BlocksWideLookup is the block width measured in 4x4 sub-blocks.
var Num4x4BlocksWideLookup = [BlockSizes]uint8{
	1, 1, 2, 2, 2, 4, 4, 4, 8, 8, 8, 16, 16,
}

// Num4x4BlocksHighLookup is the block height measured in 4x4 sub-blocks.
var Num4x4BlocksHighLookup = [BlockSizes]uint8{
	1, 2, 1, 2, 4, 2, 4, 8, 4, 8, 16, 8, 16,
}

// MiWidthLog2Lookup is log2 of the block width in 8x8 mode-info units.
var MiWidthLog2Lookup = [BlockSizes]uint8{
	0, 0, 0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3,
}

// Num8x8BlocksWideLookup is the block width measured in 8x8 sub-blocks.
var Num8x8BlocksWideLookup = [BlockSizes]uint8{
	1, 1, 1, 1, 1, 2, 2, 2, 4, 4, 4, 8, 8,
}

// Num8x8BlocksHighLookup is the block height measured in 8x8 sub-blocks.
var Num8x8BlocksHighLookup = [BlockSizes]uint8{
	1, 1, 1, 1, 2, 1, 2, 4, 2, 4, 8, 4, 8,
}

// SizeGroupLookup buckets block sizes into 4 groups used by intra mode
// probability tables — min(3, min(log2_w, log2_h)).
var SizeGroupLookup = [BlockSizes]uint8{
	0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 3,
}

// NumPelsLog2Lookup is log2 of the number of pixels in the block.
var NumPelsLog2Lookup = [BlockSizes]uint8{
	4, 5, 5, 6, 7, 7, 8, 9, 9, 10, 11, 11, 12,
}

// PartitionLookup recovers the PartitionType decision that produced the
// inner BlockSize from an outer 4x4 / 8x8 / 16x16 / 32x32 / 64x64 root.
// Each row is indexed by BlockSizes; PartitionInvalid marks shapes that
// could not have come from the corresponding outer size.
var PartitionLookup = [5][BlockSizes]PartitionType{
	{ // outer 4x4
		PartitionNone, PartitionInvalid, PartitionInvalid, PartitionInvalid,
		PartitionInvalid, PartitionInvalid, PartitionInvalid, PartitionInvalid,
		PartitionInvalid, PartitionInvalid, PartitionInvalid, PartitionInvalid,
		PartitionInvalid,
	},
	{ // outer 8x8
		PartitionSplit, PartitionVert, PartitionHorz, PartitionNone,
		PartitionInvalid, PartitionInvalid, PartitionInvalid, PartitionInvalid,
		PartitionInvalid, PartitionInvalid, PartitionInvalid, PartitionInvalid,
		PartitionInvalid,
	},
	{ // outer 16x16
		PartitionSplit, PartitionSplit, PartitionSplit, PartitionSplit,
		PartitionVert, PartitionHorz, PartitionNone, PartitionInvalid,
		PartitionInvalid, PartitionInvalid, PartitionInvalid, PartitionInvalid,
		PartitionInvalid,
	},
	{ // outer 32x32
		PartitionSplit, PartitionSplit, PartitionSplit, PartitionSplit,
		PartitionSplit, PartitionSplit, PartitionSplit, PartitionVert,
		PartitionHorz, PartitionNone, PartitionInvalid, PartitionInvalid,
		PartitionInvalid,
	},
	{ // outer 64x64
		PartitionSplit, PartitionSplit, PartitionSplit, PartitionSplit,
		PartitionSplit, PartitionSplit, PartitionSplit, PartitionSplit,
		PartitionSplit, PartitionSplit, PartitionVert, PartitionHorz,
		PartitionNone,
	},
}

// SubsizeLookup gives the resulting BlockSize after applying a partition
// to an outer block. Indexed by [PartitionType][outer BlockSize].
var SubsizeLookup = [PartitionTypes][BlockSizes]BlockSize{
	{ // PartitionNone — identity
		Block4x4, Block4x8, Block8x4, Block8x8, Block8x16, Block16x8,
		Block16x16, Block16x32, Block32x16, Block32x32, Block32x64,
		Block64x32, Block64x64,
	},
	{ // PartitionHorz
		BlockInvalid, BlockInvalid, BlockInvalid, Block8x4, BlockInvalid,
		BlockInvalid, Block16x8, BlockInvalid, BlockInvalid, Block32x16,
		BlockInvalid, BlockInvalid, Block64x32,
	},
	{ // PartitionVert
		BlockInvalid, BlockInvalid, BlockInvalid, Block4x8, BlockInvalid,
		BlockInvalid, Block8x16, BlockInvalid, BlockInvalid, Block16x32,
		BlockInvalid, BlockInvalid, Block32x64,
	},
	{ // PartitionSplit
		BlockInvalid, BlockInvalid, BlockInvalid, Block4x4, BlockInvalid,
		BlockInvalid, Block8x8, BlockInvalid, BlockInvalid, Block16x16,
		BlockInvalid, BlockInvalid, Block32x32,
	},
}

// MaxTxsizeLookup is the largest legal transform size for each block shape.
var MaxTxsizeLookup = [BlockSizes]TxSize{
	Tx4x4, Tx4x4, Tx4x4, Tx8x8, Tx8x8, Tx8x8, Tx16x16,
	Tx16x16, Tx16x16, Tx32x32, Tx32x32, Tx32x32, Tx32x32,
}

// TxsizeToBsize maps each transform size to the smallest square BlockSize
// that fits it.
var TxsizeToBsize = [TxSizes]BlockSize{
	Block4x4,   // Tx4x4
	Block8x8,   // Tx8x8
	Block16x16, // Tx16x16
	Block32x32, // Tx32x32
}

// TxModeToBiggestTxSize caps the per-frame transform-size budget by TxMode.
var TxModeToBiggestTxSize = [TxModes]TxSize{
	Tx4x4,   // Only4x4
	Tx8x8,   // Allow8x8
	Tx16x16, // Allow16x16
	Tx32x32, // Allow32x32
	Tx32x32, // TxModeSelect
}

// SsSizeLookup projects a luma BlockSize onto the chroma plane under
// each (ss_x, ss_y) subsampling configuration. Indexed as
// [BlockSize][ss_x][ss_y].
var SsSizeLookup = [BlockSizes][2][2]BlockSize{
	// ss_x=0/ss_y=0   ss_x=0/ss_y=1     ss_x=1/ss_y=0     ss_x=1/ss_y=1
	{{Block4x4, BlockInvalid}, {BlockInvalid, BlockInvalid}},   // 4x4
	{{Block4x8, Block4x4}, {BlockInvalid, BlockInvalid}},       // 4x8
	{{Block8x4, BlockInvalid}, {Block4x4, BlockInvalid}},       // 8x4
	{{Block8x8, Block8x4}, {Block4x8, Block4x4}},               // 8x8
	{{Block8x16, Block8x8}, {BlockInvalid, Block4x8}},          // 8x16
	{{Block16x8, BlockInvalid}, {Block8x8, Block8x4}},          // 16x8
	{{Block16x16, Block16x8}, {Block8x16, Block8x8}},           // 16x16
	{{Block16x32, Block16x16}, {BlockInvalid, Block8x16}},      // 16x32
	{{Block32x16, BlockInvalid}, {Block16x16, Block16x8}},      // 32x16
	{{Block32x32, Block32x16}, {Block16x32, Block16x16}},       // 32x32
	{{Block32x64, Block32x32}, {BlockInvalid, Block16x32}},     // 32x64
	{{Block64x32, BlockInvalid}, {Block32x32, Block32x16}},     // 64x32
	{{Block64x64, Block64x32}, {Block32x64, Block32x32}},       // 64x64
}

// UvTxsizeLookup picks the chroma transform size given the luma block
// shape, luma transform size, and (ss_x, ss_y) chroma subsampling. Index
// order: [BlockSize][TxSize][ss_x][ss_y].
var UvTxsizeLookup = [BlockSizes][TxSizes][2][2]TxSize{
	{ // Block4x4
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
	},
	{ // Block4x8
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
	},
	{ // Block8x4
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
	},
	{ // Block8x8
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx4x4}, {Tx4x4, Tx4x4}},
	},
	{ // Block8x16
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx4x4, Tx4x4}},
	},
	{ // Block16x8
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx4x4}, {Tx8x8, Tx4x4}},
		{{Tx8x8, Tx4x4}, {Tx8x8, Tx8x8}},
		{{Tx8x8, Tx4x4}, {Tx8x8, Tx8x8}},
	},
	{ // Block16x16
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx8x8}, {Tx8x8, Tx8x8}},
	},
	{ // Block16x32
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx16x16}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx16x16}, {Tx8x8, Tx8x8}},
	},
	{ // Block32x16
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx8x8}, {Tx16x16, Tx8x8}},
		{{Tx16x16, Tx8x8}, {Tx16x16, Tx8x8}},
	},
	{ // Block32x32
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx16x16}, {Tx16x16, Tx16x16}},
		{{Tx32x32, Tx16x16}, {Tx16x16, Tx16x16}},
	},
	{ // Block32x64
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx16x16}, {Tx16x16, Tx16x16}},
		{{Tx32x32, Tx32x32}, {Tx16x16, Tx16x16}},
	},
	{ // Block64x32
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx16x16}, {Tx16x16, Tx16x16}},
		{{Tx32x32, Tx16x16}, {Tx32x32, Tx16x16}},
	},
	{ // Block64x64
		{{Tx4x4, Tx4x4}, {Tx4x4, Tx4x4}},
		{{Tx8x8, Tx8x8}, {Tx8x8, Tx8x8}},
		{{Tx16x16, Tx16x16}, {Tx16x16, Tx16x16}},
		{{Tx32x32, Tx32x32}, {Tx32x32, Tx32x32}},
	},
}

// PartitionContextLookup is the 4-bit (above, left) partition context
// pair used to select the partition probability model for a given block
// size. Each bit represents whether a level of split is implied by the
// block size: 1111 -> split possible at every level down to 8x8.
var PartitionContextLookup = [BlockSizes]struct {
	Above, Left int8
}{
	{15, 15}, // 4x4   - {0b1111, 0b1111}
	{15, 14}, // 4x8   - {0b1111, 0b1110}
	{14, 15}, // 8x4   - {0b1110, 0b1111}
	{14, 14}, // 8x8   - {0b1110, 0b1110}
	{14, 12}, // 8x16  - {0b1110, 0b1100}
	{12, 14}, // 16x8  - {0b1100, 0b1110}
	{12, 12}, // 16x16 - {0b1100, 0b1100}
	{12, 8},  // 16x32 - {0b1100, 0b1000}
	{8, 12},  // 32x16 - {0b1000, 0b1100}
	{8, 8},   // 32x32 - {0b1000, 0b1000}
	{8, 0},   // 32x64 - {0b1000, 0b0000}
	{0, 8},   // 64x32 - {0b0000, 0b1000}
	{0, 0},   // 64x64 - {0b0000, 0b0000}
}
