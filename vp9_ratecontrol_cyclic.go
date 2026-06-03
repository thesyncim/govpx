package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// prepareOnePassCBRCyclicGoldenFrame mirrors libvpx
// vp9_rc_get_one_pass_cbr_params (vp9_ratectrl.c:2518-2530): when
// frames_till_gf_update_due == 0, install baseline_gf_interval. For the
// CYCLIC_REFRESH_AQ mode the interval comes from
// vp9_cyclic_refresh_set_golden_update (vp9_ratectrl.c:2519-2520); otherwise
// it is the midpoint of the configured GF-interval range,
// (min_gf_interval + max_gf_interval) / 2 (vp9_ratectrl.c:2521-2523). libvpx
// then seeds frames_till_gf_update_due, clamps it to frames_to_key, and sets
// refresh_golden_frame for both key and inter frames; only inter frames use
// the scheduled bit in the refresh mask, since the key path already refreshes
// all references. Seeding the countdown on the key frame is required so the
// first inter frame does not immediately re-trigger a GF refresh.
func (rc *vp9RateControlState) prepareOnePassCBRCyclicGoldenFrame(
	isKey, intraOnly bool,
	aqMode VP9AQMode,
	cyclic *encoder.CyclicRefreshState,
	gfCBRBoostPct int,
	extRefreshPending bool,
) {
	rc.refreshGoldenFrame = false
	if rc == nil || !rc.enabled || rc.mode != RateControlCBR || intraOnly {
		return
	}
	if extRefreshPending || gfCBRBoostPct != 0 {
		return
	}
	if rc.framesTillGFUpdateDue != 0 {
		return
	}
	cyclicEnabled := aqMode == VP9AQCyclicRefresh && cyclic != nil && cyclic.Enabled
	var interval int
	if cyclicEnabled {
		// vp9_ratectrl.c:2519-2520.
		interval = cyclic.SetGoldenUpdate(encoder.CyclicRefreshSetGoldenUpdateArgs{
			RateControlIsVBR:  false,
			AvgFrameLowMotion: rc.avgFrameLowMotion,
			FramesSinceKey:    int(rc.framesSinceKey),
		})
		if interval <= 0 {
			interval = 40
		}
	} else {
		// vp9_ratectrl.c:2521-2523: baseline_gf_interval =
		// (min_gf_interval + max_gf_interval) / 2. initOnePassVBRState already
		// computes that midpoint into baselineGFInterval using the same
		// min/max defaults from vp9_rc_set_gf_interval_range.
		interval = int(rc.baselineGFInterval)
		if interval <= 0 {
			interval = (encoder.MinGFInterval + encoder.MaxGFInterval) >> 1
		}
	}
	rc.baselineGFInterval = uint8(min(interval, 255))
	rc.framesTillGFUpdateDue = interval
	if rc.framesToKey > 0 && rc.framesTillGFUpdateDue > rc.framesToKey {
		rc.framesTillGFUpdateDue = rc.framesToKey
	}
	if !isKey {
		rc.refreshGoldenFrame = true
	}
}

func (rc *vp9RateControlState) seedFramesToKey(maxKeyframeInterval int, isKey bool) {
	if isKey {
		if maxKeyframeInterval > 0 {
			rc.framesToKey = maxKeyframeInterval
		} else {
			rc.framesToKey = encoder.MaxGFInterval * 16
		}
	}
}

func (rc *vp9RateControlState) postOnePassCBRGoldenCadence(refreshFlags uint8) {
	if rc == nil || !rc.enabled || rc.mode != RateControlCBR {
		return
	}
	refreshGolden := refreshFlags&(1<<vp9GoldenRefSlot) != 0
	refreshAlt := refreshFlags&(1<<vp9AltRefSlot) != 0
	// libvpx update_golden_frame_stats (vp9_ratectrl.c:1759-1784): a golden
	// refresh still decrements frames_till_gf_update_due; only the non-golden
	// path bumps frames_since_golden.
	if refreshGolden {
		if rc.framesTillGFUpdateDue > 0 {
			rc.framesTillGFUpdateDue--
		}
		return
	}
	if refreshAlt {
		return
	}
	if rc.framesTillGFUpdateDue > 0 {
		rc.framesTillGFUpdateDue--
	}
}

// computeFrameLowMotion mirrors vp9_compute_frame_low_motion
// (vp9_ratectrl.c:1819-1837): percent of inter LAST blocks with |mv| < 16,
// EMA-smoothed into avg_frame_low_motion.
func (rc *vp9RateControlState) computeFrameLowMotion(miRows, miCols int, miAt func(miRow, miCol int) *vp9dec.NeighborMi) {
	if rc == nil || !rc.enabled || miRows <= 0 || miCols <= 0 || miAt == nil {
		return
	}
	zeroMV := 0
	total := miRows * miCols
	for miRow := range miRows {
		for miCol := range miCols {
			mi := miAt(miRow, miCol)
			if mi == nil || mi.RefFrame[0] != vp9dec.LastFrame {
				continue
			}
			mv := mi.Mv[0]
			if mv.Row > -16 && mv.Row < 16 && mv.Col > -16 && mv.Col < 16 {
				zeroMV++
			}
		}
	}
	if total <= 0 {
		return
	}
	pct := 100 * zeroMV / total
	rc.avgFrameLowMotion = (3*rc.avgFrameLowMotion + pct) >> 2
}

func (rc *vp9RateControlState) decrementFramesToKey(showFrame bool) {
	if rc == nil || !showFrame || rc.framesToKey <= 0 {
		return
	}
	rc.framesToKey--
}

// applyCyclicRefreshPostencodeResult mirrors the libvpx tail of
// vp9_cyclic_refresh_postencode (vp9_aq_cyclicrefresh.c:294-317) applied
// after the frame is coded and before reference buffers refresh: optional
// golden veto, resize-driven set_golden_update + forced golden refresh.
func (e *VP9Encoder) applyCyclicRefreshPostencodeResult(
	header *vp9dec.UncompressedHeader,
	res encoder.CyclicRefreshPostencodeResult,
) {
	if e == nil || header == nil {
		return
	}
	if res.ClearRefreshGolden {
		header.RefreshFrameFlags &^= 1 << vp9GoldenRefSlot
	}
	if res.SetGoldenUpdate {
		interval := e.cyclicAQ.SetGoldenUpdate(encoder.CyclicRefreshSetGoldenUpdateArgs{
			RateControlIsVBR:  e.rc.mode == RateControlVBR,
			AvgFrameLowMotion: e.rc.avgFrameLowMotion,
			FramesSinceKey:    int(e.rc.framesSinceKey),
		})
		if interval <= 0 {
			interval = 40
		}
		e.rc.baselineGFInterval = uint8(min(interval, 255))
		e.rc.framesTillGFUpdateDue = interval
		if e.rc.framesToKey > 0 && e.rc.framesTillGFUpdateDue > e.rc.framesToKey {
			e.rc.framesTillGFUpdateDue = e.rc.framesToKey
		}
	}
	if res.ForceGoldenRefresh {
		header.RefreshFrameFlags |= 1 << vp9GoldenRefSlot
	}
}
