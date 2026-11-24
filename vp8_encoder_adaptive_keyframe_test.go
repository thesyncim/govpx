package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestEncodeIntoAdaptiveKeyFramesFollowsLibvpxRealtimeSpeedGate(t *testing.T) {
	e := newAdaptiveSceneCutTestEncoder(t, true)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 20, 90, 170)
	fillImage(second, 230, 90, 170)
	dst := make([]byte, 8192)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame || result.SceneCut {
		t.Fatalf("adaptive result = key:%t sceneCut:%t, want libvpx realtime-speed interframe", result.KeyFrame, result.SceneCut)
	}
	info, err := PeekVP8StreamInfo(result.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if info.KeyFrame {
		t.Fatalf("packet KeyFrame = true, want interframe packet")
	}
	if oracleTraceBuild && e.oracleTraceMBBufferLenForTest() != 0 {
		t.Fatalf("discarded inter-attempt MB trace rows = %d, want 0", e.oracleTraceMBBufferLenForTest())
	}
}

func TestEncodeIntoAdaptiveKeyFramesRecodeUsesLibvpxDecideKeyFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             80,
		Height:            80,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
		KeyFrameInterval:  120,
		AdaptiveKeyFrames: true,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	first := testImage(80, 80)
	second := testImage(80, 80)
	fillImage(first, 0, 90, 170)
	fillImage(second, 128, 90, 170)
	dst := make([]byte, 65536)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	e.lastFramePercentIntra = 0

	result, err := e.EncodeInto(dst, second, 1, 1, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || !result.SceneCut {
		intra, last, golden, alt := countInterFrameRefUsage(e.interFrameModes)
		t.Fatalf("auto-key recode result = key:%t scene:%t refs:%d/%d/%d/%d, want key scene-cut",
			result.KeyFrame, result.SceneCut, intra, last, golden, alt)
	}
	info, err := PeekVP8StreamInfo(result.Data)
	if err != nil {
		t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
	}
	if !info.KeyFrame {
		t.Fatalf("packet KeyFrame = false, want keyframe packet")
	}
}

func TestEncodeIntoAdaptiveKeyFramesDisabledByDefault(t *testing.T) {
	e := newAdaptiveSceneCutTestEncoder(t, false)
	first := testImage(32, 32)
	second := testImage(32, 32)
	fillImage(first, 20, 90, 170)
	fillImage(second, 230, 90, 170)
	dst := make([]byte, 8192)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame || result.SceneCut {
		t.Fatalf("default result = key:%t sceneCut:%t, want inter frame", result.KeyFrame, result.SceneCut)
	}
}

func TestShouldRecodeInterAttemptAsKeyFrameMirrorsLibvpxGate(t *testing.T) {
	e := &VP8Encoder{
		opts:                  EncoderOptions{AdaptiveKeyFrames: true, Deadline: DeadlineGoodQuality},
		lastFramePercentIntra: 20,
		interFrameModes: []vp8enc.InterFrameMacroblockMode{
			{Mode: vp8common.DCPred},
			{Mode: vp8common.DCPred},
			{Mode: vp8common.DCPred},
			{Mode: vp8common.ZeroMV, RefFrame: vp8common.LastFrame},
		},
	}
	if pct, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, false, false, false); pct != 75 || !ok {
		t.Fatalf("auto-key recode = pct:%d ok:%t, want pct75 true", pct, ok)
	}
	if pct, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, true, false, false); pct != 75 || ok {
		t.Fatalf("golden-refresh auto-key recode = pct:%d ok:%t, want pct75 false", pct, ok)
	}

	e.interFrameModes[3] = vp8enc.InterFrameMacroblockMode{Mode: vp8common.DCPred}
	if pct, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, true, false, false); pct != 100 || !ok {
		t.Fatalf("unconditional auto-key recode = pct:%d ok:%t, want pct100 true", pct, ok)
	}

	e.opts.Deadline = DeadlineRealtime
	if _, ok := e.shouldRecodeInterAttemptAsKeyFrame(4, false, false, false); ok {
		t.Fatalf("realtime auto-key recode = true, want false for compressor_speed 2")
	}
}
