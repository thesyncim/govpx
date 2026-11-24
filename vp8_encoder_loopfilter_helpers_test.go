package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"testing"
)

func TestEncoderLoopFilterUsesPreviousInterLevelWithLibvpxClamp(t *testing.T) {
	e := &VP8Encoder{
		opts:            EncoderOptions{Sharpness: 3},
		rc:              rateControlState{currentQuantizer: 40},
		loopFilterLevel: 13,
	}
	level, sharpness := e.encoderLoopFilter(vp8common.InterFrame)
	if level != 13 || sharpness != 3 {
		t.Fatalf("inter loop filter = level:%d sharpness:%d, want previous 13 sharpness 3", level, sharpness)
	}

	e.loopFilterLevel = 0
	level, _ = e.encoderLoopFilter(vp8common.InterFrame)
	if level != 5 {
		t.Fatalf("clamped inter loop filter level = %d, want libvpx min q/8 = 5", level)
	}

	level, sharpness = e.encoderLoopFilter(vp8common.KeyFrame)
	if level != 15 || sharpness != 0 {
		t.Fatalf("key loop filter = level:%d sharpness:%d, want q*3/8=15 sharpness 0", level, sharpness)
	}
}

func TestEncodeIntoRealtimeHighSpeedWritesSimpleLoopFilter(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		TargetBitrateKbps: 300,
		MinQuantizer:      20,
		MaxQuantizer:      20,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -14,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}

	keySource := testImage(32, 32)
	fillImage(keySource, 80, 128, 128)
	key, err := e.EncodeInto(make([]byte, 4096), keySource, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if keyState.LoopFilter.Type != vp8dec.SimpleLoopFilter {
		t.Fatalf("key loop filter type = %d, want simple", keyState.LoopFilter.Type)
	}

	interSource := testImage(32, 32)
	fillImage(interSource, 82, 128, 128)
	inter, err := e.EncodeInto(make([]byte, 4096), interSource, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.Dropped {
		t.Fatalf("inter frame dropped, want encoded interframe")
	}
	interState := packetState(t, inter.Data)
	if interState.LoopFilter.Type != vp8dec.SimpleLoopFilter {
		t.Fatalf("inter loop filter type = %d, want simple", interState.LoopFilter.Type)
	}
}
