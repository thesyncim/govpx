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
// Flag default is OFF, so production and every other VP9 oracle gate are
// byte-identical (the seed stays in vp9LongFixtureParityGapSeeds); this test
// flips it locally. It is the progress anchor for closing the FIRST byte-exact
// full-RD inter frame: the prefix that matches grows as the remaining frontier
// (the handle_inter_mode model_rd_for_sb filter-loop ref_best_rd breakouts that
// prune NEAR/NEW before their genuine RD, vp9_rdopt.c:3155) lands.
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

	// Pin: the deep driver closes at least the leading z-order prefix of 46
	// leaves libvpx-exact (mode/ref/interp/mv). The milestone jumped 13 -> 46 by
	// disabling the coefficient trellis (vp9_optimize_b) in the genuine inter
	// super_block_yrd / super_block_uvrd producers for REALTIME speed >= 1
	// (cpu4): libvpx's do_trellis_opt returns 0 there because the speed feature
	// sets trellis_opt_tx_rd.method = DISABLE_TRELLIS_OPT (vp9_speed_features.c:
	// 485-488) and optimize_coefficients = 0 (vp9_speed_features.c:553). The
	// producers previously ran the trellis UNCONDITIONALLY (correct only for the
	// cpu0 {0,2,0,0,2} seed, where speed 0 keeps ENABLE_TRELLIS_OPT), which
	// wrongly zeroed AC coefficients libvpx keeps and flipped per-leaf decisions
	// deep in the SB. The first such flip was mi(2,3): with the trellis disabled
	// govpx's NEARMV (18,-6) yrd no longer undercuts NEWMV, so mi(2,3) commits
	// NEWMV (52,-58) EIGHTTAP exactly as libvpx, and the matched prefix advances
	// through it to mi(7,2). Asserting a floor (not the exact count) lets future
	// frontier work raise the prefix without editing this test, while catching a
	// regression below the milestone.
	//
	// First divergence after the milestone: mi(7,2) — the lone whole-8x8 INTRA
	// DC leaf. libvpx commits DC_PRED intra (mode=0, ref=INTRA, uv=DC, TX_4X4),
	// but govpx commits NEWMV (mode=13) ref=LAST EIGHTTAP_SMOOTH mv=(-26,60)
	// (libvpx ground truth, $TMPDIR vpxenc encode_b fprintf 2026-06-05). The
	// genuine intra-vs-inter RD for the inter frame's >=8x8 intra block is the
	// next frontier: the intra DC RD must beat every inter candidate here, which
	// requires the genuine intra-sb producer (vp9_fullrd_inter_intra_sb.go) to be
	// threaded into the >=8x8 leaf decision (it is verified standalone but not yet
	// the committed score for this path).
	const minPrefix = 46
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
	t.Logf("deep use-partition: %d/64 leading z-order leaves libvpx-exact "+
		"(mode/ref/interp/mv); first divergence at mi(%d,%d)",
		matched, firstDiff[0], firstDiff[1])
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
