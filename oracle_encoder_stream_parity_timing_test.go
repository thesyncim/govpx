//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestOracleEncoderStreamByteParityTiming pins byte-parity across the
// FPS / timebase / PTS axes that the existing matrices barely touch.
// The base parity matrix only covers FPS={15, 30, 60} and a single
// timebase override (1001/30000). Real callers run vp8 over a much
// wider grid of cadences — cinematic 24p, PAL 25/50, NTSC 1001/30000
// and 1001/60000, plus the high-frame-rate 90/120 corner — and each
// fps changes the per-frame rate-control budget, the section target
// computation, and (under VBR) the rate-allocator denominator. Any
// arithmetic regression in those paths surfaces here as a byte
// divergence on a smooth panning fixture.
//
// Each subtest follows the same protocol as [TestOracleEncoderStreamByteParity]:
// feed the same I420 fixture into govpx and the patched vpxenc-oracle
// with matching options, then assert byte equality of the per-frame
// VP8 packet payloads. Cases that diverge are pinned with `limit:` so
// the gap is visible in the per-frame status lines without regressing
// the strict gate.
func TestOracleEncoderStreamByteParityTiming(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	const (
		targetKbps    = 700
		defaultFrames = 16
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}

	cases := []struct {
		name     string
		deadline Deadline
		cpuUsed  int
		fx       fixture
		// limit semantics mirror the base matrix: 0=require full
		// budget, >0=pin clean prefix, <0=disable strict gate.
		limit int
		// frames overrides the default 16-frame budget. Used by the
		// long-PTS cases that need a longer sequence to stress the
		// fps-tick accumulator.
		frames int
		// rcMode + rcModeSet mirror the base matrix.
		rcMode    RateControlMode
		rcModeSet bool
		// fpsOverride is the simple-FPS axis. 0 leaves the case using
		// timebase overrides only.
		fpsOverride int
		// timebaseNum / timebaseDen drive the explicit caller
		// timebase. The harness passes one tick per frame so the
		// effective oracle --fps becomes timebaseDen/timebaseNum.
		timebaseNum int
		timebaseDen int
		// threads / tokenPartitions / cqLevel mirror the base matrix
		// so the FPS axis can be crossed with the threading and RC
		// axes the base matrix already pins at fps=30.
		threads         int
		tokenPartitions int
		lookaheadFrames int
		autoAltRef      bool
		cqLevel         int
		extraArgs       []string
	}{
		// ----- FPS sweep on 16x16 single-MB. The smallest fixture that
		// byte-matches at the base fps=30/cpu-3 anchor; covering the
		// new FPS values here pins the per-frame-budget arithmetic on
		// the simplest possible bitstream.
		{name: "realtime-cbr-cpu-3-16x16-fps5", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 5},
		{name: "realtime-cbr-cpu-3-16x16-fps10", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 10},
		{name: "realtime-cbr-cpu-3-16x16-fps24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 24},
		{name: "realtime-cbr-cpu-3-16x16-fps25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 25},
		{name: "realtime-cbr-cpu-3-16x16-fps50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 50},
		{name: "realtime-cbr-cpu-3-16x16-fps90", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 90},
		{name: "realtime-cbr-cpu-3-16x16-fps120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 120},

		// ----- FPS sweep on 32x32. Each row exercises the
		// per-frame-budget trajectory plus the multi-MB writer
		// path (rate-control bands track the MB count).
		{name: "realtime-cbr-cpu-3-32x32-fps5", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 5},
		{name: "realtime-cbr-cpu-3-32x32-fps10", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 10},
		{name: "realtime-cbr-cpu-3-32x32-fps24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 24},
		{name: "realtime-cbr-cpu-3-32x32-fps25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 25},
		{name: "realtime-cbr-cpu-3-32x32-fps50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 50},
		{name: "realtime-cbr-cpu-3-32x32-fps90", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 90},
		{name: "realtime-cbr-cpu-3-32x32-fps120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 120},

		// ----- FPS sweep on 64x64 panning. cpu-3 + 64x64 panning is
		// the base-matrix anchor that byte-matches at fps={15,30,60};
		// the new FPS values here extend that anchor across the
		// full cinematic/PAL/NTSC/HFR axis on a 16-MB grid.
		{name: "realtime-cbr-cpu-3-64x64-fps5", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 5},
		{name: "realtime-cbr-cpu-3-64x64-fps10", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 10},
		{name: "realtime-cbr-cpu-3-64x64-fps24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 24},
		{name: "realtime-cbr-cpu-3-64x64-fps25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 25},
		{name: "realtime-cbr-cpu-3-64x64-fps50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 50},
		{name: "realtime-cbr-cpu-3-64x64-fps90", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 90},
		{name: "realtime-cbr-cpu-3-64x64-fps120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 120},

		// ----- Timebase sweep at 16x16. Drives the explicit
		// timebase path (FPS=0 in govpx, --timebase + --fps in
		// libvpx) for every real-world cadence:
		//   1001/24000 -> 24p
		//   1001/25000 -> 25p (PAL)
		//   1001/30000 -> NTSC (already pinned at fps=30 by the base
		//                  matrix; re-pinned here at cpu-3 for
		//                  completeness)
		//   1001/60000 -> 60p NTSC
		//   1/24, 1/25, 1/50, 1/90, 1/120 -> integer fps via timebase
		{name: "realtime-cbr-cpu-3-16x16-timebase-1001-24000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1001, timebaseDen: 24000},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1001-25000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1001, timebaseDen: 25000},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1001-30000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1001, timebaseDen: 30000},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1001-60000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1001, timebaseDen: 60000},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1-24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1, timebaseDen: 24},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1-25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1, timebaseDen: 25},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1-50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1, timebaseDen: 50},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1-90", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1, timebaseDen: 90},
		{name: "realtime-cbr-cpu-3-16x16-timebase-1-120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1, timebaseDen: 120},

		// ----- Timebase sweep at 32x32.
		{name: "realtime-cbr-cpu-3-32x32-timebase-1001-24000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1001, timebaseDen: 24000},
		{name: "realtime-cbr-cpu-3-32x32-timebase-1001-25000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1001, timebaseDen: 25000},
		{name: "realtime-cbr-cpu-3-32x32-timebase-1001-60000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1001, timebaseDen: 60000},
		{name: "realtime-cbr-cpu-3-32x32-timebase-1-24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1, timebaseDen: 24},
		{name: "realtime-cbr-cpu-3-32x32-timebase-1-25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1, timebaseDen: 25},
		{name: "realtime-cbr-cpu-3-32x32-timebase-1-90", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1, timebaseDen: 90},
		{name: "realtime-cbr-cpu-3-32x32-timebase-1-120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1, timebaseDen: 120},

		// ----- Timebase sweep at 64x64.
		{name: "realtime-cbr-cpu-3-64x64-timebase-1001-24000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 24000},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1001-25000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 25000},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1001-60000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 60000},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1-24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1, timebaseDen: 24},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1-25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1, timebaseDen: 25},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1-90", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1, timebaseDen: 90},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1-120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1, timebaseDen: 120},

		// ----- Timing + lookahead/ARF. ARF scheduling is PTS-sensitive, so
		// cross both simple FPS and explicit timebase with usable lag.
		{name: "realtime-cbr-cpu-3-64x64-fps24-lookahead4-auto-alt-ref", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 24, lookaheadFrames: 4, autoAltRef: true},
		{name: "realtime-cbr-cpu-3-64x64-timebase-1001-30000-lookahead4-auto-alt-ref", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 30000, lookaheadFrames: 4, autoAltRef: true},

		// ----- Cross-product: FPS + cpu_used + threads. The base
		// matrix only covers threads=2 at fps=30; these rows pin
		// the threaded row-pool against the FPS-driven rate
		// trajectory at the cinematic / PAL / HFR cadences.
		{name: "realtime-cbr-cpu-3-64x64-fps24-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 24, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-fps25-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 25, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-fps50-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 50, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-3-64x64-fps120-threads2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 120, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-8-64x64-fps24-threads2", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 24, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "realtime-cbr-cpu-8-64x64-fps60-threads2", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 60, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "good-quality-cbr-cpu4-32x32-fps24-threads2", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, fpsOverride: 24, threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "good-quality-cbr-cpu4-32x32-fps50-threads2", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning32, fpsOverride: 50, threads: 2, extraArgs: []string{"--threads=2"}},

		// ----- Cross-product: FPS + cpu_used spanning the cpu axis.
		// cpu=8 takes the fast static-Speed branch where the per-
		// frame budget arithmetic feeds the early-exit gate.
		{name: "realtime-cbr-cpu8-64x64-fps24", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 24},
		{name: "realtime-cbr-cpu8-64x64-fps25", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 25},
		{name: "realtime-cbr-cpu8-64x64-fps50", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 50},
		{name: "realtime-cbr-cpu8-64x64-fps120", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning64, fpsOverride: 120},
		{name: "realtime-cbr-cpu-8-64x64-fps24", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 24},
		{name: "realtime-cbr-cpu-8-64x64-fps25", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 25},
		{name: "realtime-cbr-cpu-8-64x64-fps50", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 50},
		{name: "realtime-cbr-cpu-8-64x64-fps120", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning64, fpsOverride: 120},
		{name: "good-quality-cbr-cpu4-16x16-fps24", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, fpsOverride: 24},
		{name: "good-quality-cbr-cpu4-16x16-fps25", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, fpsOverride: 25},
		{name: "good-quality-cbr-cpu4-16x16-fps50", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, fpsOverride: 50},
		{name: "good-quality-cbr-cpu4-16x16-fps120", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: panning16, fpsOverride: 120},

		// ----- FPS + RC mode. VBR's section-target computation
		// depends on fps (target_bandwidth = bitrate * fps_den /
		// fps_num); CQ skips the bandwidth path but still respects
		// the fps-derived per-frame duration. These rows pin both.
		{name: "realtime-vbr-cpu-3-16x16-fps24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 24, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-16x16-fps25", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 25, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-16x16-fps50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 50, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-16x16-fps120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 120, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-32x32-fps24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 24, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-32x32-fps50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 50, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-64x64-fps24", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 24, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-64x64-fps50", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 50, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-64x64-fps120", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 120, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-64x64-timebase-1001-24000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 24000, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},
		{name: "realtime-vbr-cpu-3-64x64-timebase-1001-60000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 60000, rcMode: RateControlVBR, rcModeSet: true, extraArgs: []string{"--end-usage=vbr"}},

		// FPS + CQ. CQ mode anchors on cq_level so the fps axis
		// only changes the frame budget the keyframe writer sees;
		// these rows pin that.
		{name: "realtime-cq-cpu-3-16x16-fps24-cq20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 24, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cq-cpu-3-16x16-fps50-cq20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 50, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-cq-cpu-3-16x16-fps120-cq20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 120, rcMode: RateControlCQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=cq", "--cq-level=20"}},
		{name: "realtime-q-cpu-3-16x16-fps24-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 24, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},
		{name: "realtime-q-cpu-3-16x16-fps50-q20", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 50, rcMode: RateControlQ, rcModeSet: true, cqLevel: 20, extraArgs: []string{"--end-usage=q", "--cq-level=20"}},

		// ----- Long-running PTS / accumulator stability. The
		// strict harness feeds one tick per frame so the
		// absolute PTS reaches ~frames * 1 ticks. The high
		// timebase (1/120, 1001/60000) maximizes the tick count
		// inside the rate-control / write-out path for each
		// real-time frame. 64 frames keeps wallclock bounded.
		{name: "long-pts-realtime-cbr-cpu-3-16x16-fps120-frames64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, fpsOverride: 120, frames: 64},
		{name: "long-pts-realtime-cbr-cpu-3-16x16-timebase-1-120-frames64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1, timebaseDen: 120, frames: 64},
		{name: "long-pts-realtime-cbr-cpu-3-16x16-timebase-1001-60000-frames64", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning16, timebaseNum: 1001, timebaseDen: 60000, frames: 64},
		{name: "long-pts-realtime-cbr-cpu-3-32x32-fps120-frames48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, fpsOverride: 120, frames: 48},
		{name: "long-pts-realtime-cbr-cpu-3-32x32-timebase-1001-60000-frames48", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, timebaseNum: 1001, timebaseDen: 60000, frames: 48},
		// Longer 64x64 budget exercises the same accumulator on a
		// fully populated MB grid where rounding under the per-
		// frame budget compounds across more frames.
		{name: "long-pts-realtime-cbr-cpu-3-64x64-fps120-frames32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, fpsOverride: 120, frames: 32},
		{name: "long-pts-realtime-cbr-cpu-3-64x64-timebase-1001-60000-frames32", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, timebaseNum: 1001, timebaseDen: 60000, frames: 32},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := defaultFrames
			if tc.frames > 0 {
				frames = tc.frames
			}
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			rcMode := tc.rcMode
			if !tc.rcModeSet {
				rcMode = RateControlCBR
			}
			cqLevel := 0
			if tc.cqLevel > 0 {
				cqLevel = tc.cqLevel
			}
			caseFPS := 30
			if tc.fpsOverride > 0 {
				caseFPS = tc.fpsOverride
			}
			optsFPS := caseFPS
			if tc.timebaseNum > 0 {
				optsFPS = 0
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               optsFPS,
				TimebaseNum:       tc.timebaseNum,
				TimebaseDen:       tc.timebaseDen,
				RateControlMode:   rcMode,
				TargetBitrateKbps: targetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				CQLevel:           cqLevel,
				KeyFrameInterval:  999,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				TokenPartitions:   tc.tokenPartitions,
				Threads:           tc.threads,
				LookaheadFrames:   tc.lookaheadFrames,
				AutoAltRef:        tc.autoAltRef,
			}

			govpxFrames := encodeFramesWithGovpx(t, opts, sources)
			extraArgs := libvpxEndUsageArgs(tc.extraArgs)
			libvpxFrames := encodeFramesWithLibvpxOracle(t, vpxencOracle, tc.name, opts, targetKbps, sources, extraArgs)

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
