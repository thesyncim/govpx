package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

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
