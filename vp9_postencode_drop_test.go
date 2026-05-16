package govpx

import (
	"errors"
	"testing"
)

func TestVP9SetPostEncodeDropRejectsWithoutCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetPostEncodeDrop(true); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetPostEncodeDrop(true) on VBR err = %v, want ErrInvalidConfig",
			err)
	}
}

func TestVP9SetPostEncodeDropAppliesInCBR(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  600,
		DropFrameAllowed:   true,
		DropFrameWaterMark: 50,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetPostEncodeDrop(true); err != nil {
		t.Fatalf("SetPostEncodeDrop: %v", err)
	}
	if !e.opts.PostEncodeDrop || !e.rc.postEncodeDrop {
		t.Fatalf("opts=%v rc=%v, want both true",
			e.opts.PostEncodeDrop, e.rc.postEncodeDrop)
	}
}

func TestVP9EncoderRejectsPostEncodeDropOutsideCBR(t *testing.T) {
	if _, err := NewVP9Encoder(VP9EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
		PostEncodeDrop:     true,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9ShouldPostEncodeDropTriggers(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		postEncodeDrop:      true,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 80,
		bufferOptimalBits:   100000,
		bufferSizeBits:      150000,
		bufferLevelBits:     50000,
		bitsPerFrame:        4000,
		frameTargetBits:     4000,
	}
	overshootBits := rc.frameTargetBits * vp9PostEncodeDropOvershootFactor * 2
	if !rc.shouldPostEncodeDrop(false, true, overshootBits) {
		t.Fatalf("shouldPostEncodeDrop on heavy overshoot = false, want true")
	}
}

func TestVP9ShouldPostEncodeDropSkipsModestOvershoot(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		postEncodeDrop:      true,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 80,
		bufferOptimalBits:   100000,
		bufferSizeBits:      150000,
		bufferLevelBits:     80000,
		bitsPerFrame:        4000,
		frameTargetBits:     4000,
	}
	encoded := rc.frameTargetBits * 2
	if rc.shouldPostEncodeDrop(false, true, encoded) {
		t.Fatalf("shouldPostEncodeDrop on modest overshoot = true, want false")
	}
}

func TestVP9ShouldPostEncodeDropSkipsKeyAndDisabled(t *testing.T) {
	rc := &vp9RateControlState{
		enabled:             true,
		mode:                RateControlCBR,
		postEncodeDrop:      true,
		dropFrameAllowed:    true,
		dropFramesWaterMark: 80,
		bufferOptimalBits:   100000,
		bufferSizeBits:      150000,
		bufferLevelBits:     20000,
		bitsPerFrame:        4000,
		frameTargetBits:     4000,
	}
	encoded := rc.frameTargetBits * vp9PostEncodeDropOvershootFactor * 4
	if rc.shouldPostEncodeDrop(true, true, encoded) {
		t.Fatalf("shouldPostEncodeDrop on key frame = true, want false")
	}
	rc.postEncodeDrop = false
	if rc.shouldPostEncodeDrop(false, true, encoded) {
		t.Fatalf("shouldPostEncodeDrop disabled = true, want false")
	}
}

func TestVP9PostEncodeDropEndToEnd(t *testing.T) {
	// Configure a tight CBR setup so an inter-frame overshoot fires the
	// post-encode drop. The first frame is a key (no drop), the second
	// frame is an inter that overshoots and should be dropped.
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   100,
		BufferSizeMs:        500,
		BufferInitialSizeMs: 200,
		BufferOptimalSizeMs: 250,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  80,
		PostEncodeDrop:      true,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	if !e.rc.postEncodeDrop {
		t.Fatalf("rc.postEncodeDrop = false, want true")
	}
	// Force buffer to be in critical region and overshoot encoded bits.
	e.rc.bufferLevelBits = e.rc.bufferOptimalBits / 4
	encodedBits := e.rc.frameTargetBits * vp9PostEncodeDropOvershootFactor * 4
	if !e.rc.shouldPostEncodeDrop(false, true, encodedBits) {
		t.Fatalf("shouldPostEncodeDrop in critical buffer = false, want true")
	}
	beforeBuffer := e.rc.bufferLevelBits
	e.rc.postEncodeDropFrame()
	if e.rc.bufferLevelBits <= beforeBuffer {
		t.Fatalf("buffer not credited after drop: before=%d after=%d",
			beforeBuffer, e.rc.bufferLevelBits)
	}
}
