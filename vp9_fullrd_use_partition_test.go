package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9UsePartitionSeed0_1_1_0_1Frame1 is the libvpx v1.16.0 ground-truth
// committed block decomposition of frame 1 (first inter frame), superblock 0,
// for the {0,1,1,0,1} long-fixture parity-gap seed (CBR 700 kbps kf=30 realtime
// cpu4, VAR_BASED_PARTITION, one-pass q=145). Captured 2026-06-05 from a private
// $TMPDIR-built libvpx vpxenc with an encode_b (vp9_encodeframe.c:2226) GTBLK
// fprintf probe; the produced two-frame IVF was md5-identical to the unmodified
// pinned vpxenc-vp9 oracle, proving the probe non-mutating. Single-motion-search
// inputs/outputs were cross-checked with a second probe in single_motion_search
// (vp9_rdopt.c:2563): mi(0,0) 8x8 NEWMV uses step_param=6, search_method=FAST_HEX
// and lands at full-pel+subpel (8,14).
//
// Every leaf is BLOCK_8X8 NONE; single-ref LAST; one whole-8x8 intra DC at
// mi(7,2); no compound; interp filters EIGHTTAP(0) + EIGHTTAP_SMOOTH(1) only
// (no SHARP). Fields: {MiRow, MiCol, Mode, Ref0, Interp, MvRow, MvCol}; Mode is
// PREDICTION_MODE (0=DC .. 10=NEARESTMV 11=NEARMV 13=NEWMV), Ref0 is
// ref_frame[0] (0=INTRA 1=LAST), Interp is interp_filter (0=EIGHTTAP
// 1=EIGHTTAP_SMOOTH; 3=SWITCHABLE default on the intra block). MV is in 1/8-pel.
var vp9UsePartitionSeed0_1_1_0_1Frame1 = [64][7]int{
	{0, 0, 13, 1, 0, 8, 14}, {0, 1, 10, 1, 1, 8, 14}, {0, 2, 13, 1, 1, -2, 30}, {0, 3, 13, 1, 1, -8, 40}, {0, 4, 11, 1, 1, -2, 30}, {0, 5, 13, 1, 0, 8, 10}, {0, 6, 10, 1, 0, 8, 10}, {0, 7, 13, 1, 1, 16, 10},
	{1, 0, 10, 1, 1, 8, 14}, {1, 1, 10, 1, 1, 8, 14}, {1, 2, 11, 1, 1, 8, 14}, {1, 3, 13, 1, 1, 54, -64}, {1, 4, 13, 1, 0, 10, 6}, {1, 5, 11, 1, 1, 10, 6}, {1, 6, 13, 1, 0, 8, 0}, {1, 7, 10, 1, 1, 16, 10},
	{2, 0, 10, 1, 0, 8, 14}, {2, 1, 10, 1, 1, 8, 14}, {2, 2, 13, 1, 0, 18, -6}, {2, 3, 13, 1, 0, 52, -58}, {2, 4, 10, 1, 1, 10, 6}, {2, 5, 10, 1, 1, 10, 6}, {2, 6, 10, 1, 0, 8, 0}, {2, 7, 10, 1, 1, 16, 10},
	{3, 0, 13, 1, 0, 0, 24}, {3, 1, 11, 1, 1, 0, 24}, {3, 2, 10, 1, 1, 18, -6}, {3, 3, 11, 1, 1, 18, -6}, {3, 4, 10, 1, 1, 10, 6}, {3, 5, 10, 1, 1, 10, 6}, {3, 6, 13, 1, 1, 2, 20}, {3, 7, 13, 1, 0, 10, 8},
	{4, 0, 13, 1, 0, -8, 32}, {4, 1, 10, 1, 0, 0, 24}, {4, 2, 13, 1, 0, 16, 8}, {4, 3, 13, 1, 1, 22, -6}, {4, 4, 13, 1, 1, 26, -2}, {4, 5, 10, 1, 1, 10, 6}, {4, 6, 13, 1, 1, 8, 0}, {4, 7, 13, 1, 0, 14, 6},
	{5, 0, 13, 1, 0, 12, 10}, {5, 1, 10, 1, 0, 0, 24}, {5, 2, 10, 1, 0, 16, 8}, {5, 3, 13, 1, 0, 4, 18}, {5, 4, 13, 1, 0, 8, 8}, {5, 5, 11, 1, 0, 8, 8}, {5, 6, 13, 1, 1, 0, 26}, {5, 7, 13, 1, 1, 18, 6},
	{6, 0, 10, 1, 1, 12, 10}, {6, 1, 10, 1, 1, 0, 24}, {6, 2, 10, 1, 0, 16, 8}, {6, 3, 10, 1, 1, 4, 18}, {6, 4, 11, 1, 1, 4, 18}, {6, 5, 11, 1, 1, 4, 18}, {6, 6, 11, 1, 1, 4, 18}, {6, 7, 13, 1, 0, 14, -2},
	{7, 0, 10, 1, 1, 12, 10}, {7, 1, 11, 1, 1, 12, 10}, {7, 2, 0, 0, 3, 0, 0}, {7, 3, 10, 1, 1, 4, 18}, {7, 4, 10, 1, 1, 4, 18}, {7, 5, 10, 1, 1, 4, 18}, {7, 6, 10, 1, 1, 4, 18}, {7, 7, 10, 1, 0, 14, -2},
}

// TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1 exercises the VAR_BASED_PARTITION
// full-RD inter driver (vp9InterUseDeepRDUsePartition) for the {0,1,1,0,1}
// frame-1 superblock and pins the prefix of 8x8 leaves that already commit the
// libvpx-exact (mode, ref, interp, mv). The headline assertion is that the deep
// path's libvpx-faithful single_motion_search (step_param via auto_mv_step_size
// + adaptive_motion_search boffset; FAST_HEX dispatch) plus the x->pred_mv[ref]
// candidate[2] threading produce mi(0,0) NEWMV mv=(8,14) — the exact libvpx
// full-pel+subpel result — instead of the model-RD (4,22).
//
// The VAR_BASED use-partition stack is production-default for this cpu4 lane;
// this test still sets the global guards explicitly so test order cannot
// quarantine it. With the genuine larger-block intra producer threaded in
// (pickVP9FullRDInterIntraLeaf), the entire 64-leaf SB committed (mode, ref,
// interp, mv) DECOMPOSITION now matches libvpx.
func TestVP9FullRDUsePartitionSeed0_1_1_0_1Frame1(t *testing.T) {
	const width, height = 64, 64

	saved := vp9InterUseDeepRDUsePartition
	savedRBR := vp9InterUseDeepRDRefBestRD
	defer func() {
		vp9InterUseDeepRDUsePartition = saved
		vp9InterUseDeepRDRefBestRD = savedRBR
	}()
	vp9InterUseDeepRDUsePartition = true
	// Thread the running best_rd as the genuine handle_inter_mode budget so the
	// mode-pre-filtering breakouts (super_block_yrd txfm-RD early-exit etc.)
	// prune NEW/NEAR exactly as libvpx — the mechanism that closes mi(1,1).
	vp9InterUseDeepRDRefBestRD = true

	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 30,
		Deadline:            DeadlineRealtime,
		CpuUsed:             4,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	sources := []*image.YCbCr{
		vp9test.NewPanningYCbCr(width, height, 0),
		vp9test.NewPanningYCbCr(width, height, 1),
	}
	dst := make([]byte, 1<<20)
	for i := 0; i < 2; i++ {
		if _, err := e.EncodeIntoWithResult(sources[i], dst); err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
	}

	// The committed mi grid (miRows*miCols, stride miCols) holds frame 1's blocks.
	const miCols = (width + 7) / 8
	if len(e.miGrid) < miCols*miCols {
		t.Fatalf("miGrid too small: %d", len(e.miGrid))
	}
	mode := func(r, c int) *vp9dec.NeighborMi { return &e.miGrid[r*miCols+c] }

	// Headline: mi(0,0) NEWMV must be the libvpx full-pel+subpel result (8,14).
	mi00 := mode(0, 0)
	if mi00.Mode != common.NewMv || mi00.RefFrame[0] != vp9dec.LastFrame ||
		mi00.Mv[0] != (vp9dec.MV{Row: 8, Col: 14}) {
		t.Fatalf("mi(0,0) = mode %d ref %d mv %v; want NEWMV LAST (8,14) "+
			"(libvpx single_motion_search FAST_HEX step_param=6)",
			mi00.Mode, mi00.RefFrame[0], mi00.Mv[0])
	}

	// Walk the SB in z-order (the rd_use_partition / writeVP9ModesSb recursion
	// order) and count the leading prefix whose committed (mode, ref, interp, mv)
	// is libvpx-exact. The count is pinned so a regression (or progress) trips
	// the test; the leading prefix is what the deep driver already closes.
	lut := map[[2]int][7]int{}
	for _, b := range vp9UsePartitionSeed0_1_1_0_1Frame1 {
		lut[[2]int{b[0], b[1]}] = b
	}
	zorder := vp9SB64ZOrder8x8()
	matched := 0
	var firstDiff [2]int
	firstDiffSet := false
	for _, pos := range zorder {
		r, c := pos[0], pos[1]
		want := lut[[2]int{r, c}]
		mi := mode(r, c)
		gotInterp := int(mi.InterpFilter)
		// Intra leaf: libvpx leaves interp at SWITCHABLE(3); govpx may park a
		// different don't-care there, so compare interp only on inter leaves.
		ok := int(mi.Mode) == want[2] && int(mi.RefFrame[0]) == want[3] &&
			int(mi.Mv[0].Row) == want[5] && int(mi.Mv[0].Col) == want[6]
		if want[3] == int(vp9dec.LastFrame) {
			ok = ok && gotInterp == want[4]
		}
		if ok {
			matched++
		} else {
			firstDiff = [2]int{r, c}
			firstDiffSet = true
			break
		}
	}

	// Pin: the deep driver closes the ENTIRE 64-leaf z-order SB libvpx-exact
	// (mode/ref/interp/mv) — frame-1 SB0's committed mode decomposition is FULLY
	// CLOSED (the residual coefficient bitstream is the next frontier). The prefix
	// climbed in stages: 13 -> 46 by
	// disabling the coefficient trellis (vp9_optimize_b) in the genuine inter
	// super_block_yrd / super_block_uvrd producers for REALTIME speed >= 1 (cpu4,
	// do_trellis_opt == 0 via DISABLE_TRELLIS_OPT + optimize_coefficients = 0,
	// vp9_speed_features.c:485-488,553), then 46 -> 64 by wiring the genuine
	// larger-block intra RD producer (vp9FullRDInterIntraSB, the
	// ref_frame==INTRA_FRAME arm of vp9_rd_pick_inter_mode_sb) into the >= 8x8
	// leaf score (pickVP9FullRDInterIntraLeaf), so the lone whole-8x8 INTRA leaf
	// at mi(7,2) commits DC_PRED intra (mode=0, ref=INTRA, uv=DC, TX_4X4, mv=0)
	// exactly as libvpx — its intra DC this_rd (42633654) beats NEARESTMV
	// (51074099), NEWMV(-26,60) (43705965) and NEARMV (44857337) on the identical
	// post-ref/post-skip-bit RD basis (libvpx ground truth, $TMPDIR vpxenc
	// rd_pick_inter_mode_sb intra-arm fprintf 2026-06-05: rate_y=98860 dist_y=77516
	// rate_uv=14341 dist_uv=7663 mbmode_cost=489 uv_mode_cost=560 rate2=116746
	// dist2=85179 ref_cost=2473 skip0=23). The committed intra mv is 0
	// (vp9_rdopt.c:3990, the full-RD intra arm, NOT the nonrd INVALID_MV at
	// vp9_pickmode.c:2645). Asserting a floor (not the exact count) lets future
	// frontier work (later SBs / later frames) extend without editing this test,
	// while catching a regression below the milestone.
	const minPrefix = 64
	if matched < minPrefix {
		fd := "none"
		if firstDiffSet {
			fd = string(rune('0'+firstDiff[0])) + "," + string(rune('0'+firstDiff[1]))
		}
		t.Fatalf("deep use-partition closed z-order prefix = %d (< %d); "+
			"first diverging leaf mi(%s). The genuine MV search / pred-mv "+
			"threading, the model_rd_for_sb + ref_best_rd breakout "+
			"(vp9InterUseDeepRDRefBestRD), or the do_trellis_opt gate "+
			"(vp9DoTrellisOptInterY) regressed.", matched, minPrefix, fd)
	}
	if matched >= 64 {
		t.Logf("deep use-partition: %d/64 z-order leaves libvpx-exact "+
			"(mode/ref/interp/mv) — frame-1 SB0 mode decomposition FULLY CLOSED",
			matched)
	} else {
		t.Logf("deep use-partition: %d/64 leading z-order leaves libvpx-exact "+
			"(mode/ref/interp/mv); first divergence at mi(%d,%d)",
			matched, firstDiff[0], firstDiff[1])
	}
}

func TestVP9FullRDUsePartitionProductionDefaultScope(t *testing.T) {
	const width, height = 64, 64
	cpu4, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MaxKeyframeInterval: 30,
		Deadline:            DeadlineRealtime,
		CpuUsed:             4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(cpu4): %v", err)
	}
	defer cpu4.Close()
	if !cpu4.vp9UseDeepRDUsePartitionPath() {
		t.Fatalf("cpu4 VAR_BASED use-partition deep RD disabled by default")
	}
	if !cpu4.vp9UseDeepRDRefBestPath() {
		t.Fatalf("cpu4 VAR_BASED ref-best RD budget disabled by default")
	}

	cpu0, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MaxKeyframeInterval: 999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder(cpu0): %v", err)
	}
	defer cpu0.Close()
	if cpu0.vp9UseDeepRDUsePartitionPath() {
		t.Fatalf("cpu0 SearchPartition unexpectedly enabled use-partition deep RD")
	}
	if cpu0.vp9UseDeepRDRefBestPath() {
		t.Fatalf("cpu0 SearchPartition unexpectedly enabled ref-best RD budget")
	}
}

// vp9SB64ZOrder8x8 returns the 64 (miRow, miCol) positions of a 64x64 superblock
// in the depth-first split recursion order (64->32->16->8) that both libvpx
// rd_use_partition and govpx writeVP9ModesSb walk.
func vp9SB64ZOrder8x8() [][2]int {
	var out [][2]int
	var rec func(r, c, size int)
	rec = func(r, c, size int) {
		if size == 1 {
			out = append(out, [2]int{r, c})
			return
		}
		h := size / 2
		rec(r, c, h)
		rec(r, c+h, h)
		rec(r+h, c, h)
		rec(r+h, c+h, h)
	}
	rec(0, 0, 8)
	return out
}
