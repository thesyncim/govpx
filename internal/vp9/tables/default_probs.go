package tables

// VP9 default probability tables. Ported byte-for-byte from libvpx
// v1.16.0 vp9/common/vp9_entropymode.c. These seed every per-frame
// FRAME_CONTEXT when the decoder starts a new key/intra-only frame or
// when frame_context_idx forces a reset.
//
// Wire-stable: a single byte off in any of these would skew every
// boolean-coded decode the compressed header doesn't explicitly
// override.

// DefaultIntraInter mirrors default_intra_inter_p.
var DefaultIntraInter = [4]uint8{9, 102, 187, 225}

// DefaultCompInter mirrors default_comp_inter_p.
var DefaultCompInter = [5]uint8{239, 183, 119, 96, 41}

// DefaultCompRef mirrors default_comp_ref_p.
var DefaultCompRef = [5]uint8{50, 126, 123, 221, 226}

// DefaultSingleRef mirrors default_single_ref_p (REF_CONTEXTS × 2).
var DefaultSingleRef = [5][2]uint8{
	{33, 16}, {77, 74}, {142, 142}, {172, 170}, {238, 247},
}

// DefaultSkipProbs mirrors default_skip_probs.
var DefaultSkipProbs = [3]uint8{192, 128, 64}

// DefaultSwitchableInterpProb mirrors default_switchable_interp_prob —
// (SWITCHABLE_FILTER_CONTEXTS = 4) × (SWITCHABLE_FILTERS-1 = 2).
var DefaultSwitchableInterpProb = [4][2]uint8{
	{235, 162},
	{36, 255},
	{34, 3},
	{149, 144},
}

// DefaultPartitionProbs mirrors default_partition_probs —
// PARTITION_CONTEXTS=16 × (PARTITION_TYPES-1=3).
var DefaultPartitionProbs = [16][3]uint8{
	// 8x8 -> 4x4
	{199, 122, 141},
	{147, 63, 159},
	{148, 133, 118},
	{121, 104, 114},
	// 16x16 -> 8x8
	{174, 73, 87},
	{92, 41, 83},
	{82, 99, 50},
	{53, 39, 39},
	// 32x32 -> 16x16
	{177, 58, 59},
	{68, 26, 63},
	{52, 79, 25},
	{17, 14, 12},
	// 64x64 -> 32x32
	{222, 34, 30},
	{72, 16, 44},
	{58, 32, 12},
	{10, 7, 6},
}

// DefaultInterModeProbs mirrors default_inter_mode_probs —
// INTER_MODE_CONTEXTS=7 × (INTER_MODES-1=3).
var DefaultInterModeProbs = [7][3]uint8{
	{2, 173, 34},
	{7, 145, 85},
	{7, 166, 63},
	{7, 94, 66},
	{8, 64, 46},
	{17, 81, 31},
	{25, 29, 30},
}

// DefaultTxProbs mirrors libvpx's default_tx_probs — the seed for
// FRAME_CONTEXT.tx_probs. Layout matches the struct tx_probs triangle.
var (
	DefaultTxProbsP32x32 = [2][3]uint8{
		{3, 136, 37},
		{5, 52, 13},
	}
	DefaultTxProbsP16x16 = [2][2]uint8{
		{20, 152},
		{15, 101},
	}
	DefaultTxProbsP8x8 = [2][1]uint8{
		{100},
		{66},
	}
)

// DefaultIfYProbs mirrors default_if_y_probs — the inter-frame intra
// Y-mode probability table indexed by block-size group.
var DefaultIfYProbs = [4][9]uint8{
	{65, 32, 18, 144, 162, 194, 41, 51, 98},
	{132, 68, 18, 165, 217, 196, 45, 40, 78},
	{173, 80, 19, 176, 240, 193, 64, 35, 46},
	{221, 135, 38, 194, 248, 121, 96, 85, 29},
}

// DefaultIfUvProbs mirrors default_if_uv_probs — the inter-frame intra
// UV-mode probability table indexed by the selected Y mode.
var DefaultIfUvProbs = [10][9]uint8{
	{120, 7, 76, 176, 208, 126, 28, 54, 103},
	{48, 12, 154, 155, 139, 90, 34, 117, 119},
	{67, 6, 25, 204, 243, 158, 13, 21, 96},
	{97, 5, 44, 131, 176, 139, 48, 68, 97},
	{83, 5, 42, 156, 111, 152, 26, 49, 152},
	{80, 5, 58, 178, 74, 83, 33, 62, 145},
	{86, 5, 32, 154, 192, 168, 14, 22, 163},
	{85, 5, 32, 156, 216, 148, 19, 29, 73},
	{77, 7, 64, 116, 132, 122, 37, 126, 120},
	{101, 21, 107, 181, 192, 103, 19, 67, 125},
}
