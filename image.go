package govpx

// Image is an I420/YV12-style planar 8-bit 4:2:0 image.
//
// Plane slices may include stride padding. Width and Height define the visible
// image size, and callers must honor the per-plane strides.
type Image struct {
	// Width and Height are the visible luma dimensions in pixels.
	Width  int
	Height int

	// Y, U, and V hold visible plane data plus any caller-owned stride padding.
	// U and V use 4:2:0 chroma dimensions: (Width+1)/2 by (Height+1)/2.
	Y []byte
	U []byte
	V []byte

	// YStride, UStride, and VStride are bytes between adjacent rows in each
	// plane. They must be at least the visible width of their plane.
	YStride int
	UStride int
	VStride int
}

func (img Image) validForEncode(width int, height int) bool {
	if img.Width != width || img.Height != height {
		return false
	}
	if width <= 0 || height <= 0 {
		return false
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	if img.YStride < width || img.UStride < uvWidth || img.VStride < uvWidth {
		return false
	}
	if len(img.Y) < planeLen(img.YStride, height, width) {
		return false
	}
	if len(img.U) < planeLen(img.UStride, uvHeight, uvWidth) {
		return false
	}
	if len(img.V) < planeLen(img.VStride, uvHeight, uvWidth) {
		return false
	}
	return true
}

func planeLen(stride int, rows int, visibleWidth int) int {
	if rows <= 0 {
		return 0
	}
	return stride*(rows-1) + visibleWidth
}
