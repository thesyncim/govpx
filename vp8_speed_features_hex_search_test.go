package govpx

import "testing"

// TestVP8HEXSearchGateMirrorsLibvpxRealisticSpeed pins the libvpx-realistic
// HEX/iterative_sub_pixel gate. The gate mirrors the improved_mv_pred gate
// pattern: at cpu_used > 0 RT after frame 0, govpx's auto-select Speed is
// pinned to its stable Speed=0 region by interFrameAutoSpeedTimingCompensation
// while libvpx's cpi->Speed evolves to cpu_used+1. Targeting the
// search_method gate at the libvpx-realistic Speed (without disturbing the
// rest of the speed cascade) flips NSTEP+iterative → HEX+step for the
// cpu_used > 0 RT path that previously sat at NSTEP+iterative under the pin.
//
// Pre-first-frame (frameCount==0) and cpu_used <= 0 RT keep the
// non-realistic gate semantics so the cold-start path and the
// byte-parity-gated cpu_used == 0 path are unchanged.
func TestVP8HEXSearchGateMirrorsLibvpxRealisticSpeed(t *testing.T) {
	tests := []struct {
		name           string
		deadline       Deadline
		cpuUsed        int
		frameCount     uint64
		wantGateSpeed  int
		wantHexAfterRT bool
		wantStepFracRT bool
		// width/height drive libvpxRealtimeAutoSelectSpeedRamps: the
		// realistic cpu_used+1 ramp only applies at/above the ~1500-MB
		// auto-select boundary. Zero (the early-return cases) leaves the
		// frame geometry irrelevant.
		width     int
		height    int
		autoSpeed int
	}{
		{
			// cpu_used=8 RT pre-first-frame: gate falls back to
			// libvpxCPUUsed()=4 (cold start). Speed > 4 is false →
			// NSTEP+iterative preserved (matches the existing test
			// TestInterAnalysisSearchConfigKeepsLibvpxSpeed4RealtimeSearch).
			name:           "rt-cpu8-pre-first-frame",
			deadline:       DeadlineRealtime,
			cpuUsed:        8,
			frameCount:     0,
			wantGateSpeed:  4,
			wantHexAfterRT: false,
			wantStepFracRT: false,
		},
		{
			// cpu_used=8 RT after first frame on a large (720p = 3600-MB)
			// frame: at/above the auto-select ramp boundary the gate
			// escalates to cpu_used+1=9. Speed > 4 is true → HEX. The Step
			// transition that originally moved with the HEX gate is now
			// owned by libvpxRealtimeCPISpeedForSubPelSearchGate; the
			// fractional path further escalates past Step to Half via
			// libvpxRealtimeCPISpeedForQuarterPelGate (Speed > 8). So at
			// cpu_used=8 frameCount=1 RT the final fractionalSearch is
			// Half (not Step). This case still pins that the HEX gate
			// itself fires; the post-Step transitions are pinned by the
			// sub-pel and quarter-pel gate tests.
			name:           "rt-cpu8-post-first-frame-large",
			deadline:       DeadlineRealtime,
			cpuUsed:        8,
			frameCount:     1,
			width:          1280,
			height:         720,
			wantGateSpeed:  9,
			wantHexAfterRT: true,
			wantStepFracRT: false,
		},
		{
			// cpu_used=8 RT after first frame on a tiny (64x64 = 16-MB)
			// frame: below the auto-select ramp boundary, so the gate must
			// surface the real autoSpeed (=4) instead of the cpu_used+1
			// ramp. Speed > 4 is false → NSTEP+iterative preserved, matching
			// libvpx which keeps cpi->Speed at the realtime floor of 4 for
			// frames that encode in microseconds. This is the corrected
			// behavior behind the tiny cpu_used=8 RT byte-parity fix.
			name:           "rt-cpu8-post-first-frame-tiny",
			deadline:       DeadlineRealtime,
			cpuUsed:        8,
			frameCount:     1,
			width:          64,
			height:         64,
			autoSpeed:      4,
			wantGateSpeed:  4,
			wantHexAfterRT: false,
			wantStepFracRT: false,
		},
		{
			// cpu_used=0 RT (byte-parity-gated path): gate falls back
			// to libvpxCPUUsed()=4 (cold start) or autoSpeed; either
			// way it stays at the realistic Speed=4 for the stable
			// region. Speed > 4 is false → NSTEP+iterative preserved,
			// byte-parity sentinels hold.
			name:           "rt-cpu0-post-first-frame",
			deadline:       DeadlineRealtime,
			cpuUsed:        0,
			frameCount:     5,
			wantGateSpeed:  0, // autoSpeed=0 from cold-start path (post-first-frame)
			wantHexAfterRT: false,
			wantStepFracRT: false,
		},
		{
			// cpu_used=-5 RT (explicit Speed=5): gate falls back to
			// libvpxCPUUsed()=5 because the negative-cpu_used branch
			// in libvpxRealtimeCPISpeedForHEXSearchGate returns early.
			// Speed > 4 is true → HEX+step (matches the existing
			// "realtime explicit speed five switches to hex" test).
			name:           "rt-negcpu5-post-first-frame",
			deadline:       DeadlineRealtime,
			cpuUsed:        -5,
			frameCount:     1,
			wantGateSpeed:  5,
			wantHexAfterRT: true,
			wantStepFracRT: true,
		},
		{
			// Non-realtime (good quality): gate falls back to
			// libvpxCPUUsed() because the non-realtime branch in the
			// gate function returns early. No change from the previous
			// behavior on the good-quality path.
			name:           "good-cpu8-post-first-frame",
			deadline:       DeadlineGoodQuality,
			cpuUsed:        8,
			frameCount:     1,
			wantGateSpeed:  5, // good-quality clamps cpu_used to 5
			wantHexAfterRT: false,
			wantStepFracRT: false, // good-quality skips the RT-only gate
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
			if got := e.libvpxRealtimeCPISpeedForHEXSearchGate(); got != tc.wantGateSpeed {
				t.Errorf("libvpxRealtimeCPISpeedForHEXSearchGate() = %d, want %d", got, tc.wantGateSpeed)
			}
			cfg := e.interAnalysisSearchConfig()
			gotHex := cfg.fullPixelSearch == interAnalysisFullPixelSearchHex
			if gotHex != tc.wantHexAfterRT {
				t.Errorf("search_method=HEX = %t, want %t (libvpx-realistic Speed > 4)", gotHex, tc.wantHexAfterRT)
			}
			gotStep := cfg.fractionalSearch == interAnalysisFractionalSearchStep
			if gotStep != tc.wantStepFracRT {
				t.Errorf("fractional=Step = %t, want %t (libvpx onyx_if.c:954 iterative_sub_pixel=0 coupled with search_method=HEX)", gotStep, tc.wantStepFracRT)
			}
		})
	}
}
