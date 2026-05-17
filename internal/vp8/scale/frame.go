package scale

// Ported from libvpx v1.16.0 vpx_scale/generic/vpx_scale.c (vpx_scale_frame)
// and vpx_scale/yv12config.h (YV12_BUFFER_CONFIG slot layout).

// Frame describes a planar YV12 buffer view that ScaleFrame operates on.
// Mirrors the slots ScaleFrame reads from libvpx's YV12_BUFFER_CONFIG
// (vpx_scale/yv12config.h). All slices are aliased into the caller's
// frame buffers; no allocation is performed.
type Frame struct {
	Y, U, V []byte

	// YStride and UVStride are the row pitches in bytes for the Y and
	// UV planes respectively.
	YStride  int
	UVStride int

	// YWidth and YHeight are the visible dimensions of the luma plane.
	// UV dimensions are derived as (YWidth+1)/2 and (YHeight+1)/2 per
	// YV12 / VP8 4:2:0 subsampling.
	YWidth, YHeight int
}

// uvDim returns the chroma dimension for a given luma dimension under
// 4:2:0 subsampling, matching libvpx YV12_BUFFER_CONFIG's uv_width /
// uv_height which are stored as y_dim / 2 with no rounding (libvpx
// only allocates buffers for even luma dimensions on the encoder
// path).
func uvDim(yDim int) int { return yDim / 2 }

// ScaleFrame ports libvpx's vpx_scale_frame
// (vpx_scale/generic/vpx_scale.c:477-531). Resamples src into dst using
// (hscale, hratio, vscale, vratio) ratios, with the right and bottom
// borders of dst replicated from the last produced column / row when
// the scaled output is smaller than dst's allocated dimensions.
//
// tempArea must be at least (max(sourceBandHeight) + 1) * dst.YStride
// bytes. interlaced selects scale1d_2t1_ps over scale1d_2t1_i for the
// vertical band 2:1 path (currently only consulted by the 1:2 mode).
func ScaleFrame(src, dst *Frame, tempArea []byte, tempHeight int,
	hscale, hratio, vscale, vratio int,
	interlaced bool,
) {
	dw := (hscale - 1 + src.YWidth*hratio) / hscale
	dh := (vscale - 1 + src.YHeight*vratio) / vscale

	Scale2D(src.Y, src.YStride, src.YWidth, src.YHeight,
		dst.Y, dst.YStride, dw, dh,
		tempArea, tempHeight,
		hscale, hratio, vscale, vratio, interlaced)

	// Right border replication for Y. libvpx fills (dst.YWidth - dw + 1)
	// bytes starting one byte before the last written column with the
	// pixel two columns before it. The "+1" overlaps the last column
	// with the previously-written value; mirror verbatim.
	if dw < dst.YWidth {
		for i := range dh {
			row := dst.Y[i*dst.YStride:]
			fill := row[dw-2]
			for k := dw - 1; k < dst.YWidth; k++ {
				row[k] = fill
			}
		}
	}

	// Bottom border replication for Y. libvpx copies row (dh-2) into
	// every row from (dh-1) to dst.YHeight-1, copying dst.YWidth + 1
	// bytes per row. We replicate the +1 by copying dst.YWidth bytes
	// (the +1 overlaps a stride byte that is already replicated in
	// libvpx's allocation; harmless if dst is stride-aligned).
	if dh < dst.YHeight {
		srcRow := dst.Y[(dh-2)*dst.YStride : (dh-2)*dst.YStride+dst.YWidth]
		for i := dh - 1; i < dst.YHeight; i++ {
			copy(dst.Y[i*dst.YStride:i*dst.YStride+dst.YWidth], srcRow)
		}
	}

	scaleAndExtendUV(src.U, dst.U, src.YWidth, src.YHeight, dst.YHeight, src.UVStride, dst.UVStride, dw, dh, tempArea, tempHeight, hscale, hratio, vscale, vratio, interlaced)
	scaleAndExtendUV(src.V, dst.V, src.YWidth, src.YHeight, dst.YHeight, src.UVStride, dst.UVStride, dw, dh, tempArea, tempHeight, hscale, hratio, vscale, vratio, interlaced)
}

// scaleAndExtendUV mirrors libvpx's per-UV-plane Scale2D + right/bottom
// border replication (vpx_scale/generic/vpx_scale.c:501-531). U and V
// share the same routine; called twice from ScaleFrame.
func scaleAndExtendUV(srcPlane, dstPlane []byte,
	srcYWidth, srcYHeight, dstYHeight int,
	srcUVStride, dstUVStride int,
	dw, dh int, tempArea []byte, tempHeight int,
	hscale, hratio, vscale, vratio int, interlaced bool,
) {
	uvSrcW := uvDim(srcYWidth)
	uvSrcH := uvDim(srcYHeight)
	uvDstW := uvDim(dstYHeight) // placeholder; replaced below with derived dim
	_ = uvDstW

	uvDstWidth := uvDim(2 * (dw / 2)) // libvpx passes dw/2 as dst width
	uvDstHeight := uvDim(2 * (dh / 2))

	Scale2D(srcPlane, srcUVStride, uvSrcW, uvSrcH,
		dstPlane, dstUVStride, dw/2, dh/2,
		tempArea, tempHeight,
		hscale, hratio, vscale, vratio, interlaced)

	if dw/2 < uvDstWidth {
		for i := 0; i < uvDim(dstYHeight); i++ {
			row := dstPlane[i*dstUVStride:]
			fill := row[dw/2-2]
			for k := dw/2 - 1; k < uvDstWidth; k++ {
				row[k] = fill
			}
		}
	}

	if dh/2 < uvDstHeight {
		srcRow := dstPlane[(dh/2-2)*dstUVStride : (dh/2-2)*dstUVStride+uvDstWidth]
		for i := dh/2 - 1; i < uvDim(dstYHeight); i++ {
			copy(dstPlane[i*dstUVStride:i*dstUVStride+uvDstWidth], srcRow)
		}
	}
}
