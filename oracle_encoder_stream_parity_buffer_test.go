//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestOracleEncoderStreamByteParityBuffer widens the strict byte-parity
// matrix beyond the buffer-size combinations already pinned by
// [TestOracleEncoderStreamByteParity] (which covers
// {500/500/500, 1000/500/600, 2000/1000/1500, 4000/1000/3000}). It
// targets the buffer-model edge cases — very tight buffers, very
// large buffers, near-zero buffers, asymmetric initial/optimal vs.
// total — plus their cross-products with bitrate, drop-frame
// threshold, max-intra-rate, gf-cbr-boost, and undershoot/overshoot
// extremes. The probe surface here is the libvpx rate-control buffer
// model arithmetic, so any divergence in the buffer-level update or
// drop-frame gate becomes visible in the per-frame logs.
//
// Each subtest follows the protocol of the base matrix: feed the
// same I420 fixture to govpx and to the patched vpxenc-oracle under
// matching options, then assert the encoded frame payloads
// byte-match. Cases that diverge are pinned with `limit:` so the
// gap stays visible without regressing the strict gate.
func TestOracleEncoderStreamByteParityBuffer(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}

	cases := []struct {
		name                string
		deadline            Deadline
		cpuUsed             int
		fx                  fixture
		limit               int
		rcMode              RateControlMode
		rcModeSet           bool
		maxIntraBitratePct  int
		gfCBRBoostPct       int
		undershootPct       int
		overshootPct        int
		bufferSizeMs        int
		bufferInitialSizeMs int
		bufferOptimalSizeMs int
		dropFrameAllowed    bool
		dropFrameWaterMark  int
		targetKbpsOverride  int
		extraArgs           []string
	}{
		// 1. Tight buffer at small fixtures. 200/100/150 is the
		// real-time WebRTC "tight" preset; 500/100/300 is a
		// smaller initial-vs-optimal asymmetry. Both pin the
		// libvpx buffer-level clamp at sub-1s buffer durations.
		{name: "buffer-200-100-150-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "buffer-200-100-150-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "buffer-500-100-300-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, bufferSizeMs: 500, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 300, extraArgs: []string{"--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"}},
		{name: "buffer-500-100-300-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 500, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 300, extraArgs: []string{"--buf-sz=500", "--buf-initial-sz=100", "--buf-optimal-sz=300"}},

		// 1b. Near-zero buffer. 1/1/1 stresses the path where
		// libvpx clamps tiny buffer values inside the
		// vp8_rc_init_minq_luts allocator. Pinned at strict
		// byte parity (was a divergence gap until the
		// CBR-full-buffer active-worst arithmetic and
		// raw-target-rate cap were ported; see
		// libvpxCBRFullBufferActiveWorst and
		// libvpxRawTargetRateCapKbps).
		{name: "buffer-1-1-1-realtime-cpu-3-32x32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, bufferSizeMs: 1, bufferInitialSizeMs: 1, bufferOptimalSizeMs: 1, extraArgs: []string{"--buf-sz=1", "--buf-initial-sz=1", "--buf-optimal-sz=1"}},
		{name: "buffer-1-1-1-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 1, bufferInitialSizeMs: 1, bufferOptimalSizeMs: 1, extraArgs: []string{"--buf-sz=1", "--buf-initial-sz=1", "--buf-optimal-sz=1"}},

		// 2. Very large buffer (10000/5000/7500). Pins the
		// buffer-saturation path where the rate controller has
		// plenty of slack — libvpx still applies its minimum-q
		// floor here, so byte parity should hold.
		{name: "buffer-10000-5000-7500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 10000, bufferInitialSizeMs: 5000, bufferOptimalSizeMs: 7500, extraArgs: []string{"--buf-sz=10000", "--buf-initial-sz=5000", "--buf-optimal-sz=7500"}},
		{name: "buffer-10000-5000-7500-realtime-cpu0-64x64", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning64, bufferSizeMs: 10000, bufferInitialSizeMs: 5000, bufferOptimalSizeMs: 7500, extraArgs: []string{"--buf-sz=10000", "--buf-initial-sz=5000", "--buf-optimal-sz=7500"}},

		// Asymmetric: size >> initial + optimal (large
		// over-provisioned reservoir with low starting fill).
		{name: "buffer-8000-200-500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 8000, bufferInitialSizeMs: 200, bufferOptimalSizeMs: 500, extraArgs: []string{"--buf-sz=8000", "--buf-initial-sz=200", "--buf-optimal-sz=500"}},

		// 3. Drop-frame threshold sweep at 64x64 across cpu=-3
		// and cpu=8. The drop-frame gate is per-frame; with the
		// smooth panning fixture and the default 700kbps target,
		// none of these should actually drop, so they pin the
		// "drop gate enabled, no drops" branch of the buffer
		// model.
		{name: "drop-frame5-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 5, extraArgs: []string{"--drop-frame=5"}},
		{name: "drop-frame25-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 25, extraArgs: []string{"--drop-frame=25"}},
		{name: "drop-frame75-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 75, extraArgs: []string{"--drop-frame=75"}},
		{name: "drop-frame99-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 99, extraArgs: []string{"--drop-frame=99"}},
		{name: "drop-frame5-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 5, extraArgs: []string{"--drop-frame=5"}},
		{name: "drop-frame25-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 25, extraArgs: []string{"--drop-frame=25"}},
		{name: "drop-frame75-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 75, extraArgs: []string{"--drop-frame=75"}},
		{name: "drop-frame99-realtime-cpu8-64x64", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, dropFrameAllowed: true, dropFrameWaterMark: 99, extraArgs: []string{"--drop-frame=99"}},

		// 4. Undershoot/overshoot grid. (0,0) pins the
		// zero-percent path; (100,0)/(0,100) pin the asymmetric
		// trims; (100,100) is the libvpx upper bound — libvpx
		// rejects rc_undershoot_pct/rc_overshoot_pct > 100
		// ("out of range [..100]"), so the task's (200,200)
		// suggestion can never be checked against the oracle
		// and is mapped to the documented edge instead.
		{name: "undershoot0-overshoot0-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 0, overshootPct: 0, extraArgs: []string{"--undershoot-pct=0", "--overshoot-pct=0"}},
		{name: "undershoot100-overshoot0-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 100, overshootPct: 0, extraArgs: []string{"--undershoot-pct=100", "--overshoot-pct=0"}},
		{name: "undershoot0-overshoot100-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 0, overshootPct: 100, extraArgs: []string{"--undershoot-pct=0", "--overshoot-pct=100"}},
		{name: "undershoot100-overshoot100-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, undershootPct: 100, overshootPct: 100, extraArgs: []string{"--undershoot-pct=100", "--overshoot-pct=100"}},

		// 5. max-intra-rate extremes. 50 caps the keyframe budget
		// well below the inter target; 500/2000/10000 progressively
		// relax it. Frame 0 is the only keyframe in this 16-frame
		// budget so this primarily pins the KF-cap arithmetic.
		{name: "max-intra-rate50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 50, extraArgs: []string{"--max-intra-rate=50"}},
		{name: "max-intra-rate500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 500, extraArgs: []string{"--max-intra-rate=500"}},
		{name: "max-intra-rate2000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 2000, extraArgs: []string{"--max-intra-rate=2000"}},
		{name: "max-intra-rate10000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, maxIntraBitratePct: 10000, extraArgs: []string{"--max-intra-rate=10000"}},

		// 6. gf-cbr-boost extremes. 0 disables the golden-frame
		// CBR boost; 500/2000 push the upper-bound clamp.
		{name: "gf-cbr-boost0-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 0, extraArgs: []string{"--gf-cbr-boost=0"}},
		{name: "gf-cbr-boost500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 500, extraArgs: []string{"--gf-cbr-boost=500"}},
		{name: "gf-cbr-boost2000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, gfCBRBoostPct: 2000, extraArgs: []string{"--gf-cbr-boost=2000"}},

		// 7. CBR bitrate extremes at 64x64. 25 kbps is below the
		// libvpx-recommended VP8 minimum (the rate allocator
		// clamps internally); 5000/10000 push the upper-band
		// clamp.
		{name: "low-bitrate25-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 25, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=25"}},
		{name: "low-bitrate50-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 50, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=50"}},
		{name: "high-bitrate5000-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 5000, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=5000"}},
		{name: "high-bitrate10000-cbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, targetKbpsOverride: 10000, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=10000"}},

		// 8. VBR bitrate extremes at 64x64. Mirrors the CBR
		// sweep so the VBR allocator's clamp band is also
		// pinned.
		{name: "low-bitrate25-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 25, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=25"}},
		{name: "low-bitrate50-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 50, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=50"}},
		{name: "high-bitrate5000-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 5000, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=5000"}},
		{name: "high-bitrate10000-vbr-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, rcMode: RateControlVBR, rcModeSet: true, targetKbpsOverride: 10000, extraArgs: []string{"--end-usage=vbr", "--target-bitrate=10000"}},

		// Cross with bitrate extremes: tight buffer + low bitrate
		// (the underflow-prone combination) and large buffer +
		// high bitrate (the overflow-prone one).
		{name: "buffer-200-100-150-low-bitrate50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, targetKbpsOverride: 50, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=50", "--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150"}},
		{name: "buffer-10000-5000-7500-high-bitrate5000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 10000, bufferInitialSizeMs: 5000, bufferOptimalSizeMs: 7500, targetKbpsOverride: 5000, extraArgs: []string{"--end-usage=cbr", "--target-bitrate=5000", "--buf-sz=10000", "--buf-initial-sz=5000", "--buf-optimal-sz=7500"}},

		// Cross with drop-frame: tight buffer + drop gate. With
		// 25 kbps the under-provisioned buffer plus aggressive
		// drop threshold should still byte-match (the panning
		// fixture is smooth enough that the per-frame bit
		// consumption stays within bounds).
		{name: "buffer-200-100-150-drop-frame50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, dropFrameAllowed: true, dropFrameWaterMark: 50, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=50"}},

		// Cross with max-intra-rate / gf-cbr-boost: tight
		// buffer interacts with the KF/GF allocator caps.
		{name: "buffer-200-100-150-max-intra-rate500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, maxIntraBitratePct: 500, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--max-intra-rate=500"}},
		{name: "buffer-200-100-150-gf-cbr-boost500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, bufferSizeMs: 200, bufferInitialSizeMs: 100, bufferOptimalSizeMs: 150, gfCBRBoostPct: 500, extraArgs: []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--gf-cbr-boost=500"}},
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
			opts := EncoderOptions{
				Width:               tc.fx.w,
				Height:              tc.fx.h,
				FPS:                 fps,
				RateControlMode:     rcMode,
				TargetBitrateKbps:   caseTargetKbps,
				MinQuantizer:        4,
				MaxQuantizer:        56,
				KeyFrameInterval:    999,
				Deadline:            tc.deadline,
				CpuUsed:             tc.cpuUsed,
				Tuning:              TunePSNR,
				MaxIntraBitratePct:  tc.maxIntraBitratePct,
				GFCBRBoostPct:       tc.gfCBRBoostPct,
				UndershootPct:       tc.undershootPct,
				OvershootPct:        tc.overshootPct,
				BufferSizeMs:        tc.bufferSizeMs,
				BufferInitialSizeMs: tc.bufferInitialSizeMs,
				BufferOptimalSizeMs: tc.bufferOptimalSizeMs,
				DropFrameAllowed:    tc.dropFrameAllowed,
				DropFrameWaterMark:  tc.dropFrameWaterMark,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, caseTargetKbps, sources, extraArgs)

			if len(govpxFrames) != len(libvpxFrames) {
				if tc.limit < 0 {
					t.Logf("frame count mismatch (not asserted, known gap): govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
					return
				}
				t.Fatalf("frame count mismatch: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
			}

			limit := len(govpxFrames)
			switch {
			case tc.limit < 0:
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

func TestOracleEncoderStreamByteParityRTCExternalRateControl(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run RTC external-rate-control byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps    = 30
		frames = 16
		width  = 64
		height = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	cases := []struct {
		name                string
		targetKbps          int
		undershootPct       int
		overshootPct        int
		bufferSizeMs        int
		bufferInitialSizeMs int
		bufferOptimalSizeMs int
		dropFrameAllowed    bool
		dropFrameWaterMark  int
		extraArgs           []string
	}{
		{
			name:                "drop-buffer-low-bitrate",
			targetKbps:          80,
			bufferSizeMs:        200,
			bufferInitialSizeMs: 100,
			bufferOptimalSizeMs: 150,
			dropFrameAllowed:    true,
			dropFrameWaterMark:  60,
			extraArgs:           []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=60"},
		},
		{
			name:       "default-buffer-mid-bitrate",
			targetKbps: 700,
		},
		{
			name:          "undershoot-overshoot-edges",
			targetKbps:    700,
			undershootPct: 0,
			overshootPct:  100,
			extraArgs:     []string{"--undershoot-pct=0", "--overshoot-pct=100"},
		},
		{
			name:                "tight-buffer-mid-bitrate",
			targetKbps:          400,
			bufferSizeMs:        200,
			bufferInitialSizeMs: 100,
			bufferOptimalSizeMs: 150,
			dropFrameAllowed:    true,
			dropFrameWaterMark:  50,
			extraArgs:           []string{"--buf-sz=200", "--buf-initial-sz=100", "--buf-optimal-sz=150", "--drop-frame=50"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := EncoderOptions{
				Width:                  width,
				Height:                 height,
				FPS:                    fps,
				RateControlMode:        RateControlCBR,
				TargetBitrateKbps:      tc.targetKbps,
				MinQuantizer:           4,
				MaxQuantizer:           56,
				KeyFrameInterval:       999,
				Deadline:               DeadlineRealtime,
				CpuUsed:                -3,
				Tuning:                 TunePSNR,
				UndershootPct:          tc.undershootPct,
				OvershootPct:           tc.overshootPct,
				BufferSizeMs:           tc.bufferSizeMs,
				BufferInitialSizeMs:    tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:    tc.bufferOptimalSizeMs,
				DropFrameAllowed:       tc.dropFrameAllowed,
				DropFrameWaterMark:     tc.dropFrameWaterMark,
				RTCExternalRateControl: true,
			}
			extraArgs := []string{"--end-usage=cbr", "--rtc-external=1"}
			extraArgs = append(extraArgs, tc.extraArgs...)
			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "rtc-external-"+tc.name, opts, tc.targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "rtc-external-rate-control-"+tc.name, govpxFrames, libvpxFrames, 0)
		})
	}
}
