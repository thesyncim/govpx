package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// These tests pin the variance-tree helpers against expected values derived by
// hand from the libvpx v1.16.0 source.
//
// libvpx refs:
//   - fill_variance:           vp9/encoder/vp9_encodeframe.c:440-444
//   - get_variance:            vp9/encoder/vp9_encodeframe.c:446-452
//   - sum_2_variances:         vp9/encoder/vp9_encodeframe.c:454-458
//   - tree_to_node:            vp9/encoder/vp9_encodeframe.c:397-437
//   - fill_variance_tree:      vp9/encoder/vp9_encodeframe.c:460-470
//   - fill_variance_4x4avg:    vp9/encoder/vp9_encodeframe.c:714-748
//   - fill_variance_8x8avg:    vp9/encoder/vp9_encodeframe.c:750-784
//   - set_block_size:          vp9/encoder/vp9_encodeframe.c:335-342
//   - set_vt_partitioning:     vp9/encoder/vp9_encodeframe.c:472-547

// TestVP9FillVarianceAssigns pins fill_variance: it must copy the three
// inputs verbatim into the Var struct and must NOT touch .variance.
func TestVP9FillVarianceAssigns(t *testing.T) {
	v := varianceStat{Variance: 0xDEAD}
	fillVariance(1234, -56, 4, &v)
	if v.SumSquareError != 1234 {
		t.Errorf("SumSquareError = %d, want 1234", v.SumSquareError)
	}
	if v.SumError != -56 {
		t.Errorf("SumError = %d, want -56", v.SumError)
	}
	if v.Log2Count != 4 {
		t.Errorf("Log2Count = %d, want 4", v.Log2Count)
	}
	if v.Variance != 0xDEAD {
		t.Errorf("Variance = %d, want 0xDEAD (fill_variance must not touch it)",
			v.Variance)
	}
}

// TestVP9GetVarianceFormula reproduces libvpx's exact get_variance
// arithmetic for several hand-checked cases:
//
//	variance = (int)(256 * (sse - (uint32_t)((int64_t)sum*sum >> log2_count))
//	                  >> log2_count)
//
// Case 1: sse=0, sum=0, log2_count=4 => variance = 0.
// Case 2: sse=64, sum=8, log2_count=4 => bias = (64>>4) = 4;
//
//	variance = (256 * (64-4)) >> 4 = (256 * 60) >> 4 = 15360 >> 4 = 960.
//
// Case 3: sse=400, sum=20, log2_count=2 => bias = (400>>2) = 100;
//
//	variance = (256 * (400-100)) >> 2 = (256 * 300) >> 2 = 76800>>2 = 19200.
//
// Case 4: sse=10000, sum=-100, log2_count=6 => bias = (10000>>6) = 156;
//
//	variance = (256 * (10000-156)) >> 6 = (256*9844)>>6 = 2520064>>6 = 39376.
func TestVP9GetVarianceFormula(t *testing.T) {
	cases := []struct {
		name      string
		sse       uint32
		sum       int32
		log2Count int
		want      int
	}{
		{"zero", 0, 0, 4, 0},
		{"sum8_sse64_l4", 64, 8, 4, 960},
		{"sum20_sse400_l2", 400, 20, 2, 19200},
		{"sumNeg100_sse10000_l6", 10000, -100, 6, 39376},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := varianceStat{
				SumSquareError: tc.sse,
				SumError:       tc.sum,
				Log2Count:      tc.log2Count,
			}
			getVariance(&v)
			if v.Variance != tc.want {
				t.Errorf("variance = %d, want %d", v.Variance, tc.want)
			}
		})
	}
}

// TestVP9Sum2VariancesAddsAndBumpsCount pins sum_2_variances: it adds
// sum_square_error and sum_error pairwise and bumps log2_count by 1. The
// .variance field is reset (libvpx fill_variance leaves it untouched,
// but the destination is a fresh Var in libvpx callers so it starts at
// 0; here we observe that fill_variance does not overwrite it — same as
// TestVP9FillVarianceAssigns).
func TestVP9Sum2VariancesAddsAndBumpsCount(t *testing.T) {
	a := varianceStat{SumSquareError: 100, SumError: 10, Log2Count: 3}
	b := varianceStat{SumSquareError: 50, SumError: -4, Log2Count: 3}
	var r varianceStat
	sum2Variances(&a, &b, &r)
	if r.SumSquareError != 150 {
		t.Errorf("SumSquareError = %d, want 150", r.SumSquareError)
	}
	if r.SumError != 6 {
		t.Errorf("SumError = %d, want 6", r.SumError)
	}
	if r.Log2Count != 4 {
		t.Errorf("Log2Count = %d, want 4 (bumped from 3)", r.Log2Count)
	}
}

// TestVP9Sum2VariancesMismatchPanics pins the libvpx
// assert(a->log2_count == b->log2_count).
func TestVP9Sum2VariancesMismatchPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on log2_count mismatch")
		}
	}()
	a := varianceStat{Log2Count: 2}
	b := varianceStat{Log2Count: 3}
	var r varianceStat
	sum2Variances(&a, &b, &r)
}

// TestVP9TreeToNodeDispatch pins tree_to_node for each BLOCK_SIZE: the
// PartVariances pointer must alias the typed tree's part_variances, and
// the four Split pointers must alias the children's part_variances.None
// (or in the BLOCK_4X4 case, the leaf Var entries).
func TestVP9TreeToNodeDispatch(t *testing.T) {
	t.Run("v64x64", func(t *testing.T) {
		var v64 V64x64
		var node varianceNode
		treeToNode(&node, common.Block64x64, &v64, nil, nil, nil, nil)
		if node.PartVariances != &v64.PartVariances {
			t.Errorf("PartVariances mismatch")
		}
		for i := range 4 {
			if node.Split[i] != &v64.Split[i].PartVariances.None {
				t.Errorf("Split[%d] mismatch", i)
			}
		}
	})
	t.Run("v32x32", func(t *testing.T) {
		var v32 V32x32
		var node varianceNode
		treeToNode(&node, common.Block32x32, nil, &v32, nil, nil, nil)
		if node.PartVariances != &v32.PartVariances {
			t.Errorf("PartVariances mismatch")
		}
		for i := range 4 {
			if node.Split[i] != &v32.Split[i].PartVariances.None {
				t.Errorf("Split[%d] mismatch", i)
			}
		}
	})
	t.Run("v16x16", func(t *testing.T) {
		var v16 V16x16
		var node varianceNode
		treeToNode(&node, common.Block16x16, nil, nil, &v16, nil, nil)
		if node.PartVariances != &v16.PartVariances {
			t.Errorf("PartVariances mismatch")
		}
		for i := range 4 {
			if node.Split[i] != &v16.Split[i].PartVariances.None {
				t.Errorf("Split[%d] mismatch", i)
			}
		}
	})
	t.Run("v8x8", func(t *testing.T) {
		var v8 V8x8
		var node varianceNode
		treeToNode(&node, common.Block8x8, nil, nil, nil, &v8, nil)
		if node.PartVariances != &v8.PartVariances {
			t.Errorf("PartVariances mismatch")
		}
		for i := range 4 {
			if node.Split[i] != &v8.Split[i].PartVariances.None {
				t.Errorf("Split[%d] mismatch", i)
			}
		}
	})
	t.Run("v4x4_fallthrough", func(t *testing.T) {
		var v4 V4x4
		var node varianceNode
		treeToNode(&node, common.Block4x4, nil, nil, nil, nil, &v4)
		if node.PartVariances != &v4.PartVariances {
			t.Errorf("PartVariances mismatch")
		}
		for i := range 4 {
			if node.Split[i] != &v4.Split[i] {
				t.Errorf("Split[%d] mismatch (expected leaf Var addr)", i)
			}
		}
	})
}

// TestVP9FillVarianceTreeAggregates pins fill_variance_tree by manually
// computing the expected horz / vert / none aggregates. The variance
// tree is BLOCK_8X8 (so split[k] are four .none Vars on the v4x4
// children); we seed each child with distinct sum_square_error /
// sum_error and log2_count=4, then verify the aggregation matches the
// four sum_2_variances calls libvpx makes.
func TestVP9FillVarianceTreeAggregates(t *testing.T) {
	var v8 V8x8
	// Seed the four v4x4 children's part_variances.none.
	seed := [4]struct {
		sse uint32
		sum int32
	}{
		{100, 10},
		{200, -20},
		{50, 5},
		{75, 7},
	}
	const log2Count = 4
	for i := range 4 {
		v8.Split[i].PartVariances.None = varianceStat{
			SumSquareError: seed[i].sse,
			SumError:       seed[i].sum,
			Log2Count:      log2Count,
		}
	}
	fillVarianceTreeV8x8(&v8)

	// horz[0] = split[0] + split[1]
	horz0 := v8.PartVariances.Horz[0]
	if horz0.SumSquareError != seed[0].sse+seed[1].sse ||
		horz0.SumError != seed[0].sum+seed[1].sum ||
		horz0.Log2Count != log2Count+1 {
		t.Errorf("horz[0] = %+v, want sse=%d sum=%d log2=%d",
			horz0, seed[0].sse+seed[1].sse, seed[0].sum+seed[1].sum,
			log2Count+1)
	}
	// horz[1] = split[2] + split[3]
	horz1 := v8.PartVariances.Horz[1]
	if horz1.SumSquareError != seed[2].sse+seed[3].sse ||
		horz1.SumError != seed[2].sum+seed[3].sum ||
		horz1.Log2Count != log2Count+1 {
		t.Errorf("horz[1] = %+v", horz1)
	}
	// vert[0] = split[0] + split[2]
	vert0 := v8.PartVariances.Vert[0]
	if vert0.SumSquareError != seed[0].sse+seed[2].sse ||
		vert0.SumError != seed[0].sum+seed[2].sum ||
		vert0.Log2Count != log2Count+1 {
		t.Errorf("vert[0] = %+v", vert0)
	}
	// vert[1] = split[1] + split[3]
	vert1 := v8.PartVariances.Vert[1]
	if vert1.SumSquareError != seed[1].sse+seed[3].sse ||
		vert1.SumError != seed[1].sum+seed[3].sum ||
		vert1.Log2Count != log2Count+1 {
		t.Errorf("vert[1] = %+v", vert1)
	}
	// none = vert[0] + vert[1] = split[0]+split[1]+split[2]+split[3]
	none := v8.PartVariances.None
	totSse := seed[0].sse + seed[1].sse + seed[2].sse + seed[3].sse
	totSum := seed[0].sum + seed[1].sum + seed[2].sum + seed[3].sum
	if none.SumSquareError != totSse || none.SumError != totSum ||
		none.Log2Count != log2Count+2 {
		t.Errorf("none = %+v, want sse=%d sum=%d log2=%d",
			none, totSse, totSum, log2Count+2)
	}
}

// TestVP9Avg4x4Avg8x8 pins the avg helpers against hand-computed values
// (libvpx vpx_avg_4x4_c / vpx_avg_8x8_c, vpx_dsp/avg.c:17-35).
func TestVP9Avg4x4Avg8x8(t *testing.T) {
	t.Run("avg4x4_constant", func(t *testing.T) {
		buf := make([]uint8, 16)
		for i := range buf {
			buf[i] = 100
		}
		got := avg4x4(buf, 4)
		// sum = 16 * 100 = 1600; (1600+8)>>4 = 1608>>4 = 100.
		if got != 100 {
			t.Errorf("avg4x4 = %d, want 100", got)
		}
	})
	t.Run("avg8x8_constant", func(t *testing.T) {
		buf := make([]uint8, 64)
		for i := range buf {
			buf[i] = 200
		}
		got := avg8x8(buf, 8)
		// sum = 64*200 = 12800; (12800+32)>>6 = 12832>>6 = 200.
		if got != 200 {
			t.Errorf("avg8x8 = %d, want 200", got)
		}
	})
	t.Run("avg4x4_rounding", func(t *testing.T) {
		// One row of 0..3, sum=6 per row, 4 rows => sum=24. (24+8)>>4 = 2.
		buf := []uint8{
			0, 1, 2, 3,
			0, 1, 2, 3,
			0, 1, 2, 3,
			0, 1, 2, 3,
		}
		got := avg4x4(buf, 4)
		if got != 2 {
			t.Errorf("avg4x4 = %d, want 2", got)
		}
	})
	t.Run("avg8x8_rounding", func(t *testing.T) {
		// 0..7 in each row, sum=28 per row, 8 rows => sum=224.
		// (224+32)>>6 = 256>>6 = 4.
		buf := make([]uint8, 64)
		for r := range 8 {
			for c := range 8 {
				buf[r*8+c] = uint8(c)
			}
		}
		got := avg8x8(buf, 8)
		if got != 4 {
			t.Errorf("avg8x8 = %d, want 4", got)
		}
	})
}

func TestVP9AvgClampedReplicatesVisibleEdge(t *testing.T) {
	src := []uint8{10, 20, 30}
	if got := avg4x4Clamped(src, 1, 0, 0, 1, 3); got != 23 {
		t.Fatalf("avg4x4Clamped = %d, want 23", got)
	}
	if got := avg8x8Clamped(src, 1, 0, 0, 1, 3); got != 26 {
		t.Fatalf("avg8x8Clamped = %d, want 26", got)
	}
}

// TestVP9FillVariance4x4AvgKeyFrame pins fill_variance_4x4avg in its
// keyframe form (d_avg forced to 128). Source is a constant 200 plane,
// 8x8 region inside an 8x8 frame.
//
// For each of 4 sub-blocks: s_avg = 200, d_avg = 128 => diff = 72,
// sse = 72*72 = 5184, sum = 72.
func TestVP9FillVariance4x4AvgKeyFrame(t *testing.T) {
	src := make([]uint8, 64)
	for i := range src {
		src[i] = 200
	}
	var v8 V8x8
	fillVariance4x4Avg(src, 8, nil, 0, 0, 0, &v8, 8, 8, true)
	for k := range 4 {
		got := v8.Split[k].PartVariances.None
		if got.SumError != 72 {
			t.Errorf("split[%d].SumError = %d, want 72", k, got.SumError)
		}
		if got.SumSquareError != 5184 {
			t.Errorf("split[%d].SumSquareError = %d, want 5184", k,
				got.SumSquareError)
		}
		if got.Log2Count != 0 {
			t.Errorf("split[%d].Log2Count = %d, want 0", k, got.Log2Count)
		}
	}
}

// TestVP9FillVariance4x4AvgInter pins fill_variance_4x4avg for the inter
// case: distinct src / dst planes drive d_avg through vp9_avg_4x4 on the
// predictor. Both planes are constant => diff = src - dst.
func TestVP9FillVariance4x4AvgInter(t *testing.T) {
	src := make([]uint8, 64)
	dst := make([]uint8, 64)
	for i := range src {
		src[i] = 200
		dst[i] = 100
	}
	var v8 V8x8
	fillVariance4x4Avg(src, 8, dst, 8, 0, 0, &v8, 8, 8, false)
	for k := range 4 {
		got := v8.Split[k].PartVariances.None
		// s_avg = 200, d_avg = 100 => diff = 100, sse = 10000.
		if got.SumError != 100 {
			t.Errorf("split[%d].SumError = %d, want 100", k, got.SumError)
		}
		if got.SumSquareError != 10000 {
			t.Errorf("split[%d].SumSquareError = %d, want 10000", k,
				got.SumSquareError)
		}
	}
}

// TestVP9FillVariance4x4AvgClipToFrame pins the out-of-frame guard
// (pixels_wide/pixels_high bound). For k=1 and k=3 the x4_idx (= 4) is
// >= pixels_wide=4, so libvpx zeros (sse=0, sum=0).
func TestVP9FillVariance4x4AvgClipToFrame(t *testing.T) {
	src := make([]uint8, 64)
	for i := range src {
		src[i] = 200
	}
	var v8 V8x8
	// pixels_wide = 4 clips k=1 / k=3 (x4_idx = 4 >= 4).
	fillVariance4x4Avg(src, 8, nil, 0, 0, 0, &v8, 4, 8, true)
	// k=0: x4=0,y4=0 in-range, s_avg=200, d_avg=128 => diff=72.
	if v8.Split[0].PartVariances.None.SumError != 72 {
		t.Errorf("split[0] should be in-range")
	}
	// k=1: x4=4,y4=0 clipped.
	if v8.Split[1].PartVariances.None.SumError != 0 {
		t.Errorf("split[1] clipped: got %d, want 0",
			v8.Split[1].PartVariances.None.SumError)
	}
	if v8.Split[1].PartVariances.None.SumSquareError != 0 {
		t.Errorf("split[1] clipped sse: got %d, want 0",
			v8.Split[1].PartVariances.None.SumSquareError)
	}
	// k=2: x4=0,y4=4 in-range.
	if v8.Split[2].PartVariances.None.SumError != 72 {
		t.Errorf("split[2] should be in-range")
	}
	// k=3: x4=4,y4=4 clipped.
	if v8.Split[3].PartVariances.None.SumError != 0 {
		t.Errorf("split[3] clipped")
	}
}

func TestVP9FillVariance4x4AvgOddEdgeReplicates(t *testing.T) {
	src := []uint8{10, 20, 30}
	var v8 V8x8
	fillVariance4x4Avg(src, 1, nil, 0, 0, 0, &v8, 1, 3, true)

	got := v8.Split[0].PartVariances.None
	if got.SumError != -105 {
		t.Fatalf("split[0].SumError = %d, want -105", got.SumError)
	}
	if got.SumSquareError != 11025 {
		t.Fatalf("split[0].SumSquareError = %d, want 11025",
			got.SumSquareError)
	}
	if v8.Split[1].PartVariances.None.SumError != 0 ||
		v8.Split[2].PartVariances.None.SumError != 0 ||
		v8.Split[3].PartVariances.None.SumError != 0 {
		t.Fatalf("out-of-frame 4x4 splits should stay zero")
	}
}

// TestVP9FillVariance8x8AvgKeyFrame pins fill_variance_8x8avg for the
// keyframe form. Constant src=200, d=128 => diff=72, sse=5184 per
// 8x8 sub-block.
func TestVP9FillVariance8x8AvgKeyFrame(t *testing.T) {
	src := make([]uint8, 256) // 16x16
	for i := range src {
		src[i] = 200
	}
	var v16 V16x16
	fillVariance8x8Avg(src, 16, nil, 0, 0, 0, &v16, 16, 16, true)
	for k := range 4 {
		got := v16.Split[k].PartVariances.None
		if got.SumError != 72 {
			t.Errorf("split[%d].SumError = %d, want 72", k, got.SumError)
		}
		if got.SumSquareError != 5184 {
			t.Errorf("split[%d].SumSquareError = %d, want 5184", k,
				got.SumSquareError)
		}
	}
}

// TestVP9FillVariance8x8AvgClipToFrame pins the out-of-frame guard.
func TestVP9FillVariance8x8AvgClipToFrame(t *testing.T) {
	src := make([]uint8, 256)
	for i := range src {
		src[i] = 200
	}
	var v16 V16x16
	// pixels_wide = 8 clips k=1 (x8=8) and k=3 (x8=8).
	fillVariance8x8Avg(src, 16, nil, 0, 0, 0, &v16, 8, 16, true)
	if v16.Split[0].PartVariances.None.SumError != 72 {
		t.Errorf("split[0] in-range")
	}
	if v16.Split[1].PartVariances.None.SumError != 0 {
		t.Errorf("split[1] clipped")
	}
	if v16.Split[2].PartVariances.None.SumError != 72 {
		t.Errorf("split[2] in-range")
	}
	if v16.Split[3].PartVariances.None.SumError != 0 {
		t.Errorf("split[3] clipped")
	}
}

// TestVP9SetBlockSizeBounds pins set_block_size: writes only when
// mi_row < mi_rows && mi_col < mi_cols. Out-of-range writes are no-ops.
func TestVP9SetBlockSizeBounds(t *testing.T) {
	miRows, miCols := 4, 4
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	// In-range write at (1, 2).
	setBlockSize(grid, miRows, miCols, 1, 2, common.Block16x16)
	if grid[1*miCols+2].SbType != common.Block16x16 {
		t.Errorf("in-range write: got %d, want Block16x16",
			grid[1*miCols+2].SbType)
	}
	// Out-of-range write: mi_col = mi_cols.
	setBlockSize(grid, miRows, miCols, 1, miCols, common.Block32x32)
	for _, mi := range grid {
		if mi.SbType == common.Block32x32 {
			t.Errorf("out-of-range write should be a no-op")
		}
	}
	// Out-of-range write: mi_row = mi_rows.
	setBlockSize(grid, miRows, miCols, miRows, 0, common.Block32x32)
	for _, mi := range grid {
		if mi.SbType == common.Block32x32 {
			t.Errorf("out-of-range write should be a no-op")
		}
	}
	// Negative coordinates: ignored.
	setBlockSize(grid, miRows, miCols, -1, 0, common.Block32x32)
	setBlockSize(grid, miRows, miCols, 0, -1, common.Block32x32)
	for _, mi := range grid {
		if mi.SbType == common.Block32x32 {
			t.Errorf("negative coord write should be a no-op")
		}
	}
}

// TestVP9SetVTPartitioningForceSplit pins the force_split=1 short
// circuit (vp9/encoder/vp9_encodeframe.c:485): the function returns 0
// (false) immediately and writes nothing.
func TestVP9SetVTPartitioningForceSplit(t *testing.T) {
	miRows, miCols := 16, 16
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	var v16 V16x16
	args := setVTPartitioningArgs{V16: &v16}
	got := setVTPartitioning(grid, miRows, miCols, 0, 0,
		common.Block16x16, common.Block16x16, 1000, true, false, args, nil)
	if got {
		t.Errorf("force_split: return = true, want false")
	}
	for i, mi := range grid {
		if mi.SbType != common.Block4x4 {
			t.Errorf("force_split must not write: grid[%d] = %v",
				i, mi.SbType)
		}
	}
}

// TestVP9SetVTPartitioningBSizeMinLowVarianceWrites pins the bsize ==
// bsize_min branch with low variance: the block is claimed and bsize
// stamped at (mi_row, mi_col).
func TestVP9SetVTPartitioningBSizeMinLowVarianceWrites(t *testing.T) {
	miRows, miCols := 16, 16
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	var v16 V16x16
	// Pre-set the none.Variance lower than threshold (inter path skips
	// the get_variance recomputation when isKeyFrame=false, so we can
	// seed Variance directly).
	v16.PartVariances.None.Variance = 5
	args := setVTPartitioningArgs{V16: &v16}
	got := setVTPartitioning(grid, miRows, miCols, 0, 0,
		common.Block16x16, common.Block16x16, 100, false, false, args, nil)
	if !got {
		t.Errorf("low-variance bsize_min: return = false, want true")
	}
	if grid[0].SbType != common.Block16x16 {
		t.Errorf("grid[0].SbType = %v, want Block16x16", grid[0].SbType)
	}
}

// TestVP9SetVTPartitioningBSizeMinHighVarianceDoesNotWrite pins the
// bsize == bsize_min branch with high variance: variance >= threshold
// so the function returns 0 (caller should split).
func TestVP9SetVTPartitioningBSizeMinHighVarianceDoesNotWrite(t *testing.T) {
	miRows, miCols := 16, 16
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	var v16 V16x16
	v16.PartVariances.None.Variance = 9999
	args := setVTPartitioningArgs{V16: &v16}
	got := setVTPartitioning(grid, miRows, miCols, 0, 0,
		common.Block16x16, common.Block16x16, 100, false, false, args, nil)
	if got {
		t.Errorf("high-variance bsize_min: return = true, want false")
	}
	if grid[0].SbType != common.Block4x4 {
		t.Errorf("grid[0].SbType modified, want untouched (Block4x4 zero value)")
	}
}

// TestVP9SetVTPartitioningKeyFrameBSizeAbove32x32ForcesSplit pins the
// libvpx keyframe early-split path: bsize > BLOCK_32X32 OR
// variance > (threshold << 4) on a keyframe returns 0.
func TestVP9SetVTPartitioningKeyFrameBSizeAbove32x32ForcesSplit(t *testing.T) {
	miRows, miCols := 16, 16
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	var v64 V64x64
	// Seed enough child .none values so get_variance has well-defined
	// inputs (log2_count > 0 to avoid div-by-zero shift). The variance
	// outcome doesn't matter here — the bsize > BLOCK_32X32 predicate
	// kicks in first.
	for i := range 4 {
		v64.Split[i].PartVariances.None = varianceStat{Log2Count: 1}
	}
	args := setVTPartitioningArgs{V64: &v64}
	got := setVTPartitioning(grid, miRows, miCols, 0, 0,
		common.Block64x64, common.Block16x16, 100, false, true, args, nil)
	if got {
		t.Errorf("keyframe bsize=64x64: return = true, want false (force split)")
	}
}

// TestVP9SetVTPartitioningVertSplitClaimsBlock pins the vertical-split
// branch: vert[0] and vert[1] variances below threshold, chroma OK =>
// the function stamps the subsize at (mi_row, mi_col) and
// (mi_row, mi_col + bw/2).
func TestVP9SetVTPartitioningVertSplitClaimsBlock(t *testing.T) {
	miRows, miCols := 16, 16
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	var v32 V32x32
	// none variance must NOT be below threshold so we fall through to
	// vertical split.
	v32.PartVariances.None.Variance = 9999
	v32.PartVariances.Vert[0].Variance = 5
	v32.PartVariances.Vert[1].Variance = 5
	// Pre-seed the .none values; vert[0], vert[1] variances are
	// recomputed inside set_vt_partitioning via getVariance, so we
	// need their sum_square_error / sum_error / log2_count to round-
	// trip to a small variance. Easiest: set log2_count=1 and zeros.
	for i := range 2 {
		v32.PartVariances.Vert[i] = varianceStat{Log2Count: 1}
	}
	args := setVTPartitioningArgs{V32: &v32}
	got := setVTPartitioning(grid, miRows, miCols, 0, 0,
		common.Block32x32, common.Block16x16, 100, false, false, args, nil)
	if !got {
		t.Errorf("vert split (low var): return = false, want true")
	}
	// BLOCK_32X32 with PartitionVert => BLOCK_32X16.
	bw := int(common.Num8x8BlocksWideLookup[common.Block32x32])
	half := bw / 2
	want := common.SubsizeLookup[common.PartitionVert][common.Block32x32]
	if grid[0].SbType != want {
		t.Errorf("grid[0].SbType = %v, want %v", grid[0].SbType, want)
	}
	if grid[half].SbType != want {
		t.Errorf("grid[%d].SbType = %v, want %v",
			half, grid[half].SbType, want)
	}
}

// TestVP9SetVTPartitioningChromaPlaneBlocksBlocksVertSplit pins the
// chroma-plane OK gate: if the caller's chromaPlaneBlockOK reports the
// vertical subsize is invalid, the vert split is not taken (libvpx:
// get_plane_block_size(subsize, &xd->plane[1]) < BLOCK_INVALID).
func TestVP9SetVTPartitioningChromaPlaneBlocksBlocksVertSplit(t *testing.T) {
	miRows, miCols := 16, 16
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	var v32 V32x32
	v32.PartVariances.None.Variance = 9999
	for i := range 2 {
		v32.PartVariances.Vert[i] = varianceStat{Log2Count: 1}
		v32.PartVariances.Horz[i] = varianceStat{Log2Count: 1}
	}
	args := setVTPartitioningArgs{V32: &v32}
	got := setVTPartitioning(grid, miRows, miCols, 0, 0,
		common.Block32x32, common.Block16x16, 100, false, false, args,
		func(common.BlockSize) bool { return false }) // chroma always invalid
	if got {
		t.Errorf("chroma invalid: return = true, want false (fall through)")
	}
}
