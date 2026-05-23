//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8OracleEncoderStreamByteParityLossRecovery exercises packet-loss /
// error-resilience recovery flows that the existing matrices do not pin:
//
//   - Mid-stream EncodeForceKeyFrame insertions at multiple frame
//     indices (the canonical "recover after packet loss" pattern).
//   - EncodeForceKeyFrame combined with cfg.g_error_resilient = 1/2/3.
//   - EncodeNoUpdate{Last|Golden|AltRef} periodic patterns (every 2nd,
//     every 3rd, every 4th inter frame) — the per-frame upd-mask paths
//     that real-time WebRTC stacks use to make frame loss tolerable
//     by anchoring layered reference state.
//   - EncodeNoReference{Last|Golden|AltRef} periodic patterns — the
//     "drop a stale reference from inter prediction" pattern that
//     scalable layering uses when an upstream packet is known lost.
//
// All cases drive the libvpx side through the
// `vpxenc-frameflags` companion (see vpxenc_frameflags.c), which is the
// only path that exposes per-frame frame_flags on the libvpx encode
// call. Strict byte parity must hold for every frame; any divergence
// is asserted unless the case is explicitly pinned with `limit:`.
func TestVP8OracleEncoderStreamByteParityLossRecovery(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	driver := vp8test.VpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 8
	)

	type fixture struct {
		name   string
		w, h   int
		source func(w, h, i int) Image
	}
	panning16 := fixture{name: "panning-16x16", w: 16, h: 16, source: encoderValidationPanningFrame}
	panning32 := fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}

	// patternEveryN returns a flag schedule of length n+1 (index 0 is
	// the initial keyframe and receives no flag) where every k-th
	// inter frame (1, 1+k, 1+2k, ...) carries `f`.
	patternEveryN := func(total int, k int, f EncodeFlags) []EncodeFlags {
		out := make([]EncodeFlags, total)
		for i := 1; i < total; i++ {
			if (i-1)%k == 0 {
				out[i] = f
			}
		}
		return out
	}

	cases := []struct {
		name       string
		deadline   Deadline
		cpuUsed    int
		fx         fixture
		limit      int
		flags      []EncodeFlags
		rcMode     RateControlMode
		rcModeSet  bool
		cqLevel    int
		tokenParts int
		// errorResilient drives cfg.g_error_resilient and the
		// govpx-side ErrorResilient bool. The value is the libvpx
		// bitmask (0..3): bit 0 = VPX_ERROR_RESILIENT_DEFAULT,
		// bit 1 = VPX_ERROR_RESILIENT_PARTITIONS.
		errorResilientBits int
		extraArgs          []string
	}{
		// ----- 1. Mid-stream force-keyframe at multiple indices -----
		//
		// These mimic the WebRTC "received NACK -> emit recovery I"
		// pattern. The kf-min-dist/kf-max-dist arithmetic stays at the
		// 999 default so the only keyframes are frame 0 (implicit) and
		// the explicit forced-KF index. Frame indices 1, 2, 4, 5, 6
		// each get one row to cover both "immediate" recovery
		// (frame 1) and late recovery near the budget boundary.
		{name: "force-kf-frame1-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, EncodeForceKeyFrame}},
		{name: "force-kf-frame2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame4-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame5-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame6-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, 0, 0, 0, EncodeForceKeyFrame}},
		// Same recovery schedule at 32x32 / 48x48 / cpu-3 so the
		// keyframe writer + post-recovery inter cascade is pinned
		// across the cpu_used and frame-size sweep.
		{name: "force-kf-frame2-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame4-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame4-panning48-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, flags: []EncodeFlags{0, 0, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame5-panning64-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, flags: []EncodeFlags{0, 0, 0, 0, 0, EncodeForceKeyFrame}},
		// Double recovery: forced-KF at both frame 2 AND frame 5
		// (simulates two distinct loss events).
		{name: "force-kf-frame2-and-5-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, EncodeForceKeyFrame, 0, 0, EncodeForceKeyFrame}},
		{name: "force-kf-frame2-and-5-panning32-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, 0, EncodeForceKeyFrame, 0, 0, EncodeForceKeyFrame}},

		// ----- 2. Force-KF combined with ErrorResilient bits -----
		//
		// Pin all three error_resilient bitmask values against the
		// mid-stream recovery flag. With error_resilient=1 the
		// encoder skips the entropy adaptation update for the
		// keyframe; bit 2 (=PARTITIONS) makes each per-token-context
		// independent. The recovery I-frame is the prime use case
		// for these bits — once the receiver decodes the KF the
		// stream needs to keep going without referring to any state
		// from before the loss.
		{name: "force-kf-frame3-er1-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, errorResilientBits: 1, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=1"}},
		{name: "force-kf-frame3-er2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, errorResilientBits: 2, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=2"}},
		{name: "force-kf-frame3-er3-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, errorResilientBits: 3, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=3"}},
		{name: "force-kf-frame3-er1-panning32-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, errorResilientBits: 1, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=1"}},
		{name: "force-kf-frame3-er3-panning32-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, errorResilientBits: 3, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=3"}},
		// Force-KF mid-stream + error-resilient + token-parts so the
		// per-partition writer also encounters the entropy reset.
		{name: "force-kf-frame3-er1-panning32-4partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, errorResilientBits: 1, tokenParts: 2, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "force-kf-frame3-er3-panning32-8partitions", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, errorResilientBits: 3, tokenParts: 3, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame}, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},

		// ----- 3. EncodeNoUpdate{Last,Golden,AltRef} periodic patterns -----
		//
		// The existing frame-flags matrix covers "every inter frame"
		// patterns. This batch adds explicit periodicities (every
		// 2nd / 3rd / 4th inter frame) which surface the bookkeeping
		// path that toggles the upd-mask between sequential frames
		// — a regression in the per-frame mask transition is invisible
		// when the same bit is set on every frame.
		{name: "no-upd-last-every2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 2, EncodeNoUpdateLast)},
		{name: "no-upd-last-every3-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 3, EncodeNoUpdateLast)},
		{name: "no-upd-last-every4-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: patternEveryN(frames, 4, EncodeNoUpdateLast)},
		{name: "no-upd-gf-every2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 2, EncodeNoUpdateGolden)},
		{name: "no-upd-gf-every3-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: patternEveryN(frames, 3, EncodeNoUpdateGolden)},
		{name: "no-upd-arf-every2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 2, EncodeNoUpdateAltRef)},
		{name: "no-upd-arf-every4-panning32-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: patternEveryN(frames, 4, EncodeNoUpdateAltRef)},
		// Combined no-update bits in periodic schedules — both
		// patterns are common in conf-call codecs that freeze a
		// long-term reference between scenes.
		{name: "no-upd-gf-arf-every2-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: patternEveryN(frames, 2, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)},
		{name: "no-upd-last-gf-every3-panning48-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, flags: patternEveryN(frames, 3, EncodeNoUpdateLast|EncodeNoUpdateGolden)},

		// ----- 4. EncodeNoReference{Last,Golden,AltRef} periodic patterns -----
		//
		// Same periodicities applied to the no-reference axis. The
		// picker must drop the masked-out reference from the inter
		// candidate set on those frames only; on the unmasked frames
		// the full candidate set is back. Any drift between the
		// govpx and libvpx picker's "stale-reference suppression"
		// state machines will surface as a per-frame mismatch.
		// EncodeNoReferenceLast applied at frame 1 (the first inter
		// frame) right after a keyframe. The keyframe seeded GOLDEN
		// and ALTREF with copies of the LAST reconstruction, so when
		// LAST is masked here the picker still has those alias slots
		// available — libvpx's onyx_if.c walks the cm->ref_frame_flags
		// mask, finds GOLDEN/ALTREF valid, and emits a normal inter
		// frame whose MBs fall back to zero-MV against the KF-aliased
		// GOLDEN. interReferenceAvailability mirrors that by only
		// suppressing the aliased slot when its primary is itself
		// reachable; with LAST masked, GOLDEN becomes the surviving
		// picker candidate. Strict byte parity holds across the run.
		{name: "no-ref-last-every3-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 3, EncodeNoReferenceLast)},
		{name: "no-ref-gf-every2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 2, EncodeNoReferenceGolden)},
		{name: "no-ref-gf-every3-panning32", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, flags: patternEveryN(frames, 3, EncodeNoReferenceGolden)},
		{name: "no-ref-arf-every2-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: patternEveryN(frames, 2, EncodeNoReferenceAltRef)},
		{name: "no-ref-arf-every4-panning32-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: patternEveryN(frames, 4, EncodeNoReferenceAltRef)},
		{name: "no-ref-gf-arf-every2-panning48", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning48, flags: patternEveryN(frames, 2, EncodeNoReferenceGolden|EncodeNoReferenceAltRef)},

		// Force-KF + no-update-LAST/GF/ARF — anchor pattern: the
		// recovery KF refreshes everything, but on the subsequent
		// frames the upd-mask is held off so the layered references
		// the receiver already had remain valid as long as those
		// frames continue to be decoded.
		{name: "force-kf-frame3-then-no-upd-gf-arf-panning16", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning16, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame, EncodeNoUpdateGolden | EncodeNoUpdateAltRef, EncodeNoUpdateGolden | EncodeNoUpdateAltRef, EncodeNoUpdateGolden | EncodeNoUpdateAltRef, EncodeNoUpdateGolden | EncodeNoUpdateAltRef}},
		{name: "force-kf-frame3-then-no-ref-gf-arf-panning32-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, flags: []EncodeFlags{0, 0, 0, EncodeForceKeyFrame, EncodeNoReferenceGolden | EncodeNoReferenceAltRef, EncodeNoReferenceGolden | EncodeNoReferenceAltRef, EncodeNoReferenceGolden | EncodeNoReferenceAltRef, EncodeNoReferenceGolden | EncodeNoReferenceAltRef}},
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
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      fps,
				RateControlMode:          rcMode,
				TargetBitrateKbps:        targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				CQLevel:                  tc.cqLevel,
				KeyFrameInterval:         999,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   TunePSNR,
				TokenPartitions:          tc.tokenParts,
				ErrorResilient:           tc.errorResilientBits&1 != 0,
				ErrorResilientPartitions: tc.errorResilientBits&2 != 0,
			}

			govpxFrames := encodeFramesWithGovpxFrameFlags(t, opts, sources, tc.flags)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, tc.name, opts, targetKbps, sources, tc.flags, tc.extraArgs)

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
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
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

// TestVP8OracleEncoderStreamByteParityLossStaticOpts widens the
// strict byte-parity gate for static error-resilient configurations
// that the existing matrices do not cover, even though they sit at
// the heart of WebRTC's packet-loss handling:
//
//   - ErrorResilient + TokenPartitions ∈ {1,2,3} at the 96x96 fixtures
//     (panning + splitmv), which the existing matrices only pin at
//     16x16, 32x32, 48x48, and 64x64.
//   - ErrorResilient + denoiser noise-sensitivity ∈ {1,2,4,5,6} at
//     48x48. The existing matrices only pin noise-sensitivity=3 with
//     error-resilient; the per-level temporal denoiser interaction with
//     the entropy-suppression reset has no coverage at noise=1/2/4/5/6.
//   - ErrorResilient + sharpness ∈ {1..6} at panning-64x64 / panning-32x32.
//     The existing matrices only pin sharpness=7 with error-resilient,
//     leaving the in-band sharpness values untested with the
//     resilience reset path.
//   - ErrorResilient + drop-frame + buffer-override combined — the
//     drop-frame gate consults the buffer-fullness arithmetic that the
//     resilience header clears, so the three knobs interact and need
//     a joint coverage row.
//
// Each case is the unified static-knob path: the vpxenc-oracle driver
// receives the corresponding `--error-resilient=N` arg and the same
// per-fixture knobs. No per-frame frame_flags are needed; the driver
// is the standard vpxenc-oracle binary.
func TestVP8OracleEncoderStreamByteParityLossStaticOpts(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder stream byte-parity gate")
	}
	vpxencOracle := vp8test.VpxencOracle(t)

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
	panning48 := fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}
	panning64 := fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}
	panning96 := fixture{name: "panning-96x96", w: 96, h: 96, source: encoderValidationPanningFrame}
	splitmv96 := fixture{name: "splitmv-96x96", w: 96, h: 96, source: encoderValidationSplitMVQuadrantFrame}

	cases := []struct {
		name                     string
		deadline                 Deadline
		cpuUsed                  int
		fx                       fixture
		limit                    int
		errorResilient           bool
		errorResilientPartitions bool
		tokenPartitions          int
		noiseSensitivity         int
		sharpness                int
		dropFrameAllowed         bool
		dropFrameWaterMark       int
		bufferSizeMs             int
		bufferInitialSizeMs      int
		bufferOptimalSizeMs      int
		extraArgs                []string
	}{
		// ----- 5. ErrorResilient + TokenPartitions at 96x96 -----
		//
		// panning-96x96 with all three er bitmask values × token-parts
		// {1,2,3}. The 96x96 fixture's MB count is 36 (6x6), large
		// enough to populate every token partition with a non-trivial
		// number of MBs (a single token partition holds 6 MBs at
		// token-parts=3, which is enough to exercise the partition
		// writer end-to-end).
		{name: "panning96-er1-2partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilient: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=1", "--token-parts=1"}},
		{name: "panning96-er1-4partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "panning96-er1-8partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilient: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=1", "--token-parts=3"}},
		{name: "panning96-er2-2partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=2", "--token-parts=1"}},
		{name: "panning96-er2-4partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilientPartitions: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=2", "--token-parts=2"}},
		{name: "panning96-er2-8partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=2", "--token-parts=3"}},
		{name: "panning96-er3-2partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=3", "--token-parts=1"}},
		{name: "panning96-er3-4partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
		{name: "panning96-er3-8partitions-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning96, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
		// panning-96x96 + er1 across cpu_used presets to round out the
		// CPU axis on the new fixture.
		{name: "panning96-er1-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning96, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "panning96-er1-cpu-8", deadline: DeadlineRealtime, cpuUsed: -8, fx: panning96, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		{name: "panning96-er1-cpu4", deadline: DeadlineRealtime, cpuUsed: 4, fx: panning96, errorResilient: true, extraArgs: []string{"--error-resilient=1"}},
		// splitmv-96x96 + error-resilient + token-parts. The SPLITMV
		// fixture stresses the sub-MV picker; combining with the
		// partition writer here probes both surfaces.
		{name: "splitmv96-er1-2partitions-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv96, errorResilient: true, tokenPartitions: 1, extraArgs: []string{"--error-resilient=1", "--token-parts=1"}},
		{name: "splitmv96-er1-4partitions-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv96, errorResilient: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=1", "--token-parts=2"}},
		{name: "splitmv96-er1-8partitions-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv96, errorResilient: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=1", "--token-parts=3"}},
		{name: "splitmv96-er2-4partitions-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv96, errorResilientPartitions: true, tokenPartitions: 2, extraArgs: []string{"--error-resilient=2", "--token-parts=2"}},
		{name: "splitmv96-er3-8partitions-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv96, errorResilient: true, errorResilientPartitions: true, tokenPartitions: 3, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},

		// ----- 6. ErrorResilient + denoiser noise-sensitivity {1,2,4,5,6} -----
		//
		// 48x48 panning is the existing denoiser+error-resilient anchor
		// fixture (noise=3 byte-matches in the extended matrix). Each
		// row here adds the missing per-level coverage.
		{name: "panning48-er1-noise1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilient: true, noiseSensitivity: 1, extraArgs: []string{"--error-resilient=1", "--noise-sensitivity=1"}},
		{name: "panning48-er1-noise2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilient: true, noiseSensitivity: 2, extraArgs: []string{"--error-resilient=1", "--noise-sensitivity=2"}},
		{name: "panning48-er1-noise4", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilient: true, noiseSensitivity: 4, extraArgs: []string{"--error-resilient=1", "--noise-sensitivity=4"}},
		{name: "panning48-er1-noise5", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilient: true, noiseSensitivity: 5, extraArgs: []string{"--error-resilient=1", "--noise-sensitivity=5"}},
		{name: "panning48-er1-noise6", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilient: true, noiseSensitivity: 6, extraArgs: []string{"--error-resilient=1", "--noise-sensitivity=6"}},
		// er2 + noise — bit 2 (PARTITIONS) interacts independently of
		// the per-MB denoiser path, so the er2 axis needs the same
		// per-level sweep.
		{name: "panning48-er2-noise1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilientPartitions: true, noiseSensitivity: 1, extraArgs: []string{"--error-resilient=2", "--noise-sensitivity=1"}},
		{name: "panning48-er2-noise6", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning48, errorResilientPartitions: true, noiseSensitivity: 6, extraArgs: []string{"--error-resilient=2", "--noise-sensitivity=6"}},

		// ----- 7. ErrorResilient + sharpness {1..6} -----
		//
		// 64x64 panning is the natural anchor (the existing
		// sharpness=7 + error-resilient3 row already byte-matches at
		// 64x64). Fill in sharpness 1..6 across er1 / er3.
		{name: "panning64-er1-sharpness1", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, sharpness: 1, extraArgs: []string{"--error-resilient=1", "--sharpness=1"}},
		{name: "panning64-er1-sharpness2", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, sharpness: 2, extraArgs: []string{"--error-resilient=1", "--sharpness=2"}},
		{name: "panning64-er1-sharpness3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, sharpness: 3, extraArgs: []string{"--error-resilient=1", "--sharpness=3"}},
		{name: "panning64-er1-sharpness4", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, sharpness: 4, extraArgs: []string{"--error-resilient=1", "--sharpness=4"}},
		{name: "panning64-er1-sharpness5", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, sharpness: 5, extraArgs: []string{"--error-resilient=1", "--sharpness=5"}},
		{name: "panning64-er1-sharpness6", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, sharpness: 6, extraArgs: []string{"--error-resilient=1", "--sharpness=6"}},
		// Mid sharpness × er3 to cross the partitions bit with the
		// per-frame loopfilter strength setting.
		{name: "panning32-er3-sharpness4", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, errorResilient: true, errorResilientPartitions: true, sharpness: 4, extraArgs: []string{"--error-resilient=3", "--sharpness=4"}},
		{name: "panning64-er3-sharpness5", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, errorResilientPartitions: true, sharpness: 5, extraArgs: []string{"--error-resilient=3", "--sharpness=5"}},

		// ----- 8. ErrorResilient + drop-frame + buffer overrides -----
		//
		// The drop-frame gate consults rc_buf_*; the resilience reset
		// is layered on top. These rows hit all three knobs together
		// at 32x32 and 64x64 to pin the joint path. The panning fixture
		// at the test target bitrate is well above the drop threshold,
		// so no frames should actually drop — the parity check is on
		// the writer that walks through the drop-eligible state.
		{name: "panning32-er1-dropframe60-buffer-defaults", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning32, errorResilient: true, dropFrameAllowed: true, dropFrameWaterMark: 60, extraArgs: []string{"--error-resilient=1", "--drop-frame=60"}},
		{name: "panning64-er1-dropframe60-buffer-1000-500-600", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, dropFrameAllowed: true, dropFrameWaterMark: 60, bufferSizeMs: 1000, bufferInitialSizeMs: 500, bufferOptimalSizeMs: 600, extraArgs: []string{"--error-resilient=1", "--drop-frame=60", "--buf-sz=1000", "--buf-initial-sz=500", "--buf-optimal-sz=600"}},
		{name: "panning64-er3-dropframe60-buffer-2000-1000-1500", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning64, errorResilient: true, errorResilientPartitions: true, dropFrameAllowed: true, dropFrameWaterMark: 60, bufferSizeMs: 2000, bufferInitialSizeMs: 1000, bufferOptimalSizeMs: 1500, extraArgs: []string{"--error-resilient=3", "--drop-frame=60", "--buf-sz=2000", "--buf-initial-sz=1000", "--buf-optimal-sz=1500"}},
		{name: "panning32-er2-dropframe30-buffer-1500-750-1000", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning32, errorResilientPartitions: true, dropFrameAllowed: true, dropFrameWaterMark: 30, bufferSizeMs: 1500, bufferInitialSizeMs: 750, bufferOptimalSizeMs: 1000, extraArgs: []string{"--error-resilient=2", "--drop-frame=30", "--buf-sz=1500", "--buf-initial-sz=750", "--buf-optimal-sz=1000"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			opts := EncoderOptions{
				Width:                    tc.fx.w,
				Height:                   tc.fx.h,
				FPS:                      fps,
				RateControlMode:          RateControlCBR,
				TargetBitrateKbps:        targetKbps,
				MinQuantizer:             4,
				MaxQuantizer:             56,
				KeyFrameInterval:         999,
				Deadline:                 tc.deadline,
				CpuUsed:                  tc.cpuUsed,
				Tuning:                   TunePSNR,
				ErrorResilient:           tc.errorResilient,
				ErrorResilientPartitions: tc.errorResilientPartitions,
				Sharpness:                tc.sharpness,
				NoiseSensitivity:         tc.noiseSensitivity,
				TokenPartitions:          tc.tokenPartitions,
				DropFrameAllowed:         tc.dropFrameAllowed,
				DropFrameWaterMark:       tc.dropFrameWaterMark,
				BufferSizeMs:             tc.bufferSizeMs,
				BufferInitialSizeMs:      tc.bufferInitialSizeMs,
				BufferOptimalSizeMs:      tc.bufferOptimalSizeMs,
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
				firstDiff := testutil.FirstByteDiff(govpxFrames[i], libvpxFrames[i])
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
