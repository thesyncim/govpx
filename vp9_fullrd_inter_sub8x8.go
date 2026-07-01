package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// vp9_fullrd_inter_sub8x8.go ports the GENUINE VP9 sub-8x8 joint-motion RD:
// rd_pick_best_sub8x8_mode (vp9/encoder/vp9_rdopt.c:2077-2427) and its driver
// vp9_rd_pick_inter_mode_sub8x8 (vp9_rdopt.c:4294-4930), as a standalone,
// verified producer.
//
// Unlike pickVP9Sub8InterMode (vp9_encoder_inter_modes.go:1944) — the model
// stand-in that scores only ZEROMV/NEARESTMV/NEARMV with the SSE-model score
// and never runs the per-sub-block NEWMV joint search — this producer runs the
// real rd_pick_best_sub8x8_mode loop: for each label (1/2/4 partitions of the
// 4x4/4x8/8x4 block) it evaluates NEARESTMV/NEARMV/ZEROMV/NEWMV, runs the
// per-sub-block full-pixel + sub-pixel motion search for NEWMV (single-ref,
// mi_buf_shift'd to the sub-block), costs the segment MV via set_and_cost_bmi_mvs
// (MV_COST_WEIGHT_SUB=120), and computes the residual RD via encode_inter_mb_segment
// (per-4x4 fdct4x4 + vpx_quantize_b + vp9_block_error + cost_coeffs), accumulating
// bsi->rdcost. The wrapper drives this over the ref frames + the inter_mode_mask,
// adds the UV-RD + ref-frame signalling + the skip pick, and returns the best.
//
// All RD/cost primitives are reused verbatim: the full-pixel diamond
// (encoder.FullPixelDiamond), the subpel tree (refineVP9InterSubpelMv-style via
// the bordered subpel variance), set_and_cost_bmi_mvs cost (encoder.CostMvRef +
// encoder.MvBitCostSub), fdct4x4 (encoder.ForwardDCT4x4Into), vpx_quantize_b
// (encoder.QuantizeBWithQ), vp9_block_error (encoder.BlockErrorFP + sum-of-
// squares), cost_coeffs (vp9InterCoeffBlockRateCostQ), and the verified
// super_block_uvrd producer (vp9FullRDInterSuperBlockUVRD). No constants are
// re-derived.
//
// GATED OFF: this producer is consulted only behind vp9InterUseDeepRDSub8x8 and
// (separately) the oracle-trace pin. Production keeps the pickVP9Sub8InterMode
// model stand-in, so production byte-parity is untouched.
//
// libvpx ground truth (vpxenc-vp9 cpu0 CBR 1200 kbps, kf=999, fps 30, the
// panning source; TEMPORARY fprintf in rd_pick_best_sub8x8_mode, reverted):
// frame 1, the 16x16(0,0)'s four 8x8 children — 8x8(0,0)=NONE, (0,1)=SPLIT→4x4,
// (1,0)=HORZ→8x4, (1,1)=VERT→4x8. For the (0,1) SPLIT(4x4) ref=LAST
// EIGHTTAP segment (rdmult=139158 rddiv=7), the four labels are:
//
//	block 0: NEARESTMV mv=(9,15) brate=3989 byrate=3229 bdist=15809 bsse=22289 brdcost=3107734 eob=1
//	block 1: NEARESTMV mv=(9,15) brate=5226 byrate=4466 bdist=3769  bsse=29329 brdcost=1902822 eob=1
//	block 2: NEWMV     mv=(9,4)  brate=11906 byrate=7296 bdist=16990 bsse=24526 brdcost=5410688 eob=9
//	block 3: NEARESTMV mv=(9,4)  brate=33832 byrate=33072 bdist=23453 bsse=734187 brdcost=12197284 eob=16
//
// accumulating bsi->r=54953 bsi->d=60021 bsi->sse=810331 segment_rd=22618528.

// vp9Sub8x8Label is one label (sub-block) RD outcome inside
// rd_pick_best_sub8x8_mode: the selected mode/MV and the encode_inter_mb_segment
// rate/dist/sse + the brdcost (vp9_rdopt.c:2338-2359 SEG_RDSTAT).
type vp9Sub8x8Label struct {
	Block   int                   // raster sub-block index 0..3
	Mode    common.PredictionMode // mode_selected
	Mv      vp9dec.MV             // mode_mv[mode_selected][0]
	Brate   int                   // brate = byrate + mode/MV rate
	Byrate  int                   // labelyrate (cost_coeffs only)
	Bdist   uint64                // bdist (thisdistortion >> 2)
	Bsse    uint64                // bsse (thissse >> 2)
	Brdcost uint64                // brdcost = RDCOST(byrate,bdist) + RDCOST(brate-byrate,0)
	Eob     int                   // the label's first 4x4 eob (matches the probe)
	AnyEob  bool                  // any 4x4 in the label had eob > 0
}

// vp9Sub8x8SegResult is rd_pick_best_sub8x8_mode's output for one (bsize, ref,
// filter): the segment RD totals + the per-label quartet.
type vp9Sub8x8SegResult struct {
	SegmentRD    uint64 // bsi->segment_rd (== sum of label brdcost)
	R            int    // bsi->r (sum brate)
	D            uint64 // bsi->d (sum bdist)
	SSE          uint64 // bsi->sse (sum bsse)
	SegmentYrate int    // bsi->segment_yrate (sum byrate)
	Skippable    bool   // vp9_is_skippable_in_plane(BLOCK_8X8, 0)
	Labels       [4]vp9Sub8x8Label
	Bmi          [4]vp9dec.Bmi // the per-sub bmi quartet (mode + mv)
	// SegEntropy is the 8x8 block's plane[0] above/left entropy context AFTER all
	// labels are coded (the running t_above[2]/t_left[2] at segment end,
	// vp9_rdopt.c:2398-2399). The partition recursion's encode_sb stamps this into
	// pd->above_context/left_context so the next sibling 8x8's sub-8x8 RD seed
	// reads it (vp9_encodeframe.c:2167-2218 save_context/restore_context +
	// :4163-4166 encode_sb for split children with index != 3).
	SegEntropy vp9Sub8x8SegmentEntropy
	Valid      bool
}

// (The vp9_rd_pick_inter_mode_sub8x8 wrapper — driving rdPickBestSub8x8Mode over
// the ref frames + filters, adding the UV-RD + ref-frame signalling + skip pick,
// and producing the serialized block decision — is the NEXT step; this deliverable
// is the verified rd_pick_best_sub8x8_mode producer.)

// vp9Sub8x8Input carries the per-SB picker context rd_pick_best_sub8x8_mode and
// the wrapper need that is not derivable from (bsize, ref, filter) alone.
type vp9Sub8x8Input struct {
	tile          vp9dec.TileBounds
	miRows        int
	miCols        int
	miRow         int
	miCol         int
	interModeMask int
	switchableCtx int
	above         *vp9dec.NeighborMi
	left          *vp9dec.NeighborMi
	rdmult        int
	bestRD        uint64 // best_rd_so_far (segment budget)
	bestRDInf     bool

	// inject* override the grid-derived candidate context. Used ONLY by the
	// oracle-trace verification (the production partition search never reaches
	// the documented 8x8 child, so the mi grid lacks the SPLIT-recursion
	// neighbour state libvpx has there). When injectValid is false the producer
	// derives bestRefMv/modeContext/seed from the live grid as in production.
	injectValid       bool
	injectBestRefMv   vp9dec.MV
	injectModeContext int
	injectSeed        vp9Sub8x8SegmentEntropy
	// injectFrameMv overrides the per-block append_sub8x8_mvs NEAREST/NEAR
	// candidates (CAND probe), which depend on the SPLIT-recursion grid state.
	injectFrameMv [4]vp9Sub8x8FrameMvPair

	// segMvs is the per-(block, ref_frame) NEWMV motion-search cache libvpx keeps
	// as seg_mvs[4][MAX_REF_FRAMES] at vp9_rd_pick_inter_mode_sub8x8 function
	// scope (vp9/encoder/vp9_rdopt.c:4327, initialised to INVALID_MV once at
	// :4343-4346 BEFORE the ref + switchable-filter loops). The per-sub-block
	// single-ref NEWMV search runs only when seg_mvs[block][ref] == INVALID_MV
	// (:2170), so its result is computed ONCE per (block, ref) and reused across
	// every switchable filter and ref_index iteration of the same 8x8 block. The
	// wrapper supplies a shared cache here so the three filter passes of one ref
	// reuse the first pass's NEW MVs; without it each filter re-runs the search
	// from its own (filter-dependent) committed-prior-block seed and converges to
	// a different MV, corrupting the per-filter segment_rd (the {0,2,0,0,2} mi(2,5)
	// SMOOTH-vs-SHARP divergence). When nil the producer falls back to a local
	// per-call cache (the oracle-trace single-filter drivers).
	segMvs *vp9Sub8x8SegMvCache
}

// vp9Sub8x8SegMvCache is libvpx's seg_mvs[4][MAX_REF_FRAMES] NEWMV cache for one
// 8x8 block, shared across the block's switchable-filter passes.
type vp9Sub8x8SegMvCache struct {
	mv    [4][vp9dec.MaxRefFrames]vp9dec.MV
	valid [4][vp9dec.MaxRefFrames]bool
}

// vp9Sub8x8FrameMvPair is one block's injected NEAREST/NEAR candidate pair.
type vp9Sub8x8FrameMvPair struct {
	nearest vp9dec.MV
	near    vp9dec.MV
}

// rdPickBestSub8x8Mode ports rd_pick_best_sub8x8_mode (vp9_rdopt.c:2077-2427)
// for a single reference frame and a single interp filter. It walks the labels
// (idy/idx step by num_4x4_blocks_high/wide) and, per label, evaluates the
// inter modes, runs the NEWMV motion search, costs the segment MV, and computes
// the encode_inter_mb_segment RD, accumulating bsi->segment_rd.
//
// filterIdx == 0 here (the EIGHTTAP entry); the filter-reuse short-circuit
// (vp9_rdopt.c:2301-2336, filter_idx>0) is not modelled — the verified producer
// evaluates each filter independently, which yields the same per-filter
// segment_rd libvpx caches.
func (e *VP9Encoder) rdPickBestSub8x8Mode(inter *vp9InterEncodeState,
	in vp9Sub8x8Input, bsize common.BlockSize, refFrame int8,
	filter vp9dec.InterpFilter,
) vp9Sub8x8SegResult {
	if inter == nil || inter.dq == nil || inter.ref == nil || !inter.ref.valid ||
		bsize >= common.Block8x8 {
		return vp9Sub8x8SegResult{}
	}
	num4x4W := int(common.Num4x4BlocksWideLookup[bsize])
	num4x4H := int(common.Num4x4BlocksHighLookup[bsize])

	// bsi->ref_mv[0] = best_ref_mv = mbmi_ext->ref_mvs[ref_frame][0]
	// (vp9_rdopt.c:4577). This is the NEAREST candidate for the whole 8x8 block,
	// found by vp9_find_mv_refs(BLOCK_8X8). bsi->mvp seeds from it (block 0).
	bestRefMv, _ := e.vp9EncoderInterModeCandidateMv(in.tile, in.miRows, in.miCols,
		in.miRow, in.miCol, common.Block8x8, common.NearestMv, refFrame,
		inter.allowHP, inter.refSignBias)

	interModeCtxArr := vp9dec.InterModeContext(e.miGrid, in.miCols, in.tile,
		in.miRows, in.miRow, in.miCol, common.Block8x8)
	modeContext := interModeCtxArr // mbmi_ext->mode_context[ref_frame]
	if in.injectValid {
		bestRefMv = in.injectBestRefMv
		modeContext = in.injectModeContext
	}

	// Running mi.bmi[] quartet: append_sub8x8_mvs / set_and_cost_bmi_mvs read
	// prior blocks' bmi (vp9_rdopt.c:1595-1602, 2187-2189). Seed the partition.
	var mi vp9dec.NeighborMi
	mi.SbType = bsize
	mi.RefFrame = [2]int8{refFrame, vp9dec.NoRefFrame}
	mi.InterpFilter = uint8(filter)

	// Per-block frame_mv[NEARESTMV/NEARMV/ZEROMV] are recomputed per label via
	// append_sub8x8; ZEROMV is always (0,0). seg_mvs caches the NEWMV result
	// across the 8x8 block's switchable-filter passes (libvpx seg_mvs[4][REF],
	// vp9_rdopt.c:4327/2170). Use the wrapper-shared cache when supplied; else a
	// local per-call cache (the single-filter oracle-trace drivers).
	segCache := in.segMvs
	if segCache == nil {
		segCache = &vp9Sub8x8SegMvCache{}
	}
	refIdx := int(refFrame)
	if refIdx < 0 || refIdx >= vp9dec.MaxRefFrames {
		return vp9Sub8x8SegResult{}
	}

	res := vp9Sub8x8SegResult{Valid: true}
	var thisSegmentRD uint64

	// t_above[2]/t_left[2]: seed from the 8x8 block's plane[0] above/left
	// context (vp9_rdopt.c:2120-2121), carried across labels.
	var segEnt vp9Sub8x8SegmentEntropy
	if in.injectValid {
		segEnt = in.injectSeed
	} else {
		e.vp9Sub8x8SeedEntropy(&segEnt, in.miRow, in.miCol)
	}

	for idy := 0; idy < 2; idy += num4x4H {
		for idx := 0; idx < 2; idx += num4x4W {
			block := idy*2 + idx

			// append_sub8x8_mvs_for_idx(block) → frame_mv[NEAREST]/[NEAR].
			var nearestMv, nearMv vp9dec.MV
			if in.injectValid {
				nearestMv = in.injectFrameMv[block].nearest
				nearMv = in.injectFrameMv[block].near
			} else {
				nearestMv = e.vp9AppendSub8x8MvsForIdx(&mi, in.tile, in.miRows,
					in.miCols, in.miRow, in.miCol, bsize, common.NearestMv, block, 0,
					refFrame, inter.refSignBias)
				nearMv = e.vp9AppendSub8x8MvsForIdx(&mi, in.tile, in.miRows,
					in.miCols, in.miRow, in.miCol, bsize, common.NearMv, block, 0,
					refFrame, inter.refSignBias)
				vp9dec.LowerMvPrecision(&nearestMv, inter.allowHP)
				vp9dec.LowerMvPrecision(&nearMv, inter.allowHP)
			}
			frameMv := map[common.PredictionMode]vp9dec.MV{
				common.NearestMv: nearestMv,
				common.NearMv:    nearMv,
				common.ZeroMv:    {},
			}

			var bestLabel vp9Sub8x8Label
			var bestEnt vp9Sub8x8SegmentEntropy
			bestLabelRD := rdCostMaxLocal
			bestModeSel := common.ZeroMv
			haveBest := false

			for _, thisMode := range [...]common.PredictionMode{
				common.NearestMv, common.NearMv, common.ZeroMv, common.NewMv,
			} {
				if in.interModeMask&(1<<uint(thisMode)) == 0 {
					continue
				}
				if !vp9CheckBestZeroMv(&inter.selectFc, modeContext, frameMv,
					thisMode) {
					continue
				}

				thisMv := frameMv[thisMode]
				if thisMode == common.NewMv {
					if segCache.valid[block][refIdx] {
						thisMv = segCache.mv[block][refIdx]
					} else {
						// best_rd < label_mv_thresh early-break (vp9_rdopt.c:2182):
						// label_mv_thresh == bsi->mvthresh/4 == 0 (mvthresh always
						// 0 on this path), so the search always runs.
						// per-sub-block motion search (single predictor case).
						searchMv, sok := e.vp9Sub8x8NewMvSearch(inter, in, bsize,
							block, refFrame, &mi, bestRefMv)
						if !sok {
							continue
						}
						thisMv = searchMv
						segCache.mv[block][refIdx] = searchMv
						segCache.valid[block][refIdx] = true
						// libvpx rd_pick_best_sub8x8_mode (vp9/encoder/vp9_rdopt.c:
						// 2259): x->pred_mv[mi->ref_frame[0]] = *new_mv — every NEW
						// sub-block's subpel search result overwrites x->pred_mv[ref],
						// so after the segment the value is the LAST sub-block's NEW MV.
						// This is the same x->pred_mv that single_motion_search writes
						// (:2750); the depth-first recursion threads it across sibling
						// blocks as vp9_mv_pred's third candidate (pred_mv[2], vp9_rd.c:
						// 613) for the next (smaller-or-equal) block. govpx mirrors that
						// here so a sibling committed as a sub-8x8 SPLIT leaves the
						// sub-8x8 last-NEW MV in fullRDPredMv (not the stale 8x8-NONE
						// search result), matching the seed libvpx feeds the next 8x8
						// NONE leaf. Without this, the next leaf's NEWMV search seeds
						// from the wrong candidate and can converge to a spuriously
						// better MV, flipping its NONE-vs-SPLIT partition pick (the
						// {0,2,0,0,2} mi(1,6) divergence: govpx kept an 8x8 NONE NEWMV
						// where libvpx splits to 4x4). Gated on the deep stack exactly
						// like the single_motion_search write; production (flags off)
						// never reads fullRDPredMv so it is inert there.
						if (vp9InterUseDeepRDSub8x8 || vp9InterUseDeepRDUsePartition) &&
							refFrame > vp9dec.IntraFrame &&
							int(refFrame) < len(e.fullRDPredMv) {
							e.fullRDPredMv[refFrame] = searchMv
						}
					}
				}

				// set_and_cost_bmi_mvs (vp9_rdopt.c:1557-1606): writes
				// mi.bmi[block..].as_mv/as_mode, returns mode/MV rate.
				modeMvRate := e.setAndCostBmiMvs(inter, &mi, block, thisMode,
					thisMv, bestRefMv, modeContext, inter.allowHP, num4x4W,
					num4x4H)

				// mv_check_bounds (vp9_rdopt.c:2296): drop vectors past UMV.
				mvLimits := encoder.EncoderMvLimits(in.miRows, in.miCols, in.miRow,
					in.miCol, bsize)
				if vp9MvCheckBounds(&mvLimits, thisMv) {
					continue
				}

				// encode_inter_mb_segment RD (vp9_rdopt.c:2338-2354). Each mode
				// candidate starts from the label-entry entropy context (libvpx
				// snapshots t_above/t_left into rdstat[block][mode].ta/tl, line
				// 2163-2166); only the SELECTED mode's resulting context carries.
				labelBudget := rdCostMaxLocal
				if !in.bestRDInf {
					// bsi->segment_rd - this_segment_rd (the running budget).
					if in.bestRD > thisSegmentRD {
						labelBudget = in.bestRD - thisSegmentRD
					} else {
						labelBudget = 0
					}
				}
				candEnt := segEnt
				seg, sok := e.encodeInterMbSegment(inter, in, &candEnt, bsize,
					block, refFrame, &mi, labelBudget)
				if !sok {
					continue
				}
				// brdcost += RDCOST(rdmult, rddiv, brate, 0); brate += byrate.
				brdcost := seg.rdcost
				if brdcost < rdCostMaxLocal {
					brdcost += encoder.RDCost(in.rdmult, encoder.RDDivBits,
						modeMvRate, 0)
				}
				brate := modeMvRate + seg.byrate

				if brdcost < bestLabelRD {
					bestLabelRD = brdcost
					bestModeSel = thisMode
					bestEnt = candEnt
					haveBest = true
					bestLabel = vp9Sub8x8Label{
						Block:   block,
						Mode:    thisMode,
						Mv:      thisMv,
						Brate:   brate,
						Byrate:  seg.byrate,
						Bdist:   seg.dist,
						Bsse:    seg.sse,
						Brdcost: brdcost,
						Eob:     seg.eob,
						AnyEob:  seg.anyEob,
					}
				}
			}

			if !haveBest {
				return vp9Sub8x8SegResult{}
			}

			// Carry the selected mode's entropy context forward
			// (vp9_rdopt.c:2372-2373).
			segEnt = bestEnt
			// Commit the selected mode into mi.bmi[block..] (vp9_rdopt.c:2375).
			e.setAndCostBmiMvs(inter, &mi, block, bestModeSel, bestLabel.Mv,
				bestRefMv, modeContext, inter.allowHP, num4x4W, num4x4H)

			res.Labels[block] = bestLabel
			res.R += bestLabel.Brate
			res.D += bestLabel.Bdist
			res.SSE += bestLabel.Bsse
			res.SegmentYrate += bestLabel.Byrate
			thisSegmentRD += bestLabel.Brdcost

			if !in.bestRDInf && thisSegmentRD > in.bestRD {
				return vp9Sub8x8SegResult{}
			}
		}
	}

	res.SegmentRD = thisSegmentRD
	res.Bmi = mi.Bmi
	// segEnt now holds the running t_above[2]/t_left[2] after the last label's
	// selected mode (vp9_rdopt.c:2398-2399 memcpy(t_above, rdstat[block].ta)).
	res.SegEntropy = segEnt
	// vp9_is_skippable_in_plane(BLOCK_8X8, 0): skippable iff every 4x4 sub-block
	// eob == 0 (vp9_rdopt.c:2422). Track via the per-label anyEob.
	res.Skippable = true
	for i := range res.Labels {
		if res.Labels[i].AnyEob {
			res.Skippable = false
			break
		}
	}
	if !in.bestRDInf && res.SegmentRD > in.bestRD {
		return vp9Sub8x8SegResult{}
	}
	return res
}

// vp9CheckBestZeroMv ports check_best_zero_mv (vp9_rdopt.c:1801-1835) for the
// single-reference case.
func vp9CheckBestZeroMv(fc *vp9dec.FrameContext, modeContext int,
	frameMv map[common.PredictionMode]vp9dec.MV, thisMode common.PredictionMode,
) bool {
	if thisMode != common.NearMv && thisMode != common.NearestMv &&
		thisMode != common.ZeroMv {
		return true
	}
	if frameMv[thisMode] != (vp9dec.MV{}) {
		return true
	}
	// ref_frames[1] == NO_REF_FRAME (single ref): the second-ref clause holds.
	c1 := encoder.CostMvRef(fc, modeContext, common.NearMv)
	c2 := encoder.CostMvRef(fc, modeContext, common.NearestMv)
	c3 := encoder.CostMvRef(fc, modeContext, common.ZeroMv)
	switch thisMode {
	case common.NearMv:
		if c1 > c3 {
			return false
		}
	case common.NearestMv:
		if c2 > c3 {
			return false
		}
	default: // ZEROMV
		if (c3 >= c2 && frameMv[common.NearestMv] == (vp9dec.MV{})) ||
			(c3 >= c1 && frameMv[common.NearMv] == (vp9dec.MV{})) {
			return false
		}
	}
	return true
}

// vp9MvCheckBounds ports mv_check_bounds (vp9_rdopt.c:1764-1769).
func vp9MvCheckBounds(limits *encoder.MvLimits, mv vp9dec.MV) bool {
	return (int(mv.Row)>>3) < limits.RowMin || (int(mv.Row)>>3) > limits.RowMax ||
		(int(mv.Col)>>3) < limits.ColMin || (int(mv.Col)>>3) > limits.ColMax
}

// setAndCostBmiMvs ports set_and_cost_bmi_mvs (vp9_rdopt.c:1557-1606) for the
// single-reference case: writes mi.bmi[i..].as_mv[0]/as_mode and returns
// cost_mv_ref(mode) + (NEWMV ? vp9_mv_bit_cost(MV_COST_WEIGHT_SUB) : 0).
func (e *VP9Encoder) setAndCostBmiMvs(inter *vp9InterEncodeState,
	mi *vp9dec.NeighborMi, i int, mode common.PredictionMode,
	thisMv, bestRefMv vp9dec.MV, modeContext int, allowHP bool, num4x4W, num4x4H int,
) int {
	thisMvCost := 0
	if mode == common.NewMv {
		thisMvCost = encoder.MvBitCostSub(thisMv, bestRefMv, &inter.selectFc.Nmvc,
			allowHP)
	}
	mi.Bmi[i].AsMv[0] = thisMv
	mi.Bmi[i].AsMode = mode
	// memmove fill across the label's 4x4 footprint (vp9_rdopt.c:1600-1602).
	for idy := range num4x4H {
		for idx := range num4x4W {
			mi.Bmi[i+idy*2+idx] = mi.Bmi[i]
		}
	}
	return encoder.CostMvRef(&inter.selectFc, modeContext, mode) + thisMvCost
}
