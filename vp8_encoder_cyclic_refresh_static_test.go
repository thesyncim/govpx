package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"testing"
)

func TestEncodeIntoStaticThresholdRotatesCyclicRefreshSegments(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               80,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, 65536)
	keySource := testImage(80, 64)
	fillImage(keySource, 128, 128, 128)
	key, err := e.EncodeInto(packet, keySource, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}

	var prevRefreshed []int
	nonEmptyRefreshes := 0
	for frame := range 4 {
		src := publicImageFromVP8(&e.lastRef.Img)
		inter, err := e.EncodeInto(packet, src, uint64(frame+1), 1, 0)
		if err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
		if err := d.Decode(inter.Data); err != nil {
			t.Fatalf("inter %d Decode returned error: %v", frame, err)
		}
		refreshed := make([]int, 0, len(d.modes))
		for i := range d.modes {
			if d.modes[i].SegmentID == vp8enc.StaticSegmentID {
				refreshed = append(refreshed, i)
			}
		}
		if len(refreshed) == 0 {
			t.Fatalf("inter %d refreshed set empty, want cyclic refresh activity", frame)
		}
		if nonEmptyRefreshes > 0 && len(refreshed) == len(prevRefreshed) {
			same := true
			for i := range refreshed {
				if refreshed[i] != prevRefreshed[i] {
					same = false
					break
				}
			}
			if same {
				t.Fatalf("inter %d refreshed set %v, want rotation from previous frame %v", frame, refreshed, prevRefreshed)
			}
		}
		prevRefreshed = append(prevRefreshed[:0], refreshed...)
		nonEmptyRefreshes++
		if _, ok := d.NextFrame(); !ok {
			t.Fatalf("inter %d NextFrame returned no frame", frame)
		}
	}
	if nonEmptyRefreshes < 2 {
		t.Fatalf("non-empty refresh frames = %d, want at least 2 to verify rotation", nonEmptyRefreshes)
	}
}

// TestEncodeIntoCyclicRefreshIndexPreservedAcrossKeyFrames pins libvpx
// vp8/encoder/onyx_if.c cyclic_background_refresh: the cyclic_refresh_mode_index
// is reset to 0 only on init (line 1213) and resize, not on each key frame
// (the iteration loop at line 534 is gated on frame_type != KEY_FRAME so a
// key frame leaves the index untouched). govpx must mirror that — resetting
// the index on each forced keyframe shifts the rolling refresh window
// relative to libvpx for every GOP after the first.

func TestEncodeIntoCyclicRefreshIndexPreservedAcrossKeyFrames(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               80,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	packet := make([]byte, 65536)
	src := testImage(80, 64)
	fillImage(src, 128, 128, 128)
	if _, err := e.EncodeInto(packet, src, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	// Drive a few inter frames so the cyclic refresh index advances away
	// from zero. A 5x4 frame has 20 MBs and refreshCount = 20/20 = 1, so
	// each inter frame advances the index by exactly 1.
	for frame := range 3 {
		s := publicImageFromVP8(&e.lastRef.Img)
		if _, err := e.EncodeInto(packet, s, uint64(frame+1), 1, 0); err != nil {
			t.Fatalf("inter %d EncodeInto returned error: %v", frame, err)
		}
	}
	beforeKey := e.cyclicRefreshIndex
	if beforeKey == 0 {
		t.Fatalf("cyclicRefreshIndex stayed at 0 after 3 inter frames; expected forward progress")
	}
	// Force a second keyframe and confirm the index survives.
	e.ForceKeyFrame()
	src2 := testImage(80, 64)
	fillImage(src2, 128, 128, 128)
	if _, err := e.EncodeInto(packet, src2, 4, 1, 0); err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if e.cyclicRefreshIndex != beforeKey {
		t.Fatalf("cyclicRefreshIndex after key frame = %d, want libvpx-preserved %d", e.cyclicRefreshIndex, beforeKey)
	}
}

func TestCommitKeyFrameCyclicRefreshMapPreservesRollingMap(t *testing.T) {
	e := &VP8Encoder{
		cyclicRefreshMap:        []int8{0, -1, 1, 0},
		cyclicRefreshAttemptMap: []int8{1, 0, -1, 1},
	}
	modes := []vp8enc.KeyFrameMacroblockMode{
		{SegmentID: 0},
		{SegmentID: 1},
		{SegmentID: 0},
		{SegmentID: 0},
	}

	e.commitKeyFrameCyclicRefreshMap(2, 2, modes, true)

	wantMap := []int8{0, -1, 1, 0}
	wantAttempt := []int8{1, 0, -1, 1}
	for i := range wantMap {
		if e.cyclicRefreshMap[i] != wantMap[i] {
			t.Fatalf("cyclicRefreshMap[%d] = %d, want %d (map=%v)", i, e.cyclicRefreshMap[i], wantMap[i], e.cyclicRefreshMap)
		}
		if e.cyclicRefreshAttemptMap[i] != wantAttempt[i] {
			t.Fatalf("cyclicRefreshAttemptMap[%d] = %d, want %d (map=%v)", i, e.cyclicRefreshAttemptMap[i], wantAttempt[i], e.cyclicRefreshAttemptMap)
		}
	}
}

func TestEncodeIntoStaticThresholdWritesCyclicRefreshSegmentationForMatchingReference(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               32,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        20,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := segmentedQuantizationTestImage()
	keyPacket := make([]byte, 16384)
	key, err := e.EncodeInto(keyPacket, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	reconstructed := publicImageFromVP8(&e.lastRef.Img)
	interPacket := make([]byte, 16384)

	inter, err := e.EncodeInto(interPacket, reconstructed, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want matching-reference interframe")
	}
	state := packetState(t, inter.Data)
	if !state.Segmentation.Enabled || !state.Segmentation.UpdateMap || !state.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want map and data update for matching reference", state.Segmentation)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter segment IDs = %d/%d, want no cyclic refresh blocks in tiny frame", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "matching-reference segmented inter", frame, publicImageFromVP8(&e.current.Img))
}

func TestEncodeIntoStaticThresholdSkipsTemporalEnhancementLayerSegmentation(t *testing.T) {
	e := newTemporalTestEncoder(t, TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers})
	if err := e.SetStaticThreshold(1); err != nil {
		t.Fatalf("SetStaticThreshold returned error: %v", err)
	}
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	packet := make([]byte, 4096)

	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled {
		t.Fatalf("key segmentation disabled, want base-layer cyclic refresh enabled")
	}

	enhancement, err := e.EncodeInto(packet, src, 1, 1, 0)
	if err != nil {
		t.Fatalf("enhancement EncodeInto returned error: %v", err)
	}
	if enhancement.TemporalLayerID != 1 {
		t.Fatalf("enhancement temporal layer = %d, want 1", enhancement.TemporalLayerID)
	}
	enhancementState := packetState(t, enhancement.Data)
	if enhancementState.Segmentation.Enabled || enhancementState.Segmentation.UpdateMap || enhancementState.Segmentation.UpdateData {
		t.Fatalf("enhancement segmentation = %+v, want disabled like libvpx non-base temporal layer", enhancementState.Segmentation)
	}
}
