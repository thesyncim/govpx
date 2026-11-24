package common

// Canonical token trees that drive every VP9 mode / partition / inter-
// mode / interp-filter / segmentation decode through the boolean
// range coder's vpx_read_tree primitive. Ported byte-for-byte from
// libvpx v1.16.0:
//   - vp9/common/vp9_entropymode.c (intra, inter, partition, switchable
//     interp)
//   - vp9/common/vp9_seg_common.c (segment)
//
// TreeSize(N) = 2*N - 2 — the flat layout libvpx's TREE_SIZE macro
// produces for a binary tree with N leaves. Each pair of consecutive
// entries holds the (left, right) link from an internal node;
// positive values are next-node indices into the same array, zero or
// negative values are leaf labels stored as the negation of the
// decoded value.

// IntraModeTree mirrors libvpx's vp9_intra_mode_tree — 10 intra-pred
// modes ordered for the canonical traversal.
var IntraModeTree = [18]int8{
	-int8(DcPred), 2, // 0: DC_NODE
	-int8(TmPred), 4, // 1: TM_NODE
	-int8(VPred), 6, // 2: V_NODE
	8, 12, // 3: COM_NODE
	-int8(HPred), 10, // 4: H_NODE
	-int8(D135Pred), -int8(D117Pred), // 5: D135_NODE
	-int8(D45Pred), 14, // 6: D45_NODE
	-int8(D63Pred), 16, // 7: D63_NODE
	-int8(D153Pred), -int8(D207Pred), // 8: D153_NODE
}

// Inter mode indices used by the tree below. INTER_OFFSET(mode) maps
// {NEARESTMV, NEARMV, ZEROMV, NEWMV} to {0, 1, 2, 3} by subtracting
// NEARESTMV.
const (
	interOffsetNearestMv = 0
	interOffsetNearMv    = 1
	interOffsetZeroMv    = 2
	interOffsetNewMv     = 3
)

// InterModeTree mirrors libvpx's vp9_inter_mode_tree. Note the
// INTER_OFFSET indirection: leaf values are offsets into the inter
// mode subspace, not raw PredictionMode codes.
var InterModeTree = [6]int8{
	-interOffsetZeroMv, 2,
	-interOffsetNearestMv, 4,
	-interOffsetNearMv, -interOffsetNewMv,
}

// PartitionTree mirrors libvpx's vp9_partition_tree.
var PartitionTree = [6]int8{
	-int8(PartitionNone), 2,
	-int8(PartitionHorz), 4,
	-int8(PartitionVert), -int8(PartitionSplit),
}

// SwitchableInterpTree mirrors libvpx's vp9_switchable_interp_tree.
// Leaves are the InterpFilter constants — see internal/vp9/decoder
// for the matching enum.
const (
	// Mirrored from internal/vp9/decoder/header_size.go to keep this
	// tree self-contained at the common layer. Any drift would skew
	// every motion-compensation filter dispatch.
	interpFilterEighttap       = 0
	interpFilterEighttapSmooth = 1
	interpFilterEighttapSharp  = 2
)

var SwitchableInterpTree = [4]int8{
	-interpFilterEighttap, 2,
	-interpFilterEighttapSmooth, -interpFilterEighttapSharp,
}

// SegmentTree mirrors libvpx's vp9_segment_tree — a balanced 8-leaf
// tree for the segmentation index.
var SegmentTree = [14]int8{
	2, 4, 6, 8, 10, 12, 0, -1, -2, -3, -4, -5, -6, -7,
}
