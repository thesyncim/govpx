package govpx

import (
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9Equator360AQRateRatios mirrors libvpx's rate_ratio[] in
// vp9_aq_360.c. The same per-segment ratio array as the Variance AQ
// table; libvpx reuses it for the equator-biased segments.
var vp9Equator360AQRateRatios = [vp9dec.MaxSegments]struct {
	num int
	den int
}{
	{1, 1},
	{3, 4},
	{3, 5},
	{1, 2},
	{2, 5},
	{3, 10},
	{1, 4},
	{1, 1},
}

// vp9Equator360AQSegmentID returns the segment index libvpx's
// vp9_360aq_segment_id assigns to the given mode-info row. Equatorial
// rows take segment 0 (no boost), the temperate band takes segment 1,
// and the polar caps take segment 2. The other segment slots remain
// unused; libvpx's reference implementation only fills 3 segments.
func vp9Equator360AQSegmentID(miRow, miRows int) uint8 {
	if miRow < 0 {
		miRow = 0
	}
	if miRows <= 0 {
		return 0
	}
	if miRow < miRows/8 || miRow > miRows-miRows/8 {
		return 2
	}
	if miRow < miRows/4 || miRow > miRows-miRows/4 {
		return 1
	}
	return 0
}

// vp9Equator360AQSegmentationParams builds the per-segment AltQ deltas
// libvpx emits for AQ_360. Intra frames refresh the segment data;
// inter frames inherit it via segmentation header inheritance, so we
// only emit UpdateData on intra or other refresh frames to mirror
// libvpx's frame_is_intra_only || force_update_segmentation gate.
func vp9Equator360AQSegmentationParams(baseQIndex int, intraFrame bool) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: true,
		AbsDelta:  false,
	}
	initVP9SegmentationProbDefaults(&seg)
	if !intraFrame {
		return seg
	}
	seg.UpdateData = true
	for i, ratio := range vp9Equator360AQRateRatios {
		if ratio.num == ratio.den {
			continue
		}
		delta := vp9ComputeQDeltaByRate(0, 255, false, baseQIndex,
			ratio.num, ratio.den)
		if baseQIndex != 0 && baseQIndex+delta == 0 {
			delta = -baseQIndex + 1
		}
		if delta < -255 {
			delta = -255
		} else if delta > 255 {
			delta = 255
		}
		seg.FeatureMask[i] |= 1 << uint(vp9dec.SegLvlAltQ)
		seg.FeatureData[i][vp9dec.SegLvlAltQ] = int16(delta)
	}
	return seg
}
