package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

// TestVP8Task364AdaptiveErrorBinGateUsesRealisticSpeed pins the task #364
// targeted port: the adaptive error-bin RD-threshold adjustment inside
// vp8_set_speed_features case 2 (libvpx onyx_if.c:957-1010) must fire on
// the libvpx-realistic cpi->Speed convergence for cpu_used > 0 RT,
// even when govpx's timing-pinned e.autoSpeed stays low.
//
// Audit reference: same anti-pattern as task #350 (improved_mv_pred
// gate). libvpx runs vp8_set_speed_features every frame via
// vp8_initialize_rd_consts → rdopt.c:163, so at cpu_used > 0 RT cpi->
// Speed climbs to cpu_used+1 (audit-observed cpi_speed=9 by frame 2 on
// cpu=8 720p RT) and the line-957 gate fires the error_bins[] scan.
// govpx's e.autoSpeed evolution stays in the Speed=0 stable region
// under the task #278 interFrameAutoSpeedTimingCompensation pin, so
// without a targeted override the cpu>0 RT ladder never enters the
// adaptive-threshold path. Clamping e.autoSpeed itself would cascade
// every other Speed-conditioned feature simultaneously (see
// libvpxRealtimeCPISpeedForImprovedMVPredGate doc); the gate-only port
// matches libvpx's per-frame trajectory without disturbing the rest of
// the Speed-feature cascade.
func TestVP8Task364AdaptiveErrorBinGateUsesRealisticSpeed(t *testing.T) {
	// cpu_used=8 RT, frameCount=2, autoSpeed pinned at 4 (the post-#278
	// stable region). libvpxCPUUsed() returns autoSpeed=4 (≤6), so
	// without the gate-override the error-bin path stays inert.
	e := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 320, Height: 240},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 2,
		autoSpeed:  4,
	}
	if got := e.libvpxCPUUsed(); got != 4 {
		t.Fatalf("libvpxCPUUsed = %d, want pinned autoSpeed=4 fixture precondition", got)
	}
	if got := e.libvpxRealtimeCPISpeedForErrorBinGate(); got != 9 {
		t.Fatalf("libvpxRealtimeCPISpeedForErrorBinGate = %d, want libvpx-realistic cpu_used+1 = 9", got)
	}

	// Seed the previous-frame error_bins so the percentile-bisection
	// inside libvpxRealtimeAdaptiveInterModeThreshold has data to walk.
	// Distribute counts across the 1024-bin space so total_skip and the
	// (Speed-6)*remaining*0.1 cutoff land on a bin > minimum/128.
	for i := range 1024 {
		e.interModeSpeedErrorBins[i] = 1
	}

	// Build the picker context the way interModeRDThresholdsBaseline
	// does: two-reference inter frame (LAST + GOLDEN).
	var refImg vp8common.Image
	refs := []interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &refImg},
		{Frame: vp8common.GoldenFrame, Img: &refImg},
	}
	got := e.interModeRDThresholdsForReferences(40, refs, len(refs))

	// Without the task #364 gate-override, the path is skipped and the
	// THR_NEW1 / THR_NEAREST1 / THR_NEAR1 multipliers fall back to the
	// continuous speed_map outputs (which collapse to 2000 at the very
	// tail but DO NOT scale by the (Speed-6) error_bins percentile). The
	// presence of the adaptive path is detected by NEAREST1 / NEAR1
	// becoming non-zero (they are forced to 0 by the static
	// thresh_mult[THR_NEAREST1/NEAR1] = 0 base; only the adaptive path
	// overwrites them with thresh >> 1).
	if got[libvpxThrNearest1] == 0 || got[libvpxThrNear1] == 0 {
		t.Fatalf("THR_NEAREST1/THR_NEAR1 = %d/%d, want non-zero adaptive override (the libvpx onyx_if.c:993-994 path)",
			got[libvpxThrNearest1], got[libvpxThrNear1])
	}
	if got[libvpxThrNearest1] != got[libvpxThrNear1] {
		t.Fatalf("THR_NEAREST1/THR_NEAR1 = %d/%d, libvpx onyx_if.c:993-994 forces them equal to thresh>>1",
			got[libvpxThrNearest1], got[libvpxThrNear1])
	}

	// cpu_used=0 RT (the byte-parity-gated ladder) must NOT enter the
	// adaptive path: the realistic Speed stays at 4 (cold-start cap),
	// below the Speed > 6 gate. This preserves the threads=4 cpu=0 RT
	// byte-parity sentinels.
	eZero := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 0, Width: 320, Height: 240},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 2,
		autoSpeed:  4,
	}
	if got := eZero.libvpxRealtimeCPISpeedForErrorBinGate(); got > 6 {
		t.Fatalf("cpu_used=0 RT realistic Speed = %d, must be <= 6 to preserve byte-parity sentinels", got)
	}

	// Non-realtime deadlines must pass through unchanged (the libvpx
	// good-quality / best-quality ladder does not run vp8_auto_select_
	// speed and the line-957 path is realtime-gated).
	eGQ := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineGoodQuality, CpuUsed: 5, Width: 320, Height: 240},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 2,
		autoSpeed:  0,
	}
	if got, want := eGQ.libvpxRealtimeCPISpeedForErrorBinGate(), eGQ.libvpxCPUUsed(); got != want {
		t.Fatalf("good-quality deadline gate = %d, want libvpxCPUUsed pass-through %d", got, want)
	}

	// Pre-first-frame must pass through unchanged (no cpi->Speed
	// trajectory has been observed yet; libvpx's vp8_create_compressor
	// seeds cpi->Speed = oxcf.cpu_used at onyx_if.c:1706).
	eCold := &VP8Encoder{
		opts:       EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: 320, Height: 240},
		rc:         rateControlState{currentQuantizer: 40},
		frameCount: 0,
		autoSpeed:  8,
	}
	if got, want := eCold.libvpxRealtimeCPISpeedForErrorBinGate(), eCold.libvpxCPUUsed(); got != want {
		t.Fatalf("cold-start gate = %d, want libvpxCPUUsed pass-through %d", got, want)
	}
}

// TestVP8Task364ErrorBinGateSpeedFallsBackToCpiSpeed ensures the helper-
// added context.errorBinGateSpeed field defaults to "use cpiSpeed" when
// the caller leaves it zero. This preserves byte-parity for the
// libvpxInterModeThresholdMultipliersForContext callers that route via
// the -cpu_used negate-pass-through (TestLibvpxInterModeThreshold
// MultipliersApplyRealtimeErrorBins, the cpu_used=-8 fixture used as
// the autoSpeed=8 stand-in for the threshold table audits) and the
// good-quality / best-quality test paths that supply no errorBins[].
func TestVP8Task364ErrorBinGateSpeedFallsBackToCpiSpeed(t *testing.T) {
	var bins [1024]uint32
	// errorBinGateSpeed == 0 (default): the gate must read cpiSpeed.
	// cpiSpeed=8 > 6 → adaptive path fires, NEAREST2 becomes thresh.
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
	// continuousSpeed=15 → znn map terminal entry).
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
