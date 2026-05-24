package encoder

import vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"

// equator360AQRateRatios mirrors libvpx's rate_ratio[] in
// vp9_aq_360.c. The same per-segment ratio array as the Variance AQ
// table; libvpx reuses it for the equator-biased segments.
var equator360AQRateRatios = [vp9dec.MaxSegments]struct {
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

// Equator360AQApplies reports whether the configured frame
// dimensions are plausibly a 360 (equirectangular) projection.
// Equirectangular 360 has a 2:1 aspect ratio (full sphere mapped
// to a rectangle). When the source aspect ratio is well outside
// that band the latitude-driven Q bias just adds noise to the
// polar SBs without recovering bits anywhere else, so we keep
// the segmentation inactive instead of hurting the rate-distortion
// curve. The 1.5:1 floor is wide enough to cover stereoscopic
// half-and-half 360 layouts while still rejecting square / 16:9 /
// 4:3 panel sources.
func Equator360AQApplies(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	// height*3 < width*2 ⇔ width/height > 1.5.
	if int64(height)*3 >= int64(width)*2 {
		return false
	}
	// The latitude band thresholds are derived as miRows/8 and
	// miRows/4. Below ~16 mi rows the floors collapse and the
	// segmentation degenerates into "every row is polar". Gate the
	// minimum height at 128 luma pixels (16 mi rows) so we never
	// reach that degenerate regime.
	if height < 128 {
		return false
	}
	return true
}

// Equator360AQSegmentID returns the segment index libvpx's
// vp9_360aq_segment_id assigns to the given mode-info row. Equatorial
// rows take segment 0 (no boost), the temperate band takes segment 1,
// and the polar caps take segment 2. The other segment slots remain
// unused; libvpx's reference implementation only fills 3 segments.
func Equator360AQSegmentID(miRow, miRows int) uint8 {
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

// Equator360AQSegmentationParams builds the per-segment AltQ deltas
// libvpx emits for AQ_360. The FeatureMask + FeatureData slots are
// populated on every frame so the encoder's local dequant table
// (built from this seg by SetupSegmentationDequant) matches the
// decoder's view, which inherits per-segment data from the most
// recent frame that set UpdateData=true. Only intra (or other
// refresh) frames toggle UpdateData on so the bitstream stays
// compact for the inter stream; inter frames carry the same
// deltas in the encoder's seg struct but skip retransmission.
//
// Without populating the feature data on inter frames the encoder
// would reconstruct using base-qindex dequant while the decoder
// dequantizes using the inherited per-segment deltas, drifting the
// loop filter state and tanking decoded PSNR. The fix mirrors
// libvpx's behavior where cm->seg is a persistent struct.
func Equator360AQSegmentationParams(baseQIndex int, intraFrame bool) vp9dec.SegmentationParams {
	seg := vp9dec.SegmentationParams{
		Enabled:   true,
		UpdateMap: true,
		AbsDelta:  false,
	}
	initSegmentationProbDefaults(&seg)
	for i, ratio := range equator360AQRateRatios {
		if ratio.num == ratio.den {
			continue
		}
		delta := ComputeQDeltaByRate(0, 255, false, baseQIndex,
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
	if intraFrame {
		seg.UpdateData = true
	}
	return seg
}
