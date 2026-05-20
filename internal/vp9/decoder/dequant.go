package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// VP9 dequantizer init. Ported from libvpx v1.16.0
// vp9/common/vp9_quant_common.c (vp9_dc_quant, vp9_ac_quant,
// vp9_get_qindex) and vp9/decoder/vp9_decodeframe.c
// (setup_segmentation_dequant).

// BitDepth mirrors libvpx's vpx_bit_depth_t. The decoder picks the matching
// qlookup table based on this value via internal/vp9/common.
type BitDepth = common.BitDepth

const (
	BitDepth8  = common.Bits8
	BitDepth10 = common.Bits10
	BitDepth12 = common.Bits12
)

const (
	// MaxQ mirrors libvpx's MAXQ — the upper bound of the dequant
	// table index after qindex + delta saturation.
	MaxQ = common.MaxQ

	// SegmentAbsdata mirrors libvpx's SEGMENT_ABSDATA — when set on
	// SegmentationParams.AbsDelta the segment alt-Q overrides the
	// frame qindex instead of nudging it.
	SegmentAbsdata = 1
)

// VpxDcQuant mirrors libvpx's vp9_dc_quant. Returns the DC dequant
// for `qindex + delta` clamped to [0, MaxQ], picking the per-bit-depth
// dc_qlookup table.
func VpxDcQuant(qindex, delta int, bd BitDepth) int16 {
	return common.DcQuant(qindex, delta, bd)
}

// VpxAcQuant mirrors libvpx's vp9_ac_quant.
func VpxAcQuant(qindex, delta int, bd BitDepth) int16 {
	return common.AcQuant(qindex, delta, bd)
}

// GetSegmentQindex mirrors libvpx's vp9_get_qindex. When SEG_LVL_ALT_Q
// is active for the segment the segment data either replaces or
// offsets the base qindex (selected by seg.AbsDelta == SEGMENT_ABSDATA).
func GetSegmentQindex(seg *SegmentationParams, segID, baseQindex int) int {
	if !SegFeatureActive(seg, segID, SegLvlAltQ) {
		return baseQindex
	}
	data := int(GetSegData(seg, segID, SegLvlAltQ))
	if seg.AbsDelta {
		return common.ClampQIndex(data)
	}
	return common.ClampQIndex(baseQindex + data)
}

// DequantTables holds the y/uv per-segment dequant pairs the tile
// driver consults during reconstruct. Mirrors libvpx's
// VP9_COMMON.y_dequant / uv_dequant arrays — [MAX_SEGMENTS][2] each.
type DequantTables struct {
	Y  [MaxSegments][2]int16
	Uv [MaxSegments][2]int16
}

// SetupSegmentationDequantArgs bundles the per-frame inputs to
// SetupSegmentationDequant. Mirrors the cm->base_qindex /
// y_dc_delta_q / uv_dc_delta_q / uv_ac_delta_q quartet.
type SetupSegmentationDequantArgs struct {
	BaseQindex int
	YDcDeltaQ  int
	UvDcDeltaQ int
	UvAcDeltaQ int
	BitDepth   BitDepth
}

// SetupSegmentationDequant mirrors setup_segmentation_dequant. Builds
// the per-segment y/uv (DC, AC) dequant pairs. When segmentation is
// disabled only slot 0 is filled; libvpx leaves the rest as don't
// cares (we follow suit).
func SetupSegmentationDequant(seg *SegmentationParams, args SetupSegmentationDequantArgs, out *DequantTables) {
	if seg.Enabled {
		for i := range MaxSegments {
			qindex := GetSegmentQindex(seg, i, args.BaseQindex)
			out.Y[i][0] = VpxDcQuant(qindex, args.YDcDeltaQ, args.BitDepth)
			out.Y[i][1] = VpxAcQuant(qindex, 0, args.BitDepth)
			out.Uv[i][0] = VpxDcQuant(qindex, args.UvDcDeltaQ, args.BitDepth)
			out.Uv[i][1] = VpxAcQuant(qindex, args.UvAcDeltaQ, args.BitDepth)
		}
		return
	}
	q := args.BaseQindex
	out.Y[0][0] = VpxDcQuant(q, args.YDcDeltaQ, args.BitDepth)
	out.Y[0][1] = VpxAcQuant(q, 0, args.BitDepth)
	out.Uv[0][0] = VpxDcQuant(q, args.UvDcDeltaQ, args.BitDepth)
	out.Uv[0][1] = VpxAcQuant(q, args.UvAcDeltaQ, args.BitDepth)
}
