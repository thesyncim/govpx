package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9FullRDSearchPartitionFrame2Sub8x8IntraReplayRegression(t *testing.T) {
	const width, height, frames = 64, 64, 3
	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
	}
	enc, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()
	dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}

	dst := make([]byte, 1<<20)
	for i, src := range vp9test.NewPanningSources(width, height, frames) {
		res, err := enc.EncodeIntoWithResult(src, dst)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
		if err := dec.Decode(res.Data); err != nil {
			t.Fatalf("Decode frame %d: %v", i, err)
		}
	}
}
