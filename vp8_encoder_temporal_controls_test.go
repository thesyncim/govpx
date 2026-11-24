package govpx

import (
	"errors"
	"testing"
)

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
