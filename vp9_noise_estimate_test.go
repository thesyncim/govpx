package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

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
			// encoder.NoiseEstimateState.Init based on width/height; override the
			// dynamic fields directly to reach each consumer branch.
			e.noiseEstimate.Enabled = tc.neEnabled
			e.noiseEstimate.Value = tc.neValue

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
// e.noiseEstimate.Enabled to match enable_noise_estimation's predicate when
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
		if e.noiseEstimate.Enabled {
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
		if !e.noiseEstimate.Enabled {
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
		if e.noiseEstimate.Enabled {
			t.Errorf("enabled = true on screen-content config; want false (libvpx vp9_noise_estimate.c:69)")
		}
	})
}

func TestVP9NoiseEstimateUpdatePrimesLastDimensions(t *testing.T) {
	const width = 640
	const height = 360
	e := newVP9NoiseEstimateUpdateEncoderForTest(t, width, height)
	defer e.Close()
	miRows, miCols := seedVP9NoiseEstimateUpdateForTest(e,
		newVP9YCbCrForTest(width, height, 128, 128, 128), 7)
	e.frameIndex = 8

	e.vp9UpdateNoiseEstimate(newVP9CheckerYCbCrForTest(width, height, 128, 130, 128, 128),
		miRows, miCols, false)

	if e.noiseEstimate.LastW != width || e.noiseEstimate.LastH != height {
		t.Fatalf("last dimensions = %dx%d, want %dx%d",
			e.noiseEstimate.LastW, e.noiseEstimate.LastH, width, height)
	}
	if e.noiseEstimate.Value != 0 || e.noiseEstimate.Count != 0 {
		t.Fatalf("priming update value/count = %d/%d, want 0/0",
			e.noiseEstimate.Value, e.noiseEstimate.Count)
	}
}

func TestVP9NoiseEstimateUpdateHistogramFromLowMotionBlocks(t *testing.T) {
	const width = 640
	const height = 360
	e := newVP9NoiseEstimateUpdateEncoderForTest(t, width, height)
	defer e.Close()
	miRows, miCols := seedVP9NoiseEstimateUpdateForTest(e,
		newVP9YCbCrForTest(width, height, 128, 128, 128), 7)
	e.noiseEstimate.LastW = width
	e.noiseEstimate.LastH = height
	e.frameIndex = 8

	e.vp9UpdateNoiseEstimate(newVP9CheckerYCbCrForTest(width, height, 128, 130, 128, 128),
		miRows, miCols, false)

	if e.noiseEstimate.Value != 20 {
		t.Fatalf("noise value = %d, want 20 from max variance bin 2 scaled by 40/4",
			e.noiseEstimate.Value)
	}
	if e.noiseEstimate.Count != 1 {
		t.Fatalf("noise count = %d, want 1", e.noiseEstimate.Count)
	}
	if e.noiseEstimate.Level != encoder.NoiseLevelLowLow {
		t.Fatalf("noise level = %d, want LowLow before estimate window completes",
			e.noiseEstimate.Level)
	}
}

func TestVP9NoiseEstimateUpdateLowMotionGateUsesZeroBin(t *testing.T) {
	const width = 640
	const height = 360
	e := newVP9NoiseEstimateUpdateEncoderForTest(t, width, height)
	defer e.Close()
	miRows, miCols := seedVP9NoiseEstimateUpdateForTest(e,
		newVP9YCbCrForTest(width, height, 128, 128, 128), 0)
	e.noiseEstimate.LastW = width
	e.noiseEstimate.LastH = height
	e.noiseEstimate.Value = 80
	e.frameIndex = 8

	e.vp9UpdateNoiseEstimate(newVP9CheckerYCbCrForTest(width, height, 128, 130, 128, 128),
		miRows, miCols, false)

	if e.noiseEstimate.Value != 60 {
		t.Fatalf("noise value = %d, want 60 when frame_low_motion=0 leaves max_bin at zero",
			e.noiseEstimate.Value)
	}
	if e.noiseEstimate.Count != 1 {
		t.Fatalf("noise count = %d, want 1", e.noiseEstimate.Count)
	}
}

func TestVP9NoiseEstimateUpdateExtractsLevelAtWindow(t *testing.T) {
	const width = 640
	const height = 360
	e := newVP9NoiseEstimateUpdateEncoderForTest(t, width, height)
	defer e.Close()
	miRows, miCols := seedVP9NoiseEstimateUpdateForTest(e,
		newVP9YCbCrForTest(width, height, 128, 128, 128), 7)
	e.noiseEstimate.LastW = width
	e.noiseEstimate.LastH = height
	e.noiseEstimate.Value = 200
	e.noiseEstimate.Count = 0
	e.noiseEstimate.NumFramesEstimate = 1
	e.noiseEstimate.Level = encoder.NoiseLevelLowLow
	e.frameIndex = 8

	e.vp9UpdateNoiseEstimate(newVP9CheckerYCbCrForTest(width, height, 128, 130, 128, 128),
		miRows, miCols, false)

	if e.noiseEstimate.NumFramesEstimate != 30 {
		t.Fatalf("numFramesEstimate = %d, want 30 after first completed estimate window",
			e.noiseEstimate.NumFramesEstimate)
	}
	if e.noiseEstimate.Count != 0 {
		t.Fatalf("noise count = %d, want reset to 0", e.noiseEstimate.Count)
	}
	if e.noiseEstimate.Level != encoder.NoiseLevelMedium {
		t.Fatalf("noise level = %d, want Medium after extracting value %d",
			e.noiseEstimate.Level, e.noiseEstimate.Value)
	}
}

func TestVP9DenoiserUsesNoiseEstimateLowLowAsInactive(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              640,
		Height:             360,
		CpuUsed:            8,
		Deadline:           DeadlineRealtime,
		RateControlMode:    RateControlCBR,
		RateControlModeSet: true,
		TargetBitrateKbps:  2000,
		NoiseSensitivity:   4,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if !e.noiseEstimate.Enabled {
		t.Fatal("noise estimate disabled; want enabled for VP9 temporal denoiser branch")
	}
	e.noiseEstimate.Value = 0

	src := newVP9YCbCrForTest(640, 360, 102, 98, 158)
	if got := e.prepareVP9DenoiserSource(src); got != src {
		t.Fatal("prepareVP9DenoiserSource returned denoiser source at LowLow; want caller source")
	}
	if got := e.denoiser.level; got != vp9DenoiserLowLow {
		t.Fatalf("denoiser level = %d, want LowLow from noise estimate", got)
	}
	if e.denoiser.active() {
		t.Fatal("denoiser active at LowLow noise estimate; want inactive")
	}
}

func newVP9NoiseEstimateUpdateEncoderForTest(t *testing.T, width, height int) *VP9Encoder {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
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
	if !e.noiseEstimate.Enabled {
		t.Fatal("noise estimate disabled; want enabled for realtime CBR cyclic-AQ")
	}
	return e
}

func seedVP9NoiseEstimateUpdateForTest(e *VP9Encoder, last *image.YCbCr, consec uint8) (int, int) {
	e.vp9CommitLastSource(last, true, false)
	miRows := (e.opts.Height + 7) >> 3
	miCols := (e.opts.Width + 7) >> 3
	e.cyclicAQ.vp9CyclicRefreshAlloc(miRows, miCols)
	for i := range e.cyclicAQ.consecZeroMv {
		e.cyclicAQ.consecZeroMv[i] = consec
	}
	return miRows, miCols
}
