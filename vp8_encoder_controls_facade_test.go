package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestVP8EncoderRejectsInvalidOptions(t *testing.T) {
	valid := govpx.EncoderOptions{
		Width:             640,
		Height:            480,
		FPS:               30,
		TargetBitrateKbps: 1200,
	}
	tests := []struct {
		name string
		edit func(*govpx.EncoderOptions)
		want error
	}{
		{name: "missing bitrate", edit: func(opts *govpx.EncoderOptions) {
			opts.TargetBitrateKbps = 0
		}, want: govpx.ErrInvalidBitrate},
		{name: "bad quantizer range", edit: func(opts *govpx.EncoderOptions) {
			opts.MinQuantizer = 60
			opts.MaxQuantizer = 4
		}, want: govpx.ErrInvalidQuantizer},
		{name: "zero width", edit: func(opts *govpx.EncoderOptions) {
			opts.Width = 0
		}, want: govpx.ErrInvalidConfig},
		{name: "too many token partitions", edit: func(opts *govpx.EncoderOptions) {
			opts.TokenPartitions = 4
		}, want: govpx.ErrInvalidConfig},
		{name: "cq level above range", edit: func(opts *govpx.EncoderOptions) {
			opts.RateControlMode = govpx.RateControlCQ
			opts.MinQuantizer = 4
			opts.MaxQuantizer = 56
			opts.CQLevel = 64
		}, want: govpx.ErrInvalidQuantizer},
		{name: "constant-quality level above range", edit: func(opts *govpx.EncoderOptions) {
			opts.RateControlMode = govpx.RateControlQ
			opts.MinQuantizer = 4
			opts.MaxQuantizer = 56
			opts.CQLevel = 64
		}, want: govpx.ErrInvalidQuantizer},
		{name: "negative max intra bitrate", edit: func(opts *govpx.EncoderOptions) {
			opts.MaxIntraBitratePct = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "negative golden-frame CBR boost", edit: func(opts *govpx.EncoderOptions) {
			opts.GFCBRBoostPct = -1
		}, want: govpx.ErrInvalidConfig},
		{name: "undershoot percent above range", edit: func(opts *govpx.EncoderOptions) {
			opts.UndershootPct = 101
		}, want: govpx.ErrInvalidConfig},
		{name: "overshoot percent above range", edit: func(opts *govpx.EncoderOptions) {
			opts.OvershootPct = 101
		}, want: govpx.ErrInvalidConfig},
		{name: "screen content mode above range", edit: func(opts *govpx.EncoderOptions) {
			opts.ScreenContentMode = 3
		}, want: govpx.ErrInvalidConfig},
		{name: "bad tuning mode", edit: func(opts *govpx.EncoderOptions) {
			opts.Tuning = govpx.Tuning(2)
		}, want: govpx.ErrInvalidConfig},
		{name: "noise sensitivity above range", edit: func(opts *govpx.EncoderOptions) {
			opts.NoiseSensitivity = 7
		}, want: govpx.ErrInvalidConfig},
		{name: "ARNR max frames above range", edit: func(opts *govpx.EncoderOptions) {
			opts.ARNRMaxFrames = 16
		}, want: govpx.ErrInvalidConfig},
		{name: "ARNR type above range", edit: func(opts *govpx.EncoderOptions) {
			opts.ARNRType = 4
		}, want: govpx.ErrInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := valid
			tt.edit(&opts)
			_, err := govpx.NewVP8Encoder(opts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("NewVP8Encoder error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestVP8EncoderAcceptsHighDenoiseARNRBounds(t *testing.T) {
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:             640,
		Height:            480,
		FPS:               30,
		TargetBitrateKbps: 1200,
		NoiseSensitivity:  6,
		ARNRMaxFrames:     15,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestVP8EncoderEncodeIntoRejectsSmallBuffer(t *testing.T) {
	e := newVP8FacadeEncoder(t)

	_, err := e.EncodeInto(nil, newVP8FacadeImage(16, 16), 0, 1, 0)
	if !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("error = %v, want ErrBufferTooSmall", err)
	}
}

func TestVP8EncoderEncodeIntoWritesDecodableKeyFrame(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 22, 3, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if len(result.Data) == 0 || result.SizeBytes != len(result.Data) || !result.KeyFrame || result.PTS != 22 || result.Duration != 3 {
		t.Fatalf("EncodeResult = %+v, want populated keyframe result", result)
	}

	frame := decodeVP8FacadeFrame(t, result.Data)
	if frame.Width != 16 || frame.Height != 16 || frame.Y[0] >= 128 {
		t.Fatalf("decoded frame = %dx%d Y0=%d, want 16x16 dark source-directed frame", frame.Width, frame.Height, frame.Y[0])
	}
}

func TestVP8EncoderSetRateControlRejectsInvalidConfig(t *testing.T) {
	e := newVP8FacadeEncoder(t)

	err := e.SetRateControl(govpx.RateControlConfig{
		Mode:                govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        56,
		MaxQuantizer:        4,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if !errors.Is(err, govpx.ErrInvalidQuantizer) {
		t.Fatalf("error = %v, want ErrInvalidQuantizer", err)
	}

	err = e.SetRateControl(govpx.RateControlConfig{
		Mode:                govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       101,
		OvershootPct:        100,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("undershoot error = %v, want ErrInvalidConfig", err)
	}

	err = e.SetRateControl(govpx.RateControlConfig{
		Mode:                govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        101,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("overshoot error = %v, want ErrInvalidConfig", err)
	}
}

func TestVP8EncoderSetRateControlCQLevelAffectsNextEncode(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	err := e.SetRateControl(govpx.RateControlConfig{
		Mode:                govpx.RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             28,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	result, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	wantQIndex := vp8common.PublicQuantizerToQIndex(28)
	if result.Quantizer != 28 || vp8PacketBaseQIndex(t, result.Data) != wantQIndex {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 28 / qindex %d",
			result.Quantizer, vp8PacketBaseQIndex(t, result.Data), wantQIndex)
	}
}

func TestVP8EncoderSetCQLevelValidationAndNextEncode(t *testing.T) {
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetCQLevel(64); !errors.Is(err, govpx.ErrInvalidQuantizer) {
		t.Fatalf("out-of-range SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(3); !errors.Is(err, govpx.ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel error = %v, want ErrInvalidQuantizer", err)
	}
	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("SetCQLevel returned error: %v", err)
	}

	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	result, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	wantQIndex := vp8common.PublicQuantizerToQIndex(40)
	if result.Quantizer != 40 || vp8PacketBaseQIndex(t, result.Data) != wantQIndex {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 40 / qindex %d",
			result.Quantizer, vp8PacketBaseQIndex(t, result.Data), wantQIndex)
	}
}

func TestVP8EncoderRuntimeControlsRejectInvalidConfig(t *testing.T) {
	e := newVP8FacadeEncoder(t)

	checks := []struct {
		name string
		fn   func() error
	}{
		{"SetTokenPartitions negative", func() error { return e.SetTokenPartitions(-1) }},
		{"SetTokenPartitions out of range", func() error { return e.SetTokenPartitions(4) }},
		{"SetSharpness negative", func() error { return e.SetSharpness(-1) }},
		{"SetSharpness out of range", func() error { return e.SetSharpness(8) }},
		{"SetStaticThreshold negative", func() error { return e.SetStaticThreshold(-1) }},
		{"SetScreenContentMode negative", func() error { return e.SetScreenContentMode(-1) }},
		{"SetScreenContentMode out of range", func() error { return e.SetScreenContentMode(3) }},
		{"SetNoiseSensitivity out of range", func() error { return e.SetNoiseSensitivity(7) }},
		{"SetARNR max frames out of range", func() error { return e.SetARNR(16, 3, 3) }},
		{"SetARNR type zero", func() error { return e.SetARNR(3, 3, 0) }},
		{"SetMaxIntraBitratePct negative", func() error { return e.SetMaxIntraBitratePct(-1) }},
		{"SetGFCBRBoostPct negative", func() error { return e.SetGFCBRBoostPct(-1) }},
		{"SetDeadline invalid", func() error { return e.SetDeadline(govpx.Deadline(-1)) }},
		{"SetCPUUsed out of range", func() error { return e.SetCPUUsed(17) }},
		{"SetKeyFrameInterval negative", func() error { return e.SetKeyFrameInterval(-1) }},
		{"SetRealtimeTarget negative width", func() error {
			return e.SetRealtimeTarget(govpx.RealtimeTarget{Width: -1, Height: 16})
		}},
		{"SetRealtimeTarget negative height", func() error {
			return e.SetRealtimeTarget(govpx.RealtimeTarget{Width: 16, Height: -1})
		}},
		{"SetRealtimeTarget oversized width", func() error {
			return e.SetRealtimeTarget(govpx.RealtimeTarget{Width: 1 << 30, Height: 16})
		}},
		{"SetRealtimeTarget bad frame-drop mode", func() error {
			return e.SetRealtimeTarget(govpx.RealtimeTarget{FrameDrop: govpx.RealtimeFrameDropMode(99)})
		}},
	}
	for _, check := range checks {
		if err := check.fn(); !errors.Is(err, govpx.ErrInvalidConfig) {
			t.Fatalf("%s error = %v, want ErrInvalidConfig", check.name, err)
		}
	}

	if err := e.SetRealtimeTarget(govpx.RealtimeTarget{Width: 16, Height: 16}); err != nil {
		t.Fatalf("same-resolution SetRealtimeTarget returned error: %v", err)
	}
}

func TestVP8EncoderSetBitrateKbpsRejectsConfiguredBounds(t *testing.T) {
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1000,
		MinBitrateKbps:      500,
		MaxBitrateKbps:      1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	if err := e.SetBitrateKbps(499); !errors.Is(err, govpx.ErrInvalidBitrate) {
		t.Fatalf("below-min error = %v, want ErrInvalidBitrate", err)
	}
	if err := e.SetBitrateKbps(1501); !errors.Is(err, govpx.ErrInvalidBitrate) {
		t.Fatalf("above-max error = %v, want ErrInvalidBitrate", err)
	}
	if err := e.SetBitrateKbps(1200); err != nil {
		t.Fatalf("in-range SetBitrateKbps returned error: %v", err)
	}
}

func TestVP8EncoderSetRealtimeTargetRejectsInvalidCQBounds(t *testing.T) {
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             24,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if err := e.SetRealtimeTarget(govpx.RealtimeTarget{MinQuantizer: 30}); !errors.Is(err, govpx.ErrInvalidQuantizer) {
		t.Fatalf("SetRealtimeTarget error = %v, want ErrInvalidQuantizer", err)
	}
}

func TestVP8EncoderSetMaxIntraBitratePctAffectsKeyFrameTarget(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	if err := e.SetMaxIntraBitratePct(150); err != nil {
		t.Fatalf("SetMaxIntraBitratePct returned error: %v", err)
	}

	result, err := e.EncodeInto(make([]byte, 4096), newVP8FacadeImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.FrameTargetBits != 9199 {
		t.Fatalf("key target bits = %d, want raw-rate-capped 150%% intra target 9199", result.FrameTargetBits)
	}
}

func TestVP8EncoderRateControlTracksReachableTargetsAcrossClip(t *testing.T) {
	low := encodeVP8FacadeRateControlClip(t, 25)
	high := encodeVP8FacadeRateControlClip(t, 35)

	if low.bitrateErrorPct < -35 || low.bitrateErrorPct > 35 {
		t.Fatalf("25kbps bitrate error = %.2f%%, want within +/-35%%", low.bitrateErrorPct)
	}
	if high.bitrateErrorPct < -35 || high.bitrateErrorPct > 35 {
		t.Fatalf("35kbps bitrate error = %.2f%%, want within +/-35%%", high.bitrateErrorPct)
	}
	if high.outputBytes <= low.outputBytes {
		t.Fatalf("output bytes = low:%d high:%d, want higher target to emit more bits", low.outputBytes, high.outputBytes)
	}
	if high.meanQuantizer >= low.meanQuantizer {
		t.Fatalf("mean quantizers = low:%.2f high:%.2f, want higher target to use lower quantizer",
			low.meanQuantizer, high.meanQuantizer)
	}
}

func TestVP8EncoderRuntimeControlsAffectNextPacket(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	if err := e.SetTokenPartitions(3); err != nil {
		t.Fatalf("SetTokenPartitions returned error: %v", err)
	}
	if err := e.SetSharpness(3); err != nil {
		t.Fatalf("SetSharpness returned error: %v", err)
	}
	if err := e.SetStaticThreshold(1); err != nil {
		t.Fatalf("SetStaticThreshold returned error: %v", err)
	}
	if err := e.SetScreenContentMode(1); err != nil {
		t.Fatalf("SetScreenContentMode returned error: %v", err)
	}
	if err := e.SetAdaptiveKeyFrames(true); err != nil {
		t.Fatalf("SetAdaptiveKeyFrames returned error: %v", err)
	}
	if err := e.SetNoiseSensitivity(6); err != nil {
		t.Fatalf("SetNoiseSensitivity returned error: %v", err)
	}
	if err := e.SetARNR(15, 6, 3); err != nil {
		t.Fatalf("SetARNR returned error: %v", err)
	}

	dst := make([]byte, 8192)
	result, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	state := vp8PacketStateHeader(t, result.Data)
	if state.TokenPartition != vp8common.EightPartition {
		t.Fatalf("token partition = %d, want eight", state.TokenPartition)
	}
	if state.LoopFilter.SharpnessLevel != 0 {
		t.Fatalf("key sharpness = %d, want libvpx keyframe sharpness 0", state.LoopFilter.SharpnessLevel)
	}
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("segmentation = %+v, want static-threshold map/data update", state.Segmentation)
	}

	interSource := decodeVP8FacadeFrame(t, result.Data)
	inter, err := e.EncodeInto(dst, interSource, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interState := vp8PacketStateHeader(t, inter.Data)
	if interState.LoopFilter.SharpnessLevel != 3 {
		t.Fatalf("inter sharpness = %d, want runtime sharpness 3", interState.LoopFilter.SharpnessLevel)
	}
}

type vp8FacadeRateControlClipResult struct {
	outputBytes     int
	bitrateErrorPct float64
	meanQuantizer   float64
}

func encodeVP8FacadeRateControlClip(t testing.TB, targetKbps int) vp8FacadeRateControlClipResult {
	t.Helper()
	const (
		width  = 32
		height = 32
		fps    = 30
		frames = 20
	)
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 fps,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   targetKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineGoodQuality,
		CpuUsed:             0,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	dst := make([]byte, 4096)
	outputBytes := 0
	quantizerSum := 0
	encodedFrames := 0
	for i := range frames {
		result, err := e.EncodeInto(dst, newVP8FacadeRateControlFrame(width, height, i), uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.Dropped {
			continue
		}
		outputBytes += result.SizeBytes
		quantizerSum += result.Quantizer
		encodedFrames++
	}
	if encodedFrames != frames {
		t.Fatalf("encoded frames = %d, want %d", encodedFrames, frames)
	}

	outputKbps := float64(outputBytes*8*fps) / float64(frames*1000)
	errorPct := (outputKbps - float64(targetKbps)) * 100 / float64(targetKbps)
	return vp8FacadeRateControlClipResult{
		outputBytes:     outputBytes,
		bitrateErrorPct: errorPct,
		meanQuantizer:   float64(quantizerSum) / float64(encodedFrames),
	}
}

func newVP8FacadeRateControlFrame(width int, height int, index int) govpx.Image {
	img := newVP8FacadeImage(width, height)
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}
