package common

// Ported byte-for-byte from libvpx v1.16.0 vp9/common/vp9_enums.h. The
// numeric values are part of the wire format (probability tables, context
// arrays, scan orders, segmentation features all index by these), so the
// ordering must not change.

// Mode-info grid: each 64x64 superblock is an 8x8 grid of 8x8 mode-info
// units. These constants come straight from MI_SIZE_LOG2 / MI_BLOCK_SIZE_
// LOG2 in vp9_enums.h.
const (
	MiSizeLog2      = 3
	MiBlockSizeLog2 = 6 - MiSizeLog2

	MiSize      = 1 << MiSizeLog2      // pixels per mi-unit
	MiBlockSize = 1 << MiBlockSizeLog2 // mi-units per max block

	MiMask = MiBlockSize - 1
)

// BitstreamProfile is the 2-or-3-bit profile field in the uncompressed
// header. Profile 0 is 8-bit 4:2:0; profile 1 adds 4:4:4 / 4:2:2 / 4:4:0;
// profiles 2 and 3 carry 10/12-bit samples.
type BitstreamProfile uint8

const (
	Profile0 BitstreamProfile = iota
	Profile1
	Profile2
	Profile3
	MaxProfiles
)

// BlockSize lists every partition shape that can appear in a VP9
// superblock. Values are byte-stable: scan-order tables, partition
// probability tables, and SAD/variance kernels all index by these.
type BlockSize uint8

const (
	Block4x4 BlockSize = iota
	Block4x8
	Block8x4
	Block8x8
	Block8x16
	Block16x8
	Block16x16
	Block16x32
	Block32x16
	Block32x32
	Block32x64
	Block64x32
	Block64x64
	BlockSizes
	BlockInvalid = BlockSizes
)

// PartitionType is the recursive-split decision emitted for every
// non-leaf node in the partition tree.
type PartitionType uint8

const (
	PartitionNone PartitionType = iota
	PartitionHorz
	PartitionVert
	PartitionSplit
	PartitionTypes
	PartitionInvalid = PartitionTypes
)

// Partition context layout: 4 partition-context bins per block size, 4
// block sizes that need them — see vp9_enums.h.
const (
	PartitionPloffset = 4
	PartitionContexts = 4 * PartitionPloffset
)

// TxSize is the transform block size. The four valid values (4, 8, 16,
// 32) directly map to the inverse-transform kernel indices.
type TxSize uint8

const (
	Tx4x4   TxSize = 0
	Tx8x8   TxSize = 1
	Tx16x16 TxSize = 2
	Tx32x32 TxSize = 3
	TxSizes TxSize = 4
)

// TxMode is the frame-level transform mode signalled in the compressed
// header. TxModeSelect lets every block pick its own size; the others
// cap the largest transform allowed.
type TxMode uint8

const (
	Only4x4      TxMode = 0
	Allow8x8     TxMode = 1
	Allow16x16   TxMode = 2
	Allow32x32   TxMode = 3
	TxModeSelect TxMode = 4
	TxModes      TxMode = 5
)

// TxType is the {horizontal,vertical} transform pair used by 4x4 / 8x8 /
// 16x16 intra blocks driven by intra prediction direction.
type TxType uint8

const (
	DctDct   TxType = 0 // DCT in both directions
	AdstDct  TxType = 1 // ADST vertical, DCT horizontal
	DctAdst  TxType = 2 // DCT vertical, ADST horizontal
	AdstAdst TxType = 3 // ADST both directions
	TxTypes  TxType = 4
)

// VP9 reference-frame bit flags used by VP9_REFFRAME in vp9_enums.h.
const (
	Vp9LastFlag = 1 << 0
	Vp9GoldFlag = 1 << 1
	Vp9AltFlag  = 1 << 2
)

// PlaneType separates luma and chroma where the codec treats them with
// independent probability / dequant tables.
type PlaneType uint8

const (
	PlaneTypeY  PlaneType = 0
	PlaneTypeUV PlaneType = 1
	PlaneTypes  PlaneType = 2
)

// PredictionMode covers the 10 intra modes (DC..TM) and the 4 inter
// modes (NEARESTMV / NEARMV / ZEROMV / NEWMV). The numeric ordering is
// part of every probability table indexed by mode, so it must not move.
type PredictionMode uint8

const (
	DcPred    PredictionMode = 0
	VPred     PredictionMode = 1
	HPred     PredictionMode = 2
	D45Pred   PredictionMode = 3
	D135Pred  PredictionMode = 4
	D117Pred  PredictionMode = 5
	D153Pred  PredictionMode = 6
	D207Pred  PredictionMode = 7
	D63Pred   PredictionMode = 8
	TmPred    PredictionMode = 9
	NearestMv PredictionMode = 10
	NearMv    PredictionMode = 11
	ZeroMv    PredictionMode = 12
	NewMv     PredictionMode = 13

	MbModeCount = 14
	IntraModes  = int(TmPred) + 1
	InterModes  = 1 + int(NewMv) - int(NearestMv)

	SkipContexts       = 3
	InterModeContexts  = 7
	MaxMvRefCandidates = 2
	IntraInterContexts = 4
	CompInterContexts  = 5
	RefContexts        = 5
)
