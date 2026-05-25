package govpx

import (
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
	"testing"
)

func TestROIMapDisableClearsRuntimeSegmentationPreserve(t *testing.T) {
	e := newTestEncoder(t)
	rows := geometry.MacroblockRows(e.opts.Height)
	cols := geometry.MacroblockCols(e.opts.Width)
	roi := ROIMap{
		Enabled:   true,
		Rows:      rows,
		Cols:      cols,
		SegmentID: make([]uint8, rows*cols),
	}
	roi.DeltaQuantizer[1] = -10
	for row := range rows {
		for col := range cols {
			if row == 0 || col == 0 || row == rows-1 || col == cols-1 {
				roi.SegmentID[row*cols+col] = 1
			}
		}
	}
	if err := e.SetROIMap(&roi); err != nil {
		t.Fatalf("SetROIMap(border1): %v", err)
	}
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	if !e.assignKeyFrameROISegments(rows, cols, modes) {
		t.Fatalf("assignKeyFrameROISegments failed")
	}
	e.rememberSegmentationConfig(e.roiSegmentationConfig())
	if err := e.SetRTCExternalRateControl(true); err != nil {
		t.Fatalf("SetRTCExternalRateControl(true): %v", err)
	}
	if !e.runtimePreserveSegmentation {
		t.Fatalf("runtimePreserveSegmentation = false, want true after ROI header")
	}
	if err := e.SetROIMap(nil); err != nil {
		t.Fatalf("SetROIMap(nil): %v", err)
	}
	if e.runtimePreserveSegmentation || e.runtimePreservedSegmentation.Enabled || e.segmentationHeaderEnabled {
		t.Fatalf("runtime segmentation preserve after ROI disable = preserve:%t preserved:%t header:%t, want all false",
			e.runtimePreserveSegmentation, e.runtimePreservedSegmentation.Enabled, e.segmentationHeaderEnabled)
	}
}
