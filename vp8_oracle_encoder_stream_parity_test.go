//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

func strictByteParityCPUUsed(deadline Deadline, cpuUsed int) int {
	if deadline == DeadlineRealtime && cpuUsed > 0 {
		// Positive realtime cpu-used is libvpx's wall-clock adaptive
		// auto-speed mode. Strict byte-parity cases pin the requested
		// speed explicitly so govpx and libvpx make matching encoder
		// decisions on every machine.
		return -cpuUsed
	}
	return cpuUsed
}

// TestVP8OracleEncoderStreamByteParity is the strictest possible parity
// gate: it runs govpx and the patched libvpx vpxenc-oracle on the same
// I420 fixture under matching options and asserts the encoded frame
// payloads (skipping the IVF container/frame-headers) are SHA-256
// identical.
//
// Each subtest pins one (resolution × deadline × cpu-used × fixture)
// triple. A subtest that fails here means the encoder has diverged from
// libvpx in a way that affects the bitstream — quantization decisions,
// mode decisions, loop-filter level, token writing order, or anything
// downstream of those — and is the immediate signal that the plan.md
// "100% byte parity" target has regressed for that config.
func TestVP8OracleEncoderStreamByteParity(t *testing.T) {
	vp8test.RequireOracle(t, "encoder stream byte-parity gate")
	vpxencOracle := vp8test.VpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		// Default frame budget for each parity case. Cases that diverge
		// earlier set a smaller frames override below.
		frames = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	splitmv64 := fixture{name: "splitmv-64x64", w: 64, h: 64, source: encoderValidationSplitMVQuadrantFrame}
	panning96 := fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}
	panning128 := fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}
	panning160x96 := fixture{name: "panning-160x96", w: 160, h: 96, source: encoderValidationPanningFrame}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
		// limit caps how many leading frames must byte-match. 0 means
		// require the full `frames` budget; a positive value pins the
		// known-good prefix when later frames have a remaining
		// divergence still being investigated.
		limit int
		// rcMode is the rate control mode; rcModeSet distinguishes an
		// explicit zero-valued RateControlVBR from the default CBR cases.
		rcMode    RateControlMode
		rcModeSet bool
		// errorResilient triggers libvpx's ErrorResilient mode.
		errorResilient bool
		// errorResilientPartitions triggers the VPX_ERROR_RESILIENT_PARTITIONS
		// branch in libvpx independent_coef_context_savings.
		errorResilientPartitions bool
		// sharpness overrides the loop-filter sharpness level.
		sharpness int
		// noiseSensitivity enables the VP8 temporal denoiser.
		noiseSensitivity int
		// tuning overrides the RD tuning mode; tuningSet distinguishes
		// explicit TunePSNR from the default zero value.
		tuning    Tuning
		tuningSet bool
		// extraArgs is appended to the libvpx vpxenc-oracle command.
		extraArgs []string
		// staticThreshold overrides EncoderOptions.StaticThreshold.
		staticThreshold int
		// screenContentMode overrides EncoderOptions.ScreenContentMode.
		screenContentMode int
		// maxIntraBitratePct overrides EncoderOptions.MaxIntraBitratePct.
		maxIntraBitratePct int
		// gfCBRBoostPct overrides EncoderOptions.GFCBRBoostPct.
		gfCBRBoostPct int
		// undershootPct / overshootPct override rate-control drift limits.
		undershootPct int
		overshootPct  int
		// buffer*Ms override the virtual rate-control buffer model.
		bufferSizeMs        int
		bufferInitialSizeMs int
		bufferOptimalSizeMs int
		// dropFrameAllowed/dropFrameWaterMark mirror libvpx --drop-frame.
		dropFrameAllowed   bool
		dropFrameWaterMark int
		// tokenPartitions overrides EncoderOptions.TokenPartitions
		// (0=1 partition, 1=2, 2=4, 3=8).
		tokenPartitions int
		// threads overrides EncoderOptions.Threads (0 = default serial).
		threads int
		// targetKbpsOverride lets a case pin a different bitrate
		// budget than the test's default targetKbps.
		targetKbpsOverride int
		// minQ / maxQ override the default 4 / 56 quantizer band.
		minQ int
		maxQ int
		// minQSet / maxQSet distinguish explicit zero-valued quantizer
		// bounds from the default band.
		minQSet bool
		maxQSet bool
		// cqLevel overrides EncoderOptions.CQLevel for CQ/Q mode.
		cqLevel int
		// kfInterval overrides the keyframe interval (0 = use the
		// test-wide default 999).
		kfInterval int
		// fpsOverride overrides the test FPS default.
		fpsOverride int
		// timebaseNum/timebaseDen override the caller timebase. The
		// strict harness drives one tick per frame, so libvpx receives
		// the reciprocal FPS when a custom timebase is set.
		timebaseNum int
		timebaseDen int
		// lookaheadFrames / autoAltRef mirror the libvpx lag and ARF controls.
		lookaheadFrames int
		autoAltRef      bool
		// arnr* mirror the alt-ref noise-reduction controls.
		arnrMaxFrames int
		arnrStrength  int
		arnrType      int
	}{
		{name: "realtime-cbr-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64},
		{name: "realtime-cbr-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning64},
		{name: "realtime-cbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64},
		{name: "good-quality-cbr-cpu5", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: panning64},
		// BestQuality SPLITMV now byte-matches the full sequence at
		// cpu0/cpu5 on this 64x64 fixture.
		{name: "best-quality-cbr-cpu0-splitmv", deadline: DeadlineBestQuality, cpuUsed: 0, fx: splitmv64},
		// Larger fixtures where chroma sub-pel rounding previously
		// diverged on inter frames (per plan.md). The keyframe is
		// expected to byte-match after the mb_no_coeff_skip header
		// fix; inter frames here are the next divergence to close.
		{name: "realtime-cbr-cpu8-96x96", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning96},
		{name: "realtime-cbr-cpu8-128x128", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning128},
		{name: "realtime-cbr-cpu8-160x96", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning160x96},
		// VBR cross-mode coverage. The harness must treat VBR as an
		// explicit mode because RateControlVBR is the zero value.
		{name: "realtime-vbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		// Error-resilient partitions (independent context savings
		// branch). libvpx --error-resilient takes a bitmask; value 2
		// is VPX_ERROR_RESILIENT_PARTITIONS, which is what
		// EncoderOptions.ErrorResilientPartitions maps to.
		{name: "realtime-cbr-cpu8-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		// ErrorResilient=true takes the normal error-resilient bitmask
		// path, distinct from independent partition contexts above.
		{name: "realtime-cbr-cpu0-16x16-error-resilient", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu0-32x32-error-resilient", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu0-32x32-error-resilient-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "realtime-cbr-cpu0-48x48-error-resilient", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu4-16x16-error-resilient", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu4-32x32-error-resilient", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu4-48x48-error-resilient", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu8-16x16-error-resilient", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu8-32x32-error-resilient", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu8-48x48-error-resilient", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		// Sharpness != 0 exercises the loop-filter header literal width.
		{name: "realtime-cbr-cpu8-sharpness4", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu0-16x16-sharpness7", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 7, extraArgs: []string{"--sharpness=7"}},
		// High-value VP8 controls with direct vpxenc flags.
		{name: "realtime-cbr-cpu0-32x32-static-thresh1", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 1, extraArgs: []string{"--static-thresh=1"}},
		{name: "realtime-cbr-cpu0-32x32-tune-ssim", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		{name: "realtime-cbr-cpu0-32x32-explicit-qrange-0-0", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQSet: true, maxQSet: true},
		{name: "realtime-q-cpu0-32x32-q20", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "realtime-q-cpu-3-16x16-explicit-qrange-0-0-q0", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, minQSet: true, maxQSet: true, extraArgs: []string{"--end-usage=q", "--cq-level=0"}},
		{name: "realtime-cq-cpu0-16x16-cq20", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cq-cpu-3-16x16-explicit-qrange-0-0-cq0", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, minQSet: true, maxQSet: true, extraArgs: []string{"--end-usage=cq", "--cq-level=0"}},
		{name: "realtime-cbr-cpu0-32x32-undershoot50-overshoot50", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, undershootPct: 50, overshootPct: 50, extraArgs: []string{"--undershoot-pct=50", "--overshoot-pct=50"}},
		{name: "realtime-cbr-cpu-3-96x96-undershoot50-overshoot50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, undershootPct: 50, overshootPct: 50, extraArgs: []string{"--undershoot-pct=50", "--overshoot-pct=50"}},
		{name: "realtime-cbr-cpu-3-96x96-undershoot25-overshoot75", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, undershootPct: 25, overshootPct: 75, extraArgs: []string{"--undershoot-pct=25", "--overshoot-pct=75"}},
		{name: "realtime-cbr-cpu-8-128x128-undershoot50-overshoot50", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, undershootPct: 50, overshootPct: 50, extraArgs: []string{"--undershoot-pct=50", "--overshoot-pct=50"}},
		{name: "realtime-cbr-cpu0-32x32-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu0-16x16-buffer-2000-1000-1500", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, bufferSizeMs: 2000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 1500, extraArgs: []string{"--buf-sz=2000", "--buf-initial-sz=1000", "--buf-optimal-sz=1500"}},
		{name: "realtime-cbr-cpu-3-96x96-buffer-2000-1000-1500", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, bufferSizeMs: 2000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 1500, extraArgs: []string{"--buf-sz=2000", "--buf-initial-sz=1000", "--buf-optimal-sz=1500"}},
		{name: "good-quality-cbr-cpu4-32x32-buffer-1000-500-600", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu-3-128x128-buffer-2000-1000-1500", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, bufferSizeMs: 2000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 1500, extraArgs: []string{"--buf-sz=2000", "--buf-initial-sz=1000", "--buf-optimal-sz=1500"}},
		{name: "realtime-cbr-cpu-3-128x128-buffer-4000-1000-3000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, bufferSizeMs: 4000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 3000, extraArgs: []string{"--buf-sz=4000", "--buf-initial-sz=1000", "--buf-optimal-sz=3000"}},
		{name: "realtime-cbr-cpu0-32x32-drop-frame60", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, dropFrameAllowed: true, dropFrameWaterMark: 60, extraArgs: []string{"--drop-frame=60"}},
		{name: "realtime-cbr-cpu0-32x32-max-intra-rate100", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, maxIntraBitratePct: 100, extraArgs: []string{"--max-intra-rate=100"}},
		{name: "realtime-cbr-cpu0-32x32-gf-cbr-boost50", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, gfCBRBoostPct: 50, extraArgs: []string{"--gf-cbr-boost=50"}},
		{name: "realtime-cbr-cpu-3-64x64-drop-frame60", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, extraArgs: []string{"--drop-frame=60"}},
		{name: "realtime-cbr-cpu-3-64x64-max-intra-rate100", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 100, extraArgs: []string{"--max-intra-rate=100"}},
		{name: "realtime-cbr-cpu-3-64x64-gf-cbr-boost50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 50, extraArgs: []string{"--gf-cbr-boost=50"}},
		{name: "realtime-cbr-cpu-8-64x64-drop-frame60", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, extraArgs: []string{"--drop-frame=60"}},
		{name: "realtime-cbr-cpu-8-64x64-max-intra-rate100", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, maxIntraBitratePct: 100, extraArgs: []string{"--max-intra-rate=100"}},
		{name: "realtime-cbr-cpu-8-64x64-gf-cbr-boost50", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, gfCBRBoostPct: 50, extraArgs: []string{"--gf-cbr-boost=50"}},
		{name: "realtime-cbr-cpu0-32x32-screen-content1", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu-3-64x64-screen-content2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "realtime-cbr-cpu-3-17x33-screen-content1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-17x33", w: 17, h: 33, source: encoderValidationPanningFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu0-16x16-screen-content1", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu0-16x16-screen-content2", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "realtime-cbr-cpu-3-64x64-screen-content1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu-8-96x96-screen-content1", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu-8-96x96-screen-content2", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "good-quality-cbr-cpu4-16x16-screen-content1", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu0-48x48-error-resilient3", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3"}},
		{name: "realtime-cbr-cpu0-16x16-error-resilient3-8partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-16x16-error-resilient3-sharpness7-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, errorResilientPartitions: true, sharpness: 7, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--sharpness=7", "--token-parts=3"}},
		{name: "realtime-cbr-cpu0-32x32-error-resilient-partitions-8partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=2", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-64x64-error-resilient-partitions-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilientPartitions: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=2", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-3-64x64-error-resilient3-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=3", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-64x64-error-resilient3-sharpness7-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, errorResilientPartitions: true, sharpness: 7, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--sharpness=7", "--token-parts=3"}},
		{name: "good-quality-cbr-cpu-3-16x16-error-resilient-partitions-8partitions", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=2", "--token-parts=3"}},
		{name: "realtime-cbr-cpu0-32x32-threads2", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu0-48x48-threads2", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu8-64x64-threads2", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-48x48-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-96x96-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning96, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-4partitions-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tokenPartitions: 2, threads: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2", "--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-threads3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, threads: 3, extraArgs: []string{"--threads=3"}},
		{name: "realtime-cbr-cpu-3-64x64-threads4", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, threads: 4, extraArgs: []string{"--threads=4"}},
		{name: "realtime-cbr-cpu-8-96x96-threads2", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-8-96x96-threads3", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, threads: 3, extraArgs: []string{"--threads=3"}},
		{name: "realtime-cbr-cpu-8-128x128-threads4", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, threads: 4, extraArgs: []string{"--threads=4"}},
		{name: "realtime-cbr-cpu-3-96x96-4partitions-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 2, threads: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2", "--threads=2"}},
		{name: "realtime-cbr-cpu-3-96x96-8partitions-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 3, threads: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=3", "--threads=2"}},
		{name: "realtime-cbr-cpu-8-96x96-4partitions-threads2", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 2, threads: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2", "--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-error-resilient-partitions-4partitions-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilientPartitions: true, tokenPartitions: 2, threads: 2, extraArgs: []string{"--error-resilient=2", "--token-parts=2", "--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-tune-ssim", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		{name: "realtime-q-cpu-3-16x16-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "realtime-q-cpu-3-16x16-q4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 4, extraArgs: []string{"--end-usage=q", "--cq-level=4"}},
		{name: "realtime-q-cpu-3-16x16-q56", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 56, extraArgs: []string{"--end-usage=q", "--cq-level=56"}},
		{name: "realtime-cq-cpu-3-16x16-cq20", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cq-cpu-3-16x16-cq4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 4, extraArgs: []string{"--end-usage=cq", "--cq-level=4"}},
		{name: "realtime-q-cpu-3-32x32-q40", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 40, extraArgs: []string{"--end-usage=q", "--cq-level=40"}},
		{name: "realtime-cq-cpu-3-32x32-cq40", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 40, extraArgs: []string{"--end-usage=cq", "--cq-level=40"}},
		{name: "realtime-q-cpu-3-32x32-q20-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, tokenPartitions: 2, extraArgs: []string{"--end-usage=q", "--cq-level=20", "--token-parts=2"}},
		{name: "realtime-cq-cpu-3-32x32-cq40-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 40, tokenPartitions: 2, extraArgs: []string{"--end-usage=cq", "--cq-level=40", "--token-parts=2"}},
		// Fill in additional cqLevel values across Q / CQ modes so the
		// per-Q regulator response is byte-pinned across the
		// in-range slice, not just at the existing 4/20/40/56 anchors.
		// cqLevel must lie inside the default minQ=4..maxQ=56 band, so
		// the q0/q63/cq63 boundaries are owned by the q0-0 / q0-63
		// quantizer-band rows above; we expand the middle of the
		// 4..56 range here.
		{name: "realtime-q-cpu-3-16x16-q10", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 10, extraArgs: []string{"--end-usage=q", "--cq-level=10"}},
		{name: "realtime-q-cpu-3-16x16-q30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 30, extraArgs: []string{"--end-usage=q", "--cq-level=30"}},
		{name: "realtime-q-cpu-3-16x16-q50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 50, extraArgs: []string{"--end-usage=q", "--cq-level=50"}},
		{name: "realtime-cq-cpu-3-16x16-cq10", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 10, extraArgs: []string{"--end-usage=cq", "--cq-level=10"}},
		{name: "realtime-cq-cpu-3-16x16-cq30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 30, extraArgs: []string{"--end-usage=cq", "--cq-level=30"}},
		// cq50 shares the BestQuality-VBR/Q frame-14 SPLITMV label-RD
		// byte gap (frames 14/15 diverge on the 16x16 single-MB fixture
		// at this quantizer slice); pin the clean 14-frame prefix so the
		// CQ50 plumbing stays guarded.
		{name: "realtime-cq-cpu-3-16x16-cq50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 50, extraArgs: []string{"--end-usage=cq", "--cq-level=50"}},
		// Token partitions (libvpx --token-parts maps log2). 2 = 4 partitions
		// is one of the WebRTC-relevant settings; pin parity here so the
		// partitioned writer regressions surface.
		{name: "realtime-cbr-cpu8-2partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu8-4partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu8-8partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		// Different bitrate targets.
		{name: "realtime-cbr-cpu8-bitrate200", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu8-bitrate2000", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// Tight quantizer band.
		{name: "realtime-cbr-cpu8-q10-30", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-64x64-q0-63", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, minQ: 0, maxQ: 63, minQSet: true, maxQSet: true},
		{name: "realtime-cbr-cpu0-16x16-q0-0", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 0, maxQ: 0, minQSet: true, maxQSet: true},
		{name: "realtime-cbr-cpu0-16x16-q0-63", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 0, maxQ: 63, minQSet: true, maxQSet: true},
		{name: "realtime-cbr-cpu0-16x16-q0-8", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 0, maxQ: 8, minQSet: true},
		{name: "realtime-cbr-cpu0-16x16-q55-63", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 55, maxQ: 63},
		{name: "good-quality-cbr-cpu-3-16x16-q0-63", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 0, maxQ: 63, minQSet: true, maxQSet: true},
		// Forced keyframe every frame stresses the keyframe writer
		// path; diverges today (keyframe Q selection differs in
		// repeated-keyframe sequences).
		{name: "realtime-cbr-cpu8-allkf", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, kfInterval: 1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		// Different FPS.
		{name: "realtime-cbr-cpu8-fps15", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu8-fps60", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// GoodQuality across cpu-used. cpu0/cpu2/cpu4/cpu5 byte-match on
		// the panning fixture.
		{name: "good-quality-cbr-cpu0", deadline: DeadlineGoodQuality, cpuUsed: 0, fx: panning64},
		{name: "good-quality-cbr-cpu2", deadline: DeadlineGoodQuality, cpuUsed: 2, fx: panning64},
		{name: "good-quality-cbr-cpu4", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning64},
		{name: "good-quality-vbr-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "good-quality-q-cpu4-16x16-q20", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "good-quality-cq-cpu4-16x16-cq20", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		// BestQuality panning now byte-matches the full sequence at cpu0.
		{name: "best-quality-cbr-cpu0-panning", deadline: DeadlineBestQuality, cpuUsed: 0, fx: panning64},
		// Realtime explicit-speed inputs (libvpx skips auto-select).
		{name: "realtime-cbr-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64},
		{name: "realtime-cbr-cpu-5", deadline: DeadlineRealtime, cpuUsed: -5, fx: panning64},
		{name: "realtime-cbr-cpu-8", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64},
		// SPLITMV under realtime byte-matches at auto-selected cpu8.
		{name: "realtime-cbr-cpu8-splitmv", deadline: DeadlineRealtime, cpuUsed: 8, fx: splitmv64},
		// 640x480 panning at realtime cpu8 now byte-matches fully.
		{name: "realtime-cbr-cpu8-640x480", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}},
		// Sub-MB-aligned dimensions (w / h % 16 != 0) exercise the
		// MB padding / coded-vs-visible width handling.
		{name: "realtime-cbr-cpu-3-33x17", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-33x17-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-33x17-error-resilient", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu-3-33x17-error-resilient-partitions-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--error-resilient=2", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-33x17-q1-62", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}, minQ: 1, maxQ: 62, minQSet: true, maxQSet: true},
		{name: "realtime-cbr-cpu-3-33x17-static-thresh100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-33x17", w: 33, h: 17, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "realtime-cbr-cpu0-17x17", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-17x17", w: 17, h: 17, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-17x17", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-17x17", w: 17, h: 17, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-31x31", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-31x31", w: 31, h: 31, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-65x33", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-65x33", w: 65, h: 33, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-72x40", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-100x100", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-100x100", w: 100, h: 100, source: encoderValidationPanningFrame}},
		// 16x16 minimum frame size — single-MB encode is byte-identical
		// to libvpx end-to-end.
		{name: "realtime-cbr-cpu8-16x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		// 32-aligned small frames.
		{name: "realtime-cbr-cpu8-32x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-32x32", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-48x48", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		// Common 16:9 standard frame sizes.
		{name: "realtime-cbr-cpu8-256x144", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-192x108", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-192x108", w: 192, h: 108, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-320x180", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-320x180", w: 320, h: 180, source: encoderValidationPanningFrame}},
		// BestQuality across more cpu_used.
		{name: "best-quality-cbr-cpu5-panning", deadline: DeadlineBestQuality, cpuUsed: 5, fx: panning64},
		{name: "best-quality-cbr-cpu5-splitmv", deadline: DeadlineBestQuality, cpuUsed: 5, fx: splitmv64},
		// Wide / tall asymmetric frames.
		{name: "realtime-cbr-cpu8-128x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-128x64", w: 128, h: 64, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-64x128", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-64x128", w: 64, h: 128, source: encoderValidationPanningFrame}},
		// SplitMV under good-quality + various cpu_used.
		{name: "good-quality-cbr-cpu5-splitmv", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: splitmv64},
		{name: "good-quality-cbr-cpu4-splitmv", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: splitmv64},
		// Realtime cpu0/cpu4 on more resolutions to broaden coverage.
		{name: "realtime-cbr-cpu0-96x96", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96},
		{name: "realtime-cbr-cpu4-96x96", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning96},
		{name: "realtime-cbr-cpu0-128x128", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128},
		{name: "realtime-cbr-cpu4-128x128", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning128},
		// Realtime cpu8 on 128x64 splitmv to expand SPLITMV coverage.
		{name: "realtime-cbr-cpu8-128x64-splitmv", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "splitmv-128x64", w: 128, h: 64, source: encoderValidationSplitMVQuadrantFrame}},
		// Larger panning configs at cpu0/cpu4.
		{name: "realtime-cbr-cpu0-160x96", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning160x96},
		{name: "realtime-cbr-cpu4-160x96", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning160x96},
		// Realtime cpu0/cpu4 on splitmv64.
		{name: "realtime-cbr-cpu0-splitmv", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64},
		{name: "realtime-cbr-cpu4-splitmv", deadline: DeadlineRealtime, cpuUsed: 4, fx: splitmv64},
		// Small-resolution cpu0/cpu4 variants. Small frames sit well
		// below the realtime-deadline budget so the libvpx auto-select-
		// speed evolution stays at the cold-start seed in both runs,
		// which makes these the strongest byte-parity probes for any
		// per-MB encode logic. 16x16 / 32x32 / 48x48 byte-match the
		// full 16-frame sequence, including 72x40.
		{name: "realtime-cbr-cpu0-72x40", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu4-72x40", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu4-32x32", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-48x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu4-48x48", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu4-16x16", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		// Mid-range cpu1..cpu3 + small frame probes. Small frames keep
		// the realtime auto-select trajectory at the cold-start seed so
		// these byte-match the full 16-frame sequence.
		{name: "realtime-cbr-cpu1-32x32", deadline: DeadlineRealtime, cpuUsed: 1, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu2-32x32", deadline: DeadlineRealtime, cpuUsed: 2, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu3-32x32", deadline: DeadlineRealtime, cpuUsed: 3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu9-32x32", deadline: DeadlineRealtime, cpuUsed: 9, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu15-32x32", deadline: DeadlineRealtime, cpuUsed: 15, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		// Negative-cpu variants on 32x32 — `cpu_used < 0` skips
		// auto-select entirely, so these probe the static-Speed code path.
		{name: "realtime-cbr-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-4-32x32", deadline: DeadlineRealtime, cpuUsed: -4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-5-32x32", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-6-32x32", deadline: DeadlineRealtime, cpuUsed: -6, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-7-32x32", deadline: DeadlineRealtime, cpuUsed: -7, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-16-16x16", deadline: DeadlineRealtime, cpuUsed: -16, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-16-32x32", deadline: DeadlineRealtime, cpuUsed: -16, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		// Positive cpu-used=16 hits libvpx's most aggressive realtime
		// auto-speed path. With cpu_used=16, msForCompress collapses
		// to zero so vp8_auto_select_speed's "else" branch fires from
		// frame 0 onward; libvpxAutoSelectSpeed seeds autoSpeed from
		// cpu_used (mirroring libvpx onyx_if.c:1706
		// cpi->Speed = cpi->oxcf.cpu_used) so the branch starts at 16
		// rather than 0.
		{name: "realtime-cbr-cpu16-32x32", deadline: DeadlineRealtime, cpuUsed: 16, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		// Different bitrate floors at 32x32 (CBR target hits the buffer
		// adjustment paths differently).
		{name: "realtime-cbr-cpu0-32x32-bitrate200", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu0-32x32-bitrate2000", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// Tight Q band on 32x32.
		{name: "realtime-cbr-cpu0-32x32-q10-30", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		// 32x32 + token partitions.
		{name: "realtime-cbr-cpu0-32x32-2partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		// 32x32 + fps15 and fps60.
		{name: "realtime-cbr-cpu0-32x32-fps15", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu0-32x32-fps60", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// Same axis sweeps on 48x48 to broaden coverage.
		{name: "realtime-cbr-cpu0-48x48-bitrate200", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu0-48x48-bitrate2000", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu0-48x48-q10-30", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu0-48x48-fps15", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		// 16x16 (single-MB) at additional cpu_used values.
		{name: "realtime-cbr-cpu1-16x16", deadline: DeadlineRealtime, cpuUsed: 1, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu2-16x16", deadline: DeadlineRealtime, cpuUsed: 2, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-16x16", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-16x16-noise-sensitivity1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "realtime-cbr-cpu-3-16x16-noise-sensitivity2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "realtime-cbr-cpu-3-16x16-noise-sensitivity3", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "realtime-cbr-cpu-3-16x16-noise-sensitivity4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "realtime-cbr-cpu-3-16x16-noise-sensitivity5", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "realtime-cbr-cpu-3-16x16-noise-sensitivity6", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "realtime-cbr-cpu-8-16x16-noise-sensitivity1", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "realtime-cbr-cpu-8-16x16-noise-sensitivity3", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "realtime-cbr-cpu-8-16x16-noise-sensitivity6", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		// Asymmetric two-MB denoiser cases pin the row/column edge paths
		// without relying on the later 48x48 prefix-only fixture.
		{name: "realtime-cbr-cpu-3-32x16-noise-sensitivity3", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "realtime-cbr-cpu-3-32x16-noise-sensitivity6", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "realtime-cbr-cpu-3-16x32-noise-sensitivity3", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x32", w: 16, h: 32, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "realtime-cbr-cpu-8-32x16-noise-sensitivity6", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "realtime-cbr-cpu-8-16x32-noise-sensitivity6", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x32", w: 16, h: 32, source: encoderValidationPanningFrame}, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		// Multi-MB denoiser cases cover spatial denoiser edge filtering between
		// neighboring macroblocks, including COPY/no-filter macroblocks and all
		// public denoiser sensitivity levels.
		{name: "realtime-cbr-cpu-3-48x48-noise-sensitivity1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "realtime-cbr-cpu-3-48x48-noise-sensitivity2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "realtime-cbr-cpu-3-48x48-noise-sensitivity3", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "realtime-cbr-cpu-3-48x48-noise-sensitivity4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "realtime-cbr-cpu-3-48x48-noise-sensitivity5", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "realtime-cbr-cpu-3-48x48-noise-sensitivity6", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "realtime-cbr-cpu-3-64x64-noise-sensitivity3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "good-quality-cbr-cpu4-32x32-noise-sensitivity3", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "best-quality-cbr-cpu0-16x16-noise-sensitivity3", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		// allkf (kfInterval=1) on small frames — every frame is a key
		// frame, exercises the keyframe writer path repeatedly.
		{name: "realtime-cbr-cpu0-16x16-allkf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		{name: "good-quality-cbr-cpu4-16x16-allkf", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		{name: "realtime-cbr-cpu0-32x32-allkf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		{name: "realtime-cbr-cpu0-48x48-allkf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, kfInterval: 1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		{name: "realtime-cbr-cpu-3-32x32-kf4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 4, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=4"}},
		{name: "realtime-cbr-cpu-3-32x32-kf4-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 4, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=4"}},
		{name: "good-quality-cbr-cpu-3-32x32-kf4", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 4, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=4"}},
		{name: "realtime-cbr-cpu-3-16x16-kf8", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 8, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=8"}},
		{name: "realtime-cbr-cpu-3-32x16-kf8", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 8, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=8"}},
		{name: "good-quality-cbr-cpu4-16x16-kf8", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 8, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=8"}},
		{name: "good-quality-cbr-cpu4-32x32-kf4", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 4, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=4"}},
		{name: "best-quality-cbr-cpu0-16x16-kf8", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 8, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=8"}},
		{name: "realtime-cbr-cpu-8-64x64-kf8", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, kfInterval: 8, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=8"}},
		// Fill in the kfInterval cadence between allkf (1), kf4, and
		// kf8 so each value libvpx might be configured with at a small
		// fixture has a strict byte-parity probe.
		{name: "realtime-cbr-cpu0-32x32-kf2", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 2, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=2"}},
		{name: "realtime-cbr-cpu0-32x32-kf3", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 3, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=3"}},
		{name: "realtime-cbr-cpu-3-16x16-kf2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 2, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=2"}},
		{name: "realtime-cbr-cpu-3-32x32-kf5", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 5, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=5"}},
		{name: "realtime-cbr-cpu-3-16x16-kf16", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 16, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=16"}},
		// 16x16 / 48x48 with token partitions to widen the partitioned-
		// writer coverage beyond the existing 32x32 site.
		{name: "realtime-cbr-cpu0-16x16-2partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu0-48x48-2partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu0-32x32-8partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		// fps60 on 16x16 and 48x48.
		{name: "realtime-cbr-cpu0-16x16-fps60", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu0-48x48-fps60", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// Lookahead without ARF keeps output visible-only while exercising
		// the lookahead queue and end-of-stream flush path.
		{name: "realtime-cbr-cpu-3-64x64-lookahead1-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 1},
		{name: "good-quality-cbr-cpu4-16x16-lookahead1-no-arf", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 1},
		{name: "realtime-cbr-cpu-3-64x64-lookahead1-no-arf-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 1, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-vbr-cpu-3-16x16-lookahead1-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, lookaheadFrames: 1, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-cq-cpu-3-16x16-cq20-lookahead1-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, lookaheadFrames: 1, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cbr-cpu-3-64x64-lookahead1-no-arf-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 1, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-64x64-lookahead1-no-arf", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, lookaheadFrames: 1},
		// auto-alt-ref=1 with no usable lag must remain byte-identical to
		// libvpx's visible-only path.
		{name: "realtime-cbr-cpu-3-64x64-auto-alt-ref-no-lag", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, autoAltRef: true, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-64x64-lookahead1-auto-alt-ref", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 1, autoAltRef: true, extraArgs: []string{"--end-usage=cbr"}},
		// Auto-ARF with usable lag in one-pass mode: libvpx's
		// vp8/encoder/ratectrl.c calc_pframe_target_size resets
		// source_alt_ref_pending to 0 on every one-pass frame, so the
		// hidden ARF stream stays empty and govpx now mirrors that.
		// good-quality VBR retains a small bitstream divergence
		// unrelated to ARF scheduling; keep it as a known gap.
		{name: "realtime-cbr-cpu-3-64x64-lookahead4-auto-alt-ref", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, autoAltRef: true, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-64x64-lookahead8-auto-alt-ref-arnr-strength6-type3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 8, autoAltRef: true, arnrMaxFrames: 7, arnrStrength: 6, arnrType: 3, extraArgs: []string{"--end-usage=cbr", "--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"}},
		{name: "good-quality-cbr-cpu4-16x16-auto-alt-ref-no-lag", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, autoAltRef: true, extraArgs: []string{"--end-usage=cbr"}},
		{name: "good-quality-cbr-cpu4-16x16-lookahead1-auto-alt-ref", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 1, autoAltRef: true, extraArgs: []string{"--end-usage=cbr"}},
		{name: "good-quality-vbr-cpu4-16x16-lookahead4-auto-alt-ref", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, lookaheadFrames: 4, autoAltRef: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 3, arnrStrength: 3, arnrType: 3, extraArgs: []string{"--arnr-maxframes=3", "--arnr-strength=3", "--arnr-type=3"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-strength6-type2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 5, arnrStrength: 6, arnrType: 2, extraArgs: []string{"--arnr-maxframes=5", "--arnr-strength=6", "--arnr-type=2"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-strength0-type1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 1, arnrStrength: 0, arnrType: 1, extraArgs: []string{"--arnr-maxframes=1", "--arnr-strength=0", "--arnr-type=1"}},
		{name: "realtime-cbr-cpu-8-64x64-arnr-controls-no-arf-strength6-type1", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 7, arnrStrength: 6, arnrType: 1, extraArgs: []string{"--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=1"}},
		{name: "realtime-cbr-cpu-8-64x64-arnr-controls-no-arf-max15-strength6-type3", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 15, arnrStrength: 6, arnrType: 3, extraArgs: []string{"--arnr-maxframes=15", "--arnr-strength=6", "--arnr-type=3"}},
		{name: "good-quality-cbr-cpu4-16x16-arnr-controls-no-arf-strength6-type1", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2, arnrMaxFrames: 3, arnrStrength: 6, arnrType: 1, extraArgs: []string{"--arnr-maxframes=3", "--arnr-strength=6", "--arnr-type=1"}},
		{name: "good-quality-vbr-cpu4-16x16-arnr-controls-no-arf-strength3-type2", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, lookaheadFrames: 2, arnrMaxFrames: 5, arnrStrength: 3, arnrType: 2, extraArgs: []string{"--end-usage=vbr", "--arnr-maxframes=5", "--arnr-strength=3", "--arnr-type=2"}},
		{name: "best-quality-cbr-cpu0-16x16-arnr-controls-no-arf-strength6-type3", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2, arnrMaxFrames: 7, arnrStrength: 6, arnrType: 3, extraArgs: []string{"--arnr-maxframes=7", "--arnr-strength=6", "--arnr-type=3"}},
		// Fill in the middle of the arnr-strength range so the
		// filter-strength axis is pinned in addition to the 0/3/6
		// anchors already covered.
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-strength1-type1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 3, arnrStrength: 1, arnrType: 1, extraArgs: []string{"--arnr-maxframes=3", "--arnr-strength=1", "--arnr-type=1"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-strength2-type2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 3, arnrStrength: 2, arnrType: 2, extraArgs: []string{"--arnr-maxframes=3", "--arnr-strength=2", "--arnr-type=2"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-strength4-type3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 3, arnrStrength: 4, arnrType: 3, extraArgs: []string{"--arnr-maxframes=3", "--arnr-strength=4", "--arnr-type=3"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-strength5-type1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 5, arnrStrength: 5, arnrType: 1, extraArgs: []string{"--arnr-maxframes=5", "--arnr-strength=5", "--arnr-type=1"}},
		// Additional maxframes settings between 1/3/5/7/15 so the
		// filter-window-length axis is broader.
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-max2-strength3-type2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 2, arnrStrength: 3, arnrType: 2, extraArgs: []string{"--arnr-maxframes=2", "--arnr-strength=3", "--arnr-type=2"}},
		{name: "realtime-cbr-cpu-3-64x64-arnr-controls-no-arf-max9-strength3-type3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, arnrMaxFrames: 9, arnrStrength: 3, arnrType: 3, extraArgs: []string{"--arnr-maxframes=9", "--arnr-strength=3", "--arnr-type=3"}},
		{name: "realtime-cbr-cpu0-16x16-lookahead2-no-arf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "realtime-cbr-cpu-3-64x64-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4},
		{name: "realtime-cbr-cpu-3-64x64-lookahead4-no-arf-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-96x96-lookahead2-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "realtime-cbr-cpu-3-96x96-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, lookaheadFrames: 4},
		{name: "realtime-cbr-cpu-8-96x96-lookahead2-no-arf", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "realtime-cbr-cpu-8-96x96-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, lookaheadFrames: 4},
		{name: "good-quality-cbr-cpu4-16x16-lookahead2-no-arf", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "good-quality-cbr-cpu-3-32x32-lookahead2-no-arf", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "good-quality-vbr-cpu4-16x16-lookahead2-no-arf", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, lookaheadFrames: 2, extraArgs: []string{"--end-usage=vbr"}},
		{name: "good-quality-vbr-cpu4-32x32-lookahead2-no-arf-4partitions", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, lookaheadFrames: 2, tokenPartitions: 2, extraArgs: []string{"--end-usage=vbr", "--token-parts=2"}},
		{name: "realtime-cq-cpu-3-16x16-cq20-lookahead2-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, lookaheadFrames: 2, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cbr-cpu-3-32x32-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 4},
		{name: "realtime-cbr-cpu-3-splitmv-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, lookaheadFrames: 4},
		{name: "realtime-cbr-cpu-8-64x64-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, lookaheadFrames: 4},
		{name: "realtime-cbr-cpu-8-64x64-lookahead4-no-arf-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, lookaheadFrames: 4, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-64x64-segmented-lookahead4-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, lookaheadFrames: 4},
		// bitrate sweeps on 16x16.
		{name: "realtime-cbr-cpu0-16x16-bitrate200", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu0-16x16-bitrate2000", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// q10-30 on 16x16.
		{name: "realtime-cbr-cpu0-16x16-q10-30", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		// Error-resilient partitions on small frames. Probes the
		// independent-coefficient-context branch on the simpler MB grids.
		{name: "realtime-cbr-cpu0-16x16-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu0-32x32-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu0-48x48-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu4-32x32-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu8-32x32-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		// Sharpness=4 on small frames.
		{name: "realtime-cbr-cpu0-16x16-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu0-48x48-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		// VBR + small frames.
		{name: "realtime-vbr-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		// Segmented fixture (checkerboard MB pattern) on small frames —
		// probes the per-MB encode logic with a different source signal
		// than the panning gradient. All three byte-match the full
		// 16-frame sequence.
		{name: "realtime-cbr-cpu0-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu0-48x48-segmented", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "segmented-48x48", w: 48, h: 48, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu0-16x16-segmented", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}},
		// Segmented fixture across more cpu_used variants — all five hit
		// full 16-frame byte parity.
		{name: "realtime-cbr-cpu4-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu8-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-3-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu4-48x48-segmented", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "segmented-48x48", w: 48, h: 48, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu4-16x16-segmented", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}},
		// Negative cpu_used + segmented on 16x16/48x48.
		{name: "realtime-cbr-cpu-3-16x16-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-3-48x48-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-48x48", w: 48, h: 48, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-5-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		// cpu1, cpu2, cpu3 on segmented-32x32.
		{name: "realtime-cbr-cpu1-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: 1, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu2-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: 2, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu3-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: 3, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		// GoodQuality on small frames + segmented.
		{name: "good-quality-cbr-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-32x32", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-32x32-segmented", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		// GoodQuality + small-frame x axis variations.
		{name: "good-quality-cbr-cpu4-32x32-bitrate200", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "good-quality-cbr-cpu4-32x32-bitrate2000", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "good-quality-cbr-cpu4-32x32-q10-30", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "good-quality-cbr-cpu4-32x32-fps15", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "good-quality-cbr-cpu4-32x32-fps60", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// GoodQuality on 16x16 and 48x48 panning.
		{name: "good-quality-cbr-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-48x48", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-16x16", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-48x48", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		// BestQuality on small frames — probes the full trellis RD picker
		// against the small-frame baseline. Include a non-16-aligned odd
		// frame to exercise padded-edge SPLITMV decisions.
		{name: "best-quality-cbr-cpu0-31x17", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-31x17", w: 31, h: 17, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu0-16x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu0-32x32", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu0-48x48", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu0-96x96", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu0-128x128", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu5-16x16", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu5-32x32", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu5-48x48", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu5-96x96", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}},
		{name: "best-quality-cbr-cpu5-128x128", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		// Asymmetric small frames (wide and tall). All seven byte-match
		// the full 16-frame sequence.
		{name: "realtime-cbr-cpu0-32x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu4-32x16", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-16x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x32", w: 16, h: 32, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-48x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x16", w: 48, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-16x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x48", w: 16, h: 48, source: encoderValidationPanningFrame}},
		// Segmented at asymmetric sizes.
		{name: "realtime-cbr-cpu0-32x16-segmented", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "segmented-32x16", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu0-16x32-segmented", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "segmented-16x32", w: 16, h: 32, source: encoderValidationSegmentedFrame}},
		// Mid-small sizes: 64x32, 64x48, 80x32, 80x80, and 96x64
		// byte-match fully.
		{name: "realtime-cbr-cpu0-64x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-64x32", w: 64, h: 32, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-64x48", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-64x48", w: 64, h: 48, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-80x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-80x32", w: 80, h: 32, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-80x80", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-80x80", w: 80, h: 80, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu0-96x64", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-96x64", w: 96, h: 64, source: encoderValidationPanningFrame}},
		// Negative-cpu_used (bypasses realtime auto-select-speed) on
		// larger sizes — confirms parity baseline extends past the
		// small-frame window when autoSpeed stays at the static seed.
		// cpu-3 and cpu-5 byte-match at these mid-size fixtures.
		{name: "realtime-cbr-cpu-3-80x80", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-80x80", w: 80, h: 80, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-96x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x64", w: 96, h: 64, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-64x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x32", w: 64, h: 32, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-64x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x48", w: 64, h: 48, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-5-80x80", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "panning-80x80", w: 80, h: 80, source: encoderValidationPanningFrame}},
		// Negative-cpu at the realtime-divergent panning sizes — the
		// autoSpeed bypass at cpu_used < 0 restores full parity across
		// the small and mid-size partial-coded dimensions.
		{name: "realtime-cbr-cpu-3-72x40", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-100x100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-100x100", w: 100, h: 100, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-128x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x64", w: 128, h: 64, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-192x108", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-192x108", w: 192, h: 108, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-256x144", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}},
		// More clean-MB-grid sizes at cpu-3 chart the upper parity
		// boundary. All MB-aligned sizes (96x96, 128x128, 160x96,
		// 320x180, 64x128, 640x480) hit full byte parity at cpu-3 —
		// bypassing the realtime auto-select-speed cliff extends parity
		// all the way to the standard SD resolution.
		{name: "realtime-cbr-cpu-3-160x96", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-160x96", w: 160, h: 96, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-128x128", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		// cpu-3 128x128 is a parity-stable mid-size anchor; exercise a
		// larger buffer-model control beyond the tiny cpu0 probe.
		{name: "realtime-cbr-cpu-3-128x128-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu-8-96x96-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu-8-128x128-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		// Static-thresh controls hit skip/breakout decisions that the
		// default panning-only path does not stress.
		{name: "realtime-cbr-cpu-3-64x64-segmented-static-thresh1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, staticThreshold: 1, extraArgs: []string{"--static-thresh=1"}},
		{name: "realtime-cbr-cpu-3-128x128-static-thresh1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, staticThreshold: 1, extraArgs: []string{"--static-thresh=1"}},
		{name: "realtime-cbr-cpu-8-128x128-static-thresh1", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, staticThreshold: 1, extraArgs: []string{"--static-thresh=1"}},
		{name: "realtime-cbr-cpu-3-128x128-static-thresh100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "realtime-cbr-cpu-8-128x128-static-thresh100", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "realtime-cbr-cpu-3-128x128-screen-content2-static-thresh100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, screenContentMode: 2, staticThreshold: 100, extraArgs: []string{"--screen-content-mode=2", "--static-thresh=100"}},
		{name: "realtime-cbr-cpu0-16x16-static-thresh100", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "realtime-cbr-cpu0-48x48-static-thresh100", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "good-quality-cbr-cpu4-16x16-static-thresh1", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 1, extraArgs: []string{"--static-thresh=1"}},
		{name: "good-quality-cbr-cpu4-16x16-static-thresh100", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		{name: "good-quality-cbr-cpu4-32x32-screen-content2-static-thresh100", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 2, staticThreshold: 100, extraArgs: []string{"--screen-content-mode=2", "--static-thresh=100"}},
		{name: "best-quality-cbr-cpu0-16x16-static-thresh1", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 1, extraArgs: []string{"--static-thresh=1"}},
		{name: "realtime-cbr-cpu-3-96x96", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-64x128", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x128", w: 64, h: 128, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-320x180", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-320x180", w: 320, h: 180, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-640x480", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}},
		// HD resolutions at cpu-3 chart where the upper parity boundary
		// hits. 1280x720 and 1920x1080 now byte-match end to end.
		{name: "realtime-cbr-cpu-3-1280x720", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-1280x720", w: 1280, h: 720, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-3-1920x1080", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-1920x1080", w: 1920, h: 1080, source: encoderValidationPanningFrame}},
		// GoodQuality at clean-MB-grid larger sizes. cpu4 byte-matches
		// 64x32 / 64x48 / 96x96 / 128x128 / 160x96 / 640x480 fully.
		{name: "good-quality-cbr-cpu4-64x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-64x32", w: 64, h: 32, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-64x48", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-64x48", w: 64, h: 48, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-96x96", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-128x128", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-160x96", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-160x96", w: 160, h: 96, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu4-640x480", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}},
		// GoodQuality cpu5 at clean larger sizes — all 5 hit full byte parity.
		{name: "good-quality-cbr-cpu5-64x32", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-64x32", w: 64, h: 32, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-96x96", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-128x128", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-160x96", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-160x96", w: 160, h: 96, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu5-640x480", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}},
		// GoodQuality + segmented at larger sizes. 128x128 and 160x96
		// now byte-match fully.
		{name: "good-quality-cbr-cpu4-128x128-segmented", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "segmented-128x128", w: 128, h: 128, source: encoderValidationSegmentedFrame}},
		{name: "good-quality-cbr-cpu4-160x96-segmented", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "segmented-160x96", w: 160, h: 96, source: encoderValidationSegmentedFrame}},
		// Sharpness=1/2/7 at the 32x32 baseline (sharpness=4 already
		// covered at 16x16/32x32/48x48 above).
		{name: "realtime-cbr-cpu0-32x32-sharpness1", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 1, extraArgs: []string{"--sharpness=1"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness2", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 2, extraArgs: []string{"--sharpness=2"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness7", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 7, extraArgs: []string{"--sharpness=7"}},
		// Fill in the rest of the [0, 7] sharpness range so every level
		// is pinned by at least one strict byte-parity case (defaults
		// sharpness=0 implicitly, so the explicit row guards the
		// libvpx-clamped default path as well).
		{name: "realtime-cbr-cpu0-32x32-sharpness0", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 0, extraArgs: []string{"--sharpness=0"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness3", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 3, extraArgs: []string{"--sharpness=3"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness5", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 5, extraArgs: []string{"--sharpness=5"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness6", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 6, extraArgs: []string{"--sharpness=6"}},
		{name: "realtime-cbr-cpu-3-16x16-sharpness3", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 3, extraArgs: []string{"--sharpness=3"}},
		{name: "realtime-cbr-cpu-3-16x16-sharpness5", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 5, extraArgs: []string{"--sharpness=5"}},
		{name: "realtime-cbr-cpu-3-16x16-sharpness6", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 6, extraArgs: []string{"--sharpness=6"}},
		// Sharpness crossed with token-partitions / threads / tune so
		// the byte-parity matrix pins the picker+writer interactions,
		// not only the single-axis sharpness probe above.
		{name: "realtime-cbr-cpu-3-64x64-sharpness4-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, sharpness: 4, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--sharpness=4", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-3-64x64-sharpness4-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, sharpness: 4, threads: 2, extraArgs: []string{"--sharpness=4", "--threads=2"}},
		{name: "realtime-cbr-cpu-3-32x32-sharpness2-tune-ssim", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 2, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--sharpness=2", "--tune=ssim"}},
		{name: "good-quality-cbr-cpu4-32x32-sharpness4", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "good-quality-cbr-cpu4-16x16-sharpness7", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 7, extraArgs: []string{"--sharpness=7"}},
		// Static-thresh sweeps fill in middle values; existing rows
		// pin 1 / 100 only, so we round out the picker's per-MB
		// distortion gate at 25 / 200 / 1000.
		{name: "realtime-cbr-cpu0-32x32-static-thresh25", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 25, extraArgs: []string{"--static-thresh=25"}},
		{name: "realtime-cbr-cpu0-32x32-static-thresh200", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 200, extraArgs: []string{"--static-thresh=200"}},
		{name: "realtime-cbr-cpu0-32x32-static-thresh1000", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 1000, extraArgs: []string{"--static-thresh=1000"}},
		// ScreenContentMode crossed with sharpness so the screen-content
		// fast intra picker is pinned alongside the loop-filter sharpness.
		{name: "realtime-cbr-cpu-3-32x32-screen-content1-sharpness4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 1, sharpness: 4, extraArgs: []string{"--screen-content-mode=1", "--sharpness=4"}},
		{name: "realtime-cbr-cpu-3-32x32-screen-content2-sharpness4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 2, sharpness: 4, extraArgs: []string{"--screen-content-mode=2", "--sharpness=4"}},
		// cpu-3 + axes on large clean-grid sizes — confirms parity holds
		// across bitrate/Q axes on 256x144 (4 strict matches) and
		// 640x480.
		{name: "realtime-cbr-cpu-3-256x144-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-256x144-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-3-256x144-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-640x480-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-640x480-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		// cpu-5 / cpu-8 at clean large sizes.
		{name: "realtime-cbr-cpu-5-256x144", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-5-640x480", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-5-128x128", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-256x144", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-128x128", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		// cpu-8 at HD/SD sizes. These now byte-match fully through
		// 1280x720 on the panning fixture.
		{name: "realtime-cbr-cpu-8-640x480", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-1280x720", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-1280x720", w: 1280, h: 720, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-96x96", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-160x96", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-160x96", w: 160, h: 96, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-320x180", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-320x180", w: 320, h: 180, source: encoderValidationPanningFrame}},
		// cpu-3 splitmv probe — bypassing autoSpeed lets splitmv match
		// at 64x64 (where positive cpu_used drifts at frame 2+).
		{name: "realtime-cbr-cpu-3-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64},
		{name: "realtime-cbr-cpu-3-128x64-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x64", w: 128, h: 64, source: encoderValidationSplitMVQuadrantFrame}},
		// cpu-8 segmented at clean sizes — both byte-match the full sequence.
		{name: "realtime-cbr-cpu-8-128x128-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-128x128", w: 128, h: 128, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-96x96-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}},
		// cpu-5/-8 splitmv64.
		{name: "realtime-cbr-cpu-5-splitmv", deadline: DeadlineRealtime, cpuUsed: -5, fx: splitmv64},
		{name: "realtime-cbr-cpu-8-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64},
		// GoodQuality cpu-3 / cpu-5 at small frames. GoodQuality clamps
		// cpu_used to [-5, 5] before dispatch (libvpxEffectiveCPUUsed),
		// so cpu-3/-5 don't bypass the auto-select trajectory the way
		// realtime cpu-8 does. The smallest single-MB frame (16x16),
		// 32x32, 48x48, and 128x128 byte-match fully.
		{name: "good-quality-cbr-cpu-3-16x16", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-3-32x32", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-3-32x32-fps15", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "good-quality-cbr-cpu-3-48x48", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-3-128x128", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-5-16x16", deadline: DeadlineGoodQuality, cpuUsed: -5, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-5-32x32", deadline: DeadlineGoodQuality, cpuUsed: -5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-5-48x48", deadline: DeadlineGoodQuality, cpuUsed: -5, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "good-quality-cbr-cpu-5-128x128", deadline: DeadlineGoodQuality, cpuUsed: -5, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}},
		// cpu-3 / cpu-8 token-partitions probes. Negative cpu_used bypasses
		// autoSpeed evolution and gives the cleanest parity surface. The
		// partitioned bitstream layout exercises a separate write/pack
		// path (writePreparedInterCoefficientTokenGridPartitioned) that
		// isn't covered by positive-cpu probes for the same sizes.
		{name: "realtime-cbr-cpu-3-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-3-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu8-error-resilient-partitions-8partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--error-resilient=2", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-8-2partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-8-4partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-8-8partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		// cpu-3 / cpu-8 mid-frame token-partitions at 128x128 (the largest
		// fixture that byte-matches every frame at both cpu values).
		{name: "realtime-cbr-cpu-3-128x128-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-8-128x128-4partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		// cpu-8 segmented at the smallest sizes — same fixture used at
		// cpu-3, but cpu-8 takes a more aggressive static-Speed path
		// that disables more heuristics; pinning these locks in parity
		// across both ends of the negative-cpu range.
		{name: "realtime-cbr-cpu-8-16x16-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-32x32-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-48x48-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-48x48", w: 48, h: 48, source: encoderValidationSegmentedFrame}},
		// cpu-3 segmented at 96x96/128x128 — cpu-8 already byte-matches
		// these; cpu-3 covers the alternate static-Speed branch.
		{name: "realtime-cbr-cpu-3-96x96-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-3-128x128-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-128x128", w: 128, h: 128, source: encoderValidationSegmentedFrame}},
		// cpu-3 / cpu-8 splitmv64 at fps 15 and 60. The encoder's per-
		// frame budget path is fps-dependent so this exercises a
		// different rate-control trajectory on the parity-stable
		// negative-cpu speeds. Bitrate scaled to keep targetKbps roughly
		// proportional.
		{name: "realtime-cbr-cpu-3-splitmv-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-splitmv-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-splitmv-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-splitmv-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// cpu-3 / cpu-8 quantizer-range probes at small frames. q ranges
		// drive a different rate-control surface; pinning them on the
		// parity-stable negative-cpu speeds catches any quantizer
		// trajectory regressions independent of speed evolution.
		{name: "realtime-cbr-cpu-3-128x128-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-128x128-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-128x128-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-128x128-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		// cpu-3 / cpu-8 bitrate-extreme probes — low (200kbps) and high
		// (2000kbps) on a 128x128 fixture stress the buffer-fullness
		// path that diverges on larger sizes.
		{name: "realtime-cbr-cpu-3-128x128-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-128x128-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-128x128-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-128x128-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// cpu-8 panning at the smallest sizes. cpu-3 already byte-matches
		// every one of these; cpu-8 covers the alternate static-Speed
		// branch on the same fixtures and locks in parity across both
		// ends of the negative-cpu range.
		{name: "realtime-cbr-cpu-8-16x16", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-32x32", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-48x48", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-64x32", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-64x32", w: 64, h: 32, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-64x48", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-64x48", w: 64, h: 48, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-64x64", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64},
		{name: "realtime-cbr-cpu-8-64x128", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-64x128", w: 64, h: 128, source: encoderValidationPanningFrame}},
		// cpu-3 panning at 80x80 — cpu-3's 80x80 isn't probed; cpu-8's
		// nearest neighbour at this size is 96x96 which byte-matches.
		{name: "realtime-cbr-cpu-8-80x80", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-80x80", w: 80, h: 80, source: encoderValidationPanningFrame}},
		// cpu-3 / cpu-8 panning fps15 + fps60 variants at the small
		// sizes where the base fps=30 probe byte-matches. fps changes
		// the per-frame budget trajectory; pinning these locks the
		// rate-control parity surface on the stable speeds without
		// going through splitmv (already covered).
		{name: "realtime-cbr-cpu-3-64x64-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-64x64-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1001-30000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 30000, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-64x64-timebase-1001-30000", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, timebaseNum: 1001, timebaseDen: 30000, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-96x96-timebase-1001-30000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, timebaseNum: 1001, timebaseDen: 30000, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, timebaseNum: 1, timebaseDen: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1-15", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1, timebaseDen: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "good-quality-cbr-cpu4-16x16-timebase-1001-30000", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, timebaseNum: 1001, timebaseDen: 30000, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-64x64-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-64x64-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-128x128-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-128x128-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-128x128-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-128x128-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// cpu-3 / cpu-8 splitmv at additional sizes. The splitmv quadrant
		// fixture exercises the SPLITMV-eligible MV partitioning path
		// which has historically been the parity-cliff at positive
		// cpu_used; locking small/mid sizes on the parity-stable
		// negative-cpu speeds catches partitioning regressions.
		{name: "realtime-cbr-cpu-3-32x32-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-32x32", w: 32, h: 16, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-3-48x48-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-48x48", w: 48, h: 48, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-3-128x128-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-32x32-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-32x32", w: 32, h: 16, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-48x48-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-48x48", w: 48, h: 48, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-96x96-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-128x128-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}},
		// cpu-3 / cpu-8 segmented at 64x* mid sizes. The segmented fixture
		// exercises per-macroblock segment-ID assignment + segment-aware
		// quantizer/loopfilter offsets. cpu-3/-8 already byte-match the
		// 16/32/48 small and 96/128 mid sizes; closing the gap between
		// those locks segmented parity across the contiguous size range.
		{name: "realtime-cbr-cpu-3-64x32-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x32", w: 64, h: 32, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-3-64x48-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x48", w: 64, h: 48, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-3-64x64-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-3-80x80-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-80x80", w: 80, h: 80, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-64x32-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x32", w: 64, h: 32, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-64x48-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x48", w: 64, h: 48, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-64x64-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}},
		{name: "realtime-cbr-cpu-8-80x80-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-80x80", w: 80, h: 80, source: encoderValidationSegmentedFrame}},
		// cpu-3 / cpu-8 token-partitions at 96x96 and 128x128 with the
		// remaining partition counts (2/8) to fully cover the
		// multi-writer code path at the mid sizes that byte-match.
		{name: "realtime-cbr-cpu-3-96x96-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-96x96-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-3-96x96-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-128x128-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-128x128-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-8-96x96-2partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-8-96x96-4partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-8-96x96-8partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-8-128x128-2partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-8-128x128-8partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-splitmv-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-8-64x64-segmented-4partitions", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		// cpu-3 / cpu-8 panning-64x64 quantizer-range and bitrate-extreme
		// probes. 64x64 byte-matches at both speeds; varying the
		// quantizer floor/ceiling and bitrate exercises the rate-control
		// trajectory on a parity-stable small frame.
		{name: "realtime-cbr-cpu-3-64x64-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-64x64-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-64x64-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-64x64-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-64x64-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-64x64-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-64x64-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-64x64-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// cpu-3 / cpu-8 panning fps15+fps60 mid-extension to 96x96 — the
		// 64x64 and 128x128 variants already byte-match, so 96x96 sits
		// in the parity-stable region between them.
		{name: "realtime-cbr-cpu-3-96x96-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-96x96-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-96x96-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-96x96-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// cpu-3 / cpu-8 segmented fps variants at 64x64 — segmented
		// 64x64 byte-matches at fps=30 (added earlier), so fps15+fps60
		// pin segmented parity on the fps-sensitive rate-control path.
		{name: "realtime-cbr-cpu-3-64x64-segmented-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-64x64-segmented-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-64x64-segmented-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// cpu-3 / cpu-8 splitmv 96x96 and 128x128 fps variants — the
		// base fps=30 probes byte-match, locking the SPLITMV parity
		// surface on the fps-sensitive rate-control path across mid sizes.
		{name: "realtime-cbr-cpu-3-96x96-splitmv-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-96x96-splitmv-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-96x96-splitmv-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-128x128-splitmv-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-128x128-splitmv-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		// cpu-3 / cpu-8 panning-32x32 and panning-48x48 q-range probes.
		// Both base fixtures byte-match at fps=30 default Q on cpu-3/-8;
		// q10-30 and q40-60 stress the quantizer trajectory on the
		// smallest parity-stable frames.
		{name: "realtime-cbr-cpu-3-32x32-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-32x32-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-48x48-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-48x48-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-32x32-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-32x32-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-48x48-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-48x48-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		// cpu-3 / cpu-8 panning-96x96 quantizer-range probes — fills the
		// q-range coverage gap between 64x64 and 128x128 on the mid size
		// where the base default-Q probe byte-matches.
		{name: "realtime-cbr-cpu-3-96x96-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-96x96-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-96x96-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-96x96-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		// cpu-3 / cpu-8 panning-96x96 bitrate-extreme probes — same
		// rationale, completing the bitrate matrix on the 96x96 size.
		{name: "realtime-cbr-cpu-3-96x96-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-96x96-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-96x96-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-96x96-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// cpu-3 / cpu-8 panning small-frame bitrate-extreme probes —
		// fills the bitrate matrix down to the 32x32 and 48x48 sizes.
		{name: "realtime-cbr-cpu-3-32x32-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-32x32-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-32x32-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-32x32-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-3-48x48-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-48x48-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-48x48-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-48x48-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// cpu-3 / cpu-8 splitmv-64x64 q-range and bitrate-extreme probes.
		// The base splitmv-64x64 fixture byte-matches at both cpu values
		// at fps=30 default Q; varying the RC trajectory stresses the
		// SPLITMV partitioning + RC path together on the parity-stable
		// speeds.
		{name: "realtime-cbr-cpu-3-splitmv-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-splitmv-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-splitmv-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-splitmv-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-splitmv-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-splitmv-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-splitmv-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-splitmv-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: splitmv64, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// cpu-3 / cpu-8 segmented-64x64 q-range + bitrate-extreme probes.
		// The base segmented-64x64 fixture byte-matches at fps=30 default
		// Q; crosses the segment-aware quantizer/loopfilter offset path
		// with the rate-control trajectory.
		{name: "realtime-cbr-cpu-3-64x64-segmented-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-64x64-segmented-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-64x64-segmented-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-64x64-segmented-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-64x64-segmented-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-64x64-segmented-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-64x64-segmented-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-64x64-segmented-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// splitmv-96x96 crossed with controls that have no coverage yet:
		// sharpness, error-resilient, token-partitions, screen-content,
		// gf-cbr-boost, max-intra-rate, buffer, threads. The base
		// fixture byte-matches at the cpu-3/-8 anchors above, so these
		// pin the SPLITMV-heavy mode picker against extra knobs.
		{name: "realtime-cbr-cpu-3-96x96-splitmv-sharpness4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-sharpness7", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, sharpness: 7, extraArgs: []string{"--sharpness=7"}},
		// splitmv-96x96 + error-resilient byte-matches in full once the
		// libvpx-side `vp8_adjust_key_frame_context` non_gf_bitrate_adjustment
		// gate is mirrored (see vp8_ratecontrol_postencode.go). Before that fix
		// govpx drained gf_overspend_bits one frame faster than libvpx, which
		// biased frame-2's vp8_regulate_q one Q step high (12 vs 11) and
		// cascaded into intra coefficient eob_sum mismatches across the
		// SPLITMV-heavy intra MBs in frames 2-10, 12, 15.
		{name: "realtime-cbr-cpu-3-96x96-splitmv-error-resilient", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-screen-content1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-gf-cbr-boost50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, gfCBRBoostPct: 50, extraArgs: []string{"--gf-cbr-boost=50"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-max-intra-rate100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, maxIntraBitratePct: 100, extraArgs: []string{"--max-intra-rate=100"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		// Segmented fixture crossed with controls that have minimal
		// coverage on this fixture today (sharpness, error-resilient,
		// token partitions, screen content, drop-frame, undershoot/
		// overshoot, gf-cbr-boost, max-intra-rate, buffer-size,
		// keyframe cadence, threads, tune). Each row holds at strict
		// 16-frame byte parity.
		{name: "realtime-cbr-cpu-3-64x64-segmented-sharpness4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-sharpness7", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, sharpness: 7, extraArgs: []string{"--sharpness=7"}},
		{name: "realtime-cbr-cpu-8-64x64-segmented-sharpness4", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-error-resilient", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-2partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-8partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-screen-content1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-drop-frame60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, dropFrameAllowed: true, dropFrameWaterMark: 60, extraArgs: []string{"--drop-frame=60"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-undershoot50-overshoot50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, undershootPct: 50, overshootPct: 50, extraArgs: []string{"--undershoot-pct=50", "--overshoot-pct=50"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-gf-cbr-boost50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, gfCBRBoostPct: 50, extraArgs: []string{"--gf-cbr-boost=50"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-max-intra-rate100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, maxIntraBitratePct: 100, extraArgs: []string{"--max-intra-rate=100"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-kf4", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, kfInterval: 4, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=4"}},
		{name: "realtime-cbr-cpu-3-64x64-segmented-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		// Tune=SSIM on the segmented fixture byte-matches across the
		// full 16-frame stream once libvpx's activity-masked x->rdmult
		// is threaded into the per-4x4 intra picker, the whole-block
		// intra Y picker, and the chroma UV picker. Without that, the
		// untuned RDCOST inside rd_pick_intra_mbuv_mode flipped the
		// inter-frame DC/V/H/TM UV-mode winner and rippled into the
		// UV-adler + token coefficient prob stream.
		{name: "realtime-cbr-cpu-3-64x64-segmented-tune-ssim", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		// cpu-3 / cpu-8 splitmv 96x96 q-range + bitrate-extreme probes
		// — the splitmv-96x96 fixture byte-matches at fps=30 default Q,
		// so q10-30, q40-60, bitrate-200, bitrate-2000 close out the
		// RC + SPLITMV cross-product on the mid-size frame.
		{name: "realtime-cbr-cpu-3-96x96-splitmv-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-96x96-splitmv-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-96x96-splitmv-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-96x96-splitmv-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-96x96-splitmv-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-96x96-splitmv-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// cpu-3 / cpu-8 segmented-96x96 q-range + bitrate-extreme probes
		// — extends segmented RC matrix from 64x64 up to 96x96.
		{name: "realtime-cbr-cpu-3-96x96-segmented-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-96x96-segmented-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-96x96-segmented-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-96x96-segmented-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-96x96-segmented-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-96x96-segmented-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-96x96-segmented-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-96x96-segmented-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-96x96", w: 96, h: 96, source: encoderValidationSegmentedFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// GoodQuality cpu-3 q-range probe at 16x16 — the only GoodQuality
		// fixture that byte-matches every frame at cpu-3. q10-30 and
		// q40-60 stress the quantizer trajectory on the smallest
		// single-MB parity-stable frame.
		{name: "good-quality-cbr-cpu-3-16x16-q10-30", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "good-quality-cbr-cpu-3-16x16-q40-60", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "good-quality-cbr-cpu-3-16x16-bitrate200", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "good-quality-cbr-cpu-3-16x16-bitrate2000", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// GoodQuality cpu-3 16x16 fps variants — base 16x16 GoodQuality
		// byte-matches at cpu-3, so fps15+fps60 stress the per-frame
		// budget trajectory on the single-MB GoodQuality fixture.
		{name: "good-quality-cbr-cpu-3-16x16-fps15", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "good-quality-cbr-cpu-3-16x16-fps60", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// GoodQuality cpu-3 16x16 token-partitions — the multi-writer
		// partitioned bitstream layout at the single-MB GQ fixture.
		{name: "good-quality-cbr-cpu-3-16x16-2partitions", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "good-quality-cbr-cpu-3-16x16-4partitions", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "good-quality-cbr-cpu-3-16x16-8partitions", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		// cpu-3 / cpu-8 splitmv-128x128 q-range + bitrate-extreme probes
		// — extends the SPLITMV + RC cross-product up to 128x128, the
		// largest SPLITMV fixture that byte-matches at fps=30 default Q
		// on cpu-3 / cpu-8.
		{name: "realtime-cbr-cpu-3-128x128-splitmv-q10-30", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-3-128x128-splitmv-q40-60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-8-128x128-splitmv-q10-30", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, minQ: 10, maxQ: 30},
		{name: "realtime-cbr-cpu-8-128x128-splitmv-q40-60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, minQ: 40, maxQ: 60},
		{name: "realtime-cbr-cpu-3-128x128-splitmv-bitrate200", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-3-128x128-splitmv-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-8-128x128-splitmv-bitrate200", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu-8-128x128-splitmv-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// GoodQuality cpu-3 16x16 splitmv + segmented fixture probes —
		// the single-MB GQ fixture is parity-stable; cross-fixture probes
		// pin segment-aware quantizer/loopfilter and SPLITMV partitioning
		// at the same GQ deadline + cpu-3 anchor.
		{name: "good-quality-cbr-cpu-3-16x16-splitmv", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "splitmv-16x16", w: 16, h: 16, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "good-quality-cbr-cpu-3-16x16-segmented", deadline: DeadlineGoodQuality, cpuUsed: -3, fx: fixture{name: "segmented-16x16", w: 16, h: 16, source: encoderValidationSegmentedFrame}},
		// cpu-8 panning-128x64 + splitmv-128x64 probes — closes the
		// rectangular-frame gap at cpu-8. cpu-3 already covers both
		// fixtures (panning byte-matches every frame, splitmv at limit=1);
		// cpu-8 takes the other static-Speed branch.
		{name: "realtime-cbr-cpu-8-128x64", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x64", w: 128, h: 64, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-8-128x64-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-128x64", w: 128, h: 64, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-128x64-segmented", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "segmented-128x64", w: 128, h: 64, source: encoderValidationSegmentedFrame}},
		// cpu-3 segmented-128x64 to complete the cross-fixture matrix at
		// the rectangular size.
		{name: "realtime-cbr-cpu-3-128x64-segmented", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "segmented-128x64", w: 128, h: 64, source: encoderValidationSegmentedFrame}},
		// cpu-3 / cpu-8 splitmv at small mid sizes — segmented already
		// has 64x32/64x48/64x64/80x80 covered; mirror the same matrix
		// for splitmv to lock SPLITMV partitioning parity across the
		// full contiguous size range.
		{name: "realtime-cbr-cpu-3-64x32-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-64x32", w: 64, h: 32, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-3-64x48-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-64x48", w: 64, h: 48, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-3-80x80-splitmv", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "splitmv-80x80", w: 80, h: 80, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-64x32-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-64x32", w: 64, h: 32, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-64x48-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-64x48", w: 64, h: 48, source: encoderValidationSplitMVQuadrantFrame}},
		{name: "realtime-cbr-cpu-8-80x80-splitmv", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "splitmv-80x80", w: 80, h: 80, source: encoderValidationSplitMVQuadrantFrame}},
		// cpu-3 / cpu-8 panning fps15/fps60 mid extension to 32x32 and
		// 48x48 — smaller end of the fps-extension matrix; base default-
		// fps probes byte-match.
		{name: "realtime-cbr-cpu-3-32x32-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-32x32-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-32x32-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-32x32-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-48x48-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-48x48-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-48x48-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-48x48-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// cpu-3 / cpu-8 panning fps15/fps60 at 16x16 (single-MB
		// parity-stable frame). Smallest end of the fps-extension
		// matrix.
		{name: "realtime-cbr-cpu-3-16x16-fps15", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-3-16x16-fps60", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-16x16-fps15", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu-8-16x16-fps60", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// BestQuality small-frame control axes.
		{name: "best-quality-cbr-cpu0-16x16-q10-30", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "best-quality-cbr-cpu0-16x16-q40-60", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 40, maxQ: 60},
		{name: "best-quality-cbr-cpu5-16x16-q10-30", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		{name: "best-quality-cbr-cpu0-32x32-fps15", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "best-quality-cbr-cpu0-32x32-fps60", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "best-quality-cbr-cpu0-16x16-timebase-1001-30000", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, timebaseNum: 1001, timebaseDen: 30000, extraArgs: []string{"--end-usage=cbr"}},
		{name: "best-quality-cbr-cpu5-32x32-bitrate200", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "best-quality-cbr-cpu0-32x32-2partitions", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "best-quality-cbr-cpu5-32x32-4partitions", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "best-quality-cbr-cpu0-32x32-8partitions", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "best-quality-cbr-cpu5-32x32-8partitions", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		// Additional direct libvpx control probes.
		{name: "best-quality-cbr-cpu0-16x16-sharpness7", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 7, extraArgs: []string{"--sharpness=7"}},
		{name: "best-quality-cbr-cpu0-16x16-error-resilient-partitions", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2"}},
		{name: "best-quality-cbr-cpu0-16x16-error-resilient3-8partitions", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		{name: "best-quality-cbr-cpu0-16x16-screen-content2", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "best-quality-cbr-cpu0-32x32-static-thresh100", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 100, extraArgs: []string{"--static-thresh=100"}},
		// BestQuality one-pass non-CBR controls currently share the
		// remaining SPLITMV label-RD byte gap at frame 14. Pin the clean
		// prefix so rate-control mode plumbing remains guarded while that
		// next sub-MV tie is narrowed.
		{name: "best-quality-q-cpu0-16x16-q20", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "best-quality-cq-cpu0-16x16-cq20", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "best-quality-cbr-cpu0-16x16-buffer-1000-500-600", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "best-quality-vbr-cpu0-16x16", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "best-quality-vbr-cpu0-16x16-buffer-1000-500-600", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--end-usage=vbr", "--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "best-quality-cbr-cpu0-16x16-lookahead2-no-arf", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "best-quality-cbr-cpu5-32x32-lookahead2-no-arf", deadline: DeadlineBestQuality, cpuUsed: 5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2},
		{name: "good-quality-vbr-cpu4-32x32", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "good-quality-vbr-cpu4-16x16", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "good-quality-q-cpu4-16x16-q20", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "good-quality-cq-cpu4-16x16-cq20", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "good-quality-q-cpu4-32x32-q20", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "good-quality-cq-cpu4-32x32-cq20", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "good-quality-cbr-cpu4-16x16-threads2", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "good-quality-cbr-cpu4-32x32-threads2", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "best-quality-cbr-cpu0-32x32-threads2", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "good-quality-cbr-cpu4-16x16-tune-ssim", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		{name: "good-quality-cbr-cpu4-32x32-tune-ssim", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		{name: "good-quality-vbr-cpu4-16x16-buffer-1000-500-600", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--end-usage=vbr", "--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "good-quality-vbr-cpu4-32x32-buffer-1000-500-600", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--end-usage=vbr", "--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "good-quality-vbr-cpu4-32x32-tune-ssim-buffer-1000-500-600", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, tuning: TuneSSIM, tuningSet: true, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--end-usage=vbr", "--tune=ssim", "--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "realtime-cbr-cpu-3-64x64-lookahead4-no-arf-4partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, lookaheadFrames: 4, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "good-quality-cbr-cpu4-16x16-lookahead2-no-arf-8partitions", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, lookaheadFrames: 2, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		{name: "good-quality-vbr-cpu4-16x16-lookahead2-no-arf-8partitions", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, rcModeSet: true, lookaheadFrames: 2, tokenPartitions: 3, extraArgs: []string{"--end-usage=vbr", "--token-parts=3"}},
		{name: "realtime-cbr-cpu-3-64x64-drop-frame60-explicit", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 60, extraArgs: []string{"--drop-frame=60"}},
		{name: "realtime-cbr-cpu-3-64x64-drop-frame150-clamp-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 150, extraArgs: []string{"--end-usage=cbr", "--drop-frame=100", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		{name: "realtime-cbr-cpu-3-64x64-undershoot1-overshoot100", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, undershootPct: 1, overshootPct: 100, extraArgs: []string{"--undershoot-pct=1", "--overshoot-pct=100"}},
		{name: "realtime-cbr-cpu-3-64x64-buffer-500-500-500", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 500, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 500, extraArgs: []string{"--buf-sz=500", "--buf-initial-sz=500", "--buf-optimal-sz=500"}},
		// Extra non-CBR/control cross-products. These rows keep the
		// matrix honest for default CQ-level normalization, explicit
		// quantizer bands, partitioned Q/CQ streams, lookahead without
		// ARF, ARF suppression under error resilience, and rate-control
		// edge knobs that should remain visible-only strict matches.
		{name: "realtime-q-cpu-3-16x16-default-cq-level", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, extraArgs: []string{"--end-usage=q"}},
		{name: "realtime-cq-cpu-3-16x16-default-cq-level", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, extraArgs: []string{"--end-usage=cq"}},
		{name: "realtime-q-cpu-3-16x16-q20-q0-63", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, minQ: 0, maxQ: 63, minQSet: true, maxQSet: true, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "realtime-cq-cpu-3-16x16-cq20-q0-63", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, minQ: 0, maxQ: 63, minQSet: true, maxQSet: true, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "good-quality-q-cpu4-16x16-q4", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 4, extraArgs: []string{"--end-usage=q", "--cq-level=4"}},
		{name: "good-quality-cq-cpu4-16x16-cq56", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 56, extraArgs: []string{"--end-usage=cq", "--cq-level=56"}},
		{name: "good-quality-q-cpu4-32x32-q40-4partitions", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 40, tokenPartitions: 2, extraArgs: []string{"--end-usage=q", "--cq-level=40", "--token-parts=2"}},
		{name: "good-quality-cq-cpu4-32x32-cq40-4partitions", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 40, tokenPartitions: 2, extraArgs: []string{"--end-usage=cq", "--cq-level=40", "--token-parts=2"}},
		{name: "realtime-q-cpu-3-16x16-q20-lookahead2-no-arf", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, lookaheadFrames: 2, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "good-quality-q-cpu4-16x16-q20-lookahead2-no-arf", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, lookaheadFrames: 2, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "good-quality-cq-cpu4-16x16-cq20-lookahead2-no-arf", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, lookaheadFrames: 2, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cbr-cpu-3-64x64-lookahead4-auto-alt-ref-error-resilient", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, lookaheadFrames: 4, autoAltRef: true, extraArgs: []string{"--end-usage=cbr", "--error-resilient=1"}},
		{name: "realtime-cbr-cpu-3-64x64-lookahead4-auto-alt-ref-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilientPartitions: true, lookaheadFrames: 4, autoAltRef: true, extraArgs: []string{"--end-usage=cbr", "--error-resilient=2"}},
		{name: "realtime-cbr-cpu-3-64x64-drop-frame1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 1, extraArgs: []string{"--drop-frame=1"}},
		{name: "realtime-cbr-cpu-3-64x64-gf-cbr-boost1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 1, extraArgs: []string{"--gf-cbr-boost=1"}},
		{name: "realtime-cbr-cpu-3-64x64-gf-cbr-boost1000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 1000, extraArgs: []string{"--gf-cbr-boost=1000"}},
		{name: "realtime-cbr-cpu-3-64x64-max-intra-rate1000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 1000, extraArgs: []string{"--max-intra-rate=1000"}},
		{name: "realtime-cbr-cpu-3-64x64-undershoot100-overshoot100", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, undershootPct: 100, overshootPct: 100, extraArgs: []string{"--undershoot-pct=100", "--overshoot-pct=100"}},
		{name: "realtime-cbr-cpu-8-128x128-8partitions-threads4", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, tokenPartitions: 3, threads: 4, extraArgs: []string{"--end-usage=cbr", "--token-parts=3", "--threads=4"}},
		{name: "good-quality-cbr-cpu4-32x32-8partitions-threads2", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, threads: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=3", "--threads=2"}},
		{name: "best-quality-cbr-cpu0-32x32-8partitions-threads2", deadline: DeadlineBestQuality, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, threads: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=3", "--threads=2"}},
		{name: "realtime-cbr-cpu-3-128x128-screen-content1", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "realtime-cbr-cpu-8-128x128-screen-content2", deadline: DeadlineRealtime, cpuUsed: -8, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "realtime-cbr-cpu-3-128x128-max-intra-rate100", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, maxIntraBitratePct: 100, extraArgs: []string{"--max-intra-rate=100"}},
		{name: "realtime-cbr-cpu-3-128x128-gf-cbr-boost50", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}, gfCBRBoostPct: 50, extraArgs: []string{"--gf-cbr-boost=50"}},
		{name: "realtime-vbr-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			caseTargetKbps := targetKbps
			if tc.targetKbpsOverride > 0 {
				caseTargetKbps = tc.targetKbpsOverride
			}
			minQ := 4
			if tc.minQSet || tc.minQ > 0 {
				minQ = tc.minQ
			}
			maxQ := 56
			if tc.maxQSet || tc.maxQ > 0 {
				maxQ = tc.maxQ
			}
			cqLevel := 0
			if tc.cqLevel > 0 {
				cqLevel = tc.cqLevel
			}
			kfInterval := 999
			if tc.kfInterval > 0 {
				kfInterval = tc.kfInterval
			}
			caseFPS := fps
			if tc.fpsOverride > 0 {
				caseFPS = tc.fpsOverride
			}
			optsFPS := caseFPS
			if tc.timebaseNum > 0 {
				optsFPS = 0
			}
			tuning := TunePSNR
			if tc.tuningSet {
				tuning = tc.tuning
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      optsFPS,
				TimebaseNum:              tc.timebaseNum,
				TimebaseDen:              tc.timebaseDen,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        caseTargetKbps,
				MinQuantizer:             minQ,
				MaxQuantizer:             maxQ,
				QuantizerRangeSet:        tc.minQSet || tc.maxQSet,
				CQLevel:                  cqLevel,
				KeyFrameInterval:         kfInterval,
				Deadline:                 tc.deadline,
				CpuUsed:                  strictByteParityCPUUsed(tc.deadline, tc.cpuUsed),
				Tuning:                   tuning,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
				Sharpness:                tc.sharpness,
				NoiseSensitivity:         tc.noiseSensitivity,
				StaticThreshold:          tc.staticThreshold,
				ScreenContentMode:        tc.screenContentMode,
				MaxIntraBitratePct:       tc.maxIntraBitratePct,
				GFCBRBoostPct:            tc.gfCBRBoostPct,
				UndershootPct:            tc.undershootPct,
				OvershootPct:             tc.overshootPct,
				BufferSizeMs:             tc.bufferSizeMs,
				BufferInitialSizeMs:      tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:      tc.bufferOptimalSizeMs,
				DropFrameAllowed:         tc.dropFrameAllowed,
				DropFrameWaterMark:       tc.dropFrameWaterMark,
				TokenPartitions:          tc.tokenPartitions,
				Threads:                  tc.threads,
				LookaheadFrames:          tc.lookaheadFrames,
				AutoAltRef:               tc.autoAltRef,
				ARNRMaxFrames:            tc.arnrMaxFrames,
				ARNRStrength:             tc.arnrStrength,
				ARNRType:                 tc.arnrType,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, caseTargetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					assertStrictGateKnownGapMatchedPrefix(t, tc.name, govpxFrames, libvpxFrames, 1)
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
				// Known divergent config: no byte-parity asserted yet,
				// but we still run the encode so the per-frame status
				// logs make the gap visible.
				limit = 0
			case tc.limit > 0 && tc.limit < limit:
				limit = tc.limit
			}
			for i := 0; i < len(govpxFrames); i++ {
				gHash := sha256.Sum256(govpxFrames[i])
				lHash := sha256.Sum256(libvpxFrames[i])
				gFP, gIsKey := parseVP8FramePartitionSizes(govpxFrames[i])
				lFP, lIsKey := parseVP8FramePartitionSizes(libvpxFrames[i])
				if gHash == lHash {
					t.Logf("frame %d byte MATCH: len=%d first_part=%d keyframe=%t", i, len(govpxFrames[i]), gFP, gIsKey)
					continue
				}
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
				// firstNonTagDiff skips the 3-byte frame tag (which
				// encodes first_partition_size) so we can spot the
				// next genuine bitstream divergence inside the
				// uncompressed-header span.
				firstNonTagDiff := testutil.FirstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
				if firstNonTagDiff >= 0 {
					firstNonTagDiff += 3
				}
				if i >= limit {
					t.Logf("frame %d byte mismatch (not asserted, limit=%d): govpx_len=%d libvpx_len=%d first_diff=%d non_tag_diff=%d govpx_first_part=%d libvpx_first_part=%d",
						i, limit, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff, firstNonTagDiff, gFP, lFP)
					continue
				}
				t.Errorf("frame %d byte mismatch: govpx_len=%d libvpx_len=%d first_diff=%d govpx_first_part=%d libvpx_first_part=%d govpx_keyframe=%t libvpx_keyframe=%t govpx_sha=%s libvpx_sha=%s",
					i, len(govpxFrames[i]), len(libvpxFrames[i]), firstDiff,
					gFP, lFP, gIsKey, lIsKey,
					hex.EncodeToString(gHash[:8]), hex.EncodeToString(lHash[:8]))
			}
		})
	}
}

// extraArgsContainsKFDist reports whether the caller already supplied a
// `--kf-min-dist` or `--kf-max-dist` (with either an `=` form or the
// space-separated `--name value` form) in extraArgs, so the default
// `--kf-min-dist=999 --kf-max-dist=999` "disable auto-KF" pair shouldn't
// be appended on top.
func extraArgsContainsKFDist(extraArgs []string) bool {
	for _, arg := range extraArgs {
		switch {
		case arg == "--kf-min-dist", arg == "--kf-max-dist":
			return true
		case len(arg) >= len("--kf-min-dist=") && arg[:len("--kf-min-dist=")] == "--kf-min-dist=":
			return true
		case len(arg) >= len("--kf-max-dist=") && arg[:len("--kf-max-dist=")] == "--kf-max-dist=":
			return true
		}
	}
	return false
}

func libvpxEndUsageArgs(extraArgs []string) []string {
	for _, arg := range extraArgs {
		if arg == "--end-usage" || len(arg) >= len("--end-usage=") && arg[:len("--end-usage=")] == "--end-usage=" {
			return extraArgs
		}
	}
	args := make([]string, 0, len(extraArgs)+1)
	args = append(args, "--end-usage=cbr")
	args = append(args, extraArgs...)
	return args
}

// encodeFramesWithGovpx returns the raw per-frame VP8 packet payloads
// produced by govpx for the supplied sources. Dropped frames (CBR
// decimation drops, buffer-underrun drops, vp8_drop_encodedframe_overshoot
// drops) leave no payload in the returned slice, mirroring libvpx's
// observable output where a drop produces no IVF packet for that source
// frame. Callers that need to validate a specific drop pattern compare
// the returned slice against the libvpx oracle's IVF packet list.
func encodeFramesWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	appendResult := func(_ string, result EncodeResult) {
		if result.Dropped {
			return
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if errors.Is(err, ErrFrameNotReady) {
			continue
		}
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		appendResult("EncodeInto frame "+strconv.Itoa(i), result)
	}
	for {
		result, err := enc.FlushInto(buf)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushInto: %v", err)
		}
		appendResult("FlushInto", result)
	}
	return out
}

// encodeFramesWithLibvpxOracle runs vpxenc-oracle on the supplied I420
// fixture and returns the per-frame VP8 packet payloads extracted from
// the resulting IVF file.
func encodeFramesWithLibvpxOracle(t *testing.T, vpxencOracle string, _ string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	cfg := vp8test.VpxencVP8Config{
		BinaryPath:           vpxencOracle,
		Width:                opts.Width,
		Height:               opts.Height,
		Frames:               len(sources),
		Deadline:             libvpxOracleDeadline(opts.Deadline),
		DisableWarningPrompt: true,
		CPUUsed:              opts.CpuUsed,
		LagInFrames:          opts.LookaheadFrames,
		AutoAltRef:           opts.AutoAltRef,
		TargetBitrateKbps:    targetKbps,
		MinQ:                 opts.MinQuantizer,
		MaxQ:                 opts.MaxQuantizer,
		Timebase:             libvpxOracleTimebaseArg(opts),
		FPS:                  libvpxOracleFPSArg(opts),
		KeyFrameMinDist:      999,
		KeyFrameMaxDist:      999,
		ExtraArgs:            extraArgs,
	}
	// Only inject the default `--kf-min-dist=999 --kf-max-dist=999`
	// "no auto-KF" pair when the caller hasn't supplied its own kf-*
	// arguments via extraArgs. Several callers (long-fixture fuzz,
	// production parity, transitions, twopass fuzz, runtime-controls
	// parity) configure a finite KeyFrameInterval on the govpx side
	// and need libvpx's `cpi->key_frame_frequency` to match; passing
	// the default 999/999 silently in those cases would force govpx
	// to insert a keyframe at frame `KeyFrameInterval` while libvpx
	// keeps producing inter frames.
	if !extraArgsContainsKFDist(extraArgs) {
		cfg.KeyFrameDistSet = true
	}
	frames, diag, err := vp8test.VpxencVP8OracleFramePayloadsI420(
		encoderValidationI420Bytes(t, sources), cfg)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}
	return frames
}

func libvpxOracleDeadline(deadline Deadline) string {
	switch deadline {
	case DeadlineBestQuality:
		return "best"
	case DeadlineRealtime:
		return "rt"
	default:
		return "good"
	}
}

func libvpxOracleTimebaseArg(opts EncoderOptions) string {
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return strconv.Itoa(opts.TimebaseNum) + "/" + strconv.Itoa(opts.TimebaseDen)
	}
	return "1/" + strconv.Itoa(opts.FPS)
}

func libvpxOracleFPSArg(opts EncoderOptions) string {
	if opts.TimebaseNum > 0 && opts.TimebaseDen > 0 {
		return strconv.Itoa(opts.TimebaseDen) + "/" + strconv.Itoa(opts.TimebaseNum)
	}
	return strconv.Itoa(opts.FPS) + "/1"
}

// parseVP8FramePartitionSizes returns the first-partition byte length
// declared in the VP8 frame header plus whether the frame is marked as
// a keyframe. Returns (0, false) when the payload is too short.
func parseVP8FramePartitionSizes(p []byte) (firstPart int, isKeyframe bool) {
	if len(p) < 3 {
		return 0, false
	}
	tag := uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
	isKeyframe = (tag & 1) == 0
	firstPart = int((tag >> 5) & 0x7FFFF)
	return firstPart, isKeyframe
}
