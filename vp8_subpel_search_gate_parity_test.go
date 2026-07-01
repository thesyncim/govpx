package govpx

import "testing"

// TestVP8SubPelSearchGatePreservesNegativeCPUUsedCascade pins the
// negative-cpu_used (explicit cpi->Speed) fractional-search cascade.
// Every cpu_used in [-3, -16] must produce the
// interAnalysisSearchConfig.fractionalSearch dispatch matching libvpx's
// `Speed > 4 -> Step`, `Speed > 8 -> Half`, `Speed >= 15 -> Skip`
// cascade at vp8/encoder/onyx_if.c:954/1012/1023.
func TestVP8SubPelSearchGatePreservesNegativeCPUUsedCascade(t *testing.T) {
	cases := []struct {
		cpiSpeed int
		want     interAnalysisFractionalSearchMethod
	}{
		// Speed <= 4: iterative_sub_pixel=1 -> Iterative.
		{cpiSpeed: 3, want: interAnalysisFractionalSearchIterative},
		{cpiSpeed: 4, want: interAnalysisFractionalSearchIterative},
		// Speed > 4 (line 954): iterative_sub_pixel=0; quarter/half on ->
		// vp8_find_best_sub_pixel_step -> Step.
		{cpiSpeed: 5, want: interAnalysisFractionalSearchStep},
		{cpiSpeed: 6, want: interAnalysisFractionalSearchStep},
		{cpiSpeed: 7, want: interAnalysisFractionalSearchStep},
		{cpiSpeed: 8, want: interAnalysisFractionalSearchStep},
		// Speed > 8 (line 1012): quarter_pixel_search=0; half on ->
		// vp8_find_best_half_pixel_step -> Half.
		{cpiSpeed: 9, want: interAnalysisFractionalSearchHalf},
		{cpiSpeed: 10, want: interAnalysisFractionalSearchHalf},
		{cpiSpeed: 12, want: interAnalysisFractionalSearchHalf},
		{cpiSpeed: 14, want: interAnalysisFractionalSearchHalf},
		// Speed >= 15 (line 1023): half_pixel_search=0 -> vp8_skip_
		// fractional_mv_step -> Skip.
		{cpiSpeed: 15, want: interAnalysisFractionalSearchSkip},
		{cpiSpeed: 16, want: interAnalysisFractionalSearchSkip},
	}
	for _, tc := range cases {
		cpuUsed := -tc.cpiSpeed
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}
			if got := e.libvpxCPUUsed(); got != tc.cpiSpeed {
				t.Fatalf("libvpxCPUUsed() = %d, want %d", got, tc.cpiSpeed)
			}
			cfg := e.interAnalysisSearchConfig()
			if got := cfg.fractionalSearch; got != tc.want {
				t.Errorf("fractionalSearch = %d, want %d (libvpx onyx_if.c:1064-1071 after 954/1012/1023)", got, tc.want)
			}
		})
	}
}

// TestVP8SubPelSearchColdStartIterative pins that the cold-start frame
// (frameCount == 0) dispatches fractional search at the libvpxCPUUsed()
// cold-start Speed of 4 for every positive cpu_used: Speed > 4 is false,
// so the keyframe encode keeps Iterative sub-pel, matching libvpx where
// vp8_auto_select_speed's `avg_pick_mode_time == 0` branch (rdopt.c:284)
// seeds cpi->Speed = 4 for the first frame.
func TestVP8SubPelSearchColdStartIterative(t *testing.T) {
	for cpuUsed := 1; cpuUsed <= 8; cpuUsed++ {
		t.Run(cpuUsedTag(cpuUsed), func(t *testing.T) {
			e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: cpuUsed}}
			// frameCount == 0 (zero value) at construction.
			if got := e.libvpxCPUUsed(); got != 4 {
				t.Fatalf("cold-start libvpxCPUUsed() = %d, want 4", got)
			}
			cfg := e.interAnalysisSearchConfig()
			if got := cfg.fractionalSearch; got != interAnalysisFractionalSearchIterative {
				t.Errorf("cold-start fractionalSearch = %d, want Iterative (Speed=4, onyx_if.c:954 gate closed)", got)
			}
		})
	}
}

// TestVP8SubPelSearchFollowsAutoSelectSpeed pins that the fractional
// sub-pel dispatch for positive-cpu_used realtime follows the actual
// auto-select Speed (e.autoSpeed via libvpxCPUUsed()) uniformly --
// independent of frame geometry. libvpx feeds one cpi->Speed into every
// vp8_set_speed_features gate; on the reference host the production
// vpxenc keeps cpi->Speed at the realtime floor of 4 (Iterative) for
// cpu_used > 0 RT streams, and govpx's deterministic timing pin follows
// the same trajectory. A former per-gate "realistic cpu_used+1"
// override at >= 1500 MBs forced Step/Half here while libvpx ran
// Iterative -- a root-cause component of the 720p RT cpu=8 frame-drop
// divergence (see vp8_realtime_drop_parity_test.go).
func TestVP8SubPelSearchFollowsAutoSelectSpeed(t *testing.T) {
	cases := []struct {
		name      string
		autoSpeed int
		want      interAnalysisFractionalSearchMethod
	}{
		{name: "speed4-floor", autoSpeed: 4, want: interAnalysisFractionalSearchIterative},
		{name: "speed5", autoSpeed: 5, want: interAnalysisFractionalSearchStep},
		{name: "speed8", autoSpeed: 8, want: interAnalysisFractionalSearchStep},
		{name: "speed9", autoSpeed: 9, want: interAnalysisFractionalSearchHalf},
		{name: "speed14", autoSpeed: 14, want: interAnalysisFractionalSearchHalf},
		{name: "speed15", autoSpeed: 15, want: interAnalysisFractionalSearchSkip},
		{name: "speed16", autoSpeed: 16, want: interAnalysisFractionalSearchSkip},
	}
	geometries := []struct {
		name  string
		w, h  int
		label string
	}{
		{name: "720p", w: 1280, h: 720, label: "3600 MBs"},
		{name: "tiny", w: 64, h: 64, label: "16 MBs"},
	}
	for _, tc := range cases {
		for _, g := range geometries {
			t.Run(tc.name+"/"+g.name, func(t *testing.T) {
				e := &VP8Encoder{opts: EncoderOptions{Deadline: DeadlineRealtime, CpuUsed: 8, Width: g.w, Height: g.h}}
				e.autoSpeed = tc.autoSpeed
				e.frameCount = 1
				if got := e.libvpxCPUUsed(); got != tc.autoSpeed {
					t.Fatalf("libvpxCPUUsed() = %d, want autoSpeed=%d", got, tc.autoSpeed)
				}
				cfg := e.interAnalysisSearchConfig()
				if got := cfg.fractionalSearch; got != tc.want {
					t.Errorf("fractionalSearch = %d, want %d (uniform cpi->Speed dispatch, %s geometry must not matter)", got, tc.want, g.label)
				}
			})
		}
	}
}

// TestVP8SubPelSearchNonRTUnchanged pins that non-realtime deadlines
// (good / best quality) bypass the realtime fractional cascade entirely.
// libvpx's Speed > 4 / > 8 / >= 15 sub-pel gates live in the
// vp8_set_speed_features case-2 (MODE_REALTIME) branch only.
func TestVP8SubPelSearchNonRTUnchanged(t *testing.T) {
	for _, deadline := range []Deadline{DeadlineGoodQuality, DeadlineBestQuality} {
		for cpuUsed := 0; cpuUsed <= 8; cpuUsed++ {
			t.Run(string(rune('A'+int(deadline)))+"/"+cpuUsedTag(cpuUsed), func(t *testing.T) {
				e := &VP8Encoder{opts: EncoderOptions{Deadline: deadline, CpuUsed: cpuUsed}}
				e.autoSpeed = 4
				e.frameCount = 3
				cfg := e.interAnalysisSearchConfig()
				if got := cfg.fractionalSearch; got != interAnalysisFractionalSearchIterative {
					t.Errorf("non-RT fractionalSearch = %d, want Iterative (cpu_used=%d, deadline=%v)", got, cpuUsed, deadline)
				}
			})
		}
	}
}
