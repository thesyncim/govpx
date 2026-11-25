// VP8 codec implementation aligned with libvpx v1.16.0.
package codec

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	"github.com/thesyncim/govpx/internal/vpx/geometry"
)

type roiMapState struct {
	enabled               bool
	updateMap             bool
	updateData            bool
	suppressCyclicRefresh bool
	rows                  int
	cols                  int
	segmentID             []uint8
	deltaQuantizer        [vp8common.MaxMBSegments]int8
	deltaLoopFilter       [vp8common.MaxMBSegments]int8
	staticThreshold       [vp8common.MaxMBSegments]int
}

func (r *roiMapState) disable() {
	r.enabled = false
	r.updateMap = false
	r.updateData = false
	r.rows = 0
	r.cols = 0
	r.segmentID = nil
	r.deltaQuantizer = [vp8common.MaxMBSegments]int8{}
	r.deltaLoopFilter = [vp8common.MaxMBSegments]int8{}
	r.staticThreshold = [vp8common.MaxMBSegments]int{}
}

func (r *roiMapState) reset() {
	r.disable()
	r.suppressCyclicRefresh = false
}

func (e *VP8Encoder) disableROIMap() {
	e.roi.disable()
	e.rememberSegmentationConfig(vp8enc.SegmentationConfig{})
	e.clearRuntimePreservedSegmentationHeader()
}

// SetROIMap installs a libvpx-style region-of-interest map. ROI segmentation
// applies to key and inter frames and takes precedence over cyclic refresh
// while enabled. Pass nil, Enabled=false, nil SegmentID, or all-zero
// DeltaQuantizer/DeltaLoopFilter/StaticThreshold values to disable ROI.
func (e *VP8Encoder) SetROIMap(m *ROIMap) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if m == nil || !m.Enabled || m.SegmentID == nil {
		e.disableROIMap()
		return nil
	}
	expectedRows := geometry.MacroblockRows(e.opts.Height)
	expectedCols := geometry.MacroblockCols(e.opts.Width)
	if m.Rows != expectedRows || m.Cols != expectedCols || len(m.SegmentID) < m.Rows*m.Cols {
		return ErrInvalidConfig
	}

	var next roiMapState
	next.rows = m.Rows
	next.cols = m.Cols
	for i := range vp8common.MaxMBSegments {
		dq := m.DeltaQuantizer[i]
		dlf := m.DeltaLoopFilter[i]
		st := m.StaticThreshold[i]
		if dq < -maxQuantizer || dq > maxQuantizer || dlf < -63 || dlf > 63 || st < 0 {
			return ErrInvalidConfig
		}
		next.deltaQuantizer[i] = int8(roiQuantizerDeltaToQIndex(dq))
		next.deltaLoopFilter[i] = int8(dlf)
		next.staticThreshold[i] = st
		if dq != 0 || dlf != 0 || st != 0 {
			next.enabled = true
		}
	}
	if !next.enabled {
		e.disableROIMap()
		return nil
	}
	count := m.Rows * m.Cols
	for _, segmentID := range m.SegmentID[:count] {
		if segmentID >= vp8common.MaxMBSegments {
			return ErrInvalidConfig
		}
	}
	if cap(e.roi.segmentID) < count {
		next.segmentID = make([]uint8, count)
	} else {
		next.segmentID = e.roi.segmentID[:count]
	}
	copy(next.segmentID, m.SegmentID[:count])
	next.updateMap = true
	next.updateData = true
	next.suppressCyclicRefresh = true
	for i := range vp8common.MaxMBSegments {
		e.segmentEncodeBreakout[i] = next.staticThreshold[i]
		if next.staticThreshold[i] != 0 {
			e.useROIStaticThreshold = true
		}
	}
	e.roi = next
	return nil
}

func (r *roiMapState) clearUpdateFlags() {
	r.updateMap = false
	r.updateData = false
}

func roiQuantizerDeltaToQIndex(delta int) int {
	if delta < 0 {
		return -vp8common.PublicQuantizerToQIndex(-delta)
	}
	return vp8common.PublicQuantizerToQIndex(delta)
}

func (e *VP8Encoder) roiSegmentationConfig() vp8enc.SegmentationConfig {
	if !e.roi.enabled {
		return vp8enc.SegmentationConfig{}
	}
	cfg := vp8enc.SegmentationConfig{
		Enabled:    true,
		UpdateMap:  e.roi.updateMap,
		UpdateData: e.roi.updateData,
	}
	for segment := range vp8common.MaxMBSegments {
		if delta := e.roi.deltaQuantizer[segment]; delta != 0 {
			cfg.FeatureEnabled[vp8common.MBLvlAltQ][segment] = true
			cfg.FeatureData[vp8common.MBLvlAltQ][segment] = delta
		}
		if delta := e.roi.deltaLoopFilter[segment]; delta != 0 {
			cfg.FeatureEnabled[vp8common.MBLvlAltLF][segment] = true
			cfg.FeatureData[vp8common.MBLvlAltLF][segment] = delta
		}
	}
	return cfg
}

func (e *VP8Encoder) interStaticThresholdForSegment(segmentID uint8) int {
	// libvpx encodeframe.c: when segmentation_enabled always uses
	// segment_encode_breakout[segment_id], regardless of use_roi_static_threshold.
	if e.roi.enabled && segmentID < vp8common.MaxMBSegments {
		return e.segmentEncodeBreakout[segmentID]
	}
	return e.opts.StaticThreshold
}

func (e *VP8Encoder) assignKeyFrameROISegments(rows int, cols int, modes []vp8enc.KeyFrameMacroblockMode) bool {
	count := rows * cols
	if !e.roi.enabled || e.roi.rows != rows || e.roi.cols != cols || len(e.roi.segmentID) < count || len(modes) < count {
		return false
	}
	// Sub-slice both inputs to len count so the compiler can elide the
	// per-iter IsInBounds on segs[i] and m[i].
	segs := e.roi.segmentID[:count:count]
	m := modes[:count:count]
	for i := range count {
		m[i].SegmentID = segs[i]
	}
	return true
}

func (e *VP8Encoder) assignInterFrameROISegments(rows int, cols int, modes []vp8enc.InterFrameMacroblockMode) bool {
	count := rows * cols
	if !e.roi.enabled || e.roi.rows != rows || e.roi.cols != cols || len(e.roi.segmentID) < count || len(modes) < count {
		return false
	}
	segs := e.roi.segmentID[:count:count]
	m := modes[:count:count]
	for i := range count {
		m[i].SegmentID = segs[i]
	}
	return true
}
