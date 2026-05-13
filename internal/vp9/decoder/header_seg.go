package decoder

// VP9 segmentation parser. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c:setup_segmentation.

// Segmentation constants from vp9/common/vp9_seg_common.h.
const (
	MaxSegments     = 8
	SegTreeProbs    = MaxSegments - 1
	PredictionProbs = 3
	SegLvlMax       = 4
	MaxProb         = 255
)

// SegLvl* identify each per-segment feature slot. Matches
// SEG_LVL_FEATURES in libvpx.
const (
	SegLvlAltQ     = 0
	SegLvlAltLf    = 1
	SegLvlRefFrame = 2
	SegLvlSkip     = 3
)

// Maximum data magnitude per feature. Mirrors seg_feature_data_max in
// vp9/common/vp9_seg_common.c. MAXQ = 255, MAX_LOOP_FILTER = 63.
var segFeatureDataMax = [SegLvlMax]int{255, 63, 3, 0}

// Whether the per-segment data is signed. Mirrors
// seg_feature_data_signed in libvpx.
var segFeatureDataSigned = [SegLvlMax]bool{true, true, false, false}

// SegmentationParams holds the parsed segmentation state.
type SegmentationParams struct {
	Enabled        bool
	UpdateMap      bool
	UpdateData     bool
	AbsDelta       bool
	TemporalUpdate bool

	TreeProbs   [SegTreeProbs]uint8
	PredProbs   [PredictionProbs]uint8
	FeatureMask [MaxSegments]uint32
	FeatureData [MaxSegments][SegLvlMax]int16
}

// ReadSegmentation mirrors setup_segmentation. Caller passes an
// existing SegmentationParams; the parser preserves previous-frame
// state when the corresponding update flag is 0. The non-update path
// resets UpdateMap / UpdateData to false.
func ReadSegmentation(r *BitReader, seg *SegmentationParams) {
	seg.UpdateMap = false
	seg.UpdateData = false

	seg.Enabled = r.ReadBit() != 0
	if !seg.Enabled {
		return
	}

	// Map update.
	seg.UpdateMap = r.ReadBit() != 0
	if seg.UpdateMap {
		for i := range SegTreeProbs {
			if r.ReadBit() != 0 {
				seg.TreeProbs[i] = uint8(r.ReadLiteral(8))
			} else {
				seg.TreeProbs[i] = MaxProb
			}
		}
		seg.TemporalUpdate = r.ReadBit() != 0
		if seg.TemporalUpdate {
			for i := range PredictionProbs {
				if r.ReadBit() != 0 {
					seg.PredProbs[i] = uint8(r.ReadLiteral(8))
				} else {
					seg.PredProbs[i] = MaxProb
				}
			}
		} else {
			for i := range PredictionProbs {
				seg.PredProbs[i] = MaxProb
			}
		}
	}

	// Data update.
	seg.UpdateData = r.ReadBit() != 0
	if !seg.UpdateData {
		return
	}
	seg.AbsDelta = r.ReadBit() != 0

	// Reset feature mask + data — mirrors vp9_clearall_segfeatures.
	for i := range seg.FeatureMask {
		seg.FeatureMask[i] = 0
	}
	for i := range seg.FeatureData {
		for j := range seg.FeatureData[i] {
			seg.FeatureData[i][j] = 0
		}
	}

	for i := range MaxSegments {
		for j := range SegLvlMax {
			var data int
			if r.ReadBit() != 0 {
				seg.FeatureMask[i] |= 1 << uint(j)
				data = decodeUnsignedMax(r, segFeatureDataMax[j])
				if segFeatureDataSigned[j] {
					if r.ReadBit() != 0 {
						data = -data
					}
				}
			}
			seg.FeatureData[i][j] = int16(data)
		}
	}
}

// decodeUnsignedMax mirrors the static helper in libvpx — read
// get_unsigned_bits(max) bits, then saturate at max.
func decodeUnsignedMax(r *BitReader, max int) int {
	data := int(r.ReadLiteral(getUnsignedBits(max)))
	if data > max {
		return max
	}
	return data
}

// getUnsignedBits returns the number of bits needed to encode any
// value in [0, n]. Mirrors get_unsigned_bits in vp9_common.h.
func getUnsignedBits(n int) int {
	if n <= 0 {
		return 0
	}
	bits := 0
	for v := uint(n); v > 0; v >>= 1 {
		bits++
	}
	return bits
}
