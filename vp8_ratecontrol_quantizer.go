package govpx

import vp8common "github.com/thesyncim/govpx/internal/vp8/common"

func (rc *rateControlState) selectQuantizerForFrame(keyFrame bool, macroblocks int) {
	rc.selectQuantizerForFrameKind(keyFrame, false, macroblocks)
}

func (rc *rateControlState) selectQuantizerForFrameKind(keyFrame bool, goldenFrame bool, macroblocks int) {
	rc.selectQuantizerForFrameKindWithScreenContent(keyFrame, goldenFrame, macroblocks, 0)
}

func (rc *rateControlState) selectQuantizerForFrameKindWithScreenContent(keyFrame bool, goldenFrame bool, macroblocks int, screenContentMode int) {
	rc.selectQuantizerForFrameKindWithAltRef(keyFrame, goldenFrame, false, macroblocks, screenContentMode)
}

// selectQuantizerForFrameKindWithAltRef extends
// selectQuantizerForFrameKindWithScreenContent with libvpx's
// `cm->refresh_alt_ref_frame` branch from
// `vp8/encoder/onyx_if.c:encode_frame_to_data_rate`. In one-pass mode, libvpx
// folds an ARF refresh into the same active-best/worst regulation path as a
// golden refresh: both arms gate on
// `(cm->refresh_golden_frame || cpi->common.refresh_alt_ref_frame)` with
// `oxcf.number_of_layers == 1`, and both consult `gf_high_motion_minq` for
// the active-best-quality floor. The split only matters for the
// `zbin_oq_high` cap (see libvpxZbinOverQuantHigh) and for the recode
// rate-correction-factor accounting (which is already keyed off
// `goldenFrame`). Pass `altRefFrame=true` from the encode driver when
// `cpi->common.refresh_alt_ref_frame` is set; pass `goldenFrame=true` when
// the encoder is producing an overlay or a regular GF refresh.
func (rc *rateControlState) selectQuantizerForFrameKindWithAltRef(keyFrame bool, goldenFrame bool, altRefFrame bool, macroblocks int, screenContentMode int) {
	if macroblocks <= 0 {
		return
	}
	if !rateControlModeUsesQuantizerRegulator(rc.mode) {
		return
	}
	targetBits := rc.frameTargetBits
	if targetBits <= 0 {
		targetBits = rc.bitsPerFrame
	}
	if targetBits <= 0 {
		return
	}
	rc.activeWorstQChanged = false
	gfOrArf := goldenFrame || altRefFrame
	correctionFactor := rc.rateCorrectionFactorForFrame(keyFrame, gfOrArf)
	activeBest, activeWorst := rc.libvpxActiveQuantizerBoundsForFrame(keyFrame, goldenFrame, altRefFrame)
	rc.activeBestQuantizer = activeBest
	rc.currentQuantizer, rc.currentZbinOverQuant = libvpxRegulatedQuantizerWithZbinAltRef(keyFrame, goldenFrame, altRefFrame, targetBits, macroblocks, activeBest, activeWorst, correctionFactor)
	if rc.cqFloorActive() && !keyFrame && !gfOrArf {
		// libvpx vp8/encoder/onyx_if.c lines 3727-3739: for one-pass
		// CQ (USAGE_CONSTRAINED_QUALITY) on the ni_frames<=150 branch,
		// active_best_quality stays at cpi->best_quality for KF/GF/ARF
		// frames and is only floored to cq_target_quality for inter
		// non-refresh frames. govpx mirrors that floor through
		// libvpxActiveQuantizerBoundsForFrame; do not re-apply the
		// cq_level clamp here for KF/GF/ARF or those reference frames
		// get over-quantized relative to libvpx (oracle parity gap on
		// realtime-cq-cpu0-16x16-cq20).
		rc.currentQuantizer = rc.clampedCQQuantizerValue(rc.currentQuantizer)
		if rc.currentQuantizer < vp8common.MaxQ {
			rc.currentZbinOverQuant = 0
		}
	}
	rc.clampQuantizer()
	if rc.mode == RateControlCBR && screenContentMode > 0 && !keyFrame {
		rc.currentQuantizer = libvpxLimitCBRInterQuantizerDrop(rc.lastInterQuantizer, rc.currentQuantizer)
	}
	if rc.currentQuantizer < vp8common.MaxQ {
		rc.currentZbinOverQuant = 0
	}
}

// libvpxActiveQuantizerBoundsForFrame ports the active-best/worst-Q selection
// at `vp8/encoder/onyx_if.c:3616-3750`. The ARF refresh case follows the
// single-layer GF branch (`cm->refresh_golden_frame ||
// cpi->common.refresh_alt_ref_frame`) which uses gf_high_motion_minq for the
// one-pass active-best floor and may pull `Q` toward `cpi->avg_frame_qindex`
// when it is below `active_worst_quality`. For altRefFrame=true callers, the
// branch fires regardless of `goldenFrame` so the caller can drive a hidden
// ARF without first marking the source frame as a golden refresh.
func (rc *rateControlState) libvpxActiveQuantizerBoundsForFrame(keyFrame bool, goldenFrame bool, altRefFrame bool) (int, int) {
	activeWorst := rc.libvpxActiveWorstQuantizerForFrame(keyFrame)
	if rc.mode == RateControlCBR && rc.bufferOptimalBits > 0 && rc.bufferLevelBits >= rc.bufferOptimalBits {
		activeWorst = rc.libvpxCBRFullBufferActiveWorst(activeWorst)
	}
	activeWorst = rc.clampedQuantizerValue(activeWorst)

	gfOrArf := goldenFrame || altRefFrame
	activeBest := clampQuantizerValue(rc.activeBestQuantizer, rc.minQuantizer, rc.maxQuantizer)
	if !keyFrame && rc.bufferOptimalBits > 0 {
		activeBest = rc.minQuantizer
	}
	// libvpx vp8/encoder/onyx_if.c line 3619 gates the active-best
	// branches on `(cpi->pass == 2) || (cpi->ni_frames > 150)`. govpx
	// historically only honored the ni_frames>150 arm; for pass-2 we
	// also enable the libvpxKeyFrameHighMotionMinQ / GoldenMinQ /
	// InterMinQ floor lookups so the regulator sees the same active
	// best-Q lower bound libvpx does. Without this, pass-2 inter
	// frames pick Q values much lower than libvpx (q_match=8% on
	// desktopqvga vs 100% target_match) because the regulator's
	// activeBest stays at minQuantizer.
	pass2 := rc.pass2ActiveWorstQValid
	if rc.normalInterFrames > 150 || pass2 {
		q := clampQuantizerValue(activeWorst, 0, vp8common.MaxQ)
		switch {
		case keyFrame:
			// libvpx pass-2 KF branch (onyx_if.c lines 3624-3630):
			// kf_low_motion_minq when gfu_boost > 600, else
			// kf_high_motion_minq. The ni_frames>150 fallthrough that
			// govpx also enters here matches libvpx's one-pass branch
			// at line 3646, which unconditionally uses
			// kf_high_motion_minq (the >600 split is pass-2 only). So
			// only consult gfuBoost when the pass-2 driver has armed
			// it (gfuBoostValid && pass2); otherwise stay on the
			// conservative high-motion table.
			if pass2 && rc.gfuBoostValid && rc.gfuBoost > 600 {
				activeBest = libvpxKeyFrameLowMotionMinQ[q]
			} else {
				activeBest = libvpxKeyFrameHighMotionMinQ[q]
			}
			// libvpx vp8/encoder/onyx_if.c:3636-3642 forced-key
			// active-best clamp. When the current KF was emitted only
			// because we hit the maximum key-frame interval (not
			// scene-cut), libvpx pins active_best into
			// [avg_frame_qindex >> 2, avg_frame_qindex * 7 / 8] to
			// keep it close to the surrounding inter Q. govpx's
			// pass-2 KF branch above always falls through to the
			// high-motion lookup, so without this clamp a forced KF
			// can sit at active_best=0 while the inter run is at
			// avg_frame_qindex=40, causing the regulator to pick a
			// Q lower than libvpx by ~10 qindices on every
			// max-interval forced KF.
			if pass2 && rc.thisKeyFrameForced && rc.avgFrameQuantizer > 0 {
				avg := rc.avgFrameQuantizer
				if activeBest > (avg*7)/8 {
					activeBest = (avg * 7) / 8
				} else if activeBest < (avg >> 2) {
					activeBest = avg >> 2
				}
			}
		case gfOrArf && rc.currentTemporalLayers <= 1:
			if rc.framesSinceKeyframe > 1 && rc.avgFrameQuantizer < q {
				q = rc.avgFrameQuantizer
			}
			if rc.cqFloorActive() && q < rc.cqLevel {
				q = rc.cqLevel
			}
			q = clampQuantizerValue(q, 0, vp8common.MaxQ)
			// libvpx vp8/encoder/onyx_if.c:3667-3674 pass-2 GF branch:
			//   if (cpi->gfu_boost > 1000)        gf_low_motion_minq[Q]
			//   else if (cpi->gfu_boost < 400)    gf_high_motion_minq[Q]
			//   else                              gf_mid_motion_minq[Q]
			// The matching one-pass arm at line 3683 unconditionally
			// uses gf_high_motion_minq, so the >1000 / <400 split is
			// pass-2 only. Match libvpx by gating on
			// (pass2 && gfuBoostValid); on the ni_frames>150 one-pass
			// fallthrough path stay on the conservative high-motion
			// table.
			if pass2 && rc.gfuBoostValid {
				switch {
				case rc.gfuBoost > 1000:
					activeBest = libvpxGoldenFrameLowMotionMinQ[q]
				case rc.gfuBoost < 400:
					activeBest = libvpxGoldenFrameHighMotionMinQ[q]
				default:
					activeBest = libvpxGoldenFrameMidMotionMinQ[q]
				}
			} else {
				activeBest = libvpxGoldenFrameHighMotionMinQ[q]
			}
			// libvpx vp8/encoder/onyx_if.c:3677-3679 pass-2 CQ GF/ARF
			// "slightly lower active best" lowering. After the
			// gf_*_motion_minq lookup, pass-2 CQ refreshes drop
			// active_best by a factor of 15/16 so the GF/ARF carries
			// slightly more quality than the surrounding inter run.
			// Gated on pass==2 in libvpx (the branch is inside the
			// `cpi->pass == 2` arm at line 3667); govpx mirrors with
			// pass2ActiveWorstQValid since that is govpx's pass-2
			// surface.
			if pass2 && rc.cqFloorActive() {
				activeBest = activeBest * 15 / 16
			}
		default:
			activeBest = libvpxInterMinQ[q]
			if rc.cqFloorActive() && activeBest < rc.cqLevel {
				activeBest = rc.cqLevel
			}
		}
		if rc.mode == RateControlCBR {
			activeBest = rc.libvpxCBRFullBufferActiveBest(activeBest)
		}
	} else if rc.cqFloorActive() {
		if keyFrame || gfOrArf {
			activeBest = rc.minQuantizer
		} else if activeBest < rc.cqLevel {
			activeBest = rc.cqLevel
		}
	}

	activeBest = rc.clampedQuantizerValue(activeBest)
	// Two distinct libvpx bumps clamp the active_worst / active_best pair.
	//
	//  (a) vp8/encoder/ratectrl.c:833-834: when they collapse
	//      (`active_worst <= active_best`), bump
	//      `active_worst = active_best + 1`. This fires inside
	//      calc_pframe_target_size which is gated on `cpi->pass == 0`
	//      (ratectrl.c:690) — the one-pass / pass-0 path only. Pass-2
	//      frames go through the `this_frame_target = per_frame_bandwidth`
	//      arm at ratectrl.c:590 and never enter this recompute.
	//
	//  (b) vp8/encoder/onyx_if.c:3748-3750: runs for every frame
	//      (pass-0 and pass-2) inside encode_frame_to_data_rate:
	//      `if (active_worst < active_best) active_worst = active_best;`
	//      A strict `<` test with `=` assignment, NOT the `<=` / `+1`
	//      of (a). This is the libvpx-universal safety clamp.
	//
	// govpx historically applied (a) unconditionally with `<=` / `+1`.
	// That matched libvpx for one-pass but propagated a stale-by-1 Q
	// ceiling on every pass-2 KF group when the regulator picked
	// `tmp_q == active_best_quality` (e.g. the
	// regression_twopass_6573b9b5 fuzz seed: mid-stream KF=4 + cpu=4
	// + 500kbps lands tmp_q=4 == best_quality=4, which the unconditional
	// +1 bump shifted to 5 and propagated to every inter frame in the KF
	// group, producing a one-byte second-partition divergence).
	//
	// Split the two bumps: gate (a) on `!pass2ActiveWorstQValid`
	// (one-pass surfaces only) and apply (b) for all frames as a
	// `<` / `=` clamp.
	if !rc.pass2ActiveWorstQValid {
		if activeWorst <= activeBest {
			activeWorst = activeBest + 1
		}
	} else if activeWorst < activeBest {
		activeWorst = activeBest
	}
	if activeWorst > vp8common.MaxQ {
		activeWorst = vp8common.MaxQ
	}
	rc.activeWorstQuantizer = activeWorst
	return activeBest, activeWorst
}

// libvpxActiveWorstQuantizerForFrame ports both libvpx active-worst-quality
// pathways:
//
//   - For inter frames (keyFrame=false): vp8/encoder/ratectrl.c
//     `calc_pframe_target_size` (lines 690-837) scales active_worst_quality
//     from ni_av_qi up to worst_quality based on buffer fullness.
//
//   - For keyframes (keyFrame=true): vp8/encoder/ratectrl.c
//     `calc_iframe_target_size` line 374 unconditionally resets
//     `cpi->active_worst_quality = cpi->worst_quality` in one-pass mode
//     (pass != 2). The buffer-based P-frame formula does NOT run for KFs,
//     so a long inter run that drifted ni_av_qi to a low value (e.g. 14
//     on splitmv VBR 700kbps tight-buf good-quality) must not propagate
//     into the keyframe's active-worst floor — otherwise the KF's
//     active_best (read from kf_high_motion_minq[active_worst]) collapses
//     to a tiny value and the keyframe is encoded at Q≈4 instead of the
//     libvpx Q≈20, exploding the bitstream and breaking byte parity at
//     the next forced-KF boundary (long-fixture splitmv fuzz frame 180).
func (rc *rateControlState) libvpxActiveWorstQuantizerForFrame(keyFrame bool) int {
	activeWorst := rc.maxQuantizer
	// libvpx vp8/encoder/firstpass.c vp8_second_pass first-frame branch
	// (lines 2349-2363) overwrites active_worst_quality with the
	// estimate_max_q result, then damps it on subsequent frames
	// (lines 2381-2392). When the pass-2 driver has seeded the
	// override we substitute it here so the regulator's worst-Q
	// ceiling matches libvpx's. The override is clamped to the
	// user-configured [minQuantizer, maxQuantizer] envelope to honor
	// CLI / public-API bounds.
	if rc.pass2ActiveWorstQValid {
		override := max(min(rc.pass2ActiveWorstQOverride, rc.maxQuantizer), rc.minQuantizer)
		activeWorst = override
	}
	// libvpx calc_iframe_target_size line 374: one-pass KFs reset
	// active_worst_quality to cpi->worst_quality (== maxQuantizer in
	// internal Q-index space). Skip the P-frame buffer-based formula
	// entirely. The pass-2 branch (pass2ActiveWorstQValid) preserves the
	// twoPassState-driven override above; the one-pass keyframe branch
	// pins to maxQuantizer here so the subsequent kf_high_motion_minq
	// lookup in libvpxActiveQuantizerBoundsForFrame sees the libvpx-side
	// active_worst value, not the inter-tracked ni_av_qi.
	if keyFrame && !rc.pass2ActiveWorstQValid {
		if rc.cqFloorActive() && activeWorst < rc.cqLevel {
			activeWorst = rc.cqLevel
		}
		return activeWorst
	}
	// libvpx vp8/encoder/ratectrl.c lines 690-837 cover both CBR
	// (USAGE_STREAM_FROM_SERVER) and VBR/local-file when `buffered_mode`
	// is enabled (optimal_buffer_level > 0). The active-worst formula
	// applies whenever `auto_worst_q && ni_frames > 150`, regardless of
	// the end_usage. The CBR-only details (using min(buffer_level,
	// bits_off_target) as critical_buffer_level instead of just
	// bits_off_target) do not affect this port because govpx tracks
	// `bufferLevelBits` as the bits_off_target equivalent (see
	// vp8_ratecontrol_postencode.go:566). Keep the CBR-only adjustment in
	// libvpxCBRFullBufferActiveWorst, but the base formula must run for
	// both modes — otherwise VBR encoders return maxQuantizer here even
	// when libvpx is locked to ni_av_qi, and the regulator's worst-Q
	// ceiling diverges (oracle parity gap on long-fixture VBR seed
	// fuzz-long-rc-847b68e0 frame 173).
	if !rateControlModeUsesQuantizerRegulator(rc.mode) || rc.normalInterFrames <= 150 || rc.bufferOptimalBits <= 0 {
		if rc.cqFloorActive() && activeWorst < rc.cqLevel {
			activeWorst = rc.cqLevel
		}
		return activeWorst
	}
	if rc.bufferLevelBits >= rc.bufferOptimalBits {
		activeWorst = rc.normalInterAvgQuantizer
	} else if rc.bufferLevelBits > rc.bufferOptimalBits>>2 {
		denom := (rc.bufferOptimalBits * 3) >> 2
		if denom > 0 {
			qadjustmentRange := rc.maxQuantizer - rc.normalInterAvgQuantizer
			aboveBase := rc.bufferLevelBits - (rc.bufferOptimalBits >> 2)
			activeWorst = rc.maxQuantizer - int((int64(qadjustmentRange)*int64(aboveBase))/int64(denom))
		}
	}
	// libvpx vp8/encoder/ratectrl.c:849-852: at the end of
	// `calc_pframe_target_size` for one-pass mode, an unconditional CQ
	// floor lifts active_worst_quality up to cq_target_quality when it
	// drops below it. Govpx's earlier-return branches (KF + !pass2,
	// non-regulator / ni<=150 / unbuffered) already apply this floor; the
	// buffered inter-ni>150 branch above can leave activeWorst at
	// normalInterAvgQuantizer or the buffer-fullness-scaled value, both of
	// which can fall below cqLevel after a long low-Q run. Without this
	// floor the regulator's worst-Q ceiling diverges from libvpx for CQ
	// mode once normalInterAvgQuantizer drifts under cq_target_quality.
	if rc.cqFloorActive() && activeWorst < rc.cqLevel {
		activeWorst = rc.cqLevel
	}
	return activeWorst
}

// libvpxCBRFullBufferActiveWorst ports the
// `(end_usage == USAGE_STREAM_FROM_SERVER) && (buffer_level >= optimal) &&
// buffered_mode` arm at vp8/encoder/onyx_if.c:3585-3614. The caller already
// gated on `buffer_level >= optimal`; this routine computes the
// active-worst-Q drop that libvpx applies when the buffer is at or above
// optimal. The inner `if (buffer_level < maximum_buffer_size)` branch is
// the only place libvpx scales the adjustment by buffer fullness; when the
// buffer is at or above maximum (including the degenerate
// max==optimal==buffer_level case the 1ms buffer model produces), libvpx
// skips that branch and subtracts the full `active_worst_quality / 4`. The
// old govpx guard `if maximumBufferBits <= bufferOptimalBits { return }`
// short-circuited that path, leaving active_worst pinned at the user
// max-Q (oracle parity gap on buffer-1-1-1).
func (rc *rateControlState) libvpxCBRFullBufferActiveWorst(activeWorst int) int {
	adjustment := activeWorst / 4
	if adjustment <= 0 {
		return activeWorst
	}
	if rc.bufferLevelBits < rc.maximumBufferBits {
		if rc.maximumBufferBits <= rc.bufferOptimalBits {
			// Degenerate band (max <= optimal) but buffer below max:
			// libvpx's `(max - optimal) / Adjustment` would underflow
			// or trip a divide-by-zero. Mirror the buff_lvl_step==0
			// fall-through by zeroing the adjustment.
			adjustment = 0
		} else {
			bufferLevelStep := (rc.maximumBufferBits - rc.bufferOptimalBits) / adjustment
			if bufferLevelStep > 0 {
				adjustment = (rc.bufferLevelBits - rc.bufferOptimalBits) / bufferLevelStep
			} else {
				adjustment = 0
			}
		}
	}
	return activeWorst - adjustment
}

func (rc *rateControlState) libvpxCBRFullBufferActiveBest(activeBest int) int {
	if rc.bufferOptimalBits <= 0 || rc.maximumBufferBits <= rc.bufferOptimalBits {
		return activeBest
	}
	switch {
	case rc.bufferLevelBits >= rc.maximumBufferBits:
		return rc.minQuantizer
	case rc.bufferLevelBits > rc.bufferOptimalBits:
		fraction := int((int64(rc.bufferLevelBits-rc.bufferOptimalBits) * 128) / int64(rc.maximumBufferBits-rc.bufferOptimalBits))
		minQAdjustment := ((activeBest - rc.minQuantizer) * fraction) / 128
		return activeBest - minQAdjustment
	default:
		return activeBest
	}
}

func (rc *rateControlState) clampBuffer() {
	if rc.bufferLevelBits > rc.maximumBufferBits {
		rc.bufferLevelBits = rc.maximumBufferBits
	}
}

func (rc *rateControlState) clampQuantizer() {
	rc.currentQuantizer = min(max(rc.currentQuantizer, rc.minQuantizer), rc.maxQuantizer)
}
