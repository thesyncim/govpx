package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// HeaderRenderSize returns the render dimensions signalled by the
// uncompressed header, falling back to coded dimensions when no render size was
// present.
func HeaderRenderSize(hdr *UncompressedHeader) (int, int) {
	if hdr == nil {
		return 0, 0
	}
	if hdr.Render.Width > 0 && hdr.Render.Height > 0 {
		return int(hdr.Render.Width), int(hdr.Render.Height)
	}
	return int(hdr.Width), int(hdr.Height)
}

// SupportedOutputFormat reports whether hdr describes an output format that
// the public decoder can currently publish.
func SupportedOutputFormat(hdr *UncompressedHeader) bool {
	if hdr.Profile != common.Profile0 ||
		hdr.BitDepthColor.BitDepth != Bits8 ||
		hdr.BitDepthColor.SubsamplingX != 1 ||
		hdr.BitDepthColor.SubsamplingY != 1 {
		return false
	}
	return true
}

// FrameRefSignBias converts the uncompressed-header reference sign-bias fields
// into the full VP9 reference-frame bias table.
func FrameRefSignBias(hdr *UncompressedHeader) [MaxRefFrames]uint8 {
	var signBias [MaxRefFrames]uint8
	for i := range common.RefsPerFrame {
		signBias[LastFrame+i] = hdr.InterRef.SignBias[i]
	}
	return signBias
}

// CompoundReferenceAllowedForHeader reports whether compound prediction is
// allowed for this uncompressed header.
func CompoundReferenceAllowedForHeader(hdr *UncompressedHeader) bool {
	if hdr.FrameType == common.KeyFrame || hdr.IntraOnly {
		return false
	}
	return CompoundReferenceAllowed(FrameRefSignBias(hdr))
}

// HeaderResetsPastIndependence reports whether this header resets state that
// depends on prior frames.
func HeaderResetsPastIndependence(hdr *UncompressedHeader) bool {
	return hdr != nil && (hdr.FrameType == common.KeyFrame ||
		hdr.IntraOnly || hdr.ErrorResilientMode)
}

// PartitionContextUpdateWidth returns the context update width used by libvpx
// for a partition whose half-size is measured in 8x8 mode-info units.
func PartitionContextUpdateWidth(halfBlock8x8 int) int {
	width := 2 * halfBlock8x8
	if width == 0 {
		return 1
	}
	return width
}

// PlaneEntropyLen returns the entropy-context length for miCount mode-info
// units at the plane subsampling factor.
func PlaneEntropyLen(miCount int, subsampling uint8) int {
	return (miCount * 2) >> subsampling
}

// TileOffset returns the tile boundary offset in mode-info units.
func TileOffset(idx, mis, log2 int) int {
	sbCols := common.AlignToSB(mis) >> common.MiBlockSizeLog2
	offset := ((idx * sbCols) >> uint(log2)) << common.MiBlockSizeLog2
	return min(offset, mis)
}
