package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func vp9EncodeCountsForState(key *vp9KeyframeEncodeState,
	inter *vp9InterEncodeState,
) *encoder.FrameCounts {
	if key != nil && key.counts != nil {
		return key.counts
	}
	if inter != nil {
		return inter.counts
	}
	return nil
}

func txModeForMi(mi vp9dec.NeighborMi) common.TxMode {
	if mi.TxSize >= common.Tx32x32 {
		return common.Allow32x32
	}
	if mi.TxSize >= common.Tx16x16 {
		return common.Allow16x16
	}
	if mi.TxSize >= common.Tx8x8 {
		return common.Allow8x8
	}
	return common.Only4x4
}

func vp9InterFrameTxMode(inter *vp9InterEncodeState) common.TxMode {
	if inter == nil || inter.txMode >= common.TxModes {
		return common.TxModeSelect
	}
	if inter.txMode == common.Only4x4 && !inter.lossless {
		return common.TxModeSelect
	}
	return inter.txMode
}

// vp9EncoderFrameTxMode is a verbatim port of libvpx select_tx_mode at
// vp9/encoder/vp9_encodeframe.c:4334-4345:
//
//	static TX_MODE select_tx_mode(const VP9_COMP *cpi, MACROBLOCKD *const xd) {
//	  if (xd->lossless) return ONLY_4X4;
//	  if (cpi->common.frame_type == KEY_FRAME && cpi->sf.use_nonrd_pick_mode)
//	    return ALLOW_16X16;
//	  if (cpi->sf.tx_size_search_method == USE_LARGESTALL)
//	    return ALLOW_32X32;
//	  else if (cpi->sf.tx_size_search_method == USE_FULL_RD ||
//	           cpi->sf.tx_size_search_method == USE_TX_8X8)
//	    return TX_MODE_SELECT;
//	  else
//	    return cpi->common.tx_mode;
//	}
//
// libvpx's select_tx_mode runs once per frame at
// vp9/encoder/vp9_encodeframe.c:5650 (the top of vp9_encode_frame_internal),
// AFTER set_speed_features_framesize_dependent has refreshed the per-frame
// speed-feature snapshot (vp9_encoder.c:3754/3765). govpx mirrors that
// protocol via the per-frame vp9ApplySpeedFeatures call at
// encodeVP9FrameIntoWithFlagsResultInternal:2546, so e.sf carries the live
// per-frame value at the time select_tx_mode runs. The
// vp9SelectTxModeSpeedFeatures helper additionally precomputes the
// (use_nonrd_pick_mode, tx_size_search_method) pair libvpx's
// set_*_speed_feature would produce for the (deadline, cpu_used, isKey,
// intraOnly) tuple so the use_nonrd_pick_mode predicate can be evaluated
// independently of the live sf snapshot — see
// vp9_speed_features.c:485-504,558-583,1042 (RT path) and
// vp9_speed_features.c:1250-1252,1286-1288,1310,1447-1449,1539-1541,
// 1595-1597 (GOOD path).
//
// libvpx INTRA_ONLY frames go through the non-KEY_FRAME branch because
// the KEY_FRAME predicate compares cm->frame_type == KEY_FRAME literally
// (vp9_blockd.h:34-38 — INTRA_ONLY uses cm->frame_type == INTER_FRAME with
// the intra_only flag set). govpx now honours the libvpx
// tx_size_search_method dispatch for both KEY_FRAME and intra-only via a
// shared switch — the keyframe-source block writer at writeVP9ModeBlock:6885+
// and the vp9ModeTreeKeyframe fallback (introduced in commit 0dfca64)
// both plumb the TxModeSelect-shaped tx_probs row, so neither path
// panics in WriteSelectedTxSize. write_mb_modes_kf at
// vp9_bitstream.c:344-376 services both frame types via
// frame_is_intra_only(cm) at vp9_bitstream.c:395-396.
//
// Inter frames use the same tx_size_search_method dispatch as key and
// intra-only frames. This matters for GOOD/RT lower cpu-used settings where
// libvpx selects USE_LARGESTALL -> ALLOW_32X32 instead of TX_MODE_SELECT.
func (e *VP9Encoder) vp9EncoderFrameTxMode(isKey, intraOnly, lossless bool) common.TxMode {
	if lossless {
		return common.Only4x4
	}
	useNonrd, _ := e.vp9SelectTxModeSpeedFeatures(isKey, intraOnly)
	if isKey && useNonrd {
		// libvpx vp9_encodeframe.c:4336-4337 — the KEY_FRAME &&
		// use_nonrd_pick_mode ALLOW_16X16 clamp ported verbatim.
		// Note: libvpx's `frame_type == KEY_FRAME` predicate is
		// literal — intra-only frames carry frame_type INTER_FRAME
		// and fall through to the tx_size_search_method dispatch
		// below.
		return common.Allow16x16
	}
	// libvpx vp9_encodeframe.c:4338-4344 fallthrough once the KEY_FRAME &&
	// use_nonrd_pick_mode clamp above has been ruled out:
	//   USE_LARGESTALL            -> ALLOW_32X32 (:4338-4339)
	//   USE_FULL_RD or USE_TX_8X8 -> TX_MODE_SELECT (:4340-4342)
	switch e.sf.TxSizeSearchMethod {
	case UseLargestAll:
		return common.Allow32x32
	default:
		return common.TxModeSelect
	}
}

// vp9SelectTxModeSpeedFeatures returns the (use_nonrd_pick_mode,
// tx_size_search_method) pair libvpx's per-frame speed-feature dispatcher
// would set for this frame, given the current (deadline, cpu_used,
// isKey, intraOnly) triple. Mirrors the relevant assignments inside
// vp9_speed_features.c set_good_speed_feature / set_rt_speed_feature
// without requiring a full per-frame re-apply. The KEY_FRAME &&
// use_nonrd_pick_mode clamp consumes useNonrd; the final
// tx_size_search_method switch reads e.sf.TxSizeSearchMethod after the real
// per-frame refresh.
func (e *VP9Encoder) vp9SelectTxModeSpeedFeatures(isKey, intraOnly bool) (useNonrd bool, txSearch TxSizeSearchMethod) {
	speed := e.vp9SpeedFeatureCPUUsed()
	mode := vp9ResolveDeadlineMode(e.opts.Deadline)
	if mode == vp9ModeRealtime {
		// libvpx vp9_speed_features.c:492-493 (RT speed>=1):
		//   tx_size_search_method =
		//       frame_is_intra_only(cm) ? USE_FULL_RD : USE_LARGESTALL;
		if speed >= 1 {
			if intraOnly {
				txSearch = UseFullRD
			} else {
				txSearch = UseLargestAll
			}
		}
		// libvpx vp9_speed_features.c:597-598 (RT speed>=5):
		//   sf->use_nonrd_pick_mode = 1;
		//   tx_size_search_method = is_keyframe ? USE_LARGESTALL : USE_TX_8X8;
		if speed >= 5 {
			useNonrd = true
			if isKey {
				txSearch = UseLargestAll
			} else {
				txSearch = UseTx8x8
			}
		}
		return useNonrd, txSearch
	}
	if mode == vp9ModeBest {
		return false, UseFullRD
	}
	// GOOD path. libvpx vp9_speed_features.c:1042 dispatch.
	// libvpx vp9_speed_features.c:929-940 best-quality default:
	//   tx_size_search_method = USE_FULL_RD.
	txSearch = UseFullRD
	// libvpx vp9_speed_features.c:326-327 (GOOD speed>=2):
	//   sf->tx_size_search_method =
	//       frame_is_boosted(cpi) ? USE_FULL_RD : USE_LARGESTALL;
	// govpx's KF predicate is the simplest is_boosted approximation
	// available pre-per-frame-SF-refresh — KF is always boosted; GF/ARF
	// boostedness requires the per-frame dispatcher.
	if speed >= 2 {
		if isKey {
			txSearch = UseFullRD
		} else {
			txSearch = UseLargestAll
		}
	}
	// libvpx vp9_speed_features.c:381-382 (GOOD speed>=3):
	//   sf->tx_size_search_method =
	//       frame_is_intra_only(cm) ? USE_FULL_RD : USE_LARGESTALL;
	if speed >= 3 {
		if intraOnly {
			txSearch = UseFullRD
		} else {
			txSearch = UseLargestAll
		}
	}
	// libvpx vp9_speed_features.c:386 (GOOD speed>=4):
	//   sf->tx_size_search_method = USE_LARGESTALL;
	if speed >= 4 {
		txSearch = UseLargestAll
	}
	// libvpx vp9_speed_features.c:415-416 (GOOD speed>=5):
	//   sf->tx_size_search_method =
	//       frame_is_intra_only(cm) ? USE_LARGESTALL : USE_TX_8X8;
	//   sf->use_nonrd_pick_mode = 1;
	if speed >= 5 {
		useNonrd = true
		if intraOnly {
			txSearch = UseLargestAll
		} else {
			txSearch = UseTx8x8
		}
	}
	return useNonrd, txSearch
}

// vp9EncoderFrameTxModeFromCounts demotes the per-frame tx_mode after
// the counts pass.
//
// The TxModeSelect branch is a verbatim port of libvpx's post-encode
// TX_MODE_SELECT demotion ladder at vp9/encoder/vp9_encodeframe.c:
// 5911-5944, which walks the partition-context counts
// (counts->tx.p32x32 / p16x16 / p8x8) into six bucketed totals and
// applies the four-way demotion cascade. The libvpx outer gate
// `cpi->sf.frame_parameter_update` at vp9_encodeframe.c:5846 is
// honoured via frameParameterUpdate: when the speed feature is off
// (e.g. RT speed>=4 — vp9_speed_features.c:568) libvpx leaves
// cm->tx_mode untouched, so this function must too.
//
// libvpx vp9_encodeframe.c:5911 inner gate `cm->tx_mode == TX_MODE_SELECT`
// means non-TxModeSelect frames bypass the demotion entirely; the
// returned tx_mode is whatever select_tx_mode emitted at
// vp9_encodeframe.c:5650 (lossless -> ONLY_4X4, USE_LARGESTALL ->
// ALLOW_32X32, USE_FULL_RD/USE_TX_8X8 -> TX_MODE_SELECT, KEY_FRAME &&
// use_nonrd_pick_mode -> ALLOW_16X16). This function reflects that gate
// strictly: callers receive the unmodified txMode for every mode that
// isn't TX_MODE_SELECT.
func vp9EncoderFrameTxModeFromCounts(txMode common.TxMode, lossless bool,
	frameParameterUpdate bool, counts *encoder.FrameCounts,
) common.TxMode {
	if lossless {
		return common.Only4x4
	}
	if counts == nil {
		return txMode
	}
	// libvpx vp9_encodeframe.c:5911 — non-TxModeSelect frames bypass
	// the demotion entirely. Mirrors the libvpx if-gate exactly: any
	// fixed tx_mode (ONLY_4X4 / ALLOW_8X8 / ALLOW_16X16 / ALLOW_32X32)
	// emitted by select_tx_mode (vp9_encodeframe.c:4334-4345) is
	// written to the bitstream verbatim.
	if txMode != common.TxModeSelect {
		return txMode
	}
	// libvpx vp9_encodeframe.c:5846 — the entire post-encode
	// demotion block (including the TX_MODE_SELECT ladder at
	// :5911-5944) is gated on cpi->sf.frame_parameter_update.
	// vp9_speed_features.c:568 zeroes it at RT speed >= 4, and
	// vp9_speed_features.c:929 sets it to 1 elsewhere.
	if !frameParameterUpdate {
		return txMode
	}
	// Verbatim port of libvpx vp9/encoder/vp9_encodeframe.c:5911-5944.
	// Bucket the partition-context tx counts across the
	// TX_SIZE_CONTEXTS == 2 contexts (vp9_entropymode.h:25), summing
	// counts->tx.p32x32[i][T] + counts->tx.p16x16[i][T] +
	// counts->tx.p8x8[i][T] into the six trackers libvpx uses to
	// decide whether to collapse TX_MODE_SELECT into a fixed mode.
	var count4x4, count8x8Lp, count8x8p8x8 uint32
	var count16x16p16x16, count16x16Lp, count32x32 uint32
	for i := range vp9dec.TxSizeContexts {
		// libvpx vp9_encodeframe.c:5918-5920.
		count4x4 += counts.TxMode.P32x32[i][common.Tx4x4]
		count4x4 += counts.TxMode.P16x16[i][common.Tx4x4]
		count4x4 += counts.TxMode.P8x8[i][common.Tx4x4]

		// libvpx vp9_encodeframe.c:5922-5924.
		count8x8Lp += counts.TxMode.P32x32[i][common.Tx8x8]
		count8x8Lp += counts.TxMode.P16x16[i][common.Tx8x8]
		count8x8p8x8 += counts.TxMode.P8x8[i][common.Tx8x8]

		// libvpx vp9_encodeframe.c:5926-5928.
		count16x16p16x16 += counts.TxMode.P16x16[i][common.Tx16x16]
		count16x16Lp += counts.TxMode.P32x32[i][common.Tx16x16]
		count32x32 += counts.TxMode.P32x32[i][common.Tx32x32]
	}
	// libvpx vp9_encodeframe.c:5930-5933 — ALLOW_8X8 demotion: no
	// 4x4 anywhere, and no count larger than 8x8 anywhere. The caller
	// mirrors reset_skip_tx_size(cm, TX_8X8) by clamping the cached
	// leaf decisions before replaying the frame with the reduced mode.
	if count4x4 == 0 && count16x16Lp == 0 && count16x16p16x16 == 0 &&
		count32x32 == 0 {
		return common.Allow8x8
	}
	// libvpx vp9_encodeframe.c:5934-5937 — ONLY_4X4 demotion: no
	// 8x8 / 16x16 / 32x32 hits anywhere (only 4x4 was selected).
	// The caller mirrors reset_skip_tx_size(cm, TX_4X4) at :5937.
	if count8x8p8x8 == 0 && count16x16p16x16 == 0 &&
		count8x8Lp == 0 && count16x16Lp == 0 && count32x32 == 0 {
		return common.Only4x4
	}
	// libvpx vp9_encodeframe.c:5938-5939 — ALLOW_32X32 demotion: no
	// 4x4 anywhere and no "lp" demotion from the p32x32 sub-table
	// (the largest max-tx context never picked a smaller size). No
	// reset_skip_tx_size call (libvpx leaves mi tx_sizes alone since
	// the new ceiling is still Tx32x32).
	if count8x8Lp == 0 && count16x16Lp == 0 && count4x4 == 0 {
		return common.Allow32x32
	}
	// libvpx vp9_encodeframe.c:5940-5943 — ALLOW_16X16 demotion:
	// p32x32 only ever picked Tx16x16 (no 32x32, no 8x8 demotion
	// from p32x32), and no 4x4 anywhere. The caller mirrors
	// reset_skip_tx_size(cm, TX_16X16) at :5942.
	if count32x32 == 0 && count8x8Lp == 0 && count4x4 == 0 {
		return common.Allow16x16
	}
	return common.TxModeSelect
}

// vp9EncoderFrameInterpFilter mirrors libvpx's frame-level interp_filter
// assignment at vp9/encoder/vp9_encoder.c:2141 —
//
//	cm->interp_filter = cpi->sf.default_interp_filter;
//
// The speed-features configurator
// (vp9/encoder/vp9_speed_features.c:1008) initialises
// default_interp_filter to SWITCHABLE for every speed; only the
// realtime cpu_used>=9 low-motion gate at vp9_speed_features.c:812
// downgrades it to BILINEAR. All other speeds (including cpu_used=8
// realtime and good-quality) inherit SWITCHABLE, which enables the
// per-block 3-filter RD search already wired through
// vp9_pick_inter_mode_nonrd.go (filterRef / predFilterSearch loop)
// and pickVP9InterMode (vp9InterInterpFilterCandidates).
//
// Lossless / keyframe / intra-only frames carry the same field —
// libvpx does not special-case them at this assignment site; the
// uncompressed-header writer omits the field for intra-only frames
// (header_writer.go:196) so the value is harmless for those frame
// types.
func (e *VP9Encoder) vp9EncoderFrameInterpFilter(isKey, intraOnly, lossless bool) vp9dec.InterpFilter {
	if e == nil {
		return vp9dec.InterpEighttap
	}
	filter := e.sf.DefaultInterpFilter
	if filter > vp9dec.InterpSwitchable {
		// Unset / out-of-range falls back to the libvpx initial value
		// at vp9_speed_features.c:1008.
		return vp9dec.InterpSwitchable
	}
	return filter
}

// vp9GetFrameTypeForFilterThreshes mirrors libvpx's get_frame_type at
// vp9/encoder/vp9_encodeframe.c:4323-4332, used to index into
// filter_threshes[MV_REFERENCE_FRAME] for the per-frame SWITCHABLE ->
// concrete InterpFilter demotion. The mapping is exactly:
//
//	if (frame_is_intra_only(cm))                          return INTRA_FRAME;
//	else if (rc.is_src_frame_alt_ref && refresh_golden)   return ALTREF_FRAME;
//	else if (refresh_golden || refresh_alt_ref)           return GOLDEN_FRAME;
//	else                                                  return LAST_FRAME;
//
// govpx tracks `is_src_frame_alt_ref` on the visible lookahead entry that
// supplied an earlier hidden ARF; refresh_golden / refresh_alt_ref are
// decoded from the libvpx-shaped refresh_frame_flags slot bits
// (vp9GoldenRefSlot / vp9AltRefSlot at vp9_encoder.c:2773-2774).
func vp9GetFrameTypeForFilterThreshes(isKey, intraOnly, isSrcFrameAltRef,
	refreshGolden, refreshAlt bool,
) int {
	if isKey || intraOnly {
		return vp9dec.IntraFrame
	}
	if isSrcFrameAltRef && refreshGolden {
		return vp9dec.AltrefFrame
	}
	if refreshGolden || refreshAlt {
		return vp9dec.GoldenFrame
	}
	return vp9dec.LastFrame
}

// vp9GetInterpFilterFromThreshes is the verbatim port of libvpx's
// get_interp_filter at vp9/encoder/vp9_encodeframe.c:5759-5773:
//
//	if (!is_alt_ref && threshes[EIGHTTAP_SMOOTH] > threshes[EIGHTTAP] &&
//	    threshes[EIGHTTAP_SMOOTH] > threshes[EIGHTTAP_SHARP] &&
//	    threshes[EIGHTTAP_SMOOTH] > threshes[SWITCHABLE - 1]) {
//	  return EIGHTTAP_SMOOTH;
//	} else if (threshes[EIGHTTAP_SHARP] > threshes[EIGHTTAP] &&
//	           threshes[EIGHTTAP_SHARP] > threshes[SWITCHABLE - 1]) {
//	  return EIGHTTAP_SHARP;
//	} else if (threshes[EIGHTTAP] > threshes[SWITCHABLE - 1]) {
//	  return EIGHTTAP;
//	} else {
//	  return SWITCHABLE;
//	}
//
// Note that libvpx indexes the threshold array up to SWITCHABLE_FILTER_CONTEXTS
// (== SWITCHABLE_FILTERS + 1 == 4 here), and the gate slot
// `threshes[SWITCHABLE - 1]` is `threshes[3]` (the BILINEAR slot, repurposed
// here as the "switchable wins" comparator — libvpx's filter_diff accumulator
// uses index SWITCHABLE_FILTERS for the switchable rd cost, which lands at 3
// since the EIGHTTAP family occupies 0..2). See the matching slot use in the
// post-encode merge at vp9_encodeframe.c:5890-5891.
func vp9GetInterpFilterFromThreshes(
	threshes [vp9dec.SwitchableFilterContexts]int64, isAltRef bool,
) vp9dec.InterpFilter {
	const switchableSlot = int(vp9dec.InterpSwitchable) - 1
	if !isAltRef &&
		threshes[vp9dec.InterpEighttapSmooth] > threshes[vp9dec.InterpEighttap] &&
		threshes[vp9dec.InterpEighttapSmooth] > threshes[vp9dec.InterpEighttapSharp] &&
		threshes[vp9dec.InterpEighttapSmooth] > threshes[switchableSlot] {
		return vp9dec.InterpEighttapSmooth
	}
	if threshes[vp9dec.InterpEighttapSharp] > threshes[vp9dec.InterpEighttap] &&
		threshes[vp9dec.InterpEighttapSharp] > threshes[switchableSlot] {
		return vp9dec.InterpEighttapSharp
	}
	if threshes[vp9dec.InterpEighttap] > threshes[switchableSlot] {
		return vp9dec.InterpEighttap
	}
	return vp9dec.InterpSwitchable
}

// vp9SaveEncodeParamsFilterThreshes mirrors the filter_threshes subset of
// libvpx's save_encode_params at vp9/encoder/vp9_encoder.c:3927-3946:
//
//	for (j = 0; j < SWITCHABLE_FILTER_CONTEXTS; j++)
//	  rd_opt->filter_threshes_prev[i][j] = rd_opt->filter_threshes[i][j];
//
// (The prediction_type_thresh and per-tile freq_fact halves are owned by
// other ports; this routine only handles the InterpFilter snapshot.)
// Called once per frame before vp9EncodeFrame mutates filter_threshes
// (libvpx call site: vp9_encoder.c:5355, ahead of encode_frame_to_data_rate).
func (e *VP9Encoder) vp9SaveEncodeParamsFilterThreshes() {
	e.vp9FilterThreshesPrev = e.vp9FilterThreshes
}

// vp9RestoreEncodeParamsFilterThreshes mirrors the filter_threshes subset of
// libvpx's restore_encode_params at vp9/encoder/vp9_encodeframe.c:5798-5820:
//
//	for (j = 0; j < SWITCHABLE_FILTER_CONTEXTS; j++)
//	  rd_opt->filter_threshes[i][j] = rd_opt->filter_threshes_prev[i][j];
//
// libvpx calls this at the top of every vp9_encode_frame so each recode
// iteration starts from the same baseline; govpx encodes each frame once
// today, so the restore is a no-op in steady state — kept verbatim because
// the recode loop is on the roadmap.
func (e *VP9Encoder) vp9RestoreEncodeParamsFilterThreshes() {
	e.vp9FilterThreshes = e.vp9FilterThreshesPrev
}

// vp9DemoteSwitchableInterpFilter applies the per-frame SWITCHABLE -> concrete
// filter demotion at libvpx vp9/encoder/vp9_encodeframe.c:5846-5877:
//
//	if (cpi->sf.frame_parameter_update) {
//	  ...
//	  const MV_REFERENCE_FRAME frame_type = get_frame_type(cpi);
//	  int64_t *const filter_thrs = rd_opt->filter_threshes[frame_type];
//	  const int is_alt_ref = frame_type == ALTREF_FRAME;
//	  ...
//	  if (cm->interp_filter == SWITCHABLE)
//	    cm->interp_filter = get_interp_filter(filter_thrs, is_alt_ref);
//	}
//
// The gate `cpi->sf.frame_parameter_update` matches govpx
// `e.sf.FrameParameterUpdate != 0` (vp9_speed_features.go:336,766,1517).
// Demotion is skipped entirely outside that path, leaving header.InterpFilter
// at SWITCHABLE so the per-block 3-filter RD search drives the per-block
// mi->interp_filter writes.
func (e *VP9Encoder) vp9DemoteSwitchableInterpFilter(currentFilter vp9dec.InterpFilter,
	isKey, intraOnly, isSrcFrameAltRef, refreshGolden, refreshAlt bool,
) vp9dec.InterpFilter {
	if e == nil || e.sf.FrameParameterUpdate == 0 {
		return currentFilter
	}
	if currentFilter != vp9dec.InterpSwitchable {
		return currentFilter
	}
	frameType := vp9GetFrameTypeForFilterThreshes(isKey, intraOnly,
		isSrcFrameAltRef, refreshGolden, refreshAlt)
	isAltRef := frameType == vp9dec.AltrefFrame
	return vp9GetInterpFilterFromThreshes(e.vp9FilterThreshes[frameType], isAltRef)
}

// vp9FixInterpFilter is the verbatim port of libvpx's fix_interp_filter
// at vp9/encoder/vp9_bitstream.c:864-885:
//
//	static void fix_interp_filter(VP9_COMMON *cm, FRAME_COUNTS *counts) {
//	  if (cm->interp_filter == SWITCHABLE) {
//	    // Check to see if only one of the filters is actually used
//	    int count[SWITCHABLE_FILTERS];
//	    int i, j, c = 0;
//	    for (i = 0; i < SWITCHABLE_FILTERS; ++i) {
//	      count[i] = 0;
//	      for (j = 0; j < SWITCHABLE_FILTER_CONTEXTS; ++j)
//	        count[i] += counts->switchable_interp[j][i];
//	      c += (count[i] > 0);
//	    }
//	    if (c == 1) {
//	      // Only one filter is used. So set the filter at frame level
//	      for (i = 0; i < SWITCHABLE_FILTERS; ++i) {
//	        if (count[i]) { cm->interp_filter = i; break; }
//	      }
//	    }
//	  }
//	}
//
// libvpx call site is vp9_bitstream.c:1312, sandwiched between
// write_frame_size_with_refs and write_interp_filter inside
// write_uncompressed_header. Because write_uncompressed_header runs
// before write_compressed_header (libvpx vp9_bitstream.c:1425,1453), the
// compressed-header writer sees the already-demoted cm->interp_filter
// at vp9_bitstream.c:1356 (`if (cm->interp_filter == SWITCHABLE)
// update_switchable_interp_probs...`). govpx inverts that order
// (compressed first, to size FirstPartitionSize), so the demotion runs
// right after collectVP9EncodeFrameCounts produces the counts and
// before WriteCompressedHeaderFromCounts reads InterpFilter.
//
// SWITCHABLE_FILTERS is the libvpx constant 3 (the count of real filters,
// excluding the SWITCHABLE sentinel); govpx exposes it as
// vp9dec.SwitchableFilters via internal/vp9/decoder/compressed_inter.go.
// counts.SwitchableInterp is the [SwitchableFilterContexts][SwitchableFilters]
// table populated by countVP9SwitchableInterp (vp9_encoder.go:3957).
func vp9FixInterpFilter(currentFilter vp9dec.InterpFilter,
	counts *encoder.FrameCounts,
) vp9dec.InterpFilter {
	if currentFilter != vp9dec.InterpSwitchable || counts == nil {
		return currentFilter
	}
	var count [vp9dec.SwitchableFilters]int
	c := 0
	for i := range vp9dec.SwitchableFilters {
		count[i] = 0
		for j := range vp9dec.SwitchableFilterContexts {
			count[i] += int(counts.SwitchableInterp[j][i])
		}
		if count[i] > 0 {
			c++
		}
	}
	if c != 1 {
		return currentFilter
	}
	for i := range vp9dec.SwitchableFilters {
		if count[i] != 0 {
			return vp9dec.InterpFilter(i)
		}
	}
	return currentFilter
}

// vp9UpdateFilterThreshesPostEncode merges this frame's accumulated
// rdc.filter_diff into the persistent filter_threshes via libvpx
// vp9/encoder/vp9_encodeframe.c:5890-5891:
//
//	for (i = 0; i < SWITCHABLE_FILTER_CONTEXTS; ++i)
//	  filter_thrs[i] = (filter_thrs[i] + rdc->filter_diff[i] / cm->MBs) / 2;
//
// The gate is identical to the demotion gate (sf.frame_parameter_update):
// libvpx hangs both off the same if-block at vp9_encodeframe.c:5846. mbs is
// the libvpx cm->MBs which govpx tracks as encoder.MacroblockCount(miRows, miCols).
// Per-block contributions land in vp9FilterDiff through the full-RD mode
// picker, mirroring vp9_encodeframe.c:1881 once the final leaf decision is
// known.
func (e *VP9Encoder) vp9UpdateFilterThreshesPostEncode(isKey, intraOnly,
	isSrcFrameAltRef, refreshGolden, refreshAlt bool, mbs int,
) {
	if e == nil || e.sf.FrameParameterUpdate == 0 || mbs <= 0 {
		// Always clear the per-frame accumulator so a stale value
		// cannot leak into the next frame even when the gate is off.
		e.vp9FilterDiff = [vp9dec.SwitchableFilterContexts]int64{}
		return
	}
	frameType := vp9GetFrameTypeForFilterThreshes(isKey, intraOnly,
		isSrcFrameAltRef, refreshGolden, refreshAlt)
	for i := range vp9dec.SwitchableFilterContexts {
		// libvpx: filter_thrs[i] = (filter_thrs[i] + filter_diff[i] / MBs) / 2
		e.vp9FilterThreshes[frameType][i] =
			(e.vp9FilterThreshes[frameType][i] +
				e.vp9FilterDiff[i]/int64(mbs)) / 2
	}
	e.vp9FilterDiff = [vp9dec.SwitchableFilterContexts]int64{}
}

func vp9EncoderFrameAllowHighPrecisionMv(isKey, intraOnly bool) bool {
	return !isKey && !intraOnly
}

// vp9EncoderLoopFilterParams builds the per-frame Loopfilter header
// fields. The filter level is now selected by
// (*VP9Encoder).vp9PickFilterLevel which dispatches across the three
// libvpx LPF_PICK_METHOD modes; the static closed-form fallback is
// reserved for the lossless / disabled paths.
//
// When sf.LpfPick selects LPF_PICK_FROM_FULL_IMAGE / SUBIMAGE the
// final level is decided post-tile by vp9EncoderRunFullImagePicker,
// which calls vp9SearchFilterLevel — and that search seeds filt_mid
// from e.vp9LastFiltLevel (libvpx vp9_picklpf.c:90). Therefore the
// pre-tile call here must NOT clobber e.vp9LastFiltLevel with the
// from-Q placeholder; otherwise the search at cpu_used<5 starts at
// the from-Q seed instead of the libvpx-correct last_filt_level
// (which is 0 on a non-forced KEY_FRAME, libvpx vp9_encoder.c:3445).
// Only the post-tile picker writes vp9LastFiltLevel in that case.
func (e *VP9Encoder) vp9EncoderLoopFilterParams(qindex int, isKey, intraOnly, resetDeltas, lossless, segEnabled bool,
	sharpness uint8, width, height int, txMode common.TxMode,
) vp9dec.LoopfilterParams {
	// libvpx vp9_encoder.c:3442-3446 — at a non-forced keyframe the
	// picker is seeded with last_filt_level=0 so the quadratic search
	// starts fresh. govpx tracks `is_src_frame_alt_ref` only when
	// AltRef is enabled, so we conservatively reset on every key /
	// intra-only frame, matching libvpx's "reset on KEY_FRAME &&
	// !this_key_frame_forced" path for the common case
	// (this_key_frame_forced is the libvpx GF-derived forced-key
	// signal; govpx does not emit forced keys distinct from natural
	// key intervals).
	if isKey || intraOnly {
		e.vp9LastFiltLevel = 0
	}
	// Search-based methods need a placeholder filter_level for the
	// uncompressed-header pre-write; the real level is decided post-
	// tile against the reconstructed luma. Use the closed-form
	// FROM_Q value as the placeholder so that the disable / lossless
	// gates below (and the runFullImageSearch != 0 gate at line
	// 2644) see the same coarse magnitude libvpx would emit. Do not
	// update e.vp9LastFiltLevel here — the post-tile picker reads it
	// as the search seed (libvpx vp9_picklpf.c:90) and the libvpx-
	// correct seed is the just-reset value (0 on keyframes, the
	// prior frame's level otherwise).
	searchMethod := e.sf.LpfPick == LpfPickFromFullImage ||
		e.sf.LpfPick == LpfPickFromSubImage
	var level uint8
	if searchMethod {
		level = uint8(e.vp9PickLpfFromQ(qindex, isKey, segEnabled, width, height))
	} else {
		level = uint8(e.vp9PickFilterLevel(e.sf.LpfPick, qindex, isKey, segEnabled,
			width, height, txMode, false /* partialFrame */, nil /* sseFn */))
	}
	if lossless {
		level = 0
	}
	if !searchMethod {
		// libvpx vp9_encoder.c:3448 — `lf->last_filt_level =
		// lf->filter_level` after the picker returns. For
		// non-search methods the picker is final here; for search
		// methods the post-tile path (line 2692) refreshes
		// vp9LastFiltLevel after the real search runs.
		e.vp9LastFiltLevel = level
	} else if lossless || level == 0 {
		// Search-mode path where the post-tile picker will be gated
		// off (the dispatcher requires header.Loopfilter.FilterLevel
		// != 0 to run): the placeholder is final, so commit it to
		// vp9LastFiltLevel just like the non-search branch.
		// libvpx vp9_encoder.c:3429-3430 explicitly resets
		// last_filt_level = 0 when lossless.
		e.vp9LastFiltLevel = level
	}
	return vp9dec.LoopfilterParams{
		FilterLevel:         level,
		SharpnessLevel:      sharpness,
		ModeRefDeltaEnabled: true,
		ModeRefDeltaUpdate:  resetDeltas,
		RefDeltas:           [vp9dec.MaxRefLfDeltas]int8{1, 0, -1, -1},
		ModeDeltas:          [vp9dec.MaxModeLfDeltas]int8{0, 0},
	}
}

func (e *VP9Encoder) vp9EncoderLoopFilterPrevDeltas(reset bool) (
	[vp9dec.MaxRefLfDeltas]int8,
	[vp9dec.MaxModeLfDeltas]int8,
) {
	if reset {
		return [vp9dec.MaxRefLfDeltas]int8{},
			[vp9dec.MaxModeLfDeltas]int8{}
	}
	return e.lfRefDeltas, e.lfModeDeltas
}

func (e *VP9Encoder) commitVP9EncoderLoopFilterDeltas(
	lf *vp9dec.LoopfilterParams, reset bool,
) {
	if reset {
		e.lfRefDeltas = [vp9dec.MaxRefLfDeltas]int8{}
		e.lfModeDeltas = [vp9dec.MaxModeLfDeltas]int8{}
	}
	if lf == nil || !lf.ModeRefDeltaEnabled || !lf.ModeRefDeltaUpdate {
		return
	}
	e.lfRefDeltas = lf.RefDeltas
	e.lfModeDeltas = lf.ModeDeltas
}

func (e *VP9Encoder) applyVP9EncoderLoopFilter(hdr *vp9dec.UncompressedHeader,
	seg *vp9dec.SegmentationParams,
) bool {
	if hdr.Loopfilter.FilterLevel == 0 {
		return true
	}
	layout := common.NewFrameLayout(int(hdr.Width), int(hdr.Height))
	vp9dec.LoopFilterFrameInit(&e.lfi, &hdr.Loopfilter, seg,
		int(hdr.Loopfilter.FilterLevel))
	d := VP9Decoder{
		lfi:                e.lfi,
		miGrid:             e.miGrid,
		frameYFull:         e.reconYFull,
		frameUFull:         e.reconUFull,
		frameVFull:         e.reconVFull,
		frameYOrigin:       layout.YOrigin,
		frameUOrigin:       layout.UVOrigin,
		frameVOrigin:       layout.UVOrigin,
		lastFrame:          e.reconFrame,
		vp9LoopFilterMasks: e.vp9LoopFilterMasks,
	}
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	var ok bool
	if pool := e.vp9TilePool; pool != nil && pool.workerCount > 1 {
		// libvpx loopfilter_frame (vp9/encoder/vp9_encoder.c:3461-3465)
		// routes through vp9_loop_filter_frame_mt whenever the encoder
		// owns more than one worker; reuse the tile worker pool the same
		// way cpi->workers back both the tile encode and the loop filter.
		ok = e.applyVP9EncoderLoopFilterMT(&d, pool, miRows, miCols,
			int(hdr.Width))
	} else {
		ok = d.applyVP9LoopFilterSerial(miRows, miCols)
	}
	e.vp9LoopFilterMasks = d.vp9LoopFilterMasks
	return ok
}

func vp9ModeTreeInterpFilter(kind vp9ModeTreeKind, inter *vp9InterEncodeState) vp9dec.InterpFilter {
	if kind == vp9ModeTreeInterSource || kind == vp9ModeTreeInterSkip {
		if inter != nil {
			return inter.interpFilter
		}
		return vp9dec.InterpSwitchable
	}
	return vp9dec.InterpEighttap
}

var vp9SwitchableInterpFilterOrder = [...]vp9dec.InterpFilter{
	vp9dec.InterpEighttap,
	vp9dec.InterpEighttapSmooth,
	vp9dec.InterpEighttapSharp,
}

// vp9NonrdSwitchableInterpFilterOrder is the realtime (nonrd) per-mode
// filter sweep. libvpx's vp9_pickmode.c::search_filter_ref iterates
// filter_start..filter_end where filter_end is EIGHTTAP_SMOOTH (NOT
// EIGHTTAP_SHARP) — the realtime path never evaluates EIGHTTAP_SHARP.
//
// libvpx: vp9/encoder/vp9_pickmode.c:1523-1525
//
//	INTERP_FILTER filter_start = force_smooth_filter ? EIGHTTAP_SMOOTH : EIGHTTAP;
//	INTERP_FILTER filter_end = EIGHTTAP_SMOOTH;
//	for (filter = filter_start; filter <= filter_end; ++filter) {
var vp9NonrdSwitchableInterpFilterOrder = [...]vp9dec.InterpFilter{
	vp9dec.InterpEighttap,
	vp9dec.InterpEighttapSmooth,
}

var (
	vp9EighttapInterpFilterOrder = [...]vp9dec.InterpFilter{vp9dec.InterpEighttap}
	vp9SmoothInterpFilterOrder   = [...]vp9dec.InterpFilter{vp9dec.InterpEighttapSmooth}
	vp9SharpInterpFilterOrder    = [...]vp9dec.InterpFilter{vp9dec.InterpEighttapSharp}
	vp9BilinearInterpFilterOrder = [...]vp9dec.InterpFilter{vp9dec.InterpBilinear}
)

func vp9InterpFilterOrderForSingle(filter vp9dec.InterpFilter) []vp9dec.InterpFilter {
	switch filter {
	case vp9dec.InterpEighttapSmooth:
		return vp9SmoothInterpFilterOrder[:]
	case vp9dec.InterpEighttapSharp:
		return vp9SharpInterpFilterOrder[:]
	case vp9dec.InterpBilinear:
		return vp9BilinearInterpFilterOrder[:]
	default:
		return vp9EighttapInterpFilterOrder[:]
	}
}

// vp9NonrdFilterRef mirrors libvpx's filter_ref derivation in
// vp9_pickmode.c:1874-1880. filter_ref starts as cm->interp_filter; when
// sf.default_interp_filter != BILINEAR, it is overwritten from the first
// inter neighbour (above, then left). The result is consumed by the
// per-mode filter gate at vp9_pickmode.c:2318-2330 and by the non-search
// branch at :2330.
//
// libvpx: vp9/encoder/vp9_pickmode.c:1874-1880
//
//	filter_ref = cm->interp_filter;
//	if (cpi->sf.default_interp_filter != BILINEAR) {
//	  if (xd->above_mi && is_inter_block(xd->above_mi))
//	    filter_ref = xd->above_mi->interp_filter;
//	  else if (xd->left_mi && is_inter_block(xd->left_mi))
//	    filter_ref = xd->left_mi->interp_filter;
//	}
func vp9NonrdFilterRef(frameInterp vp9dec.InterpFilter,
	defaultInterpFilter vp9dec.InterpFilter,
	above, left *vp9dec.NeighborMi,
) vp9dec.InterpFilter {
	filterRef := frameInterp
	if defaultInterpFilter != vp9dec.InterpBilinear {
		if above != nil && encoder.NeighborIsInter(above) {
			filterRef = vp9dec.InterpFilter(above.InterpFilter)
		} else if left != nil && encoder.NeighborIsInter(left) {
			filterRef = vp9dec.InterpFilter(left.InterpFilter)
		}
	}
	return filterRef
}

// vp9NonrdPredFilterSearch mirrors libvpx's pred_filter_search derivation
// in vp9_pickmode.c:1732 and 1862-1869. The base value is
// (cm->interp_filter == SWITCHABLE); when sf.cb_pred_filter_search is set,
// it is refined by a chessboard pattern keyed on
// (mi_row + mi_col) >> log2(mi_width(bsize)) + (current_video_frame & 1).
// For non-SWITCHABLE frames cb_pred_filter_search forces it to 0.
//
// libvpx: vp9/encoder/vp9_pickmode.c:1862-1869
//
//	if (cpi->sf.cb_pred_filter_search) {
//	  const int bsl = mi_width_log2_lookup[bsize];
//	  pred_filter_search = cm->interp_filter == SWITCHABLE
//	                           ? (((mi_row + mi_col) >> bsl) +
//	                              get_chessboard_index(cm->current_video_frame)) &
//	                                 0x1
//	                           : 0;
//	}
func vp9NonrdPredFilterSearch(frameInterp vp9dec.InterpFilter,
	cbPredFilterSearch int, miRow, miCol int,
	bsize common.BlockSize, frameIndex int,
) bool {
	predFilterSearch := frameInterp == vp9dec.InterpSwitchable
	if cbPredFilterSearch != 0 {
		if frameInterp != vp9dec.InterpSwitchable {
			return false
		}
		bsl := int(common.MiWidthLog2Lookup[bsize])
		chess := frameIndex & 0x1
		predFilterSearch = (((miRow+miCol)>>bsl)+chess)&0x1 != 0
	}
	return predFilterSearch
}

func vp9InterFrameInterpFilter(inter *vp9InterEncodeState) vp9dec.InterpFilter {
	if inter == nil {
		return vp9dec.InterpSwitchable
	}
	return inter.interpFilter
}

func vp9InterInterpFilterCandidates(inter *vp9InterEncodeState) []vp9dec.InterpFilter {
	switch vp9InterFrameInterpFilter(inter) {
	case vp9dec.InterpSwitchable:
		return vp9SwitchableInterpFilterOrder[:]
	case vp9dec.InterpEighttapSmooth:
		return vp9SmoothInterpFilterOrder[:]
	case vp9dec.InterpEighttapSharp:
		return vp9SharpInterpFilterOrder[:]
	case vp9dec.InterpBilinear:
		return vp9BilinearInterpFilterOrder[:]
	default:
		return vp9EighttapInterpFilterOrder[:]
	}
}

func vp9InterInterpFilterRateCost(inter *vp9InterEncodeState, fc *vp9dec.FrameContext,
	ctx int, filter vp9dec.InterpFilter,
) int {
	if vp9InterFrameInterpFilter(inter) != vp9dec.InterpSwitchable {
		return 0
	}
	return encoder.SwitchableInterpRateCost(fc, ctx, filter)
}

const vp9UnsetFilterRDScore = ^uint64(0)

const (
	vp9MaxFilterDiff = int64(^uint64(0) >> 1)
	vp9MinFilterDiff = -vp9MaxFilterDiff - 1
)

func vp9InitFilterRDScores(scores *[vp9dec.SwitchableFilterContexts]uint64) {
	if scores == nil {
		return
	}
	for i := range scores {
		scores[i] = vp9UnsetFilterRDScore
	}
}

func vp9RecordFilterRDScore(scores *[vp9dec.SwitchableFilterContexts]uint64,
	filter vp9dec.InterpFilter, fixedScore, switchableScore uint64,
) {
	if scores == nil || filter >= vp9dec.InterpBilinear {
		return
	}
	filterIdx := int(filter)
	if fixedScore < scores[filterIdx] {
		scores[filterIdx] = fixedScore
	}
	if switchableScore < scores[vp9dec.SwitchableFilters] {
		scores[vp9dec.SwitchableFilters] = switchableScore
	}
}

func vp9FilterDiffFromScores(bestScore, filterScore uint64) int64 {
	if bestScore >= filterScore {
		delta := bestScore - filterScore
		if delta > uint64(vp9MaxFilterDiff) {
			return vp9MaxFilterDiff
		}
		return int64(delta)
	}
	delta := filterScore - bestScore
	if delta >= uint64(vp9MaxFilterDiff)+1 {
		return vp9MinFilterDiff
	}
	return -int64(delta)
}

func (e *VP9Encoder) vp9ShouldCollectInterFilterRD(inter *vp9InterEncodeState,
	useNonrd bool,
) bool {
	if e == nil || inter == nil {
		return false
	}
	if e.sf.FrameParameterUpdate == 0 || useNonrd {
		return false
	}
	if inter.counts == nil {
		return false
	}
	return vp9InterFrameInterpFilter(inter) == vp9dec.InterpSwitchable
}

func (e *VP9Encoder) vp9StoreBlockFilterRDScores(
	scores *[vp9dec.SwitchableFilterContexts]uint64,
) {
	if e == nil || scores == nil {
		return
	}
	e.vp9BlockFilterRDScores = *scores
	e.vp9BlockFilterRDValid = true
}

func (e *VP9Encoder) vp9ClearBlockFilterRDScores() {
	if e == nil {
		return
	}
	e.vp9BlockFilterRDValid = false
	e.vp9BlockFilterRDScores = [vp9dec.SwitchableFilterContexts]uint64{}
}

func (e *VP9Encoder) vp9AccumulateBlockFilterDiff(inter *vp9InterEncodeState,
	bestScore uint64, skip bool,
) {
	if e == nil || inter == nil || !e.vp9BlockFilterRDValid {
		return
	}
	if inter.counts == nil || skip {
		e.vp9ClearBlockFilterRDScores()
		return
	}
	for i, score := range e.vp9BlockFilterRDScores {
		if score == vp9UnsetFilterRDScore {
			continue
		}
		e.vp9FilterDiff[i] += vp9FilterDiffFromScores(bestScore, score)
	}
	e.vp9ClearBlockFilterRDScores()
}

func addVP9FilterDiff(dst, src *[vp9dec.SwitchableFilterContexts]int64) {
	if dst == nil || src == nil {
		return
	}
	for i := range dst {
		dst[i] += src[i]
	}
}

func vp9MvHasSubpel(mv vp9dec.MV) bool {
	return int(mv.Row)%8 != 0 || int(mv.Col)%8 != 0
}
