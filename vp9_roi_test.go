package govpx

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderSetROIMapValidationAndCopy(t *testing.T) {
	const width, height = 16, 16
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	roi := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: []uint8{1, 0, 0, 0},
	}
	roi.DeltaQuantizer[1] = -10
	roi.DeltaLoopFilter[1] = 3
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap(valid): %v", err)
	}
	if !e.roi.enabled || e.roi.rows != rows || e.roi.cols != cols {
		t.Fatalf("ROI state = enabled:%t dims:%dx%d, want true %dx%d",
			e.roi.enabled, e.roi.rows, e.roi.cols, rows, cols)
	}
	if got, want := e.roi.deltaQuantizer[1], int16(-vp9PublicQuantizerToQIndex(10)); got != want {
		t.Fatalf("ROI delta q[1] = %d, want %d", got, want)
	}
	if got := e.roi.deltaLoopFilter[1]; got != 3 {
		t.Fatalf("ROI delta lf[1] = %d, want 3", got)
	}
	roi.SegmentID[0] = 0
	if e.roi.segmentID[0] != 1 {
		t.Fatal("SetROIMap kept caller segment map instead of copying")
	}

	oldRows, oldCols := e.roi.rows, e.roi.cols
	oldDQ, oldDLF := e.roi.deltaQuantizer, e.roi.deltaLoopFilter
	oldMap := append([]uint8(nil), e.roi.segmentID...)
	badDims := roi
	badDims.Rows = rows - 1
	if err := e.SetROIMap(&badDims); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad dimension SetROIMap err = %v, want ErrInvalidConfig", err)
	}
	if e.roi.rows != oldRows || e.roi.cols != oldCols ||
		e.roi.deltaQuantizer != oldDQ || e.roi.deltaLoopFilter != oldDLF ||
		!bytes.Equal(e.roi.segmentID, oldMap) {
		t.Fatal("invalid-dimension SetROIMap mutated encoder state")
	}

	badSegment := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: []uint8{4, 0, 0, 0},
	}
	badSegment.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(&badSegment); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad segment SetROIMap err = %v, want ErrInvalidConfig", err)
	}
	badStatic := roi
	badStatic.Rows = rows
	badStatic.StaticThreshold[1] = 1
	if err := e.SetROIMap(&badStatic); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("static threshold SetROIMap err = %v, want ErrInvalidConfig", err)
	}

	zero := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: []uint8{1, 0, 0, 0},
	}
	if err := e.SetROIMap(&zero); err != nil {
		t.Fatalf("zero SetROIMap: %v", err)
	}
	if e.roi.enabled || e.roi.rows != 0 || e.roi.cols != 0 {
		t.Fatalf("zero ROI state = enabled:%t dims:%dx%d, want disabled",
			e.roi.enabled, e.roi.rows, e.roi.cols)
	}
	if err := e.SetROIMap(nil); err != nil {
		t.Fatalf("nil SetROIMap: %v", err)
	}
	if e.roi.enabled {
		t.Fatal("nil SetROIMap did not disable ROI")
	}
}

// TestVP9EncoderSetROIMapSkipAndRefFrameWiring pins the libvpx-faithful
// translation of ROIMap.Skip[]/ROIMap.RefFrame[] (the libvpx
// vp9_set_roi_map skip[] and ref_frame[] arrays) into VP9 segmentation
// header bits.  The encoder is expected to set SegLvlSkip and SegLvlRefFrame
// in the segmentation header for any segment with a non-zero override.
//
// libvpx: vp9/encoder/vp9_encoder.c:693 vp9_set_roi_map — skip[]/ref_frame[]
// flow into cpi->roi.skip and cpi->roi.ref_frame, then into the per-frame
// segmentation header bits at vp9/encoder/vp9_pickmode.c::set_segment_index_
// roi (delegated through the segmentation feature mask).
func TestVP9EncoderSetROIMapSkipAndRefFrameWiring(t *testing.T) {
	const width, height = 16, 16
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	roi := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: []uint8{1, 2, 0, 0},
	}
	roi.Skip[1] = 1
	roi.RefFrame[2] = 2 // GOLDEN_FRAME per libvpx ref_frame_range == 3 mapping
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	if !e.roi.enabled {
		t.Fatalf("ROI not enabled after Skip/RefFrame-only set")
	}
	if e.roi.skip[1] != 1 {
		t.Fatalf("skip[1] = %d, want 1", e.roi.skip[1])
	}
	if e.roi.refFrame[2] != 2 {
		t.Fatalf("refFrame[2] = %d, want 2 (GOLDEN)", e.roi.refFrame[2])
	}
	seg := e.roi.segmentationParams()
	if seg.FeatureMask[1]&(1<<uint(vp9dec.SegLvlSkip)) == 0 {
		t.Fatalf("Skip[1] not surfaced as SegLvlSkip in segmentationParams")
	}
	if seg.FeatureMask[2]&(1<<uint(vp9dec.SegLvlRefFrame)) == 0 {
		t.Fatalf("RefFrame[2] not surfaced as SegLvlRefFrame in segmentationParams")
	}
	if seg.FeatureData[2][vp9dec.SegLvlRefFrame] != 2 {
		t.Fatalf("SegLvlRefFrame data = %d, want 2 (GOLDEN)",
			seg.FeatureData[2][vp9dec.SegLvlRefFrame])
	}
}

// TestVP9EncoderSetROIMapSkipAndRefFrameValidationRanges pins the libvpx
// vp9_set_roi_map range checks (vp9/encoder/vp9_encoder.c:699-704).  Out-
// of-range Skip / RefFrame values must return ErrInvalidConfig.
func TestVP9EncoderSetROIMapSkipAndRefFrameValidationRanges(t *testing.T) {
	const width, height = 16, 16
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	base := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: []uint8{0, 0, 0, 0},
	}
	for _, tc := range []struct {
		name string
		mut  func(*ROIMap)
	}{
		{name: "Skip > 1", mut: func(r *ROIMap) { r.Skip[1] = 2 }},
		{name: "Skip < 0", mut: func(r *ROIMap) { r.Skip[1] = -1 }},
		{name: "RefFrame > 3", mut: func(r *ROIMap) { r.RefFrame[1] = 4 }},
		{name: "RefFrame < -1", mut: func(r *ROIMap) { r.RefFrame[1] = -2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roi := base
			tc.mut(&roi)
			if err := e.SetROIMap(&roi); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("SetROIMap(%s) err = %v, want ErrInvalidConfig",
					tc.name, err)
			}
		})
	}
}

// TestVP9EncoderSetROIMapRefFrameMinusOneEqualsZero pins the libvpx-faithful
// no-override sentinel: both RefFrame == -1 (libvpx convention) and
// RefFrame == 0 (Go zero value) MUST disable the segment ref-frame
// override.  Skip == 0 must do the same for the skip override.
func TestVP9EncoderSetROIMapRefFrameMinusOneEqualsZero(t *testing.T) {
	const width, height = 16, 16
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	rows := (height + 7) >> 3
	cols := (width + 7) >> 3
	roi := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: []uint8{1, 0, 0, 0},
	}
	roi.RefFrame[1] = -1
	roi.RefFrame[2] = 0
	// Add a real delta so the ROI is enabled but Skip/RefFrame both stay off.
	roi.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	if !e.roi.enabled {
		t.Fatalf("ROI not enabled with non-zero delta")
	}
	for i, want := range [vp9dec.MaxSegments]int8{-1, -1, -1, -1, -1, -1, -1, -1} {
		if e.roi.refFrame[i] != want {
			t.Fatalf("refFrame[%d] = %d, want -1 (no override)",
				i, e.roi.refFrame[i])
		}
	}
	seg := e.roi.segmentationParams()
	for i := range vp9dec.MaxSegments {
		if seg.FeatureMask[i]&(1<<uint(vp9dec.SegLvlRefFrame)) != 0 {
			t.Fatalf("SegLvlRefFrame surfaced on segment %d with no override",
				i)
		}
		if seg.FeatureMask[i]&(1<<uint(vp9dec.SegLvlSkip)) != 0 {
			t.Fatalf("SegLvlSkip surfaced on segment %d with no override",
				i)
		}
	}
}

func TestVP9EncoderROIMapInterBlocksUseSegmentMap(t *testing.T) {
	const width, height = 16, 16
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keyPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 64, 128, 128))
	if err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	keyHeader, _ := parseVP9EncoderHeaderForTest(t, keyPacket)
	roi := ROIMap{
		Enabled:   true,
		Rows:      (height + 7) >> 3,
		Cols:      (width + 7) >> 3,
		SegmentID: []uint8{1, 0, 0, 0},
	}
	roi.DeltaQuantizer[1] = -10
	roi.DeltaLoopFilter[1] = 3
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	interPacket, err := e.Encode(newVP9YCbCrForTest(width, height, 180, 128, 128))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	var br vp9dec.BitReader
	br.Init(interPacket)
	header, err := vp9dec.ReadUncompressedHeader(&br, &keyHeader,
		func(uint8) (uint32, uint32) { return width, height })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if !header.Seg.Enabled || !header.Seg.UpdateMap || !header.Seg.UpdateData || header.Seg.AbsDelta {
		t.Fatalf("ROI segmentation flags = enabled:%t updateMap:%t updateData:%t absDelta:%t, want true true true false",
			header.Seg.Enabled, header.Seg.UpdateMap, header.Seg.UpdateData,
			header.Seg.AbsDelta)
	}
	if got, want := header.Seg.FeatureData[1][vp9dec.SegLvlAltQ], int16(-vp9PublicQuantizerToQIndex(10)); got != want {
		t.Fatalf("ROI header alt-q[1] = %d, want %d", got, want)
	}
	if got := header.Seg.FeatureData[1][vp9dec.SegLvlAltLf]; got != 3 {
		t.Fatalf("ROI header alt-lf[1] = %d, want 3", got)
	}
	if !vp9dec.SegFeatureActive(&header.Seg, 1, vp9dec.SegLvlAltQ) ||
		!vp9dec.SegFeatureActive(&header.Seg, 1, vp9dec.SegLvlAltLf) {
		t.Fatal("ROI segment 1 did not enable alt-q and alt-lf")
	}

	if len(e.miGrid) < 4 {
		t.Fatalf("miGrid len = %d, want at least 4", len(e.miGrid))
	}
	if e.miGrid[0].SegmentID != 1 {
		t.Fatalf("miGrid[0] segment = %d, want ROI segment 1", e.miGrid[0].SegmentID)
	}
	for _, idx := range []int{1, 2, 3} {
		if e.miGrid[idx].SegmentID != 0 {
			t.Fatalf("miGrid[%d] segment = %d, want ROI segment 0",
				idx, e.miGrid[idx].SegmentID)
		}
	}
}

func TestVP9EncoderROIMapPreservesNonzeroROIUnderActiveMap(t *testing.T) {
	const width, height = 16, 16
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if _, err := e.Encode(newVP9YCbCrForTest(width, height, 64, 128, 128)); err != nil {
		t.Fatalf("Encode key: %v", err)
	}
	roi := ROIMap{
		Enabled:   true,
		Rows:      (height + 7) >> 3,
		Cols:      (width + 7) >> 3,
		SegmentID: []uint8{1, 0, 0, 0},
	}
	roi.DeltaQuantizer[1] = -10
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap: %v", err)
	}
	if err := e.SetActiveMap([]uint8{0}, encoderMacroblockRows(height),
		encoderMacroblockCols(width)); err != nil {
		t.Fatalf("SetActiveMap: %v", err)
	}
	if _, err := e.Encode(newVP9YCbCrForTest(width, height, 180, 128, 128)); err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	if got := e.miGrid[0].SegmentID; got != 1 {
		t.Fatalf("inactive ROI segment = %d, want nonzero ROI segment 1 preserved", got)
	}
	for _, idx := range []int{1, 2, 3} {
		mi := e.miGrid[idx]
		if mi.SegmentID != vp9ActiveMapSegmentInactive || mi.Skip != 1 ||
			mi.Mode != common.ZeroMv ||
			mi.RefFrame != [2]int8{vp9dec.LastFrame, vp9dec.NoRefFrame} {
			t.Fatalf("inactive zero-ROI miGrid[%d] = seg:%d skip:%d mode:%d refs:%v, want active-map skip",
				idx, mi.SegmentID, mi.Skip, mi.Mode, mi.RefFrame)
		}
	}
}
