package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestCyclicRefreshSegmentationConfigUsesAltLFUnderAggressiveDenoise(t *testing.T) {
	e := VP8Encoder{}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	// Aggressive denoise (mode 3) brings consec_zerolast=15 and qp_thresh=80.
	// Pick Q below qp_thresh and frames_since_key past 2*consec_zerolast=30.
	e.opts.NoiseSensitivity = 3
	e.denoiser.allocated = true
	e.denoiser.mode, e.denoiser.params = vp8enc.DenoiserSetParameters(vp8enc.DenoiserModeForSensitivity(e.opts.NoiseSensitivity))
	e.rc.currentQuantizer = 40
	e.rc.framesSinceKeyframe = 100
	cfg := e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("aggressive-denoise cyclic segmentation disabled, want enabled with alt-LF")
	}
	if cfg.FeatureEnabled[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID] {
		t.Fatalf("aggressive-denoise alt-Q feature still set, want suppressed in favour of alt-LF")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] {
		t.Fatalf("aggressive-denoise alt-LF feature = false, want enabled")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID]; got != -40 {
		t.Fatalf("aggressive-denoise alt-LF delta = %d, want libvpx -40", got)
	}

	// Q at or above qp_thresh: alt-Q path resumes.
	e.rc.currentQuantizer = 80
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] {
		t.Fatalf("Q>=qp_thresh alt-LF still set, want libvpx fallback to alt-Q delta")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID] {
		t.Fatalf("Q>=qp_thresh alt-Q feature = false, want enabled")
	}

	// Too soon after keyframe: alt-Q path resumes too.
	e.rc.currentQuantizer = 40
	e.rc.framesSinceKeyframe = 10
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if cfg.FeatureEnabled[vp8common.MBLvlAltLF][vp8enc.StaticSegmentID] {
		t.Fatalf("frames_since_key<=2*consec_zerolast alt-LF still set, want fallback to alt-Q")
	}
	if !cfg.FeatureEnabled[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID] {
		t.Fatalf("frames_since_key<=2*consec_zerolast alt-Q feature = false, want enabled")
	}
}

func TestCyclicRefreshSegmentationConfigDisabledUnderForceMaxQuantizer(t *testing.T) {
	e := VP8Encoder{}
	e.cyclicRefreshConfigured = true
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 30
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("baseline CBR cyclic segmentation disabled, want enabled")
	}
	e.forceMaxQuantizer = true
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("force_maxqp cyclic segmentation = %+v, want disabled per libvpx force_maxqp gate", cfg)
	}
	e.forceMaxQuantizer = false
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("after clearing force_maxqp cyclic segmentation disabled, want enabled")
	}
}

func TestDropEncodedFrameOvershootReadsCurrentPredictionError(t *testing.T) {
	e := VP8Encoder{}
	e.opts.ScreenContentMode = 2
	e.rc.mode = RateControlCBR
	e.rc.dropFrameAllowed = true
	e.rc.currentQuantizer = 40
	e.rc.maxQuantizer = vp8common.MaxQ
	e.rc.bitsPerFrame = 8000
	e.rc.bufferOptimalBits = 16000
	e.rc.bufferLevelBits = 2000
	e.framePredictionError = int64((200<<4)+1) * 10
	e.lastPredErrorMB = 100

	if !e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, 4000, 10, false) {
		t.Fatalf("overshoot drop = false, want true when current pred_err_mb crosses libvpx gates")
	}
	if !e.forceMaxQuantizer {
		t.Fatalf("forceMaxQuantizer = false, want true after overshoot drop")
	}
	if e.rc.bufferLevelBits != e.rc.bufferOptimalBits {
		t.Fatalf("buffer level = %d, want reset to optimal %d", e.rc.bufferLevelBits, e.rc.bufferOptimalBits)
	}
	if e.lastPredErrorMB != 100 {
		t.Fatalf("lastPredErrorMB changed inside drop helper to %d, want caller-owned value retained", e.lastPredErrorMB)
	}

	e = VP8Encoder{}
	e.opts.ScreenContentMode = 2
	e.opts.RTCExternalRateControl = true
	e.rc.mode = RateControlCBR
	e.rc.dropFrameAllowed = true
	e.rc.currentQuantizer = 40
	e.rc.maxQuantizer = vp8common.MaxQ
	e.rc.bitsPerFrame = 8000
	e.rc.bufferOptimalBits = 16000
	e.rc.bufferLevelBits = 2000
	e.framePredictionError = int64((200<<4)+1) * 10
	e.lastPredErrorMB = 100
	if e.vp8DropEncodedframeOvershoot(e.rc.currentQuantizer, 4000, 10, false) {
		t.Fatalf("RTC external rate-control overshoot drop = true, want disabled")
	}
}

func TestCyclicRefreshSegmentTransitionsClearOnNonZeroLast(t *testing.T) {
	// updateCyclicRefreshMapFromInterFrame is the per-MB segment-transition
	// recorder. After a frame:
	//   - Refreshed segment-1 MBs become -1 (cooldown).
	//   - Cooldown counters increment; ZEROMV-LAST flips a 1 to 0 (eligible).
	//   - Anything else sets the entry to 1 (dirty).
	refreshMap := []int8{-1, 1, 0, -1}
	modes := []vp8enc.InterFrameMacroblockMode{
		// MB0 was in segment 1 → final state -1
		{SegmentID: vp8enc.StaticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		// MB1 ZEROMV-LAST flips dirty→eligible
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		// MB2 NewMV last → dirty (1)
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		// MB3 GOLDEN ZEROMV → dirty (1)
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}
	updateCyclicRefreshMapFromInterFrame(modes, refreshMap)
	want := []int8{-1, 0, 1, 1}
	for i := range want {
		if refreshMap[i] != want[i] {
			t.Fatalf("MB%d post-frame map = %d, want libvpx state %d", i, refreshMap[i], want[i])
		}
	}
}

// TestSetActiveMapOracleVectorPreservesEveryInactiveMB exercises a
// checkerboard active-map pattern and confirms libvpx's per-MB invariants
// across the whole frame: every inactive MB codes as ZEROMV-LAST with
// MBSkipCoeff=1 and segment 0, every inactive MB decodes back to the prior
// LAST reconstruction byte-for-byte, every active MB updates, and a second
// encode of the same source under the same active map is deterministic
// (decoder-stable). This is the active-map oracle vector for the
// single-threaded encodeframe path; govpx does not implement libvpx's
// row-threaded encodeframe loop so the threaded variant is N/A.
