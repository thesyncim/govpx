package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_sub8x8_wrapper.go ports vp9_rd_pick_inter_mode_sub8x8
// (vp9/encoder/vp9_rdopt.c:4294-4930): the driver that runs the genuine
// rd_pick_best_sub8x8_mode producer over the usable reference frames + the
// switchable interp filters, adds the sub-8x8 UV-RD (super_block_uvrd on
// BLOCK_8X8) + the ref-frame signalling cost + the skip-vs-noskip pick, and
// returns the 8x8 block's committed rate/dist/this_rd + the per-sub-block bmi
// quartet for the bitstream writer.
//
// SCOPE: this is the single-reference inter path (the path the frame-1
// realtime cpu0 SB0 sub-8x8 children exercise for ref=LAST) plus libvpx's
// INTRA_FRAME arm. Compound prediction (joint_motion_search) is not yet ported
// here. cm->interp_filter == SWITCHABLE on this path, so each of the three
// switchable filters (EIGHTTAP, EIGHTTAP_SMOOTH, EIGHTTAP_SHARP) is evaluated
// and the best by tmp_rd (segment_rd + RDCOST(switchable_rate,0)) selected
// (vp9_rdopt.c:4569-4625).
//
// GATED behind vp9InterUseDeepRDSub8x8 (and the deep partition flag). Production
// (flag off) keeps the pickVP9Sub8InterMode model stand-in.

// vp9Sub8x8WrapperResult is vp9_rd_pick_inter_mode_sub8x8's output for one 8x8
// block: the committed decision + the RD totals the partition recursion compares.
type vp9Sub8x8WrapperResult struct {
	bmi          [4]vp9dec.Bmi
	mode         common.PredictionMode // mi->mode = bmi[3].as_mode
	mv           [2]vp9dec.MV          // mi->mv[0] = bmi[3].as_mv[0]
	refFrame     int8
	interpFilter vp9dec.InterpFilter
	uvMode       common.PredictionMode
	rate         int    // rd_cost->rate (rate2 after the skip pick)
	distortion   uint64 // rd_cost->dist
	thisRD       uint64 // rd_cost->rdcost
	rateY        int
	rateUV       int
	distUV       uint64
	skippable    bool
	skip2        bool
	// intra is set when the committed decision is the INTRA_FRAME sub-8x8 mode
	// (vp9_ref_order ref_index 5). When true bmi[].as_mode holds the per-sub-block
	// intra Y mode, mode = bmi[3].as_mode, refFrame = INTRA_FRAME, interpFilter =
	// SWITCHABLE_FILTERS, mv = 0 (vp9_rdopt.c:4759-4766).
	intra bool
	// segEntropy is the committed segment's plane[0] above/left entropy context
	// after all sub-blocks are coded; the partition recursion stamps it into
	// pd->above_context/left_context so the next sibling 8x8 reads it
	// (vp9_encodeframe.c encode_sb / save_context-restore_context). For the intra
	// commit it is the post-coding plane[0] context from rd_pick_intra4x4block.
	segEntropy vp9Sub8x8SegmentEntropy
	valid      bool
}

type vp9Sub8x8InterCapture struct {
	MiRow        int
	MiCol        int
	Bsize        common.BlockSize
	Mode         common.PredictionMode
	RefFrame     int8
	InterpFilter vp9dec.InterpFilter
	Bmi          [4]vp9dec.Bmi
	Rate         int
	RateY        int
	RateUV       int
	Distortion   uint64
	DistUV       uint64
	ThisRD       uint64
	Skip2        bool
}

// rdPickInterModeSub8x8 ports vp9_rd_pick_inter_mode_sub8x8 for the single-ref
// inter path. bsize is the sub-8x8 partition shape (BLOCK_4X4/8X4/4X8); the
// block footprint is always the 8x8 at (miRow, miCol). best_rd_so_far gates the
// segment + UV early-exits.
func (e *VP9Encoder) rdPickInterModeSub8x8(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, bestRDSoFar uint64, bestRDInf bool,
) (vp9Sub8x8WrapperResult, bool) {
	if inter == nil || inter.dq == nil || inter.ref == nil ||
		bsize >= common.Block8x8 {
		return vp9Sub8x8WrapperResult{}, false
	}

	var left *vp9dec.NeighborMi
	if miCol > tile.MiColStart {
		left = e.vp9MiAt(miRows, miCols, miRow, miCol-1)
	}
	above := e.vp9MiAt(miRows, miCols, miRow-1, miCol)
	switchableCtx := vp9dec.GetPredContextSwitchableInterp(above, left)
	rdmult := e.cbRdmult
	if rdmult <= 0 {
		rdmult = e.rc.rdmult
	}
	if rdmult <= 0 {
		rdmult = 1
	}
	rddiv := encoder.RDDivBits
	interModeMask := e.vp9InterModeMaskFor(bsize)

	// Switchable filters evaluated (cm->interp_filter == SWITCHABLE):
	// EIGHTTAP, EIGHTTAP_SMOOTH, EIGHTTAP_SHARP (vp9_rdopt.c:4569).
	filters := [...]vp9dec.InterpFilter{
		vp9dec.InterpEighttap,
		vp9dec.InterpEighttapSmooth,
		vp9dec.InterpEighttapSharp,
	}
	switchable := vp9InterFrameInterpFilter(inter) == vp9dec.InterpSwitchable

	bestRD := bestRDSoFar
	bestRDValid := !bestRDInf
	// best_yrd = best_rd - RDCOST(rate_uv, distortion_uv); seeded to best_rd.
	bestYRD := bestRDSoFar
	bestYRDInf := bestRDInf

	var bestRes vp9Sub8x8WrapperResult
	bestSet := false

	// seg_mvs[4][MAX_REF_FRAMES] NEWMV cache (libvpx vp9_rdopt.c:4327, init to
	// INVALID_MV once at :4343-4346 before the ref + filter loops). Shared across
	// every ref-frame iteration AND every switchable filter so each per-sub-block
	// NEWMV search runs once per (block, ref). Declared here (function scope) to
	// match libvpx exactly.
	var segMvCache vp9Sub8x8SegMvCache
	// libvpx disables the sub-8x8 ref-index threshold gate only for two-pass
	// internal formatting-bar edges (vp9_rdopt.c:4333-4334). govpx's current
	// full-RD oracle path is one-pass and has no inactive-zone model, so this is
	// false until that source state is ported.
	internalActiveEdge := false
	sub8x8RefSkipped := func(refIndex int) bool {
		return !internalActiveEdge &&
			e.rdThresh.Sub8x8RefSkipped(bestRD, bsize, refIndex)
	}

	// Reference-frame loop. Frame-1 realtime cpu0 single-ref: only the refs in
	// inter.refMask are usable (LAST on the steady inter frame).
	refFramesAll := [...]struct {
		refFrame int8
		refIndex int
	}{
		{vp9dec.LastFrame, sfThrLast},
		{vp9dec.GoldenFrame, sfThrGold},
		{vp9dec.AltrefFrame, sfThrAltr},
	}
	savedRef := inter.ref
	defer func() { inter.ref = savedRef }()
	for _, refDef := range refFramesAll {
		refFrame := refDef.refFrame
		refSlot, ok := e.vp9InterReferenceSlot(inter, refFrame)
		if !ok {
			continue
		}
		if sub8x8RefSkipped(refDef.refIndex) {
			continue
		}
		inter.ref = &e.refFrames[refSlot]

		// ref_costs_single[ref] for SINGLE_REFERENCE == intra_inter(1) +
		// single_ref signalling (estimate_ref_frame_costs, vp9_rdopt.c:2461-2467).
		refRate := encoder.SingleRefModeRateCost(&inter.selectFc, above, left,
			inter.referenceMode, inter.compoundRefs, refFrame)

		// --- switchable-filter loop: pick the filter with lowest tmp_rd.
		var segBest vp9Sub8x8SegResult
		segBestFilter := vp9dec.InterpEighttap
		segBestRD := rdCostMaxLocal
		segBestSet := false
		for _, filter := range filters {
			in := vp9Sub8x8Input{
				tile:          tile,
				miRows:        miRows,
				miCols:        miCols,
				miRow:         miRow,
				miCol:         miCol,
				interModeMask: interModeMask,
				switchableCtx: switchableCtx,
				above:         above,
				left:          left,
				rdmult:        rdmult,
				bestRD:        bestYRD,
				bestRDInf:     bestYRDInf,
				segMvs:        &segMvCache,
			}
			seg := e.rdPickBestSub8x8Mode(inter, in, bsize, refFrame, filter)
			if !seg.Valid {
				continue
			}
			tmpRD := seg.SegmentRD
			// rs_rd = RDCOST(switchable_rate, 0); filter_cache tracks min.
			// tmp_rd += rs_rd when cm->interp_filter == SWITCHABLE.
			if switchable {
				rs := encoder.SwitchableInterpRateCost(&inter.selectFc,
					switchableCtx, filter)
				tmpRD += encoder.RDCost(rdmult, rddiv, rs, 0)
			}
			if !segBestSet || tmpRD < segBestRD {
				segBestRD = tmpRD
				segBest = seg
				segBestFilter = filter
				segBestSet = true
			}
		}
		if !segBestSet {
			continue
		}

		// rate2 = segment_rate; distortion2 = segment_dist.
		rate2 := segBest.R
		distortion2 := segBest.D
		rateY := segBest.SegmentYrate
		totalSSE := segBest.SSE
		skippable := segBest.Skippable
		if switchable {
			rate2 += encoder.SwitchableInterpRateCost(&inter.selectFc,
				switchableCtx, segBestFilter)
		}

		// --- UV-RD (vp9_rdopt.c:4668-4692). tmp_best_rdu = best_rd -
		// min(RDCOST(rate2,distortion2), RDCOST(0,total_sse)); only run UV when
		// tmp_best_rdu > 0.
		rateUV := 0
		var distUV uint64
		var uvBudget uint64
		uvBudgetValid := true
		if !bestRDValid {
			uvBudget = ^uint64(0)
		} else {
			yCost := encoder.RDCost(rdmult, rddiv, rate2, distortion2)
			if floor := encoder.RDCost(rdmult, rddiv, 0, totalSSE); floor < yCost {
				yCost = floor
			}
			if yCost >= bestRD {
				// tmp_best_rdu <= 0: skip UV (libvpx keeps rate_uv=0 etc.).
				uvBudgetValid = false
			} else {
				uvBudget = bestRD - yCost
			}
		}
		if uvBudgetValid {
			uv, ok := e.vp9Sub8x8UVRD(inter, miRows, miCols, miRow, miCol, bsize,
				refFrame, segBestFilter, &segBest.Bmi, rdmult, uvBudget)
			if !ok {
				continue
			}
			rate2 += uv.Rate
			distortion2 += uv.Distortion
			rateUV = uv.Rate
			distUV = uv.Distortion
			skippable = skippable && uv.Skippable
			totalSSE += uv.SSE
		}

		// --- ref-frame signalling (vp9_rdopt.c:4707-4711, single-ref branch).
		rate2 += refRate

		// --- skip pick (vp9_rdopt.c:4713-4742). Sub-8x8 always codes skip at
		// mode-info level (ref != INTRA, !lossless).
		skip2 := false
		skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
		skip0 := encoder.VP9CostBit(skipProb, 0)
		skip1 := encoder.VP9CostBit(skipProb, 1)
		if refFrame > vp9dec.IntraFrame && !inter.lossless {
			noSkip := encoder.RDCost(rdmult, rddiv, rateY+rateUV+skip0, distortion2)
			skip := encoder.RDCost(rdmult, rddiv, skip1, totalSSE)
			if noSkip < skip {
				rate2 += skip0
			} else {
				rate2 += skip1
				distortion2 = totalSSE
				rate2 -= rateY + rateUV
				skip2 = true
			}
		} else {
			rate2 += skip0
		}

		thisRD := encoder.RDCost(rdmult, rddiv, rate2, distortion2)
		// best mode so far? (this_rd < best_rd).
		better := !bestSet
		if bestSet && (!bestRDValid || thisRD < bestRD) {
			better = true
		}
		if better {
			bestRD = thisRD
			bestRDValid = true
			// best_yrd = best_rd - RDCOST(rate_uv, distortion_uv)
			// (vp9_rdopt.c:4772-4773): the Y-only budget for the next ref's
			// segment + UV early-exit.
			uvRDC := encoder.RDCost(rdmult, rddiv, rateUV, distUV)
			if bestRD > uvRDC {
				bestYRD = bestRD - uvRDC
				bestYRDInf = false
			} else {
				bestYRD = 0
				bestYRDInf = false
			}
			bestRes = vp9Sub8x8WrapperResult{
				bmi:          segBest.Bmi,
				mode:         segBest.Bmi[3].AsMode,
				mv:           [2]vp9dec.MV{segBest.Bmi[3].AsMv[0]},
				refFrame:     refFrame,
				interpFilter: segBestFilter,
				uvMode:       common.DcPred,
				rate:         rate2,
				distortion:   distortion2,
				thisRD:       thisRD,
				rateY:        rateY,
				rateUV:       rateUV,
				distUV:       distUV,
				skippable:    skippable,
				skip2:        skip2,
				segEntropy:   segBest.SegEntropy,
				valid:        true,
			}
			bestSet = true
			// Oracle-trace pin: record the live-derived committed segment Y rate
			// (bsi->r) + filter for mi=(0,1) so the wrapper test can assert the
			// sibling entropy-context propagation closed the rate gap. Zero-cost in
			// non-trace builds.
			e.recordVP9Sub8x8WrapperCommit(miRow, miCol, segBest.R, segBestFilter)
		}
	}

	// --- INTRA evaluation (vp9_ref_order ref_index 5: INTRA_FRAME, evaluated
	// AFTER every inter ref). Ports the ref_frame==INTRA_FRAME branch of
	// vp9_rd_pick_inter_mode_sub8x8 (vp9_rdopt.c:4511-4528) + the shared
	// ref-signalling/skip/RDCOST/commit tail (vp9_rdopt.c:4707-4775).
	inter.ref = savedRef
	if !sub8x8RefSkipped(sfThrIntra) {
		if intraRes, intraCap, ok := e.rdPickInterSub8x8Intra(inter, tile, miRows,
			miCols, miRow, miCol, bsize, above, left, rdmult, rddiv, bestRD,
			bestRDValid); ok {
			better := !bestSet
			if bestSet && (!bestRDValid || intraRes.thisRD < bestRD) {
				better = true
			}
			if better {
				bestRD = intraRes.thisRD
				bestRDValid = true
				bestRes = intraRes
				bestSet = true
				// Oracle-trace pin: record the committed intra leaf (mi=(1,0)) so the
				// wrapper test can assert the intra Y/UV rate + per-sub-block modes match
				// libvpx. Zero-cost in non-trace builds.
				e.recordVP9Sub8x8IntraCommit(intraCap)
			}
		}
	}

	if !bestSet {
		return vp9Sub8x8WrapperResult{}, false
	}
	if !bestRes.intra {
		e.recordVP9Sub8x8InterCommit(vp9Sub8x8InterCapture{
			MiRow:        miRow,
			MiCol:        miCol,
			Bsize:        bsize,
			Mode:         bestRes.mode,
			RefFrame:     bestRes.refFrame,
			InterpFilter: bestRes.interpFilter,
			Bmi:          bestRes.bmi,
			Rate:         bestRes.rate,
			RateY:        bestRes.rateY,
			RateUV:       bestRes.rateUV,
			Distortion:   bestRes.distortion,
			DistUV:       bestRes.distUV,
			ThisRD:       bestRes.thisRD,
			Skip2:        bestRes.skip2,
		})
	}
	// vp9_rd_pick_inter_mode_sub8x8 finalisation (vp9_rdopt.c:4894-4906):
	// mi->mv[0] = bmi[3].as_mv[0]; second-ref mv zeroed (no second ref here).
	return bestRes, true
}

// rdPickInterSub8x8Intra ports the ref_frame==INTRA_FRAME arm of
// vp9_rd_pick_inter_mode_sub8x8 (vp9_rdopt.c:4511-4528) plus the shared
// ref-signalling + no-skip-flag + RDCOST tail (vp9_rdopt.c:4707-4742), returning
// a wrapper result with the intra mode committed. bestRD/bestRDValid are the
// running best after the inter ref loop; intra's rd_pick_intra_sub_8x8_y_mode
// early-exits against bestRD (vp9_rdopt.c:4513-4514).
func (e *VP9Encoder) rdPickInterSub8x8Intra(inter *vp9InterEncodeState,
	tile vp9dec.TileBounds, miRows, miCols, miRow, miCol int,
	bsize common.BlockSize, above, left *vp9dec.NeighborMi, rdmult, rddiv int,
	bestRD uint64, bestRDValid bool,
) (vp9Sub8x8WrapperResult, vp9Sub8x8IntraCapture, bool) {
	// Build the mi the intra Y search writes its per-sub-block bmi modes into and
	// the predictor reads (sb_type = the sub-8x8 shape so the per-4x4 grid drives).
	mi := vp9dec.NeighborMi{
		SbType:   bsize,
		TxSize:   common.Tx4x4,
		RefFrame: [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame},
	}

	yBudget := ^uint64(0)
	if bestRDValid {
		yBudget = bestRD
	}
	// rate, rate_y, distortion_y = rd_pick_intra_sub_8x8_y_mode(... best_rd).
	// If its returned RD >= best_rd, skip intra (vp9_rdopt.c:4513-4514).
	yRes, ok := e.rdPickInterSub8x8IntraYMode(inter, tile, miRows, miCols,
		miRow, miCol, bsize, &mi, rdmult, yBudget)
	if !ok {
		return vp9Sub8x8WrapperResult{}, vp9Sub8x8IntraCapture{}, false
	}
	if bestRDValid && yRes.rd >= bestRD {
		return vp9Sub8x8WrapperResult{}, vp9Sub8x8IntraCapture{}, false
	}

	// rate2 = rate; rate2 += intra_cost_penalty; distortion2 = distortion_y.
	rate2 := yRes.rate
	// vp9_get_intra_cost_penalty(cpi, bsize, cm->base_qindex, cm->y_dc_delta_q)
	// (vp9_rdopt.c:4356). bsize <= BLOCK_8X8 → reduction_fac 4. y_dc_delta_q is 0
	// in govpx (vp9_encoder_rd.go:130).
	intraPenalty := encoder.IntraCostPenalty(e.vp9EncoderModeDecisionQIndex(), 0,
		bsize, e.noiseEstimate.Enabled, e.noiseEstimate.ExtractLevel())
	rate2 += intraPenalty
	distortion2 := yRes.distortion

	// choose_intra_uv_mode (once): rate_uv_intra, rate_uv (tokenonly), dist_uv.
	uv, ok := e.vp9Sub8x8IntraUVRD(inter, tile, miRows, miCols, miRow, miCol,
		mi.Mode, rdmult)
	if !ok {
		return vp9Sub8x8WrapperResult{}, vp9Sub8x8IntraCapture{}, false
	}
	rate2 += uv.rate
	rateUV := uv.rateTokenOnly
	distortion2 += uv.distortion

	// ref-frame signalling (single-ref branch): rate2 += ref_costs_single[INTRA]
	// = vp9_cost_bit(intra_inter_p, 0) (estimate_ref_frame_costs vp9_rdopt.c:2471,
	// consumed at vp9_rdopt.c:4710).
	rate2 += encoder.IntraInterRateCost(&inter.selectFc, above, left, 0)

	// no-skip flag (vp9_rdopt.c:4736-4738): for INTRA the skip path takes the
	// else branch — rate2 += skip_cost0 (no skip override).
	skipProb := e.fc.SkipProbs[vp9dec.GetSkipContext(above, left)]
	rate2 += encoder.VP9CostBit(skipProb, 0)

	thisRD := encoder.RDCost(rdmult, rddiv, rate2, distortion2)

	res := vp9Sub8x8WrapperResult{
		bmi:  yRes.bmi,
		mode: yRes.mode,
		mv:   [2]vp9dec.MV{},
		// libvpx vp9_rdopt.c:4765 — mi->interp_filter = SWITCHABLE_FILTERS for the
		// committed intra block (== vp9dec.SwitchableFilters; the decoder stamps the
		// same in intra_driver.go).
		refFrame:     vp9dec.IntraFrame,
		interpFilter: vp9dec.InterpFilter(vp9dec.SwitchableFilters),
		uvMode:       uv.mode,
		rate:         rate2,
		distortion:   distortion2,
		thisRD:       thisRD,
		skippable:    yRes.rateY == 0 && rateUV == 0,
		skip2:        false,
		intra:        true,
		segEntropy:   yRes.segEntropy,
		valid:        true,
	}
	cap := vp9Sub8x8IntraCapture{
		MiRow: miRow, MiCol: miCol, Bsize: bsize,
		Mode: yRes.mode,
		Bmi: [4]common.PredictionMode{yRes.bmi[0].AsMode, yRes.bmi[1].AsMode,
			yRes.bmi[2].AsMode, yRes.bmi[3].AsMode},
		UVMode:     uv.mode,
		Rate:       rate2,
		YRate:      yRes.rate,
		UVRate:     uv.rate,
		Distortion: distortion2,
		ThisRD:     thisRD,
	}
	return res, cap, true
}

// vp9Sub8x8IntraCapture holds the committed intra sub-8x8 leaf decomposition for
// the oracle-trace pin (frame-1 SB0 16x16(0,0) child at mi=(1,0) BLOCK_8X4 INTRA).
type vp9Sub8x8IntraCapture struct {
	MiRow      int
	MiCol      int
	Bsize      common.BlockSize
	Mode       common.PredictionMode
	Bmi        [4]common.PredictionMode
	UVMode     common.PredictionMode
	Rate       int    // rate2 (the committed rd_cost->rate)
	YRate      int    // rd_pick_intra_sub_8x8_y_mode *rate (incl. mbmode_cost)
	UVRate     int    // rate_uv_intra (tokenonly + uv mode cost)
	Distortion uint64 // distortion2
	ThisRD     uint64 // this_rd
}

// vp9Sub8x8UVRDResult is the sub-8x8 chroma RD (super_block_uvrd on BLOCK_8X8).
type vp9Sub8x8UVRDResult struct {
	Rate       int
	Distortion uint64
	SSE        uint64
	Skippable  bool
}

// vp9Sub8x8UVRD builds the sub-8x8 chroma predictor (the per-sub-block bmi MVs
// averaged via the decoder's mi_mv_pred) and runs super_block_uvrd over the
// BLOCK_8X8 chroma extent (vp9_rdopt.c:4675-4678
// vp9_build_inter_predictors_sbuv(BLOCK_8X8) + super_block_uvrd(BLOCK_8X8)).
func (e *VP9Encoder) vp9Sub8x8UVRD(inter *vp9InterEncodeState,
	miRows, miCols, miRow, miCol int, bsize common.BlockSize, refFrame int8,
	filter vp9dec.InterpFilter, bmi *[4]vp9dec.Bmi, rdmult int, refBestRD uint64,
) (vp9Sub8x8UVRDResult, bool) {
	// Build the full-block mi with the sub-block bmi so reconstructVP9Inter-
	// PredictBlock averages the chroma MV (mi_mv_pred_q4/q2, inter_mv.go) the
	// way the decoder will. SbType stays the sub-8x8 shape so the averaging
	// kicks in; tx_size TX_4X4 (sub-8x8 luma tx) feeds get_uv_tx_size.
	// libvpx vp9_rd_pick_inter_mode_sub8x8 builds the chroma predictor +
	// super_block_uvrd over BLOCK_8X8 (vp9_rdopt.c:4675-4678). The mode_info
	// footprint of a sub-8x8 partition is the 8x8 (one MODE_INFO covering it);
	// mi->sb_type stays the sub-8x8 shape so reconstructVP9InterPredictBlock's
	// SbType<BLOCK_8X8 branch averages the per-sub-block bmi MVs into the chroma
	// MV (mi_mv_pred / AverageSplitMvs). Passing BLOCK_4X4 as the predict bsize
	// makes the chroma plane_bsize BLOCK_INVALID (ss_size_lookup[BLOCK_4X4][1][1]),
	// so the predict bsize MUST be BLOCK_8X8.
	mi := vp9dec.NeighborMi{
		SbType:       bsize,
		TxSize:       common.Tx4x4,
		Mode:         bmi[3].AsMode,
		InterpFilter: uint8(filter),
		RefFrame:     [2]int8{refFrame, vp9dec.NoRefFrame},
		Mv:           [2]vp9dec.MV{bmi[3].AsMv[0]},
		Bmi:          *bmi,
	}
	if !e.predictVP9InterBlock(inter, miRows, miCols, miRow, miCol,
		common.Block8x8, &mi) {
		return vp9Sub8x8UVRDResult{}, false
	}
	// super_block_uvrd over BLOCK_8X8 chroma (uv_tx_size from BLOCK_8X8 + TX_4X4).
	uv := e.vp9FullRDInterSuperBlockUVRDForMi(inter, miRows, miCols, miRow, miCol,
		common.Block8x8, &mi, rdmult, true, refBestRD)
	if !uv.Valid {
		return vp9Sub8x8UVRDResult{}, false
	}
	return vp9Sub8x8UVRDResult{
		Rate:       uv.Rate,
		Distortion: uv.Distortion,
		SSE:        uv.SSE,
		Skippable:  uv.Skippable,
	}, true
}
