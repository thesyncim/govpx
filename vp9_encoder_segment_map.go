package govpx

import (
	"image"
	"math"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

func (e *VP9Encoder) vp9DynamicSegmentMapActive() bool {
	if e == nil {
		return false
	}
	if e.roi.enabled || e.activeMapEnabled {
		return true
	}
	if e.cyclicAQ.Enabled && e.cyclicAQ.Apply {
		return true
	}
	// Variance-AQ is suppressed in fixed-Q / pure-Q mode because the
	// rate controller cannot absorb its per-segment qindex shifts;
	// matching the suppression here keeps the segment-aware partition
	// splitter and segment-map writer from emitting per-block segment
	// IDs that the segmentation header would otherwise be carrying.
	if e.opts.AQMode == VP9AQVariance && !e.vp9VarianceAQRateControlFixedQ() {
		return true
	}
	return false
}

// vp9ActiveSegmentMapCodingChooser reports whether the segmentation
// map-coding chooser should run for this frame.
//
// libvpx invokes vp9_choose_segmap_coding_method unconditionally from
// encode_segmentation whenever seg->enabled && seg->update_map
// (vp9/encoder/vp9_bitstream.c:773). That includes cyclic-refresh, ROI,
// active-map, and every AQ mode that emits a per-block segment map.
// Gating it to the active-map / non-ROI case was a govpx-only narrowing
// that denied cyclic-AQ and ROI frames the cost-based temporal_update=1
// choice and forced them down the spatial-coding path even when the
// realized mi_grid would have predicted cheaper.
//
// vp9ChooseSegmentMapCodingMethod itself short-circuits when
// seg.UpdateMap is false, so widening this gate to any frame that may
// emit a dynamic segment map matches libvpx without false-positive
// counting work on plain-Q frames.
func (e *VP9Encoder) vp9ActiveSegmentMapCodingChooser() bool {
	if e == nil {
		return false
	}
	// All dynamic segment-map producers: ROI, active-map, cyclic-AQ
	// (when apply gate fires), variance-AQ (non-fixed-Q), plus the
	// AQ modes whose segmentationParams always set UpdateMap=true
	// (complexity, equator360, perceptual). Mirrors libvpx's blanket
	// chooser invocation in encode_segmentation.
	if e.vp9DynamicSegmentMapActive() {
		return true
	}
	switch e.opts.AQMode {
	case VP9AQComplexity, VP9AQEquator360, VP9AQPerceptual:
		return true
	}
	// User-configured static segmentation that still emits a map.
	if e.opts.Segmentation.Enabled && e.opts.Segmentation.UpdateMap {
		return true
	}
	return false
}

func (e *VP9Encoder) vp9StaticSegmentIDForMap() uint8 {
	if e != nil && e.opts.AQMode == VP9AQComplexity {
		return vp9ComplexityAQDefaultSegment
	}
	if e == nil || e.opts.Segmentation.SegmentID >= vp9dec.MaxSegments {
		return 0
	}
	return e.opts.Segmentation.SegmentID
}

func (e *VP9Encoder) vp9PartitionSegmentID(miRow int, miCol int,
	staticSegID uint8, img *image.YCbCr, inter *vp9InterEncodeState,
) uint8 {
	segID, ok := e.vp9DynamicSegmentID(miRow, miCol, img, inter)
	if ok {
		return segID
	}
	return staticSegID
}

func (e *VP9Encoder) vp9DynamicSegmentID(miRow int, miCol int,
	img *image.YCbCr, inter *vp9InterEncodeState,
) (uint8, bool) {
	if e == nil {
		return 0, false
	}
	if img == nil && inter != nil {
		img = inter.img
	}
	activeMapNeedsSegment := e.vp9ActiveMapInactiveNeedsSegment(inter, miRow, miCol)
	if segID, ok := e.roi.segmentIDAt(miRow, miCol); ok {
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if e.opts.AQMode == VP9AQVariance && !e.vp9VarianceAQRateControlFixedQ() {
		if segID, ok := e.vp9VarianceAQSegmentID(img, miRow, miCol); ok {
			if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
				return vp9ActiveMapSegmentInactive, true
			}
			return segID, true
		}
	}
	if e.opts.AQMode == VP9AQEquator360 && vp9Equator360AQApplies(e.opts.Width, e.opts.Height) {
		miRows := (e.opts.Height + 7) >> 3
		segID := vp9Equator360AQSegmentID(miRow, miRows)
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if e.opts.AQMode == VP9AQPerceptual && e.perceptualAQ.Ready {
		segID := e.perceptualAQ.SegmentIDForBlock(miRow, miCol)
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if e.cyclicAQ.Enabled && e.cyclicAQ.Apply {
		segID := e.cyclicAQ.SegmentID(miRow, miCol)
		if segID == vp9ActiveMapSegmentActive && activeMapNeedsSegment {
			return vp9ActiveMapSegmentInactive, true
		}
		return segID, true
	}
	if activeMapNeedsSegment {
		return vp9ActiveMapSegmentInactive, true
	}
	return 0, false
}

func (e *VP9Encoder) vp9ActiveMapInactiveNeedsSegment(inter *vp9InterEncodeState,
	miRow, miCol int,
) bool {
	if !e.vp9ActiveMapInactive(miRow, miCol) {
		return false
	}
	if inter == nil || inter.img == nil || !e.refFrames[vp9LastRefSlot].valid {
		return true
	}
	return !vp9SourceMatchesReferenceMI(inter.img, &e.refFrames[vp9LastRefSlot],
		miRow, miCol)
}

func (e *VP9Encoder) vp9VarianceAQSegmentID(img *image.YCbCr,
	miRow, miCol int,
) (uint8, bool) {
	if img == nil || miRow < 0 || miCol < 0 {
		return 0, false
	}
	src, stride, width, height := vp9EncoderSourcePlane(img, 0)
	if len(src) == 0 || stride <= 0 {
		return 0, false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 >= width || y0 >= height {
		return 0, false
	}
	w := min(common.MiSize, width-x0)
	h := min(common.MiSize, height-y0)
	if w <= 0 || h <= 0 {
		return 0, false
	}
	// libvpx's vp9_block_energy computes:
	//     energy = round(log(per_pixel_variance + 1.0)) - DEFAULT_E_MIDPOINT
	// where per_pixel_variance = (Σ(x - mean(x))²) / area and the
	// midpoint is 10.0. encoder.BlockSourceVariance128 already returns the
	// unscaled Σ(x - mean(x))² accumulator, so we divide by the area
	// here to land on the same per-pixel scale. The earlier port
	// multiplied the accumulator by 256 before dividing by area, which
	// inflated every block's energy by log(256) ≈ 5.5 and pinned
	// virtually all non-flat blocks at energy=1 (segment 4). That
	// caused the variance-AQ probe to penalise the textured half with
	// a +24 qindex delta while still over-spending the flat half at
	// segment 0 (delta ≈ -42 at qindex=64), tanking BD-rate by +77%.
	variance := encoder.BlockSourceVariance128(src, stride, x0, y0, w, h)
	scaled := variance / uint64(w*h)
	energy := int(math.Round(math.Log(float64(scaled)+1.0))) - 10
	if energy < -4 {
		energy = -4
	} else if energy > 1 {
		energy = 1
	}
	return vp9VarianceAQSegmentIDFromEnergy(energy), true
}

func (e *VP9Encoder) applyVP9ComplexityAQSegment(inter *vp9InterEncodeState,
	miRow, miCol int, bsize common.BlockSize, mi *vp9dec.NeighborMi,
	projectedRate int,
) {
	if e == nil || inter == nil || mi == nil ||
		e.opts.AQMode != VP9AQComplexity {
		return
	}
	if e.vp9ActiveMapInactive(miRow, miCol) {
		if e.vp9ActiveMapInactiveNeedsSegment(inter, miRow, miCol) {
			mi.SegmentID = vp9ActiveMapSegmentInactive
			mi.SegIDPredicted = 0
		}
		return
	}
	segID, ok := e.vp9ComplexityAQSegmentID(inter.img, miRow, miCol, bsize,
		projectedRate)
	if !ok {
		return
	}
	mi.SegmentID = segID
	mi.SegIDPredicted = 0
}

func (e *VP9Encoder) vp9ComplexityAQSegmentID(img *image.YCbCr,
	miRow, miCol int, bsize common.BlockSize, projectedRate int,
) (uint8, bool) {
	if e == nil || img == nil || miRow < 0 || miCol < 0 ||
		bsize >= common.BlockSizes {
		return 0, false
	}
	sb64TargetRate := e.vp9ComplexityAQSB64TargetRate()
	if sb64TargetRate < vp9ComplexityAQMinSB64TargetRate {
		return 0, false
	}
	src, stride, width, height := vp9EncoderSourcePlane(img, 0)
	if len(src) == 0 || stride <= 0 {
		return 0, false
	}
	x0 := miCol * common.MiSize
	y0 := miRow * common.MiSize
	if x0 >= width || y0 >= height {
		return 0, false
	}
	blockW := int(common.Num4x4BlocksWideLookup[bsize]) * 4
	blockH := int(common.Num4x4BlocksHighLookup[bsize]) * 4
	w := min(blockW, width-x0)
	h := min(blockH, height-y0)
	if w <= 0 || h <= 0 {
		return 0, false
	}
	variance := encoder.BlockSourceVariance128(src, stride, x0, y0, w, h)
	logVar := math.Log(float64(variance) + 1.0)
	xmis := min(e.vp9MiCols(), miCol+int(common.Num8x8BlocksWideLookup[bsize])) - miCol
	ymis := min(e.vp9MiRows(), miRow+int(common.Num8x8BlocksHighLookup[bsize])) - miRow
	if xmis <= 0 || ymis <= 0 {
		return 0, false
	}
	targetRate := int((int64(sb64TargetRate) * int64(xmis) *
		int64(ymis) * 256) / (8 * 8))
	if targetRate <= 0 {
		return 0, false
	}
	if projectedRate < 0 {
		projectedRate = 0
	}
	strength := vp9ComplexityAQStrength(e.vp9EncoderModeDecisionQIndex())
	for i, transition := range vp9ComplexityAQTransitions[strength] {
		if int64(projectedRate)*int64(transition.den) <
			int64(targetRate)*int64(transition.num) &&
			logVar < vp9ComplexityAQLowVarThreshold+
				vp9ComplexityAQVarThresholds[strength][i] {
			return uint8(i), true
		}
	}
	return vp9ComplexityAQSegments - 1, true
}

func (e *VP9Encoder) vp9ComplexityAQSB64TargetRate() int {
	if e == nil || e.rc.frameTargetBits <= 0 {
		return 0
	}
	sbCols := (e.vp9MiCols() + 7) >> 3
	sbRows := (e.vp9MiRows() + 7) >> 3
	sbCount := sbCols * sbRows
	if sbCount <= 0 {
		return 0
	}
	return e.rc.frameTargetBits / sbCount
}

func (e *VP9Encoder) vp9MiRows() int {
	if e == nil || e.opts.Height <= 0 {
		return 0
	}
	return (e.opts.Height + 7) >> 3
}

func (e *VP9Encoder) vp9MiCols() int {
	if e == nil || e.opts.Width <= 0 {
		return 0
	}
	return (e.opts.Width + 7) >> 3
}

func vp9VarianceAQSegmentIDFromEnergy(energy int) uint8 {
	switch {
	case energy <= -4:
		return 0
	case energy <= -3:
		return 1
	case energy <= -2:
		return 1
	case energy <= -1:
		return 2
	case energy <= 0:
		return 3
	default:
		return 4
	}
}

func vp9SourceMatchesReferenceMI(src *image.YCbCr, ref *vp9ReferenceFrame,
	miRow, miCol int,
) bool {
	if src == nil || ref == nil || !ref.valid {
		return false
	}
	for plane := range vp9dec.MaxMbPlane {
		srcPixels, srcStride, srcW, srcH := vp9EncoderSourcePlane(src, plane)
		refPixels, refStride, refW, refH := vp9ReferenceVisiblePlane(ref, plane)
		if len(srcPixels) == 0 || len(refPixels) == 0 ||
			srcStride <= 0 || refStride <= 0 {
			return false
		}
		subsampling := 0
		if plane > 0 {
			subsampling = 1
		}
		x0 := (miCol * common.MiSize) >> subsampling
		y0 := (miRow * common.MiSize) >> subsampling
		w := common.MiSize >> subsampling
		h := common.MiSize >> subsampling
		if x0 < 0 || y0 < 0 || x0 >= srcW || y0 >= srcH ||
			x0 >= refW || y0 >= refH {
			return false
		}
		if w > srcW-x0 {
			w = srcW - x0
		}
		if w > refW-x0 {
			w = refW - x0
		}
		if h > srcH-y0 {
			h = srcH - y0
		}
		if h > refH-y0 {
			h = refH - y0
		}
		if w <= 0 || h <= 0 {
			return false
		}
		for y := 0; y < h; y++ {
			srcRow := srcPixels[(y0+y)*srcStride+x0:]
			refRow := refPixels[(y0+y)*refStride+x0:]
			for x := 0; x < w; x++ {
				if srcRow[x] != refRow[x] {
					return false
				}
			}
		}
	}
	return true
}

func (e *VP9Encoder) vp9ActiveMapInactive(miRow int, miCol int) bool {
	if e == nil || !e.activeMapEnabled || miRow < 0 || miCol < 0 ||
		miRow >= e.activeMapMiRows || miCol >= e.activeMapMiCols ||
		e.activeMapMiCols <= 0 {
		return false
	}
	idx := miRow*e.activeMapMiCols + miCol
	return idx >= 0 && idx < len(e.activeMap) &&
		e.activeMap[idx] == vp9ActiveMapSegmentInactive
}

func vp9EncoderForcedSegmentRefFrame(seg *vp9dec.SegmentationParams, segID int) (int8, bool) {
	if !vp9dec.SegFeatureActive(seg, segID, vp9dec.SegLvlRefFrame) {
		return 0, false
	}
	ref := int8(vp9dec.GetSegData(seg, segID, vp9dec.SegLvlRefFrame))
	if ref < vp9dec.IntraFrame || ref > vp9dec.AltrefFrame {
		return 0, false
	}
	return ref, true
}

func vp9EncoderMiSegmentID(mi *vp9dec.NeighborMi) int {
	if mi == nil || mi.SegmentID >= vp9dec.MaxSegments {
		return 0
	}
	return int(mi.SegmentID)
}
