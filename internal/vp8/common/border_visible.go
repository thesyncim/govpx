package common

// Ported from libvpx v1.16.0 vpx_scale/generic/yv12extend.c
// (vp8_yv12_extend_frame_borders_c).

// ExtendBordersFromVisible matches libvpx vp8_yv12_extend_frame_borders_c
// which extends from `y_crop_width` / `y_crop_height` rather than the
// 16-aligned coded dimensions. The coded-but-padded region between the
// visible edge and the 16-aligned coded edge is OVERWRITTEN with the
// last visible sample, and the border on top of that carries the same
// visible-edge value.
//
// This matches the post-loop-filter `vp8_yv12_extend_frame_borders`
// state libvpx writes onto cm->frame_to_show (vp8/encoder/onyx_if.c
// line ~3212) before the reference rotation. Inter prediction taps on
// the padded right column / bottom row of a reference see the
// visible-edge value instead of the raw reconstruction sample.
//
// Currently exposed as a separate entry point (not wired into the
// reference rotation) because a global swap to this behavior interacts
// with the per-MB picker's reference reads in ways that surface
// regressions on previously-passing odd-axis fixtures (see
// TestOracleEncoderStreamByteParity 17x33/33x17 baselines). The helper
// stays in place so the localization of the libvpx-faithful behavior is
// preserved and can be wired in once the picker-side interaction is
// understood and fixed.
//
// On 16-aligned frames Visible == Coded and this collapses to the same
// result as ExtendBorders.
func (fb *FrameBuffer) ExtendBordersFromVisible() {
	if fb == nil {
		return
	}
	visibleWidth := fb.Img.Width
	visibleHeight := fb.Img.Height
	if visibleWidth <= 0 || visibleHeight <= 0 {
		fb.ExtendBorders()
		return
	}
	rightExt := fb.border + (fb.Img.CodedWidth - visibleWidth)
	bottomExt := fb.border + (fb.Img.CodedHeight - visibleHeight)
	extendPlane(
		fb.buf[fb.yPlaneOff:fb.uPlaneOff],
		fb.Img.YStride,
		visibleWidth,
		visibleHeight,
		fb.border,
		rightExt,
		fb.border,
		bottomExt,
	)

	uvBorder := (fb.border + 1) >> 1
	codedUVWidth := (fb.Img.CodedWidth + 1) >> 1
	codedUVHeight := (fb.Img.CodedHeight + 1) >> 1
	visibleUVWidth := (visibleWidth + 1) >> 1
	visibleUVHeight := (visibleHeight + 1) >> 1
	uvRightExt := uvBorder + (codedUVWidth - visibleUVWidth)
	uvBottomExt := uvBorder + (codedUVHeight - visibleUVHeight)
	extendPlane(
		fb.buf[fb.uPlaneOff:fb.vPlaneOff],
		fb.Img.UStride,
		visibleUVWidth,
		visibleUVHeight,
		uvBorder,
		uvRightExt,
		uvBorder,
		uvBottomExt,
	)
	extendPlane(
		fb.buf[fb.vPlaneOff:],
		fb.Img.VStride,
		visibleUVWidth,
		visibleUVHeight,
		uvBorder,
		uvRightExt,
		uvBorder,
		uvBottomExt,
	)
}
