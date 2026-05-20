package encoder

import "testing"

func TestNoiseEstimateInit(t *testing.T) {
	cases := []struct {
		name        string
		width       int
		height      int
		wantThresh  int
		wantLevel   NoiseLevel
		wantAdaptTH int
	}{
		{name: "below_360p", width: 320, height: 180, wantThresh: 90, wantLevel: NoiseLevelLowLow, wantAdaptTH: 135},
		{name: "360p_bucket", width: 640, height: 360, wantThresh: 115, wantLevel: NoiseLevelLowLow, wantAdaptTH: 172},
		{name: "720p_bucket", width: 1280, height: 720, wantThresh: 140, wantLevel: NoiseLevelLow, wantAdaptTH: 210},
		{name: "1080p_bucket", width: 1920, height: 1080, wantThresh: 200, wantLevel: NoiseLevelLow, wantAdaptTH: 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ne NoiseEstimateState
			ne.Init(tc.width, tc.height)
			if ne.Enabled {
				t.Fatal("Enabled = true, want false")
			}
			if ne.Value != 0 {
				t.Errorf("Value = %d, want 0", ne.Value)
			}
			if ne.Count != 0 {
				t.Errorf("Count = %d, want 0", ne.Count)
			}
			if ne.LastW != 0 || ne.LastH != 0 {
				t.Errorf("last dimensions = %dx%d, want 0x0", ne.LastW, ne.LastH)
			}
			if ne.NumFramesEstimate != 15 {
				t.Errorf("NumFramesEstimate = %d, want 15", ne.NumFramesEstimate)
			}
			if ne.Thresh != tc.wantThresh {
				t.Errorf("Thresh = %d, want %d", ne.Thresh, tc.wantThresh)
			}
			if ne.Level != tc.wantLevel {
				t.Errorf("Level = %d, want %d", ne.Level, tc.wantLevel)
			}
			if ne.AdaptThresh != tc.wantAdaptTH {
				t.Errorf("AdaptThresh = %d, want %d", ne.AdaptThresh, tc.wantAdaptTH)
			}
		})
	}
}

func TestNoiseEstimateExtractLevel(t *testing.T) {
	cases := []struct {
		name   string
		thresh int
		value  int
		want   NoiseLevel
	}{
		{name: "nil_defaults_lowlow", thresh: 115, value: -1, want: NoiseLevelLowLow},
		{name: "below_half_threshold", thresh: 115, value: 57, want: NoiseLevelLowLow},
		{name: "above_half_threshold", thresh: 115, value: 58, want: NoiseLevelLow},
		{name: "at_threshold_still_low", thresh: 115, value: 115, want: NoiseLevelLow},
		{name: "above_threshold_medium", thresh: 115, value: 116, want: NoiseLevelMedium},
		{name: "at_double_threshold_still_medium", thresh: 115, value: 230, want: NoiseLevelMedium},
		{name: "above_double_threshold_high", thresh: 115, value: 231, want: NoiseLevelHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value < 0 {
				var ne *NoiseEstimateState
				if got := ne.ExtractLevel(); got != tc.want {
					t.Fatalf("nil ExtractLevel = %d, want %d", got, tc.want)
				}
				return
			}
			ne := NoiseEstimateState{Value: tc.value, Thresh: tc.thresh}
			if got := ne.ExtractLevel(); got != tc.want {
				t.Fatalf("ExtractLevel(value=%d, thresh=%d) = %d, want %d",
					tc.value, tc.thresh, got, tc.want)
			}
		})
	}
}

func TestEnableNoiseEstimation(t *testing.T) {
	base := EnableNoiseEstimationArgs{
		NoiseSensitivity:    0,
		UseSVC:              false,
		Pass:                0,
		RcModeCBR:           true,
		AqModeCyclicRefresh: true,
		Speed:               5,
		ResizeStateOrig:     true,
		ResizePending:       false,
		ScreenContent:       false,
		Width:               640,
		Height:              360,
	}
	cases := []struct {
		name string
		mod  func(*EnableNoiseEstimationArgs)
		want bool
	}{
		{name: "cyclic_aq_baseline_640x360", mod: func(*EnableNoiseEstimationArgs) {}, want: true},
		{name: "below_640x360_disables", mod: func(a *EnableNoiseEstimationArgs) { a.Width, a.Height = 480, 270 }, want: false},
		{name: "non_cbr_disables", mod: func(a *EnableNoiseEstimationArgs) { a.RcModeCBR = false }, want: false},
		{name: "non_cyclic_aq_disables", mod: func(a *EnableNoiseEstimationArgs) { a.AqModeCyclicRefresh = false }, want: false},
		{name: "speed_4_disables", mod: func(a *EnableNoiseEstimationArgs) { a.Speed = 4 }, want: false},
		{name: "speed_5_at_threshold_enables", mod: func(a *EnableNoiseEstimationArgs) { a.Speed = 5 }, want: true},
		{name: "speed_9_enables", mod: func(a *EnableNoiseEstimationArgs) { a.Speed = 9 }, want: true},
		{name: "screen_content_disables", mod: func(a *EnableNoiseEstimationArgs) { a.ScreenContent = true }, want: false},
		{name: "resize_pending_disables", mod: func(a *EnableNoiseEstimationArgs) { a.ResizePending = true }, want: false},
		{name: "resize_state_not_orig_disables", mod: func(a *EnableNoiseEstimationArgs) { a.ResizeStateOrig = false }, want: false},
		{name: "use_svc_disables", mod: func(a *EnableNoiseEstimationArgs) { a.UseSVC = true }, want: false},
		{name: "twopass_disables", mod: func(a *EnableNoiseEstimationArgs) { a.Pass = 1 }, want: false},
		{name: "highbitdepth_disables", mod: func(a *EnableNoiseEstimationArgs) { a.UseHighBitdepth = true }, want: false},
		{name: "denoiser_branch_320x180_minimum", mod: func(a *EnableNoiseEstimationArgs) {
			a.NoiseSensitivity = 1
			a.RcModeCBR = false
			a.AqModeCyclicRefresh = false
			a.Speed = 0
			a.Width = 320
			a.Height = 180
		}, want: true},
		{name: "denoiser_branch_below_320_width_disables", mod: func(a *EnableNoiseEstimationArgs) {
			a.NoiseSensitivity = 1
			a.RcModeCBR = false
			a.AqModeCyclicRefresh = false
			a.Width = 240
			a.Height = 180
		}, want: false},
		{name: "denoiser_branch_below_180_height_disables", mod: func(a *EnableNoiseEstimationArgs) {
			a.NoiseSensitivity = 1
			a.RcModeCBR = false
			a.AqModeCyclicRefresh = false
			a.Width = 320
			a.Height = 144
		}, want: false},
		{name: "denoiser_branch_use_svc_disables", mod: func(a *EnableNoiseEstimationArgs) {
			a.NoiseSensitivity = 1
			a.RcModeCBR = false
			a.AqModeCyclicRefresh = false
			a.UseSVC = true
			a.Width = 640
			a.Height = 360
		}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := base
			tc.mod(&args)
			if got := EnableNoiseEstimation(args); got != tc.want {
				t.Fatalf("EnableNoiseEstimation(%+v) = %v, want %v", args, got, tc.want)
			}
		})
	}
}

func TestNoiseEstimateUpdatePrimesLastDimensions(t *testing.T) {
	ne, args := newNoiseEstimateUpdateCase(640, 360, 7)
	ne.Update(args)

	if ne.LastW != args.Width || ne.LastH != args.Height {
		t.Fatalf("last dimensions = %dx%d, want %dx%d",
			ne.LastW, ne.LastH, args.Width, args.Height)
	}
	if ne.Value != 0 || ne.Count != 0 {
		t.Fatalf("priming update value/count = %d/%d, want 0/0",
			ne.Value, ne.Count)
	}
}

func TestNoiseEstimateUpdateHistogramFromLowMotionBlocks(t *testing.T) {
	ne, args := newNoiseEstimateUpdateCase(640, 360, 7)
	ne.LastW = args.Width
	ne.LastH = args.Height

	ne.Update(args)

	if ne.Value != 20 {
		t.Fatalf("noise value = %d, want 20 from max variance bin 2 scaled by 40/4",
			ne.Value)
	}
	if ne.Count != 1 {
		t.Fatalf("noise count = %d, want 1", ne.Count)
	}
	if ne.Level != NoiseLevelLowLow {
		t.Fatalf("noise level = %d, want LowLow before estimate window completes",
			ne.Level)
	}
}

func TestNoiseEstimateUpdateLowMotionGateUsesZeroBin(t *testing.T) {
	ne, args := newNoiseEstimateUpdateCase(640, 360, 0)
	ne.LastW = args.Width
	ne.LastH = args.Height
	ne.Value = 80

	ne.Update(args)

	if ne.Value != 60 {
		t.Fatalf("noise value = %d, want 60 when frame_low_motion=0 leaves max_bin at zero",
			ne.Value)
	}
	if ne.Count != 1 {
		t.Fatalf("noise count = %d, want 1", ne.Count)
	}
}

func TestNoiseEstimateUpdateExtractsLevelAtWindow(t *testing.T) {
	ne, args := newNoiseEstimateUpdateCase(640, 360, 7)
	ne.LastW = args.Width
	ne.LastH = args.Height
	ne.Value = 200
	ne.Count = 0
	ne.NumFramesEstimate = 1
	ne.Level = NoiseLevelLowLow

	ne.Update(args)

	if ne.NumFramesEstimate != 30 {
		t.Fatalf("numFramesEstimate = %d, want 30 after first completed estimate window",
			ne.NumFramesEstimate)
	}
	if ne.Count != 0 {
		t.Fatalf("noise count = %d, want reset to 0", ne.Count)
	}
	if ne.Level != NoiseLevelMedium {
		t.Fatalf("noise level = %d, want Medium after extracting value %d",
			ne.Level, ne.Value)
	}
}

func TestNoiseEstimateUpdateClearsCyclicCountersOnIntraOnly(t *testing.T) {
	ne, args := newNoiseEstimateUpdateCase(640, 360, 7)
	args.IntraOnly = true
	ne.LastW = args.Width
	ne.LastH = args.Height

	ne.Update(args)

	for i, got := range args.ConsecZeroMV {
		if got != 0 {
			t.Fatalf("consecZeroMV[%d] = %d, want 0 on intra-only frame", i, got)
		}
	}
	if ne.Value != 0 {
		t.Fatalf("noise value = %d, want 0 after intra-only counter reset", ne.Value)
	}
}

func newNoiseEstimateUpdateCase(width, height int, consec uint8) (NoiseEstimateState, NoiseEstimateUpdateArgs) {
	last := make([]byte, width*height)
	src := make([]byte, width*height)
	for i := range last {
		last[i] = 128
	}
	for y := range height {
		row := src[y*width:]
		for x := range width {
			if (x+y)&1 == 0 {
				row[x] = 128
			} else {
				row[x] = 130
			}
		}
	}

	miRows := (height + 7) >> 3
	miCols := (width + 7) >> 3
	consecZeroMV := make([]uint8, miRows*miCols)
	for i := range consecZeroMV {
		consecZeroMV[i] = consec
	}

	var ne NoiseEstimateState
	ne.Init(width, height)
	ne.Enabled = true
	return ne, NoiseEstimateUpdateArgs{
		Width:             width,
		Height:            height,
		FrameCounter:      8,
		NoiseSensitivity:  0,
		MIRows:            miRows,
		MICols:            miCols,
		SourceY:           src,
		SourceYStride:     width,
		SourceWidth:       width,
		SourceHeight:      height,
		LastSourceY:       last,
		LastSourceYStride: width,
		LastSourceWidth:   width,
		LastSourceHeight:  height,
		LastSourceValid:   true,
		ConsecZeroMV:      consecZeroMV,
	}
}
