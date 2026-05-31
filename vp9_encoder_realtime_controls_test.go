package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"reflect"
	"testing"
)

func TestVP9EncoderSetRealtimeTargetUpdatesExplicitVBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 600, FPS: 60}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.TargetBitrateKbps != 600 || e.rc.targetBitrateKbps != 600 ||
		e.rc.bitsPerFrame != 10000 {
		t.Fatalf("rate state after target = opts:%d rc:%d bits:%d, want 600/600/10000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps, e.rc.bitsPerFrame)
	}
}

func TestVP9EncoderSetRealtimeTargetUpdatesHints(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		TargetBitrateKbps: 900,
	})

	if err := e.SetRealtimeTarget(RealtimeTarget{
		BitrateKbps: 1500,
		FPS:         60,
	}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.TargetBitrateKbps != 1500 ||
		e.opts.FPS != 60 ||
		e.opts.TimebaseNum != 1 ||
		e.opts.TimebaseDen != 60 {
		t.Fatalf("opts after target = %+v, want bitrate 1500 and 1/60 timebase",
			e.opts)
	}
}

func TestVP9EncoderSetRealtimeTargetResizeForcesKeyFrame(t *testing.T) {
	const (
		w1 = 64
		h1 = 64
		w2 = 96
		h2 = 80
	)
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: w1, Height: h1})
	if _, err := e.Encode(vp9test.NewYCbCr(w1, h1, 72, 128, 128)); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(vp9test.NewYCbCr(w1, h1, 92, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if h, _ := vp9test.ParseHeader(t, inter); h.FrameType != common.InterFrame {
		t.Fatalf("pre-resize frame type = %d, want inter", h.FrameType)
	}
	if !e.refValid[vp9LastRefSlot] || !e.refFrames[vp9LastRefSlot].valid {
		t.Fatal("LAST reference not valid before resize")
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: w2, Height: h2}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.opts.Width != w2 || e.opts.Height != h2 {
		t.Fatalf("dims after resize = %dx%d, want %dx%d",
			e.opts.Width, e.opts.Height, w2, h2)
	}
	if !e.IsKeyFrameNext() || !e.forceKeyFrame {
		t.Fatal("resize did not force the next VP9 frame to keyframe")
	}
	for slot := range e.refValid {
		if e.refValid[slot] || e.refFrames[slot].valid {
			t.Fatalf("reference slot %d still valid after resize", slot)
		}
	}
	if _, err := e.Encode(vp9test.NewYCbCr(w1, h1, 100, 128, 128)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("old-size Encode after resize err = %v, want ErrInvalidConfig", err)
	}

	resized, err := e.Encode(vp9test.NewYCbCr(w2, h2, 111, 123, 211))
	if err != nil {
		t.Fatalf("Encode resized keyframe: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, resized)
	if h.FrameType != common.KeyFrame || h.Width != w2 || h.Height != h2 {
		t.Fatalf("resized header = type:%d %dx%d, want key %dx%d",
			h.FrameType, h.Width, h.Height, w2, h2)
	}
	if e.forceKeyFrame {
		t.Fatal("forceKeyFrame still set after resized keyframe")
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(resized); err != nil {
		t.Fatalf("Decode resized keyframe: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after resized keyframe")
	}
	assertVP9FilledFrameWithin(t, frame, w2, h2, 111, 123, 211, 1)
}

func TestVP9EncoderSetRealtimeTargetResizeRecomputesRateControl(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  10_000,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.rc.effectiveBitrateKbps != 2949 || e.rc.bitsPerFrame != 98300 {
		t.Fatalf("initial effective rate = %d/%d, want 2949/98300",
			e.rc.effectiveBitrateKbps, e.rc.bitsPerFrame)
	}

	if err := e.SetRealtimeTarget(RealtimeTarget{Width: 32, Height: 32}); err != nil {
		t.Fatalf("SetRealtimeTarget resize: %v", err)
	}
	if e.opts.Width != 32 || e.opts.Height != 32 ||
		e.rc.codedWidth != 32 || e.rc.codedHeight != 32 {
		t.Fatalf("resize state = opts:%dx%d rc:%dx%d, want 32x32 on both",
			e.opts.Width, e.opts.Height, e.rc.codedWidth, e.rc.codedHeight)
	}
	if e.rc.targetBitrateKbps != 10_000 ||
		e.rc.effectiveBitrateKbps != 737 ||
		e.rc.targetBandwidthBits != 737000 ||
		e.rc.bitsPerFrame != 24567 {
		t.Fatalf("resized rate = target:%d effective:%d bandwidth:%d bpf:%d, want 10000/737/737000/24567",
			e.rc.targetBitrateKbps, e.rc.effectiveBitrateKbps,
			e.rc.targetBandwidthBits, e.rc.bitsPerFrame)
	}
	if e.rc.bufferSizeBits != 4_422_000 ||
		e.rc.bufferInitialBits != 2_948_000 ||
		e.rc.bufferOptimalBits != 3_685_000 {
		t.Fatalf("resized buffers = size:%d initial:%d optimal:%d, want raw-capped defaults",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits)
	}
	if !e.forceKeyFrame {
		t.Fatal("resize did not force the next VP9 frame to keyframe")
	}
}

func TestVP9EncoderSetRealtimeTargetValidationNoMutation(t *testing.T) {
	const width, height = 64, 64
	cases := []struct {
		name   string
		target RealtimeTarget
		want   error
	}{
		{"negative bitrate", RealtimeTarget{BitrateKbps: -1}, ErrInvalidConfig},
		{"one dimension", RealtimeTarget{Width: 96}, ErrInvalidConfig},
		{"too wide", RealtimeTarget{Width: maxVP9Dimension + 1, Height: 64}, ErrInvalidConfig},
		{"explicit frame drop", RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}, ErrInvalidConfig},
		{"bad quantizer range", RealtimeTarget{MinQuantizer: 50, MaxQuantizer: 20}, ErrInvalidQuantizer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
			if err := e.SetRealtimeTarget(tc.target); !errors.Is(err, tc.want) {
				t.Fatalf("SetRealtimeTarget error = %v, want %v", err, tc.want)
			}
			if e.opts.Width != width || e.opts.Height != height ||
				e.forceKeyFrame || e.opts.TargetBitrateKbps != 0 {
				t.Fatalf("encoder mutated after reject: opts=%+v forceKeyFrame=%t",
					e.opts, e.forceKeyFrame)
			}
			packet, err := e.Encode(vp9test.NewYCbCr(width, height, 80, 128, 128))
			if err != nil {
				t.Fatalf("Encode after rejected target: %v", err)
			}
			info, err := PeekVP9StreamInfo(packet)
			if err != nil {
				t.Fatalf("PeekVP9StreamInfo: %v", err)
			}
			if !info.KeyFrame || info.Width != width || info.Height != height {
				t.Fatalf("info after rejected target = %+v, want original keyframe", info)
			}
		})
	}
}

func TestVP9EncoderSetRealtimeTargetUpdatesQuantizerBounds(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{MinQuantizer: 20, MaxQuantizer: 20}); err != nil {
		t.Fatalf("SetRealtimeTarget: %v", err)
	}
	if e.opts.MinQuantizer != 20 || e.opts.MaxQuantizer != 20 {
		t.Fatalf("quantizer bounds = %d/%d, want 20/20",
			e.opts.MinQuantizer, e.opts.MaxQuantizer)
	}
	packet, err := e.Encode(vp9test.NewYCbCr(width, height, 80, 128, 128))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if got, want := int(h.Quant.BaseQindex), encoder.PublicQuantizerToQIndex(20); got != want {
		t.Fatalf("BaseQindex = %d, want %d", got, want)
	}
}

func TestVP9EncoderSetBitrateKbpsUpdatesRateControl(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		TargetBitrateKbps:   300,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled: true,
			Mode:    TemporalLayeringTwoLayers,
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = maxInt()
	if err := e.SetBitrateKbps(900); err != nil {
		t.Fatalf("SetBitrateKbps: %v", err)
	}
	if e.opts.TargetBitrateKbps != 900 || e.rc.targetBitrateKbps != 900 ||
		e.rc.targetBandwidthBits != 900000 || e.rc.bitsPerFrame != 30000 {
		t.Fatalf("bitrate state = opts:%d rc:%d bandwidth:%d bpf:%d, want 900/900/900000/30000",
			e.opts.TargetBitrateKbps, e.rc.targetBitrateKbps,
			e.rc.targetBandwidthBits, e.rc.bitsPerFrame)
	}
	if e.rc.bufferSizeBits != 540000 || e.rc.bufferInitialBits != 360000 ||
		e.rc.bufferOptimalBits != 450000 || e.rc.bufferLevelBits != 540000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 540000/360000/450000/540000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}
	if got := e.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 540 || got[1] != 900 {
		t.Fatalf("temporal bitrates after SetBitrateKbps = %v, want [540 900 ...]", got)
	}

	oldRC := e.rc
	oldOpts := e.opts
	oldTemporal := e.temporal
	oldTwoPass := e.twoPass
	if err := e.SetBitrateKbps(0); !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("invalid SetBitrateKbps err = %v, want ErrInvalidBitrate", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) ||
		e.temporal != oldTemporal || !reflect.DeepEqual(e.twoPass, oldTwoPass) {
		t.Fatal("invalid SetBitrateKbps mutated encoder state")
	}

	publicQ, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	if err := publicQ.SetBitrateKbps(900); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("public-Q SetBitrateKbps err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9EncoderSetCQLevelUpdatesPublicQAndRateControl(t *testing.T) {
	publicQ, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder public-Q: %v", err)
	}
	beforeQ := publicQ.vp9EncoderPublicQModeQIndex(false, false, 1<<vp9LastRefSlot)
	if err := publicQ.SetCQLevel(20); err != nil {
		t.Fatalf("public-Q SetCQLevel: %v", err)
	}
	afterQ := publicQ.vp9EncoderPublicQModeQIndex(false, false, 1<<vp9LastRefSlot)
	if publicQ.opts.CQLevel != 20 || afterQ == beforeQ {
		t.Fatalf("public-Q CQ update = opts:%d q:%d before:%d, want changed level 20",
			publicQ.opts.CQLevel, afterQ, beforeQ)
	}

	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  700,
		RateControlModeSet: true,
		RateControlMode:    RateControlQ,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		CQLevel:            20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder RC-Q: %v", err)
	}
	if err := e.SetCQLevel(24); err != nil {
		t.Fatalf("RC-Q SetCQLevel: %v", err)
	}
	if e.opts.CQLevel != 24 ||
		e.rc.cqLevel != uint8(encoder.PublicQuantizerToQIndex(24)) {
		t.Fatalf("RC-Q CQ update = opts:%d rc:%d, want 24/%d",
			e.opts.CQLevel, e.rc.cqLevel, encoder.PublicQuantizerToQIndex(24))
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetCQLevel(3); !errors.Is(err, ErrInvalidQuantizer) {
		t.Fatalf("below-min SetCQLevel err = %v, want ErrInvalidQuantizer", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetCQLevel mutated encoder state")
	}
	if err := e.SetCQLevel(0); err != nil {
		t.Fatalf("reset SetCQLevel: %v", err)
	}
	if e.opts.CQLevel != 0 ||
		e.rc.cqLevel != uint8(encoder.PublicQuantizerToQIndex(vp9DefaultCQLevel)) {
		t.Fatalf("reset CQ state = opts:%d rc:%d, want 0/%d",
			e.opts.CQLevel, e.rc.cqLevel,
			encoder.PublicQuantizerToQIndex(vp9DefaultCQLevel))
	}
}
