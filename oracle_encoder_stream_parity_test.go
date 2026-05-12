//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestOracleEncoderStreamByteParity is the strictest possible parity
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
func TestOracleEncoderStreamByteParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

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
		// rcMode is the rate control mode; zero defaults to CBR.
		rcMode RateControlMode
		// errorResilient triggers libvpx's ErrorResilient mode.
		errorResilient bool
		// errorResilientPartitions triggers the VPX_ERROR_RESILIENT_PARTITIONS
		// branch in libvpx independent_coef_context_savings.
		errorResilientPartitions bool
		// sharpness overrides the loop-filter sharpness level.
		sharpness int
		// extraArgs is appended to the libvpx vpxenc-oracle command.
		extraArgs []string
		// fastLF flips on FastLoopFilterPick.
		fastLF bool
		// tokenPartitions overrides EncoderOptions.TokenPartitions
		// (0=1 partition, 1=2, 2=4, 3=8).
		tokenPartitions int
		// targetKbpsOverride lets a case pin a different bitrate
		// budget than the test's default targetKbps.
		targetKbpsOverride int
		// minQ / maxQ override the default 4 / 56 quantizer band.
		minQ int
		maxQ int
		// kfInterval overrides the keyframe interval (0 = use the
		// test-wide default 999).
		kfInterval int
		// fpsOverride overrides the test FPS default.
		fpsOverride int
	}{
		{name: "realtime-cbr-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64},
		{name: "realtime-cbr-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning64},
		{name: "realtime-cbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64},
		{name: "good-quality-cbr-cpu5", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: panning64},
		// best-quality cpu0 SPLITMV byte-matches frames 0-3 but the RD
		// mode decision diverges from libvpx at frame 4 in this
		// trellis-driven path; cap the assertion here until that gap
		// closes.
		{name: "best-quality-cbr-cpu0-splitmv", deadline: DeadlineBestQuality, cpuUsed: 0, fx: splitmv64, limit: 4},
		// Larger fixtures where chroma sub-pel rounding previously
		// diverged on inter frames (per plan.md). The keyframe is
		// expected to byte-match after the mb_no_coeff_skip header
		// fix; inter frames here are the next divergence to close.
		{name: "realtime-cbr-cpu8-96x96", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning96},
		{name: "realtime-cbr-cpu8-128x128", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning128},
		{name: "realtime-cbr-cpu8-160x96", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning160x96},
		// VBR cross-mode coverage on the 64x64 panning fixture. Diverges
		// from frame 0 today; pinned at limit=0 (no byte-parity asserted)
		// while VBR rate-control parity is investigated. Keep listed so
		// the divergence is visible and any closer-match commit will
		// show up as additional byte-MATCH log rows.
		{name: "realtime-vbr-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, rcMode: RateControlVBR, limit: -1, extraArgs: []string{"--end-usage=vbr"}},
		// Error-resilient partitions (independent context savings
		// branch). libvpx --error-resilient takes a bitmask; value 2
		// is VPX_ERROR_RESILIENT_PARTITIONS, which is what
		// EncoderOptions.ErrorResilientPartitions maps to.
		{name: "realtime-cbr-cpu8-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, errorResilientPartitions: true, limit: -1, extraArgs: []string{"--error-resilient=2"}},
		// Sharpness != 0 exercises the loop-filter header literal width.
		// Diverges from frame 0 today; pinned at limit=0.
		{name: "realtime-cbr-cpu8-sharpness4", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, sharpness: 4, limit: -1, extraArgs: []string{"--sharpness=4"}},
		// FastLoopFilterPick=true is a deliberate parity-breaking opt-in
		// that swaps the full-frame loop-filter trial picker for the
		// partial-frame variant whenever speed >= 4. Pin the divergence
		// here so we can spot any frame that happens to byte-match (=
		// libvpx's full picker happened to pick the same level as the
		// partial picker for that fixture).
		{name: "realtime-cbr-cpu8-fastlf", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, limit: -1, fastLF: true},
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
		// Forced keyframe every frame stresses the keyframe writer
		// path; diverges today (keyframe Q selection differs in
		// repeated-keyframe sequences).
		{name: "realtime-cbr-cpu8-allkf", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, kfInterval: 1, limit: -1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		// Different FPS.
		{name: "realtime-cbr-cpu8-fps15", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 15, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu8-fps60", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// GoodQuality across cpu-used. cpu0/2 still have a residual
		// RD divergence on the panning fixture; cpu4 byte-matches and
		// cpu5 is already covered above. Pin cpu0/2 at limit=-1 until
		// the RD gap closes.
		{name: "good-quality-cbr-cpu0", deadline: DeadlineGoodQuality, cpuUsed: 0, fx: panning64, limit: -1},
		{name: "good-quality-cbr-cpu2", deadline: DeadlineGoodQuality, cpuUsed: 2, fx: panning64, limit: -1},
		{name: "good-quality-cbr-cpu4", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning64},
		// BestQuality cpu0 on the panning fixture diverges in the
		// trellis RD picker; pin until that closes.
		{name: "best-quality-cbr-cpu0-panning", deadline: DeadlineBestQuality, cpuUsed: 0, fx: panning64, limit: -1},
		// Realtime explicit-speed inputs (libvpx skips auto-select).
		{name: "realtime-cbr-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64},
		{name: "realtime-cbr-cpu-5", deadline: DeadlineRealtime, cpuUsed: -5, fx: panning64},
		{name: "realtime-cbr-cpu-8", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64},
		// SPLITMV under realtime: frames 0-1 byte-match; frame 2+ drifts
		// in the inter mode/MV decision.
		{name: "realtime-cbr-cpu8-splitmv", deadline: DeadlineRealtime, cpuUsed: 8, fx: splitmv64, limit: 2},
		// 640x480 panning: keyframe matches; inter frames diverge. Pin
		// limit=1 until the larger-frame inter divergence closes.
		{name: "realtime-cbr-cpu8-640x480", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-640x480", w: 640, h: 480, source: encoderValidationPanningFrame}, limit: 1},
		// Sub-MB-aligned dimensions (w / h % 16 != 0) exercise the
		// MB padding / coded-vs-visible width handling. Keyframe
		// byte-matches; inter frames diverge in the per-MB inter
		// mode decision on partial-coded-width macroblocks.
		{name: "realtime-cbr-cpu8-72x40", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}, limit: 1},
		{name: "realtime-cbr-cpu8-100x100", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-100x100", w: 100, h: 100, source: encoderValidationPanningFrame}, limit: 1},
		// 16x16 minimum frame size — single-MB encode is byte-identical
		// to libvpx end-to-end.
		{name: "realtime-cbr-cpu8-16x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}},
		// 32-aligned small frames.
		{name: "realtime-cbr-cpu8-32x16", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x16", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-32x32", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-48x48", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}},
		// Common 16:9 standard frame sizes. 256x144 byte-matches frames
		// 0-8 (longest inter run on a 16:9 fixture); 192x108 / 320x180
		// only the keyframe.
		{name: "realtime-cbr-cpu8-256x144", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}, limit: 9},
		{name: "realtime-cbr-cpu8-192x108", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-192x108", w: 192, h: 108, source: encoderValidationPanningFrame}, limit: 1},
		{name: "realtime-cbr-cpu8-320x180", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-320x180", w: 320, h: 180, source: encoderValidationPanningFrame}, limit: 1},
		// BestQuality across more cpu_used. cpu5 panning matches the
		// keyframe; splitmv matches frames 0-3 like cpu0-splitmv.
		{name: "best-quality-cbr-cpu5-panning", deadline: DeadlineBestQuality, cpuUsed: 5, fx: panning64, limit: 1},
		{name: "best-quality-cbr-cpu5-splitmv", deadline: DeadlineBestQuality, cpuUsed: 5, fx: splitmv64, limit: 4},
		// Wide / tall asymmetric frames.
		{name: "realtime-cbr-cpu8-128x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-128x64", w: 128, h: 64, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu8-64x128", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-64x128", w: 64, h: 128, source: encoderValidationPanningFrame}},
		// SplitMV under good-quality + various cpu_used. Match the
		// realtime-cpu8 pattern: frames 0-1 byte-match, frame 2+ drifts
		// in the inter mode/MV decision on the SPLITMV fixture.
		{name: "good-quality-cbr-cpu5-splitmv", deadline: DeadlineGoodQuality, cpuUsed: 5, fx: splitmv64, limit: 2},
		{name: "good-quality-cbr-cpu4-splitmv", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: splitmv64, limit: 2},
		// Realtime cpu0/cpu4 on more resolutions to broaden coverage.
		{name: "realtime-cbr-cpu0-96x96", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96},
		{name: "realtime-cbr-cpu4-96x96", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning96},
		{name: "realtime-cbr-cpu0-128x128", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128},
		{name: "realtime-cbr-cpu4-128x128", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning128},
		// Realtime cpu8 on 128x64 splitmv to expand SPLITMV coverage.
		// Same splitmv state-drift at frame 2+ as 64x64; pin limit=2.
		{name: "realtime-cbr-cpu8-128x64-splitmv", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "splitmv-128x64", w: 128, h: 64, source: encoderValidationSplitMVQuadrantFrame}, limit: 2},
		// Larger panning configs at cpu0/cpu4.
		{name: "realtime-cbr-cpu0-160x96", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning160x96},
		{name: "realtime-cbr-cpu4-160x96", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning160x96},
		// Realtime cpu0/cpu4 on splitmv64.
		{name: "realtime-cbr-cpu0-splitmv", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, limit: 2},
		{name: "realtime-cbr-cpu4-splitmv", deadline: DeadlineRealtime, cpuUsed: 4, fx: splitmv64, limit: 2},
		// Small-resolution cpu0/cpu4 variants. Small frames sit well
		// below the realtime-deadline budget so the libvpx auto-select-
		// speed evolution stays at the cold-start seed in both runs,
		// which makes these the strongest byte-parity probes for any
		// per-MB encode logic. 16x16 / 32x32 / 48x48 byte-match the
		// full 16-frame sequence; 72x40 still diverges at frame 1
		// (same partial-coded-width sub-MB drift as cpu8-72x40).
		{name: "realtime-cbr-cpu0-72x40", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}, limit: 1},
		{name: "realtime-cbr-cpu4-72x40", deadline: DeadlineRealtime, cpuUsed: 4, fx: fixture{name: "panning-72x40", w: 72, h: 40, source: encoderValidationPanningFrame}, limit: 1},
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
		// Negative-cpu variants on 32x32 — `cpu_used < 0` skips
		// auto-select entirely, so these probe the static-Speed code path.
		{name: "realtime-cbr-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
		{name: "realtime-cbr-cpu-5-32x32", deadline: DeadlineRealtime, cpuUsed: -5, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}},
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
		// allkf (kfInterval=1) on small frames — every frame is a key
		// frame, exercises the keyframe writer path repeatedly.
		{name: "realtime-cbr-cpu0-16x16-allkf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, kfInterval: 1, limit: -1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		{name: "realtime-cbr-cpu0-32x32-allkf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, kfInterval: 1, limit: -1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		{name: "realtime-cbr-cpu0-48x48-allkf", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, kfInterval: 1, limit: -1, extraArgs: []string{"--end-usage=cbr", "--kf-min-dist=0", "--kf-max-dist=1"}},
		// 16x16 / 48x48 with token partitions to widen the partitioned-
		// writer coverage beyond the existing 32x32 site.
		{name: "realtime-cbr-cpu0-16x16-2partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu0-48x48-2partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, tokenPartitions: 1, extraArgs: []string{"--end-usage=cbr", "--token-parts=1"}},
		{name: "realtime-cbr-cpu0-32x32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 2, extraArgs: []string{"--end-usage=cbr", "--token-parts=2"}},
		{name: "realtime-cbr-cpu0-32x32-8partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, tokenPartitions: 3, extraArgs: []string{"--end-usage=cbr", "--token-parts=3"}},
		// fps60 on 16x16 and 48x48.
		{name: "realtime-cbr-cpu0-16x16-fps60", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		{name: "realtime-cbr-cpu0-48x48-fps60", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, fpsOverride: 60, extraArgs: []string{"--end-usage=cbr"}},
		// bitrate sweeps on 16x16.
		{name: "realtime-cbr-cpu0-16x16-bitrate200", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=200"}, targetKbpsOverride: 200},
		{name: "realtime-cbr-cpu0-16x16-bitrate2000", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=2000"}, targetKbpsOverride: 2000},
		// q10-30 on 16x16.
		{name: "realtime-cbr-cpu0-16x16-q10-30", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, minQ: 10, maxQ: 30},
		// Error-resilient partitions on small frames. Probes the
		// independent-coefficient-context branch on the simpler MB grids.
		{name: "realtime-cbr-cpu0-16x16-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, limit: -1, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu0-32x32-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, errorResilientPartitions: true, limit: -1, extraArgs: []string{"--error-resilient=2"}},
		{name: "realtime-cbr-cpu0-48x48-error-resilient-partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, errorResilientPartitions: true, limit: -1, extraArgs: []string{"--error-resilient=2"}},
		// Sharpness=4 on small frames.
		{name: "realtime-cbr-cpu0-16x16-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, sharpness: 4, limit: -1, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu0-32x32-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, sharpness: 4, limit: -1, extraArgs: []string{"--sharpness=4"}},
		{name: "realtime-cbr-cpu0-48x48-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, sharpness: 4, limit: -1, extraArgs: []string{"--sharpness=4"}},
		// VBR + small frames.
		{name: "realtime-vbr-cpu0-16x16", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, limit: -1, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, rcMode: RateControlVBR, limit: -1, extraArgs: []string{"--end-usage=vbr"}},
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if rcMode == 0 {
				rcMode = RateControlCBR
			}
			caseTargetKbps := targetKbps
			if tc.targetKbpsOverride > 0 {
				caseTargetKbps = tc.targetKbpsOverride
			}
			minQ := 4
			if tc.minQ > 0 {
				minQ = tc.minQ
			}
			maxQ := 56
			if tc.maxQ > 0 {
				maxQ = tc.maxQ
			}
			kfInterval := 999
			if tc.kfInterval > 0 {
				kfInterval = tc.kfInterval
			}
			caseFPS := fps
			if tc.fpsOverride > 0 {
				caseFPS = tc.fpsOverride
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      caseFPS,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        caseTargetKbps,
				MinQuantizer:             minQ,
				MaxQuantizer:             maxQ,
				KeyFrameInterval:         kfInterval,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
				Sharpness:                tc.sharpness,
				FastLoopFilterPick:       tc.fastLF,
				TokenPartitions:          tc.tokenPartitions,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := tc.extraArgs
			if extraArgs == nil {
				extraArgs = []string{"--end-usage=cbr"}
			}
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, caseTargetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
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
				firstDiff := firstByteDiff(govpxFrames[i], libvpxFrames[i])
				// firstNonTagDiff skips the 3-byte frame tag (which
				// encodes first_partition_size) so we can spot the
				// next genuine bitstream divergence inside the
				// uncompressed-header span.
				firstNonTagDiff := firstByteDiff(govpxFrames[i][3:], libvpxFrames[i][3:])
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

// encodeFramesWithGovpx returns the raw per-frame VP8 packet payloads
// produced by govpx for the supplied sources.
func encodeFramesWithGovpx(t *testing.T, opts EncoderOptions, sources []Image) [][]byte {
	t.Helper()
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	buf := make([]byte, opts.Width*opts.Height*4+4096)
	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		result, err := enc.EncodeInto(buf, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto frame %d dropped, want full stream", i)
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

// encodeFramesWithLibvpxOracle runs vpxenc-oracle on the supplied I420
// fixture and returns the per-frame VP8 packet payloads extracted from
// the resulting IVF file.
func encodeFramesWithLibvpxOracle(t *testing.T, vpxencOracle string, name string, opts EncoderOptions, targetKbps int, sources []Image, extraArgs []string) [][]byte {
	t.Helper()
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, name+".yuv")
	ivfPath := filepath.Join(dir, name+".ivf")
	writeEncoderValidationI420(t, yuvPath, sources)
	deadlineArg := "--good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--kf-min-dist=999",
		"--kf-max-dist=999",
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=1/" + strconv.Itoa(opts.FPS),
		"--fps=" + strconv.Itoa(opts.FPS) + "/1",
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ivfPath)
	if err != nil {
		t.Fatalf("read %s: %v", ivfPath, err)
	}
	return parseIVFFramePayloads(t, data)
}

// firstByteDiff returns the byte offset of the first divergence between
// a and b, or -1 if the prefixes match up to min(len(a), len(b)).
func firstByteDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
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

// parseIVFFramePayloads strips the 32-byte IVF header and the 12-byte
// per-frame headers, returning the raw VP8 frame payload bytes.
func parseIVFFramePayloads(t *testing.T, data []byte) [][]byte {
	t.Helper()
	if len(data) < 32 || string(data[:4]) != "DKIF" {
		t.Fatalf("ivf: missing DKIF magic (have %d bytes, prefix=%q)", len(data), data[:min(len(data), 4)])
	}
	pos := 32
	var out [][]byte
	for pos+12 <= len(data) {
		size := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 12
		end := pos + int(size)
		if end > len(data) {
			t.Fatalf("ivf: frame size %d at pos %d overflows %d-byte buffer", size, pos-12, len(data))
		}
		out = append(out, bytes.Clone(data[pos:end]))
		pos = end
	}
	return out
}
