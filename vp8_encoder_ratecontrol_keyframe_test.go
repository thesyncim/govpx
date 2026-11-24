package govpx

import (
	"testing"
)

func TestEncodeIntoUsesLibvpxInitialKeyFrameTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.FrameTargetBits != e.rc.bufferInitialBits/2 {
		t.Fatalf("key target = key:%t bits:%d, want initial buffer half %d", key.KeyFrame, key.FrameTargetBits, e.rc.bufferInitialBits/2)
	}
	wantRC := e.rc
	wantRC.beginFrame(false)

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame || inter.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("inter target = key:%t bits:%d, want libvpx CBR buffer target %d", inter.KeyFrame, inter.FrameTargetBits, wantRC.frameTargetBits)
	}
}

func TestEncodeIntoCapsKeyFrameTargetBitsWithMaxIntraBitrate(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxIntraBitratePct:  200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	result, err := e.EncodeInto(make([]byte, 4096), testImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	if result.FrameTargetBits != e.rc.bitsPerFrame*2 {
		t.Fatalf("key target bits = %d, want max intra cap %d", result.FrameTargetBits, e.rc.bitsPerFrame*2)
	}
}

func TestEncodeIntoUsesLibvpxLaterForcedKeyFrameTargetBits(t *testing.T) {
	e := newTestEncoder(t)
	dst := make([]byte, 4096)
	for i := range 20 {
		if _, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, i), uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
	}
	wantRC := e.rc
	wantRC.beginFrameWithTargetAndContext(true, wantRC.bitsPerFrame, rateControlFrameContext{
		forcedKeyFrame:     true,
		temporalLayerCount: 1,
		timing:             e.timing,
	})

	e.ForceKeyFrame()
	result, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 20), 20, 1, 0)
	if err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.FrameTargetBits != wantRC.frameTargetBits {
		t.Fatalf("forced key target = key:%t bits:%d, want %d", result.KeyFrame, result.FrameTargetBits, wantRC.frameTargetBits)
	}
}
