package govpx

import (
	vp8analysis "github.com/thesyncim/govpx/internal/vp8/analysis"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// ROIMap configures libvpx-style region-of-interest segments. SegmentID is a
// row-major rows*cols map with codec-specific cell size: VP8 uses 16x16
// macroblocks and VP9 uses 8x8 MI cells. Each value must be in [0, 3] for this
// public type. DeltaQuantizer uses libvpx public quantizer deltas in [-63, 63]
// and is translated to codec qindex deltas internally. DeltaLoopFilter uses raw
// loop-filter deltas in [-63, 63]. StaticThreshold sets VP8 per-segment
// encode-breakout thresholds for inter frames; VP9 rejects non-zero
// StaticThreshold values.
//
// Skip and RefFrame are VP9-only extensions (libvpx vp9_set_roi_map at
// vp9/encoder/vp9_encoder.c:693).  Skip[i] forces segment i to use the
// SegLvlSkip feature when nonzero (0 disables, 1 enables); the rest of the
// libvpx skip_range (always 1) is rejected.  RefFrame[i] in [0, 3]
// activates SegLvlRefFrame for segment i with the libvpx semantics
// (0=intra, 1=LAST, 2=GOLDEN, 3=ALTREF); RefFrame[i] == -1 disables the
// override.  VP8 ignores both arrays.
type ROIMap struct {
	// Enabled turns the ROI map on. A nil *ROIMap, Enabled=false, nil
	// SegmentID, or an all-zero delta/threshold configuration disables ROI.
	Enabled bool
	// Rows is the number of codec ROI rows in SegmentID.
	Rows int
	// Cols is the number of codec ROI columns in SegmentID.
	Cols int
	// SegmentID contains one segment id per codec ROI cell in row-major order.
	SegmentID []uint8
	// DeltaQuantizer contains per-segment public quantizer deltas.
	DeltaQuantizer [vp8common.MaxMBSegments]int
	// DeltaLoopFilter contains per-segment loop-filter deltas.
	DeltaLoopFilter [vp8common.MaxMBSegments]int
	// StaticThreshold contains per-segment static encode-breakout thresholds.
	StaticThreshold [vp8common.MaxMBSegments]int
	// Skip (VP9-only) flags per-segment skip override; 1 forces skip, 0
	// disables.  libvpx skip[] range is [0, 1].
	Skip [vp8common.MaxMBSegments]int
	// RefFrame (VP9-only) overrides the per-segment reference frame.  -1
	// disables the override; 0..3 select intra/last/golden/altref per
	// libvpx ref_frame_range == 3.  Initialize to all -1 to disable.
	RefFrame [vp8common.MaxMBSegments]int
}

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
	// libvpx encodeframe.c: when segmentation_enabled always uses
	// segment_encode_breakout[segment_id], regardless of use_roi_static_threshold.
	if e.roi.enabled && segmentID < vp8common.MaxMBSegments {
		return e.segmentEncodeBreakout[segmentID]
	}
	return e.opts.StaticThreshold
}

// interStaticThresholdForSegmentMB is the per-MB hint-aware variant of
// interStaticThresholdForSegment. When the GPU analyzer has flagged
// this MB as [vp8analysis.FlagSkipLikely] AND the caller opted into
// VP8AnalysisConfig.UseEncodeHints, the threshold is inflated to a
// value larger than any plausible MB SSE so that
// staticInterRDEncodeBreakoutDistortion / staticInterFastEncodeBreakout
// take the skip path for that MB. This is the second documented
// hint-driven optimization (see docs/vp8_gpu_hint_consumption.md #3):
// route hint-flagged MBs into the encoder's existing
// static-encode-breakout path so transform / quantize / tokenize are
// skipped, not just mode decision.
//
// On the canonical path (UseEncodeHints=false) the function returns
// the same value as interStaticThresholdForSegment with one extra
// branch — no heap, no atomic, no GPU cost.
func (e *VP8Encoder) interStaticThresholdForSegmentMB(segmentID uint8, mbRow, mbCol, mbCols int) int {
	base := e.interStaticThresholdForSegment(segmentID)
	if !e.opts.Analysis.UseEncodeHints || e.analyzer == nil {
		return base
	}
	fa := &e.analysisOutput
	if !fa.Observed || fa.MBCols != mbCols {
		return base
	}
	idx := mbRow*mbCols + mbCol
	if idx < 0 || idx >= len(fa.MB) {
		return base
	}
	if fa.MB[idx].Flags&vp8analysis.FlagSkipLikely == 0 {
		return base
	}
	// Inflate to a value larger than any plausible 16x16 SSE
	// (max 16*16*255*255 < 2^26). 1<<28 leaves plenty of headroom
	// and is comfortably below max int on 64-bit.
	const hintSkipThreshold = 1 << 28
	e.hintForceSkipCount++
	return hintSkipThreshold
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
