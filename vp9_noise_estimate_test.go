package govpx

import (
	"testing"
)

// TestVP9NoiseEstimateInitVerbatim pins the vp9_noise_estimate_init body
// (libvpx vp9/encoder/vp9_noise_estimate.c:33-50) against hand-derived
// expectations across the four resolution buckets:
//
//	width*height >= 1920*1080 → thresh=200
//	width*height >= 1280*720  → thresh=140
//	width*height >= 640*360   → thresh=115
//	otherwise                 → thresh=90
//
// And the libvpx-defined level seed:
//
//	level = (width*height < 1280*720) ? kLowLow : kLow
//
// And the post-init invariants:
//
//	enabled        = 0
//	value          = 0
//	count          = 0
//	last_w         = 0
//	last_h         = 0
//	num_frames_estimate = 15
//	adapt_thresh   = (3 * thresh) >> 1
func TestVP9NoiseEstimateInitVerbatim(t *testing.T) {
	cases := []struct {
		name        string
		width       int
		height      int
		wantThresh  int
		wantLevel   vp9NoiseLevel
		wantAdaptTH int
	}{
		// Below 640*360 → thresh=90, level=kLowLow.
		{"qcif", 176, 144, 90, vp9NoiseLevelLowLow, (3 * 90) >> 1},
		{"cif", 352, 288, 90, vp9NoiseLevelLowLow, (3 * 90) >> 1},
		// 640*360 exactly → thresh=115.
		{"vga", 640, 360, 115, vp9NoiseLevelLowLow, (3 * 115) >> 1},
		{"ntsc", 720, 480, 115, vp9NoiseLevelLowLow, (3 * 115) >> 1},
		// 1280*720 exactly → thresh=140 + level=kLow.
		{"hd720p", 1280, 720, 140, vp9NoiseLevelLow, (3 * 140) >> 1},
		{"hd900p", 1600, 900, 140, vp9NoiseLevelLow, (3 * 140) >> 1},
		// 1920*1080 exactly → thresh=200.
		{"hd1080p", 1920, 1080, 200, vp9NoiseLevelLow, (3 * 200) >> 1},
		{"uhd4k", 3840, 2160, 200, vp9NoiseLevelLow, (3 * 200) >> 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ne vp9NoiseEstimateState
			vp9NoiseEstimateInit(&ne, tc.width, tc.height)
			if ne.enabled {
				t.Errorf("enabled = true, want false (libvpx vp9_noise_estimate.c:34)")
			}
			if ne.value != 0 {
				t.Errorf("value = %d, want 0 (libvpx vp9_noise_estimate.c:36)", ne.value)
			}
			if ne.count != 0 {
				t.Errorf("count = %d, want 0 (libvpx vp9_noise_estimate.c:37)", ne.count)
			}
			if ne.lastW != 0 || ne.lastH != 0 {
				t.Errorf("lastW=%d,lastH=%d, want 0,0 (libvpx vp9_noise_estimate.c:39-40)",
					ne.lastW, ne.lastH)
			}
			if ne.numFramesEstimate != 15 {
				t.Errorf("numFramesEstimate = %d, want 15 (libvpx vp9_noise_estimate.c:48)",
					ne.numFramesEstimate)
			}
			if ne.thresh != tc.wantThresh {
				t.Errorf("thresh = %d, want %d (libvpx vp9_noise_estimate.c:38-47)",
					ne.thresh, tc.wantThresh)
			}
			if ne.level != tc.wantLevel {
				t.Errorf("level = %d, want %d (libvpx vp9_noise_estimate.c:35)",
					ne.level, tc.wantLevel)
			}
			if ne.adaptThresh != tc.wantAdaptTH {
				t.Errorf("adaptThresh = %d, want %d (libvpx vp9_noise_estimate.c:49)",
					ne.adaptThresh, tc.wantAdaptTH)
			}
		})
	}
}

// TestVP9NoiseEstimateExtractLevelVerbatim pins the
// vp9_noise_estimate_extract_level body (libvpx
// vp9/encoder/vp9_noise_estimate.c:94-107) against hand-derived expectations
// across the four (value, thresh) boundary regions:
//
//	value > (thresh << 1) → kHigh
//	value > thresh         → kMedium
//	value > (thresh >> 1)  → kLow
//	otherwise              → kLowLow
func TestVP9NoiseEstimateExtractLevelVerbatim(t *testing.T) {
	const thresh = 115
	cases := []struct {
		name  string
		value int
		want  vp9NoiseLevel
	}{
		{"zero_below_half", 0, vp9NoiseLevelLowLow},
		{"equal_half", thresh >> 1, vp9NoiseLevelLowLow},
		{"above_half", (thresh >> 1) + 1, vp9NoiseLevelLow},
		{"equal_thresh", thresh, vp9NoiseLevelLow},
		{"above_thresh", thresh + 1, vp9NoiseLevelMedium},
		{"equal_double", thresh << 1, vp9NoiseLevelMedium},
		{"above_double", (thresh << 1) + 1, vp9NoiseLevelHigh},
		{"way_above_double", thresh * 10, vp9NoiseLevelHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ne := vp9NoiseEstimateState{value: tc.value, thresh: thresh}
			got := vp9NoiseEstimateExtractLevel(&ne)
			if got != tc.want {
				t.Errorf("extract_level(value=%d,thresh=%d) = %d, want %d",
					tc.value, thresh, got, tc.want)
			}
		})
	}
}

// TestVP9EnableNoiseEstimationVerbatim pins enable_noise_estimation (libvpx
// vp9/encoder/vp9_noise_estimate.c:52-74) across the predicate's branches.
// The full libvpx predicate is the OR of:
//
//	denoiser-on branch: noise_sensitivity > 0 && noise_est_svc && w>=320 && h>=180
//	cyclic-AQ branch:   pass==0 && rc==CBR && aq==CYCLIC_REFRESH_AQ && speed>=5
//	                    && resize_state==ORIG && !resize_pending && !use_svc
//	                    && content!=SCREEN && w*h >= 640*360
//
// Both branches return false when use_highbitdepth is true (govpx is 8-bit
// so this is always false in production).
func TestVP9EnableNoiseEstimationVerbatim(t *testing.T) {
	// Baseline cyclic-AQ branch — should enable.
	base := vp9EnableNoiseEstimationArgs{
		UseHighBitdepth:     false,
		NoiseSensitivity:    0,
		UseSVC:              false,
		Pass:                0,
		RcModeCBR:           true,
		AqModeCyclicRefresh: true,
		Speed:               5,
		ResizeStateOrig:     true,
		ResizePending:       false,
		Content:             vp9ContentDefault,
		Width:               640,
		Height:              360,
	}
	cases := []struct {
		name string
		mod  func(a *vp9EnableNoiseEstimationArgs)
		want bool
	}{
		{
			name: "cyclic_aq_baseline_640x360",
			mod:  func(a *vp9EnableNoiseEstimationArgs) {},
			want: true,
		},
		{
			name: "below_640x360_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Width = 480
				a.Height = 270
			},
			want: false,
		},
		{
			name: "non_cbr_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.RcModeCBR = false
			},
			want: false,
		},
		{
			name: "non_cyclic_aq_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.AqModeCyclicRefresh = false
			},
			want: false,
		},
		{
			name: "speed_4_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Speed = 4
			},
			want: false,
		},
		{
			name: "speed_5_at_threshold_enables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Speed = 5
			},
			want: true,
		},
		{
			name: "speed_9_enables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Speed = 9
			},
			want: true,
		},
		{
			name: "screen_content_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Content = vp9ContentScreen
			},
			want: false,
		},
		{
			name: "film_content_enables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Content = vp9ContentFilm
			},
			want: true,
		},
		{
			name: "resize_pending_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.ResizePending = true
			},
			want: false,
		},
		{
			name: "resize_state_not_orig_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.ResizeStateOrig = false
			},
			want: false,
		},
		{
			name: "use_svc_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.UseSVC = true
			},
			want: false,
		},
		{
			name: "twopass_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.Pass = 1
			},
			want: false,
		},
		{
			name: "highbitdepth_disables_even_when_otherwise_eligible",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.UseHighBitdepth = true
			},
			want: false,
		},
		// Denoiser-on branch (CONFIG_VP9_TEMPORAL_DENOISING).
		{
			name: "denoiser_branch_320x180_minimum",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.NoiseSensitivity = 1
				a.RcModeCBR = false // proves it's the denoiser branch
				a.AqModeCyclicRefresh = false
				a.Speed = 0
				a.Width = 320
				a.Height = 180
			},
			want: true,
		},
		{
			name: "denoiser_branch_below_320_width_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.NoiseSensitivity = 1
				a.RcModeCBR = false
				a.AqModeCyclicRefresh = false
				a.Width = 240
				a.Height = 180
			},
			want: false,
		},
		{
			name: "denoiser_branch_below_180_height_disables",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.NoiseSensitivity = 1
				a.RcModeCBR = false
				a.AqModeCyclicRefresh = false
				a.Width = 320
				a.Height = 144
			},
			want: false,
		},
		{
			name: "denoiser_branch_use_svc_disables_noise_est_svc",
			mod: func(a *vp9EnableNoiseEstimationArgs) {
				a.NoiseSensitivity = 1
				a.RcModeCBR = false
				a.AqModeCyclicRefresh = false
				a.UseSVC = true
				a.Width = 640
				a.Height = 360
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := base
			tc.mod(&args)
			if got := vp9EnableNoiseEstimation(args); got != tc.want {
				t.Errorf("vp9EnableNoiseEstimation(%+v) = %v, want %v",
					args, got, tc.want)
			}
		})
	}
}

// TestVP9NoiseEstimateConsumerShortCircuitLowTempVar verifies the
// libvpx vp9_speed_features.c:777-782 consumer that drops
// sf.short_circuit_low_temp_var from 3 to 2 on HD CBR sources whose noise
// estimate is medium-or-higher. Mirrors the libvpx body verbatim:
//
//	if (cpi->noise_estimate.enabled && cm->width >= 1280 && cm->height >= 720) {
//	  NOISE_LEVEL noise_level =
//	      vp9_noise_estimate_extract_level(&cpi->noise_estimate);
//	  if (noise_level >= kMedium) sf->short_circuit_low_temp_var = 2;
//	}
func TestVP9NoiseEstimateConsumerShortCircuitLowTempVar(t *testing.T) {
	cases := []struct {
		name                string
		width               int
		height              int
		neEnabled           bool
		neValue             int
		wantShortCircuitLow int
	}{
		{
			// HD + enabled + value > thresh<<1 → kHigh → 2.
			name:                "hd_enabled_high_noise_drops_to_2",
			width:               1280,
			height:              720,
			neEnabled:           true,
			neValue:             (140 << 1) + 1,
			wantShortCircuitLow: 2,
		},
		{
			// HD + enabled + value > thresh → kMedium → 2.
			name:                "hd_enabled_medium_noise_drops_to_2",
			width:               1280,
			height:              720,
			neEnabled:           true,
			neValue:             141,
			wantShortCircuitLow: 2,
		},
		{
			// HD + enabled + value > thresh>>1 → kLow → keep 3.
			name:                "hd_enabled_low_noise_keeps_3",
			width:               1280,
			height:              720,
			neEnabled:           true,
			neValue:             (140 >> 1) + 1,
			wantShortCircuitLow: 3,
		},
		{
			// HD + enabled + value 0 → kLowLow → keep 3.
			name:                "hd_enabled_lowlow_noise_keeps_3",
			width:               1280,
			height:              720,
			neEnabled:           true,
			neValue:             0,
			wantShortCircuitLow: 3,
		},
		{
			// HD + disabled → keep 3 regardless of value.
			name:                "hd_disabled_keeps_3",
			width:               1280,
			height:              720,
			neEnabled:           false,
			neValue:             10000,
			wantShortCircuitLow: 3,
		},
		{
			// Sub-HD width → consumer guard skips → keep 3.
			name:                "below_hd_width_keeps_3",
			width:               1024,
			height:              720,
			neEnabled:           true,
			neValue:             10000,
			wantShortCircuitLow: 3,
		},
		{
			// Sub-HD height → consumer guard skips → keep 3.
			name:                "below_hd_height_keeps_3",
			width:               1280,
			height:              480,
			neEnabled:           true,
			neValue:             10000,
			wantShortCircuitLow: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Speed 8 + DeadlineRealtime + RateControlCBR + CyclicRefresh
			// is the libvpx vp9_speed_features.c:771-789 entry path.
			e, err := NewVP9Encoder(VP9EncoderOptions{
				Width:              tc.width,
				Height:             tc.height,
				CpuUsed:            8,
				Deadline:           DeadlineRealtime,
				RateControlMode:    RateControlCBR,
				RateControlModeSet: true,
				TargetBitrateKbps:  1000,
				AQMode:             VP9AQCyclicRefresh,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}
			defer e.Close()

			// Seed the noise estimate state. The thresh has been set by
			// vp9NoiseEstimateInit based on width/height; override the
			// dynamic fields directly to reach each consumer branch.
			e.noiseEstimate.enabled = tc.neEnabled
			e.noiseEstimate.value = tc.neValue

			// Re-run the configurator. The consumer at
			// vp9_speed_features.c:777-782 is reached on speed >= 8 in
			// the realtime CBR non-screen path.
			ctx := e.vp9DefaultSpeedFrameContext()
			vp9SetSpeedFeaturesFramesizeIndependent(e, &e.sf, 8, ctx)
			vp9SetSpeedFeaturesFramesizeDependent(e, &e.sf, 8, ctx)

			if got := e.sf.ShortCircuitLowTempVar; got != tc.wantShortCircuitLow {
				t.Errorf("ShortCircuitLowTempVar = %d, want %d (libvpx vp9_speed_features.c:777-782)",
					got, tc.wantShortCircuitLow)
			}
		})
	}
}

// TestVP9NoiseEstimateRefreshEnabledFromEncoderOptions verifies that
// vp9NoiseEstimateRefreshEnabled (called from vp9ApplySpeedFeatures) updates
// e.noiseEstimate.enabled to match enable_noise_estimation's predicate when
// the encoder options reach the eligible cyclic-AQ branch. Mirrors libvpx's
// vp9_update_noise_estimate ne->enabled assignment
// (vp9_noise_estimate.c:129).
func TestVP9NoiseEstimateRefreshEnabledFromEncoderOptions(t *testing.T) {
	t.Run("disabled_when_below_640x360", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:              320,
			Height:             240,
			CpuUsed:            5,
			Deadline:           DeadlineRealtime,
			RateControlMode:    RateControlCBR,
			RateControlModeSet: true,
			TargetBitrateKbps:  500,
			AQMode:             VP9AQCyclicRefresh,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if e.noiseEstimate.enabled {
			t.Errorf("enabled = true on 320x240; want false (libvpx enable_noise_estimation requires w*h >= 640*360)")
		}
	})

	t.Run("enabled_on_eligible_cyclic_aq_path", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:              640,
			Height:             360,
			CpuUsed:            5,
			Deadline:           DeadlineRealtime,
			RateControlMode:    RateControlCBR,
			RateControlModeSet: true,
			TargetBitrateKbps:  1000,
			AQMode:             VP9AQCyclicRefresh,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if !e.noiseEstimate.enabled {
			t.Errorf("enabled = false on eligible 640x360 cyclic-AQ CBR speed-5 config; want true (libvpx vp9_noise_estimate.c:66-71)")
		}
	})

	t.Run("disabled_on_screen_content", func(t *testing.T) {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:              640,
			Height:             360,
			CpuUsed:            5,
			Deadline:           DeadlineRealtime,
			RateControlMode:    RateControlCBR,
			RateControlModeSet: true,
			TargetBitrateKbps:  1000,
			AQMode:             VP9AQCyclicRefresh,
			ScreenContentMode:  int8(VP9ScreenContentScreen),
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder: %v", err)
		}
		defer e.Close()
		if e.noiseEstimate.enabled {
			t.Errorf("enabled = true on screen-content config; want false (libvpx vp9_noise_estimate.c:69)")
		}
	})
}
