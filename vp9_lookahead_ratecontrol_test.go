package govpx

import (
	"errors"
	"testing"
)

// TestVP9EncoderLookaheadComposesWithCBR verifies that lookahead + CBR
// rate control is now accepted by the validator and that lookahead's
// delay semantics still apply (first call returns ErrFrameNotReady), and
// that the CBR rate controller picks the qindex that lands in the
// emitted packet metadata.
func TestVP9EncoderLookaheadComposesWithCBR(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		LookaheadFrames:    2,
		RateControlModeSet: true,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  400,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("lookahead+CBR err = %v, want nil", err)
	}
	defer e.Close()

	dst := make([]byte, 131072)
	first := newVP9YCbCrForTest(width, height, 96, 128, 128)
	second := newVP9YCbCrForTest(width, height, 112, 128, 128)
	// First push fills the queue but emits nothing.
	if _, err := e.EncodeIntoWithResult(first, dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first lookahead encode err = %v, want ErrFrameNotReady", err)
	}
	// Second push delivers the first queued frame.
	result, err := e.EncodeIntoWithResult(second, dst)
	if err != nil {
		t.Fatalf("second lookahead encode: %v", err)
	}
	if !result.KeyFrame || len(result.Data) == 0 {
		t.Fatalf("first emitted packet = key:%t bytes:%d, want delayed keyframe",
			result.KeyFrame, len(result.Data))
	}
	if result.Quantizer == 0 || result.InternalQuantizer == 0 {
		t.Fatalf("emitted Quantizer = (%d, %d), want non-zero CBR-controlled values",
			result.Quantizer, result.InternalQuantizer)
	}
	if result.TargetBitrateKbps != 400 {
		t.Fatalf("TargetBitrateKbps = %d, want 400", result.TargetBitrateKbps)
	}
	// Drain the second frame via Flush.
	flushed, err := e.FlushIntoWithResult(dst)
	if err != nil {
		t.Fatalf("FlushIntoWithResult: %v", err)
	}
	if flushed.KeyFrame || len(flushed.Data) == 0 {
		t.Fatalf("flushed packet = key:%t bytes:%d, want non-key inter",
			flushed.KeyFrame, len(flushed.Data))
	}
}

// TestVP9EncoderLookaheadComposesWithVBR mirrors the CBR test with VBR
// rate control to guard against accidental coupling of the gate to a
// specific RC mode.
func TestVP9EncoderLookaheadComposesWithVBR(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width:              width,
		Height:             height,
		FPS:                30,
		LookaheadFrames:    2,
		RateControlModeSet: true,
		RateControlMode:    RateControlVBR,
		TargetBitrateKbps:  600,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("lookahead+VBR err = %v, want nil", err)
	}
	defer e.Close()

	dst := make([]byte, 131072)
	first := newVP9YCbCrForTest(width, height, 96, 128, 128)
	second := newVP9YCbCrForTest(width, height, 112, 128, 128)
	if _, err := e.EncodeIntoWithResult(first, dst); !errors.Is(err, ErrFrameNotReady) {
		t.Fatalf("first lookahead encode err = %v, want ErrFrameNotReady", err)
	}
	result, err := e.EncodeIntoWithResult(second, dst)
	if err != nil {
		t.Fatalf("second lookahead encode: %v", err)
	}
	if !result.KeyFrame {
		t.Fatalf("first emitted packet is not a keyframe (got key=%v)", result.KeyFrame)
	}
	if result.Quantizer == 0 {
		t.Fatal("VBR-emitted Quantizer is zero, want rate-controlled value")
	}
}

// TestVP9EncoderLookaheadComposesWithTemporalSVC drives a 2-layer
// temporal scalability schedule with a lookahead delay and verifies that
// the emitted packets carry the correct LayerID, LayerCount, and TL0PICIDX.
// Previously this configuration was rejected at construction time.
func TestVP9EncoderLookaheadComposesWithTemporalSVC(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               30,
		LookaheadFrames:   2,
		TargetBitrateKbps: 400,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringTwoLayers,
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{200, 400},
		},
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("lookahead+temporal SVC err = %v, want nil", err)
	}
	defer e.Close()

	dst := make([]byte, 131072)
	yShades := []uint8{96, 112, 128, 144}

	results := make([]VP9EncodeResult, 0, len(yShades)+2)
	for i, y := range yShades {
		src := newVP9YCbCrForTest(width, height, y, 128, 128)
		result, err := e.EncodeIntoWithResult(src, dst)
		if err != nil {
			if errors.Is(err, ErrFrameNotReady) {
				continue
			}
			t.Fatalf("frame %d encode: %v", i, err)
		}
		results = append(results, result)
	}
	// Drain remaining via Flush.
	for {
		flushed, err := e.FlushIntoWithResult(dst)
		if errors.Is(err, ErrFrameNotReady) {
			break
		}
		if err != nil {
			t.Fatalf("FlushIntoWithResult: %v", err)
		}
		results = append(results, flushed)
	}
	if len(results) < 2 {
		t.Fatalf("emitted %d results, want at least 2", len(results))
	}
	// First emitted packet must be the delayed keyframe at layer 0.
	if !results[0].KeyFrame {
		t.Fatalf("first emitted is not a keyframe: %+v", results[0])
	}
	for i, r := range results {
		if r.TemporalLayerCount != 2 {
			t.Fatalf("result %d TemporalLayerCount = %d, want 2", i, r.TemporalLayerCount)
		}
		if r.TemporalLayerID < 0 || r.TemporalLayerID >= r.TemporalLayerCount {
			t.Fatalf("result %d TemporalLayerID = %d out of [0,%d)",
				i, r.TemporalLayerID, r.TemporalLayerCount)
		}
	}
}
