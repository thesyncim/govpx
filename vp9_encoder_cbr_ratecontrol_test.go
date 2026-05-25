package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"reflect"
	"testing"
)

func TestVP9EncoderCBRDropBufferUnderrunReturnsDropped(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	dst := make([]byte, 65536)
	key, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if !key.KeyFrame || key.Dropped || len(key.Data) == 0 {
		t.Fatalf("key result = key:%t dropped:%t data:%d, want encoded keyframe",
			key.KeyFrame, key.Dropped, len(key.Data))
	}

	e.rc.bufferLevelBits = -e.rc.bitsPerFrame - 1
	drainedBuffer := e.rc.bufferLevelBits
	wantBufferAfterRefill := drainedBuffer + e.rc.bitsPerFrame
	inter, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("inter EncodeIntoWithResult: %v", err)
	}
	if !inter.Dropped || inter.KeyFrame || len(inter.Data) != 0 || inter.SizeBytes != 0 {
		t.Fatalf("inter result = key:%t dropped:%t size:%d data:%d, want dropped inter",
			inter.KeyFrame, inter.Dropped, inter.SizeBytes, len(inter.Data))
	}
	if inter.TargetBitrateKbps != 1 || inter.FrameTargetBits != encoder.FrameOverhead {
		t.Fatalf("inter rate = kbps:%d target:%d, want 1/%d",
			inter.TargetBitrateKbps, inter.FrameTargetBits, encoder.FrameOverhead)
	}
	if inter.BufferLevelBits != wantBufferAfterRefill {
		t.Fatalf("buffer after drop = %d, want %d",
			inter.BufferLevelBits, wantBufferAfterRefill)
	}
}

func TestVP9EncoderCBRSelectsLibvpxQuantizers(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		TargetBitrateKbps:   700,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	wantQ := [...]int{16, 145, 145, 162}
	for i, want := range wantQ {
		src := vp9test.NewYCbCr(width, height, uint8(96+i*11), 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
		if result.InternalQuantizer != want {
			t.Fatalf("frame %d internal quantizer = %d, want %d",
				i, result.InternalQuantizer, want)
		}
	}
}

// TestVP9EncoderCBRFrameTargetMatchesLibvpx asserts the one-pass CBR
// frame-target formula matches libvpx vp9_calc_iframe_target_size_one_pass_cbr
// (kf_boost ramp uses starting_buffer_level/2 on the very first frame) and
// the inter-frame per-frame bandwidth target on subsequent inter frames.
// Prior to the kf_boost port the keyframe target was hard-coded to the
// per-frame bandwidth, which produced a slightly higher base qindex than the
// libvpx CLI on small frames (libvpx: vp9_ratectrl.c:2205-2232).

func TestVP9EncoderCBRFrameTargetMatchesLibvpx(t *testing.T) {
	const width, height = 64, 64
	const fps = 30
	const bufferInitialSizeMs = 400
	for _, targetKbps := range [...]int{700, 140} {
		e, err := NewVP9Encoder(VP9EncoderOptions{
			Width:               width,
			Height:              height,
			FPS:                 fps,
			TargetBitrateKbps:   targetKbps,
			RateControlModeSet:  true,
			RateControlMode:     RateControlCBR,
			BufferSizeMs:        600,
			BufferInitialSizeMs: bufferInitialSizeMs,
			BufferOptimalSizeMs: 500,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
		})
		if err != nil {
			t.Fatalf("NewVP9Encoder target %d: %v", targetKbps, err)
		}
		dst := make([]byte, 65536)
		wantKeyTarget := targetKbps * bufferInitialSizeMs / 2
		for i := range 3 {
			want := 0
			if i == 0 {
				// libvpx: vp9_calc_iframe_target_size_one_pass_cbr returns
				// starting_buffer_level/2 on the very first video frame.
				want = wantKeyTarget
			} else {
				want = e.rc.onePassCBRInterFrameTargetBits(
					e.vp9InterRefreshFrameFlags(0))
			}
			src := vp9test.NewYCbCr(width, height, uint8(96+i*11), 128, 128)
			result, err := e.EncodeIntoWithResult(src, dst)
			if err != nil {
				t.Fatalf("EncodeIntoWithResult target %d frame %d: %v",
					targetKbps, i, err)
			}
			if result.FrameTargetBits != want {
				t.Fatalf("target %d frame %d target bits = %d, want %d",
					targetKbps, i, result.FrameTargetBits, want)
			}
		}
	}
}

func TestVP9EncoderCBRDropDoesNotDropKeyOrInvisibleFrame(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		TargetBitrateKbps:  1,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	dst := make([]byte, 65536)

	e.rc.bufferLevelBits = -1
	key, err := e.EncodeIntoWithResult(src, dst)
	if err != nil {
		t.Fatalf("key EncodeIntoWithResult: %v", err)
	}
	if !key.KeyFrame || key.Dropped || len(key.Data) == 0 {
		t.Fatalf("key result = key:%t dropped:%t data:%d, want encoded keyframe",
			key.KeyFrame, key.Dropped, len(key.Data))
	}

	e.rc.bufferLevelBits = -1
	hidden, err := e.EncodeIntoWithFlagsResult(src, dst, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("hidden EncodeIntoWithFlagsResult: %v", err)
	}
	if hidden.Dropped || hidden.KeyFrame || hidden.ShowFrame || len(hidden.Data) == 0 {
		t.Fatalf("hidden result = key:%t show:%t dropped:%t data:%d, want encoded hidden inter",
			hidden.KeyFrame, hidden.ShowFrame, hidden.Dropped, len(hidden.Data))
	}
}

func TestVP9RateControlDropWatermarkDecimation(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		bufferSizeBits:      12000,
		bitsPerFrame:        1000,
	}

	rc.bufferLevelBits = 6000
	reason, drop := rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("first watermark check = reason:%d drop:%t factor:%d count:%d, want arm only",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
	reason, drop = rc.testDropInterFrame()
	if !drop || reason != vp9DropWatermarkDecimation || rc.decimationFactor != 1 || rc.decimationCount != 0 {
		t.Fatalf("second watermark check = reason:%d drop:%t factor:%d count:%d, want decimation drop",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
	reason, drop = rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("third watermark check = reason:%d drop:%t factor:%d count:%d, want re-arm",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}

	rc.bufferLevelBits = 7000
	reason, drop = rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.decimationFactor != 0 || rc.decimationCount != 0 {
		t.Fatalf("recovered watermark check = reason:%d drop:%t factor:%d count:%d, want reset",
			reason, drop, rc.decimationFactor, rc.decimationCount)
	}
}

func TestVP9RateControlDropNegativeBufferBypassesWatermark(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		decimationFactor:    1,
		decimationCount:     1,
		bufferLevelBits:     -1,
	}

	reason, drop := rc.testDropInterFrame()
	if !drop || reason != vp9DropNegativeBuffer {
		t.Fatalf("negative buffer drop = reason:%d drop:%t, want negative-buffer drop",
			reason, drop)
	}
	if rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("negative buffer changed decimation = factor:%d count:%d, want unchanged 1/1",
			rc.decimationFactor, rc.decimationCount)
	}
}

func TestVP9RateControlPreEncodeRefillPrecedesDropGate(t *testing.T) {
	rc := vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 60,
		bufferOptimalBits:   10000,
		bufferSizeBits:      12000,
		bitsPerFrame:        1000,
		bufferLevelBits:     -1,
	}

	rc.preEncodeFrame(true)
	reason, drop := rc.testDropInterFrame()
	if drop || reason != vp9DropNone || rc.bufferLevelBits != 999 ||
		rc.decimationFactor != 1 || rc.decimationCount != 1 {
		t.Fatalf("first drop gate = reason:%d drop:%t buffer:%d factor:%d count:%d, want pre-refill arm only",
			reason, drop, rc.bufferLevelBits, rc.decimationFactor,
			rc.decimationCount)
	}
	rc.preEncodeFrame(true)
	reason, drop = rc.testDropInterFrame()
	if !drop || reason != vp9DropWatermarkDecimation ||
		rc.bufferLevelBits != 1999 || rc.decimationFactor != 1 ||
		rc.decimationCount != 0 {
		t.Fatalf("second drop gate = reason:%d drop:%t buffer:%d factor:%d count:%d, want watermark decimation",
			reason, drop, rc.bufferLevelBits, rc.decimationFactor,
			rc.decimationCount)
	}
}

func TestVP9EncoderSetRealtimeTargetFrameDropMode(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		TargetBitrateKbps:  300,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		DropFrameAllowed:   true,
		DropFrameWaterMark: 75,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{BitrateKbps: 600}); err != nil {
		t.Fatalf("bitrate SetRealtimeTarget: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed {
		t.Fatal("bitrate-only SetRealtimeTarget disabled frame dropping")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropDisabled}); err != nil {
		t.Fatalf("disable FrameDrop: %v", err)
	}
	if e.rc.dropFrameAllowed || e.opts.DropFrameAllowed {
		t.Fatal("FrameDrop disabled did not clear VP9 drop toggle")
	}
	if err := e.SetRealtimeTarget(RealtimeTarget{FrameDrop: RealtimeFrameDropEnabled}); err != nil {
		t.Fatalf("enable FrameDrop: %v", err)
	}
	if !e.rc.dropFrameAllowed || !e.opts.DropFrameAllowed ||
		e.rc.dropFramesWaterMark != 75 {
		t.Fatalf("drop state = allowed:%t opts:%t mark:%d, want true/true/75",
			e.rc.dropFrameAllowed, e.opts.DropFrameAllowed, e.rc.dropFramesWaterMark)
	}
}

func TestVP9EncoderSetRateControlBufferUpdatesBufferModel(t *testing.T) {
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
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.rc.bufferLevelBits = 100000
	if err := e.SetRateControlBuffer(200, 100, 150); err != nil {
		t.Fatalf("SetRateControlBuffer: %v", err)
	}
	if e.opts.BufferSizeMs != 200 || e.opts.BufferInitialSizeMs != 100 ||
		e.opts.BufferOptimalSizeMs != 150 {
		t.Fatalf("buffer opts = %d/%d/%d, want 200/100/150",
			e.opts.BufferSizeMs, e.opts.BufferInitialSizeMs,
			e.opts.BufferOptimalSizeMs)
	}
	if e.rc.bufferSizeBits != 60000 || e.rc.bufferInitialBits != 30000 ||
		e.rc.bufferOptimalBits != 45000 || e.rc.bufferLevelBits != 60000 {
		t.Fatalf("buffer bits = size:%d initial:%d optimal:%d level:%d, want 60000/30000/45000/60000",
			e.rc.bufferSizeBits, e.rc.bufferInitialBits,
			e.rc.bufferOptimalBits, e.rc.bufferLevelBits)
	}

	oldRC := e.rc
	oldOpts := e.opts
	if err := e.SetRateControlBuffer(0, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid SetRateControlBuffer err = %v, want ErrInvalidConfig", err)
	}
	if e.rc != oldRC || !reflect.DeepEqual(e.opts, oldOpts) {
		t.Fatal("invalid SetRateControlBuffer mutated encoder state")
	}
}

func TestVP9EncoderSetRateControlBufferRequiresCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRateControlBuffer(200, 100, 150); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRateControlBuffer without CBR err = %v, want ErrInvalidConfig", err)
	}
}
