package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

type vp9ROIMapState struct {
	enabled         bool
	rows            int
	cols            int
	segmentID       []uint8
	deltaQuantizer  [vp9dec.MaxSegments]int16
	deltaLoopFilter [vp9dec.MaxSegments]int16
	// skip mirrors libvpx's vpx_roi_map_t.skip[8] (vp9_encoder.h).  Each
	// entry is 0 or 1; 1 activates SegLvlSkip on that segment.
	skip [vp9dec.MaxSegments]int8
	// refFrame mirrors libvpx's vpx_roi_map_t.ref_frame[8].  -1 disables
	// the override; 0..3 select intra/last/golden/altref.
	refFrame [vp9dec.MaxSegments]int8
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
	r.skip = [vp9dec.MaxSegments]int8{}
	for i := range r.refFrame {
		r.refFrame[i] = -1
	}
}

// SetROIMap installs a VP9 region-of-interest map for subsequent inter frames.
// VP9 ROI cells are 8x8 MI cells, so rows and cols must equal
// ceil(height/8) and ceil(width/8). Key frames ignore the ROI map. Pass nil,
// Enabled=false, nil SegmentID, or an all-zero/no-override field set to
// disable ROI. VP9 does not support ROI StaticThreshold through this control,
// so non-zero StaticThreshold values return ErrInvalidConfig.
//
// Mirrors libvpx's vp9_set_roi_map (vp9/encoder/vp9_encoder.c:693): the
// delta_q/delta_lf range is [-63, 63] and skip is in [0, 1].  The
// ref_frame[] dimension is [-1, 3] in libvpx, but Go's zero value 0 also
// means "no override" here to keep the existing govpx ROIMap callers (which
// never initialised RefFrame) green; ref_frame values of -1 or 0 disable
// the override and 1..3 select LAST/GOLDEN/ALTREF.  Callers that want to
// force intra prediction per segment must use govpx segmentation directly.
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
	// libvpx's ref_frame[] convention uses -1 as the "no override"
	// sentinel; govpx accepts both -1 and 0 because Go's zero value is 0
	// (existing ROIMap callers leave the array unset and expect ROI to
	// stay off in that slot).  The state mirror stores -1 for "no
	// override" to keep the encode-time check uniform.
	for i := range next.refFrame {
		next.refFrame[i] = -1
	}
	for i := range m.DeltaQuantizer {
		dq := m.DeltaQuantizer[i]
		dlf := m.DeltaLoopFilter[i]
		st := m.StaticThreshold[i]
		skip := m.Skip[i]
		ref := m.RefFrame[i]
		// libvpx ranges: delta_q/delta_lf ±63, skip [0,1], ref_frame
		// [-1, 3] (vp9_encoder.c:699-704).  govpx widens the lower
		// ref_frame bound to 0 (treated as -1 below).
		if dq < -encoder.MaxPublicQuantizer ||
			dq > encoder.MaxPublicQuantizer ||
			dlf < -63 || dlf > 63 || st != 0 ||
			skip < 0 || skip > 1 ||
			ref < -1 || ref > 3 {
			return ErrInvalidConfig
		}
		next.deltaQuantizer[i] = int16(vp9ROIQuantizerDeltaToQIndex(dq))
		next.deltaLoopFilter[i] = int16(dlf)
		next.skip[i] = int8(skip)
		refOverride := ref >= 1
		if refOverride {
			next.refFrame[i] = int8(ref)
		}
		if dq != 0 || dlf != 0 || skip != 0 || refOverride {
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
	next.segmentID = buffers.EnsureLen(e.roi.segmentID, count)
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
		// libvpx vp9_set_roi_map writes the skip and ref-frame overrides
		// directly into the per-segment feature mask/data the same way
		// alt-q and alt-lf are written above (vp9/encoder/vp9_encoder.c:
		// 693-740 then carried to common/vp9_seg_common.h's per-segment
		// SegLvl* indexing).  SegLvlSkip's feature_data is unused so it
		// stays at zero; SegLvlRefFrame stores the reference index in
		// feature_data (vp9_seg_common.h::seg_feature_data_signed: only
		// AltQ/AltLf are signed, RefFrame/Skip are unsigned).
		if r.skip[i] != 0 {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlSkip)
		}
		if ref := r.refFrame[i]; ref >= 0 {
			seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlRefFrame)
			seg.FeatureData[i][vp9dec.SegLvlRefFrame] = int16(ref)
		}
	}
	return seg
}

func vp9ROIQuantizerDeltaToQIndex(delta int) int {
	if delta < 0 {
		return -encoder.PublicQuantizerToQIndex(-delta)
	}
	return encoder.PublicQuantizerToQIndex(delta)
}
