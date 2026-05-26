package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// prepareOnePassCBRCyclicGoldenFrame mirrors libvpx
// vp9_rc_get_one_pass_cbr_params (vp9_ratectrl.c:2518-2529): when
// frames_till_gf_update_due == 0 on a cyclic-refresh CBR inter frame,
// install baseline_gf_interval via vp9_cyclic_refresh_set_golden_update
// and schedule a golden refresh for this frame.
func (rc *vp9RateControlState) prepareOnePassCBRCyclicGoldenFrame(
	isKey, intraOnly bool,
	aqMode VP9AQMode,
	cyclic *encoder.CyclicRefreshState,
	gfCBRBoostPct int,
	extRefreshPending bool,
) {
	rc.refreshGoldenFrame = false
	if rc == nil || !rc.enabled || rc.mode != RateControlCBR || isKey || intraOnly {
		return
	}
	if aqMode != VP9AQCyclicRefresh || cyclic == nil || !cyclic.Enabled {
		return
	}
	if extRefreshPending || gfCBRBoostPct != 0 {
		return
	}
	if rc.framesTillGFUpdateDue != 0 {
		return
	}
	interval := cyclic.SetGoldenUpdate(encoder.CyclicRefreshSetGoldenUpdateArgs{
		RateControlIsVBR:  false,
		AvgFrameLowMotion: rc.avgFrameLowMotion,
		FramesSinceKey:    int(rc.framesSinceKey),
	})
	if interval <= 0 {
		interval = 40
	}
	rc.baselineGFInterval = uint8(min(interval, 255))
	rc.framesTillGFUpdateDue = interval
	if rc.framesToKey > 0 && rc.framesTillGFUpdateDue > rc.framesToKey {
		rc.framesTillGFUpdateDue = rc.framesToKey
	}
	rc.refreshGoldenFrame = true
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
	if rc.framesTillGFUpdateDue > 0 {
		rc.framesTillGFUpdateDue--
	}
	if refreshGolden || refreshAlt {
		return
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
