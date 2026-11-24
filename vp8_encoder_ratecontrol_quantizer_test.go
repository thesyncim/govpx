package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

func TestEncodeIntoCQLevelSelectsQuantizer(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 4096)
	// libvpx vp8/encoder/onyx_if.c lines 3727-3739: in CQ mode the
	// cq_target_quality floor only applies to inter non-refresh frames;
	// keyframes/golden/altref stay at best_quality. So the keyframe Q is
	// the regulator-picked value (which for a 16x16 fixture with a high
	// target bitrate sits at minQuantizer), not cqLevel.
	key, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if key.Quantizer >= 32 {
		t.Fatalf("key quantizer = %d, want below CQ level 32 (libvpx allows KF below cq_target_quality)", key.Quantizer)
	}
	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Quantizer != 32 || packetBaseQIndex(t, inter.Data) != vp8common.PublicQuantizerToQIndex(32) {
		t.Fatalf("inter quantizer = result:%d packet:%d, want public CQ level 32 / qindex %d", inter.Quantizer, packetBaseQIndex(t, inter.Data), vp8common.PublicQuantizerToQIndex(32))
	}
}

func TestEncodeIntoWritesLibvpxFrameQuantDeltas(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        0,
		MaxQuantizer:        1,
		CQLevel:             0,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 4096)
	key, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyQuant := packetState(t, key.Data).Quant
	if keyQuant.BaseQIndex != 0 || keyQuant.Y2DCDelta != 4 {
		t.Fatalf("key quant = %+v, want base Q 0 with Y2 DC delta 4", keyQuant)
	}

	screen, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             56,
		ScreenContentMode:   1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("screen NewVP8Encoder returned error: %v", err)
	}
	if _, err := screen.EncodeInto(dst, rateControlTestFrame(16, 16, 0), 0, 1, 0); err != nil {
		t.Fatalf("screen key EncodeInto returned error: %v", err)
	}
	inter, err := screen.EncodeInto(dst, rateControlTestFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interQuant := packetState(t, inter.Data).Quant
	wantUVDelta := int8(-15)
	if interQuant.BaseQIndex != uint8(vp8common.PublicQuantizerToQIndex(56)) || interQuant.UVDCDelta != wantUVDelta || interQuant.UVACDelta != wantUVDelta {
		t.Fatalf("inter quant = %+v, want screen-content UV deltas %d at qindex %d", interQuant, wantUVDelta, vp8common.PublicQuantizerToQIndex(56))
	}
}

func TestEncodeIntoCQDefaultLevelMirrorsLibvpx(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	if e.opts.CQLevel != defaultCQLevel || e.rc.currentQuantizer != vp8common.PublicQuantizerToQIndex(defaultCQLevel) {
		t.Fatalf("default CQ = opts:%d q:%d, want public %d / qindex %d", e.opts.CQLevel, e.rc.currentQuantizer, defaultCQLevel, vp8common.PublicQuantizerToQIndex(defaultCQLevel))
	}
}

func TestEncodeIntoCQOutputBitrateAdaptsToContent(t *testing.T) {
	newCQEncoder := func(t *testing.T) *VP8Encoder {
		t.Helper()
		e, err := NewVP8Encoder(EncoderOptions{
			Width:               32,
			Height:              32,
			FPS:                 30,
			RateControlMode:     RateControlCQ,
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
		return e
	}
	flat := testImage(32, 32)
	fillImage(flat, 90, 90, 170)
	detailed := rateControlTestFrame(32, 32, 3)
	dst := make([]byte, 16384)

	flatResult, err := newCQEncoder(t).EncodeInto(dst, flat, 0, 1, 0)
	if err != nil {
		t.Fatalf("flat EncodeInto returned error: %v", err)
	}
	detailedResult, err := newCQEncoder(t).EncodeInto(dst, detailed, 0, 1, 0)
	if err != nil {
		t.Fatalf("detailed EncodeInto returned error: %v", err)
	}
	if detailedResult.SizeBytes <= flatResult.SizeBytes {
		t.Fatalf("CQ sizes = detailed:%d flat:%d, want detailed content to use more bits", detailedResult.SizeBytes, flatResult.SizeBytes)
	}
}

func TestEncodeIntoWritesConfiguredTokenPartitions(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		TokenPartitions:     int(vp8common.EightPartition),
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if got := packetTokenPartition(t, key.Data); got != vp8common.EightPartition {
		t.Fatalf("key token partition = %d, want eight", got)
	}

	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want inter frame")
	}
	if got := packetTokenPartition(t, inter.Data); got != vp8common.EightPartition {
		t.Fatalf("inter token partition = %d, want eight", got)
	}
}
