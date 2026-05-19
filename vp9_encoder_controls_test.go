package govpx

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestNewVP9EncoderRequiresDimensions(t *testing.T) {
	cases := []VP9EncoderOptions{
		{Width: 0, Height: 480},
		{Width: 640, Height: 0},
		{Width: -1, Height: 480},
	}
	for i, opts := range cases {
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("case %d: err = %v, want ErrInvalidConfig", i, err)
		}
	}
}

// TestNewVP9EncoderRejectsBadOptions covers the per-field bounds
// checks beyond the dimension gate.
func TestNewVP9EncoderRejectsBadOptions(t *testing.T) {
	base := VP9EncoderOptions{Width: 320, Height: 240}
	type bad struct {
		mutate func(*VP9EncoderOptions)
		want   error
	}
	cases := []bad{
		{func(o *VP9EncoderOptions) { o.Width = maxVP9Dimension + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Height = maxVP9Dimension + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Threads = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.RowMT = true }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RowMT = true
			o.Threads = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Log2TileRows = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Log2TileRows = 3 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Height = 64
			o.Log2TileRows = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Deadline = Deadline(-1) }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.CpuUsed = -10 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.CpuUsed = 10 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TargetBitrateKbps = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Quantizer = 256 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MinQuantizer = -1 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.MaxQuantizer = 64 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) { o.CQLevel = 64 }, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.MinQuantizer = 40
			o.MaxQuantizer = 20
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.MinQuantizer = 20
			o.MaxQuantizer = 40
			o.CQLevel = 10
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Quantizer = 64
			o.CQLevel = 32
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringTwoLayers,
			}
		}, ErrInvalidBitrate},
		{func(o *VP9EncoderOptions) {
			o.TargetBitrateKbps = 300
			o.TemporalScalability = TemporalScalabilityConfig{
				Enabled: true,
				Mode:    TemporalLayeringMode(99),
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled: true,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: VP9MaxSpatialLayers + 1,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: 2,
				LayerID:    2,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:              true,
				LayerCount:           2,
				InterLayerDependency: true,
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:    true,
				LayerCount: 2,
				LayerID:    1,
				Width:      [VP9RTPMaxSpatialLayers]uint16{32, 64},
				Height:     [VP9RTPMaxSpatialLayers]uint16{32, 64},
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.SpatialScalability = VP9SpatialScalabilityConfig{
				Enabled:           true,
				LayerCount:        2,
				LayerID:           1,
				ResolutionPresent: true,
				Width:             [VP9RTPMaxSpatialLayers]uint16{32, 32},
				Height:            [VP9RTPMaxSpatialLayers]uint16{32, 32},
			}
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Lossless = true
			o.Quantizer = 1
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Lossless = true
			o.Segmentation.Enabled = true
			o.Segmentation.AltQEnabled[0] = true
			o.Segmentation.AltQ[0] = 1
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Lossless = true
			o.Segmentation.Enabled = true
			o.Segmentation.AltLFEnabled[0] = true
			o.Segmentation.AltLF[0] = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.MinKeyframeInterval = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.MaxKeyframeInterval = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.MinKeyframeInterval = 3
			o.MaxKeyframeInterval = 2
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.MinKeyframeInterval = 129 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.LookaheadFrames = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.LookaheadFrames = vp9MaxLookaheadFrames + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRMaxFrames = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRMaxFrames = maxARNRFrames + 1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRStrength = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRStrength = 7 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRType = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ARNRType = 4 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ScreenContentMode = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.ScreenContentMode = 3 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.NoiseSensitivity = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.NoiseSensitivity = 7 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.Sharpness = 8 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.StaticThreshold = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AutoAltRef = true }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AutoAltRef = true
			o.LookaheadFrames = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AutoAltRef = true
			o.LookaheadFrames = 2
			o.ErrorResilient = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AQMode = VP9AQMode(99) }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AQMode = VP9AQComplexity }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQComplexity
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
			o.TargetBitrateKbps = 300
			o.Lossless = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQComplexity
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
			o.TargetBitrateKbps = 300
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.AQMode = VP9AQCyclicRefresh }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
			o.Lossless = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQVariance
			o.Lossless = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.AQMode = VP9AQVariance
			o.Segmentation.Enabled = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlMode(99)
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
		}, ErrInvalidBitrate},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlVBR
			o.TargetBitrateKbps = 300
			o.DropFrameAllowed = true
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.TwoPassStats = finalizedVP9TwoPassTestStats(100, 200)
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
			o.TwoPassStats = finalizedVP9TwoPassTestStats(100, 200)
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.RateControlModeSet = true
			o.RateControlMode = RateControlQ
			o.TargetBitrateKbps = 300
			o.TwoPassStats = finalizedVP9TwoPassTestStats(100, 200)
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TwoPassVBRBiasPct = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TwoPassMinPct = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TwoPassMaxPct = -1 }, ErrInvalidConfig},
		// Lookahead + CBR/SVC are now permitted; the validator only
		// rejects the cyclic-refresh AQ combination via validateVP9AQOptions.
		{func(o *VP9EncoderOptions) {
			o.LookaheadFrames = 2
			o.AQMode = VP9AQCyclicRefresh
			o.RateControlModeSet = true
			o.RateControlMode = RateControlCBR
			o.TargetBitrateKbps = 300
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.FPS = -1 }, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) { o.TimebaseNum = 1 }, ErrInvalidConfig}, // missing Den
		{func(o *VP9EncoderOptions) { o.TimebaseDen = 1 }, ErrInvalidConfig}, // missing Num
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.SegmentID = VP9MaxSegments
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.SegmentID = 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.AltQEnabled[0] = true
			o.Segmentation.AltQ[0] = -256
		}, ErrInvalidQuantizer},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.AltLFEnabled[0] = true
			o.Segmentation.AltLF[0] = 64
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.RefFrameEnabled[0] = true
			o.Segmentation.RefFrame[0] = VP9RefFrameIntra - 1
		}, ErrInvalidConfig},
		{func(o *VP9EncoderOptions) {
			o.Segmentation.Enabled = true
			o.Segmentation.RefFrameEnabled[0] = true
			o.Segmentation.RefFrame[0] = vp9dec.AltrefFrame + 1
		}, ErrInvalidConfig},
	}
	for i, c := range cases {
		opts := base
		c.mutate(&opts)
		_, err := NewVP9Encoder(opts)
		if !errors.Is(err, c.want) {
			t.Errorf("case %d: err = %v, want %v", i, err, c.want)
		}
	}
}

// TestNewVP9EncoderAcceptsMinimalOptions: a bare {Width,Height}
// works.
func TestNewVP9EncoderAcceptsMinimalOptions(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.Codec(); got != CodecVP9 {
		t.Errorf("Codec() = %v, want CodecVP9", got)
	}
	if e.opts.Deadline != DeadlineRealtime || e.opts.CpuUsed != vp9DefaultCPUUsed {
		t.Fatalf("default VP9 speed = deadline:%d cpu:%d, want realtime/%d",
			e.opts.Deadline, e.opts.CpuUsed, vp9DefaultCPUUsed)
	}
}

func TestVP9EncoderSpeedControlsUpdateSpeedFeatures(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	// applyInterFrameSF mirrors what vp9ApplySpeedFeatures does for an
	// inter-frame at the top of encode_frame_to_data_rate. libvpx's
	// vp9_bitstream.c:691-693 gate consults cpi->sf.tx_size_search_method
	// directly, so the helper reflects whatever per-frame SF context the
	// encoder last computed. Exercise the inter-frame branch explicitly
	// (the framesize-independent dispatcher splits on is_keyframe at
	// vp9_speed_features.c:579-581 / :611-613 and the configurator drops
	// USE_TX_8X8 for keyframes).
	applyInterFrameSF := func() {
		ctx := e.vp9DefaultSpeedFrameContext()
		ctx.frameType = common.InterFrame
		ctx.intraOnly = false
		e.vp9ApplySpeedFeatures(ctx)
	}
	// applyKeyFrameSF rebuilds the SF using the default (key-frame, intra-only)
	// context. vp9RealtimeVariancePartitionEnabled is consumed by the keyframe
	// var-partition gate (vp9_encoder.go:5753 vp9CBRKeyframeVariancePartitionEnabled),
	// so it must be inspected with the same context.
	applyKeyFrameSF := func() {
		e.vp9ApplySpeedFeatures(e.vp9DefaultSpeedFrameContext())
	}

	applyInterFrameSF()
	if got := e.vp9CoeffProbAppxStep(); got != 4 {
		t.Fatalf("default coeff step = %d, want 4", got)
	}
	// libvpx RT speed >= 4 non-key sets tx_size_search_method = USE_TX_8X8
	// (vp9_speed_features.c:581), so the compressed-header gate at
	// vp9_bitstream.c:692 fires for default cpu_used=8 RT.
	if !e.vp9SkipTx16PlusCoefUpdates() {
		t.Fatal("default speed should skip tx16+ coef updates")
	}
	applyKeyFrameSF()
	if !e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("default speed should enable realtime variance partition")
	}

	if err := e.SetDeadline(DeadlineGoodQuality); err != nil {
		t.Fatalf("SetDeadline(good): %v", err)
	}
	applyInterFrameSF()
	// libvpx good-mode never sets coeff_prob_appx_step (only realtime speed 5
	// does, vp9_speed_features.c:610), so the field stays at the best-quality
	// default of 1.
	if got := e.vp9CoeffProbAppxStep(); got != 1 {
		t.Fatalf("good coeff step = %d, want 1", got)
	}
	// libvpx GOOD speed >= 1 sets tx_size_search_method = USE_LARGESTALL for
	// inter frames (vp9_speed_features.c:492-493), and the speed >= 4 GOOD
	// branch does NOT flip it to USE_TX_8X8 — only use_fast_coef_updates is
	// flipped to ONE_LOOP_REDUCED (vp9_speed_features.c:395). The wire gate
	// at vp9_bitstream.c:692 keys strictly on tx_size_search_method, so GOOD
	// cpu8 must NOT request the skip even though the FAST_COEFF_UPDATE
	// path runs.
	if e.vp9SkipTx16PlusCoefUpdates() {
		t.Fatal("good cpu8 should not request tx16+ skip per libvpx tx_size_search_method=USE_LARGESTALL")
	}
	// libvpx good-mode never selects VAR_BASED_PARTITION (it's an RT-only
	// path).
	applyKeyFrameSF()
	if e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("good deadline should not use realtime variance partition")
	}

	// Going back through good-mode speed 0 (cpu_used=0) confirms the skip
	// stays off (default USE_FULL_RD).
	if err := e.SetCPUUsed(0); err != nil {
		t.Fatalf("SetCPUUsed(0) good: %v", err)
	}
	applyInterFrameSF()
	if e.vp9SkipTx16PlusCoefUpdates() {
		t.Fatal("good cpu0 should not request tx16+ skip")
	}
	if err := e.SetCPUUsed(8); err != nil {
		t.Fatalf("SetCPUUsed(8) good: %v", err)
	}

	if err := e.SetDeadline(DeadlineRealtime); err != nil {
		t.Fatalf("SetDeadline(rt): %v", err)
	}
	if err := e.SetCPUUsed(5); err != nil {
		t.Fatalf("SetCPUUsed(5): %v", err)
	}
	applyInterFrameSF()
	if got := e.vp9CoeffProbAppxStep(); got != 4 {
		t.Fatalf("rt cpu5 coeff step = %d, want 4", got)
	}
	if !e.vp9SkipTx16PlusCoefUpdates() {
		t.Fatal("rt cpu5 should skip tx16+ coef updates")
	}
	applyKeyFrameSF()
	if e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("rt cpu5 should not enable speed8 variance partition")
	}

	if err := e.SetCPUUsed(4); err != nil {
		t.Fatalf("SetCPUUsed(4): %v", err)
	}
	applyInterFrameSF()
	if got := e.vp9CoeffProbAppxStep(); got != 1 {
		t.Fatalf("rt cpu4 coeff step = %d, want 1", got)
	}
	if !e.vp9SkipTx16PlusCoefUpdates() {
		t.Fatal("rt cpu4 should still skip tx16+ coef updates")
	}

	if err := e.SetCPUUsed(0); err != nil {
		t.Fatalf("SetCPUUsed(0): %v", err)
	}
	applyInterFrameSF()
	if e.vp9SkipTx16PlusCoefUpdates() {
		t.Fatal("rt cpu0 should not skip tx16+ coef updates")
	}
	if err := e.SetCPUUsed(-9); err != nil {
		t.Fatalf("SetCPUUsed(-9): %v", err)
	}
	applyKeyFrameSF()
	if !e.vp9RealtimeVariancePartitionEnabled() {
		t.Fatal("rt cpu-9 should use abs(cpu-used) speed")
	}

	beforeDeadline, beforeCPU := e.opts.Deadline, e.opts.CpuUsed
	if err := e.SetDeadline(Deadline(-1)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetDeadline invalid err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetCPUUsed(10); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetCPUUsed invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.Deadline != beforeDeadline || e.opts.CpuUsed != beforeCPU {
		t.Fatal("invalid VP9 speed controls mutated encoder")
	}
}

func TestVP9EncoderSetNoiseSensitivity(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for _, tc := range []struct {
		sensitivity int
		level       int8
	}{
		{sensitivity: 1, level: vp9DenoiserLow},
		{sensitivity: 2, level: vp9DenoiserMedium},
		{sensitivity: 3, level: vp9DenoiserHigh},
		{sensitivity: 4, level: vp9DenoiserHigh},
		{sensitivity: 5, level: vp9DenoiserHigh},
		{sensitivity: 6, level: vp9DenoiserHigh},
	} {
		if err := e.SetNoiseSensitivity(tc.sensitivity); err != nil {
			t.Fatalf("SetNoiseSensitivity(%d): %v", tc.sensitivity, err)
		}
		if e.opts.NoiseSensitivity != int8(tc.sensitivity) {
			t.Fatalf("NoiseSensitivity = %d, want %d",
				e.opts.NoiseSensitivity, tc.sensitivity)
		}
		if e.denoiser.sensitivity != int8(tc.sensitivity) ||
			e.denoiser.level != tc.level {
			t.Fatalf("denoiser sensitivity/level = %d/%d, want %d/%d",
				e.denoiser.sensitivity, e.denoiser.level,
				tc.sensitivity, tc.level)
		}
	}
	if err := e.SetNoiseSensitivity(7); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetNoiseSensitivity invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.NoiseSensitivity != 6 {
		t.Fatal("invalid SetNoiseSensitivity mutated encoder")
	}
	if err := e.SetNoiseSensitivity(0); err != nil {
		t.Fatalf("SetNoiseSensitivity(0): %v", err)
	}
	if e.opts.NoiseSensitivity != 0 || e.denoiser.sensitivity != 0 {
		t.Fatalf("disabled noise sensitivity = opts:%d state:%d, want 0/0",
			e.opts.NoiseSensitivity, e.denoiser.sensitivity)
	}
}

func TestVP9EncoderSetScreenContentMode(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for _, mode := range []int{0, 1, 2} {
		if err := e.SetScreenContentMode(mode); err != nil {
			t.Fatalf("SetScreenContentMode(%d): %v", mode, err)
		}
		if e.opts.ScreenContentMode != int8(mode) {
			t.Fatalf("ScreenContentMode = %d, want %d",
				e.opts.ScreenContentMode, mode)
		}
	}
	if err := e.SetScreenContentMode(3); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetScreenContentMode invalid err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.ScreenContentMode != 2 {
		t.Fatal("invalid SetScreenContentMode mutated encoder")
	}

	if got := vp9NoReferenceIntraModeCount(common.Block32x32, 0); got != 1 {
		t.Fatalf("default 32x32 no-ref intra mode count = %d, want 1", got)
	}
	if got := vp9NoReferenceIntraModeCount(common.Block32x32, 1); got != 3 {
		t.Fatalf("screen 32x32 no-ref intra mode count = %d, want 3", got)
	}
	if got := vp9NoReferenceIntraModeCount(common.Block32x32, 2); got != 1 {
		t.Fatalf("film 32x32 no-ref intra mode count = %d, want 1", got)
	}
	if got := vp9NoReferenceIntraModeCount(common.Block16x16, 0); got != 3 {
		t.Fatalf("default 16x16 no-ref intra mode count = %d, want 3", got)
	}
}

func TestVP9EncoderSetKeyFrameInterval(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetKeyFrameInterval(2); err != nil {
		t.Fatalf("SetKeyFrameInterval(2): %v", err)
	}
	if e.opts.MinKeyframeInterval != 0 || e.opts.MaxKeyframeInterval != 2 {
		t.Fatalf("keyframe interval range = %d/%d, want 0/2",
			e.opts.MinKeyframeInterval, e.opts.MaxKeyframeInterval)
	}
	dst := make([]byte, 65536)
	results := make([]VP9EncodeResult, 3)
	for frame := range results {
		src := newVP9YCbCrForTest(width, height, uint8(96+frame), 128, 128)
		results[frame], err = e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult[%d]: %v", frame, err)
		}
	}
	if !results[0].KeyFrame || results[1].KeyFrame || !results[2].KeyFrame {
		t.Fatalf("keyframe cadence = [%t %t %t], want [true false true]",
			results[0].KeyFrame, results[1].KeyFrame, results[2].KeyFrame)
	}
	before := e.opts.MaxKeyframeInterval
	if err := e.SetKeyFrameInterval(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetKeyFrameInterval(-1) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.MaxKeyframeInterval != before {
		t.Fatal("invalid SetKeyFrameInterval mutated encoder")
	}
	if err := e.SetKeyFrameInterval(0); err != nil {
		t.Fatalf("SetKeyFrameInterval(0): %v", err)
	}
	if e.opts.MinKeyframeInterval != 0 || e.opts.MaxKeyframeInterval != 0 {
		t.Fatalf("keyframe interval reset = %d/%d, want 0/0",
			e.opts.MinKeyframeInterval, e.opts.MaxKeyframeInterval)
	}
	if err := e.SetKeyFrameIntervalRange(2, 2); err != nil {
		t.Fatalf("SetKeyFrameIntervalRange(2,2): %v", err)
	}
	if e.opts.MinKeyframeInterval != 2 || e.opts.MaxKeyframeInterval != 2 {
		t.Fatalf("keyframe interval range = %d/%d, want 2/2",
			e.opts.MinKeyframeInterval, e.opts.MaxKeyframeInterval)
	}
	beforeMin, beforeMax := e.opts.MinKeyframeInterval, e.opts.MaxKeyframeInterval
	if err := e.SetKeyFrameIntervalRange(3, 2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetKeyFrameIntervalRange(3,2) err = %v, want ErrInvalidConfig", err)
	}
	if e.opts.MinKeyframeInterval != beforeMin || e.opts.MaxKeyframeInterval != beforeMax {
		t.Fatal("invalid SetKeyFrameIntervalRange mutated encoder")
	}
	if err := e.SetKeyFrameInterval(1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetKeyFrameInterval(1) below active min err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetARNR(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:           width,
		Height:          height,
		LookaheadFrames: 4,
		AutoAltRef:      true,
		ARNRMaxFrames:   1,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if len(e.vp9ARNRScratch.Y) != 0 {
		t.Fatal("ARNR scratch allocated before ARNR is enabled")
	}
	if err := e.SetARNR(5, 6, 1); err != nil {
		t.Fatalf("SetARNR: %v", err)
	}
	if e.opts.ARNRMaxFrames != 5 || e.opts.ARNRStrength != 6 ||
		e.opts.ARNRType != 1 {
		t.Fatalf("ARNR opts = max:%d strength:%d type:%d, want 5/6/1",
			e.opts.ARNRMaxFrames, e.opts.ARNRStrength, e.opts.ARNRType)
	}
	if len(e.vp9ARNRScratch.Y) == 0 {
		t.Fatal("SetARNR did not allocate ARNR scratch for active auto-alt-ref")
	}
	for frame := range 4 {
		src := newVP9YCbCrForTest(width, height, uint8(96+frame*12), 128, 128)
		if err := e.pushVP9Lookahead(src, 0); err != nil {
			t.Fatalf("pushVP9Lookahead[%d]: %v", frame, err)
		}
	}
	future, ok := e.newestVP9LookaheadEntry()
	if !ok {
		t.Fatal("newestVP9LookaheadEntry returned !ok")
	}
	if !e.applyVP9ARNRFilter(future) {
		t.Fatal("runtime SetARNR filter returned false")
	}
	if bytes.Equal(e.vp9ARNRScratch.Y, future.img.Y) {
		t.Fatal("runtime SetARNR left ARNR scratch equal to source")
	}
	before := e.opts
	for _, tc := range []struct {
		name string
		max  int
		str  int
		typ  int
	}{
		{name: "max low", max: -1, str: 3, typ: 3},
		{name: "max high", max: maxARNRFrames + 1, str: 3, typ: 3},
		{name: "strength low", max: 5, str: -1, typ: 3},
		{name: "strength high", max: 5, str: 7, typ: 3},
		{name: "type low", max: 5, str: 3, typ: -1},
		{name: "type high", max: 5, str: 3, typ: 4},
	} {
		if err := e.SetARNR(tc.max, tc.str, tc.typ); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("%s SetARNR err = %v, want ErrInvalidConfig", tc.name, err)
		}
		if e.opts.ARNRMaxFrames != before.ARNRMaxFrames ||
			e.opts.ARNRStrength != before.ARNRStrength ||
			e.opts.ARNRType != before.ARNRType {
			t.Fatalf("%s invalid SetARNR mutated opts", tc.name)
		}
	}
}

func TestVP9EncoderNoiseSensitivityDenoisesInterLuma(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 100, 96, 160)
	interSrc := newVP9YCbCrForTest(width, height, 102, 98, 158)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	if !e.denoiser.active() {
		t.Fatal("denoiser inactive after noise-sensitive encode")
	}
	if got := interSrc.Y[0]; got != 102 {
		t.Fatalf("caller source was mutated: Y[0]=%d, want 102", got)
	}
	if got := e.denoiser.runningAvg[vp9DenoiserAvgLast].Y[0]; got != 100 {
		t.Fatalf("denoised LAST running average Y[0] = %d, want 100", got)
	}
	if got := e.denoiser.source.Y[0]; got != 100 {
		t.Fatalf("denoised encoder source Y[0] = %d, want 100", got)
	}
}

func TestVP9EncoderNoiseSensitivityDenoisesInterChroma(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:            width,
		Height:           height,
		NoiseSensitivity: 3,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := newVP9YCbCrForTest(width, height, 100, 96, 160)
	interSrc := newVP9YCbCrForTest(width, height, 102, 98, 158)
	dst := make([]byte, 65536)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	if !e.denoiser.active() {
		t.Fatal("denoiser inactive after noise-sensitive encode")
	}
	if got := interSrc.Cb[0]; got != 98 {
		t.Fatalf("caller source was mutated: Cb[0]=%d, want 98", got)
	}
	if got := interSrc.Cr[0]; got != 158 {
		t.Fatalf("caller source was mutated: Cr[0]=%d, want 158", got)
	}
	if got := e.denoiser.runningAvg[vp9DenoiserAvgLast].Cb[0]; got != 96 {
		t.Fatalf("denoised LAST running average Cb[0] = %d, want 96", got)
	}
	if got := e.denoiser.runningAvg[vp9DenoiserAvgLast].Cr[0]; got != 160 {
		t.Fatalf("denoised LAST running average Cr[0] = %d, want 160", got)
	}
	if got := e.denoiser.source.Cb[0]; got != 96 {
		t.Fatalf("denoised encoder source Cb[0] = %d, want 96", got)
	}
	if got := e.denoiser.source.Cr[0]; got != 160 {
		t.Fatalf("denoised encoder source Cr[0] = %d, want 160", got)
	}
}

// TestVP9EncoderClose: after Close, Encode/EncodeInto return
// ErrClosed.
func TestVP9EncoderClose(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 320, Height: 240})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	img := image.NewYCbCr(image.Rect(0, 0, 320, 240), image.YCbCrSubsampleRatio420)
	if _, err := e.Encode(img); !errors.Is(err, ErrClosed) {
		t.Errorf("Encode after Close err = %v, want ErrClosed", err)
	}
}

// TestVP9EncoderIsKeyFrameNextCadence: first frame is always a key;
// later frames key on MaxKeyframeInterval boundaries (default 128).
func TestVP9EncoderIsKeyFrameNextCadence(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: 320, Height: 240, MaxKeyframeInterval: 4,
	})
	if !e.IsKeyFrameNext() {
		t.Error("first frame should be key")
	}
	// Pretend we encoded one frame.
	e.frameIndex = 1
	if e.IsKeyFrameNext() {
		t.Error("frame 1 should NOT be key when cadence=4")
	}
	e.frameIndex = 4
	if !e.IsKeyFrameNext() {
		t.Error("frame 4 should be key (cadence boundary)")
	}
	// After Close → never key.
	e.Close()
	if e.IsKeyFrameNext() {
		t.Error("closed encoder should never report key")
	}
}
