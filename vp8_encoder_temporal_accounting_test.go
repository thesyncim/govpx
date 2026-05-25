package govpx

import (
	"github.com/thesyncim/govpx/internal/vpx/arith"
	vpxrc "github.com/thesyncim/govpx/internal/vpx/ratecontrol"
	"testing"
)

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
		sizes[i] = vpxrc.EncodedSizeBits(result.SizeBytes)
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

func TestEncodeIntoTracksTemporalLayerBufferOnDroppedFrame(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 4096)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyBits := vpxrc.EncodedSizeBits(key.SizeBytes)
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
	bits := vpxrc.EncodedSizeBits(result.SizeBytes)
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
	level = arith.SaturatingAdd(level, frameBandwidth)
	level = arith.SaturatingSub(level, encodedBits)
	if level > maximum {
		return maximum
	}
	return level
}
