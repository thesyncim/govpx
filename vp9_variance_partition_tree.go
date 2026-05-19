package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9_variance_partition_tree.go is the Phase B substrate for the libvpx
// VP9 choose_partitioning port: the variance-tree data types, fill /
// aggregation helpers, and the set_vt_partitioning recursive descent.
//
// Verbatim port of:
//   - vp9/encoder/vp9_encodeframe.c:335-342 (set_block_size)
//   - vp9/encoder/vp9_encodeframe.c:344-396 (Var, partition_variance,
//     v4x4 / v8x8 / v16x16 / v32x32 / v64x64, variance_node, TREE_LEVEL)
//   - vp9/encoder/vp9_encodeframe.c:397-470 (tree_to_node, fill_variance,
//     get_variance, sum_2_variances, fill_variance_tree)
//   - vp9/encoder/vp9_encodeframe.c:472-547 (set_vt_partitioning)
//   - vp9/encoder/vp9_encodeframe.c:714-784 (fill_variance_4x4avg,
//     fill_variance_8x8avg)
//
// Phase A landed the threshold substrate at 39edcf8 / 906ff68 (see
// vp9_variance_partition.go). Phase C will wire these helpers into
// choose_partitioning and replace the existing
// pickVP9CBRVariancePartitionBlockSize / pickVP9KeyframeVariancePartitionBlockSize
// adapters. This commit lands only the substrate plus pinning unit tests
// — no live caller consumes these symbols yet.

// vp9Var mirrors libvpx's Var struct (vp9/encoder/vp9_encodeframe.c:344-353):
//
//	typedef struct {
//	  uint32_t sum_square_error;
//	  int32_t sum_error;
//	  int log2_count;
//	  int variance;
//	} Var;
//
// The libvpx comment documents that uint32_t for sum_square_error suffices
// even at high bitdepth (2^12 * 2^12 * 16 * 16 = 2^32). govpx mirrors the
// exact field widths.
type vp9Var struct {
	SumSquareError uint32
	SumError       int32
	Log2Count      int
	Variance       int
}

// vp9PartitionVariance mirrors libvpx's partition_variance struct
// (vp9/encoder/vp9_encodeframe.c:355-359):
//
//	typedef struct {
//	  Var none;
//	  Var horz[2];
//	  Var vert[2];
//	} partition_variance;
type vp9PartitionVariance struct {
	None vp9Var
	Horz [2]vp9Var
	Vert [2]vp9Var
}

// vp9V4x4 mirrors libvpx's v4x4 (vp9/encoder/vp9_encodeframe.c:361-364).
//
//	typedef struct {
//	  partition_variance part_variances;
//	  Var split[4];
//	} v4x4;
type vp9V4x4 struct {
	PartVariances vp9PartitionVariance
	Split         [4]vp9Var
}

// vp9V8x8 mirrors libvpx's v8x8 (vp9/encoder/vp9_encodeframe.c:366-369).
//
//	typedef struct {
//	  partition_variance part_variances;
//	  v4x4 split[4];
//	} v8x8;
type vp9V8x8 struct {
	PartVariances vp9PartitionVariance
	Split         [4]vp9V4x4
}

// vp9V16x16 mirrors libvpx's v16x16 (vp9/encoder/vp9_encodeframe.c:371-374).
//
//	typedef struct {
//	  partition_variance part_variances;
//	  v8x8 split[4];
//	} v16x16;
type vp9V16x16 struct {
	PartVariances vp9PartitionVariance
	Split         [4]vp9V8x8
}

// vp9V32x32 mirrors libvpx's v32x32 (vp9/encoder/vp9_encodeframe.c:376-379).
//
//	typedef struct {
//	  partition_variance part_variances;
//	  v16x16 split[4];
//	} v32x32;
type vp9V32x32 struct {
	PartVariances vp9PartitionVariance
	Split         [4]vp9V16x16
}

// vp9V64x64 mirrors libvpx's v64x64 (vp9/encoder/vp9_encodeframe.c:381-384).
//
//	typedef struct {
//	  partition_variance part_variances;
//	  v32x32 split[4];
//	} v64x64;
type vp9V64x64 struct {
	PartVariances vp9PartitionVariance
	Split         [4]vp9V32x32
}

// vp9VarianceNode mirrors libvpx's variance_node
// (vp9/encoder/vp9_encodeframe.c:386-389):
//
//	typedef struct {
//	  partition_variance *part_variances;
//	  Var *split[4];
//	} variance_node;
//
// This is the flat dispatch view produced by tree_to_node — it carries
// pointers into one of the typed v{4,8,16,32,64} trees so the variance
// fill / aggregation logic can be written once.
type vp9VarianceNode struct {
	PartVariances *vp9PartitionVariance
	Split         [4]*vp9Var
}

// vp9TreeLevel mirrors libvpx's TREE_LEVEL enum
// (vp9/encoder/vp9_encodeframe.c:391-395):
//
//	typedef enum {
//	  V16X16,
//	  V32X32,
//	  V64X64,
//	} TREE_LEVEL;
type vp9TreeLevel int

const (
	vp9TreeLevelV16x16 vp9TreeLevel = iota
	vp9TreeLevelV32x32
	vp9TreeLevelV64x64
)

// vp9TreeToNode is the verbatim port of libvpx's tree_to_node
// (vp9/encoder/vp9_encodeframe.c:397-437). It populates the
// vp9VarianceNode dispatch view with pointers into one of the typed
// v{4,8,16,32,64} trees, keyed by BLOCK_SIZE.
//
// The caller passes one of vp9V64x64 / vp9V32x32 / vp9V16x16 / vp9V8x8 /
// vp9V4x4 (matching the bsize) and we wire its PartVariances pointer and
// the four .Split children's .PartVariances.None back into node.Split[].
// For BLOCK_4X4 (the default fallthrough in libvpx) the .Split entries
// point at the four leaf Var fields directly.
func vp9TreeToNode(node *vp9VarianceNode, bsize common.BlockSize,
	v64 *vp9V64x64, v32 *vp9V32x32, v16 *vp9V16x16, v8 *vp9V8x8, v4 *vp9V4x4,
) {
	node.PartVariances = nil
	switch bsize {
	case common.Block64x64:
		node.PartVariances = &v64.PartVariances
		for i := range 4 {
			node.Split[i] = &v64.Split[i].PartVariances.None
		}
	case common.Block32x32:
		node.PartVariances = &v32.PartVariances
		for i := range 4 {
			node.Split[i] = &v32.Split[i].PartVariances.None
		}
	case common.Block16x16:
		node.PartVariances = &v16.PartVariances
		for i := range 4 {
			node.Split[i] = &v16.Split[i].PartVariances.None
		}
	case common.Block8x8:
		node.PartVariances = &v8.PartVariances
		for i := range 4 {
			node.Split[i] = &v8.Split[i].PartVariances.None
		}
	default: // BLOCK_4X4 (libvpx asserts bsize == BLOCK_4X4 here).
		node.PartVariances = &v4.PartVariances
		for i := range 4 {
			node.Split[i] = &v4.Split[i]
		}
	}
}

// vp9FillVariance is the verbatim port of libvpx's fill_variance
// (vp9/encoder/vp9_encodeframe.c:440-444):
//
//	static void fill_variance(uint32_t s2, int32_t s, int c, Var *v) {
//	  v->sum_square_error = s2;
//	  v->sum_error = s;
//	  v->log2_count = c;
//	}
//
// Note that fill_variance leaves variance untouched — the .variance field
// is computed lazily on demand by vp9GetVariance.
func vp9FillVariance(sumSquareError uint32, sumError int32, log2Count int, v *vp9Var) {
	v.SumSquareError = sumSquareError
	v.SumError = sumError
	v.Log2Count = log2Count
}

// vp9GetVariance is the verbatim port of libvpx's get_variance
// (vp9/encoder/vp9_encodeframe.c:446-452):
//
//	static void get_variance(Var *v) {
//	  v->variance =
//	      (int)(256 * (v->sum_square_error -
//	                   (uint32_t)(((int64_t)v->sum_error * v->sum_error) >>
//	                              v->log2_count)) >>
//	            v->log2_count);
//	}
//
// libvpx applies the bias correction (E[X^2] - E[X]^2 expressed in fixed
// point via the >> log2_count shifts) and scales by 256 before the final
// shift. The cast to uint32_t on the mean-of-squares term is part of the
// C source — we mirror that exact arithmetic.
func vp9GetVariance(v *vp9Var) {
	bias := uint32((int64(v.SumError) * int64(v.SumError)) >> uint(v.Log2Count))
	v.Variance = int((uint32(256) * (v.SumSquareError - bias)) >> uint(v.Log2Count))
}

// vp9Sum2Variances is the verbatim port of libvpx's sum_2_variances
// (vp9/encoder/vp9_encodeframe.c:454-458):
//
//	static void sum_2_variances(const Var *a, const Var *b, Var *r) {
//	  assert(a->log2_count == b->log2_count);
//	  fill_variance(a->sum_square_error + b->sum_square_error,
//	                a->sum_error + b->sum_error, a->log2_count + 1, r);
//	}
//
// The assert is preserved as a panic in govpx — caller violation indicates
// a malformed variance tree, not user input.
func vp9Sum2Variances(a, b, r *vp9Var) {
	if a.Log2Count != b.Log2Count {
		panic("vp9Sum2Variances: log2_count mismatch")
	}
	vp9FillVariance(a.SumSquareError+b.SumSquareError,
		a.SumError+b.SumError, a.Log2Count+1, r)
}

// vp9FillVarianceTree is the verbatim port of libvpx's fill_variance_tree
// (vp9/encoder/vp9_encodeframe.c:460-470):
//
//	static void fill_variance_tree(void *data, BLOCK_SIZE bsize) {
//	  variance_node node;
//	  memset(&node, 0, sizeof(node));
//	  tree_to_node(data, bsize, &node);
//	  sum_2_variances(node.split[0], node.split[1], &node.part_variances->horz[0]);
//	  sum_2_variances(node.split[2], node.split[3], &node.part_variances->horz[1]);
//	  sum_2_variances(node.split[0], node.split[2], &node.part_variances->vert[0]);
//	  sum_2_variances(node.split[1], node.split[3], &node.part_variances->vert[1]);
//	  sum_2_variances(&node.part_variances->vert[0], &node.part_variances->vert[1],
//	                  &node.part_variances->none);
//	}
//
// Aggregates the four children of a tree node into the four directional
// halves (horz[0,1], vert[0,1]) and the full-block none variance.
//
// Five typed overloads are provided so callers don't have to dispatch by
// BLOCK_SIZE manually — each delegates to the same logic against the
// caller-typed sub-tree.
func vp9FillVarianceTreeV64x64(vt *vp9V64x64) {
	var node vp9VarianceNode
	vp9TreeToNode(&node, common.Block64x64, vt, nil, nil, nil, nil)
	vp9FillVarianceTreeBody(&node)
}

func vp9FillVarianceTreeV32x32(vt *vp9V32x32) {
	var node vp9VarianceNode
	vp9TreeToNode(&node, common.Block32x32, nil, vt, nil, nil, nil)
	vp9FillVarianceTreeBody(&node)
}

func vp9FillVarianceTreeV16x16(vt *vp9V16x16) {
	var node vp9VarianceNode
	vp9TreeToNode(&node, common.Block16x16, nil, nil, vt, nil, nil)
	vp9FillVarianceTreeBody(&node)
}

func vp9FillVarianceTreeV8x8(vt *vp9V8x8) {
	var node vp9VarianceNode
	vp9TreeToNode(&node, common.Block8x8, nil, nil, nil, vt, nil)
	vp9FillVarianceTreeBody(&node)
}

func vp9FillVarianceTreeV4x4(vt *vp9V4x4) {
	var node vp9VarianceNode
	vp9TreeToNode(&node, common.Block4x4, nil, nil, nil, nil, vt)
	vp9FillVarianceTreeBody(&node)
}

// vp9FillVarianceTreeBody is the libvpx logic body — the four directional
// sums plus the final horz->none aggregation. Shared by every typed
// fill_variance_tree wrapper above.
func vp9FillVarianceTreeBody(node *vp9VarianceNode) {
	vp9Sum2Variances(node.Split[0], node.Split[1], &node.PartVariances.Horz[0])
	vp9Sum2Variances(node.Split[2], node.Split[3], &node.PartVariances.Horz[1])
	vp9Sum2Variances(node.Split[0], node.Split[2], &node.PartVariances.Vert[0])
	vp9Sum2Variances(node.Split[1], node.Split[3], &node.PartVariances.Vert[1])
	vp9Sum2Variances(&node.PartVariances.Vert[0], &node.PartVariances.Vert[1],
		&node.PartVariances.None)
}

// vp9Avg4x4 computes the rounded average of a 4x4 luma block. Mirrors
// libvpx's vpx_avg_4x4 (vpx_dsp/avg.c:18-29) — the source samples summed
// and then rounded with +8 before >>4 (16 = 4*4 pels). govpx replicates
// only the 8-bit path; CONFIG_VP9_HIGHBITDEPTH is off in the default
// libvpx build that govpx targets.
func vp9Avg4x4(src []uint8, stride int) int {
	sum := 0
	for r := range 4 {
		row := src[r*stride:]
		sum += int(row[0]) + int(row[1]) + int(row[2]) + int(row[3])
	}
	return (sum + 8) >> 4
}

// vp9Avg4x4Clamped matches libvpx's effective edge reads on a YV12 buffer
// whose borders have already been extended; Go callers pass raw visible planes.
func vp9Avg4x4Clamped(src []uint8, stride, x0, y0, pixelsWide, pixelsHigh int) int {
	if pixelsWide <= 0 || pixelsHigh <= 0 {
		return 0
	}
	sum := 0
	maxX := pixelsWide - 1
	maxY := pixelsHigh - 1
	for r := range 4 {
		y := min(y0+r, maxY)
		row := src[y*stride:]
		for c := range 4 {
			x := min(x0+c, maxX)
			sum += int(row[x])
		}
	}
	return (sum + 8) >> 4
}

// vp9Avg8x8 computes the rounded average of an 8x8 luma block. Mirrors
// libvpx's vpx_avg_8x8 (vpx_dsp/avg.c:13-22) — +32 before >>6 (64 pels).
func vp9Avg8x8(src []uint8, stride int) int {
	sum := 0
	for r := range 8 {
		row := src[r*stride:]
		sum += int(row[0]) + int(row[1]) + int(row[2]) + int(row[3]) +
			int(row[4]) + int(row[5]) + int(row[6]) + int(row[7])
	}
	return (sum + 32) >> 6
}

// vp9Avg8x8Clamped is the 8x8 counterpart to vp9Avg4x4Clamped.
func vp9Avg8x8Clamped(src []uint8, stride, x0, y0, pixelsWide, pixelsHigh int) int {
	if pixelsWide <= 0 || pixelsHigh <= 0 {
		return 0
	}
	sum := 0
	maxX := pixelsWide - 1
	maxY := pixelsHigh - 1
	for r := range 8 {
		y := min(y0+r, maxY)
		row := src[y*stride:]
		for c := range 8 {
			x := min(x0+c, maxX)
			sum += int(row[x])
		}
	}
	return (sum + 32) >> 6
}

// vp9FillVariance4x4Avg is the verbatim port of libvpx's
// fill_variance_4x4avg (vp9/encoder/vp9_encodeframe.c:714-748).
//
// For each of the four 4x4 sub-blocks inside an 8x8 region, compute the
// SAD-of-averages between the source and the predictor, then call
// fill_variance with (sse, sum, log2_count=0). For key frames the
// predictor average is forced to 128 — the libvpx d_avg = 128 default
// covers the case where d is unused.
//
// govpx reuses the source slice (src) for both s and d when called on a
// key frame (the predictor is never read on the key-frame path); inter
// callers pass distinct slices and dStride.
func vp9FillVariance4x4Avg(src []uint8, srcStride int, dst []uint8, dstStride int,
	x8Idx, y8Idx int, vst *vp9V8x8, pixelsWide, pixelsHigh int, isKeyFrame bool,
) {
	for k := range 4 {
		x4Idx := x8Idx + ((k & 1) << 2)
		y4Idx := y8Idx + ((k >> 1) << 2)
		var sse uint32
		var sum int32
		if x4Idx < pixelsWide && y4Idx < pixelsHigh {
			sAvg := vp9Avg4x4Clamped(src, srcStride, x4Idx, y4Idx,
				pixelsWide, pixelsHigh)
			dAvg := 128
			if !isKeyFrame {
				dAvg = vp9Avg4x4Clamped(dst, dstStride, x4Idx, y4Idx,
					pixelsWide, pixelsHigh)
			}
			diff := sAvg - dAvg
			sum = int32(diff)
			sse = uint32(diff * diff)
		}
		vp9FillVariance(sse, sum, 0, &vst.Split[k].PartVariances.None)
	}
}

// vp9FillVariance8x8Avg is the verbatim port of libvpx's
// fill_variance_8x8avg (vp9/encoder/vp9_encodeframe.c:750-784).
//
// Same shape as fill_variance_4x4avg but stepping over the four 8x8
// sub-blocks of a 16x16 region.
func vp9FillVariance8x8Avg(src []uint8, srcStride int, dst []uint8, dstStride int,
	x16Idx, y16Idx int, vst *vp9V16x16, pixelsWide, pixelsHigh int, isKeyFrame bool,
) {
	for k := range 4 {
		x8Idx := x16Idx + ((k & 1) << 3)
		y8Idx := y16Idx + ((k >> 1) << 3)
		var sse uint32
		var sum int32
		if x8Idx < pixelsWide && y8Idx < pixelsHigh {
			sAvg := vp9Avg8x8Clamped(src, srcStride, x8Idx, y8Idx,
				pixelsWide, pixelsHigh)
			dAvg := 128
			if !isKeyFrame {
				dAvg = vp9Avg8x8Clamped(dst, dstStride, x8Idx, y8Idx,
					pixelsWide, pixelsHigh)
			}
			diff := sAvg - dAvg
			sum = int32(diff)
			sse = uint32(diff * diff)
		}
		vp9FillVariance(sse, sum, 0, &vst.Split[k].PartVariances.None)
	}
}

// vp9SetBlockSize is the verbatim port of libvpx's set_block_size
// (vp9/encoder/vp9_encodeframe.c:335-342):
//
//	static void set_block_size(VP9_COMP *const cpi, MACROBLOCK *const x,
//	                           MACROBLOCKD *const xd, int mi_row, int mi_col,
//	                           BLOCK_SIZE bsize) {
//	  if (cpi->common.mi_cols > mi_col && cpi->common.mi_rows > mi_row) {
//	    set_mode_info_offsets(&cpi->common, x, xd, mi_row, mi_col);
//	    xd->mi[0]->sb_type = bsize;
//	  }
//	}
//
// govpx writes directly into the caller-supplied MI grid slice rather
// than through libvpx's cpi/x/xd offset machinery. The bounds check
// mirrors libvpx's `mi_cols > mi_col && mi_rows > mi_row` predicate
// exactly; out-of-range coordinates are dropped (libvpx returns without
// touching xd->mi[0]).
func vp9SetBlockSize(miGrid []vp9dec.NeighborMi, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize,
) {
	if miCols > miCol && miRows > miRow && miRow >= 0 && miCol >= 0 {
		idx := miRow*miCols + miCol
		if idx >= 0 && idx < len(miGrid) {
			miGrid[idx].SbType = bsize
		}
	}
}

// vp9SetVTPartitioningArgs bundles the libvpx pointer-typed inputs to
// set_vt_partitioning so the Go signature stays manageable. The variance
// tree is dispatched through the same vp9TreeToNode mechanism as
// fill_variance_tree — callers pass whichever typed tree matches bsize
// and leave the others nil.
type vp9SetVTPartitioningArgs struct {
	V64 *vp9V64x64
	V32 *vp9V32x32
	V16 *vp9V16x16
	V8  *vp9V8x8
	V4  *vp9V4x4
}

// vp9SetVTPartitioning is the verbatim port of libvpx's set_vt_partitioning
// (vp9/encoder/vp9_encodeframe.c:472-547). Returns true (libvpx's `1`)
// when this level claimed the block (terminal or split into halves);
// false (libvpx's `0`) when the caller should recurse into the four
// quadrants.
//
// Phase B notes:
//
//   - libvpx's `frame_is_intra_only(cm)` becomes the explicit isKeyFrame
//     argument so we don't pull in the encoder context here.
//
//   - libvpx's `get_plane_block_size(subsize, &xd->plane[1]) < BLOCK_INVALID`
//     check defends against chroma block sizes that don't exist (e.g.
//     a horz/vert split that would create an unrepresentable chroma
//     shape). govpx wires this via the chromaPlaneBlockOK callback so
//     the substrate doesn't depend on the per-plane geometry helpers
//     that Phase C will reuse from the production encoder.
//
//   - libvpx aborts via `if (force_split == 1) return 0` before any
//     tree-to-node work. govpx mirrors this.
//
//   - bsize ranges over BLOCK_8X8..BLOCK_64X64; bsizeMin is the
//     downsample floor (BLOCK_16X16 / BLOCK_8X8 typically, picked by
//     the caller per libvpx's downsample heuristic).
func vp9SetVTPartitioning(miGrid []vp9dec.NeighborMi, miRows, miCols, miRow, miCol int,
	bsize, bsizeMin common.BlockSize, threshold int64, forceSplit bool, isKeyFrame bool,
	args vp9SetVTPartitioningArgs,
	chromaPlaneBlockOK func(subsize common.BlockSize) bool,
) bool {
	var vt vp9VarianceNode
	blockWidth := int(common.Num8x8BlocksWideLookup[bsize])
	blockHeight := int(common.Num8x8BlocksHighLookup[bsize])
	if blockWidth != blockHeight {
		// libvpx: assert(block_height == block_width)
		panic("vp9SetVTPartitioning: non-square bsize")
	}
	vp9TreeToNode(&vt, bsize, args.V64, args.V32, args.V16, args.V8, args.V4)

	if forceSplit {
		return false
	}

	// For bsize == bsize_min, select if variance is below threshold,
	// otherwise split will be selected.
	if bsize == bsizeMin {
		if isKeyFrame {
			vp9GetVariance(&vt.PartVariances.None)
		}
		if miCol+blockWidth/2 < miCols &&
			miRow+blockHeight/2 < miRows &&
			int64(vt.PartVariances.None.Variance) < threshold {
			vp9SetBlockSize(miGrid, miRows, miCols, miRow, miCol, bsize)
			return true
		}
		return false
	} else if bsize > bsizeMin {
		if isKeyFrame {
			vp9GetVariance(&vt.PartVariances.None)
		}
		// For key frame: take split for bsize above 32X32 or very high
		// variance.
		if isKeyFrame &&
			(bsize > common.Block32x32 ||
				int64(vt.PartVariances.None.Variance) > (threshold<<4)) {
			return false
		}
		// If variance is low, take the bsize (no split).
		if miCol+blockWidth/2 < miCols &&
			miRow+blockHeight/2 < miRows &&
			int64(vt.PartVariances.None.Variance) < threshold {
			vp9SetBlockSize(miGrid, miRows, miCols, miRow, miCol, bsize)
			return true
		}

		// Check vertical split.
		if miRow+blockHeight/2 < miRows {
			subsize := common.SubsizeLookup[common.PartitionVert][bsize]
			vp9GetVariance(&vt.PartVariances.Vert[0])
			vp9GetVariance(&vt.PartVariances.Vert[1])
			chromaOK := true
			if chromaPlaneBlockOK != nil {
				chromaOK = chromaPlaneBlockOK(subsize)
			}
			if int64(vt.PartVariances.Vert[0].Variance) < threshold &&
				int64(vt.PartVariances.Vert[1].Variance) < threshold &&
				chromaOK {
				vp9SetBlockSize(miGrid, miRows, miCols, miRow, miCol, subsize)
				vp9SetBlockSize(miGrid, miRows, miCols, miRow, miCol+blockWidth/2, subsize)
				return true
			}
		}
		// Check horizontal split.
		if miCol+blockWidth/2 < miCols {
			subsize := common.SubsizeLookup[common.PartitionHorz][bsize]
			vp9GetVariance(&vt.PartVariances.Horz[0])
			vp9GetVariance(&vt.PartVariances.Horz[1])
			chromaOK := true
			if chromaPlaneBlockOK != nil {
				chromaOK = chromaPlaneBlockOK(subsize)
			}
			if int64(vt.PartVariances.Horz[0].Variance) < threshold &&
				int64(vt.PartVariances.Horz[1].Variance) < threshold &&
				chromaOK {
				vp9SetBlockSize(miGrid, miRows, miCols, miRow, miCol, subsize)
				vp9SetBlockSize(miGrid, miRows, miCols, miRow+blockHeight/2, miCol, subsize)
				return true
			}
		}

		return false
	}
	return false
}
