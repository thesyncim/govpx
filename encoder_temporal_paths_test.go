package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
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

func TestEncodeIntoTracksLibvpxTemporalLayerAccounting(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	var sizes [4]int
	layerBuffer := [2]int{288000, 480000}
	layerFrameBandwidth := [2]int{48000, 40000}
	layerMaximumBuffer := [2]int{432000, 720000}
	var layerInput [2]int
	var layerEncoded [2]int
	var layerTotal [2]int
	var layerBits [2]int
	for i := range sizes {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		sizes[i] = encodedSizeBits(result.SizeBytes)
		layerInput[result.TemporalLayerID]++
		for layer := result.TemporalLayerID; layer < result.TemporalLayerCount; layer++ {
			layerTotal[layer]++
			layerBits[layer] += sizes[i]
			layerBuffer[layer] = temporalTestBufferAfterFrame(layerBuffer[layer], layerFrameBandwidth[layer], layerMaximumBuffer[layer], sizes[i])
		}
		if !result.KeyFrame {
			layerEncoded[result.TemporalLayerID]++
		}
		if result.TemporalLayerFrameBandwidthBits != layerFrameBandwidth[result.TemporalLayerID] ||
			result.TemporalLayerMaximumBufferBits != layerMaximumBuffer[result.TemporalLayerID] ||
			result.TemporalLayerBufferLevelBits != layerBuffer[result.TemporalLayerID] {
			t.Fatalf("result[%d] temporal buffer = frame:%d level:%d max:%d, want %d/%d/%d", i, result.TemporalLayerFrameBandwidthBits, result.TemporalLayerBufferLevelBits, result.TemporalLayerMaximumBufferBits, layerFrameBandwidth[result.TemporalLayerID], layerBuffer[result.TemporalLayerID], layerMaximumBuffer[result.TemporalLayerID])
		}
		if result.TemporalLayerInputFrames != layerInput[result.TemporalLayerID] ||
			result.TemporalLayerEncodedFrames != layerEncoded[result.TemporalLayerID] ||
			result.TemporalLayerTotalEncodedFrames != layerTotal[result.TemporalLayerID] ||
			result.TemporalLayerEncodedBits != layerBits[result.TemporalLayerID] {
			t.Fatalf("result[%d] temporal counters = input:%d encoded:%d total:%d bits:%d, want %d/%d/%d/%d", i, result.TemporalLayerInputFrames, result.TemporalLayerEncodedFrames, result.TemporalLayerTotalEncodedFrames, result.TemporalLayerEncodedBits, layerInput[result.TemporalLayerID], layerEncoded[result.TemporalLayerID], layerTotal[result.TemporalLayerID], layerBits[result.TemporalLayerID])
		}
	}

	wantLayer0 := temporalLayerAccounting{
		InputFrames:        2,
		EncodedFrames:      1,
		TotalEncodedFrames: 2,
		EncodedBits:        sizes[0] + sizes[2],
		FrameBandwidthBits: layerFrameBandwidth[0],
		MaximumBufferBits:  layerMaximumBuffer[0],
		BufferLevelBits:    layerBuffer[0],
	}
	wantLayer1 := temporalLayerAccounting{
		InputFrames:        2,
		EncodedFrames:      2,
		TotalEncodedFrames: 4,
		EncodedBits:        sizes[0] + sizes[1] + sizes[2] + sizes[3],
		FrameBandwidthBits: layerFrameBandwidth[1],
		MaximumBufferBits:  layerMaximumBuffer[1],
		BufferLevelBits:    layerBuffer[1],
	}
	if got := e.temporal.accounting[0]; got != wantLayer0 {
		t.Fatalf("layer0 accounting = %+v, want %+v", got, wantLayer0)
	}
	if got := e.temporal.accounting[1]; got != wantLayer1 {
		t.Fatalf("layer1 accounting = %+v, want %+v", got, wantLayer1)
	}

	e.Reset()
	if got := e.temporal.accounting; got != ([MaxTemporalLayers]temporalLayerAccounting{}) {
		t.Fatalf("accounting after reset = %+v, want zero", got)
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

func TestEncodeIntoTracksTemporalLayerBufferOnDroppedFrame(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyBits := encodedSizeBits(key.SizeBytes)
	layer0Buffer := temporalTestBufferAfterFrame(288000, 48000, 432000, keyBits)
	layer1Buffer := temporalTestBufferAfterFrame(480000, 40000, 720000, keyBits)

	e.temporal.codingState[1].BufferLevelBits = -1
	dropped, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("dropped EncodeInto returned error: %v", err)
	}
	if !dropped.Dropped || dropped.TemporalLayerID != 1 {
		t.Fatalf("dropped result = dropped:%t layer:%d, want dropped layer 1", dropped.Dropped, dropped.TemporalLayerID)
	}
	layer1Buffer = temporalTestBufferAfterFrame(layer1Buffer, 40000, 720000, 0)
	if dropped.TemporalLayerFrameBandwidthBits != 40000 || dropped.TemporalLayerMaximumBufferBits != 720000 || dropped.TemporalLayerBufferLevelBits != layer1Buffer {
		t.Fatalf("dropped temporal buffer = frame:%d level:%d max:%d, want 40000/%d/720000", dropped.TemporalLayerFrameBandwidthBits, dropped.TemporalLayerBufferLevelBits, dropped.TemporalLayerMaximumBufferBits, layer1Buffer)
	}
	if dropped.TemporalLayerInputFrames != 1 || dropped.TemporalLayerEncodedFrames != 0 ||
		dropped.TemporalLayerTotalEncodedFrames != 1 || dropped.TemporalLayerEncodedBits != keyBits {
		t.Fatalf("dropped temporal counters = input:%d encoded:%d total:%d bits:%d, want 1/0/1/%d", dropped.TemporalLayerInputFrames, dropped.TemporalLayerEncodedFrames, dropped.TemporalLayerTotalEncodedFrames, dropped.TemporalLayerEncodedBits, keyBits)
	}

	wantLayer0 := temporalLayerAccounting{
		InputFrames:        1,
		TotalEncodedFrames: 1,
		EncodedBits:        keyBits,
		FrameBandwidthBits: 48000,
		MaximumBufferBits:  432000,
		BufferLevelBits:    layer0Buffer,
	}
	wantLayer1 := temporalLayerAccounting{
		InputFrames:        1,
		TotalEncodedFrames: 1,
		EncodedBits:        keyBits,
		FrameBandwidthBits: 40000,
		MaximumBufferBits:  720000,
		BufferLevelBits:    layer1Buffer,
	}
	if got := e.temporal.accounting[0]; got != wantLayer0 {
		t.Fatalf("layer0 accounting after drop = %+v, want %+v", got, wantLayer0)
	}
	if got := e.temporal.accounting[1]; got != wantLayer1 {
		t.Fatalf("layer1 accounting after drop = %+v, want %+v", got, wantLayer1)
	}
}

func TestEncodeIntoInvisibleTemporalFrameUsesLibvpxLayerOverheadAccounting(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	result, err := e.EncodeInto(dst, src, 0, 1, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("invisible temporal EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.TemporalLayerID != 0 {
		t.Fatalf("result = key:%t layer:%d, want invisible base keyframe", result.KeyFrame, result.TemporalLayerID)
	}
	bits := encodedSizeBits(result.SizeBytes)
	wantLayer0Buffer := temporalTestBufferAfterFrame(288000, e.temporal.accounting[0].FrameBandwidthBits, e.temporal.accounting[0].MaximumBufferBits, bits)
	wantLayer1Buffer := temporalTestBufferAfterFrame(480000, e.temporal.accounting[1].FrameBandwidthBits, e.temporal.accounting[1].MaximumBufferBits, bits)

	if result.TemporalLayerBufferLevelBits != wantLayer0Buffer || e.temporal.accounting[0].BufferLevelBits != wantLayer0Buffer {
		t.Fatalf("layer0 invisible buffer = result:%d accounting:%d, want %d", result.TemporalLayerBufferLevelBits, e.temporal.accounting[0].BufferLevelBits, wantLayer0Buffer)
	}
	if e.temporal.accounting[1].BufferLevelBits != wantLayer1Buffer {
		t.Fatalf("layer1 invisible propagated buffer = %d, want %d", e.temporal.accounting[1].BufferLevelBits, wantLayer1Buffer)
	}
}

func temporalTestBufferAfterFrame(level int, frameBandwidth int, maximum int, encodedBits int) int {
	level = saturatingAdd(level, frameBandwidth)
	level = saturatingSub(level, encodedBits)
	if level > maximum {
		return maximum
	}
	return level
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

func TestEncodeIntoRefreshesEntropyUnlessDisabled(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	first := testImage(16, 16)
	second := rateControlTestFrame(16, 16, 1)
	fillImage(first, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !packetState(t, key.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("key refresh entropy = false, want libvpx default true")
	}
	keyData := append([]byte(nil), key.Data...)
	inter, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if !packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("inter refresh entropy = false, want libvpx default true")
	}
	interData := append([]byte(nil), inter.Data...)
	decoded := decodeFrameSequence(t, keyData, interData)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}

	e = newEntropyRefreshTestEncoder(t, false)
	key, err = e.EncodeInto(dst, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("second key EncodeInto returned error: %v", err)
	}
	keyData = append([]byte(nil), key.Data...)
	inter, err = e.EncodeInto(dst, second, 1, 1, EncodeNoUpdateEntropy)
	if err != nil {
		t.Fatalf("no-update-entropy inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("no-update-entropy inter refresh entropy = true, want false")
	}
	interData = append([]byte(nil), inter.Data...)
	decoded = decodeFrameSequence(t, keyData, interData)
	if len(decoded) != 2 {
		t.Fatalf("no-update-entropy decoded frame count = %d, want 2", len(decoded))
	}
}

func TestEncodeIntoForcedKeyHonorsNoUpdateEntropy(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	dst := make([]byte, 8192)

	for i := range 3 {
		src := rateControlTestFrame(16, 16, i)
		if _, err := e.EncodeInto(dst, src, uint64(i), 1, 0); err != nil {
			t.Fatalf("warm frame %d EncodeInto returned error: %v", i, err)
		}
	}
	forced, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 3), 3, 1, EncodeForceKeyFrame|EncodeNoUpdateEntropy)
	if err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if !forced.KeyFrame {
		t.Fatalf("forced KeyFrame = false, want true")
	}
	if packetState(t, forced.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("forced key refresh entropy = true, want libvpx no-update flag honored")
	}
}

func TestEncodeIntoErrorResilientUsesTransientEntropyUpdates(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if keyState.Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient key refresh entropy = true, want libvpx forced false")
	}
	if keyState.Probability.UpdateCount == 0 {
		t.Fatalf("error-resilient key coefficient updates = 0, want transient updates")
	}
	committedKeyProbs := e.coefProbs
	if committedKeyProbs != vp8tables.DefaultCoefProbs {
		t.Fatalf("error-resilient key committed coefficient probabilities, want default snapshot")
	}

	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 2), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient inter refresh entropy = true, want false")
	}
	if e.coefProbs != committedKeyProbs {
		t.Fatalf("error-resilient inter committed transient coefficient probabilities")
	}
}

func TestEncodeIntoErrorResilientPartitionsRefreshesKeyEntropyOnly(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	e.opts.ErrorResilientPartitions = true
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient-partitions key refresh entropy = false, want libvpx forced true")
	}
	committedKeyProbs := e.coefProbs

	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 2), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient-partitions inter refresh entropy = true, want false")
	}
	if e.coefProbs != committedKeyProbs {
		t.Fatalf("error-resilient-partitions inter committed transient coefficient probabilities")
	}
}

func TestCoefficientEntropySavingsUsesIndependentContextWhenErrorResilient(t *testing.T) {
	// The independent-context coefficient entropy-savings path mirrors
	// libvpx's VPX_ERROR_RESILIENT_PARTITIONS branch (bit 0x2). The plain
	// `--error-resilient=1` (DEFAULT, bit 0x1) does NOT enable that branch
	// in libvpx; only the partitions mode does. govpx exposes this as
	// EncoderOptions.ErrorResilientPartitions; the simpler ErrorResilient
	// bool stays on the default coef-savings path so the keyframe coef-prob
	// emission stays byte-equivalent with libvpx's `--error-resilient=1`.
	e := &VP8Encoder{
		opts: EncoderOptions{
			Width:                    16,
			Height:                   16,
			ErrorResilientPartitions: true,
		},
		coefProbs: vp8tables.DefaultCoefProbs,
		interFrameModes: []vp8enc.InterFrameMacroblockMode{{
			RefFrame: vp8common.LastFrame,
			Mode:     vp8common.ZeroMV,
		}},
		keyFrameCoeffs: make([]vp8enc.MacroblockCoefficients, 1),
		tokenAbove:     make([]vp8enc.TokenContextPlanes, 1),
	}
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					e.coefProbs[block][band][ctx][node] = 1
				}
			}
		}
	}
	e.keyFrameCoeffs[0].QCoeff[0][0] = 1
	e.keyFrameCoeffs[0].SetBlockEOB(0, 1)
	got := e.coefficientEntropySavingsBits(false, 1)
	above := make([]vp8enc.TokenContextPlanes, 1)
	want, err := vp8enc.InterCoefficientEntropySavingsIndependent(1, 1, e.interFrameModes, e.keyFrameCoeffs, above, &e.coefProbs)
	if err != nil {
		t.Fatalf("InterCoefficientEntropySavingsIndependent returned error: %v", err)
	}
	if got != want {
		t.Fatalf("error-resilient coefficient entropy savings = %d, want independent-context savings %d", got, want)
	}
	if got == 0 {
		t.Fatalf("error-resilient coefficient entropy savings = 0, want recode accounting to include independent-context branch")
	}
}

func TestEncodeIntoTemporalBaseLayerIsDecodableWithoutEnhancementFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	dst := make([]byte, 8192)
	basePackets := make([][]byte, 0, 3)

	for i := range 6 {
		src := rateControlTestFrame(16, 16, i)
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto %d returned error: %v", i, err)
		}
		if result.TemporalLayerID == 0 {
			basePackets = append(basePackets, append([]byte(nil), result.Data...))
		}
	}
	if len(basePackets) != 3 {
		t.Fatalf("base packet count = %d, want 3", len(basePackets))
	}
	decoded := decodeFrameSequence(t, basePackets...)
	if len(decoded) != len(basePackets) {
		t.Fatalf("decoded base frame count = %d, want %d", len(decoded), len(basePackets))
	}
}

func TestSetTemporalScalabilityControlsNextFrames(t *testing.T) {
	e := newTestEncoder(t)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	plain, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("plain EncodeInto returned error: %v", err)
	}
	if plain.TemporalLayerID != 0 || plain.TemporalLayerCount != 1 {
		t.Fatalf("plain temporal = id:%d count:%d, want 0/1", plain.TemporalLayerID, plain.TemporalLayerCount)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}); err != nil {
		t.Fatalf("SetTemporalScalability returned error: %v", err)
	}

	key, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("temporal key EncodeInto returned error: %v", err)
	}
	enhancement, err := e.EncodeInto(dst, src, 2, 1, 0)
	if err != nil {
		t.Fatalf("temporal enhancement EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.TemporalLayerID != 0 || key.TL0PICIDX != 0 {
		t.Fatalf("first temporal result = key:%t id:%d tl0:%d, want key/0/0", key.KeyFrame, key.TemporalLayerID, key.TL0PICIDX)
	}
	if enhancement.KeyFrame || enhancement.TemporalLayerID != 1 || enhancement.TL0PICIDX != 0 || !enhancement.TemporalLayerSync {
		t.Fatalf("second temporal result = key:%t id:%d tl0:%d sync:%t, want inter/1/0/sync", enhancement.KeyFrame, enhancement.TemporalLayerID, enhancement.TL0PICIDX, enhancement.TemporalLayerSync)
	}
}

func TestSetTemporalScalabilitySeedsNewLayerFilterLevelFromColdContext(t *testing.T) {
	e := newTestEncoder(t)
	e.loopFilterLevel = 9
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}); err != nil {
		t.Fatalf("SetTemporalScalability returned error: %v", err)
	}
	if got := e.temporal.codingState[0].FilterLevel; got != 9 {
		t.Fatalf("base-layer filter seed = %d, want inherited single-layer seed 9", got)
	}
	if got := e.temporal.codingState[1].FilterLevel; got != 0 {
		t.Fatalf("new enhancement-layer filter seed = %d, want cold layer-context seed 0", got)
	}
}

func TestSetTemporalScalabilityOffRestoresBaseLayerCodingState(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	fullBitsPerFrame := e.rc.bitsPerFrame
	fullBufferInitialBits := e.rc.bufferInitialBits
	fullBufferOptimalBits := e.rc.bufferOptimalBits
	fullMaximumBufferBits := e.rc.maximumBufferBits
	fullBufferSizeBits := e.rc.bufferSizeBits
	e.temporal.codingState[0] = temporalLayerCodingState{
		FilterLevel:              7,
		BufferLevelBits:          123,
		BufferInitialBits:        111,
		BufferOptimalBits:        222,
		MaximumBufferBits:        333,
		BitsPerFrame:             444,
		TotalActualBits:          555,
		RateCorrectionFactor:     1.25,
		KeyFrameCorrectionFactor: 1.5,
		GoldenCorrectionFactor:   1.75,
		ActiveBestQuantizer:      9,
		AvgFrameQuantizer:        44,
		LastQuantizer:            45,
		LastInterQuantizer:       46,
		CurrentZbinOverQuant:     11,
		ForceMaxQuantizer:        true,
		LastFramePercentIntra:    12,
		InterFrameTarget:         345,
	}
	e.temporal.codingValid[0] = true
	e.loopFilterLevel = 2
	e.rc.bufferLevelBits = 999
	e.rc.rateCorrectionFactor = 9
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{}); err != nil {
		t.Fatalf("SetTemporalScalability(off) returned error: %v", err)
	}
	if e.temporal.enabled {
		t.Fatalf("temporal enabled after disabling")
	}
	if got := e.loopFilterLevel; got != 7 {
		t.Fatalf("loop filter after disabling = %d, want restored base-layer 7", got)
	}
	if got := e.rc.rateCorrectionFactor; got != 1.25 {
		t.Fatalf("rate correction after disabling = %v, want restored base-layer 1.25", got)
	}
	if got := e.rc.activeBestQuantizer; got != 9 {
		t.Fatalf("active best after disabling = %d, want restored base-layer 9", got)
	}
	if got := e.rc.bufferLevelBits; got != fullBufferInitialBits {
		t.Fatalf("buffer level after disabling = %d, want full-stream initial %d", got, fullBufferInitialBits)
	}
	if e.rc.bitsPerFrame != fullBitsPerFrame ||
		e.rc.bufferInitialBits != fullBufferInitialBits ||
		e.rc.bufferOptimalBits != fullBufferOptimalBits ||
		e.rc.maximumBufferBits != fullMaximumBufferBits ||
		e.rc.bufferSizeBits != fullBufferSizeBits {
		t.Fatalf("full-stream rate geometry was not preserved after disabling")
	}
	if e.rc.currentTemporalLayers != 1 || e.rc.currentTemporalLayerID != 0 || e.currentTemporalLayer != 0 {
		t.Fatalf("current temporal state after disabling = layers:%d layer:%d encoder:%d, want 1/0/0", e.rc.currentTemporalLayers, e.rc.currentTemporalLayerID, e.currentTemporalLayer)
	}
}

func TestSetBitrateKbpsRefreshesTemporalLayerCodingGeometry(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	e.temporal.codingState[0].BufferLevelBits = 111
	e.temporal.codingState[1].BufferLevelBits = 222
	if err := e.SetBitrateKbps(900); err != nil {
		t.Fatalf("SetBitrateKbps returned error: %v", err)
	}
	if got := e.temporal.config.LayerTargetBitrateKbps; got[0] != 540 || got[1] != 900 {
		t.Fatalf("temporal bitrates after SetBitrateKbps = %v, want 540/900", got)
	}
	wantL0Initial := 540 * e.rc.bufferInitialSizeMs
	wantL1Initial := 900 * e.rc.bufferInitialSizeMs
	if got := e.temporal.codingState[0].BufferInitialBits; got != wantL0Initial {
		t.Fatalf("layer 0 initial buffer = %d, want %d", got, wantL0Initial)
	}
	if got := e.temporal.codingState[1].BufferInitialBits; got != wantL1Initial {
		t.Fatalf("layer 1 initial buffer = %d, want %d", got, wantL1Initial)
	}
	if got := e.temporal.codingState[0].BufferLevelBits; got != 111 {
		t.Fatalf("layer 0 live buffer = %d, want preserved 111", got)
	}
	if got := e.temporal.codingState[1].BufferLevelBits; got != 222 {
		t.Fatalf("layer 1 live buffer = %d, want preserved 222", got)
	}
	wantL0BitsPerFrame := computeLayerBitsPerFrame(540*1000, e.timing, e.temporal.pattern.RateDecimator[0], 1)
	wantL1BitsPerFrame := computeLayerBitsPerFrame(900*1000, e.timing, e.temporal.pattern.RateDecimator[1], 1)
	if got := e.temporal.codingState[0].BitsPerFrame; got != wantL0BitsPerFrame {
		t.Fatalf("layer 0 bits per frame = %d, want %d", got, wantL0BitsPerFrame)
	}
	if got := e.temporal.codingState[1].BitsPerFrame; got != wantL1BitsPerFrame {
		t.Fatalf("layer 1 bits per frame = %d, want %d", got, wantL1BitsPerFrame)
	}
}

func TestSetTemporalLayerIDOverridesNextFrames(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !key.KeyFrame || key.TemporalLayerID != 0 || key.TL0PICIDX != 0 {
		t.Fatalf("key temporal = key:%t id:%d tl0:%d, want key/0/0", key.KeyFrame, key.TemporalLayerID, key.TL0PICIDX)
	}
	if err := e.SetTemporalLayerID(0); err != nil {
		t.Fatalf("SetTemporalLayerID returned error: %v", err)
	}
	base, err := e.EncodeInto(dst, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("manual base EncodeInto returned error: %v", err)
	}
	if base.TemporalLayerID != 0 || base.TL0PICIDX != 1 || base.TemporalLayerTargetBitrateKbps != 720 || base.TemporalLayerCumulativeBitrateKbps != 720 {
		t.Fatalf("manual base temporal = id:%d tl0:%d target:%d cumulative:%d, want 0/1/720/720", base.TemporalLayerID, base.TL0PICIDX, base.TemporalLayerTargetBitrateKbps, base.TemporalLayerCumulativeBitrateKbps)
	}
	if err := e.SetTemporalLayerID(1); err != nil {
		t.Fatalf("SetTemporalLayerID enhancement returned error: %v", err)
	}
	enhancement, err := e.EncodeInto(dst, src, 2, 1, 0)
	if err != nil {
		t.Fatalf("manual enhancement EncodeInto returned error: %v", err)
	}
	if enhancement.TemporalLayerID != 1 || enhancement.TL0PICIDX != 1 || enhancement.TemporalLayerTargetBitrateKbps != 480 || enhancement.TemporalLayerCumulativeBitrateKbps != 1200 {
		t.Fatalf("manual enhancement temporal = id:%d tl0:%d target:%d cumulative:%d, want 1/1/480/1200", enhancement.TemporalLayerID, enhancement.TL0PICIDX, enhancement.TemporalLayerTargetBitrateKbps, enhancement.TemporalLayerCumulativeBitrateKbps)
	}
}

func TestSetTemporalLayerIDValidation(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetTemporalLayerID(0); err != nil {
		t.Fatalf("SetTemporalLayerID one-layer returned error: %v", err)
	}
	if err := e.SetTemporalLayerID(1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID one-layer high error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTemporalScalability(TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers}); err != nil {
		t.Fatalf("SetTemporalScalability returned error: %v", err)
	}
	if err := e.SetTemporalLayerID(-1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID negative error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetTemporalLayerID(2); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetTemporalLayerID two-layer high error = %v, want ErrInvalidConfig", err)
	}
}

func TestEncodeIntoInterFrameCanSkipGoldenAndAltRefRefresh(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)

	inter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	decoded := decodeFrameSequence(t, key.Data, inter.Data)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}
	assertImagesEqual(t, "current", decoded[1], publicImageFromVP8(&e.current.Img))
	assertImagesEqual(t, "last", decoded[1], publicImageFromVP8(&e.lastRef.Img))
	assertImagesEqual(t, "golden", keyFrame, publicImageFromVP8(&e.goldenRef.Img))
	assertImagesEqual(t, "alt", keyFrame, publicImageFromVP8(&e.altRef.Img))
}

func TestEncodeIntoNoReferenceLastCanUseGoldenReference(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}
	keyFrame := decodeSingleFrame(t, key.Data)
	interPacket := make([]byte, 4096)
	secondInter, err := e.EncodeInto(interPacket, second, 1, 1, EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}

	thirdPacket := make([]byte, 4096)
	result, err := e.EncodeInto(thirdPacket, keyFrame, 2, 1, EncodeNoReferenceLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using golden when last reference is disallowed")
	}
	if e.interFrameModes[0].RefFrame != vp8common.GoldenFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped GOLDEN/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, secondInter.Data, result.Data)
	if len(decoded) != 3 {
		t.Fatalf("decoded frame count = %d, want 3", len(decoded))
	}
	assertImagesEqual(t, "golden interframe", keyFrame, decoded[2])
}

func TestEncodeIntoNoReferenceLastOrGoldenCanUseAltRef(t *testing.T) {
	e := newTestEncoder(t)
	if err := e.SetDeadline(DeadlineBestQuality); err != nil {
		t.Fatalf("SetDeadline returned error: %v", err)
	}
	keySrc := testImage(16, 16)
	altSrc := testImage(16, 16)
	fillImage(keySrc, 220, 90, 170)
	fillImage(altSrc, 40, 91, 171)
	keyPacket := make([]byte, 4096)
	key, err := e.EncodeInto(keyPacket, keySrc, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	interPacket := make([]byte, 4096)
	altInter, err := e.EncodeInto(interPacket, altSrc, 1, 1, EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("alt refresh EncodeInto returned error: %v", err)
	}
	altState := packetState(t, altInter.Data)
	if altState.Refresh.RefreshLast || altState.Refresh.RefreshGolden || !altState.Refresh.RefreshAltRef {
		t.Fatalf("alt refresh flags = %+v, want alt-only refresh", altState.Refresh)
	}
	altData := append([]byte(nil), altInter.Data...)
	altDecoded := decodeFrameSequence(t, key.Data, altData)
	if len(altDecoded) != 2 {
		t.Fatalf("alt refresh decoded frame count = %d, want 2", len(altDecoded))
	}
	altFrame := altDecoded[1]

	result, err := e.EncodeInto(interPacket, altFrame, 2, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want interframe using altref")
	}
	if e.interFrameModes[0].RefFrame != vp8common.AltRefFrame || e.interFrameModes[0].Mode != vp8common.ZeroMV || !e.interFrameModes[0].MBSkipCoeff {
		t.Fatalf("mode[0] = %+v, want skipped ALTREF/ZEROMV", e.interFrameModes[0])
	}
	decoded := decodeFrameSequence(t, key.Data, altData, result.Data)
	if len(decoded) != 3 {
		t.Fatalf("decoded frame count = %d, want 3", len(decoded))
	}
	assertImagesEqual(t, "altref interframe", altFrame, decoded[2])
}

func TestEncodeIntoNoReferencesStaysInterFrame(t *testing.T) {
	e := newTestEncoder(t)
	first := testImage(16, 16)
	second := testImage(16, 16)
	fillImage(first, 220, 90, 170)
	fillImage(second, 40, 90, 170)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, first, 0, 1, 0); err != nil {
		t.Fatalf("first EncodeInto returned error: %v", err)
	}

	result, err := e.EncodeInto(dst, second, 1, 1, EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if result.KeyFrame {
		t.Fatalf("KeyFrame = true, want libvpx-compatible inter frame with intra macroblocks when all references are disallowed")
	}
}

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
		t.Fatalf("default result = key:%t sceneCut:%t, want legacy interframe", result.KeyFrame, result.SceneCut)
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

func TestEncodeIntoLookaheadBuffersAndFlushes(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		KeyFrameInterval:    120,
		LookaheadFrames:     2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	first := testImage(16, 16)
	second := testImage(16, 16)
	third := testImage(16, 16)
	fillImage(first, 30, 90, 170)
	fillImage(second, 50, 90, 170)
	fillImage(third, 70, 90, 170)

	if _, err := e.EncodeInto(dst, first, 10, 1, 0); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first EncodeInto error = %v, want ErrFrameNotReady", err)
	}
	result, err := e.EncodeInto(dst, second, 11, 1, 0)
	if err != nil {
		t.Fatalf("second EncodeInto returned error: %v", err)
	}
	if !result.KeyFrame || result.PTS != 10 || result.LookaheadDepth != 1 {
		t.Fatalf("second result = key:%t pts:%d depth:%d, want first queued keyframe with depth 1", result.KeyFrame, result.PTS, result.LookaheadDepth)
	}
	result, err = e.EncodeInto(dst, third, 12, 1, 0)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	if result.PTS != 11 || result.LookaheadDepth != 1 {
		t.Fatalf("third result pts/depth = %d/%d, want second queued frame/depth 1", result.PTS, result.LookaheadDepth)
	}
	result, err = e.FlushInto(dst)
	if err != nil {
		t.Fatalf("FlushInto returned error: %v", err)
	}
	if result.PTS != 12 || result.LookaheadDepth != 0 {
		t.Fatalf("flush result pts/depth = %d/%d, want final queued frame/depth 0", result.PTS, result.LookaheadDepth)
	}
	if _, err := e.FlushInto(dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("empty FlushInto error = %v, want ErrFrameNotReady", err)
	}
}

func TestEncodeIntoARNRAndSpatialDenoiserReportPreprocessing(t *testing.T) {
	// libvpx vp8_temporal_filter_prepare_c only fires for the hidden alt-ref
	// source (gated on `cpi->source_alt_ref_pending`). govpx mirrors that by
	// running ARNR only when the encode flags carry the hidden-ARF combo
	// (EncodeForceAltRefFrame|EncodeInvisibleFrame). Drive the encoder with
	// AutoAltRef=true and a synthetic two-pass stats section so the auto-ARF
	// driver schedules a hidden frame on the libvpx-faithful path
	// (calc_pframe_target_size clears source_alt_ref_pending on every
	// one-pass frame, so ARF only ever schedules in two-pass mode); on that
	// hidden frame both ARNR and the spatial denoiser report having run.
	// Use backward ARNR here: at the hidden ARF point this fixture has
	// adjacent prior lookahead frames available, while forward/centered
	// ARNR may legitimately prepare a center-only window and skip filtering.
	stats := make([]FirstPassFrameStats, 32)
	for i := range stats {
		stats[i] = FirstPassFrameStats{
			IntraError:    20000,
			CodedError:    200,
			PcntInter:     0.95,
			PcntMotion:    0.4,
			PcntSecondRef: 0.0,
			PcntNeutral:   0.0,
			MVrAbs:        5,
			MVcAbs:        5,
			Count:         1,
			Duration:      1,
		}
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  120,
		LookaheadFrames:   8,
		AutoAltRef:        true,
		ARNRMaxFrames:     3,
		ARNRStrength:      6,
		ARNRType:          1,
		NoiseSensitivity:  2,
		TwoPassStats:      FinalizeFirstPassStats(stats),
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	noisy := testImage(16, 16)
	for i := range noisy.Y {
		if i%2 == 0 {
			noisy.Y[i] = 40
		} else {
			noisy.Y[i] = 60
		}
	}
	clean := testImage(16, 16)
	fillImage(clean, 50, 90, 170)
	const totalFrames = 12
	frames := make([]Image, totalFrames)
	for i := range frames {
		if i == 0 {
			frames[i] = noisy
		} else {
			frames[i] = clean
		}
	}
	var sawARNR bool
	for i, src := range frames {
		result, err := e.EncodeInto(dst, src, uint64(i), 1, 0)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
		if result.ARNRFiltered {
			if !result.Denoised {
				t.Fatalf("frame %d arnr=true but denoised=false", i)
			}
			sawARNR = true
			break
		}
	}
	if !sawARNR {
		// Drain the lookahead so the hidden ARF can fire on flush.
		for {
			result, err := e.FlushInto(dst)
			if err != nil {
				if errors.Is(err, ErrFrameNotReady) {
					break
				}
				t.Fatalf("FlushInto returned error: %v", err)
			}
			if result.ARNRFiltered {
				if !result.Denoised {
					t.Fatalf("flush arnr=true but denoised=false")
				}
				sawARNR = true
				break
			}
		}
	}
	if !sawARNR {
		t.Fatalf("no encoded frame reported ARNR filtering: auto-ARF driver did not emit a hidden ARF on the configured fixture")
	}
}
