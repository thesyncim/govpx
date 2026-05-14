package govpx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestSetROIMapValidationAndDisable(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	valid := ROIMap{
		Enabled:   true,
		Rows:      1,
		Cols:      2,
		SegmentID: []uint8{1, 0},
	}
	valid.DeltaQuantizer[1] = -10
	valid.DeltaLoopFilter[1] = 7
	valid.StaticThreshold[1] = 900

	if err := e.SetROIMap(&valid); err != nil {
		t.Fatalf("SetROIMap(valid) returned error: %v", err)
	}
	if !e.roi.enabled || e.roi.rows != 1 || e.roi.cols != 2 {
		t.Fatalf("roi state = %+v, want enabled 1x2", e.roi)
	}
	if !e.roi.suppressCyclicRefresh {
		t.Fatalf("ROI did not suppress cyclic refresh")
	}
	if got, want := e.roi.deltaQuantizer[1], int8(-libvpxPublicQuantizerToQIndex(10)); got != want {
		t.Fatalf("roi delta q[1] = %d, want %d", got, want)
	}
	if got := e.interStaticThresholdForSegment(1); got != 900 {
		t.Fatalf("roi static threshold[1] = %d, want 900", got)
	}

	badRange := valid
	badRange.DeltaQuantizer[1] = 64
	if err := e.SetROIMap(&badRange); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetROIMap bad q range error = %v, want ErrInvalidConfig", err)
	}
	badLF := valid
	badLF.DeltaLoopFilter[1] = -64
	if err := e.SetROIMap(&badLF); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetROIMap bad lf range error = %v, want ErrInvalidConfig", err)
	}
	badSegment := valid
	badSegment.SegmentID = []uint8{4, 0}
	if err := e.SetROIMap(&badSegment); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetROIMap bad segment error = %v, want ErrInvalidConfig", err)
	}
	badDims := valid
	badDims.Rows = 2
	if err := e.SetROIMap(&badDims); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetROIMap bad dims error = %v, want ErrInvalidConfig", err)
	}

	zero := ROIMap{Enabled: true, Rows: 1, Cols: 2, SegmentID: []uint8{1, 0}}
	if err := e.SetROIMap(&zero); err != nil {
		t.Fatalf("SetROIMap zero returned error: %v", err)
	}
	if e.roi.enabled {
		t.Fatalf("zero-effect ROI left enabled")
	}
	if !e.roi.suppressCyclicRefresh || e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("zero-effect ROI disable restored cyclic refresh")
	}
	if err := e.SetROIMap(nil); err != nil {
		t.Fatalf("SetROIMap(nil) returned error: %v", err)
	}
	if e.roi.enabled {
		t.Fatalf("nil ROI left enabled")
	}
	if !e.roi.suppressCyclicRefresh || e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("nil ROI disable restored cyclic refresh")
	}
	e.Reset()
	if e.roi.suppressCyclicRefresh || !e.cyclicRefreshModeEnabled(false) {
		t.Fatalf("Reset roi suppress=%t cyclic=%t, want suppress=false cyclic=true", e.roi.suppressCyclicRefresh, e.cyclicRefreshModeEnabled(false))
	}
}

func TestSetROIMapWritesSegmentationMap(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	roi := ROIMap{
		Enabled:   true,
		Rows:      1,
		Cols:      2,
		SegmentID: []uint8{1, 0},
	}
	roi.DeltaQuantizer[1] = -10
	roi.DeltaLoopFilter[1] = 3
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap returned error: %v", err)
	}

	dst := make([]byte, 16384)
	key, err := e.EncodeInto(dst, segmentedQuantizationTestImage(), 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Segmentation.Enabled || !keyState.Segmentation.UpdateMap || !keyState.Segmentation.UpdateData {
		t.Fatalf("key segmentation = %+v, want ROI map/data update", keyState.Segmentation)
	}
	if got, want := keyState.Segmentation.FeatureData[vp8common.MBLvlAltQ][1], int8(-libvpxPublicQuantizerToQIndex(10)); got != want {
		t.Fatalf("key ROI alt-q[1] = %d, want %d", got, want)
	}
	if got := keyState.Segmentation.FeatureData[vp8common.MBLvlAltLF][1]; got != 3 {
		t.Fatalf("key ROI alt-lf[1] = %d, want 3", got)
	}

	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(key.Data); err != nil {
		t.Fatalf("key Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 1 || d.modes[1].SegmentID != 0 {
		t.Fatalf("key ROI segment IDs = %d/%d, want 1/0", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}

	second := segmentedQuantizationTestImage()
	for row := 0; row < second.Height; row++ {
		for col := range 16 {
			second.Y[row*second.YStride+col] = 220
		}
	}
	inter, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want inter frame")
	}
	interState := packetState(t, inter.Data)
	if !interState.Segmentation.Enabled || interState.Segmentation.UpdateMap || interState.Segmentation.UpdateData {
		t.Fatalf("inter segmentation = %+v, want ROI enabled with retained map/data", interState.Segmentation)
	}
	if err := d.Decode(inter.Data); err != nil {
		t.Fatalf("inter Decode returned error: %v", err)
	}
	if d.modes[0].SegmentID != 1 || d.modes[1].SegmentID != 0 {
		t.Fatalf("inter ROI segment IDs = %d/%d, want 1/0", d.modes[0].SegmentID, d.modes[1].SegmentID)
	}

	forced, err := e.EncodeInto(dst, second, 2, 1, EncodeForceKeyFrame)
	if err != nil {
		t.Fatalf("forced keyframe EncodeInto returned error: %v", err)
	}
	if !forced.KeyFrame {
		t.Fatalf("forced KeyFrame = false, want keyframe")
	}
	forcedState := packetState(t, forced.Data)
	if !forcedState.Segmentation.Enabled || !forcedState.Segmentation.UpdateMap || !forcedState.Segmentation.UpdateData {
		t.Fatalf("forced keyframe segmentation = %+v, want ROI map/data update", forcedState.Segmentation)
	}

	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap refresh returned error: %v", err)
	}
	third, err := e.EncodeInto(dst, second, 3, 1, 0)
	if err != nil {
		t.Fatalf("third EncodeInto returned error: %v", err)
	}
	thirdState := packetState(t, third.Data)
	if !thirdState.Segmentation.Enabled || !thirdState.Segmentation.UpdateMap || !thirdState.Segmentation.UpdateData {
		t.Fatalf("third segmentation = %+v, want refreshed ROI map/data update", thirdState.Segmentation)
	}
}

func TestRuntimeConfigChangeResendsROIMapAndData(t *testing.T) {
	e := newSizedTestEncoder(t, 32, 16)
	roi := ROIMap{
		Enabled:   true,
		Rows:      1,
		Cols:      2,
		SegmentID: []uint8{1, 0},
	}
	roi.DeltaQuantizer[1] = -10
	roi.StaticThreshold[1] = 900
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap returned error: %v", err)
	}

	dst := make([]byte, 16384)
	source := segmentedQuantizationTestImage()
	if _, err := e.EncodeInto(dst, source, 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	retained, err := e.EncodeInto(dst, source, 1, 1, 0)
	if err != nil {
		t.Fatalf("retained inter EncodeInto returned error: %v", err)
	}
	retainedState := packetState(t, retained.Data)
	if !retainedState.Segmentation.Enabled || retainedState.Segmentation.UpdateMap || retainedState.Segmentation.UpdateData {
		t.Fatalf("retained segmentation = %+v, want enabled without map/data update", retainedState.Segmentation)
	}

	if err := e.SetNoiseSensitivity(3); err != nil {
		t.Fatalf("SetNoiseSensitivity returned error: %v", err)
	}
	updated, err := e.EncodeInto(dst, source, 2, 1, 0)
	if err != nil {
		t.Fatalf("config-change inter EncodeInto returned error: %v", err)
	}
	updatedState := packetState(t, updated.Data)
	if !updatedState.Segmentation.Enabled || !updatedState.Segmentation.UpdateMap || !updatedState.Segmentation.UpdateData {
		t.Fatalf("config-change segmentation = %+v, want ROI map/data update", updatedState.Segmentation)
	}
}

func TestROIMapStaticThresholdFallsBackToGlobalThreshold(t *testing.T) {
	e := newSizedTestEncoder(t, 16, 16)
	e.opts.StaticThreshold = 123
	roi := ROIMap{Enabled: true, Rows: 1, Cols: 1, SegmentID: []uint8{1}}
	roi.DeltaQuantizer[1] = 1
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap returned error: %v", err)
	}
	if got := e.interStaticThresholdForSegment(1); got != 123 {
		t.Fatalf("static threshold without ROI thresholds = %d, want global 123", got)
	}
	roi.StaticThreshold[1] = 456
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap with threshold returned error: %v", err)
	}
	if got := e.interStaticThresholdForSegment(1); got != 456 {
		t.Fatalf("static threshold with ROI threshold = %d, want 456", got)
	}
}
