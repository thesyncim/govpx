package govpx

import "testing"

// TestVP8HEXSearchGateFollowsCPISpeed pins that the search_method=HEX /
// iterative_sub_pixel=0 promotion (libvpx vp8_set_speed_features case 2,
// vp8/encoder/onyx_if.c:951-955, `Speed > 4`) consults the SAME cpi->Speed
// as every other speed-conditioned feature: the deterministic auto-select
// model surfaced by libvpxCPUUsed().
//
// libvpx evaluates all of vp8_set_speed_features against one cpi->Speed
// value per frame. On the reference host the production vpxenc keeps
// cpi->Speed clamped at the realtime floor of 4 for cpu_used > 0 RT
// (per-frame encode time sits far below the ms_for_compress budget, so
// vp8_auto_select_speed rdopt.c:296 keeps decrementing), and govpx's
// pinned autoSpeed follows the same trajectory. A former per-gate
// "libvpx-realistic cpu_used+1" override at >= 1500 MBs was calibrated
// against instrumented-oracle timings and made govpx run HEX while the
// production vpxenc ran NSTEP -- the root cause of the 720p RT cpu=8
// frame-drop divergence (see the drop-parity audit note in
// vp8_encoder_config.go and vp8_realtime_drop_parity_test.go).
func TestVP8HEXSearchGateFollowsCPISpeed(t *testing.T) {
	tests := []struct {
		name         string
		deadline     Deadline
		cpuUsed      int
		frameCount   uint64
		width        int
		height       int
		autoSpeed    int
		wantSpeed    int
		wantHex      bool
		wantStepFrac bool
	}{
		{
			// cpu_used=8 RT pre-first-frame: libvpxCPUUsed()=4 (cold
			// start). Speed > 4 is false -> NSTEP+iterative preserved.
			name:         "rt-cpu8-pre-first-frame",
			deadline:     DeadlineRealtime,
			cpuUsed:      8,
			frameCount:   0,
			wantSpeed:    4,
			wantHex:      false,
			wantStepFrac: false,
		},
		{
			// cpu_used=8 RT after first frame on a large (720p = 3600-MB)
			// frame with the pinned autoSpeed=4 trajectory: Speed > 4 is
			// false -> NSTEP+iterative, matching the production vpxenc
			// whose cpi->Speed stays at the realtime floor of 4. This is
			// the drop-parity fix pin: the former realistic-speed override
			// forced HEX here and inflated interframe sizes ~1.9x.
			name:         "rt-cpu8-post-first-frame-720p-floor",
			deadline:     DeadlineRealtime,
			cpuUsed:      8,
			frameCount:   1,
			width:        1280,
			height:       720,
			autoSpeed:    4,
			wantSpeed:    4,
			wantHex:      false,
			wantStepFrac: false,
		},
		{
			// cpu_used=8 RT after first frame on a tiny (64x64 = 16-MB)
			// frame: same uniform dispatch, autoSpeed=4 -> NSTEP+iterative,
			// matching libvpx which keeps cpi->Speed at the realtime floor
			// of 4 for frames that encode in microseconds.
			name:         "rt-cpu8-post-first-frame-tiny",
			deadline:     DeadlineRealtime,
			cpuUsed:      8,
			frameCount:   1,
			width:        64,
			height:       64,
			autoSpeed:    4,
			wantSpeed:    4,
			wantHex:      false,
			wantStepFrac: false,
		},
		{
			// If auto-select ever ramps (autoSpeed=9, e.g. a genuinely
			// overloaded host trajectory mirrored by the timing model),
			// the same uniform dispatch fires HEX; the fractional path
			// escalates past Step to Half via the Speed > 8 gate.
			name:         "rt-cpu8-post-first-frame-ramped",
			deadline:     DeadlineRealtime,
			cpuUsed:      8,
			frameCount:   1,
			width:        1280,
			height:       720,
			autoSpeed:    9,
			wantSpeed:    9,
			wantHex:      true,
			wantStepFrac: false, // Speed > 8 promotes Step -> Half
		},
		{
			// cpu_used=0 RT (byte-parity-gated path): autoSpeed=0 ->
			// Speed 0, NSTEP+iterative preserved, byte-parity sentinels
			// hold.
			name:         "rt-cpu0-post-first-frame",
			deadline:     DeadlineRealtime,
			cpuUsed:      0,
			frameCount:   5,
			wantSpeed:    0,
			wantHex:      false,
			wantStepFrac: false,
		},
		{
			// cpu_used=-5 RT (explicit Speed=5): Speed > 4 is true ->
			// HEX+step (libvpx encodeframe.c:686-687 explicit-Speed
			// branch).
			name:         "rt-negcpu5-post-first-frame",
			deadline:     DeadlineRealtime,
			cpuUsed:      -5,
			frameCount:   1,
			wantSpeed:    5,
			wantHex:      true,
			wantStepFrac: true,
		},
		{
			// Non-realtime (good quality): the RT-only gates are skipped
			// entirely; good-quality clamps cpu_used to 5.
			name:         "good-cpu8-post-first-frame",
			deadline:     DeadlineGoodQuality,
			cpuUsed:      8,
			frameCount:   1,
			wantSpeed:    5,
			wantHex:      false,
			wantStepFrac: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &VP8Encoder{
				opts: EncoderOptions{
					Deadline: tc.deadline,
					CpuUsed:  tc.cpuUsed,
					Width:    tc.width,
					Height:   tc.height,
				},
				frameCount: tc.frameCount,
				autoSpeed:  tc.autoSpeed,
			}
			if got := e.libvpxCPUUsed(); got != tc.wantSpeed {
				t.Errorf("libvpxCPUUsed() = %d, want %d", got, tc.wantSpeed)
			}
			cfg := e.interAnalysisSearchConfig()
			gotHex := cfg.fullPixelSearch == interAnalysisFullPixelSearchHex
			if gotHex != tc.wantHex {
				t.Errorf("search_method=HEX = %t, want %t (libvpx onyx_if.c:951 Speed > 4)", gotHex, tc.wantHex)
			}
			gotStep := cfg.fractionalSearch == interAnalysisFractionalSearchStep
			if gotStep != tc.wantStepFrac {
				t.Errorf("fractional=Step = %t, want %t (libvpx onyx_if.c:954 iterative_sub_pixel=0 coupled with search_method=HEX)", gotStep, tc.wantStepFrac)
			}
		})
	}
}
