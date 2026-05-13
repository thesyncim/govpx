package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

func TestEncodeIntoStaticThresholdWritesCyclicRefreshSegmentation(t *testing.T) {
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
	packet := make([]byte, 16384)

	key, err := e.EncodeInto(packet, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want map and data update", keyState.Segmentation)
	}
	wantAltQ := int8(libvpxPublicQuantizerToQIndex(20)/2 - libvpxPublicQuantizerToQIndex(20))
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != wantAltQ {
		t.Fatalf("key static segment alt-q = %d, want %d", got, wantAltQ)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("key segment IDs = %d/%d, want all zero for cyclic refresh keyframe", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("key NextFrame returned no frame")
	}
	assertImagesEqual(t, "static key current", keyFrame, publicImageFromVP8(&e.current.Img))

	second := segmentedQuantizationTestImage()
	for row := 0; row < second.Height; row++ {
		for col := range 16 {
			second.Y[row*second.YStride+col] = 96
		}
	}
	inter, err := e.EncodeInto(packet, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want map and data update", interState.Segmentation)
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 0 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter segment IDs = %d/%d, want no cyclic refresh blocks in tiny frame", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}
	interFrame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("inter NextFrame returned no frame")
	}
	assertImagesEqual(t, "static inter current", interFrame, publicImageFromVP8(&e.current.Img))
}

func TestCyclicRefreshSegmentationConfigMirrorsLibvpxEnablementAndBoost(t *testing.T) {
	e := VP8Encoder{}
	e.rc.mode = RateControlCBR
	e.rc.currentQuantizer = 20

	cfg := e.cyclicRefreshSegmentationConfig(false)

	if !cfg.Enabled || !cfg.UpdateMap || !cfg.UpdateData {
		t.Fatalf("cyclic segmentation = %+v, want enabled map/data update", cfg)
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -10 {
		t.Fatalf("cyclic segment alt-q = %d, want background delta -10", got)
	}

	e.rc.currentQuantizer = 21
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("q=21 cyclic segmentation disabled, want background boost")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -11 {
		t.Fatalf("q=21 cyclic segment alt-q = %d, want libvpx Q/2-Q delta -11", got)
	}

	e.rc.currentQuantizer = 1
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled {
		t.Fatalf("q=1 cyclic segmentation disabled, want libvpx Q/2-Q delta enabled")
	}
	if got := cfg.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -1 {
		t.Fatalf("q=1 cyclic segment alt-q = %d, want libvpx Q/2-Q delta -1", got)
	}

	e.rc.currentQuantizer = 0
	cfg = e.cyclicRefreshSegmentationConfig(false)
	if !cfg.Enabled || cfg.FeatureEnabled[vp8common.MBLvlAltQ][staticSegmentID] {
		t.Fatalf("q=0 cyclic segmentation = %+v, want enabled with no alt-q feature", cfg)
	}

	e.rc.mode = RateControlVBR
	e.opts.StaticThreshold = 1
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("VBR static-threshold cyclic segmentation = %+v, want disabled", cfg)
	}

	e.rc.mode = RateControlCBR
	e.opts.ScreenContentMode = 2
	if cfg := e.cyclicRefreshSegmentationConfig(true); cfg.Enabled {
		t.Fatalf("screen-content mode 2 golden-refresh segmentation = %+v, want disabled", cfg)
	}
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("screen-content mode 2 non-golden cyclic segmentation disabled, want enabled")
	}

	e.opts.RTCExternalRateControl = true
	if cfg := e.cyclicRefreshSegmentationConfig(false); cfg.Enabled {
		t.Fatalf("RTC external rate-control cyclic segmentation = %+v, want disabled", cfg)
	}
	e.opts.RTCExternalRateControl = false
	if cfg := e.cyclicRefreshSegmentationConfig(false); !cfg.Enabled {
		t.Fatalf("after disabling RTC external rate-control cyclic segmentation disabled, want enabled")
	}
}

func TestEncodeIntoDefaultCBREnablesLibvpxCyclicRefreshSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want libvpx default cyclic refresh", keyState.Segmentation)
	}
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -2 {
		t.Fatalf("key cyclic alt-q = %d, want libvpx q/2-q delta -2", got)
	}

	inter, err := e.EncodeInto(dst, publicImageFromVP8(&e.lastRef.Img), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || !interState.Segmentation.UpdateMap || !interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want libvpx default cyclic refresh", interState.Segmentation)
	}
}

func TestCyclicRefreshSegmentationUsesPreRecodeQuantizer(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             128,
		Height:            128,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 700,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           -3,
		KeyFrameInterval:  999,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 1<<20)
	var result EncodeResult
	for frame := range 3 {
		result, err = e.EncodeInto(dst, encoderValidationPanningFrame(128, 128, frame), uint64(frame), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", frame, err)
		}
	}
	state := packetState(t, result.Data)
	if got := state.Quant.BaseQIndex; got != 4 {
		t.Fatalf("frame 2 base Q = %d, want recoded final Q 4", got)
	}
	if got := state.Segmentation.FeatureData[vp8common.MBLvlAltQ][staticSegmentID]; got != -7 {
		t.Fatalf("frame 2 cyclic alt-q = %d, want pre-recode Q/2-Q delta -7", got)
	}
}

func TestEncodeIntoScreenContentMode2KeepsKeyFrameCyclicSegmentation(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		ScreenContentMode:   2,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 8192)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("screen-content mode 2 key segmentation = %+v, want map/data update", keyState.Segmentation)
	}
}

func TestCyclicRefreshSegmentationTreeProbsMirrorLibvpxCounts(t *testing.T) {
	cfg := vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	keyModes := []vp8enc.KeyFrameMacroblockMode{{SegmentID: 0}, {SegmentID: 0}}

	updateKeyFrameSegmentationTreeProbs(&cfg, keyModes)
	if cfg.TreeProbUpdated != ([vp8common.MBFeatureTreeProbs]bool{}) {
		t.Fatalf("key tree prob updates = %v, want none for all-zero segment map", cfg.TreeProbUpdated)
	}

	cfg = vp8enc.SegmentationConfig{Enabled: true, UpdateMap: true, UpdateData: true}
	interModes := make([]vp8enc.InterFrameMacroblockMode, 40)
	interModes[0].SegmentID = staticSegmentID
	interModes[1].SegmentID = staticSegmentID

	updateInterFrameSegmentationTreeProbs(&cfg, interModes)
	if cfg.TreeProbUpdated[0] || !cfg.TreeProbUpdated[1] || cfg.TreeProbUpdated[2] {
		t.Fatalf("inter tree prob update flags = %v, want only branch 1 updated", cfg.TreeProbUpdated)
	}
	if got := cfg.TreeProbs[1]; got != 242 {
		t.Fatalf("inter tree prob[1] = %d, want libvpx count-derived 242", got)
	}
}

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshCadence(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 40)
	refreshCount := cyclicRefreshMaxMBsPerFrameForLayers(4, 10, 1)

	assignInterFrameStaticSegments(4, 10, 0, refreshCount, modes)

	if modes[0].SegmentID != staticSegmentID || modes[1].SegmentID != staticSegmentID {
		t.Fatalf("first cyclic segment IDs = %d/%d, want refreshed", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != 0 || modes[len(modes)-1].SegmentID != 0 {
		t.Fatalf("later cyclic segment IDs = %d/%d, want zero", modes[2].SegmentID, modes[len(modes)-1].SegmentID)
	}

	assignInterFrameStaticSegments(4, 10, 2, refreshCount, modes)
	if modes[0].SegmentID != 0 || modes[1].SegmentID != 0 {
		t.Fatalf("previous cyclic segment IDs = %d/%d, want cleared", modes[0].SegmentID, modes[1].SegmentID)
	}
	if modes[2].SegmentID != staticSegmentID || modes[3].SegmentID != staticSegmentID {
		t.Fatalf("rotated cyclic segment IDs = %d/%d, want refreshed", modes[2].SegmentID, modes[3].SegmentID)
	}

	assignInterFrameStaticSegments(4, 10, 39, refreshCount, modes)
	if modes[39].SegmentID != staticSegmentID || modes[0].SegmentID != staticSegmentID {
		t.Fatalf("wrapped cyclic segment IDs = %d/%d, want refreshed", modes[39].SegmentID, modes[0].SegmentID)
	}
	if modes[1].SegmentID != 0 || modes[38].SegmentID != 0 {
		t.Fatalf("wrapped neighbor segment IDs = %d/%d, want zero", modes[1].SegmentID, modes[38].SegmentID)
	}
}

func TestAssignInterFrameStaticSegmentsUsesCyclicRefreshMapEligibility(t *testing.T) {
	modes := make([]vp8enc.InterFrameMacroblockMode, 5)
	refreshMap := []int8{0, -1, 1, 0, 0}

	next := assignInterFrameStaticSegmentsWithMap(1, 5, 0, 2, refreshMap, modes)

	if next != 4 {
		t.Fatalf("next cyclic refresh index = %d, want 4 after libvpx-style eligible refresh budget", next)
	}
	if modes[0].SegmentID != staticSegmentID || modes[3].SegmentID != staticSegmentID {
		t.Fatalf("segment IDs = %d/%d, want refreshed MB0 and MB3 under libvpx eligible budget", modes[0].SegmentID, modes[3].SegmentID)
	}
	if modes[1].SegmentID != 0 || modes[2].SegmentID != 0 || modes[4].SegmentID != 0 {
		t.Fatalf("ineligible segment IDs = %d/%d/%d, want zero", modes[1].SegmentID, modes[2].SegmentID, modes[4].SegmentID)
	}
	if refreshMap[1] != 0 {
		t.Fatalf("cooldown map[1] = %d, want incremented to candidate 0", refreshMap[1])
	}
	if refreshMap[2] != 1 {
		t.Fatalf("dirty map[2] = %d, want unchanged", refreshMap[2])
	}
}

func TestCyclicRefreshStaticClassificationPopulatesSkinMapOnly(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               160,
		Height:              64,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		StaticThreshold:     1,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	src := testImage(160, 64)
	fillImage(src, 128, 128, 128)
	fillMacroblock(src, 0, 0, 120, 117, 150)
	modes := make([]vp8enc.InterFrameMacroblockMode, 40)
	source := sourceImageFromPublic(src)

	e.prepareInterFrameSkinMap(source, 4, 10)
	next := e.assignInterFrameStaticSegments(source, 4, 10, modes)

	if e.skinMap[0] != 1 {
		t.Fatalf("skinMap[0] = %d, want libvpx skin classification", e.skinMap[0])
	}
	refreshed := 0
	for i := range 3 {
		if modes[i].SegmentID == staticSegmentID {
			refreshed++
		}
	}
	if refreshed == 0 {
		t.Fatalf("segment IDs = %d/%d/%d, want cyclic refresh activity", modes[0].SegmentID, modes[1].SegmentID, modes[2].SegmentID)
	}
	if next != 2 {
		t.Fatalf("next cyclic refresh index = %d, want 2 from cyclic refresh cadence", next)
	}
}

func TestUpdateConsecutiveZeroLastMirrorsLibvpxCounter(t *testing.T) {
	counters := []uint8{0, 254, 7}
	modes := []vp8enc.InterFrameMacroblockMode{
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}

	updateConsecutiveZeroLast(modes, counters)
	updateConsecutiveZeroLast(modes, counters)

	want := []uint8{2, 255, 0}
	for i := range want {
		if counters[i] != want[i] {
			t.Fatalf("counter[%d] = %d, want %d", i, counters[i], want[i])
		}
	}
}

func TestUpdateCyclicRefreshMapFromInterFrameMirrorsLibvpxStates(t *testing.T) {
	refreshMap := []int8{0, 1, 0, 0}
	modes := []vp8enc.InterFrameMacroblockMode{
		{SegmentID: staticSegmentID, RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV},
		{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV},
		{RefFrame: vp8common.GoldenFrame, Mode: vp8common.ZeroMV},
	}

	updateCyclicRefreshMapFromInterFrame(modes, refreshMap)

	want := []int8{-1, 0, 1, 1}
	for i := range want {
		if refreshMap[i] != want[i] {
			t.Fatalf("refreshMap[%d] = %d, want %d", i, refreshMap[i], want[i])
		}
	}
}

func TestCyclicRefreshMaxMBsPerFrameMirrorsLibvpxLayerCadence(t *testing.T) {
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 1); got != 3 {
		t.Fatalf("one-layer cyclic refresh MBs = %d, want libvpx MBs/20", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 2); got != 6 {
		t.Fatalf("two-layer cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForLayers(8, 8, 3); got != 9 {
		t.Fatalf("three-layer cyclic refresh MBs = %d, want libvpx MBs/7", got)
	}
}

func TestCyclicRefreshMaxMBsPerFrameMirrorsLibvpxScreenContentCadence(t *testing.T) {
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 100, 0, 0); got != 6 {
		t.Fatalf("screen-content high-q cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 2, 80, 0, 0); got != 6 {
		t.Fatalf("aggressive screen-content high-q cyclic refresh MBs = %d, want libvpx MBs/10", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 19, 251, 61); got != 0 {
		t.Fatalf("screen-content stable low-q cyclic refresh MBs = %d, want disabled", got)
	}
	if got := cyclicRefreshMaxMBsPerFrameForConfig(8, 8, 3, 1, 19, 251, 60); got != 3 {
		t.Fatalf("screen-content low-q cyclic refresh MBs = %d, want libvpx MBs/20", got)
	}
}

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
			if d.modes[i].SegmentID == staticSegmentID {
				refreshed = append(refreshed, i)
			}
		}
		if frame == 0 {
			if len(refreshed) != 0 {
				t.Fatalf("inter 0 refreshed set = %v, want empty after key-frame dirty-map update", refreshed)
			}
			if _, ok := d.NextFrame(); !ok {
				t.Fatalf("inter %d NextFrame returned no frame", frame)
			}
			continue
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
