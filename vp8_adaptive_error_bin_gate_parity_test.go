package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// TestVP8AdaptiveErrorBinGateFollowsCPISpeed verifies that the adaptive
// error-bin RD-threshold adjustment inside vp8_set_speed_features case 2
// (libvpx onyx_if.c:957-1010, `Speed > 6`) consults the SAME cpi->Speed
// as every other speed-conditioned feature: the deterministic
// auto-select model surfaced by libvpxCPUUsed().
//
// On the reference host the production vpxenc keeps cpi->Speed clamped
// at the realtime floor of 4 for cpu_used > 0 RT streams (per-frame
// encode time sits far below the ms_for_compress budget), so the
// error-bin path stays INERT -- and govpx's pinned autoSpeed trajectory
// must leave it inert too. A former "libvpx-realistic cpu_used+1"
// override at >= 1500 MBs forced the path active while the production
// vpxenc left it off; see the drop-parity audit note in
// vp8_encoder_config.go.
func TestVP8AdaptiveErrorBinGateFollowsCPISpeed(t *testing.T) {
	var refImg vp8common.Image
	refs := []interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &refImg},
		{Frame: vp8common.GoldenFrame, Img: &refImg},
	}

	// cpu_used=8 realtime, frameCount=2, autoSpeed pinned at 4, on a
	// 1280x720 = 3600-MB frame: Speed=4 <= 6, so the adaptive error-bin
	// path must stay inert (THR_NEAREST1/THR_NEAR1 keep their static 0
	// base), matching the production vpxenc trajectory.
	e := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 1280, Height: 720},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 2,
		autoSpeed:  4,
	}
	if got := e.libvpxCPUUsed(); got != 4 {
		t.Fatalf("libvpxCPUUsed = %d, want pinned autoSpeed=4 fixture precondition", got)
	}
	for i := range 1024 {
		e.interModeSpeedErrorBins[i] = 1
	}
	got := e.interModeRDThresholdsForReferences(40, refs, len(refs))
	if got[libvpxThrNearest1] != 0 || got[libvpxThrNear1] != 0 {
		t.Fatalf("THR_NEAREST1/THR_NEAR1 = %d/%d, want 0/0: Speed=4 <= 6 keeps the onyx_if.c:957-1010 adaptive path inert",
			got[libvpxThrNearest1], got[libvpxThrNear1])
	}

	// If auto-select ever evolves the Speed past 6 (autoSpeed=9), the
	// same uniform dispatch fires the error_bins[] scan: NEAREST1/NEAR1
	// pick up the thresh>>1 override (libvpx onyx_if.c:993-994).
	eRamped := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 1280, Height: 720},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 2,
		autoSpeed:  9,
	}
	for i := range 1024 {
		eRamped.interModeSpeedErrorBins[i] = 1
	}
	gotRamped := eRamped.interModeRDThresholdsForReferences(40, refs, len(refs))
	if gotRamped[libvpxThrNearest1] == 0 || gotRamped[libvpxThrNear1] == 0 {
		t.Fatalf("THR_NEAREST1/THR_NEAR1 = %d/%d, want non-zero adaptive override at Speed=9 (libvpx onyx_if.c:993-994)",
			gotRamped[libvpxThrNearest1], gotRamped[libvpxThrNear1])
	}
	if gotRamped[libvpxThrNearest1] != gotRamped[libvpxThrNear1] {
		t.Fatalf("THR_NEAREST1/THR_NEAR1 = %d/%d, libvpx onyx_if.c:993-994 forces them equal to thresh>>1",
			gotRamped[libvpxThrNearest1], gotRamped[libvpxThrNear1])
	}

	// cpu_used=0 realtime (the byte-parity-gated ladder) keeps Speed <= 6
	// under the pinned trajectory, preserving the threads=4 cpu=0 RT
	// byte-parity sentinels.
	eZero := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 0, Width: 320, Height: 240},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 2,
		autoSpeed:  4,
	}
	if got := eZero.libvpxCPUUsed(); got > 6 {
		t.Fatalf("cpu_used=0 RT Speed = %d, must be <= 6 to preserve byte-parity sentinels", got)
	}
}

// TestVP8ErrorBinGateSpeedFallsBackToCpiSpeed ensures
// context.errorBinGateSpeed defaults to "use cpiSpeed" when callers leave it
// zero. This preserves the existing threshold-table callers that route through
// the negative-cpu_used pass-through and the good-quality / best-quality paths
// that supply no errorBins[].
func TestVP8ErrorBinGateSpeedFallsBackToCpiSpeed(t *testing.T) {
	var bins [1024]uint32
	// errorBinGateSpeed == 0 (default): the gate must read cpiSpeed.
	// cpiSpeed=8 > 6 -> adaptive path fires, NEAREST2 becomes thresh.
	ctxDefault := libvpxInterModeThresholdContext{
		refFrameCount: 3,
		totalMBs:      100,
		errorBins:     &bins,
	}
	gotDefault := libvpxInterModeThresholdMultipliersForCPISpeed(DeadlineRealtime, 8, ctxDefault)
	if gotDefault[libvpxThrNearest2] == 0 {
		t.Fatalf("NEAREST2 with errorBinGateSpeed=0 cpiSpeed=8 = 0, want adaptive override (gate must default to cpiSpeed)")
	}
	// errorBinGateSpeed=4 (override below gate), cpiSpeed=8 above. The
	// override must take precedence, so the adaptive path skips and
	// NEAREST2 retains its znn=0 base assignment for slot 2 (znn at
	// continuousSpeed=15 -> znn map terminal entry).
	ctxOverride := libvpxInterModeThresholdContext{
		refFrameCount:     3,
		totalMBs:          100,
		errorBins:         &bins,
		errorBinGateSpeed: 4,
	}
	gotOverride := libvpxInterModeThresholdMultipliersForCPISpeed(DeadlineRealtime, 8, ctxOverride)
	if gotOverride[libvpxThrNearest1] != 0 {
		t.Fatalf("NEAREST1 with errorBinGateSpeed=4 override = %d, want adaptive path skipped (base znn / 0)", gotOverride[libvpxThrNearest1])
	}
	if gotOverride[libvpxThrNear1] != 0 {
		t.Fatalf("NEAR1 with errorBinGateSpeed=4 override = %d, want adaptive path skipped (base znn / 0)", gotOverride[libvpxThrNear1])
	}
}
