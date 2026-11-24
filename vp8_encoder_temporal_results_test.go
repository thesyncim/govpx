package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

func TestEncodeIntoAppliesTemporalScalabilityMode1(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	var results [4]EncodeResult
	for i := range results {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		results[i] = result
		results[i].Data = append([]byte(nil), result.Data...)
	}

	wantLayerID := []int{0, 1, 0, 1}
	wantTL0 := []uint8{0, 0, 1, 1}
	wantLayerSync := []bool{false, true, false, false}
	// Frame[0] is the initial key frame, so temporal mode uses the base
	// layer's starting buffer / 2: 720 kbps * 400 ms / 2 = 144000 bits.
	// The following inter targets include libvpx-style per-layer buffer
	// adjustment after the key-frame drain.
	wantTargetBits := []int{144000, 29920, 46560, 32000}
	wantLayerBitrate := []int{720, 480, 720, 480}
	wantCumulativeBitrate := []int{720, 1200, 720, 1200}
	for i := range results {
		if results[i].TemporalLayerID != wantLayerID[i] ||
			results[i].TemporalLayerCount != 2 ||
			results[i].TL0PICIDX != wantTL0[i] ||
			results[i].TemporalLayerSync != wantLayerSync[i] ||
			results[i].FrameTargetBits != wantTargetBits[i] ||
			results[i].TemporalLayerTargetBitrateKbps != wantLayerBitrate[i] ||
			results[i].TemporalLayerCumulativeBitrateKbps != wantCumulativeBitrate[i] {
			t.Fatalf("result[%d] temporal = id:%d count:%d tl0:%d sync:%t target:%d layerKbps:%d cumulativeKbps:%d, want %d/2/%d/%t/%d/%d/%d", i, results[i].TemporalLayerID, results[i].TemporalLayerCount, results[i].TL0PICIDX, results[i].TemporalLayerSync, results[i].FrameTargetBits, results[i].TemporalLayerTargetBitrateKbps, results[i].TemporalLayerCumulativeBitrateKbps, wantLayerID[i], wantTL0[i], wantLayerSync[i], wantTargetBits[i], wantLayerBitrate[i], wantCumulativeBitrate[i])
		}
	}
	if !results[0].KeyFrame || results[1].KeyFrame || results[2].KeyFrame || results[3].KeyFrame {
		t.Fatalf("keyframe flags = %t/%t/%t/%t, want only first keyframe", results[0].KeyFrame, results[1].KeyFrame, results[2].KeyFrame, results[3].KeyFrame)
	}

	enhancement := packetState(t, results[1].Data)
	if enhancement.Refresh.RefreshLast || !enhancement.Refresh.RefreshGolden || enhancement.Refresh.RefreshAltRef {
		t.Fatalf("enhancement refresh = %+v, want golden-only refresh", enhancement.Refresh)
	}
	base := packetState(t, results[2].Data)
	if !base.Refresh.RefreshLast || base.Refresh.RefreshGolden || base.Refresh.RefreshAltRef {
		t.Fatalf("base refresh = %+v, want last-only refresh", base.Refresh)
	}
	secondEnhancement := packetState(t, results[3].Data)
	if secondEnhancement.Refresh.RefreshLast || !secondEnhancement.Refresh.RefreshGolden || secondEnhancement.Refresh.RefreshAltRef {
		t.Fatalf("second enhancement refresh = %+v, want golden-only refresh", secondEnhancement.Refresh)
	}
}

func TestTemporalFiveLayerNoRefOnlyUsesDefaultLastRefresh(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               64,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   700,
		MinQuantizer:        2,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
		KeyFrameInterval:    3000,
		StaticThreshold:     1,
		MaxIntraBitratePct:  1000,
		UndershootPct:       50,
		OvershootPct:        50,
		BufferSizeMs:        1000,
		BufferInitialSizeMs: 600,
		BufferOptimalSizeMs: 600,
		TokenPartitions:     int(vp8common.TwoPartition),
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringFiveLayers,
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{100, 220, 360, 520, 700},
		},
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 1<<20)
	var frame8 EncodeResult
	for frame := 0; frame <= 8; frame++ {
		result, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, frame), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", frame, err)
		}
		if result.Dropped {
			t.Fatalf("EncodeInto %d dropped, want emitted temporal frame", frame)
		}
		if frame == 8 {
			frame8 = result
		}
	}
	if frame8.KeyFrame || frame8.TemporalLayerID != 1 {
		t.Fatalf("frame 8 temporal = key:%t layer:%d, want inter layer 1", frame8.KeyFrame, frame8.TemporalLayerID)
	}
	state := packetState(t, frame8.Data)
	if !state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("frame 8 refresh = %+v, want LAST-only default refresh", state.Refresh)
	}
}

func TestEncodeIntoTemporalOneLayerKeepsDefaultInterRefresh(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	inter, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.TemporalLayerID != 0 || inter.TemporalLayerCount != 1 {
		t.Fatalf("temporal = id:%d count:%d, want 0/1", inter.TemporalLayerID, inter.TemporalLayerCount)
	}
	state := packetState(t, inter.Data)
	if !state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef {
		t.Fatalf("one-layer refresh = %+v, want last-only default refresh", state.Refresh)
	}
}

func TestEncodeIntoTemporalPacketRefreshFlagsMatchLibvpxPatterns(t *testing.T) {
	tests := []struct {
		name   string
		cfg    TemporalScalabilityConfig
		frames int
	}{
		{name: "one-layer", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringOneLayer}, frames: 4},
		{name: "two-layers", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}, frames: 4},
		{name: "two-layers-three-frame", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayersThreeFrame}, frames: 5},
		{name: "three-layers-six-frame", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersSixFrame}, frames: 7},
		{name: "three-layers-no-inter-layer-prediction", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersNoInterLayerPrediction}, frames: 5},
		{name: "three-layers-layer-one-prediction", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersLayerOnePrediction}, frames: 5},
		{name: "three-layers", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayers}, frames: 5},
		{name: "five-layers", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringFiveLayers, LayerTargetBitrateKbps: [MaxTemporalLayers]int{200, 400, 700, 950, 1200}}, frames: 8},
		{name: "two-layers-with-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayersWithSync}, frames: 9},
		{name: "three-layers-with-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersWithSync}, frames: 9},
		{name: "three-layers-altref-with-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersAltRefWithSync}, frames: 9},
		{name: "three-layers-one-reference", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersOneReference}, frames: 5},
		{name: "three-layers-no-sync", cfg: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersNoSync}, frames: 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTemporalRefreshFlagTestEncoder(t, tt.cfg)
			pattern, ok := temporalLayeringPattern(tt.cfg.Mode)
			if !ok {
				t.Fatalf("temporalLayeringPattern returned false")
			}
			dst := make([]byte, 8192)
			for frame := 0; frame < tt.frames; frame++ {
				result, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, frame), uint64(frame), 1, 0)
				if err != nil {
					t.Fatalf("EncodeInto %d returned error: %v", frame, err)
				}
				if result.KeyFrame {
					continue
				}
				flags := pattern.Flags[frame%pattern.FlagPeriodicity]
				if tt.cfg.Mode != TemporalLayeringFiveLayers && frame > 0 && frame%pattern.FlagPeriodicity == 0 {
					flags &^= EncodeForceKeyFrame
				}
				state := packetState(t, result.Data)
				wantLast := flags&EncodeNoUpdateLast == 0
				wantGolden := pattern.Layers > 1 && flags&EncodeNoUpdateGolden == 0
				wantAltRef := pattern.Layers > 1 && flags&EncodeNoUpdateAltRef == 0
				wantEntropy := flags&EncodeNoUpdateEntropy == 0
				if state.Refresh.RefreshLast != wantLast || state.Refresh.RefreshGolden != wantGolden || state.Refresh.RefreshAltRef != wantAltRef || state.Refresh.RefreshEntropyProbs != wantEntropy {
					t.Fatalf("frame %d refresh = %+v, want last:%t golden:%t alt:%t entropy:%t", frame, state.Refresh, wantLast, wantGolden, wantAltRef, wantEntropy)
				}
			}
		})
	}
}

func TestEncodeIntoReportsLibvpxTemporalDroppableFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayersWithSync})
	src := testImage(16, 16)
	fillImage(src, 96, 120, 150)
	dst := make([]byte, 4096)

	results := make([]EncodeResult, 8)
	for i := range results {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		results[i] = result
		results[i].Data = append([]byte(nil), result.Data...)
	}

	for i, result := range results[:7] {
		if result.Droppable {
			t.Fatalf("result[%d].Droppable = true, want false for reference/entropy-updating temporal frame", i)
		}
	}
	if !results[7].Droppable {
		t.Fatalf("result[7].Droppable = false, want libvpx droppable temporal frame")
	}
	state := packetState(t, results[7].Data)
	if state.Refresh.RefreshLast || state.Refresh.RefreshGolden || state.Refresh.RefreshAltRef || state.Refresh.RefreshEntropyProbs {
		t.Fatalf("droppable refresh = %+v, want no reference or entropy refresh", state.Refresh)
	}
}
