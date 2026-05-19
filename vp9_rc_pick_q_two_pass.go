package govpx

// VP9 two-pass per-frame Q picker — port of libvpx
// vp9_rc_pick_q_and_bounds_two_pass.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:1468 vp9_rc_pick_q_and_bounds_two_pass
//
// This function consumes the GF group decision produced by vp9DefineGFGroup
// (rf_level, layer_depth, gf_group.max_layer_depth, gfu_boost) and the
// per-frame state to emit (active_best_quality, active_worst_quality, q).
// govpx's existing vbrQuantizer / vbrQuantizerWithBounds remain the
// runtime path; this port is exercised through tests and is wired in
// over time as the remaining deferrals are populated.
//
// Deferred fields cited inline:
//   - last_qindex_of_arf_layer[] tracking (libvpx vp9_ratectrl.c:1554).
//     We treat the per-layer-depth floor as 0 until the post-encode
//     hook that updates this is ported.
//   - extend_minq_fast (libvpx vp9_ratectrl.c twopass->extend_minq_fast).
//     Defaults to 0.
const (
	vp9StaticMotionThresh   = 95
	vp9StaticKFGroupThresh  = 99
	vp9SmallKFFramePixels   = 352 * 288
	vp9DefaultKeyFrameBoost = 2000
	vp9DefaultGFUBoost      = 2000
)

// vp9RCPickQAndBoundsTwoPassInputs aggregates the libvpx state the
// two-pass Q picker reads.
type vp9RCPickQAndBoundsTwoPassInputs struct {
	// libvpx: frame_is_intra_only(cm).
	IsIntraOnly bool
	// libvpx: cpi->refresh_golden_frame || cpi->refresh_alt_ref_frame.
	BoostFrame bool
	// libvpx: rc->is_src_frame_alt_ref (overlay slot of previous ARF).
	IsSrcFrameAltRef bool
	// libvpx: rc->this_key_frame_forced.
	ThisKeyFrameForced bool
	// libvpx: rc->frames_since_key.
	FramesSinceKey int
	// libvpx: rc->avg_frame_qindex[INTER_FRAME].
	AvgFrameQIndexInter int
	// libvpx: rc->last_kf_qindex.
	LastKFQIndex int
	// libvpx: rc->last_boosted_qindex.
	LastBoostedQIndex int
	// libvpx: rc->best_quality / rc->worst_quality.
	BestQuality  int
	WorstQuality int
	// libvpx: rc->this_frame_target / rc->max_frame_bandwidth.
	ThisFrameTarget   int
	MaxFrameBandwidth int
	// libvpx: cpi->twopass.active_worst_quality.
	ActiveWorstQuality int
	// libvpx: twopass->extend_minq / extend_maxq / extend_minq_fast.
	ExtendMinQ     int
	ExtendMaxQ     int
	ExtendMinQFast int
	// libvpx: twopass->last_qindex_of_arf_layer[max_layer_depth-1].
	LastQIndexOfMaxLayerDepth int
	// libvpx: twopass->last_kfgroup_zeromotion_pct.
	LastKFGroupZeroMotionPct int
	// libvpx: twopass->kf_zeromotion_pct.
	KFZeroMotionPct int
	// libvpx: rc->kf_boost.
	KeyFrameBoost int
	// libvpx: cm->width / cm->height.
	FrameWidth  int
	FrameHeight int
	// libvpx: get_active_cq_level_two_pass(...).
	CQLevel int
	// libvpx: cpi->oxcf.rc_mode == VPX_CQ.
	IsCQ bool
	// libvpx: rc->arf_active_best_quality_adjustment_factor.
	ARFActiveBestQualityAdjustmentFactor float64
	// libvpx: rc->arf_increase_active_best_quality.
	ARFIncreaseActiveBestQuality int
	// libvpx: rc->gfu_boost or gf_group->gfu_boost[index] for multi-layer ARF.
	GFUBoost int
	// libvpx: gf_group->rf_level[gf_group_index].
	RFLevel uint8
	// libvpx: gf_group->layer_depth[gf_group_index].
	LayerDepth int
	// libvpx: gf_group->max_layer_depth.
	MaxLayerDepth int
}

// vp9RCPickQAndBoundsTwoPassResult is the (active_best, active_worst, q)
// tuple returned by libvpx vp9_rc_pick_q_and_bounds_two_pass.
type vp9RCPickQAndBoundsTwoPassResult struct {
	ActiveBest  int
	ActiveWorst int
	Q           int
}

// vp9RCPickQAndBoundsTwoPass ports libvpx vp9_rc_pick_q_and_bounds_two_pass.
//
// libvpx: vp9/encoder/vp9_ratectrl.c:1468
//
// targetBitsPerMB is the libvpx target_bits_per_mb used by vp9_rc_regulate_q
// (a function of rc->this_frame_target / mb_count / correction_factor). To
// avoid coupling this pure port to the full regulator stack, callers must
// pass in an externally-regulated `regulatedQ` produced by the existing
// govpx regulator; the picker then applies the libvpx-faithful clamping +
// adjustments around it. This mirrors the libvpx call
// vp9_rc_regulate_q(cpi, rc->this_frame_target, active_best_quality,
// active_worst_quality) (vp9_ratectrl.c:1606).
func vp9RCPickQAndBoundsTwoPass(in vp9RCPickQAndBoundsTwoPassInputs, regulatedQ int) vp9RCPickQAndBoundsTwoPassResult {
	var activeBest int
	activeWorst := in.ActiveWorstQuality

	// libvpx: pick_kf_q_bound_two_pass / boost_frame / non-boost branches.
	if in.IsIntraOnly {
		// libvpx vp9_ratectrl.c:1363-1429 pick_kf_q_bound_two_pass.
		if in.ThisKeyFrameForced {
			if in.LastKFGroupZeroMotionPct >= vp9StaticMotionThresh {
				qindex := min(in.LastKFQIndex, in.LastBoostedQIndex)
				activeBest = qindex
				deltaQIndex := vp9ComputeQDelta(in.BestQuality, in.WorstQuality, qindex, 125, 100)
				activeWorst = min(qindex+deltaQIndex, activeWorst)
			} else {
				qindex := in.LastBoostedQIndex
				deltaQIndex := vp9ComputeQDelta(in.BestQuality, in.WorstQuality, qindex, 75, 100)
				activeBest = max(qindex+deltaQIndex, in.BestQuality)
			}
		} else {
			qAdjFactorNum := 1050
			keyFrameBoost := in.KeyFrameBoost
			if keyFrameBoost == 0 {
				keyFrameBoost = vp9DefaultKeyFrameBoost
			}
			activeBest = vp9KFActiveQualityWithBoost(activeWorst, keyFrameBoost)
			if in.KFZeroMotionPct >= vp9StaticKFGroupThresh {
				activeBest /= 4
			}
			activeBest = min(activeWorst, max(1, activeBest))
			if in.FrameWidth > 0 && in.FrameHeight > 0 &&
				in.FrameWidth*in.FrameHeight <= vp9SmallKFFramePixels {
				qAdjFactorNum -= 250
			}
			qAdjFactorNum -= in.KFZeroMotionPct
			activeBest += vp9ComputeQDelta(in.BestQuality, in.WorstQuality,
				activeBest, qAdjFactorNum, 1000)
		}
	} else if in.BoostFrame {
		// libvpx vp9_ratectrl.c:1492-1531.
		var q int
		if in.FramesSinceKey > 1 && in.AvgFrameQIndexInter < activeWorst {
			q = in.AvgFrameQIndexInter
		} else {
			q = activeWorst
		}
		if in.IsCQ && q < in.CQLevel {
			q = in.CQLevel
		}
		gfuBoost := in.GFUBoost
		if gfuBoost == 0 {
			gfuBoost = vp9DefaultGFUBoost
		}
		activeBest = vp9GFActiveQualityWithBoost(q, gfuBoost)
		// libvpx vp9_ratectrl.c:1509-1515: arf_increase_active_best_quality
		// branches use the high-motion / low-motion MINQ tables.
		arfActiveBestQHL := activeBest
		switch in.ARFIncreaseActiveBestQuality {
		case 1:
			arfActiveBestQHL = vp9GFHighMotionActiveQuality(q)
		case -1:
			arfActiveBestQHL = vp9GFLowMotionActiveQuality(q)
		}
		factor := in.ARFActiveBestQualityAdjustmentFactor
		activeBest = int(float64(activeBest)*factor + float64(arfActiveBestQHL)*(1.0-factor))

		// libvpx vp9_ratectrl.c:1524-1531: GF_ARF_LOW layer-depth bias.
		if in.RFLevel == vp9RFLGFARFLow && in.LayerDepth > 0 {
			activeBest = ((in.LayerDepth-1)*q + activeBest + in.LayerDepth/2) / in.LayerDepth
		}
	} else {
		// libvpx vp9_ratectrl.c:1532-1540.
		activeBest = vp9InterMINQ(activeWorst)
		if in.IsCQ && activeBest < in.CQLevel {
			activeBest = in.CQLevel
		}
	}

	// libvpx vp9_ratectrl.c:1544-1569: extend_minq / extend_maxq.
	if in.IsIntraOnly || in.BoostFrame {
		activeBest -= in.ExtendMinQ + in.ExtendMinQFast
		activeWorst += in.ExtendMaxQ / 2
		if in.RFLevel == vp9RFLGFARFLow && in.LayerDepth > 1 {
			if in.LastQIndexOfMaxLayerDepth > activeBest {
				activeBest = in.LastQIndexOfMaxLayerDepth
			}
		}
	} else {
		activeBest -= (in.ExtendMinQ + in.ExtendMinQFast) / 2
		activeWorst += in.ExtendMaxQ
		if in.MaxLayerDepth > 0 && in.LastQIndexOfMaxLayerDepth > activeBest {
			activeBest = in.LastQIndexOfMaxLayerDepth
		}
	}

	// libvpx vp9_ratectrl.c:1591-1594: clamp to [best_quality, worst_quality].
	if activeBest < in.BestQuality {
		activeBest = in.BestQuality
	}
	if activeBest > in.WorstQuality {
		activeBest = in.WorstQuality
	}
	if activeWorst < activeBest {
		activeWorst = activeBest
	}
	if activeWorst > in.WorstQuality {
		activeWorst = in.WorstQuality
	}

	// libvpx vp9_ratectrl.c:1596-1615: pick q.
	var q int
	switch {
	case in.IsIntraOnly && in.ThisKeyFrameForced:
		if in.LastKFGroupZeroMotionPct >= vp9StaticMotionThresh {
			q = min(in.LastKFQIndex, in.LastBoostedQIndex)
		} else {
			q = in.LastBoostedQIndex
		}
	case in.IsIntraOnly && !in.ThisKeyFrameForced:
		q = activeBest
	default:
		// libvpx calls vp9_rc_regulate_q(cpi, rc->this_frame_target,
		// active_best_quality, active_worst_quality). We accept the
		// caller-provided regulated Q (computed via the existing govpx
		// regulator on the [active_best, active_worst] interval) and
		// apply the libvpx clamp + max-frame-bandwidth special case.
		q = regulatedQ
		if q > activeWorst {
			if in.ThisFrameTarget >= in.MaxFrameBandwidth {
				activeWorst = q
			} else {
				q = activeWorst
			}
		}
	}

	// Final invariants from libvpx (vp9_ratectrl.c:1620-1623).
	if q < in.BestQuality {
		q = in.BestQuality
	}
	if q > in.WorstQuality {
		q = in.WorstQuality
	}

	return vp9RCPickQAndBoundsTwoPassResult{
		ActiveBest:  activeBest,
		ActiveWorst: activeWorst,
		Q:           q,
	}
}
