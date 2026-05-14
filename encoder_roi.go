package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// ROIMap configures libvpx-style per-macroblock region-of-interest segments.
// SegmentID is a row-major rows*cols map of 16x16 macroblocks; each value must
// be in [0, 3]. DeltaQuantizer uses libvpx public quantizer deltas in [-63, 63]
// and is translated to VP8 qindex deltas internally. DeltaLoopFilter uses raw
// VP8 loop-filter deltas in [-63, 63]. StaticThreshold sets per-segment
// encode-breakout thresholds for inter frames.
type ROIMap struct {
	// Enabled turns the ROI map on. A nil *ROIMap, Enabled=false, nil
	// SegmentID, or an all-zero delta/threshold configuration disables ROI.
	Enabled bool
	// Rows is the number of macroblock rows in SegmentID.
	Rows int
	// Cols is the number of macroblock columns in SegmentID.
	Cols int
	// SegmentID contains one segment id per macroblock in row-major order.
	SegmentID []uint8
	// DeltaQuantizer contains per-segment public quantizer deltas.
	DeltaQuantizer [vp8common.MaxMBSegments]int
	// DeltaLoopFilter contains per-segment loop-filter deltas.
	DeltaLoopFilter [vp8common.MaxMBSegments]int
	// StaticThreshold contains per-segment static encode-breakout thresholds.
	StaticThreshold [vp8common.MaxMBSegments]int
}

type roiMapState struct {
	enabled                bool
	staticThresholdEnabled bool
	updateMap              bool
	updateData             bool
	suppressCyclicRefresh  bool
	rows                   int
	cols                   int
	segmentID              []uint8
	deltaQuantizer         [vp8common.MaxMBSegments]int8
	deltaLoopFilter        [vp8common.MaxMBSegments]int8
	staticThreshold        [vp8common.MaxMBSegments]int
}

func (r *roiMapState) disable() {
	r.enabled = false
	r.staticThresholdEnabled = false
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
	e.rtcExternalPreserveSegmentation = false
	e.rtcExternalPreservedSegmentation = vp8enc.SegmentationConfig{}
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
	expectedRows := encoderMacroblockRows(e.opts.Height)
	expectedCols := encoderMacroblockCols(e.opts.Width)
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
		if st != 0 {
			next.staticThresholdEnabled = true
		}
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
	e.roi = next
	return nil
}

func (r *roiMapState) clearUpdateFlags() {
	r.updateMap = false
	r.updateData = false
}

func roiQuantizerDeltaToQIndex(delta int) int {
	if delta < 0 {
		return -libvpxPublicQuantizerToQIndex(-delta)
	}
	return libvpxPublicQuantizerToQIndex(delta)
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
	if e.roi.enabled && e.roi.staticThresholdEnabled && segmentID < vp8common.MaxMBSegments {
		return e.roi.staticThreshold[segmentID]
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
