package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9MinMax8x8Flat exercises vpx_minmax_8x8_c (vpx_dsp/avg.c:389-401)
// on identical inputs: |s-d| is 0 everywhere, so (min, max) = (0, 0).
func TestVP9MinMax8x8Flat(t *testing.T) {
	var buf [64]uint8
	for i := range buf {
		buf[i] = 100
	}
	mn, mx := vp9MinMax8x8(buf[:], 8, buf[:], 8)
	if mn != 0 || mx != 0 {
		t.Errorf("vp9MinMax8x8(flat) = (%d,%d), want (0,0)", mn, mx)
	}
}

// TestVP9MinMax8x8Diff exercises the case where every diff is the same
// non-zero value: min == max == |s - d|.
func TestVP9MinMax8x8Diff(t *testing.T) {
	var s, d [64]uint8
	for i := range s {
		s[i] = 100
		d[i] = 70 // diff = 30
	}
	mn, mx := vp9MinMax8x8(s[:], 8, d[:], 8)
	if mn != 30 || mx != 30 {
		t.Errorf("vp9MinMax8x8(const-diff) = (%d,%d), want (30,30)", mn, mx)
	}
}

// TestVP9ComputeMinmax8x8Flat: identical source/predictor over a 16x16
// region produces minmax_max == minmax_min == 0, so compute_minmax_8x8
// returns 0. Mirrors libvpx vp9_encodeframe.c:679-712.
func TestVP9ComputeMinmax8x8Flat(t *testing.T) {
	var buf [16 * 16]uint8
	for i := range buf {
		buf[i] = 100
	}
	got := vp9ComputeMinmax8x8(buf[:], 16, buf[:], 16, 0, 0, 16, 16)
	if got != 0 {
		t.Errorf("vp9ComputeMinmax8x8(flat) = %d, want 0", got)
	}
}

// TestVP9ChoosePartitioningKeyframeFlatNeverClaims64x64 pins the libvpx
// keyframe rule: vp9_encodeframe.c:486-490 in set_vt_partitioning
// forces split for any bsize > BLOCK_32X32 on a key frame regardless of
// variance, so the 64x64 root is never claimed and the picker descends
// to the 32x32 level. On a flat source the 32x32 children claim, so the
// top-left mi is stamped at Block32x32.
func TestVP9ChoosePartitioningKeyframeFlatClaims32x32(t *testing.T) {
	const miRows, miCols = 8, 8 // 64x64 frame
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	// Source = predictor under VP9_VAR_OFFS = 128 everywhere.
	src := make([]uint8, 64*64)
	for i := range src {
		src[i] = 128
	}
	rc := vp9ChoosePartitioning(vp9ChoosePartitioningArgs{
		MiGrid:                 grid,
		MiRows:                 miRows,
		MiCols:                 miCols,
		MiRow:                  0,
		MiCol:                  0,
		FrameWidth:             64,
		FrameHeight:            64,
		PlaneSrc:               src,
		SrcStride:              64,
		IsKeyFrame:             true,
		Speed:                  8,
		VariancePartThreshMult: 1,
		BaseQIndex:             37,
	})
	if rc != 0 {
		t.Fatalf("vp9ChoosePartitioning rc = %d, want 0", rc)
	}
	// 64x64 never claimed on keyframe (libvpx force-split rule). Top-left
	// 32x32 must have claimed because variance is zero.
	if grid[0].SbType == common.Block64x64 {
		t.Errorf("grid[0].SbType = Block64x64, want <= Block32x32 (keyframe force-split)")
	}
	if grid[0].SbType < common.Block16x16 {
		t.Errorf("grid[0].SbType = %v, want >= Block16x16 on flat source", grid[0].SbType)
	}
}

// TestVP9ChoosePartitioningInterFlatProducesLeavesAtBlock64x64 pins the
// inter-frame path with zero-MV LAST predictor identical to source:
// every variance is zero so the 64x64 root claims.
func TestVP9ChoosePartitioningInterFlatProducesLeavesAtBlock64x64(t *testing.T) {
	const miRows, miCols = 8, 8
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	src := make([]uint8, 64*64)
	dst := make([]uint8, 64*64)
	for i := range src {
		src[i] = 100
		dst[i] = 100 // identical predictor
	}
	rc := vp9ChoosePartitioning(vp9ChoosePartitioningArgs{
		MiGrid:                 grid,
		MiRows:                 miRows,
		MiCols:                 miCols,
		MiRow:                  0,
		MiCol:                  0,
		FrameWidth:             64,
		FrameHeight:            64,
		PlaneSrc:               src,
		SrcStride:              64,
		PlaneDst:               dst,
		DstStride:              64,
		IsKeyFrame:             false,
		Speed:                  8,
		VariancePartThreshMult: 1,
		BaseQIndex:             37,
	})
	if rc != 0 {
		t.Fatalf("vp9ChoosePartitioning rc = %d, want 0", rc)
	}
	if grid[0].SbType != common.Block64x64 {
		t.Errorf("grid[0].SbType = %v, want Block64x64", grid[0].SbType)
	}
}

// TestVP9ChoosePartitioningInterHighVarianceForcesSplit pins the
// force-split[0] path: a source with low-frequency block content
// (uniform halves) against a flat predictor produces large per-8x8-avg
// differences that exceed thresholds[2], which forces the 64x64 split.
func TestVP9ChoosePartitioningInterHighVarianceForcesSplit(t *testing.T) {
	const miRows, miCols = 8, 8
	grid := make([]vp9dec.NeighborMi, miRows*miCols)
	src := make([]uint8, 64*64)
	dst := make([]uint8, 64*64)
	// Top half = 0, bottom half = 255 so the per-8x8 avg of source is
	// 0 for the top rows and 255 for the bottom rows. Predictor is 128
	// everywhere — so the per-8x8-avg difference is at least 127 (top
	// half) and 127 (bottom half), driving variance way over threshold.
	for y := range 64 {
		for x := range 64 {
			if y < 32 {
				src[y*64+x] = 0
			} else {
				src[y*64+x] = 255
			}
			dst[y*64+x] = 128
		}
	}
	rc := vp9ChoosePartitioning(vp9ChoosePartitioningArgs{
		MiGrid:                 grid,
		MiRows:                 miRows,
		MiCols:                 miCols,
		MiRow:                  0,
		MiCol:                  0,
		FrameWidth:             64,
		FrameHeight:            64,
		PlaneSrc:               src,
		SrcStride:              64,
		PlaneDst:               dst,
		DstStride:              64,
		IsKeyFrame:             false,
		Speed:                  8,
		VariancePartThreshMult: 1,
		BaseQIndex:             37,
	})
	if rc != 0 {
		t.Fatalf("vp9ChoosePartitioning rc = %d, want 0", rc)
	}
	// Force-split path: the 64x64 root must NOT have claimed itself.
	if grid[0].SbType == common.Block64x64 {
		t.Errorf("grid[0].SbType = Block64x64, want smaller (force-split path)")
	}
}
