package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestSetRateControlPinsLibvpxCyclicRefreshMode(t *testing.T) {
	cbr, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder(CBR): %v", err)
	}
	defer cbr.Close()
	if !cbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("CBR-born encoder cyclic refresh disabled, want enabled")
	}
	if err := cbr.SetRateControl(RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   cbr.rc.targetBitrateKbps,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("CBR-born SetRateControl(VBR): %v", err)
	}
	if !cbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("CBR-born runtime VBR cyclic refresh disabled, want libvpx-pinned at construction")
	}

	vbr, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder(VBR): %v", err)
	}
	defer vbr.Close()
	if vbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("VBR-born encoder cyclic refresh enabled, want disabled")
	}
	if err := vbr.SetRateControl(RateControlConfig{
		Mode:                RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("VBR-born SetRateControl(CBR): %v", err)
	}
	if vbr.cyclicRefreshModeEnabled(false) {
		t.Fatalf("VBR-born runtime CBR cyclic refresh enabled, want libvpx-pinned at construction")
	}
}

// TestSetRateControlVBRKeepsLibvpxCyclicRefreshHeader asserts the
// segmentation header carries through a CBR→VBR runtime transition on a
// CBR-born encoder. libvpx pins cyclic_refresh_mode_enabled at compressor
// creation, so VBR inter frames emitted after vpx_codec_enc_config_set
// keep cyclic refresh active and continue to re-emit the segment map and
// alt-Q feature data each frame, matching pack_bitstream output on the
// VBR→CBR / CBR→VBR runtime-transition byte-parity oracles.

func TestSetRateControlVBRKeepsLibvpxCyclicRefreshHeader(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 8192)
	key, err := e.EncodeInto(dst, encoderValidationPanningFrame(16, 16, 0), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	keyState := packetState(t, key.Data)
	wantDelta := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData || wantDelta == 0 {
		t.Fatalf("key segmentation = %+v, want cyclic-refresh map/data with nonzero alt-q", keyState.Segmentation)
	}

	cfg := RateControlConfig{
		Mode:                RateControlVBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}
	if err := e.SetRateControl(cfg); err != nil {
		t.Fatalf("SetRateControl(VBR): %v", err)
	}
	if !e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("cyclic refresh disabled after VBR config, want construction-pinned active for CBR-born")
	}
	inter, err := e.EncodeInto(dst, encoderValidationPanningFrame(16, 16, 1), 1, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto inter: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("VBR inter segmentation = %+v, want cyclic-refresh map/data update", interState.Segmentation)
	}
	if got := interState.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("VBR inter alt-q delta = %d, want carried %d", got, wantDelta)
	}
}

func TestSetRateControlCQRefreshesPreservedSegmentationDelta(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineRealtime,
		CpuUsed:           0,
		Tuning:            TunePSNR,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer e.Close()

	dst := make([]byte, 1<<20)
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 0), 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto key: %v", err)
	}
	if _, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 1), 1, 1, 0); err != nil {
		t.Fatalf("EncodeInto first inter: %v", err)
	}
	if err := e.SetRateControl(RateControlConfig{
		Mode:                RateControlCQ,
		TargetBitrateKbps:   300,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             30,
		UndershootPct:       100,
		OvershootPct:        100,
		BufferSizeMs:        6000,
		BufferInitialSizeMs: 4000,
		BufferOptimalSizeMs: 5000,
	}); err != nil {
		t.Fatalf("SetRateControl(CQ): %v", err)
	}
	inter, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 2), 2, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto CQ inter: %v", err)
	}
	state := packetState(t, inter.Data)
	wantQ := vp8common.PublicQuantizerToQIndex(30)
	wantDelta := cyclicRefreshQuantizerDeltaForQuantizer(wantQ)
	if state.Quant.BaseQIndex != uint8(wantQ) {
		t.Fatalf("CQ base q = %d, want %d", state.Quant.BaseQIndex, wantQ)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("CQ preserved alt-q delta = %d, want refreshed %d", got, wantDelta)
	}

	if err := e.SetCQLevel(40); err != nil {
		t.Fatalf("SetCQLevel(40): %v", err)
	}
	next, err := e.EncodeInto(dst, encoderValidationSegmentedFrame(64, 64, 3), 3, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto CQ-level inter: %v", err)
	}
	state = packetState(t, next.Data)
	wantQ = vp8common.PublicQuantizerToQIndex(40)
	wantDelta = cyclicRefreshQuantizerDeltaForQuantizer(wantQ)
	if state.Quant.BaseQIndex != uint8(wantQ) {
		t.Fatalf("CQ level base q = %d, want %d", state.Quant.BaseQIndex, wantQ)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][vp8enc.StaticSegmentID]; got != wantDelta {
		t.Fatalf("CQ level preserved alt-q delta = %d, want refreshed %d", got, wantDelta)
	}
}
