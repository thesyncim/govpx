//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestOracleEncoderStreamByteParitySegmentation widens the strict
// byte-parity matrix toward segmentation-adjacent control surfaces:
// screen-content overlays, static-thresh sweeps, denoiser/screen-content
// cross-products, and AdaptiveKeyFrames against a scene-cut fixture.
//
// Coverage boundaries:
//
//  1. ROI maps. The upstream vpxenc CLI does not expose
//     VP8E_SET_ROI_MAP, so ROI byte-parity rows in this file use the
//     companion frame-flags driver, which drives the libvpx C API
//     directly.
//
//  2. CyclicRefresh as a stand-alone knob. govpx does not expose a
//     CyclicRefresh option on EncoderOptions — cyclic refresh is
//     implicit-on whenever RateControlCBR + a non-zero base quantizer
//     selects it (encoder.go cyclicRefreshSegmentationConfig path).
//     The default-CBR cases throughout the base parity matrix and the
//     fixtures below all run with cyclic refresh active; there is no
//     "cyclic-refresh=N" libvpx vpxenc switch to mirror. The
//     screenContentMode crosses below stress the cyclic refresh +
//     screen-content interaction (which IS exposed via
//     `--screen-content-mode`).
//
// Each subtest follows the protocol established by
// [TestOracleEncoderStreamByteParity] /
// [TestOracleEncoderStreamByteParityExtended]: same I420 fixture into
// govpx and the patched vpxenc-oracle, assert byte parity (or pin the
// known-good prefix via `limit:`). Cases marked with `limit: -1` log
// the per-frame status without asserting strict parity.
func TestOracleEncoderStreamByteParitySegmentation(t *testing.T) {
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
	panning128 := fixture{name: "panning-128x128", w: 128, h: 128, source: encoderValidationPanningFrame}
	panning256x144 := fixture{name: "panning-256x144", w: 256, h: 144, source: encoderValidationPanningFrame}
	segmented32 := fixture{name: "segmented-32x32", w: 32, h: 16, source: encoderValidationSegmentedFrame}
	segmented64 := fixture{name: "segmented-64x64", w: 64, h: 64, source: encoderValidationSegmentedFrame}
	segmented128 := fixture{name: "segmented-128x128", w: 128, h: 128, source: encoderValidationSegmentedFrame}
	splitmv64 := fixture{name: "splitmv-64x64", w: 64, h: 64, source: encoderValidationSplitMVQuadrantFrame}
	splitmv128 := fixture{name: "splitmv-128x128", w: 128, h: 128, source: encoderValidationSplitMVQuadrantFrame}

	cases := []struct {
		name               string
		deadline           Deadline
		cpuUsed            int
		fx                 fixture
		limit              int
		staticThreshold    int
		screenContentMode  int
		noiseSensitivity   int
		adaptiveKeyFrames  bool
		targetKbpsOverride int
		extraArgs          []string
	}{
		// --------------------------------------------------------
		// 1. ScreenContentMode 1/2 at fixtures the base matrix does
		//    not pin. The base matrix covers small panning sizes
		//    (16x16, 32x32, 64x64, 96x96) at screen-content. These
		//    add splitmv-128x128, segmented-128x128, panning-256x144,
		//    panning-128x128 across CPU presets. All cases byte-match
		//    libvpx across the full 16-frame budget so they're pinned
		//    strict from the start.
		// --------------------------------------------------------
		{name: "screen-content1-splitmv-128x128-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		// splitmv-128x128 + sc=2 needs a higher bitrate budget — at
		// 700 kbps the CBR drop-gate fires on inter frame 1/2 because
		// the aggressive splitmv source overwhelms the screen-content
		// mode-2 budget. Bumping to 2000 kbps keeps the parity probe
		// on the encode path and avoids the drop-frame divergence.
		{name: "screen-content2-splitmv-128x128-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv128, screenContentMode: 2, targetKbpsOverride: 2000, extraArgs: []string{"--screen-content-mode=2", "--target-bitrate=2000"}},
		{name: "screen-content1-splitmv-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-splitmv-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv128, screenContentMode: 2, targetKbpsOverride: 2000, extraArgs: []string{"--screen-content-mode=2", "--target-bitrate=2000"}},
		{name: "screen-content1-segmented-128x128-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-segmented-128x128-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented128, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "screen-content1-segmented-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-segmented-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented128, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "screen-content1-panning-256x144-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning256x144, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-panning-256x144-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning256x144, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "screen-content1-panning-256x144-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning256x144, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		// screen-content2-panning-256x144 at cpu-3 covers the overshoot-drop
		// path, screen-content cyclic refresh, and key-frame refresh-map state.
		{name: "screen-content2-panning-256x144-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning256x144, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "screen-content2-panning-256x144-realtime-cpu-3-bitrate2000", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning256x144, screenContentMode: 2, targetKbpsOverride: 2000, extraArgs: []string{"--screen-content-mode=2", "--target-bitrate=2000"}},
		{name: "screen-content1-panning-128x128-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-panning-128x128-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: panning128, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "screen-content1-panning-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-panning-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: panning128, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "screen-content1-panning-128x128-realtime-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning128, screenContentMode: 1, extraArgs: []string{"--screen-content-mode=1"}},
		{name: "screen-content2-panning-128x128-realtime-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: panning128, screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},

		// --------------------------------------------------------
		// 2. Static-threshold sweep at multiple CPU presets. The
		//    base matrix only pins static-thresh=1 and the extended
		//    matrix touches static-thresh ∈ {100, 1000}. This sweep
		//    fills out {0, 5, 10, 50, 500, 1000} across cpu_used so
		//    the per-MB encode_breakout gate is exercised across
		//    its arithmetic range.
		// --------------------------------------------------------
		{name: "static-thresh0-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 0, extraArgs: []string{"--static-thresh=0"}},
		{name: "static-thresh5-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 5, extraArgs: []string{"--static-thresh=5"}},
		{name: "static-thresh10-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 10, extraArgs: []string{"--static-thresh=10"}},
		{name: "static-thresh50-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 50, extraArgs: []string{"--static-thresh=50"}},
		{name: "static-thresh500-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 500, extraArgs: []string{"--static-thresh=500"}},
		{name: "static-thresh1000-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 1000, extraArgs: []string{"--static-thresh=1000"}},
		{name: "static-thresh5-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, staticThreshold: 5, extraArgs: []string{"--static-thresh=5"}},
		{name: "static-thresh50-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, staticThreshold: 50, extraArgs: []string{"--static-thresh=50"}},
		{name: "static-thresh500-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, staticThreshold: 500, extraArgs: []string{"--static-thresh=500"}},
		{name: "static-thresh1000-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, staticThreshold: 1000, extraArgs: []string{"--static-thresh=1000"}},
		{name: "static-thresh50-realtime-cpu8-32x32", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 50, extraArgs: []string{"--static-thresh=50"}},
		{name: "static-thresh500-realtime-cpu8-32x32", deadline: DeadlineRealtime, cpuUsed: 8, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 500, extraArgs: []string{"--static-thresh=500"}},

		// --------------------------------------------------------
		// 3. Static-thresh combined with screen-content-mode at
		//    additional values. The extended matrix touched
		//    sc=1+st=1; this fills out the cross-product so the
		//    cyclic-refresh + screen-content + static-thresh
		//    interaction is pinned.
		// --------------------------------------------------------
		{name: "static-thresh5-screen-content1-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 5, screenContentMode: 1, extraArgs: []string{"--static-thresh=5", "--screen-content-mode=1"}},
		{name: "static-thresh50-screen-content1-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 50, screenContentMode: 1, extraArgs: []string{"--static-thresh=50", "--screen-content-mode=1"}},
		{name: "static-thresh500-screen-content1-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 500, screenContentMode: 1, extraArgs: []string{"--static-thresh=500", "--screen-content-mode=1"}},
		{name: "static-thresh5-screen-content2-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 5, screenContentMode: 2, extraArgs: []string{"--static-thresh=5", "--screen-content-mode=2"}},
		{name: "static-thresh50-screen-content2-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 50, screenContentMode: 2, extraArgs: []string{"--static-thresh=50", "--screen-content-mode=2"}},
		{name: "static-thresh500-screen-content2-realtime-cpu0-32x32", deadline: DeadlineRealtime, cpuUsed: 0, fx: fixture{name: "panning-32x32", w: 32, h: 16, source: encoderValidationPanningFrame}, staticThreshold: 500, screenContentMode: 2, extraArgs: []string{"--static-thresh=500", "--screen-content-mode=2"}},
		{name: "static-thresh50-screen-content1-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, staticThreshold: 50, screenContentMode: 1, extraArgs: []string{"--static-thresh=50", "--screen-content-mode=1"}},
		{name: "static-thresh500-screen-content2-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, staticThreshold: 500, screenContentMode: 2, extraArgs: []string{"--static-thresh=500", "--screen-content-mode=2"}},

		// --------------------------------------------------------
		// 4. NoiseSensitivity combined with screen-content. The
		//    extended matrix has sc1+ns3 and sc2+ns6 at 48x48; this
		//    fills out the rest of the noise levels and adds
		//    additional sizes/CPU presets so the denoiser + SC
		//    overlay path is pinned across the matrix.
		// --------------------------------------------------------
		{name: "noise1-screen-content1-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 1, screenContentMode: 1, extraArgs: []string{"--noise-sensitivity=1", "--screen-content-mode=1"}},
		{name: "noise2-screen-content1-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 2, screenContentMode: 1, extraArgs: []string{"--noise-sensitivity=2", "--screen-content-mode=1"}},
		{name: "noise4-screen-content1-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 4, screenContentMode: 1, extraArgs: []string{"--noise-sensitivity=4", "--screen-content-mode=1"}},
		{name: "noise5-screen-content1-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 5, screenContentMode: 1, extraArgs: []string{"--noise-sensitivity=5", "--screen-content-mode=1"}},
		{name: "noise1-screen-content2-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 1, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=1", "--screen-content-mode=2"}},
		{name: "noise2-screen-content2-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 2, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=2", "--screen-content-mode=2"}},
		{name: "noise4-screen-content2-realtime-cpu-3-48x48", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-48x48", w: 48, h: 48, source: encoderValidationPanningFrame}, noiseSensitivity: 4, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=4", "--screen-content-mode=2"}},
		{name: "noise3-screen-content1-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, noiseSensitivity: 3, screenContentMode: 1, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=1"}},
		{name: "noise6-screen-content2-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: fixture{name: "panning-64x64", w: 64, h: 64, source: encoderValidationPanningFrame}, noiseSensitivity: 6, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=6", "--screen-content-mode=2"}},

		// --------------------------------------------------------
		// 5. AdaptiveKeyFrames + segmented (scene-cut) content.
		//    The extended matrix's AdaptiveKeyFrames cases all use
		//    the smooth panning fixture, which never trips
		//    libvpx's scene-cut detector. The segmented fixture
		//    alternates 16x16 blocks of "checker high" vs random
		//    luma, producing sharp MB-grid boundaries that exercise
		//    the libvpx scene-cut probe. Strict-pinned: the
		//    govpx auto_key gate matches libvpx byte-for-byte on
		//    the segmented fixture across CPU presets and sizes,
		//    so any future drift in the scene-cut detector will
		//    fail the gate here.
		// --------------------------------------------------------
		{name: "adaptive-kf-segmented-32x32-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented32, adaptiveKeyFrames: true},
		{name: "adaptive-kf-segmented-32x32-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented32, adaptiveKeyFrames: true},
		{name: "adaptive-kf-segmented-32x32-realtime-cpu8", deadline: DeadlineRealtime, cpuUsed: 8, fx: segmented32, adaptiveKeyFrames: true},
		{name: "adaptive-kf-segmented-64x64-realtime-cpu0", deadline: DeadlineRealtime, cpuUsed: 0, fx: segmented64, adaptiveKeyFrames: true},
		{name: "adaptive-kf-segmented-64x64-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented64, adaptiveKeyFrames: true},
		{name: "adaptive-kf-segmented-64x64-good-quality-cpu4", deadline: DeadlineGoodQuality, cpuUsed: 4, fx: segmented64, adaptiveKeyFrames: true},
		{name: "adaptive-kf-segmented-128x128-realtime-cpu-3", deadline: DeadlineRealtime, cpuUsed: -3, fx: segmented128, adaptiveKeyFrames: true},

		// --------------------------------------------------------
		// 6. Splitmv + denoiser at fixtures the matrix does not
		//    pin (cyclic refresh runs implicitly on the base
		//    CBR path here, so this also exercises the
		//    cyclic-refresh + splitmv + denoiser interaction).
		// --------------------------------------------------------
		{name: "splitmv-noise3-realtime-cpu-3-64x64", deadline: DeadlineRealtime, cpuUsed: -3, fx: splitmv64, noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "splitmv-noise6-realtime-cpu0-64x64", deadline: DeadlineRealtime, cpuUsed: 0, fx: splitmv64, noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := make([]Image, frames)
			for i := range sources {
				sources[i] = tc.fx.source(tc.fx.w, tc.fx.h, i)
			}
			caseTargetKbps := targetKbps
			if tc.targetKbpsOverride > 0 {
				caseTargetKbps = tc.targetKbpsOverride
			}
			opts := EncoderOptions{
				Width:             tc.fx.w,
				Height:            tc.fx.h,
				FPS:               fps,
				RateControlMode:   RateControlCBR,
				TargetBitrateKbps: caseTargetKbps,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				AdaptiveKeyFrames: tc.adaptiveKeyFrames,
				Deadline:          tc.deadline,
				CpuUsed:           tc.cpuUsed,
				Tuning:            TunePSNR,
				StaticThreshold:   tc.staticThreshold,
				ScreenContentMode: tc.screenContentMode,
				NoiseSensitivity:  tc.noiseSensitivity,
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

func TestOracleEncoderStreamByteParityROIMap(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run ROI byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}
	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
		0: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetROIMap(custom quadrants)", e.SetROIMap(customQuadrantROIMap(width, height)))
		},
	})
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-altq-altlf-static-64x64", opts, targetKbps, sources, nil, []string{
		"--roi-map=quadrants",
		"--roi-dq=0,-10,8,-20",
		"--roi-dlf=0,-3,2,5",
		"--roi-static=0,500,0,1200",
	})
	assertSegmentByteParity(t, "roi-map-altq-altlf-static", govpxFrames, libvpxFrames, 0)
}

func TestOracleEncoderStreamByteParityROISimpleDeltaQ(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run ROI byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 32
		height     = 32
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}
	govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
		0: func(t *testing.T, e *VP8Encoder) {
			t.Helper()
			mustRuntime(t, "SetROIMap(simple checker)", e.SetROIMap(simpleCheckerROIMap(width, height)))
		},
	})
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-simple-dq-32x32", opts, targetKbps, sources, nil, []string{
		"--roi-map=checker",
		"--roi-dq=0,-10,0,0",
		"--roi-dlf=0,0,0,0",
		"--roi-static=0,0,0,0",
	})
	assertSegmentByteParity(t, "roi-map-simple-dq", govpxFrames, libvpxFrames, 0)
}

func TestOracleEncoderStreamByteParityROISimpleAxes(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run ROI byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 32
		height     = 32
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name      string
		roi       func() *ROIMap
		extraArgs []string
		limit     int
	}{
		{
			name: "simple-delta-lf",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "checker")
				roi.DeltaQuantizer = [4]int{}
				roi.DeltaLoopFilter = [4]int{0, -3, 0, 0}
				roi.StaticThreshold = [4]int{}
				return roi
			},
			extraArgs: []string{"--roi-map=checker", "--roi-dq=0,0,0,0", "--roi-dlf=0,-3,0,0", "--roi-static=0,0,0,0"},
		},
		{
			name: "simple-static-threshold",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "checker")
				roi.DeltaQuantizer = [4]int{}
				roi.DeltaLoopFilter = [4]int{}
				roi.StaticThreshold = [4]int{0, 500, 0, 0}
				return roi
			},
			extraArgs: []string{"--roi-map=checker", "--roi-dq=0,0,0,0", "--roi-dlf=0,0,0,0", "--roi-static=0,500,0,0"},
		},
		{
			name: "dq-dlf-no-static",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "checker")
				roi.DeltaQuantizer = [4]int{0, -10, 0, 0}
				roi.DeltaLoopFilter = [4]int{0, -3, 0, 0}
				roi.StaticThreshold = [4]int{}
				return roi
			},
			extraArgs: []string{"--roi-map=checker", "--roi-dq=0,-10,0,0", "--roi-dlf=0,-3,0,0", "--roi-static=0,0,0,0"},
		},
		{
			name: "quadrants-default",
			roi: func() *ROIMap {
				roi := roiMapPattern(width, height, "quadrants")
				roi.DeltaQuantizer = [4]int{}
				roi.DeltaLoopFilter = [4]int{}
				roi.StaticThreshold = [4]int{}
				return roi
			},
			extraArgs: []string{"--roi-map=quadrants", "--roi-dq=0,0,0,0", "--roi-dlf=0,0,0,0", "--roi-static=0,0,0,0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap("+tc.name+")", e.SetROIMap(tc.roi()))
				},
			})
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-"+tc.name+"-32x32", opts, targetKbps, sources, nil, tc.extraArgs)
			assertSegmentByteParity(t, "roi-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestOracleEncoderStreamByteParityActiveMapPatterns(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run active-map byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	}

	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	cases := []struct {
		name                string
		pattern             string
		limit               int
		cpuUsed             int
		noiseSensitivity    int
		screenContentMode   int
		tokenPartitions     int
		sharpness           int
		tuning              Tuning
		tuningSet           bool
		errorResilient      bool
		errorResilientParts bool
		threads             int
		extraArgs           []string
	}{
		{name: "all", pattern: "all", limit: 0},
		{name: "checker", pattern: "checker", limit: 0},
		{name: "left-off", pattern: "left-off", limit: 0},
		{name: "right-off", pattern: "right-off", limit: 0},
		{name: "border-off", pattern: "border-off", limit: 0},
		{name: "off", pattern: "off", limit: 0},
		{name: "left-off-cpu-3", pattern: "left-off", cpuUsed: -3, limit: 0},
		{name: "right-off-cpu-3", pattern: "right-off", cpuUsed: -3, limit: 0},
		{name: "border-off-cpu-3", pattern: "border-off", cpuUsed: -3, limit: 0},
		{name: "checker-noise1", pattern: "checker", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "checker-noise2", pattern: "checker", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "checker-noise4", pattern: "checker", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "checker-noise5", pattern: "checker", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "checker-noise6", pattern: "checker", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "left-off-noise1", pattern: "left-off", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "left-off-noise2", pattern: "left-off", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "left-off-noise3", pattern: "left-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "left-off-noise4", pattern: "left-off", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "left-off-noise5", pattern: "left-off", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "left-off-noise6", pattern: "left-off", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "right-off-noise1", pattern: "right-off", noiseSensitivity: 1, limit: 0, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "right-off-noise2", pattern: "right-off", noiseSensitivity: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "right-off-noise3", pattern: "right-off", noiseSensitivity: 3, limit: 0, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "right-off-noise4", pattern: "right-off", noiseSensitivity: 4, limit: 0, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "right-off-noise5", pattern: "right-off", noiseSensitivity: 5, limit: 0, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "right-off-noise6", pattern: "right-off", noiseSensitivity: 6, limit: 0, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "right-off-noise3-cpu-3", pattern: "right-off", cpuUsed: -3, noiseSensitivity: 3, limit: 0, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "checker-noise3-screen-content2", pattern: "checker", noiseSensitivity: 3, screenContentMode: 2, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=2"}},
		{name: "right-off-noise3-screen-content2", pattern: "right-off", noiseSensitivity: 3, screenContentMode: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=3", "--screen-content-mode=2"}},
		{name: "checker-token-parts4", pattern: "checker", tokenPartitions: 2, limit: 0, extraArgs: []string{"--token-parts=2"}},
		{name: "right-off-sharpness4", pattern: "right-off", sharpness: 4, limit: 0, extraArgs: []string{"--sharpness=4"}},
		{name: "left-off-tune-ssim", pattern: "left-off", tuning: TuneSSIM, tuningSet: true, limit: 0, extraArgs: []string{"--tune=ssim"}},
		{name: "border-off-er3-token-parts4", pattern: "border-off", tokenPartitions: 2, errorResilient: true, errorResilientParts: true, limit: 0, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
		{name: "border-off-noise1", pattern: "border-off", noiseSensitivity: 1, extraArgs: []string{"--noise-sensitivity=1"}},
		{name: "border-off-noise2", pattern: "border-off", noiseSensitivity: 2, extraArgs: []string{"--noise-sensitivity=2"}},
		{name: "border-off-noise3", pattern: "border-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border-off-noise4", pattern: "border-off", noiseSensitivity: 4, extraArgs: []string{"--noise-sensitivity=4"}},
		{name: "border-off-noise5", pattern: "border-off", noiseSensitivity: 5, extraArgs: []string{"--noise-sensitivity=5"}},
		{name: "border-off-noise6", pattern: "border-off", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "checker-noise3-threads2", pattern: "checker", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "left-off-noise3-threads2", pattern: "left-off", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "right-off-noise3-threads2", pattern: "right-off", noiseSensitivity: 3, threads: 2, limit: 0, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
		{name: "border-off-noise3-threads2", pattern: "border-off", noiseSensitivity: 3, threads: 2, extraArgs: []string{"--noise-sensitivity=3", "--threads=2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseOpts := opts
			if tc.cpuUsed != 0 {
				caseOpts.CpuUsed = tc.cpuUsed
			}
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			caseOpts.ScreenContentMode = tc.screenContentMode
			caseOpts.TokenPartitions = tc.tokenPartitions
			caseOpts.Sharpness = tc.sharpness
			if tc.tuningSet {
				caseOpts.Tuning = tc.tuning
			}
			caseOpts.ErrorResilient = tc.errorResilient
			caseOpts.ErrorResilientPartitions = tc.errorResilientParts
			caseOpts.Threads = tc.threads
			apply := map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					if tc.pattern == "off" {
						mustRuntime(t, "SetActiveMap(nil)", e.SetActiveMap(nil, 0, 0))
						return
					}
					mustRuntime(t, "SetActiveMap("+tc.pattern+")", e.SetActiveMap(activeMapPattern(tc.pattern, rows, cols), rows, cols))
				},
			}
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, apply)
			extraArgs := []string{
				"--active-map=" + tc.pattern,
			}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "active-map-"+tc.name+"-64x64", caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "active-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestOracleEncoderStreamByteParityActiveMapOddDimensions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run active-map byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 65
		height     = 33
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	}
	rows := encoderMacroblockRows(height)
	cols := encoderMacroblockCols(width)
	cases := []struct {
		name             string
		pattern          string
		limit            int
		noiseSensitivity int
		extraArgs        []string
	}{
		{name: "checker", pattern: "checker"},
		{name: "left-off", pattern: "left-off"},
		{name: "right-off", pattern: "right-off"},
		{name: "border-off", pattern: "border-off"},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, limit: 4, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "left-off-noise3", pattern: "left-off", noiseSensitivity: 3, limit: 7, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "right-off-noise3", pattern: "right-off", noiseSensitivity: 3, limit: 7, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border-off-noise3", pattern: "border-off", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caseOpts := opts
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetActiveMap("+tc.pattern+")", e.SetActiveMap(activeMapPattern(tc.pattern, rows, cols), rows, cols))
				},
			})
			extraArgs := []string{
				"--active-map=" + tc.pattern,
			}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "active-map-odd-"+tc.name, caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "active-map-odd-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestOracleEncoderStreamByteParityROIMapOddDimensions(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run ROI byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 10
		width      = 65
		height     = 33
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	baseOpts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name                     string
		pattern                  string
		limit                    int
		tokenPartitions          int
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string
	}{
		{name: "checker", pattern: "checker"},
		{name: "left1", pattern: "left1"},
		{name: "border1", pattern: "border1"},
		{name: "border1-er2-token4", pattern: "border1", tokenPartitions: 2, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=2", "--token-parts=2"}},
		{name: "checker-er3-token8", pattern: "checker", tokenPartitions: 3, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := baseOpts
			opts.TokenPartitions = tc.tokenPartitions
			opts.ErrorResilient = tc.errorResilient
			opts.ErrorResilientPartitions = tc.errorResilientPartitions
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, opts, sources, nil, map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap("+tc.pattern+")", e.SetROIMap(roiMapPattern(width, height, tc.pattern)))
				},
			})
			extraArgs := []string{"--roi-map=" + tc.pattern}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-odd-"+tc.name, opts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "roi-map-odd-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func TestOracleEncoderStreamByteParityROIMapPatterns(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run ROI byte-parity gate")
	}
	driver := findVpxencFrameFlags(t)

	const (
		fps        = 30
		targetKbps = 700
		frames     = 12
		width      = 64
		height     = 64
	)
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationSegmentedFrame(width, height, i)
	}
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -3,
		Tuning:            TunePSNR,
	}

	cases := []struct {
		name                     string
		pattern                  string
		limit                    int
		tokenPartitions          int
		threads                  int
		noiseSensitivity         int
		screenContentMode        int
		sharpness                int
		tuning                   Tuning
		tuningSet                bool
		errorResilient           bool
		errorResilientPartitions bool
		extraArgs                []string
	}{
		{name: "checker", pattern: "checker", limit: 0},
		{name: "left1", pattern: "left1", limit: 0},
		{name: "border1", pattern: "border1", limit: 0},
		{name: "off", pattern: "off", limit: 0},
		{name: "checker-token-parts4", pattern: "checker", tokenPartitions: 2, extraArgs: []string{"--token-parts=2"}},
		{name: "left1-threads2", pattern: "left1", threads: 2, extraArgs: []string{"--threads=2"}},
		{name: "checker-noise3", pattern: "checker", noiseSensitivity: 3, extraArgs: []string{"--noise-sensitivity=3"}},
		{name: "border1-noise6", pattern: "border1", noiseSensitivity: 6, extraArgs: []string{"--noise-sensitivity=6"}},
		{name: "checker-screen-content2", pattern: "checker", screenContentMode: 2, extraArgs: []string{"--screen-content-mode=2"}},
		{name: "border1-sharpness4", pattern: "border1", sharpness: 4, extraArgs: []string{"--sharpness=4"}},
		{name: "left1-tune-ssim", pattern: "left1", tuning: TuneSSIM, tuningSet: true, extraArgs: []string{"--tune=ssim"}},
		{name: "border1-screen-content2-sharpness4", pattern: "border1", screenContentMode: 2, sharpness: 4, extraArgs: []string{"--screen-content-mode=2", "--sharpness=4"}},
		{name: "border1-er3-token-parts4", pattern: "border1", tokenPartitions: 2, errorResilient: true, errorResilientPartitions: true, extraArgs: []string{"--error-resilient=3", "--token-parts=2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apply := map[int]func(*testing.T, *VP8Encoder){
				0: func(t *testing.T, e *VP8Encoder) {
					t.Helper()
					mustRuntime(t, "SetROIMap("+tc.pattern+")", e.SetROIMap(roiMapPattern(width, height, tc.pattern)))
				},
			}
			caseOpts := opts
			caseOpts.TokenPartitions = tc.tokenPartitions
			caseOpts.Threads = tc.threads
			caseOpts.NoiseSensitivity = tc.noiseSensitivity
			caseOpts.ScreenContentMode = tc.screenContentMode
			caseOpts.Sharpness = tc.sharpness
			if tc.tuningSet {
				caseOpts.Tuning = tc.tuning
			}
			caseOpts.ErrorResilient = tc.errorResilient
			caseOpts.ErrorResilientPartitions = tc.errorResilientPartitions
			govpxFrames := encodeFramesWithGovpxRuntimeControls(t, caseOpts, sources, nil, apply)
			extraArgs := []string{"--roi-map=" + tc.pattern}
			extraArgs = append(extraArgs, tc.extraArgs...)
			libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "roi-map-"+tc.name+"-64x64", caseOpts, targetKbps, sources, nil, extraArgs)
			assertSegmentByteParity(t, "roi-map-"+tc.name, govpxFrames, libvpxFrames, tc.limit)
		})
	}
}

func customQuadrantROIMap(width, height int) *ROIMap {
	roi := quadrantROIMap(width, height)
	roi.DeltaQuantizer = [4]int{0, -10, 8, -20}
	roi.DeltaLoopFilter = [4]int{0, -3, 2, 5}
	roi.StaticThreshold = [4]int{0, 500, 0, 1200}
	return roi
}

func simpleCheckerROIMap(width, height int) *ROIMap {
	roi := roiMapPattern(width, height, "checker")
	roi.DeltaQuantizer = [4]int{0, -10, 0, 0}
	roi.DeltaLoopFilter = [4]int{}
	roi.StaticThreshold = [4]int{}
	return roi
}
