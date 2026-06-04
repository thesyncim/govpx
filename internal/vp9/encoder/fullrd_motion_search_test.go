package encoder

import "testing"

// TestInitSearchRangeMatchesLibvpx pins vp9_init_search_range
// (vp9/encoder/vp9_mcomp.c:69-78) against hand-computed libvpx values.
//
//	sr = 0; size = max(16, size);
//	while ((size << sr) < MAX_FULL_PEL_VAL /*1023*/) sr++;
//	sr = min(sr, MAX_MVSEARCH_STEPS - 2 /*9*/);
func TestInitSearchRangeMatchesLibvpx(t *testing.T) {
	cases := []struct {
		size int
		want int
	}{
		// size clamped to 16: 16<<sr first >= 1023 at sr=6 (16<<6=1024).
		{0, 6},
		{16, 6},
		// 32<<sr >= 1023 at sr=5 (32<<5=1024).
		{32, 5},
		// 64<<5=2048 >= 1023 already at sr=4 (64<<4=1024).
		{64, 4},
		// 320<<sr: 320<<1=640<1023, 320<<2=1280>=1023 -> sr=2.
		{320, 2},
		// 176<<sr: 176<<2=704<1023, 176<<3=1408 -> sr=3.
		{176, 3},
		// 240<<sr: 240<<2=960<1023, 240<<3=1920 -> sr=3.
		{240, 3},
		// 1<<sr never needs >9 even for size=16 (capped at MAX_MVSEARCH_STEPS-2).
		{16384, 0},
	}
	for _, c := range cases {
		if got := InitSearchRange(c.size); got != c.want {
			t.Errorf("InitSearchRange(%d) = %d, want %d", c.size, got, c.want)
		}
	}
}

// TestFullRdStepParamNoRecodeRealtimeIsZero pins requirement (A): on the
// no-recode realtime path, set_mv_search_params never runs (vp9_encoder.c:4413
// vs DISALLOW_RECODE :5392), so cpi->mv_step_param stays 0, and full-RD
// single_motion_search (vp9_rdopt.c:2623) uses 0 — NOT the SF field (6) the
// NONRD path uses (vp9_pickmode.c:171).
func TestFullRdStepParamNoRecodeRealtimeIsZero(t *testing.T) {
	// On the no-recode RT path cpi->mv_step_param is the zero value 0; with
	// auto_mv_step_size off (or show_frame off) the resolver returns it as-is.
	const runtimeMvStepParam = 0
	const maxMvContext = 0

	// auto_mv_step_size == false: step_param = cpi->mv_step_param = 0.
	if got := FullRdSingleMotionStepParam(runtimeMvStepParam, maxMvContext,
		false, true); got != 0 {
		t.Fatalf("full-RD step_param (no-recode RT, auto off) = %d, want 0", got)
	}

	// auto_mv_step_size == true but !show_frame: still uses mv_step_param.
	if got := FullRdSingleMotionStepParam(runtimeMvStepParam, maxMvContext,
		true, false); got != 0 {
		t.Fatalf("full-RD step_param (auto on, !show_frame) = %d, want 0", got)
	}

	// auto on + show_frame + max_mv_context 0: (init_search_range(0)=6 + 0)/2 =
	// 3 — still NOT the NONRD SF value 6.
	if got := FullRdSingleMotionStepParam(runtimeMvStepParam, maxMvContext,
		true, true); got != 3 {
		t.Fatalf("full-RD step_param (auto on, show_frame, ctx 0) = %d, want 3",
			got)
	}
	if got := FullRdSingleMotionStepParam(runtimeMvStepParam, maxMvContext,
		true, true); got == 6 {
		t.Fatalf("full-RD step_param must not equal NONRD SF value 6")
	}
}

// TestMvSearchParamsRecodePath pins set_mv_search_params (vp9_encoder.c:
// 3728-3751): the value cpi->mv_step_param takes when the recode loop DOES run
// it. For a 320x176 frame max_mv_def = min(320,176) = 176, and
// vp9_init_search_range(176) = 3.
func TestMvSearchParamsRecodePath(t *testing.T) {
	// auto_mv_step_size off: mv_step_param = init_search_range(176) = 3.
	stepParam, maxMag := MvSearchParams(320, 176, false, false, true, 0)
	if stepParam != 3 {
		t.Fatalf("mv_step_param (320x176, auto off) = %d, want 3", stepParam)
	}
	if maxMag != 0 {
		t.Fatalf("max_mv_magnitude unchanged when auto off = %d, want 0", maxMag)
	}

	// auto on + intra-only: max_mv_magnitude := max_mv_def(176); step stays 3.
	stepParam, maxMag = MvSearchParams(320, 176, true, true, true, 0)
	if stepParam != 3 || maxMag != 176 {
		t.Fatalf("intra-only: step=%d max=%d, want 3/176", stepParam, maxMag)
	}

	// auto on + inter + show_frame: VPXMIN(176, 2*prevMax) with prevMax=176 =>
	// 176 -> init_search_range(176)=3; max reset to 0.
	stepParam, maxMag = MvSearchParams(320, 176, true, false, true, 176)
	if stepParam != 3 || maxMag != 0 {
		t.Fatalf("inter show: step=%d max=%d, want 3/0", stepParam, maxMag)
	}

	// auto on + inter + show_frame with small prevMax=4: VPXMIN(176, 8)=8 ->
	// init_search_range(8) clamps size to 16 -> 6.
	stepParam, maxMag = MvSearchParams(320, 176, true, false, true, 4)
	if stepParam != 6 || maxMag != 0 {
		t.Fatalf("inter show small: step=%d max=%d, want 6/0", stepParam, maxMag)
	}
}

// flatSadAtMinusVariance builds a paraboloid SAD/variance surface with a single
// global minimum at (targetRow, targetCol). SAD(r,c) and var(r,c) both grow
// monotonically with squared distance from the target, scaled large enough
// that the mvsad_err_cost / mv_err_cost additions never reorder neighbours
// closer to vs farther from the target. This lets the test assert the diamond
// converges to the known global minimum without hand-tracing each step.
func paraboloidSurfaces(targetRow, targetCol int) (
	func(r, c int) (uint64, bool),
	func(r, c int) uint64,
) {
	sadAt := func(r, c int) (uint64, bool) {
		dr := r - targetRow
		dc := c - targetCol
		// 1000*dist^2: dominates the ~hundreds-scale mvsad_err_cost so the
		// SAD ordering is by geometric distance to the target.
		return uint64(1000 * (dr*dr + dc*dc)), true
	}
	varAt := func(r, c int) uint64 {
		dr := r - targetRow
		dc := c - targetCol
		return uint64(1000 * (dr*dr + dc*dc))
	}
	return sadAt, varAt
}

// TestDiamondSearchSADConvergesToGlobalMinimum exercises requirement (B)'s
// inner kernel vp9_diamond_search_sad_c (vp9_mcomp.c:2055-2190) with
// step_param=0 (full 11-step coarse-to-fine walk), seed=[0,0], ref/center=[0,0],
// joint cost 600/300 via FullPelMVSADCost. On a convex paraboloid the diamond
// is guaranteed to reach the global minimum.
func TestDiamondSearchSADConvergesToGlobalMinimum(t *testing.T) {
	limits := &MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	const sadPerBit = 16
	targets := [][2]int{{0, 0}, {5, -3}, {-7, 6}, {12, 9}, {-20, -14}}
	for _, tg := range targets {
		sadAt, _ := paraboloidSurfaces(tg[0], tg[1])
		// libvpx: start_mv_sad = sad(mvp_full) + mvsad_err_cost(mvp_full,
		// ref_mv_full, sadpb). Seed mvp_full = [0,0], ref_mv_full = [0,0].
		startSad, _ := sadAt(0, 0)
		startSad += uint64(FullPelMVSADCost(0, 0, 0, 0, sadPerBit))

		res := DiamondSearchSAD(0, 0, startSad, 0, sadPerBit, 0, 0, limits, sadAt)
		if res.BestRow != tg[0] || res.BestCol != tg[1] {
			t.Errorf("DiamondSearchSAD target=%v got=(%d,%d)", tg,
				res.BestRow, res.BestCol)
		}
	}
}

// TestDiamondSearchSADNum00CenterStays pins the num00 accounting: when the
// global minimum is the seed itself (target=[0,0]), every step's best site is
// the centre (best stays at the start), so num00 increments each step.
//
// libvpx: best_site stays -1 (no improving site) so best_site==last_site (both
// -1) and best_address==in_what => (*num00)++ for all tot_steps=11 steps.
func TestDiamondSearchSADNum00CenterStays(t *testing.T) {
	limits := &MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	const sadPerBit = 16
	sadAt, _ := paraboloidSurfaces(0, 0)
	startSad, _ := sadAt(0, 0)
	startSad += uint64(FullPelMVSADCost(0, 0, 0, 0, sadPerBit))

	res := DiamondSearchSAD(0, 0, startSad, 0, sadPerBit, 0, 0, limits, sadAt)
	if res.BestRow != 0 || res.BestCol != 0 {
		t.Fatalf("center target: got (%d,%d), want (0,0)", res.BestRow, res.BestCol)
	}
	// tot_steps = total_steps - search_param = 11 - 0 = 11.
	if res.Num00 != 11 {
		t.Fatalf("num00 = %d, want 11 (one per step with no improving site)",
			res.Num00)
	}
}

// TestFullPixelDiamondVarianceRescoring exercises requirement (B)'s
// full_pixel_diamond (vp9_mcomp.c:2486-2605), which re-scores the diamond's MV
// with vp9_get_mvpred_var (variance, not SAD). With matching paraboloid SAD and
// variance surfaces the variance re-scoring confirms the same global minimum,
// and the final 1-away refining search keeps it.
func TestFullPixelDiamondVarianceRescoring(t *testing.T) {
	limits := &MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	const sadPerBit = 16
	// further_steps = MAX_MVSEARCH_STEPS - 1 - step_param = 10 for step_param=0.
	const stepParam = 0
	const furtherSteps = MaxMvSearchSteps - 1 - stepParam

	targets := [][2]int{{0, 0}, {5, -3}, {-7, 6}, {12, 9}}
	for _, tg := range targets {
		sadAt, varAt := paraboloidSurfaces(tg[0], tg[1])
		startSad, _ := sadAt(0, 0)
		startSad += uint64(FullPelMVSADCost(0, 0, 0, 0, sadPerBit))

		res := FullPixelDiamond(0, 0, startSad, stepParam, sadPerBit,
			furtherSteps, true, 0, 0, limits, sadAt, varAt)
		if res.BestRow != tg[0] || res.BestCol != tg[1] {
			t.Errorf("FullPixelDiamond target=%v got=(%d,%d)", tg,
				res.BestRow, res.BestCol)
		}
		// At the global minimum the variance is 0; the only additive term is
		// mv_err_cost(mv*8, ref_mv) which the closure folds into varAt. Here
		// varAt is pure variance (0 at target) so BestSme must be 0.
		wantSme := varAt(tg[0], tg[1])
		if res.BestSme != wantSme {
			t.Errorf("FullPixelDiamond target=%v BestSme=%d, want %d", tg,
				res.BestSme, wantSme)
		}
	}
}

// TestFullPixelDiamondPicksBySadScoresByVariance pins the SAD-vs-variance
// split: the diamond walk (and refining search) selects the MV by SAD, but the
// returned bestsme is that MV's vp9_get_mvpred_var VARIANCE score
// (vp9_mcomp.c:2536/2553/2572 -> 1454), not its SAD. We make the SAD minimum at
// the seed (0,0) so the diamond never moves, then give varAt a different value
// so the returned BestSme equals variance(0,0), proving the re-scoring uses the
// variance closure rather than the SAD one.
func TestFullPixelDiamondPicksBySadScoresByVariance(t *testing.T) {
	limits := &MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	const sadPerBit = 16
	const stepParam = 0
	const furtherSteps = MaxMvSearchSteps - 1 - stepParam

	// SAD minimum at the seed (0,0): the diamond and refining search never find
	// an improving site, so dst_mv stays (0,0).
	sadAt, _ := paraboloidSurfaces(0, 0)
	startSad, _ := sadAt(0, 0)
	startSad += uint64(FullPelMVSADCost(0, 0, 0, 0, sadPerBit))

	// Distinct variance surface whose value at (0,0) is a fixed sentinel that
	// is NOT derivable from the SAD surface (sad(0,0)=600*sadPerBit>>9, var
	// here is 4242). If full_pixel_diamond mistakenly returned the SAD-domain
	// score this would fail.
	const wantSme = 4242
	varAt := func(r, c int) uint64 {
		if r == 0 && c == 0 {
			return wantSme
		}
		return 1 << 30 // every other MV scores far worse.
	}

	res := FullPixelDiamond(0, 0, startSad, stepParam, sadPerBit, furtherSteps,
		true, 0, 0, limits, sadAt, varAt)
	if res.BestRow != 0 || res.BestCol != 0 {
		t.Fatalf("dst_mv = (%d,%d), want (0,0)", res.BestRow, res.BestCol)
	}
	if res.BestSme != wantSme {
		t.Fatalf("BestSme = %d, want %d (variance score, not SAD)",
			res.BestSme, wantSme)
	}
}

// TestDiamondSearchSADStepParamShiftsCoarseness pins the search_param
// indexing into ss_mv (vp9_mcomp.c:2082-2084): search_param selects the
// starting step block and tot_steps = total_steps - search_param. With a
// nearby target a coarser step_param still converges, but a finer (larger)
// step_param skips the large initial strides. We assert step_param=6 (the SF
// value the NONRD path would pass) still reaches a within-range close target,
// confirming the indexing is consistent for both the full-RD (0) and SF (6)
// step params.
func TestDiamondSearchSADStepParamConsistency(t *testing.T) {
	limits := &MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	const sadPerBit = 16
	// Target within reach of the finer step blocks (max stride at step block 6
	// is 1<<(11-1-6)=16).
	sadAt, _ := paraboloidSurfaces(3, -2)
	startSad, _ := sadAt(0, 0)
	startSad += uint64(FullPelMVSADCost(0, 0, 0, 0, sadPerBit))

	for _, sp := range []int{0, 6} {
		res := DiamondSearchSAD(0, 0, startSad, sp, sadPerBit, 0, 0, limits, sadAt)
		if res.BestRow != 3 || res.BestCol != -2 {
			t.Errorf("step_param=%d: got (%d,%d), want (3,-2)", sp,
				res.BestRow, res.BestCol)
		}
	}
}
