package encoder

// fullrd_motion_search.go ports libvpx v1.16.0's full-RD full-pixel motion
// search building blocks used by vp9_rdopt.c::single_motion_search:
//
//   (A) the full-RD step_param resolver (vp9_rdopt.c:2613-2624 +
//       set_mv_search_params @ vp9_encoder.c:3728-3751 + vp9_init_search_range
//       @ vp9_mcomp.c:69-78), and
//
//   (B) the variance-rescoring diamond search: vp9_diamond_search_sad_c
//       (vp9_mcomp.c:2055-2190) driving full_pixel_diamond
//       (vp9_mcomp.c:2486-2605), which re-scores every candidate MV (and the
//       final) with vp9_get_mvpred_var (variance, not SAD; vp9_mcomp.c:1454).
//
// The 8-site-per-step search_site_config is the one built by
// vp9_init3smotion_compensation (vp9_mcomp.c:116-134), which is what
// cpi->ss_cfg holds on the RD path. total_steps = 11, searches_per_step = 8.

// ssStepCount is search_site_config.total_steps for the 8-site config:
//
//	cfg->total_steps = ss_count / cfg->searches_per_step;
//
// ss_count walks len = MAX_FIRST_STEP (=1024) down to 1 halving each time, so
// it iterates 11 times, 8 sites each => ss_count=88, total_steps=11.
// (vp9_mcomp.c:116-134, MAX_MVSEARCH_STEPS=11.)
const (
	ssSearchesPerStep = 8
	ssTotalSteps      = MaxMvSearchSteps // 11
)

// ssMV3 holds the precomputed ss_mv[] table from
// vp9_init3smotion_compensation (vp9_mcomp.c:116-134). For each step length
// len = 1024, 512, ..., 1 it stores 8 sites:
//
//	{ -len, 0 }, { len, 0 }, { 0, -len }, { 0, len },
//	{ -len, -len }, { -len, len }, { len, -len }, { len, len }
//
// laid out flat as ss_mv[step*8 + site]. This is the {row, col} motion-vector
// delta consumed by vp9_diamond_search_sad_c (the ss_os[] byte-offset table is
// just ss_mv[].row*stride + ss_mv[].col, which the closure-based port folds
// into the sadAt(row, col) callback).
var ssMV3 = func() [ssSearchesPerStep * ssTotalSteps]fullpelPatternCandidate {
	var ss [ssSearchesPerStep * ssTotalSteps]fullpelPatternCandidate
	// libvpx: for (len = MAX_FIRST_STEP; len > 0; len /= 2).
	// MAX_FIRST_STEP = 1 << (MAX_MVSEARCH_STEPS-1) = 1024.
	ssCount := 0
	for length := 1 << (MaxMvSearchSteps - 1); length > 0; length /= 2 {
		sites := [ssSearchesPerStep]fullpelPatternCandidate{
			{row: -length, col: 0},
			{row: length, col: 0},
			{row: 0, col: -length},
			{row: 0, col: length},
			{row: -length, col: -length},
			{row: -length, col: length},
			{row: length, col: -length},
			{row: length, col: length},
		}
		for i := 0; i < ssSearchesPerStep; i, ssCount = i+1, ssCount+1 {
			ss[ssCount] = sites[i]
		}
	}
	return ss
}()

// InitSearchRange ports vp9_init_search_range (vp9/encoder/vp9_mcomp.c:69-78).
//
//	int vp9_init_search_range(int size) {
//	  int sr = 0;
//	  size = VPXMAX(16, size);
//	  while ((size << sr) < MAX_FULL_PEL_VAL) sr++;
//	  sr = VPXMIN(sr, MAX_MVSEARCH_STEPS - 2);
//	  return sr;
//	}
func InitSearchRange(size int) int {
	sr := 0
	// libvpx: size = VPXMAX(16, size).
	if size < 16 {
		size = 16
	}
	// libvpx: while ((size << sr) < MAX_FULL_PEL_VAL) sr++.
	for (size << sr) < MaxFullPelVal {
		sr++
	}
	// libvpx: sr = VPXMIN(sr, MAX_MVSEARCH_STEPS - 2).
	if sr > MaxMvSearchSteps-2 {
		sr = MaxMvSearchSteps - 2
	}
	return sr
}

// MvSearchParams resolves cpi->mv_step_param for the full-RD path.
//
// libvpx set_mv_search_params (vp9_encoder.c:3728-3751):
//
//	const unsigned int max_mv_def = VPXMIN(cm->width, cm->height);
//	cpi->mv_step_param = vp9_init_search_range(max_mv_def);
//	if (cpi->sf.mv.auto_mv_step_size) {
//	  if (frame_is_intra_only(cm)) {
//	    cpi->max_mv_magnitude = max_mv_def;
//	  } else {
//	    if (cm->show_frame)
//	      cpi->mv_step_param = vp9_init_search_range(
//	          VPXMIN(max_mv_def, 2 * cpi->max_mv_magnitude));
//	    cpi->max_mv_magnitude = 0;
//	  }
//	}
//
// CRITICAL: set_mv_search_params runs only inside encode_with_recode_loop
// (vp9_encoder.c:4413). On the realtime no-recode path
// (sf.recode_loop == DISALLOW_RECODE, vp9_encoder.c:5392) it is never called,
// so cpi->mv_step_param keeps its zero-value 0 for full-RD
// single_motion_search. That is why full-RD single_motion_search
// (vp9_rdopt.c:2673 via step_param=cpi->mv_step_param at :2623) must use the
// runtime mv_step_param (0 on no-recode RT), NOT sf.mv.fullpel_search_step_param
// (which is NONRD-only, vp9_pickmode.c:171).
//
// MvSearchParams returns the value set_mv_search_params would compute when it
// does run (e.g. on a recode path). Callers on the no-recode RT path must use
// 0 (mvStepParam never updated). isIntraOnly mirrors frame_is_intra_only.
func MvSearchParams(width, height int, autoMvStepSize, isIntraOnly,
	showFrame bool, maxMvMagnitude int,
) (mvStepParam, newMaxMvMagnitude int) {
	// libvpx: max_mv_def = VPXMIN(cm->width, cm->height).
	maxMvDef := width
	if height < maxMvDef {
		maxMvDef = height
	}
	// libvpx: cpi->mv_step_param = vp9_init_search_range(max_mv_def).
	mvStepParam = InitSearchRange(maxMvDef)
	newMaxMvMagnitude = maxMvMagnitude

	if autoMvStepSize {
		if isIntraOnly {
			// libvpx: cpi->max_mv_magnitude = max_mv_def.
			newMaxMvMagnitude = maxMvDef
		} else {
			if showFrame {
				// libvpx: cpi->mv_step_param = vp9_init_search_range(
				//   VPXMIN(max_mv_def, 2 * cpi->max_mv_magnitude)).
				bound := maxMvDef
				if doubled := 2 * maxMvMagnitude; doubled < bound {
					bound = doubled
				}
				mvStepParam = InitSearchRange(bound)
			}
			// libvpx: cpi->max_mv_magnitude = 0.
			newMaxMvMagnitude = 0
		}
	}
	return mvStepParam, newMaxMvMagnitude
}

// FullRdSingleMotionStepParam ports the full-RD step_param computation at the
// head of single_motion_search (vp9_rdopt.c:2613-2624):
//
//	if (cpi->sf.mv.auto_mv_step_size && cm->show_frame) {
//	  step_param =
//	      (vp9_init_search_range(x->max_mv_context[ref]) + cpi->mv_step_param)
//	      / 2;
//	} else {
//	  step_param = cpi->mv_step_param;
//	}
//
// mvStepParam is the RUNTIME cpi->mv_step_param (0 on the no-recode RT path;
// see MvSearchParams). maxMvContext is x->max_mv_context[ref]
// (vp9_rd.c:618 max_mv tracker, surfaced through MvPredResult.MaxMvContext).
//
// This is intentionally distinct from sf.mv.fullpel_search_step_param, which
// the NONRD path passes (vp9_pickmode.c:171); full-RD must NOT use the SF
// field.
func FullRdSingleMotionStepParam(mvStepParam, maxMvContext int,
	autoMvStepSize, showFrame bool,
) int {
	if autoMvStepSize && showFrame {
		return (InitSearchRange(maxMvContext) + mvStepParam) / 2
	}
	return mvStepParam
}

// FullRdSingleMotionStepParamAdaptive layers libvpx single_motion_search's
// adaptive_motion_search step_param bump (vp9_rdopt.c:2626-2638) on top of
// FullRdSingleMotionStepParam:
//
//	if (cpi->sf.adaptive_motion_search && bsize < BLOCK_64X64) {
//	  const int boffset =
//	      2 * (b_width_log2_lookup[BLOCK_64X64] -
//	           VPXMIN(b_height_log2_lookup[bsize], b_width_log2_lookup[bsize]));
//	  step_param = VPXMAX(step_param, boffset);
//	}
//	if (cpi->sf.adaptive_motion_search) {
//	  int tlevel = x->pred_mv_sad[ref] >> (bwl + bhl + 4);
//	  if (tlevel < 5) step_param += 2;
//	}
//
// bWidthLog2_64x64 is b_width_log2_lookup[BLOCK_64X64] (== 4, in 4x4-block
// units). bWidthLog2Bsize / bHeightLog2Bsize are b_{width,height}_log2_lookup
// for the current bsize (also in 4x4-block units — the caller converts from
// govpx's pixel-log2 tables by subtracting 2). isBlock64 reports bsize ==
// BLOCK_64X64 (the bsize < BLOCK_64X64 guard). predMvSad is x->pred_mv_sad[ref];
// the tlevel shift is (bwl + bhl + 4) in those same 4x4-block-log2 units.
func FullRdSingleMotionStepParamAdaptive(stepParam int, adaptiveMotionSearch,
	isBlock64 bool, bWidthLog2_64x64, bWidthLog2Bsize, bHeightLog2Bsize,
	predMvSad int,
) int {
	if adaptiveMotionSearch && !isBlock64 {
		minLog2 := bHeightLog2Bsize
		if bWidthLog2Bsize < minLog2 {
			minLog2 = bWidthLog2Bsize
		}
		boffset := 2 * (bWidthLog2_64x64 - minLog2)
		if boffset > stepParam {
			stepParam = boffset
		}
	}
	if adaptiveMotionSearch {
		tlevel := predMvSad >> uint(bWidthLog2Bsize+bHeightLog2Bsize+4)
		if tlevel < 5 {
			stepParam += 2
		}
	}
	return stepParam
}

// DiamondSearchResult is the output of DiamondSearchSAD: the best full-pel MV
// (relative to the search-buffer origin, in the same {row, col} integer-pel
// units libvpx's MV uses), the best SAD (the bestsad return of
// vp9_diamond_search_sad_c), and num00 (the count of steps whose best site was
// the centre, consumed by full_pixel_diamond's do_refine / step-skip logic).
type DiamondSearchResult struct {
	BestRow int
	BestCol int
	BestSad uint64
	Num00   int
}

// DiamondSAD4Func compares four full-pel candidates in the same order libvpx's
// sdx4df path receives them.
type DiamondSAD4Func func(row0, col0, row1, col1, row2, col2, row3, col3 int) (sad0, sad1, sad2, sad3 uint64, ok bool)

// DiamondSearchSAD ports vp9_diamond_search_sad_c (vp9/encoder/vp9_mcomp.c:
// 2055-2190) over the 8-site search_site_config (ssMV3, total_steps=11).
//
// Inputs mirror the C signature:
//
//		int vp9_diamond_search_sad_c(const MACROBLOCK *x, const search_site_config
//		    *cfg, MV *ref_mv, uint32_t start_mv_sad, MV *best_mv, int search_param,
//		    int sad_per_bit, int *num00, const vp9_sad_fn_ptr_t *sad_fn_ptr,
//		    const MV *center_mv);
//
//	  - refRow/refCol: ref_mv (the search start point, mvp_full), in full-pel.
//	  - startMvSad: precomputed start_mv_sad (SAD at ref_mv + mvsad_err_cost).
//	  - searchParam: step_param.
//	  - sadPerBit: sad_per_bit.
//	  - centerRow/centerCol: center_mv in 1/8-pel (libvpx does >>3 internally).
//	  - limits: x->mv_limits, full-pel.
//	  - sadAt(row, col): fn_ptr SAD of the source block vs the reference block at
//	    full-pel offset (row, col) relative to the buffer origin (== ss_os[] +
//	    best_address dereference in C). ok=false signals an unreadable offset
//	    (the C code never reads out-of-buffer because the bounds tests gate it,
//	    so well-formed callers always return ok=true within limits).
//
// The closure folds the ss_os[] byte-offset arithmetic (ss_mv[].row*stride +
// ss_mv[].col added to best_address): the caller resolves (row, col) -> buffer
// address. all_in / is_mv_in bounds are evaluated on the MVs directly, exactly
// as the C does, so the sdx4df batched path and the per-point sdf path produce
// identical site selection.
func DiamondSearchSAD(refRow, refCol int, startMvSad uint64,
	searchParam, sadPerBit, centerRow, centerCol int, limits *MvLimits,
	sadAt func(row, col int) (uint64, bool),
) DiamondSearchResult {
	return DiamondSearchSADWithBatch(refRow, refCol, startMvSad, searchParam,
		sadPerBit, centerRow, centerCol, limits, sadAt, nil)
}

// DiamondSearchSADWithBatch is DiamondSearchSAD with an optional x4 SAD hook for
// the all-in candidate groups libvpx dispatches through sdx4df.
func DiamondSearchSADWithBatch(refRow, refCol int, startMvSad uint64,
	searchParam, sadPerBit, centerRow, centerCol int, limits *MvLimits,
	sadAt func(row, col int) (uint64, bool), sadAt4 DiamondSAD4Func,
) DiamondSearchResult {
	// libvpx: const MV *ss_mv = &cfg->ss_mv[search_param * searches_per_step].
	// const int tot_steps = cfg->total_steps - search_param.
	ssBase := searchParam * ssSearchesPerStep
	totSteps := ssTotalSteps - searchParam

	// libvpx: const MV fcenter_mv = { center_mv->row >> 3, center_mv->col >> 3 }.
	fcenterRow := centerRow >> 3
	fcenterCol := centerCol >> 3

	// libvpx: bestsad = start_mv_sad; best_site = -1; last_site = -1.
	bestSad := startMvSad
	bestSite := -1
	lastSite := -1
	var num00 int // libvpx: *num00 = 0.

	// libvpx: best_mv->row = ref_row; best_mv->col = ref_col; (the search
	// start, in full-pel). best_address = in_what (offset 0 relative to
	// best_mv). We track best as (bestRow, bestCol) and the "is best_address ==
	// in_what" centre test as (bestRow==refRow && bestCol==refCol).
	bestRow := refRow
	bestCol := refCol

	// mvsadErrCost ports mvsad_err_cost (vp9_encoder.c:1243-1251):
	//   ROUND_POWER_OF_TWO((unsigned)mv_cost(diff, jointsad, sadcost) *
	//       sad_per_bit, VP9_PROB_COST_SHIFT) — folded into FullPelMVSADCost.
	mvsadErrCost := func(row, col int) uint64 {
		return uint64(FullPelMVSADCost(row, col, fcenterRow, fcenterCol, sadPerBit))
	}

	// libvpx: i = 0; (flat site cursor within the current step block).
	i := 0
	// libvpx: for (step = 0; step < tot_steps; step++).
	for step := 0; step < totSteps; step++ {
		// libvpx: all_in is true if every checked point is within bounds.
		// The four representative sites tested are i, i+1, i+2, i+3 against
		// row_min/row_max/col_min/col_max with STRICT >/<.
		allIn := true
		s0 := ssMV3[ssBase+i]
		s1 := ssMV3[ssBase+i+1]
		s2 := ssMV3[ssBase+i+2]
		s3 := ssMV3[ssBase+i+3]
		if limits != nil {
			allIn = allIn && (bestRow+s0.row) > limits.RowMin
			allIn = allIn && (bestRow+s1.row) < limits.RowMax
			allIn = allIn && (bestCol+s2.col) > limits.ColMin
			allIn = allIn && (bestCol+s3.col) < limits.ColMax
		}

		if allIn {
			// libvpx: batched sdx4df path over searches_per_step in groups of
			// 4. Site selection is identical to the per-point path; the only
			// observable difference is none for a deterministic SAD source.
			for j := 0; j < ssSearchesPerStep; j += 4 {
				if sadAt4 != nil {
					site0 := ssMV3[ssBase+i]
					site1 := ssMV3[ssBase+i+1]
					site2 := ssMV3[ssBase+i+2]
					site3 := ssMV3[ssBase+i+3]
					rows := [4]int{
						bestRow + site0.row,
						bestRow + site1.row,
						bestRow + site2.row,
						bestRow + site3.row,
					}
					cols := [4]int{
						bestCol + site0.col,
						bestCol + site1.col,
						bestCol + site2.col,
						bestCol + site3.col,
					}
					sad0, sad1, sad2, sad3, ok := sadAt4(rows[0], cols[0],
						rows[1], cols[1], rows[2], cols[2], rows[3], cols[3])
					if ok {
						sad4 := [4]uint64{sad0, sad1, sad2, sad3}
						for t := 0; t < 4; t, i = t+1, i+1 {
							sad := sad4[t]
							if sad < bestSad {
								sad += mvsadErrCost(rows[t], cols[t])
								if sad < bestSad {
									bestSad = sad
									bestSite = i
								}
							}
						}
						continue
					}
				}
				for t := 0; t < 4; t, i = t+1, i+1 {
					site := ssMV3[ssBase+i]
					row := bestRow + site.row
					col := bestCol + site.col
					sad, ok := sadAt(row, col)
					if !ok {
						continue
					}
					// libvpx: if (sad_array[t] < bestsad) { sad +=
					//   mvsad_err_cost; if (sad < bestsad) { bestsad=sad;
					//   best_site=i; } }
					if sad < bestSad {
						sad += mvsadErrCost(row, col)
						if sad < bestSad {
							bestSad = sad
							bestSite = i
						}
					}
				}
			}
		} else {
			// libvpx: per-point sdf path with is_mv_in trap.
			for j := 0; j < ssSearchesPerStep; j++ {
				site := ssMV3[ssBase+i]
				row := bestRow + site.row
				col := bestCol + site.col
				if limits.InFullpelRange(row, col) {
					sad, ok := sadAt(row, col)
					if ok && sad < bestSad {
						sad += mvsadErrCost(row, col)
						if sad < bestSad {
							bestSad = sad
							bestSite = i
						}
					}
				}
				i++
			}
		}

		// libvpx: if (best_site != last_site) move best_mv to the site;
		// else if (best_address == in_what) (*num00)++.
		if bestSite != lastSite {
			site := ssMV3[ssBase+bestSite]
			bestRow += site.row
			bestCol += site.col
			lastSite = bestSite
		} else if bestRow == refRow && bestCol == refCol {
			num00++
		}
	}

	return DiamondSearchResult{
		BestRow: bestRow,
		BestCol: bestCol,
		BestSad: bestSad,
		Num00:   num00,
	}
}

// FullPixelDiamondResult is the output of FullPixelDiamond: the best full-pel
// MV (dst_mv) and bestsme (the variance-domain RD score returned by
// full_pixel_diamond).
type FullPixelDiamondResult struct {
	BestRow  int
	BestCol  int
	BestSme  uint64
	DoRefine bool
}

// FullPixelDiamond ports full_pixel_diamond (vp9/encoder/vp9_mcomp.c:
// 2486-2605) — the NSTEP/MESH motion search for RD. It runs a sequence of
// diamond searches in shrinking steps, re-scoring each candidate (and the
// final) with vp9_get_mvpred_var (variance, not SAD).
//
// Inputs mirror the C signature's load-bearing arguments:
//
//   - mvpRow/mvpCol: mvp_full (already clamped to mv_limits by the caller; the
//     C clamp_mv at :2504-2505 is applied by the caller before invoking).
//   - startMvSad: the precomputed start_mv_sad from full_pixel_diamond
//     (:2509-2515), including mvsad_err_cost.
//   - stepParam, sadPerBit, furtherSteps, doRefine: as in C
//     (further_steps = MAX_MVSEARCH_STEPS-1-step_param, do_refine=1 from
//     vp9_full_pixel_search).
//   - refRow/refCol: ref_mv in 1/8-pel (center_mv for the rescoring and the
//     diamond's mvsad_err_cost; the diamond does >>3 internally).
//   - sadAt(row, col): fn_ptr->sdf for the diamond, full-pel offset.
//   - varAt(row, col): vp9_get_mvpred_var(x, mv, ref_mv, fn_ptr, 1) — the
//     variance vfp->vf at full-pel offset (row, col) PLUS
//     mv_err_cost(mv*8, ref_mv). This is the closure form of vp9_mcomp.c:1454.
//   - limits: x->mv_limits, full-pel (also used by the refining search).
func FullPixelDiamond(mvpRow, mvpCol int, startMvSad uint64,
	stepParam, sadPerBit, furtherSteps int, doRefine bool,
	refRow, refCol int, limits *MvLimits,
	sadAt func(row, col int) (uint64, bool),
	varAt func(row, col int) uint64,
) FullPixelDiamondResult {
	return FullPixelDiamondWithBatch(mvpRow, mvpCol, startMvSad, stepParam,
		sadPerBit, furtherSteps, doRefine, refRow, refCol, limits, sadAt, nil,
		varAt)
}

// FullPixelDiamondWithBatch is FullPixelDiamond with an optional x4 SAD hook for
// the diamond search stages. Refinement remains scalar, matching the small
// four-neighbour loop shape in libvpx.
func FullPixelDiamondWithBatch(mvpRow, mvpCol int, startMvSad uint64,
	stepParam, sadPerBit, furtherSteps int, doRefine bool,
	refRow, refCol int, limits *MvLimits,
	sadAt func(row, col int) (uint64, bool), sadAt4 DiamondSAD4Func,
	varAt func(row, col int) uint64,
) FullPixelDiamondResult {
	// libvpx: bestsme = cpi->diamond_search_sad(x, ss_cfg, mvp_full,
	//   start_mv_sad, &temp_mv, step_param, sadpb, &n, &sad_fn_ptr, ref_mv).
	ds := DiamondSearchSADWithBatch(mvpRow, mvpCol, startMvSad, stepParam,
		sadPerBit, refRow, refCol, limits, sadAt, sadAt4)
	n := ds.Num00
	tempRow, tempCol := ds.BestRow, ds.BestCol

	// libvpx: if (bestsme < INT_MAX) bestsme = vp9_get_mvpred_var(x, &temp_mv,
	//   ref_mv, fn_ptr, 1).
	bestSme := varAt(tempRow, tempCol)
	// libvpx: *dst_mv = temp_mv.
	dstRow, dstCol := tempRow, tempCol

	// libvpx: if (n > further_steps) do_refine = 0.
	if n > furtherSteps {
		doRefine = false
	}

	// libvpx: while (n < further_steps) { ++n; if (num00) num00--; else { ... } }
	num00 := 0
	for n < furtherSteps {
		n++
		if num00 > 0 {
			num00--
			continue
		}
		// libvpx: thissme = cpi->diamond_search_sad(..., step_param + n, sadpb,
		//   &num00, ...). The C reuses start_mv_sad for every step.
		stepDs := DiamondSearchSADWithBatch(mvpRow, mvpCol, startMvSad,
			stepParam+n, sadPerBit, refRow, refCol, limits, sadAt, sadAt4)
		num00 = stepDs.Num00
		// libvpx: if (thissme < INT_MAX) thissme = vp9_get_mvpred_var(...).
		thisSme := varAt(stepDs.BestRow, stepDs.BestCol)

		// libvpx: if (num00 > further_steps - n) do_refine = 0.
		if num00 > furtherSteps-n {
			doRefine = false
		}
		// libvpx: if (thissme < bestsme) { bestsme = thissme; *dst_mv =
		//   temp_mv; }
		if thisSme < bestSme {
			bestSme = thisSme
			dstRow, dstCol = stepDs.BestRow, stepDs.BestCol
		}
	}

	// libvpx: final 1-away diamond refining search (:2570-2576).
	//   if (do_refine) {
	//     const int search_range = 8;
	//     MV best_mv = *dst_mv;
	//     thissme = vp9_refining_search_sad(x, &best_mv, sadpb, search_range,
	//                                       &sad_fn_ptr, ref_mv);
	//     if (thissme < INT_MAX)
	//       thissme = vp9_get_mvpred_var(x, &best_mv, ref_mv, fn_ptr, 1);
	//     if (thissme < bestsme) { bestsme = thissme; *dst_mv = best_mv; }
	//   }
	if doRefine {
		refRowOut, refColOut := refiningSearchSAD(dstRow, dstCol, sadPerBit,
			8, refRow, refCol, limits, sadAt)
		thisSme := varAt(refRowOut, refColOut)
		if thisSme < bestSme {
			bestSme = thisSme
			dstRow, dstCol = refRowOut, refColOut
		}
	}

	return FullPixelDiamondResult{
		BestRow:  dstRow,
		BestCol:  dstCol,
		BestSme:  bestSme,
		DoRefine: doRefine,
	}
}

// refiningSearchNeighbors is the 4-neighbour set from vp9_refining_search_sad
// (vp9_mcomp.c): { {-1,0}, {0,-1}, {0,1}, {1,0} } in {row, col}.
var refiningSearchNeighbors = [4]fullpelPatternCandidate{
	{row: -1, col: 0},
	{row: 0, col: -1},
	{row: 0, col: 1},
	{row: 1, col: 0},
}

// refiningSearchSAD ports vp9_refining_search_sad (vp9/encoder/vp9_mcomp.c:
// 1981-2053): a search_range-iteration 1-away refinement around best_mv that
// minimizes SAD + mvsad_err_cost, with the batched all-in optimization folded
// into the per-point form (identical site selection for a deterministic SAD).
func refiningSearchSAD(startRow, startCol, sadPerBit, searchRange,
	centerRow, centerCol int, limits *MvLimits,
	sadAt func(row, col int) (uint64, bool),
) (int, int) {
	fcenterRow := centerRow >> 3
	fcenterCol := centerCol >> 3
	bestRow, bestCol := startRow, startCol

	// libvpx: best_sad = fn_ptr->sdf(...) at best_full_mv +
	//   mvsad_err_cost(best_full_mv, fcenter_mv).
	bestSad, ok := sadAt(bestRow, bestCol)
	if !ok {
		return bestRow, bestCol
	}
	bestSad += uint64(FullPelMVSADCost(bestRow, bestCol, fcenterRow, fcenterCol, sadPerBit))

	// libvpx: for (i = 0; i < search_range; i++) { best_site = -1; ...
	//   for (j = 0; j < 4; j++) check neighbor; if best_site==-1 break; }
	for i := 0; i < searchRange; i++ {
		bestSite := -1
		for j := 0; j < 4; j++ {
			nb := refiningSearchNeighbors[j]
			row := bestRow + nb.row
			col := bestCol + nb.col
			if !limits.InFullpelRange(row, col) {
				continue
			}
			sad, sok := sadAt(row, col)
			if !sok || sad >= bestSad {
				continue
			}
			sad += uint64(FullPelMVSADCost(row, col, fcenterRow, fcenterCol, sadPerBit))
			if sad < bestSad {
				bestSad = sad
				bestSite = j
			}
		}
		if bestSite == -1 {
			break
		}
		nb := refiningSearchNeighbors[bestSite]
		bestRow += nb.row
		bestCol += nb.col
	}
	return bestRow, bestCol
}
