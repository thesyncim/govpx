package govpx

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

type vp9ROIMapState struct {
	enabled         bool
	rows            int
	cols            int
	segmentID       []uint8
	deltaQuantizer  [vp9dec.MaxSegments]int16
	deltaLoopFilter [vp9dec.MaxSegments]int16
}

func (r *vp9ROIMapState) disable() {
	r.enabled = false
	r.rows = 0
	r.cols = 0
	if r.segmentID != nil {
		r.segmentID = r.segmentID[:0]
	}
	r.deltaQuantizer = [vp9dec.MaxSegments]int16{}
	r.deltaLoopFilter = [vp9dec.MaxSegments]int16{}
}

// SetROIMap installs a VP9 region-of-interest map for subsequent inter frames.
// VP9 ROI cells are 8x8 MI cells, so rows and cols must equal
// ceil(height/8) and ceil(width/8). Key frames ignore the ROI map. Pass nil,
// Enabled=false, nil SegmentID, or all-zero DeltaQuantizer/DeltaLoopFilter
// values to disable ROI. VP9 does not support ROI StaticThreshold through this
// control, so non-zero StaticThreshold values return ErrInvalidConfig.
func (e *VP9Encoder) SetROIMap(m *ROIMap) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if m == nil || !m.Enabled || m.SegmentID == nil {
		e.roi.disable()
		return nil
	}
	expectedRows := (e.opts.Height + 7) >> 3
	expectedCols := (e.opts.Width + 7) >> 3
	if m.Rows != expectedRows || m.Cols != expectedCols ||
		len(m.SegmentID) < m.Rows*m.Cols {
		return ErrInvalidConfig
	}

	var next vp9ROIMapState
	next.rows = m.Rows
	next.cols = m.Cols
	for i := range m.DeltaQuantizer {
		dq := m.DeltaQuantizer[i]
		dlf := m.DeltaLoopFilter[i]
		st := m.StaticThreshold[i]
		if dq < -maxQuantizer || dq > maxQuantizer ||
			dlf < -63 || dlf > 63 || st != 0 {
			return ErrInvalidConfig
		}
		next.deltaQuantizer[i] = int16(vp9ROIQuantizerDeltaToQIndex(dq))
		next.deltaLoopFilter[i] = int16(dlf)
		if dq != 0 || dlf != 0 {
			next.enabled = true
		}
	}
	if !next.enabled {
		e.roi.disable()
		return nil
	}

	count := m.Rows * m.Cols
	for _, segmentID := range m.SegmentID[:count] {
		if segmentID >= uint8(len(m.DeltaQuantizer)) {
			return ErrInvalidConfig
		}
	}
	if cap(e.roi.segmentID) < count {
		next.segmentID = make([]uint8, count)
	} else {
		next.segmentID = e.roi.segmentID[:count]
	}
	copy(next.segmentID, m.SegmentID[:count])
	e.roi = next
	return nil
}

func (r *vp9ROIMapState) segmentIDAt(miRow int, miCol int) (uint8, bool) {
	if r == nil || !r.enabled || miRow < 0 || miCol < 0 ||
		miRow >= r.rows || miCol >= r.cols || r.cols <= 0 {
		return 0, false
	}
	idx := miRow*r.cols + miCol
	if idx < 0 || idx >= len(r.segmentID) {
		return 0, false
	}
	segID := r.segmentID[idx]
	if segID >= vp9dec.MaxSegments {
		return 0, false
	}
	return segID, true
}

func (r *vp9ROIMapState) segmentationParams() vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:    true,
		UpdateMap:  true,
		UpdateData: true,
		AbsDelta:   false,
	}
	initVP9SegmentationProbDefaults(&seg)
	for i := range vp9dec.MaxSegments {
		if delta := r.deltaQuantizer[i]; delta != 0 {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
			seg.FeatureData[i][vp9dec.SegLvlAltQ] = delta
		}
		if delta := r.deltaLoopFilter[i]; delta != 0 {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltLf)
			seg.FeatureData[i][vp9dec.SegLvlAltLf] = delta
		}
	}
	return seg
}

func vp9ROIQuantizerDeltaToQIndex(delta int) int {
	if delta < 0 {
		return -vp9PublicQuantizerToQIndex(-delta)
	}
	return vp9PublicQuantizerToQIndex(delta)
}
