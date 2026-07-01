package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9SetRDSpeedThresholdsAdaptive verifies the thresh_mult table after
// vp9_set_rd_speed_thresholds with adaptive_rd_thresh != 0. Spot-checks the
// libvpx vp9_rd.c:703-744 column-by-column adjustments.
func TestVP9SetRDSpeedThresholdsAdaptive(t *testing.T) {
	var rd RDThreshState
	rd.SetRDSpeedThresholds(4, false)
	cases := []struct {
		mode ThrMode
		want int
		why  string
	}{
		{vp9ThrNearestMV, 300, "nearest LAST: adaptive=300"},
		{vp9ThrNearestG, 300, "nearest GOLDEN: adaptive=300"},
		{vp9ThrNearestA, 300, "nearest ALTREF: adaptive=300"},
		{vp9ThrDC, 1000, "DC +1000"},
		{vp9ThrNewMV, 1000, "NEW LAST +1000"},
		{vp9ThrNewA, 1000, "NEW ALTREF +1000"},
		{vp9ThrNewG, 1000, "NEW GOLDEN +1000"},
		{vp9ThrNearMV, 1000, "NEAR LAST +1000"},
		{vp9ThrNearA, 1000, "NEAR ALTREF +1000"},
		{vp9ThrTM, 1000, "TM +1000"},
		{vp9ThrCompNearestLA, 1000, "compound nearestLA +1000"},
		{vp9ThrCompNearestGA, 1000, "compound nearestGA +1000"},
		{vp9ThrCompNearLA, 1500, "compound nearLA +1500"},
		{vp9ThrCompNewLA, 2000, "compound newLA +2000"},
		{vp9ThrNearG, 1000, "NEAR GOLDEN +1000"},
		{vp9ThrCompNearGA, 1500, "compound nearGA +1500"},
		{vp9ThrCompNewGA, 2000, "compound newGA +2000"},
		{vp9ThrZeroMV, 2000, "ZERO LAST +2000"},
		{vp9ThrZeroG, 2000, "ZERO GOLDEN +2000"},
		{vp9ThrZeroA, 2000, "ZERO ALTREF +2000"},
		{vp9ThrCompZeroLA, 2500, "compound zeroLA +2500"},
		{vp9ThrCompZeroGA, 2500, "compound zeroGA +2500"},
		{vp9ThrHPred, 2000, "H_PRED +2000"},
		{vp9ThrVPred, 2000, "V_PRED +2000"},
		{vp9ThrD45Pred, 2500, "D45_PRED +2500"},
		{vp9ThrD135Pred, 2500, "D135_PRED +2500"},
		{vp9ThrD117Pred, 2500, "D117_PRED +2500"},
		{vp9ThrD153Pred, 2500, "D153_PRED +2500"},
		{vp9ThrD207Pred, 2500, "D207_PRED +2500"},
		{vp9ThrD63Pred, 2500, "D63_PRED +2500"},
	}
	for _, c := range cases {
		if got := rd.threshMult[c.mode]; got != c.want {
			t.Errorf("threshMult[%d] = %d, want %d (%s)", c.mode, got, c.want, c.why)
		}
	}
}

// TestVP9SetRDSpeedThresholdsNonAdaptive verifies the table when
// adaptive_rd_thresh == 0: NEARESTMV/G/A reset to 0, all other adjustments
// apply uniformly.
func TestVP9SetRDSpeedThresholdsNonAdaptive(t *testing.T) {
	var rd RDThreshState
	rd.SetRDSpeedThresholds(0, false)
	if got := rd.threshMult[vp9ThrNearestMV]; got != 0 {
		t.Errorf("threshMult[NEARESTMV] non-adaptive = %d, want 0", got)
	}
	if got := rd.threshMult[vp9ThrNearestG]; got != 0 {
		t.Errorf("threshMult[NEARESTG] non-adaptive = %d, want 0", got)
	}
	if got := rd.threshMult[vp9ThrNearestA]; got != 0 {
		t.Errorf("threshMult[NEARESTA] non-adaptive = %d, want 0", got)
	}
	if got := rd.threshMult[vp9ThrDC]; got != 1000 {
		t.Errorf("threshMult[DC] non-adaptive = %d, want 1000", got)
	}
}

func TestVP9SetRDSpeedThresholdsBestQuality(t *testing.T) {
	var rd RDThreshState
	rd.SetRDSpeedThresholds(4, true)
	cases := []struct {
		mode ThrMode
		want int
	}{
		{vp9ThrNearestMV, 300},
		{vp9ThrDC, 500},
		{vp9ThrNewMV, 500},
		{vp9ThrCompNearLA, 1000},
		{vp9ThrCompNewLA, 1500},
		{vp9ThrZeroMV, 1500},
		{vp9ThrCompZeroLA, 2000},
		{vp9ThrD45Pred, 2000},
	}
	for _, c := range cases {
		if got := rd.threshMult[c.mode]; got != c.want {
			t.Errorf("best-quality threshMult[%d] = %d, want %d",
				c.mode, got, c.want)
		}
	}
}

// TestVP9ModeIdxTableMatchesLibvpx verifies the mode_idx table layout
// (vp9_pickmode.c:1098-1103).
func TestVP9ModeIdxTableMatchesLibvpx(t *testing.T) {
	// INTRA_FRAME row.
	if ModeIdxTable[vp9dec.IntraFrame][0] != vp9ThrDC {
		t.Errorf("mode_idx[INTRA][DC] = %d, want THR_DC=%d",
			ModeIdxTable[vp9dec.IntraFrame][0], vp9ThrDC)
	}
	if ModeIdxTable[vp9dec.IntraFrame][1] != vp9ThrVPred {
		t.Errorf("mode_idx[INTRA][V] mismatch")
	}
	if ModeIdxTable[vp9dec.IntraFrame][2] != vp9ThrHPred {
		t.Errorf("mode_idx[INTRA][H] mismatch")
	}
	if ModeIdxTable[vp9dec.IntraFrame][3] != vp9ThrTM {
		t.Errorf("mode_idx[INTRA][TM] mismatch")
	}
	// LAST_FRAME row: NEAREST/NEAR/ZERO/NEW.
	want := [4]ThrMode{vp9ThrNearestMV, vp9ThrNearMV, vp9ThrZeroMV, vp9ThrNewMV}
	for i, w := range want {
		if got := ModeIdxTable[vp9dec.LastFrame][i]; got != w {
			t.Errorf("mode_idx[LAST][%d] = %d, want %d", i, got, w)
		}
	}
}

func TestVP9FullRDModeOrderMatchesLibvpx(t *testing.T) {
	cases := []struct {
		index ThrMode
		mode  common.PredictionMode
		ref   [2]int8
	}{
		{vp9ThrNearestMV, common.NearestMv, [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
		{vp9ThrNearestA, common.NearestMv, [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame}},
		{vp9ThrNearestG, common.NearestMv, [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame}},
		{vp9ThrDC, common.DcPred, [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
		{vp9ThrNewMV, common.NewMv, [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
		{vp9ThrNewA, common.NewMv, [2]int8{vp9dec.AltrefFrame, vp9dec.NoRefFrame}},
		{vp9ThrNewG, common.NewMv, [2]int8{vp9dec.GoldenFrame, vp9dec.NoRefFrame}},
		{vp9ThrNearMV, common.NearMv, [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
		{vp9ThrZeroMV, common.ZeroMv, [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame}},
		{vp9ThrCompNewLA, common.NewMv, [2]int8{vp9dec.LastFrame, vp9dec.AltrefFrame}},
		{vp9ThrD45Pred, common.D45Pred, [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame}},
	}
	for _, tc := range cases {
		got := FullRDModeOrder[tc.index]
		if got.Mode != tc.mode || got.RefFrame != tc.ref {
			t.Fatalf("FullRDModeOrder[%d] = {%d %v}, want {%d %v}",
				tc.index, got.Mode, got.RefFrame, tc.mode, tc.ref)
		}
		idx, ok := FullRDModeIndex(tc.mode, tc.ref[0], tc.ref[1])
		if !ok {
			t.Fatalf("FullRDModeIndex(%d,%v) not found", tc.mode, tc.ref)
		}
		if idx != tc.index {
			t.Fatalf("FullRDModeIndex(%d,%v) = %d, want %d",
				tc.mode, tc.ref, idx, tc.index)
		}
		if tc.ref[1] == vp9dec.NoRefFrame && tc.ref[0] > vp9dec.IntraFrame {
			singleIdx, ok := FullRDSingleModeIndex(tc.mode, tc.ref[0])
			if !ok {
				t.Fatalf("FullRDSingleModeIndex(%d,%d) not found", tc.mode, tc.ref[0])
			}
			if singleIdx != tc.index {
				t.Fatalf("FullRDSingleModeIndex(%d,%d) = %d, want %d",
					tc.mode, tc.ref[0], singleIdx, tc.index)
			}
		}
	}
}

func TestVP9FullRDCorrectNewMVMode(t *testing.T) {
	nearest := [2]vp9dec.MV{{Row: 8, Col: 16}, {Row: -8, Col: 4}}
	near := [2]vp9dec.MV{{Row: 12, Col: 20}, {Row: -4, Col: 2}}
	validBoth := [2]bool{true, true}
	if got := FullRDCorrectNewMVMode(common.NewMv,
		[2]vp9dec.MV{nearest[0]}, false, nearest, near,
		validBoth, validBoth); got != common.NearestMv {
		t.Fatalf("single NEWMV matching nearest corrected to %d, want NEARESTMV", got)
	}
	if got := FullRDCorrectNewMVMode(common.NewMv,
		[2]vp9dec.MV{near[0]}, false, nearest, near,
		validBoth, validBoth); got != common.NearMv {
		t.Fatalf("single NEWMV matching near corrected to %d, want NEARMV", got)
	}
	if got := FullRDCorrectNewMVMode(common.NewMv,
		[2]vp9dec.MV{}, false, nearest, near,
		validBoth, validBoth); got != common.ZeroMv {
		t.Fatalf("single zero NEWMV corrected to %d, want ZEROMV", got)
	}
	if got := FullRDCorrectNewMVMode(common.NewMv, nearest, true,
		nearest, near, validBoth, validBoth); got != common.NearestMv {
		t.Fatalf("compound NEWMV matching nearest corrected to %d, want NEARESTMV", got)
	}
	if got := FullRDCorrectNewMVMode(common.NewMv,
		[2]vp9dec.MV{nearest[0], near[1]}, true,
		nearest, near, validBoth, validBoth); got != common.NewMv {
		t.Fatalf("compound partial nearest match corrected to %d, want NEWMV", got)
	}
	if got := FullRDCorrectNewMVMode(common.NearestMv,
		[2]vp9dec.MV{near[0]}, false, nearest, near,
		validBoth, validBoth); got != common.NearestMv {
		t.Fatalf("non-NEWMV corrected to %d, want original mode", got)
	}
}

// TestVP9SetBlockThresholdsPopulatesGEBlock8x8 verifies set_block_thresholds
// fills the bsize>=BLOCK_8X8 rows with finite values; sub-8x8 rows stay zero
// (govpx does not surface the sub-8x8 picker).
func TestVP9SetBlockThresholdsPopulatesGEBlock8x8(t *testing.T) {
	var rd RDThreshState
	rd.SetRDSpeedThresholds(4, false)
	rd.SetBlockThresholds(64, 0)

	// Sub-8x8 rows stay zero because govpx does not run the sub-8x8 RD
	// picker here.
	for b := range common.Block8x8 {
		for i := range vp9MaxModes {
			if rd.threshes[b][i] != 0 {
				t.Errorf("threshes[%d][%d] sub-8x8 should be 0, got %d",
					b, i, rd.threshes[b][i])
			}
		}
	}
	// Block64x64 NEARESTMV must be positive (q*32*300/4 >> 0).
	if rd.threshes[common.Block64x64][vp9ThrNearestMV] <= 0 {
		t.Errorf("threshes[64x64][NEARESTMV] should be positive, got %d",
			rd.threshes[common.Block64x64][vp9ThrNearestMV])
	}
	// And it should scale with the block_size_factor (32 for 64x64 vs 8 for
	// 16x16 → roughly 4x).
	t16 := rd.threshes[common.Block16x16][vp9ThrNearestMV]
	t64 := rd.threshes[common.Block64x64][vp9ThrNearestMV]
	if t64 < 3*t16 {
		t.Errorf("Block64x64 threshold should be ≥3x Block16x16, got 64x=%d 16x=%d",
			t64, t16)
	}
}

func TestVP9FullRDModeThresholdZerosFrontSchedule(t *testing.T) {
	var rd RDThreshState
	rd.SetRDSpeedThresholds(4, false)
	rd.SetBlockThresholds(64, 0)
	rd.InitFreqFact()
	for mode := ThrMode(0); mode <= FullRDLastNewMVIndex; mode++ {
		if got := rd.FullRDModeRDThreshold(common.Block16x16, mode, false, false); got != 0 {
			t.Fatalf("full-RD threshold for front-schedule mode %d = %d, want 0", mode, got)
		}
	}
	near := rd.FullRDModeRDThreshold(common.Block16x16, vp9ThrNearMV, false, false)
	if near <= 0 {
		t.Fatalf("full-RD threshold for NEARMV = %d, want positive", near)
	}
	doubled := rd.FullRDModeRDThreshold(common.Block16x16, vp9ThrNearMV, true, true)
	if doubled != near<<1 {
		t.Fatalf("full-RD skippable scheduled threshold = %d, want %d", doubled, near<<1)
	}
	if !rd.FullRDModeSkipped(uint64(near-1), common.Block16x16,
		vp9ThrNearMV, false, false) {
		t.Fatalf("full-RD threshold gate did not skip below threshold")
	}
	if rd.FullRDModeSkipped(uint64(near), common.Block16x16,
		vp9ThrNearMV, false, false) {
		t.Fatalf("full-RD threshold gate skipped at threshold")
	}
	if !rd.FullRDModeSkipped(uint64((near<<1)-1), common.Block16x16,
		vp9ThrNearMV, true, true) {
		t.Fatalf("full-RD scheduled skippable gate did not use doubled threshold")
	}
}

// TestVP9RDLessThanThreshFires verifies the gate's fire condition.
func TestVP9RDLessThanThreshFires(t *testing.T) {
	// bestRd=10, thresh=100, fact=32 ⇒ rhs = 100*32>>5 = 100. 10<100 → true.
	if !RDLessThanThresh(10, 100, 32) {
		t.Error("expected fire when bestRd=10 < thresh*fact>>5=100")
	}
	// bestRd=1000 same params ⇒ 1000<100 false.
	if RDLessThanThresh(1000, 100, 32) {
		t.Error("expected no fire when bestRd=1000 >= 100")
	}
	// INT_MAX thresh always fires (libvpx vp9_rd.h:195).
	if !RDLessThanThresh(1<<62, 1<<31-1, 32) {
		t.Error("expected fire for INT_MAX thresh")
	}
}

func TestVP9UpdateFullRDThreshFactTouchesNeighborBlockSizes(t *testing.T) {
	var rd RDThreshState
	rd.InitFreqFact()
	rd.UpdateFullRDThreshFact(common.Block16x16, vp9ThrNewMV, 4)

	for bs := common.Block16x8; bs <= common.Block32x16; bs++ {
		if got := rd.threshFreqFact[bs][vp9ThrNewMV]; got != 30 {
			t.Fatalf("best-mode fact for block size %d = %d, want 30", bs, got)
		}
		if got := rd.threshFreqFact[bs][vp9ThrNearMV]; got != 33 {
			t.Fatalf("loser fact for block size %d = %d, want 33", bs, got)
		}
	}
	if got := rd.threshFreqFact[common.Block8x8][vp9ThrNewMV]; got != 32 {
		t.Fatalf("outside-neighborhood fact changed to %d, want 32", got)
	}
	if got := rd.threshFreqFact[common.Block32x32][vp9ThrNewMV]; got != 32 {
		t.Fatalf("outside-neighborhood fact changed to %d, want 32", got)
	}
}

func BenchmarkVP9UpdateFullRDThreshFact(b *testing.B) {
	var rd RDThreshState
	rd.InitFreqFact()
	modes := [...]ThrMode{vp9ThrNewMV, vp9ThrNearMV, vp9ThrZeroMV, vp9ThrCompNewLA}
	sizes := [...]common.BlockSize{common.Block8x8, common.Block16x16, common.Block32x32, common.Block64x64}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rd.UpdateFullRDThreshFact(sizes[i&3], modes[i&3], 4)
	}
}

// TestVP9UpdateThreshFreqFactBestMode verifies the best-mode branch reduces
// freq_fact by fact>>4 (libvpx vp9_pickmode.c:1154).
func TestVP9UpdateThreshFreqFactBestMode(t *testing.T) {
	var rd RDThreshState
	rd.InitFreqFact()
	// Pre: NEARESTMV fact = 32; update with this_mode == best_mode_idx.
	bestModeIdx := ModeIdxTable[vp9dec.LastFrame][0] // NEARESTMV slot.
	rd.UpdateThreshFreqFact(100, common.Block16x16,
		vp9dec.LastFrame, bestModeIdx, common.NearestMv, 0, 4)
	// fact -= fact>>4 → 32 - 2 = 30.
	if got := rd.threshFreqFact[common.Block16x16][bestModeIdx]; got != 30 {
		t.Errorf("freq_fact after best update = %d, want 30 (32 - 32>>4)", got)
	}
}

// TestVP9UpdateThreshFreqFactLoser verifies the non-best, non-NEWMV branch
// caps freq_fact at adaptive_rd_thresh * RD_THRESH_MAX_FACT (libvpx
// vp9_pickmode.c:1159-1162).
func TestVP9UpdateThreshFreqFactLoser(t *testing.T) {
	var rd RDThreshState
	rd.InitFreqFact()
	// adaptive_rd_thresh=4, cap=4*64=256. Pre: NEARMV fact=32. Update with
	// mode!=best (best is NEWMV, this is NEAR).
	bestModeIdx := ModeIdxTable[vp9dec.LastFrame][3] // NEWMV slot.
	rd.UpdateThreshFreqFact(100, common.Block16x16,
		vp9dec.LastFrame, bestModeIdx, common.NearMv, 0, 4)
	nearIdx := ModeIdxTable[vp9dec.LastFrame][1] // NEAR slot.
	if got := rd.threshFreqFact[common.Block16x16][nearIdx]; got != 33 {
		t.Errorf("freq_fact after loser update = %d, want 33 (32+1)", got)
	}
}
