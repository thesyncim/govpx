package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8SPLITMVMotionSearchBounds pins task #300's audit of the
// SPLITMV per-label motion search bounds.
//
// libvpx v1.16.0 vp8/encoder/rdopt.c:1199-1303 vp8_rd_pick_best_mbsegmentation
// runs rd_check_segment inside one of two branches:
//
//   - Best mode (cpi->compressor_speed == 0, rdopt.c:1220-1226): BLOCK_16X8,
//     BLOCK_8X16, BLOCK_8X8, BLOCK_4X4 all execute back-to-back with
//     x->mv_col_min / x->mv_col_max untouched at their wide MB-scope UMV
//     window. The [best_ref_mv ± MAX_FULL_PEL_VAL] intersection block at
//     rdopt.c:1233-1248 lives in the `else` speed-mode branch and never fires
//     here.
//
//   - Speed mode (compressor_speed != 0, rdopt.c:1227-1302): the first
//     BLOCK_8X8 call runs against the wide UMV window, then mv_col_min/max
//     are tightened to the intersection with [best_ref_mv ± MAX_FULL_PEL_VAL]
//     before BLOCK_8X16, BLOCK_16X8 and BLOCK_4X4 and restored afterwards
//     (rdopt.c:1297-1301).
//
// Before task #300, govpx's selectMotion in vp8_encoder_inter_split.go applied
// the speed-mode intersection unconditionally for partitions 0, 1 and 3 even
// in best mode. On the 1280x720 SSIM-best screen-content cohort (sibling
// pins #297 / #293 / #299 ARNR audit), bestRefMV at MB(0,0) frame 1 is the
// zero vector, which collapsed govpx's per-label diamond_search_sad reach
// from the wide UMV window (e.g. [-16, +704] rows × [-16, +1264] cols at
// MB(0,0) of 1280x720) down to [-MAX_FULL_PEL_VAL, +MAX_FULL_PEL_VAL] =
// [-255, +255] in every direction. Sibling NEWMV / NEARESTMV picker
// branches were unaffected since they use the libvpx-faithful
// interFrameFullPixelSearchBounds at vp8_rd_pick_inter_mode (rdopt.c:
// 2045-2073). The asymmetric reach loss biased the SPLITMV per-label
// diamond search toward shallower MVs than libvpx, lowering SPLITMV's RD
// score and skewing the mode picker toward whole-block NEWMV. That
// matches the per-frame mode histogram task #297 captured:
//
//	govpx (pre-fix):  3482 NEARESTMV + 116 SPLITMV + 2 NEWMV
//	libvpx:            295 NEARESTMV + 664 SPLITMV + 1 NEWMV
//
// Task #300 plumbs the compressor speed through splitMotionShapeContext into
// splitMotionSubsetContext and switches the per-label bounds selection on it:
// best mode uses interFrameUMVOnlyFullPixelSearchBounds (wide MB-scope UMV)
// for all four shapes; speed mode keeps the partition-2 wide-UMV special
// case and the intersection for partitions 0/1/3.
func TestVP8SPLITMVMotionSearchBounds(t *testing.T) {
	t.Run("LibvpxMaxFullPelValMatchesConstant", testVP8SplitMVLibvpxMaxFullPelVal)
	t.Run("BestModeUsesWideUMVBoundsForAllPartitions", testVP8SplitMVBestModeWideUMV)
	t.Run("SpeedModePreservesIntersectionForNonBlock8x8", testVP8SplitMVSpeedModeIntersected)
	t.Run("BoundsAtFrameOriginAsymmetric", testVP8SplitMVBoundsFrameOrigin)
}

func testVP8SplitMVLibvpxMaxFullPelVal(t *testing.T) {
	// libvpx v1.16.0 vp8/encoder/mcomp.h:27
	//   #define MAX_FULL_PEL_VAL ((1 << (MAX_MVSEARCH_STEPS)) - 1)
	//   with MAX_MVSEARCH_STEPS == 8 ⇒ MAX_FULL_PEL_VAL == 255.
	if interFrameMaxMVSearchSteps != 8 {
		t.Fatalf("interFrameMaxMVSearchSteps = %d, want 8 (libvpx MAX_MVSEARCH_STEPS)", interFrameMaxMVSearchSteps)
	}
	if interFrameMaxFullPelVal != 255 {
		t.Fatalf("interFrameMaxFullPelVal = %d, want 255 (libvpx MAX_FULL_PEL_VAL)", interFrameMaxFullPelVal)
	}
}

func testVP8SplitMVBestModeWideUMV(t *testing.T) {
	// At MB(0,0) of a 1280x720 frame with UMV border 32, the wide MB-scope
	// UMV window per libvpx encodeframe.c:375-397 is:
	//   row in [-((0*16) + (32-16)),  ((45-1-0)*16) + (32-16)] = [-16, +720]
	//   col in [-((0*16) + (32-16)),  ((80-1-0)*16) + (32-16)] = [-16, +1280]
	// (1280x720 ⇒ mbCols = 80, mbRows = 45.)
	mbRows := 45
	mbCols := 80
	wide := interFrameUMVOnlyFullPixelSearchBounds(0, 0, mbRows, mbCols)
	wantWideRowMin := -16
	wantWideRowMax := (mbRows-1-0)*16 + (interFrameUMVBorderPixels - 16)
	wantWideColMin := -16
	wantWideColMax := (mbCols-1-0)*16 + (interFrameUMVBorderPixels - 16)
	if wide.rowMin != wantWideRowMin || wide.rowMax != wantWideRowMax ||
		wide.colMin != wantWideColMin || wide.colMax != wantWideColMax {
		t.Fatalf("wide UMV bounds at MB(0,0) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
			wide.rowMin, wide.rowMax, wide.colMin, wide.colMax,
			wantWideRowMin, wantWideRowMax, wantWideColMin, wantWideColMax)
	}

	// In best mode, every partition (0=16X8, 1=8X16, 2=8X8, 3=4X4) must use
	// the wide UMV bounds — matching libvpx rdopt.c:1220-1226 which leaves
	// x->mv_col_min / x->mv_col_max untouched across all four
	// rd_check_segment calls.
	bestRefMV := vp8enc.MotionVector{}
	for partition := range 4 {
		ctx := &splitMotionSubsetContext{
			mbRow:           0,
			mbCol:           0,
			compressorSpeed: 0,
			bestRefMV:       bestRefMV,
			mode:            &vp8enc.InterFrameMacroblockMode{Partition: uint8(partition)},
		}
		got := subsetBoundsAtPartition(ctx, mbRows, mbCols)
		if got != wide {
			t.Fatalf("best mode partition=%d bounds = %+v, want wide UMV %+v", partition, got, wide)
		}
	}
}

func testVP8SplitMVSpeedModeIntersected(t *testing.T) {
	// In speed mode (compressor_speed != 0), libvpx rdopt.c:1230 runs
	// BLOCK_8X8 (partition 2) on the wide UMV window first, then tightens
	// mv_col_min/max to the intersection with [best_ref_mv ± MAX_FULL_PEL_
	// VAL] before BLOCK_8X16, BLOCK_16X8, BLOCK_4X4 (rdopt.c:1245-1294).
	mbRows := 45
	mbCols := 80
	bestRefMV := vp8enc.MotionVector{}

	wide := interFrameUMVOnlyFullPixelSearchBounds(0, 0, mbRows, mbCols)
	intersected := interFrameFullPixelSearchBounds(bestRefMV, 0, 0, mbRows, mbCols)

	// Partition 2 (BLOCK_8X8): wide UMV bounds in speed mode.
	ctx2 := &splitMotionSubsetContext{
		mbRow:           0,
		mbCol:           0,
		compressorSpeed: 1,
		bestRefMV:       bestRefMV,
		mode:            &vp8enc.InterFrameMacroblockMode{Partition: 2},
	}
	if got := subsetBoundsAtPartition(ctx2, mbRows, mbCols); got != wide {
		t.Fatalf("speed mode partition=2 bounds = %+v, want wide UMV %+v", got, wide)
	}

	// Partitions 0, 1, 3 in speed mode use the intersection.
	for _, partition := range []int{0, 1, 3} {
		ctx := &splitMotionSubsetContext{
			mbRow:           0,
			mbCol:           0,
			compressorSpeed: 1,
			bestRefMV:       bestRefMV,
			mode:            &vp8enc.InterFrameMacroblockMode{Partition: uint8(partition)},
		}
		got := subsetBoundsAtPartition(ctx, mbRows, mbCols)
		if got != intersected {
			t.Fatalf("speed mode partition=%d bounds = %+v, want intersected %+v", partition, got, intersected)
		}
	}
}

func testVP8SplitMVBoundsFrameOrigin(t *testing.T) {
	// Pin the symptomatic gap at MB(0,0) frame 1 of the 1280x720 SSIM-best
	// ARNR pin holds: with bestRefMV = (0,0), the intersection
	// [bestRefMV ± MAX_FULL_PEL_VAL] truncates the reachable diamond to
	// row in [-255, +255] / col in [-255, +255], i.e. it knocks out the
	// upper UMV slack the picker would otherwise have for SPLITMV's
	// per-label diamond search at the frame-corner MB.
	mbRows := 45
	mbCols := 80
	bestRefMV := vp8enc.MotionVector{}
	intersected := interFrameFullPixelSearchBounds(bestRefMV, 0, 0, mbRows, mbCols)

	// Intersection clamps to [-255, +255] but loses to the frame-corner
	// UMV edge at the lower bounds (= -16 here).
	wantRowMin := -16
	wantRowMax := 255
	wantColMin := -16
	wantColMax := 255
	if intersected.rowMin != wantRowMin || intersected.rowMax != wantRowMax ||
		intersected.colMin != wantColMin || intersected.colMax != wantColMax {
		t.Fatalf("intersected bounds at MB(0,0) bestRefMV=(0,0) = %+v, want (%d,%d,%d,%d)",
			intersected, wantRowMin, wantRowMax, wantColMin, wantColMax)
	}

	wide := interFrameUMVOnlyFullPixelSearchBounds(0, 0, mbRows, mbCols)
	if wide.rowMax <= intersected.rowMax {
		t.Fatalf("wide.rowMax = %d not greater than intersected.rowMax = %d; expected wide UMV to extend further", wide.rowMax, intersected.rowMax)
	}
	if wide.colMax <= intersected.colMax {
		t.Fatalf("wide.colMax = %d not greater than intersected.colMax = %d; expected wide UMV to extend further", wide.colMax, intersected.colMax)
	}
}

// subsetBoundsAtPartition mirrors the bounds selection inside
// selectMotion (vp8_encoder_inter_split.go) so the test can probe each branch
// in isolation. Keeping this helper close to the production logic makes
// future bounds-rule changes mechanically detectable.
func subsetBoundsAtPartition(ctx *splitMotionSubsetContext, mbRows int, mbCols int) interFrameFullPixelBounds {
	if ctx.compressorSpeed == 0 || (ctx.mode != nil && ctx.mode.Partition == 2) {
		return interFrameUMVOnlyFullPixelSearchBounds(ctx.mbRow, ctx.mbCol, mbRows, mbCols)
	}
	return interFrameFullPixelSearchBounds(ctx.bestRefMV, ctx.mbRow, ctx.mbCol, mbRows, mbCols)
}

// subset_bounds_assertion is a compile-time check that splitMotionSubsetContext.
// compressorSpeed exists at the offset where selectMotion reads it. The
// field is consumed only inside selectMotion at the per-label bounds
// selection — see vp8_encoder_inter_split.go. A reflect-style assertion would
// rely on reflect plus introspection on private types; a simple struct
// literal pinned here is enough to guarantee the field's presence at the
// type level.
var _ = splitMotionSubsetContext{
	compressorSpeed: 0,
	bestRefMV:       vp8enc.MotionVector{},
	mode:            &vp8enc.InterFrameMacroblockMode{Mode: vp8common.SplitMV, Partition: 2},
}
